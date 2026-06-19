package shopify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Error struct {
	Errors any `json:"errors"`
}

func (e *Error) Error() string {
	switch typed := e.Errors.(type) {
	case string:
		return "shopify: " + typed
	case map[string]any:
		parts := []string{}
		for key, value := range typed {
			parts = append(parts, fmt.Sprintf("%s: %v", key, value))
		}
		return "shopify: " + strings.Join(parts, "; ")
	default:
		return fmt.Sprintf("shopify: %v", e.Errors)
	}
}

func (n *Node) do(ctx context.Context, cred Credential, method string, path string, body any) (*http.Response, error) {
	for attempt := 0; attempt < 3; attempt++ {
		var reader io.Reader
		if body != nil {
			data, err := json.Marshal(body)
			if err != nil {
				return nil, err
			}
			reader = bytes.NewReader(data)
		}
		request, err := http.NewRequestWithContext(ctx, method, n.requestURL(cred, path), reader)
		if err != nil {
			return nil, err
		}
		request.Header.Set("X-Shopify-Access-Token", cred.AuthHeader())
		request.Header.Set("Accept", "application/json")
		if body != nil {
			request.Header.Set("Content-Type", "application/json")
		}
		response, err := n.client.Do(request)
		if err != nil {
			return nil, fmt.Errorf("HTTP %s %s: %w", method, path, err)
		}
		if response.StatusCode != http.StatusTooManyRequests {
			return response, nil
		}
		response.Body.Close()
		wait := 2 * time.Second
		if retryAfter := response.Header.Get("Retry-After"); retryAfter != "" {
			if seconds, err := strconv.ParseFloat(retryAfter, 64); err == nil {
				wait = time.Duration(seconds * float64(time.Second))
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
	return nil, fmt.Errorf("shopify rate limited")
}

func (n *Node) doJSON(ctx context.Context, cred Credential, method string, path string, body any) (map[string]any, error) {
	response, err := n.do(ctx, cred, method, path, body)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 16*1024*1024))
	if err != nil {
		return nil, err
	}
	if response.StatusCode == http.StatusNoContent {
		return map[string]any{"success": true}, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var apiError Error
		if len(bytes.TrimSpace(data)) > 0 && json.Unmarshal(data, &apiError) == nil && apiError.Errors != nil {
			return nil, &apiError
		}
		return nil, fmt.Errorf("shopify HTTP %d: %s", response.StatusCode, string(data))
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]any{"success": true}, nil
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (n *Node) requestURL(cred Credential, path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	baseURL := n.baseURL
	if baseURL == "" {
		baseURL = cred.BaseURL()
	}
	return strings.TrimRight(baseURL, "/") + path
}
