package engine

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type Semaphore struct {
	ch      chan struct{}
	maxSize int64
	current atomic.Int64
	name    string
}

func NewSemaphore(name string, n int) *Semaphore {
	if n < 0 {
		n = 0
	}
	return &Semaphore{ch: make(chan struct{}, n), maxSize: int64(n), name: name}
}

func (s *Semaphore) Acquire(ctx context.Context) error {
	if s.maxSize <= 0 {
		return nil
	}
	select {
	case s.ch <- struct{}{}:
		s.current.Add(1)
		return nil
	case <-ctx.Done():
		return fmt.Errorf("semaphore %q acquire canceled: %w", s.name, ctx.Err())
	}
}

func (s *Semaphore) AcquireWithTimeout(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return s.Acquire(ctx)
}

func (s *Semaphore) TryAcquire() bool {
	if s.maxSize <= 0 {
		return true
	}
	select {
	case s.ch <- struct{}{}:
		s.current.Add(1)
		return true
	default:
		return false
	}
}

func (s *Semaphore) Release() bool {
	if s.maxSize <= 0 {
		return true
	}
	select {
	case <-s.ch:
		s.current.Add(-1)
		return true
	default:
		return false
	}
}

func (s *Semaphore) Current() int64 {
	return s.current.Load()
}

func (s *Semaphore) MaxSize() int64 {
	return s.maxSize
}

func (s *Semaphore) Available() int64 {
	if s.maxSize <= 0 {
		return 0
	}
	available := s.maxSize - s.current.Load()
	if available < 0 {
		return 0
	}
	return available
}

func (s *Semaphore) IsFull() bool {
	return s.maxSize > 0 && s.current.Load() >= s.maxSize
}

type ExecutionPriority int

const (
	PriorityLow ExecutionPriority = iota
	PriorityNormal
	PriorityHigh
	PriorityCritical
)

type QueuedExecution struct {
	ID         string
	WorkflowID string
	Priority   ExecutionPriority
	EnqueuedAt time.Time
	Ready      chan struct{}
	sequence   int64
	granted    atomic.Bool
}

type PriorityQueue struct {
	mu       sync.Mutex
	items    []*QueuedExecution
	maxSize  int
	sequence int64
}

func NewPriorityQueue(maxSize int) *PriorityQueue {
	return &PriorityQueue{items: make([]*QueuedExecution, 0), maxSize: maxSize}
}

func (pq *PriorityQueue) Enqueue(exec *QueuedExecution) error {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	return pq.enqueueLocked(exec)
}

func (pq *PriorityQueue) Dequeue() (*QueuedExecution, bool) {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	return pq.dequeueLocked()
}

func (pq *PriorityQueue) Remove(exec *QueuedExecution) bool {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	return pq.removeLocked(exec)
}

func (pq *PriorityQueue) Len() int {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	return len(pq.items)
}

func (pq *PriorityQueue) enqueueLocked(exec *QueuedExecution) error {
	if pq.maxSize > 0 && len(pq.items) >= pq.maxSize {
		return fmt.Errorf("execution queue is full (%d items)", pq.maxSize)
	}
	pq.sequence++
	exec.sequence = pq.sequence
	pos := 0
	for pos < len(pq.items) {
		current := pq.items[pos]
		if exec.Priority > current.Priority || exec.Priority == current.Priority && exec.sequence < current.sequence {
			break
		}
		pos++
	}
	pq.items = append(pq.items, nil)
	copy(pq.items[pos+1:], pq.items[pos:])
	pq.items[pos] = exec
	return nil
}

func (pq *PriorityQueue) dequeueLocked() (*QueuedExecution, bool) {
	if len(pq.items) == 0 {
		return nil, false
	}
	item := pq.items[0]
	copy(pq.items, pq.items[1:])
	pq.items[len(pq.items)-1] = nil
	pq.items = pq.items[:len(pq.items)-1]
	return item, true
}

func (pq *PriorityQueue) removeLocked(exec *QueuedExecution) bool {
	for i, item := range pq.items {
		if item != exec {
			continue
		}
		copy(pq.items[i:], pq.items[i+1:])
		pq.items[len(pq.items)-1] = nil
		pq.items = pq.items[:len(pq.items)-1]
		return true
	}
	return false
}

func (pq *PriorityQueue) lenLocked() int {
	return len(pq.items)
}

type ConcurrencyConfig struct {
	MaxGlobalConcurrent      int
	MaxPerWorkflowConcurrent int
	WorkflowOverrides        map[string]int
	QueueSize                int
	AcquireTimeout           time.Duration
}

