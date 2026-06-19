package audit

import "time"

type EventType string

const (
	EventWorkflowCreated     EventType = "n8n.workflow.created"
	EventWorkflowUpdated     EventType = "n8n.workflow.updated"
	EventWorkflowDeleted     EventType = "n8n.workflow.deleted"
	EventWorkflowActivated   EventType = "n8n.workflow.activated"
	EventWorkflowDeactivated EventType = "n8n.workflow.deactivated"
	EventWorkflowExported    EventType = "n8n.workflow.exported"
	EventWorkflowImported    EventType = "n8n.workflow.imported"
	EventCredentialCreated   EventType = "n8n.auth.credential.created"
	EventCredentialUpdated   EventType = "n8n.auth.credential.updated"
	EventCredentialDeleted   EventType = "n8n.auth.credential.deleted"
	EventCredentialShared    EventType = "n8n.auth.credential.shared"
	EventUserCreated         EventType = "n8n.user.created"
	EventUserUpdated         EventType = "n8n.user.updated"
	EventUserDeleted         EventType = "n8n.user.deleted"
	EventUserLoggedIn        EventType = "n8n.user.login.success"
	EventUserLoginFailed     EventType = "n8n.user.login.failed"
	EventUserSignedOut       EventType = "n8n.user.logout"
	EventUserInvited         EventType = "n8n.user.invited"
	EventUserRoleChanged     EventType = "n8n.user.role.updated"
	EventExecutionStarted    EventType = "n8n.workflow.execution.started"
	EventExecutionSuccess    EventType = "n8n.workflow.execution.completed"
	EventExecutionError      EventType = "n8n.workflow.execution.errored"
	EventExecutionCanceled   EventType = "n8n.workflow.execution.canceled"
	EventAPIKeyCreated       EventType = "n8n.api.key.created"
	EventAPIKeyDeleted       EventType = "n8n.api.key.deleted"
	EventVariableCreated     EventType = "n8n.variable.created"
	EventVariableUpdated     EventType = "n8n.variable.updated"
	EventVariableDeleted     EventType = "n8n.variable.deleted"
)

type ResourceType string

const (
	ResourceWorkflow   ResourceType = "workflow"
	ResourceCredential ResourceType = "credential"
	ResourceUser       ResourceType = "user"
	ResourceExecution  ResourceType = "execution"
	ResourceAPIKey     ResourceType = "api-key"
	ResourceVariable   ResourceType = "variable"
	ResourceUnknown    ResourceType = "unknown"
)

type Event struct {
	ID           string         `json:"id"`
	Timestamp    time.Time      `json:"timestamp"`
	EventType    EventType      `json:"eventType"`
	UserID       string         `json:"userId,omitempty"`
	UserEmail    string         `json:"userEmail,omitempty"`
	ResourceType ResourceType   `json:"resourceType,omitempty"`
	ResourceID   string         `json:"resourceId,omitempty"`
	ResourceName string         `json:"resourceName,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	UserAgent    string         `json:"userAgent,omitempty"`
	IP           string         `json:"ip,omitempty"`
}

type Filter struct {
	StartDate    *time.Time
	EndDate      *time.Time
	EventTypes   []EventType
	UserID       string
	ResourceType ResourceType
	ResourceID   string
	Limit        int
	Offset       int
}
