package discord

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func stringParam(params map[string]any, keys ...string) string {
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

func stringValue(params map[string]any, keys ...string) string {
	return stringParam(params, keys...)
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

func mapParam(params map[string]any, key string) map[string]any {
	if value, ok := params[key]; ok {
		if object, ok := value.(map[string]any); ok {
			return object
		}
	}
	return map[string]any{}
}

func parseEmbeds(params map[string]any) ([]Embed, error) {
	raw := stringParam(params, "embed", "embeds")
	if raw == "" {
		return nil, nil
	}
	var embeds []Embed
	if strings.HasPrefix(strings.TrimSpace(raw), "[") {
		if err := json.Unmarshal([]byte(raw), &embeds); err != nil {
			return nil, err
		}
		return embeds, nil
	}
	var embed Embed
	if err := json.Unmarshal([]byte(raw), &embed); err != nil {
		return nil, err
	}
	return []Embed{embed}, nil
}

func outputList(values []map[string]any) []map[string]any {
	if values == nil {
		return []map[string]any{}
	}
	return values
}
