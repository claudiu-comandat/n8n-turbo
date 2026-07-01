package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func TestCompactOutputKeepsAllItems(t *testing.T) {
	t.Parallel()

	items := make([]dataplane.Item, 300)
	for i := range items {
		items[i] = dataplane.Item{JSON: map[string]any{"index": i}}
	}

	output := compactOutput(context.Background(), dataplane.MainOutput(items), nil)
	if len(output[0]) != len(items) {
		t.Fatalf("expected all items to be kept, got %d of %d", len(output[0]), len(items))
	}
	for _, item := range output[0] {
		if _, ok := item.JSON["__n8nTurboMeta"]; ok {
			t.Fatalf("run data should not inject truncation metadata: %#v", item.JSON)
		}
	}
}

func TestCompactJSONKeepsLargeArrays(t *testing.T) {
	t.Parallel()

	values := make([]any, 300)
	for i := range values {
		values[i] = map[string]any{"value": i}
	}

	compacted, ok := compactJSONValue(values).([]any)
	if !ok {
		t.Fatalf("expected compacted array, got %T", compactJSONValue(values))
	}
	if len(compacted) != len(values) {
		t.Fatalf("expected all array entries to be kept, got %d of %d", len(compacted), len(values))
	}
}

func TestCompactJSONStillTruncatesLargeStrings(t *testing.T) {
	t.Parallel()

	compacted, ok := compactJSONValue(strings.Repeat("x", maxStoredStringValueBytes+1)).(string)
	if !ok {
		t.Fatalf("expected compacted string")
	}
	if !strings.HasSuffix(compacted, "...[truncated]") {
		t.Fatalf("expected large string to stay truncated")
	}
}
