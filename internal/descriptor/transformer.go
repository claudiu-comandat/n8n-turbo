package descriptor

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type ResponseTransformer struct{}

func NewResponseTransformer() *ResponseTransformer {
	return &ResponseTransformer{}
}

func (t *ResponseTransformer) Apply(data any, expr string) (any, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" || expr == "." {
		return data, nil
	}
	parts := strings.SplitN(expr, "|", 2)
	result, err := t.applySegment(data, strings.TrimSpace(parts[0]))
	if err != nil {
		return nil, err
	}
	if len(parts) > 1 {
		return t.Apply(result, parts[1])
	}
	return result, nil
}

func (t *ResponseTransformer) ApplyToItems(data []any, expr string) ([]any, error) {
	result := make([]any, 0, len(data))
	for _, item := range data {
		transformed, err := t.Apply(item, expr)
		if err != nil {
			return nil, err
		}
		result = append(result, transformed)
	}
	return result, nil
}

func (t *ResponseTransformer) applySegment(data any, segment string) (any, error) {
	if segment == "" || segment == "." {
		return data, nil
	}
	if segment == ".[]" {
		if arr, ok := data.([]any); ok {
			return arr, nil
		}
		return nil, fmt.Errorf("transformer: value is not an array")
	}
	if strings.HasPrefix(segment, ".") {
		return accessTransformPath(data, strings.TrimPrefix(segment, "."))
	}
	return data, nil
}

func accessTransformPath(data any, path string) (any, error) {
	current := data
	for _, token := range transformTokens(path) {
		switch typed := current.(type) {
		case map[string]any:
			current = typed[token]
		case []any:
			index, err := strconv.Atoi(strings.Trim(token, "[]"))
			if err != nil || index < 0 || index >= len(typed) {
				return nil, fmt.Errorf("transformer: index %s out of bounds", token)
			}
			current = typed[index]
		default:
			return nil, fmt.Errorf("transformer: cannot access %s on %T", token, current)
		}
	}
	return current, nil
}

func transformTokens(path string) []string {
	if path == "" {
		return nil
	}
	parts := strings.Split(path, ".")
	tokens := make([]string, 0, len(parts))
	for _, part := range parts {
		for part != "" {
			bracket := strings.Index(part, "[")
			if bracket < 0 {
				tokens = append(tokens, part)
				break
			}
			if bracket > 0 {
				tokens = append(tokens, part[:bracket])
			}
			end := strings.Index(part[bracket:], "]")
			if end < 0 {
				tokens = append(tokens, part[bracket:])
				break
			}
			end += bracket
			tokens = append(tokens, part[bracket:end+1])
			part = part[end+1:]
		}
	}
	return tokens
}

func MarshalForDebug(value any) string {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprintf("marshal error: %v", err)
	}
	return string(data)
}
