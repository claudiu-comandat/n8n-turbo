package nodes

import (
	"context"
	"os"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

func TestGoCodeCompilesOnceAndRunsPerItem(t *testing.T) {
	goBin, err := goBinary()
	if err != nil {
		t.Skip("go toolchain not available")
	}
	source := `return map[string]any{"doubled": itemIndex * 2}, nil`

	out, err := (Code{}).Execute(context.Background(), engine.ExecuteInput{
		Node: dataplane.Node{
			Name: "Code",
			Type: "n8n-nodes-base.code",
			Parameters: map[string]any{
				"language": "golang",
				"goCode":   source,
				"mode":     "runOnceForEachItem",
			},
		},
		InputData: dataplane.MainOutput([]dataplane.Item{
			{JSON: map[string]any{"a": 1}},
			{JSON: map[string]any{"a": 2}},
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	items := firstInput(out)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	for index, item := range items {
		if got := item.JSON["doubled"]; got != float64(index*2) {
			t.Fatalf("item %d: expected doubled=%d, got %v", index, index*2, got)
		}
	}

	binPath, err := compileGoCode(context.Background(), goBin, source)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(binPath)
	if err != nil {
		t.Fatal(err)
	}
	again, err := compileGoCode(context.Background(), goBin, source)
	if err != nil {
		t.Fatal(err)
	}
	if again != binPath {
		t.Fatalf("cache returned different path: %s vs %s", again, binPath)
	}
	after, err := os.Stat(binPath)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("binary was rebuilt instead of served from cache")
	}
}
