package api

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
	"github.com/n8n-io/n8n-turbo/internal/persistence"
)

func (s *Server) handleWaitingWebhook(w http.ResponseWriter, r *http.Request) {
	s.handleWaitingResume(w, r, false)
}

func (s *Server) handleWaitingForm(w http.ResponseWriter, r *http.Request) {
	s.handleWaitingResume(w, r, true)
}

func (s *Server) handleWaitingResume(w http.ResponseWriter, r *http.Request, form bool) {
	executionID, suffix := waitingResumePath(r.URL.Path, form)
	if executionID == "" {
		writeError(w, http.StatusNotFound, "waiting execution not found")
		return
	}
	row, err := s.executionStore.GetByID(r.Context(), executionID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	stored, err := runExecutionDataFromStored(row.Data)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if stored.ResumeToken != "" {
		valid, tokenSuffix := validateWaitingResumeToken(r, stored.ResumeToken)
		if !valid {
			if form {
				writeHTML(w, http.StatusUnauthorized, "Invalid token")
			} else {
				writeError(w, http.StatusUnauthorized, "Invalid token")
			}
			return
		}
		if suffix == "" && tokenSuffix != "" {
			suffix = tokenSuffix
		}
	}
	if form && suffix == "n8n-execution-status" {
		status := row.Status
		if status == "waiting" {
			status = "form-waiting"
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(status))
		return
	}
	if row.Status == "running" {
		writeError(w, http.StatusConflict, fmt.Sprintf("The execution %q is running already.", executionID))
		return
	}
	if row.Status != "waiting" {
		writeError(w, http.StatusConflict, fmt.Sprintf("The execution %q is not waiting.", executionID))
		return
	}
	workflow, waitNode, err := waitingWorkflowAndNode(*row, form, suffix, r.Method)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err := s.validateWaitingResumeAuth(r, waitNode); err != nil {
		w.Header().Set("WWW-Authenticate", `Basic realm="n8n"`)
		if form {
			writeHTML(w, http.StatusUnauthorized, "Authentication required")
		} else {
			writeError(w, http.StatusUnauthorized, err.Error())
		}
		return
	}
	if form && r.Method == http.MethodGet {
		writeHTML(w, http.StatusOK, waitingFormHTML(r, waitNode))
		return
	}
	payload, err := parseWebhookPayload(r, suffix, map[string]string{})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	resumeCtx, cancel := context.WithTimeout(context.Background(), s.executionJobTimeout()+15*time.Second)
	defer cancel()
	result := s.resumeWaitingExecutionResult(resumeCtx, *row, dataplane.MainOutput([]dataplane.Item{payload.Item}))
	if result.StartErr != nil {
		writeError(w, http.StatusTooManyRequests, result.StartErr.Error())
		return
	}
	if result.StoreErr != nil {
		writeError(w, http.StatusInternalServerError, result.StoreErr.Error())
		return
	}
	if result.RunErr != nil {
		writeError(w, http.StatusInternalServerError, result.RunErr.Error())
		return
	}
	if form {
		writeHTML(w, http.StatusOK, waitingFormCompletionHTML(waitNode))
		return
	}
	writeWebhookHTTPResponse(w, webhookResponseFromTrigger(waitNode, executionID, workflow.ID, result.Status, result.Result))
}

func (s *Server) validateWaitingResumeAuth(r *http.Request, node dataplane.Node) error {
	mode := strings.ToLower(strings.TrimSpace(firstNonEmpty(
		parameterText(node.Parameters, "incomingAuthentication"),
		parameterText(node.Parameters, "authMode"),
		parameterText(node.Parameters, "authentication"),
		parameterText(node.Parameters, "webhookAuthentication"),
	)))
	if mode == "" || mode == "<nil>" || mode == "none" {
		return nil
	}
	if mode != "basicauth" && mode != "basic" {
		return fmt.Errorf("unsupported waiting authentication")
	}
	user, password, ok := r.BasicAuth()
	expectedUser := firstNonEmpty(parameterText(node.Parameters, "user"), parameterText(node.Parameters, "username"))
	expectedPassword := parameterText(node.Parameters, "password")
	if (expectedUser == "" || expectedPassword == "") && s.credentialStore != nil {
		resolved, err := s.resolveNodeCredentials(r.Context(), node)
		if err == nil {
			for _, credential := range resolved {
				expectedUser = firstNonEmpty(expectedUser, stringFromMap(credential, "user"), stringFromMap(credential, "username"))
				expectedPassword = firstNonEmpty(expectedPassword, stringFromMap(credential, "password"))
			}
		}
	}
	if !ok || expectedUser == "" || expectedPassword == "" || user != expectedUser || password != expectedPassword {
		return fmt.Errorf("invalid waiting basic auth")
	}
	return nil
}

type waitingFormField struct {
	Name        string
	Label       string
	Type        string
	Placeholder string
	Default     string
	Required    bool
	Multiple    bool
	Accept      string
	Options     []string
	HTML        string
}

func waitingFormHTML(r *http.Request, node dataplane.Node) string {
	title := firstNonEmpty(parameterText(node.Parameters, "formTitle"), node.Name)
	description := parameterText(node.Parameters, "formDescription")
	fields := waitingFormFields(node)
	enctype := ""
	for _, field := range fields {
		if field.Type == "file" {
			enctype = ` enctype="multipart/form-data"`
			break
		}
	}
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><title>")
	b.WriteString(html.EscapeString(title))
	b.WriteString("</title><style>body{margin:0;background:#f5f1e8;color:#211a13;font-family:Georgia,serif}main{max-width:720px;margin:48px auto;padding:36px;background:#fffaf0;border:1px solid #decfb6;box-shadow:0 24px 80px rgba(49,35,16,.14)}h1{margin:0 0 12px;font-size:42px;line-height:1.05}p{color:#665944;line-height:1.55}.field{margin:22px 0}label{display:block;font-weight:700;margin-bottom:8px}input,textarea,select{box-sizing:border-box;width:100%;font:inherit;padding:12px 14px;border:1px solid #bcae96;background:white;color:#211a13}textarea{min-height:130px}button{margin-top:20px;padding:13px 22px;border:0;background:#1f4d3a;color:white;font-weight:700;font:inherit;cursor:pointer}.choice{display:flex;gap:10px;align-items:center;margin:8px 0}.choice input{width:auto}</style></head><body><main><h1>")
	b.WriteString(html.EscapeString(title))
	b.WriteString("</h1>")
	if description != "" {
		b.WriteString("<p>")
		b.WriteString(html.EscapeString(description))
		b.WriteString("</p>")
	}
	b.WriteString("<form method=\"post\" action=\"")
	b.WriteString(html.EscapeString(r.URL.RequestURI()))
	b.WriteString("\"")
	b.WriteString(enctype)
	b.WriteString(">")
	for _, field := range fields {
		b.WriteString(waitingFormFieldHTML(field))
	}
	b.WriteString("<button type=\"submit\">Submit</button></form></main></body></html>")
	return b.String()
}

func waitingFormCompletionHTML(node dataplane.Node) string {
	title := firstNonEmpty(parameterText(node.Parameters, "completionTitle"), "Form submitted")
	message := firstNonEmpty(parameterText(node.Parameters, "completionMessage"), "Your response has been recorded.")
	return "<!doctype html><html><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><title>" + html.EscapeString(title) + "</title><style>body{margin:0;background:#f5f1e8;color:#211a13;font-family:Georgia,serif}main{max-width:640px;margin:64px auto;padding:36px;background:#fffaf0;border:1px solid #decfb6;box-shadow:0 24px 80px rgba(49,35,16,.14)}h1{margin:0 0 12px;font-size:38px}p{color:#665944;line-height:1.55}</style></head><body><main><h1>" + html.EscapeString(title) + "</h1><p>" + html.EscapeString(message) + "</p></main></body></html>"
}

func waitingFormFieldHTML(field waitingFormField) string {
	name := html.EscapeString(field.Name)
	label := html.EscapeString(field.Label)
	required := ""
	if field.Required {
		required = " required"
	}
	placeholder := ""
	if field.Placeholder != "" {
		placeholder = ` placeholder="` + html.EscapeString(field.Placeholder) + `"`
	}
	value := ""
	if field.Default != "" {
		value = ` value="` + html.EscapeString(field.Default) + `"`
	}
	switch field.Type {
	case "html":
		return `<div class="field">` + html.EscapeString(field.HTML) + `</div>`
	case "hiddenField":
		return `<input type="hidden" name="` + name + `"` + value + `>`
	case "textarea":
		return `<div class="field"><label for="` + name + `">` + label + `</label><textarea id="` + name + `" name="` + name + `"` + placeholder + required + `>` + html.EscapeString(field.Default) + `</textarea></div>`
	case "dropdown":
		multiple := ""
		if field.Multiple {
			multiple = " multiple"
		}
		var b strings.Builder
		b.WriteString(`<div class="field"><label for="`)
		b.WriteString(name)
		b.WriteString(`">`)
		b.WriteString(label)
		b.WriteString(`</label><select id="`)
		b.WriteString(name)
		b.WriteString(`" name="`)
		b.WriteString(name)
		b.WriteString(`"`)
		b.WriteString(multiple)
		b.WriteString(required)
		b.WriteString(`>`)
		for _, option := range field.Options {
			escaped := html.EscapeString(option)
			selected := ""
			if option == field.Default {
				selected = " selected"
			}
			b.WriteString(`<option value="`)
			b.WriteString(escaped)
			b.WriteString(`"`)
			b.WriteString(selected)
			b.WriteString(`>`)
			b.WriteString(escaped)
			b.WriteString(`</option>`)
		}
		b.WriteString(`</select></div>`)
		return b.String()
	case "radio", "checkbox":
		inputType := field.Type
		var b strings.Builder
		b.WriteString(`<fieldset class="field"><legend>`)
		b.WriteString(label)
		b.WriteString(`</legend>`)
		for _, option := range field.Options {
			escaped := html.EscapeString(option)
			checked := ""
			if option == field.Default {
				checked = " checked"
			}
			b.WriteString(`<label class="choice"><input type="`)
			b.WriteString(inputType)
			b.WriteString(`" name="`)
			b.WriteString(name)
			b.WriteString(`" value="`)
			b.WriteString(escaped)
			b.WriteString(`"`)
			b.WriteString(checked)
			b.WriteString(required)
			b.WriteString(`>`)
			b.WriteString(escaped)
			b.WriteString(`</label>`)
		}
		b.WriteString(`</fieldset>`)
		return b.String()
	case "file":
		multiple := ""
		if field.Multiple {
			multiple = " multiple"
		}
		accept := ""
		if field.Accept != "" {
			accept = ` accept="` + html.EscapeString(field.Accept) + `"`
		}
		return `<div class="field"><label for="` + name + `">` + label + `</label><input id="` + name + `" type="file" name="` + name + `"` + accept + multiple + required + `></div>`
	default:
		inputType := field.Type
		if inputType == "" || inputType == "multiSelect" {
			inputType = "text"
		}
		return `<div class="field"><label for="` + name + `">` + label + `</label><input id="` + name + `" type="` + html.EscapeString(inputType) + `" name="` + name + `"` + placeholder + value + required + `></div>`
	}
}

func waitingFormFields(node dataplane.Node) []waitingFormField {
	raw := node.Parameters["formFields"]
	if values := mapValues(raw, "values"); len(values) > 0 {
		raw = values
	}
	items := anySlice(raw)
	fields := make([]waitingFormField, 0, len(items))
	for index, item := range items {
		data := waitingAnyMap(item)
		if len(data) == 0 {
			continue
		}
		name := firstNonEmpty(parameterText(data, "fieldName"), parameterText(data, "fieldLabel"), fmt.Sprintf("field_%d", index+1))
		label := firstNonEmpty(parameterText(data, "fieldLabel"), name)
		fieldType := firstNonEmpty(parameterText(data, "fieldType"), "text")
		field := waitingFormField{
			Name:        name,
			Label:       label,
			Type:        fieldType,
			Placeholder: parameterText(data, "placeholder"),
			Default:     firstNonEmpty(parameterText(data, "defaultValue"), parameterText(data, "fieldValue")),
			Required:    boolParameter(data, "requiredField", false),
			Multiple:    boolParameter(data, "multipleFiles", false) || boolParameter(data, "multiselect", false),
			Accept:      parameterText(data, "acceptFileTypes"),
			Options:     waitingFormOptions(data["fieldOptions"]),
			HTML:        parameterText(data, "html"),
		}
		fields = append(fields, field)
	}
	return fields
}

func waitingFormOptions(raw any) []string {
	if values := mapValues(raw, "values"); len(values) > 0 {
		raw = values
	}
	items := anySlice(raw)
	options := make([]string, 0, len(items))
	for _, item := range items {
		if data := waitingAnyMap(item); len(data) > 0 {
			if option := parameterText(data, "option"); option != "" {
				options = append(options, option)
			}
			continue
		}
		if text := fmt.Sprint(item); text != "" && text != "<nil>" {
			options = append(options, text)
		}
	}
	return options
}

func mapValues(raw any, key string) []any {
	if data := waitingAnyMap(raw); len(data) > 0 {
		return anySlice(data[key])
	}
	return nil
}

func anySlice(raw any) []any {
	switch typed := raw.(type) {
	case []any:
		return typed
	case []map[string]any:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, item)
		}
		return result
	default:
		return nil
	}
}

