package nodes

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type Sort struct{}

type sortParams struct {
	Type               string
	SortFields         []sortField
	CaseSensitive      bool
	NullsPosition      string
	NumericSort        bool
	LocaleCompare      string
	StableSort         bool
	DisableDotNotation bool
	Seed               int64
}

type sortField struct {
	FieldName string
	Order     string
}

func (Sort) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	items := cloneItems(firstInput(in.InputData))
	params := newSortParams(in.Node.Parameters)
	if len(items) <= 1 {
		return dataplane.MainOutput(items), nil
	}
	switch params.Type {
	case "random":
		sortRandomItems(items, params)
		return dataplane.MainOutput(items), nil
	case "simple", "":
		if len(params.SortFields) == 0 {
			return nil, fmt.Errorf("sort: at least one sort field is required")
		}
		sortSimpleItems(items, params)
		return dataplane.MainOutput(items), nil
	case "expression":
		return nil, fmt.Errorf("sort: expression type is not supported by this runtime yet")
	default:
		return nil, fmt.Errorf("sort: unsupported type %s", params.Type)
	}
}

func newSortParams(raw map[string]any) sortParams {
	options := mergeObject(raw["options"])
	params := sortParams{
		Type:               strings.ToLower(firstNonEmptyNode(stringParam(raw, "type", "operation"), "simple")),
		SortFields:         parseSortFields(raw),
		CaseSensitive:      boolParam(options, "caseSensitive", boolParam(raw, "caseSensitive", false)),
		NullsPosition:      strings.ToLower(firstNonEmptyNode(stringParam(options, "nullsPosition"), stringParam(raw, "nullsPosition"), "last")),
		NumericSort:        boolParam(options, "numericSort", boolParam(raw, "numericSort", false)),
		LocaleCompare:      stringParam(options, "localeCompare"),
		StableSort:         boolParam(options, "stableSort", boolParam(raw, "stableSort", true)),
		DisableDotNotation: boolParam(options, "disableDotNotation", boolParam(raw, "disableDotNotation", false)),
		Seed:               int64ParamSort(firstNonNil(raw["seed"], options["seed"]), 0),
	}
	if params.NullsPosition != "first" {
		params.NullsPosition = "last"
	}
	return params
}

func parseSortFields(raw map[string]any) []sortField {
	for _, key := range []string{"sortFieldsUi", "sortFields", "fields"} {
		if fields := parseSortFieldCollection(raw[key]); len(fields) > 0 {
			return fields
		}
	}
	fieldName := stringParam(raw, "fieldName", "field", "key")
	if fieldName == "" {
		return nil
	}
	return []sortField{{FieldName: fieldName, Order: firstNonEmptyNode(stringParam(raw, "order", "direction"), "ascending")}}
}

func parseSortFieldCollection(raw any) []sortField {
	if object, ok := rawObject(raw); ok {
		for _, key := range []string{"sortField", "sortFields", "fields", "values"} {
			if values, ok := object[key].([]any); ok {
				return parseSortFieldList(values)
			}
		}
		if field := parseSortFieldObject(object); field.FieldName != "" {
			return []sortField{field}
		}
	}
	if values, ok := raw.([]any); ok {
		return parseSortFieldList(values)
	}
	return nil
}

func parseSortFieldList(values []any) []sortField {
	fields := make([]sortField, 0, len(values))
	for _, value := range values {
		object, ok := rawObject(value)
		if !ok {
			continue
		}
		field := parseSortFieldObject(object)
		if field.FieldName != "" {
			fields = append(fields, field)
		}
	}
	return fields
}

func parseSortFieldObject(object map[string]any) sortField {
	return sortField{
		FieldName: stringParam(object, "fieldName", "field", "key"),
		Order:     strings.ToLower(firstNonEmptyNode(stringParam(object, "order", "direction"), "ascending")),
	}
}

func sortRandomItems(items []dataplane.Item, params sortParams) {
	seed := params.Seed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	rng := rand.New(rand.NewSource(seed))
	rng.Shuffle(len(items), func(i int, j int) {
		items[i], items[j] = items[j], items[i]
	})
}

func sortSimpleItems(items []dataplane.Item, params sortParams) {
	less := func(i int, j int) bool {
		for _, field := range params.SortFields {
			cmp := compareSortValues(sortItemValue(items[i].JSON, field.FieldName, params), sortItemValue(items[j].JSON, field.FieldName, params), params)
			if cmp == 0 {
				continue
			}
			if strings.EqualFold(field.Order, "descending") || strings.EqualFold(field.Order, "desc") {
				return cmp > 0
			}
			return cmp < 0
		}
		return false
	}
	if params.StableSort {
		sort.SliceStable(items, less)
		return
	}
	sort.Slice(items, less)
}

