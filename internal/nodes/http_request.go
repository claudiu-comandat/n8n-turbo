package nodes

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/binarydata"
	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
	"github.com/n8n-io/n8n-turbo/internal/expr"
)

type HTTPRequest struct{}

func (HTTPRequest) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	client := httpClientForNode(in.Node.Parameters)
	items := firstInput(in.InputData)
	if len(items) == 0 {
		items = []dataplane.Item{{JSON: map[string]any{}}}
	}
	output := make([]dataplane.Item, 0, len(items))
	batchSize, batchInterval := httpBatching(in.Node.Parameters)
	for index := range items {
		if index > 0 && batchSize > 0 && batchInterval > 0 && index%batchSize == 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(batchInterval) * time.Millisecond):
			}
		}
		pageItems, err := executeHTTPItemWithPagination(ctx, &client, in, items, index)
		if err != nil {
			return nil, err
		}
		output = append(output, pageItems...)
	}
	return dataplane.MainOutput(output), nil
}

func doHTTPRequestWithRetry(ctx context.Context, client *http.Client, in engine.ExecuteInput, items []dataplane.Item, index int) (*http.Response, error) {
	return doHTTPRequestWithRetryAndQuery(ctx, client, in, items, index, nil)
}

func doHTTPRequestWithRetryAndQuery(ctx context.Context, client *http.Client, in engine.ExecuteInput, items []dataplane.Item, index int, extraQuery map[string]any) (*http.Response, error) {
	maxTries := httpRetryMaxTries(in.Node.Parameters)
	wait := httpRetryWait(in.Node.Parameters)
	var lastErr error
	for attempt := 0; attempt < maxTries; attempt++ {
		if attempt > 0 && wait > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(wait) * time.Millisecond):
			}
		}
		req, err := httpRequestForItemWithQuery(ctx, in, items, index, extraQuery)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode < 500 || attempt == maxTries-1 {
			return resp, nil
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
		_ = resp.Body.Close()
	}
	return nil, lastErr
}

func executeHTTPItemWithPagination(ctx context.Context, client *http.Client, in engine.ExecuteInput, items []dataplane.Item, index int) ([]dataplane.Item, error) {
	config := httpPaginationConfig(in.Node.Parameters)
	pageCount := 0
	extraQuery := map[string]any(nil)
	result := []dataplane.Item{}
	for {
		resp, err := doHTTPRequestWithRetryAndQuery(ctx, client, in, items, index, extraQuery)
		if err != nil {
			return nil, err
		}
		item, responseBody, err := httpResponsePageItem(ctx, resp, in)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
		if !config.enabled {
			return result, nil
		}
		pageCount++
		if config.maxRequests > 0 && pageCount >= config.maxRequests {
			return result, nil
		}
		if config.completeExpression != "" && httpPaginationComplete(in, items, index, pageCount, responseBody, item, config.completeExpression) {
			return result, nil
		}
		nextQuery := httpPaginationQuery(in, items, index, pageCount, responseBody, item, config.parameters)
		if len(nextQuery) == 0 {
			return result, nil
		}
		extraQuery = nextQuery
	}
}

func httpResponsePageItem(ctx context.Context, resp *http.Response, in engine.ExecuteInput) (dataplane.Item, any, error) {
	if httpResponseFormat(in.Node.Parameters) == "binary" || httpResponseFormat(in.Node.Parameters) == "file" {
		if resp.StatusCode >= 400 && !httpNeverError(in.Node.Parameters) {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
			_ = resp.Body.Close()
			if readErr != nil {
				return dataplane.Item{}, nil, readErr
			}
			return dataplane.Item{}, nil, fmt.Errorf("http request failed with status %d: %s", resp.StatusCode, string(body))
		}
		item, itemErr := httpBinaryResponseItem(ctx, resp, in.Node.Parameters, in.BinaryStore)
		_ = resp.Body.Close()
		if itemErr != nil {
			return dataplane.Item{}, nil, itemErr
		}
		return item, item.JSON["body"], nil
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024))
	_ = resp.Body.Close()
	if readErr != nil {
		return dataplane.Item{}, nil, readErr
	}
	if resp.StatusCode >= 400 && !httpNeverError(in.Node.Parameters) {
		return dataplane.Item{}, nil, fmt.Errorf("http request failed with status %d: %s", resp.StatusCode, string(body))
	}
	item := httpResponseItem(resp, body, in.Node.Parameters)
	return item, httpPaginationBody(body, item), nil
}

