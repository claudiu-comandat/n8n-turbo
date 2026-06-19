package nodes

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
	"github.com/n8n-io/n8n-turbo/internal/expr"
)

func firstInput(input dataplane.Output) []dataplane.Item {
	if len(input) == 0 {
		return []dataplane.Item{}
	}
	return input[0]
}

func cloneItem(item dataplane.Item) dataplane.Item {
	next := dataplane.Item{JSON: make(map[string]any, len(item.JSON)), Binary: item.Binary, PairedItem: item.PairedItem}
	for key, value := range item.JSON {
		next.JSON[key] = value
	}
	if next.JSON == nil {
		next.JSON = map[string]any{}
	}
	return next
}

func stringParam(params map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := params[key]; ok {
			return fmt.Sprint(value)
		}
	}
	return ""
}

func intParam(params map[string]any, key string, fallback int) int {
	value, ok := params[key]
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case int:
		return typed
	case float64:
		return int(typed)
	case string:
		parsed, err := strconv.Atoi(typed)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func boolParam(params map[string]any, key string, fallback bool) bool {
	value, ok := params[key]
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(typed, "true")
	default:
		return fallback
	}
}

func resolveValue(in engine.ExecuteInput, items []dataplane.Item, itemIndex int, value any) any {
	if in.Expr == nil {
		return value
	}
	return in.Expr.MustResolve(value, expr.Context{
		Items:         items,
		CurrentIndex:  itemIndex,
		RunData:       in.RunData,
		Variables:     in.Variables,
		Secrets:       in.Secrets,
		WorkflowID:    in.WorkflowID,
		WorkflowName:  in.WorkflowName,
		ExecutionID:   in.ExecutionID,
		ExecutionMode: in.ExecutionMode,
		ResumeURL:     in.ResumeURL,
		ResumeFormURL: in.ResumeFormURL,
		ScheduledTime: in.ScheduledTime,
	})
}

func rawObject(value any) (map[string]any, bool) {
	if value == nil {
		return nil, false
	}
	if typed, ok := value.(map[string]any); ok {
		return typed, true
	}
	bytes, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	result := map[string]any{}
	if err := json.Unmarshal(bytes, &result); err != nil {
		return nil, false
	}
	return result, true
}
