package nodes

import (
	"context"
	"reflect"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func TestSplitOutMatchesOfficialMultipleFieldsAndInclude(t *testing.T) {
	t.Parallel()

	out, err := (SplitOut{}).Execute(context.Background(), testInput(map[string]any{
		"fieldToSplitOut": "titles,descriptions",
		"include":         "allOtherFields",
		"options": map[string]any{
			"destinationFieldName": "title,description",
		},
	}, []dataplane.Item{{JSON: map[string]any{
		"id":           "p1",
		"titles":       []any{"T1", "T2"},
		"descriptions": []any{"D1", "D2"},
	}}}))
	if err != nil {
		t.Fatalf("split out execute: %v", err)
	}
	got := []map[string]any{out[0][0].JSON, out[0][1].JSON}
	want := []map[string]any{
		{"id": "p1", "title": "T1", "description": "D1"},
		{"id": "p1", "title": "T2", "description": "D2"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected split output\n got: %#v\nwant: %#v", got, want)
	}
}

func TestSplitOutMergesObjectWhenNoOtherFields(t *testing.T) {
	t.Parallel()

	out, err := (SplitOut{}).Execute(context.Background(), testInput(map[string]any{
		"fieldToSplitOut": "rows",
		"include":         "noOtherFields",
	}, []dataplane.Item{{JSON: map[string]any{
		"rows": []any{map[string]any{"name": "Ana", "age": float64(3)}},
	}}}))
	if err != nil {
		t.Fatalf("split out execute: %v", err)
	}
	want := map[string]any{"name": "Ana", "age": float64(3)}
	if !reflect.DeepEqual(out[0][0].JSON, want) {
		t.Fatalf("unexpected split object output\n got: %#v\nwant: %#v", out[0][0].JSON, want)
	}
}

func TestSplitOutStripsJsonPrefixLikeOfficial(t *testing.T) {
	t.Parallel()

	out, err := (SplitOut{}).Execute(context.Background(), testInput(map[string]any{
		"fieldToSplitOut": "$json.rows",
		"include":         "noOtherFields",
	}, []dataplane.Item{{JSON: map[string]any{
		"rows": []any{"a", "b"},
	}}}))
	if err != nil {
		t.Fatalf("split out execute: %v", err)
	}
	if len(out[0]) != 2 || out[0][0].JSON["rows"] != "a" || out[0][1].JSON["rows"] != "b" {
		t.Fatalf("unexpected prefixed split output: %#v", out)
	}
}
