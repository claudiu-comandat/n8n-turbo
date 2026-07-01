package nodes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

func TestCodeRunOnceForEachItemNormalizesPlainObject(t *testing.T) {
	t.Parallel()

	out, err := (Code{}).Execute(context.Background(), testInput(map[string]any{
		"mode":   "runOnceForEachItem",
		"jsCode": "return { value: $json.value + 1 };",
	}, []dataplane.Item{{JSON: map[string]any{"value": int64(1)}}}))
	if err != nil {
		t.Fatalf("code execute: %v", err)
	}
	want := map[string]any{"value": int64(2)}
	if !reflect.DeepEqual(out[0][0].JSON, want) {
		t.Fatalf("unexpected output\n got: %#v\nwant: %#v", out[0][0].JSON, want)
	}
}

func TestCodeRunOnceForEachItemRejectsArrayLikeOfficialNode(t *testing.T) {
	t.Parallel()

	_, err := (Code{}).Execute(context.Background(), testInput(map[string]any{
		"mode":   "runOnceForEachItem",
		"jsCode": "return [{ json: { value: 1 } }];",
	}, []dataplane.Item{{JSON: map[string]any{"value": 1}}}))
	if err == nil {
		t.Fatalf("expected array return to fail in runOnceForEachItem mode")
	}
}

func TestCodeRejectsUnknownTopLevelKeysWhenJsonIsReturned(t *testing.T) {
	t.Parallel()

	_, err := (Code{}).Execute(context.Background(), testInput(map[string]any{
		"mode":   "runOnceForAllItems",
		"jsCode": "return [{ json: { value: 1 }, value: 2 }];",
	}, []dataplane.Item{{JSON: map[string]any{"value": 1}}}))
	if err == nil {
		t.Fatalf("expected mixed json and unknown top-level keys to fail")
	}
}

func TestCodeSupportsMigrationNodeGlobals(t *testing.T) {
	t.Parallel()

	out, err := (Code{}).Execute(context.Background(), engine.ExecuteInput{
		Node: dataplane.Node{Parameters: map[string]any{
			"jsCode": `
const crypto = require('crypto');
const encoded = Buffer.from('user:pass').toString('base64');
return [{ json: {
  encoded,
  length: Buffer.byteLength('abc'),
  md5: crypto.createHash('md5').update('abc').digest('hex'),
  first: $('Previous').first().json.value,
  item: $items('Previous')[0].json.value
}}];`,
		}},
		InputData: dataplane.MainOutput([]dataplane.Item{{JSON: map[string]any{}}}),
		RunData: dataplane.RunData{
			"Previous": []dataplane.TaskData{{
				Data: dataplane.NodeExecutionData{"main": [][]dataplane.Item{{{JSON: map[string]any{"value": "ok"}}}}},
			}},
		},
	})
	if err != nil {
		t.Fatalf("code execute: %v", err)
	}
	got := out[0][0].JSON
	if got["encoded"] != "dXNlcjpwYXNz" || got["length"] != int64(3) || got["md5"] != "900150983cd24fb0d6963f7d28e17f72" || got["first"] != "ok" || got["item"] != "ok" {
		t.Fatalf("unexpected compat output: %#v", got)
	}
}

func TestCodeSupportsOfficialInputItemProperty(t *testing.T) {
	t.Parallel()

	out, err := (Code{}).Execute(context.Background(), testInput(map[string]any{
		"jsCode": "return [{ json: { value: $input.item.json.value } }];",
	}, []dataplane.Item{{JSON: map[string]any{"value": "ok"}}}))
	if err != nil {
		t.Fatalf("code execute: %v", err)
	}
	if out[0][0].JSON["value"] != "ok" {
		t.Fatalf("unexpected item property output: %#v", out[0][0].JSON)
	}
}

