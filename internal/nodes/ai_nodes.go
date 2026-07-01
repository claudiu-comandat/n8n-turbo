package nodes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type StickyNote struct{}

func (StickyNote) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	return dataplane.EmptyOutput(), nil
}

type GoogleGeminiChatModel struct{}

func (GoogleGeminiChatModel) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	return dataplane.EmptyOutput(), nil
}

type DeepSeekChatModel struct{}

func (DeepSeekChatModel) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	return dataplane.EmptyOutput(), nil
}

type OpenRouterChatModel struct{}

func (OpenRouterChatModel) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	return dataplane.EmptyOutput(), nil
}

type AIAgent struct{}

func (AIAgent) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	items := firstInput(in.InputData)
	if len(items) == 0 {
		return dataplane.EmptyOutput(), nil
	}
	models := connectedNodesByType(in.ConnectedNodes, "ai_languageModel")
	if len(models) == 0 {
		return nil, fmt.Errorf("AI Agent requires a connected ai_languageModel node")
	}
	if boolParam(in.Node.Parameters, "needsFallback", false) && len(models) < 2 {
		return nil, fmt.Errorf("Please connect a model to the Fallback Model input or disable the fallback option")
	}
	if boolParam(in.Node.Parameters, "hasOutputParser", false) && len(connectedNodesByType(in.ConnectedNodes, "ai_outputParser")) == 0 {
		return nil, fmt.Errorf("Please connect an output parser or disable the specific output format option")
	}
	output := make([]dataplane.Item, 0, len(items))
	for itemIndex, item := range items {
		prompt := aiAgentPrompt(in, items, itemIndex)
		if strings.TrimSpace(prompt) == "" {
			prompt = aiAgentFallbackPrompt(item)
		}
		systemMessage := aiAgentSystemMessage(in, items, itemIndex)
		text, err := executeAIAgentPrompt(ctx, prompt, systemMessage, models)
		if err != nil {
			return nil, err
		}
		next := cloneItem(item)
		next.JSON["output"] = text
		output = append(output, next)
	}
	return dataplane.MainOutput(output), nil
}

func connectedNodesByType(inputs map[string][][]engine.ConnectedNode, connectionType string) []engine.ConnectedNode {
	grouped := inputs[connectionType]
	if len(grouped) == 0 {
		return nil
	}
	result := make([]engine.ConnectedNode, 0, len(grouped))
	for _, entries := range grouped {
		result = append(result, entries...)
	}
	return result
}

func aiAgentPrompt(in engine.ExecuteInput, items []dataplane.Item, itemIndex int) string {
	params := in.Node.Parameters
	promptType := strings.ToLower(firstNonEmptyNode(stringParam(params, "promptType"), "define"))
	switch promptType {
	case "define":
		return fmt.Sprint(resolveValue(in, items, itemIndex, firstPresentValue(params, "text", "prompt", "input")))
	case "auto":
		item := items[itemIndex]
		return aiAgentFallbackPrompt(item)
	default:
		return fmt.Sprint(resolveValue(in, items, itemIndex, firstPresentValue(params, "text", "prompt", "input")))
	}
}

func aiAgentSystemMessage(in engine.ExecuteInput, items []dataplane.Item, itemIndex int) string {
	options, _ := rawObject(in.Node.Parameters["options"])
	value := firstPresentValue(options, "systemMessage")
	if value == nil {
		value = firstPresentValue(in.Node.Parameters, "systemMessage")
	}
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(resolveValue(in, items, itemIndex, value)))
}

func aiAgentFallbackPrompt(item dataplane.Item) string {
	for _, key := range []string{"chatInput", "input", "prompt", "text", "message"} {
		if value, ok := item.JSON[key]; ok && strings.TrimSpace(fmt.Sprint(value)) != "" {
			return fmt.Sprint(value)
		}
	}
	if len(item.JSON) == 0 {
		return ""
	}
	bytes, err := json.Marshal(item.JSON)
	if err != nil {
		return fmt.Sprint(item.JSON)
	}
	return string(bytes)
}

