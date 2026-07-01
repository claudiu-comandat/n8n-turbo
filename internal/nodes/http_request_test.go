package nodes

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func TestHTTPRequestAcceptsOfficialLegacyUIParameters(t *testing.T) {
	t.Parallel()

	var capturedQuery string
	var capturedHeader string
	var capturedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.Query().Get("q")
		capturedHeader = r.Header.Get("X-Test")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	out, err := (HTTPRequest{}).Execute(context.Background(), testInput(map[string]any{
		"requestMethod":  "POST",
		"url":            server.URL,
		"responseFormat": "string",
		"queryParametersUi": map[string]any{"parameter": []any{
			map[string]any{"name": "q", "value": "legacy-query"},
		}},
		"headerParametersUi": map[string]any{"parameter": []any{
			map[string]any{"name": "X-Test", "value": "legacy-header"},
		}},
		"bodyParametersUi": map[string]any{"parameter": []any{
			map[string]any{"name": "payload", "value": "legacy-body"},
		}},
	}, []dataplane.Item{{JSON: map[string]any{}}}))
	if err != nil {
		t.Fatalf("http execute: %v", err)
	}
	if capturedQuery != "legacy-query" {
		t.Fatalf("queryParametersUi not sent, got %q", capturedQuery)
	}
	if capturedHeader != "legacy-header" {
		t.Fatalf("headerParametersUi not sent, got %q", capturedHeader)
	}
	if !reflect.DeepEqual(capturedBody, map[string]any{"payload": "legacy-body"}) {
		t.Fatalf("bodyParametersUi not sent as JSON, got %#v", capturedBody)
	}
	if got := out[0][0].JSON["data"]; got != `{"ok":true}` {
		t.Fatalf("responseFormat string should keep text body, got %#v", out[0][0].JSON)
	}
}

func TestHTTPRequestHonorsOfficialSendBodyFalse(t *testing.T) {
	t.Parallel()

	var bodyLength int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodyLength = len(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	_, err := (HTTPRequest{}).Execute(context.Background(), testInput(map[string]any{
		"method":   "POST",
		"url":      server.URL,
		"sendBody": false,
		"bodyParameters": map[string]any{"parameters": []any{
			map[string]any{"name": "mustNotSend", "value": "x"},
		}},
	}, []dataplane.Item{{JSON: map[string]any{}}}))
	if err != nil {
		t.Fatalf("http execute: %v", err)
	}
	if bodyLength != 0 {
		t.Fatalf("sendBody=false should not send a body, got %d bytes", bodyLength)
	}
}

func TestHTTPRequestSendsOfficialJsonExpressionBody(t *testing.T) {
	t.Parallel()

	var capturedContentType string
	var capturedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	_, err := (HTTPRequest{}).Execute(context.Background(), testInput(map[string]any{
		"method":      "POST",
		"url":         server.URL,
		"sendBody":    true,
		"specifyBody": "json",
		"jsonBody":    "={{ { products: $json.products } }}",
	}, []dataplane.Item{{JSON: map[string]any{"products": []any{map[string]any{"sku": "A1"}}}}}))
	if err != nil {
		t.Fatalf("http execute: %v", err)
	}
	if capturedContentType != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", capturedContentType)
	}
	want := map[string]any{"products": []any{map[string]any{"sku": "A1"}}}
	if !reflect.DeepEqual(capturedBody, want) {
		t.Fatalf("jsonBody expression was not sent as JSON, got %#v", capturedBody)
	}
}

func TestHTTPRequestSendsOfficialRawBodyAndContentType(t *testing.T) {
	t.Parallel()

	var capturedContentType string
	var capturedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	_, err := (HTTPRequest{}).Execute(context.Background(), testInput(map[string]any{
		"method":         "POST",
		"url":            server.URL,
		"sendBody":       true,
		"contentType":    "raw",
		"body":           "=[{\"page\": $json.page}]",
		"rawContentType": "text/plain;charset=UTF-8",
	}, []dataplane.Item{{JSON: map[string]any{"page": 3}}}))
	if err != nil {
		t.Fatalf("http execute: %v", err)
	}
	if capturedContentType != "text/plain;charset=UTF-8" {
		t.Fatalf("Content-Type = %q, want raw content type", capturedContentType)
	}
	if capturedBody != `[{"page":3}]` {
		t.Fatalf("raw body = %q", capturedBody)
	}
}

func TestHTTPRequestSupportsOfficialPaginationWithResponseBodyExpression(t *testing.T) {
	t.Parallel()

	pages := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		pages = append(pages, page)
		w.Header().Set("Content-Type", "application/json")
		if page == "3" {
			_, _ = w.Write([]byte(`{"meta":{"current_page":3},"links":{"next":null},"data":[3]}`))
			return
		}
		_, _ = w.Write([]byte(`{"meta":{"current_page":` + page + `},"links":{"next":"yes"},"data":[` + page + `]}`))
	}))
	defer server.Close()

	out, err := (HTTPRequest{}).Execute(context.Background(), testInput(map[string]any{
		"url":       server.URL,
		"sendQuery": true,
		"queryParameters": map[string]any{"parameters": []any{
			map[string]any{"name": "page", "value": "1"},
		}},
		"options": map[string]any{
			"pagination": map[string]any{
				"pagination": map[string]any{
					"parameters": map[string]any{"parameters": []any{
						map[string]any{"name": "page", "value": "=  {{ $response.body.meta.current_page + 1 }}"},
					}},
					"paginationCompleteWhen": "other",
					"completeExpression":     "={{ $response.body.links.next == null }}",
				},
			},
		},
	}, []dataplane.Item{{JSON: map[string]any{}}}))
	if err != nil {
		t.Fatalf("http execute: %v", err)
	}
	if !reflect.DeepEqual(pages, []string{"1", "2", "3"}) {
		t.Fatalf("unexpected requested pages: %#v", pages)
	}
	if len(out[0]) != 3 {
		t.Fatalf("expected one item per page, got %#v", out)
	}
}

func TestHTTPRequestSupportsOfficialPaginationWithPageCountAndMaxRequests(t *testing.T) {
	t.Parallel()

	ranges := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ranges = append(ranges, r.URL.Query().Get("s")+"-"+r.URL.Query().Get("t"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	out, err := (HTTPRequest{}).Execute(context.Background(), testInput(map[string]any{
		"url":       server.URL,
		"sendQuery": true,
		"queryParameters": map[string]any{"parameters": []any{
			map[string]any{"name": "s", "value": "=1"},
			map[string]any{"name": "t", "value": "=500"},
		}},
		"options": map[string]any{
			"pagination": map[string]any{
				"pagination": map[string]any{
					"parameters": map[string]any{"parameters": []any{
						map[string]any{"name": "s", "value": "={{ 1 + ($pageCount * 500) }}"},
						map[string]any{"name": "t", "value": "={{ 500 + ($pageCount * 500) }}"},
					}},
					"limitPagesFetched": true,
					"maxRequests":       3,
				},
			},
		},
	}, []dataplane.Item{{JSON: map[string]any{}}}))
	if err != nil {
		t.Fatalf("http execute: %v", err)
	}
	if !reflect.DeepEqual(ranges, []string{"1-500", "501-1000", "1001-1500"}) {
		t.Fatalf("unexpected ranges: %#v", ranges)
	}
	if len(out[0]) != 3 {
		t.Fatalf("expected one item per page, got %#v", out)
	}
}
