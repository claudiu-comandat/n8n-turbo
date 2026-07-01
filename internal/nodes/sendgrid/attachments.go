package sendgrid

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/n8n-io/n8n-turbo/internal/binarydata"
	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

const maxAttachmentBytes = 25 * 1024 * 1024

func (n *Node) buildAttachments(ctx context.Context, params map[string]any, item dataplane.Item) ([]Attachment, error) {
	out := []Attachment{}
	properties := stringSlice(params, "binaryPropertyAttachment")
	if len(properties) == 0 {
		properties = stringSlice(nestedMap(params, "additionalFields"), "attachments")
	}
	for _, property := range properties {
		binary, ok := item.Binary[property]
		if !ok {
			return nil, fmt.Errorf("binary property %s not found", property)
		}
		attachment, err := n.attachmentFromBinary(ctx, binary)
		if err != nil {
			return nil, err
		}
		out = append(out, attachment)
	}
	manual, err := n.manualAttachments(ctx, params)
	if err != nil {
		return nil, err
	}
	out = append(out, manual...)
	return out, nil
}

func (n *Node) attachmentFromBinary(ctx context.Context, binary dataplane.Binary) (Attachment, error) {
	if binary.FileSize > maxAttachmentBytes {
		return Attachment{}, fmt.Errorf("attachment %s exceeds 25MB", binary.FileName)
	}
	data, err := binarydata.Read(ctx, n.binaryStore, binary)
	if err != nil {
		return Attachment{}, err
	}
	if len(data) > maxAttachmentBytes {
		return Attachment{}, fmt.Errorf("attachment %s exceeds 25MB", binary.FileName)
	}
	fileName := binary.FileName
	if fileName == "" {
		fileName = "attachment"
	}
	mimeType := binary.MimeType
	if mimeType == "" {
		mimeType = mimeForName(fileName)
	}
	return Attachment{Content: base64.StdEncoding.EncodeToString(data), Filename: fileName, Type: mimeType, Disposition: "attachment"}, nil
}

func (n *Node) manualAttachments(ctx context.Context, params map[string]any) ([]Attachment, error) {
	raw, ok := params["attachments"]
	if !ok || raw == nil {
		return nil, nil
	}
	values, err := attachmentValues(raw)
	if err != nil {
		return nil, err
	}
	out := make([]Attachment, 0, len(values))
	for _, value := range values {
		attachment := Attachment{
			Content:     stringValue(value, "content"),
			Filename:    stringValue(value, "filename", "fileName", "name"),
			Type:        stringValue(value, "type", "mimeType"),
			Disposition: stringValue(value, "disposition"),
			ContentID:   stringValue(value, "contentId", "content_id"),
		}
		if attachment.Filename == "" {
			attachment.Filename = "attachment"
		}
		if strings.HasPrefix(attachment.Content, "http://") || strings.HasPrefix(attachment.Content, "https://") {
			downloaded, err := n.downloadAttachment(ctx, attachment.Content, attachment.Filename)
			if err != nil {
				return nil, err
			}
			if attachment.Type != "" {
				downloaded.Type = attachment.Type
			}
			downloaded.Disposition = attachment.Disposition
			downloaded.ContentID = attachment.ContentID
			attachment = downloaded
		}
		if attachment.Type == "" {
			attachment.Type = mimeForName(attachment.Filename)
		}
		if attachment.Disposition == "" {
			attachment.Disposition = "attachment"
		}
		out = append(out, attachment)
	}
	return out, nil
}

func attachmentValues(raw any) ([]map[string]any, error) {
	switch typed := raw.(type) {
	case []map[string]any:
		return typed, nil
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if object, ok := item.(map[string]any); ok {
				out = append(out, object)
			}
		}
		return out, nil
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil, nil
		}
		var out []map[string]any
		if err := json.Unmarshal([]byte(typed), &out); err != nil {
			return nil, err
		}
		return out, nil
	default:
		return nil, fmt.Errorf("attachments must be an array")
	}
}

func (n *Node) downloadAttachment(ctx context.Context, url string, fileName string) (Attachment, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Attachment{}, err
	}
	response, err := n.client.Do(request)
	if err != nil {
		return Attachment{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Attachment{}, fmt.Errorf("download attachment returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxAttachmentBytes+1))
	if err != nil {
		return Attachment{}, err
	}
	if len(data) > maxAttachmentBytes {
		return Attachment{}, fmt.Errorf("attachment %s exceeds 25MB", fileName)
	}
	mimeType := response.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = mimeForName(fileName)
	}
	return Attachment{Content: base64.StdEncoding.EncodeToString(data), Filename: fileName, Type: mimeType, Disposition: "attachment"}, nil
}

func mimeForName(fileName string) string {
	if value := mime.TypeByExtension(filepath.Ext(fileName)); value != "" {
		return value
	}
	return "application/octet-stream"
}
