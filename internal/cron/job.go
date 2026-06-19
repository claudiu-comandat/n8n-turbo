package cron

import (
	"context"
	"sync"
	"time"
)

type JobFunc func(at time.Time)

type Job struct {
	id        string
	schedule  *Schedule
	fn        JobFunc
	mu        sync.Mutex
	lastRun   time.Time
	nextRun   time.Time
	runCount  int64
	failCount int64
	running   bool
	now       func() time.Time
}

type JobStats struct {
	ID        string
	NextRun   time.Time
	LastRun   time.Time
	RunCount  int64
	FailCount int64
	IsRunning bool
}

func newJob(id string, schedule *Schedule, fn JobFunc, now func() time.Time) *Job {
	if now == nil {
		now = time.Now
	}
	job := &Job{id: id, schedule: schedule, fn: fn, now: now}
	job.nextRun = schedule.Next(now())
	return job
}

func (j *Job) run(ctx context.Context) {
	j.mu.Lock()
	if j.running {
		j.mu.Unlock()
		return
	}
	at := j.now()
	j.running = true
	j.runCount++
	j.lastRun = at
	j.nextRun = j.schedule.Next(at)
	j.mu.Unlock()
	defer func() {
		if recovered := recover(); recovered != nil {
			j.mu.Lock()
			j.failCount++
			j.mu.Unlock()
		}
		j.mu.Lock()
		j.running = false
		j.mu.Unlock()
	}()
	if j.fn != nil {
		j.fn(at)
	}
}

func (j *Job) Stats() JobStats {
	j.mu.Lock()
	defer j.mu.Unlock()
	return JobStats{
		ID:        j.id,
		NextRun:   j.nextRun,
		LastRun:   j.lastRun,
		RunCount:  j.runCount,
		FailCount: j.failCount,
		IsRunning: j.running,
	}
}
