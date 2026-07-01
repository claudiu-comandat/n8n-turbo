package nodes

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
	"github.com/n8n-io/n8n-turbo/internal/expr"
)

func TestRemoveDuplicatesAcceptsOfficialSelectedFieldsAndRemoveOtherFields(t *testing.T) {
	t.Parallel()

	out, err := (RemoveDuplicates{}).Execute(context.Background(), testInput(map[string]any{
		"operation":       "removeDuplicateInputItems",
		"compare":         "selectedFields",
		"fieldsToCompare": "user.id",
		"options": map[string]any{
			"removeOtherFields": true,
		},
	}, []dataplane.Item{
		{JSON: map[string]any{"user": map[string]any{"id": "1", "name": "Ana"}, "drop": true}},
		{JSON: map[string]any{"user": map[string]any{"id": "1", "name": "Ana 2"}, "drop": false}},
		{JSON: map[string]any{"user": map[string]any{"id": "2", "name": "Bob"}, "drop": true}},
	}))
	if err != nil {
		t.Fatalf("remove duplicates execute: %v", err)
	}
	got := []map[string]any{out[0][0].JSON, out[0][1].JSON}
	want := []map[string]any{
		{"user": map[string]any{"id": "1"}},
		{"user": map[string]any{"id": "2"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected dedupe output\n got: %#v\nwant: %#v", got, want)
	}
}

func TestRemoveDuplicatesErrorsWhenSelectedFieldIsMissingLikeOfficial(t *testing.T) {
	t.Parallel()

	_, err := (RemoveDuplicates{}).Execute(context.Background(), testInput(map[string]any{
		"operation":       "removeDuplicateInputItems",
		"compare":         "selectedFields",
		"fieldsToCompare": "user.id",
	}, []dataplane.Item{
		{JSON: map[string]any{"user": map[string]any{"id": "1"}}},
		{JSON: map[string]any{"user": map[string]any{"name": "Ana"}}},
	}))
	if err == nil || !strings.Contains(err.Error(), `"user.id" field is missing`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRemoveDuplicatesErrorsWhenFieldTypeChangesLikeOfficial(t *testing.T) {
	t.Parallel()

	_, err := (RemoveDuplicates{}).Execute(context.Background(), testInput(map[string]any{
		"operation":       "removeDuplicateInputItems",
		"compare":         "selectedFields",
		"fieldsToCompare": "id",
	}, []dataplane.Item{
		{JSON: map[string]any{"id": "1"}},
		{JSON: map[string]any{"id": 1}},
	}))
	if err == nil || !strings.Contains(err.Error(), `"id" isn't always the same type`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRemoveDuplicatesAcceptsOfficialAllFieldsExcept(t *testing.T) {
	t.Parallel()

	out, err := (RemoveDuplicates{}).Execute(context.Background(), testInput(map[string]any{
		"operation":       "removeDuplicateInputItems",
		"compare":         "allFieldsExcept",
		"fieldsToExclude": "updatedAt",
	}, []dataplane.Item{
		{JSON: map[string]any{"id": "1", "updatedAt": "old"}},
		{JSON: map[string]any{"id": "1", "updatedAt": "new"}},
		{JSON: map[string]any{"id": "2", "updatedAt": "new"}},
	}))
	if err != nil {
		t.Fatalf("remove duplicates execute: %v", err)
	}
	if len(out[0]) != 2 {
		t.Fatalf("expected two unique items, got %#v", out[0])
	}
}

func TestRemoveDuplicatesTracksItemsSeenInPreviousExecutions(t *testing.T) {
	in := removeDuplicatesHistoryInput("remove-history-keys", map[string]any{
		"operation":   "removeItemsSeenInPreviousExecutions",
		"logic":       "removeItemsWithAlreadySeenKeyValues",
		"dedupeValue": "={{ $json.id }}",
		"scope":       "node",
	}, []dataplane.Item{
		{JSON: map[string]any{"id": "1"}},
		{JSON: map[string]any{"id": "2"}},
	})

	first, err := (RemoveDuplicates{}).Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("first execute: %v", err)
	}
	if len(first) != 2 || len(first[0]) != 2 || len(first[1]) != 0 {
		t.Fatalf("first run should return two new outputs and no processed outputs, got %#v", first)
	}
	second, err := (RemoveDuplicates{}).Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("second execute: %v", err)
	}
	if len(second) != 2 || len(second[0]) != 0 || len(second[1]) != 2 {
		t.Fatalf("second run should return no new outputs and two processed outputs, got %#v", second)
	}
}

func TestRemoveDuplicatesClearDeduplicationHistoryKeepsInputAndClearsState(t *testing.T) {
	seenInput := removeDuplicatesHistoryInput("remove-history-clear", map[string]any{
		"operation":   "removeItemsSeenInPreviousExecutions",
		"logic":       "removeItemsWithAlreadySeenKeyValues",
		"dedupeValue": "={{ $json.id }}",
	}, []dataplane.Item{{JSON: map[string]any{"id": "1"}}})
	if _, err := (RemoveDuplicates{}).Execute(context.Background(), seenInput); err != nil {
		t.Fatalf("seed history: %v", err)
	}

	clearInput := removeDuplicatesHistoryInput("remove-history-clear", map[string]any{
		"operation": "clearDeduplicationHistory",
	}, []dataplane.Item{{JSON: map[string]any{"id": "kept"}}})
	cleared, err := (RemoveDuplicates{}).Execute(context.Background(), clearInput)
	if err != nil {
		t.Fatalf("clear history: %v", err)
	}
	if len(cleared) != 1 || len(cleared[0]) != 1 || cleared[0][0].JSON["id"] != "kept" {
		t.Fatalf("clear should pass through input items, got %#v", cleared)
	}

	afterClear, err := (RemoveDuplicates{}).Execute(context.Background(), seenInput)
	if err != nil {
		t.Fatalf("after clear: %v", err)
	}
	if len(afterClear) != 2 || len(afterClear[0]) != 1 || len(afterClear[1]) != 0 {
		t.Fatalf("item should be new after clear, got %#v", afterClear)
	}
}

func TestRemoveDuplicatesTracksIncrementalHistory(t *testing.T) {
	in := removeDuplicatesHistoryInput("remove-history-incremental", map[string]any{
		"operation":              "removeItemsSeenInPreviousExecutions",
		"logic":                  "removeItemsUpToStoredIncrementalKey",
		"incrementalDedupeValue": "={{ $json.seq }}",
	}, []dataplane.Item{
		{JSON: map[string]any{"seq": 1}},
		{JSON: map[string]any{"seq": 3}},
	})
	if _, err := (RemoveDuplicates{}).Execute(context.Background(), in); err != nil {
		t.Fatalf("seed incremental history: %v", err)
	}
	next := removeDuplicatesHistoryInput("remove-history-incremental", in.Node.Parameters, []dataplane.Item{
		{JSON: map[string]any{"seq": 2}},
		{JSON: map[string]any{"seq": 4}},
	})
	out, err := (RemoveDuplicates{}).Execute(context.Background(), next)
	if err != nil {
		t.Fatalf("incremental execute: %v", err)
	}
	if len(out) != 2 || len(out[0]) != 1 || len(out[1]) != 1 || out[0][0].JSON["seq"] != 4 || out[1][0].JSON["seq"] != 2 {
		t.Fatalf("unexpected incremental outputs: %#v", out)
	}
}

func removeDuplicatesHistoryInput(workflowID string, params map[string]any, items []dataplane.Item) engine.ExecuteInput {
	return engine.ExecuteInput{
		Node:       dataplane.Node{Name: "Remove Duplicates", Parameters: params},
		InputData:  dataplane.MainOutput(items),
		WorkflowID: workflowID,
		Expr:       expr.NewResolver(0),
	}
}
