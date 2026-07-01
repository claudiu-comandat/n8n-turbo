package descriptor

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type Executor struct {
	descriptor Descriptor
}

func NewExecutor(descriptor Descriptor) Executor {
	descriptor.Normalize()
	return Executor{descriptor: descriptor}
}

func (e Executor) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	params := withOfficialParamAliases(e.descriptor.NodeType, in.Node.Parameters)
	operationName := e.operationName(params)
	operation, ok := e.descriptor.Operations[operationName]
	if !ok {
		return nil, fmt.Errorf("operation %s not found for %s", operationName, e.descriptor.NodeType)
	}
	if err := validateParams(operation, params); err != nil {
		return nil, err
	}
	if e.descriptor.Name == "slack" && operation.Name == "uploadFile" {
		aliasedInput := in
		aliasedInput.Node.Parameters = params
		return e.executeSlackUpload(ctx, aliasedInput)
	}
	if e.descriptor.Name == "github" && operation.Name == "deleteFile" {
		withSHA, err := e.withGitHubFileSHA(ctx, params, in.Credentials)
		if err != nil {
			return nil, err
		}
		params = withSHA
	}
	if e.descriptor.Name == "openai" && operation.Name == "chatCompletion" && boolValue(params, "stream", false) {
		return e.executeOpenAIStream(ctx, operation, params, in.Credentials)
	}
	if pagination := effectivePagination(e.descriptor, operation); pagination != nil {
		if pagination.Type == "link" {
			items, err := e.executeLinkPagination(ctx, operation, params, in.Credentials, pagination)
			if err != nil {
				return nil, err
			}
			return dataplane.MainOutput(items), nil
		}
		items, err := NewPaginationHandler().CollectAll(ctx, func(pageParams map[string]any) ([]byte, error) {
			merged := mergeParams(params, pageParams)
			raw, _, _, err := e.executeRaw(ctx, operation, merged, in.Credentials)
			return raw, err
		}, pagination)
		if err != nil {
			return nil, err
		}
		return dataplane.MainOutput(items), nil
	}
	raw, headers, statusCode, err := e.executeRaw(ctx, operation, params, in.Credentials)
	if err != nil {
		return nil, err
	}
	if operation.ResponseMap != "" || operation.Transform != "" {
		return transformedOutput(raw, operation, params)
	}
	item := dataplane.Item{JSON: map[string]any{"statusCode": statusCode, "headers": headers}}
	var decoded any
	if json.Unmarshal(raw, &decoded) == nil {
		if operation.Transform != "" {
			transformed, err := NewResponseTransformer().Apply(decoded, operation.Transform)
			if err != nil {
				return nil, err
			}
			decoded = transformed
		}
		item.JSON["body"] = decoded
	} else {
		item.JSON["body"] = string(raw)
	}
	return dataplane.MainOutput([]dataplane.Item{item}), nil
}

func (e Executor) operationName(params map[string]any) string {
	operationName := stringValue(params, "operation")
	if operationName == "" {
		operationName = "default"
	}
	if _, ok := e.descriptor.Operations[operationName]; ok {
		return operationName
	}
	if alias := officialOperationAlias(e.descriptor.NodeType, stringValue(params, "resource"), operationName); alias != "" {
		return alias
	}
	return operationName
}

func (e Executor) executeRaw(ctx context.Context, operation Operation, params map[string]any, credentials map[string]map[string]any) ([]byte, http.Header, int, error) {
	endpoint, err := e.endpoint(operation, params, credentials)
	if err != nil {
		return nil, nil, 0, err
	}
	return e.executeRawURL(ctx, operation, endpoint, params, credentials)
}

