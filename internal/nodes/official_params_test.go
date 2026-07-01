package nodes

import (
	"context"
	"reflect"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
	"github.com/n8n-io/n8n-turbo/internal/expr"
)

func TestSetAcceptsOfficialRawModeAndIncludeOptions(t *testing.T) {
	t.Parallel()

	out, err := (Set{}).Execute(context.Background(), testInput(map[string]any{
		"mode":          "raw",
		"jsonOutput":    `{"added": 2}`,
		"include":       "selected",
		"includeFields": "keep,nested.value",
	}, []dataplane.Item{{JSON: map[string]any{
		"keep":   "yes",
		"drop":   "no",
		"nested": map[string]any{"value": "ok"},
	}}}))
	if err != nil {
		t.Fatalf("set execute: %v", err)
	}
	got := out[0][0].JSON
	want := map[string]any{"keep": "yes", "value": "ok", "added": float64(2)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected output\n got: %#v\nwant: %#v", got, want)
	}
	if out[0][0].PairedItem == nil || out[0][0].PairedItem.Item != 0 {
		t.Fatalf("Set should set paired item like n8n original: %#v", out[0][0].PairedItem)
	}
}

func TestSetLatestIncludeAndBinaryOptionsMatchOfficial(t *testing.T) {
	t.Parallel()

	input := engine.ExecuteInput{
		Node: dataplane.Node{
			Name:        "Set",
			TypeVersion: 3.4,
			Parameters: map[string]any{
				"mode":               "manual",
				"includeOtherFields": false,
				"assignments": map[string]any{"assignments": []any{map[string]any{
					"name":  "answer",
					"type":  "number",
					"value": "42",
				}}},
			},
		},
		InputData: dataplane.MainOutput([]dataplane.Item{{
			JSON:       map[string]any{"old": "drop"},
			Binary:     map[string]dataplane.Binary{"data": {FileName: "a.txt"}},
			PairedItem: &dataplane.PairedItem{Item: 99},
		}}),
		Expr: expr.NewResolver(0),
	}

	out, err := (Set{}).Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("set execute: %v", err)
	}
	got := out[0][0]
	wantJSON := map[string]any{"answer": float64(42)}
	if !reflect.DeepEqual(got.JSON, wantJSON) {
		t.Fatalf("unexpected output\n got: %#v\nwant: %#v", got.JSON, wantJSON)
	}
	if got.Binary != nil {
		t.Fatalf("binary should be stripped by default on latest Set node: %#v", got.Binary)
	}
	if got.PairedItem == nil || got.PairedItem.Item != 0 {
		t.Fatalf("paired item should point to source index: %#v", got.PairedItem)
	}
}

func TestSetLatestCanKeepBinaryWhenStripBinaryDisabled(t *testing.T) {
	t.Parallel()

	input := engine.ExecuteInput{
		Node: dataplane.Node{
			Name:        "Set",
			TypeVersion: 3.4,
			Parameters: map[string]any{
				"mode":               "raw",
				"jsonOutput":         `{"added": true}`,
				"includeOtherFields": true,
				"include":            "all",
				"options":            map[string]any{"stripBinary": false},
			},
		},
		InputData: dataplane.MainOutput([]dataplane.Item{{
			JSON:   map[string]any{"old": "keep"},
			Binary: map[string]dataplane.Binary{"data": {FileName: "a.txt"}},
		}}),
		Expr: expr.NewResolver(0),
	}

	out, err := (Set{}).Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("set execute: %v", err)
	}
	if out[0][0].Binary == nil || out[0][0].Binary["data"].FileName != "a.txt" {
		t.Fatalf("binary should be kept when stripBinary is disabled: %#v", out[0][0].Binary)
	}
}

