package engine

import (
	"time"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func buildErrorOutput(node dataplane.Node, inputs dataplane.Output, err error) dataplane.Output {
	items := errorItems(node, flattenOutput(inputs), err)
	switch node.EffectiveOnError() {
	case dataplane.OnErrorContinueErrorOutput:
		return dataplane.Output{[]dataplane.Item{}, items}
	default:
		return dataplane.Output{items}
	}
}

func errorItems(node dataplane.Node, inputs []dataplane.Item, err error) []dataplane.Item {
	if len(inputs) == 0 {
		inputs = []dataplane.Item{{JSON: map[string]any{}}}
	}
	result := make([]dataplane.Item, 0, len(inputs))
	for _, item := range inputs {
		next := dataplane.Item{JSON: make(map[string]any, len(item.JSON)+1), Binary: item.Binary, PairedItem: item.PairedItem}
		for key, value := range item.JSON {
			next.JSON[key] = value
		}
		next.Error = &dataplane.NodeError{
			Name:      "NodeOperationError",
			Message:   err.Error(),
			Timestamp: time.Now().UTC().UnixMilli(),
			Context:   map[string]any{"node": node.Name, "nodeType": node.Type},
		}
		next.JSON["error"] = map[string]any{
			"message":   err.Error(),
			"name":      next.Error.Name,
			"node":      node.Name,
			"nodeType":  node.Type,
			"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		}
		result = append(result, next)
	}
	return result
}

func flattenOutput(output dataplane.Output) []dataplane.Item {
	items := make([]dataplane.Item, 0)
	for _, branch := range output {
		items = append(items, branch...)
	}
	return items
}
