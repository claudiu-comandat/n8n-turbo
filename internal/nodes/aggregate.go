package nodes

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type Aggregate struct{}

type aggregateField struct {
	Field        string
	Rename       string
	Aggregation  string
	IncludeEmpty bool
	Separator    string
}

type aggregateParams struct {
	Mode               string
	FieldToAggregate   string
	DestinationField   string
	KeepMissingValues  bool
	DisableDotNotation bool
	SortField          string
	SortOrder          string
	FieldsToAggregate  []aggregateField
}

func (Aggregate) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	params := parseAggregateParams(in.Node.Parameters)
	items := firstInput(in.InputData)
	switch strings.ToLower(params.Mode) {
	case "", "aggregateallitemdata", "all":
		return dataplane.MainOutput([]dataplane.Item{{JSON: aggregateAllItems(items, params)}}), nil
	case "aggregateindividualfields", "fields", "individual":
		json, err := aggregateIndividualFields(items, params)
		if err != nil {
			return nil, err
		}
		return dataplane.MainOutput([]dataplane.Item{{JSON: json}}), nil
	default:
		return nil, fmt.Errorf("unsupported aggregate mode %q", params.Mode)
	}
}

func parseAggregateParams(raw map[string]any) aggregateParams {
	options := mergeObject(raw["options"])
	params := aggregateParams{
		Mode:               firstNonEmptyNode(stringParam(raw, "aggregate"), stringParam(raw, "mode"), "aggregateAllItemData"),
		FieldToAggregate:   stringParam(raw, "fieldToAggregate", "fieldName", "field"),
		DestinationField:   stringParam(raw, "destinationFieldName", "outputFieldName"),
		KeepMissingValues:  boolParam(raw, "keepMissingValues", boolParam(options, "keepMissingValues", false)),
		DisableDotNotation: boolParam(options, "disableDotNotation", boolParam(raw, "disableDotNotation", false)),
		SortField:          firstNonEmptyNode(stringParam(options, "sortField"), stringParam(raw, "sortField")),
		SortOrder:          firstNonEmptyNode(stringParam(options, "sortOrder"), stringParam(raw, "sortOrder")),
	}
	params.FieldsToAggregate = parseAggregateFields(raw["fieldsToAggregate"])
	return params
}

func parseAggregateFields(raw any) []aggregateField {
	if object, ok := rawObject(raw); ok {
		for _, key := range []string{"values", "fields", "field"} {
			if values, ok := object[key].([]any); ok {
				return parseAggregateFieldList(values)
			}
		}
		if field := parseAggregateField(object); field.Field != "" {
			return []aggregateField{field}
		}
	}
	if values, ok := raw.([]any); ok {
		return parseAggregateFieldList(values)
	}
	return nil
}

func parseAggregateFieldList(values []any) []aggregateField {
	fields := make([]aggregateField, 0, len(values))
	for _, value := range values {
		object, ok := rawObject(value)
		if !ok {
			continue
		}
		field := parseAggregateField(object)
		if field.Field != "" {
			fields = append(fields, field)
		}
	}
	return fields
}

func parseAggregateField(object map[string]any) aggregateField {
	return aggregateField{
		Field:        stringParam(object, "aggregateField", "field", "fieldName"),
		Rename:       stringParam(object, "renameField", "outputFieldName"),
		Aggregation:  firstNonEmptyNode(stringParam(object, "aggregation", "operation"), "append"),
		IncludeEmpty: boolParam(object, "includeEmpty", false),
		Separator:    firstNonEmptyNode(stringParam(object, "separatorForConcatenate", "separator"), ","),
	}
}

func aggregateAllItems(items []dataplane.Item, params aggregateParams) map[string]any {
	values := make([]any, 0, len(items))
	for _, item := range sortedAggregateItems(items, params) {
		value := any(item.JSON)
		if params.FieldToAggregate != "" {
			value = aggregateFieldValue(item.JSON, params.FieldToAggregate, params.DisableDotNotation)
		}
		if value == nil && !params.KeepMissingValues {
			continue
		}
		values = append(values, deepCopySetValue(value))
	}
	field := params.DestinationField
	if field == "" {
		field = firstNonEmptyNode(params.FieldToAggregate, "data")
	}
	return map[string]any{field: values, "count": len(values)}
}

