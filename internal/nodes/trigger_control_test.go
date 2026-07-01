package nodes

import (
	"context"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

func TestManualTriggerAlwaysEmitsSingleEmptyItemLikeOfficial(t *testing.T) {
	t.Parallel()

	output, err := (ManualTrigger{}).Execute(context.Background(), engine.ExecuteInput{
		InputData: dataplane.MainOutput([]dataplane.Item{{JSON: map[string]any{"should": "ignore"}}}),
	})
	if err != nil {
		t.Fatalf("manual trigger execute: %v", err)
	}
	if len(output) != 1 || len(output[0]) != 1 || len(output[0][0].JSON) != 0 {
		t.Fatalf("manual trigger should output one empty item, got %#v", output)
	}
}

func TestExecuteWorkflowAddsPairedItemsToSubWorkflowResults(t *testing.T) {
	t.Parallel()

	output, err := (ExecuteWorkflow{}).Execute(context.Background(), engine.ExecuteInput{
		Node: dataplane.Node{Name: "Execute Workflow", Parameters: map[string]any{"operation": "call_workflow", "workflowId": "child"}},
		InputData: dataplane.MainOutput([]dataplane.Item{
			{JSON: map[string]any{"id": 1}},
			{JSON: map[string]any{"id": 2}},
		}),
		SubWorkflow: func(ctx context.Context, req engine.SubWorkflowRequest) (engine.SubWorkflowResult, error) {
			return engine.SubWorkflowResult{Data: dataplane.MainOutput([]dataplane.Item{
				{JSON: map[string]any{"result": 1}},
				{JSON: map[string]any{"result": 2}},
			})}, nil
		},
	})
	if err != nil {
		t.Fatalf("execute workflow: %v", err)
	}
	if output[0][1].PairedItem == nil || output[0][1].PairedItem.Item != 1 {
		t.Fatalf("expected subworkflow result paired to input item 1, got %#v", output[0][1].PairedItem)
	}
}
