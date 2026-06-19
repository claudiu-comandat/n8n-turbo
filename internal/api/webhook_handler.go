package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type webhookMatch struct {
	Workflow dataplane.Workflow
	Node     dataplane.Node
	Params   map[string]string
}

func (s *Server) handleProductionWebhook(w http.ResponseWriter, r *http.Request) {
	s.handleWebhook(w, r, false)
}

func (s *Server) handleTestWebhook(w http.ResponseWriter, r *http.Request) {
	s.handleWebhook(w, r, true)
}

func (s *Server) handleProductionForm(w http.ResponseWriter, r *http.Request) {
	s.handleForm(w, r, false)
}

func (s *Server) handleTestForm(w http.ResponseWriter, r *http.Request) {
	s.handleForm(w, r, true)
}

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request, isTest bool) {
	path := strings.TrimPrefix(r.URL.Path, "/webhook/")
	if isTest {
		path = strings.TrimPrefix(r.URL.Path, "/webhook-test/")
	}
	match, err := s.findWebhookWorkflow(r, path, isTest)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	applyWebhookCORS(w, r, match.Node)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if shouldIgnoreWebhookBot(match.Node, r) {
		writeError(w, http.StatusForbidden, "webhook ignored bot request")
		return
	}
	if err := validateWebhookIPWhitelist(match.Node, r); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	payload, err := parseWebhookPayload(r, path, match.Params, match.Node.Parameters)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.validateWebhookRequestAuth(match.Node, r, payload.RawBody); err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	mode := "webhook"
	if isTest {
		mode = "webhook-test"
	}
	execution, err := s.executionStore.Create(r.Context(), match.Workflow, mode)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	variables, err := s.resolvedVariables(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	secrets, err := s.resolvedSecretsRequest(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	dispatchRequest := executionDispatchRequest{
		ExecutionID: execution.ID,
		Workflow:    match.Workflow,
		Mode:        mode,
		Options: engine.ExecuteOptions{
			Variables:    variables,
			Secrets:      secrets,
			BinaryStore:  s.binaryStore,
			Credentials:  s.resolveNodeCredentials,
			TriggerNode:  match.Node.Name,
			TriggerItems: []dataplane.Item{payload.Item},
			Mode:         mode,
			OnStarted:    s.pushExecutionStarted,
			OnNodeAfter:  s.pushNodeAfter,
			OnFinished:   s.pushExecutionFinished,
		},
		StartData: map[string]any{"destinationNode": match.Node.Name},
		ErrorName: "WebhookExecutionError",
	}
	if webhookResponseMode(match.Node) == "onreceived" {
		if err := s.dispatchWorkflowAsync(r.Context(), dispatchRequest); err != nil {
			writeError(w, http.StatusTooManyRequests, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"executionId": execution.ID, "workflowId": match.Workflow.ID, "status": "running", "message": "Workflow was started"}})
		return
	}
	dispatchResult := s.dispatchWorkflowSync(r.Context(), dispatchRequest)
	if dispatchResult.StartErr != nil {
		writeError(w, http.StatusTooManyRequests, dispatchResult.StartErr.Error())
		return
	}
	if dispatchResult.StoreErr != nil {
		writeError(w, http.StatusInternalServerError, dispatchResult.StoreErr.Error())
		return
	}
	if dispatchResult.RunErr != nil {
		writeError(w, http.StatusInternalServerError, dispatchResult.RunErr.Error())
		return
	}
	s.writeWebhookResponse(w, match, execution.ID, dispatchResult.Status, dispatchResult.Result)
}

func (s *Server) handleForm(w http.ResponseWriter, r *http.Request, isTest bool) {
	path := strings.TrimPrefix(r.URL.Path, "/form/")
	if isTest {
		path = strings.TrimPrefix(r.URL.Path, "/form-test/")
	}
	match, err := s.findFormWorkflow(r, path, isTest)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err := s.validateWaitingResumeAuth(r, match.Node); err != nil {
		w.Header().Set("WWW-Authenticate", `Basic realm="n8n"`)
		writeHTML(w, http.StatusUnauthorized, "Authentication required")
		return
	}
	if r.Method == http.MethodGet {
		writeHTML(w, http.StatusOK, waitingFormHTML(r, match.Node))
		return
	}
	payload, err := parseWebhookPayload(r, path, match.Params, match.Node.Parameters)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	mode := "form"
	if isTest {
		mode = "form-test"
	}
	execution, err := s.executionStore.Create(r.Context(), match.Workflow, mode)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	variables, err := s.resolvedVariables(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	secrets, err := s.resolvedSecretsRequest(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	dispatchResult := s.dispatchWorkflowSync(r.Context(), executionDispatchRequest{
		ExecutionID: execution.ID,
		Workflow:    match.Workflow,
		Mode:        mode,
		Options: engine.ExecuteOptions{
			Variables:    variables,
			Secrets:      secrets,
			BinaryStore:  s.binaryStore,
			Credentials:  s.resolveNodeCredentials,
			TriggerNode:  match.Node.Name,
			TriggerItems: []dataplane.Item{payload.Item},
			Mode:         mode,
			OnStarted:    s.pushExecutionStarted,
			OnNodeAfter:  s.pushNodeAfter,
			OnFinished:   s.pushExecutionFinished,
		},
		StartData: map[string]any{"destinationNode": match.Node.Name},
		ErrorName: "FormExecutionError",
	})
	if dispatchResult.StartErr != nil {
		writeError(w, http.StatusTooManyRequests, dispatchResult.StartErr.Error())
		return
	}
	if dispatchResult.StoreErr != nil {
		writeError(w, http.StatusInternalServerError, dispatchResult.StoreErr.Error())
		return
	}
	if dispatchResult.RunErr != nil {
		writeError(w, http.StatusInternalServerError, dispatchResult.RunErr.Error())
		return
	}
	if strings.EqualFold(parameterText(match.Node.Parameters, "responseMode"), "lastNode") {
		writeWebhookHTTPResponse(w, webhookResponseFromItems(http.StatusOK, lastNodeItems(dispatchResult.Result)))
		return
	}
	writeHTML(w, http.StatusOK, waitingFormCompletionHTML(match.Node))
}

func webhookResponseMode(node dataplane.Node) string {
	mode := strings.ToLower(parameterText(node.Parameters, "responseMode"))
	if mode == "" {
		return "onreceived"
	}
	return mode
}

func (s *Server) validateWebhookRequestAuth(node dataplane.Node, r *http.Request, rawBody []byte) error {
	mode := strings.ToLower(strings.TrimSpace(firstNonEmpty(
		parameterText(node.Parameters, "authMode"),
		parameterText(node.Parameters, "authentication"),
		parameterText(node.Parameters, "webhookAuthentication"),
	)))
	if mode == "basicauth" || mode == "basic" {
		return s.validateWaitingResumeAuth(r, node)
	}
	return validateWebhookAuth(node, r, rawBody)
}

func (s *Server) writeWebhookResponse(w http.ResponseWriter, match *webhookMatch, executionID string, status string, result *engine.Result) {
	response := webhookResponseFromTrigger(match.Node, executionID, match.Workflow.ID, status, result)
	switch strings.ToLower(fmt.Sprint(match.Node.Parameters["responseMode"])) {
	case "lastnode":
		// webhookResponseFromTrigger already applies responseData to the last node's data.
	case "responsenode":
		response = responseNodeWebhookResponse(match.Workflow, result)
	}
	writeWebhookHTTPResponse(w, response)
}

type webhookHTTPResponse struct {
	StatusCode  int
	Headers     map[string]string
	Body        any
	BinaryBody  []byte
	NoBody      bool
	ContentType string
}

func webhookResponseFromTrigger(node dataplane.Node, executionID string, workflowID string, status string, result *engine.Result) webhookHTTPResponse {
	options := webhookNodeOptions(node)
	statusCode := webhookResponseCode(node)
	headers := map[string]string{}
	for key, value := range stringMapParameter(node.Parameters, "responseHeaders") {
		headers[key] = value
	}
	for key, value := range stringMapParameter(options, "responseHeaders") {
		headers[key] = value
	}
	response := webhookHTTPResponse{
		StatusCode:  statusCode,
		Headers:     headers,
		Body:        defaultWebhookResponse(executionID, workflowID, status, result),
		NoBody:      strings.EqualFold(parameterText(node.Parameters, "responseData"), "noData") || boolParameter(options, "noResponseBody", boolParameter(node.Parameters, "noResponseBody", false)),
		ContentType: firstNonEmpty(parameterText(options, "responseContentType"), parameterText(node.Parameters, "responseContentType")),
	}
	applyWebhookResponseData(&response, node, lastNodeItems(result))
	return response
}

func applyWebhookResponseData(response *webhookHTTPResponse, node dataplane.Node, items []dataplane.Item) {
	switch strings.ToLower(parameterText(node.Parameters, "responseData")) {
	case "allentries":
		response.Body = itemJSONList(items)
		response.ContentType = firstNonEmpty(response.ContentType, "application/json")
	case "firstentryjson", "":
		if len(items) > 0 {
			response.Body = items[0].JSON
			response.ContentType = firstNonEmpty(response.ContentType, "application/json")
		}
	case "firstentrybinary":
		options := webhookNodeOptions(node)
		property := firstNonEmpty(parameterText(options, "binaryPropertyName"), parameterText(node.Parameters, "binaryPropertyName"), "data")
		if binary, ok := firstRespondBinary(items, property); ok {
			response.BinaryBody = decodeBinaryData(binary.Data)
			response.ContentType = firstNonEmpty(response.ContentType, binary.MimeType, "application/octet-stream")
			if binary.FileName != "" {
				response.Headers["Content-Disposition"] = fmt.Sprintf(`attachment; filename="%s"`, binary.FileName)
			}
		}
	case "nodata":
		response.NoBody = true
	}
}

func webhookResponseCode(node dataplane.Node) int {
	options := webhookNodeOptions(node)
	if code := fixedCollectionResponseCode(options); code != 0 {
		return code
	}
	if code := intParameter(options, "responseCode", 0); code != 0 {
		return code
	}
	return intParameter(node.Parameters, "responseCode", http.StatusOK)
}

func fixedCollectionResponseCode(options map[string]any) int {
	collection := mapParameter(options, "responseCode")
	if collection == nil {
		return 0
	}
	values := mapParameter(collection, "values")
	if values == nil {
		values = collection
	}
	if strings.EqualFold(parameterText(values, "responseCode"), "customCode") {
		return intParameter(values, "customCode", http.StatusOK)
	}
	return intParameter(values, "responseCode", 0)
}

func writeWebhookHTTPResponse(w http.ResponseWriter, response webhookHTTPResponse) {
	if response.StatusCode == 0 {
		response.StatusCode = http.StatusOK
	}
	for key, value := range response.Headers {
		w.Header().Set(key, value)
	}
	if response.ContentType != "" {
		w.Header().Set("Content-Type", response.ContentType)
	}
	if response.NoBody {
		w.WriteHeader(response.StatusCode)
		return
	}
	if response.BinaryBody != nil {
		if response.ContentType == "" {
			w.Header().Set("Content-Type", "application/octet-stream")
		}
		w.WriteHeader(response.StatusCode)
		_, _ = w.Write(response.BinaryBody)
		return
	}
	if text, ok := response.Body.(string); ok {
		if response.ContentType == "" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		}
		w.WriteHeader(response.StatusCode)
		_, _ = w.Write([]byte(text))
		return
	}
	if response.ContentType != "" {
		payload, err := json.Marshal(response.Body)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to serialize webhook response")
			return
		}
		w.WriteHeader(response.StatusCode)
		_, _ = w.Write(payload)
		return
	}
	writeJSON(w, response.StatusCode, response.Body)
}

func responseNodeWebhookResponse(workflow dataplane.Workflow, result *engine.Result) webhookHTTPResponse {
	if result == nil {
		return webhookHTTPResponse{StatusCode: http.StatusOK}
	}
	for _, node := range workflow.Nodes {
		if node.Type != "n8n-nodes-base.respondToWebhook" {
			continue
		}
		items := nodeItems(result, node.Name)
		return webhookResponseFromRespondNode(node, items)
	}
	return webhookResponseFromItems(http.StatusOK, lastNodeItems(result))
}

func responseNodeOutput(workflow dataplane.Workflow, result *engine.Result) any {
	if result == nil {
		return nil
	}
	for _, node := range workflow.Nodes {
		if node.Type != "n8n-nodes-base.respondToWebhook" {
			continue
		}
		tasks := result.RunData[node.Name]
		if len(tasks) == 0 {
			continue
		}
		output := tasks[len(tasks)-1].Data["main"]
		if len(output) > 0 {
			return output[0]
		}
	}
	return lastNodeOutput(result)
}

func webhookResponseFromRespondNode(node dataplane.Node, items []dataplane.Item) webhookHTTPResponse {
	statusCode := intParameter(node.Parameters, "statusCode", intParameter(node.Parameters, "responseCode", http.StatusOK))
	headers := respondNodeHeaders(node.Parameters)
	options := mapParameter(node.Parameters, "options")
	contentType := firstNonEmpty(parameterText(options, "responseContentType"), parameterText(node.Parameters, "responseContentType"))
	respondWith := strings.ToLower(parameterText(node.Parameters, "respondWith"))
	if respondWith == "" {
		respondWith = "firstincomingitem"
	}
	switch respondWith {
	case "allincomingitems":
		return webhookHTTPResponse{StatusCode: statusCode, Headers: headers, Body: itemJSONList(items), ContentType: firstNonEmpty(contentType, "application/json")}
	case "firstentryjson", "firstincomingitem":
		body := itemBody(items)
		if property := firstNonEmpty(parameterText(options, "responseDataPropertyName"), parameterText(node.Parameters, "responseDataPropertyName")); property != "" && len(items) > 0 {
			body = extractJSONPath(items[0].JSON, property)
		}
		return webhookHTTPResponse{StatusCode: statusCode, Headers: headers, Body: body, ContentType: firstNonEmpty(contentType, "application/json")}
	case "firstentrybinary", "binary":
		binary, ok := firstRespondBinary(items, firstNonEmpty(parameterText(options, "binaryPropertyName"), parameterText(node.Parameters, "binaryPropertyName"), "data"))
		if !ok {
			return webhookHTTPResponse{StatusCode: http.StatusNotFound, Headers: headers, Body: map[string]any{"error": "binary property not found"}}
		}
		if binary.FileName != "" {
			headers["Content-Disposition"] = fmt.Sprintf(`attachment; filename="%s"`, binary.FileName)
		}
		return webhookHTTPResponse{StatusCode: statusCode, Headers: headers, BinaryBody: decodeBinaryData(binary.Data), ContentType: firstNonEmpty(contentType, binary.MimeType, "application/octet-stream")}
	case "text":
		return webhookHTTPResponse{StatusCode: statusCode, Headers: headers, Body: parameterText(node.Parameters, "responseBody"), ContentType: firstNonEmpty(contentType, "text/plain; charset=utf-8")}
	case "json":
		var parsed any
		raw := parameterText(node.Parameters, "responseBody")
		if raw == "" {
			raw = "{}"
		}
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			parsed = map[string]any{"error": "invalid response json", "message": err.Error()}
			statusCode = http.StatusInternalServerError
		}
		return webhookHTTPResponse{StatusCode: statusCode, Headers: headers, Body: parsed, ContentType: firstNonEmpty(contentType, "application/json")}
	case "nodata":
		if statusCode == http.StatusOK {
			statusCode = http.StatusNoContent
		}
		return webhookHTTPResponse{StatusCode: statusCode, Headers: headers, NoBody: true}
	case "redirect":
		location := firstNonEmpty(parameterText(node.Parameters, "redirectURL"), parameterText(node.Parameters, "redirectUrl"), parameterText(node.Parameters, "responseBody"), "/")
		if statusCode == http.StatusOK {
			statusCode = http.StatusFound
		}
		headers["Location"] = location
		return webhookHTTPResponse{StatusCode: statusCode, Headers: headers, NoBody: true}
	default:
		response := webhookResponseFromItems(statusCode, items)
		response.Headers = headers
		response.ContentType = contentType
		return response
	}
}

