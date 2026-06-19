package activemanager

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

var ErrAlreadyActive = errors.New("workflow already active")
var ErrNotActive = errors.New("workflow not active")

type ActivationError struct {
	WorkflowID string
	Cause      error
}

func (e *ActivationError) Error() string {
	return fmt.Sprintf("activate workflow %s: %v", e.WorkflowID, e.Cause)
}

func (e *ActivationError) Unwrap() error {
	return e.Cause
}

type WorkflowActivation struct {
	WorkflowID   string
	WorkflowName string
	ActivatedAt  time.Time
	WebhookIDs   []string
	CronJobIDs   []string
	PollingIDs   []string
}

type ActiveWorkflows struct {
	mu   sync.RWMutex
	data map[string]WorkflowActivation
}

func NewActiveWorkflows() *ActiveWorkflows {
	return &ActiveWorkflows{data: make(map[string]WorkflowActivation)}
}

func (a *ActiveWorkflows) Add(activation WorkflowActivation) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.data[activation.WorkflowID] = activation
}

func (a *ActiveWorkflows) Remove(workflowID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.data, workflowID)
}

func (a *ActiveWorkflows) Get(workflowID string) (WorkflowActivation, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	activation, ok := a.data[workflowID]
	return activation, ok
}

func (a *ActiveWorkflows) IsActive(workflowID string) bool {
	_, ok := a.Get(workflowID)
	return ok
}

func (a *ActiveWorkflows) All() []WorkflowActivation {
	a.mu.RLock()
	defer a.mu.RUnlock()
	result := make([]WorkflowActivation, 0, len(a.data))
	for _, activation := range a.data {
		result = append(result, activation)
	}
	return result
}

func (a *ActiveWorkflows) Count() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.data)
}

type WorkflowRepository interface {
	GetByID(ctx context.Context, id string) (dataplane.Workflow, error)
	ListActive(ctx context.Context) ([]dataplane.Workflow, error)
	SetActive(ctx context.Context, id string, active bool) error
}

type TriggerActivator interface {
	ActivateWorkflow(ctx context.Context, workflow dataplane.Workflow) ([]string, error)
	DeactivateWorkflow(ctx context.Context, workflowID string) error
}

type StateChange struct {
	WorkflowID string
	Event      string
	Error      error
}

type RecoveryConfig struct {
	MaxConcurrent  int
	ErrorThreshold int
	Timeout        time.Duration
}

type RecoveryResult struct {
	Total    int
	Success  int
	Failed   int
	Errors   map[string]error
	Duration time.Duration
}

type Manager struct {
	mu       sync.Mutex
	active   *ActiveWorkflows
	repo     WorkflowRepository
	webhooks TriggerActivator
	cron     TriggerActivator
	polling  TriggerActivator
	changes  chan StateChange
	now      func() time.Time
}

func NewManager(repo WorkflowRepository, webhooks TriggerActivator, cron TriggerActivator, polling TriggerActivator) *Manager {
	return &Manager{
		active:   NewActiveWorkflows(),
		repo:     repo,
		webhooks: webhooks,
		cron:     cron,
		polling:  polling,
		changes:  make(chan StateChange, 100),
		now:      func() time.Time { return time.Now().UTC() },
	}
}

func (m *Manager) IsActive(workflowID string) bool {
	return m.active.IsActive(workflowID)
}

func (m *Manager) GetActivation(workflowID string) (WorkflowActivation, bool) {
	return m.active.Get(workflowID)
}

func (m *Manager) GetAllActive() []WorkflowActivation {
	return m.active.All()
}

func (m *Manager) ActiveCount() int {
	return m.active.Count()
}

func (m *Manager) Changes() <-chan StateChange {
	return m.changes
}

func (m *Manager) Activate(ctx context.Context, workflowID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active.IsActive(workflowID) {
		return ErrAlreadyActive
	}
	workflow, err := m.repo.GetByID(ctx, workflowID)
	if err != nil {
		return err
	}
	if !workflow.Active {
		return fmt.Errorf("workflow %s is not active in storage", workflowID)
	}
	activation := WorkflowActivation{WorkflowID: workflow.ID, WorkflowName: workflow.Name, ActivatedAt: m.now()}
	if activation.WorkflowID == "" {
		activation.WorkflowID = workflowID
	}
	if ids, err := activateOptional(ctx, m.webhooks, workflow); err != nil {
		m.rollback(ctx, activation)
		m.notify(workflowID, "error", err)
		return &ActivationError{WorkflowID: workflowID, Cause: err}
	} else {
		activation.WebhookIDs = ids
	}
	if ids, err := activateOptional(ctx, m.cron, workflow); err != nil {
		m.rollback(ctx, activation)
		m.notify(workflowID, "error", err)
		return &ActivationError{WorkflowID: workflowID, Cause: err}
	} else {
		activation.CronJobIDs = ids
	}
	if ids, err := activateOptional(ctx, m.polling, workflow); err != nil {
		m.rollback(ctx, activation)
		m.notify(workflowID, "error", err)
		return &ActivationError{WorkflowID: workflowID, Cause: err}
	} else {
		activation.PollingIDs = ids
	}
	m.active.Add(activation)
	m.notify(workflowID, "activated", nil)
	return nil
}

func (m *Manager) Deactivate(ctx context.Context, workflowID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.active.IsActive(workflowID) {
		return ErrNotActive
	}
	m.rollback(ctx, WorkflowActivation{WorkflowID: workflowID})
	m.active.Remove(workflowID)
	m.notify(workflowID, "deactivated", nil)
	return nil
}

func (m *Manager) RecoverActiveWorkflows(ctx context.Context, cfg RecoveryConfig) (RecoveryResult, error) {
	start := time.Now()
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 5
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	workflows, err := m.repo.ListActive(ctx)
	if err != nil {
		return RecoveryResult{}, err
	}
	result := RecoveryResult{Total: len(workflows), Errors: map[string]error{}}
	sem := make(chan struct{}, cfg.MaxConcurrent)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, workflow := range workflows {
		workflow := workflow
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			activateCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
			defer cancel()
			if err := m.Activate(activateCtx, workflow.ID); err != nil && !errors.Is(err, ErrAlreadyActive) {
				mu.Lock()
				result.Failed++
				result.Errors[workflow.ID] = err
				mu.Unlock()
				return
			}
			mu.Lock()
			result.Success++
			mu.Unlock()
		}()
	}
	wg.Wait()
	result.Duration = time.Since(start)
	if cfg.ErrorThreshold > 0 && result.Failed > cfg.ErrorThreshold {
		return result, fmt.Errorf("active workflow recovery failed for %d workflows", result.Failed)
	}
	return result, nil
}

func (m *Manager) rollback(ctx context.Context, activation WorkflowActivation) {
	_ = deactivateOptional(ctx, m.polling, activation.WorkflowID)
	_ = deactivateOptional(ctx, m.cron, activation.WorkflowID)
	_ = deactivateOptional(ctx, m.webhooks, activation.WorkflowID)
}

func (m *Manager) notify(workflowID string, event string, err error) {
	select {
	case m.changes <- StateChange{WorkflowID: workflowID, Event: event, Error: err}:
	default:
	}
}

func activateOptional(ctx context.Context, activator TriggerActivator, workflow dataplane.Workflow) ([]string, error) {
	if activator == nil {
		return nil, nil
	}
	return activator.ActivateWorkflow(ctx, workflow)
}

func deactivateOptional(ctx context.Context, activator TriggerActivator, workflowID string) error {
	if activator == nil {
		return nil
	}
	return activator.DeactivateWorkflow(ctx, workflowID)
}
