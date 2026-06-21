package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/n8n-io/n8n-turbo/internal/auth"
	"github.com/n8n-io/n8n-turbo/internal/descriptor"
	"github.com/n8n-io/n8n-turbo/internal/metadata"
	"github.com/n8n-io/n8n-turbo/internal/nodes"
	"github.com/n8n-io/n8n-turbo/internal/persistence"
)

type credentialRequest struct {
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Type string         `json:"type"`
	Data map[string]any `json:"data"`
}

type credentialTestRequest struct {
	ID          string             `json:"id"`
	Type        string             `json:"type"`
	Data        map[string]any     `json:"data"`
	Credentials *credentialRequest `json:"credentials"`
}

type credentialTestResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

func (s *Server) handleListCredentials(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFromContext(r.Context())
	rows, err := s.credentialStore.List(r.Context(), user.ID, queryInt(r, "limit", 100))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rows = filterCredentialRows(rows, r.URL.Query().Get("type"), firstNonEmpty(r.URL.Query().Get("search"), r.URL.Query().Get("name")))
	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		result = append(result, s.credentialMeta(r, row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

func (s *Server) handleGetCredential(w http.ResponseWriter, r *http.Request) {
	row, err := s.credentialStore.GetByID(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !credentialCanAccess(*row, r.Context()) {
		writeError(w, http.StatusForbidden, "credential access denied")
		return
	}
	data, err := s.decryptCredentialData(row.Data)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	result := s.credentialMeta(r, *row)
	result["data"] = maskSensitive(data)
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

func (s *Server) handleNewCredentialName(w http.ResponseWriter, r *http.Request) {
	base := strings.TrimSpace(r.URL.Query().Get("name"))
	if base == "" {
		base = "Unnamed credential"
	}
	name := base
	user, _ := auth.UserFromContext(r.Context())
	rows, err := s.credentialStore.List(r.Context(), user.ID, 1000)
	if err == nil {
		existing := make(map[string]struct{}, len(rows))
		for _, row := range rows {
			existing[strings.ToLower(strings.TrimSpace(row.Name))] = struct{}{}
		}
		if _, found := existing[strings.ToLower(name)]; found {
			for index := 2; ; index++ {
				candidate := fmt.Sprintf("%s %d", base, index)
				if _, taken := existing[strings.ToLower(candidate)]; !taken {
					name = candidate
					break
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{"name": name},
		"name": name,
	})
}

func (s *Server) handleSaveCredential(w http.ResponseWriter, r *http.Request) {
	var payload credentialRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid credential body")
		return
	}
	if id := chi.URLParam(r, "id"); id != "" {
		payload.ID = id
	}
	if payload.Name == "" || payload.Type == "" {
		writeError(w, http.StatusBadRequest, "credential name and type are required")
		return
	}
	if _, ok := metadata.CredentialTypeByName(payload.Type); !ok {
		writeError(w, http.StatusBadRequest, "credential type is not supported")
		return
	}
	if payload.ID != "" {
		existing, err := s.credentialStore.GetByID(r.Context(), payload.ID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if !credentialCanAccess(*existing, r.Context()) {
			writeError(w, http.StatusForbidden, "credential access denied")
			return
		}
		existingData, err := s.decryptCredentialData(existing.Data)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		payload.Data = mergeMaskedCredentialData(payload.Data, existingData)
	}
	encrypted, err := s.encryptCredentialData(payload.Data)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	user, _ := auth.UserFromContext(r.Context())
	row, err := s.credentialStore.Save(r.Context(), persistence.CredentialRow{
		ID:      payload.ID,
		Name:    payload.Name,
		Type:    payload.Type,
		Data:    encrypted,
		OwnerID: user.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	result := s.credentialMeta(r, *row)
	result["data"] = maskSensitive(payload.Data)
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

func (s *Server) handleDeleteCredential(w http.ResponseWriter, r *http.Request) {
	row, err := s.credentialStore.GetByID(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !credentialCanAccess(*row, r.Context()) {
		writeError(w, http.StatusForbidden, "credential access denied")
		return
	}
	if err := s.credentialStore.Delete(r.Context(), row.ID); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": true})
}

func (s *Server) handleTestCredential(w http.ResponseWriter, r *http.Request) {
	payload := credentialTestRequest{ID: chi.URLParam(r, "id")}
	if r.Body != nil && r.Body != http.NoBody {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1024*1024))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid credential test body")
			return
		}
		if len(strings.TrimSpace(string(body))) > 0 {
			if err := json.Unmarshal(body, &payload); err != nil {
				writeError(w, http.StatusBadRequest, "invalid credential test body")
				return
			}
			if id := chi.URLParam(r, "id"); id != "" {
				payload.ID = id
			}
		}
	}
	if payload.Credentials != nil {
		if payload.ID == "" {
			payload.ID = payload.Credentials.ID
		}
		if payload.Type == "" {
			payload.Type = payload.Credentials.Type
		}
		if payload.Data == nil {
			payload.Data = payload.Credentials.Data
		}
	}
	credentialType := payload.Type
	data := payload.Data
	if payload.ID != "" {
		row, err := s.credentialStore.GetByID(r.Context(), payload.ID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if !credentialCanAccess(*row, r.Context()) {
			writeError(w, http.StatusForbidden, "credential access denied")
			return
		}
		credentialType = row.Type
		data, err = s.decryptCredentialData(row.Data)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"data": credentialTestResult{Success: false, Status: "Error", Message: err.Error()}})
			return
		}
	}
	if credentialType == "" || data == nil {
		writeError(w, http.StatusBadRequest, "credential id or type and data are required")
		return
	}
	result := s.runCredentialTest(r.Context(), credentialType, data)
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

func (s *Server) handleCredentialTypes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": metadata.CredentialTypes()})
}

func (s *Server) handleCredentialType(w http.ResponseWriter, r *http.Request) {
	credential, ok := metadata.CredentialTypeByName(chi.URLParam(r, "name"))
	if !ok {
		writeError(w, http.StatusNotFound, "credential type not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": credential})
}

func (s *Server) credentialMeta(r *http.Request, row persistence.CredentialRow) map[string]any {
	return map[string]any{
		"id":                 row.ID,
		"name":               row.Name,
		"type":               row.Type,
		"createdAt":          row.CreatedAt,
		"updatedAt":          row.UpdatedAt,
		"scopes":             frontendCredentialScopes(),
		"homeProject":        projectListItem(s.personalProject(r)),
		"sharedWithProjects": []map[string]any{},
		"isManaged":          false,
		"isGlobal":           false,
	}
}

func frontendCredentialScopes() []string {
	return []string{
		"credential:create",
		"credential:read",
		"credential:update",
		"credential:delete",
		"credential:list",
		"credential:move",
		"credential:share",
		"credential:unshare",
		"credential:shareGlobally",
	}
}

func credentialCanAccess(row persistence.CredentialRow, ctx context.Context) bool {
	if row.OwnerID == "" {
		return true
	}
	user, ok := auth.UserFromContext(ctx)
	return !ok || user.ID == "" || row.OwnerID == user.ID
}

func (s *Server) encryptCredentialData(data map[string]any) (json.RawMessage, error) {
	if data == nil {
		data = map[string]any{}
	}
	plain, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	encrypted, err := s.vault.Encrypt(string(plain))
	if err != nil {
		return nil, err
	}
	return json.Marshal(encrypted)
}

func (s *Server) decryptCredentialData(raw json.RawMessage) (map[string]any, error) {
	var encrypted string
	if err := json.Unmarshal(raw, &encrypted); err != nil {
		return map[string]any{}, nil
	}
	plain, err := s.vault.Decrypt(encrypted)
	if err != nil {
		return nil, err
	}
	result := map[string]any{}
	if err := json.Unmarshal([]byte(plain), &result); err != nil {
		return nil, err
	}
	return result, nil
}

func maskSensitive(data map[string]any) map[string]any {
	result := make(map[string]any, len(data))
	for key, value := range data {
		if isSensitiveKey(key) {
			result[key] = maskedCredentialValue()
			continue
		}
		if nested, ok := value.(map[string]any); ok {
			result[key] = maskSensitive(nested)
			continue
		}
		result[key] = value
	}
	return result
}

func mergeMaskedCredentialData(incoming map[string]any, existing map[string]any) map[string]any {
	if incoming == nil {
		return existing
	}
	result := make(map[string]any, len(incoming))
	for key, value := range incoming {
		if text, ok := value.(string); ok && isMaskedCredentialValue(text) {
			if existingValue, exists := existing[key]; exists {
				result[key] = existingValue
				continue
			}
		}
		nestedIncoming, incomingIsMap := value.(map[string]any)
		nestedExisting, existingIsMap := existing[key].(map[string]any)
		if incomingIsMap && existingIsMap {
			result[key] = mergeMaskedCredentialData(nestedIncoming, nestedExisting)
			continue
		}
		result[key] = value
	}
	return result
}

func maskedCredentialValue() string {
	return "__n8n_EMPTY_VALUE_7b1af746-3729-4c60-9b9b-e08eb29e58da"
}

func isMaskedCredentialValue(value string) bool {
	return value == maskedCredentialValue() || value == "__n8n_MASKED_VALUE__"
}

func filterCredentialRows(rows []persistence.CredentialRow, credentialType string, search string) []persistence.CredentialRow {
	credentialType = strings.TrimSpace(credentialType)
	search = strings.ToLower(strings.TrimSpace(search))
	if credentialType == "" && search == "" {
		return rows
	}
	result := make([]persistence.CredentialRow, 0, len(rows))
	for _, row := range rows {
		if credentialType != "" && row.Type != credentialType {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(row.Name), search) {
			continue
		}
		result = append(result, row)
	}
	return result
}

func (s *Server) runCredentialTest(ctx context.Context, credentialType string, data map[string]any) credentialTestResult {
	credential, ok := metadata.CredentialTypeByName(credentialType)
	if !ok {
		return credentialTestResult{Success: false, Status: "Error", Message: "credential type is not supported"}
	}
	if err := validateCredentialData(credential, data); err != nil {
		return credentialTestResult{Success: false, Status: "Error", Message: err.Error()}
	}
	if credentialType == "postgres" {
		if err := nodes.PostgresTestConnection(ctx, data); err != nil {
			return credentialTestResult{Success: false, Status: "Error", Message: err.Error()}
		}
		return credentialTestResult{Success: true, Status: "OK", Message: "Connection tested successfully"}
	}
	desc, operation, ok := credentialTestOperation(credentialType)
	if !ok {
		return credentialTestResult{Success: true, Status: "OK", Message: "Credential schema validated"}
	}
	baseURL := credentialStringValue(data, "baseUrl", "testUrl", "_testBaseUrl")
	if baseURL == "" {
		baseURL = desc.BaseURL
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(operation.Path, "/")
	req, err := http.NewRequestWithContext(ctx, operation.Method, endpoint, nil)
	if err != nil {
		return credentialTestResult{Success: false, Status: "Error", Message: "could not create credential test request"}
	}
	for key, value := range desc.DefaultHeaders {
		req.Header.Set(key, value)
	}
	if err := descriptor.NewAuthInjector().Inject(req, data, desc.AuthType, desc.AuthConfig); err != nil {
		return credentialTestResult{Success: false, Status: "Error", Message: err.Error()}
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return credentialTestResult{Success: false, Status: "Error", Message: err.Error()}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if resp.StatusCode >= 400 {
		return credentialTestResult{Success: false, Status: "Error", Message: fmt.Sprintf("credential test returned HTTP %d", resp.StatusCode)}
	}
	if err := descriptorAPIError(body, desc.ErrorCheck); err != nil {
		return credentialTestResult{Success: false, Status: "Error", Message: err.Error()}
	}
	return credentialTestResult{Success: true, Status: "OK", Message: "Credential tested successfully"}
}

func validateCredentialData(credential metadata.CredentialType, data map[string]any) error {
	for _, prop := range credential.Properties {
		if prop.Default != "" || prop.Name == "organizationId" || prop.Name == "baseUrl" || prop.Name == "scope" || prop.Name == "refreshToken" {
			continue
		}
		if credentialStringValue(data, prop.Name) == "" {
			return fmt.Errorf("missing credential field %s", prop.Name)
		}
	}
	return nil
}

func credentialTestOperation(credentialType string) (descriptor.Descriptor, descriptor.Operation, bool) {
	for _, desc := range descriptor.Builtins() {
		if desc.CredentialType != credentialType {
			continue
		}
		operation, ok := desc.Operations["default"]
		if !ok || desc.BaseURL == "" || operation.Path == "" {
			return descriptor.Descriptor{}, descriptor.Operation{}, false
		}
		return desc, operation, true
	}
	return descriptor.Descriptor{}, descriptor.Operation{}, false
}

func descriptorAPIError(body []byte, check *descriptor.ErrorCheck) error {
	if check == nil || len(body) == 0 {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}
	value, ok := payload[check.Field]
	if !ok {
		return nil
	}
	if check.FalseIsError {
		if boolValue, ok := value.(bool); ok && !boolValue {
			message := credentialStringValue(payload, check.MessageField)
			if message == "" {
				message = "credential test failed"
			}
			return fmt.Errorf("%s", message)
		}
	}
	return nil
}

func credentialStringValue(data map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := data[key]
		if !ok || value == nil {
			continue
		}
		text := strings.TrimSpace(fmt.Sprint(value))
		if text != "" {
			return text
		}
	}
	return ""
}

func isSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, token := range []string{"password", "secret", "token", "key", "credential", "value"} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}
