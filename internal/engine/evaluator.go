package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/binarydata"
	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/expr"
)

type Evaluator struct {
	registry Registry
	resolver *expr.Resolver
}

type Result struct {
	RunData          dataplane.RunData
	LastNodeExecuted string
	StartedAt        time.Time
	StoppedAt        time.Time
}

type DestinationMode string

const (
	DestinationInclusive DestinationMode = "inclusive"
	DestinationExclusive DestinationMode = "exclusive"
)

type DestinationNode struct {
	NodeName string
	Mode     DestinationMode
}

type ExecuteOptions struct {
	Destination   *DestinationNode
	Variables     map[string]any
	Secrets       map[string]map[string]string
	BinaryStore   binarydata.Store
	Credentials   func(context.Context, dataplane.Node) (map[string]map[string]any, error)
	TriggerNode   string
	TriggerItems  []dataplane.Item
	InitialInputs map[string]map[int][]dataplane.Item
	StartNodes    []string
	RunData       dataplane.RunData
	PinData       map[string][]dataplane.Item
	Mode          string
	ScheduledTime time.Time
	ResumeURL     string
	ResumeFormURL string
	ResumeToken   string
	OnStarted     func(ExecutionStartedEvent)
	OnNodeAfter   func(NodeAfterEvent)
	OnFinished    func(ExecutionFinishedEvent)
	SubWorkflow   SubWorkflowExecutor
	CallStack     []string
	Hooks         *Hooks
}

type ExecutionStartedEvent struct {
	ExecutionID  string
	WorkflowID   string
	WorkflowName string
	Mode         string
	StartedAt    time.Time
}

type NodeAfterEvent struct {
	ExecutionID string
	WorkflowID  string
	NodeName    string
	NodeType    string
	TaskData    dataplane.TaskData
	Status      string
}

type ExecutionFinishedEvent struct {
	ExecutionID string
	WorkflowID  string
	Status      string
	RunData     dataplane.RunData
	StartedAt   time.Time
	StoppedAt   time.Time
}

func NewEvaluator(registry Registry) *Evaluator {
	return &Evaluator{registry: registry, resolver: expr.NewResolver(5 * time.Second)}
}

func (e *Evaluator) Execute(ctx context.Context, workflow dataplane.Workflow, executionID string) (*Result, error) {
	return e.ExecuteWithOptions(ctx, workflow, executionID, ExecuteOptions{})
}

func (e *Evaluator) ExecuteWithOptions(ctx context.Context, workflow dataplane.Workflow, executionID string, options ExecuteOptions) (*Result, error) {
	startedAt := time.Now().UTC()
	mode := options.Mode
	if mode == "" {
		mode = "manual"
	}
	options.Mode = mode
	emitStarted(ctx, options, workflow, executionID, mode, startedAt)
	runData := cloneRunData(options.RunData, len(workflow.Nodes))
	graph := dataplane.NewGraph(workflow)
	allowed, err := allowedNodes(graph, options.Destination)
	if err != nil {
		return nil, err
	}
	inputs := make(map[string]map[int][]dataplane.Item, len(workflow.Nodes))
	for nodeName, nodeInputs := range options.InitialInputs {
		inputs[nodeName] = cloneNodeInputs(nodeInputs)
	}
	if options.TriggerNode != "" {
		inputs[options.TriggerNode] = map[int][]dataplane.Item{0: options.TriggerItems}
	}
	queue := startNodes(workflow, allowed, options)
	queued := make(map[string]bool, len(queue))
	for _, node := range queue {
		queued[node.Name] = true
	}

	lastNode := ""
	for len(queue) > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		batch := queue
		queue = nil
		results := e.executeReadyBatch(ctx, workflow, graph, executionID, mode, batch, inputs, runData, options)
		var stopErr error
		for _, nodeResult := range results {
			delete(queued, nodeResult.Node.Name)
			lastNode = nodeResult.Node.Name
			task := compactTaskData(ctx, taskData(nodeResult.StartedAt, nodeResult.Output, nodeResult.Err), options.BinaryStore)
			task.ExecutionIndex = len(runData[nodeResult.Node.Name])
			runData[nodeResult.Node.Name] = append(runData[nodeResult.Node.Name], task)
			emitNodeAfter(ctx, options, workflow, executionID, nodeResult.Node, task, nodeResult.Err)
			if _, ok := AsSuspendError(nodeResult.Err); ok {
				result := &Result{RunData: runData, LastNodeExecuted: lastNode, StartedAt: startedAt, StoppedAt: time.Now().UTC()}
				emitFinished(ctx, options, workflow, executionID, "waiting", result)
				return result, nodeResult.Err
			}
			if nodeResult.Err != nil && (isContextError(nodeResult.Err) || nodeResult.Node.EffectiveOnError() == dataplane.OnErrorStopWorkflow) {
				if stopErr == nil {
					stopErr = nodeResult.Err
				}
				continue
			}
			if stopErr != nil {
				continue
			}
			for _, next := range e.deliverOutputs(graph, nodeResult.Node.Name, nodeResult.Output, inputs, allowed, options.Destination) {
				if queued[next.Name] {
					continue
				}
				queue = append(queue, next)
				queued[next.Name] = true
			}
		}
		if stopErr != nil {
			result := &Result{RunData: runData, LastNodeExecuted: lastNode, StartedAt: startedAt, StoppedAt: time.Now().UTC()}
			emitFinished(ctx, options, workflow, executionID, "error", result)
			return result, stopErr
		}
	}

	result := &Result{RunData: runData, LastNodeExecuted: lastNode, StartedAt: startedAt, StoppedAt: time.Now().UTC()}
	emitFinished(ctx, options, workflow, executionID, "success", result)
	return result, nil
}

