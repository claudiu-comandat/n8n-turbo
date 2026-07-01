package shopify

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func (n *Node) handleCustomer(ctx context.Context, cred Credential, operation string, params map[string]any, item dataplane.Item) ([]dataplane.Item, error) {
	switch operation {
	case OpGetAll, "list":
		return n.listCustomers(ctx, cred, params)
	case OpGet:
		id := firstInt64(params, "customerId", "id")
		if id == 0 {
			return nil, fmt.Errorf("customerId is required")
		}
		result, err := n.doJSON(ctx, cred, http.MethodGet, fmt.Sprintf("/customers/%d.json", id), nil)
		return singleValue(result["customer"], err)
	case OpSearch:
		return n.searchCustomers(ctx, cred, params)
	case OpCreate:
		return singleValue(n.createCustomer(ctx, cred, params, item))
	case OpUpdate:
		return singleValue(n.updateCustomer(ctx, cred, params))
	case OpDelete:
		id := firstInt64(params, "customerId", "id")
		if id == 0 {
			return nil, fmt.Errorf("customerId is required")
		}
		return singleValue(n.doJSON(ctx, cred, http.MethodDelete, fmt.Sprintf("/customers/%d.json", id), nil))
	default:
		return nil, fmt.Errorf("unknown customer operation %s", operation)
	}
}

func (n *Node) listCustomers(ctx context.Context, cred Credential, params map[string]any) ([]dataplane.Item, error) {
	query := url.Values{}
	limit := intParam(params, "limit")
	if limit <= 0 {
		limit = 50
	}
	query.Set("limit", fmt.Sprint(limit))
	for param, queryName := range map[string]string{
		"email":        "email",
		"sinceId":      "since_id",
		"createdAtMin": "created_at_min",
		"createdAtMax": "created_at_max",
		"fields":       "fields",
	} {
		if value := stringParam(params, param); value != "" {
			query.Set(queryName, value)
		}
	}
	path := "/customers.json?" + query.Encode()
	if boolParam(params, "returnAll") {
		return n.fetchAllPages(ctx, cred, path, "customers")
	}
	result, err := n.doJSON(ctx, cred, http.MethodGet, path, nil)
	return itemsFromArray(listFrom(result, "customers"), err)
}

func (n *Node) searchCustomers(ctx context.Context, cred Credential, params map[string]any) ([]dataplane.Item, error) {
	query := stringParam(params, "query")
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	limit := intParam(params, "limit")
	if limit <= 0 {
		limit = 50
	}
	path := fmt.Sprintf("/customers/search.json?query=%s&limit=%d", url.QueryEscape(query), limit)
	result, err := n.doJSON(ctx, cred, http.MethodGet, path, nil)
	return itemsFromArray(listFrom(result, "customers"), err)
}

func (n *Node) createCustomer(ctx context.Context, cred Credential, params map[string]any, item dataplane.Item) (map[string]any, error) {
	customer, err := customerBody(params, item, false)
	if err != nil {
		return nil, err
	}
	result, err := n.doJSON(ctx, cred, http.MethodPost, "/customers.json", map[string]any{"customer": customer})
	if err != nil {
		return nil, err
	}
	if object, ok := result["customer"].(map[string]any); ok {
		return object, nil
	}
	return result, nil
}

func (n *Node) updateCustomer(ctx context.Context, cred Credential, params map[string]any) (map[string]any, error) {
	id := firstInt64(params, "customerId", "id")
	if id == 0 {
		return nil, fmt.Errorf("customerId is required")
	}
	customer, err := customerBody(params, dataplane.Item{JSON: map[string]any{}}, true)
	if err != nil {
		return nil, err
	}
	customer["id"] = id
	result, err := n.doJSON(ctx, cred, http.MethodPut, fmt.Sprintf("/customers/%d.json", id), map[string]any{"customer": customer})
	if err != nil {
		return nil, err
	}
	if object, ok := result["customer"].(map[string]any); ok {
		return object, nil
	}
	return result, nil
}

func customerBody(params map[string]any, item dataplane.Item, partial bool) (map[string]any, error) {
	if raw, err := mapParam(params, "customer"); err != nil {
		return nil, err
	} else if raw != nil {
		return raw, nil
	}
	customer := map[string]any{}
	for param, key := range map[string]string{
		"email":     "email",
		"firstName": "first_name",
		"lastName":  "last_name",
		"phone":     "phone",
		"tags":      "tags",
		"password":  "password",
	} {
		setString(customer, key, stringParam(params, param))
	}
	extraKey := "additionalFields"
	if partial {
		extraKey = "updateFields"
	}
	if extra, err := mapParam(params, extraKey); err != nil {
		return nil, err
	} else {
		for key, value := range extra {
			customer[key] = value
		}
	}
	if customer["email"] == nil && item.JSON["email"] != nil {
		customer["email"] = item.JSON["email"]
	}
	if !partial && customer["email"] == nil {
		return nil, fmt.Errorf("email is required")
	}
	if password, ok := customer["password"]; ok {
		customer["password_confirmation"] = password
	}
	if boolParam(params, "sendWelcomeEmail") {
		customer["send_email_welcome"] = true
	}
	return customer, nil
}
