package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

func (n *Node) callAPI(ctx context.Context, cred *Credential, method string, body any, chatID string) (map[string]any, error) {
	if err := n.rateLimiter.Wait(ctx, chatID); err != nil {
		return nil, err
	}
	return n.callAPIWithRetry(ctx, cred, method, body)
}

func (n *Node) callAPIWithRetry(ctx context.Context, cred *Credential, method string, body any) (map[string]any, error) {
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		result, err := n.callJSON(ctx, cred, method, body)
		if err == nil {
			return result, nil
		}
		last = err
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.ErrorCode != http.StatusTooManyRequests {
			return nil, err
		}
		retryAfter := time.Duration(apiErr.Parameters.RetryAfter) * time.Second
		if retryAfter <= 0 {
			retryAfter = time.Duration(1<<attempt) * time.Second
		}
		timer := time.NewTimer(retryAfter)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, fmt.Errorf("telegram max retries exceeded: %w", last)
}

func (n *Node) callJSON(ctx context.Context, cred *Credential, method string, body any) (map[string]any, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal telegram request: %w", err)
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, cred.BaseURL()+"/"+method, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := n.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("telegram HTTP request: %w", err)
	}
	defer response.Body.Close()
	return decodeResponse(response)
}

func (n *Node) callMultipart(ctx context.Context, cred *Credential, method string, fields map[string]string, fileField string, fileName string, reader io.Reader) (map[string]any, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if value != "" {
			if err := writer.WriteField(key, value); err != nil {
				return nil, err
			}
		}
	}
	part, err := writer.CreateFormFile(fileField, fileName)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(part, reader); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, cred.BaseURL()+"/"+method, &body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := n.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("telegram multipart request: %w", err)
	}
	defer response.Body.Close()
	return decodeResponse(response)
}

func decodeResponse(response *http.Response) (map[string]any, error) {
	data, err := io.ReadAll(io.LimitReader(response.Body, 16*1024*1024))
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode telegram response status %d: %w", response.StatusCode, err)
	}
	if ok, _ := result["ok"].(bool); !ok {
		apiErr := &APIError{}
		_ = json.Unmarshal(data, apiErr)
		if apiErr.ErrorCode == 0 {
			apiErr.ErrorCode = response.StatusCode
		}
		if apiErr.Description == "" {
			apiErr.Description = strings.TrimSpace(string(data))
		}
		return nil, apiErr
	}
	return result, nil
}