func httpRequestForItem(ctx context.Context, in engine.ExecuteInput, items []dataplane.Item, index int) (*http.Request, error) {
	return httpRequestForItemWithQuery(ctx, in, items, index, nil)
}

func httpRequestForItemWithQuery(ctx context.Context, in engine.ExecuteInput, items []dataplane.Item, index int, extraQuery map[string]any) (*http.Request, error) {
	rawURLValue := resolveValue(in, items, index, in.Node.Parameters["url"])
	if rawURLValue == nil {
		return nil, fmt.Errorf("url is required: expression resolved to empty value")
	}
	rawURL := strings.TrimSpace(fmt.Sprint(rawURLValue))
	if rawURL == "" || rawURL == "<nil>" {
		return nil, fmt.Errorf("url is required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid url %q: expected absolute URL with http or https scheme", rawURL)
	}
	query := parsed.Query()
	for key, value := range httpRequestQuery(in, items, index, in.Node.Parameters) {
		query.Set(key, fmt.Sprint(value))
	}
	for key, value := range extraQuery {
		query.Set(key, fmt.Sprint(value))
	}
	parsed.RawQuery = query.Encode()
	method := strings.ToUpper(stringParam(in.Node.Parameters, "method", "requestMethod"))
	if method == "" {
		method = http.MethodGet
	}
	body, contentType, err := httpBody(in, items, index)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, parsed.String(), body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for key, value := range httpRequestHeaders(in, items, index, in.Node.Parameters) {
		req.Header.Set(key, fmt.Sprint(value))
	}
	appliedCredential, err := applyCredentialAuth(ctx, req, in.Credentials)
	if err != nil {
		return nil, err
	}
	if !appliedCredential {
		applyHTTPAuth(req, in.Node.Parameters)
	}
	return req, nil
}

