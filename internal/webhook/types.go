package webhook

import "time"

type ResponseMode string

const (
	ResponseModeOnReceived   ResponseMode = "onReceived"
	ResponseModeLastNode     ResponseMode = "lastNode"
	ResponseModeResponseNode ResponseMode = "responseNode"
	ResponseModeFormPage     ResponseMode = "formPage"
)

type Request struct {
	Method      string
	Path        string
	Headers     map[string][]string
	Query       map[string][]string
	Body        any
	RawBody     []byte
	ContentType string
	Params      map[string]string
	ClientIP    string
	ReceivedAt  time.Time
}

type Response struct {
	StatusCode  int
	Headers     map[string]string
	Body        any
	RawBody     []byte
	ContentType string
}

type RegisteredWebhook struct {
	WebhookID    string
	WorkflowID   string
	NodeID       string
	NodeName     string
	Path         string
	Method       string
	ResponseMode ResponseMode
	IsTest       bool
	AuthMode     string
	HMACSecret   string
	HMACHeader   string
	HMACAlgo     string
	Options      Options
	CreatedAt    time.Time
}

type Options struct {
	RawBody         bool
	BinaryData      bool
	ResponseHeaders map[string]string
	ResponseCode    int
	AllowedOrigins  string
	NoResponseBody  bool
}

type RouteMatch struct {
	Webhook *RegisteredWebhook
	Params  map[string]string
}

type ExecutionResult struct {
	ExecutionID string
	Data        any
	StatusCode  int
	Headers     map[string]string
	Error       error
}
