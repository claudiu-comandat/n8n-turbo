package trello

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Error struct {
	Message string `json:"message"`
	Error   string `json:"error"`
}

func (e Error) Err() error {
	if e.Message != "" {
		return fmt.Errorf("trello: %s", e.Message)
	}
	return fmt.Errorf("trello: %s", e.Error)
}

func (n *Node) doJSON(ctx context.Context, cred Credential, method string, path string, body any) (map[string]any, error) {
	data, err := n.doRaw(ctx, cred, method, path, body, "application/json")
	if err != nil {
		return nil, err
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

func (n *Node) doArray(ctx context.Context, cred Credential, method string, path string, body any) ([]any, error) {
	data, err := n.doRaw(ctx, cred, method, path, body, "application/json")
	if err != nil {
		return nil, err
	}
	var result []any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (n *Node) doRaw(ctx context.Context, cred Credential, method string, path string, body any, contentType string) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, n.authURL(cred, path), reader)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", contentType)
	}
	response, err := n.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 32*1024*1024))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var trelloErr Error
		if json.Unmarshal(data, &trelloErr) == nil && (trelloErr.Message != "" || trelloErr.Error != "") {
			return nil, trelloErr.Err()
		}
		return nil, fmt.Errorf("trello HTTP %d: %s", response.StatusCode, string(data))
	}
	return data, nil
}

func (n *Node) authURL(cred Credential, path string) string {
	base := strings.TrimRight(n.baseURL, "/") + path
	separator := "?"
	if strings.Contains(base, "?") {
		separator = "&"
	}
	return base + separator + cred.AuthParams().Encode()
}