func httpBody(in engine.ExecuteInput, items []dataplane.Item, index int) (io.Reader, string, error) {
	if _, ok := in.Node.Parameters["sendBody"]; ok && !boolParam(in.Node.Parameters, "sendBody", false) {
		return nil, "", nil
	}
	contentTypeMode := strings.ToLower(firstNonEmptyNode(stringParam(in.Node.Parameters, "contentType"), stringParam(in.Node.Parameters, "bodyContentTypeMode")))
	specifyBody := strings.ToLower(stringParam(in.Node.Parameters, "specifyBody"))
	if contentTypeMode == "form-urlencoded" || contentTypeMode == "formurlencoded" {
		if specifyBody == "string" {
			body := resolveValue(in, items, index, in.Node.Parameters["body"])
			if body == nil {
				return nil, "application/x-www-form-urlencoded", nil
			}
			return strings.NewReader(fmt.Sprint(body)), "application/x-www-form-urlencoded", nil
		}
		values := url.Values{}
		for key, value := range httpNameValueMap(in, items, index, httpFirstPresent(in.Node.Parameters, "bodyParameters", "bodyParametersUi")) {
			values.Set(key, fmt.Sprint(value))
		}
		return strings.NewReader(values.Encode()), "application/x-www-form-urlencoded", nil
	}
	if contentTypeMode == "multipart-form-data" || contentTypeMode == "multipart" {
		var buffer bytes.Buffer
		writer := multipart.NewWriter(&buffer)
		for _, entry := range httpCollectionEntries(in, items, index, httpFirstPresent(in.Node.Parameters, "bodyParameters", "bodyParametersUi")) {
			if strings.EqualFold(stringParam(entry, "parameterType"), "formBinaryData") {
				fieldName := firstNonEmptyNode(stringParam(entry, "name"), stringParam(entry, "key"))
				binaryProperty := stringParam(entry, "inputDataFieldName")
				if fieldName == "" || binaryProperty == "" {
					continue
				}
				fileName, mimeType, reader, err := httpBinaryBody(in, items, index, binaryProperty)
				if err != nil {
					return nil, "", err
				}
				part, err := writer.CreateFormFile(fieldName, fileName)
				if err != nil {
					return nil, "", err
				}
				if _, err := io.Copy(part, reader); err != nil {
					return nil, "", err
				}
				if closer, ok := reader.(io.Closer); ok {
					_ = closer.Close()
				}
				_ = mimeType
				continue
			}
			key := firstNonEmptyNode(stringParam(entry, "name"), stringParam(entry, "key"))
			if key == "" {
				continue
			}
			if err := writer.WriteField(key, fmt.Sprint(resolveValue(in, items, index, firstPresent(entry, "value", "headerValue")))); err != nil {
				return nil, "", err
			}
		}
		if err := writer.Close(); err != nil {
			return nil, "", err
		}
		return &buffer, writer.FormDataContentType(), nil
	}
	if contentTypeMode == "binarydata" {
		property := firstNonEmptyNode(stringParam(in.Node.Parameters, "inputDataFieldName"), stringParam(in.Node.Parameters, "binaryPropertyName"), "data")
		_, mimeType, reader, err := httpBinaryBody(in, items, index, property)
		if err != nil {
			return nil, "", err
		}
		return reader, firstNonEmptyNode(mimeType, "application/octet-stream"), nil
	}
	if contentTypeMode == "raw" {
		value := resolveValue(in, items, index, in.Node.Parameters["body"])
		if value == nil {
			return nil, firstNonEmptyNode(stringParam(in.Node.Parameters, "rawContentType"), "text/plain"), nil
		}
		if text, ok := value.(string); ok {
			return strings.NewReader(text), firstNonEmptyNode(stringParam(in.Node.Parameters, "rawContentType"), "text/plain"), nil
		}
		bytes, err := json.Marshal(value)
		if err != nil {
			return nil, "", err
		}
		return bytesReader(bytes), firstNonEmptyNode(stringParam(in.Node.Parameters, "rawContentType"), "text/plain"), nil
	}
	raw, ok := in.Node.Parameters["body"]
	if !ok || (specifyBody == "json" && strings.EqualFold(contentTypeMode, "json")) {
		raw = httpFirstPresent(in.Node.Parameters, "jsonBody", "bodyParametersJson")
	}
	if raw == nil {
		bodyParameters := httpNameValueMap(in, items, index, httpFirstPresent(in.Node.Parameters, "bodyParameters", "bodyParametersUi"))
		for key, value := range httpMap(in, items, index, in.Node.Parameters["bodyParametersJson"]) {
			bodyParameters[key] = value
		}
		if len(bodyParameters) > 0 {
			bytes, err := json.Marshal(bodyParameters)
			if err != nil {
				return nil, "", err
			}
			return bytesReader(bytes), "application/json", nil
		}
	}
	if raw == nil {
		return nil, "", nil
	}
	value := resolveValue(in, items, index, raw)
	if text, ok := value.(string); ok {
		if strings.TrimSpace(text) == "" {
			return nil, "", nil
		}
		return strings.NewReader(text), firstNonEmptyNode(stringParam(in.Node.Parameters, "contentType"), "application/json"), nil
	}
	bytes, err := json.Marshal(value)
	if err != nil {
		return nil, "", err
	}
	return bytesReader(bytes), "application/json", nil
}

func httpResponseItem(resp *http.Response, body []byte, params map[string]any) dataplane.Item {
	item := dataplane.Item{JSON: map[string]any{}}
	format := httpResponseFormat(params)
	outputProperty := firstNonEmptyNode(httpNestedString(params, []string{"options", "response"}, "outputPropertyName"), stringParam(params, "outputPropertyName"), "data")
	if format == "binary" {
		item.JSON["body"] = map[string]any{}
		item.Binary = map[string]dataplane.Binary{outputProperty: {Data: base64.StdEncoding.EncodeToString(body), MimeType: resp.Header.Get("Content-Type"), FileSize: int64(len(body))}}
		return item
	}
	var decoded any
	if httpFullResponse(params) {
		item.JSON["statusCode"] = resp.StatusCode
		item.JSON["headers"] = resp.Header
		if format != "text" && json.Unmarshal(body, &decoded) == nil {
			item.JSON["body"] = decoded
		} else {
			item.JSON["body"] = string(body)
		}
		item.JSON["statusMessage"] = resp.Status
		return item
	}
	if format != "text" && json.Unmarshal(body, &decoded) == nil {
		if object, ok := rawObject(decoded); ok {
			item.JSON = object
			return item
		}
		item.JSON[outputProperty] = decoded
		return item
	}
	item.JSON[outputProperty] = string(body)
	return item
}

