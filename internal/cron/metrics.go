package cron

import "sync/atomic"

type Metrics struct {
	jobsScheduled atomic.Int64
	jobsExecuted  atomic.Int64
	jobsFailed    atomic.Int64
	isLeader      atomic.Bool
}

type MetricsSnapshot struct {
	JobsScheduled int64
	JobsExecuted  int64
	JobsFailed    int64
	IsLeader      bool
}

func NewMetrics() *Metrics {
	return &Metrics{}
}

func (m *Metrics) RecordJobStart() {
	m.jobsScheduled.Add(1)
}

func (m *Metrics) RecordJobSuccess() {
	m.jobsExecuted.Add(1)
}

func (m *Metrics) RecordJobFailure() {
	m.jobsFailed.Add(1)
}

func (m *Metrics) SetIsLeader(value bool) {
	m.isLeader.Store(value)
}

func (m *Metrics) Snapshot() MetricsSnapshot {
	return MetricsSnapshot{
		JobsScheduled: m.jobsScheduled.Load(),
		JobsExecuted:  m.jobsExecuted.Load(),
		JobsFailed:    m.jobsFailed.Load(),
		IsLeader:      m.isLeader.Load(),
	}
}
