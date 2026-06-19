package discord

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func (n *Node) handleReaction(ctx context.Context, cred Credential, operation string, params map[string]any) ([]dataplane.Item, error) {
	channelID, messageID, err := channelAndMessage(params)
	if err != nil {
		return nil, err
	}
	switch operation {
	case "add":
		emoji := stringParam(params, "emoji")
		if emoji == "" {
			return nil, fmt.Errorf("emoji is required")
		}
		return single(n.doJSON(ctx, cred, http.MethodPut, fmt.Sprintf("/channels/%s/messages/%s/reactions/%s/@me", channelID, messageID, url.PathEscape(emoji)), nil))
	case "remove":
		emoji := stringParam(params, "emoji")
		if emoji == "" {
			return nil, fmt.Errorf("emoji is required")
		}
		userID := stringParam(params, "userId", "user_id")
		if userID == "" {
			userID = "@me"
		}
		return single(n.doJSON(ctx, cred, http.MethodDelete, fmt.Sprintf("/channels/%s/messages/%s/reactions/%s/%s", channelID, messageID, url.PathEscape(emoji), userID), nil))
	case "getAll", "list":
		emoji := stringParam(params, "emoji")
		if emoji == "" {
			return nil, fmt.Errorf("emoji is required")
		}
		limit := intParam(params, "limit")
		if limit <= 0 {
			limit = 25
		}
		return itemsFromList(n.doList(ctx, cred, http.MethodGet, fmt.Sprintf("/channels/%s/messages/%s/reactions/%s?limit=%d", channelID, messageID, url.PathEscape(emoji), limit), nil))
	case "removeAll":
		return single(n.doJSON(ctx, cred, http.MethodDelete, fmt.Sprintf("/channels/%s/messages/%s/reactions", channelID, messageID), nil))
	default:
		return nil, fmt.Errorf("unknown reaction operation %s", operation)
	}
}
