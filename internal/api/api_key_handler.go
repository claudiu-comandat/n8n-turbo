package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/n8n-io/n8n-turbo/internal/auth"
)

const apiKeysSettingsKey = "publicApi.keys"

type publicAPIKey struct {
	ID        string         `json:"id"`
	Label     string         `json:"label"`
	APIKey    string         `json:"apiKey,omitempty"`
	CreatedAt string         `json:"createdAt"`
	UpdatedAt string         `json:"updatedAt"`
	Scopes    []string       `json:"scopes"`
	Owner     map[string]any `json:"owner"`
}

type apiKeysState struct {
	Keys []publicAPIKey `json:"keys"`
}

type apiKeyRequest struct {
	Label  string   `json:"label"`
	Scopes []string `json:"scopes"`
}

func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	state, err := s.loadAPIKeys(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	ownership := r.URL.Query().Get("ownership")
	label := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("label")))
	items := make([]publicAPIKey, 0, len(state.Keys))
	for _, key := range state.Keys {
		if ownership == "mine" && !apiKeyOwnedByRequestUser(key, r) {
			continue
		}
		if label != "" && !strings.Contains(strings.ToLower(key.Label), label) {
			continue
		}
		items = append(items, redactAPIKey(key))
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].CreatedAt > items[j].CreatedAt
	})

	totalMine := 0
	for _, key := range state.Keys {
		if apiKeyOwnedByRequestUser(key, r) {
			totalMine++
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"items": items,
			"counts": map[string]int{
				"mine": len(items),
				"all":  len(items),
			},
			"totals": map[string]int{
				"mine": totalMine,
				"all":  len(state.Keys),
			},
		},
	})
}

func (s *Server) handleAPIKeyScopes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": apiKeyScopes()})
}

func (s *Server) handleAPIKeyExists(w http.ResponseWriter, r *http.Request) {
	state, err := s.loadAPIKeys(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	targetID := strings.TrimSpace(chi.URLParam(r, "id"))
	targetLabel := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("label")))
	if targetLabel == "" {
		targetLabel = strings.ToLower(strings.TrimSpace(r.URL.Query().Get("name")))
	}

	exists := false
	for _, key := range state.Keys {
		if targetID != "" && key.ID == targetID {
			exists = true
			break
		}
		if targetLabel != "" && strings.ToLower(strings.TrimSpace(key.Label)) == targetLabel {
			exists = true
			break
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data":   map[string]any{"exists": exists},
		"exists": exists,
	})
}

func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var payload apiKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid api key body")
		return
	}
	state, err := s.loadAPIKeys(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	secret := "n8n_turbo_" + randomHex(32)
	key := publicAPIKey{
		ID:        "pak_" + randomHex(12),
		Label:     firstNonEmpty(strings.TrimSpace(payload.Label), "API key"),
		APIKey:    secret,
		CreatedAt: now,
		UpdatedAt: now,
		Scopes:    normalizeAPIKeyScopes(payload.Scopes),
		Owner:     apiKeyOwner(r),
	}
	state.Keys = append(state.Keys, key)
	if err := s.saveAPIKeys(r, state); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": key})
}

func (s *Server) handleUpdateAPIKey(w http.ResponseWriter, r *http.Request) {
	var payload apiKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid api key body")
		return
	}
	state, err := s.loadAPIKeys(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	id := chi.URLParam(r, "id")
	for i := range state.Keys {
		if state.Keys[i].ID != id {
			continue
		}
		if strings.TrimSpace(payload.Label) != "" {
			state.Keys[i].Label = strings.TrimSpace(payload.Label)
		}
		if payload.Scopes != nil {
			state.Keys[i].Scopes = normalizeAPIKeyScopes(payload.Scopes)
		}
		state.Keys[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if err := s.saveAPIKeys(r, state); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": redactAPIKey(state.Keys[i])})
		return
	}
	writeError(w, http.StatusNotFound, "api key not found")
}

func (s *Server) handleDeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	state, err := s.loadAPIKeys(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	id := chi.URLParam(r, "id")
	next := state.Keys[:0]
	deleted := false
	for _, key := range state.Keys {
		if key.ID == id {
			deleted = true
			continue
		}
		next = append(next, key)
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "api key not found")
		return
	}
	state.Keys = next
	if err := s.saveAPIKeys(r, state); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": true})
}

func (s *Server) loadAPIKeys(r *http.Request) (apiKeysState, error) {
	if s.settingsStore == nil {
		return apiKeysState{Keys: []publicAPIKey{}}, nil
	}
	raw, err := s.settingsStore.Get(r.Context(), apiKeysSettingsKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return apiKeysState{Keys: []publicAPIKey{}}, nil
	}
	var state apiKeysState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return apiKeysState{Keys: []publicAPIKey{}}, nil
	}
	if state.Keys == nil {
		state.Keys = []publicAPIKey{}
	}
	return state, nil
}

func (s *Server) saveAPIKeys(r *http.Request, state apiKeysState) error {
	if s.settingsStore == nil {
		return nil
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return s.settingsStore.Set(r.Context(), apiKeysSettingsKey, string(data))
}

func redactAPIKey(key publicAPIKey) publicAPIKey {
	if key.APIKey != "" {
		key.APIKey = "******" + lastString(key.APIKey, 4)
	}
	return key
}

func normalizeAPIKeyScopes(scopes []string) []string {
	allowed := map[string]bool{}
	for _, scope := range apiKeyScopes() {
		allowed[scope] = true
	}
	if len(scopes) == 0 {
		scopes = apiKeyScopes()
	}
	result := make([]string, 0, len(scopes))
	seen := map[string]bool{}
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" || !allowed[scope] || seen[scope] {
			continue
		}
		seen[scope] = true
		result = append(result, scope)
	}
	return result
}

func apiKeyScopes() []string {
	return []string{
		"workflow:create", "workflow:read", "workflow:update", "workflow:delete", "workflow:list",
		"workflow:execute", "credential:create", "credential:read", "credential:update", "credential:delete",
		"credential:list", "execution:read", "execution:delete", "tag:create", "tag:read", "tag:update",
		"tag:delete", "tag:list", "variable:create", "variable:read", "variable:update", "variable:delete",
		"variable:list",
	}
}

func apiKeyOwner(r *http.Request) map[string]any {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		return map[string]any{"id": "owner", "email": "owner@n8n.local", "firstName": "Owner", "lastName": "User"}
	}
	return map[string]any{"id": user.ID, "email": user.Email, "firstName": user.FirstName, "lastName": user.LastName}
}

func apiKeyOwnedByRequestUser(key publicAPIKey, r *http.Request) bool {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		return true
	}
	id, _ := key.Owner["id"].(string)
	return id == "" || id == user.ID
}

func lastString(value string, count int) string {
	if len(value) <= count {
		return value
	}
	return value[len(value)-count:]
}
