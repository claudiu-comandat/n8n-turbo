package distribution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type RedisConfig struct {
	StreamKey         string
	GroupName         string
	ConsumerID        string
	ResultStream      string
	DeadLetterKey     string
	Block             time.Duration
	BatchSize         int64
	KeepaliveInterval time.Duration
	ReclaimInterval   time.Duration
	ReclaimMinIdle    time.Duration
	CloseClient       bool
}

type RedisDistributor struct {
	client *redis.Client
	cfg    RedisConfig
	active sync.Map
	closed atomic.Bool
	done   chan struct{}
}

type redisActiveJob struct {
	streamID string
	job      Job
}

func NewRedisDistributor(client *redis.Client, cfg RedisConfig) (*RedisDistributor, error) {
	if client == nil {
		return nil, fmt.Errorf("redis client is required")
	}
	cfg = normalizeRedisConfig(cfg)
	distributor := &RedisDistributor{client: client, cfg: cfg, done: make(chan struct{})}
	err := client.XGroupCreateMkStream(context.Background(), cfg.StreamKey, cfg.GroupName, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return nil, fmt.Errorf("create redis consumer group: %w", err)
	}
	go distributor.keepaliveLoop()
	go distributor.reapLoop()
	return distributor, nil
}

func NewRedisDistributorFromOptions(options *redis.Options, cfg RedisConfig) (*RedisDistributor, error) {
	if options == nil {
		return nil, fmt.Errorf("redis options are required")
	}
	cfg.CloseClient = true
	return NewRedisDistributor(redis.NewClient(options), cfg)
}

func (d *RedisDistributor) Enqueue(ctx context.Context, job Job) error {
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
	return d.client.XAdd(ctx, &redis.XAddArgs{
		Stream: d.cfg.StreamKey,
		Values: map[string]any{"job": string(data), "jobId": job.ID, "workflowId": job.WorkflowID, "priority": job.Priority},
	}).Err()
}

func (d *RedisDistributor) Dequeue(ctx context.Context) (Job, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if d.closed.Load() {
		return Job{}, ErrDistributorClosed
	}
	for {
		streams, err := d.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    d.cfg.GroupName,
			Consumer: d.cfg.ConsumerID,
			Streams:  []string{d.cfg.StreamKey, ">"},
			Count:    d.cfg.BatchSize,
			Block:    d.cfg.Block,
		}).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				select {
				case <-ctx.Done():
					return Job{}, fmt.Errorf("dequeue canceled: %w", ctx.Err())
				default:
					continue
				}
			}
			return Job{}, err
		}
		for _, stream := range streams {
			for _, message := range stream.Messages {
				job, err := redisJobFromMessage(message)
				if err != nil {
					_ = d.client.XAck(ctx, d.cfg.StreamKey, d.cfg.GroupName, message.ID).Err()
					_ = d.client.XDel(ctx, d.cfg.StreamKey, message.ID).Err()
					continue
				}
				if job.Metadata == nil {
					job.Metadata = map[string]string{}
				}
				job.Metadata["_streamID"] = message.ID
				d.active.Store(job.ID, redisActiveJob{streamID: message.ID, job: job})
				return job, nil
			}
		}
	}
}

func (d *RedisDistributor) Acknowledge(ctx context.Context, jobID string, result JobResult) error {
	if ctx == nil {
		ctx = context.Background()
	}
	active, ok := d.active.LoadAndDelete(jobID)
	if !ok {
		return fmt.Errorf("job %q not found in active jobs", jobID)
	}
	entry := active.(redisActiveJob)
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
	// Publish the result, ack, and delete atomically so a crash between them
	// can't leave the entry stuck pending after its result was already emitted.
	pipe := d.client.TxPipeline()
	if d.cfg.ResultStream != "" {
		pipe.XAdd(ctx, &redis.XAddArgs{Stream: d.cfg.ResultStream, Values: map[string]any{"result": string(data), "jobId": result.JobID, "workflowId": result.WorkflowID}})
	}
	pipe.XAck(ctx, d.cfg.StreamKey, d.cfg.GroupName, entry.streamID)
	pipe.XDel(ctx, d.cfg.StreamKey, entry.streamID)
	_, err = pipe.Exec(ctx)
	return err
}

