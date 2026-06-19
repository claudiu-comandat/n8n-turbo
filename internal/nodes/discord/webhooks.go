package discord

import (
	"context"
	"fmt"
	"net/http"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func (n *Node) handleWebhook(ctx context.Context, cred Credential, operation string, params map[string]any) ([]dataplane.Item, error) {
	switch operation {
	case "create":
		channelID := stringParam(params, "channelId", "channel_id")
		if channelID == "" {
			return nil, fmt.Errorf("channelId is required")
		}
		name := stringParam(params, "name")
		if name == "" {
			name = "n8n webhook"
		}
		return single(n.doJSON(ctx, cred, http.MethodPost, "/channels/"+channelID+"/webhooks", map[string]any{"name": name}))
	case "get":
		webhookID := stringParam(params, "webhookId", "webhook_id")
		if webhookID == "" {
			return nil, fmt.Errorf("webhookId is required")
		}
		return single(n.doJSON(ctx, cred, http.MethodGet, "/webhooks/"+webhookID, nil))
	case "getAll", "list":
		channelID := stringParam(params, "channelId", "channel_id")
		if channelID == "" {
			return nil, fmt.Errorf("channelId is required")
		}
		return itemsFromList(n.doList(ctx, cred, http.MethodGet, "/channels/"+channelID+"/webhooks", nil))
	case "execute":
		return single(n.executeWebhook(ctx, params))
	case "delete":
		webhookID := stringParam(params, "webhookId", "webhook_id")
		if webhookID == "" {
			return nil, fmt.Errorf("webhookId is required")
		}
		return single(n.doJSON(ctx, cred, http.MethodDelete, "/webhooks/"+webhookID, nil))
	default:
		return nil, fmt.Errorf("unknown webhook operation %s", operation)
	}
}

func (n *Node) executeWebhook(ctx context.Context, params map[string]any) (map[string]any, error) {
	webhookURL := stringParam(params, "webhookUrl", "url")
	if webhookURL == "" {
		webhookID := stringParam(params, "webhookId", "webhook_id")
		webhookToken := stringParam(params, "webhookToken", "webhook_token")
		if webhookID == "" || webhookToken == "" {
			return nil, fmt.Errorf("webhookUrl or webhookId/webhookToken is required")
		}
		webhookURL = n.baseURL + "/webhooks/" + webhookID + "/" + webhookToken
	}
	body := map[string]any{}
	if content := stringParam(params, "content", "text"); content != "" {
		body["content"] = content
	}
	if username := stringParam(params, "username"); username != "" {
		body["username"] = username
	}
	if avatar := stringParam(params, "avatarUrl", "avatar_url"); avatar != "" {
		body["avatar_url"] = avatar
	}
	if boolParam(params, "tts") {
		body["tts"] = true
	}
	embeds, err := parseEmbeds(params)
	if err != nil {
		return nil, err
	}
	if len(embeds) > 0 {
		body["embeds"] = embeds
	}
	return n.doUnauthenticatedJSON(ctx, http.MethodPost, webhookURL, body)
}
