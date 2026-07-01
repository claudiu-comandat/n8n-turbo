package discord

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
	"github.com/n8n-io/n8n-turbo/internal/metadata"
)

type discordRequest struct {
	method string
	path   string
	query  string
	body   map[string]any
}

func TestDiscordSendMessageUsesOfficialOptionsAndEmbeds(t *testing.T) {
	requests := executeDiscordForTest(t, map[string]any{
		"resource":  "message",
		"operation": "send",
		"guildId":   map[string]any{"value": "guild-1"},
		"channelId": map[string]any{"value": "channel-1"},
		"content":   "hello",
		"options": map[string]any{
			"flags":             []any{"SUPPRESS_EMBEDS", "SUPPRESS_NOTIFICATIONS"},
			"message_reference": "message-1",
			"tts":               true,
		},
		"embeds": map[string]any{"values": []any{
			map[string]any{
				"inputMethod": "fields",
				"title":       "Title",
				"author":      "Ana",
				"color":       "#ff0000",
				"image":       "https://example.test/image.png",
			},
		}},
	})
	got := requests[len(requests)-1]
	if got.method != http.MethodPost || got.path != "/channels/channel-1/messages" {
		t.Fatalf("request = %s %s", got.method, got.path)
	}
	if got.body["flags"] != float64(4100) || got.body["tts"] != true {
		t.Fatalf("body = %#v", got.body)
	}
	reference := got.body["message_reference"].(map[string]any)
	if reference["message_id"] != "message-1" || reference["guild_id"] != "guild-1" {
		t.Fatalf("message_reference = %#v", reference)
	}
	embed := got.body["embeds"].([]any)[0].(map[string]any)
	if embed["title"] != "Title" || embed["color"] != float64(16711680) {
		t.Fatalf("embed = %#v", embed)
	}
	if embed["author"].(map[string]any)["name"] != "Ana" {
		t.Fatalf("embed author = %#v", embed["author"])
	}
}

func TestDiscordSendMessageToUserCreatesDM(t *testing.T) {
	requests := executeDiscordForTest(t, map[string]any{
		"resource":  "message",
		"operation": "send",
		"sendTo":    "user",
		"userId":    map[string]any{"value": "user-1"},
		"content":   "hello",
	})

	if len(requests) != 2 {
		t.Fatalf("request count = %d", len(requests))
	}
	if requests[0].method != http.MethodPost || requests[0].path != "/users/@me/channels" || requests[0].body["recipient_id"] != "user-1" {
		t.Fatalf("dm request = %#v", requests[0])
	}
	if requests[1].method != http.MethodPost || requests[1].path != "/channels/dm-1/messages" {
		t.Fatalf("message request = %#v", requests[1])
	}
}

func TestDiscordSendAndWaitUsesOfficialMessageParam(t *testing.T) {
	requests := executeDiscordForTest(t, map[string]any{
		"resource":  "message",
		"operation": "sendAndWait",
		"channelId": map[string]any{"value": "channel-1"},
		"message":   "approve?",
	})
	got := requests[len(requests)-1]
	if got.method != http.MethodPost || got.path != "/channels/channel-1/messages" {
		t.Fatalf("request = %#v", got)
	}
	if got.body["content"] != "approve?" {
		t.Fatalf("body = %#v", got.body)
	}
}

func TestDiscordSendLegacyUsesOriginalDefaultResource(t *testing.T) {
	requests := executeDiscordForTest(t, map[string]any{
		"operation": "sendLegacy",
		"channelId": map[string]any{"value": "channel-1"},
		"content":   "legacy hello",
	})
	got := requests[len(requests)-1]
	if got.method != http.MethodPost || got.path != "/channels/channel-1/messages" {
		t.Fatalf("request = %#v", got)
	}
	if got.body["content"] != "legacy hello" {
		t.Fatalf("body = %#v", got.body)
	}
}

func TestDiscordMessageGetAllUsesLimit(t *testing.T) {
	requests := executeDiscordForTest(t, map[string]any{
		"resource":  "message",
		"operation": "getAll",
		"channelId": map[string]any{"value": "channel-1"},
		"returnAll": false,
		"limit":     25,
	})
	got := requests[len(requests)-1]
	if got.method != http.MethodGet || got.path != "/channels/channel-1/messages" || got.query != "limit=25" {
		t.Fatalf("request = %#v", got)
	}
}

