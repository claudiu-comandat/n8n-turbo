package expr

import (
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func TestResolverSupportsOfficialNodeLookup(t *testing.T) {
	t.Parallel()

	resolver := NewResolver(0)
	resolved, err := resolver.Resolve(`{{ $('Parsare Factură în JSON').first().json.numar_factura }}, {{ JSON.stringify($('Parsare Factură în JSON').first().json.articole) }}`, Context{
		RunData: dataplane.RunData{
			"Parsare Factură în JSON": []dataplane.TaskData{{
				Data: dataplane.NodeExecutionData{"main": [][]dataplane.Item{{{
					JSON: map[string]any{
						"numar_factura": "JEU602702-47-53-300626214358",
						"articole": []any{
							map[string]any{"sku": "LPNHK450426047", "pret_total": 118.07},
							map[string]any{"sku": "LPNHK533189646", "pret_total": 118.07},
						},
					},
				}}}},
			}},
		},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	want := `JEU602702-47-53-300626214358,[{"sku":"LPNHK450426047","pret_total":118.07},{"sku":"LPNHK533189646","pret_total":118.07}]`
	if resolved != want {
		t.Fatalf("unexpected resolved value:\n got: %#v\nwant: %#v", resolved, want)
	}
}
