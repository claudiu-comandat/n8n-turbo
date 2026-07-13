package distribution

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type LocalConfig struct {
	QueueSize  int
	MaxWorkers int
	RetryDelay time.Duration
}

func DefaultLocalConfig() LocalConfig {
	return LocalConfig{QueueSize: 1000, MaxWorkers: 10, RetryDelay: 5 * time.Second}
}

type LocalDistributor struct {
	jobQueue     chan Job
	results      chan JobResult
	done         chan struct{}
	closeOnce    sync.Once
	closed       atomic.Bool
	activeJobs   sync.Map
	pendingJobs  sync.Map
	completed    sync.Map
	waiters      sync.Map
	activeCount  atomic.Int64
	pendingCount atomic.Int64
	cfg          LocalConfig
}

func NewLocalDistributor(cfg LocalConfig) *LocalDistributor {
	defaults := DefaultLocalConfig()
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = defaults.QueueSize
	}
	if cfg.MaxWorkers <= 0 {
		cfg.MaxWorkers = defaults.MaxWorkers
	}
	if cfg.RetryDelay < 0 {
		cfg.RetryDelay = 0
	}
	if cfg.RetryDelay == 0 {
		cfg.RetryDelay = defaults.RetryDelay
	}
	return &LocalDistributor{
		jobQueue: make(chan Job, cfg.QueueSize),
		results:  make(chan JobResult, cfg.QueueSize),
		done:     make(chan struct{}),
		cfg:      cfg,
	}
}

func (d *LocalDistributor) Enqueue(ctx context.Context, job Job) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if d.closed.Load() {
		return ErrDistributorClosed
	}
	if job.ID == "" {
		return fmt.Errorf("job ID is required")
	}
	job = normalizeJob(job)
	select {
	case d.jobQueue <- job:
		d.pendingJobs.Store(job.ID, job)
		d.pendingCount.Add(1)
		return nil
	case <-ctx.Done():
		return fmt.Errorf("enqueue canceled: %w", ctx.Err())
	default:
		return fmt.Errorf("job queue is full (%d items)", d.cfg.QueueSize)
	}
}

func (d *LocalDistributor) EnqueueBlocking(ctx context.Context, job Job) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if d.closed.Load() {
		return ErrDistributorClosed
	}
	if job.ID == "" {
		return fmt.Errorf("job ID is required")
	}
	job = normalizeJob(job)
	select {
	case d.jobQueue <- job:
		d.pendingJobs.Store(job.ID, job)
		d.pendingCount.Add(1)
		return nil
	case <-ctx.Done():
		return fmt.Errorf("enqueue blocked: %w", ctx.Err())
	case <-d.done:
		return fmt.Errorf("distributor closed while waiting to enqueue")
	}
}

func (d *LocalDistributor) Dequeue(ctx context.Context) (Job, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case job := <-d.jobQueue:
		d.pendingJobs.Delete(job.ID)
		d.pendingCount.Add(-1)
		d.activeJobs.Store(job.ID, job)
		d.activeCount.Add(1)
		return job, nil
	case <-ctx.Done():
		return Job{}, fmt.Errorf("dequeue canceled: %w", ctx.Err())
	case <-d.done:
		return Job{}, ErrDistributorClosed
	}
}

func (d *LocalDistributor) Acknowledge(ctx context.Context, jobID string, result JobResult) error {
	if jobID == "" {
		return fmt.Errorf("job ID is required")
	}
	if _, ok := d.activeJobs.LoadAndDelete(jobID); ok {
		d.activeCount.Add(-1)
	}
	result.JobID = firstNonEmpty(result.JobID, jobID)
	if waiter, ok := d.waiters.LoadAndDelete(jobID); ok {
		ch := waiter.(chan JobResult)
		ch <- result
		close(ch)
	} else {
		d.completed.Store(jobID, result)
	}
	select {
	case d.results <- result:
	default:
	}
	return nil
}

func (d *LocalDistributor) Nack(ctx context.Context, jobID string, reason string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	value, ok := d.activeJobs.LoadAndDelete(jobID)
	if !ok {
		return fmt.Errorf("job %q not found in active jobs", jobID)
	}
	d.activeCount.Add(-1)
	job := value.(Job)
	if job.Metadata == nil {
		job.Metadata = map[string]string{}
	}
	job.Metadata["nackReason"] = reason
	go func() {
		timer := time.NewTimer(d.cfg.RetryDelay)
		defer timer.Stop()
		select {
		case <-timer.C:
			if !d.closed.Load() {
				_ = d.EnqueueBlocking(context.Background(), job)
			}
		case <-d.done:
		}
	}()
	return nil
}

func (d *LocalDistributor) Active(ctx context.Context) ([]Job, error) {
	jobs := []Job{}
	d.activeJobs.Range(func(key any, value any) bool {
		jobs = append(jobs, value.(Job))
		return true
	})
	return jobs, nil
}

func (d *LocalDistributor) Pending(ctx context.Context) ([]Job, error) {
	jobs := []Job{}
	d.pendingJobs.Range(func(key any, value any) bool {
		jobs = append(jobs, value.(Job))
		return true
	})
	return jobs, nil
}

func (d *LocalDistributor) PendingCount() int64 {
	return d.pendingCount.Load()
}

func (d *LocalDistributor) ActiveCount() int64 {
	return d.activeCount.Load()
}

func (d *LocalDistributor) Results() <-chan JobResult {
	return d.results
}

func (d *LocalDistributor) WaitResult(ctx context.Context, jobID string) (JobResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if value, ok := d.completed.LoadAndDelete(jobID); ok {
		return value.(JobResult), nil
	}
	ch := make(chan JobResult, 1)
	actual, loaded := d.waiters.LoadOrStore(jobID, ch)
	if loaded {
		ch = actual.(chan JobResult)
	}
	if value, ok := d.completed.LoadAndDelete(jobID); ok {
		d.waiters.Delete(jobID)
		return value.(JobResult), nil
	}
	select {
	case result := <-ch:
		return result, nil
	case <-ctx.Done():
		// !ok means Acknowledge already claimed the channel and will send; drain it.
		if _, ok := d.waiters.LoadAndDelete(jobID); !ok {
			return <-ch, nil
		}
		return JobResult{}, fmt.Errorf("wait result canceled: %w", ctx.Err())
	case <-d.done:
		if _, ok := d.waiters.LoadAndDelete(jobID); !ok {
			return <-ch, nil
		}
		return JobResult{}, ErrDistributorClosed
	}
}

func (d *LocalDistributor) Close() error {
	d.closeOnce.Do(func() {
		d.closed.Store(true)
		close(d.done)
	})
	return nil
}
