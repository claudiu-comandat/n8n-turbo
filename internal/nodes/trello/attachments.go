package trello

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/n8n-io/n8n-turbo/internal/binarydata"
	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func (n *Node) handleAttachment(ctx context.Context, cred Credential, operation string, params map[string]any, item dataplane.Item) ([]dataplane.Item, error) {
	cardID := stringValue(params, "cardId")
	if cardID == "" {
		return nil, fmt.Errorf("cardId is required")
	}
	switch operation {
	case "getAll", "list":
		return itemsFromArray(n.doArray(ctx, cred, http.MethodGet, "/cards/"+cardID+"/attachments?fields=id,name,url,bytes,date,mimeType", nil))
	case "get":
		id := stringValue(params, "attachmentId")
		if id == "" {
			return nil, fmt.Errorf("attachmentId is required")
		}
		return single(n.doJSON(ctx, cred, http.MethodGet, "/cards/"+cardID+"/attachments/"+id, nil))
	case "create":
		if sourceURL := stringValue(params, "url", "urlSource"); sourceURL != "" {
			body := map[string]any{"url": sourceURL}
			setString(body, "name", stringValue(params, "name"))
			return single(n.doJSON(ctx, cred, http.MethodPost, "/cards/"+cardID+"/attachments", body))
		}
		return single(n.uploadAttachmentBinary(ctx, cred, cardID, params, item))
	case "delete":
		id := stringValue(params, "attachmentId")
		if id == "" {
			return nil, fmt.Errorf("attachmentId is required")
		}
		_, err := n.doRaw(ctx, cred, http.MethodDelete, "/cards/"+cardID+"/attachments/"+id, nil, "application/json")
		return single(map[string]any{"success": true}, err)
	default:
		return nil, fmt.Errorf("unknown attachment operation %s", operation)
	}
}

func (n *Node) uploadAttachmentBinary(ctx context.Context, cred Credential, cardID string, params map[string]any, item dataplane.Item) (map[string]any, error) {
	property := stringValue(params, "binaryPropertyName")
	if property == "" {
		property = "data"
	}
	binary, ok := item.Binary[property]
	if !ok {
		return nil, fmt.Errorf("binary property %s not found", property)
	}
	data, err := binarydata.Read(ctx, n.binaryStore, binary)
	if err != nil {
		return nil, err
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("key", cred.APIKey)
	_ = writer.WriteField("token", cred.APIToken)
	name := stringValue(params, "name")
	if name == "" {
		name = binary.FileName
	}
	if name == "" {
		name = property
	}
	_ = writer.WriteField("name", name)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(part, bytes.NewReader(data)); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, n.baseURL+"/cards/"+cardID+"/attachments", &body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := n.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 32*1024*1024))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var trelloErr Error
		if json.Unmarshal(responseBody, &trelloErr) == nil && (trelloErr.Message != "" || trelloErr.Error != "") {
			return nil, trelloErr.Err()
		}
		return nil, fmt.Errorf("trello HTTP %d: %s", response.StatusCode, string(responseBody))
	}
	var result map[string]any
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return nil, err
	}
	return result, nil
}