func sortItemValue(data map[string]any, field string, params sortParams) any {
	if !params.DisableDotNotation && strings.Contains(field, ".") {
		return nestedMergeValue(data, field)
	}
	return data[field]
}

func compareSortValues(left any, right any, params sortParams) int {
	if left == nil && right == nil {
		return 0
	}
	if left == nil {
		if params.NullsPosition == "first" {
			return -1
		}
		return 1
	}
	if right == nil {
		if params.NullsPosition == "first" {
			return 1
		}
		return -1
	}
	leftNumber, leftOK := sortFloat(left)
	rightNumber, rightOK := sortFloat(right)
	if leftOK && rightOK {
		return compareSortFloat(leftNumber, rightNumber)
	}
	leftText := sortString(left)
	rightText := sortString(right)
	if !params.CaseSensitive {
		leftText = strings.ToLower(leftText)
		rightText = strings.ToLower(rightText)
	}
	if params.NumericSort {
		if cmp := compareSortNatural(leftText, rightText); cmp != 0 {
			return cmp
		}
	}
	if params.LocaleCompare != "" {
		return compareSortLocale(leftText, rightText, params.LocaleCompare)
	}
	return compareSortText(leftText, rightText)
}

func sortFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int8:
		return float64(typed), true
	case int16:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint8:
		return float64(typed), true
	case uint16:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case float32:
		return float64(typed), true
	case float64:
		return typed, true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func compareSortFloat(left float64, right float64) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func sortString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return fmt.Sprint(value)
	}
}

func compareSortNatural(left string, right string) int {
	leftParts := splitSortNumericParts(left)
	rightParts := splitSortNumericParts(right)
	for index := 0; index < len(leftParts) && index < len(rightParts); index++ {
		leftNumber, leftErr := strconv.ParseFloat(leftParts[index], 64)
		rightNumber, rightErr := strconv.ParseFloat(rightParts[index], 64)
		if leftErr == nil && rightErr == nil {
			if cmp := compareSortFloat(leftNumber, rightNumber); cmp != 0 {
				return cmp
			}
			continue
		}
		if cmp := compareSortText(leftParts[index], rightParts[index]); cmp != 0 {
			return cmp
		}
	}
	if len(leftParts) < len(rightParts) {
		return -1
	}
	if len(leftParts) > len(rightParts) {
		return 1
	}
	return 0
}

func splitSortNumericParts(value string) []string {
	parts := []string{}
	var current strings.Builder
	inNumber := false
	for _, char := range value {
		isNumber := unicode.IsDigit(char) || char == '.'
		if current.Len() == 0 {
			inNumber = isNumber
		}
		if isNumber != inNumber {
			parts = append(parts, current.String())
			current.Reset()
			inNumber = isNumber
		}
		current.WriteRune(char)
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

func compareSortLocale(left string, right string, locale string) int {
	switch strings.ToLower(locale) {
	case "ro", "ro-ro", "romanian":
		left = normalizeSortRomanian(left)
		right = normalizeSortRomanian(right)
	case "de", "de-de", "german":
		left = normalizeSortGerman(left)
		right = normalizeSortGerman(right)
	}
	return compareSortText(left, right)
}

func normalizeSortRomanian(value string) string {
	replacements := map[rune]string{
		'\u0103': "a", '\u00e2': "a", '\u00ee': "i", '\u0219': "s", '\u015f': "s", '\u021b': "t", '\u0163': "t",
		'\u0102': "A", '\u00c2': "A", '\u00ce': "I", '\u0218': "S", '\u015e': "S", '\u021a': "T", '\u0162': "T",
	}
	var builder strings.Builder
	for _, char := range value {
		if replacement, ok := replacements[char]; ok {
			builder.WriteString(replacement)
			continue
		}
		builder.WriteRune(char)
	}
	return builder.String()
}

func normalizeSortGerman(value string) string {
	replacements := map[rune]string{
		'\u00e4': "ae", '\u00f6': "oe", '\u00fc': "ue", '\u00df': "ss",
		'\u00c4': "Ae", '\u00d6': "Oe", '\u00dc': "Ue",
	}
	var builder strings.Builder
	for _, char := range value {
		if replacement, ok := replacements[char]; ok {
			builder.WriteString(replacement)
			continue
		}
		builder.WriteRune(char)
	}
	return builder.String()
}

func compareSortText(left string, right string) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func int64ParamSort(value any, fallback int64) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case string:
		parsed, err := strconv.ParseInt(typed, 10, 64)
		if err == nil {
			return parsed
		}
	}
	return fallback
}
