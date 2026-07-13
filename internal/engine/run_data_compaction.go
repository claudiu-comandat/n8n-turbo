package engine

import (
	"context"
	"encoding/base64"
	"strings"

	"github.com/n8n-io/n8n-turbo/internal/binarydata"
	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

const (
	maxStoredStringValueBytes = 64 * 1024
)

func compactTaskData(ctx context.Context, task dataplane.TaskData, store binarydata.Store) dataplane.TaskData {
	if len(task.Data) == 0 {
		return task
	}
	compacted := task
	compacted.Data = make(dataplane.NodeExecutionData, len(task.Data))
	for channel, output := range task.Data {
		compacted.Data[channel] = compactOutput(ctx, output, store)
	}
	return compacted
}

func compactOutput(ctx context.Context, output dataplane.Output, store binarydata.Store) dataplane.Output {
	compacted := make(dataplane.Output, len(output))
	for outputIndex, items := range output {
		next := make([]dataplane.Item, 0, len(items))
		for _, item := range items {
			next = append(next, compactItem(ctx, item, store))
		}
		compacted[outputIndex] = next
	}
	return compacted
}

func compactItem(ctx context.Context, item dataplane.Item, store binarydata.Store) dataplane.Item {
	next := dataplane.Item{
		JSON:       compactJSONMap(item.JSON),
		PairedItem: item.PairedItem,
		Error:      item.Error,
	}
	if len(item.Binary) > 0 {
		next.Binary = make(map[string]dataplane.Binary, len(item.Binary))
		for key, binary := range item.Binary {
			next.Binary[key] = compactBinary(ctx, binary, store)
		}
	}
	return next
}

func compactJSONMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	next := make(map[string]any, len(input))
	for key, value := range input {
		next[key] = compactJSONValue(value)
	}
	return next
}

func compactJSONValue(value any) any {
	switch typed := value.(type) {
	case string:
		if len(typed) <= maxStoredStringValueBytes {
			return typed
		}
		return typed[:maxStoredStringValueBytes] + "...[truncated]"
	case []string:
		return append([]string{}, typed...)
	case []any:
		next := make([]any, 0, len(typed))
		for _, entry := range typed {
			next = append(next, compactJSONValue(entry))
		}
		return next
	case map[string]any:
		next := make(map[string]any, len(typed))
		for key, entry := range typed {
			next[key] = compactJSONValue(entry)
		}
		return next
	default:
		return value
	}
}

func compactBinary(ctx context.Context, binary dataplane.Binary, store binarydata.Store) dataplane.Binary {
	if binary.Data == "" {
		return binary
	}
	if store != nil {
		if ref, err := storeInlineBinary(ctx, store, binary); err == nil {
			compacted := binarydata.BinaryFromRef(ref)
			compacted.FileType = binary.FileType
			compacted.FileExtension = firstNonEmpty(binary.FileExtension, compacted.FileExtension)
			return compacted
		}
	}
	if len(binary.Data) <= maxStoredStringValueBytes {
		return binary
	}
	binary.Data = ""
	return binary
}

func storeInlineBinary(ctx context.Context, store binarydata.Store, binary dataplane.Binary) (binarydata.Ref, error) {
	mimeType := firstNonEmpty(binary.MimeType, "application/octet-stream")
	fileName := firstNonEmpty(binary.FileName, "data.bin")
	reader := base64.NewDecoder(base64.StdEncoding, strings.NewReader(binary.Data))
	return store.Put(ctx, mimeType, fileName, reader)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
