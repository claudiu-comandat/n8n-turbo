package telegram

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/binarydata"
	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type Node struct {
	client      *http.Client
	binaryStore binarydata.Store
	baseURL     string
	rateLimiter *RateLimiter
}

func New(binaryStore binarydata.Store) *Node {
	return &Node{
		client:      &http.Client{Timeout: 30 * time.Second},
		binaryStore: binaryStore,
		rateLimiter: NewRateLimiter(),
	}
}

func NewWithBaseURL(binaryStore binarydata.Store, baseURL string) *Node {
	node := New(binaryStore)
	node.baseURL = baseURL
	return node
}

func (n *Node) WithHTTPClient(client *http.Client) *Node {
	if client != nil {
		n.client = client
	}
	return n
}

func (n *Node) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	cred, err := extractCredential(in.Credentials, n.baseURL)
	if err != nil {
		return nil, err
	}
	if err := cred.Validate(); err != nil {
		return nil, err
	}
	params := in.Node.Parameters
	resource := stringParam(params, "resource")
	operation := stringParam(params, "operation")
	if operation == "" {
		operation = "sendMessage"
	}
	items := firstInput(in.InputData)
	if len(items) == 0 {
		items = []dataplane.Item{{JSON: map[string]any{}}}
	}
	out := []dataplane.Item{}
	for _, item := range items {
		result, err := n.executeOne(ctx, cred, resource, operation, params, item)
		if err != nil {
			return nil, fmt.Errorf("telegram %s: %w", operation, err)
		}
		out = append(out, result...)
	}
	return dataplane.MainOutput(out), nil
}

func (n *Node) executeOne(ctx context.Context, cred *Credential, resource string, operation string, params map[string]any, item dataplane.Item) ([]dataplane.Item, error) {
	key := resource + ":" + operation
	if resource == "" {
		key = operation
	}
	switch key {
	case "sendMessage", "message:sendMessage":
		return single(n.sendMessage(ctx, cred, params))
	case "sendPhoto", "message:sendPhoto":
		return single(n.sendMedia(ctx, cred, params, item, "sendPhoto", "photo"))
	case "sendDocument", "message:sendDocument":
		return single(n.sendMedia(ctx, cred, params, item, "sendDocument", "document"))
	case "sendVideo", "message:sendVideo":
		return single(n.sendMedia(ctx, cred, params, item, "sendVideo", "video"))
	case "sendAudio", "message:sendAudio":
		return single(n.sendMedia(ctx, cred, params, item, "sendAudio", "audio"))
	case "sendLocation", "message:sendLocation":
		return single(n.sendLocation(ctx, cred, params))
	case "sendChatAction", "message:sendChatAction":
		return single(n.simpleChatCall(ctx, cred, params, "sendChatAction", "action"))
	case "editMessageText", "message:editMessageText":
		return single(n.editMessageText(ctx, cred, params))
	case "deleteMessage", "message:deleteMessage":
		return single(n.simpleChatCall(ctx, cred, params, "deleteMessage", "message_id"))
	case "pinChatMessage", "message:pinChatMessage":
		return single(n.simpleChatCall(ctx, cred, params, "pinChatMessage", "message_id"))
	case "unpinChatMessage", "message:unpinChatMessage":
		return single(n.simpleChatCall(ctx, cred, params, "unpinChatMessage", "message_id"))
	case "getUpdates", "update:getUpdates":
		return n.getUpdates(ctx, cred, params)
	case "answerCallbackQuery", "callback:answerCallbackQuery":
		return single(n.answerCallbackQuery(ctx, cred, params))
	case "getChat", "chat:getChat":
		return single(n.simpleChatCall(ctx, cred, params, "getChat"))
	case "getChatMemberCount", "chat:getChatMemberCount":
		return single(n.simpleChatCall(ctx, cred, params, "getChatMemberCount"))
	case "banChatMember", "chat:banChatMember":
		return single(n.simpleChatCall(ctx, cred, params, "banChatMember", "user_id"))
	case "unbanChatMember", "chat:unbanChatMember":
		return single(n.simpleChatCall(ctx, cred, params, "unbanChatMember", "user_id"))
	case "setWebhook", "webhook:setWebhook":
		return single(n.setWebhook(ctx, cred, params))
	case "deleteWebhook", "webhook:deleteWebhook":
		return single(n.callAPI(ctx, cred, "deleteWebhook", map[string]any{"drop_pending_updates": boolParam(params, "dropPendingUpdates")}, ""))
	case "getWebhookInfo", "webhook:getWebhookInfo":
		return single(n.callAPI(ctx, cred, "getWebhookInfo", map[string]any{}, ""))
	case "getFile", "file:getFile":
		return single(n.getFile(ctx, cred, params))
	case "downloadFile", "file:downloadFile":
		return single(n.downloadFile(ctx, cred, params))
	default:
		return nil, fmt.Errorf("unsupported operation %s/%s", resource, operation)
	}
}

func single(result map[string]any, err error) ([]dataplane.Item, error) {
	if err != nil {
		return nil, err
	}
	return []dataplane.Item{{JSON: result}}, nil
}

func firstInput(input dataplane.Output) []dataplane.Item {
	if len(input) == 0 || len(input[0]) == 0 {
		return nil
	}
	return input[0]
}