func firstRespondBinary(items []dataplane.Item, property string) (dataplane.Binary, bool) {
	if property == "" {
		property = "data"
	}
	if len(items) == 0 || items[0].Binary == nil {
		return dataplane.Binary{}, false
	}
	binary, ok := items[0].Binary[property]
	return binary, ok
}

func decodeBinaryData(data string) []byte {
	if decoded, err := base64.StdEncoding.DecodeString(data); err == nil {
		return decoded
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(data); err == nil {
		return decoded
	}
	return []byte(data)
}

func extractJSONPath(data map[string]any, path string) any {
	if path == "" {
		return data
	}
	var current any = data
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = object[part]
	}
	return current
}

func webhookResponseFromItems(statusCode int, items []dataplane.Item) webhookHTTPResponse {
	return webhookHTTPResponse{StatusCode: statusCode, Body: itemBody(items)}
}

func itemBody(items []dataplane.Item) any {
	if len(items) == 0 {
		return nil
	}
	if len(items) == 1 {
		return items[0].JSON
	}
	return itemJSONList(items)
}

func itemJSONList(items []dataplane.Item) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, item.JSON)
	}
	return result
}

func lastNodeItems(result *engine.Result) []dataplane.Item {
	if result == nil || result.LastNodeExecuted == "" {
		return nil
	}
	return nodeItems(result, result.LastNodeExecuted)
}

