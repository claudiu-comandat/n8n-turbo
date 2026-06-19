package descriptor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func (e Executor) executeOpenAIStream(ctx context.Context, operation Operation, params map[string]any, credentials map[string]map[string]any) (dataplane.Output, error) {
	endpoint, err := e.endpoint(operation, params, credentials)
	if err != nil {
		return nil, err
	}
	body, contentType, err := requestBody(operation, params)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, operation.Method, endpoint, body)
	if err != nil {
		return nil, err
	}
	for key, value := range e.descriptor.DefaultHeaders {
		req.Header.Set(key, value)
	}
	for key, value := range operation.Headers {
		req.Header.Set(key, value)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "text/event-stream")
	if err := NewAuthInjector().Inject(req, credentialFromDescriptor(e.descriptor, credentials), e.descriptor.AuthType, e.descriptor.AuthConfig); err != nil {
		return nil, err
	}
	client := http.Client{Timeout: time.Duration(intValue(params, "timeout", 120000)) * time.Millisecond}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024))
		return nil, ParseOpenAIError(resp.StatusCode, raw)
	}
	content, model, finishReason, err := CollectOpenAISSE(resp.Body)
	if err != nil {
		return nil, err
	}
	return dataplane.MainOutput([]dataplane.Item{{JSON: map[string]any{"content": content, "role": "assistant", "finish_reason": finishReason, "model": model, "streamed": true}}}), nil
}

func CollectOpenAISSE(reader io.Reader) (string, string, string, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024), 16*1024*1024)
	var content strings.Builder
	var model string
	var finishReason string
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		chunk, err := ParseOpenAISSEChunk([]byte(data))
		if err != nil {
			continue
		}
		if model == "" {
			model = chunk.Model
		}
		if len(chunk.Choices) > 0 {
			content.WriteString(chunk.Choices[0].Delta.Content)
			if chunk.Choices[0].FinishReason != "" {
				finishReason = chunk.Choices[0].FinishReason
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", "", "", err
	}
	return content.String(), model, finishReason, nil
}

type OpenAISSEChunk struct {
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
			Role    string `json:"role"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

func ParseOpenAISSEChunk(data []byte) (OpenAISSEChunk, error) {
	var chunk OpenAISSEChunk
	if err := json.Unmarshal(data, &chunk); err != nil {
		return OpenAISSEChunk{}, err
	}
	return chunk, nil
}

func ParseOpenAIError(statusCode int, body []byte) error {
	var decoded struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    any    `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &decoded) == nil && decoded.Error.Message != "" {
		return fmt.Errorf("OpenAI error [%s] HTTP %d: %s", decoded.Error.Type, statusCode, decoded.Error.Message)
	}
	return fmt.Errorf("HTTP %d: %s", statusCode, string(body))
}
