package sendgrid

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

type sendGridRequest struct {
	method string
	path   string
	query  string
	body   map[string]any
}

func TestSendGridMailSendUsesOfficialFields(t *testing.T) {
	got := executeSendGridForTest(t, map[string]any{
		"resource":        "mail",
		"operation":       "send",
		"fromEmail":       "sender@example.test",
		"fromName":        "Sender",
		"toEmail":         "ana@example.test,bob@example.test",
		"subject":         "Salut",
		"contentType":     "text/html",
		"contentValue":    "<b>Hi</b>",
		"dynamicTemplate": false,
		"additionalFields": map[string]any{
			"ccEmail":       "cc@example.test",
			"bccEmail":      "bcc@example.test",
			"categories":    "a,b",
			"enableSandbox": true,
			"ipPoolName":    "pool",
			"replyToEmail":  "reply@example.test",
			"headers": map[string]any{"details": []any{
				map[string]any{"key": "X-Test", "value": "1"},
			}},
		},
	}, nil)

	if got.method != http.MethodPost || got.path != "/mail/send" {
		t.Fatalf("request = %s %s", got.method, got.path)
	}
	personalization := got.body["personalizations"].([]any)[0].(map[string]any)
	content := got.body["content"].([]any)[0].(map[string]any)
	if content["type"] != "text/html" || content["value"] != "<b>Hi</b>" {
		t.Fatalf("content = %#v", content)
	}
	if personalization["subject"] != "Salut" || got.body["ip_pool_name"] != "pool" {
		t.Fatalf("body = %#v", got.body)
	}
	if personalization["cc"].([]any)[0].(map[string]any)["email"] != "cc@example.test" {
		t.Fatalf("cc = %#v", personalization["cc"])
	}
	if personalization["bcc"].([]any)[0].(map[string]any)["email"] != "bcc@example.test" {
		t.Fatalf("bcc = %#v", personalization["bcc"])
	}
	if got.body["reply_to_list"].([]any)[0].(map[string]any)["email"] != "reply@example.test" {
		t.Fatalf("reply_to_list = %#v", got.body["reply_to_list"])
	}
	if got.body["headers"].(map[string]any)["X-Test"] != "1" {
		t.Fatalf("headers = %#v", got.body["headers"])
	}
	sandbox := got.body["mail_settings"].(map[string]any)["sandbox_mode"].(map[string]any)
	if sandbox["enable"] != true {
		t.Fatalf("mail_settings = %#v", got.body["mail_settings"])
	}
}

func TestSendGridContactUpsertUsesOfficialNestedFields(t *testing.T) {
	got := executeSendGridForTest(t, map[string]any{
		"resource":  "contact",
		"operation": "upsert",
		"email":     "ana@example.test",
		"additionalFields": map[string]any{
			"firstName":       "Ana",
			"alternateEmails": "ana2@example.test, ana3@example.test",
			"addressUi": map[string]any{"addressValues": map[string]any{
				"address1": "Main 1",
			}},
			"listIdsUi": map[string]any{"listIdValues": map[string]any{
				"listIds": []any{"list-1", "list-2"},
			}},
			"customFieldsUi": map[string]any{"customFieldValues": []any{
				map[string]any{"fieldId": "field-1", "fieldValue": "value-1"},
			}},
		},
	}, nil)

	if got.method != http.MethodPut || got.path != "/marketing/contacts" {
		t.Fatalf("request = %s %s", got.method, got.path)
	}
	contact := got.body["contacts"].([]any)[0].(map[string]any)
	if contact["first_name"] != "Ana" || contact["address_line_1"] != "Main 1" {
		t.Fatalf("contact = %#v", contact)
	}
	if contact["alternate_emails"].([]any)[1] != "ana3@example.test" {
		t.Fatalf("alternate_emails = %#v", contact["alternate_emails"])
	}
	if got.body["list_ids"].([]any)[0] != "list-1" {
		t.Fatalf("list_ids = %#v", got.body["list_ids"])
	}
	if contact["custom_fields"].(map[string]any)["field-1"] != "value-1" {
		t.Fatalf("custom_fields = %#v", contact["custom_fields"])
	}
}

