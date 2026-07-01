package nodes

import (
	"reflect"
	"sort"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/metadata"
)

func TestMongoDBRuntimeSupportsOriginalOperations(t *testing.T) {
	t.Parallel()

	got := originalMongoDBOperations(t)
	want := map[string][]string{
		"document":      {"aggregate", "delete", "find", "findOneAndReplace", "findOneAndUpdate", "insert", "update"},
		"searchIndexes": {"createSearchIndex", "dropSearchIndex", "listSearchIndexes", "updateSearchIndex"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MongoDB original operations changed or runtime coverage is stale\n got: %#v\nwant: %#v", got, want)
	}
}

func originalMongoDBOperations(t *testing.T) map[string][]string {
	t.Helper()

	node, ok := metadata.NodeTypeByName("n8n-nodes-base.mongoDb", []string{"n8n-nodes-base.mongoDb"})
	if !ok || node.Raw == nil {
		t.Fatal("mongodb original metadata is unavailable")
	}
	properties, ok := node.Raw["properties"].([]any)
	if !ok {
		t.Fatal("mongodb metadata has no properties")
	}
	result := map[string][]string{}
	for _, raw := range properties {
		prop, ok := raw.(map[string]any)
		if !ok || prop["name"] != "operation" {
			continue
		}
		display, _ := prop["displayOptions"].(map[string]any)
		show, _ := display["show"].(map[string]any)
		options, _ := prop["options"].([]any)
		for _, resource := range mongoDBStringList(show["resource"]) {
			for _, rawOption := range options {
				option, ok := rawOption.(map[string]any)
				if !ok {
					continue
				}
				if value, ok := option["value"].(string); ok {
					result[resource] = append(result[resource], value)
				}
			}
		}
	}
	for resource := range result {
		sort.Strings(result[resource])
	}
	return result
}

func mongoDBStringList(value any) []string {
	values, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, raw := range values {
		if text, ok := raw.(string); ok {
			result = append(result, text)
		}
	}
	return result
}