func DefaultConcurrencyConfig() ConcurrencyConfig {
	return ConcurrencyConfig{
		MaxGlobalConcurrent:      100,
		MaxPerWorkflowConcurrent: 0,
		WorkflowOverrides:        map[string]int{},
		QueueSize:                1000,
		AcquireTimeout:           5 * time.Minute,
	}
}

type ConcurrencyMetrics struct {
	TotalAcquired atomic.Int64
	TotalReleased atomic.Int64
	TotalRejected atomic.Int64
	TotalQueued   atomic.Int64
	TotalTimedOut atomic.Int64
	MaxConcurrent atomic.Int64
}

type WaitOptions struct {
	ExecutionID string
	WorkflowID  string
	Priority    ExecutionPriority
}

type ConcurrencyControlService struct {
	mu           sync.Mutex
	globalSem    *Semaphore
	workflowSems map[string]*Semaphore
	queue        *PriorityQueue
	config       ConcurrencyConfig
	metrics      ConcurrencyMetrics
}

func NewConcurrencyControlService(cfg ConcurrencyConfig) *ConcurrencyControlService {
	if cfg.WorkflowOverrides == nil {
		cfg.WorkflowOverrides = map[string]int{}
	}
	service := &ConcurrencyControlService{
		workflowSems: make(map[string]*Semaphore),
		queue:        NewPriorityQueue(cfg.QueueSize),
		config:       cfg,
	}
	if cfg.MaxGlobalConcurrent > 0 {
		service.globalSem = NewSemaphore("global", cfg.MaxGlobalConcurrent)
	}
	return service
}

func (svc *ConcurrencyControlService) Wait(ctx context.Context, workflowID string) (func(), error) {
	return svc.WaitWithOptions(ctx, WaitOptions{WorkflowID: workflowID, Priority: PriorityNormal})
}

func (svc *ConcurrencyControlService) WaitWithOptions(ctx context.Context, opts WaitOptions) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if svc == nil {
		return func() {}, nil
	}
	if svc.config.AcquireTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, svc.config.AcquireTimeout)
		defer cancel()
	}
	svc.mu.Lock()
	if svc.queue.lenLocked() == 0 && svc.tryAcquireLocked(opts.WorkflowID) {
		svc.recordAcquireLocked()
		release := svc.releaseFunc(opts.WorkflowID)
		svc.mu.Unlock()
		return release, nil
	}
	if svc.config.QueueSize <= 0 {
		svc.metrics.TotalRejected.Add(1)
		svc.mu.Unlock()
		return nil, fmt.Errorf("concurrency limit reached")
	}
	queued := &QueuedExecution{
		ID:         opts.ExecutionID,
		WorkflowID: opts.WorkflowID,
		Priority:   opts.Priority,
		EnqueuedAt: time.Now().UTC(),
		Ready:      make(chan struct{}),
	}
	if err := svc.queue.enqueueLocked(queued); err != nil {
		svc.metrics.TotalRejected.Add(1)
		svc.mu.Unlock()
		return nil, err
	}
	svc.metrics.TotalQueued.Add(1)
	svc.mu.Unlock()
	select {
	case <-queued.Ready:
		return svc.releaseFunc(opts.WorkflowID), nil
	case <-ctx.Done():
		svc.mu.Lock()
		removed := svc.queue.removeLocked(queued)
		granted := queued.granted.Load()
		if removed {
			svc.metrics.TotalTimedOut.Add(1)
			svc.mu.Unlock()
			return nil, fmt.Errorf("concurrency wait canceled: %w", ctx.Err())
		}
		svc.mu.Unlock()
		if granted {
			return svc.releaseFunc(opts.WorkflowID), nil
		}
		svc.metrics.TotalTimedOut.Add(1)
		return nil, fmt.Errorf("concurrency wait canceled: %w", ctx.Err())
	}
}