func TestIfAcceptsOfficialFilterOperatorObject(t *testing.T) {
	t.Parallel()

	out, err := (If{}).Execute(context.Background(), testInput(map[string]any{
		"conditions": map[string]any{
			"options": map[string]any{"caseSensitive": true, "typeValidation": "strict"},
			"conditions": []any{map[string]any{
				"leftValue":  "={{ $json.name }}",
				"rightValue": "Ana",
				"operator":   map[string]any{"type": "string", "operation": "equals"},
			}},
			"combinator": "and",
		},
	}, []dataplane.Item{
		{JSON: map[string]any{"name": "Ana"}},
		{JSON: map[string]any{"name": "ana"}},
	}))
	if err != nil {
		t.Fatalf("if execute: %v", err)
	}
	if len(out) != 2 || len(out[0]) != 1 || len(out[1]) != 1 {
		t.Fatalf("unexpected IF split: %#v", out)
	}
	if out[0][0].PairedItem == nil || out[0][0].PairedItem.Item != 0 {
		t.Fatalf("IF should set paired item like n8n original: %#v", out[0][0].PairedItem)
	}
}

func TestFilterReturnsDiscardedOutputAndPairedItems(t *testing.T) {
	t.Parallel()

	out, err := (Filter{}).Execute(context.Background(), testInput(map[string]any{
		"conditions": officialSwitchCondition("={{ $json.keep }}", true),
	}, []dataplane.Item{
		{JSON: map[string]any{"keep": true}},
		{JSON: map[string]any{"keep": false}},
	}))
	if err != nil {
		t.Fatalf("filter execute: %v", err)
	}
	if len(out) != 2 || len(out[0]) != 1 || len(out[1]) != 1 {
		t.Fatalf("unexpected filter output: %#v", out)
	}
	if out[1][0].PairedItem == nil || out[1][0].PairedItem.Item != 1 {
		t.Fatalf("Filter should keep paired item for discarded output: %#v", out[1][0].PairedItem)
	}
}

func TestSwitchAcceptsOfficialNestedRulesAndFallback(t *testing.T) {
	t.Parallel()

	out, err := (Switch{}).Execute(context.Background(), testInput(map[string]any{
		"rules": map[string]any{
			"values": []any{
				map[string]any{"conditions": officialSwitchCondition("={{ $json.kind }}", "a")},
				map[string]any{"conditions": officialSwitchCondition("={{ $json.kind }}", "b")},
			},
		},
		"options": map[string]any{"fallbackOutput": "extra", "allMatchingOutputs": true},
	}, []dataplane.Item{
		{JSON: map[string]any{"kind": "a"}},
		{JSON: map[string]any{"kind": "b"}},
		{JSON: map[string]any{"kind": "c"}},
	}))
	if err != nil {
		t.Fatalf("switch execute: %v", err)
	}
	if len(out) != 3 || len(out[0]) != 1 || len(out[1]) != 1 || len(out[2]) != 1 {
		t.Fatalf("unexpected switch output: %#v", out)
	}
}

func TestSwitchExpressionUsesOfficialOutputParameter(t *testing.T) {
	t.Parallel()

	out, err := (Switch{}).Execute(context.Background(), testInput(map[string]any{
		"mode":          "expression",
		"numberOutputs": 4,
		"output":        "={{ $json.route }}",
	}, []dataplane.Item{{JSON: map[string]any{"route": 2}}}))
	if err != nil {
		t.Fatalf("switch execute: %v", err)
	}
	if len(out) != 4 || len(out[2]) != 1 {
		t.Fatalf("expected item on output 2 of 4, got %#v", out)
	}
	if out[2][0].PairedItem == nil || out[2][0].PairedItem.Item != 0 {
		t.Fatalf("Switch should set paired item like n8n original: %#v", out[2][0].PairedItem)
	}
}

func officialSwitchCondition(left string, right any) map[string]any {
	return map[string]any{
		"options": map[string]any{"caseSensitive": true, "typeValidation": "strict"},
		"conditions": []any{map[string]any{
			"leftValue":  left,
			"rightValue": right,
			"operator":   map[string]any{"type": "string", "operation": "equals"},
		}},
		"combinator": "and",
	}
}

func testInput(params map[string]any, items []dataplane.Item) engine.ExecuteInput {
	return engine.ExecuteInput{
		Node:      dataplane.Node{Name: "Node", Parameters: params},
		InputData: dataplane.MainOutput(items),
		Expr:      expr.NewResolver(0),
	}
}
