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
	FieldsToSplitOut   []string
	Include            string
	FieldsToInclude    []string
	DisableDotNotation bool
	DestinationFields  []string
	IncludeBinary      bool
}

func (SplitOut) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	params := parseSplitOutParams(in.Node.Parameters)
	if len(params.FieldsToSplitOut) == 0 {
		return nil, fmt.Errorf("splitOut fieldToSplitOut is required")
	}
	if len(params.DestinationFields) > 0 && len(params.DestinationFields) != len(params.FieldsToSplitOut) {
		return nil, fmt.Errorf("splitOut: if multiple fields to split out are given, the same number of destination fields must be given")
	}
	result := make([]dataplane.Item, 0)
	for itemIndex, item := range firstInput(in.InputData) {
		splitItems := make([]dataplane.Item, 0)
		multiSplit := len(params.FieldsToSplitOut) > 1
		for fieldIndex, field := range params.FieldsToSplitOut {
			destination := ""
			if fieldIndex < len(params.DestinationFields) {
				destination = params.DestinationFields[fieldIndex]
			}
			values := splitOutFieldValues(item, field, params)
			for valueIndex, value := range values {
				for len(splitItems) <= valueIndex {
					splitItems = append(splitItems, dataplane.Item{
						JSON:       map[string]any{},
						PairedItem: &dataplane.PairedItem{Item: itemIndex},
					})
				}
				applySplitOutValue(&splitItems[valueIndex], field, destination, value, params.Include, multiSplit)
			}
		}
		for _, splitItem := range splitItems {
			next := splitItem
			switch strings.ToLower(params.Include) {
			case "allotherfields", "all":
				base := deepCopySetMap(item.JSON)
				for _, field := range params.FieldsToSplitOut {
					removeSplitOutField(base, field, params.DisableDotNotation)
				}
				for key, value := range splitItem.JSON {
					base[key] = value
				}
				next.JSON = base
			case "selectedotherfields", "selected":
				if len(params.FieldsToInclude) == 0 {
					return nil, fmt.Errorf("splitOut: no fields specified to include")
				}
				for _, field := range params.FieldsToInclude {
					next.JSON[field] = deepCopySetValue(splitOutJSONValue(item.JSON, field, params.DisableDotNotation))
				}
			}
			if params.IncludeBinary && item.Binary != nil && next.Binary == nil {
				next.Binary = item.Binary
			}
			result = append(result, next)
		}
	}
	return dataplane.MainOutput(result), nil
}

func parseSplitOutParams(raw map[string]any) splitOutParams {
	options := mergeObject(raw["options"])
	fieldsToSplitOut := splitOutFields(firstNonEmptyNode(stringParam(raw, "fieldToSplitOut"), stringParam(raw, "field", "propertyName")))
	return splitOutParams{
		FieldsToSplitOut:   fieldsToSplitOut,
		Include:            firstNonEmptyNode(stringParam(raw, "include"), "noOtherFields"),
		FieldsToInclude:    splitOutFields(raw["fieldsToInclude"]),
		DisableDotNotation: boolParam(options, "disableDotNotation", boolParam(raw, "disableDotNotation", false)),
		DestinationFields:  splitOutFields(firstNonEmptyNode(stringParam(options, "destinationFieldName"), stringParam(raw, "destinationFieldName"))),
		IncludeBinary:      boolParam(options, "includeBinary", boolParam(raw, "includeBinary", false)),
	}
}

func splitOutFields(value any) []string {
	if values, ok := value.([]any); ok {
		result := make([]string, 0, len(values))
		for _, value := range values {
			if text := normalizeSplitOutField(fmt.Sprint(value)); text != "" {
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
		if trimmed := normalizeSplitOutField(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func normalizeSplitOutField(field string) string {
	field = strings.TrimSpace(field)
	if strings.HasPrefix(field, "$json.") {
		return strings.TrimPrefix(field, "$json.")
	}
	return field
}

func splitOutFieldValues(item dataplane.Item, field string, params splitOutParams) []any {
	if field == "$binary" {
		values := make([]any, 0, len(item.Binary))
		for key, value := range item.Binary {
			values = append(values, map[string]dataplane.Binary{key: value})
		}
		return values
	}
	return splitOutValues(splitOutJSONValue(item.JSON, field, params.DisableDotNotation))
}

func splitOutJSONValue(data map[string]any, field string, disableDotNotation bool) any {
	if !disableDotNotation && strings.Contains(field, ".") {
		return nestedMergeValue(data, field)
	}
	return data[field]
}

func splitOutValues(value any) []any {
	switch typed := value.(type) {
	case nil:
		return nil
	case []any:
		return typed
	case map[string]any:
		values := make([]any, 0, len(typed))
		for _, value := range typed {
			values = append(values, value)
		}
		return values
	default:
		return []any{typed}
	}
}

func applySplitOutValue(item *dataplane.Item, field string, destination string, value any, include string, multiSplit bool) {
	if field == "$binary" {
		object, ok := value.(map[string]dataplane.Binary)
		if !ok {
			return
		}
		if item.Binary == nil {
			item.Binary = map[string]dataplane.Binary{}
		}
		for key, value := range object {
			item.Binary[key] = value
		}
		return
	}
	fieldName := destination
	if fieldName == "" {
		fieldName = field
	}
	if object, ok := value.(map[string]any); ok && strings.EqualFold(include, "noOtherFields") && destination == "" && !multiSplit {
		for key, value := range object {
			item.JSON[key] = deepCopySetValue(value)
		}
		return
	}
	item.JSON[fieldName] = deepCopySetValue(value)
}

func removeSplitOutField(data map[string]any, field string, disableDotNotation bool) {
	if disableDotNotation || !strings.Contains(field, ".") {
		delete(data, field)
		return
	}
	parts := strings.Split(field, ".")
	current := data
	for index, part := range parts {
		if index == len(parts)-1 {
			delete(current, part)
			return
		}
		next, ok := current[part].(map[string]any)
		if !ok {
			return
		}
		current = next
	}
}
