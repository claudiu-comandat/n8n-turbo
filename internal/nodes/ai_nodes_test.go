package nodes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

func TestAIAgentUsesConnectedGeminiModel(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("key"); got != "gemini-secret" {
			t.Fatalf("unexpected api key %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"Salut, Ana"}]}}]}`))
	}))
	defer server.Close()

	registry := engine.NewRegistry()
	registry.Register("n8n-nodes-base.manualTrigger", ManualTrigger{})
	registry.Register("n8n-nodes-base.stickyNote", StickyNote{})
	registry.Register("@n8n/n8n-nodes-langchain.agent", AIAgent{})
	registry.Register("@n8n/n8n-nodes-langchain.lmChatGoogleGemini", GoogleGeminiChatModel{})

	workflow := dataplane.Workflow{
		ID:   "wf-ai",
		Name: "AI workflow",
		Nodes: []dataplane.Node{
			{Name: "Manual Trigger", Type: "n8n-nodes-base.manualTrigger"},
			{
				Name: "Google Gemini Chat Model",
				Type: "@n8n/n8n-nodes-langchain.lmChatGoogleGemini",
				Parameters: map[string]any{
					"modelName": "models/gemini-2.5-flash",
				},
			},
			{
				Name: "AI Agent",
				Type: "@n8n/n8n-nodes-langchain.agent",
				Parameters: map[string]any{
					"promptType": "define",
					"text":       "={{ $json.prompt }}",
					"options": map[string]any{
						"systemMessage": "Be concise.",
					},
				},
			},
		},
		Connections: dataplane.Connections{
			"Manual Trigger": {
				"main": [][]dataplane.Connection{{
					{Node: "AI Agent", Type: "main", Index: 0},
				}},
			},
			"Google Gemini Chat Model": {
				"ai_languageModel": [][]dataplane.Connection{{
					{Node: "AI Agent", Type: "ai_languageModel", Index: 0},
				}},
			},
		},
	}

	evaluator := engine.NewEvaluator(registry)
	result, err := evaluator.ExecuteWithOptions(context.Background(), workflow, "exec-ai", engine.ExecuteOptions{
		TriggerNode:  "Manual Trigger",
		TriggerItems: []dataplane.Item{{JSON: map[string]any{"prompt": "Salut-o pe Ana"}}},
		Credentials: func(ctx context.Context, node dataplane.Node) (map[string]map[string]any, error) {
			if node.Type != "@n8n/n8n-nodes-langchain.lmChatGoogleGemini" {
				return nil, nil
			}
			return map[string]map[string]any{
				"googlePalmApi": {
					"type":   "googlePalmApi",
					"host":   server.URL,
					"apiKey": "gemini-secret",
				},
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("execute workflow: %v", err)
	}
	got := result.RunData["AI Agent"][0].Data["main"][0][0].JSON["output"]
	if got != "Salut, Ana" {
		t.Fatalf("unexpected agent output: %#v", got)
	}
}

func TestGeminiPromptSendsOfficialOptions(t *testing.T) {
	t.Parallel()

	requests := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		requests <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`))
	}))
	defer server.Close()

	_, err := executeGeminiPrompt(context.Background(), "Salut", "", engine.ConnectedNode{
		Node: dataplane.Node{
			Name: "Google Gemini Chat Model",
			Type: "@n8n/n8n-nodes-langchain.lmChatGoogleGemini",
			Parameters: map[string]any{
				"modelName": "models/gemini-2.5-flash",
				"options": map[string]any{
					"maxOutputTokens": 321,
					"temperature":     0.2,
					"topK":            12,
					"topP":            0.8,
					"safetySettings": map[string]any{
						"values": []any{
							map[string]any{
								"category":  "HARM_CATEGORY_DANGEROUS_CONTENT",
								"threshold": "BLOCK_ONLY_HIGH",
							},
						},
					},
				},
			},
		},
		Credentials: map[string]map[string]any{
			"googlePalmApi": {
				"type":   "googlePalmApi",
				"host":   server.URL,
				"apiKey": "gemini-secret",
			},
		},
	})
	if err != nil {
		t.Fatalf("execute gemini prompt: %v", err)
	}

	body := <-requests
	config, ok := body["generationConfig"].(map[string]any)
	if !ok {
		t.Fatalf("missing generationConfig in request: %#v", body)
	}
	if config["maxOutputTokens"] != float64(321) || config["temperature"] != 0.2 || config["topK"] != float64(12) || config["topP"] != 0.8 {
		t.Fatalf("unexpected generationConfig: %#v", config)
	}
	safety, ok := body["safetySettings"].([]any)
	if !ok || len(safety) != 1 {
		t.Fatalf("unexpected safetySettings: %#v", body["safetySettings"])
	}
}

func TestOpenAICompatiblePromptSendsOfficialOptions(t *testing.T) {
	t.Parallel()

	requests := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer provider-secret" {
			t.Fatalf("unexpected auth header %q", got)
		}
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		requests <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	text, err := executeOpenAICompatiblePrompt(context.Background(), "Salut", "Be concise.", engine.ConnectedNode{
		Node: dataplane.Node{
			Name: "DeepSeek Chat Model",
			Type: "@n8n/n8n-nodes-langchain.lmChatDeepSeek",
			Parameters: map[string]any{
				"model": "deepseek-chat",
				"options": map[string]any{
					"maxTokens":        123,
					"temperature":      0.2,
					"topP":             0.8,
					"frequencyPenalty": 0.1,
					"presencePenalty":  0.3,
					"responseFormat":   "json_object",
				},
			},
		},
		Credentials: map[string]map[string]any{
			"deepSeekApi": {"type": "deepSeekApi", "url": server.URL, "apiKey": "provider-secret"},
		},
	}, "deepSeekApi", "https://api.deepseek.com", "deepseek-chat")
	if err != nil {
		t.Fatalf("execute OpenAI-compatible prompt: %v", err)
	}
	if text != "ok" {
		t.Fatalf("unexpected text %q", text)
	}

	body := <-requests
	if body["model"] != "deepseek-chat" || body["max_tokens"] != float64(123) || body["temperature"] != 0.2 || body["top_p"] != 0.8 {
		t.Fatalf("unexpected request body: %#v", body)
	}
	responseFormat, ok := body["response_format"].(map[string]any)
	if !ok || responseFormat["type"] != "json_object" {
		t.Fatalf("unexpected response_format: %#v", body["response_format"])
	}
}

func TestAIAgentRequiresFallbackModelWhenEnabled(t *testing.T) {
	t.Parallel()

	_, err := (AIAgent{}).Execute(context.Background(), engine.ExecuteInput{
		Node: dataplane.Node{
			Name: "AI Agent",
			Type: "@n8n/n8n-nodes-langchain.agent",
			Parameters: map[string]any{
				"promptType":    "define",
				"text":          "Salut",
				"needsFallback": true,
			},
		},
		InputData: dataplane.MainOutput([]dataplane.Item{{JSON: map[string]any{"prompt": "Salut"}}}),
		ConnectedNodes: map[string][][]engine.ConnectedNode{
			"ai_languageModel": {{
				{Node: dataplane.Node{Name: "Google Gemini Chat Model", Type: "@n8n/n8n-nodes-langchain.lmChatGoogleGemini"}},
			}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "Fallback Model") {
		t.Fatalf("expected missing fallback model error, got %v", err)
	}
}

func TestAIAgentRequiresOutputParserWhenSpecificFormatEnabled(t *testing.T) {
	t.Parallel()

	_, err := (AIAgent{}).Execute(context.Background(), engine.ExecuteInput{
		Node: dataplane.Node{
			Name: "AI Agent",
			Type: "@n8n/n8n-nodes-langchain.agent",
			Parameters: map[string]any{
				"promptType":      "define",
				"text":            "Salut",
				"hasOutputParser": true,
			},
		},
		InputData: dataplane.MainOutput([]dataplane.Item{{JSON: map[string]any{"prompt": "Salut"}}}),
		ConnectedNodes: map[string][][]engine.ConnectedNode{
			"ai_languageModel": {{
				{Node: dataplane.Node{Name: "Google Gemini Chat Model", Type: "@n8n/n8n-nodes-langchain.lmChatGoogleGemini"}},
			}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "output parser") {
		t.Fatalf("expected missing output parser error, got %v", err)
	}
}

func TestStickyNoteCanExecuteAsStartNode(t *testing.T) {
	t.Parallel()

	registry := engine.NewRegistry()
	registry.Register("n8n-nodes-base.stickyNote", StickyNote{})

	evaluator := engine.NewEvaluator(registry)
	workflow := dataplane.Workflow{
		ID:   "wf-sticky",
		Name: "Sticky workflow",
		Nodes: []dataplane.Node{
			{Name: "Sticky Note", Type: "n8n-nodes-base.stickyNote"},
		},
	}

	if _, err := evaluator.Execute(context.Background(), workflow, "exec-sticky"); err != nil {
		t.Fatalf("sticky note should not fail execution: %v", err)
	}
}
