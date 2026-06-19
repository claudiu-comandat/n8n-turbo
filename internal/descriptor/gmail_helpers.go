package descriptor

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"mime/multipart"
	"net/textproto"
	"strings"
)

type GmailAttachmentInfo struct {
	ID       string   `json:"id,omitempty"`
	Filename string   `json:"filename,omitempty"`
	MimeType string   `json:"mimeType,omitempty"`
	Size     int      `json:"size,omitempty"`
	PartID   string   `json:"partId,omitempty"`
	Headers  []Header `json:"headers,omitempty"`
}

type GmailParsedMessage struct {
	ID          string                `json:"id,omitempty"`
	ThreadID    string                `json:"threadId,omitempty"`
	Subject     string                `json:"subject,omitempty"`
	From        string                `json:"from,omitempty"`
	To          string                `json:"to,omitempty"`
	CC          string                `json:"cc,omitempty"`
	Date        string                `json:"date,omitempty"`
	Body        string                `json:"body,omitempty"`
	BodyHTML    string                `json:"bodyHtml,omitempty"`
	Labels      []string              `json:"labels,omitempty"`
	Attachments []GmailAttachmentInfo `json:"attachments,omitempty"`
	Snippet     string                `json:"snippet,omitempty"`
}

type Header struct {
	Name  string `json:"name,omitempty"`
	Value string `json:"value,omitempty"`
}

type GmailEmailAttachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

type GmailEmailParams struct {
	To          string
	CC          string
	BCC         string
	From        string
	ReplyTo     string
	Subject     string
	Body        string
	IsHTML      bool
	Attachments []GmailEmailAttachment
}

func BuildGmailEmailRaw(params GmailEmailParams) (string, error) {
	if strings.TrimSpace(params.To) == "" {
		return "", fmt.Errorf("gmail: to is required")
	}
	var buffer bytes.Buffer
	writeHeader(&buffer, "To", params.To)
	writeHeader(&buffer, "Cc", params.CC)
	writeHeader(&buffer, "Bcc", params.BCC)
	writeHeader(&buffer, "From", params.From)
	writeHeader(&buffer, "Reply-To", params.ReplyTo)
	writeHeader(&buffer, "Subject", params.Subject)
	buffer.WriteString("MIME-Version: 1.0\r\n")
	if len(params.Attachments) == 0 {
		if params.IsHTML {
			buffer.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
		} else {
			buffer.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
		}
		buffer.WriteString("\r\n")
		buffer.WriteString(params.Body)
		return base64URL(buffer.Bytes()), nil
	}
	writer := multipart.NewWriter(&buffer)
	buffer.WriteString("Content-Type: multipart/mixed; boundary=" + writer.Boundary() + "\r\n\r\n")
	bodyHeader := textproto.MIMEHeader{}
	if params.IsHTML {
		bodyHeader.Set("Content-Type", "text/html; charset=UTF-8")
	} else {
		bodyHeader.Set("Content-Type", "text/plain; charset=UTF-8")
	}
	bodyPart, err := writer.CreatePart(bodyHeader)
	if err != nil {
		return "", err
	}
	if _, err := bodyPart.Write([]byte(params.Body)); err != nil {
		return "", err
	}
	for _, attachment := range params.Attachments {
		filename := firstText(attachment.Filename, "attachment")
		contentType := firstText(attachment.ContentType, "application/octet-stream")
		header := textproto.MIMEHeader{}
		header.Set("Content-Type", contentType)
		header.Set("Content-Transfer-Encoding", "base64")
		header.Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(filename, `"`, "")+`"`)
		part, err := writer.CreatePart(header)
		if err != nil {
			return "", err
		}
		encoder := base64.NewEncoder(base64.StdEncoding, part)
		if _, err := encoder.Write(attachment.Data); err != nil {
			return "", err
		}
		if err := encoder.Close(); err != nil {
			return "", err
		}
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	return base64URL(buffer.Bytes()), nil
}

func ParseGmailMessage(raw map[string]any) (GmailParsedMessage, error) {
	message := GmailParsedMessage{
		ID:       stringFromAny(raw["id"]),
		ThreadID: stringFromAny(raw["threadId"]),
		Snippet:  stringFromAny(raw["snippet"]),
		Labels:   stringListFromAny(raw["labelIds"]),
	}
	payload, ok := mapFromAny(raw["payload"])
	if !ok {
		if encoded := stringFromAny(raw["raw"]); encoded != "" {
			decoded, err := DecodeGmailBase64URL(encoded)
			if err == nil {
				message.Body = string(decoded)
			}
		}
		return message, nil
	}
	for _, header := range gmailHeaders(payload["headers"]) {
		switch strings.ToLower(header.Name) {
		case "subject":
			message.Subject = header.Value
		case "from":
			message.From = header.Value
		case "to":
			message.To = header.Value
		case "cc":
			message.CC = header.Value
		case "date":
			message.Date = header.Value
		}
	}
	message.Body, message.BodyHTML, message.Attachments = extractGmailParts(payload)
	return message, nil
}

func DecodeGmailAttachmentData(item map[string]any) ([]byte, error) {
	data := stringFromAny(item["data"])
	if data == "" {
		body, ok := mapFromAny(item["body"])
		if ok {
			data = stringFromAny(body["data"])
		}
	}
	return DecodeGmailBase64URL(data)
}

