package msteams

import (
	"context"
	"fmt"
	"net/http"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func (n *Node) handleMessage(ctx context.Context, cred *Credential, operation string, params map[string]any, item dataplane.Item) ([]dataplane.Item, error) {
	switch operation {
	case "send", "create", "sendChannelMessage":
		return single(n.sendChannelMessage(ctx, cred, params))
	case "reply":
		return single(n.replyToMessage(ctx, cred, params))
	case "get":
		return single(n.getMessage(ctx, cred, params))
	case "getAll", "list":
		return itemsFromValue(n.listMessages(ctx, cred, params))
	case "delete":
		return single(n.deleteMessage(ctx, cred, params))
	case "sendDirect":
		return single(n.sendDirectMessage(ctx, cred, params))
	default:
		return nil, fmt.Errorf("unknown message operation %s", operation)
	}
}

func (n *Node) handleChatMessage(ctx context.Context, cred *Credential, operation string, params map[string]any) ([]dataplane.Item, error) {
	switch operation {
	case "create", "send", "sendAndWait":
		return single(n.sendChatMessage(ctx, cred, params))
	case "get":
		chatID := stringValue(params, "chatId")
		messageID := stringValue(params, "messageId")
		if chatID == "" || messageID == "" {
			return nil, fmt.Errorf("chatId and messageId are required")
		}
		return single(n.doJSON(ctx, cred, http.MethodGet, fmt.Sprintf("/chats/%s/messages/%s", chatID, messageID), nil))
	case "getAll", "list":
		chatID := stringValue(params, "chatId")
		if chatID == "" {
			return nil, fmt.Errorf("chatId is required")
		}
		return itemsFromValue(n.doJSON(ctx, cred, http.MethodGet, fmt.Sprintf("/chats/%s/messages", chatID), nil))
	default:
		return nil, fmt.Errorf("unknown chatMessage operation %s", operation)
	}
}

func messageBody(params map[string]any) (map[string]any, error) {
	content := stringValue(params, "message", "content", "text")
	if content == "" {
		return nil, fmt.Errorf("message content is required")
	}
	contentType := stringValue(params, "contentType")
	if contentType == "" {
		contentType = "text"
	}
	body := map[string]any{"body": map[string]any{"contentType": contentType, "content": content}}
	if subject := stringValue(params, "subject"); subject != "" {
		body["subject"] = subject
	}
	if importance := stringValue(params, "importance"); importance != "" {
		body["importance"] = importance
	}
	if card := stringValue(params, "adaptiveCard"); card != "" {
		body["attachments"] = []map[string]any{{"id": "1", "contentType": "application/vnd.microsoft.card.adaptive", "content": card}}
	}
	return body, nil
}

func (n *Node) sendChannelMessage(ctx context.Context, cred *Credential, params map[string]any) (map[string]any, error) {
	teamID := stringValue(params, "teamId")
	channelID := stringValue(params, "channelId")
	if teamID == "" || channelID == "" {
		return nil, fmt.Errorf("teamId and channelId are required")
	}
	body, err := messageBody(params)
	if err != nil {
		return nil, err
	}
	if replyID := stringValue(mapParam(params, "options"), "makeReply"); replyID != "" {
		return n.doJSON(ctx, cred, http.MethodPost, fmt.Sprintf("/teams/%s/channels/%s/messages/%s/replies", teamID, channelID, replyID), body)
	}
	return n.doJSON(ctx, cred, http.MethodPost, fmt.Sprintf("/teams/%s/channels/%s/messages", teamID, channelID), body)
}

func (n *Node) sendChatMessage(ctx context.Context, cred *Credential, params map[string]any) (map[string]any, error) {
	chatID := stringValue(params, "chatId")
	if chatID == "" {
		return nil, fmt.Errorf("chatId is required")
	}
	body, err := messageBody(params)
	if err != nil {
		return nil, err
	}
	return n.doJSON(ctx, cred, http.MethodPost, fmt.Sprintf("/chats/%s/messages", chatID), body)
}

func (n *Node) replyToMessage(ctx context.Context, cred *Credential, params map[string]any) (map[string]any, error) {
	teamID := stringValue(params, "teamId")
	channelID := stringValue(params, "channelId")
	messageID := stringValue(params, "messageId")
	if teamID == "" || channelID == "" || messageID == "" {
		return nil, fmt.Errorf("teamId, channelId, and messageId are required")
	}
	body, err := messageBody(params)
	if err != nil {
		return nil, err
	}
	return n.doJSON(ctx, cred, http.MethodPost, fmt.Sprintf("/teams/%s/channels/%s/messages/%s/replies", teamID, channelID, messageID), body)
}

func (n *Node) getMessage(ctx context.Context, cred *Credential, params map[string]any) (map[string]any, error) {
	teamID := stringValue(params, "teamId")
	channelID := stringValue(params, "channelId")
	messageID := stringValue(params, "messageId")
	if teamID == "" || channelID == "" || messageID == "" {
		return nil, fmt.Errorf("teamId, channelId, and messageId are required")
	}
	return n.doJSON(ctx, cred, http.MethodGet, fmt.Sprintf("/teams/%s/channels/%s/messages/%s", teamID, channelID, messageID), nil)
}

func (n *Node) listMessages(ctx context.Context, cred *Credential, params map[string]any) (map[string]any, error) {
	teamID := stringValue(params, "teamId")
	channelID := stringValue(params, "channelId")
	if teamID == "" || channelID == "" {
		return nil, fmt.Errorf("teamId and channelId are required")
	}
	return n.doJSON(ctx, cred, http.MethodGet, fmt.Sprintf("/teams/%s/channels/%s/messages", teamID, channelID), nil)
}

func (n *Node) deleteMessage(ctx context.Context, cred *Credential, params map[string]any) (map[string]any, error) {
	teamID := stringValue(params, "teamId")
	channelID := stringValue(params, "channelId")
	messageID := stringValue(params, "messageId")
	if teamID == "" || channelID == "" || messageID == "" {
		return nil, fmt.Errorf("teamId, channelId, and messageId are required")
	}
	return n.doJSON(ctx, cred, http.MethodDelete, fmt.Sprintf("/teams/%s/channels/%s/messages/%s", teamID, channelID, messageID), nil)
}

func (n *Node) sendDirectMessage(ctx context.Context, cred *Credential, params map[string]any) (map[string]any, error) {
	userID := stringValue(params, "userId")
	if userID == "" {
		email := stringValue(params, "userEmail")
		if email == "" {
			return nil, fmt.Errorf("userId or userEmail is required")
		}
		found, err := n.getUserIDByEmail(ctx, cred, email)
		if err != nil {
			return nil, err
		}
		userID = found
	}
	chatID, err := n.createOneOnOneChat(ctx, cred, userID)
	if err != nil {
		return nil, err
	}
	body, err := messageBody(params)
	if err != nil {
		return nil, err
	}
	return n.doJSON(ctx, cred, http.MethodPost, fmt.Sprintf("/chats/%s/messages", chatID), body)
}