func TestDiscordMemberRoleAddUsesOfficialRoleParam(t *testing.T) {
	requests := executeDiscordForTest(t, map[string]any{
		"resource":  "member",
		"operation": "roleAdd",
		"guildId":   map[string]any{"value": "guild-1"},
		"userId":    map[string]any{"value": "user-1"},
		"role":      []any{"role-1", "role-2"},
	})

	if len(requests) != 2 {
		t.Fatalf("request count = %d", len(requests))
	}
	if requests[0].method != http.MethodPut || requests[0].path != "/guilds/guild-1/members/user-1/roles/role-1" {
		t.Fatalf("first request = %#v", requests[0])
	}
	if requests[1].method != http.MethodPut || requests[1].path != "/guilds/guild-1/members/user-1/roles/role-2" {
		t.Fatalf("second request = %#v", requests[1])
	}
}

func TestDiscordChannelCreateUsesOfficialOptions(t *testing.T) {
	requests := executeDiscordForTest(t, map[string]any{
		"resource":  "channel",
		"operation": "create",
		"guildId":   map[string]any{"value": "guild-1"},
		"name":      "new-channel",
		"type":      "0",
		"options": map[string]any{
			"categoryId":          map[string]any{"value": "category-1"},
			"rate_limit_per_user": 7,
			"topic":               "topic",
		},
	})
	got := requests[len(requests)-1]
	if got.method != http.MethodPost || got.path != "/guilds/guild-1/channels" {
		t.Fatalf("request = %#v", got)
	}
	if got.body["parent_id"] != "category-1" || got.body["rate_limit_per_user"] != float64(7) || got.body["topic"] != "topic" {
		t.Fatalf("body = %#v", got.body)
	}
}

func TestDiscordRuntimeSupportsOriginalOperations(t *testing.T) {
	got := originalDiscordOperations(t)
	want := map[string][]string{
		"":        {"sendLegacy"},
		"channel": {"create", "deleteChannel", "get", "getAll", "update"},
		"member":  {"getAll", "roleAdd", "roleRemove"},
		"message": {"deleteMessage", "get", "getAll", "react", "send", "sendAndWait"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Discord original operations changed or runtime coverage is stale\n got: %#v\nwant: %#v", got, want)
	}
}

func executeDiscordForTest(t *testing.T, params map[string]any) []discordRequest {
	t.Helper()

	requests := []discordRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := discordRequest{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, body: map[string]any{}}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if len(data) > 0 {
			if err := json.Unmarshal(data, &got.body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
		}
		requests = append(requests, got)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/users/@me/channels":
			_, _ = w.Write([]byte(`{"id":"dm-1"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/channels/channel-1/messages":
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPut || r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			_, _ = w.Write([]byte(`{"id":"ok"}`))
		}
	}))
	t.Cleanup(server.Close)

	node := NewWithBaseURL(server.URL)
	_, err := node.Execute(context.Background(), engine.ExecuteInput{
		Node:        dataplane.Node{Parameters: params},
		Credentials: map[string]map[string]any{"discordBotApi": {"botToken": "Bot test-token"}},
		InputData:   dataplane.Output{{{JSON: map[string]any{}}}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return requests
}

func originalDiscordOperations(t *testing.T) map[string][]string {
	t.Helper()

	node, ok := metadata.NodeTypeByName("n8n-nodes-base.discord", []string{"n8n-nodes-base.discord"})
	if !ok || node.Raw == nil {
		t.Fatal("discord original metadata is unavailable")
	}
	properties, ok := node.Raw["properties"].([]any)
	if !ok {
		t.Fatal("discord metadata has no properties")
	}
	result := map[string][]string{}
	for _, raw := range properties {
		prop, ok := raw.(map[string]any)
		if !ok || prop["name"] != "operation" {
			continue
		}
		display, _ := prop["displayOptions"].(map[string]any)
		show, _ := display["show"].(map[string]any)
		resources := discordStringList(show["resource"])
		if len(resources) == 0 {
			resources = []string{""}
		}
		options, _ := prop["options"].([]any)
		for _, resource := range resources {
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

func discordStringList(value any) []string {
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
