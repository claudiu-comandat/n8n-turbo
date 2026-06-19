package nodes

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type If struct{}

type ifCondition struct {
	Left          any
	Right         any
	Operation     string
	CaseSensitive bool
}

func (If) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	trueItems := make([]dataplane.Item, 0)
	falseItems := make([]dataplane.Item, 0)
	items := firstInput(in.InputData)
	for index, item := range items {
		if conditionMatches(in, items, index, item, in.Node.Parameters) {
			trueItems = append(trueItems, item)
		} else {
			falseItems = append(falseItems, item)
		}
	}
	return dataplane.Output{trueItems, falseItems}, nil
}

func conditionMatches(in engine.ExecuteInput, items []dataplane.Item, itemIndex int, item dataplane.Item, params map[string]any) bool {
	if conditions, ok := rawObject(params["conditions"]); ok {
		return conditionsMatch(in, items, itemIndex, item, conditions)
	}
	fieldRaw := firstPresent(params, "field", "leftValue")
	field := strings.TrimPrefix(fmt.Sprint(resolveValue(in, items, itemIndex, fieldRaw)), "$json.")
	if field == "" {
		return true
	}
	actual := item.JSON[field]
	if strings.Contains(field, ".") {
		actual = nestedIFValue(item.JSON, field)
	}
	expected := resolveValue(in, items, itemIndex, firstPresent(params, "value", "rightValue"))
	return ApplyOperation(actual, expected, stringParam(params, "operation"), boolParam(params, "caseSensitive", false), true)
}

func conditionsMatch(in engine.ExecuteInput, items []dataplane.Item, itemIndex int, item dataplane.Item, conditions map[string]any) bool {
	groups := parseIFGroups(conditions)
	if len(groups) > 0 {
		return combineIFResults(evaluateIFGroups(in, items, itemIndex, item, groups), ifCombinator(conditions, "all"))
	}
	values := parseIFConditions(conditions)
	if len(values) == 0 {
		return true
	}
	return combineIFResults(evaluateIFConditions(in, items, itemIndex, values), ifCombinator(conditions, "all"))
}

func parseIFGroups(conditions map[string]any) []map[string]any {
	values, ok := conditions["groups"].([]any)
	if !ok {
		return nil
	}
	result := make([]map[string]any, 0, len(values))
	for _, value := range values {
		object, ok := rawObject(value)
		if ok {
			result = append(result, object)
		}
	}
	return result
}

func evaluateIFGroups(in engine.ExecuteInput, items []dataplane.Item, itemIndex int, item dataplane.Item, groups []map[string]any) []bool {
	results := make([]bool, 0, len(groups))
	for _, group := range groups {
		results = append(results, conditionsMatch(in, items, itemIndex, item, group))
	}
	return results
}

func parseIFConditions(conditions map[string]any) []ifCondition {
	values, ok := conditions["conditions"].([]any)
	if ok {
		return parseIFConditionList(values)
	}
	result := []ifCondition{}
	for _, key := range []string{"string", "number", "boolean", "dateTime", "date"} {
		values, ok := conditions[key].([]any)
		if !ok {
			continue
		}
		for _, value := range values {
			object, ok := rawObject(value)
			if !ok {
				continue
			}
			condition := parseIFCondition(object)
			result = append(result, condition)
		}
	}
	return result
}

func parseIFConditionList(values []any) []ifCondition {
	result := make([]ifCondition, 0, len(values))
	for _, value := range values {
		object, ok := rawObject(value)
		if ok {
			result = append(result, parseIFCondition(object))
		}
	}
	return result
}

func parseIFCondition(object map[string]any) ifCondition {
	return ifCondition{
		Left:          firstPresent(object, "leftValue", "value1"),
		Right:         firstPresent(object, "rightValue", "value2"),
		Operation:     firstString(object, "operation", "operator", "type"),
		CaseSensitive: boolParam(object, "caseSensitive", false),
	}
}

