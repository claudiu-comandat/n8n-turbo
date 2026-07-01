package nodes

import (
	"context"
	"reflect"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func TestAggregateAcceptsOfficialIndividualFieldsCollection(t *testing.T) {
	t.Parallel()

	out, err := (Aggregate{}).Execute(context.Background(), testInput(map[string]any{
		"aggregate": "aggregateIndividualFields",
		"fieldsToAggregate": map[string]any{
			"fieldToAggregate": []any{
				map[string]any{"fieldToAggregate": "product.title", "renameField": false},
			},
		},
		"options": map[string]any{"mergeLists": true},
	}, []dataplane.Item{
		{JSON: map[string]any{"product": map[string]any{"title": []any{"A", nil}}}},
		{JSON: map[string]any{"product": map[string]any{"title": []any{"B"}}}},
	}))
	if err != nil {
		t.Fatalf("aggregate execute: %v", err)
	}
	want := map[string]any{"title": []any{"A", "B"}}
	if !reflect.DeepEqual(out[0][0].JSON, want) {
		t.Fatalf("unexpected aggregate output\n got: %#v\nwant: %#v", out[0][0].JSON, want)
	}
}

func TestAggregateAllItemDataMatchesOfficialIncludeOutput(t *testing.T) {
	t.Parallel()

	out, err := (Aggregate{}).Execute(context.Background(), testInput(map[string]any{
		"aggregate":            "aggregateAllItemData",
		"destinationFieldName": "data",
		"include":              "allFieldsExcept",
		"fieldsToExclude":      "secret",
	}, []dataplane.Item{
		{JSON: map[string]any{"id": "1", "secret": "x"}},
		{JSON: map[string]any{"id": "2", "secret": "y"}},
	}))
	if err != nil {
		t.Fatalf("aggregate execute: %v", err)
	}
	want := map[string]any{"data": []any{map[string]any{"id": "1"}, map[string]any{"id": "2"}}}
	if !reflect.DeepEqual(out[0][0].JSON, want) {
		t.Fatalf("unexpected aggregate all output\n got: %#v\nwant: %#v", out[0][0].JSON, want)
	}
	if _, exists := out[0][0].JSON["count"]; exists {
		t.Fatalf("official aggregate all output must not add count: %#v", out[0][0].JSON)
	}
}

func TestAggregateIncludesBinariesWithSuffixes(t *testing.T) {
	t.Parallel()

	out, err := (Aggregate{}).Execute(context.Background(), testInput(map[string]any{
		"aggregate":            "aggregateAllItemData",
		"destinationFieldName": "data",
		"options":              map[string]any{"includeBinaries": true},
	}, []dataplane.Item{
		{
			JSON:   map[string]any{"id": 1},
			Binary: map[string]dataplane.Binary{"file": {MimeType: "text/plain", FileName: "a.txt"}},
		},
		{
			JSON:   map[string]any{"id": 2},
			Binary: map[string]dataplane.Binary{"file": {MimeType: "text/csv", FileName: "b.csv"}},
		},
	}))
	if err != nil {
		t.Fatalf("aggregate execute: %v", err)
	}
	binary := out[0][0].Binary
	if binary["file"].FileName != "a.txt" || binary["file_1"].FileName != "b.csv" {
		t.Fatalf("unexpected binaries: %#v", binary)
	}
}

func TestAggregateKeepsOnlyUniqueBinaries(t *testing.T) {
	t.Parallel()

	out, err := (Aggregate{}).Execute(context.Background(), testInput(map[string]any{
		"aggregate":            "aggregateAllItemData",
		"destinationFieldName": "data",
		"options": map[string]any{
			"includeBinaries": true,
			"keepOnlyUnique":  true,
		},
	}, []dataplane.Item{
		{
			JSON:   map[string]any{"id": 1},
			Binary: map[string]dataplane.Binary{"file": {MimeType: "text/plain", FileSize: 10, FileExtension: "txt"}},
		},
		{
			JSON:   map[string]any{"id": 2},
			Binary: map[string]dataplane.Binary{"other": {MimeType: "text/plain", FileSize: 10, FileExtension: "txt"}},
		},
	}))
	if err != nil {
		t.Fatalf("aggregate execute: %v", err)
	}
	if len(out[0][0].Binary) != 1 {
		t.Fatalf("expected one unique binary, got %#v", out[0][0].Binary)
	}
}
