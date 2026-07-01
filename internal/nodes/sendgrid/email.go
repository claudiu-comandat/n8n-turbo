package sendgrid

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func (n *Node) handleEmail(ctx context.Context, cred Credential, operation string, params map[string]any, item dataplane.Item) (map[string]any, error) {
	switch operation {
	case "send", "sendMail", "sendEmail":
		return n.sendEmail(ctx, cred, params, item)
	default:
		return nil, fmt.Errorf("unknown email operation %s", operation)
	}
}

func (n *Node) sendEmail(ctx context.Context, cred Credential, params map[string]any, item dataplane.Item) (map[string]any, error) {
	to, err := ParseEmailList(stringParam(params, "toEmail", "to"))
	if err != nil {
		return nil, err
	}
	additionalFields := nestedMap(params, "additionalFields")
	fromEmail := stringParam(params, "fromEmail", "from")
	if fromEmail == "" {
		return nil, fmt.Errorf("fromEmail is required")
	}
	req := SendEmailRequest{
		Personalizations: []Personalization{{To: to}},
		From:             EmailAddress{Email: fromEmail, Name: stringParam(params, "fromName")},
		Subject:          stringParam(params, "subject"),
		MailSettings:     map[string]any{"sandbox_mode": map[string]any{"enable": boolParam(additionalFields, "enableSandbox")}},
	}
	if cc := firstText(stringParam(params, "ccEmail", "cc"), stringParam(additionalFields, "ccEmail")); cc != "" {
		req.Personalizations[0].CC, err = ParseEmailList(cc)
		if err != nil {
			return nil, err
		}
	}
	if bcc := firstText(stringParam(params, "bccEmail", "bcc"), stringParam(additionalFields, "bccEmail")); bcc != "" {
		req.Personalizations[0].BCC, err = ParseEmailList(bcc)
		if err != nil {
			return nil, err
		}
	}
	if replyTo := firstText(stringParam(params, "replyTo", "replyToEmail"), stringParam(additionalFields, "replyToEmail")); replyTo != "" {
		parsed, err := ParseEmailList(replyTo)
		if err != nil {
			return nil, err
		}
		req.ReplyTo = &parsed[0]
		req.ReplyToList = parsed
	}
	req.TemplateID = stringParam(params, "templateId", "templateID")
	if boolParam(params, "dynamicTemplate") && req.TemplateID != "" {
		data, err := dynamicTemplateData(params)
		if err != nil {
			return nil, err
		}
		req.Personalizations[0].DynamicTemplateData = data
	} else {
		req.Personalizations[0].Subject = req.Subject
		req.Content = buildContent(params)
		if len(req.Content) == 0 {
			return nil, fmt.Errorf("textContent or htmlContent is required")
		}
	}
	if categories := firstStringSlice(stringSlice(params, "categories"), stringSlice(additionalFields, "categories")); len(categories) > 0 {
		req.Categories = categories
	}
	if sendAt := firstText(stringParam(params, "sendAt"), stringParam(additionalFields, "sendAt")); sendAt != "" {
		parsed, err := time.Parse(time.RFC3339, sendAt)
		if err != nil {
			return nil, err
		}
		req.SendAt = parsed.Unix()
		req.Personalizations[0].SendAt = req.SendAt
	}
	if headers, err := headerMap(additionalFields["headers"]); err != nil {
		return nil, err
	} else if len(headers) > 0 {
		req.Headers = headers
	}
	req.IPPoolName = stringParam(additionalFields, "ipPoolName")
	if boolParam(params, "disableTracking") {
		req.TrackingSettings = map[string]any{
			"click_tracking": map[string]any{"enable": false, "enable_text": false},
			"open_tracking":  map[string]any{"enable": false},
		}
	}
	attachments, err := n.buildAttachments(ctx, params, item)
	if err != nil {
		return nil, err
	}
	req.Attachments = attachments
	return n.doJSON(ctx, cred, http.MethodPost, "/mail/send", req)
}

func buildContent(params map[string]any) []Content {
	mode := strings.ToLower(stringParam(params, "contentMode"))
	text := stringParam(params, "textContent", "text", "body")
	html := stringParam(params, "htmlContent", "html")
	if contentValue := stringParam(params, "contentValue"); contentValue != "" {
		return []Content{{Type: firstText(stringParam(params, "contentType"), "text/plain"), Value: contentValue}}
	}
	content := []Content{}
	if (mode == "" || mode == "text" || mode == "both") && text != "" {
		content = append(content, Content{Type: "text/plain", Value: text})
	}
	if (mode == "html" || mode == "both" || (mode == "" && text == "")) && html != "" {
		content = append(content, Content{Type: "text/html", Value: html})
	}
	return content
}

func dynamicTemplateData(params map[string]any) (map[string]any, error) {
	if data, err := mapParam(params, "dynamicTemplateData"); err != nil || len(data) > 0 {
		return data, err
	}
	fields := nestedMap(params, "dynamicTemplateFields")["fields"]
	switch values := fields.(type) {
	case []any:
		out := map[string]any{}
		for _, value := range values {
			if object, ok := value.(map[string]any); ok {
				key := stringValue(object, "key")
				if key != "" {
					out[key] = object["value"]
				}
			}
		}
		return out, nil
	default:
		return map[string]any{}, nil
	}
}

func headerMap(raw any) (map[string]string, error) {
	object, ok := raw.(map[string]any)
	if !ok {
		return nil, nil
	}
	details, ok := object["details"].([]any)
	if !ok {
		return nil, nil
	}
	out := map[string]string{}
	for _, value := range details {
		detail, ok := value.(map[string]any)
		if !ok {
			continue
		}
		key := stringValue(detail, "key")
		if key != "" {
			out[key] = stringValue(detail, "value")
		}
	}
	return out, nil
}

func firstText(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstStringSlice(values ...[]string) []string {
	for _, value := range values {
		if len(value) > 0 {
			return value
		}
	}
	return nil
}

func mapParam(params map[string]any, key string) (map[string]any, error) {
	value, ok := params[key]
	if !ok || value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case map[string]any:
		return typed, nil
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil, nil
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(typed), &out); err != nil {
			return nil, err
		}
		return out, nil
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return nil, err
		}
		var out map[string]any
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, err
		}
		return out, nil
	}
}

func stringMapParam(params map[string]any, key string) (map[string]string, error) {
	value, err := mapParam(params, key)
	if err != nil || value == nil {
		return nil, err
	}
	out := make(map[string]string, len(value))
	for key, item := range value {
		out[key] = fmt.Sprint(item)
	}
	return out, nil
}
