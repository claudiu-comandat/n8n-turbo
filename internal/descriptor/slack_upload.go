package descriptor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/binarydata"
	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type slackUploadURLResponse struct {
	OK        bool   `json:"ok"`
	Error     string `json:"error"`
	UploadURL string `json:"upload_url"`
	FileID    string `json:"file_id"`
}

type slackRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]time.Time
}

var globalSlackLimiter = &slackRateLimiter{buckets: make(map[string]time.Time)}

func (e Executor) executeSlackUpload(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	credential := credentialFromDescriptor(e.descriptor, in.Credentials)
	token := credentialString(credential, "accessToken", "token", "apiKey")
	if token == "" {
		return nil, fmt.Errorf("slack: accessToken missing")
	}
	fileName := stringValue(in.Node.Parameters, "filename", "fileName")
	if fileName == "" {
		return nil, fmt.Errorf("slack: filename is required")
	}
	content, err := slackUploadContent(ctx, in)
	if err != nil {
		return nil, err
	}
	baseURL := strings.TrimRight(firstNonEmptyAuth(stringValue(in.Node.Parameters, "baseUrl"), e.descriptor.BaseURL), "/")
	upload, err := e.slackGetUploadURL(ctx, baseURL, token, fileName, len(content))
	if err != nil {
		return nil, err
	}
	if err := e.slackUploadToURL(ctx, upload.UploadURL, fileName, content); err != nil {
		return nil, err
	}
	result, err := e.slackCompleteUpload(ctx, baseURL, token, upload.FileID, in.Node.Parameters)
	if err != nil {
		return nil, err
	}
	return dataplane.MainOutput(toItems(result)), nil
}

func slackUploadContent(ctx context.Context, in engine.ExecuteInput) ([]byte, error) {
	if content := stringValue(in.Node.Parameters, "content"); content != "" {
		return []byte(content), nil
	}
	property := firstNonEmptyAuth(stringValue(in.Node.Parameters, "binary_data", "binaryPropertyName", "binaryProperty"), "data")
	if len(in.InputData) == 0 || len(in.InputData[0]) == 0 {
		return []byte{}, nil
	}
	binary, ok := in.InputData[0][0].Binary[property]
	if !ok {
		return []byte{}, nil
	}
	return binarydata.Read(ctx, in.BinaryStore, binary)
}

func (e Executor) slackGetUploadURL(ctx context.Context, baseURL string, token string, fileName string, length int) (slackUploadURLResponse, error) {
	globalSlackLimiter.wait(ctx, "files.getUploadURLExternal")
	endpoint, err := url.Parse(baseURL + "/files.getUploadURLExternal")
	if err != nil {
		return slackUploadURLResponse{}, err
	}
	query := endpoint.Query()
	query.Set("filename", filepath.Base(fileName))
	query.Set("length", strconv.Itoa(length))
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return slackUploadURLResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return slackUploadURLResponse{}, err
	}
	defer resp.Body.Close()
	var result slackUploadURLResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return slackUploadURLResponse{}, err
	}
	if !result.OK {
		return slackUploadURLResponse{}, fmt.Errorf("slack: %s", firstNonEmptyAuth(result.Error, "upload_url_error"))
	}
	if result.UploadURL == "" || result.FileID == "" {
		return slackUploadURLResponse{}, fmt.Errorf("slack: upload URL response missing fields")
	}
	return result, nil
}

func (e Executor) slackUploadToURL(ctx context.Context, uploadURL string, fileName string, content []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, bytes.NewReader(content))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Length", strconv.Itoa(len(content)))
	req.Header.Set("X-Slack-Filename", filepath.Base(fileName))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("slack: upload failed %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (e Executor) slackCompleteUpload(ctx context.Context, baseURL string, token string, fileID string, params map[string]any) (any, error) {
	globalSlackLimiter.wait(ctx, "files.completeUploadExternal")
	file := map[string]any{"id": fileID}
	if title := stringValue(params, "title"); title != "" {
		file["title"] = title
	}
	payload := map[string]any{"files": []map[string]any{file}}
	if channel := stringValue(params, "channel_id", "channel"); channel != "" {
		payload["channel_id"] = channel
	}
	if comment := stringValue(params, "initial_comment"); comment != "" {
		payload["initial_comment"] = comment
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/files.completeUploadExternal", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if ok, _ := result["ok"].(bool); !ok {
		return nil, fmt.Errorf("slack: %s", firstNonEmptyAuth(fmt.Sprint(result["error"]), "complete_upload_error"))
	}
	return result, nil
}

func (r *slackRateLimiter) wait(ctx context.Context, method string) {
	interval := slackInterval(method)
	// Reserve the slot under the lock, then sleep outside it.
	r.mu.Lock()
	next := r.buckets[method].Add(interval)
	if now := time.Now(); next.Before(now) {
		next = now
	}
	r.buckets[method] = next
	r.mu.Unlock()
	delay := time.Until(next)
	if delay <= 0 {
		return
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}

func slackInterval(method string) time.Duration {
	switch method {
	case "conversations.list", "users.list":
		return 3 * time.Second
	default:
		return time.Second
	}
}
