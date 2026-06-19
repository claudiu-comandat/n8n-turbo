package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/n8n-io/n8n-turbo/internal/persistence"
)

type tagRequest struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (s *Server) handleListTags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.tagStore.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": tags})
}

func (s *Server) handleGetTag(w http.ResponseWriter, r *http.Request) {
	tag, err := s.tagStore.GetByID(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": tag})
}

func (s *Server) handleSaveTag(w http.ResponseWriter, r *http.Request) {
	var payload tagRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid tag body")
		return
	}
	if id := chi.URLParam(r, "id"); id != "" {
		payload.ID = id
	}
	if payload.Name == "" {
		writeError(w, http.StatusBadRequest, "tag name is required")
		return
	}
	tag, err := s.tagStore.Save(r.Context(), persistence.TagRow{ID: payload.ID, Name: payload.Name})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": tag})
}

func (s *Server) handleDeleteTag(w http.ResponseWriter, r *http.Request) {
	if err := s.tagStore.Delete(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": true})
}

func writeStoreError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if err == persistence.ErrNotFound {
		status = http.StatusNotFound
	}
	writeError(w, status, err.Error())
}
