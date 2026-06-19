package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type Summarize struct{}

type summarizeParams struct {
	FieldsToSummarize      []summarizeField
	GroupByFields          []string
	OutputFormat           string
	OutputSingleItem       bool
	IgnoreBlankValues      bool
	ContinueIfFieldMissing bool
	SkipEmptySplitFields   bool
	DisableDotNotation     bool
}

type summarizeField struct {
	Aggregation     string
	Field           string
	NewFieldName    string
	IncludeEmpty    bool
	Separator       string
	CustomSeparator string
}

func (Summarize) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	items := cloneItems(firstInput(in.InputData))
	params := newSummarizeParams(in.Node.Parameters)
	if len(params.FieldsToSummarize) == 0 {
		return nil, fmt.Errorf("summarize: at least one field is required")
	}
	for _, field := range params.FieldsToSummarize {
		if field.Field == "" {
			return nil, fmt.Errorf("summarize: field is required")
		}
	}
	if err := validateSummarizeFields(items, params); err != nil {
		return nil, err
	}
	result, err := executeSummarize(items, params)
	if err != nil {
		return nil, err
	}
	return dataplane.MainOutput(result), nil
}

func newSummarizeParams(raw map[string]any) summarizeParams {
	options := mergeObject(raw["options"])
	outputFormat := firstNonEmptyNode(stringParam(options, "outputFormat"), stringParam(raw, "outputFormat"), "separateItems")
	params := summarizeParams{
		FieldsToSummarize:      parseSummarizeFields(raw["fieldsToSummarize"]),
		GroupByFields:          firstNonEmptyStringList(raw["fieldsToSplitBy"], options["groupByFields"], raw["groupByFields"]),
		OutputFormat:           outputFormat,
		OutputSingleItem:       boolParam(options, "outputSingleItem", boolParam(raw, "outputSingleItem", outputFormat == "singleItem")),
		IgnoreBlankValues:      boolParam(options, "ignoreBlankValues", boolParam(raw, "ignoreBlankValues", false)),
		ContinueIfFieldMissing: boolParam(options, "continueIfFieldMissing", boolParam(options, "continueIfFieldNotFound", boolParam(raw, "continueIfFieldMissing", false))),
		SkipEmptySplitFields:   boolParam(options, "skipEmptySplitFields", boolParam(raw, "skipEmptySplitFields", false)),
		DisableDotNotation:     boolParam(options, "disableDotNotation", boolParam(raw, "disableDotNotation", false)),
	}
	return params
}

func parseSummarizeFields(raw any) []summarizeField {
	if object, ok := rawObject(raw); ok {
		for _, key := range []string{"values", "fields", "field"} {
			if values, ok := object[key].([]any); ok {
				return parseSummarizeFieldList(values)
			}
		}
		if field := parseSummarizeField(object); field.Field != "" {
			return []summarizeField{field}
		}
	}
	if values, ok := raw.([]any); ok {
		return parseSummarizeFieldList(values)
	}
	return nil
}

func parseSummarizeFieldList(values []any) []summarizeField {
	fields := make([]summarizeField, 0, len(values))
	for _, value := range values {
		object, ok := rawObject(value)
		if !ok {
			continue
		}
		field := parseSummarizeField(object)
		if field.Field != "" {
			fields = append(fields, field)
		}
	}
	return fields
}

func parseSummarizeField(object map[string]any) summarizeField {
	separator := stringParam(object, "separateBy", "separator")
	if separator == "other" {
		separator = stringParam(object, "customSeparator")
	}
	if separator == "" {
		separator = ","
	}
	return summarizeField{
		Aggregation:     firstNonEmptyNode(stringParam(object, "aggregation", "operation"), "count"),
		Field:           stringParam(object, "field", "aggregateField", "fieldName"),
		NewFieldName:    stringParam(object, "newFieldName", "renameField", "outputFieldName"),
		IncludeEmpty:    boolParam(object, "includeEmpty", false),
		Separator:       separator,
		CustomSeparator: stringParam(object, "customSeparator"),
	}
}

func firstNonEmptyStringList(values ...any) []string {
	for _, value := range values {
		list := stringList(value)
		if len(list) > 0 {
			return list
		}
	}
	return nil
}

