package api

import (
	"github.com/n8n-io/n8n-turbo/internal/engine"
	"github.com/n8n-io/n8n-turbo/internal/push"
)

func (s *Server) pushExecutionStarted(event engine.ExecutionStartedEvent) {
	if s.pushHub == nil {
		return
	}
	s.pushHub.Publish(push.ExecutionStarted(event.ExecutionID, event.WorkflowID, event.WorkflowName, event.Mode, event.StartedAt))
}

func (s *Server) pushNodeAfter(event engine.NodeAfterEvent) {
	if s.pushHub == nil {
		return
	}
	s.pushHub.BroadcastToExecution(event.ExecutionID, push.NodeExecuteAfter(event.ExecutionID, event.WorkflowID, event.NodeName, event.Status, event.TaskData))
	s.pushHub.BroadcastToExecution(event.ExecutionID, push.NodeExecuteAfterData(event.ExecutionID, event.WorkflowID, event.NodeName, event.TaskData))
}

func (s *Server) pushNodeAfterToSession(sessionID string, event engine.NodeAfterEvent) {
	if s.pushHub == nil {
		return
	}
	message := push.NodeExecuteAfter(event.ExecutionID, event.WorkflowID, event.NodeName, event.Status, event.TaskData)
	if sessionID == "" {
		s.pushHub.BroadcastToExecution(event.ExecutionID, message)
		s.pushHub.BroadcastToExecution(event.ExecutionID, push.NodeExecuteAfterData(event.ExecutionID, event.WorkflowID, event.NodeName, event.TaskData))
		return
	}
	s.pushHub.BroadcastToSession(sessionID, message)
	s.pushHub.BroadcastToSession(sessionID, push.NodeExecuteAfterData(event.ExecutionID, event.WorkflowID, event.NodeName, event.TaskData))
}

func (s *Server) pushExecutionFinished(event engine.ExecutionFinishedEvent) {
	if s.pushHub == nil {
		return
	}
	s.pushHub.Publish(push.ExecutionFinished(event.ExecutionID, event.WorkflowID, event.Status, event.RunData, event.StoppedAt))
}

func (s *Server) pushExecutionFinishedToSession(sessionID string, event engine.ExecutionFinishedEvent) {
	if s.pushHub == nil {
		return
	}
	message := push.ExecutionFinished(event.ExecutionID, event.WorkflowID, event.Status, event.RunData, event.StoppedAt)
	if sessionID == "" {
		s.pushHub.Publish(message)
		return
	}
	s.pushHub.BroadcastToSession(sessionID, message)
}

func (s *Server) pushWorkflowActivated(workflowID string, workflowName string) {
	if s.pushHub == nil {
		return
	}
	s.pushHub.Publish(push.WorkflowActivated(workflowID, workflowName))
}

func (s *Server) pushWorkflowDeactivated(workflowID string) {
	if s.pushHub == nil {
		return
	}
	s.pushHub.Publish(push.WorkflowDeactivated(workflowID))
}
