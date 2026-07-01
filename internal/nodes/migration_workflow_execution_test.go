package nodes

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

func TestMigrationWorkflowsExecuteWithMockData(t *testing.T) {
	dir := filepath.FromSlash("C:/Users/titam/Documents/migrare_n8n")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("migration workflow folder is unavailable: %v", err)
	}

	registry := engine.NewRegistry()
	RegisterBuiltins(registry)
	evaluator := engine.NewEvaluator(registry)

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		entry := entry
		t.Run(entry.Name(), func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				t.Fatalf("read workflow: %v", err)
			}
			var workflow dataplane.Workflow
			if err := json.Unmarshal(raw, &workflow); err != nil {
				t.Fatalf("parse workflow: %v", err)
			}

			pinData := map[string][]dataplane.Item{}
			for _, node := range workflow.Nodes {
				if shouldMockMigrationNode(workflow, node) || isMigrationStartNode(workflow, node) {
					pinData[node.Name] = []dataplane.Item{mockMigrationItem(node.Name)}
				}
			}

			nodeErrors := []string{}
			result, err := evaluator.ExecuteWithOptions(context.Background(), workflow, "migration-mock-"+migrationExecutionID(entry.Name()), engine.ExecuteOptions{
				PinData: pinData,
				Credentials: func(context.Context, dataplane.Node) (map[string]map[string]any, error) {
					return map[string]map[string]any{}, nil
				},
				Variables: map[string]any{
					"mock": true,
				},
				OnNodeAfter: func(event engine.NodeAfterEvent) {
					if event.Status == "error" && event.TaskData.Error != nil {
						nodeErrors = append(nodeErrors, event.NodeName+": "+event.TaskData.Error.Message)
					}
				},
			})
			if err != nil {
				t.Fatalf("execute with mock data failed after %q: %v\nnode errors: %s", resultLastNodeName(result), err, strings.Join(nodeErrors, "\n"))
			}
			if result == nil || len(result.RunData) == 0 {
				t.Fatalf("expected workflow to produce run data")
			}
		})
	}
}

func shouldMockMigrationNode(workflow dataplane.Workflow, node dataplane.Node) bool {
	switch node.Type {
	case "n8n-nodes-base.httpRequest",
		"n8n-nodes-base.postgres",
		"n8n-nodes-base.n8n",
		"n8n-nodes-base.extractFromFile",
		"n8n-nodes-base.compression",
		"@n8n/n8n-nodes-langchain.agent":
		return true
	case "n8n-nodes-base.code":
		source := stringParam(node.Parameters, "jsCode", "functionCode", "code")
		if strings.Contains(source, "require('https')") ||
			strings.Contains(source, `require("https")`) ||
			strings.Contains(source, "require('node-fetch')") ||
			strings.Contains(source, `require("node-fetch")`) {
			return true
		}
		return !hasMigrationMainOutputs(workflow, node.Name)
	default:
		return false
	}
}

func isMigrationStartNode(workflow dataplane.Workflow, node dataplane.Node) bool {
	if node.Disabled {
		return false
	}
	if isMigrationTriggerType(node.Type) {
		return true
	}
	inverted := dataplane.InvertConnections(workflow.Connections)
	return len(inverted[node.Name]["main"]) == 0 && !isMigrationAiUtilityNode(node.Type)
}

func hasMigrationMainOutputs(workflow dataplane.Workflow, nodeName string) bool {
	byType := workflow.Connections[nodeName]
	for _, edges := range byType["main"] {
		if len(edges) > 0 {
			return true
		}
	}
	return false
}

func isMigrationTriggerType(nodeType string) bool {
	switch nodeType {
	case "n8n-nodes-base.manualTrigger", "n8n-nodes-base.webhook", "n8n-nodes-base.scheduleTrigger":
		return true
	default:
		return false
	}
}

func isMigrationAiUtilityNode(nodeType string) bool {
	return strings.HasPrefix(nodeType, "@n8n/n8n-nodes-langchain.lmChat")
}

