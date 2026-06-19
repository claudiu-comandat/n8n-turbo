package shopify

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
	client  *http.Client
	baseURL string
}

func New() *Node {
	return &Node{client: &http.Client{Timeout: 30 * time.Second}}
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
		resource = ResourceOrder
	}
	operation := stringParam(params, "operation")
	if operation == "" {
		operation = OpGetAll
	}
	items := firstInput(in.InputData)
	if len(items) == 0 {
		items = []dataplane.Item{{JSON: map[string]any{}}}
	}
	output := []dataplane.Item{}
	for _, item := range items {
		result, err := n.dispatch(ctx, cred, resource, operation, params, item)
		if err != nil {
			return nil, fmt.Errorf("shopify %s/%s: %w", resource, operation, err)
		}
		output = append(output, result...)
	}
	return dataplane.MainOutput(output), nil
}

func (n *Node) dispatch(ctx context.Context, cred Credential, resource string, operation string, params map[string]any, item dataplane.Item) ([]dataplane.Item, error) {
	switch resource {
	case ResourceOrder, "orders":
		return n.handleOrder(ctx, cred, operation, params, item)
	case ResourceProduct, "products":
		return n.handleProduct(ctx, cred, operation, params, item)
	case ResourceVariant, "variants":
		return n.handleVariant(ctx, cred, operation, params)
	case ResourceCustomer, "customers":
		return n.handleCustomer(ctx, cred, operation, params, item)
	case ResourceInventory:
		return n.handleInventory(ctx, cred, operation, params)
	case ResourceWebhook, "webhooks":
		return n.handleWebhook(ctx, cred, operation, params)
	case "shop":
		return singleValue(n.doJSON(ctx, cred, http.MethodGet, "/shop.json", nil))
	default:
		return nil, fmt.Errorf("unknown resource %s", resource)
	}
}
