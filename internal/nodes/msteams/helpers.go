package msteams

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
			text := strings.TrimSpace(textValue(value))
			if text != "" && text != "<nil>" {
				return text
			}
		}
	}
	return ""
}

func textValue(value any) string {
	if object, ok := value.(map[string]any); ok {
		if raw, ok := object["value"]; ok {
			return textValue(raw)
		}
	}
	return fmt.Sprint(value)
}

func intValue(params map[string]any, key string) int {
	return int(int64Value(params, key))
}

func int64Value(params map[string]any, keys ...string) int64 {
	for _, key := range keys {
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
	}
	return 0
}

func boolValue(params map[string]any, key string, def bool) bool {
	if value, ok := params[key]; ok {
		switch typed := value.(type) {
		case bool:
			return typed
		case string:
			parsed, err := strconv.ParseBool(typed)
			if err == nil {
				return parsed
			}
		}
	}
	return def
}

func mapParam(params map[string]any, key string) map[string]any {
	if value, ok := params[key].(map[string]any); ok {
		return value
	}
	return map[string]any{}
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
			text := strings.TrimSpace(textValue(item))
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

func firstInput(input dataplane.Output) []dataplane.Item {
	if len(input) == 0 || len(input[0]) == 0 {
		return nil
	}
	return input[0]
}

func single(result map[string]any, err error) ([]dataplane.Item, error) {
	if err != nil {
		return nil, err
	}
	return []dataplane.Item{{JSON: result}}, nil
}

func itemsFromValue(result map[string]any, err error) ([]dataplane.Item, error) {
	if err != nil {
		return nil, err
	}
	raw, _ := result["value"].([]any)
	items := make([]dataplane.Item, 0, len(raw))
	for _, value := range raw {
		if object, ok := value.(map[string]any); ok {
			items = append(items, dataplane.Item{JSON: object})
		}
	}
	return items, nil
}
