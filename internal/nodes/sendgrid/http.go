package sendgrid

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func (n *Node) doJSON(ctx context.Context, cred Credential, method string, path string, body any) (map[string]any, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, n.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", cred.AuthHeader())
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := n.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusAccepted {
		return map[string]any{"success": true, "messageId": response.Header.Get("X-Message-Id")}, nil
	}
	if response.StatusCode == http.StatusNoContent {
		return map[string]any{"success": true}, nil
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 16*1024*1024))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var apiError Error
		if len(data) > 0 && json.Unmarshal(data, &apiError) == nil && len(apiError.Errors) > 0 {
			return nil, &apiError
		}
		return nil, fmt.Errorf("sendgrid HTTP %d: %s", response.StatusCode, string(data))
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]any{"success": true}, nil
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err == nil {
		return object, nil
	}
	var array []any
	if err := json.Unmarshal(data, &array); err == nil {
		return map[string]any{"result": array}, nil
	}
	return nil, fmt.Errorf("sendgrid decode response: %s", string(data))
}
