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
	case "json", "raw":
		return setJSONFields(in, items, itemIndex, item, params)
	default:
		return dataplane.Item{}, fmt.Errorf("unknown set mode %q", mode)
	}
}

func setManualFields(in engine.ExecuteInput, items []dataplane.Item, itemIndex int, item dataplane.Item, params map[string]any) (dataplane.Item, error) {
	next := dataplane.Item{JSON: setBaseJSON(item, params, in.Node.TypeVersion), Binary: setBaseBinary(item, params, in.Node.TypeVersion), PairedItem: &dataplane.PairedItem{Item: itemIndex}, Error: item.Error}
	fields := collectSetFields(params)
	dotNotation := boolParam(rawOptions(params), "dotNotation", boolParam(params, "dotNotation", true))
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
	next := dataplane.Item{JSON: setBaseJSON(item, params, in.Node.TypeVersion), Binary: setBaseBinary(item, params, in.Node.TypeVersion), PairedItem: &dataplane.PairedItem{Item: itemIndex}, Error: item.Error}
	raw := firstNonNil(params["jsonOutput"], params["json"], params["value"])
	if raw == nil || fmt.Sprint(raw) == "" {
		return next, nil
	}
	resolved := resolveValue(in, items, itemIndex, raw)
	output, err := setJSONObject(resolved)
	if err != nil {
		return dataplane.Item{}, err
	}
	for key, value := range output {
		next.JSON[key] = value
	}
	return next, nil
}

func setBaseJSON(item dataplane.Item, params map[string]any, nodeVersion float64) map[string]any {
	dotNotation := boolParam(rawOptions(params), "dotNotation", boolParam(params, "dotNotation", true))
	switch effectiveSetInclude(params, nodeVersion) {
	case "none":
		return map[string]any{}
	case "selected":
		return copySetFields(item.JSON, stringParam(params, "includeFields"), dotNotation)
	case "except":
		next := deepCopySetMap(item.JSON)
		for _, field := range splitSetFieldList(stringParam(params, "excludeFields")) {
			if dotNotation {
				unsetNestedSetValue(next, field)
			} else {
				delete(next, field)
			}
		}
		return next
	case "all":
		return deepCopySetMap(item.JSON)
	}
	return map[string]any{}
}

func setBaseBinary(item dataplane.Item, params map[string]any, nodeVersion float64) map[string]dataplane.Binary {
	if len(item.Binary) == 0 {
		return nil
	}
	include := effectiveSetInclude(params, nodeVersion)
	if include == "none" {
		return nil
	}
	options := rawOptions(params)
	if nodeVersion >= 3.4 {
		if boolParam(options, "stripBinary", true) {
			return nil
		}
	} else if !boolParam(options, "includeBinary", true) {
		return nil
	}
	return item.Binary
}

func effectiveSetInclude(params map[string]any, nodeVersion float64) string {
	include := strings.ToLower(strings.TrimSpace(stringParam(params, "include")))
	if include == "" {
		include = "all"
	}
	if boolParam(params, "keepOnlySet", false) {
		return "none"
	}
	if nodeVersion >= 3.3 && !boolParam(params, "includeOtherFields", false) {
		return "none"
	}
	if nodeVersion < 3.3 && !boolParam(params, "includeOtherFields", true) && include == "" {
		return "none"
	}
	return include
}

func copySetFields(source map[string]any, rawFields string, dotNotation bool) map[string]any {
	result := map[string]any{}
	for _, field := range splitSetFieldList(rawFields) {
		if dotNotation && strings.Contains(field, ".") {
			if value := nestedIFValue(source, field); value != nil {
				setNestedSetValue(result, lastSetPathSegment(field), deepCopySetValue(value))
			}
			continue
		}
		if value, ok := source[field]; ok {
			result[field] = deepCopySetValue(value)
		}
	}
	return result
}

func splitSetFieldList(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == '\t'
	})
	fields := make([]string, 0, len(parts))
	for _, part := range parts {
		if field := strings.TrimSpace(part); field != "" {
			fields = append(fields, field)
		}
	}
	return fields
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
	fieldType = strings.TrimSuffix(fieldType, "Value")
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

func unsetNestedSetValue(target map[string]any, path string) {
	parts := strings.Split(path, ".")
	current := target
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

func lastSetPathSegment(path string) string {
	parts := strings.Split(path, ".")
	return parts[len(parts)-1]
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