func httpBinaryResponseItem(ctx context.Context, resp *http.Response, params map[string]any, store binarydata.Store) (dataplane.Item, error) {
	item := dataplane.Item{JSON: map[string]any{"statusCode": resp.StatusCode, "headers": resp.Header, "body": map[string]any{}}}
	outputProperty := firstNonEmptyNode(httpNestedString(params, []string{"options", "response"}, "outputPropertyName"), stringParam(params, "outputPropertyName"), "data")
	mimeType := firstNonEmptyNode(resp.Header.Get("Content-Type"), "application/octet-stream")
	fileName := httpResponseFileName(resp)
	if store != nil {
		ref, err := store.Put(ctx, mimeType, fileName, resp.Body)
		if err != nil {
			return dataplane.Item{}, err
		}
		binary := binarydata.BinaryFromRef(ref)
		binary.FileExtension = strings.TrimPrefix(path.Ext(fileName), ".")
		item.Binary = map[string]dataplane.Binary{outputProperty: binary}
		return item, nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return dataplane.Item{}, err
	}
	item.Binary = map[string]dataplane.Binary{outputProperty: {
		Data:          base64.StdEncoding.EncodeToString(body),
		MimeType:      mimeType,
		FileName:      fileName,
		FileSize:      int64(len(body)),
		FileExtension: strings.TrimPrefix(path.Ext(fileName), "."),
	}}
	return item, nil
}

func httpResponseFileName(resp *http.Response) string {
	if resp != nil {
		if disposition := resp.Header.Get("Content-Disposition"); disposition != "" {
			if _, params, err := mime.ParseMediaType(disposition); err == nil {
				if fileName := strings.TrimSpace(params["filename"]); fileName != "" {
					return path.Base(strings.ReplaceAll(fileName, "\\", "/"))
				}
			}
		}
		if resp.Request != nil && resp.Request.URL != nil {
			if base := path.Base(strings.TrimSpace(resp.Request.URL.Path)); base != "" && base != "." && base != "/" {
				return base
			}
		}
	}
	return "response.bin"
}

func httpMap(in engine.ExecuteInput, items []dataplane.Item, index int, raw any) map[string]any {
	value := resolveValue(in, items, index, raw)
	if object, ok := rawObject(value); ok {
		return object
	}
	if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
		result := map[string]any{}
		if json.Unmarshal([]byte(text), &result) == nil {
			return result
		}
	}
	return map[string]any{}
}

func httpCollectionEntries(in engine.ExecuteInput, items []dataplane.Item, index int, raw any) []map[string]any {
	value := resolveValue(in, items, index, raw)
	object, ok := rawObject(value)
	if !ok {
		return nil
	}
	for _, key := range []string{"parameters", "values", "entries"} {
		entries, ok := object[key].([]any)
		if !ok {
			continue
		}
		result := make([]map[string]any, 0, len(entries))
		for _, entry := range entries {
			if entryObject, ok := rawObject(entry); ok {
				result = append(result, entryObject)
			}
		}
		return result
	}
	if entries, ok := object["parameter"].([]any); ok {
		result := make([]map[string]any, 0, len(entries))
		for _, entry := range entries {
			if entryObject, ok := rawObject(entry); ok {
				result = append(result, entryObject)
			}
		}
		return result
	}
	return nil
}

func httpNameValueMap(in engine.ExecuteInput, items []dataplane.Item, index int, raw any) map[string]any {
	result := map[string]any{}
	if entries := httpCollectionEntries(in, items, index, raw); len(entries) > 0 {
		for _, entryObject := range entries {
			name := firstNonEmptyNode(stringParam(entryObject, "name"), stringParam(entryObject, "key"))
			if name == "" {
				continue
			}
			result[name] = resolveValue(in, items, index, firstPresent(entryObject, "value", "headerValue"))
		}
		return result
	}
	value := resolveValue(in, items, index, raw)
	object, ok := rawObject(value)
	if !ok {
		return result
	}
	for key, rawValue := range object {
		if key == "parameters" || key == "values" || key == "entries" {
			continue
		}
		result[key] = rawValue
	}
	return result
}

