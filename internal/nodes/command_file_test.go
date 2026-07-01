package nodes

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func TestExecuteCommandDefaultsToExecuteOnceAndTrimsOutputLikeOfficial(t *testing.T) {
	t.Parallel()

	out, err := (ExecuteCommand{}).Execute(context.Background(), testInput(map[string]any{
		"command": "echo hello",
	}, []dataplane.Item{
		{JSON: map[string]any{"id": 1}},
		{JSON: map[string]any{"id": 2}},
	}))
	if err != nil {
		t.Fatalf("execute command: %v", err)
	}
	if got := len(out[0]); got != 1 {
		t.Fatalf("official default executeOnce should return one item, got %d", got)
	}
	item := out[0][0]
	if item.JSON["stdout"] != "hello" || item.JSON["stderr"] != "" || item.JSON["exitCode"] != 0 {
		t.Fatalf("unexpected command output: %#v", item.JSON)
	}
	if item.PairedItem == nil || item.PairedItem.Item != 0 {
		t.Fatalf("expected paired item 0, got %#v", item.PairedItem)
	}
}

func TestExecuteCommandCanRunForEachItemWhenExecuteOnceDisabled(t *testing.T) {
	t.Parallel()

	out, err := (ExecuteCommand{}).Execute(context.Background(), testInput(map[string]any{
		"command":     "echo hello",
		"executeOnce": false,
	}, []dataplane.Item{
		{JSON: map[string]any{"id": 1}},
		{JSON: map[string]any{"id": 2}},
	}))
	if err != nil {
		t.Fatalf("execute command: %v", err)
	}
	if got := len(out[0]); got != 2 {
		t.Fatalf("executeOnce=false should return two items, got %d", got)
	}
	for index, item := range out[0] {
		if item.JSON["stdout"] != "hello" {
			t.Fatalf("item %d stdout = %#v", index, item.JSON["stdout"])
		}
		if item.PairedItem == nil || item.PairedItem.Item != index {
			t.Fatalf("item %d pairedItem = %#v", index, item.PairedItem)
		}
	}
}

func TestReadWriteFileWriteUsesOfficialFileNameAndPreservesItem(t *testing.T) {
	t.Parallel()

	target := filepath.Join(t.TempDir(), "out.txt")
	out, err := (ReadWriteFile{}).Execute(context.Background(), testInput(map[string]any{
		"operation":        "write",
		"fileName":         target,
		"dataPropertyName": "data",
	}, []dataplane.Item{{
		JSON: map[string]any{"keep": "yes"},
		Binary: map[string]dataplane.Binary{
			"data": {Data: base64.StdEncoding.EncodeToString([]byte("hello")), FileName: "in.txt", MimeType: "text/plain"},
		},
	}}))
	if err != nil {
		t.Fatalf("write file: %v", err)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(content) != "hello" {
		t.Fatalf("target content = %q", content)
	}
	item := out[0][0]
	if item.JSON["keep"] != "yes" || item.JSON["fileName"] != target {
		t.Fatalf("write should preserve input JSON and add fileName, got %#v", item.JSON)
	}
	if _, ok := item.Binary["data"]; !ok {
		t.Fatalf("write should preserve input binary, got %#v", item.Binary)
	}
	if item.PairedItem == nil || item.PairedItem.Item != 0 {
		t.Fatalf("expected paired item 0, got %#v", item.PairedItem)
	}
}

func TestReadWriteFileReadUsesOfficialFileSelector(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o600); err != nil {
		t.Fatalf("write a: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o600); err != nil {
		t.Fatalf("write b: %v", err)
	}
	out, err := (ReadWriteFile{}).Execute(context.Background(), testInput(map[string]any{
		"operation":    "read",
		"fileSelector": filepath.Join(dir, "*.txt"),
		"options": map[string]any{
			"dataPropertyName": "file",
		},
	}, []dataplane.Item{{JSON: map[string]any{"source": "item"}}}))
	if err != nil {
		t.Fatalf("read file selector: %v", err)
	}
	if got := len(out[0]); got != 2 {
		t.Fatalf("expected two files, got %d", got)
	}
	for index, item := range out[0] {
		if _, ok := item.Binary["file"]; !ok {
			t.Fatalf("item %d missing file binary: %#v", index, item.Binary)
		}
		if item.JSON["fileName"] == "" || item.JSON["fileExtension"] != "txt" || item.JSON["fileSize"] == nil {
			t.Fatalf("item %d metadata = %#v", index, item.JSON)
		}
		if item.PairedItem == nil || item.PairedItem.Item != 0 {
			t.Fatalf("item %d paired item = %#v", index, item.PairedItem)
		}
	}
}
