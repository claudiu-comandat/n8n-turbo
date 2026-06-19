package engine

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/binarydata"
	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/expr"
)

type ExecuteInput struct {
	Node          dataplane.Node
	NextNodes     []dataplane.Node
	InputData     dataplane.Output
	RunData       dataplane.RunData
	Variables     map[string]any
	Secrets       map[string]map[string]string
	Credentials   map[string]map[string]any
	BinaryStore   binarydata.Store
	WorkflowID    string
	WorkflowName  string
	ExecutionID   string
	ExecutionMode string
	ResumeURL     string
	ResumeFormURL string
	ScheduledTime time.Time
	RunIndex      int
	Expr          *expr.Resolver
	SubWorkflow   SubWorkflowExecutor
	CallStack     []string
}

type SubWorkflowExecutor func(context.Context, SubWorkflowRequest) (SubWorkflowResult, error)

type SubWorkflowRequest struct {
	WorkflowID        string
	Items             []dataplane.Item
	Wait              bool
	StartNode         string
	ParentExecutionID string
	ParentWorkflowID  string
	ParentNodeName    string
	Variables         map[string]any
	Secrets           map[string]map[string]string
	CallStack         []string
}

type SubWorkflowResult struct {
	ExecutionID string
	WorkflowID  string
	Status      string
	Data        dataplane.Output
}

type NodeExecutor interface {
	Execute(ctx context.Context, in ExecuteInput) (dataplane.Output, error)
}

type Registry interface {
	Executor(nodeType string) (NodeExecutor, bool)
	Register(nodeType string, executor NodeExecutor)
	KnownTypes() []string
}

type BuiltinRegistry struct {
	mu        sync.RWMutex
	executors map[string]NodeExecutor
}

func NewRegistry() *BuiltinRegistry {
	return &BuiltinRegistry{executors: make(map[string]NodeExecutor)}
}

func (r *BuiltinRegistry) Executor(nodeType string) (NodeExecutor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	executor, ok := r.executors[nodeType]
	return executor, ok
}

func (r *BuiltinRegistry) Register(nodeType string, executor NodeExecutor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.executors[nodeType] = executor
}

func (r *BuiltinRegistry) KnownTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	types := make([]string, 0, len(r.executors))
	for nodeType := range r.executors {
		types = append(types, nodeType)
	}
	sort.Strings(types)
	return types
}
