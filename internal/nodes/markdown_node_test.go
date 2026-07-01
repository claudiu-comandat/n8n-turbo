package nodes

import (
	"context"
	"strings"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

func TestMarkdownSupportsOfficialHTMLToMarkdownMode(t *testing.T) {
	t.Parallel()

	output, err := (Markdown{}).Execute(context.Background(), engine.ExecuteInput{
		Node: dataplane.Node{
			Name: "Markdown",
			Type: "n8n-nodes-base.markdown",
			Parameters: map[string]any{
				"mode":           "htmlToMarkdown",
				"html":           "<h1>Hello</h1><p>world</p>",
				"destinationKey": "data.markdown",
			},
		},
		InputData: dataplane.MainOutput([]dataplane.Item{{JSON: map[string]any{"keep": true}}}),
	})
	if err != nil {
		t.Fatalf("execute markdown: %v", err)
	}
	data := output[0][0].JSON["data"].(map[string]any)
	markdown := data["markdown"].(string)
	if !strings.Contains(markdown, "# Hello") || !strings.Contains(markdown, "world") {
		t.Fatalf("unexpected markdown output: %q", markdown)
	}
	if output[0][0].JSON["keep"] != true {
		t.Fatalf("original fields should be preserved: %#v", output[0][0].JSON)
	}
}

func TestMarkdownSupportsOfficialMarkdownToHTMLMode(t *testing.T) {
	t.Parallel()

	output, err := (Markdown{}).Execute(context.Background(), engine.ExecuteInput{
		Node: dataplane.Node{
			Name: "Markdown",
			Type: "n8n-nodes-base.markdown",
			Parameters: map[string]any{
				"mode":           "markdownToHtml",
				"markdown":       "## Hello",
				"destinationKey": "html",
			},
		},
		InputData: dataplane.MainOutput([]dataplane.Item{{JSON: map[string]any{}}}),
	})
	if err != nil {
		t.Fatalf("execute markdown: %v", err)
	}
	html := output[0][0].JSON["html"].(string)
	if !strings.Contains(html, "<h2") || !strings.Contains(html, "Hello") {
		t.Fatalf("unexpected html output: %q", html)
	}
}
