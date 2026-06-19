package nodes

import (
	"context"
	"fmt"
	"strings"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type SplitOut struct{}

type splitOutParams struct {
	FieldToSplitOut      string
	Include              string
	FieldsToInclude      []string
	DisableDotNotation   bool
	DestinationFieldName string
}

func (SplitOut) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	params := parseSplitOutParams(in.Node.Parameters)
	if params.FieldToSplitOut == "" {
		return nil, fmt.Errorf("splitOut fieldToSplitOut is required")
	}
	result := make([]dataplane.Item, 0)
	for _, item := range firstInput(in.InputData) {
		values := splitOutValues(splitOutSourceValue(item.JSON, params))
		if len(values) == 0 {
			result = append(result, buildSplitOutItem(item, params, nil, false))
			continue
		}
		for _, value := range values {
			result = append(result, buildSplitOutItem(item, params, value, true))
		}
	}
	return dataplane.MainOutput(result), nil
}

func parseSplitOutParams(raw map[string]any) splitOutParams {
	options := mergeObject(raw["options"])
	return splitOutParams{
		FieldToSplitOut:      stringParam(raw, "fieldToSplitOut", "field", "propertyName"),
		Include:              firstNonEmptyNode(stringParam(raw, "include"), "noOtherFields"),
		FieldsToInclude:      splitOutFields(raw["fieldsToInclude"]),
		DisableDotNotation:   boolParam(options, "disableDotNotation", boolParam(raw, "disableDotNotation", false)),
		DestinationFieldName: firstNonEmptyNode(stringParam(options, "destinationFieldName"), stringParam(raw, "destinationFieldName")),
	}
}

func splitOutFields(value any) []string {
	if values, ok := value.([]any); ok {
		result := make([]string, 0, len(values))
		for _, value := range values {
			if text := strings.TrimSpace(fmt.Sprint(value)); text != "" {
				result = append(result, text)
			}
		}
		return result
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return nil
	}
	parts := strings.Split(text, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func splitOutSourceValue(data map[string]any, params splitOutParams) any {
	if !params.DisableDotNotation && strings.Contains(params.FieldToSplitOut, ".") {
		return nestedMergeValue(data, params.FieldToSplitOut)
	}
	return data[params.FieldToSplitOut]
}

func splitOutValues(value any) []any {
	switch typed := value.(type) {
	case nil:
		return nil
	case []any:
		return typed
	default:
		return []any{typed}
	}
}

func buildSplitOutItem(source dataplane.Item, params splitOutParams, value any, hasValue bool) dataplane.Item {
	next := dataplane.Item{JSON: splitOutBaseJSON(source.JSON, params), Binary: source.Binary, PairedItem: source.PairedItem, Error: source.Error}
	if !hasValue {
		return next
	}
	destination := params.DestinationFieldName
	if destination != "" {
		next.JSON[destination] = value
		return next
	}
	if object, ok := value.(map[string]any); ok {
		for key, value := range object {
			next.JSON[key] = deepCopySetValue(value)
		}
		return next
	}
	next.JSON[params.FieldToSplitOut] = value
	return next
}

func splitOutBaseJSON(source map[string]any, params splitOutParams) map[string]any {
	switch strings.ToLower(params.Include) {
	case "allotherfields", "all":
		result := deepCopySetMap(source)
		delete(result, params.FieldToSplitOut)
		return result
	case "selectedotherfields", "selected":
		result := map[string]any{}
		for _, field := range params.FieldsToInclude {
			value := source[field]
			if !params.DisableDotNotation && strings.Contains(field, ".") {
				value = nestedMergeValue(source, field)
			}
			if value != nil {
				result[field] = deepCopySetValue(value)
			}
		}
		return result
	default:
		return map[string]any{}
	}
}
