package sendgrid

import (
	"fmt"
	"strings"
)

const NodeType = "n8n-nodes-base.sendGrid"

type EmailAddress struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

type Personalization struct {
	To                  []EmailAddress    `json:"to"`
	CC                  []EmailAddress    `json:"cc,omitempty"`
	BCC                 []EmailAddress    `json:"bcc,omitempty"`
	Subject             string            `json:"subject,omitempty"`
	Headers             map[string]string `json:"headers,omitempty"`
	Substitutions       map[string]string `json:"substitutions,omitempty"`
	DynamicTemplateData map[string]any    `json:"dynamic_template_data,omitempty"`
	CustomArgs          map[string]string `json:"custom_args,omitempty"`
	SendAt              int64             `json:"send_at,omitempty"`
}

type Content struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type Attachment struct {
	Content     string `json:"content"`
	Type        string `json:"type"`
	Filename    string `json:"filename"`
	Disposition string `json:"disposition,omitempty"`
	ContentID   string `json:"content_id,omitempty"`
}

type SendEmailRequest struct {
	Personalizations []Personalization `json:"personalizations"`
	From             EmailAddress      `json:"from"`
	ReplyTo          *EmailAddress     `json:"reply_to,omitempty"`
	Subject          string            `json:"subject,omitempty"`
	Content          []Content         `json:"content,omitempty"`
	Attachments      []Attachment      `json:"attachments,omitempty"`
	TemplateID       string            `json:"template_id,omitempty"`
	Headers          map[string]string `json:"headers,omitempty"`
	Categories       []string          `json:"categories,omitempty"`
	CustomArgs       map[string]string `json:"custom_args,omitempty"`
	SendAt           int64             `json:"send_at,omitempty"`
	BatchID          string            `json:"batch_id,omitempty"`
	IPPoolName       string            `json:"ip_pool_name,omitempty"`
	TrackingSettings map[string]any    `json:"tracking_settings,omitempty"`
}

type Contact struct {
	Email            string            `json:"email"`
	FirstName        string            `json:"first_name,omitempty"`
	LastName         string            `json:"last_name,omitempty"`
	AddressLine1     string            `json:"address_line_1,omitempty"`
	AddressLine2     string            `json:"address_line_2,omitempty"`
	City             string            `json:"city,omitempty"`
	StateProvince    string            `json:"state_province_region,omitempty"`
	Country          string            `json:"country,omitempty"`
	PostalCode       string            `json:"postal_code,omitempty"`
	PhoneNumber      string            `json:"phone_number,omitempty"`
	UniqueExternalID string            `json:"unique_name,omitempty"`
	CustomFields     map[string]string `json:"custom_fields,omitempty"`
}

type Error struct {
	Errors []struct {
		Message string `json:"message"`
		Field   string `json:"field"`
		Help    string `json:"help"`
	} `json:"errors"`
}

func (e *Error) Error() string {
	if len(e.Errors) == 0 {
		return "sendgrid unknown error"
	}
	messages := make([]string, 0, len(e.Errors))
	for _, item := range e.Errors {
		message := item.Message
		if item.Field != "" {
			message += " field=" + item.Field
		}
		messages = append(messages, message)
	}
	return "sendgrid: " + strings.Join(messages, "; ")
}

func ParseEmailList(input string) ([]EmailAddress, error) {
	parts := strings.Split(input, ",")
	addresses := make([]EmailAddress, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if index := strings.Index(part, "<"); index >= 0 {
			name := strings.TrimSpace(part[:index])
			email := strings.Trim(strings.TrimSpace(part[index+1:]), ">")
			if email != "" {
				addresses = append(addresses, EmailAddress{Email: email, Name: name})
			}
			continue
		}
		addresses = append(addresses, EmailAddress{Email: part})
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("no valid email addresses found")
	}
	return addresses, nil
}
