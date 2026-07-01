package nodes

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

func TestN8nExecutionGetAllUsesOfficialFiltersAndPagination(t *testing.T) {
	t.Parallel()

	requests := make(chan map[string]string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-N8N-API-KEY"); got != "secret" {
			t.Fatalf("unexpected api key %q", got)
		}
		requests <- map[string]string{
			"path":       r.URL.Path,
			"limit":      r.URL.Query().Get("limit"),
			"workflowId": r.URL.Query().Get("workflowId"),
			"status":     r.URL.Query().Get("status"),
			"cursor":     r.URL.Query().Get("cursor"),
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("cursor") == "" {
			_, _ = w.Write([]byte(`{"data":[{"id":"1"}],"nextCursor":"next"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"2"}]}`))
	}))
	defer server.Close()

	output, err := (N8n{}).Execute(context.Background(), engine.ExecuteInput{
		Node: dataplane.Node{
			Name: "Get all previous executions",
			Type: "n8n-nodes-base.n8n",
			Parameters: map[string]any{
				"resource":  "execution",
				"operation": "getAll",
				"returnAll": true,
				"filters": map[string]any{
					"workflowId": map[string]any{"value": "wf-123", "mode": "list"},
					"status":     "success",
				},
			},
		},
		Credentials: map[string]map[string]any{
			"n8nApi": {"type": "n8nApi", "baseUrl": server.URL, "apiKey": "secret"},
		},
	})
	if err != nil {
		t.Fatalf("execute n8n node: %v", err)
	}
	if len(output) != 1 || len(output[0]) != 2 {
		encoded, _ := json.Marshal(output)
		t.Fatalf("expected two execution items, got %s", encoded)
	}

	first := <-requests
	if first["path"] != "/executions" || first["limit"] != "100" || first["workflowId"] != "wf-123" || first["status"] != "success" || first["cursor"] != "" {
		t.Fatalf("unexpected first request: %#v", first)
	}
	second := <-requests
	if second["cursor"] != "next" {
		t.Fatalf("expected pagination cursor, got %#v", second)
	}
}

func TestN8nExecutionGetAllHonorsLimitWhenReturnAllDisabled(t *testing.T) {
	t.Parallel()

	requests := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.Query().Get("limit")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()

	_, err := (N8n{}).Execute(context.Background(), engine.ExecuteInput{
		Node: dataplane.Node{
			Name: "Get executions",
			Type: "n8n-nodes-base.n8n",
			Parameters: map[string]any{
				"resource":  "execution",
				"operation": "getAll",
				"returnAll": false,
				"limit":     7,
			},
		},
		Credentials: map[string]map[string]any{
			"n8nApi": {"type": "n8nApi", "baseUrl": server.URL, "apiKey": "secret"},
		},
	})
	if err != nil {
		t.Fatalf("execute n8n node: %v", err)
	}
	if got := <-requests; got != "7" {
		t.Fatalf("limit = %q, want 7", got)
	}
}

func TestN8nExecutionDefaultsToGetAllWhenOperationIsMissing(t *testing.T) {
	t.Parallel()

	requests := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.Path
		_, _ = w.Write([]byte(`{"data":[{"id":"1"}]}`))
	}))
	defer server.Close()

	output, err := (N8n{}).Execute(context.Background(), engine.ExecuteInput{
		Node: dataplane.Node{
			Name: "Get all previous executions",
			Type: "n8n-nodes-base.n8n",
			Parameters: map[string]any{
				"resource":  "execution",
				"returnAll": true,
				"filters":   map[string]any{},
				"options":   map[string]any{},
			},
		},
		Credentials: map[string]map[string]any{
			"n8nApi": {"type": "n8nApi", "baseUrl": server.URL, "apiKey": "secret"},
		},
	})
	if err != nil {
		t.Fatalf("execute n8n node: %v", err)
	}
	if got := <-requests; got != "/executions" {
		t.Fatalf("path = %q, want /executions", got)
	}
	if len(output) != 1 || len(output[0]) != 1 {
		t.Fatalf("expected one execution item, got %#v", output)
	}
}

