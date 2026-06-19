package engine

import (
	"context"
	"encoding/base64"
	"strings"

	"github.com/n8n-io/n8n-turbo/internal/binarydata"
	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

const (
	maxStoredItemsPerBranch   = 256
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
		kept := items
		truncated := false
		if len(items) > maxStoredItemsPerBranch {
			kept = items[:maxStoredItemsPerBranch]
			truncated = true
		}
		next := make([]dataplane.Item, 0, len(kept)+1)
		for _, item := range kept {
			next = append(next, compactItem(ctx, item, store))
		}
		if truncated {
			next = append(next, dataplane.Item{
				JSON: map[string]any{
					"__n8nTurboMeta": map[string]any{
						"truncated":  true,
						"totalItems": len(items),
						"keptItems":  len(kept),
						"outputIndex": outputIndex,
					},
				},
			})
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
		if len(typed) <= maxStoredItemsPerBranch {
			return typed
		}
		return append(append([]string{}, typed[:maxStoredItemsPerBranch]...), "...[truncated]")
	case []any:
		if len(typed) <= maxStoredItemsPerBranch {
			return typed
		}
		next := make([]any, 0, maxStoredItemsPerBranch+1)
		for _, entry := range typed[:maxStoredItemsPerBranch] {
			next = append(next, compactJSONValue(entry))
		}
		return append(next, map[string]any{"__n8nTurboMeta": map[string]any{"truncated": true, "totalItems": len(typed)}})
	case map[string]any:
		next := make(map[string]any, len(typed))
		count := 0
		for key, entry := range typed {
			if count >= maxStoredItemsPerBranch {
				next["__n8nTurboMeta"] = map[string]any{"truncated": true, "totalKeys": len(typed)}
				break
			}
			next[key] = compactJSONValue(entry)
			count++
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
	ref, err := store.Put(ctx, mimeType, fileName, reader)
	if err == nil {
		return ref, nil
	}
	return store.Put(ctx, mimeType, fileName, strings.NewReader(binary.Data))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
