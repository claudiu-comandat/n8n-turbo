package nodes

import (
	"context"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type Webhook struct{}

type ErrorTrigger struct{}

type ExecuteWorkflowTrigger struct{}

type ScheduleTrigger struct{}

type RespondToWebhook struct{}

func (Webhook) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	if len(in.InputData) > 0 && len(in.InputData[0]) > 0 {
		return dataplane.MainOutput(in.InputData[0]), nil
	}
	return dataplane.MainOutput([]dataplane.Item{{JSON: map[string]any{"headers": map[string]any{}, "query": map[string]any{}, "body": map[string]any{}}}}), nil
}

func (ErrorTrigger) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	if len(in.InputData) > 0 && len(in.InputData[0]) > 0 {
		return dataplane.MainOutput(in.InputData[0]), nil
	}
	return dataplane.MainOutput([]dataplane.Item{{JSON: map[string]any{"error": map[string]any{}, "$execution": map[string]any{}, "$workflow": map[string]any{}}}}), nil
}

func (ExecuteWorkflowTrigger) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	items := firstInput(in.InputData)
	if len(items) == 0 {
		items = []dataplane.Item{{JSON: map[string]any{}}}
	}
	inputSource := stringParam(in.Node.Parameters, "inputSource")
	if inputSource == "workflowInputs" {
		items = filterWorkflowInputItems(items, in.Node.Parameters["workflowInputs"])
	}
	return dataplane.MainOutput(items), nil
}

func (RespondToWebhook) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return dataplane.MainOutput(firstInput(in.InputData)), nil
}

func filterWorkflowInputItems(items []dataplane.Item, raw any) []dataplane.Item {
	object, ok := rawObject(raw)
	if !ok {
		return items
	}
	values, ok := object["values"].([]any)
	if !ok || len(values) == 0 {
		return items
	}
	keys := make([]string, 0, len(values))
	for _, value := range values {
		entry, ok := rawObject(value)
		if !ok {
			continue
		}
		name := stringParam(entry, "name")
		if name != "" {
			keys = append(keys, name)
		}
	}
	if len(keys) == 0 {
		return items
	}
	result := make([]dataplane.Item, 0, len(items))
	for _, item := range items {
		next := dataplane.Item{JSON: map[string]any{}, Binary: item.Binary, PairedItem: item.PairedItem}
		for _, key := range keys {
			if value, ok := item.JSON[key]; ok {
				next.JSON[key] = value
			} else {
				next.JSON[key] = nil
			}
		}
		result = append(result, next)
	}
	return result
}
