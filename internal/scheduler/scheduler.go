package scheduler

import (
	"context"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/cron"
)

type Scheduler = cron.Scheduler
type Config = cron.Config
type JobStats = cron.JobStats
type Metrics = cron.Metrics
type MetricsSnapshot = cron.MetricsSnapshot
type Schedule = cron.Schedule
type JobFunc = cron.JobFunc

func New(config Config) *Scheduler {
	return cron.NewScheduler(config)
}

func Parse(expr string, loc *time.Location) (*Schedule, error) {
	return cron.Parse(expr, loc)
}

func Run(ctx context.Context, scheduler *Scheduler) error {
	return scheduler.Start(ctx)
}
