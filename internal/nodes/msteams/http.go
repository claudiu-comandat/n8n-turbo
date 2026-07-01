package msteams

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type GraphError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (e GraphError) ErrorMessage() error {
	return fmt.Errorf("Microsoft Graph API error [%s]: %s", e.Error.Code, e.Error.Message)
}

func (n *Node) doJSON(ctx context.Context, cred *Credential, method string, path string, body any) (map[string]any, error) {
	return n.doJSONWithHeaders(ctx, cred, method, path, body, nil)
}

func (n *Node) doJSONWithHeaders(ctx context.Context, cred *Credential, method string, path string, body any, headers map[string]string) (map[string]any, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, n.requestURL(path), reader)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", cred.AuthHeader())
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := n.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("HTTP %s %s: %w", method, path, err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return map[string]any{"success": true}, nil
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 16*1024*1024))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var graphErr GraphError
		if json.Unmarshal(data, &graphErr) == nil && graphErr.Error.Code != "" {
			return nil, graphErr.ErrorMessage()
		}
		return nil, fmt.Errorf("Microsoft Graph HTTP %d: %s", response.StatusCode, string(data))
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]any{"success": true}, nil
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return result, nil
}

func (n *Node) requestURL(path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return strings.TrimRight(n.baseURL, "/") + path
}
