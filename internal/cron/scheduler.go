package cron

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

type Leader interface {
	Start(ctx context.Context)
	Stop(ctx context.Context) error
	IsLeader() bool
}

type Config struct {
	Location   *time.Location
	Leader     Leader
	InstanceID string
}

type Scheduler struct {
	mu       sync.RWMutex
	jobs     map[string]*Job
	location *time.Location
	leader   Leader
	running  bool
	stop     chan struct{}
	done     chan struct{}
	now      func() time.Time
}

func NewScheduler(config Config) *Scheduler {
	location := config.Location
	if location == nil {
		location = time.UTC
	}
	return &Scheduler{jobs: make(map[string]*Job), location: location, leader: config.Leader, now: time.Now}
}

func (s *Scheduler) AddJob(expr string, timezone string, fn JobFunc) (string, error) {
	return s.AddJobWithID("", expr, timezone, fn)
}

func (s *Scheduler) AddJobWithID(id string, expr string, timezone string, fn JobFunc) (string, error) {
	if id == "" {
		id = uuid.NewString()
	}
	location := s.location
	if timezone != "" {
		loaded, err := time.LoadLocation(timezone)
		if err != nil {
			return "", fmt.Errorf("invalid timezone %q: %w", timezone, err)
		}
		location = loaded
	}
	schedule, err := Parse(expr, location)
	if err != nil {
		return "", err
	}
	job := newJob(id, schedule, fn, s.now)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[id] = job
	return id, nil
}

func (s *Scheduler) RemoveJob(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.jobs[id]; !ok {
		return fmt.Errorf("cron job %s not found", id)
	}
	delete(s.jobs, id)
	return nil
}

func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("cron scheduler already running")
	}
	s.running = true
	s.stop = make(chan struct{})
	s.done = make(chan struct{})
	stop := s.stop
	done := s.done
	s.mu.Unlock()
	if s.leader != nil {
		go s.leader.Start(ctx)
	}
	go s.run(ctx, stop, done)
	return nil
}

func (s *Scheduler) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	stop := s.stop
	done := s.done
	s.mu.Unlock()
	close(stop)
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	if s.leader != nil {
		_ = s.leader.Stop(ctx)
	}
	s.mu.Lock()
	s.running = false
	s.stop = nil
	s.done = nil
	s.mu.Unlock()
	return nil
}

func (s *Scheduler) GetStats() []JobStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	stats := make([]JobStats, 0, len(s.jobs))
	for _, job := range s.jobs {
		stats = append(stats, job.Stats())
	}
	return stats
}

func (s *Scheduler) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.jobs)
}

func (s *Scheduler) run(ctx context.Context, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	timer := time.NewTimer(s.nextWakeup())
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			s.drain()
			return
		case <-stop:
			s.drain()
			return
		case <-timer.C:
			s.tick(ctx)
			timer.Reset(s.nextWakeup())
		}
	}
}

func (s *Scheduler) tick(ctx context.Context) {
	if s.leader != nil && !s.leader.IsLeader() {
		return
	}
	now := s.now()
	due := []*Job{}
	s.mu.RLock()
	for _, job := range s.jobs {
		stats := job.Stats()
		if !stats.NextRun.IsZero() && !stats.NextRun.After(now.Add(500*time.Millisecond)) {
			due = append(due, job)
		}
	}
	s.mu.RUnlock()
	for _, job := range due {
		go job.run(ctx)
	}
}

func (s *Scheduler) nextWakeup() time.Duration {
	now := s.now()
	next := time.Minute
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, job := range s.jobs {
		stats := job.Stats()
		if stats.NextRun.IsZero() {
			continue
		}
		duration := stats.NextRun.Sub(now)
		if duration < 0 {
			duration = 0
		}
		if duration < next {
			next = duration
		}
	}
	if next < 50*time.Millisecond {
		return 50 * time.Millisecond
	}
	return next
}

func (s *Scheduler) drain() {
	deadline := s.now().Add(30 * time.Second)
	for s.now().Before(deadline) {
		running := false
		s.mu.RLock()
		for _, job := range s.jobs {
			if job.Stats().IsRunning {
				running = true
				break
			}
		}
		s.mu.RUnlock()
		if !running {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
}

type LocalLeader struct {
	leader atomic.Bool
}

func NewLocalLeader() *LocalLeader {
	leader := &LocalLeader{}
	leader.leader.Store(true)
	return leader
}

func (l *LocalLeader) Start(ctx context.Context) {
	l.leader.Store(true)
}

func (l *LocalLeader) Stop(ctx context.Context) error {
	l.leader.Store(false)
	return nil
}

func (l *LocalLeader) IsLeader() bool {
	return l.leader.Load()
}
