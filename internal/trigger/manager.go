package trigger

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var ErrTriggerExists = errors.New("trigger already exists")
var ErrTriggerNotFound = errors.New("trigger not found")

type Manager struct {
	mu       sync.Mutex
	registry *Registry
	requests chan ExecutionRequest
}

func NewManager(buffer int) *Manager {
	if buffer <= 0 {
		buffer = 100
	}
	return &Manager{registry: NewRegistry(), requests: make(chan ExecutionRequest, buffer)}
}

func (m *Manager) Requests() <-chan ExecutionRequest {
	return m.requests
}

func (m *Manager) Register(ctx context.Context, trigger Trigger) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.registry.Get(trigger.ID()); ok {
		return ErrTriggerExists
	}
	if err := trigger.Start(ctx); err != nil {
		return err
	}
	m.registry.Add(trigger)
	return nil
}

func (m *Manager) Remove(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	trigger := m.registry.Remove(id)
	if trigger == nil {
		return ErrTriggerNotFound
	}
	return trigger.Stop(ctx)
}

func (m *Manager) RemoveWorkflow(ctx context.Context, workflowID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	triggers := m.registry.ByWorkflow(workflowID)
	var errs []error
	for _, trigger := range triggers {
		_ = m.registry.Remove(trigger.ID())
		if err := trigger.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", trigger.ID(), err))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func (m *Manager) Get(id string) (Trigger, bool) {
	return m.registry.Get(id)
}

func (m *Manager) ByWorkflow(workflowID string) []Trigger {
	return m.registry.ByWorkflow(workflowID)
}

func (m *Manager) Count() int {
	return m.registry.Count()
}

func (m *Manager) Emit(request ExecutionRequest) bool {
	select {
	case m.requests <- request:
		return true
	default:
		return false
	}
}
