package trigger

import (
	"context"
	"sync"
	"time"
)

type Type string

const (
	TypeWebhook  Type = "webhook"
	TypeSchedule Type = "schedule"
	TypePolling  Type = "polling"
	TypeManual   Type = "manual"
	TypeForm     Type = "form"
	TypeChat     Type = "chat"
)

type TickCallback func(ctx context.Context, at time.Time) error

type Trigger interface {
	ID() string
	Type() Type
	WorkflowID() string
	NodeID() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	IsRunning() bool
}

type ExecutionRequest struct {
	WorkflowID    string
	NodeID        string
	TriggerType   Type
	TriggerData   map[string]any
	ScheduledTime *time.Time
	Mode          string
}

type PollingState struct {
	TriggerID  string
	WorkflowID string
	NodeID     string
	LastRunAt  time.Time
	LastItemID string
	IsRunning  bool
}

type ScheduledJob struct {
	JobID      string
	WorkflowID string
	NodeID     string
	CronExpr   string
	Timezone   string
	Callback   TickCallback
	LastRun    time.Time
	NextRun    time.Time
}

type Registry struct {
	mu       sync.RWMutex
	triggers map[string]Trigger
}

func NewRegistry() *Registry {
	return &Registry{triggers: make(map[string]Trigger)}
}

func (r *Registry) Add(trigger Trigger) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.triggers[trigger.ID()] = trigger
}

func (r *Registry) Remove(id string) Trigger {
	r.mu.Lock()
	defer r.mu.Unlock()
	trigger := r.triggers[id]
	delete(r.triggers, id)
	return trigger
}

func (r *Registry) Get(id string) (Trigger, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	trigger, ok := r.triggers[id]
	return trigger, ok
}

func (r *Registry) ByWorkflow(workflowID string) []Trigger {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Trigger, 0)
	for _, trigger := range r.triggers {
		if trigger.WorkflowID() == workflowID {
			result = append(result, trigger)
		}
	}
	return result
}

func (r *Registry) All() []Trigger {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Trigger, 0, len(r.triggers))
	for _, trigger := range r.triggers {
		result = append(result, trigger)
	}
	return result
}

func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.triggers)
}
