package engine

import (
	"context"
	"sync"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

type HookType string

const (
	HookWorkflowExecuteBefore HookType = "workflowExecuteBefore"
	HookWorkflowExecuteAfter  HookType = "workflowExecuteAfter"
	HookNodeExecuteBefore     HookType = "nodeExecuteBefore"
	HookNodeExecuteAfter      HookType = "nodeExecuteAfter"
	HookAll                   HookType = "*"
)

type HookHandler func(context.Context, any) error

type Hooks struct {
	mu       sync.RWMutex
	handlers map[HookType][]HookHandler
}

type WorkflowExecuteBeforeData struct {
	ExecutionID  string
	WorkflowID   string
	WorkflowName string
	Mode         string
	StartedAt    time.Time
}

type WorkflowExecuteAfterData struct {
	ExecutionID  string
	WorkflowID   string
	WorkflowName string
	Mode         string
	Status       string
	Result       *Result
	StartedAt    time.Time
	FinishedAt   time.Time
}

type NodeExecuteBeforeData struct {
	ExecutionID string
	WorkflowID  string
	NodeName    string
	NodeType    string
	InputData   dataplane.Output
	StartedAt   time.Time
}

type NodeExecuteAfterData struct {
	ExecutionID string
	WorkflowID  string
	NodeName    string
	NodeType    string
	Status      string
	TaskData    dataplane.TaskData
	Error       error
	FinishedAt  time.Time
}

func NewHooks() *Hooks {
	return &Hooks{handlers: map[HookType][]HookHandler{}}
}

func (h *Hooks) On(hookType HookType, handler HookHandler) {
	if h == nil || handler == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.handlers[hookType] = append(h.handlers[hookType], handler)
}

func (h *Hooks) Emit(ctx context.Context, hookType HookType, data any) []error {
	if h == nil {
		return nil
	}
	h.mu.RLock()
	handlers := append([]HookHandler(nil), h.handlers[hookType]...)
	handlers = append(handlers, h.handlers[HookAll]...)
	h.mu.RUnlock()
	var errs []error
	for _, handler := range handlers {
		if err := handler(ctx, data); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

func (h *Hooks) Count(hookType HookType) int {
	if h == nil {
		return 0
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.handlers[hookType])
}