func evaluateIFConditions(in engine.ExecuteInput, items []dataplane.Item, itemIndex int, conditions []ifCondition) []bool {
	results := make([]bool, 0, len(conditions))
	for _, condition := range conditions {
		left := resolveValue(in, items, itemIndex, condition.Left)
		right := resolveValue(in, items, itemIndex, condition.Right)
		results = append(results, ApplyOperation(left, right, condition.Operation, condition.CaseSensitive, true))
	}
	return results
}

func combineIFResults(results []bool, combinator string) bool {
	if len(results) == 0 {
		return true
	}
	if combinator == "any" {
		for _, result := range results {
			if result {
				return true
			}
		}
		return false
	}
	for _, result := range results {
		if !result {
			return false
		}
	}
	return true
}

func ifCombinator(params map[string]any, fallback string) string {
	raw := strings.ToLower(strings.TrimSpace(firstNonEmptyNode(stringParam(params, "combineOperation"), stringParam(params, "combinator"))))
	switch raw {
	case "any", "or":
		return "any"
	case "all", "and":
		return "all"
	default:
		return fallback
	}
}

func firstPresent(params map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := params[key]; ok {
			return value
		}
	}
	return ""
}

func compareValues(left any, right any, operation string) bool {
	return ApplyOperation(left, right, operation, false, true)
}

func ApplyOperation(left any, right any, operation string, caseSensitive bool, looseType bool) bool {
	switch normalizeIFOperation(operation) {
	case "equal":
		return equalIFValues(left, right, caseSensitive, looseType)
	case "notEqual":
		return !equalIFValues(left, right, caseSensitive, looseType)
	case "contains":
		return strings.Contains(stringIFValue(left, caseSensitive), stringIFValue(right, caseSensitive))
	case "notContains":
		return !strings.Contains(stringIFValue(left, caseSensitive), stringIFValue(right, caseSensitive))
	case "startsWith":
		return strings.HasPrefix(stringIFValue(left, caseSensitive), stringIFValue(right, caseSensitive))
	case "endsWith":
		return strings.HasSuffix(stringIFValue(left, caseSensitive), stringIFValue(right, caseSensitive))
	case "matchesRegex":
		regex, err := regexp.Compile(fmt.Sprint(right))
		return err == nil && regex.MatchString(fmt.Sprint(left))
	case "notMatchesRegex":
		regex, err := regexp.Compile(fmt.Sprint(right))
		return err != nil || !regex.MatchString(fmt.Sprint(left))
	case "larger":
		leftNumber, rightNumber, ok := comparableIFFloats(left, right)
		return ok && leftNumber > rightNumber
	case "largerEqual":
		leftNumber, rightNumber, ok := comparableIFFloats(left, right)
		return ok && leftNumber >= rightNumber
	case "smaller":
		leftNumber, rightNumber, ok := comparableIFFloats(left, right)
		return ok && leftNumber < rightNumber
	case "smallerEqual":
		leftNumber, rightNumber, ok := comparableIFFloats(left, right)
		return ok && leftNumber <= rightNumber
	case "exists":
		return !emptyIFValue(left)
	case "notExists":
		return emptyIFValue(left)
	case "isEmpty":
		return emptyIFValue(left)
	case "isNotEmpty":
		return !emptyIFValue(left)
	case "dateAfter":
		leftTime, leftOK := parseIFDate(left)
		rightTime, rightOK := parseIFDate(right)
		return leftOK && rightOK && leftTime.After(rightTime)
	case "dateBefore":
		leftTime, leftOK := parseIFDate(left)
		rightTime, rightOK := parseIFDate(right)
		return leftOK && rightOK && leftTime.Before(rightTime)
	case "isTrue":
		value, ok := boolIFValue(left)
		return ok && value
	case "isFalse":
		value, ok := boolIFValue(left)
		return ok && !value
	default:
		return equalIFValues(left, right, caseSensitive, looseType)
	}
}