func (e Executor) executeRawURL(ctx context.Context, operation Operation, endpoint string, params map[string]any, credentials map[string]map[string]any) ([]byte, http.Header, int, error) {
	body, contentType, err := requestBody(operation, params)
	if err != nil {
		return nil, nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, operation.Method, endpoint, body)
	if err != nil {
		return nil, nil, 0, err
	}
	for key, value := range e.descriptor.DefaultHeaders {
		req.Header.Set(key, value)
	}
	for key, value := range operation.Headers {
		req.Header.Set(key, value)
	}
	applyHeaderParams(req, operation, params)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if e.descriptor.Name == "stripe" && operation.Method == http.MethodPost && req.Header.Get("Idempotency-Key") == "" {
		req.Header.Set("Idempotency-Key", GenerateStripeIdempotencyKey())
	}
	if e.descriptor.AuthType != "" {
		if err := NewAuthInjector().Inject(req, credentialFromDescriptor(e.descriptor, credentials), e.descriptor.AuthType, e.descriptor.AuthConfig); err != nil {
			return nil, nil, 0, err
		}
	} else if !applyCredentialAuth(req, credentials) {
		applyAuth(req, params)
	}
	client := http.Client{Timeout: time.Duration(intValue(params, "timeout", 300000)) * time.Millisecond}
	if e.descriptor.Name == "github" {
		if err := defaultGitHubRateLimiter.WaitIfNeeded(ctx); err != nil {
			return nil, nil, 0, fmt.Errorf("github rate limit wait: %w", err)
		}
	}
	if e.descriptor.Name == "airtable" {
		if err := defaultAirtableRateLimiter.Wait(ctx); err != nil {
			return nil, nil, 0, fmt.Errorf("airtable rate limit wait: %w", err)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, 0, err
	}
	defer resp.Body.Close()
	if e.descriptor.Name == "github" {
		defaultGitHubRateLimiter.UpdateFromHeaders(resp.Header)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024))
	if err != nil {
		return nil, nil, 0, err
	}
	if resp.StatusCode >= 400 {
		if e.descriptor.Name == "github" {
			if limited, wait := HandleGitHubSecondaryRateLimit(resp); limited {
				return nil, nil, 0, GitHubRateLimitError(wait)
			}
		}
		if e.descriptor.Name == "stripe" {
			return nil, nil, 0, ParseStripeError(resp.StatusCode, raw)
		}
		if e.descriptor.Name == "openai" {
			return nil, nil, 0, ParseOpenAIError(resp.StatusCode, raw)
		}
		return nil, nil, 0, parseHTTPError(resp.StatusCode, raw)
	}
	if err := checkAPIError(raw, e.descriptor.ErrorCheck); err != nil {
		return nil, nil, 0, err
	}
	return raw, resp.Header, resp.StatusCode, nil
}

func (e Executor) executeLinkPagination(ctx context.Context, operation Operation, params map[string]any, credentials map[string]map[string]any, pagination *Pagination) ([]dataplane.Item, error) {
	endpoint, err := e.endpoint(operation, params, credentials)
	if err != nil {
		return nil, err
	}
	limitParam := firstText(pagination.LimitParam, pagination.PerPageParam, "per_page")
	if limit := firstInt(pagination.DefaultLimit, 100); limit > 0 {
		endpoint, err = AppendPaginationParams(endpoint, map[string]any{limitParam: limit})
		if err != nil {
			return nil, err
		}
	}
	return NewPaginationHandler().CollectLinkPagination(ctx, endpoint, func(fullURL string) ([]byte, http.Header, error) {
		raw, headers, _, err := e.executeRawURL(ctx, operation, fullURL, params, credentials)
		return raw, headers, err
	}, pagination)
}