func validateSummarizeFields(items []dataplane.Item, params summarizeParams) error {
	if params.ContinueIfFieldMissing {
		return nil
	}
	for _, field := range params.FieldsToSummarize {
		found := false
		for _, item := range items {
			if summarizeValue(item.JSON, field.Field, params) != nil {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("summarize: field %s does not exist in any items", field.Field)
		}
	}
	return nil
}

func executeSummarize(items []dataplane.Item, params summarizeParams) ([]dataplane.Item, error) {
	if len(params.GroupByFields) == 0 {
		json, err := summarizeGroup(items, params)
		if err != nil {
			return nil, err
		}
		return []dataplane.Item{{JSON: json, PairedItem: summarizePairedItem(items)}}, nil
	}
	groups, order := summarizeGroups(items, params)
	if params.OutputSingleItem || params.OutputFormat == "singleItem" {
		return []dataplane.Item{{JSON: summarizeGroupsAsObject(groups, order, params)}}, nil
	}
	result := make([]dataplane.Item, 0, len(order))
	for _, key := range order {
		groupItems := groups[key]
		json, err := summarizeGroup(groupItems, params)
		if err != nil {
			return nil, err
		}
		for _, groupField := range params.GroupByFields {
			json[normalizeSummarizeFieldName(groupField)] = summarizeValue(groupItems[0].JSON, groupField, params)
		}
		result = append(result, dataplane.Item{JSON: json, PairedItem: summarizePairedItem(groupItems)})
	}
	return result, nil
}

func summarizeGroups(items []dataplane.Item, params summarizeParams) (map[string][]dataplane.Item, []string) {
	groups := map[string][]dataplane.Item{}
	order := []string{}
	for _, item := range items {
		parts := make([]string, 0, len(params.GroupByFields))
		skip := false
		for _, field := range params.GroupByFields {
			value := summarizeValue(item.JSON, field, params)
			if params.SkipEmptySplitFields && summarizeBlank(value) {
				skip = true
				break
			}
			parts = append(parts, summarizeKey(value))
		}
		if skip {
			continue
		}
		key := strings.Join(parts, "\x00")
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], item)
	}
	return groups, order
}

func summarizeGroupsAsObject(groups map[string][]dataplane.Item, order []string, params summarizeParams) map[string]any {
	result := map[string]any{}
	for _, key := range order {
		groupItems := groups[key]
		current := result
		for index, field := range params.GroupByFields {
			groupValue := fmt.Sprint(summarizeValue(groupItems[0].JSON, field, params))
			if index == len(params.GroupByFields)-1 {
				json, err := summarizeGroup(groupItems, params)
				if err == nil {
					current[groupValue] = json
				}
				continue
			}
			next, ok := current[groupValue].(map[string]any)
			if !ok {
				next = map[string]any{}
				current[groupValue] = next
			}
			current = next
		}
	}
	return result
}

func summarizeGroup(items []dataplane.Item, params summarizeParams) (map[string]any, error) {
	result := map[string]any{}
	for _, field := range params.FieldsToSummarize {
		values := summarizeValues(items, field, params)
		aggregated, err := applySummarizeAggregation(items, values, field)
		if err != nil {
			if params.ContinueIfFieldMissing {
				result[summarizeOutputName(field)] = nil
				continue
			}
			return nil, fmt.Errorf("summarize %s(%s): %w", field.Aggregation, field.Field, err)
		}
		result[summarizeOutputName(field)] = aggregated
	}
	return result, nil
}

func summarizeValues(items []dataplane.Item, field summarizeField, params summarizeParams) []any {
	values := make([]any, 0, len(items))
	for _, item := range items {
		value := summarizeValue(item.JSON, field.Field, params)
		if !field.IncludeEmpty && (params.IgnoreBlankValues || summarizeIgnoresBlank(field.Aggregation)) && summarizeBlank(value) {
			continue
		}
		values = append(values, value)
	}
	return values
}

func summarizeValue(data map[string]any, field string, params summarizeParams) any {
	if field == "*" {
		return data
	}
	if !params.DisableDotNotation && strings.Contains(field, ".") {
		return nestedMergeValue(data, field)
	}
	return data[field]
}

func applySummarizeAggregation(items []dataplane.Item, values []any, field summarizeField) (any, error) {
	switch strings.ToLower(field.Aggregation) {
	case "append", "list":
		return values, nil
	case "concatenate":
		return concatenateSummarizeValues(values, field.Separator), nil
	case "count":
		if field.Field == "*" {
			return len(items), nil
		}
		return len(values), nil
	case "countunique":
		return len(uniqueSummarizeValues(values)), nil
	case "listunique", "appendunique":
		return uniqueSummarizeValues(values), nil
	case "sum":
		return sumSummarizeValues(values)
	case "average", "mean":
		return averageSummarizeValues(values)
	case "min":
		return minMaxSummarizeValues(values, false), nil
	case "max":
		return minMaxSummarizeValues(values, true), nil
	case "counttruthy":
		return countTruthySummarizeValues(values), nil
	case "first":
		return firstSummarizeValue(values), nil
	case "last":
		return lastSummarizeValue(values), nil
	case "median":
		return medianSummarizeValues(values)
	case "variance":
		return varianceSummarizeValues(values)
	case "stddev", "standarddeviation":
		variance, err := varianceSummarizeValues(values)
		if err != nil {
			return nil, err
		}
		return math.Sqrt(variance), nil
	default:
		return nil, fmt.Errorf("unknown aggregation %s", field.Aggregation)
	}
}

