package msteams

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type Node struct {
	client          *http.Client
	baseURL         string
	tokenURLPattern string
}

func New() *Node {
	return &Node{
		client:          &http.Client{Timeout: 30 * time.Second},
		baseURL:         "https://graph.microsoft.com/v1.0",
		tokenURLPattern: "https://login.microsoftonline.com/%s/oauth2/v2.0/token",
	}
}

func NewWithBaseURL(baseURL string) *Node {
	node := New()
	node.baseURL = strings.TrimRight(baseURL, "/")
	return node
}

func NewWithURLs(baseURL string, tokenURLPattern string) *Node {
	node := NewWithBaseURL(baseURL)
	node.tokenURLPattern = tokenURLPattern
	return node
}

func (n *Node) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	cred, err := extractCredential(in.Credentials)
	if err != nil {
		return nil, err
	}
	if cred.IsExpired() && cred.RefreshToken != "" {
		if err := n.refreshToken(ctx, &cred); err != nil {
			return nil, fmt.Errorf("msteams token refresh failed: %w", err)
		}
	}
	params := in.Node.Parameters
	resource := stringValue(params, "resource")
	if resource == "" {
		resource = "message"
	}
	operation := stringValue(params, "operation")
	if operation == "" {
		operation = "send"
	}
	items := firstInput(in.InputData)
	if len(items) == 0 {
		items = []dataplane.Item{{JSON: map[string]any{}}}
	}
	output := []dataplane.Item{}
	for _, item := range items {
		result, err := n.dispatch(ctx, &cred, resource, operation, params, item)
		if err != nil {
			return nil, fmt.Errorf("msteams %s/%s: %w", resource, operation, err)
		}
		output = append(output, result...)
	}
	return dataplane.MainOutput(output), nil
}

func (n *Node) dispatch(ctx context.Context, cred *Credential, resource string, operation string, params map[string]any, item dataplane.Item) ([]dataplane.Item, error) {
	switch resource {
	case "message", "channelMessage":
		return n.handleMessage(ctx, cred, operation, params, item)
	case "chatMessage":
		return n.handleChatMessage(ctx, cred, operation, params)
	case "team", "teams":
		return n.handleTeam(ctx, cred, operation, params)
	case "channel", "channels":
		return n.handleChannel(ctx, cred, operation, params)
	case "user", "users":
		return n.handleUser(ctx, cred, operation, params)
	case "chat", "chats":
		return n.handleChat(ctx, cred, operation, params)
	case "task", "tasks":
		return n.handleTask(ctx, cred, operation, params)
	default:
		return nil, fmt.Errorf("unknown resource %s", resource)
	}
}
