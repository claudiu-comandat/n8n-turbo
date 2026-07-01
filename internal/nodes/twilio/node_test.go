package twilio

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
	"github.com/n8n-io/n8n-turbo/internal/metadata"
)

func TestTwilioSMSSendMatchesOfficialWhatsappParams(t *testing.T) {
	t.Parallel()

	path, form := executeTwilioForTest(t, map[string]any{
		"resource":   "sms",
		"operation":  "send",
		"from":       "+14155238886",
		"to":         "+14155238887",
		"toWhatsapp": true,
		"message":    "hello",
		"options": map[string]any{
			"statusCallback": "https://example.test/status",
		},
	})
	if path != "/Messages.json" {
		t.Fatalf("path = %q", path)
	}
	if form.Get("From") != "whatsapp:+14155238886" || form.Get("To") != "whatsapp:+14155238887" || form.Get("Body") != "hello" || form.Get("StatusCallback") != "https://example.test/status" {
		t.Fatalf("form = %#v", form)
	}
}

func TestTwilioCallMakeWrapsOfficialMessageAsTwiML(t *testing.T) {
	t.Parallel()

	path, form := executeTwilioForTest(t, map[string]any{
		"resource":  "call",
		"operation": "make",
		"from":      "+14155238886",
		"to":        "+14155238887",
		"twiml":     false,
		"message":   "Hi <Ana>",
		"options": map[string]any{
			"statusCallback": "https://example.test/call-status",
		},
	})
	if path != "/Calls.json" {
		t.Fatalf("path = %q", path)
	}
	wantTwiML := "<Response><Say>Hi &lt;Ana&gt;</Say></Response>"
	if form.Get("Twiml") != wantTwiML || form.Get("StatusCallback") != "https://example.test/call-status" {
		t.Fatalf("form = %#v", form)
	}
}

func TestTwilioCallMakeUsesOfficialTwiMLMessageWhenEnabled(t *testing.T) {
	t.Parallel()

	_, form := executeTwilioForTest(t, map[string]any{
		"resource":  "call",
		"operation": "make",
		"from":      "+14155238886",
		"to":        "+14155238887",
		"twiml":     true,
		"message":   "<Response><Pause length=\"1\"/></Response>",
	})
	if form.Get("Twiml") != "<Response><Pause length=\"1\"/></Response>" {
		t.Fatalf("form = %#v", form)
	}
}

func TestTwilioRuntimeSupportsOriginalOperations(t *testing.T) {
	t.Parallel()

	got := originalTwilioOperations(t)
	want := map[string][]string{
		"call": {"make"},
		"sms":  {"send"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Twilio original operations changed or runtime coverage is stale\n got: %#v\nwant: %#v", got, want)
	}
}

func executeTwilioForTest(t *testing.T, params map[string]any) (string, url.Values) {
	t.Helper()

	var gotPath string
	var gotForm url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sid":"SM123"}`))
	}))
	t.Cleanup(server.Close)

	node := NewWithBaseURL(server.URL)
	_, err := node.Execute(context.Background(), engine.ExecuteInput{
		Node: dataplane.Node{Parameters: params},
		Credentials: map[string]map[string]any{"twilioApi": {
			"accountSid": "AC" + strings.Repeat("a", 32),
			"authToken":  strings.Repeat("b", 32),
		}},
		InputData: dataplane.Output{{{JSON: map[string]any{}}}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return gotPath, gotForm
}

func originalTwilioOperations(t *testing.T) map[string][]string {
	t.Helper()

	node, ok := metadata.NodeTypeByName("n8n-nodes-base.twilio", []string{"n8n-nodes-base.twilio"})
	if !ok || node.Raw == nil {
		t.Fatal("twilio original metadata is unavailable")
	}
	properties, ok := node.Raw["properties"].([]any)
	if !ok {
		t.Fatal("twilio metadata has no properties")
	}
	result := map[string][]string{}
	for _, raw := range properties {
		prop, ok := raw.(map[string]any)
		if !ok || prop["name"] != "operation" {
			continue
		}
		display, _ := prop["displayOptions"].(map[string]any)
		show, _ := display["show"].(map[string]any)
		resources := twilioStringList(show["resource"])
		options, _ := prop["options"].([]any)
		for _, resource := range resources {
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

func twilioStringList(value any) []string {
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
