package shopify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func (n *Node) handleWebhook(ctx context.Context, cred Credential, operation string, params map[string]any) ([]dataplane.Item, error) {
	switch operation {
	case OpGetAll, "list":
		result, err := n.doJSON(ctx, cred, http.MethodGet, "/webhooks.json", nil)
		return itemsFromArray(listFrom(result, "webhooks"), err)
	case OpGet:
		id := int64Param(params, "webhookId")
		if id == 0 {
			return nil, fmt.Errorf("webhookId is required")
		}
		result, err := n.doJSON(ctx, cred, http.MethodGet, fmt.Sprintf("/webhooks/%d.json", id), nil)
		return singleValue(result["webhook"], err)
	case OpCreate:
		return singleValue(n.createWebhook(ctx, cred, params))
	case OpUpdate:
		return singleValue(n.updateWebhook(ctx, cred, params))
	case OpDelete:
		id := int64Param(params, "webhookId")
		if id == 0 {
			return nil, fmt.Errorf("webhookId is required")
		}
		return singleValue(n.doJSON(ctx, cred, http.MethodDelete, fmt.Sprintf("/webhooks/%d.json", id), nil))
	default:
		return nil, fmt.Errorf("unknown webhook operation %s", operation)
	}
}

func (n *Node) createWebhook(ctx context.Context, cred Credential, params map[string]any) (map[string]any, error) {
	webhook := map[string]any{
		"topic":   stringParam(params, "topic"),
		"address": stringParam(params, "callbackUrl", "address"),
		"format":  "json",
	}
	if format := stringParam(params, "format"); format != "" {
		webhook["format"] = format
	}
	if webhook["topic"] == "" || webhook["address"] == "" {
		return nil, fmt.Errorf("topic and callbackUrl are required")
	}
	result, err := n.doJSON(ctx, cred, http.MethodPost, "/webhooks.json", map[string]any{"webhook": webhook})
	if err != nil {
		return nil, err
	}
	if object, ok := result["webhook"].(map[string]any); ok {
		return object, nil
	}
	return result, nil
}

func (n *Node) updateWebhook(ctx context.Context, cred Credential, params map[string]any) (map[string]any, error) {
	id := int64Param(params, "webhookId")
	if id == 0 {
		return nil, fmt.Errorf("webhookId is required")
	}
	webhook := map[string]any{"id": id}
	setString(webhook, "address", stringParam(params, "callbackUrl", "address"))
	setString(webhook, "format", stringParam(params, "format"))
	result, err := n.doJSON(ctx, cred, http.MethodPut, fmt.Sprintf("/webhooks/%d.json", id), map[string]any{"webhook": webhook})
	if err != nil {
		return nil, err
	}
	if object, ok := result["webhook"].(map[string]any); ok {
		return object, nil
	}
	return result, nil
}

func VerifyWebhookHMAC(secret string, data []byte, hmacHeader string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(data)
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(hmacHeader))
}