func (e Executor) endpoint(operation Operation, params map[string]any, credentials map[string]map[string]any) (string, error) {
	base := stringValue(params, "baseUrl")
	if base == "" {
		base = e.descriptor.BaseURL
	}
	base = replaceTemplateValues(base, params, credentials)
	path := operation.Path
	path = replaceTemplateValues(path, params, credentials)
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		endpoint, err := url.Parse(path)
		if err != nil {
			return "", err
		}
		query := endpoint.Query()
		for _, param := range operation.Params {
			if param.In != "query" {
				continue
			}
			value, ok := descriptorParamValue(params, param)
			if ok {
				for _, text := range queryValues(param, value) {
					query.Add(param.Name, text)
				}
			}
		}
		endpoint.RawQuery = query.Encode()
		return endpoint.String(), nil
	}
	endpoint, err := url.Parse(strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/"))
	if err != nil {
		return "", err
	}
	query := endpoint.Query()
	if raw, ok := params["query"].(map[string]any); ok {
		for key, value := range raw {
			query.Set(key, fmt.Sprint(value))
		}
	}
	for _, param := range operation.Params {
		if param.In != "query" {
			continue
		}
		value, ok := descriptorParamValue(params, param)
		if ok {
			for _, text := range queryValues(param, value) {
				query.Add(param.Name, text)
			}
		}
	}
	applyCredentialQuery(query, credentials)
	endpoint.RawQuery = query.Encode()
	return endpoint.String(), nil
}

func applyCredentialQuery(query url.Values, credentials map[string]map[string]any) {
	if credential := credentialByType(credentials, "trelloApi"); credential != nil {
		if key := credentialString(credential, "apiKey", "key"); key != "" {
			query.Set("key", key)
		}
		if token := credentialString(credential, "token", "apiToken"); token != "" {
			query.Set("token", token)
		}
	}
}

func replaceTemplateValues(value string, params map[string]any, credentials map[string]map[string]any) string {
	for key, param := range params {
		text := templateText(param)
		value = strings.ReplaceAll(value, "{"+key+"}", url.PathEscape(text))
		value = strings.ReplaceAll(value, "{{."+key+"}}", url.PathEscape(text))
	}
	for _, credential := range credentials {
		for key, param := range credential {
			text := templateText(param)
			value = strings.ReplaceAll(value, "{"+key+"}", url.PathEscape(text))
			value = strings.ReplaceAll(value, "{{."+key+"}}", url.PathEscape(text))
		}
	}
	return value
}

func templateText(value any) string {
	if object, ok := value.(map[string]any); ok {
		if raw, ok := object["value"]; ok {
			return fmt.Sprint(raw)
		}
	}
	return fmt.Sprint(value)
}

func requestBody(operation Operation, params map[string]any) (io.Reader, string, error) {
	if operation.Name == "sendMessage" && strings.Contains(operation.Path, "/messages/send") {
		if raw := stringValue(params, "raw"); raw != "" {
			data, err := json.Marshal(map[string]any{"raw": raw})
			if err != nil {
				return nil, "", err
			}
			return bytes.NewReader(data), "application/json", nil
		}
		raw, err := buildGmailRawMessage(params)
		if err != nil {
			return nil, "", err
		}
		data, err := json.Marshal(map[string]any{"raw": raw})
		if err != nil {
			return nil, "", err
		}
		return bytes.NewReader(data), "application/json", nil
	}
	if body, ok, err := gmailRequestBody(operation, params); ok {
		if err != nil {
			return nil, "", err
		}
		data, err := json.Marshal(body)
		if err != nil {
			return nil, "", err
		}
		return bytes.NewReader(data), "application/json", nil
	}
	if body, ok, err := airtableRequestBody(operation, params); ok {
		if err != nil {
			return nil, "", err
		}
		data, err := json.Marshal(body)
		if err != nil {
			return nil, "", err
		}
		return bytes.NewReader(data), "application/json", nil
	}
	if body, ok, err := googleSheetsBatchBody(operation, params); ok {
		if err != nil {
			return nil, "", err
		}
		data, err := json.Marshal(body)
		if err != nil {
			return nil, "", err
		}
		return bytes.NewReader(data), "application/json", nil
	}
	if body, ok, err := githubRequestBody(operation, params); ok {
		if err != nil {
			return nil, "", err
		}
		data, err := json.Marshal(body)
		if err != nil {
			return nil, "", err
		}
		return bytes.NewReader(data), "application/json", nil
	}
	if len(operation.Params) > 0 {
		bodyParams := map[string]any{}
		for _, param := range operation.Params {
			if param.In != "body" {
				continue
			}
			value, ok := descriptorParamValue(params, param)
			if ok {
				bodyParams[param.Name] = value
			}
		}
		if strings.Contains(operation.Path, "/values/") && (operation.Name == "append" || operation.Name == "appendValues" || operation.Name == "update") {
			if _, ok := bodyParams["values"]; !ok {
				if objects, ok := sheetObjectsFromAny(firstPresent(params, "data", "objects")); ok {
					bodyParams["values"] = BuildSheetsAppendBody(objects, sheetHeaderOrderFromAny(params["headerOrder"]))["values"]
				}
			}
			delete(bodyParams, "data")
			delete(bodyParams, "objects")
		}
		if len(bodyParams) == 0 {
			return nil, "", nil
		}
		switch operation.BodyType {
		case "form":
			form := url.Values{}
			for key, value := range bodyParams {
				form.Set(key, fmt.Sprint(value))
			}
			return strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", nil
		case "multipart":
			var buffer bytes.Buffer
			writer := multipart.NewWriter(&buffer)
			for key, value := range bodyParams {
				switch typed := value.(type) {
				case []byte:
					part, err := writer.CreateFormFile(key, key)
					if err != nil {
						return nil, "", err
					}
					if _, err := part.Write(typed); err != nil {
						return nil, "", err
					}
				default:
					if err := writer.WriteField(key, fmt.Sprint(value)); err != nil {
						return nil, "", err
					}
				}
			}
			if err := writer.Close(); err != nil {
				return nil, "", err
			}
			return &buffer, writer.FormDataContentType(), nil
		case "raw":
			return strings.NewReader(fmt.Sprint(bodyParams["data"])), "text/plain", nil
		default:
			raw, err := json.Marshal(bodyParams)
			if err != nil {
				return nil, "", err
			}
			return bytes.NewReader(raw), "application/json", nil
		}
	}
	value, ok := params["body"]
	if !ok || value == nil {
		return nil, "", nil
	}
	if text, ok := value.(string); ok {
		return strings.NewReader(text), "application/json", nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, "", err
	}
	return bytes.NewReader(raw), "application/json", nil
}

