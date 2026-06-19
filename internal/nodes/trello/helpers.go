package trello

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

func itemsFromArray(values []any, err error) ([]dataplane.Item, error) {
	if err != nil {
		return nil, err
	}
	items := make([]dataplane.Item, 0, len(values))
	for _, value := range values {
		if object, ok := value.(map[string]any); ok {
			items = append(items, dataplane.Item{JSON: object})
		}
	}
	return items, nil
}

func setString(body map[string]any, key string, value string) {
	if value != "" {
		body[key] = value
	}
}
