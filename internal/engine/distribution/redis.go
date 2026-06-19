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
	"github.com/redis/go-redis/v9"
)

type RedisConfig struct {
	StreamKey     string
	GroupName     string
	ConsumerID    string
	ResultStream  string
	DeadLetterKey string
	Block         time.Duration
	BatchSize     int64
	CloseClient   bool
}

type RedisDistributor struct {
	client *redis.Client
	cfg    RedisConfig
	active sync.Map
	closed atomic.Bool
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
	distributor := &RedisDistributor{client: client, cfg: cfg}
	err := client.XGroupCreateMkStream(context.Background(), cfg.StreamKey, cfg.GroupName, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return nil, fmt.Errorf("create redis consumer group: %w", err)
	}
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
	if d.cfg.ResultStream != "" {
		if err := d.client.XAdd(ctx, &redis.XAddArgs{Stream: d.cfg.ResultStream, Values: map[string]any{"result": string(data), "jobId": result.JobID, "workflowId": result.WorkflowID}}).Err(); err != nil {
			return err
		}
	}
	if err := d.client.XAck(ctx, d.cfg.StreamKey, d.cfg.GroupName, entry.streamID).Err(); err != nil {
		return err
	}
	return d.client.XDel(ctx, d.cfg.StreamKey, entry.streamID).Err()
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
	if d.cfg.DeadLetterKey != "" {
		if err := d.client.XAdd(ctx, &redis.XAddArgs{Stream: d.cfg.DeadLetterKey, Values: map[string]any{"job": string(data), "jobId": job.ID, "reason": reason}}).Err(); err != nil {
			return err
		}
	}
	if err := d.client.XAck(ctx, d.cfg.StreamKey, d.cfg.GroupName, entry.streamID).Err(); err != nil {
		return err
	}
	if err := d.client.XDel(ctx, d.cfg.StreamKey, entry.streamID).Err(); err != nil {
		return err
	}
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
	messages, err := d.client.XRangeN(ctx, d.cfg.StreamKey, "-", "+", 250).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return []Job{}, nil
		}
		return nil, err
	}
	activeStreamIDs := map[string]bool{}
	d.active.Range(func(key any, value any) bool {
		activeStreamIDs[value.(redisActiveJob).streamID] = true
		return true
	})
	jobs := []Job{}
	for _, message := range messages {
		if activeStreamIDs[message.ID] {
			continue
		}
		job, err := redisJobFromMessage(message)
		if err != nil {
			continue
		}
		jobs = append(jobs, job)
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
	if d.cfg.CloseClient {
		return d.client.Close()
	}
	return nil
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