func (e *Evaluator) executeNode(ctx context.Context, workflow dataplane.Workflow, graph *dataplane.Graph, executionID string, mode string, node dataplane.Node, nodeInputs map[int][]dataplane.Item, runData dataplane.RunData, options ExecuteOptions) (dataplane.Output, error) {
	executor, ok := e.registry.Executor(node.Type)
	if !ok {
		return nil, fmt.Errorf("node type %s is not registered", node.Type)
	}
	credentials := map[string]map[string]any{}
	if options.Credentials != nil {
		resolved, err := options.Credentials(ctx, node)
		if err != nil {
			return nil, err
		}
		credentials = resolved
	}
	inputData := flattenInputs(nodeInputs)
	input := ExecuteInput{
		Node:          node,
		NextNodes:     downstreamNodes(graph, node.Name),
		InputData:     inputData,
		RunData:       runData,
		Variables:     options.Variables,
		Secrets:       options.Secrets,
		Credentials:   credentials,
		BinaryStore:   options.BinaryStore,
		WorkflowID:    workflow.ID,
		WorkflowName:  workflow.Name,
		ExecutionID:   executionID,
		ExecutionMode: mode,
		ResumeURL:     options.ResumeURL,
		ResumeFormURL: options.ResumeFormURL,
		ScheduledTime: options.ScheduledTime,
		Expr:          e.resolver,
		SubWorkflow:   options.SubWorkflow,
		CallStack:     options.CallStack,
	}
	emitNodeBefore(ctx, options, workflow, executionID, node, input.InputData)
	output, err := executeNodeWithRetry(ctx, executor, input)
	if err != nil && !isContextError(err) && node.EffectiveOnError() != dataplane.OnErrorStopWorkflow {
		return buildErrorOutput(node, input.InputData, err), err
	}
	return output, err
}

type readyResult struct {
	Node      dataplane.Node
	StartedAt time.Time
	Output    dataplane.Output
	Err       error
}

func (e *Evaluator) executeReadyBatch(ctx context.Context, workflow dataplane.Workflow, graph *dataplane.Graph, executionID string, mode string, batch []dataplane.Node, inputs map[string]map[int][]dataplane.Item, runData dataplane.RunData, options ExecuteOptions) []readyResult {
	results := make([]readyResult, len(batch))
	if len(batch) == 1 {
		node := batch[0]
		startedAt := time.Now().UTC()
		output, err := e.executeReadyNode(ctx, workflow, graph, executionID, mode, node, inputs[node.Name], runData, options)
		results[0] = readyResult{Node: node, StartedAt: startedAt, Output: output, Err: err}
		return results
	}
	batchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(len(batch))
	for index, node := range batch {
		index := index
		node := node
		go func() {
			defer wg.Done()
			startedAt := time.Now().UTC()
			output, err := e.executeReadyNode(batchCtx, workflow, graph, executionID, mode, node, inputs[node.Name], runData, options)
			results[index] = readyResult{Node: node, StartedAt: startedAt, Output: output, Err: err}
			if err != nil && (isContextError(err) || node.EffectiveOnError() == dataplane.OnErrorStopWorkflow) {
				cancel()
			}
		}()
	}
	wg.Wait()
	return results
}

func (e *Evaluator) executeReadyNode(ctx context.Context, workflow dataplane.Workflow, graph *dataplane.Graph, executionID string, mode string, node dataplane.Node, nodeInputs map[int][]dataplane.Item, runData dataplane.RunData, options ExecuteOptions) (dataplane.Output, error) {
	if output, pinned := pinnedOutput(options.PinData, node.Name); pinned {
		return output, nil
	}
	return e.executeNode(ctx, workflow, graph, executionID, mode, node, nodeInputs, runData, options)
}

