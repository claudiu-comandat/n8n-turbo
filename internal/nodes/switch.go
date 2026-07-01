package nodes

import (
	"context"
	"fmt"
	"strings"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type Switch struct{}

type switchRule struct {
	Operation     string
	Value1        any
	Value2        any
	OutputIndex   int
	CaseSensitive bool
	Conditions    map[string]any
}

type switchParams struct {
	Mode           string
	Value          any
	DataType       string
	OutputType     string
	NumberOutputs  int
	FallbackOutput *int
	Rules          []switchRule
}

func (Switch) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	params := parseSwitchParams(in.Node.Parameters)
	items := firstInput(in.InputData)
	output := dataplane.Output{}
	for index, item := range items {
		item = itemWithPairedIndex(item, index, true)
		if strings.EqualFold(params.Mode, "expression") && index == 0 && params.NumberOutputs > 0 {
			output = ensureOutput(output, params.NumberOutputs-1)
		}
		targets, err := switchTargets(in, params, items, index, item)
		if err != nil {
			return nil, err
		}
		for _, target := range targets {
			if target < 0 {
				continue
			}
			if strings.EqualFold(params.Mode, "expression") && params.NumberOutputs > 0 && target >= params.NumberOutputs {
				return nil, fmt.Errorf("switch: output %d is not allowed; it has to be between 0 and %d", target, params.NumberOutputs-1)
			}
			output = ensureOutput(output, target)
			output[target] = append(output[target], item)
		}
	}
	if len(output) == 0 {
		output = ensureOutput(output, maxSwitchOutput(params))
	}
	return output, nil
}

func parseSwitchParams(raw map[string]any) switchParams {
	params := switchParams{Mode: "rules", DataType: "number", OutputType: "single"}
	if value := stringParam(raw, "mode"); value != "" {
		params.Mode = value
	}
	if value, ok := raw["value"]; ok {
		params.Value = value
	}
	if strings.EqualFold(params.Mode, "expression") {
		params.Value = firstPresent(raw, "output", "value")
		params.NumberOutputs = intParam(raw, "numberOutputs", 4)
	}
	if value := stringParam(raw, "dataType"); value != "" {
		params.DataType = value
	}
	if value := stringParam(raw, "output"); value == "single" || value == "multiple" {
		params.OutputType = value
	}
	params.Rules = parseSwitchRules(raw["rules"])
	options, _ := rawObject(raw["options"])
	if boolParam(options, "allMatchingOutputs", false) {
		params.OutputType = "multiple"
	}
	if value := switchFallbackOutput(firstNonNil(options["fallbackOutput"], raw["fallbackOutput"]), len(params.Rules)); value != nil {
		params.FallbackOutput = value
	}
	return params
}

func parseSwitchRules(raw any) []switchRule {
	if object, ok := rawObject(raw); ok {
		if values, ok := object["values"].([]any); ok {
			return parseSwitchRuleList(values)
		}
	}
	if values, ok := raw.([]any); ok {
		return parseSwitchRuleList(values)
	}
	return nil
}

func parseSwitchRuleList(values []any) []switchRule {
	rules := make([]switchRule, 0, len(values))
	for index, value := range values {
		object, ok := rawObject(value)
		if !ok {
			continue
		}
		outputIndex := index
		if parsed := intOptional(object["outputIndex"]); parsed != nil {
			outputIndex = *parsed
		}
		conditions, _ := rawObject(object["conditions"])
		rules = append(rules, switchRule{
			Operation:     firstString(object, "operation", "operator", "type"),
			Value1:        firstPresent(object, "value1", "leftValue"),
			Value2:        firstPresent(object, "value2", "rightValue"),
			OutputIndex:   outputIndex,
			CaseSensitive: boolParam(object, "caseSensitive", false),
			Conditions:    conditions,
		})
	}
	return rules
}