func TestN8nExecutionGetAndDeleteUseOfficialRoutes(t *testing.T) {
	t.Parallel()

	requests := make(chan map[string]string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- map[string]string{
			"method":      r.Method,
			"path":        r.URL.Path,
			"includeData": r.URL.Query().Get("includeData"),
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"exec1"}`))
	}))
	defer server.Close()

	credentials := map[string]map[string]any{
		"n8nApi": {"type": "n8nApi", "baseUrl": server.URL, "apiKey": "secret"},
	}
	cases := []struct {
		name        string
		params      map[string]any
		method      string
		path        string
		includeData string
	}{
		{
			name: "get",
			params: map[string]any{
				"resource":    "execution",
				"operation":   "get",
				"executionId": "exec1",
				"options":     map[string]any{"activeWorkflows": true},
			},
			method:      http.MethodGet,
			path:        "/executions/exec1",
			includeData: "true",
		},
		{
			name: "delete",
			params: map[string]any{
				"resource":    "execution",
				"operation":   "delete",
				"executionId": "exec1",
			},
			method: http.MethodDelete,
			path:   "/executions/exec1",
		},
	}

	for _, tc := range cases {
		_, err := (N8n{}).Execute(context.Background(), engine.ExecuteInput{
			Node:        dataplane.Node{Name: tc.name, Type: "n8n-nodes-base.n8n", Parameters: tc.params},
			Credentials: credentials,
		})
		if err != nil {
			t.Fatalf("%s execute: %v", tc.name, err)
		}
		got := <-requests
		if got["method"] != tc.method || got["path"] != tc.path || got["includeData"] != tc.includeData {
			t.Fatalf("%s request = %#v, want %s %s includeData=%q", tc.name, got, tc.method, tc.path, tc.includeData)
		}
	}
}

func TestN8nDefaultsToWorkflowGetAllWhenResourceAndOperationAreMissing(t *testing.T) {
	t.Parallel()

	requests := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.Path
		_, _ = w.Write([]byte(`{"data":[{"id":"wf1"}]}`))
	}))
	defer server.Close()

	output, err := (N8n{}).Execute(context.Background(), engine.ExecuteInput{
		Node: dataplane.Node{
			Name:       "n8n",
			Type:       "n8n-nodes-base.n8n",
			Parameters: map[string]any{},
		},
		Credentials: map[string]map[string]any{
			"n8nApi": {"type": "n8nApi", "baseUrl": server.URL, "apiKey": "secret"},
		},
	})
	if err != nil {
		t.Fatalf("execute n8n node: %v", err)
	}
	if got := <-requests; got != "/workflows" {
		t.Fatalf("path = %q, want /workflows", got)
	}
	if len(output) != 1 || len(output[0]) != 1 {
		t.Fatalf("expected one workflow item, got %#v", output)
	}
}

func TestN8nWorkflowOperationsUseOfficialRoutes(t *testing.T) {
	t.Parallel()

	requests := make(chan map[string]string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- map[string]string{
			"method": r.Method,
			"path":   r.URL.Path,
			"query":  r.URL.RawQuery,
			"body":   string(body),
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/workflows" && r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"data":[{"id":"wf1"}],"nextCursor":"next"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	credentials := map[string]map[string]any{
		"n8nApi": {"type": "n8nApi", "baseUrl": server.URL, "apiKey": "secret"},
	}
	cases := []struct {
		name   string
		params map[string]any
		method string
		path   string
	}{
		{
			name: "activate",
			params: map[string]any{
				"resource":         "workflow",
				"operation":        "activate",
				"workflowId":       map[string]any{"value": "wf1"},
				"additionalFields": map[string]any{"versionId": "v1"},
			},
			method: http.MethodPost,
			path:   "/workflows/wf1/activate",
		},
		{
			name: "deactivate",
			params: map[string]any{
				"resource":   "workflow",
				"operation":  "deactivate",
				"workflowId": map[string]any{"value": "wf1"},
			},
			method: http.MethodPost,
			path:   "/workflows/wf1/deactivate",
		},
		{
			name: "create",
			params: map[string]any{
				"resource":       "workflow",
				"operation":      "create",
				"workflowObject": `{"name":"Created","nodes":[],"connections":{},"settings":{}}`,
			},
			method: http.MethodPost,
			path:   "/workflows",
		},
		{
			name: "delete",
			params: map[string]any{
				"resource":   "workflow",
				"operation":  "delete",
				"workflowId": map[string]any{"value": "wf1"},
			},
			method: http.MethodDelete,
			path:   "/workflows/wf1",
		},
		{
			name: "get",
			params: map[string]any{
				"resource":   "workflow",
				"operation":  "get",
				"workflowId": map[string]any{"value": "wf1"},
			},
			method: http.MethodGet,
			path:   "/workflows/wf1",
		},
		{
			name: "getVersion",
			params: map[string]any{
				"resource":   "workflow",
				"operation":  "getVersion",
				"workflowId": map[string]any{"value": "wf1"},
				"versionId":  "v1",
			},
			method: http.MethodGet,
			path:   "/workflows/wf1/v1",
		},
		{
			name: "update",
			params: map[string]any{
				"resource":       "workflow",
				"operation":      "update",
				"workflowId":     map[string]any{"value": "wf1"},
				"workflowObject": `{"name":"Updated","nodes":[],"connections":{},"settings":{}}`,
			},
			method: http.MethodPut,
			path:   "/workflows/wf1",
		},
	}

	for _, tc := range cases {
		_, err := (N8n{}).Execute(context.Background(), engine.ExecuteInput{
			Node:        dataplane.Node{Name: tc.name, Type: "n8n-nodes-base.n8n", Parameters: tc.params},
			Credentials: credentials,
		})
		if err != nil {
			t.Fatalf("%s execute: %v", tc.name, err)
		}
		got := <-requests
		if got["method"] != tc.method || got["path"] != tc.path {
			t.Fatalf("%s request = %#v, want %s %s", tc.name, got, tc.method, tc.path)
		}
	}
}

func TestN8nWorkflowGetAllUsesOfficialFiltersAndPagination(t *testing.T) {
	t.Parallel()

	requests := make(chan map[string]string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- map[string]string{
			"path":              r.URL.Path,
			"limit":             r.URL.Query().Get("limit"),
			"active":            r.URL.Query().Get("active"),
			"name":              r.URL.Query().Get("name"),
			"excludePinnedData": r.URL.Query().Get("excludePinnedData"),
			"cursor":            r.URL.Query().Get("cursor"),
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("cursor") == "" {
			_, _ = w.Write([]byte(`{"data":[{"id":"wf1"}],"nextCursor":"next"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"wf2"}]}`))
	}))
	defer server.Close()

	output, err := (N8n{}).Execute(context.Background(), engine.ExecuteInput{
		Node: dataplane.Node{
			Name: "Get workflows",
			Type: "n8n-nodes-base.n8n",
			Parameters: map[string]any{
				"resource":  "workflow",
				"operation": "getAll",
				"returnAll": true,
				"filters": map[string]any{
					"activeWorkflows":   true,
					"name":              "sync",
					"excludePinnedData": true,
				},
			},
		},
		Credentials: map[string]map[string]any{
			"n8nApi": {"type": "n8nApi", "baseUrl": server.URL, "apiKey": "secret"},
		},
	})
	if err != nil {
		t.Fatalf("execute n8n node: %v", err)
	}
	if len(output) != 1 || len(output[0]) != 2 {
		t.Fatalf("expected two workflow items, got %#v", output)
	}
	first := <-requests
	if first["path"] != "/workflows" || first["limit"] != "100" || first["active"] != "true" || first["name"] != "sync" || first["excludePinnedData"] != "true" {
		t.Fatalf("unexpected first workflow request: %#v", first)
	}
	second := <-requests
	if second["cursor"] != "next" {
		t.Fatalf("expected workflow pagination cursor, got %#v", second)
	}
}

func TestN8nCredentialAndAuditOperationsUseOfficialRoutes(t *testing.T) {
	t.Parallel()

	requests := make(chan map[string]string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- map[string]string{"method": r.Method, "path": r.URL.Path, "body": string(body)}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	credentials := map[string]map[string]any{
		"n8nApi": {"type": "n8nApi", "baseUrl": server.URL, "apiKey": "secret"},
	}
	cases := []struct {
		params map[string]any
		method string
		path   string
	}{
		{
			params: map[string]any{"resource": "audit", "operation": "generate", "additionalOptions": map[string]any{"categories": []any{"nodes"}}},
			method: http.MethodPost,
			path:   "/audit",
		},
		{
			params: map[string]any{"resource": "credential", "operation": "create", "name": "API", "credentialTypeName": "n8nApi", "data": `{"apiKey":"x"}`},
			method: http.MethodPost,
			path:   "/credentials",
		},
		{
			params: map[string]any{"resource": "credential", "operation": "delete", "credentialId": "cred1"},
			method: http.MethodDelete,
			path:   "/credentials/cred1",
		},
		{
			params: map[string]any{"resource": "credential", "operation": "getSchema", "credentialTypeName": "n8nApi"},
			method: http.MethodGet,
			path:   "/credentials/schema/n8nApi",
		},
	}
	for _, tc := range cases {
		_, err := (N8n{}).Execute(context.Background(), engine.ExecuteInput{
			Node:        dataplane.Node{Name: "n8n", Type: "n8n-nodes-base.n8n", Parameters: tc.params},
			Credentials: credentials,
		})
		if err != nil {
			t.Fatalf("execute n8n node: %v", err)
		}
		got := <-requests
		if got["method"] != tc.method || got["path"] != tc.path {
			t.Fatalf("request = %#v, want %s %s", got, tc.method, tc.path)
		}
	}
}