func normalizeIFOperation(operation string) string {
	raw := strings.ToLower(strings.TrimSpace(operation))
	switch raw {
	case "", "equal", "equals", "=", "==":
		return "equal"
	case "notequal", "not_equal", "not_equals", "not", "!=", "<>":
		return "notEqual"
	case "contains":
		return "contains"
	case "notcontains", "not_contains":
		return "notContains"
	case "startswith", "starts_with":
		return "startsWith"
	case "endswith", "ends_with":
		return "endsWith"
	case "matchesregex", "regex":
		return "matchesRegex"
	case "notmatchesregex", "not_regex":
		return "notMatchesRegex"
	case "larger", "largerthan", "greater", "greaterthan", ">":
		return "larger"
	case "largerequal", "larger_or_equal", "largerthanequal", "greaterorequal", ">=":
		return "largerEqual"
	case "smaller", "smallerthan", "less", "lessthan", "<":
		return "smaller"
	case "smallerequal", "smaller_or_equal", "smallerthanequal", "lessorequal", "<=":
		return "smallerEqual"
	case "exists":
		return "exists"
	case "notexists", "doesnotexist":
		return "notExists"
	case "isempty", "empty":
		return "isEmpty"
	case "isnotempty", "notempty":
		return "isNotEmpty"
	case "dateafter", "after":
		return "dateAfter"
	case "datebefore", "before":
		return "dateBefore"
	case "istrue", "true":
		return "isTrue"
	case "isfalse", "false":
		return "isFalse"
	default:
		return operation
	}
}

func equalIFValues(left any, right any, caseSensitive bool, looseType bool) bool {
	if looseType {
		leftNumber, rightNumber, ok := comparableIFFloats(left, right)
		if ok {
			return leftNumber == rightNumber
		}
		leftBool, leftOK := boolIFValue(left)
		rightBool, rightOK := boolIFValue(right)
		if leftOK && rightOK {
			return leftBool == rightBool
		}
	}
	return stringIFValue(left, caseSensitive) == stringIFValue(right, caseSensitive)
}

func stringIFValue(value any, caseSensitive bool) string {
	text := fmt.Sprint(value)
	if !caseSensitive {
		return strings.ToLower(text)
	}
	return text
}

func comparableIFFloats(left any, right any) (float64, float64, bool) {
	leftNumber, leftOK := floatIFValue(left)
	rightNumber, rightOK := floatIFValue(right)
	return leftNumber, rightNumber, leftOK && rightOK
}

func floatIFValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case float64:
		return typed, !math.IsNaN(typed)
	case float32:
		value := float64(typed)
		return value, !math.IsNaN(value)
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed, err == nil && !math.IsNaN(parsed)
	case bool:
		if typed {
			return 1, true
		}
		return 0, true
	default:
		return 0, false
	}
}

func boolIFValue(value any) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "yes", "1", "on":
			return true, true
		case "false", "no", "0", "off", "":
			return false, true
		default:
			return false, false
		}
	case int:
		return typed != 0, true
	case int64:
		return typed != 0, true
	case float64:
		return typed != 0 && !math.IsNaN(typed), true
	default:
		return false, false
	}
}

func emptyIFValue(value any) bool {
	if value == nil {
		return true
	}
	switch typed := value.(type) {
	case string:
		return typed == ""
	case []any:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	default:
		return false
	}
}

func parseIFDate(value any) (time.Time, bool) {
	switch typed := value.(type) {
	case time.Time:
		return typed, true
	case int:
		return unixIFDate(float64(typed)), true
	case int64:
		return unixIFDate(float64(typed)), true
	case float64:
		return unixIFDate(typed), true
	case string:
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02", time.RFC1123, time.RFC1123Z, time.RFC822, time.RFC822Z} {
			parsed, err := time.Parse(layout, typed)
			if err == nil {
				return parsed, true
			}
		}
		number, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err == nil {
			return unixIFDate(number), true
		}
	}
	return time.Time{}, false
}

func unixIFDate(value float64) time.Time {
	if value > 1e12 {
		return time.UnixMilli(int64(value)).UTC()
	}
	return time.Unix(int64(value), 0).UTC()
}

func nestedIFValue(data map[string]any, path string) any {
	var current any = data
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = object[part]
	}
	return current
}

func number(value any) float64 {
	parsed, _ := floatIFValue(value)
	return parsed
}