func switchTargets(in engine.ExecuteInput, params switchParams, items []dataplane.Item, index int, item dataplane.Item) ([]int, error) {
	switch strings.ToLower(params.Mode) {
	case "expression":
		return switchExpressionTarget(in, params, items, index)
	case "rules", "":
		return switchRuleTargets(in, params, items, index, item), nil
	default:
		return nil, fmt.Errorf("unsupported switch mode %q", params.Mode)
	}
}

func switchExpressionTarget(in engine.ExecuteInput, params switchParams, items []dataplane.Item, index int) ([]int, error) {
	raw := resolveValue(in, items, index, params.Value)
	switch strings.ToLower(params.DataType) {
	case "string":
		value := fmt.Sprint(raw)
		for _, rule := range params.Rules {
			expected := fmt.Sprint(resolveValue(in, items, index, rule.Value2))
			if rule.CaseSensitive && value == expected {
				return []int{rule.OutputIndex}, nil
			}
			if !rule.CaseSensitive && strings.EqualFold(value, expected) {
				return []int{rule.OutputIndex}, nil
			}
		}
	case "boolean":
		if truthy(raw) {
			return []int{1}, nil
		}
		return []int{0}, nil
	default:
		return []int{int(number(raw))}, nil
	}
	if params.FallbackOutput != nil {
		return []int{*params.FallbackOutput}, nil
	}
	return nil, nil
}

func switchRuleTargets(in engine.ExecuteInput, params switchParams, items []dataplane.Item, index int, item dataplane.Item) []int {
	targets := make([]int, 0)
	for _, rule := range params.Rules {
		if rule.Conditions != nil {
			if !conditionsMatch(in, items, index, item, rule.Conditions) {
				continue
			}
			targets = append(targets, rule.OutputIndex)
			if strings.ToLower(params.OutputType) != "multiple" {
				return targets
			}
			continue
		}
		left := resolveValue(in, items, index, rule.Value1)
		right := resolveValue(in, items, index, rule.Value2)
		if !rule.CaseSensitive {
			left = strings.ToLower(fmt.Sprint(left))
			right = strings.ToLower(fmt.Sprint(right))
		}
		if !ApplyOperation(left, right, rule.Operation, rule.CaseSensitive, true) {
			continue
		}
		targets = append(targets, rule.OutputIndex)
		if strings.ToLower(params.OutputType) != "multiple" {
			return targets
		}
	}
	if len(targets) == 0 && params.FallbackOutput != nil {
		return []int{*params.FallbackOutput}
	}
	return targets
}

func maxSwitchOutput(params switchParams) int {
	maxIndex := 0
	if params.FallbackOutput != nil && *params.FallbackOutput > maxIndex {
		maxIndex = *params.FallbackOutput
	}
	for _, rule := range params.Rules {
		if rule.OutputIndex > maxIndex {
			maxIndex = rule.OutputIndex
		}
	}
	return maxIndex
}

func switchFallbackOutput(value any, ruleCount int) *int {
	if text, ok := value.(string); ok {
		switch strings.ToLower(strings.TrimSpace(text)) {
		case "", "none":
			return nil
		case "extra":
			index := ruleCount
			return &index
		}
	}
	return intOptional(value)
}

func ensureOutput(output dataplane.Output, index int) dataplane.Output {
	for len(output) <= index {
		output = append(output, []dataplane.Item{})
	}
	return output
}

func intOptional(value any) *int {
	switch typed := value.(type) {
	case int:
		return &typed
	case int64:
		parsed := int(typed)
		return &parsed
	case float64:
		parsed := int(typed)
		return &parsed
	case string:
		if typed == "" || typed == "none" {
			return nil
		}
		parsed := int(number(typed))
		return &parsed
	default:
		return nil
	}
}

func firstString(params map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringParam(params, key); value != "" {
			return value
		}
	}
	return ""
}

func truthy(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return typed != "" && !strings.EqualFold(typed, "false") && typed != "0"
	default:
		return number(value) != 0
	}
}
