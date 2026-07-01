package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type dynamicNodeParameterOptionsRequest struct {
	NodeTypeAndVersion struct {
		Name string `json:"name"`
	} `json:"nodeTypeAndVersion"`
	Path        string         `json:"path"`
	MethodName  string         `json:"methodName"`
	Credentials map[string]any `json:"credentials"`
}

type dynamicOption struct {
	Name        string `json:"name"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

func (s *Server) handleDynamicNodeParameterOptions(w http.ResponseWriter, r *http.Request) {
	var request dynamicNodeParameterOptionsRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	options, err := s.dynamicNodeParameterOptions(r.Context(), request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": options})
}

func (s *Server) dynamicNodeParameterOptions(ctx context.Context, request dynamicNodeParameterOptionsRequest) ([]dynamicOption, error) {
	switch request.NodeTypeAndVersion.Name {
	case "@n8n/n8n-nodes-langchain.lmChatGoogleGemini":
		if !strings.HasSuffix(request.Path, "modelName") {
			return []dynamicOption{}, nil
		}
		if credentialID := dynamicCredentialID(request.Credentials, "googlePalmApi"); credentialID != "" {
			if options, err := s.googleGeminiModelOptions(ctx, credentialID); err == nil && len(options) > 0 {
				return options, nil
			}
		}
		return fallbackGeminiModelOptions(), nil
	case "@n8n/n8n-nodes-langchain.lmChatDeepSeek":
		if !strings.HasSuffix(request.Path, "model") {
			return []dynamicOption{}, nil
		}
		if credentialID := dynamicCredentialID(request.Credentials, "deepSeekApi"); credentialID != "" {
			if options, err := s.openAICompatibleModelOptions(ctx, credentialID, "https://api.deepseek.com"); err == nil && len(options) > 0 {
				return options, nil
			}
		}
		return []dynamicOption{{Name: "deepseek-chat", Value: "deepseek-chat"}, {Name: "deepseek-reasoner", Value: "deepseek-reasoner"}}, nil
	case "@n8n/n8n-nodes-langchain.lmChatOpenRouter":
		if !strings.HasSuffix(request.Path, "model") {
			return []dynamicOption{}, nil
		}
		if credentialID := dynamicCredentialID(request.Credentials, "openRouterApi"); credentialID != "" {
			if options, err := s.openAICompatibleModelOptions(ctx, credentialID, "https://openrouter.ai/api/v1"); err == nil && len(options) > 0 {
				return options, nil
			}
		}
		return []dynamicOption{
			{Name: "openai/gpt-4.1-mini", Value: "openai/gpt-4.1-mini"},
			{Name: "openai/gpt-4.1", Value: "openai/gpt-4.1"},
			{Name: "anthropic/claude-sonnet-4", Value: "anthropic/claude-sonnet-4"},
		}, nil
	default:
		return []dynamicOption{}, nil
	}
}

func (s *Server) googleGeminiModelOptions(ctx context.Context, credentialID string) ([]dynamicOption, error) {
	if s.credentialStore == nil {
		return nil, fmt.Errorf("credential store is unavailable")
	}
	row, err := s.credentialStore.GetByID(ctx, credentialID)
	if err != nil {
		return nil, err
	}
	credential, err := s.decryptCredentialData(row.Data)
	if err != nil {
		return nil, err
	}
	apiKey := stringFromMap(credential, "apiKey")
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("googlePalmApi apiKey is required")
	}
	host := firstNonEmpty(stringFromMap(credential, "host"), "https://generativelanguage.googleapis.com")
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	endpoint := strings.TrimRight(host, "/") + "/v1beta/models?key=" + url.QueryEscape(apiKey)
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Gemini model list HTTP %d", resp.StatusCode)
	}
	var decoded struct {
		Models []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, err
	}
	options := make([]dynamicOption, 0, len(decoded.Models))
	for _, model := range decoded.Models {
		name := strings.TrimSpace(model.Name)
		if name == "" || strings.Contains(strings.ToLower(name), "embedding") {
			continue
		}
		options = append(options, dynamicOption{Name: name, Value: name, Description: model.Description})
	}
	sort.Slice(options, func(i, j int) bool { return options[i].Name < options[j].Name })
	return options, nil
}

func (s *Server) openAICompatibleModelOptions(ctx context.Context, credentialID string, defaultBaseURL string) ([]dynamicOption, error) {
	if s.credentialStore == nil {
		return nil, fmt.Errorf("credential store is unavailable")
	}
	row, err := s.credentialStore.GetByID(ctx, credentialID)
	if err != nil {
		return nil, err
	}
	credential, err := s.decryptCredentialData(row.Data)
	if err != nil {
		return nil, err
	}
	apiKey := stringFromMap(credential, "apiKey")
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("provider apiKey is required")
	}
	baseURL := firstNonEmpty(stringFromMap(credential, "url"), defaultBaseURL)
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("model list HTTP %d", resp.StatusCode)
	}
	var decoded struct {
		Data []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, err
	}
	options := make([]dynamicOption, 0, len(decoded.Data))
	for _, model := range decoded.Data {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		name := firstNonEmpty(strings.TrimSpace(model.Name), id)
		options = append(options, dynamicOption{Name: name, Value: id, Description: model.Description})
	}
	sort.Slice(options, func(i, j int) bool { return options[i].Name < options[j].Name })
	return options, nil
}

func dynamicCredentialID(credentials map[string]any, names ...string) string {
	for _, name := range names {
		if id := dynamicCredentialEntryID(credentials[name]); id != "" {
			return id
		}
	}
	for _, raw := range credentials {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		entryType := strings.TrimSpace(fmt.Sprint(entry["type"]))
		for _, name := range names {
			if entryType == name {
				return dynamicCredentialEntryID(entry)
			}
		}
	}
	return ""
}

func dynamicCredentialEntryID(raw any) string {
	entry, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	value, ok := entry["id"]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func fallbackGeminiModelOptions() []dynamicOption {
	return []dynamicOption{
		{Name: "models/gemini-1.5-flash", Value: "models/gemini-1.5-flash"},
		{Name: "models/gemini-1.5-pro", Value: "models/gemini-1.5-pro"},
		{Name: "models/gemini-2.0-flash", Value: "models/gemini-2.0-flash"},
		{Name: "models/gemini-2.5-flash", Value: "models/gemini-2.5-flash"},
		{Name: "models/gemini-2.5-pro", Value: "models/gemini-2.5-pro"},
	}
}