func TestCodeSupportsOfficialNodeLookup(t *testing.T) {
	t.Parallel()

	out, err := (Code{}).Execute(context.Background(), engine.ExecuteInput{
		Node: dataplane.Node{Parameters: map[string]any{
			"jsCode": `return [{ json: { value: $node["Previous"].json.value } }];`,
		}},
		InputData: dataplane.MainOutput([]dataplane.Item{{JSON: map[string]any{}}}),
		RunData: dataplane.RunData{
			"Previous": []dataplane.TaskData{{
				Data: dataplane.NodeExecutionData{"main": [][]dataplane.Item{{{JSON: map[string]any{"value": "ok"}}}}},
			}},
		},
	})
	if err != nil {
		t.Fatalf("code execute: %v", err)
	}
	if out[0][0].JSON["value"] != "ok" {
		t.Fatalf("unexpected node lookup output: %#v", out[0][0].JSON)
	}
}

func TestCodeDelegatesDotAllRegexpToNodeRuntime(t *testing.T) {
	t.Parallel()

	out, err := (Code{}).Execute(context.Background(), testInput(map[string]any{
		"jsCode": `const match = "a\nb".match(/a.*b/s); return [{ json: { ok: !!match } }];`,
	}, []dataplane.Item{{JSON: map[string]any{}}}))
	if err != nil {
		t.Fatalf("code execute: %v", err)
	}
	if out[0][0].JSON["ok"] != true {
		t.Fatalf("unexpected dotAll output: %#v", out[0][0].JSON)
	}
}

func TestPythonCodeSupportsOfficialUnderscoreItems(t *testing.T) {
	t.Parallel()

	out, err := (Code{}).Execute(context.Background(), testInput(map[string]any{
		"language":   "pythonNative",
		"pythonCode": "for item in _items:\n  item['json']['added'] = True\nreturn _items",
	}, []dataplane.Item{{JSON: map[string]any{"value": "ok"}}}))
	if err != nil {
		t.Fatalf("python code execute: %v", err)
	}
	if out[0][0].JSON["added"] != true {
		t.Fatalf("unexpected python output: %#v", out[0][0].JSON)
	}
}

func TestCodeSupportsNodeFetchRequire(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	out, err := (Code{}).Execute(context.Background(), testInput(map[string]any{
		"jsCode": `
const fetch = require('node-fetch');
const res = await fetch('` + server.URL + `');
return [{ json: await res.json() }];`,
	}, []dataplane.Item{{JSON: map[string]any{}}}))
	if err != nil {
		t.Fatalf("code fetch: %v", err)
	}
	if out[0][0].JSON["ok"] != true {
		t.Fatalf("unexpected fetch output: %#v", out[0][0].JSON)
	}
}

func TestCodeSupportsHTTPRequireRequestDataEndPattern(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`hello`))
	}))
	defer server.Close()

	out, err := (Code{}).Execute(context.Background(), testInput(map[string]any{
		"jsCode": `
const http = require('http');
const { URL } = require('url');
const u = new URL('` + server.URL + `/x');
const body = await new Promise((resolve, reject) => {
  const req = http.request({ hostname: u.hostname, port: u.port, path: u.pathname, method: 'GET' }, res => {
    const chunks = [];
    res.on('data', chunk => chunks.push(chunk));
    res.on('end', () => resolve(Buffer.concat(chunks).toString('utf8')));
  });
  req.on('error', reject);
  req.end();
});
return [{ json: { body } }];`,
	}, []dataplane.Item{{JSON: map[string]any{}}}))
	if err != nil {
		t.Fatalf("code http request: %v", err)
	}
	if out[0][0].JSON["body"] != "hello" {
		t.Fatalf("unexpected http output: %#v", out[0][0].JSON)
	}
}

func TestCodeDelegatesPlaywrightExtraToNodeRuntime(t *testing.T) {
	t.Parallel()

	out, err := (Code{}).Execute(context.Background(), testInput(map[string]any{
		"jsCode": `
const { chromium } = require('playwright-extra');
return [{ json: {
  hasConnect: typeof chromium.connectOverCDP === 'function',
  first: $input.first().json.value
} }];`,
	}, []dataplane.Item{{JSON: map[string]any{"value": "ok"}}}))
	if err != nil {
		t.Fatalf("code playwright-extra: %v", err)
	}
	if out[0][0].JSON["hasConnect"] != true || out[0][0].JSON["first"] != "ok" {
		t.Fatalf("unexpected playwright-extra output: %#v", out[0][0].JSON)
	}
}
