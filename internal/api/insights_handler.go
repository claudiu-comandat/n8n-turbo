package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/n8n-io/n8n-turbo/internal/insights"
	"github.com/n8n-io/n8n-turbo/internal/persistence"
)

func (s *Server) handleInsightsSummary(w http.ResponseWriter, r *http.Request) {
	if s.insightsStore == nil {
		writeJSON(w, http.StatusOK, map[string]any{"data": insights.SummaryData{}})
		return
	}
	summary, err := s.insightsStore.Summary(r.Context(), parseInsightsQuery(r, 30))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": summary})
}

func (s *Server) handleInsightsDashboard(w http.ResponseWriter, r *http.Request) {
	if s.insightsStore == nil {
		writeJSON(w, http.StatusOK, map[string]any{"data": insights.DashboardData{}})
		return
	}
	dashboard, err := s.insightsStore.Dashboard(r.Context(), parseInsightsQuery(r, 30))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": dashboard})
}

func (s *Server) handleWorkflowStats(w http.ResponseWriter, r *http.Request) {
	if s.insightsStore == nil {
		writeError(w, http.StatusNotFound, "insights are not configured")
		return
	}
	stat, err := s.insightsStore.WorkflowStats(r.Context(), chi.URLParam(r, "id"), parseInsightsQuery(r, 30))
	if err != nil {
		if err == persistence.ErrNotFound {
			writeError(w, http.StatusNotFound, "workflow statistics not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": stat})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if s.insightsStore == nil && s.activeExecutions == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	if s.insightsStore != nil {
		summary, err := s.insightsStore.Summary(r.Context(), insights.Query{})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		_, _ = fmt.Fprintf(w, "n8n_executions_total %d\n", summary.TotalExecutions)
		_, _ = fmt.Fprintf(w, "n8n_executions_success_total %d\n", summary.SuccessfulCount)
		_, _ = fmt.Fprintf(w, "n8n_executions_error_total %d\n", summary.FailedCount)
		_, _ = fmt.Fprintf(w, "n8n_workflows_total %d\n", summary.TotalWorkflows)
		_, _ = fmt.Fprintf(w, "n8n_workflows_active %d\n", summary.ActiveWorkflows)
		_, _ = fmt.Fprintf(w, "n8n_execution_average_duration_ms %.3f\n", summary.AvgRunDurationMS)
	}
	if s.activeExecutions != nil {
		for name, value := range s.activeExecutions.Metrics() {
			_, _ = fmt.Fprintf(w, "%s %.3f\n", name, value)
		}
	}
}

func parseInsightsQuery(r *http.Request, defaultDays int) insights.Query {
	query := insights.Query{
		WorkflowID: r.URL.Query().Get("workflowId"),
		GroupBy:    r.URL.Query().Get("groupBy"),
		Limit:      intQuery(r, "limit", 10),
	}
	if start := firstQuery(r, "start", "startDate"); start != "" {
		if parsed, err := time.Parse(time.RFC3339, start); err == nil {
			query.StartDate = &parsed
		}
	}
	if end := firstQuery(r, "end", "endDate"); end != "" {
		if parsed, err := time.Parse(time.RFC3339, end); err == nil {
			query.EndDate = &parsed
		}
	}
	if query.StartDate == nil && defaultDays > 0 {
		start := time.Now().UTC().AddDate(0, 0, -defaultDays)
		query.StartDate = &start
	}
	return query
}

func firstQuery(r *http.Request, keys ...string) string {
	for _, key := range keys {
		if value := r.URL.Query().Get(key); value != "" {
			return value
		}
	}
	return ""
}