func (d *RedisDistributor) Nack(ctx context.Context, jobID string, reason string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	active, ok := d.active.LoadAndDelete(jobID)
	if !ok {
		return fmt.Errorf("job %q not found in active jobs", jobID)
	}
	entry := active.(redisActiveJob)
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
	pipe := d.client.TxPipeline()
	if d.cfg.DeadLetterKey != "" {
		pipe.XAdd(ctx, &redis.XAddArgs{Stream: d.cfg.DeadLetterKey, Values: map[string]any{"job": string(data), "jobId": job.ID, "reason": reason}})
	}
	pipe.XAck(ctx, d.cfg.StreamKey, d.cfg.GroupName, entry.streamID)
	pipe.XDel(ctx, d.cfg.StreamKey, entry.streamID)
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}
	// Immediate/durable requeue on purpose: a deferred in-memory retry would widen the crash-loss window.
	return d.Enqueue(ctx, job)
}

func (d *RedisDistributor) Active(ctx context.Context) ([]Job, error) {
	jobs := []Job{}
	d.active.Range(func(key any, value any) bool {
		jobs = append(jobs, value.(redisActiveJob).job)
		return true
	})
	return jobs, nil
}

func (d *RedisDistributor) Pending(ctx context.Context) ([]Job, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	activeStreamIDs := map[string]bool{}
	d.active.Range(func(key any, value any) bool {
		activeStreamIDs[value.(redisActiveJob).streamID] = true
		return true
	})
	// Inclusive cursor (any Redis version); skip the boundary entry each next page.
	const pageSize = 500
	jobs := []Job{}
	start := "-"
	for {
		messages, err := d.client.XRangeN(ctx, d.cfg.StreamKey, start, "+", pageSize).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				break
			}
			return nil, err
		}
		if len(messages) == 0 {
			break
		}
		for i, message := range messages {
			if start != "-" && i == 0 {
				continue // boundary entry already counted on the previous page
			}
			if activeStreamIDs[message.ID] {
				continue
			}
			job, err := redisJobFromMessage(message)
			if err != nil {
				continue
			}
			jobs = append(jobs, job)
		}
		if len(messages) < pageSize {
			break
		}
		start = messages[len(messages)-1].ID // inclusive; boundary skipped next page
	}
	return jobs, nil
}

func (d *RedisDistributor) WaitResult(ctx context.Context, jobID string) (JobResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if jobID == "" {
		return JobResult{}, fmt.Errorf("job ID is required")
	}
	lastID := "0"
	for {
		streams, err := d.client.XRead(ctx, &redis.XReadArgs{
			Streams: []string{d.cfg.ResultStream, lastID},
			Count:   d.cfg.BatchSize,
			Block:   d.cfg.Block,
		}).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				select {
				case <-ctx.Done():
					return JobResult{}, fmt.Errorf("wait result canceled: %w", ctx.Err())
				default:
					continue
				}
			}
			return JobResult{}, err
		}
		for _, stream := range streams {
			for _, message := range stream.Messages {
				lastID = message.ID
				result, err := redisResultFromMessage(message)
				if err != nil {
					continue
				}
				if result.JobID == jobID {
					return result, nil
				}
			}
		}
	}
}

func (d *RedisDistributor) Close() error {
	if !d.closed.CompareAndSwap(false, true) {
		return nil
	}
	close(d.done)
	if d.cfg.CloseClient {
		return d.client.Close()
	}
	return nil
}

// keepaliveLoop refreshes this instance's in-flight entries so another instance's
// reaper never mistakes an actively-processing job for an abandoned one.
func (d *RedisDistributor) keepaliveLoop() {
	ticker := time.NewTicker(d.cfg.KeepaliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-d.done:
			return
		case <-ticker.C:
			d.refreshActive()
		}
	}
}

func (d *RedisDistributor) refreshActive() {
	ids := []string{}
	d.active.Range(func(_ any, value any) bool {
		ids = append(ids, value.(redisActiveJob).streamID)
		return true
	})
	if len(ids) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), d.cfg.KeepaliveInterval)
	defer cancel()
	// Reclaim our own entries to ourselves with min-idle 0 to reset their idle
	// time (a liveness heartbeat); JustID avoids fetching the payloads.
	_ = d.client.XClaimJustID(ctx, &redis.XClaimArgs{
		Stream:   d.cfg.StreamKey,
		Group:    d.cfg.GroupName,
		Consumer: d.cfg.ConsumerID,
		MinIdle:  0,
		Messages: ids,
	}).Err()
}

// reapLoop re-enqueues jobs whose owning consumer died (their PEL entry went idle
// past ReclaimMinIdle), so a crashed instance's in-flight work isn't lost.
func (d *RedisDistributor) reapLoop() {
	ticker := time.NewTicker(d.cfg.ReclaimInterval)
	defer ticker.Stop()
	for {
		select {
		case <-d.done:
			return
		case <-ticker.C:
			d.reclaimAbandoned()
		}
	}
}

