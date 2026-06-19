package twilio

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

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

func appendQuery(raw string, query url.Values) string {
	if len(query) == 0 {
		return raw
	}
	if strings.Contains(raw, "?") {
		return raw + "&" + query.Encode()
	}
	return raw + "?" + query.Encode()
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

func rootFromBase(baseURL string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if index := strings.Index(baseURL, "/2010-04-01"); index >= 0 {
		return baseURL[:index]
	}
	return baseURL
}

func formBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func formAdd(values url.Values, key string, value string) {
	if strings.TrimSpace(value) != "" {
		values.Set(key, value)
	}
}

func formAddInt(values url.Values, key string, value int) {
	if value != 0 {
		values.Set(key, fmt.Sprint(value))
	}
}
