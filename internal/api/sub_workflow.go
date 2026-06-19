package api

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

func (s *Server) executeSubWorkflow(ctx context.Context, request engine.SubWorkflowRequest) (engine.SubWorkflowResult, error) {
	if s.workflowStore == nil || s.executionStore == nil || s.evaluator == nil {
		return engine.SubWorkflowResult{}, fmt.Errorf("sub-workflow execution is not configured")
	}
	workflowID := strings.TrimSpace(request.WorkflowID)
	if workflowID == "" || workflowID == "<nil>" {
		return engine.SubWorkflowResult{}, fmt.Errorf("sub-workflow id is required")
	}
	if containsWorkflowID(request.CallStack, workflowID) {
		return engine.SubWorkflowResult{}, fmt.Errorf("circular sub-workflow execution detected for workflow %s", workflowID)
	}
	row, err := s.workflowStore.GetByID(ctx, workflowID)
	if err != nil {
		return engine.SubWorkflowResult{}, err
	}
	workflow, err := workflowFromRow(row)
	if err != nil {
		return engine.SubWorkflowResult{}, err
	}
	startNode := request.StartNode
	if startNode == "" {
		startNode = executeWorkflowTriggerNode(workflow)
	}
	if startNode == "" {
		startNode = firstSubWorkflowStartNode(workflow)
	}
	if startNode == "" {
		return engine.SubWorkflowResult{}, fmt.Errorf("sub-workflow %s has no executable start node", workflowID)
	}
	execution, err := s.executionStore.Create(ctx, workflow, "integrated")
	if err != nil {
		return engine.SubWorkflowResult{}, err
	}
	if len(request.Items) == 0 {
		request.Items = []dataplane.Item{{JSON: map[string]any{}}}
	}
	runRequest := executionDispatchRequest{
		ExecutionID: execution.ID,
		Workflow:    workflow,
		Mode:        "integrated",
		Options: engine.ExecuteOptions{
			Variables:     request.Variables,
			Secrets:       request.Secrets,
			BinaryStore:   s.binaryStore,
			Credentials:   s.resolveNodeCredentials,
			InitialInputs: subWorkflowInitialInputs(startNode, request.Items),
			StartNodes:    []string{startNode},
			OnStarted:     s.pushExecutionStarted,
			OnNodeAfter:   s.pushNodeAfter,
			OnFinished:    s.pushExecutionFinished,
			CallStack:     append(request.CallStack, workflowID),
		},
		StartData: map[string]any{
			"parentExecutionId": request.ParentExecutionID,
			"parentWorkflowId":  request.ParentWorkflowID,
			"parentNode":        request.ParentNodeName,
		},
		ErrorName: "SubWorkflowExecutionError",
	}
	if !request.Wait {
		go s.runSubWorkflowDirect(context.Background(), runRequest)
		return engine.SubWorkflowResult{ExecutionID: execution.ID, WorkflowID: workflow.ID, Status: "running", Data: dataplane.MainOutput(request.Items)}, nil
	}
	result := s.runSubWorkflowDirect(ctx, runRequest)
	if result.StartErr != nil {
		return engine.SubWorkflowResult{ExecutionID: execution.ID, WorkflowID: workflow.ID, Status: result.Status}, result.StartErr
	}
	if result.StoreErr != nil {
		return engine.SubWorkflowResult{ExecutionID: execution.ID, WorkflowID: workflow.ID, Status: result.Status}, result.StoreErr
	}
	if result.RunErr != nil {
		return engine.SubWorkflowResult{ExecutionID: execution.ID, WorkflowID: workflow.ID, Status: result.Status}, result.RunErr
	}
	return engine.SubWorkflowResult{ExecutionID: execution.ID, WorkflowID: workflow.ID, Status: result.Status, Data: subWorkflowLastNodeOutput(result.Result)}, nil
}

func (s *Server) runSubWorkflowDirect(ctx context.Context, request executionDispatchRequest) executionDispatchResult {
	if ctx == nil {
		ctx = context.Background()
	}
	options := s.hydrateExecutionOptions(request.Options)
	options.Mode = request.Mode
	result, runErr := s.evaluator.ExecuteWithOptions(ctx, request.Workflow, request.ExecutionID, options)
	status := "success"
	var executionError *dataplane.ExecutionError
	if suspend, ok := engine.AsSuspendError(runErr); ok {
		data := dataplane.RunExecutionData{
			StartData: request.StartData,
			ResultData: dataplane.ResultData{
				RunData:          resultRunData(result),
				LastNodeExecuted: resultLastNode(result),
			},
		}
		waitCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		storeErr := s.executionStore.MarkWaiting(waitCtx, request.ExecutionID, suspend.ResumeAt, data)
		return executionDispatchResult{Result: result, Status: "waiting", StoreErr: storeErr, RunErr: fmt.Errorf("sub-workflow execution is waiting")}
	}
	if runErr != nil {
		status = "error"
		executionError = &dataplane.ExecutionError{Name: firstNonEmpty(request.ErrorName, "SubWorkflowExecutionError"), Message: runErr.Error(), Timestamp: time.Now().UTC().UnixMilli()}
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
	storeErr := s.executionStore.Finish(finishCtx, request.ExecutionID, status, stoppedAt, data)
	return executionDispatchResult{Result: result, Status: status, RunErr: runErr, StoreErr: storeErr}
}

func executeWorkflowTriggerNode(workflow dataplane.Workflow) string {
	for _, node := range workflow.Nodes {
		if node.Type == "n8n-nodes-base.executeWorkflowTrigger" && !node.Disabled {
			return node.Name
		}
	}
	return ""
}

func firstSubWorkflowStartNode(workflow dataplane.Workflow) string {
	for _, node := range dataplane.StartNodes(workflow) {
		if !node.Disabled {
			return node.Name
		}
	}
	return ""
}

func subWorkflowInitialInputs(startNode string, items []dataplane.Item) map[string]map[int][]dataplane.Item {
	return map[string]map[int][]dataplane.Item{
		startNode: map[int][]dataplane.Item{
			0: items,
		},
	}
}

func subWorkflowLastNodeOutput(result *engine.Result) dataplane.Output {
	if result == nil || result.LastNodeExecuted == "" {
		return dataplane.EmptyOutput()
	}
	runs := result.RunData[result.LastNodeExecuted]
	if len(runs) == 0 {
		return dataplane.EmptyOutput()
	}
	output := runs[len(runs)-1].Data["main"]
	if len(output) == 0 {
		return dataplane.EmptyOutput()
	}
	return output
}

func containsWorkflowID(stack []string, workflowID string) bool {
	for _, current := range stack {
		if current == workflowID {
			return true
		}
	}
	return false
}