func mockMigrationItem(nodeName string) dataplane.Item {
	jsonData := mockMigrationJSON(nodeName)
	return dataplane.Item{
		JSON: jsonData,
		Binary: map[string]dataplane.Binary{
			"data": {
				Data:          "bW9jayxkYXRhCg==",
				MimeType:      "text/csv",
				FileName:      "mock.csv",
				FileExtension: "csv",
			},
		},
	}
}

func mockMigrationJSON(nodeName string) map[string]any {
	data := baseMigrationJSON(nodeName)
	switch nodeName {
	case "Preluare Produse":
		data["data"] = `1:{"error":false,"result":{"data":[{"id":"auction-1","end_at":"2099-01-01T10:00:00.000Z","title":"Mock auction"}],"current_page":1,"last_page":1,"total":1}}`
	case "Primire Raspuns Microservice":
		data["body"] = []any{mockCompetitionResponse()}
	case "Trigger: Primire ASIN-uri Lipsă", "Trigger: Primire ASIN-uri Lipsă1":
		data["body"] = map[string]any{
			"missingProducts": []any{map[string]any{
				"asin":               "B000TEST01",
				"producttitle":       "Mock product",
				"productdescription": "Mock description",
			}},
		}
	case "Login FGO1":
		data["headers"] = map[string]any{"set-cookie": []any{"session=mock"}}
	}
	return data
}

func baseMigrationJSON(nodeName string) map[string]any {
	return map[string]any{
		"id":           "mock-id",
		"asin":         "B000TEST01",
		"sku":          "SKU-TEST",
		"ean":          "5940000000000",
		"title":        "Mock product title",
		"name":         "Mock name",
		"description":  "Mock description",
		"status":       "ok",
		"success":      true,
		"language":     "ro",
		"productTitle": "Mock product title",
		"productUrl":   "https://example.test/product",
		"productimage": "https://example.test/image.jpg",
		"price":        12.34,
		"quantity":     2,
		"sum":          2,
		"items":        []any{map[string]any{"id": "mock-item", "asin": "B000TEST01"}},
		"data": []any{map[string]any{
			"id":  "mock-data",
			"sku": "SKU-TEST",
		}},
		"rows": []any{map[string]any{"id": "mock-row", "value": "mock"}},
		"body": map[string]any{
			"ok":     true,
			"source": nodeName,
			"asins":  []any{"B000TEST01"},
		},
		"headers": map[string]any{
			"set-cookie": []any{"session=mock"},
		},
		"query":          map[string]any{"code": "mock-code"},
		"params":         map[string]any{},
		"mockNodeName":   nodeName,
		"chatInput":      "Mock prompt",
		"output":         "Mock AI output",
		"translatedText": "Text tradus mock",
		"candidates": []any{map[string]any{
			"content": map[string]any{
				"parts": []any{map[string]any{"text": `{"source_doc":"MOCK-1","furnizor":"Mock Supplier","factura_valoare":10,"factura_tva":1.9,"factura_total":11.9}`}},
			},
		}},
	}
}

func mockCompetitionResponse() map[string]any {
	return map[string]any{
		"asin": "B000TEST01",
		"products": []any{map[string]any{
			"productName":    "Mock competitor",
			"productImage":   "https://example.test/competitor.jpg",
			"productUrl":     "https://example.test/competitor",
			"rating":         "4.8",
			"reviewsCount":   "10",
			"oldPrice":       "120",
			"currentPrice":   "99",
			"promotionLabel": "Mock promo",
			"dealType":       "sale",
		}},
		"categories": []any{"mock-category"},
	}
}

func resultLastNodeName(result *engine.Result) string {
	if result == nil {
		return ""
	}
	return result.LastNodeExecuted
}

func migrationExecutionID(value string) string {
	value = strings.TrimSuffix(value, filepath.Ext(value))
	value = strings.ToLower(value)
	replacer := strings.NewReplacer(" ", "-", "/", "-", "\\", "-", ":", "-", "(", "-", ")", "-")
	return replacer.Replace(value)
}
