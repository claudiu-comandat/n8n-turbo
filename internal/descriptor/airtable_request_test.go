package descriptor

import (
	"reflect"
	"testing"
)

func TestAirtableUpsertBodyUsesResourceMapperColumns(t *testing.T) {
	t.Parallel()

	body, ok, err := airtableRequestBody(Operation{Name: "upsertRecord"}, map[string]any{
		"columns": map[string]any{
			"value":           map[string]any{"Email": "ana@example.test"},
			"matchingColumns": []any{"Email"},
		},
		"typecast": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("airtableRequestBody did not handle upsertRecord")
	}
	records := body["records"].([]any)
	fields := records[0].(map[string]any)["fields"]
	if !reflect.DeepEqual(fields, map[string]any{"Email": "ana@example.test"}) || body["typecast"] != true {
		t.Fatalf("upsert body = %#v", body)
	}
	upsert := body["performUpsert"].(map[string]any)
	if !reflect.DeepEqual(upsert["fieldsToMergeOn"], []any{"Email"}) {
		t.Fatalf("performUpsert = %#v", upsert)
	}
}
