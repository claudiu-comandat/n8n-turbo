package engine

import (
	"context"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

type testExecutor struct{}

func (testExecutor) Execute(ctx context.Context, in ExecuteInput) (dataplane.Output, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return dataplane.MainOutput([]dataplane.Item{{JSON: map[string]any{"node": in.Node.Name}}}), nil
}

func TestDestinationInclusiveExecutesDestinationAndRecordsRunData(t *testing.T) {
	registry := NewRegistry()
	registry.Register("test.node", testExecutor{})
	workflow := dataplane.Workflow{
		ID:   "wf",
		Name: "Workflow",
		Nodes: []dataplane.Node{
			{Name: "Start", Type: "test.node", Parameters: map[string]any{}},
			{Name: "Next", Type: "test.node", Parameters: map[string]any{}},
		},
		Connections: dataplane.Connections{
			"Start": {"main": [][]dataplane.Connection{{{Node: "Next", Type: "main", Index: 0}}}},
		},
	}

	result, err := NewEvaluator(registry).ExecuteWithOptions(context.Background(), workflow, "exec", ExecuteOptions{
		Destination: &DestinationNode{NodeName: "Start", Mode: DestinationInclusive},
		StartNodes:  []string{"Start"},
	})
	if err != nil {
		t.Fatalf("execute workflow: %v", err)
	}
	if _, ok := result.RunData["Start"]; !ok {
		t.Fatalf("destination node did not record run data: %#v", result.RunData)
	}
	if _, ok := result.RunData["Next"]; ok {
		t.Fatalf("inclusive destination should stop at destination, got downstream run data: %#v", result.RunData)
	}
}
