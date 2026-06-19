package twilio

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

const NodeType = "n8n-nodes-base.twilio"

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
		resource = "sms"
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
		result, err := n.dispatch(ctx, cred, resource, operation, params, item)
		if err != nil {
			return nil, fmt.Errorf("twilio %s/%s: %w", resource, operation, err)
		}
		output = append(output, result...)
	}
	return dataplane.MainOutput(output), nil
}

func (n *Node) dispatch(ctx context.Context, cred Credential, resource string, operation string, params map[string]any, item dataplane.Item) ([]dataplane.Item, error) {
	switch resource {
	case "sms", "message":
		return n.handleMessage(ctx, cred, operation, params)
	case "call":
		return n.handleCall(ctx, cred, operation, params)
	case "lookup":
		return n.handleLookup(ctx, cred, params)
	case "verify":
		return n.handleVerify(ctx, cred, operation, params)
	default:
		return nil, fmt.Errorf("unknown resource %s", resource)
	}
}

func (n *Node) accountURL(cred Credential) string {
	if n.baseURL != "" {
		return n.baseURL
	}
	return cred.BaseURL()
}

func (n *Node) rootURL() string {
	if n.baseURL != "" {
		return rootFromBase(n.baseURL)
	}
	return "https://api.twilio.com"
}

func (n *Node) lookupURL(cred Credential) string {
	if n.baseURL != "" {
		return n.rootURL() + "/v1/PhoneNumbers"
	}
	return cred.LookupURL()
}

func (n *Node) verifyURL(cred Credential, serviceSid string) string {
	if n.baseURL != "" {
		return n.rootURL() + "/v2/Services/" + serviceSid
	}
	return cred.VerifyURL(serviceSid)
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