func applyHTTPAuth(req *http.Request, params map[string]any) {
	if token := firstNonEmptyNode(stringParam(params, "accessToken"), stringParam(params, "token")); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if stringParam(params, "authentication") == "basicAuth" || stringParam(params, "authType") == "basic" {
		req.SetBasicAuth(stringParam(params, "user", "username"), stringParam(params, "password"))
	}
	if headerName := stringParam(params, "headerName"); headerName != "" {
		req.Header.Set(headerName, stringParam(params, "headerValue"))
	}
}

func firstNonEmptyNode(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func bytesReader(data []byte) io.Reader {
	return bytes.NewReader(data)
}

func httpClientForNode(params map[string]any) http.Client {
	timeout := time.Duration(httpTimeout(params)) * time.Millisecond
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment}
	if httpAllowUnauthorizedCerts(params) {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	if proxyURL := strings.TrimSpace(httpProxy(params)); proxyURL != "" {
		if parsed, err := url.Parse(proxyURL); err == nil {
			transport.Proxy = http.ProxyURL(parsed)
		}
	}
	client := http.Client{Timeout: timeout, Transport: transport}
	follow, maxRedirects := httpRedirectConfig(params)
	if !follow {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	} else if maxRedirects > 0 {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			return nil
		}
	}
	return client
}

func httpTimeout(params map[string]any) int {
	timeout := intParam(params, "timeout", 0)
	if timeout <= 0 {
		timeout = intParam(httpOptionsMap(params), "timeout", 0)
	}
	if timeout <= 0 {
		timeout = 300000
	}
	return timeout
}

func httpRetryMaxTries(params map[string]any) int {
	value := intParam(httpOptionGroup(params, "retry"), "maxTries", 0)
	if value <= 0 {
		value = intParam(httpMapParameter(params, "retry"), "maxTries", 0)
	}
	if value <= 0 {
		value = intParam(params, "maxTries", 1)
	}
	if value <= 0 {
		return 1
	}
	return value
}

func httpRetryWait(params map[string]any) int {
	value := intParam(httpOptionGroup(params, "retry"), "waitBetweenTries", 0)
	if value <= 0 {
		value = intParam(httpMapParameter(params, "retry"), "waitBetweenTries", 0)
	}
	return value
}

func httpNeverError(params map[string]any) bool {
	if boolParam(params, "ignoreResponseCode", false) || boolParam(params, "neverError", false) {
		return true
	}
	response := httpOptionGroup(params, "response")
	return boolParam(response, "neverError", boolParam(response, "ignoreResponseCode", false))
}

func httpFullResponse(params map[string]any) bool {
	response := httpOptionGroup(params, "response")
	return boolParam(response, "fullResponse", boolParam(params, "fullResponse", false))
}

func httpResponseFormat(params map[string]any) string {
	format := strings.ToLower(firstNonEmptyNode(
		stringParam(httpOptionGroup(params, "response"), "responseFormat"),
		stringParam(params, "responseFormat"),
	))
	if format == "string" {
		return "text"
	}
	return format
}

type httpPaginationSettings struct {
	enabled            bool
	parameters         any
	completeExpression string
	maxRequests        int
}

func httpPaginationConfig(params map[string]any) httpPaginationSettings {
	group := httpOptionGroup(params, "pagination")
	if len(group) == 0 {
		return httpPaginationSettings{}
	}
	maxRequests := 100
	if boolParam(group, "limitPagesFetched", false) {
		maxRequests = intParam(group, "maxRequests", maxRequests)
	}
	if maxRequests <= 0 {
		maxRequests = 100
	}
	return httpPaginationSettings{
		enabled:            true,
		parameters:         group["parameters"],
		completeExpression: strings.TrimSpace(stringParam(group, "completeExpression")),
		maxRequests:        maxRequests,
	}
}

