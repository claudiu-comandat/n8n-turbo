package api

import "net/http"

func (s *Server) handleLogStreamEvents(w http.ResponseWriter, r *http.Request) {
	if s.logStream == nil {
		writeJSON(w, http.StatusOK, map[string]any{"data": []any{}, "total": 0})
		return
	}
	limit := intQuery(r, "limit", 100)
	events := s.logStream.List(limit)
	writeJSON(w, http.StatusOK, map[string]any{"data": events, "total": s.logStream.Count()})
}
