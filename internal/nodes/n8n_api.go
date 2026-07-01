package nodes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type N8n struct{}

func (N8n) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	resource := strings.TrimSpace(firstNonEmptyNode(stringParam(in.Node.Parameters, "resource"), "workflow"))
	operation := strings.TrimSpace(firstNonEmptyNode(stringParam(in.Node.Parameters, "operation"), n8nDefaultOperation(resource)))
	switch resource {
	case "audit":
		return n8nAudit(ctx, in, operation)
	case "credential":
		return n8nCredential(ctx, in, operation)
	case "execution":
		return n8nExecution(ctx, in, operation)
	case "workflow":
		return n8nWorkflow(ctx, in, operation)
	default:
		return nil, fmt.Errorf("n8n node resource %q is not implemented", resource)
	}
}

func n8nDefaultOperation(resource string) string {
	switch resource {
	case "audit":
		return "generate"
	case "credential":
		return "create"
	default:
		return "getAll"
	}
}

func n8nExecution(ctx context.Context, in engine.ExecuteInput, operation string) (dataplane.Output, error) {
	switch operation {
	case "getAll":
		return n8nExecutionGetAll(ctx, in)
	case "get":
		return n8nExecutionGet(ctx, in)
	case "delete":
		return n8nExecutionDelete(ctx, in)
	default:
		return nil, fmt.Errorf("n8n execution operation %q is not implemented", operation)
	}
}

func n8nAudit(ctx context.Context, in engine.ExecuteInput, operation string) (dataplane.Output, error) {
	if operation != "generate" {
		return nil, fmt.Errorf("n8n audit operation %q is not implemented", operation)
	}
	body := map[string]any{}
	if options, ok := rawObject(in.Node.Parameters["additionalOptions"]); ok && len(options) > 0 {
		body["additionalOptions"] = options
	}
	return n8nRequestOutput(ctx, in, http.MethodPost, "/audit", nil, body)
}

func n8nCredential(ctx context.Context, in engine.ExecuteInput, operation string) (dataplane.Output, error) {
	switch operation {
	case "create":
		data, err := parseJSONObject(in.Node.Parameters["data"])
		if err != nil {
			return nil, fmt.Errorf("credential data must be valid JSON: %w", err)
		}
		body := map[string]any{
			"name": stringParam(in.Node.Parameters, "name"),
			"type": stringParam(in.Node.Parameters, "credentialTypeName"),
			"data": data,
		}
		return n8nRequestOutput(ctx, in, http.MethodPost, "/credentials", nil, body)
	case "delete":
		credentialID := strings.TrimSpace(stringParam(in.Node.Parameters, "credentialId"))
		if credentialID == "" {
			return nil, fmt.Errorf("credentialId is required")
		}
		return n8nRequestOutput(ctx, in, http.MethodDelete, "/credentials/"+url.PathEscape(credentialID), nil, nil)
	case "getSchema":
		credentialType := strings.TrimSpace(stringParam(in.Node.Parameters, "credentialTypeName"))
		if credentialType == "" {
			return nil, fmt.Errorf("credentialTypeName is required")
		}
		return n8nRequestOutput(ctx, in, http.MethodGet, "/credentials/schema/"+url.PathEscape(credentialType), nil, nil)
	default:
		return nil, fmt.Errorf("n8n credential operation %q is not implemented", operation)
	}
}

