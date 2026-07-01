package nodes

import (
	"context"
	"testing"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

func TestRespondToWebhookCanExposeOfficialResponseOutput(t *testing.T) {
	t.Parallel()

	output, err := (RespondToWebhook{}).Execute(context.Background(), engine.ExecuteInput{
		Node: dataplane.Node{
			Name: "Respond to Webhook",
			Type: "n8n-nodes-base.respondToWebhook",
			Parameters: map[string]any{
				"respondWith":          "text",
				"responseBody":         "ok",
				"responseCode":         201,
				"enableResponseOutput": true,
				"responseHeaders": map[string]any{
					"entries": []any{
						map[string]any{"name": "X-Test", "value": "yes"},
					},
				},
			},
		},
		InputData: dataplane.MainOutput([]dataplane.Item{{JSON: map[string]any{"id": 1}}}),
	})
	if err != nil {
		t.Fatalf("execute respond node: %v", err)
	}
	if len(output) != 2 {
		t.Fatalf("expected two outputs, got %#v", output)
	}
	response := output[1][0].JSON["response"].(map[string]any)
	if response["body"] != "ok" || response["statusCode"] != 201 {
		t.Fatalf("unexpected response output: %#v", response)
	}
	headers := response["headers"].(map[string]any)
	if headers["X-Test"] != "yes" {
		t.Fatalf("unexpected headers: %#v", headers)
	}
}

func TestRespondToWebhookResponseKeyWrapsIncomingItems(t *testing.T) {
	t.Parallel()

	output, err := (RespondToWebhook{}).Execute(context.Background(), engine.ExecuteInput{
		Node: dataplane.Node{
			Name: "Respond to Webhook",
			Type: "n8n-nodes-base.respondToWebhook",
			Parameters: map[string]any{
				"respondWith":          "firstIncomingItem",
				"enableResponseOutput": true,
				"options": map[string]any{
					"responseKey": "data.item",
				},
			},
		},
		InputData: dataplane.MainOutput([]dataplane.Item{{JSON: map[string]any{"id": 1}}}),
	})
	if err != nil {
		t.Fatalf("execute respond node: %v", err)
	}
	response := output[1][0].JSON["response"].(map[string]any)
	body := response["body"].(map[string]any)
	data := body["data"].(map[string]any)
	item := data["item"].(map[string]any)
	if item["id"] != 1 {
		t.Fatalf("unexpected wrapped response body: %#v", body)
	}
}

func TestExecuteWorkflowTriggerAppliesSchemaFallbackDefaults(t *testing.T) {
	t.Parallel()

	output, err := (ExecuteWorkflowTrigger{}).Execute(context.Background(), engine.ExecuteInput{
		Node: dataplane.Node{
			Name: "Execute Workflow Trigger",
			Type: "n8n-nodes-base.executeWorkflowTrigger",
			Parameters: map[string]any{
				"inputSource": "workflowInputs",
				"workflowInputs": map[string]any{"values": []any{
					map[string]any{"name": "name", "type": "string"},
					map[string]any{"name": "count", "type": "number"},
					map[string]any{"name": "enabled", "type": "boolean"},
				}},
			},
		},
		InputData: dataplane.MainOutput([]dataplane.Item{{JSON: map[string]any{"name": "Ana", "drop": true}}}),
	})
	if err != nil {
		t.Fatalf("execute workflow trigger: %v", err)
	}
	got := output[0][0].JSON
	if got["name"] != "Ana" || got["count"] != float64(0) || got["enabled"] != false {
		t.Fatalf("unexpected schema defaults: %#v", got)
	}
	if _, ok := got["drop"]; ok {
		t.Fatalf("schema output should trim unknown fields: %#v", got)
	}
}

func TestErrorTriggerManualEmptyInputReturnsOfficialExample(t *testing.T) {
	t.Parallel()

	output, err := (ErrorTrigger{}).Execute(context.Background(), engine.ExecuteInput{
		ExecutionMode: "manual",
		InputData:     dataplane.MainOutput([]dataplane.Item{{JSON: map[string]any{}}}),
	})
	if err != nil {
		t.Fatalf("error trigger execute: %v", err)
	}
	execution := output[0][0].JSON["execution"].(map[string]any)
	workflow := output[0][0].JSON["workflow"].(map[string]any)
	if execution["id"] != 231 || execution["lastNodeExecuted"] != "Node With Error" || workflow["name"] != "Example Workflow" {
		t.Fatalf("unexpected example error trigger output: %#v", output[0][0].JSON)
	}
}

func TestScheduleTriggerItemMatchesOfficialOutputShape(t *testing.T) {
	t.Parallel()

	location, err := time.LoadLocation("Europe/Bucharest")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	item := ScheduledTriggerItem(dataplane.Node{Name: "Schedule"}, time.Date(2026, 6, 25, 14, 5, 6, 0, location))
	json := item.JSON
	if json["timestamp"] != "2026-06-25T14:05:06.000+03:00" ||
		json["Readable date"] != "June 25th 2026, 2:05:06 pm" ||
		json["Readable time"] != "2:05:06 pm" ||
		json["Day of week"] != "Thursday" ||
		json["Timezone"] != "Europe/Bucharest (UTC+03:00)" {
		t.Fatalf("unexpected schedule trigger output: %#v", json)
	}
	if _, ok := json["scheduled"]; ok {
		t.Fatalf("schedule trigger should not emit non-original scheduled flag: %#v", json)
	}
}
