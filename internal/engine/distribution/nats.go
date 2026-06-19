package distribution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	nats "github.com/nats-io/nats.go"
)

type NATSConfig struct {
	URL           string
	StreamName    string
	Subject       string
	ResultSubject string
	DeadSubject   string
	ConsumerName  string
	FetchTimeout  time.Duration
	CloseConn     bool
}

type NATSDistributor struct {
	conn    *nats.Conn
	js      nats.JetStreamContext
	sub     *nats.Subscription
	cfg     NATSConfig
	active  sync.Map
	pending sync.Map
	closed  atomic.Bool
}

type natsActiveJob struct {
	msg *nats.Msg
	job Job
}

func NewNATSDistributor(conn *nats.Conn, cfg NATSConfig) (*NATSDistributor, error) {
	if conn == nil {
		return nil, fmt.Errorf("nats connection is required")
	}
	cfg = normalizeNATSConfig(cfg)
	js, err := conn.JetStream()
	if err != nil {
		return nil, err
	}
	distributor := &NATSDistributor{conn: conn, js: js, cfg: cfg}
	if err := distributor.ensureStream(); err != nil {
		return nil, err
	}
	sub, err := js.PullSubscribe(cfg.Subject, cfg.ConsumerName, nats.BindStream(cfg.StreamName))
	if err != nil {
		return nil, err
	}
	distributor.sub = sub
	return distributor, nil
}

func NewNATSDistributorFromURL(url string, cfg NATSConfig) (*NATSDistributor, error) {
	if strings.TrimSpace(url) == "" {
		url = nats.DefaultURL
	}
	cfg.CloseConn = true
	conn, err := nats.Connect(url)
	if err != nil {
		return nil, err
	}
	distributor, err := NewNATSDistributor(conn, cfg)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return distributor, nil
}

func (d *NATSDistributor) Enqueue(ctx context.Context, job Job) error {
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
	data, err := json.Marshal(job)
	if err != nil {
		return err
	}
	_, err = d.js.Publish(d.cfg.Subject, data, nats.Context(ctx))
	if err != nil {
		return err
	}
	d.pending.Store(job.ID, job)
	return nil
}

func (d *NATSDistributor) Dequeue(ctx context.Context) (Job, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if d.closed.Load() {
		return Job{}, ErrDistributorClosed
	}
	timeout := d.cfg.FetchTimeout
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining < timeout {
			timeout = remaining
		}
	}
	if timeout <= 0 {
		timeout = time.Millisecond
	}
	messages, err := d.sub.Fetch(1, nats.MaxWait(timeout))
	if err != nil {
		if errors.Is(err, nats.ErrTimeout) || ctx.Err() != nil {
			return Job{}, fmt.Errorf("dequeue canceled: %w", firstErr(ctx.Err(), err))
		}
		return Job{}, err
	}
	for _, message := range messages {
		var job Job
		if err := json.Unmarshal(message.Data, &job); err != nil {
			_ = message.Ack()
			continue
		}
		if job.Metadata == nil {
			job.Metadata = map[string]string{}
		}
		meta, _ := message.Metadata()
		if meta != nil {
			job.Metadata["_streamID"] = fmt.Sprintf("%d", meta.Sequence.Stream)
			job.Metadata["_consumerID"] = fmt.Sprintf("%d", meta.Sequence.Consumer)
		}
		d.pending.Delete(job.ID)
		d.active.Store(job.ID, natsActiveJob{msg: message, job: job})
		return job, nil
	}
	return Job{}, fmt.Errorf("dequeue returned no valid jobs")
}

func (d *NATSDistributor) Acknowledge(ctx context.Context, jobID string, result JobResult) error {
	if ctx == nil {
		ctx = context.Background()
	}
	active, ok := d.active.LoadAndDelete(jobID)
	if !ok {
		return fmt.Errorf("job %q not found in active jobs", jobID)
	}
	entry := active.(natsActiveJob)
	result.JobID = firstNonEmpty(result.JobID, jobID)
	result.WorkflowID = firstNonEmpty(result.WorkflowID, entry.job.WorkflowID)
	if result.FinishedAt == 0 {
		result.FinishedAt = time.Now().UTC().UnixMilli()
	}
	if result.Error != "" {
		result.Success = false
	} else if !result.Success {
		result.Success = true
	}
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if d.cfg.ResultSubject != "" {
		if _, err := d.js.Publish(d.cfg.ResultSubject, data, nats.Context(ctx)); err != nil {
			return err
		}
	}
	return entry.msg.Ack(nats.Context(ctx))
}

