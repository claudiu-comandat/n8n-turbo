package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"path/filepath"

	"github.com/n8n-io/n8n-turbo/internal/binarydata"
	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func (n *Node) sendMessage(ctx context.Context, cred *Credential, params map[string]any) (map[string]any, error) {
	chatID := stringParam(params, "chatId", "chat_id")
	text := stringParam(params, "text")
	if chatID == "" {
		return nil, fmt.Errorf("chatId is required")
	}
	if text == "" {
		return nil, fmt.Errorf("text is required")
	}
	body := map[string]any{"chat_id": chatID, "text": text}
	addCommonMessageFields(body, params)
	return n.callAPI(ctx, cred, "sendMessage", body, chatID)
}

func (n *Node) sendLocation(ctx context.Context, cred *Credential, params map[string]any) (map[string]any, error) {
	chatID := stringParam(params, "chatId", "chat_id")
	if chatID == "" {
		return nil, fmt.Errorf("chatId is required")
	}
	body := map[string]any{
		"chat_id":   chatID,
		"latitude":  floatParam(params, "latitude"),
		"longitude": floatParam(params, "longitude"),
	}
	for source, target := range map[string]string{"horizontalAccuracy": "horizontal_accuracy", "livePeriod": "live_period", "heading": "heading", "proximityAlertRadius": "proximity_alert_radius"} {
		if value, ok := params[source]; ok {
			body[target] = value
		}
	}
	addCommonMessageFields(body, params)
	return n.callAPI(ctx, cred, "sendLocation", body, chatID)
}

func (n *Node) editMessageText(ctx context.Context, cred *Credential, params map[string]any) (map[string]any, error) {
	text := stringParam(params, "text")
	if text == "" {
		return nil, fmt.Errorf("text is required")
	}
	body := map[string]any{"text": text}
	if chatID := stringParam(params, "chatId", "chat_id"); chatID != "" {
		body["chat_id"] = chatID
	}
	if messageID := intParam(params, "messageId"); messageID != 0 {
		body["message_id"] = messageID
	}
	if inlineID := stringParam(params, "inlineMessageId"); inlineID != "" {
		body["inline_message_id"] = inlineID
	}
	addCommonMessageFields(body, params)
	return n.callAPI(ctx, cred, "editMessageText", body, stringParam(params, "chatId", "chat_id"))
}

func (n *Node) simpleChatCall(ctx context.Context, cred *Credential, params map[string]any, method string, extra ...string) (map[string]any, error) {
	chatID := stringParam(params, "chatId", "chat_id")
	if chatID == "" {
		return nil, fmt.Errorf("chatId is required")
	}
	body := map[string]any{"chat_id": chatID}
	for _, key := range extra {
		target := key
		source := key
		switch key {
		case "message_id":
			source = "messageId"
		case "user_id":
			source = "userId"
		}
		value := stringParam(params, source, key)
		if value == "" {
			return nil, fmt.Errorf("%s is required", source)
		}
		body[target] = value
	}
	return n.callAPI(ctx, cred, method, body, chatID)
}

func (n *Node) answerCallbackQuery(ctx context.Context, cred *Credential, params map[string]any) (map[string]any, error) {
	queryID := stringParam(params, "callbackQueryId", "callback_query_id")
	if queryID == "" {
		return nil, fmt.Errorf("callbackQueryId is required")
	}
	body := map[string]any{"callback_query_id": queryID}
	if text := stringParam(params, "text"); text != "" {
		body["text"] = text
	}
	if url := stringParam(params, "url"); url != "" {
		body["url"] = url
	}
	if boolParam(params, "showAlert") {
		body["show_alert"] = true
	}
	if cacheTime := intParam(params, "cacheTime"); cacheTime > 0 {
		body["cache_time"] = cacheTime
	}
	return n.callAPI(ctx, cred, "answerCallbackQuery", body, "")
}

