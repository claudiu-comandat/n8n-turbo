package api

import (
	"context"

	"github.com/n8n-io/n8n-turbo/internal/engine"
	"github.com/n8n-io/n8n-turbo/internal/logstream"
)

func (s *Server) lifecycleHooks() *engine.Hooks {
	if s.logStream == nil {
		return nil
	}
	hooks := engine.NewHooks()
	hooks.On(engine.HookWorkflowExecuteBefore, func(ctx context.Context, data any) error {
		event := data.(engine.WorkflowExecuteBeforeData)
		s.logStream.Emit(logstream.EventWorkflowStarted, map[string]any{
			"executionId":  event.ExecutionID,
			"workflowId":   event.WorkflowID,
			"workflowName": event.WorkflowName,
			"mode":         event.Mode,
			"startedAt":    event.StartedAt,
		})
		return nil
	})
	hooks.On(engine.HookWorkflowExecuteAfter, func(ctx context.Context, data any) error {
		event := data.(engine.WorkflowExecuteAfterData)
		eventType := logstream.EventWorkflowSuccess
		if event.Status != "success" {
			eventType = logstream.EventWorkflowFailed
		}
		s.logStream.Emit(eventType, map[string]any{
			"executionId":      event.ExecutionID,
			"workflowId":       event.WorkflowID,
			"workflowName":     event.WorkflowName,
			"mode":             event.Mode,
			"status":           event.Status,
			"startedAt":        event.StartedAt,
			"finishedAt":       event.FinishedAt,
			"durationMs":       event.FinishedAt.Sub(event.StartedAt).Milliseconds(),
			"lastNodeExecuted": resultLastNode(event.Result),
		})
		return nil
	})
	hooks.On(engine.HookNodeExecuteAfter, func(ctx context.Context, data any) error {
		event := data.(engine.NodeExecuteAfterData)
		eventType := logstream.EventNodeSuccess
		if event.Status != "success" {
			eventType = logstream.EventNodeFailed
		}
		s.logStream.Emit(eventType, map[string]any{
			"executionId": event.ExecutionID,
			"workflowId":  event.WorkflowID,
			"nodeName":    event.NodeName,
			"nodeType":    event.NodeType,
			"status":      event.Status,
			"finishedAt":  event.FinishedAt,
		})
		return nil
	})
	return hooks
}
