package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/insights"
	"github.com/n8n-io/n8n-turbo/internal/persistence"
)

type InsightsStore struct {
	db *sql.DB
}

func NewInsightsStore(db *sql.DB) *InsightsStore {
	return &InsightsStore{db: db}
}

func (s *InsightsStore) Summary(ctx context.Context, query insights.Query) (insights.SummaryData, error) {
	where, args := insightsWhere(query, "")
	sqlQuery := `
		SELECT COUNT(*),
			COALESCE(SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END), 0),
			COALESCE(AVG(CASE WHEN stopped_at IS NOT NULL THEN (julianday(stopped_at) - julianday(started_at)) * 86400000 ELSE NULL END), 0)
		FROM execution_entity` + where
	var summary insights.SummaryData
	if err := s.db.QueryRowContext(ctx, sqlQuery, args...).Scan(&summary.TotalExecutions, &summary.SuccessfulCount, &summary.FailedCount, &summary.AvgRunDurationMS); err != nil {
		return summary, fmt.Errorf("insights summary: %w", err)
	}
	if summary.TotalExecutions > 0 {
		summary.ErrorRate = float64(summary.FailedCount) / float64(summary.TotalExecutions)
	}
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workflow_entity`).Scan(&summary.TotalWorkflows)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workflow_entity WHERE active = 1`).Scan(&summary.ActiveWorkflows)
	return summary, nil
}

func (s *InsightsStore) Dashboard(ctx context.Context, query insights.Query) (insights.DashboardData, error) {
	daily, err := s.dailyStats(ctx, query)
	if err != nil {
		return insights.DashboardData{}, err
	}
	top, err := s.topWorkflows(ctx, query)
	if err != nil {
		return insights.DashboardData{}, err
	}
	status, err := s.statusBreakdown(ctx, query)
	if err != nil {
		return insights.DashboardData{}, err
	}
	return insights.DashboardData{ExecutionsByDay: daily, TopWorkflows: top, StatusBreakdown: status}, nil
}

func (s *InsightsStore) WorkflowStats(ctx context.Context, workflowID string, query insights.Query) (insights.WorkflowStat, error) {
	query.WorkflowID = workflowID
	stats, err := s.topWorkflows(ctx, query)
	if err != nil {
		return insights.WorkflowStat{}, err
	}
	if len(stats) == 0 {
		return insights.WorkflowStat{}, persistence.ErrNotFound
	}
	return stats[0], nil
}

func (s *InsightsStore) dailyStats(ctx context.Context, query insights.Query) ([]insights.DailyStats, error) {
	where, args := insightsWhere(query, "")
	group := "substr(started_at, 1, 10)"
	switch query.GroupBy {
	case "hour":
		group = "substr(started_at, 1, 13) || ':00'"
	case "week":
		group = "strftime('%Y-%W', started_at)"
	case "month":
		group = "substr(started_at, 1, 7)"
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+group+`,
			COUNT(*),
			COALESCE(SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'canceled' THEN 1 ELSE 0 END), 0),
			COALESCE(AVG(CASE WHEN stopped_at IS NOT NULL THEN (julianday(stopped_at) - julianday(started_at)) * 86400000 ELSE NULL END), 0)
		FROM execution_entity`+where+`
		GROUP BY `+group+`
		ORDER BY `+group+` ASC
		LIMIT 366`, args...)
	if err != nil {
		return nil, fmt.Errorf("insights daily stats: %w", err)
	}
	defer rows.Close()
	result := make([]insights.DailyStats, 0)
	for rows.Next() {
		var stat insights.DailyStats
		if err := rows.Scan(&stat.Date, &stat.Total, &stat.Success, &stat.Failed, &stat.Canceled, &stat.AvgDurationMS); err != nil {
			return nil, err
		}
		result = append(result, stat)
	}
	return result, rows.Err()
}

func (s *InsightsStore) topWorkflows(ctx context.Context, query insights.Query) ([]insights.WorkflowStat, error) {
	where, args := insightsWhere(query, "e")
	limit := query.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.workflow_id,
			COALESCE(w.name, e.workflow_id),
			COUNT(*),
			COALESCE(SUM(CASE WHEN e.status = 'success' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN e.status = 'error' THEN 1 ELSE 0 END), 0),
			COALESCE(AVG(CASE WHEN e.stopped_at IS NOT NULL THEN (julianday(e.stopped_at) - julianday(e.started_at)) * 86400000 ELSE NULL END), 0),
			MAX(e.started_at)
		FROM execution_entity e
		LEFT JOIN workflow_entity w ON w.id = e.workflow_id`+where+`
		GROUP BY e.workflow_id, w.name
		ORDER BY COUNT(*) DESC
		LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("insights top workflows: %w", err)
	}
	defer rows.Close()
	result := make([]insights.WorkflowStat, 0)
	for rows.Next() {
		var stat insights.WorkflowStat
		var lastRun string
		if err := rows.Scan(&stat.WorkflowID, &stat.WorkflowName, &stat.Total, &stat.Success, &stat.Failed, &stat.AvgDurationMS, &lastRun); err != nil {
			return nil, err
		}
		if stat.Total > 0 {
			stat.ErrorRate = float64(stat.Failed) / float64(stat.Total)
		}
		parsed, _ := time.Parse(time.RFC3339Nano, lastRun)
		stat.LastRunAt = parsed
		result = append(result, stat)
	}
	return result, rows.Err()
}

func (s *InsightsStore) statusBreakdown(ctx context.Context, query insights.Query) (insights.StatusCounts, error) {
	where, args := insightsWhere(query, "")
	rows, err := s.db.QueryContext(ctx, `SELECT status, COUNT(*) FROM execution_entity`+where+` GROUP BY status`, args...)
	if err != nil {
		return insights.StatusCounts{}, fmt.Errorf("insights status breakdown: %w", err)
	}
	defer rows.Close()
	var counts insights.StatusCounts
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return counts, err
		}
		switch status {
		case "success":
			counts.Success = count
		case "error":
			counts.Error = count
		case "canceled":
			counts.Canceled = count
		case "crashed":
			counts.Crashed = count
		case "running":
			counts.Running = count
		case "waiting":
			counts.Waiting = count
		}
	}
	return counts, rows.Err()
}

func insightsWhere(query insights.Query, alias string) (string, []any) {
	prefix := ""
	if alias != "" {
		prefix = alias + "."
	}
	parts := make([]string, 0)
	args := make([]any, 0)
	if query.WorkflowID != "" {
		parts = append(parts, prefix+"workflow_id = ?")
		args = append(args, query.WorkflowID)
	}
	if query.StartDate != nil {
		parts = append(parts, prefix+"started_at >= ?")
		args = append(args, query.StartDate.UTC().Format(time.RFC3339Nano))
	}
	if query.EndDate != nil {
		parts = append(parts, prefix+"started_at <= ?")
		args = append(args, query.EndDate.UTC().Format(time.RFC3339Nano))
	}
	if len(parts) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(parts, " AND "), args
}
