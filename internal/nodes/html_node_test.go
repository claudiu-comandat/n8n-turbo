package nodes

import (
	"context"
	"strings"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

func TestHTMLSupportsOfficialConvertToHTMLTableOperation(t *testing.T) {
	t.Parallel()

	output, err := (HTML{}).Execute(context.Background(), engine.ExecuteInput{
		Node: dataplane.Node{
			Name: "HTML",
			Type: "n8n-nodes-base.html",
			Parameters: map[string]any{
				"operation": "convertToHtmlTable",
				"options": map[string]any{
					"caption":    "Products",
					"capitalize": true,
				},
			},
		},
		InputData: dataplane.MainOutput([]dataplane.Item{
			{JSON: map[string]any{"product_title": "A", "active": true}},
			{JSON: map[string]any{"product_title": "B", "active": false}},
		}),
	})
	if err != nil {
		t.Fatalf("execute html: %v", err)
	}
	if len(output[0]) != 1 {
		t.Fatalf("expected one table item, got %#v", output)
	}
	table := output[0][0].JSON["table"].(string)
	for _, want := range []string{"<table", "<caption>Products</caption>", "<th>Product Title</th>", "<td", "A", "B", `checked="checked"`} {
		if !strings.Contains(table, want) {
			t.Fatalf("table missing %q: %s", want, table)
		}
	}
}
