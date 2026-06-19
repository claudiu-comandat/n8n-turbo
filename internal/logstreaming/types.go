package logstreaming

import "time"

type EventType string

const (
	EventWorkflowStarted  EventType = "n8n.workflow.started"
	EventWorkflowSuccess  EventType = "n8n.workflow.success"
	EventWorkflowFailed   EventType = "n8n.workflow.failed"
	EventWorkflowCanceled EventType = "n8n.workflow.canceled"
	EventNodeStarted      EventType = "n8n.node.started"
	EventNodeSuccess      EventType = "n8n.node.success"
	EventNodeFailed       EventType = "n8n.node.failed"
	EventDestinationError EventType = "n8n.destination.response.error"
	EventAITokensUsed     EventType = "n8n.ai.tokens.used"
)

type StreamEvent struct {
	ID        string         `json:"eventName"`
	Timestamp time.Time      `json:"ts"`
	Payload   map[string]any `json:"payload"`
}

type DestinationType string

const (
	DestinationWebhook DestinationType = "webhook"
	DestinationSyslog  DestinationType = "syslog"
	DestinationSentry  DestinationType = "sentry"
	DestinationDatadog DestinationType = "datadog"
	DestinationSplunk  DestinationType = "splunk"
)

type DestinationConfig struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Type              DestinationType   `json:"type"`
	Enabled           bool              `json:"enabled"`
	Events            []EventType       `json:"events,omitempty"`
	WebhookURL        string            `json:"webhookUrl,omitempty"`
	WebhookHeaders    map[string]string `json:"webhookHeaders,omitempty"`
	SyslogHost        string            `json:"syslogHost,omitempty"`
	SyslogPort        int               `json:"syslogPort,omitempty"`
	SyslogProtocol    string            `json:"syslogProtocol,omitempty"`
	SyslogFacility    int               `json:"syslogFacility,omitempty"`
	SentryDSN         string            `json:"sentryDsn,omitempty"`
	SentryEnvironment string            `json:"sentryEnvironment,omitempty"`
}

type WorkflowStartedPayload struct {
	ExecutionID  string `json:"executionId"`
	WorkflowID   string `json:"workflowId"`
	WorkflowName string `json:"workflowName"`
	Mode         string `json:"mode"`
	StartedAt    string `json:"startedAt"`
}

type WorkflowFinishedPayload struct {
	ExecutionID   string  `json:"executionId"`
	WorkflowID    string  `json:"workflowId"`
	WorkflowName  string  `json:"workflowName"`
	Status        string  `json:"status"`
	ExecutionTime float64 `json:"executionTime"`
	Error         string  `json:"error,omitempty"`
	ErrorNodeType string  `json:"errorNodeType,omitempty"`
	StartedAt     string  `json:"startedAt"`
	FinishedAt    string  `json:"finishedAt"`
	DataSent      bool    `json:"dataSent"`
	RetryOf       string  `json:"retryOf,omitempty"`
}

type NodeFinishedPayload struct {
	ExecutionID   string  `json:"executionId"`
	WorkflowID    string  `json:"workflowId"`
	WorkflowName  string  `json:"workflowName"`
	NodeID        string  `json:"nodeId"`
	NodeName      string  `json:"nodeName"`
	NodeType      string  `json:"nodeType"`
	Status        string  `json:"status"`
	ExecutionTime float64 `json:"executionTime"`
	ErrorMessage  string  `json:"errorMessage,omitempty"`
	ItemsCount    int     `json:"itemsCount"`
}