func aggregateIndividualFields(items []dataplane.Item, params aggregateParams) (map[string]any, error) {
	if len(params.FieldsToAggregate) == 0 {
		return nil, fmt.Errorf("fieldsToAggregate is required")
	}
	result := map[string]any{}
	for _, field := range params.FieldsToAggregate {
		values := collectAggregateValues(sortedAggregateItems(items, params), field, params)
		aggregated, err := applyAggregateOperation(values, field)
		if err != nil {
			return nil, fmt.Errorf("aggregate field %q: %w", field.Field, err)
		}
		output := firstNonEmptyNode(field.Rename, field.Field)
		result[output] = aggregated
	}
	return result, nil
}

func sortedAggregateItems(items []dataplane.Item, params aggregateParams) []dataplane.Item {
	if params.SortField == "" {
		return items
	}
	sorted := cloneItems(items)
	sort.SliceStable(sorted, func(i int, j int) bool {
		left := fmt.Sprint(aggregateFieldValue(sorted[i].JSON, params.SortField, params.DisableDotNotation))
		right := fmt.Sprint(aggregateFieldValue(sorted[j].JSON, params.SortField, params.DisableDotNotation))
		if strings.EqualFold(params.SortOrder, "descending") || strings.EqualFold(params.SortOrder, "desc") {
			return left > right
		}
		return left < right
	})
	return sorted
}

func collectAggregateValues(items []dataplane.Item, field aggregateField, params aggregateParams) []any {
	values := make([]any, 0, len(items))
	for _, item := range items {
		value := aggregateFieldValue(item.JSON, field.Field, params.DisableDotNotation)
		if emptyIFValue(value) && !field.IncludeEmpty && !params.KeepMissingValues {
			continue
		}
		values = append(values, value)
	}
	return values
}

func aggregateFieldValue(data map[string]any, field string, disableDotNotation bool) any {
	if !disableDotNotation && strings.Contains(field, ".") {
		return nestedMergeValue(data, field)
	}
	return data[field]
}

func applyAggregateOperation(values []any, field aggregateField) (any, error) {
	switch strings.ToLower(field.Aggregation) {
	case "sum":
		return aggregateSum(values)
	case "count":
		return float64(len(values)), nil
	case "countunique":
		return float64(len(uniqueAggregateValues(values))), nil
	case "min":
		return aggregateMinMax(values, false)
	case "max":
		return aggregateMinMax(values, true)
	case "mean", "average":
		return aggregateMean(values)
	case "first":
		if len(values) == 0 {
			return nil, nil
		}
		return values[0], nil
	case "last":
		if len(values) == 0 {
			return nil, nil
		}
		return values[len(values)-1], nil
	case "append":
		return values, nil
	case "appendunique":
		return uniqueAggregateValues(values), nil
	case "concatenate":
		return joinAggregateValues(values, field.Separator, false), nil
	case "concatenateunique":
		return joinAggregateValues(values, field.Separator, true), nil
	default:
		return nil, fmt.Errorf("unknown aggregation %q", field.Aggregation)
	}
}

func aggregateSum(values []any) (float64, error) {
	var sum float64
	for _, value := range values {
		number, ok := floatIFValue(value)
		if !ok {
			return 0, fmt.Errorf("non numeric value %v", value)
		}
		sum += number
	}
	return sum, nil
}

func aggregateMean(values []any) (any, error) {
	var sum float64
	var count float64
	for _, value := range values {
		number, ok := floatIFValue(value)
		if ok {
			sum += number
			count++
		}
	}
	if count == 0 {
		return nil, nil
	}
	return sum / count, nil
}

func aggregateMinMax(values []any, max bool) (any, error) {
	best := math.Inf(1)
	if max {
		best = math.Inf(-1)
	}
	found := false
	for _, value := range values {
		number, ok := floatIFValue(value)
		if !ok {
			continue
		}
		if !found || (!max && number < best) || (max && number > best) {
			best = number
			found = true
		}
	}
	if !found {
		return nil, nil
	}
	return best, nil
}

func uniqueAggregateValues(values []any) []any {
	seen := map[string]bool{}
	result := []any{}
	for _, value := range values {
		key := fmt.Sprintf("%#v", value)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, value)
	}
	return result
}

func joinAggregateValues(values []any, separator string, unique bool) string {
	if separator == "" {
		separator = ","
	}
	if unique {
		values = uniqueAggregateValues(values)
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprint(value))
	}
	return strings.Join(parts, separator)
}
