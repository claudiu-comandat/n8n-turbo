package nodes

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

func TestLoopOverItemsMatchesOfficialOutputOrder(t *testing.T) {
	t.Parallel()

	input := engine.ExecuteInput{
		ExecutionID: "loop-output-order",
		Node: dataplane.Node{
			ID:         "loop",
			Name:       "Loop Over Items",
			Type:       "n8n-nodes-base.splitInBatches",
			Parameters: map[string]any{"batchSize": 2},
		},
		InputData: dataplane.MainOutput([]dataplane.Item{
			{JSON: map[string]any{"id": 1}},
			{JSON: map[string]any{"id": 2}},
			{JSON: map[string]any{"id": 3}},
		}),
	}
	defaultLoopStateStore.Delete(loopStateKey(input))
	t.Cleanup(func() { defaultLoopStateStore.Delete(loopStateKey(input)) })

	first, err := (LoopOverItems{}).Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("first loop execute: %v", err)
	}
	if len(first) != 2 || len(first[0]) != 0 || len(first[1]) != 2 {
		t.Fatalf("first call should return [done empty, loop batch], got %#v", first)
	}

	second, err := (LoopOverItems{}).Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("second loop execute: %v", err)
	}
	if len(second) != 2 || len(second[0]) != 0 || len(second[1]) != 1 {
		t.Fatalf("second call should return remaining loop batch, got %#v", second)
	}

	done, err := (LoopOverItems{}).Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("done loop execute: %v", err)
	}
	if len(done) != 2 || len(done[0]) != 3 || len(done[1]) != 0 {
		t.Fatalf("done call should return [processed items, loop empty], got %#v", done)
	}
}

func TestWaitMatchesOfficialTimeIntervalRules(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	immediate, _, err := waitResumeAt(map[string]any{
		"resume": "timeInterval",
		"amount": 0,
		"unit":   "seconds",
	}, now)
	if err != nil {
		t.Fatalf("zero amount should be valid: %v", err)
	}
	if !immediate.Equal(now) {
		t.Fatalf("zero amount should not add delay, got %s", immediate)
	}

	delayed, _, err := waitResumeAt(map[string]any{
		"resume": "timeInterval",
		"amount": 1.5,
		"unit":   "seconds",
	}, now)
	if err != nil {
		t.Fatalf("decimal amount should be valid: %v", err)
	}
	if got, want := delayed.Sub(now), 1500*time.Millisecond; got != want {
		t.Fatalf("decimal amount delay mismatch: got %s want %s", got, want)
	}

	_, _, err = waitResumeAt(map[string]any{
		"resume": "timeInterval",
		"amount": 1,
		"unit":   "weeks",
	}, now)
	if err == nil || !strings.Contains(err.Error(), "unsupported wait unit weeks") {
		t.Fatalf("unexpected invalid unit error: %v", err)
	}
}
