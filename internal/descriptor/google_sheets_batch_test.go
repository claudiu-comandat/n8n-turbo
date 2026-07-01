package descriptor

import (
	"encoding/json"
	"io"
	"reflect"
	"testing"
)

func TestGoogleSheetsCreateSheetBody(t *testing.T) {
	t.Parallel()

	body := mustRequestBody(t, Operation{Name: "createSheet"}, map[string]any{"title": "ROI"})
	want := map[string]any{"requests": []any{map[string]any{
		"addSheet": map[string]any{"properties": map[string]any{"title": "ROI"}},
	}}}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("body = %#v, want %#v", body, want)
	}
}

func TestGoogleSheetsRemoveSheetBody(t *testing.T) {
	t.Parallel()

	body := mustRequestBody(t, Operation{Name: "removeSheet"}, map[string]any{"sheetName": map[string]any{"value": "12345"}})
	want := map[string]any{"requests": []any{map[string]any{
		"deleteSheet": map[string]any{"sheetId": float64(12345)},
	}}}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("body = %#v, want %#v", body, want)
	}
}

func TestGoogleSheetsDeleteRowsBody(t *testing.T) {
	t.Parallel()

	body := mustRequestBody(t, Operation{Name: "deleteDimension"}, map[string]any{
		"sheetName":      map[string]any{"value": "99"},
		"toDelete":       "rows",
		"startIndex":     float64(2),
		"numberToDelete": float64(3),
	})
	wantRange := map[string]any{"sheetId": float64(99), "dimension": "ROWS", "startIndex": float64(1), "endIndex": float64(4)}
	gotRange := body["requests"].([]any)[0].(map[string]any)["deleteDimension"].(map[string]any)["range"]
	if !reflect.DeepEqual(gotRange, wantRange) {
		t.Fatalf("range = %#v, want %#v", gotRange, wantRange)
	}
}

func TestGoogleSheetsDeleteColumnsBody(t *testing.T) {
	t.Parallel()

	body := mustRequestBody(t, Operation{Name: "deleteDimension"}, map[string]any{
		"sheetName":      map[string]any{"value": "99"},
		"toDelete":       "columns",
		"startIndex":     "C",
		"numberToDelete": float64(2),
	})
	wantRange := map[string]any{"sheetId": float64(99), "dimension": "COLUMNS", "startIndex": float64(2), "endIndex": float64(4)}
	gotRange := body["requests"].([]any)[0].(map[string]any)["deleteDimension"].(map[string]any)["range"]
	if !reflect.DeepEqual(gotRange, wantRange) {
		t.Fatalf("range = %#v, want %#v", gotRange, wantRange)
	}
}

func mustRequestBody(t *testing.T, operation Operation, params map[string]any) map[string]any {
	t.Helper()
	reader, contentType, err := requestBody(operation, params)
	if err != nil {
		t.Fatalf("requestBody: %v", err)
	}
	if contentType != "application/json" {
		t.Fatalf("contentType = %q", contentType)
	}
	raw, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return body
}