func downstreamNodes(graph *dataplane.Graph, source string) []dataplane.Node {
	if graph == nil {
		return nil
	}
	names := graph.Children(source, "main")
	if len(names) == 0 {
		return nil
	}
	result := make([]dataplane.Node, 0, len(names))
	for _, name := range names {
		node, ok := graph.Node(name)
		if !ok || node == nil {
			continue
		}
		result = append(result, *node)
	}
	return result
}

func emitStarted(ctx context.Context, options ExecuteOptions, workflow dataplane.Workflow, executionID string, mode string, startedAt time.Time) {
	if options.Hooks != nil {
		options.Hooks.Emit(ctx, HookWorkflowExecuteBefore, WorkflowExecuteBeforeData{
			ExecutionID:  executionID,
			WorkflowID:   workflow.ID,
			WorkflowName: workflow.Name,
			Mode:         mode,
			StartedAt:    startedAt,
		})
	}
	if options.OnStarted == nil {
		return
	}
	options.OnStarted(ExecutionStartedEvent{
		ExecutionID:  executionID,
		WorkflowID:   workflow.ID,
		WorkflowName: workflow.Name,
		Mode:         mode,
		StartedAt:    startedAt,
	})
}

func emitNodeBefore(ctx context.Context, options ExecuteOptions, workflow dataplane.Workflow, executionID string, node dataplane.Node, input dataplane.Output) {
	if options.Hooks == nil {
		return
	}
	options.Hooks.Emit(ctx, HookNodeExecuteBefore, NodeExecuteBeforeData{
		ExecutionID: executionID,
		WorkflowID:  workflow.ID,
		NodeName:    node.Name,
		NodeType:    node.Type,
		InputData:   input,
		StartedAt:   time.Now().UTC(),
	})
}

func emitNodeAfter(ctx context.Context, options ExecuteOptions, workflow dataplane.Workflow, executionID string, node dataplane.Node, task dataplane.TaskData, err error) {
	status := executionStatusFromError(err)
	if options.Hooks != nil {
		options.Hooks.Emit(ctx, HookNodeExecuteAfter, NodeExecuteAfterData{
			ExecutionID: executionID,
			WorkflowID:  workflow.ID,
			NodeName:    node.Name,
			NodeType:    node.Type,
			Status:      status,
			TaskData:    task,
			Error:       err,
			FinishedAt:  time.Now().UTC(),
		})
	}
	if options.OnNodeAfter == nil {
		return
	}
	options.OnNodeAfter(NodeAfterEvent{
		ExecutionID: executionID,
		WorkflowID:  workflow.ID,
		NodeName:    node.Name,
		NodeType:    node.Type,
		TaskData:    task,
		Status:      status,
	})
}

func emitFinished(ctx context.Context, options ExecuteOptions, workflow dataplane.Workflow, executionID string, status string, result *Result) {
	if options.Hooks != nil && result != nil {
		options.Hooks.Emit(ctx, HookWorkflowExecuteAfter, WorkflowExecuteAfterData{
			ExecutionID:  executionID,
			WorkflowID:   workflow.ID,
			WorkflowName: workflow.Name,
			Mode:         options.Mode,
			Status:       status,
			Result:       result,
			StartedAt:    result.StartedAt,
			FinishedAt:   result.StoppedAt,
		})
	}
	if options.OnFinished == nil || result == nil {
		return
	}
	options.OnFinished(ExecutionFinishedEvent{
		ExecutionID: executionID,
		WorkflowID:  workflow.ID,
		Status:      status,
		RunData:     result.RunData,
		StartedAt:   result.StartedAt,
		StoppedAt:   result.StoppedAt,
	})
}

func flattenInputs(inputs map[int][]dataplane.Item) dataplane.Output {
	if len(inputs) == 0 {
		return dataplane.EmptyOutput()
	}
	maxIndex := -1
	for index := range inputs {
		if index > maxIndex {
			maxIndex = index
		}
	}
	output := make(dataplane.Output, maxIndex+1)
	for index, items := range inputs {
		output[index] = items
	}
	return output
}

func taskData(startedAt time.Time, output dataplane.Output, err error) dataplane.TaskData {
	now := time.Now().UTC()
	task := dataplane.TaskData{
		StartTime:       startedAt.UnixMilli(),
		ExecutionTime:   now.Sub(startedAt).Milliseconds(),
		ExecutionStatus: executionStatusFromError(err),
		Source:          []any{},
		Data:            dataplane.NodeExecutionData{"main": output},
	}
	if err != nil {
		task.Error = &dataplane.ExecutionError{Name: "NodeExecutionError", Message: err.Error(), Timestamp: now.UnixMilli()}
	}
	return task
}