func buildGmailRawMessage(params map[string]any) (string, error) {
	return buildGmailRawMessageWithOptions(params, false)
}

func buildGmailRawMessageWithOptions(params map[string]any, allowEmptyTo bool) (string, error) {
	to := stringValue(params, "to")
	if strings.TrimSpace(to) != "" {
		if _, err := mail.ParseAddressList(to); err != nil {
			return "", fmt.Errorf("gmail: invalid to address: %w", err)
		}
	} else if !allowEmptyTo {
		return "", fmt.Errorf("gmail: to is required")
	}
	attachments, err := gmailAttachmentsFromParams(params)
	if err != nil {
		return "", err
	}
	return BuildGmailEmailRaw(GmailEmailParams{
		To:           to,
		CC:           stringValue(params, "cc"),
		BCC:          stringValue(params, "bcc"),
		From:         stringValue(params, "from"),
		ReplyTo:      stringValue(params, "replyTo"),
		Subject:      stringValue(params, "subject"),
		Body:         stringValue(params, "body"),
		IsHTML:       boolValue(params, "isHtml", false),
		Attachments:  attachments,
		AllowEmptyTo: allowEmptyTo,
	})
}

func base64URL(data []byte) string {
	encoded := make([]byte, base64.RawURLEncoding.EncodedLen(len(data)))
	base64.RawURLEncoding.Encode(encoded, data)
	return string(encoded)
}

func boolValue(params map[string]any, key string, fallback bool) bool {
	value, ok := params[key]
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(typed, "true")
	default:
		return fallback
	}
}

func applyHeaderParams(req *http.Request, operation Operation, params map[string]any) {
	for _, param := range operation.Params {
		if param.In != "header" {
			continue
		}
		value, ok := descriptorParamValue(params, param)
		if ok {
			req.Header.Set(param.Name, fmt.Sprint(value))
		}
	}
}

