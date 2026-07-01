package nodes

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
	"github.com/n8n-io/n8n-turbo/internal/metadata"
)

func TestMergeSupportsOfficialCombineByPosition(t *testing.T) {
	t.Parallel()

	output, err := (Merge{}).Execute(context.Background(), engine.ExecuteInput{
		Node: dataplane.Node{
			Name: "Merge",
			Type: "n8n-nodes-base.merge",
			Parameters: map[string]any{
				"mode":      "combine",
				"combineBy": "combineByPosition",
			},
		},
		InputData: dataplane.Output{
			{{JSON: map[string]any{"left": "a"}}},
			{{JSON: map[string]any{"right": "b"}}},
		},
	})
	if err != nil {
		t.Fatalf("execute merge: %v", err)
	}
	if output[0][0].JSON["left"] != "a" || output[0][0].JSON["right"] != "b" {
		t.Fatalf("unexpected merged item: %#v", output[0][0].JSON)
	}
}

func TestMergeSupportsOfficialFieldsToMatchStringOutputInput1(t *testing.T) {
	t.Parallel()

	output, err := (Merge{}).Execute(context.Background(), engine.ExecuteInput{
		Node: dataplane.Node{
			Name: "Merge",
			Type: "n8n-nodes-base.merge",
			Parameters: map[string]any{
				"mode":                "combine",
				"combineBy":           "combineByFields",
				"fieldsToMatchString": "id",
				"joinMode":            "keepMatches",
				"outputDataFrom":      "input1",
			},
		},
		InputData: dataplane.Output{
			{
				{JSON: map[string]any{"id": 1, "left": "match"}},
				{JSON: map[string]any{"id": 2, "left": "skip"}},
			},
			{{JSON: map[string]any{"id": 1, "right": "match"}}},
		},
	})
	if err != nil {
		t.Fatalf("execute merge: %v", err)
	}
	if len(output[0]) != 1 || output[0][0].JSON["left"] != "match" || output[0][0].JSON["right"] != nil {
		t.Fatalf("unexpected output: %#v", output[0])
	}
}

func TestMergeSupportsOfficialKeepEverything(t *testing.T) {
	t.Parallel()

	output, err := (Merge{}).Execute(context.Background(), engine.ExecuteInput{
		Node: dataplane.Node{
			Name: "Merge",
			Type: "n8n-nodes-base.merge",
			Parameters: map[string]any{
				"mode":                "combine",
				"combineBy":           "combineByFields",
				"fieldsToMatchString": "id",
				"joinMode":            "keepEverything",
				"outputDataFrom":      "both",
			},
		},
		InputData: dataplane.Output{
			{
				{JSON: map[string]any{"id": 1, "left": "match"}},
				{JSON: map[string]any{"id": 2, "left": "left-only"}},
			},
			{
				{JSON: map[string]any{"id": 1, "right": "match"}},
				{JSON: map[string]any{"id": 3, "right": "right-only"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("execute merge: %v", err)
	}
	if len(output[0]) != 3 {
		t.Fatalf("expected matched plus both unmatched items, got %#v", output[0])
	}
}

func TestMergeSupportsOfficialSQLQuery(t *testing.T) {
	t.Parallel()

	output, err := (Merge{}).Execute(context.Background(), engine.ExecuteInput{
		Node: dataplane.Node{
			Name: "Merge",
			Type: "n8n-nodes-base.merge",
			Parameters: map[string]any{
				"mode":  "combineBySql",
				"query": "SELECT input1.id AS id, input1.left AS left, input2.right AS right FROM input1 LEFT JOIN input2 ON input1.id = input2.id WHERE input2.right IS NOT NULL",
			},
		},
		InputData: dataplane.Output{
			{
				{JSON: map[string]any{"id": 1, "left": "match"}},
				{JSON: map[string]any{"id": 2, "left": "skip"}},
			},
			{{JSON: map[string]any{"id": 1, "right": "joined"}}},
		},
	})
	if err != nil {
		t.Fatalf("execute merge SQL query: %v", err)
	}
	if len(output[0]) != 1 || output[0][0].JSON["left"] != "match" || output[0][0].JSON["right"] != "joined" {
		t.Fatalf("unexpected output: %#v", output[0])
	}
}

func TestMergeRuntimeSupportsOriginalModes(t *testing.T) {
	t.Parallel()

	got := mergeOriginalOptionValues(t, "mode")
	want := []string{"append", "chooseBranch", "combine", "combineBySql"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("original merge modes changed: got %v want %v", got, want)
	}
}

func mergeOriginalOptionValues(t *testing.T, propertyName string) []string {
	t.Helper()
	node, ok := metadata.NodeTypeByName("n8n-nodes-base.merge", []string{"n8n-nodes-base.merge"})
	if !ok {
		t.Fatal("merge metadata not found")
	}
	properties, ok := node.Raw["properties"].([]any)
	if !ok {
		t.Fatal("merge metadata properties not found")
	}
	for _, rawProperty := range properties {
		property, ok := rawProperty.(map[string]any)
		if !ok || property["name"] != propertyName {
			continue
		}
		options, ok := property["options"].([]any)
		if !ok {
			t.Fatalf("property %s has no options", propertyName)
		}
		values := make([]string, 0, len(options))
		for _, rawOption := range options {
			option, ok := rawOption.(map[string]any)
			if !ok {
				continue
			}
			values = append(values, fmt.Sprint(option["value"]))
		}
		sort.Strings(values)
		return values
	}
	t.Fatalf("property %s not found", propertyName)
	return nil
}