func nodeItems(result *engine.Result, nodeName string) []dataplane.Item {
	if result == nil {
		return nil
	}
	tasks := result.RunData[nodeName]
	if len(tasks) == 0 {
		return nil
	}
	output := tasks[len(tasks)-1].Data["main"]
	if len(output) == 0 {
		return nil
	}
	return output[0]
}

func respondNodeHeaders(params map[string]any) map[string]string {
	headers := map[string]string{}
	for key, value := range stringMapParameter(params, "responseHeaders") {
		headers[key] = value
	}
	options := mapParameter(params, "options")
	for key, value := range stringMapParameter(options, "responseHeaders") {
		headers[key] = value
	}
	return headers
}

func defaultWebhookResponse(executionID string, workflowID string, status string, result *engine.Result) map[string]any {
	return map[string]any{
		"data": map[string]any{
			"executionId": executionID,
			"workflowId":  workflowID,
			"status":      status,
			"result":      lastNodeOutput(result),
		},
	}
}

func (s *Server) findWebhookWorkflow(r *http.Request, path string, isTest bool) (*webhookMatch, error) {
	return s.findHTTPTriggerWorkflow(r, path, isTest, map[string]bool{"n8n-nodes-base.webhook": true}, "webhook")
}

func (s *Server) handleFindWebhook(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": nil})
}