func executionStatusFromError(err error) string {
	if err == nil {
		return "success"
	}
	if _, ok := AsSuspendError(err); ok {
		return "waiting"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	return "error"
}

func allowedNodes(graph *dataplane.Graph, destination *DestinationNode) (map[string]bool, error) {
	if destination == nil {
		return nil, nil
	}
	if destination.Mode == "" {
		destination.Mode = DestinationInclusive
	}
	if destination.Mode != DestinationInclusive && destination.Mode != DestinationExclusive {
		return nil, fmt.Errorf("unsupported destination mode %q", destination.Mode)
	}
	if _, ok := graph.Node(destination.NodeName); !ok {
		return nil, fmt.Errorf("destination node %q not found", destination.NodeName)
	}
	return graph.Ancestors(destination.NodeName), nil
}

func startNodes(workflow dataplane.Workflow, allowed map[string]bool, options ExecuteOptions) []dataplane.Node {
	if options.TriggerNode != "" {
		node, ok := dataplane.NodeByName(workflow, options.TriggerNode)
		if ok && !node.Disabled {
			return []dataplane.Node{node}
		}
		return nil
	}
	if len(options.StartNodes) > 0 {
		nodes := make([]dataplane.Node, 0, len(options.StartNodes))
		for _, name := range options.StartNodes {
			node, ok := dataplane.NodeByName(workflow, name)
			if !ok || node.Disabled || !canExecuteNode(node.Name, allowed, options.Destination) {
				continue
			}
			nodes = append(nodes, node)
		}
		return nodes
	}
	if len(options.InitialInputs) > 0 {
		nodes := make([]dataplane.Node, 0, len(options.InitialInputs))
		for _, node := range workflow.Nodes {
			if node.Disabled || !canExecuteNode(node.Name, allowed, options.Destination) {
				continue
			}
			if _, ok := options.InitialInputs[node.Name]; ok {
				nodes = append(nodes, node)
			}
		}
		return nodes
	}
	nodes := dataplane.StartNodes(workflow)
	if allowed == nil && options.Destination == nil {
		return nodes
	}
	filtered := make([]dataplane.Node, 0, len(nodes))
	for _, node := range nodes {
		if !canExecuteNode(node.Name, allowed, options.Destination) {
			continue
		}
		filtered = append(filtered, node)
	}
	return filtered
}

func cloneNodeInputs(input map[int][]dataplane.Item) map[int][]dataplane.Item {
	result := make(map[int][]dataplane.Item, len(input))
	for index, items := range input {
		copied := make([]dataplane.Item, len(items))
		copy(copied, items)
		result[index] = copied
	}
	return result
}

func pinnedOutput(pinData map[string][]dataplane.Item, nodeName string) (dataplane.Output, bool) {
	if len(pinData) == 0 {
		return nil, false
	}
	items, ok := pinData[nodeName]
	if !ok {
		return nil, false
	}
	return dataplane.MainOutput(items), true
}

func cloneRunData(input dataplane.RunData, size int) dataplane.RunData {
	result := make(dataplane.RunData, size+len(input))
	for node, tasks := range input {
		copied := make([]dataplane.TaskData, len(tasks))
		copy(copied, tasks)
		result[node] = copied
	}
	return result
}

func canExecuteNode(name string, allowed map[string]bool, destination *DestinationNode) bool {
	if allowed != nil && !allowed[name] {
		return false
	}
	return destination == nil || destination.Mode != DestinationExclusive || name != destination.NodeName
}

func (e *Evaluator) deliverOutputs(graph *dataplane.Graph, source string, output dataplane.Output, inputs map[string]map[int][]dataplane.Item, allowed map[string]bool, destination *DestinationNode) []dataplane.Node {
	if destination != nil && destination.Mode == DestinationInclusive && source == destination.NodeName {
		return nil
	}
	next := make([]dataplane.Node, 0)
	seen := make(map[string]bool)
	for outputIndex, items := range output {
		if len(items) == 0 {
			continue
		}
		for _, edge := range graph.OutputEdges(source, "main", outputIndex) {
			if !canExecuteNode(edge.Node, allowed, destination) {
				continue
			}
			if inputs[edge.Node] == nil {
				inputs[edge.Node] = make(map[int][]dataplane.Item)
			}
			inputs[edge.Node][edge.Index] = append(inputs[edge.Node][edge.Index], items...)
			if !hasAllInputs(inputs[edge.Node], graph.InputCount(edge.Node, "main")) {
				continue
			}
			if seen[edge.Node] {
				continue
			}
			node, ok := graph.Node(edge.Node)
			if !ok || node.Disabled {
				continue
			}
			seen[edge.Node] = true
			next = append(next, *node)
		}
	}
	return next
}

func hasAllInputs(inputs map[int][]dataplane.Item, expected int) bool {
	if expected <= 1 {
		return len(inputs) > 0
	}
	if len(inputs) < expected {
		return false
	}
	for index := 0; index < expected; index++ {
		if _, ok := inputs[index]; !ok {
			return false
		}
	}
	return true
}