func DecodeGmailBase64URL(value string) ([]byte, error) {
	cleaned := strings.NewReplacer("\n", "", "\r", "", " ", "").Replace(value)
	if cleaned == "" {
		return []byte{}, nil
	}
	if decoded, err := base64.RawURLEncoding.DecodeString(cleaned); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.URLEncoding.DecodeString(cleaned); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(cleaned); err == nil {
		return decoded, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		return nil, fmt.Errorf("gmail base64 decode: %w", err)
	}
	return decoded, nil
}

func gmailAttachmentsFromParams(params map[string]any) ([]GmailEmailAttachment, error) {
	raw, ok := params["attachments"]
	if !ok || raw == nil {
		return nil, nil
	}
	values, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("gmail attachments must be an array")
	}
	attachments := make([]GmailEmailAttachment, 0, len(values))
	for _, value := range values {
		entry, ok := mapFromAny(value)
		if !ok {
			return nil, fmt.Errorf("gmail attachment must be an object")
		}
		data, err := DecodeGmailBase64URL(firstText(stringFromAny(entry["data"]), stringFromAny(entry["content"])))
		if err != nil {
			return nil, err
		}
		attachments = append(attachments, GmailEmailAttachment{
			Filename:    firstText(stringFromAny(entry["filename"]), stringFromAny(entry["fileName"]), "attachment"),
			ContentType: firstText(stringFromAny(entry["contentType"]), stringFromAny(entry["mimeType"]), "application/octet-stream"),
			Data:        data,
		})
	}
	return attachments, nil
}

func extractGmailParts(payload map[string]any) (string, string, []GmailAttachmentInfo) {
	mimeType := stringFromAny(payload["mimeType"])
	partID := stringFromAny(payload["partId"])
	headers := gmailHeaders(payload["headers"])
	var textBody string
	var htmlBody string
	attachments := []GmailAttachmentInfo{}
	if body, ok := mapFromAny(payload["body"]); ok {
		if data := stringFromAny(body["data"]); data != "" {
			if decoded, err := DecodeGmailBase64URL(data); err == nil {
				switch {
				case strings.HasPrefix(mimeType, "text/plain"):
					textBody = string(decoded)
				case strings.HasPrefix(mimeType, "text/html"):
					htmlBody = string(decoded)
				}
			}
		}
		if attachmentID := stringFromAny(body["attachmentId"]); attachmentID != "" {
			attachments = append(attachments, GmailAttachmentInfo{
				ID:       attachmentID,
				Filename: firstText(stringFromAny(payload["filename"]), headerFilename(headers)),
				MimeType: mimeType,
				Size:     intFromAny(body["size"]),
				PartID:   partID,
				Headers:  headers,
			})
		}
	}
	for _, part := range sliceFromAny(payload["parts"]) {
		partMap, ok := mapFromAny(part)
		if !ok {
			continue
		}
		childText, childHTML, childAttachments := extractGmailParts(partMap)
		if childText != "" {
			textBody = childText
		}
		if childHTML != "" {
			htmlBody = childHTML
		}
		attachments = append(attachments, childAttachments...)
	}
	return textBody, htmlBody, attachments
}

func gmailHeaders(raw any) []Header {
	values := sliceFromAny(raw)
	headers := make([]Header, 0, len(values))
	for _, value := range values {
		entry, ok := mapFromAny(value)
		if ok {
			headers = append(headers, Header{Name: stringFromAny(entry["name"]), Value: stringFromAny(entry["value"])})
		}
	}
	return headers
}

func headerFilename(headers []Header) string {
	for _, header := range headers {
		if !strings.EqualFold(header.Name, "Content-Disposition") {
			continue
		}
		for _, part := range strings.Split(header.Value, ";") {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(strings.ToLower(part), "filename=") {
				return strings.Trim(strings.TrimPrefix(part, "filename="), `"`)
			}
		}
	}
	return ""
}

func writeHeader(buffer *bytes.Buffer, name string, value string) {
	if strings.TrimSpace(value) != "" {
		buffer.WriteString(name + ": " + value + "\r\n")
	}
}

func mapFromAny(value any) (map[string]any, bool) {
	if typed, ok := value.(map[string]any); ok {
		return typed, true
	}
	if typed, ok := value.(map[string]string); ok {
		result := make(map[string]any, len(typed))
		for key, entry := range typed {
			result[key] = entry
		}
		return result, true
	}
	return nil, false
}

func sliceFromAny(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case []map[string]any:
		values := make([]any, len(typed))
		for index, entry := range typed {
			values[index] = entry
		}
		return values
	case []map[string]string:
		values := make([]any, len(typed))
		for index, entry := range typed {
			values[index] = entry
		}
		return values
	default:
		return nil
	}
}

func stringFromAny(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func stringListFromAny(value any) []string {
	values := sliceFromAny(value)
	result := make([]string, 0, len(values))
	for _, entry := range values {
		text := stringFromAny(entry)
		if text != "" {
			result = append(result, text)
		}
	}
	return result
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	default:
		return 0
	}
}
