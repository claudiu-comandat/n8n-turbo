package distribution

import (
	"context"
	"errors"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

var ErrDistributorClosed = errors.New("distributor closed")

type Job struct {
	ID           string                        `json:"id"`
	WorkflowID   string                        `json:"workflowId"`
	WorkflowData []byte                        `json:"workflowData"`
	TriggerData  []dataplane.NodeExecutionData `json:"triggerData,omitempty"`
	Mode         string                        `json:"mode"`
	Priority     int                           `json:"priority"`
	EnqueuedAt   int64                         `json:"enqueuedAt"`
	Metadata     map[string]string             `json:"metadata,omitempty"`
	RetryOf      string                        `json:"retryOf,omitempty"`
}

type JobResult struct {
	JobID      string `json:"jobId"`
	WorkflowID string `json:"workflowId"`
	Success    bool   `json:"success"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"durationMs"`
	FinishedAt int64  `json:"finishedAt"`
}

type JobDistributor interface {
	Enqueue(ctx context.Context, job Job) error
	Dequeue(ctx context.Context) (Job, error)
	Acknowledge(ctx context.Context, jobID string, result JobResult) error
	Nack(ctx context.Context, jobID string, reason string) error
	Active(ctx context.Context) ([]Job, error)
	Pending(ctx context.Context) ([]Job, error)
	Close() error
}

type JobResultWaiter interface {
	WaitResult(ctx context.Context, jobID string) (JobResult, error)
}

func normalizeJob(job Job) Job {
	if job.EnqueuedAt == 0 {
		job.EnqueuedAt = time.Now().UTC().UnixMilli()
	}
	if job.Metadata == nil {
		job.Metadata = map[string]string{}
	}
	return job
}