func TestSendGridContactDeleteUsesOfficialIDs(t *testing.T) {
	got := executeSendGridForTest(t, map[string]any{
		"resource":  "contact",
		"operation": "delete",
		"ids":       "a, b",
		"deleteAll": true,
	}, nil)

	if got.method != http.MethodDelete || got.path != "/marketing/contacts" {
		t.Fatalf("request = %s %s", got.method, got.path)
	}
	if got.query != "delete_all_contacts=true&ids=a%2Cb" {
		t.Fatalf("query = %q", got.query)
	}
}

func TestSendGridListUpdateAndContactSample(t *testing.T) {
	gotUpdate := executeSendGridForTest(t, map[string]any{
		"resource":  "list",
		"operation": "update",
		"listId":    "list-1",
		"name":      "New Name",
	}, nil)
	if gotUpdate.method != http.MethodPatch || gotUpdate.path != "/marketing/lists/list-1" || gotUpdate.body["name"] != "New Name" {
		t.Fatalf("update = %#v", gotUpdate)
	}

	gotGet := executeSendGridForTest(t, map[string]any{
		"resource":      "list",
		"operation":     "get",
		"listId":        "list-1",
		"contactSample": true,
	}, nil)
	if gotGet.method != http.MethodGet || gotGet.path != "/marketing/lists/list-1" || gotGet.query != "contact_sample=true" {
		t.Fatalf("get = %#v", gotGet)
	}
}

func TestSendGridRuntimeSupportsOriginalOperations(t *testing.T) {
	t.Parallel()

	got := originalSendGridOperations(t)
	want := map[string][]string{
		"contact": {"delete", "get", "getAll", "upsert"},
		"list":    {"create", "delete", "get", "getAll", "update"},
		"mail":    {"send"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SendGrid original operations changed or runtime coverage is stale\n got: %#v\nwant: %#v", got, want)
	}
}

func executeSendGridForTest(t *testing.T, params map[string]any, input dataplane.Output) sendGridRequest {
	t.Helper()

	got := sendGridRequest{body: map[string]any{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method = r.Method
		got.path = r.URL.Path
		got.query = r.URL.RawQuery
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if len(data) > 0 {
			if err := json.Unmarshal(data, &got.body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.URL.Path == "/mail/send" {
			w.Header().Set("X-Message-Id", "msg-1")
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":[],"id":"ok"}`))
	}))
	t.Cleanup(server.Close)

	if input == nil {
		input = dataplane.Output{{{JSON: map[string]any{}}}}
	}
	node := NewWithBaseURL(nil, server.URL)
	_, err := node.Execute(context.Background(), engine.ExecuteInput{
		Node:        dataplane.Node{Parameters: params},
		Credentials: map[string]map[string]any{"sendGridApi": {"apiKey": "SG.test"}},
		InputData:   input,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return got
}

func originalSendGridOperations(t *testing.T) map[string][]string {
	t.Helper()

	node, ok := metadata.NodeTypeByName("n8n-nodes-base.sendGrid", []string{"n8n-nodes-base.sendGrid"})
	if !ok || node.Raw == nil {
		t.Fatal("sendGrid original metadata is unavailable")
	}
	properties, ok := node.Raw["properties"].([]any)
	if !ok {
		t.Fatal("sendGrid metadata has no properties")
	}
	result := map[string][]string{}
	for _, raw := range properties {
		prop, ok := raw.(map[string]any)
		if !ok || prop["name"] != "operation" {
			continue
		}
		display, _ := prop["displayOptions"].(map[string]any)
		show, _ := display["show"].(map[string]any)
		resources := sendGridStringList(show["resource"])
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

func sendGridStringList(value any) []string {
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