func validateParams(operation Operation, params map[string]any) error {
	for _, param := range operation.Params {
		if !param.Required {
			continue
		}
		value, ok := descriptorParamValue(params, param)
		if !ok || strings.TrimSpace(fmt.Sprint(value)) == "" {
			return fmt.Errorf("required parameter %s is missing", param.Name)
		}
	}
	return nil
}

func effectivePagination(descriptor Descriptor, operation Operation) *Pagination {
	if operation.Pagination != nil {
		if operation.Pagination.Type == "" || operation.Pagination.Type == "none" {
			return nil
		}
		return operation.Pagination
	}
	if descriptor.Pagination != nil && descriptor.Pagination.Type != "" && descriptor.Pagination.Type != "none" {
		return descriptor.Pagination
	}
	return nil
}

func mergeParams(base map[string]any, patch map[string]any) map[string]any {
	merged := make(map[string]any, len(base)+len(patch))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range patch {
		merged[key] = value
	}
	return merged
}

func descriptorParamValue(params map[string]any, param Param) (any, bool) {
	value, ok := params[param.Name]
	if !ok || value == nil {
		if param.Default != nil {
			return param.Default, true
		}
		return nil, false
	}
	if object, ok := value.(map[string]any); ok {
		if locatorValue, ok := object["value"]; ok {
			return locatorValue, locatorValue != nil
		}
	}
	return value, true
}

func queryValues(param Param, value any) []string {
	if param.Type == "json" {
		data, err := json.Marshal(value)
		if err == nil {
			return []string{string(data)}
		}
	}
	switch typed := value.(type) {
	case []any:
		values := make([]string, 0, len(typed))
		for _, entry := range typed {
			values = append(values, fmt.Sprint(entry))
		}
		return values
	case []string:
		return append([]string(nil), typed...)
	default:
		return []string{fmt.Sprint(value)}
	}
}

func applyAuth(req *http.Request, params map[string]any) {
	if token := stringValue(params, "token", "accessToken", "apiKey"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if headerName := stringValue(params, "headerName"); headerName != "" {
		req.Header.Set(headerName, stringValue(params, "headerValue"))
	}
}

func parseHTTPError(statusCode int, body []byte) error {
	var decoded map[string]any
	if json.Unmarshal(body, &decoded) == nil {
		for _, key := range []string{"message", "error", "error_description"} {
			if value, ok := decoded[key]; ok && fmt.Sprint(value) != "" {
				return fmt.Errorf("API error %d: %s", statusCode, value)
			}
		}
	}
	return fmt.Errorf("HTTP %d: %s", statusCode, string(body))
}

func checkAPIError(body []byte, config *ErrorCheck) error {
	if config == nil || config.Field == "" {
		return nil
	}
	var decoded map[string]any
	if json.Unmarshal(body, &decoded) != nil {
		return nil
	}
	value, ok := decoded[config.Field]
	if !ok {
		return nil
	}
	if config.FalseIsError {
		if success, ok := value.(bool); ok && !success {
			message := "API error"
			if config.MessageField != "" {
				if raw, ok := decoded[config.MessageField]; ok {
					message = fmt.Sprint(raw)
				}
			}
			return fmt.Errorf("API error: %s", message)
		}
	}
	return nil
}

func transformedOutput(body []byte, operation Operation, params map[string]any) (dataplane.Output, error) {
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return dataplane.MainOutput([]dataplane.Item{{JSON: map[string]any{"data": string(body)}}}), nil
	}
	value := extractPath(decoded, operation.ResponseMap)
	if operation.ResponseMap == "" {
		value = decoded
	}
	if operation.ResponseMap == "values" && boolValue(params, "returnObjects", false) {
		if rows, ok := value.([]any); ok {
			return dataplane.MainOutput(toItems(sheetsRowsToObjects(rows))), nil
		}
	}
	if operation.Transform != "" {
		transformed, err := NewResponseTransformer().Apply(value, operation.Transform)
		if err != nil {
			return nil, err
		}
		value = transformed
	}
	return dataplane.MainOutput(toItems(value)), nil
}

