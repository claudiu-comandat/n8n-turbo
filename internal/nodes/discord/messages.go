package discord

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func (n *Node) handleMessage(ctx context.Context, cred Credential, operation string, params map[string]any) ([]dataplane.Item, error) {
	switch operation {
	case "send", "sendMessage", "create", "sendAndWait", "sendLegacy":
		return single(n.sendMessage(ctx, cred, params))
	case "getAll", "list":
		return itemsFromList(n.getMessages(ctx, cred, params))
	case "react":
		channelID, messageID, err := channelAndMessage(params)
		if err != nil {
			return nil, err
		}
		emoji := stringParam(params, "emoji")
		if emoji == "" {
			return nil, fmt.Errorf("emoji is required")
		}
		return single(n.doJSON(ctx, cred, http.MethodPut, fmt.Sprintf("/channels/%s/messages/%s/reactions/%s/@me", channelID, messageID, url.PathEscape(emoji)), nil))
	case "get", "getMessage":
		channelID, messageID, err := channelAndMessage(params)
		if err != nil {
			return nil, err
		}
		return single(n.doJSON(ctx, cred, http.MethodGet, fmt.Sprintf("/channels/%s/messages/%s", channelID, messageID), nil))
	case "update", "edit":
		return single(n.updateMessage(ctx, cred, params))
	case "delete", "deleteMessage":
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
	guildID := stringParam(params, "guildId", "guild_id")
	if stringParam(params, "sendTo") == "user" {
		userID := stringParam(params, "userId", "user_id")
		if userID == "" {
			return nil, fmt.Errorf("userId is required")
		}
		dm, err := n.doJSON(ctx, cred, http.MethodPost, "/users/@me/channels", map[string]any{"recipient_id": userID})
		if err != nil {
			return nil, err
		}
		channelID = stringParam(dm, "id")
	}
	if channelID == "" {
		return nil, fmt.Errorf("channelId is required")
	}
	body := map[string]any{}
	if content := stringParam(params, "content", "text", "message"); content != "" {
		body["content"] = content
	}
	options := mapParam(params, "options")
	if boolParam(params, "tts") || boolParam(options, "tts") {
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
	if replyTo := stringParam(options, "message_reference"); replyTo != "" {
		body["message_reference"] = MessageReference{MessageID: replyTo, GuildID: guildID}
	}
	if flags := discordFlags(stringSlice(options, "flags")); flags > 0 {
		body["flags"] = flags
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

func (n *Node) getMessages(ctx context.Context, cred Credential, params map[string]any) ([]map[string]any, error) {
	channelID := stringParam(params, "channelId", "channel_id")
	if channelID == "" {
		return nil, fmt.Errorf("channelId is required")
	}
	query := url.Values{}
	if !boolParam(params, "returnAll") {
		limit := intParam(params, "limit")
		if limit <= 0 {
			limit = 50
		}
		query.Set("limit", fmt.Sprint(limit))
		return n.doList(ctx, cred, http.MethodGet, fmt.Sprintf("/channels/%s/messages?%s", channelID, query.Encode()), nil)
	}
	out := []map[string]any{}
	query.Set("limit", "100")
	for {
		page, err := n.doList(ctx, cred, http.MethodGet, fmt.Sprintf("/channels/%s/messages?%s", channelID, query.Encode()), nil)
		if err != nil {
			return nil, err
		}
		if len(page) == 0 {
			break
		}
		out = append(out, page...)
		query.Set("before", stringParam(page[len(page)-1], "id"))
	}
	return out, nil
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

func discordFlags(values []string) int {
	flags := 0
	for _, value := range values {
		switch strings.ToUpper(value) {
		case "SUPPRESS_EMBEDS":
			flags |= 1 << 2
		case "SUPPRESS_NOTIFICATIONS":
			flags |= 1 << 12
		}
	}
	return flags
}

func channelAndMessage(params map[string]any) (string, string, error) {
	channelID := stringParam(params, "channelId", "channel_id")
	messageID := stringParam(params, "messageId", "message_id")
	if channelID == "" || messageID == "" {
		return "", "", fmt.Errorf("channelId and messageId are required")
	}
	return channelID, messageID, nil
}
