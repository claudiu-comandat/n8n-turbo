package engine

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type ActiveExecution struct {
	ID           string    `json:"id"`
	WorkflowID   string    `json:"workflowId"`
	WorkflowName string    `json:"workflowName"`
	Mode         string    `json:"mode"`
	StartedAt    time.Time `json:"startedAt"`
	cancel       context.CancelFunc
}

type ActiveExecutions struct {
	mu            sync.RWMutex
	executions    map[string]*ActiveExecution
	releases      map[string]func()
	counter       atomic.Int64
	maxConcurrent int64
	concurrency   *ConcurrencyControlService
}

type ActiveExecutionStats struct {
	Total           int64
	ByMode          map[ExecutionMode]int64
	OldestAge       time.Duration
	OldestStartedAt *time.Time
}

func NewActiveExecutions(maxConcurrent int64) *ActiveExecutions {
	return NewActiveExecutionsWithConfig(ConcurrencyConfig{MaxGlobalConcurrent: int(maxConcurrent)})
}

func NewActiveExecutionsWithConfig(config ConcurrencyConfig) *ActiveExecutions {
	return &ActiveExecutions{
		executions:    make(map[string]*ActiveExecution),
		releases:      make(map[string]func()),
		maxConcurrent: int64(config.MaxGlobalConcurrent),
		concurrency:   NewConcurrencyControlService(config),
	}
}

func (a *ActiveExecutions) Add(parent context.Context, executionID string, workflowID string, workflowName string, mode string) (context.Context, error) {
	if parent == nil {
		parent = context.Background()
	}
	release, err := a.concurrency.WaitWithOptions(parent, WaitOptions{ExecutionID: executionID, WorkflowID: workflowID, Priority: executionPriority(mode)})
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.executions[executionID]; ok {
		release()
		return nil, fmt.Errorf("execution %s is already active", executionID)
	}
	ctx, cancel := context.WithCancel(parent)
	a.executions[executionID] = &ActiveExecution{
		ID:           executionID,
		WorkflowID:   workflowID,
		WorkflowName: workflowName,
		Mode:         mode,
		StartedAt:    time.Now().UTC(),
		cancel:       cancel,
	}
	a.releases[executionID] = release
	a.counter.Add(1)
	return ctx, nil
}

func (a *ActiveExecutions) Remove(executionID string) {
	a.mu.Lock()
	var release func()
	if _, ok := a.executions[executionID]; ok {
		delete(a.executions, executionID)
		release = a.releases[executionID]
		delete(a.releases, executionID)
		a.counter.Add(-1)
	}
	a.mu.Unlock()
	if release != nil {
		release()
	}
}

func (a *ActiveExecutions) Stop(executionID string) bool {
	a.mu.RLock()
	execution, ok := a.executions[executionID]
	a.mu.RUnlock()
	if !ok {
		return false
	}
	execution.cancel()
	return true
}

func (a *ActiveExecutions) StopAll() int {
	ids := a.IDs()
	stopped := 0
	for _, id := range ids {
		if a.Stop(id) {
			stopped++
		}
	}
	return stopped
}

func (a *ActiveExecutions) WaitForAll(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if a.Count() == 0 {
			return true
		}
		time.Sleep(25 * time.Millisecond)
	}
	return a.Count() == 0
}

func (a *ActiveExecutions) GracefulShutdown(ctx context.Context) error {
	if a.Count() == 0 {
		return nil
	}
	stopped := a.StopAll()
	timeout := 30 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		timeout = time.Until(deadline)
	}
	if timeout <= 0 {
		return fmt.Errorf("context expired before stopping active executions")
	}
	if a.WaitForAll(timeout) {
		return nil
	}
	return fmt.Errorf("shutdown timeout: %d executions still running, stopped %d", a.Count(), stopped)
}

func (a *ActiveExecutions) Count() int {
	return int(a.counter.Load())
}

func (a *ActiveExecutions) IDs() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	ids := make([]string, 0, len(a.executions))
	for id := range a.executions {
		ids = append(ids, id)
	}
	return ids
}

func (a *ActiveExecutions) List() []ActiveExecution {
	a.mu.RLock()
	defer a.mu.RUnlock()
	result := make([]ActiveExecution, 0, len(a.executions))
	for _, execution := range a.executions {
		result = append(result, ActiveExecution{
			ID:           execution.ID,
			WorkflowID:   execution.WorkflowID,
			WorkflowName: execution.WorkflowName,
			Mode:         execution.Mode,
			StartedAt:    execution.StartedAt,
		})
	}
	return result
}

func (a *ActiveExecutions) Get(executionID string) (ActiveExecution, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	execution, ok := a.executions[executionID]
	if !ok {
		return ActiveExecution{}, false
	}
	return ActiveExecution{
		ID:           execution.ID,
		WorkflowID:   execution.WorkflowID,
		WorkflowName: execution.WorkflowName,
		Mode:         execution.Mode,
		StartedAt:    execution.StartedAt,
	}, true
}

func (a *ActiveExecutions) GetByWorkflow(workflowID string) []ActiveExecution {
	a.mu.RLock()
	defer a.mu.RUnlock()
	result := make([]ActiveExecution, 0)
	for _, execution := range a.executions {
		if execution.WorkflowID != workflowID {
			continue
		}
		result = append(result, ActiveExecution{
			ID:           execution.ID,
			WorkflowID:   execution.WorkflowID,
			WorkflowName: execution.WorkflowName,
			Mode:         execution.Mode,
			StartedAt:    execution.StartedAt,
		})
	}
	return result
}

func (a *ActiveExecutions) Stats() ActiveExecutionStats {
	a.mu.RLock()
	defer a.mu.RUnlock()
	now := time.Now().UTC()
	stats := ActiveExecutionStats{
		Total:  int64(len(a.executions)),
		ByMode: map[ExecutionMode]int64{},
	}
	for _, execution := range a.executions {
		mode := NormalizeExecutionMode(execution.Mode)
		stats.ByMode[mode]++
		age := now.Sub(execution.StartedAt)
		if age > stats.OldestAge {
			stats.OldestAge = age
			startedAt := execution.StartedAt
			stats.OldestStartedAt = &startedAt
		}
	}
	return stats
}

func (a *ActiveExecutions) Metrics() map[string]float64 {
	if a == nil {
		return map[string]float64{}
	}
	result := map[string]float64{}
	if a.concurrency != nil {
		for name, value := range a.concurrency.Metrics() {
			result[name] = value
		}
	}
	stats := a.Stats()
	result["n8n_active_executions_total"] = float64(stats.Total)
	result["n8n_active_executions_oldest_age_seconds"] = stats.OldestAge.Seconds()
	for mode, count := range stats.ByMode {
		result["n8n_active_executions_mode_"+strings.ReplaceAll(mode.String(), "-", "_")] = float64(count)
	}
	return result
}

func executionPriority(mode string) ExecutionPriority {
	switch NormalizeExecutionMode(mode) {
	case ExecutionModeTrigger, ExecutionModeWebhook, ExecutionModeScheduled, ExecutionModeForm:
		return PriorityHigh
	case ExecutionModeRetry, ExecutionModeManual, ExecutionModeWebhookTest, ExecutionModeFormTest:
		return PriorityNormal
	default:
		return PriorityLow
	}
}