func extractPath(value any, path string) any {
	current := value
	for _, part := range strings.Split(path, ".") {
		if part == "" {
			continue
		}
		switch typed := current.(type) {
		case map[string]any:
			current = typed[part]
		case []any:
			if part == "last" {
				if len(typed) == 0 {
					return nil
				}
				current = typed[len(typed)-1]
				continue
			}
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(typed) {
				return nil
			}
			current = typed[index]
		default:
			return nil
		}
	}
	return current
}

func toItems(value any) []dataplane.Item {
	switch typed := value.(type) {
	case []any:
		items := make([]dataplane.Item, 0, len(typed))
		for _, entry := range typed {
			items = append(items, dataplane.Item{JSON: itemJSON(entry)})
		}
		return items
	default:
		return []dataplane.Item{{JSON: itemJSON(value)}}
	}
}

func itemJSON(value any) map[string]any {
	if object, ok := value.(map[string]any); ok {
		return object
	}
	return map[string]any{"data": value}
}

func applyCredentialAuth(req *http.Request, credentials map[string]map[string]any) bool {
	if credential := credentialByType(credentials, "oAuth2Api", "googleOAuth2Api", "googleSheetsOAuth2Api", "microsoftTeamsOAuth2Api"); credential != nil {
		if token := credentialString(credential, "accessToken", "token"); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
			return true
		}
	}
	if credential := credentialByType(credentials, "httpHeaderAuth"); credential != nil {
		name := credentialString(credential, "name", "headerName")
		if name != "" {
			req.Header.Set(name, credentialString(credential, "value", "headerValue"))
			return true
		}
	}
	if credential := credentialByType(credentials, "httpBasicAuth"); credential != nil {
		req.SetBasicAuth(credentialString(credential, "user", "username"), credentialString(credential, "password"))
		return true
	}
	if credential := credentialByType(credentials, "jiraSoftwareCloudApi"); credential != nil {
		req.SetBasicAuth(credentialString(credential, "email", "user", "username"), credentialString(credential, "apiToken", "token", "password"))
		return true
	}
	if credential := credentialByType(credentials, "twilioApi"); credential != nil {
		req.SetBasicAuth(credentialString(credential, "accountSid", "username"), credentialString(credential, "authToken", "password"))
		return true
	}
	if credential := credentialByType(credentials, "discordBotApi"); credential != nil {
		if token := credentialString(credential, "botToken", "token", "accessToken"); token != "" {
			req.Header.Set("Authorization", "Bot "+token)
			return true
		}
	}
	if credential := credentialByType(credentials, "shopifyAccessTokenApi"); credential != nil {
		if token := credentialString(credential, "accessToken", "apiKey", "token"); token != "" {
			req.Header.Set("X-Shopify-Access-Token", token)
			return true
		}
	}
	if credential := credentialByType(credentials, "githubApi", "slackApi", "notionApi", "openAiApi", "stripeApi", "sendGridApi", "airtableApi", "hubspotApi"); credential != nil {
		if token := credentialString(credential, "accessToken", "apiKey", "token", "secretKey"); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
			return true
		}
	}
	return false
}

func credentialByType(credentials map[string]map[string]any, names ...string) map[string]any {
	for _, name := range names {
		if credential, ok := credentials[name]; ok {
			return credential
		}
	}
	for _, credential := range credentials {
		credentialType := fmt.Sprint(credential["type"])
		for _, name := range names {
			if credentialType == name {
				return credential
			}
		}
	}
	return nil
}

func credentialString(credential map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := credential[key]
		if !ok || value == nil {
			continue
		}
		text := fmt.Sprint(value)
		if strings.TrimSpace(text) != "" && text != "<nil>" {
			return text
		}
	}
	return ""
}

func stringValue(params map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := params[key]; ok {
			return fmt.Sprint(value)
		}
	}
	return ""
}

func firstPresent(params map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := params[key]; ok {
			return value
		}
	}
	return nil
}

func intValue(params map[string]any, key string, fallback int) int {
	value, ok := params[key]
	if !ok {
		return fallback
	}
	if number, ok := value.(float64); ok {
		return int(number)
	}
	return fallback
}
