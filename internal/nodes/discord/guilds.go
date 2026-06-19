package discord

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func (n *Node) handleGuild(ctx context.Context, cred Credential, operation string, params map[string]any) ([]dataplane.Item, error) {
	switch operation {
	case "get":
		guildID := stringParam(params, "guildId", "guild_id")
		if guildID == "" {
			return nil, fmt.Errorf("guildId is required")
		}
		return single(n.doJSON(ctx, cred, http.MethodGet, "/guilds/"+guildID, nil))
	case "getAll", "list":
		return itemsFromList(n.doList(ctx, cred, http.MethodGet, "/users/@me/guilds", nil))
	default:
		return nil, fmt.Errorf("unknown guild operation %s", operation)
	}
}

func (n *Node) handleMember(ctx context.Context, cred Credential, operation string, params map[string]any) ([]dataplane.Item, error) {
	guildID := stringParam(params, "guildId", "guild_id")
	if guildID == "" {
		return nil, fmt.Errorf("guildId is required")
	}
	switch operation {
	case "get":
		userID := stringParam(params, "userId", "user_id")
		if userID == "" {
			return nil, fmt.Errorf("userId is required")
		}
		return single(n.doJSON(ctx, cred, http.MethodGet, fmt.Sprintf("/guilds/%s/members/%s", guildID, userID), nil))
	case "getAll", "list":
		limit := intParam(params, "limit")
		if limit <= 0 {
			limit = 1000
		}
		query := url.Values{}
		query.Set("limit", fmt.Sprint(limit))
		if after := stringParam(params, "after"); after != "" {
			query.Set("after", after)
		}
		return itemsFromList(n.doList(ctx, cred, http.MethodGet, fmt.Sprintf("/guilds/%s/members?%s", guildID, query.Encode()), nil))
	case "kick":
		userID := stringParam(params, "userId", "user_id")
		if userID == "" {
			return nil, fmt.Errorf("userId is required")
		}
		return single(n.doJSON(ctx, cred, http.MethodDelete, fmt.Sprintf("/guilds/%s/members/%s", guildID, userID), nil))
	case "ban":
		userID := stringParam(params, "userId", "user_id")
		if userID == "" {
			return nil, fmt.Errorf("userId is required")
		}
		body := map[string]any{}
		if days := intParam(params, "deleteMessageDays"); days > 0 {
			body["delete_message_seconds"] = days * 86400
		}
		return single(n.doJSON(ctx, cred, http.MethodPut, fmt.Sprintf("/guilds/%s/bans/%s", guildID, userID), body))
	case "unban":
		userID := stringParam(params, "userId", "user_id")
		if userID == "" {
			return nil, fmt.Errorf("userId is required")
		}
		return single(n.doJSON(ctx, cred, http.MethodDelete, fmt.Sprintf("/guilds/%s/bans/%s", guildID, userID), nil))
	default:
		return nil, fmt.Errorf("unknown member operation %s", operation)
	}
}
