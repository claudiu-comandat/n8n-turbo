package nodes

import (
	"context"
	"strings"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

func TestSortSupportsOfficialCodeType(t *testing.T) {
	t.Parallel()

	output, err := (Sort{}).Execute(context.Background(), engine.ExecuteInput{
		Node: dataplane.Node{
			Name: "Sort",
			Type: "n8n-nodes-base.sort",
			Parameters: map[string]any{
				"type": "code",
				"code": "return a.json.score < b.json.score ? -1 : a.json.score > b.json.score ? 1 : 0;",
			},
		},
		InputData: dataplane.MainOutput([]dataplane.Item{
			{JSON: map[string]any{"name": "b", "score": 2}},
			{JSON: map[string]any{"name": "a", "score": 1}},
			{JSON: map[string]any{"name": "c", "score": 3}},
		}),
	})
	if err != nil {
		t.Fatalf("execute sort: %v", err)
	}
	got := []string{
		output[0][0].JSON["name"].(string),
		output[0][1].JSON["name"].(string),
		output[0][2].JSON["name"].(string),
	}
	if strings.Join(got, ",") != "a,b,c" {
		t.Fatalf("unexpected sort order: %v", got)
	}
}

func TestSortCodeRequiresReturn(t *testing.T) {
	t.Parallel()

	_, err := (Sort{}).Execute(context.Background(), engine.ExecuteInput{
		Node: dataplane.Node{
			Name: "Sort",
			Type: "n8n-nodes-base.sort",
			Parameters: map[string]any{
				"type": "code",
				"code": "a.json.score - b.json.score;",
			},
		},
		InputData: dataplane.MainOutput([]dataplane.Item{{JSON: map[string]any{"score": 1}}}),
	})
	if err == nil || !strings.Contains(err.Error(), "doesn't return") {
		t.Fatalf("expected missing return error, got %v", err)
	}
}
