package nodes

import (
	"context"
	"strings"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func TestXMLToJsonUsesOfficialModeAndReplacesItemJson(t *testing.T) {
	t.Parallel()

	out, err := executeXMLNode(context.Background(), testInput(map[string]any{
		"mode":             "xmlToJson",
		"dataPropertyName": "data",
	}, []dataplane.Item{{JSON: map[string]any{
		"data": "<root id=\"1\"><name>Ana</name></root>",
		"keep": "removed",
	}}}))
	if err != nil {
		t.Fatalf("xml to json: %v", err)
	}
	root, ok := out[0][0].JSON["root"].(map[string]any)
	if !ok {
		t.Fatalf("expected root object, got %#v", out[0][0].JSON)
	}
	if root["id"] != "1" || root["name"] != "Ana" {
		t.Fatalf("unexpected root = %#v", root)
	}
	if _, ok := out[0][0].JSON["keep"]; ok {
		t.Fatalf("xmlToJson should replace item JSON like n8n original, got %#v", out[0][0].JSON)
	}
}

func TestJSONToXMLUsesOfficialModeAndWholeItemJson(t *testing.T) {
	t.Parallel()

	out, err := executeXMLNode(context.Background(), testInput(map[string]any{
		"mode":             "jsonToxml",
		"dataPropertyName": "xml",
		"options": map[string]any{
			"headless": true,
		},
	}, []dataplane.Item{{JSON: map[string]any{
		"name": "Ana",
	}}}))
	if err != nil {
		t.Fatalf("json to xml: %v", err)
	}
	xml, ok := out[0][0].JSON["xml"].(string)
	if !ok {
		t.Fatalf("expected xml field, got %#v", out[0][0].JSON)
	}
	if !strings.Contains(xml, "<name>Ana</name>") {
		t.Fatalf("unexpected xml = %q", xml)
	}
	if out[0][0].PairedItem == nil || out[0][0].PairedItem.Item != 0 {
		t.Fatalf("expected paired item 0, got %#v", out[0][0].PairedItem)
	}
}
