package api

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

func (s *Server) launchErrorWorkflow(ctx context.Context, request executionDispatchRequest, failed *engine.Result, executionError *dataplane.ExecutionError) {
	if executionError == nil || s.workflowStore == nil || s.executionStore == nil {
		return
	}
	if !engine.ShouldLaunchErrorWorkflow(engine.NormalizeExecutionMode(request.Mode)) {
		return
	}
	errorWorkflowID := errorWorkflowSetting(request.Workflow)
	if errorWorkflowID == "" || errorWorkflowID == "DEFAULT" || errorWorkflowID == request.Workflow.ID {
		return
	}
	row, err := s.workflowStore.GetByID(ctx, errorWorkflowID)
	if err != nil {
		return
	}
	errorWorkflow, err := workflowFromRow(row)
	if err != nil {
		return
	}
	triggerName := errorTriggerNode(errorWorkflow)
	if triggerName == "" {
		return
	}
	execution, err := s.executionStore.Create(ctx, errorWorkflow, "error")
	if err != nil {
		return
	}
	item := dataplane.Item{JSON: s.errorWorkflowData(request, failed, executionError)}
	initialInputs := map[string]map[int][]dataplane.Item{
		triggerName: map[int][]dataplane.Item{
			0: []dataplane.Item{item},
		},
	}
	errorRequest := executionDispatchRequest{
		ExecutionID: execution.ID,
		Workflow:    errorWorkflow,
		Mode:        "error",
		Options: engine.ExecuteOptions{
			Variables:     request.Options.Variables,
			Secrets:       request.Options.Secrets,
			BinaryStore:   s.binaryStore,
			Credentials:   s.resolveNodeCredentials,
			InitialInputs: initialInputs,
			StartNodes:    []string{triggerName},
			OnStarted:     s.pushExecutionStarted,
			OnNodeAfter:   s.pushNodeAfter,
			OnFinished:    s.pushExecutionFinished,
		},
		StartData: map[string]any{"sourceExecutionId": request.ExecutionID, "sourceWorkflowId": request.Workflow.ID, "errorItem": item.JSON},
		ErrorName: "ErrorWorkflowExecutionError",
	}
	s.executeErrorWorkflowBackground(errorRequest)
}

func (s *Server) executeErrorWorkflowBackground(request executionDispatchRequest) {
	options := request.Options
	if triggerName := errorTriggerNode(request.Workflow); triggerName != "" {
		if errorItem, ok := request.StartData["errorItem"].(map[string]any); ok {
			options.InitialInputs = map[string]map[int][]dataplane.Item{
				triggerName: map[int][]dataplane.Item{
					0: []dataplane.Item{{JSON: errorItem}},
				},
			}
			options.StartNodes = []string{triggerName}
		}
	}
	options.Mode = "error"
	result, runErr := s.evaluator.ExecuteWithOptions(context.Background(), request.Workflow, request.ExecutionID, options)
	status := "success"
	var executionError *dataplane.ExecutionError
	if runErr != nil {
		status = "error"
		errorName := request.ErrorName
		if errorName == "" {
			errorName = "ErrorWorkflowExecutionError"
		}
		executionError = &dataplane.ExecutionError{Name: errorName, Message: runErr.Error(), Timestamp: time.Now().UTC().UnixMilli()}
	}
	data := dataplane.RunExecutionData{
		StartData: request.StartData,
		ResultData: dataplane.ResultData{
			RunData:          resultRunData(result),
			LastNodeExecuted: resultLastNode(result),
			Error:            executionError,
		},
	}
	stoppedAt := time.Now().UTC()
	if result != nil {
		stoppedAt = result.StoppedAt
	}
	finishCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = s.executionStore.Finish(finishCtx, request.ExecutionID, status, stoppedAt, data)
}

func errorWorkflowSetting(workflow dataplane.Workflow) string {
	if workflow.Settings == nil {
		return ""
	}
	for _, key := range []string{"errorWorkflow", "errorWorkflowId"} {
		if value := parameterText(workflow.Settings, key); value != "" {
			return value
		}
	}
	return ""
}

func errorTriggerNode(workflow dataplane.Workflow) string {
	for _, node := range workflow.Nodes {
		if node.Type == "n8n-nodes-base.errorTrigger" && !node.Disabled {
			return node.Name
		}
	}
	return ""
}

func (s *Server) errorWorkflowData(request executionDispatchRequest, failed *engine.Result, executionError *dataplane.ExecutionError) map[string]any {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	lastNode := resultLastNode(failed)
	return map[string]any{
		"$execution": map[string]any{
			"id":             request.ExecutionID,
			"mode":           request.Mode,
			"url":            s.executionURL(request.Workflow.ID, request.ExecutionID),
			"retryOf":        request.StartData["retryOf"],
			"retrySuccessId": request.StartData["retrySuccessId"],
			"customData":     request.StartData["customData"],
		},
		"$workflow": map[string]any{
			"id":     request.Workflow.ID,
			"name":   request.Workflow.Name,
			"active": request.Workflow.Active,
		},
		"$node": map[string]any{
			"name": lastNode,
			"type": nodeTypeByName(request.Workflow, lastNode),
		},
		"error": map[string]any{
			"name":      executionError.Name,
			"message":   executionError.Message,
			"timestamp": executionError.Timestamp,
			"node": map[string]any{
				"name": lastNode,
				"type": nodeTypeByName(request.Workflow, lastNode),
			},
		},
		"timestamp": now,
	}
}

func (s *Server) executionURL(workflowID string, executionID string) string {
	base := strings.TrimRight(firstNonEmpty(s.config.EditorBaseURL, s.resumeBaseURL()), "/")
	if base == "" {
		return fmt.Sprintf("/workflow/%s/executions/%s", workflowID, executionID)
	}
	return fmt.Sprintf("%s/workflow/%s/executions/%s", base, workflowID, executionID)
}

func nodeTypeByName(workflow dataplane.Workflow, name string) string {
	for _, node := range workflow.Nodes {
		if node.Name == name {
			return node.Type
		}
	}
	return ""
}
