package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type Set struct{}

type setField struct {
	Name  string
	Type  string
	Value any
}

func (Set) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	items := firstInput(in.InputData)
	if len(items) == 0 {
		items = []dataplane.Item{{JSON: map[string]any{}}}
	}
	result := make([]dataplane.Item, 0, len(items))
	for index, item := range items {
		next, err := setFields(in, items, index, item, in.Node.Parameters)
		if err != nil {
			return nil, fmt.Errorf("item %d: %w", index, err)
		}
		result = append(result, next)
	}
	return dataplane.MainOutput(result), nil
}

func setFields(in engine.ExecuteInput, items []dataplane.Item, itemIndex int, item dataplane.Item, params map[string]any) (dataplane.Item, error) {
	mode := strings.ToLower(strings.TrimSpace(stringParam(params, "mode")))
	if mode == "" {
		mode = "manual"
	}
	switch mode {
	case "manual":
		return setManualFields(in, items, itemIndex, item, params)
	case "json":
		return setJSONFields(in, items, itemIndex, item, params)
	default:
		return dataplane.Item{}, fmt.Errorf("unknown set mode %q", mode)
	}
}

func setManualFields(in engine.ExecuteInput, items []dataplane.Item, itemIndex int, item dataplane.Item, params map[string]any) (dataplane.Item, error) {
	next := dataplane.Item{JSON: setBaseJSON(item, params), Binary: item.Binary, PairedItem: item.PairedItem, Error: item.Error}
	fields := collectSetFields(params)
	dotNotation := boolParam(rawOptions(params), "dotNotation", boolParam(params, "dotNotation", false))
	ignoreConversion := boolParam(rawOptions(params), "ignoreConversionErrors", boolParam(params, "ignoreConversionErrors", false))
	for _, field := range fields {
		if field.Name == "" {
			continue
		}
		value := resolveValue(in, items, itemIndex, field.Value)
		converted, err := CoerceType(value, field.Type)
		if err != nil {
			if ignoreConversion {
				converted = value
			} else {
				return dataplane.Item{}, fmt.Errorf("convert field %q to %s: %w", field.Name, field.Type, err)
			}
		}
		if dotNotation && strings.Contains(field.Name, ".") {
			setNestedSetValue(next.JSON, field.Name, converted)
		} else {
			next.JSON[field.Name] = converted
		}
	}
	return next, nil
}

func setJSONFields(in engine.ExecuteInput, items []dataplane.Item, itemIndex int, item dataplane.Item, params map[string]any) (dataplane.Item, error) {
	next := dataplane.Item{JSON: map[string]any{}, Binary: item.Binary, PairedItem: item.PairedItem, Error: item.Error}
	raw := firstNonNil(params["jsonOutput"], params["json"], params["value"])
	if raw == nil || fmt.Sprint(raw) == "" {
		if includeOtherSetFields(params) {
			next.JSON = deepCopySetMap(item.JSON)
		}
		return next, nil
	}
	resolved := resolveValue(in, items, itemIndex, raw)
	output, err := setJSONObject(resolved)
	if err != nil {
		return dataplane.Item{}, err
	}
	if includeOtherSetFields(params) {
		next.JSON = deepCopySetMap(item.JSON)
		for key, value := range output {
			next.JSON[key] = value
		}
	} else {
		next.JSON = output
	}
	return next, nil
}

func setBaseJSON(item dataplane.Item, params map[string]any) map[string]any {
	if includeOtherSetFields(params) {
		return deepCopySetMap(item.JSON)
	}
	return map[string]any{}
}

func includeOtherSetFields(params map[string]any) bool {
	if boolParam(params, "keepOnlySet", false) {
		return false
	}
	if strings.EqualFold(stringParam(params, "include"), "none") {
		return false
	}
	return boolParam(params, "includeOtherFields", true)
}

func collectSetFields(params map[string]any) []setField {
	fields := parseSetFieldCollection(params["fields"])
	fields = append(fields, parseSetFieldCollection(params["assignments"])...)
	fields = append(fields, parseLegacySetValues(params["values"])...)
	return fields
}

func parseSetFieldCollection(value any) []setField {
	if value == nil {
		return nil
	}
	if object, ok := rawObject(value); ok {
		for _, key := range []string{"assignments", "values", "fields"} {
			if values, ok := object[key].([]any); ok {
				return parseSetFieldList(values)
			}
		}
		if name := stringParam(object, "name", "key"); name != "" {
			return []setField{parseSetField(object)}
		}
		return nil
	}
	if values, ok := value.([]any); ok {
		return parseSetFieldList(values)
	}
	return nil
}

func parseSetFieldList(values []any) []setField {
	fields := make([]setField, 0, len(values))
	for _, value := range values {
		object, ok := rawObject(value)
		if !ok {
			continue
		}
		fields = append(fields, parseSetField(object))
	}
	return fields
}

func parseSetField(object map[string]any) setField {
	fieldType := firstNonEmptyNode(stringParam(object, "type"), stringParam(object, "valueType"))
	if fieldType == "" {
		fieldType = inferSetFieldType(object)
	}
	value := firstNonNil(object["value"], object[fieldType+"Value"])
	for _, key := range []string{"stringValue", "numberValue", "booleanValue", "arrayValue", "objectValue"} {
		if value == nil {
			value = object[key]
		}
	}
	return setField{Name: stringParam(object, "name", "key"), Type: fieldType, Value: value}
}

