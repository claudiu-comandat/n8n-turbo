package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/audit"
)

func (s *Server) handleListAuditEvents(w http.ResponseWriter, r *http.Request) {
	if s.auditStore == nil {
		writeJSON(w, http.StatusOK, map[string]any{"data": []audit.Event{}, "total": 0})
		return
	}
	filter := audit.Filter{
		UserID:       r.URL.Query().Get("userId"),
		ResourceType: audit.ResourceType(r.URL.Query().Get("resourceType")),
		ResourceID:   r.URL.Query().Get("resourceId"),
		Limit:        intQuery(r, "limit", 20),
		Offset:       intQuery(r, "offset", 0),
	}
	for _, eventType := range r.URL.Query()["eventType"] {
		filter.EventTypes = append(filter.EventTypes, audit.EventType(eventType))
	}
	if start := timeQuery(r, "startDate"); start != nil {
		filter.StartDate = start
	}
	if end := timeQuery(r, "endDate"); end != nil {
		filter.EndDate = end
	}
	events, total, err := s.auditStore.List(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": events, "total": total})
}

func intQuery(r *http.Request, key string, fallback int) int {
	value := r.URL.Query().Get(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func timeQuery(r *http.Request, key string) *time.Time {
	value := r.URL.Query().Get(key)
	if value == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339, value)
	}
	if err != nil {
		return nil
	}
	return &parsed
}