func (d *RedisDistributor) reclaimAbandoned() {
	own := map[string]bool{}
	d.active.Range(func(_ any, value any) bool {
		own[value.(redisActiveJob).streamID] = true
		return true
	})
	ctx, cancel := context.WithTimeout(context.Background(), d.cfg.ReclaimInterval)
	defer cancel()
	start := "0-0"
	for {
		messages, next, err := d.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
			Stream:   d.cfg.StreamKey,
			Group:    d.cfg.GroupName,
			Consumer: d.cfg.ConsumerID,
			MinIdle:  d.cfg.ReclaimMinIdle,
			Start:    start,
			Count:    100,
		}).Result()
		if err != nil {
			return
		}
		for _, message := range messages {
			if own[message.ID] {
				continue // still processing on this instance; leave it be
			}
			d.recoverAbandoned(ctx, message)
		}
		if next == "" || next == "0-0" || len(messages) == 0 {
			return
		}
		start = next
	}
}

func (d *RedisDistributor) recoverAbandoned(ctx context.Context, message redis.XMessage) {
	job, err := redisJobFromMessage(message)
	if err != nil {
		// Unparseable entry: drop it so it isn't reclaimed forever.
		pipe := d.client.TxPipeline()
		pipe.XAck(ctx, d.cfg.StreamKey, d.cfg.GroupName, message.ID)
		pipe.XDel(ctx, d.cfg.StreamKey, message.ID)
		if _, err := pipe.Exec(ctx); err != nil {
			log.Printf("redis distributor: drop poison entry %s: %v", message.ID, err)
		}
		return
	}
	data, err := json.Marshal(normalizeJob(job))
	if err != nil {
		return
	}
	// Re-enqueue a fresh entry and remove the abandoned one atomically (MULTI/EXEC),
	// so a failed recovery leaves the entry pending for a later retry rather than
	// dropping the job or creating a duplicate.
	pipe := d.client.TxPipeline()
	pipe.XAdd(ctx, &redis.XAddArgs{Stream: d.cfg.StreamKey, Values: map[string]any{"job": string(data), "jobId": job.ID, "workflowId": job.WorkflowID, "priority": job.Priority}})
	pipe.XAck(ctx, d.cfg.StreamKey, d.cfg.GroupName, message.ID)
	pipe.XDel(ctx, d.cfg.StreamKey, message.ID)
	if _, err := pipe.Exec(ctx); err != nil {
		log.Printf("redis distributor: recover job %s: %v", job.ID, err)
	}
}

func normalizeRedisConfig(cfg RedisConfig) RedisConfig {
	if cfg.StreamKey == "" {
		cfg.StreamKey = "n8n:jobs"
	}
	if cfg.GroupName == "" {
		cfg.GroupName = "workers"
	}
	if cfg.ConsumerID == "" {
		cfg.ConsumerID = uuid.NewString()
	}
	if cfg.ResultStream == "" {
		cfg.ResultStream = cfg.StreamKey + ":results"
	}
	if cfg.DeadLetterKey == "" {
		cfg.DeadLetterKey = cfg.StreamKey + ":dead"
	}
	if cfg.Block <= 0 {
		cfg.Block = time.Second
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 1
	}
	if cfg.KeepaliveInterval <= 0 {
		cfg.KeepaliveInterval = 15 * time.Second
	}
	if cfg.ReclaimInterval <= 0 {
		cfg.ReclaimInterval = 30 * time.Second
	}
	if cfg.ReclaimMinIdle <= 0 {
		cfg.ReclaimMinIdle = 60 * time.Second
	}
	// The reclaim threshold must exceed the keepalive window, or a live job
	// (refreshed every KeepaliveInterval) could be reclaimed and run twice.
	if min := 3 * cfg.KeepaliveInterval; cfg.ReclaimMinIdle < min {
		cfg.ReclaimMinIdle = min
	}
	return cfg
}

func redisJobFromMessage(message redis.XMessage) (Job, error) {
	raw, ok := message.Values["job"]
	if !ok {
		return Job{}, fmt.Errorf("redis job payload missing")
	}
	var text string
	switch typed := raw.(type) {
	case string:
		text = typed
	case []byte:
		text = string(typed)
	default:
		text = fmt.Sprint(typed)
	}
	var job Job
	if err := json.Unmarshal([]byte(text), &job); err != nil {
		return Job{}, err
	}
	return job, nil
}

func redisResultFromMessage(message redis.XMessage) (JobResult, error) {
	raw, ok := message.Values["result"]
	if !ok {
		return JobResult{}, fmt.Errorf("redis result payload missing")
	}
	var text string
	switch typed := raw.(type) {
	case string:
		text = typed
	case []byte:
		text = string(typed)
	default:
		text = fmt.Sprint(typed)
	}
	var result JobResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return JobResult{}, err
	}
	return result, nil
}