func n8nWorkflow(ctx context.Context, in engine.ExecuteInput, operation string) (dataplane.Output, error) {
	switch operation {
	case "activate", "deactivate":
		workflowID := n8nResourceLocatorValue(in.Node.Parameters["workflowId"])
		if workflowID == "" {
			return nil, fmt.Errorf("workflowId is required")
		}
		body := map[string]any(nil)
		if operation == "activate" {
			if fields, ok := rawObject(in.Node.Parameters["additionalFields"]); ok && len(fields) > 0 {
				body = fields
			}
		}
		return n8nRequestOutput(ctx, in, http.MethodPost, "/workflows/"+url.PathEscape(workflowID)+"/"+operation, nil, body)
	case "create":
		body, err := parseJSONObject(in.Node.Parameters["workflowObject"])
		if err != nil {
			return nil, fmt.Errorf("workflowObject must be valid JSON: %w", err)
		}
		return n8nRequestOutput(ctx, in, http.MethodPost, "/workflows", nil, body)
	case "delete":
		workflowID := n8nResourceLocatorValue(in.Node.Parameters["workflowId"])
		if workflowID == "" {
			return nil, fmt.Errorf("workflowId is required")
		}
		return n8nRequestOutput(ctx, in, http.MethodDelete, "/workflows/"+url.PathEscape(workflowID), nil, nil)
	case "get":
		workflowID := n8nResourceLocatorValue(in.Node.Parameters["workflowId"])
		if workflowID == "" {
			return nil, fmt.Errorf("workflowId is required")
		}
		return n8nRequestOutput(ctx, in, http.MethodGet, "/workflows/"+url.PathEscape(workflowID), nil, nil)
	case "getAll":
		return n8nWorkflowGetAll(ctx, in)
	case "getVersion":
		workflowID := n8nResourceLocatorValue(in.Node.Parameters["workflowId"])
		versionID := strings.TrimSpace(stringParam(in.Node.Parameters, "versionId"))
		if workflowID == "" || versionID == "" {
			return nil, fmt.Errorf("workflowId and versionId are required")
		}
		return n8nRequestOutput(ctx, in, http.MethodGet, "/workflows/"+url.PathEscape(workflowID)+"/"+url.PathEscape(versionID), nil, nil)
	case "update":
		workflowID := n8nResourceLocatorValue(in.Node.Parameters["workflowId"])
		if workflowID == "" {
			return nil, fmt.Errorf("workflowId is required")
		}
		body, err := parseJSONObject(in.Node.Parameters["workflowObject"])
		if err != nil {
			return nil, fmt.Errorf("workflowObject must be valid JSON: %w", err)
		}
		return n8nRequestOutput(ctx, in, http.MethodPut, "/workflows/"+url.PathEscape(workflowID), nil, body)
	default:
		return nil, fmt.Errorf("n8n workflow operation %q is not implemented", operation)
	}
}

func n8nExecutionGetAll(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	limit := 100
	if !boolParam(in.Node.Parameters, "returnAll", false) {
		limit = intParam(in.Node.Parameters, "limit", 100)
	}
	query := url.Values{}
	query.Set("limit", fmt.Sprint(limit))
	filters, _ := rawObject(in.Node.Parameters["filters"])
	if workflowID := n8nResourceLocatorValue(filters["workflowId"]); workflowID != "" {
		query.Set("workflowId", workflowID)
	}
	if status := strings.TrimSpace(stringParam(filters, "status")); status != "" {
		query.Set("status", status)
	}
	options, _ := rawObject(in.Node.Parameters["options"])
	if boolParam(options, "activeWorkflows", false) {
		query.Set("includeData", "true")
	}

	items := []dataplane.Item{}
	cursor := ""
	for {
		pageQuery := cloneURLValues(query)
		if cursor != "" {
			pageQuery.Set("cursor", cursor)
		}
		body, err := n8nAPIRequest(ctx, in.Credentials, http.MethodGet, "/executions?"+pageQuery.Encode(), nil)
		if err != nil {
			return nil, err
		}
		pageItems, nextCursor, err := n8nItemsFromListResponse(body)
		if err != nil {
			return nil, err
		}
		items = append(items, pageItems...)
		if !boolParam(in.Node.Parameters, "returnAll", false) || nextCursor == "" {
			break
		}
		cursor = nextCursor
	}
	return dataplane.MainOutput(items), nil
}

func n8nWorkflowGetAll(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	limit := 100
	if !boolParam(in.Node.Parameters, "returnAll", true) {
		limit = intParam(in.Node.Parameters, "limit", 100)
	}
	query := url.Values{}
	query.Set("limit", fmt.Sprint(limit))
	filters, _ := rawObject(in.Node.Parameters["filters"])
	if active, ok := filters["activeWorkflows"]; ok {
		query.Set("active", fmt.Sprint(active))
	}
	for _, key := range []string{"tags", "name", "projectId"} {
		if value := strings.TrimSpace(stringParam(filters, key)); value != "" {
			query.Set(key, value)
		}
	}
	if exclude, ok := filters["excludePinnedData"]; ok {
		query.Set("excludePinnedData", fmt.Sprint(exclude))
	}
	return n8nPaginatedList(ctx, in, "/workflows", query, boolParam(in.Node.Parameters, "returnAll", true))
}

