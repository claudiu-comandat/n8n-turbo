package trello

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
	return &Node{client: &http.Client{Timeout: 30 * time.Second}, binaryStore: binaryStore, baseURL: "https://api.trello.com/1"}
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
	resource := stringValue(params, "resource")
	if resource == "" {
		resource = "card"
	}
	operation := stringValue(params, "operation")
	if operation == "" {
		operation = "get"
	}
	items := firstInput(in.InputData)
	if len(items) == 0 {
		items = []dataplane.Item{{JSON: map[string]any{}}}
	}
	output := []dataplane.Item{}
	for _, item := range items {
		result, err := node.dispatch(ctx, cred, resource, operation, params, item)
		if err != nil {
			return nil, fmt.Errorf("trello %s/%s: %w", resource, operation, err)
		}
		output = append(output, result...)
	}
	return dataplane.MainOutput(output), nil
}

func (n *Node) dispatch(ctx context.Context, cred Credential, resource string, operation string, params map[string]any, item dataplane.Item) ([]dataplane.Item, error) {
	switch resource {
	case "card", "cards":
		return n.handleCard(ctx, cred, operation, params, item)
	case "board", "boards":
		return n.handleBoard(ctx, cred, operation, params)
	case "list", "lists":
		return n.handleList(ctx, cred, operation, params)
	case "member", "members":
		return n.handleMember(ctx, cred, operation, params)
	case "checklist", "checklists":
		return n.handleChecklist(ctx, cred, operation, params)
	case "label", "labels":
		return n.handleLabel(ctx, cred, operation, params)
	case "attachment", "attachments":
		return n.handleAttachment(ctx, cred, operation, params, item)
	case "comment", "comments":
		return n.handleComment(ctx, cred, operation, params)
	default:
		return nil, fmt.Errorf("unknown resource %s", resource)
	}
}
