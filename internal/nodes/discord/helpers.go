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

func stringSlice(params map[string]any, key string) []string {
	value, ok := params[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
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

func parseEmbeds(params map[string]any) ([]map[string]any, error) {
	if values, ok := mapParam(params, "embeds")["values"].([]any); ok {
		return parseEmbedValues(values)
	}
	raw := stringParam(params, "embed", "embeds")
	if raw == "" {
		return nil, nil
	}
	var embeds []map[string]any
	if strings.HasPrefix(strings.TrimSpace(raw), "[") {
		if err := json.Unmarshal([]byte(raw), &embeds); err != nil {
			return nil, err
		}
		return embeds, nil
	}
	var embed map[string]any
	if err := json.Unmarshal([]byte(raw), &embed); err != nil {
		return nil, err
	}
	return []map[string]any{embed}, nil
}

func parseEmbedValues(values []any) ([]map[string]any, error) {
	out := []map[string]any{}
	for _, value := range values {
		embed, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if stringParam(embed, "inputMethod") == "json" {
			raw := embed["json"]
			if object, ok := raw.(map[string]any); ok {
				out = append(out, object)
				continue
			}
			var parsed map[string]any
			if err := json.Unmarshal([]byte(textValue(raw)), &parsed); err != nil {
				return nil, err
			}
			out = append(out, parsed)
			continue
		}
		body := map[string]any{}
		for _, key := range []string{"description", "timestamp", "title", "url"} {
			if text := stringParam(embed, key); text != "" {
				body[key] = text
			}
		}
		if author := stringParam(embed, "author"); author != "" {
			body["author"] = map[string]any{"name": author}
		}
		if color := stringParam(embed, "color"); color != "" {
			parsed, _ := strconv.ParseInt(strings.TrimPrefix(color, "#"), 16, 64)
			if parsed > 0 {
				body["color"] = parsed
			}
		}
		for _, key := range []string{"image", "thumbnail"} {
			if url := stringParam(embed, key); url != "" {
				body[key] = map[string]any{"url": url}
			}
		}
		if video := stringParam(embed, "video"); video != "" {
			body["video"] = map[string]any{"url": video, "width": 1270, "height": 720}
		}
		if len(body) > 0 {
			out = append(out, body)
		}
	}
	return out, nil
}

func outputList(values []map[string]any) []map[string]any {
	if values == nil {
		return []map[string]any{}
	}
	return values
}
