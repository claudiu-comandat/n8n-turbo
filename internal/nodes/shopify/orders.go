package shopify

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func (n *Node) handleOrder(ctx context.Context, cred Credential, operation string, params map[string]any, item dataplane.Item) ([]dataplane.Item, error) {
	switch operation {
	case OpGetAll, "list":
		return n.listOrders(ctx, cred, params)
	case OpGet:
		id := firstInt64(params, "orderId", "id")
		if id == 0 {
			return nil, fmt.Errorf("orderId is required")
		}
		result, err := n.doJSON(ctx, cred, http.MethodGet, fmt.Sprintf("/orders/%d.json", id), nil)
		return singleValue(result["order"], err)
	case OpCreate:
		return singleValue(n.createOrder(ctx, cred, params, item))
	case OpUpdate:
		return singleValue(n.updateOrder(ctx, cred, params))
	case OpCancel:
		return singleValue(n.cancelOrder(ctx, cred, params))
	case OpRefund:
		return singleValue(n.refundOrder(ctx, cred, params))
	case OpDelete:
		id := firstInt64(params, "orderId", "id")
		if id == 0 {
			return nil, fmt.Errorf("orderId is required")
		}
		return singleValue(n.doJSON(ctx, cred, http.MethodDelete, fmt.Sprintf("/orders/%d.json", id), nil))
	default:
		return nil, fmt.Errorf("unknown order operation %s", operation)
	}
}

func (n *Node) listOrders(ctx context.Context, cred Credential, params map[string]any) ([]dataplane.Item, error) {
	query := url.Values{}
	limit := intParam(params, "limit")
	if limit <= 0 {
		limit = 50
	}
	query.Set("limit", fmt.Sprint(limit))
	for param, queryName := range map[string]string{
		"status":            "status",
		"financialStatus":   "financial_status",
		"fulfillmentStatus": "fulfillment_status",
		"createdAtMin":      "created_at_min",
		"createdAtMax":      "created_at_max",
		"sinceId":           "since_id",
		"fields":            "fields",
	} {
		if value := stringParam(params, param); value != "" {
			query.Set(queryName, value)
		}
	}
	if query.Get("status") == "" {
		query.Set("status", "any")
	}
	path := "/orders.json?" + query.Encode()
	if boolParam(params, "returnAll") {
		return n.fetchAllPages(ctx, cred, path, "orders")
	}
	result, err := n.doJSON(ctx, cred, http.MethodGet, path, nil)
	return itemsFromArray(listFrom(result, "orders"), err)
}

func (n *Node) createOrder(ctx context.Context, cred Credential, params map[string]any, item dataplane.Item) (map[string]any, error) {
	order, err := orderBody(params, item)
	if err != nil {
		return nil, err
	}
	result, err := n.doJSON(ctx, cred, http.MethodPost, "/orders.json", map[string]any{"order": order})
	if err != nil {
		return nil, err
	}
	if object, ok := result["order"].(map[string]any); ok {
		return object, nil
	}
	return result, nil
}

func (n *Node) updateOrder(ctx context.Context, cred Credential, params map[string]any) (map[string]any, error) {
	id := firstInt64(params, "orderId", "id")
	if id == 0 {
		return nil, fmt.Errorf("orderId is required")
	}
	order := map[string]any{"id": id}
	if update, err := mapParam(params, "updateFields"); err != nil {
		return nil, err
	} else {
		for key, value := range update {
			order[key] = value
		}
	}
	for _, key := range []string{"email", "phone", "note", "tags"} {
		setString(order, key, stringParam(params, key))
	}
	result, err := n.doJSON(ctx, cred, http.MethodPut, fmt.Sprintf("/orders/%d.json", id), map[string]any{"order": order})
	if err != nil {
		return nil, err
	}
	if object, ok := result["order"].(map[string]any); ok {
		return object, nil
	}
	return result, nil
}

func (n *Node) cancelOrder(ctx context.Context, cred Credential, params map[string]any) (map[string]any, error) {
	id := firstInt64(params, "orderId", "id")
	if id == 0 {
		return nil, fmt.Errorf("orderId is required")
	}
	body := map[string]any{}
	setString(body, "reason", stringParam(params, "reason"))
	if boolParam(params, "sendEmail") {
		body["email"] = true
	}
	if boolParam(params, "restock") {
		body["restock"] = true
	}
	result, err := n.doJSON(ctx, cred, http.MethodPost, fmt.Sprintf("/orders/%d/cancel.json", id), body)
	if err != nil {
		return nil, err
	}
	if object, ok := result["order"].(map[string]any); ok {
		return object, nil
	}
	return result, nil
}

func (n *Node) refundOrder(ctx context.Context, cred Credential, params map[string]any) (map[string]any, error) {
	id := firstInt64(params, "orderId", "id")
	if id == 0 {
		return nil, fmt.Errorf("orderId is required")
	}
	refund := map[string]any{"notify": boolParam(params, "notifyCustomer")}
	if boolParam(params, "refundShipping") {
		refund["shipping"] = map[string]any{"full_refund": true}
	}
	if amount := stringParam(params, "amount"); amount != "" {
		transaction := map[string]any{"kind": "refund", "amount": amount}
		setString(transaction, "gateway", stringParam(params, "gateway"))
		refund["transactions"] = []map[string]any{transaction}
	}
	setString(refund, "note", stringParam(params, "note"))
	result, err := n.doJSON(ctx, cred, http.MethodPost, fmt.Sprintf("/orders/%d/refunds.json", id), map[string]any{"refund": refund})
	if err != nil {
		return nil, err
	}
	if object, ok := result["refund"].(map[string]any); ok {
		return object, nil
	}
	return result, nil
}

func orderBody(params map[string]any, item dataplane.Item) (map[string]any, error) {
	if raw, err := mapParam(params, "order"); err != nil {
		return nil, err
	} else if raw != nil {
		return raw, nil
	}
	order := map[string]any{}
	for _, key := range []string{"email", "phone", "note", "tags", "financialStatus", "fulfillmentStatus"} {
		value := stringParam(params, key)
		switch key {
		case "financialStatus":
			setString(order, "financial_status", value)
		case "fulfillmentStatus":
			setString(order, "fulfillment_status", value)
		default:
			setString(order, key, value)
		}
	}
	if extra, err := mapParam(params, "additionalFields"); err != nil {
		return nil, err
	} else {
		for key, value := range extra {
			order[key] = value
		}
	}
	if lineItems, err := arrayParam(params, "lineItems"); err != nil {
		return nil, err
	} else if len(lineItems) > 0 {
		order["line_items"] = lineItems
	} else if item.JSON["line_items"] != nil {
		order["line_items"] = item.JSON["line_items"]
	} else {
		return nil, fmt.Errorf("at least one line item is required")
	}
	if customerID := int64Param(params, "customerId"); customerID > 0 {
		order["customer"] = map[string]any{"id": customerID}
	}
	if boolParam(params, "sendReceipt") {
		order["send_receipt"] = true
	}
	if boolParam(params, "sendFulfillmentReceipt") {
		order["send_fulfillment_receipt"] = true
	}
	return order, nil
}
