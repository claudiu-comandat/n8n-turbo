package sendgrid

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
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
		for _, item := range strings.Split(typed, ",") {
			text := strings.TrimSpace(item)
			if text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func listFrom(result map[string]any, key string) []map[string]any {
	raw, _ := result[key].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if object, ok := item.(map[string]any); ok {
			out = append(out, object)
		}
	}
	return out
}