func (n *Node) getUpdates(ctx context.Context, cred *Credential, params map[string]any) ([]dataplane.Item, error) {
	body := map[string]any{}
	if offset := intParam(params, "offset"); offset > 0 {
		body["offset"] = offset
	}
	if limit := intParam(params, "limit"); limit > 0 {
		body["limit"] = limit
	}
	if timeout := intParam(params, "timeout"); timeout > 0 {
		body["timeout"] = timeout
	}
	if updates := allowedUpdates(params["allowedUpdates"]); len(updates) > 0 {
		body["allowed_updates"] = updates
	}
	response, err := n.callAPI(ctx, cred, "getUpdates", body, "")
	if err != nil {
		return nil, err
	}
	updates, ok := response["result"].([]any)
	if !ok {
		return []dataplane.Item{{JSON: response}}, nil
	}
	items := make([]dataplane.Item, 0, len(updates))
	for _, update := range updates {
		if object, ok := update.(map[string]any); ok {
			items = append(items, dataplane.Item{JSON: object})
		}
	}
	return items, nil
}

func (n *Node) setWebhook(ctx context.Context, cred *Credential, params map[string]any) (map[string]any, error) {
	url := stringParam(params, "url", "webhookUrl")
	if url == "" {
		return nil, fmt.Errorf("url is required")
	}
	body := map[string]any{"url": url}
	if secret := stringParam(params, "secretToken"); secret != "" {
		body["secret_token"] = secret
	}
	if max := intParam(params, "maxConnections"); max > 0 {
		body["max_connections"] = max
	}
	if updates := allowedUpdates(params["allowedUpdates"]); len(updates) > 0 {
		body["allowed_updates"] = updates
	}
	return n.callAPI(ctx, cred, "setWebhook", body, "")
}

func (n *Node) getFile(ctx context.Context, cred *Credential, params map[string]any) (map[string]any, error) {
	fileID := stringParam(params, "fileId", "file_id")
	if fileID == "" {
		return nil, fmt.Errorf("fileId is required")
	}
	return n.callAPI(ctx, cred, "getFile", map[string]any{"file_id": fileID}, "")
}

func addCommonMessageFields(body map[string]any, params map[string]any) {
	for source, target := range map[string]string{
		"parseMode":             "parse_mode",
		"disableNotification":   "disable_notification",
		"protectContent":        "protect_content",
		"replyToMessageId":      "reply_to_message_id",
		"disableWebPagePreview": "disable_web_page_preview",
	} {
		if value, ok := params[source]; ok {
			body[target] = value
		}
	}
	for source, target := range map[string]string{
		"parseMode":             "parse_mode",
		"disableNotification":   "disable_notification",
		"protectContent":        "protect_content",
		"replyToMessageId":      "reply_to_message_id",
		"disableWebPagePreview": "disable_web_page_preview",
	} {
		if value, ok := mapParam(params, "additionalFields")[source]; ok {
			body[target] = value
		}
	}
	if markup := stringParam(params, "replyMarkup"); markup != "" {
		value, err := parseJSONValue(markup)
		if err == nil {
			body["reply_markup"] = value
		}
	}
	if value, ok := params["replyMarkupObject"]; ok {
		body["reply_markup"] = value
	}
}

func decodeResultFile(response map[string]any) (File, error) {
	data, err := json.Marshal(response["result"])
	if err != nil {
		return File{}, err
	}
	var file File
	if err := json.Unmarshal(data, &file); err != nil {
		return File{}, err
	}
	if file.FilePath == "" {
		return File{}, fmt.Errorf("file_path missing from Telegram response")
	}
	return file, nil
}

func binaryRef(binary dataplane.Binary) binarydata.Ref {
	return binarydata.RefFromBinary(binary)
}

func mimeForPath(path string) string {
	if value := mime.TypeByExtension(filepath.Ext(path)); value != "" {
		return value
	}
	return "application/octet-stream"
}
