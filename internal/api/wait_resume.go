package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
	"github.com/n8n-io/n8n-turbo/internal/flatted"
	"github.com/n8n-io/n8n-turbo/internal/persistence"
)

func (s *Server) resumeDueWaitingExecutions(ctx context.Context, now time.Time) error {
	rows, err := s.executionStore.ListDueWaiting(ctx, now, 50)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := s.resumeWaitingExecution(ctx, row); err != nil {
			log.Printf("resume waiting execution %s: %v", row.ID, err)
		}
	}
	return nil
}

func (s *Server) resumeWaitingExecution(ctx context.Context, row persistence.ExecutionRow) error {
	return s.resumeWaitingExecutionWithOutput(ctx, row, nil)
}

func (s *Server) resumeWaitingExecutionWithOutput(ctx context.Context, row persistence.ExecutionRow, output dataplane.Output) error {
	var workflow dataplane.Workflow
	if err := json.Unmarshal(row.WorkflowData, &workflow); err != nil {
		return fmt.Errorf("decode waiting workflow: %w", err)
	}
	runData, err := runExecutionDataFromStored(row.Data)
	if err != nil {
		return err
	}
	lastNode := runData.ResultData.LastNodeExecuted
	if lastNode == "" {
		return fmt.Errorf("waiting execution has no last node")
	}
	if output != nil {
		replaceLastNodeOutput(runData.ResultData.RunData, lastNode, output)
	}
	initialInputs := resumeInputsAfterNode(workflow, lastNode, runData.ResultData.RunData)
	if len(initialInputs) == 0 {
		return s.executionStore.Finish(ctx, row.ID, "success", time.Now().UTC(), runData)
	}
	variables, err := s.resolvedVariablesContext(ctx)
	if err != nil {
		variables = map[string]any{}
	}
	secrets, err := s.resolvedSecrets(ctx)
	if err != nil {
		secrets = map[string]map[string]string{}
	}
	result := s.dispatchWorkflowSync(ctx, executionDispatchRequest{
		ExecutionID: row.ID,
		Workflow:    workflow,
		Mode:        row.Mode,
		Options: engine.ExecuteOptions{
			Variables:     variables,
			Secrets:       secrets,
			InitialInputs: initialInputs,
			RunData:       runData.ResultData.RunData,
			PinData:       runData.ResultData.PinData,
			BinaryStore:   s.binaryStore,
			Credentials:   s.resolveNodeCredentials,
			OnStarted:     s.pushExecutionStarted,
			OnNodeAfter:   s.pushNodeAfter,
			OnFinished:    s.pushExecutionFinished,
		},
		StartData: runData.StartData,
		PinData:   runData.ResultData.PinData,
		ErrorName: "WaitResumeExecutionError",
	})
	return executionStoreError(result)
}

func replaceLastNodeOutput(runData dataplane.RunData, nodeName string, output dataplane.Output) {
	runs := runData[nodeName]
	if len(runs) == 0 {
		return
	}
	runs[len(runs)-1].Data["main"] = output
	runData[nodeName] = runs
}

func runExecutionDataFromStored(raw json.RawMessage) (dataplane.RunExecutionData, error) {
	var data dataplane.RunExecutionData
	if len(raw) == 0 {
		return data, nil
	}
	if err := flatted.ParseInto(string(raw), &data); err != nil {
		return data, fmt.Errorf("decode waiting execution data: %w", err)
	}
	return data, nil
}

func resumeInputsAfterNode(workflow dataplane.Workflow, nodeName string, runData dataplane.RunData) map[string]map[int][]dataplane.Item {
	graph := dataplane.NewGraph(workflow)
	runs := runData[nodeName]
	if len(runs) == 0 {
		return nil
	}
	output := runs[len(runs)-1].Data["main"]
	if len(output) == 0 {
		return nil
	}
	initialInputs := map[string]map[int][]dataplane.Item{}
	for outputIndex, items := range output {
		for _, edge := range graph.OutputEdges(nodeName, "main", outputIndex) {
			if initialInputs[edge.Node] == nil {
				initialInputs[edge.Node] = map[int][]dataplane.Item{}
			}
			initialInputs[edge.Node][edge.Index] = append(initialInputs[edge.Node][edge.Index], items...)
		}
	}
	return initialInputs
}