func (s *Server) findFormWorkflow(r *http.Request, path string, isTest bool) (*webhookMatch, error) {
	return s.findHTTPTriggerWorkflow(r, path, isTest, map[string]bool{"n8n-nodes-base.formTrigger": true}, "form")
}

func (s *Server) findHTTPTriggerWorkflow(r *http.Request, path string, isTest bool, nodeTypes map[string]bool, label string) (*webhookMatch, error) {
	rows, err := s.workflowStore.List(r.Context(), 250)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if !row.Active && !isTest {
			continue
		}
		workflow, err := workflowFromRow(&row)
		if err != nil {
			continue
		}
		for _, node := range workflow.Nodes {
			if !nodeTypes[node.Type] || node.Disabled {
				continue
			}
			method := r.Method
			if method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				method = r.Header.Get("Access-Control-Request-Method")
			}
			params, ok := matchWebhookPath(webhookPath(node), path)
			if ok && webhookMethod(node, method) {
				return &webhookMatch{Workflow: workflow, Node: node, Params: params}, nil
			}
		}
	}
	return nil, fmt.Errorf("%s %s %s not found", label, r.Method, path)
}

func webhookPath(node dataplane.Node) string {
	for _, key := range []string{"path", "webhookPath"} {
		if value, ok := node.Parameters[key]; ok {
			return strings.Trim(fmt.Sprint(value), "/")
		}
	}
	if node.WebhookID != "" {
		return strings.Trim(node.WebhookID, "/")
	}
	return strings.Trim(node.Name, "/")
}

