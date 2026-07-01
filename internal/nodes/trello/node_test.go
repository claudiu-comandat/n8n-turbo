package trello

import (
	"context"
	"encoding/json"
	"io"
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

type trelloRequest struct {
	method string
	path   string
	query  string
	body   map[string]any
}

func TestTrelloBoardUpdateUsesOfficialIDAndUpdateFields(t *testing.T) {
	requests := executeTrelloForTest(t, map[string]any{
		"resource":  "board",
		"operation": "update",
		"id":        map[string]any{"value": "board-1"},
		"updateFields": map[string]any{
			"name":   "New Board",
			"closed": true,
		},
	})
	got := requests[len(requests)-1]
	if got.method != http.MethodPut || got.path != "/boards/board-1" {
		t.Fatalf("request = %#v", got)
	}
	if got.body["name"] != "New Board" || got.body["closed"] != true {
		t.Fatalf("body = %#v", got.body)
	}
}

func TestTrelloBoardDeleteUsesOfficialID(t *testing.T) {
	requests := executeTrelloForTest(t, map[string]any{
		"resource":  "board",
		"operation": "delete",
		"id":        map[string]any{"value": "board-1"},
	})
	got := requests[len(requests)-1]
	if got.method != http.MethodDelete || got.path != "/boards/board-1" {
		t.Fatalf("request = %#v", got)
	}
}

func TestTrelloCardCreateUsesOfficialIDList(t *testing.T) {
	requests := executeTrelloForTest(t, map[string]any{
		"resource":  "card",
		"operation": "create",
		"idList":    map[string]any{"value": "list-1"},
		"name":      "Card",
		"desc":      "Description",
	})
	got := requests[len(requests)-1]
	if got.method != http.MethodPost || got.path != "/cards" {
		t.Fatalf("request = %#v", got)
	}
	if got.body["idList"] != "list-1" || got.body["name"] != "Card" || got.body["desc"] != "Description" {
		t.Fatalf("body = %#v", got.body)
	}
}

func TestTrelloRuntimeSupportsOriginalOperations(t *testing.T) {
	t.Parallel()

	got := originalTrelloOperations(t)
	want := map[string][]string{
		"attachment":  {"create", "delete", "get", "getAll"},
		"board":       {"create", "delete", "get", "update"},
		"boardMember": {"add", "getAll", "invite", "remove"},
		"card":        {"create", "delete", "get", "update"},
		"cardComment": {"create", "delete", "update"},
		"checklist":   {"completedCheckItems", "create", "createCheckItem", "delete", "deleteCheckItem", "get", "getAll", "getCheckItem", "updateCheckItem"},
		"label":       {"addLabel", "create", "delete", "get", "getAll", "removeLabel", "update"},
		"list":        {"archive", "create", "get", "getAll", "getCards", "update"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Trello original operations changed or runtime coverage is stale\n got: %#v\nwant: %#v", got, want)
	}
}

func TestTrelloNewOriginalOperationsUseExpectedRoutes(t *testing.T) {
	tests := []struct {
		name   string
		params map[string]any
		method string
		path   string
	}{
		{
			name:   "board member add",
			params: map[string]any{"resource": "boardMember", "operation": "add", "id": "board-1", "idMember": "member-1", "type": "normal"},
			method: http.MethodPut,
			path:   "/boards/board-1/members/member-1",
		},
		{
			name:   "checklist get all",
			params: map[string]any{"resource": "checklist", "operation": "getAll", "cardId": map[string]any{"value": "card-1"}},
			method: http.MethodGet,
			path:   "/cards/card-1/checklists",
		},
		{
			name:   "check item delete",
			params: map[string]any{"resource": "checklist", "operation": "deleteCheckItem", "checklistId": "check-1", "checkItemId": "item-1"},
			method: http.MethodDelete,
			path:   "/checklists/check-1/checkItems/item-1",
		},
		{
			name:   "label add",
			params: map[string]any{"resource": "label", "operation": "addLabel", "cardId": map[string]any{"value": "card-1"}, "id": "label-1"},
			method: http.MethodPost,
			path:   "/cards/card-1/idLabels",
		},
		{
			name:   "list get cards",
			params: map[string]any{"resource": "list", "operation": "getCards", "id": "list-1"},
			method: http.MethodGet,
			path:   "/lists/list-1/cards",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			requests := executeTrelloForTest(t, tt.params)
			got := requests[len(requests)-1]
			if got.method != tt.method || got.path != tt.path {
				t.Fatalf("request = %#v, want %s %s", got, tt.method, tt.path)
			}
		})
	}
}

func executeTrelloForTest(t *testing.T, params map[string]any) []trelloRequest {
	t.Helper()

	requests := []trelloRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := trelloRequest{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, body: map[string]any{}}
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
		if r.Method == http.MethodGet && (strings.HasSuffix(r.URL.Path, "/checklists") || strings.HasSuffix(r.URL.Path, "/cards")) {
			_, _ = w.Write([]byte(`[{"id":"ok"}]`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"ok"}`))
	}))
	t.Cleanup(server.Close)

	node := NewWithBaseURL(nil, server.URL)
	_, err := node.Execute(context.Background(), engine.ExecuteInput{
		Node:        dataplane.Node{Parameters: params},
		Credentials: map[string]map[string]any{"trelloApi": {"apiKey": "key", "apiToken": "token"}},
		InputData:   dataplane.Output{{{JSON: map[string]any{}}}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return requests
}

func originalTrelloOperations(t *testing.T) map[string][]string {
	t.Helper()

	node, ok := metadata.NodeTypeByName("n8n-nodes-base.trello", []string{"n8n-nodes-base.trello"})
	if !ok || node.Raw == nil {
		t.Fatal("trello original metadata is unavailable")
	}
	result := map[string][]string{}
	properties, ok := node.Raw["properties"].([]any)
	if !ok {
		t.Fatal("trello metadata has no properties")
	}
	for _, raw := range properties {
		prop, ok := raw.(map[string]any)
		if !ok || prop["name"] != "operation" {
			continue
		}
		display, _ := prop["displayOptions"].(map[string]any)
		show, _ := display["show"].(map[string]any)
		resources := stringList(show["resource"])
		options, _ := prop["options"].([]any)
		for _, resource := range resources {
			for _, rawOption := range options {
				option, ok := rawOption.(map[string]any)
				if !ok {
					continue
				}
				value, ok := option["value"].(string)
				if ok {
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

func stringList(value any) []string {
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