func waitingAnyMap(raw any) map[string]any {
	switch typed := raw.(type) {
	case map[string]any:
		return typed
	case map[string]string:
		result := make(map[string]any, len(typed))
		for key, value := range typed {
			result[key] = value
		}
		return result
	default:
		return nil
	}
}

func (s *Server) resumeWaitingExecutionResult(ctx context.Context, row persistence.ExecutionRow, output dataplane.Output) executionDispatchResult {
	workflow, runData, initialInputs, err := waitingResumeRequestData(row, output)
	if err != nil {
		return executionDispatchResult{Status: "error", RunErr: err}
	}
	variables, err := s.resolvedVariablesContext(ctx)
	if err != nil {
		variables = map[string]any{}
	}
	secrets, err := s.resolvedSecrets(ctx)
	if err != nil {
		secrets = map[string]map[string]string{}
	}
	return s.dispatchWorkflowSync(ctx, executionDispatchRequest{
		ExecutionID: row.ID,
		Workflow:    workflow,
		Mode:        row.Mode,
		Options: engine.ExecuteOptions{
			Variables:     variables,
			Secrets:       secrets,
			InitialInputs: initialInputs,
			RunData:       runData.ResultData.RunData,
			PinData:       runData.ResultData.PinData,
			BinaryStore:   s.binaryStore,
			Credentials:   s.resolveNodeCredentials,
			OnStarted:     s.pushExecutionStarted,
			OnNodeAfter:   s.pushNodeAfter,
			OnFinished:    s.pushExecutionFinished,
		},
		StartData: runData.StartData,
		PinData:   runData.ResultData.PinData,
		ErrorName: "WaitResumeExecutionError",
	})
}

