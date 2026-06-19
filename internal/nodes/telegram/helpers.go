package telegram

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func stringParam(params map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := params[key]; ok {
			return fmt.Sprint(value)
		}
	}
	return ""
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

func floatParam(params map[string]any, key string) float64 {
	if value, ok := params[key]; ok {
		switch typed := value.(type) {
		case float64:
			return typed
		case float32:
			return float64(typed)
		case int:
			return float64(typed)
		case int64:
			return float64(typed)
		case json.Number:
			parsed, _ := typed.Float64()
			return parsed
		case string:
			parsed, _ := strconv.ParseFloat(typed, 64)
			return parsed
		}
	}
	return 0
}

func mapParam(params map[string]any, key string) map[string]any {
	if value, ok := params[key]; ok {
		if object, ok := value.(map[string]any); ok {
			return object
		}
	}
	return map[string]any{}
}

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

func parseJSONValue(raw string) (any, error) {
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil, err
	}
	return value, nil
}

func allowedUpdates(raw any) []string {
	result := []string{}
	switch typed := raw.(type) {
	case []string:
		return typed
	case []any:
		for _, value := range typed {
			if text := strings.TrimSpace(fmt.Sprint(value)); text != "" {
				result = append(result, text)
			}
		}
	case string:
		for _, value := range strings.Split(typed, ",") {
			if text := strings.TrimSpace(value); text != "" {
				result = append(result, text)
			}
		}
	}
	return result
}