func (svc *ConcurrencyControlService) Metrics() map[string]float64 {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	metrics := map[string]float64{
		"n8n_concurrency_total_acquired":  float64(svc.metrics.TotalAcquired.Load()),
		"n8n_concurrency_total_released":  float64(svc.metrics.TotalReleased.Load()),
		"n8n_concurrency_total_rejected":  float64(svc.metrics.TotalRejected.Load()),
		"n8n_concurrency_total_queued":    float64(svc.metrics.TotalQueued.Load()),
		"n8n_concurrency_total_timed_out": float64(svc.metrics.TotalTimedOut.Load()),
		"n8n_concurrency_max_concurrent":  float64(svc.metrics.MaxConcurrent.Load()),
		"n8n_concurrency_pending":         float64(svc.queue.lenLocked()),
	}
	if svc.globalSem != nil {
		metrics["n8n_concurrency_global_current"] = float64(svc.globalSem.Current())
		metrics["n8n_concurrency_global_max"] = float64(svc.globalSem.MaxSize())
		metrics["n8n_concurrency_global_available"] = float64(svc.globalSem.Available())
	}
	for workflowID, sem := range svc.workflowSems {
		metrics["n8n_concurrency_workflow_current_"+workflowID] = float64(sem.Current())
		metrics["n8n_concurrency_workflow_max_"+workflowID] = float64(sem.MaxSize())
		metrics["n8n_concurrency_workflow_available_"+workflowID] = float64(sem.Available())
	}
	return metrics
}

func (svc *ConcurrencyControlService) UpdateConfig(cfg ConcurrencyConfig) {
	if cfg.WorkflowOverrides == nil {
		cfg.WorkflowOverrides = map[string]int{}
	}
	svc.mu.Lock()
	defer svc.mu.Unlock()
	svc.config = cfg
	svc.queue.maxSize = cfg.QueueSize
	if cfg.MaxGlobalConcurrent > 0 && (svc.globalSem == nil || int(svc.globalSem.MaxSize()) != cfg.MaxGlobalConcurrent) {
		svc.globalSem = NewSemaphore("global", cfg.MaxGlobalConcurrent)
	}
	if cfg.MaxGlobalConcurrent <= 0 {
		svc.globalSem = nil
	}
	for workflowID, sem := range svc.workflowSems {
		limit := svc.workflowLimitLocked(workflowID)
		if limit <= 0 || int(sem.MaxSize()) != limit {
			delete(svc.workflowSems, workflowID)
		}
	}
	svc.dispatchLocked()
}

func (svc *ConcurrencyControlService) releaseFunc(workflowID string) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			svc.release(workflowID)
		})
	}
}

func (svc *ConcurrencyControlService) release(workflowID string) {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if workflowID != "" {
		if sem, ok := svc.workflowSems[workflowID]; ok {
			sem.Release()
		}
	}
	if svc.globalSem != nil {
		svc.globalSem.Release()
	}
	svc.metrics.TotalReleased.Add(1)
	svc.dispatchLocked()
}

func (svc *ConcurrencyControlService) dispatchLocked() {
	for {
		index := -1
		var selected *QueuedExecution
		for i, item := range svc.queue.items {
			if svc.tryAcquireLocked(item.WorkflowID) {
				index = i
				selected = item
				break
			}
		}
		if index < 0 {
			return
		}
		copy(svc.queue.items[index:], svc.queue.items[index+1:])
		svc.queue.items[len(svc.queue.items)-1] = nil
		svc.queue.items = svc.queue.items[:len(svc.queue.items)-1]
		selected.granted.Store(true)
		svc.recordAcquireLocked()
		close(selected.Ready)
	}
}

func (svc *ConcurrencyControlService) tryAcquireLocked(workflowID string) bool {
	if svc.globalSem != nil && !svc.globalSem.TryAcquire() {
		return false
	}
	workflowSem := svc.workflowSemaphoreLocked(workflowID)
	if workflowSem != nil && !workflowSem.TryAcquire() {
		if svc.globalSem != nil {
			svc.globalSem.Release()
		}
		return false
	}
	return true
}

func (svc *ConcurrencyControlService) workflowSemaphoreLocked(workflowID string) *Semaphore {
	if workflowID == "" {
		return nil
	}
	if sem, ok := svc.workflowSems[workflowID]; ok {
		return sem
	}
	limit := svc.workflowLimitLocked(workflowID)
	if limit <= 0 {
		return nil
	}
	sem := NewSemaphore("workflow-"+workflowID, limit)
	svc.workflowSems[workflowID] = sem
	return sem
}

func (svc *ConcurrencyControlService) workflowLimitLocked(workflowID string) int {
	if limit, ok := svc.config.WorkflowOverrides[workflowID]; ok {
		return limit
	}
	return svc.config.MaxPerWorkflowConcurrent
}

func (svc *ConcurrencyControlService) recordAcquireLocked() {
	svc.metrics.TotalAcquired.Add(1)
	current := int64(0)
	if svc.globalSem != nil {
		current = svc.globalSem.Current()
	}
	for {
		old := svc.metrics.MaxConcurrent.Load()
		if current <= old {
			return
		}
		if svc.metrics.MaxConcurrent.CompareAndSwap(old, current) {
			return
		}
	}
}
