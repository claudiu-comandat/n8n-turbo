package insights

import "time"

type SummaryData struct {
	TotalExecutions  int     `json:"totalExecutions"`
	SuccessfulCount  int     `json:"successfulExecutions"`
	FailedCount      int     `json:"failedExecutions"`
	ErrorRate        float64 `json:"errorRate"`
	AvgRunDurationMS float64 `json:"averageRunDuration"`
	TotalWorkflows   int     `json:"totalWorkflows"`
	ActiveWorkflows  int     `json:"activeWorkflows"`
}

type DashboardData struct {
	ExecutionsByDay []DailyStats   `json:"executionsByDay"`
	TopWorkflows    []WorkflowStat `json:"topWorkflows"`
	StatusBreakdown StatusCounts   `json:"statusBreakdown"`
}

type DailyStats struct {
	Date          string  `json:"date"`
	Total         int     `json:"total"`
	Success       int     `json:"success"`
	Failed        int     `json:"failed"`
	Canceled      int     `json:"canceled"`
	AvgDurationMS float64 `json:"avgDurationMs"`
}

type WorkflowStat struct {
	WorkflowID    string    `json:"workflowId"`
	WorkflowName  string    `json:"workflowName"`
	Total         int       `json:"total"`
	Success       int       `json:"success"`
	Failed        int       `json:"failed"`
	ErrorRate     float64   `json:"errorRate"`
	AvgDurationMS float64   `json:"avgDurationMs"`
	LastRunAt     time.Time `json:"lastRunAt"`
}

type StatusCounts struct {
	Success  int `json:"success"`
	Error    int `json:"error"`
	Canceled int `json:"canceled"`
	Crashed  int `json:"crashed"`
	Running  int `json:"running"`
	Waiting  int `json:"waiting"`
}

type Query struct {
	StartDate  *time.Time
	EndDate    *time.Time
	WorkflowID string
	GroupBy    string
	Limit      int
}