func webhookMethod(node dataplane.Node, method string) bool {
	expected := strings.ToUpper(fmt.Sprint(node.Parameters["httpMethod"]))
	if expected == "" || expected == "<NIL>" {
		expected = strings.ToUpper(fmt.Sprint(node.Parameters["method"]))
	}
	if expected == "" || expected == "<NIL>" || expected == "ALL" || expected == "*" || expected == "ANY" {
		return true
	}
	for _, part := range strings.Split(expected, ",") {
		if strings.TrimSpace(part) == strings.ToUpper(method) {
			return true
		}
	}
	return expected == strings.ToUpper(method)
}

type webhookPayload struct {
	Item    dataplane.Item
	RawBody []byte
}

func parseWebhookPayload(r *http.Request, path string, params map[string]string, optionSources ...map[string]any) (webhookPayload, error) {
	rawBody, err := readWebhookRawBody(r)
	if err != nil {
		return webhookPayload{}, err
	}
	options := map[string]any{}
	if len(optionSources) > 0 {
		options = webhookOptionsMap(optionSources[0])
	}
	body := parseWebhookBody(rawBody, r.Header.Get("Content-Type"))
	if boolParameter(options, "rawBody", false) {
		body = string(rawBody)
	}
	item := dataplane.Item{JSON: map[string]any{
		"headers":     headerMap(r.Header),
		"query":       queryMap(r),
		"params":      stringMapAny(params),
		"body":        body,
		"rawBody":     string(rawBody),
		"contentType": r.Header.Get("Content-Type"),
		"clientIp":    webhookClientIP(r),
		"receivedAt":  time.Now().UTC().Format(time.RFC3339Nano),
		"httpMethod":  r.Method,
		"path":        path,
	}}
	if boolParameter(options, "binaryData", false) {
		property := firstNonEmpty(parameterText(options, "binaryPropertyName"), "data")
		item.Binary = map[string]dataplane.Binary{
			property: {
				Data:     base64.StdEncoding.EncodeToString(rawBody),
				MimeType: r.Header.Get("Content-Type"),
				FileName: parameterText(options, "binaryFileName"),
			},
		}
	}
	return webhookPayload{Item: item, RawBody: rawBody}, nil
}

func readWebhookRawBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	bytes, err := io.ReadAll(io.LimitReader(r.Body, 16*1024*1024))
	if err != nil {
		return nil, err
	}
	return bytes, nil
}

func parseWebhookBody(bytes []byte, contentType string) any {
	if len(bytes) == 0 {
		return nil
	}
	mediaType, params, _ := mime.ParseMediaType(contentType)
	switch mediaType {
	case "application/json", "application/vnd.api+json":
		var decoded any
		if json.Unmarshal(bytes, &decoded) == nil {
			return decoded
		}
	case "application/x-www-form-urlencoded":
		values, err := url.ParseQuery(string(bytes))
		if err == nil {
			return valuesToMap(values)
		}
	case "multipart/form-data":
		if boundary := params["boundary"]; boundary != "" {
			if parsed, err := parseMultipart(bytes, boundary); err == nil {
				return parsed
			}
		}
	}
	var decoded any
	if json.Unmarshal(bytes, &decoded) == nil {
		return decoded
	}
	return string(bytes)
}

func parseMultipart(raw []byte, boundary string) (map[string]any, error) {
	reader := multipart.NewReader(bytes.NewReader(raw), boundary)
	form, err := reader.ReadForm(32 << 20)
	if err != nil {
		return nil, err
	}
	result := map[string]any{}
	for key, values := range form.Value {
		if len(values) == 1 {
			result[key] = values[0]
		} else {
			result[key] = values
		}
	}
	files := map[string]any{}
	for key, values := range form.File {
		metadata := make([]map[string]any, 0, len(values))
		for _, file := range values {
			metadata = append(metadata, map[string]any{"filename": file.Filename, "size": file.Size, "contentType": file.Header.Get("Content-Type")})
		}
		files[key] = metadata
	}
	if len(files) > 0 {
		result["files"] = files
	}
	return result, nil
}

func headerMap(headers http.Header) map[string]any {
	result := make(map[string]any, len(headers))
	for key, value := range headers {
		if len(value) == 1 {
			result[key] = value[0]
		} else {
			result[key] = value
		}
	}
	return result
}

func queryMap(r *http.Request) map[string]any {
	values := r.URL.Query()
	result := make(map[string]any, len(values))
	for key, value := range values {
		if len(value) == 1 {
			result[key] = value[0]
		} else {
			result[key] = value
		}
	}
	return result
}

func valuesToMap(values url.Values) map[string]any {
	result := make(map[string]any, len(values))
	for key, value := range values {
		if len(value) == 1 {
			result[key] = value[0]
		} else {
			result[key] = value
		}
	}
	return result
}

func stringMapAny(values map[string]string) map[string]any {
	result := make(map[string]any, len(values)+1)
	for key, value := range values {
		result[key] = value
	}
	return result
}

