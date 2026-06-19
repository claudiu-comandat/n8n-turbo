package discord

import (
	"context"
	"fmt"
	"net/http"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func (n *Node) handleChannel(ctx context.Context, cred Credential, operation string, params map[string]any) ([]dataplane.Item, error) {
	switch operation {
	case "get":
		channelID := stringParam(params, "channelId", "channel_id")
		if channelID == "" {
			return nil, fmt.Errorf("channelId is required")
		}
		return single(n.doJSON(ctx, cred, http.MethodGet, "/channels/"+channelID, nil))
	case "getAll", "list":
		guildID := stringParam(params, "guildId", "guild_id")
		if guildID == "" {
			return nil, fmt.Errorf("guildId is required")
		}
		return itemsFromList(n.doList(ctx, cred, http.MethodGet, "/guilds/"+guildID+"/channels", nil))
	case "create":
		guildID := stringParam(params, "guildId", "guild_id")
		if guildID == "" {
			return nil, fmt.Errorf("guildId is required")
		}
		body := map[string]any{"name": stringParam(params, "name"), "type": intParam(params, "type")}
		for key, value := range mapParam(params, "additionalFields") {
			body[key] = value
		}
		return single(n.doJSON(ctx, cred, http.MethodPost, "/guilds/"+guildID+"/channels", body))
	case "update":
		channelID := stringParam(params, "channelId", "channel_id")
		if channelID == "" {
			return nil, fmt.Errorf("channelId is required")
		}
		body := map[string]any{}
		if name := stringParam(params, "name"); name != "" {
			body["name"] = name
		}
		for key, value := range mapParam(params, "additionalFields") {
			body[key] = value
		}
		return single(n.doJSON(ctx, cred, http.MethodPatch, "/channels/"+channelID, body))
	case "delete":
		channelID := stringParam(params, "channelId", "channel_id")
		if channelID == "" {
			return nil, fmt.Errorf("channelId is required")
		}
		return single(n.doJSON(ctx, cred, http.MethodDelete, "/channels/"+channelID, nil))
	default:
		return nil, fmt.Errorf("unknown channel operation %s", operation)
	}
}