func (d *NATSDistributor) Nack(ctx context.Context, jobID string, reason string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	active, ok := d.active.LoadAndDelete(jobID)
	if !ok {
		return fmt.Errorf("job %q not found in active jobs", jobID)
	}
	entry := active.(natsActiveJob)
	job := entry.job
	if job.Metadata == nil {
		job.Metadata = map[string]string{}
	}
	job.Metadata["nackReason"] = reason
	job.Metadata["nackAt"] = fmt.Sprintf("%d", time.Now().UTC().UnixMilli())
	data, err := json.Marshal(job)
	if err != nil {
		return err
	}
	if d.cfg.DeadSubject != "" {
		if _, err := d.js.Publish(d.cfg.DeadSubject, data, nats.Context(ctx)); err != nil {
			return err
		}
	}
	if err := entry.msg.Ack(nats.Context(ctx)); err != nil {
		return err
	}
	return d.Enqueue(ctx, job)
}

func (d *NATSDistributor) Active(ctx context.Context) ([]Job, error) {
	jobs := []Job{}
	d.active.Range(func(key any, value any) bool {
		jobs = append(jobs, value.(natsActiveJob).job)
		return true
	})
	return jobs, nil
}

func (d *NATSDistributor) Pending(ctx context.Context) ([]Job, error) {
	jobs := []Job{}
	d.pending.Range(func(key any, value any) bool {
		jobs = append(jobs, value.(Job))
		return true
	})
	return jobs, nil
}

func (d *NATSDistributor) WaitResult(ctx context.Context, jobID string) (JobResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if jobID == "" {
		return JobResult{}, fmt.Errorf("job ID is required")
	}
	sub, err := d.js.SubscribeSync(d.cfg.ResultSubject, nats.BindStream(d.cfg.StreamName), nats.DeliverAll())
	if err != nil {
		return JobResult{}, err
	}
	defer sub.Unsubscribe()
	for {
		msg, err := sub.NextMsgWithContext(ctx)
		if err != nil {
			return JobResult{}, fmt.Errorf("wait result canceled: %w", err)
		}
		var result JobResult
		if err := json.Unmarshal(msg.Data, &result); err != nil {
			_ = msg.Ack()
			continue
		}
		_ = msg.Ack()
		if result.JobID == jobID {
			return result, nil
		}
	}
}

func (d *NATSDistributor) Close() error {
	if !d.closed.CompareAndSwap(false, true) {
		return nil
	}
	if d.sub != nil {
		_ = d.sub.Unsubscribe()
	}
	if d.cfg.CloseConn && d.conn != nil {
		d.conn.Close()
	}
	return nil
}

func (d *NATSDistributor) ensureStream() error {
	subjects := []string{d.cfg.Subject}
	if d.cfg.ResultSubject != "" {
		subjects = append(subjects, d.cfg.ResultSubject)
	}
	if d.cfg.DeadSubject != "" {
		subjects = append(subjects, d.cfg.DeadSubject)
	}
	_, err := d.js.StreamInfo(d.cfg.StreamName)
	if err == nil {
		return nil
	}
	if !errors.Is(err, nats.ErrStreamNotFound) {
		return err
	}
	_, err = d.js.AddStream(&nats.StreamConfig{
		Name:      d.cfg.StreamName,
		Subjects:  subjects,
		Storage:   nats.MemoryStorage,
		Retention: nats.LimitsPolicy,
	})
	if err != nil {
		return err
	}
	_, err = d.js.AddConsumer(d.cfg.StreamName, &nats.ConsumerConfig{
		Durable:       d.cfg.ConsumerName,
		FilterSubject: d.cfg.Subject,
		AckPolicy:     nats.AckExplicitPolicy,
		DeliverPolicy: nats.DeliverAllPolicy,
	})
	return err
}

func normalizeNATSConfig(cfg NATSConfig) NATSConfig {
	if cfg.StreamName == "" {
		cfg.StreamName = "N8N_JOBS"
	}
	if cfg.Subject == "" {
		cfg.Subject = "n8n.jobs"
	}
	if cfg.ResultSubject == "" {
		cfg.ResultSubject = cfg.Subject + ".results"
	}
	if cfg.DeadSubject == "" {
		cfg.DeadSubject = cfg.Subject + ".dead"
	}
	if cfg.ConsumerName == "" {
		cfg.ConsumerName = "workers-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	if cfg.FetchTimeout <= 0 {
		cfg.FetchTimeout = time.Second
	}
	return cfg
}

func firstErr(values ...error) error {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return context.Canceled
}
