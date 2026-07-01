package msteams

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

type teamsRequest struct {
	method  string
	path    string
	header  http.Header
	body    map[string]any
	rawBody string
}

func TestTeamsChatMessageCreateUsesChatRoute(t *testing.T) {
	requests := executeTeamsForTest(t, map[string]any{
		"resource":    "chatMessage",
		"operation":   "create",
		"chatId":      map[string]any{"value": "chat-1"},
		"contentType": "html",
		"message":     "<b>Hello</b>",
	})
	got := requests[len(requests)-1]
	if got.method != http.MethodPost || got.path != "/chats/chat-1/messages" {
		t.Fatalf("request = %#v", got)
	}
	body := got.body["body"].(map[string]any)
	if body["contentType"] != "html" || body["content"] != "<b>Hello</b>" {
		t.Fatalf("body = %#v", got.body)
	}
}

func TestTeamsChatMessageSendAndWaitUsesOfficialMessageParam(t *testing.T) {
	requests := executeTeamsForTest(t, map[string]any{
		"resource":  "chatMessage",
		"operation": "sendAndWait",
		"chatId":    map[string]any{"value": "chat-1"},
		"message":   "approve?",
	})
	got := requests[len(requests)-1]
	if got.method != http.MethodPost || got.path != "/chats/chat-1/messages" {
		t.Fatalf("request = %#v", got)
	}
	body := got.body["body"].(map[string]any)
	if body["content"] != "approve?" {
		t.Fatalf("body = %#v", got.body)
	}
}

func TestTeamsChannelUpdateAndDeleteUseOfficialFields(t *testing.T) {
	update := executeTeamsForTest(t, map[string]any{
		"resource":  "channel",
		"operation": "update",
		"teamId":    map[string]any{"value": "team-1"},
		"channelId": map[string]any{"value": "channel-1"},
		"name":      "New",
		"options":   map[string]any{"description": "Desc"},
	})
	gotUpdate := update[len(update)-1]
	if gotUpdate.method != http.MethodPatch || gotUpdate.path != "/teams/team-1/channels/channel-1" {
		t.Fatalf("update = %#v", gotUpdate)
	}
	if gotUpdate.body["displayName"] != "New" || gotUpdate.body["description"] != "Desc" {
		t.Fatalf("body = %#v", gotUpdate.body)
	}

	deleted := executeTeamsForTest(t, map[string]any{
		"resource":  "channel",
		"operation": "deleteChannel",
		"teamId":    map[string]any{"value": "team-1"},
		"channelId": map[string]any{"value": "channel-1"},
	})
	gotDelete := deleted[len(deleted)-1]
	if gotDelete.method != http.MethodDelete || gotDelete.path != "/teams/team-1/channels/channel-1" {
		t.Fatalf("delete = %#v", gotDelete)
	}
}

func TestTeamsTaskCreateUpdateDelete(t *testing.T) {
	created := executeTeamsForTest(t, map[string]any{
		"resource":  "task",
		"operation": "create",
		"planId":    map[string]any{"value": "plan-1"},
		"bucketId":  map[string]any{"value": "bucket-1"},
		"title":     "Task",
		"options": map[string]any{
			"assignedTo":      map[string]any{"value": "user-1"},
			"percentComplete": 25,
		},
	})
	gotCreate := created[len(created)-1]
	if gotCreate.method != http.MethodPost || gotCreate.path != "/planner/tasks" {
		t.Fatalf("create = %#v", gotCreate)
	}
	if gotCreate.body["planId"] != "plan-1" || gotCreate.body["bucketId"] != "bucket-1" {
		t.Fatalf("body = %#v", gotCreate.body)
	}
	if _, ok := gotCreate.body["assignments"].(map[string]any)["user-1"]; !ok {
		t.Fatalf("assignments = %#v", gotCreate.body["assignments"])
	}

	updated := executeTeamsForTest(t, map[string]any{
		"resource":  "task",
		"operation": "update",
		"taskId":    "task-1",
		"updateFields": map[string]any{
			"title":      "Updated",
			"assignedTo": map[string]any{"value": "user-2"},
		},
	})
	if len(updated) != 2 {
		t.Fatalf("update requests = %d", len(updated))
	}
	if updated[1].method != http.MethodPatch || updated[1].path != "/planner/tasks/task-1" || updated[1].header.Get("If-Match") != `W/"etag-1"` {
		t.Fatalf("patch = %#v", updated[1])
	}

	deleted := executeTeamsForTest(t, map[string]any{
		"resource":  "task",
		"operation": "deleteTask",
		"taskId":    "task-1",
	})
	if len(deleted) != 2 {
		t.Fatalf("delete requests = %d", len(deleted))
	}
	if deleted[1].method != http.MethodDelete || deleted[1].path != "/planner/tasks/task-1" || deleted[1].header.Get("If-Match") != `W/"etag-1"` {
		t.Fatalf("delete = %#v", deleted[1])
	}
}

func TestTeamsRuntimeSupportsOriginalOperations(t *testing.T) {
	got := originalTeamsOperations(t)
	want := map[string][]string{
		"channel":        {"create", "deleteChannel", "get", "getAll", "update"},
		"channelMessage": {"create", "getAll"},
		"chatMessage":    {"create", "get", "getAll", "sendAndWait"},
		"task":           {"create", "deleteTask", "get", "getAll", "update"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Microsoft Teams original operations changed or runtime coverage is stale\n got: %#v\nwant: %#v", got, want)
	}
}

func executeTeamsForTest(t *testing.T, params map[string]any) []teamsRequest {
	t.Helper()

	requests := []teamsRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := teamsRequest{method: r.Method, path: r.URL.Path, header: r.Header.Clone(), body: map[string]any{}}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		got.rawBody = string(data)
		if len(data) > 0 {
			if err := json.Unmarshal(data, &got.body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
		}
		requests = append(requests, got)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/planner/tasks/task-1":
			_, _ = w.Write([]byte(`{"id":"task-1","@odata.etag":"W/\"etag-1\""}`))
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			_, _ = w.Write([]byte(`{"id":"ok","value":[]}`))
		}
	}))
	t.Cleanup(server.Close)

	node := NewWithBaseURL(server.URL)
	_, err := node.Execute(context.Background(), engine.ExecuteInput{
		Node:        dataplane.Node{Parameters: params},
		Credentials: map[string]map[string]any{"microsoftTeamsOAuth2Api": {"accessToken": "token"}},
		InputData:   dataplane.Output{{{JSON: map[string]any{}}}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return requests
}

func originalTeamsOperations(t *testing.T) map[string][]string {
	t.Helper()

	node, ok := metadata.NodeTypeByName("n8n-nodes-base.microsoftTeams", []string{"n8n-nodes-base.microsoftTeams"})
	if !ok || node.Raw == nil {
		t.Fatal("microsoftTeams original metadata is unavailable")
	}
	properties, ok := node.Raw["properties"].([]any)
	if !ok {
		t.Fatal("microsoftTeams metadata has no properties")
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
		for _, resource := range teamsStringList(show["resource"]) {
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

func teamsStringList(value any) []string {
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
