package shopify

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func stringValue(params map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := params[key]; ok {
			text := strings.TrimSpace(fmt.Sprint(value))
			if text != "" && text != "<nil>" {
				return text
			}
		}
	}
	return ""
}

func stringParam(params map[string]any, keys ...string) string {
	return stringValue(params, keys...)
}

func intParam(params map[string]any, key string) int {
	if value, ok := params[key]; ok {
		switch typed := value.(type) {
		case int:
			return typed
		case int64:
			return int(typed)
		case float64:
			return int(typed)
		case json.Number:
			parsed, _ := typed.Int64()
			return int(parsed)
		case string:
			parsed, _ := strconv.Atoi(typed)
			return parsed
		}
	}
	return 0
}

func int64Param(params map[string]any, key string) int64 {
	if value, ok := params[key]; ok {
		switch typed := value.(type) {
		case int:
			return int64(typed)
		case int64:
			return typed
		case float64:
			return int64(typed)
		case json.Number:
			parsed, _ := typed.Int64()
			return parsed
		case string:
			parsed, _ := strconv.ParseInt(typed, 10, 64)
			return parsed
		}
	}
	return 0
}

func boolParam(params map[string]any, key string) bool {
	if value, ok := params[key]; ok {
		switch typed := value.(type) {
		case bool:
			return typed
		case string:
			parsed, _ := strconv.ParseBool(typed)
			return parsed
		}
	}
	return false
}

func stringSlice(params map[string]any, key string) []string {
	value, ok := params[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := []string{}
		for _, item := range typed {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text != "" {
				out = append(out, text)
			}
		}
		return out
	case string:
		out := []string{}
		for _, part := range strings.Split(typed, ",") {
			text := strings.TrimSpace(part)
			if text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func mapParam(params map[string]any, key string) (map[string]any, error) {
	value, ok := params[key]
	if !ok || value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case map[string]any:
		return typed, nil
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil, nil
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(typed), &out); err != nil {
			return nil, err
		}
		return out, nil
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return nil, err
		}
		var out map[string]any
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, err
		}
		return out, nil
	}
}

func arrayParam(params map[string]any, key string) ([]any, error) {
	value, ok := params[key]
	if !ok || value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case []any:
		return typed, nil
	case []map[string]any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out, nil
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil, nil
		}
		var out []any
		if err := json.Unmarshal([]byte(typed), &out); err != nil {
			return nil, err
		}
		return out, nil
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return nil, err
		}
		var out []any
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, err
		}
		return out, nil
	}
}

func firstInput(input dataplane.Output) []dataplane.Item {
	if len(input) == 0 || len(input[0]) == 0 {
		return nil
	}
	return input[0]
}

func singleValue(value any, err error) ([]dataplane.Item, error) {
	if err != nil {
		return nil, err
	}
	if object, ok := value.(map[string]any); ok {
		return []dataplane.Item{{JSON: object}}, nil
	}
	return []dataplane.Item{{JSON: map[string]any{"result": value}}}, nil
}

func itemsFromArray(values []any, err error) ([]dataplane.Item, error) {
	if err != nil {
		return nil, err
	}
	items := make([]dataplane.Item, 0, len(values))
	for _, value := range values {
		if object, ok := value.(map[string]any); ok {
			items = append(items, dataplane.Item{JSON: object})
		} else {
			items = append(items, dataplane.Item{JSON: map[string]any{"result": value}})
		}
	}
	return items, nil
}

func listFrom(result map[string]any, key string) []any {
	raw, _ := result[key].([]any)
	return raw
}

func setString(body map[string]any, key string, value string) {
	if value != "" {
		body[key] = value
	}
}

func setInt64(body map[string]any, key string, value int64) {
	if value != 0 {
		body[key] = value
	}
}