func matchWebhookPath(pattern string, actual string) (map[string]string, bool) {
	patternParts := splitWebhookPath(pattern)
	actualParts := splitWebhookPath(actual)
	params := map[string]string{}
	for index := 0; index < len(patternParts); index++ {
		if index >= len(actualParts) {
			return nil, false
		}
		part := patternParts[index]
		actualPart := actualParts[index]
		if strings.HasPrefix(part, "*") {
			name := strings.TrimPrefix(part, "*")
			if name == "" {
				name = "wildcard"
			}
			params[name] = strings.Join(actualParts[index:], "/")
			return params, true
		}
		if strings.HasPrefix(part, ":") {
			params[strings.TrimPrefix(part, ":")] = actualPart
			continue
		}
		if part != actualPart {
			return nil, false
		}
	}
	if len(patternParts) != len(actualParts) {
		return nil, false
	}
	return params, true
}

func splitWebhookPath(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

func validateWebhookAuth(node dataplane.Node, r *http.Request, rawBody []byte) error {
	mode := strings.ToLower(strings.TrimSpace(firstNonEmpty(
		parameterText(node.Parameters, "authMode"),
		parameterText(node.Parameters, "authentication"),
		parameterText(node.Parameters, "webhookAuthentication"),
	)))
	if mode == "" || mode == "<nil>" || mode == "none" {
		return nil
	}
	switch mode {
	case "hmac":
		return validateWebhookHMAC(node, r, rawBody)
	case "headerauth", "header":
		headerName := firstNonEmpty(parameterText(node.Parameters, "headerName"), parameterText(node.Parameters, "authHeaderName"))
		headerValue := firstNonEmpty(parameterText(node.Parameters, "headerValue"), parameterText(node.Parameters, "authHeaderValue"))
		if headerName == "" || headerValue == "" || r.Header.Get(headerName) != headerValue {
			return fmt.Errorf("invalid webhook header auth")
		}
	case "basicauth", "basic":
		user, password, ok := r.BasicAuth()
		expectedUser := parameterText(node.Parameters, "user")
		expectedPassword := parameterText(node.Parameters, "password")
		if !ok || user != expectedUser || password != expectedPassword {
			return fmt.Errorf("invalid webhook basic auth")
		}
	}
	return nil
}

func validateWebhookHMAC(node dataplane.Node, r *http.Request, rawBody []byte) error {
	secret := firstNonEmpty(parameterText(node.Parameters, "hmacSecret"), parameterText(node.Parameters, "secret"))
	if secret == "" || secret == "<nil>" {
		return fmt.Errorf("missing webhook hmac secret")
	}
	algo := strings.ToLower(firstNonEmpty(parameterText(node.Parameters, "hmacAlgo"), "sha256"))
	headerName := firstNonEmpty(parameterText(node.Parameters, "hmacHeader"), "X-Hub-Signature-256")
	signature := r.Header.Get(headerName)
	if signature == "" {
		return fmt.Errorf("missing webhook hmac signature")
	}
	var expected []byte
	switch algo {
	case "sha1":
		mac := hmac.New(sha1.New, []byte(secret))
		_, _ = mac.Write(rawBody)
		expected = mac.Sum(nil)
	default:
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write(rawBody)
		expected = mac.Sum(nil)
		algo = "sha256"
	}
	expectedHex := hex.EncodeToString(expected)
	signature = strings.TrimSpace(signature)
	signature = strings.TrimPrefix(signature, algo+"=")
	if !hmac.Equal([]byte(signature), []byte(expectedHex)) {
		return fmt.Errorf("invalid webhook hmac signature")
	}
	return nil
}

func applyWebhookCORS(w http.ResponseWriter, r *http.Request, node dataplane.Node) {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return
	}
	allowed := strings.TrimSpace(firstNonEmpty(parameterText(webhookNodeOptions(node), "allowedOrigins"), parameterText(node.Parameters, "allowedOrigins")))
	if allowed == "" {
		return
	}
	if allowed == "*" || listContainsToken(allowed, origin) {
		value := origin
		if allowed == "*" {
			value = "*"
		}
		w.Header().Set("Access-Control-Allow-Origin", value)
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Methods", firstNonEmpty(r.Header.Get("Access-Control-Request-Method"), "GET,POST,PUT,PATCH,DELETE,HEAD,OPTIONS"))
		w.Header().Set("Access-Control-Allow-Headers", firstNonEmpty(r.Header.Get("Access-Control-Request-Headers"), "Content-Type,Authorization"))
	}
}

