package telegram

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"

	"github.com/n8n-io/n8n-turbo/internal/binarydata"
	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func (n *Node) sendMedia(ctx context.Context, cred *Credential, params map[string]any, item dataplane.Item, method string, field string) (map[string]any, error) {
	chatID := stringParam(params, "chatId", "chat_id")
	if chatID == "" {
		return nil, fmt.Errorf("chatId is required")
	}
	property := stringParam(params, "binaryPropertyName", "binaryData")
	if property == "" {
		property = "data"
	}
	if binary, ok := item.Binary[property]; ok && binary.ID != "" {
		return n.sendBinaryMedia(ctx, cred, params, binary, method, field, chatID)
	}
	fileID := stringParam(params, "fileId", field)
	if fileID == "" {
		return nil, fmt.Errorf("fileId or binary data is required")
	}
	body := map[string]any{"chat_id": chatID, field: fileID}
	if caption := stringParam(params, "caption"); caption != "" {
		body["caption"] = caption
	}
	addCommonMessageFields(body, params)
	return n.callAPI(ctx, cred, method, body, chatID)
}

func (n *Node) sendBinaryMedia(ctx context.Context, cred *Credential, params map[string]any, binary dataplane.Binary, method string, field string, chatID string) (map[string]any, error) {
	if n.binaryStore == nil {
		return nil, fmt.Errorf("binary store is not configured")
	}
	reader, err := n.binaryStore.Open(ctx, binaryRef(binary))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	fileName := binary.FileName
	if fileName == "" {
		fileName = field
	}
	fields := map[string]string{"chat_id": chatID}
	if caption := stringParam(params, "caption"); caption != "" {
		fields["caption"] = caption
	}
	if err := n.rateLimiter.Wait(ctx, chatID); err != nil {
		return nil, err
	}
	return n.callMultipart(ctx, cred, method, fields, field, fileName, reader)
}

func (n *Node) downloadFile(ctx context.Context, cred *Credential, params map[string]any) (map[string]any, error) {
	if n.binaryStore == nil {
		return nil, fmt.Errorf("binary store is not configured")
	}
	response, err := n.getFile(ctx, cred, params)
	if err != nil {
		return nil, err
	}
	file, err := decodeResultFile(response)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, cred.FileURL()+"/"+file.FilePath, nil)
	if err != nil {
		return nil, err
	}
	httpResponse, err := n.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer httpResponse.Body.Close()
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		return nil, fmt.Errorf("download returned HTTP %d", httpResponse.StatusCode)
	}
	name := filepath.Base(file.FilePath)
	ref, err := n.binaryStore.Put(ctx, mimeForPath(file.FilePath), name, io.LimitReader(httpResponse.Body, 64*1024*1024))
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":     true,
		"result": response["result"],
		"binary": binarydata.BinaryFromRef(ref),
	}, nil
}
