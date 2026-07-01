package nodes

import (
	"context"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func TestLimitMatchesOfficialFirstAndLastItems(t *testing.T) {
	t.Parallel()

	items := []dataplane.Item{
		{JSON: map[string]any{"id": 1}},
		{JSON: map[string]any{"id": 2}},
		{JSON: map[string]any{"id": 3}},
	}
	first, err := (Limit{}).Execute(context.Background(), testInput(map[string]any{
		"maxItems": 2,
		"keep":     "firstItems",
	}, items))
	if err != nil {
		t.Fatalf("limit first: %v", err)
	}
	if len(first[0]) != 2 || first[0][0].JSON["id"] != 1 || first[0][1].JSON["id"] != 2 {
		t.Fatalf("unexpected firstItems result: %#v", first)
	}

	last, err := (Limit{}).Execute(context.Background(), testInput(map[string]any{
		"maxItems": 2,
		"keep":     "lastItems",
	}, items))
	if err != nil {
		t.Fatalf("limit last: %v", err)
	}
	if len(last[0]) != 2 || last[0][0].JSON["id"] != 2 || last[0][1].JSON["id"] != 3 {
		t.Fatalf("unexpected lastItems result: %#v", last)
	}
}

func TestLimitKeepsAllItemsWhenMaxExceedsInputLikeOfficial(t *testing.T) {
	t.Parallel()

	out, err := (Limit{}).Execute(context.Background(), testInput(map[string]any{
		"maxItems": 5,
	}, []dataplane.Item{
		{JSON: map[string]any{"id": 1}},
		{JSON: map[string]any{"id": 2}},
	}))
	if err != nil {
		t.Fatalf("limit execute: %v", err)
	}
	if len(out[0]) != 2 {
		t.Fatalf("expected all items to pass through, got %#v", out)
	}
}

func TestNoOpPassesInputThroughLikeOfficial(t *testing.T) {
	t.Parallel()

	input := []dataplane.Item{{
		JSON:       map[string]any{"id": 1},
		Binary:     map[string]dataplane.Binary{"data": {FileName: "a.txt"}},
		PairedItem: &dataplane.PairedItem{Item: 7},
	}}
	out, err := (NoOp{}).Execute(context.Background(), testInput(map[string]any{}, input))
	if err != nil {
		t.Fatalf("noop execute: %v", err)
	}
	if len(out[0]) != 1 || out[0][0].JSON["id"] != 1 || out[0][0].Binary["data"].FileName != "a.txt" || out[0][0].PairedItem.Item != 7 {
		t.Fatalf("noop should pass input through unchanged, got %#v", out)
	}
}
