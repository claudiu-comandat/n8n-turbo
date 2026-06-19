package discord

import (
	"context"
	"fmt"
	"net/http"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func (n *Node) handleMessage(ctx context.Context, cred Credential, operation string, params map[string]any) ([]dataplane.Item, error) {
	switch operation {
	case "send", "sendMessage", "create":
		return single(n.sendMessage(ctx, cred, params))
	case "get", "getMessage":
		channelID, messageID, err := channelAndMessage(params)
		if err != nil {
			return nil, err
		}
		return single(n.doJSON(ctx, cred, http.MethodGet, fmt.Sprintf("/channels/%s/messages/%s", channelID, messageID), nil))
	case "update", "edit":
		return single(n.updateMessage(ctx, cred, params))
	case "delete":
		channelID, messageID, err := channelAndMessage(params)
		if err != nil {
			return nil, err
		}
		return single(n.doJSON(ctx, cred, http.MethodDelete, fmt.Sprintf("/channels/%s/messages/%s", channelID, messageID), nil))
	case "pin":
		channelID, messageID, err := channelAndMessage(params)
		if err != nil {
			return nil, err
		}
		return single(n.doJSON(ctx, cred, http.MethodPut, fmt.Sprintf("/channels/%s/pins/%s", channelID, messageID), nil))
	case "unpin":
		channelID, messageID, err := channelAndMessage(params)
		if err != nil {
			return nil, err
		}
		return single(n.doJSON(ctx, cred, http.MethodDelete, fmt.Sprintf("/channels/%s/pins/%s", channelID, messageID), nil))
	case "getPinnedMessages":
		channelID := stringParam(params, "channelId", "channel_id")
		if channelID == "" {
			return nil, fmt.Errorf("channelId is required")
		}
		return itemsFromList(n.doList(ctx, cred, http.MethodGet, fmt.Sprintf("/channels/%s/pins", channelID), nil))
	default:
		return nil, fmt.Errorf("unknown message operation %s", operation)
	}
}

func (n *Node) sendMessage(ctx context.Context, cred Credential, params map[string]any) (map[string]any, error) {
	channelID := stringParam(params, "channelId", "channel_id")
	if channelID == "" {
		return nil, fmt.Errorf("channelId is required")
	}
	body := map[string]any{}
	if content := stringParam(params, "content", "text"); content != "" {
		body["content"] = content
	}
	if boolParam(params, "tts") {
		body["tts"] = true
	}
	if nonce := stringParam(mapParam(params, "additionalFields"), "nonce"); nonce != "" {
		body["nonce"] = nonce
	}
	if stringParam(mapParam(params, "additionalFields"), "disableMentions") == "true" || boolParam(mapParam(params, "additionalFields"), "disableMentions") {
		body["allowed_mentions"] = AllowedMentions{Parse: []string{}}
	}
	if replyTo := stringParam(params, "replyToMessageId"); replyTo != "" {
		body["message_reference"] = MessageReference{MessageID: replyTo, ChannelID: channelID}
	}
	embeds, err := parseEmbeds(params)
	if err != nil {
		return nil, err
	}
	if len(embeds) > 0 {
		body["embeds"] = embeds
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("content or embed is required")
	}
	return n.doJSON(ctx, cred, http.MethodPost, fmt.Sprintf("/channels/%s/messages", channelID), body)
}

func (n *Node) updateMessage(ctx context.Context, cred Credential, params map[string]any) (map[string]any, error) {
	channelID, messageID, err := channelAndMessage(params)
	if err != nil {
		return nil, err
	}
	body := map[string]any{}
	if content := stringParam(params, "content", "text"); content != "" {
		body["content"] = content
	}
	embeds, err := parseEmbeds(params)
	if err != nil {
		return nil, err
	}
	if len(embeds) > 0 {
		body["embeds"] = embeds
	}
	return n.doJSON(ctx, cred, http.MethodPatch, fmt.Sprintf("/channels/%s/messages/%s", channelID, messageID), body)
}

func channelAndMessage(params map[string]any) (string, string, error) {
	channelID := stringParam(params, "channelId", "channel_id")
	messageID := stringParam(params, "messageId", "message_id")
	if channelID == "" || messageID == "" {
		return "", "", fmt.Errorf("channelId and messageId are required")
	}
	return channelID, messageID, nil
}
