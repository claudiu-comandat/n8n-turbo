package msteams

import (
	"context"
	"fmt"
	"net/http"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func (n *Node) handleChat(ctx context.Context, cred *Credential, operation string, params map[string]any) ([]dataplane.Item, error) {
	switch operation {
	case "getAll", "list":
		return itemsFromValue(n.doJSON(ctx, cred, http.MethodGet, "/chats", nil))
	case "get":
		chatID := stringValue(params, "chatId")
		if chatID == "" {
			return nil, fmt.Errorf("chatId is required")
		}
		return single(n.doJSON(ctx, cred, http.MethodGet, "/chats/"+chatID, nil))
	case "create":
		userID := stringValue(params, "userId")
		if userID == "" {
			return nil, fmt.Errorf("userId is required")
		}
		chatID, err := n.createOneOnOneChat(ctx, cred, userID)
		if err != nil {
			return nil, err
		}
		return []dataplane.Item{{JSON: map[string]any{"id": chatID}}}, nil
	default:
		return nil, fmt.Errorf("unknown chat operation %s", operation)
	}
}

func (n *Node) createOneOnOneChat(ctx context.Context, cred *Credential, otherUserID string) (string, error) {
	me, err := n.doJSON(ctx, cred, http.MethodGet, "/me", nil)
	if err != nil {
		return "", err
	}
	myID := stringValue(me, "id")
	if myID == "" {
		return "", fmt.Errorf("current user id not found")
	}
	body := map[string]any{
		"chatType": "oneOnOne",
		"members":  []map[string]any{conversationMember(myID), conversationMember(otherUserID)},
	}
	result, err := n.doJSON(ctx, cred, http.MethodPost, "/chats", body)
	if err != nil {
		return "", err
	}
	id := stringValue(result, "id")
	if id == "" {
		return "", fmt.Errorf("chat id not found")
	}
	return id, nil
}

func conversationMember(userID string) map[string]any {
	return map[string]any{
		"@odata.type":     "#microsoft.graph.aadUserConversationMember",
		"roles":           []string{"owner"},
		"user@odata.bind": fmt.Sprintf("https://graph.microsoft.com/v1.0/users('%s')", userID),
	}
}
