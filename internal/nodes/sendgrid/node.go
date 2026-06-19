package sendgrid

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/binarydata"
	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type Node struct {
	client      *http.Client
	binaryStore binarydata.Store
	baseURL     string
}

func New(binaryStore binarydata.Store) *Node {
	return &Node{client: &http.Client{Timeout: 30 * time.Second}, binaryStore: binaryStore, baseURL: "https://api.sendgrid.com/v3"}
}

func NewWithBaseURL(binaryStore binarydata.Store, baseURL string) *Node {
	node := New(binaryStore)
	node.baseURL = strings.TrimRight(baseURL, "/")
	return node
}

func (n *Node) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	cred, err := extractCredential(in.Credentials)
	if err != nil {
		return nil, err
	}
	node := *n
	if node.binaryStore == nil {
		node.binaryStore = in.BinaryStore
	}
	params := in.Node.Parameters
	resource := stringParam(params, "resource")
	if resource == "" {
		resource = "email"
	}
	operation := stringParam(params, "operation")
	if operation == "" {
		operation = "send"
	}
	items := firstInput(in.InputData)
	if len(items) == 0 {
		items = []dataplane.Item{{JSON: map[string]any{}}}
	}
	output := []dataplane.Item{}
	for _, item := range items {
		result, err := node.dispatch(ctx, cred, resource, operation, params, item)
		if err != nil {
			return nil, fmt.Errorf("sendgrid %s/%s: %w", resource, operation, err)
		}
		output = append(output, result...)
	}
	return dataplane.MainOutput(output), nil
}

func (n *Node) dispatch(ctx context.Context, cred Credential, resource string, operation string, params map[string]any, item dataplane.Item) ([]dataplane.Item, error) {
	switch resource {
	case "email", "mail":
		return single(n.handleEmail(ctx, cred, operation, params, item))
	case "contact", "contacts":
		return n.handleContact(ctx, cred, operation, params)
	case "list", "lists":
		return n.handleList(ctx, cred, operation, params)
	case "suppression", "suppressions", "unsubscribe":
		return n.handleSuppression(ctx, cred, operation, params)
	case "account", "user":
		return single(n.doJSON(ctx, cred, http.MethodGet, "/user/account", nil))
	default:
		return nil, fmt.Errorf("unknown resource %s", resource)
	}
}

func firstInput(input dataplane.Output) []dataplane.Item {
	if len(input) == 0 || len(input[0]) == 0 {
		return nil
	}
	return input[0]
}

func single(result map[string]any, err error) ([]dataplane.Item, error) {
	if err != nil {
		return nil, err
	}
	return []dataplane.Item{{JSON: result}}, nil
}

func itemsFromMaps(values []map[string]any, err error) ([]dataplane.Item, error) {
	if err != nil {
		return nil, err
	}
	items := make([]dataplane.Item, 0, len(values))
	for _, value := range values {
		items = append(items, dataplane.Item{JSON: value})
	}
	return items, nil
}
