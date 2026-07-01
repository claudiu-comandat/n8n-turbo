package nodes

import (
	"context"
	"fmt"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type ExecuteWorkflow struct{}

func (ExecuteWorkflow) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	if operation := firstNonEmptyNode(stringParam(in.Node.Parameters, "operation"), "call_workflow"); operation != "call_workflow" {
		return nil, fmt.Errorf("executeWorkflow: unsupported operation %s", operation)
	}
	if in.SubWorkflow == nil {
		return nil, fmt.Errorf("sub-workflow execution is not available")
	}
	items := firstInput(in.InputData)
	if len(items) == 0 {
		items = []dataplane.Item{{JSON: map[string]any{}}}
	}
	mode := stringParam(in.Node.Parameters, "mode")
	if mode == "each" {
		return executeWorkflowEach(ctx, in, items)
	}
	return executeWorkflowOnce(ctx, in, items, 0)
}

func executeWorkflowOnce(ctx context.Context, in engine.ExecuteInput, items []dataplane.Item, itemIndex int) (dataplane.Output, error) {
	workflowID := resolveWorkflowID(in, items, itemIndex)
	wait := waitForSubWorkflow(in.Node.Parameters)
	result, err := in.SubWorkflow(ctx, engine.SubWorkflowRequest{
		WorkflowID:        workflowID,
		Items:             cloneItems(items),
		Wait:              wait,
		StartNode:         stringParam(in.Node.Parameters, "startNode"),
		ParentExecutionID: in.ExecutionID,
		ParentWorkflowID:  in.WorkflowID,
		ParentNodeName:    in.Node.Name,
		Variables:         in.Variables,
		Secrets:           in.Secrets,
		CallStack:         appendCallStack(in.CallStack, in.WorkflowID),
	})
	if err != nil {
		return nil, err
	}
	if !wait {
		return dataplane.MainOutput(cloneItems(items)), nil
	}
	if len(result.Data) == 0 {
		return dataplane.EmptyOutput(), nil
	}
	applyExecuteWorkflowPairedItems(result.Data, len(items), itemIndex)
	return result.Data, nil
}

func executeWorkflowEach(ctx context.Context, in engine.ExecuteInput, items []dataplane.Item) (dataplane.Output, error) {
	wait := waitForSubWorkflow(in.Node.Parameters)
	combined := dataplane.Output{}
	for index, item := range items {
		output, err := executeWorkflowOnce(ctx, in, []dataplane.Item{item}, index)
		if err != nil {
			return nil, fmt.Errorf("error executing workflow with item at index %d: %w", index, err)
		}
		if !wait {
			output = dataplane.MainOutput([]dataplane.Item{cloneItem(item)})
		}
		for outputIndex, outputItems := range output {
			for len(combined) <= outputIndex {
				combined = append(combined, nil)
			}
			combined[outputIndex] = append(combined[outputIndex], outputItems...)
		}
	}
	if len(combined) == 0 {
		return dataplane.EmptyOutput(), nil
	}
	return combined, nil
}

func resolveWorkflowID(in engine.ExecuteInput, items []dataplane.Item, itemIndex int) string {
	params := in.Node.Parameters
	source := stringParam(params, "source")
	if source == "currentWorkflow" {
		return in.WorkflowID
	}
	value := params["workflowId"]
	if value == nil {
		value = params["workflowID"]
	}
	if value == nil {
		value = params["workflow"]
	}
	if object, ok := rawObject(value); ok {
		if rawValue, ok := object["value"]; ok {
			value = rawValue
		}
	}
	resolved := resolveValue(in, items, itemIndex, value)
	if object, ok := rawObject(resolved); ok {
		if rawValue, ok := object["value"]; ok {
			return fmt.Sprint(rawValue)
		}
	}
	return fmt.Sprint(resolved)
}

func waitForSubWorkflow(params map[string]any) bool {
	if object, ok := rawObject(params["options"]); ok {
		return boolParam(object, "waitForSubWorkflow", true)
	}
	return boolParam(params, "waitForSubWorkflow", true)
}

func appendCallStack(stack []string, workflowID string) []string {
	result := make([]string, 0, len(stack)+1)
	result = append(result, stack...)
	if workflowID != "" {
		result = append(result, workflowID)
	}
	return result
}

func applyExecuteWorkflowPairedItems(output dataplane.Output, inputLength int, fallbackIndex int) {
	for outputIndex := range output {
		sameLength := len(output[outputIndex]) == inputLength
		for itemIndex := range output[outputIndex] {
			if output[outputIndex][itemIndex].PairedItem != nil {
				continue
			}
			sourceIndex := fallbackIndex
			if sameLength {
				sourceIndex = itemIndex
			}
			output[outputIndex][itemIndex].PairedItem = &dataplane.PairedItem{Item: sourceIndex}
		}
	}
}

func cloneItems(items []dataplane.Item) []dataplane.Item {
	result := make([]dataplane.Item, 0, len(items))
	for _, item := range items {
		result = append(result, cloneItem(item))
	}
	return result
}
