package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
	"github.com/n8n-io/n8n-turbo/internal/metadata"
)

func TestTelegramSendAnimationUsesOfficialFileParam(t *testing.T) {
	t.Parallel()

	path, body := executeTelegramForTest(t, map[string]any{
		"resource":  "message",
		"operation": "sendAnimation",
		"chatId":    "42",
		"file":      "animation-file-id",
		"additionalFields": map[string]any{
			"caption":    "hello",
			"parse_mode": "Markdown",
		},
	})
	if !strings.HasSuffix(path, "/sendAnimation") {
		t.Fatalf("path = %q", path)
	}
	if body["chat_id"] != "42" || body["animation"] != "animation-file-id" || body["caption"] != "hello" || body["parse_mode"] != "Markdown" {
		t.Fatalf("body = %#v", body)
	}
}

func TestTelegramSendAndWaitUsesOfficialMessageParam(t *testing.T) {
	t.Parallel()

	path, body := executeTelegramForTest(t, map[string]any{
		"resource":  "message",
		"operation": "sendAndWait",
		"chatId":    "42",
		"message":   "approve?",
	})
	if !strings.HasSuffix(path, "/sendMessage") {
		t.Fatalf("path = %q", path)
	}
	if body["chat_id"] != "42" || body["text"] != "approve?" {
		t.Fatalf("body = %#v", body)
	}
}

func TestTelegramChatMemberOperation(t *testing.T) {
	t.Parallel()

	path, body := executeTelegramForTest(t, map[string]any{
		"resource":  "chat",
		"operation": "member",
		"chatId":    "42",
		"userId":    "7",
	})
	if !strings.HasSuffix(path, "/getChatMember") {
		t.Fatalf("path = %q", path)
	}
	if body["chat_id"] != "42" || body["user_id"] != "7" {
		t.Fatalf("body = %#v", body)
	}
}

func TestTelegramAnswerInlineQuery(t *testing.T) {
	t.Parallel()

	path, body := executeTelegramForTest(t, map[string]any{
		"resource":         "callback",
		"operation":        "answerInlineQuery",
		"queryId":          "inline-1",
		"results":          `[{"type":"article","id":"1","title":"Hi","input_message_content":{"message_text":"Hi"}}]`,
		"additionalFields": map[string]any{"cache_time": 10},
	})
	if !strings.HasSuffix(path, "/answerInlineQuery") {
		t.Fatalf("path = %q", path)
	}
	results, ok := body["results"].([]any)
	if !ok || len(results) != 1 || body["inline_query_id"] != "inline-1" || body["cache_time"] != float64(10) {
		t.Fatalf("body = %#v", body)
	}
}

func TestTelegramSendMediaGroup(t *testing.T) {
	t.Parallel()

	path, body := executeTelegramForTest(t, map[string]any{
		"resource":  "message",
		"operation": "sendMediaGroup",
		"chatId":    "42",
		"media": map[string]any{"media": []any{
			map[string]any{"type": "photo", "media": "file-id"},
		}},
	})
	if !strings.HasSuffix(path, "/sendMediaGroup") {
		t.Fatalf("path = %q", path)
	}
	media, ok := body["media"].([]any)
	if !ok || len(media) != 1 || body["chat_id"] != "42" {
		t.Fatalf("body = %#v", body)
	}
}

func TestTelegramRuntimeSupportsOriginalOperations(t *testing.T) {
	t.Parallel()

	got := originalTelegramOperations(t)
	want := map[string][]string{
		"callback": {"answerInlineQuery", "answerQuery"},
		"chat":     {"administrators", "get", "leave", "member", "setDescription", "setTitle"},
		"file":     {"get"},
		"message": {
			"deleteMessage",
			"editMessageText",
			"pinChatMessage",
			"sendAndWait",
			"sendAnimation",
			"sendAudio",
			"sendChatAction",
			"sendDocument",
			"sendLocation",
			"sendMediaGroup",
			"sendMessage",
			"sendPhoto",
			"sendSticker",
			"sendVideo",
			"unpinChatMessage",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Telegram original operations changed or runtime coverage is stale\n got: %#v\nwant: %#v", got, want)
	}
}

func executeTelegramForTest(t *testing.T, params map[string]any) (string, map[string]any) {
	t.Helper()

	var gotPath string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"ok":true}}`))
	}))
	t.Cleanup(server.Close)

	token := "123456:" + strings.Repeat("a", 35)
	node := NewWithBaseURL(nil, server.URL)
	_, err := node.Execute(context.Background(), engine.ExecuteInput{
		Node:        dataplane.Node{Parameters: params},
		Credentials: map[string]map[string]any{"telegramApi": {"accessToken": token}},
		InputData:   dataplane.Output{{{JSON: map[string]any{}}}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return gotPath, gotBody
}

func originalTelegramOperations(t *testing.T) map[string][]string {
	t.Helper()

	node, ok := metadata.NodeTypeByName("n8n-nodes-base.telegram", []string{"n8n-nodes-base.telegram"})
	if !ok || node.Raw == nil {
		t.Fatal("telegram original metadata is unavailable")
	}
	properties, ok := node.Raw["properties"].([]any)
	if !ok {
		t.Fatal("telegram metadata has no properties")
	}
	result := map[string][]string{}
	for _, raw := range properties {
		prop, ok := raw.(map[string]any)
		if !ok || prop["name"] != "operation" {
			continue
		}
		display, _ := prop["displayOptions"].(map[string]any)
		show, _ := display["show"].(map[string]any)
		options, _ := prop["options"].([]any)
		for _, resource := range telegramStringList(show["resource"]) {
			for _, rawOption := range options {
				option, ok := rawOption.(map[string]any)
				if !ok {
					continue
				}
				if value, ok := option["value"].(string); ok {
					result[resource] = append(result[resource], value)
				}
			}
		}
	}
	for resource := range result {
		sort.Strings(result[resource])
	}
	return result
}

func telegramStringList(value any) []string {
	values, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, raw := range values {
		if text, ok := raw.(string); ok {
			result = append(result, text)
		}
	}
	return result
}
