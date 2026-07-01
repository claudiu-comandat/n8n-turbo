package nodes

import (
	"reflect"
	"sort"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/metadata"
)

func TestRedisRuntimeSupportsOriginalOperations(t *testing.T) {
	t.Parallel()

	got := originalRedisOperations(t)
	want := []string{"delete", "get", "incr", "info", "keys", "llen", "pop", "publish", "push", "set"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Redis original operations changed or runtime coverage is stale\n got: %#v\nwant: %#v", got, want)
	}
}

func TestRedisInfoToObjectMatchesN8NParserShape(t *testing.T) {
	got := redisInfoToObject("# Server\r\nredis_version:7.2.1\r\nconnected_clients:3\r\ndb0:keys=2,expires=1,avg_ttl=42\r\n")

	if got["redis_version"] != "7.2.1" {
		t.Fatalf("redis_version = %#v", got["redis_version"])
	}
	if got["connected_clients"] != float64(3) {
		t.Fatalf("connected_clients = %#v", got["connected_clients"])
	}
	db0, ok := got["db0"].(map[string]any)
	if !ok || db0["keys"] != float64(2) || db0["expires"] != float64(1) || db0["avg_ttl"] != float64(42) {
		t.Fatalf("db0 = %#v", got["db0"])
	}
}

func TestRedisSetOutputValueUsesOfficialDotNotationOption(t *testing.T) {
	withDots := map[string]any{}
	redisSetOutputValue(withDots, "data.person.name", "Ana", true)
	data := withDots["data"].(map[string]any)
	person := data["person"].(map[string]any)
	if person["name"] != "Ana" {
		t.Fatalf("dot notation output = %#v", withDots)
	}

	withoutDots := map[string]any{}
	redisSetOutputValue(withoutDots, "data.person.name", "Ana", false)
	if withoutDots["data.person.name"] != "Ana" {
		t.Fatalf("literal output = %#v", withoutDots)
	}
}

func TestRedisOfficialKeysRunsPerInputItem(t *testing.T) {
	if redisSingleOutputOperation(map[string]any{"operation": "keys"}) {
		t.Fatal("official Redis keys must run once per input item")
	}
	if !redisSingleOutputOperation(map[string]any{"operation": "info"}) {
		t.Fatal("Redis info is the single-output operation")
	}
}

func originalRedisOperations(t *testing.T) []string {
	t.Helper()

	node, ok := metadata.NodeTypeByName("n8n-nodes-base.redis", []string{"n8n-nodes-base.redis"})
	if !ok || node.Raw == nil {
		t.Fatal("redis original metadata is unavailable")
	}
	properties, ok := node.Raw["properties"].([]any)
	if !ok {
		t.Fatal("redis metadata has no properties")
	}
	var result []string
	for _, raw := range properties {
		prop, ok := raw.(map[string]any)
		if !ok || prop["name"] != "operation" {
			continue
		}
		options, _ := prop["options"].([]any)
		for _, rawOption := range options {
			option, ok := rawOption.(map[string]any)
			if !ok {
				continue
			}
			if value, ok := option["value"].(string); ok {
				result = append(result, value)
			}
		}
	}
	sort.Strings(result)
	return result
}