func waitingResumePath(path string, form bool) (string, string) {
	prefix := "/webhook-waiting/"
	if form {
		prefix = "/form-waiting/"
	}
	rest := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	if rest == "" || rest == path {
		return "", ""
	}
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], strings.Trim(parts[1], "/")
}

func validateWaitingResumeToken(r *http.Request, stored string) (bool, string) {
	token := r.URL.Query().Get("signature")
	suffix := ""
	if strings.Contains(token, "/") {
		parts := strings.SplitN(token, "/", 2)
		token = parts[0]
		suffix = strings.Trim(parts[1], "/")
	}
	if token == "" || len(token) != len(stored) {
		return false, suffix
	}
	return hmac.Equal([]byte(token), []byte(stored)), suffix
}

func writeHTML(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func waitingWorkflowAndNode(row persistence.ExecutionRow, form bool, suffix string, method string) (dataplane.Workflow, dataplane.Node, error) {
	var workflow dataplane.Workflow
	if err := json.Unmarshal(row.WorkflowData, &workflow); err != nil {
		return workflow, dataplane.Node{}, err
	}
	runData, err := runExecutionDataFromStored(row.Data)
	if err != nil {
		return workflow, dataplane.Node{}, err
	}
	node, ok := dataplane.NodeByName(workflow, runData.ResultData.LastNodeExecuted)
	if !ok {
		return workflow, dataplane.Node{}, fmt.Errorf("waiting node not found")
	}
	if node.Type != "n8n-nodes-base.wait" {
		return workflow, dataplane.Node{}, fmt.Errorf("waiting node is not a Wait node")
	}
	resume := strings.ToLower(parameterText(node.Parameters, "resume"))
	if form && resume != "form" {
		return workflow, dataplane.Node{}, fmt.Errorf("waiting form not found")
	}
	if !form && resume != "webhook" {
		return workflow, dataplane.Node{}, fmt.Errorf("waiting webhook not found")
	}
	expectedSuffix := firstNonEmpty(parameterText(mapParameter(node.Parameters, "options"), "webhookSuffix"), parameterText(node.Parameters, "webhookSuffix"))
	if expected := strings.Trim(expectedSuffix, "/"); expected != "" && expected != suffix {
		return workflow, dataplane.Node{}, fmt.Errorf("waiting webhook suffix not found")
	}
	if !form && !webhookMethod(node, method) {
		return workflow, dataplane.Node{}, fmt.Errorf("waiting webhook method not found")
	}
	return workflow, node, nil
}

func waitingResumeRequestData(row persistence.ExecutionRow, output dataplane.Output) (dataplane.Workflow, dataplane.RunExecutionData, map[string]map[int][]dataplane.Item, error) {
	var workflow dataplane.Workflow
	if err := json.Unmarshal(row.WorkflowData, &workflow); err != nil {
		return workflow, dataplane.RunExecutionData{}, nil, err
	}
	runData, err := runExecutionDataFromStored(row.Data)
	if err != nil {
		return workflow, dataplane.RunExecutionData{}, nil, err
	}
	lastNode := runData.ResultData.LastNodeExecuted
	if lastNode == "" {
		return workflow, runData, nil, fmt.Errorf("waiting execution has no last node")
	}
	if output != nil {
		replaceLastNodeOutput(runData.ResultData.RunData, lastNode, output)
	}
	initialInputs := resumeInputsAfterNode(workflow, lastNode, runData.ResultData.RunData)
	if len(initialInputs) == 0 {
		return workflow, runData, nil, fmt.Errorf("waiting node has no downstream inputs")
	}
	return workflow, runData, initialInputs, nil
}