func inferSetFieldType(object map[string]any) string {
	for _, entry := range []struct {
		Key  string
		Type string
	}{
		{"stringValue", "string"},
		{"numberValue", "number"},
		{"booleanValue", "boolean"},
		{"arrayValue", "array"},
		{"objectValue", "object"},
	} {
		if _, ok := object[entry.Key]; ok {
			return entry.Type
		}
	}
	switch object["value"].(type) {
	case bool:
		return "boolean"
	case int, int64, float32, float64, json.Number:
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "string"
}

func parseLegacySetValues(value any) []setField {
	object, ok := rawObject(value)
	if !ok {
		return nil
	}
	fields := []setField{}
	for _, entry := range []struct {
		Key  string
		Type string
	}{
		{"string", "string"},
		{"number", "number"},
		{"boolean", "boolean"},
		{"array", "array"},
		{"object", "object"},
	} {
		values, ok := object[entry.Key].([]any)
		if !ok {
			continue
		}
		for _, raw := range values {
			fieldObject, ok := rawObject(raw)
			if !ok {
				continue
			}
			field := parseSetField(fieldObject)
			field.Type = entry.Type
			fields = append(fields, field)
		}
	}
	return fields
}

func CoerceType(value any, targetType string) (any, error) {
	switch strings.ToLower(strings.TrimSpace(targetType)) {
	case "", "string":
		return coerceSetString(value)
	case "number":
		return coerceSetNumber(value)
	case "boolean":
		return coerceSetBoolean(value)
	case "array":
		return coerceSetArray(value)
	case "object":
		return coerceSetObject(value)
	default:
		return value, nil
	}
}

func coerceSetString(value any) (any, error) {
	switch typed := value.(type) {
	case nil:
		return "", nil
	case string:
		return typed, nil
	case float64:
		if math.Trunc(typed) == typed {
			return strconv.FormatInt(int64(typed), 10), nil
		}
		return strconv.FormatFloat(typed, 'f', -1, 64), nil
	case float32:
		return strconv.FormatFloat(float64(typed), 'f', -1, 32), nil
	case int:
		return strconv.Itoa(typed), nil
	case int64:
		return strconv.FormatInt(typed, 10), nil
	case bool:
		if typed {
			return "true", nil
		}
		return "false", nil
	default:
		bytes, err := json.Marshal(typed)
		if err != nil {
			return nil, err
		}
		return string(bytes), nil
	}
}

func coerceSetNumber(value any) (any, error) {
	switch typed := value.(type) {
	case nil:
		return float64(0), nil
	case float64:
		return typed, nil
	case float32:
		return float64(typed), nil
	case int:
		return float64(typed), nil
	case int64:
		return float64(typed), nil
	case json.Number:
		return typed.Float64()
	case string:
		number, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return nil, fmt.Errorf("cannot convert %q to number", typed)
		}
		return number, nil
	case bool:
		if typed {
			return float64(1), nil
		}
		return float64(0), nil
	default:
		return nil, fmt.Errorf("cannot convert %T to number", value)
	}
}

func coerceSetBoolean(value any) (any, error) {
	switch typed := value.(type) {
	case nil:
		return false, nil
	case bool:
		return typed, nil
	case float64:
		return typed != 0, nil
	case int:
		return typed != 0, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "yes", "1", "on":
			return true, nil
		case "false", "no", "0", "off", "":
			return false, nil
		default:
			return nil, fmt.Errorf("cannot convert %q to boolean", typed)
		}
	default:
		return nil, fmt.Errorf("cannot convert %T to boolean", value)
	}
}

func coerceSetArray(value any) (any, error) {
	switch typed := value.(type) {
	case nil:
		return []any{}, nil
	case []any:
		return typed, nil
	case string:
		var result []any
		if err := json.Unmarshal([]byte(typed), &result); err == nil {
			return result, nil
		}
		return []any{typed}, nil
	default:
		return []any{value}, nil
	}
}

func coerceSetObject(value any) (any, error) {
	switch typed := value.(type) {
	case nil:
		return map[string]any{}, nil
	case map[string]any:
		return typed, nil
	case string:
		result := map[string]any{}
		if err := json.Unmarshal([]byte(typed), &result); err != nil {
			return nil, fmt.Errorf("cannot convert string to object: %w", err)
		}
		return result, nil
	default:
		object, ok := rawObject(value)
		if ok {
			return object, nil
		}
		return nil, fmt.Errorf("cannot convert %T to object", value)
	}
}

func setJSONObject(value any) (map[string]any, error) {
	switch typed := value.(type) {
	case map[string]any:
		return deepCopySetMap(typed), nil
	case string:
		result := map[string]any{}
		if err := json.Unmarshal([]byte(typed), &result); err != nil {
			return nil, fmt.Errorf("json output must be an object: %w", err)
		}
		return result, nil
	default:
		object, ok := rawObject(value)
		if ok {
			return object, nil
		}
		return nil, fmt.Errorf("json output must be an object, got %T", value)
	}
}

func setNestedSetValue(target map[string]any, path string, value any) {
	parts := strings.Split(path, ".")
	current := target
	for index, part := range parts {
		if index == len(parts)-1 {
			current[part] = value
			return
		}
		next, ok := current[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[part] = next
		}
		current = next
	}
}

func deepCopySetMap(source map[string]any) map[string]any {
	if source == nil {
		return map[string]any{}
	}
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = deepCopySetValue(value)
	}
	return result
}

func deepCopySetValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return deepCopySetMap(typed)
	case []any:
		result := make([]any, len(typed))
		for index, value := range typed {
			result[index] = deepCopySetValue(value)
		}
		return result
	default:
		return typed
	}
}

func rawOptions(params map[string]any) map[string]any {
	options, ok := rawObject(params["options"])
	if !ok {
		return map[string]any{}
	}
	return options
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}
