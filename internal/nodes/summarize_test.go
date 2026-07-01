package nodes

import (
	"context"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
	"github.com/n8n-io/n8n-turbo/internal/expr"
)

func TestSummarizeGroupPairedItemUsesSourceIndex(t *testing.T) {
	t.Parallel()

	out, err := (Summarize{}).Execute(context.Background(), engine.ExecuteInput{
		Node: dataplane.Node{
			Name: "Summarize",
			Parameters: map[string]any{
				"fieldsToSummarize": map[string]any{"values": []any{map[string]any{
					"aggregation": "count",
					"field":       "value",
				}}},
				"fieldsToSplitBy": "group",
			},
		},
		InputData: dataplane.MainOutput([]dataplane.Item{
			{JSON: map[string]any{"group": "a", "value": 1}},
			{JSON: map[string]any{"group": "b", "value": 2}},
		}),
		Expr: expr.NewResolver(0),
	})
	if err != nil {
		t.Fatalf("summarize execute: %v", err)
	}
	if len(out[0]) != 2 {
		t.Fatalf("expected two grouped items, got %#v", out[0])
	}
	if out[0][1].PairedItem == nil || out[0][1].PairedItem.Item != 1 {
		t.Fatalf("second group should point at source item 1: %#v", out[0][1].PairedItem)
	}
}

func TestSummarizeV11ContinuesWhenFieldMissingLikeOfficial(t *testing.T) {
	t.Parallel()

	out, err := (Summarize{}).Execute(context.Background(), engine.ExecuteInput{
		Node: dataplane.Node{
			Name:        "Summarize",
			TypeVersion: 1.1,
			Parameters: map[string]any{
				"fieldsToSummarize": map[string]any{"values": []any{map[string]any{
					"aggregation": "count",
					"field":       "missing",
				}}},
			},
		},
		InputData: dataplane.MainOutput([]dataplane.Item{{JSON: map[string]any{"value": 1}}}),
		Expr:      expr.NewResolver(0),
	})
	if err != nil {
		t.Fatalf("summarize should continue for missing fields on v1.1: %v", err)
	}
	if got := out[0][0].JSON["count_missing"]; got != 0 {
		t.Fatalf("unexpected missing-field count: %#v", got)
	}
}