func shouldIgnoreWebhookBot(node dataplane.Node, r *http.Request) bool {
	if !boolParameter(webhookNodeOptions(node), "ignoreBots", boolParameter(node.Parameters, "ignoreBots", false)) {
		return false
	}
	agent := strings.ToLower(r.UserAgent())
	for _, marker := range []string{"bot", "crawler", "spider", "slurp", "preview", "facebookexternalhit", "discordbot", "slackbot"} {
		if strings.Contains(agent, marker) {
			return true
		}
	}
	return false
}

func validateWebhookIPWhitelist(node dataplane.Node, r *http.Request) error {
	whitelist := strings.TrimSpace(firstNonEmpty(parameterText(webhookNodeOptions(node), "ipWhitelist"), parameterText(node.Parameters, "ipWhitelist")))
	if whitelist == "" {
		return nil
	}
	client := net.ParseIP(webhookClientIP(r))
	if client == nil {
		return fmt.Errorf("invalid webhook client ip")
	}
	for _, entry := range strings.Split(whitelist, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			_, network, err := net.ParseCIDR(entry)
			if err == nil && network.Contains(client) {
				return nil
			}
			continue
		}
		if ip := net.ParseIP(entry); ip != nil && ip.Equal(client) {
			return nil
		}
	}
	return fmt.Errorf("webhook client ip is not allowed")
}

func webhookClientIP(r *http.Request) string {
	for _, header := range []string{"X-Forwarded-For", "X-Real-IP"} {
		if value := r.Header.Get(header); value != "" {
			return strings.TrimSpace(strings.Split(value, ",")[0])
		}
	}
	return strings.Split(r.RemoteAddr, ":")[0]
}

func intParameter(params map[string]any, key string, fallback int) int {
	value, ok := params[key]
	if !ok || value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case int:
		return typed
	case float64:
		return int(typed)
	case json.Number:
		parsed, err := typed.Int64()
		if err == nil {
			return int(parsed)
		}
	default:
		var parsed int
		if _, err := fmt.Sscanf(fmt.Sprint(value), "%d", &parsed); err == nil {
			return parsed
		}
	}
	return fallback
}

func boolParameter(params map[string]any, key string, fallback bool) bool {
	value, ok := params[key]
	if !ok || value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(typed, "true") || typed == "1"
	default:
		return fallback
	}
}

func mapParameter(params map[string]any, key string) map[string]any {
	value, ok := params[key]
	if !ok || value == nil {
		return nil
	}
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return nil
}

func stringMapParameter(params map[string]any, key string) map[string]string {
	result := map[string]string{}
	value := mapParameter(params, key)
	for _, collectionKey := range []string{"entries", "values"} {
		if entries, ok := value[collectionKey].([]any); ok {
			for _, entry := range entries {
				object, ok := entry.(map[string]any)
				if !ok {
					continue
				}
				name := firstNonEmpty(parameterText(object, "name"), parameterText(object, "key"))
				if name != "" {
					result[name] = firstNonEmpty(parameterText(object, "value"), parameterText(object, "headerValue"))
				}
			}
			return result
		}
	}
	for name, raw := range value {
		if name == "entries" || name == "values" {
			continue
		}
		result[name] = fmt.Sprint(raw)
	}
	return result
}

func parameterText(params map[string]any, key string) string {
	value, ok := params[key]
	if !ok || value == nil {
		return ""
	}
	text := fmt.Sprint(value)
	if text == "<nil>" {
		return ""
	}
	return text
}

func webhookNodeOptions(node dataplane.Node) map[string]any {
	return webhookOptionsMap(node.Parameters)
}

func webhookOptionsMap(params map[string]any) map[string]any {
	options := mapParameter(params, "options")
	if options == nil {
		return map[string]any{}
	}
	return options
}

func listContainsToken(list string, token string) bool {
	for _, part := range strings.Split(list, ",") {
		if strings.TrimSpace(part) == token {
			return true
		}
	}
	return false
}

func lastNodeOutput(result *engine.Result) any {
	if result == nil || result.LastNodeExecuted == "" {
		return nil
	}
	tasks := result.RunData[result.LastNodeExecuted]
	if len(tasks) == 0 {
		return nil
	}
	output := tasks[len(tasks)-1].Data["main"]
	if len(output) == 0 {
		return nil
	}
	return output[0]
}
