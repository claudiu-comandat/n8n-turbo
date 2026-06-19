package push

import (
	"encoding/json"
	"time"
)

type EventType string

const (
	EventExecutionStarted         EventType = "executionStarted"
	EventExecutionFinished        EventType = "executionFinished"
	EventExecutionWaiting         EventType = "executionWaiting"
	EventNodeExecuteBefore        EventType = "nodeExecuteBefore"
	EventNodeExecuteAfter         EventType = "nodeExecuteAfter"
	EventWorkflowActivated        EventType = "workflowActivated"
	EventWorkflowDeactivated      EventType = "workflowDeactivated"
	EventWorkflowFailedToActivate EventType = "workflowFailedToActivate"
	EventTestWebhookReceived      EventType = "testWebhookReceived"
	EventConsoleMessage           EventType = "sendConsoleMessage"
	EventReloadNodeType           EventType = "reloadNodeType"
	EventLogStream                EventType = "logStreamEvent"
)

type Message struct {
	Type EventType `json:"type"`
	Data any       `json:"data"`
}

func ExecutionStarted(executionID string, workflowID string, workflowName string, mode string, startedAt time.Time) Message {
	return Message{
		Type: EventExecutionStarted,
		Data: map[string]any{
			"executionId":  executionID,
			"workflowId":   workflowID,
			"workflowName": workflowName,
			"mode":         mode,
			"startedAt":    startedAt.UTC().Format(time.RFC3339Nano),
		},
	}
}

func NodeExecuteBefore(executionID string, workflowID string, nodeName string) Message {
	return Message{
		Type: EventNodeExecuteBefore,
		Data: map[string]any{
			"executionId": executionID,
			"workflowId":  workflowID,
			"nodeName":    nodeName,
		},
	}
}

func NodeExecuteAfter(executionID string, workflowID string, nodeName string, status string, task any) Message {
	data, _ := json.Marshal(task)
	return Message{
		Type: EventNodeExecuteAfter,
		Data: map[string]any{
			"executionId":     executionID,
			"workflowId":      workflowID,
			"nodeName":        nodeName,
			"executionStatus": status,
			"data":            json.RawMessage(data),
		},
	}
}

func ExecutionFinished(executionID string, workflowID string, status string, runData any, stoppedAt time.Time) Message {
	data, _ := json.Marshal(runData)
	return Message{
		Type: EventExecutionFinished,
		Data: map[string]any{
			"executionId": executionID,
			"workflowId":  workflowID,
			"status":      status,
			"data":        json.RawMessage(data),
			"stoppedAt":   stoppedAt.UTC().Format(time.RFC3339Nano),
		},
	}
}

func ExecutionWaiting(executionID string) Message {
	return Message{
		Type: EventExecutionWaiting,
		Data: map[string]any{"executionId": executionID},
	}
}

func TestWebhookReceived(workflowID string, executionID string, path string) Message {
	return Message{
		Type: EventTestWebhookReceived,
		Data: map[string]any{"workflowId": workflowID, "executionId": executionID, "path": path},
	}
}

func ConsoleMessage(level string, message string, executionID string) Message {
	return Message{
		Type: EventConsoleMessage,
		Data: map[string]any{"level": level, "message": message, "executionId": executionID},
	}
}

func ReloadNodeType(nodeType string) Message {
	return Message{
		Type: EventReloadNodeType,
		Data: map[string]any{"nodeType": nodeType},
	}
}

func WorkflowActivated(workflowID string, workflowName string, activeCount ...int) Message {
	count := 0
	if len(activeCount) > 0 {
		count = activeCount[0]
	}
	return Message{
		Type: EventWorkflowActivated,
		Data: map[string]any{"workflowId": workflowID, "workflowName": workflowName, "activeCount": count},
	}
}

func WorkflowDeactivated(workflowID string) Message {
	return Message{
		Type: EventWorkflowDeactivated,
		Data: map[string]any{"workflowId": workflowID},
	}
}

func WorkflowFailedToActivate(workflowID string, message string) Message {
	return Message{
		Type: EventWorkflowFailedToActivate,
		Data: map[string]any{"workflowId": workflowID, "errorMessage": message},
	}
}
