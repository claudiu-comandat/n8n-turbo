package nodes

import (
	"context"
	"reflect"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func TestDateTimeAcceptsOfficialFormatDateOperation(t *testing.T) {
	t.Parallel()

	out, err := (DateTime{}).Execute(context.Background(), testInput(map[string]any{
		"operation":       "formatDate",
		"date":            "2026-06-25T10:11:12Z",
		"format":          "yyyy-MM-dd",
		"outputFieldName": "formatted",
	}, []dataplane.Item{{JSON: map[string]any{"old": "drop"}}}))
	if err != nil {
		t.Fatalf("date time execute: %v", err)
	}
	if got := out[0][0].JSON; len(got) != 1 || got["formatted"] != "2026-06-25" {
		t.Fatalf("unexpected formatted output: %#v", got)
	}
	if out[0][0].PairedItem == nil || out[0][0].PairedItem.Item != 0 {
		t.Fatalf("DateTime should set paired item like n8n original: %#v", out[0][0].PairedItem)
	}
}

func TestDateTimeDefaultsToOfficialCurrentDateOperation(t *testing.T) {
	t.Parallel()

	out, err := (DateTime{}).Execute(context.Background(), testInput(map[string]any{}, []dataplane.Item{{JSON: map[string]any{}}}))
	if err != nil {
		t.Fatalf("date time default execute: %v", err)
	}
	if got := out[0][0].JSON["outputDate"]; got == nil || got == "" {
		t.Fatalf("expected default current date output, got %#v", out[0][0].JSON)
	}
}

func TestDateTimeAcceptsOfficialAddToDateOperation(t *testing.T) {
	t.Parallel()

	out, err := (DateTime{}).Execute(context.Background(), testInput(map[string]any{
		"operation":       "addToDate",
		"magnitude":       "2026-06-25T10:00:00Z",
		"timeUnit":        "days",
		"duration":        2,
		"outputFieldName": "result",
		"options":         map[string]any{"includeInputFields": true},
	}, []dataplane.Item{{JSON: map[string]any{"old": "keep"}}}))
	if err != nil {
		t.Fatalf("date time execute: %v", err)
	}
	got := out[0][0].JSON
	if got["old"] != "keep" || got["result"] != "2026-06-27T10:00:00Z" {
		t.Fatalf("unexpected add output: %#v", got)
	}
}

func TestDateTimeAcceptsOfficialGetTimeBetweenDatesOperation(t *testing.T) {
	t.Parallel()

	out, err := (DateTime{}).Execute(context.Background(), testInput(map[string]any{
		"operation": "getTimeBetweenDates",
		"startDate": "={{ $json.start }}",
		"endDate":   "={{ $json.end }}",
		"units":     []any{"day", "hour"},
		"options":   map[string]any{"includeInputFields": true},
	}, []dataplane.Item{{JSON: map[string]any{
		"start": "2026-01-01T00:00:00Z",
		"end":   "2026-01-03T06:00:00Z",
		"keep":  "yes",
	}}}))
	if err != nil {
		t.Fatalf("date time between execute: %v", err)
	}
	got := out[0][0].JSON
	want := map[string]any{
		"start":          "2026-01-01T00:00:00Z",
		"end":            "2026-01-03T06:00:00Z",
		"keep":           "yes",
		"timeDifference": map[string]any{"days": float64(2), "hours": float64(6)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected between output\n got: %#v\nwant: %#v", got, want)
	}
}

func TestDateTimeGetTimeBetweenDatesCanReturnISO(t *testing.T) {
	t.Parallel()

	out, err := (DateTime{}).Execute(context.Background(), testInput(map[string]any{
		"operation":       "getTimeBetweenDates",
		"startDate":       "2026-01-01T00:00:00Z",
		"endDate":         "2026-01-03T06:00:00Z",
		"units":           []any{"day", "hour"},
		"outputFieldName": "diff",
		"options":         map[string]any{"isoString": true},
	}, []dataplane.Item{{JSON: map[string]any{}}}))
	if err != nil {
		t.Fatalf("date time between iso execute: %v", err)
	}
	if got := out[0][0].JSON["diff"]; got != "P2DT6H" {
		t.Fatalf("unexpected ISO duration: %#v", got)
	}
}
