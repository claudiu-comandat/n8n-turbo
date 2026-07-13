package cron

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type RedisLeaderConfig struct {
	Addr          string
	Password      string
	DB            int
	Key           string
	InstanceID    string
	TTL           time.Duration
	RenewInterval time.Duration
}

type RedisLeader struct {
	client      *redis.Client
	key         string
	instanceID  string
	ttl         time.Duration
	interval    time.Duration
	closeClient bool
	leader      atomic.Bool
	mu          sync.Mutex
	cancel      context.CancelFunc
	done        chan struct{}
}

func NewRedisLeader(config RedisLeaderConfig) *RedisLeader {
	client := redis.NewClient(&redis.Options{
		Addr:     config.Addr,
		Password: config.Password,
		DB:       config.DB,
	})
	leader := NewRedisLeaderWithClient(client, config.Key, config.InstanceID, config.TTL)
	leader.closeClient = true
	if config.RenewInterval > 0 {
		leader.interval = config.RenewInterval
	}
	leader.interval = clampRenewInterval(leader.interval, leader.ttl)
	return leader
}

// Cap at ttl/3 so callTimeout stays positive and a stalled renewal drops leadership in time.
func clampRenewInterval(interval, ttl time.Duration) time.Duration {
	if interval <= 0 {
		interval = ttl / 3
	}
	if max := ttl / 3; max > 0 && interval > max {
		interval = max
	}
	if interval <= 0 {
		interval = time.Millisecond
	}
	return interval
}

func NewRedisLeaderWithClient(client *redis.Client, key string, instanceID string, ttl time.Duration) *RedisLeader {
	if key == "" {
		key = "n8n-turbo:scheduler:leader"
	}
	if instanceID == "" {
		instanceID = uuid.NewString()
	}
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	interval := ttl / 3
	if interval < 100*time.Millisecond {
		interval = 100 * time.Millisecond
	}
	interval = clampRenewInterval(interval, ttl)
	return &RedisLeader{client: client, key: key, instanceID: instanceID, ttl: ttl, interval: interval}
}

func (l *RedisLeader) Start(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	l.mu.Lock()
	if l.cancel != nil {
		l.mu.Unlock()
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	l.cancel = cancel
	l.done = done
	l.mu.Unlock()
	go l.run(runCtx, done)
}

func (l *RedisLeader) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	l.mu.Lock()
	cancel := l.cancel
	done := l.done
	l.cancel = nil
	l.done = nil
	l.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	err := l.release(ctx)
	l.leader.Store(false)
	if l.closeClient {
		if closeErr := l.client.Close(); err == nil {
			err = closeErr
		}
	}
	return err
}

func (l *RedisLeader) IsLeader() bool {
	return l.leader.Load()
}

func (l *RedisLeader) run(ctx context.Context, done chan<- struct{}) {
	defer close(done)
	l.tryAcquire(ctx)
	ticker := time.NewTicker(l.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			l.leader.Store(false)
			return
		case <-ticker.C:
			l.tryAcquire(ctx)
		}
	}
}

func (l *RedisLeader) tryAcquire(ctx context.Context) {
	if timeout := l.callTimeout(); timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	ok, err := l.client.SetNX(ctx, l.key, l.instanceID, l.ttl).Result()
	if err != nil {
		l.leader.Store(false)
		return
	}
	if ok {
		l.leader.Store(true)
		return
	}
	current, err := l.client.Get(ctx, l.key).Result()
	if err != nil {
		l.leader.Store(false)
		return
	}
	if current != l.instanceID {
		l.leader.Store(false)
		return
	}
	renewed, err := redisRenewLeader.Run(ctx, l.client, []string{l.key}, l.instanceID, int64(l.ttl/time.Millisecond)).Bool()
	l.leader.Store(err == nil && renewed)
}

func (l *RedisLeader) callTimeout() time.Duration {
	timeout := l.ttl - 2*l.interval
	if timeout <= 0 {
		timeout = l.ttl / 2
	}
	return timeout
}

func (l *RedisLeader) release(ctx context.Context) error {
	_, err := redisReleaseLeader.Run(ctx, l.client, []string{l.key}, l.instanceID).Result()
	return err
}

var redisRenewLeader = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("PEXPIRE", KEYS[1], ARGV[2])
end
return 0
`)

var redisReleaseLeader = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0
`)
