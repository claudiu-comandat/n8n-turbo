package distribution

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type JobExecutorFunc func(ctx context.Context, job Job) (JobResult, error)

type PoolConfig struct {
	NumWorkers       int
	MaxJobsPerWorker int
	ShutdownTimeout  time.Duration
	JobTimeout       time.Duration
}

func DefaultPoolConfig() PoolConfig {
	return PoolConfig{NumWorkers: 10, MaxJobsPerWorker: 1, ShutdownTimeout: 30 * time.Second, JobTimeout: 5 * time.Minute}
}

type PoolMetrics struct {
	JobsProcessed atomic.Int64
	JobsSucceeded atomic.Int64
	JobsFailed    atomic.Int64
	JobsTimedOut  atomic.Int64
	ActiveWorkers atomic.Int64
}

type WorkerPool struct {
	distributor JobDistributor
	executor    JobExecutorFunc
	limiter     *engine.Semaphore
	cfg         PoolConfig
	wg          sync.WaitGroup
	metrics     PoolMetrics
}

func NewWorkerPool(distributor JobDistributor, executor JobExecutorFunc, cfg PoolConfig) *WorkerPool {
	defaults := DefaultPoolConfig()
	if cfg.NumWorkers <= 0 {
		cfg.NumWorkers = defaults.NumWorkers
	}
	if cfg.MaxJobsPerWorker <= 0 {
		cfg.MaxJobsPerWorker = defaults.MaxJobsPerWorker
	}
	if cfg.ShutdownTimeout <= 0 {
		cfg.ShutdownTimeout = defaults.ShutdownTimeout
	}
	return &WorkerPool{
		distributor: distributor,
		executor:    executor,
		limiter:     engine.NewSemaphore("worker-pool", cfg.NumWorkers*cfg.MaxJobsPerWorker),
		cfg:         cfg,
	}
}

func (p *WorkerPool) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if p.distributor == nil {
		return fmt.Errorf("job distributor is required")
	}
	if p.executor == nil {
		return fmt.Errorf("job executor is required")
	}
	for i := 0; i < p.cfg.NumWorkers; i++ {
		p.wg.Add(1)
		go p.worker(ctx)
	}
	p.wg.Wait()
	return nil
}

func (p *WorkerPool) GracefulStop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("shutdown timeout: %d workers still active", p.metrics.ActiveWorkers.Load())
	}
}

func (p *WorkerPool) Metrics() map[string]float64 {
	return map[string]float64{
		"n8n_pool_jobs_processed": float64(p.metrics.JobsProcessed.Load()),
		"n8n_pool_jobs_succeeded": float64(p.metrics.JobsSucceeded.Load()),
		"n8n_pool_jobs_failed":    float64(p.metrics.JobsFailed.Load()),
		"n8n_pool_jobs_timed_out": float64(p.metrics.JobsTimedOut.Load()),
		"n8n_pool_active_workers": float64(p.metrics.ActiveWorkers.Load()),
	}
}

func DrainAndStop(ctx context.Context, pool *WorkerPool, distributor *LocalDistributor) error {
	if pool == nil || distributor == nil {
		return nil
	}
	timeout := pool.cfg.ShutdownTimeout
	if timeout <= 0 {
		timeout = DefaultPoolConfig().ShutdownTimeout
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := distributor.Close(); err != nil {
		return err
	}
	return pool.GracefulStop(shutdownCtx)
}

func (p *WorkerPool) worker(ctx context.Context) {
	defer p.wg.Done()
	p.metrics.ActiveWorkers.Add(1)
	defer p.metrics.ActiveWorkers.Add(-1)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		job, err := p.distributor.Dequeue(ctx)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, ErrDistributorClosed) {
				return
			}
			select {
			case <-time.After(25 * time.Millisecond):
				continue
			case <-ctx.Done():
				return
			}
		}
		p.processJob(ctx, job)
	}
}

func (p *WorkerPool) processJob(ctx context.Context, job Job) {
	if err := p.limiter.Acquire(ctx); err != nil {
		_ = p.distributor.Nack(ctx, job.ID, err.Error())
		return
	}
	defer p.limiter.Release()
	started := time.Now()
	jobCtx := ctx
	cancel := func() {}
	if p.cfg.JobTimeout > 0 {
		jobCtx, cancel = context.WithTimeout(ctx, p.cfg.JobTimeout)
	}
	defer cancel()
	result, err := p.executor(jobCtx, job)
	durationMS := time.Since(started).Milliseconds()
	p.metrics.JobsProcessed.Add(1)
	if err != nil {
		p.metrics.JobsFailed.Add(1)
		if jobCtx.Err() == context.DeadlineExceeded {
			p.metrics.JobsTimedOut.Add(1)
			err = fmt.Errorf("job timed out after %v: %w", p.cfg.JobTimeout, err)
		}
		_ = p.distributor.Nack(ctx, job.ID, err.Error())
		return
	}
	result.JobID = firstNonEmpty(result.JobID, job.ID)
	result.WorkflowID = firstNonEmpty(result.WorkflowID, job.WorkflowID)
	if result.Error != "" {
		result.Success = false
	} else if !result.Success {
		result.Success = true
	}
	result.DurationMS = durationMS
	result.FinishedAt = time.Now().UTC().UnixMilli()
	if result.Success {
		p.metrics.JobsSucceeded.Add(1)
	} else {
		p.metrics.JobsFailed.Add(1)
	}
	_ = p.distributor.Acknowledge(ctx, job.ID, result)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
