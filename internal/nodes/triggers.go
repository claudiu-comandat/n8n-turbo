package nodes

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

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
		if isManualEmptyErrorTriggerInput(in) {
			return dataplane.MainOutput([]dataplane.Item{exampleErrorTriggerItem()}), nil
		}
		return dataplane.MainOutput(in.InputData[0]), nil
	}
	if in.ExecutionMode == "manual" {
		return dataplane.MainOutput([]dataplane.Item{exampleErrorTriggerItem()}), nil
	}
	return dataplane.MainOutput([]dataplane.Item{{JSON: map[string]any{"error": map[string]any{}, "$execution": map[string]any{}, "$workflow": map[string]any{}}}}), nil
}

func isManualEmptyErrorTriggerInput(in engine.ExecuteInput) bool {
	if in.ExecutionMode != "manual" || len(in.InputData) == 0 || len(in.InputData[0]) != 1 {
		return false
	}
	item := in.InputData[0][0]
	return len(item.JSON) == 0 && item.Binary == nil
}

func exampleErrorTriggerItem() dataplane.Item {
	return dataplane.Item{JSON: map[string]any{
		"execution": map[string]any{
			"id":               231,
			"url":              "/execution/workflow/1/231",
			"retryOf":          "34",
			"lastNodeExecuted": "Node With Error",
			"mode":             "manual",
			"error": map[string]any{
				"message": "Example Error Message",
				"stack":   "Stacktrace",
			},
		},
		"workflow": map[string]any{
			"id":   "1",
			"name": "Example Workflow",
		},
	}}
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
	items := firstInput(in.InputData)
	if !boolParam(in.Node.Parameters, "enableResponseOutput", false) {
		return dataplane.MainOutput(items), nil
	}
	return dataplane.Output{
		items,
		[]dataplane.Item{{JSON: map[string]any{"response": respondToWebhookResponse(in.Node.Parameters, items)}}},
	}, nil
}

func respondToWebhookResponse(params map[string]any, items []dataplane.Item) map[string]any {
	statusCode := intParam(params, "statusCode", intParam(params, "responseCode", http.StatusOK))
	response := map[string]any{
		"body":       respondToWebhookBody(params, items),
		"headers":    respondToWebhookHeaders(params),
		"statusCode": statusCode,
	}
	if strings.EqualFold(stringParam(params, "respondWith"), "redirect") && statusCode == http.StatusOK {
		response["statusCode"] = http.StatusFound
	}
	if strings.EqualFold(stringParam(params, "respondWith"), "noData") && statusCode == http.StatusOK {
		response["statusCode"] = http.StatusNoContent
	}
	return response
}

func respondToWebhookBody(params map[string]any, items []dataplane.Item) any {
	options, _ := rawObject(params["options"])
	switch strings.ToLower(firstNonEmptyNode(stringParam(params, "respondWith"), "firstIncomingItem")) {
	case "allincomingitems":
		body := make([]map[string]any, 0, len(items))
		for _, item := range items {
			body = append(body, item.JSON)
		}
		if key := stringParam(options, "responseKey"); key != "" {
			return nestedMapValue(key, body)
		}
		return body
	case "text":
		return stringParam(params, "responseBody")
	case "json":
		raw := firstNonEmptyNode(stringParam(params, "responseBody"), "{}")
		var body any
		if err := json.Unmarshal([]byte(raw), &body); err != nil {
			return map[string]any{"error": "invalid response json", "message": err.Error()}
		}
		return body
	case "nodata":
		return nil
	case "redirect":
		return nil
	default:
		var body any
		if len(items) > 0 {
			body = items[0].JSON
		}
		if key := stringParam(options, "responseKey"); key != "" {
			return nestedMapValue(key, body)
		}
		return body
	}
}

func respondToWebhookHeaders(params map[string]any) map[string]any {
	headers := map[string]any{}
	addHeaders := func(raw any) {
		object, ok := rawObject(raw)
		if !ok {
			return
		}
		for key, value := range object {
			if strings.EqualFold(key, "entries") || strings.EqualFold(key, "values") || strings.EqualFold(key, "parameter") {
				if entries, ok := value.([]any); ok {
					for _, entry := range entries {
						header, ok := rawObject(entry)
						if !ok {
							continue
						}
						name := firstNonEmptyNode(stringParam(header, "name"), stringParam(header, "key"))
						if name != "" {
							headers[name] = firstNonEmptyNode(stringParam(header, "value"), stringParam(header, "headerValue"))
						}
					}
				}
				continue
			}
			headers[key] = value
		}
	}
	addHeaders(params["responseHeaders"])
	options, _ := rawObject(params["options"])
	addHeaders(options["responseHeaders"])
	return headers
}

func nestedMapValue(path string, value any) map[string]any {
	result := map[string]any{}
	current := result
	parts := strings.Split(path, ".")
	for _, part := range parts[:len(parts)-1] {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		next := map[string]any{}
		current[part] = next
		current = next
	}
	key := strings.TrimSpace(parts[len(parts)-1])
	if key == "" {
		key = path
	}
	current[key] = value
	return result
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
	defaults := map[string]any{}
	for _, value := range values {
		entry, ok := rawObject(value)
		if !ok {
			continue
		}
		name := stringParam(entry, "name")
		if name != "" {
			keys = append(keys, name)
			defaults[name] = workflowInputDefaultValue(stringParam(entry, "type"))
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
				next.JSON[key] = defaults[key]
			}
		}
		result = append(result, next)
	}
	return result
}

func workflowInputDefaultValue(valueType string) any {
	switch strings.ToLower(strings.TrimSpace(valueType)) {
	case "number":
		return float64(0)
	case "boolean":
		return false
	case "array":
		return []any{}
	case "object":
		return map[string]any{}
	default:
		return ""
	}
}
