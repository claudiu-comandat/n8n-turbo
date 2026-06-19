package discord

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
	client      *http.Client
	baseURL     string
	rateLimiter *RateLimiter
}

func New() *Node {
	return &Node{client: &http.Client{Timeout: 30 * time.Second}, baseURL: "https://discord.com/api/v10", rateLimiter: NewRateLimiter()}
}

func NewWithBaseURL(baseURL string) *Node {
	node := New()
	node.baseURL = strings.TrimRight(baseURL, "/")
	return node
}

func (n *Node) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	cred, err := extractCredential(in.Credentials)
	if err != nil {
		return nil, err
	}
	params := in.Node.Parameters
	resource := stringParam(params, "resource")
	if resource == "" {
		resource = "message"
	}
	operation := stringParam(params, "operation")
	if operation == "" {
		operation = "send"
	}
	items := firstInput(in.InputData)
	if len(items) == 0 {
		items = []dataplane.Item{{JSON: map[string]any{}}}
	}
	out := []dataplane.Item{}
	for _, item := range items {
		result, err := n.dispatch(ctx, cred, resource, operation, params, item)
		if err != nil {
			return nil, fmt.Errorf("discord %s/%s: %w", resource, operation, err)
		}
		out = append(out, result...)
	}
	return dataplane.MainOutput(out), nil
}

func (n *Node) dispatch(ctx context.Context, cred Credential, resource string, operation string, params map[string]any, item dataplane.Item) ([]dataplane.Item, error) {
	switch resource {
	case "message":
		return n.handleMessage(ctx, cred, operation, params)
	case "channel":
		return n.handleChannel(ctx, cred, operation, params)
	case "guild":
		return n.handleGuild(ctx, cred, operation, params)
	case "member":
		return n.handleMember(ctx, cred, operation, params)
	case "reaction":
		return n.handleReaction(ctx, cred, operation, params)
	case "webhook":
		return n.handleWebhook(ctx, cred, operation, params)
	case "user":
		return single(n.doJSON(ctx, cred, http.MethodGet, "/users/@me", nil))
	default:
		return nil, fmt.Errorf("unknown resource %s", resource)
	}
}

func single(result map[string]any, err error) ([]dataplane.Item, error) {
	if err != nil {
		return nil, err
	}
	return []dataplane.Item{{JSON: result}}, nil
}

func itemsFromList(list []map[string]any, err error) ([]dataplane.Item, error) {
	if err != nil {
		return nil, err
	}
	items := make([]dataplane.Item, 0, len(list))
	for _, value := range list {
		items = append(items, dataplane.Item{JSON: value})
	}
	return items, nil
}

func firstInput(input dataplane.Output) []dataplane.Item {
	if len(input) == 0 || len(input[0]) == 0 {
		return nil
	}
	return input[0]
}
