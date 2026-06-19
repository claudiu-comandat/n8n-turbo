package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/n8n-io/n8n-turbo/internal/secrets"
)

func (s *Server) resolvedSecretsRequest(r *http.Request) (map[string]map[string]string, error) {
	if s.secretsManager == nil {
		return map[string]map[string]string{}, nil
	}
	return s.secretsManager.Resolve(r.Context())
}

func (s *Server) resolvedSecrets(ctx context.Context) (map[string]map[string]string, error) {
	if s.secretsManager == nil {
		return map[string]map[string]string{}, nil
	}
	return s.secretsManager.Resolve(ctx)
}

func (s *Server) handleExternalSecretProviders(w http.ResponseWriter, r *http.Request) {
	if s.secretsManager == nil {
		writeJSON(w, http.StatusOK, map[string]any{"data": []any{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": s.secretsManager.Metadata(r.Context())})
}

func (s *Server) handleExternalSecretsList(w http.ResponseWriter, r *http.Request) {
	if s.secretsManager == nil {
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{}})
		return
	}
	resolved, err := s.secretsManager.Resolve(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	masked := map[string]any{}
	for provider, values := range resolved {
		providerValues := map[string]any{}
		for name := range values {
			providerValues[name] = "__n8n_MASKED_VALUE__"
		}
		masked[provider] = providerValues
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": masked})
}

func (s *Server) handleExternalSecretLookup(w http.ResponseWriter, r *http.Request) {
	if s.secretsManager == nil {
		writeError(w, http.StatusNotFound, "external secrets are not configured")
		return
	}
	_, err := s.secretsManager.GetSecret(r.Context(), chi.URLParam(r, "provider"), chi.URLParam(r, "name"))
	if err != nil {
		if errors.Is(err, secrets.ErrNotFound) {
			writeError(w, http.StatusNotFound, "secret not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"exists": true, "value": "__n8n_MASKED_VALUE__"}})
}
