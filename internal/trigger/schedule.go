package trigger

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type CronScheduler interface {
	AddJob(expr string, timezone string, callback func(time.Time)) (string, error)
	RemoveJob(jobID string) error
}

type ScheduleTrigger struct {
	id         string
	workflowID string
	nodeID     string
	cronExpr   string
	timezone   string
	callback   TickCallback
	scheduler  CronScheduler
	jobID      string
	running    bool
	mu         sync.Mutex
}

func NewScheduleTrigger(id string, workflowID string, nodeID string, cronExpr string, timezone string, callback TickCallback, scheduler CronScheduler) *ScheduleTrigger {
	return &ScheduleTrigger{id: id, workflowID: workflowID, nodeID: nodeID, cronExpr: cronExpr, timezone: timezone, callback: callback, scheduler: scheduler}
}

func (s *ScheduleTrigger) ID() string {
	return s.id
}

func (s *ScheduleTrigger) Type() Type {
	return TypeSchedule
}

func (s *ScheduleTrigger) WorkflowID() string {
	return s.workflowID
}

func (s *ScheduleTrigger) NodeID() string {
	return s.nodeID
}

func (s *ScheduleTrigger) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *ScheduleTrigger) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil
	}
	if s.scheduler == nil {
		return fmt.Errorf("cron scheduler is required")
	}
	jobID, err := s.scheduler.AddJob(s.cronExpr, s.timezone, func(at time.Time) {
		if s.callback != nil {
			_ = s.callback(ctx, at)
		}
	})
	if err != nil {
		return fmt.Errorf("start schedule trigger %s: %w", s.id, err)
	}
	s.jobID = jobID
	s.running = true
	return nil
}

func (s *ScheduleTrigger) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return nil
	}
	if s.scheduler != nil && s.jobID != "" {
		if err := s.scheduler.RemoveJob(s.jobID); err != nil {
			return fmt.Errorf("stop schedule trigger %s: %w", s.id, err)
		}
	}
	s.running = false
	s.jobID = ""
	return nil
}