func httpPaginationQuery(in engine.ExecuteInput, items []dataplane.Item, index int, pageCount int, responseBody any, responseItem dataplane.Item, raw any) map[string]any {
	if raw == nil {
		return nil
	}
	paginationInput := in
	paginationInput.Expr = firstNonNilResolver(in.Expr)
	result := map[string]any{}
	if entries := httpCollectionEntriesWithExtra(paginationInput, items, index, raw, httpPaginationExpressionExtra(pageCount, responseBody, responseItem)); len(entries) > 0 {
		for _, entry := range entries {
			name := firstNonEmptyNode(stringParam(entry, "name"), stringParam(entry, "key"))
			if name == "" {
				continue
			}
			result[name] = resolveHTTPValue(paginationInput, items, index, firstPresent(entry, "value", "headerValue"), httpPaginationExpressionExtra(pageCount, responseBody, responseItem))
		}
	}
	return result
}

func httpPaginationComplete(in engine.ExecuteInput, items []dataplane.Item, index int, pageCount int, responseBody any, responseItem dataplane.Item, rawExpression string) bool {
	if rawExpression == "" {
		return false
	}
	value := resolveHTTPValue(in, items, index, rawExpression, httpPaginationExpressionExtra(pageCount, responseBody, responseItem))
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func httpPaginationBody(body []byte, item dataplane.Item) any {
	var decoded any
	if len(body) > 0 && json.Unmarshal(body, &decoded) == nil {
		return decoded
	}
	if bodyValue, ok := item.JSON["body"]; ok {
		return bodyValue
	}
	return item.JSON
}

func httpPaginationExpressionExtra(pageCount int, responseBody any, responseItem dataplane.Item) map[string]any {
	return map[string]any{
		"$pageCount": pageCount,
		"$response": map[string]any{
			"body": responseBody,
			"item": responseItem.JSON,
		},
	}
}

func httpCollectionEntriesWithExtra(in engine.ExecuteInput, items []dataplane.Item, index int, raw any, extra map[string]any) []map[string]any {
	value := resolveHTTPValue(in, items, index, raw, extra)
	object, ok := rawObject(value)
	if !ok {
		return nil
	}
	for _, key := range []string{"parameters", "values", "entries"} {
		entries, ok := object[key].([]any)
		if !ok {
			continue
		}
		result := make([]map[string]any, 0, len(entries))
		for _, entry := range entries {
			if entryObject, ok := rawObject(entry); ok {
				result = append(result, entryObject)
			}
		}
		return result
	}
	return nil
}

func resolveHTTPValue(in engine.ExecuteInput, items []dataplane.Item, itemIndex int, value any, extra map[string]any) any {
	if in.Expr == nil {
		in.Expr = firstNonNilResolver(nil)
	}
	resolved, err := in.Expr.Resolve(value, expr.Context{
		Items:         items,
		CurrentIndex:  itemIndex,
		RunData:       in.RunData,
		Variables:     in.Variables,
		Secrets:       in.Secrets,
		WorkflowID:    in.WorkflowID,
		WorkflowName:  in.WorkflowName,
		ExecutionID:   in.ExecutionID,
		ExecutionMode: in.ExecutionMode,
		ResumeURL:     in.ResumeURL,
		ResumeFormURL: in.ResumeFormURL,
		ScheduledTime: in.ScheduledTime,
		RunIndex:      in.RunIndex,
		Extra:         extra,
	})
	if err != nil {
		return value
	}
	return resolved
}

func firstNonNilResolver(resolver *expr.Resolver) *expr.Resolver {
	if resolver != nil {
		return resolver
	}
	return expr.NewResolver(5 * time.Second)
}

func httpRequestQuery(in engine.ExecuteInput, items []dataplane.Item, index int, params map[string]any) map[string]any {
	result := httpMap(in, items, index, params["query"])
	if boolParam(params, "sendQuery", len(result) > 0 || params["queryParameters"] != nil || params["queryParametersUi"] != nil || params["jsonQuery"] != nil || params["queryParametersJson"] != nil) {
		if strings.EqualFold(stringParam(params, "specifyQuery"), "json") {
			for key, value := range httpMap(in, items, index, httpFirstPresent(params, "jsonQuery", "queryParametersJson")) {
				result[key] = value
			}
		} else {
			for key, value := range httpNameValueMap(in, items, index, httpFirstPresent(params, "queryParameters", "queryParametersUi")) {
				result[key] = value
			}
			for key, value := range httpMap(in, items, index, params["queryParametersJson"]) {
				result[key] = value
			}
		}
	}
	return result
}

func httpRequestHeaders(in engine.ExecuteInput, items []dataplane.Item, index int, params map[string]any) map[string]any {
	result := httpMap(in, items, index, params["headers"])
	if boolParam(params, "sendHeaders", len(result) > 0 || params["headerParameters"] != nil || params["headerParametersUi"] != nil || params["jsonHeaders"] != nil || params["headerParametersJson"] != nil) {
		if strings.EqualFold(stringParam(params, "specifyHeaders"), "json") {
			for key, value := range httpMap(in, items, index, httpFirstPresent(params, "jsonHeaders", "headerParametersJson")) {
				result[key] = value
			}
		} else {
			for key, value := range httpNameValueMap(in, items, index, httpFirstPresent(params, "headerParameters", "headerParametersUi")) {
				result[key] = value
			}
			for key, value := range httpMap(in, items, index, params["headerParametersJson"]) {
				result[key] = value
			}
		}
	}
	if boolParam(httpOptionsMap(params), "lowercaseHeaders", true) {
		normalized := map[string]any{}
		for key, value := range result {
			normalized[strings.ToLower(key)] = value
		}
		return normalized
	}
	return result
}

func httpBinaryBody(in engine.ExecuteInput, items []dataplane.Item, index int, property string) (string, string, io.Reader, error) {
	if index >= len(items) || items[index].Binary == nil {
		return "", "application/octet-stream", nil, nil
	}
	binary, ok := items[index].Binary[property]
	if !ok {
		return "", "application/octet-stream", nil, nil
	}
	if binary.ID != "" && in.BinaryStore != nil {
		reader, err := in.BinaryStore.Open(context.Background(), binarydata.RefFromBinary(binary))
		if err == nil {
			return firstNonEmptyNode(binary.FileName, property), binary.MimeType, reader, nil
		}
	}
	data, err := base64.StdEncoding.DecodeString(binary.Data)
	if err != nil {
		data = []byte(binary.Data)
	}
	return firstNonEmptyNode(binary.FileName, property), binary.MimeType, bytes.NewReader(data), nil
}

func httpOptionsMap(params map[string]any) map[string]any {
	if options := httpMapParameter(params, "options"); options != nil {
		return options
	}
	return map[string]any{}
}

func httpOptionGroup(params map[string]any, key string) map[string]any {
	group := httpMapParameter(httpOptionsMap(params), key)
	if group == nil {
		return map[string]any{}
	}
	if nested := httpMapParameter(group, key); nested != nil {
		return nested
	}
	return group
}

func httpAllowUnauthorizedCerts(params map[string]any) bool {
	return boolParam(httpOptionsMap(params), "allowUnauthorizedCerts", false)
}

func httpProxy(params map[string]any) string {
	return stringParam(httpOptionsMap(params), "proxy")
}

func httpBatching(params map[string]any) (int, int) {
	group := httpOptionGroup(params, "batching")
	batch := httpMapParameter(group, "batch")
	size := intParam(batch, "batchSize", -1)
	interval := intParam(batch, "batchInterval", 0)
	if size == 0 {
		size = 1
	}
	return size, interval
}

func httpRedirectConfig(params map[string]any) (bool, int) {
	group := httpOptionGroup(params, "redirect")
	follow := boolParam(group, "followRedirects", true)
	maxRedirects := intParam(group, "maxRedirects", 21)
	return follow, maxRedirects
}

func httpNestedString(params map[string]any, path []string, key string) string {
	current := params
	for _, part := range path {
		current = httpMapParameter(current, part)
	}
	return stringParam(current, key)
}

func httpNestedMap(params map[string]any, path ...string) map[string]any {
	current := params
	for _, part := range path {
		current = httpMapParameter(current, part)
		if current == nil {
			return map[string]any{}
		}
	}
	return current
}

func httpMapParameter(params map[string]any, key string) map[string]any {
	value, ok := params[key]
	if !ok || value == nil {
		return nil
	}
	if object, ok := rawObject(value); ok {
		return object
	}
	return nil
}

func httpFirstPresent(params map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := params[key]; ok {
			return value
		}
	}
	return nil
}
