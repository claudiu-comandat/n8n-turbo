package api

import (
	"context"
	"encoding/json"
	"testing"
)

func TestWorkflowRunRequestTriggerNodeName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty", raw: ``, want: ""},
		{name: "null", raw: `null`, want: ""},
		{name: "string", raw: `"Manual Trigger"`, want: "Manual Trigger"},
		{name: "name field", raw: `{"name":"Webhook Trigger"}`, want: "Webhook Trigger"},
		{name: "node name field", raw: `{"nodeName":"Schedule Trigger"}`, want: "Schedule Trigger"},
		{name: "nested node", raw: `{"node":{"name":"Chat Trigger"}}`, want: "Chat Trigger"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			request := workflowRunRequest{TriggerToStart: json.RawMessage(tc.raw)}
			if got := request.triggerNodeName(); got != tc.want {
				t.Fatalf("triggerNodeName() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDynamicNodeParameterOptionsReturnsGeminiFallbackModels(t *testing.T) {
	t.Parallel()

	var request dynamicNodeParameterOptionsRequest
	request.NodeTypeAndVersion.Name = "@n8n/n8n-nodes-langchain.lmChatGoogleGemini"
	request.Path = "parameters.modelName"

	options, err := (&Server{}).dynamicNodeParameterOptions(context.Background(), request)
	if err != nil {
		t.Fatalf("dynamic options: %v", err)
	}
	if len(options) == 0 {
		t.Fatal("expected Gemini fallback models")
	}
	for _, option := range options {
		if option.Name == "" || option.Value == "" {
			t.Fatalf("option should have name and value: %#v", option)
		}
		if option.Value == "gpt-4o" || option.Value == "models/deep-research-pro-preview-12-2025" {
			t.Fatalf("Gemini options must not leak OpenAI models: %#v", option)
		}
	}
}

func TestDynamicCredentialIDFindsN8NCredentialReference(t *testing.T) {
	t.Parallel()

	id := dynamicCredentialID(map[string]any{
		"googlePalmApi": map[string]any{"id": "cred-123", "name": "Gemini"},
	}, "googlePalmApi")
	if id != "cred-123" {
		t.Fatalf("unexpected credential id %q", id)
	}

	byType := dynamicCredentialID(map[string]any{
		"credential": map[string]any{"id": "cred-456", "type": "googlePalmApi"},
	}, "googlePalmApi")
	if byType != "cred-456" {
		t.Fatalf("unexpected credential id by type %q", byType)
	}
}

func TestDynamicNodeParameterOptionsReturnsProviderSpecificFallbackModels(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		nodeType string
		path     string
		want     string
		notWant  string
	}{
		{
			name:     "deepseek",
			nodeType: "@n8n/n8n-nodes-langchain.lmChatDeepSeek",
			path:     "parameters.model",
			want:     "deepseek-chat",
			notWant:  "models/gemini-2.5-flash",
		},
		{
			name:     "openrouter",
			nodeType: "@n8n/n8n-nodes-langchain.lmChatOpenRouter",
			path:     "parameters.model",
			want:     "openai/gpt-4.1-mini",
			notWant:  "models/gemini-2.5-flash",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var request dynamicNodeParameterOptionsRequest
			request.NodeTypeAndVersion.Name = tc.nodeType
			request.Path = tc.path

			options, err := (&Server{}).dynamicNodeParameterOptions(context.Background(), request)
			if err != nil {
				t.Fatalf("dynamic options: %v", err)
			}
			found := false
			for _, option := range options {
				if option.Value == tc.want {
					found = true
				}
				if option.Value == tc.notWant {
					t.Fatalf("%s options leaked %s: %#v", tc.name, tc.notWant, options)
				}
			}
			if !found {
				t.Fatalf("%s options missing %s: %#v", tc.name, tc.want, options)
			}
		})
	}
}

func TestDynamicNodeParameterOptionsIgnoresUnknownPostgresMethod(t *testing.T) {
	t.Parallel()

	var request dynamicNodeParameterOptionsRequest
	request.NodeTypeAndVersion.Name = "n8n-nodes-base.postgres"
	request.MethodName = "unknown"

	options, err := (&Server{}).dynamicNodeParameterOptions(context.Background(), request)
	if err != nil {
		t.Fatalf("dynamic options: %v", err)
	}
	if len(options) != 0 {
		t.Fatalf("unexpected postgres options: %#v", options)
	}
}

func TestDynamicParamStringReadsResourceLocatorValue(t *testing.T) {
	t.Parallel()

	request := dynamicNodeParameterOptionsRequest{
		CurrentNodeParameters: map[string]any{
			"schema": map[string]any{"mode": "list", "value": "public"},
		},
		NodeParameters: map[string]any{
			"schema": "fallback",
		},
	}
	if got := dynamicParamString(request, "schema"); got != "public" {
		t.Fatalf("schema = %q, want public", got)
	}
}
