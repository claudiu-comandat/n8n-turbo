package nodes

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
	"github.com/n8n-io/n8n-turbo/internal/metadata"
)

func TestMigrationWorkflowsUseAvailableNodeTypes(t *testing.T) {
	t.Parallel()

	dir := filepath.FromSlash("C:/Users/titam/Documents/migrare_n8n")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("migration workflow folder is unavailable: %v", err)
	}

	registry := engine.NewRegistry()
	RegisterBuiltins(registry)
	checked := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		var workflow dataplane.Workflow
		if err := json.Unmarshal(raw, &workflow); err != nil {
			t.Fatalf("parse %s: %v", entry.Name(), err)
		}
		for _, node := range workflow.Nodes {
			checked[node.Type] = true
			if _, ok := registry.Executor(node.Type); !ok {
				t.Errorf("%s uses %s, but no Go executor is registered", entry.Name(), node.Type)
			}
			if _, ok := metadata.NodeTypeByName(node.Type, registry.KnownTypes()); !ok {
				t.Errorf("%s uses %s, but no metadata is available", entry.Name(), node.Type)
			}
		}
	}
	if len(checked) != 17 {
		t.Fatalf("expected 17 distinct migration node types, got %d: %#v", len(checked), checked)
	}
}

func TestMigrationWorkflowCredentialsUseAvailableCredentialTypes(t *testing.T) {
	t.Parallel()

	dir := filepath.FromSlash("C:/Users/titam/Documents/migrare_n8n")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("migration workflow folder is unavailable: %v", err)
	}

	credentialTypes := map[string]bool{}
	for _, credential := range metadata.CredentialTypes() {
		credentialTypes[credential.Name] = true
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		var workflow dataplane.Workflow
		if err := json.Unmarshal(raw, &workflow); err != nil {
			t.Fatalf("parse %s: %v", entry.Name(), err)
		}
		for _, node := range workflow.Nodes {
			for credentialType := range node.Credentials {
				if !credentialTypes[credentialType] {
					t.Errorf("%s / %s uses credential type %s, but no metadata is available", entry.Name(), node.Name, credentialType)
				}
			}
		}
	}
}

func TestMigrationWorkflowOperationsAreCovered(t *testing.T) {
	t.Parallel()

	dir := filepath.FromSlash("C:/Users/titam/Documents/migrare_n8n")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("migration workflow folder is unavailable: %v", err)
	}

	covered := map[string]bool{
		"@n8n/n8n-nodes-langchain.agent||":                true,
		"@n8n/n8n-nodes-langchain.lmChatGoogleGemini||":   true,
		"n8n-nodes-base.code||":                           true,
		"n8n-nodes-base.compression||":                    true,
		"n8n-nodes-base.convertToFile||xlsx":              true,
		"n8n-nodes-base.extractFromFile||binaryToPropery": true,
		"n8n-nodes-base.extractFromFile||pdf":             true,
		"n8n-nodes-base.extractFromFile||xlsx":            true,
		"n8n-nodes-base.filter||":                         true,
		"n8n-nodes-base.httpRequest||":                    true,
		"n8n-nodes-base.if||":                             true,
		"n8n-nodes-base.manualTrigger||":                  true,
		"n8n-nodes-base.n8n|execution|":                   true,
		"n8n-nodes-base.postgres||":                       true,
		"n8n-nodes-base.postgres||executeQuery":           true,
		"n8n-nodes-base.postgres||select":                 true,
		"n8n-nodes-base.postgres||upsert":                 true,
		"n8n-nodes-base.respondToWebhook||":               true,
		"n8n-nodes-base.scheduleTrigger||":                true,
		"n8n-nodes-base.splitOut||":                       true,
		"n8n-nodes-base.stickyNote||":                     true,
		"n8n-nodes-base.webhook||":                        true,
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		var workflow dataplane.Workflow
		if err := json.Unmarshal(raw, &workflow); err != nil {
			t.Fatalf("parse %s: %v", entry.Name(), err)
		}
		for _, node := range workflow.Nodes {
			key := node.Type + "|" + parameterString(node.Parameters, "resource") + "|" + parameterString(node.Parameters, "operation")
			if !covered[key] {
				t.Errorf("%s / %s uses uncovered operation %s", entry.Name(), node.Name, key)
			}
		}
	}
}

func parameterString(params map[string]any, key string) string {
	if params == nil {
		return ""
	}
	value, ok := params[key]
	if !ok || value == nil {
		return ""
	}
	return stringParam(params, key)
}
