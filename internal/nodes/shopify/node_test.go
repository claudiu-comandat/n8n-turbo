package shopify

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

type shopifyRequest struct {
	method string
	path   string
	body   map[string]any
}

func TestShopifyProductUpdateUsesOfficialIDAndUpdateFields(t *testing.T) {
	requests := executeShopifyForTest(t, map[string]any{
		"resource":  "product",
		"operation": "update",
		"id":        map[string]any{"value": 123},
		"updateFields": map[string]any{
			"title":  "New Product",
			"vendor": "Vendor",
		},
	})
	got := requests[len(requests)-1]
	if got.method != http.MethodPut || got.path != "/products/123.json" {
		t.Fatalf("request = %#v", got)
	}
	product := got.body["product"].(map[string]any)
	if product["title"] != "New Product" || product["vendor"] != "Vendor" || product["id"] != float64(123) {
		t.Fatalf("product = %#v", product)
	}
}

func TestShopifyCustomerCreateMergesAdditionalFields(t *testing.T) {
	requests := executeShopifyForTest(t, map[string]any{
		"resource":  "customer",
		"operation": "create",
		"email":     "ana@example.test",
		"additionalFields": map[string]any{
			"tags": "vip",
			"note": "hello",
		},
	})
	got := requests[len(requests)-1]
	if got.method != http.MethodPost || got.path != "/customers.json" {
		t.Fatalf("request = %#v", got)
	}
	customer := got.body["customer"].(map[string]any)
	if customer["email"] != "ana@example.test" || customer["tags"] != "vip" || customer["note"] != "hello" {
		t.Fatalf("customer = %#v", customer)
	}
}

func TestShopifyInventorySetUsesResourceLocatorIDs(t *testing.T) {
	requests := executeShopifyForTest(t, map[string]any{
		"resource":        "inventory",
		"operation":       "set",
		"idLocation":      map[string]any{"value": 10},
		"idInventoryItem": map[string]any{"value": 20},
		"available":       7,
	})
	got := requests[len(requests)-1]
	if got.method != http.MethodPost || got.path != "/inventory_levels/set.json" {
		t.Fatalf("request = %#v", got)
	}
	if got.body["location_id"] != float64(10) || got.body["inventory_item_id"] != float64(20) || got.body["available"] != float64(7) {
		t.Fatalf("body = %#v", got.body)
	}
}

func TestShopifyRuntimeSupportsOriginalOperations(t *testing.T) {
	t.Parallel()

	got := originalShopifyOperations(t)
	want := map[string][]string{
		"order":   {"create", "delete", "get", "getAll", "update"},
		"product": {"create", "delete", "get", "getAll", "update"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Shopify original operations changed or runtime coverage is stale\n got: %#v\nwant: %#v", got, want)
	}
}

func executeShopifyForTest(t *testing.T, params map[string]any) []shopifyRequest {
	t.Helper()

	requests := []shopifyRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := shopifyRequest{method: r.Method, path: r.URL.Path, body: map[string]any{}}
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
		case r.URL.Path == "/products/123.json":
			_, _ = w.Write([]byte(`{"product":{"id":123}}`))
		case r.URL.Path == "/customers.json":
			_, _ = w.Write([]byte(`{"customer":{"id":1}}`))
		case r.URL.Path == "/inventory_levels/set.json":
			_, _ = w.Write([]byte(`{"inventory_level":{"id":1}}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	t.Cleanup(server.Close)

	node := NewWithBaseURL(server.URL)
	_, err := node.Execute(context.Background(), engine.ExecuteInput{
		Node:        dataplane.Node{Parameters: params},
		Credentials: map[string]map[string]any{"shopifyAccessTokenApi": {"shopSubdomain": "test", "accessToken": "token"}},
		InputData:   dataplane.Output{{{JSON: map[string]any{}}}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return requests
}

func originalShopifyOperations(t *testing.T) map[string][]string {
	t.Helper()

	node, ok := metadata.NodeTypeByName("n8n-nodes-base.shopify", []string{"n8n-nodes-base.shopify"})
	if !ok || node.Raw == nil {
		t.Fatal("shopify original metadata is unavailable")
	}
	properties, ok := node.Raw["properties"].([]any)
	if !ok {
		t.Fatal("shopify metadata has no properties")
	}
	result := map[string][]string{}
	for _, raw := range properties {
		prop, ok := raw.(map[string]any)
		if !ok || prop["name"] != "operation" {
			continue
		}
		display, _ := prop["displayOptions"].(map[string]any)
		show, _ := display["show"].(map[string]any)
		resources := shopifyStringList(show["resource"])
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

func shopifyStringList(value any) []string {
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
