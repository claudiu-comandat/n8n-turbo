package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

func (n *Node) do(ctx context.Context, cred Credential, method string, path string, body any) (*http.Response, error) {
	bucketID := method + ":" + path
	if err := n.rateLimiter.Wait(ctx, bucketID); err != nil {
		return nil, err
	}
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
	request.Header.Set("User-Agent", "n8n-turbo")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := n.client.Do(request)
	if err != nil {
		return nil, err
	}
	n.rateLimiter.Update(bucketID, response)
	if response.StatusCode == http.StatusTooManyRequests {
		defer response.Body.Close()
		retryAfter := response.Header.Get("Retry-After")
		waitSeconds, _ := strconv.ParseFloat(retryAfter, 64)
		if waitSeconds <= 0 {
			waitSeconds = 1
		}
		timer := time.NewTimer(time.Duration(waitSeconds * float64(time.Second)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
		return n.do(ctx, cred, method, path, body)
	}
	return response, nil
}

func (n *Node) doJSON(ctx context.Context, cred Credential, method string, path string, body any) (map[string]any, error) {
	response, err := n.do(ctx, cred, method, path, body)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return map[string]any{"success": true}, nil
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 16*1024*1024))
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 400 {
		apiErr := &Error{}
		_ = json.Unmarshal(data, apiErr)
		if apiErr.Code == 0 {
			apiErr.Code = response.StatusCode
		}
		if apiErr.Message == "" {
			apiErr.Message = string(data)
		}
		return nil, apiErr
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]any{"success": true}, nil
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, fmt.Errorf("decode discord response: %w", err)
	}
	return object, nil
}

func (n *Node) doList(ctx context.Context, cred Credential, method string, path string, body any) ([]map[string]any, error) {
	response, err := n.do(ctx, cred, method, path, body)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 16*1024*1024))
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 400 {
		apiErr := &Error{}
		_ = json.Unmarshal(data, apiErr)
		return nil, apiErr
	}
	var list []map[string]any
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("decode discord list response: %w", err)
	}
	return outputList(list), nil
}

func (n *Node) doUnauthenticatedJSON(ctx context.Context, method string, url string, body any) (map[string]any, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := n.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return map[string]any{"success": true}, nil
	}
	respData, err := io.ReadAll(io.LimitReader(response.Body, 16*1024*1024))
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("discord webhook HTTP %d: %s", response.StatusCode, string(respData))
	}
	if len(bytes.TrimSpace(respData)) == 0 {
		return map[string]any{"success": true}, nil
	}
	var object map[string]any
	if err := json.Unmarshal(respData, &object); err != nil {
		return nil, err
	}
	return object, nil
}