func executeAIAgentPrompt(ctx context.Context, prompt string, systemMessage string, models []engine.ConnectedNode) (string, error) {
	var lastErr error
	for _, model := range models {
		text, err := executeConnectedChatModel(ctx, prompt, systemMessage, model)
		if err == nil {
			return text, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("AI Agent has no usable connected model")
	}
	return "", lastErr
}

func executeConnectedChatModel(ctx context.Context, prompt string, systemMessage string, model engine.ConnectedNode) (string, error) {
	switch model.Node.Type {
	case "@n8n/n8n-nodes-langchain.lmChatGoogleGemini":
		return executeGeminiPrompt(ctx, prompt, systemMessage, model)
	case "@n8n/n8n-nodes-langchain.lmChatDeepSeek":
		return executeOpenAICompatiblePrompt(ctx, prompt, systemMessage, model, "deepSeekApi", "https://api.deepseek.com", "deepseek-chat")
	case "@n8n/n8n-nodes-langchain.lmChatOpenRouter":
		return executeOpenAICompatiblePrompt(ctx, prompt, systemMessage, model, "openRouterApi", "https://openrouter.ai/api/v1", "openai/gpt-4.1-mini")
	default:
		if credentialByType(model.Credentials, "googlePalmApi") != nil {
			return executeGeminiPrompt(ctx, prompt, systemMessage, model)
		}
		if credentialByType(model.Credentials, "deepSeekApi") != nil {
			return executeOpenAICompatiblePrompt(ctx, prompt, systemMessage, model, "deepSeekApi", "https://api.deepseek.com", "deepseek-chat")
		}
		if credentialByType(model.Credentials, "openRouterApi") != nil {
			return executeOpenAICompatiblePrompt(ctx, prompt, systemMessage, model, "openRouterApi", "https://openrouter.ai/api/v1", "openai/gpt-4.1-mini")
		}
		return "", fmt.Errorf("node %s uses unsupported chat model type %s", model.Node.Name, model.Node.Type)
	}
}

func executeGeminiPrompt(ctx context.Context, prompt string, systemMessage string, model engine.ConnectedNode) (string, error) {
	credential := credentialByType(model.Credentials, "googlePalmApi")
	if credential == nil {
		return "", fmt.Errorf("node %s is not configured with googlePalmApi credentials", model.Node.Name)
	}
	apiKey := credentialString(credential, "apiKey")
	if strings.TrimSpace(apiKey) == "" {
		return "", fmt.Errorf("node %s is missing googlePalmApi.apiKey", model.Node.Name)
	}
	host := firstNonEmptyNode(credentialString(credential, "host"), "https://generativelanguage.googleapis.com")
	modelName := aiModelName(model.Node.Parameters)
	if modelName == "" {
		modelName = "models/gemini-2.5-flash"
	}
	endpoint := strings.TrimRight(host, "/") + "/v1beta/" + strings.TrimLeft(modelName, "/") + ":generateContent?key=" + apiKey
	body := map[string]any{
		"contents": []map[string]any{{
			"role":  "user",
			"parts": []map[string]any{{"text": prompt}},
		}},
	}
	if generationConfig := aiGeminiGenerationConfig(model.Node.Parameters); len(generationConfig) > 0 {
		body["generationConfig"] = generationConfig
	}
	if safetySettings := aiGeminiSafetySettings(model.Node.Parameters); len(safetySettings) > 0 {
		body["safetySettings"] = safetySettings
	}
	if strings.TrimSpace(systemMessage) != "" {
		body["systemInstruction"] = map[string]any{
			"parts": []map[string]any{{"text": systemMessage}},
		}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("Gemini HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var decoded geminiGenerateContentResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", err
	}
	text := strings.TrimSpace(decoded.Text())
	if text == "" {
		return "", fmt.Errorf("Gemini returned an empty response")
	}
	return text, nil
}

func executeOpenAICompatiblePrompt(ctx context.Context, prompt string, systemMessage string, model engine.ConnectedNode, credentialType string, defaultBaseURL string, defaultModel string) (string, error) {
	credential := credentialByType(model.Credentials, credentialType)
	if credential == nil {
		return "", fmt.Errorf("node %s is not configured with %s credentials", model.Node.Name, credentialType)
	}
	apiKey := credentialString(credential, "apiKey")
	if strings.TrimSpace(apiKey) == "" {
		return "", fmt.Errorf("node %s is missing %s.apiKey", model.Node.Name, credentialType)
	}
	baseURL := firstNonEmptyNode(credentialString(credential, "url", "baseUrl"), defaultBaseURL)
	modelName := aiModelName(model.Node.Parameters)
	if modelName == "" {
		modelName = defaultModel
	}
	body := map[string]any{
		"model": modelName,
		"messages": []map[string]any{
			{"role": "user", "content": prompt},
		},
	}
	if strings.TrimSpace(systemMessage) != "" {
		body["messages"] = []map[string]any{
			{"role": "system", "content": systemMessage},
			{"role": "user", "content": prompt},
		}
	}
	if options := aiOpenAICompatibleOptions(model.Node.Parameters); len(options) > 0 {
		for key, value := range options {
			body[key] = value
		}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("%s HTTP %d: %s", model.Node.Name, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var decoded openAICompatibleChatResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", err
	}
	text := strings.TrimSpace(decoded.Text())
	if text == "" {
		return "", fmt.Errorf("%s returned an empty response", model.Node.Name)
	}
	return text, nil
}

func aiOpenAICompatibleOptions(params map[string]any) map[string]any {
	options, _ := rawObject(params["options"])
	result := map[string]any{}
	if value, ok := intOption(options, "maxTokens"); ok && value >= 0 {
		result["max_tokens"] = value
	}
	if value, ok := floatOption(options, "temperature"); ok {
		result["temperature"] = value
	}
	if value, ok := floatOption(options, "topP"); ok {
		result["top_p"] = value
	}
	if value, ok := floatOption(options, "frequencyPenalty"); ok {
		result["frequency_penalty"] = value
	}
	if value, ok := floatOption(options, "presencePenalty"); ok {
		result["presence_penalty"] = value
	}
	if format := strings.TrimSpace(stringParam(options, "responseFormat")); format != "" && format != "text" {
		result["response_format"] = map[string]any{"type": format}
	}
	return result
}

func aiModelName(params map[string]any) string {
	value := firstPresentValue(params, "modelName", "model")
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		return strings.TrimSpace(firstNonEmptyNode(
			stringParam(typed, "value"),
			stringParam(typed, "model"),
			stringParam(typed, "name"),
		))
	default:
		if value == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func aiGeminiGenerationConfig(params map[string]any) map[string]any {
	options, _ := rawObject(params["options"])
	config := map[string]any{}
	if value, ok := intOption(options, "maxOutputTokens"); ok {
		config["maxOutputTokens"] = value
	}
	if value, ok := floatOption(options, "temperature"); ok {
		config["temperature"] = value
	}
	if value, ok := floatOption(options, "topK"); ok {
		config["topK"] = value
	}
	if value, ok := floatOption(options, "topP"); ok {
		config["topP"] = value
	}
	return config
}

func aiGeminiSafetySettings(params map[string]any) []map[string]any {
	options, _ := rawObject(params["options"])
	safety, _ := rawObject(options["safetySettings"])
	rawValues := safety["values"]
	if rawValues == nil {
		return nil
	}
	values := []map[string]any{}
	switch typed := rawValues.(type) {
	case []any:
		for _, entry := range typed {
			if value, ok := aiSafetySetting(entry); ok {
				values = append(values, value)
			}
		}
	default:
		if value, ok := aiSafetySetting(rawValues); ok {
			values = append(values, value)
		}
	}
	return values
}

func aiSafetySetting(raw any) (map[string]any, bool) {
	entry, ok := rawObject(raw)
	if !ok {
		return nil, false
	}
	category := strings.TrimSpace(stringParam(entry, "category"))
	threshold := strings.TrimSpace(stringParam(entry, "threshold"))
	if category == "" || threshold == "" {
		return nil, false
	}
	return map[string]any{"category": category, "threshold": threshold}, true
}

func intOption(params map[string]any, key string) (int, bool) {
	if params == nil {
		return 0, false
	}
	if _, ok := params[key]; !ok {
		return 0, false
	}
	return intParam(params, key, 0), true
}

func floatOption(params map[string]any, key string) (float64, bool) {
	if params == nil {
		return 0, false
	}
	value, ok := params[key]
	if !ok {
		return 0, false
	}
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	case string:
		var parsed float64
		if _, err := fmt.Sscan(typed, &parsed); err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

type geminiGenerateContentResponse struct {
	Candidates []geminiCandidate `json:"candidates"`
}

type openAICompatibleChatResponse struct {
	Choices []struct {
		Message struct {
			Content any `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func (r openAICompatibleChatResponse) Text() string {
	for _, choice := range r.Choices {
		switch content := choice.Message.Content.(type) {
		case string:
			if strings.TrimSpace(content) != "" {
				return content
			}
		case []any:
			parts := make([]string, 0, len(content))
			for _, part := range content {
				entry, ok := rawObject(part)
				if !ok {
					continue
				}
				text := strings.TrimSpace(stringParam(entry, "text"))
				if text != "" {
					parts = append(parts, text)
				}
			}
			if len(parts) > 0 {
				return strings.Join(parts, "")
			}
		}
	}
	return ""
}

type geminiCandidate struct {
	Content geminiContent `json:"content"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

func (r geminiGenerateContentResponse) Text() string {
	parts := make([]string, 0, len(r.Candidates))
	for _, candidate := range r.Candidates {
		for _, part := range candidate.Content.Parts {
			if strings.TrimSpace(part.Text) == "" {
				continue
			}
			parts = append(parts, part.Text)
		}
	}
	return strings.Join(parts, "")
}

func firstPresentValue(values map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
	}
	return nil
}