func summarizeOutputName(field summarizeField) string {
	if field.NewFieldName != "" {
		return field.NewFieldName
	}
	prefix := map[string]string{
		"append":      "appended_",
		"average":     "average_",
		"concatenate": "concatenated_",
		"count":       "count_",
		"countunique": "unique_count_",
		"max":         "max_",
		"min":         "min_",
		"sum":         "sum_",
	}
	name := strings.ToLower(field.Aggregation)
	if value := prefix[name]; value != "" {
		return normalizeSummarizeFieldName(value + field.Field)
	}
	return normalizeSummarizeFieldName(name + "_" + field.Field)
}

func summarizeIgnoresBlank(aggregation string) bool {
	switch strings.ToLower(aggregation) {
	case "sum", "average", "mean", "min", "max", "countunique", "count", "append", "concatenate":
		return true
	default:
		return false
	}
}

func summarizeBlank(value any) bool {
	if value == nil {
		return true
	}
	if text, ok := value.(string); ok {
		return text == ""
	}
	return false
}

func sumSummarizeValues(values []any) (float64, error) {
	sum := float64(0)
	for _, value := range values {
		number, ok := floatIFValue(value)
		if !ok {
			return 0, fmt.Errorf("non numeric value %v", value)
		}
		sum += number
	}
	return sum, nil
}

func averageSummarizeValues(values []any) (float64, error) {
	if len(values) == 0 {
		return 0, nil
	}
	sum, err := sumSummarizeValues(values)
	if err != nil {
		return 0, err
	}
	return sum / float64(len(values)), nil
}

func minMaxSummarizeValues(values []any, maxValue bool) any {
	var best any
	for _, value := range values {
		if summarizeBlank(value) {
			continue
		}
		if best == nil {
			best = value
			continue
		}
		left, leftOK := floatIFValue(value)
		right, rightOK := floatIFValue(best)
		if leftOK && rightOK {
			if maxValue && left > right || !maxValue && left < right {
				best = value
			}
			continue
		}
		if maxValue && fmt.Sprint(value) > fmt.Sprint(best) || !maxValue && fmt.Sprint(value) < fmt.Sprint(best) {
			best = value
		}
	}
	return best
}

func countTruthySummarizeValues(values []any) int {
	count := 0
	for _, value := range values {
		truthy, ok := boolIFValue(value)
		if ok && truthy || !ok && !summarizeBlank(value) {
			count++
		}
	}
	return count
}

func firstSummarizeValue(values []any) any {
	for _, value := range values {
		if !summarizeBlank(value) {
			return value
		}
	}
	return nil
}

func lastSummarizeValue(values []any) any {
	for index := len(values) - 1; index >= 0; index-- {
		if !summarizeBlank(values[index]) {
			return values[index]
		}
	}
	return nil
}

func medianSummarizeValues(values []any) (float64, error) {
	numbers, err := summarizeNumbers(values)
	if err != nil {
		return 0, err
	}
	if len(numbers) == 0 {
		return 0, nil
	}
	sort.Float64s(numbers)
	middle := len(numbers) / 2
	if len(numbers)%2 == 0 {
		return (numbers[middle-1] + numbers[middle]) / 2, nil
	}
	return numbers[middle], nil
}

func varianceSummarizeValues(values []any) (float64, error) {
	numbers, err := summarizeNumbers(values)
	if err != nil {
		return 0, err
	}
	if len(numbers) == 0 {
		return 0, nil
	}
	sum := float64(0)
	for _, number := range numbers {
		sum += number
	}
	mean := sum / float64(len(numbers))
	squares := float64(0)
	for _, number := range numbers {
		diff := number - mean
		squares += diff * diff
	}
	return squares / float64(len(numbers)), nil
}

func summarizeNumbers(values []any) ([]float64, error) {
	numbers := []float64{}
	for _, value := range values {
		if summarizeBlank(value) {
			continue
		}
		number, ok := floatIFValue(value)
		if !ok {
			return nil, fmt.Errorf("non numeric value %v", value)
		}
		numbers = append(numbers, number)
	}
	return numbers, nil
}

func uniqueSummarizeValues(values []any) []any {
	seen := map[string]bool{}
	result := []any{}
	for _, value := range values {
		key := summarizeKey(value)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, value)
	}
	return result
}

func concatenateSummarizeValues(values []any, separator string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if object, ok := value.(map[string]any); ok {
			encoded, err := json.Marshal(object)
			if err == nil {
				parts = append(parts, string(encoded))
				continue
			}
		}
		if value == nil {
			parts = append(parts, "undefined")
			continue
		}
		parts = append(parts, fmt.Sprint(value))
	}
	return strings.Join(parts, separator)
}

func summarizeKey(value any) string {
	encoded, err := json.Marshal(value)
	if err == nil {
		return string(encoded)
	}
	return fmt.Sprintf("%#v", value)
}

func normalizeSummarizeFieldName(value string) string {
	replacer := strings.NewReplacer("[", "", "]", "", "\"", "", ".", "_", " ", "_")
	return replacer.Replace(value)
}

func summarizePairedItem(items []dataplane.Item) *dataplane.PairedItem {
	if len(items) == 0 {
		return nil
	}
	return &dataplane.PairedItem{Item: 0}
}