func n8nExecutionGet(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	executionID := strings.TrimSpace(stringParam(in.Node.Parameters, "executionId"))
	if executionID == "" {
		return nil, fmt.Errorf("executionId is required")
	}
	query := url.Values{}
	options, _ := rawObject(in.Node.Parameters["options"])
	if boolParam(options, "activeWorkflows", false) {
		query.Set("includeData", "true")
	}
	path := "/executions/" + url.PathEscape(executionID)
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	body, err := n8nAPIRequest(ctx, in.Credentials, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	return dataplane.MainOutput([]dataplane.Item{{JSON: data}}), nil
}

func n8nPaginatedList(ctx context.Context, in engine.ExecuteInput, path string, query url.Values, returnAll bool) (dataplane.Output, error) {
	items := []dataplane.Item{}
	cursor := ""
	for {
		pageQuery := cloneURLValues(query)
		if cursor != "" {
			pageQuery.Set("cursor", cursor)
		}
		body, err := n8nAPIRequest(ctx, in.Credentials, http.MethodGet, path+"?"+pageQuery.Encode(), nil)
		if err != nil {
			return nil, err
		}
		pageItems, nextCursor, err := n8nItemsFromListResponse(body)
		if err != nil {
			return nil, err
		}
		items = append(items, pageItems...)
		if !returnAll || nextCursor == "" {
			break
		}
		cursor = nextCursor
	}
	return dataplane.MainOutput(items), nil
}

func n8nExecutionDelete(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	executionID := strings.TrimSpace(stringParam(in.Node.Parameters, "executionId"))
	if executionID == "" {
		return nil, fmt.Errorf("executionId is required")
	}
	body, err := n8nAPIRequest(ctx, in.Credentials, http.MethodDelete, "/executions/"+url.PathEscape(executionID), nil)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return dataplane.MainOutput([]dataplane.Item{{JSON: map[string]any{"success": true}}}), nil
	}
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return dataplane.MainOutput([]dataplane.Item{{JSON: map[string]any{"success": true, "response": string(body)}}}), nil
	}
	return dataplane.MainOutput([]dataplane.Item{{JSON: data}}), nil
}

func n8nRequestOutput(ctx context.Context, in engine.ExecuteInput, method string, path string, query url.Values, body any) (dataplane.Output, error) {
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}
	raw, err := n8nAPIRequest(ctx, in.Credentials, method, path, reader)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return dataplane.MainOutput([]dataplane.Item{{JSON: map[string]any{"success": true}}}), nil
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return dataplane.MainOutput([]dataplane.Item{{JSON: map[string]any{"response": string(raw)}}}), nil
	}
	return dataplane.MainOutput(n8nItemsFromResponse(decoded)), nil
}

func n8nItemsFromResponse(decoded any) []dataplane.Item {
	switch typed := decoded.(type) {
	case []any:
		items := make([]dataplane.Item, 0, len(typed))
		for _, entry := range typed {
			if object, ok := rawObject(entry); ok {
				items = append(items, dataplane.Item{JSON: object})
				continue
			}
			items = append(items, dataplane.Item{JSON: map[string]any{"value": entry}})
		}
		return items
	case map[string]any:
		return []dataplane.Item{{JSON: typed}}
	default:
		return []dataplane.Item{{JSON: map[string]any{"value": decoded}}}
	}
}

func parseJSONObject(value any) (map[string]any, error) {
	if object, ok := rawObject(value); ok {
		return object, nil
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" {
		return map[string]any{}, nil
	}
	var object map[string]any
	if err := json.Unmarshal([]byte(text), &object); err != nil {
		return nil, err
	}
	return object, nil
}

func n8nAPIRequest(ctx context.Context, credentials map[string]map[string]any, method string, path string, body io.Reader) ([]byte, error) {
	credential := credentialByType(credentials, "n8nApi")
	if credential == nil {
		return nil, fmt.Errorf("node is not configured with n8nApi credentials")
	}
	baseURL := strings.TrimRight(credentialString(credential, "baseUrl"), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("n8nApi.baseUrl is required")
	}
	apiKey := strings.TrimSpace(credentialString(credential, "apiKey"))
	if apiKey == "" {
		return nil, fmt.Errorf("n8nApi.apiKey is required")
	}
	req, err := http.NewRequestWithContext(ctx, method, baseURL+"/"+strings.TrimLeft(path, "/"), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-N8N-API-KEY", apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("n8n API HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return raw, nil
}

func n8nItemsFromListResponse(raw []byte) ([]dataplane.Item, string, error) {
	var decoded struct {
		Data       []map[string]any `json:"data"`
		NextCursor string           `json:"nextCursor"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, "", err
	}
	items := make([]dataplane.Item, 0, len(decoded.Data))
	for _, entry := range decoded.Data {
		items = append(items, dataplane.Item{JSON: entry})
	}
	return items, strings.TrimSpace(decoded.NextCursor), nil
}

func n8nResourceLocatorValue(raw any) string {
	switch typed := raw.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		return strings.TrimSpace(firstNonEmptyNode(stringParam(typed, "value"), stringParam(typed, "cachedResultUrl")))
	default:
		if raw == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprint(raw))
	}
}

func cloneURLValues(values url.Values) url.Values {
	cloned := url.Values{}
	for key, entries := range values {
		cloned[key] = append([]string(nil), entries...)
	}
	return cloned
}
