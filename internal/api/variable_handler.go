package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/n8n-io/n8n-turbo/internal/persistence"
)

type variableRequest struct {
	ID    string  `json:"id"`
	Key   *string `json:"key"`
	Type  *string `json:"type"`
	Value *string `json:"value"`
}

func (s *Server) handleListVariables(w http.ResponseWriter, r *http.Request) {
	rows, err := s.variableStore.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	result := make([]persistence.VariableRow, 0, len(rows))
	for _, row := range rows {
		result = append(result, maskVariable(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

func (s *Server) handleGetVariable(w http.ResponseWriter, r *http.Request) {
	row, err := s.variableStore.GetByID(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": maskVariable(*row)})
}

func (s *Server) handleSaveVariable(w http.ResponseWriter, r *http.Request) {
	var payload variableRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid variable body")
		return
	}
	id := chi.URLParam(r, "id")
	created := id == ""
	if id == "" {
		id = payload.ID
	}
	row := persistence.VariableRow{ID: id, Type: "string"}
	if id != "" {
		existing, err := s.variableStore.GetByID(r.Context(), id)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		row = *existing
	}
	if payload.Key != nil {
		row.Key = *payload.Key
	}
	if payload.Type != nil {
		row.Type = *payload.Type
	}
	if payload.Value != nil {
		row.Value = *payload.Value
	}
	if row.Key == "" {
		writeError(w, http.StatusBadRequest, "variable key is required")
		return
	}
	if row.Type == "" {
		row.Type = "string"
	}
	if row.Type == "secret" && row.Value == "__n8n_MASKED_VALUE__" && id != "" {
		existing, err := s.variableStore.GetByID(r.Context(), id)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		row.Value = existing.Value
	}
	saved, err := s.variableStore.Save(r.Context(), row)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "already exists") {
			status = http.StatusConflict
		}
		writeError(w, status, err.Error())
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{"data": maskVariable(*saved)})
}

func (s *Server) handleDeleteVariable(w http.ResponseWriter, r *http.Request) {
	if err := s.variableStore.Delete(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": true})
}

func (s *Server) resolvedVariables(r *http.Request) (map[string]any, error) {
	return s.resolvedVariablesContext(r.Context())
}

func (s *Server) resolvedVariablesContext(ctx context.Context) (map[string]any, error) {
	if s.variableStore == nil {
		return map[string]any{}, nil
	}
	return s.variableStore.Resolve(ctx)
}

func maskVariable(row persistence.VariableRow) persistence.VariableRow {
	if row.Type == "secret" {
		row.Value = "__n8n_MASKED_VALUE__"
	}
	return row
}
