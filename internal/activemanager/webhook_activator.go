package activemanager

import (
	"context"
	"fmt"
	"strings"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/webhook"
)

type WebhookActivator struct {
	store webhook.Store
}

func NewWebhookActivator(store webhook.Store) *WebhookActivator {
	return &WebhookActivator{store: store}
}

func (a *WebhookActivator) ActivateWorkflow(ctx context.Context, workflow dataplane.Workflow) ([]string, error) {
	if a == nil || a.store == nil {
		return nil, nil
	}
	if err := a.store.DeleteByWorkflow(ctx, workflow.ID); err != nil {
		return nil, err
	}
	ids := []string{}
	for _, node := range workflow.Nodes {
		if node.Disabled || !isWebhookNode(node) {
			continue
		}
		registered := webhook.RegisteredWebhook{
			WebhookID:    firstNonEmptyWebhook(node.WebhookID, node.ID, workflow.ID+"-"+node.Name),
			WorkflowID:   workflow.ID,
			NodeID:       firstNonEmptyWebhook(node.ID, node.Name),
			NodeName:     node.Name,
			Path:         webhookNodePath(node),
			Method:       webhookNodeMethod(node),
			ResponseMode: webhook.ResponseMode(firstNonEmptyWebhook(parameterText(node.Parameters, "responseMode"), "onReceived")),
			AuthMode:     firstNonEmptyWebhook(parameterText(node.Parameters, "webhookAuthentication"), parameterText(node.Parameters, "authentication"), "none"),
			IsTest:       false,
			Options: webhook.Options{
				RawBody:         parameterBool(node.Parameters, "rawBody", parameterBool(optionsMap(node.Parameters), "rawBody", false)),
				BinaryData:      parameterBool(node.Parameters, "binaryData", parameterBool(optionsMap(node.Parameters), "binaryData", false)),
				ResponseHeaders: responseHeaders(optionsMap(node.Parameters)),
				ResponseCode:    parameterInt(optionsMap(node.Parameters), "responseCode", parameterInt(node.Parameters, "responseCode", 0)),
				AllowedOrigins:  firstNonEmptyWebhook(parameterText(optionsMap(node.Parameters), "allowedOrigins"), parameterText(node.Parameters, "allowedOrigins")),
				NoResponseBody:  parameterBool(optionsMap(node.Parameters), "noResponseBody", parameterBool(node.Parameters, "noResponseBody", false)),
			},
			HMACSecret: parameterText(node.Parameters, "hmacSecret"),
			HMACHeader: firstNonEmptyWebhook(parameterText(node.Parameters, "hmacHeader"), "X-Hub-Signature-256"),
			HMACAlgo:   firstNonEmptyWebhook(parameterText(node.Parameters, "hmacAlgo"), "sha256"),
		}
		if registered.Path == "" {
			return nil, fmt.Errorf("workflow %s webhook node %s has empty path", workflow.ID, node.Name)
		}
		if err := a.store.Save(ctx, registered); err != nil {
			return nil, err
		}
		ids = append(ids, registered.WebhookID)
	}
	return ids, nil
}

func (a *WebhookActivator) DeactivateWorkflow(ctx context.Context, workflowID string) error {
	if a == nil || a.store == nil {
		return nil
	}
	return a.store.DeleteByWorkflow(ctx, workflowID)
}

func isWebhookNode(node dataplane.Node) bool {
	switch node.Type {
	case "n8n-nodes-base.webhook", "n8n-nodes-base.formTrigger":
		return true
	default:
		return false
	}
}

func webhookNodePath(node dataplane.Node) string {
	for _, key := range []string{"path", "webhookPath"} {
		if value := parameterText(node.Parameters, key); value != "" {
			return strings.Trim(value, "/")
		}
	}
	return strings.Trim(firstNonEmptyWebhook(node.WebhookID, node.Name), "/")
}

func webhookNodeMethod(node dataplane.Node) string {
	return strings.ToUpper(firstNonEmptyWebhook(parameterText(node.Parameters, "httpMethod"), parameterText(node.Parameters, "method"), "ALL"))
}

func optionsMap(params map[string]any) map[string]any {
	value, ok := params["options"].(map[string]any)
	if ok {
		return value
	}
	return map[string]any{}
}

func responseHeaders(params map[string]any) map[string]string {
	result := map[string]string{}
	value, ok := params["responseHeaders"].(map[string]any)
	if !ok {
		return result
	}
	for key, raw := range value {
		result[key] = fmt.Sprint(raw)
	}
	return result
}

func parameterText(params map[string]any, key string) string {
	if params == nil {
		return ""
	}
	value, ok := params[key]
	if !ok || value == nil {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "<nil>" {
		return ""
	}
	return text
}

func parameterBool(params map[string]any, key string, fallback bool) bool {
	text := strings.ToLower(parameterText(params, key))
	switch text {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	default:
		return fallback
	}
}

func parameterInt(params map[string]any, key string, fallback int) int {
	switch value := params[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	default:
		return fallback
	}
}

func firstNonEmptyWebhook(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
