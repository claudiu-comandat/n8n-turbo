package nodes

import (
	"bytes"
	"context"
	"fmt"
	htmlstd "html"
	htmltmpl "html/template"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/andybalholm/cascadia"
	"github.com/microcosm-cc/bluemonday"
	"github.com/n8n-io/n8n-turbo/internal/binarydata"
	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
	nethtml "golang.org/x/net/html"
)

type htmlExtractionValue struct {
	Key         string
	Selector    string
	ReturnValue string
	Attribute   string
	ReturnArray bool
}

func executeHTMLNode(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	operation := strings.ToLower(firstNonEmptyNode(stringParam(in.Node.Parameters, "operation"), "generateHtml"))
	items := firstInput(in.InputData)
	output := make([]dataplane.Item, 0, len(items))
	for index, item := range items {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		switch operation {
		case "generatehtml", "generate":
			next, err := generateHTMLItem(in.Node.Parameters, item)
			if err != nil {
				return nil, fmt.Errorf("html generateHtml item %d: %w", index, err)
			}
			output = append(output, next)
		case "extracthtmlcontent", "extract":
			next, err := extractHTMLItem(ctx, in, item)
			if err != nil {
				return nil, fmt.Errorf("html extractHtmlContent item %d: %w", index, err)
			}
			output = append(output, next)
		default:
			return nil, fmt.Errorf("html: unsupported operation %s", operation)
		}
	}
	return dataplane.MainOutput(output), nil
}

func generateHTMLItem(params map[string]any, item dataplane.Item) (dataplane.Item, error) {
	rawTemplate := firstNonEmptyNode(stringParam(params, "html", "template"), fmt.Sprint(item.JSON[firstNonEmptyNode(stringParam(params, "fieldName"), "html")]))
	if rawTemplate == "" {
		return dataplane.Item{}, fmt.Errorf("html template is empty")
	}
	rendered, err := renderHTMLTemplate(rawTemplate, item.JSON)
	if err != nil {
		return dataplane.Item{}, err
	}
	if boolParam(params, "sanitize", false) {
		rendered = sanitizeHTMLValue(rendered, stringParam(params, "sanitizePolicy"))
	}
	if boolParam(htmlNodeOptions(params), "cleanupHTML", false) {
		rendered = cleanupHTML(rendered)
	}
	outputField := firstNonEmptyNode(stringParam(params, "outputFieldName"), "html")
	next := cloneItem(item)
	next.JSON[outputField] = rendered
	return next, nil
}

func renderHTMLTemplate(raw string, data map[string]any) (string, error) {
	converted := convertN8nHTMLTemplate(raw)
	functions := htmltmpl.FuncMap{
		"now": func() string {
			return time.Now().Format(time.RFC3339)
		},
		"upper": strings.ToUpper,
		"lower": strings.ToLower,
		"trim":  strings.TrimSpace,
		"default": func(defaultValue any, value any) any {
			if value == nil || fmt.Sprint(value) == "" {
				return defaultValue
			}
			return value
		},
		"join": func(separator string, values any) string {
			switch typed := values.(type) {
			case []any:
				parts := make([]string, 0, len(typed))
				for _, value := range typed {
					parts = append(parts, fmt.Sprint(value))
				}
				return strings.Join(parts, separator)
			case []string:
				return strings.Join(typed, separator)
			default:
				return fmt.Sprint(values)
			}
		},
		"formatDate": func(format string, value string) string {
			parsed, err := time.Parse(time.RFC3339, value)
			if err != nil {
				return value
			}
			return parsed.Format(format)
		},
		"safe": func(value string) htmltmpl.HTML {
			return htmltmpl.HTML(value)
		},
	}
	tmpl, err := htmltmpl.New("html").Funcs(functions).Parse(converted)
	if err != nil {
		return "", fmt.Errorf("html template parse: %w", err)
	}
	var buffer bytes.Buffer
	if err := tmpl.Execute(&buffer, data); err != nil {
		return "", fmt.Errorf("html template execute: %w", err)
	}
	return buffer.String(), nil
}

func convertN8nHTMLTemplate(raw string) string {
	replacements := []struct {
		pattern string
		value   string
	}{
		{`\{\{\s*\$json\.([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`, `{{index . "$1"}}`},
		{`\{\{\s*\$json\["([^"]+)"\]\s*\}\}`, `{{index . "$1"}}`},
		{`\{\{\s*#if\s+([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`, `{{if index . "$1"}}`},
		{`\{\{\s*/if\s*\}\}`, `{{end}}`},
	}
	converted := raw
	for _, replacement := range replacements {
		converted = regexp.MustCompile(replacement.pattern).ReplaceAllString(converted, replacement.value)
	}
	bare := regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)
	converted = bare.ReplaceAllStringFunc(converted, func(match string) string {
		parts := bare.FindStringSubmatch(match)
		if len(parts) != 2 || parts[1] == "end" || parts[1] == "else" {
			return match
		}
		return `{{index . "` + parts[1] + `"}}`
	})
	return converted
}

func extractHTMLItem(ctx context.Context, in engine.ExecuteInput, item dataplane.Item) (dataplane.Item, error) {
	content, err := htmlContentFromItem(ctx, in, item)
	if err != nil {
		return dataplane.Item{}, err
	}
	if content == "" {
		return dataplane.Item{}, fmt.Errorf("html content is empty")
	}
	document, err := nethtml.Parse(strings.NewReader(content))
	if err != nil {
		return dataplane.Item{}, fmt.Errorf("html parse: %w", err)
	}
	next := cloneItem(item)
	for _, value := range htmlExtractionValues(in.Node.Parameters) {
		if value.Key == "" || value.Selector == "" {
			continue
		}
		extracted, err := extractHTMLSelectorValue(document, value, in.Node.Parameters)
		if err != nil {
			next.JSON[value.Key] = nil
			continue
		}
		next.JSON[value.Key] = extracted
	}
	return next, nil
}

func htmlContentFromItem(ctx context.Context, in engine.ExecuteInput, item dataplane.Item) (string, error) {
	source := strings.ToLower(firstNonEmptyNode(stringParam(in.Node.Parameters, "sourceData"), "json"))
	property := firstNonEmptyNode(stringParam(in.Node.Parameters, "dataProperty", "fieldName"), "html")
	if source == "binary" {
		binaryProperty := firstNonEmptyNode(stringParam(in.Node.Parameters, "binaryPropertyName", "binaryProperty"), "data")
		binary, ok := item.Binary[binaryProperty]
		if !ok {
			return "", fmt.Errorf("binary property %s not found", binaryProperty)
		}
		data, err := binarydata.Read(ctx, in.BinaryStore, binary)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	value, ok := item.JSON[property]
	if !ok {
		return "", fmt.Errorf("field %s not found", property)
	}
	return fmt.Sprint(value), nil
}

func htmlExtractionValues(params map[string]any) []htmlExtractionValue {
	raw := params["extractionValues"]
	if raw == nil {
		raw = params["values"]
	}
	if mapped, ok := raw.(map[string]any); ok {
		raw = firstNonNilHTMLValue(mapped["values"], mapped["extractionValues"])
	}
	values, ok := raw.([]any)
	if !ok {
		return nil
	}
	result := make([]htmlExtractionValue, 0, len(values))
	for _, rawValue := range values {
		object, ok := rawValue.(map[string]any)
		if !ok {
			continue
		}
		result = append(result, htmlExtractionValue{
			Key:         firstNonEmptyNode(stringParam(object, "key", "name"), stringParam(object, "fieldName")),
			Selector:    stringParam(object, "cssSelector", "selector"),
			ReturnValue: strings.ToLower(firstNonEmptyNode(stringParam(object, "returnValue", "type"), "text")),
			Attribute:   stringParam(object, "attribute", "attributeName"),
			ReturnArray: boolParam(object, "returnArray", false) || boolParam(object, "returnAll", false),
		})
	}
	return result
}

func extractHTMLSelectorValue(document *nethtml.Node, value htmlExtractionValue, params map[string]any) (any, error) {
	selector, err := cascadia.ParseGroup(value.Selector)
	if err != nil {
		return nil, err
	}
	nodes := []*nethtml.Node{}
	if value.ReturnArray {
		nodes = cascadia.QueryAll(document, selector)
	} else if node := cascadia.Query(document, selector); node != nil {
		nodes = append(nodes, node)
	}
	if len(nodes) == 0 {
		if value.ReturnArray {
			return []any{}, nil
		}
		return nil, nil
	}
	extract := func(node *nethtml.Node) any {
		return extractHTMLNodeValue(node, value, params)
	}
	if !value.ReturnArray {
		return extract(nodes[0]), nil
	}
	result := make([]any, 0, len(nodes))
	for _, node := range nodes {
		result = append(result, extract(node))
	}
	return result, nil
}

func extractHTMLNodeValue(node *nethtml.Node, value htmlExtractionValue, params map[string]any) any {
	options := htmlNodeOptions(params)
	switch value.ReturnValue {
	case "html":
		return renderHTMLNodeString(node)
	case "innerhtml":
		var builder strings.Builder
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			builder.WriteString(renderHTMLNodeString(child))
		}
		return builder.String()
	case "value":
		if node.Data == "input" || node.Data == "textarea" {
			return htmlNodeAttr(node, "value")
		}
		if node.Data == "select" {
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				if child.Type == nethtml.ElementNode && child.Data == "option" && htmlNodeAttr(child, "selected") != "" {
					return strings.TrimSpace(htmlNodeTextValue(child))
				}
			}
			return nil
		}
		return strings.TrimSpace(htmlNodeTextValue(node))
	case "attribute":
		attribute := htmlNodeAttr(node, value.Attribute)
		if boolParam(options, "unfurlLinks", false) {
			attribute = resolveHTMLNodeURL(attribute, stringParam(options, "baseURL", "baseUrl"), value.Attribute)
		}
		return attribute
	default:
		text := htmlNodeTextValue(node)
		if boolParam(options, "trimWhitespace", true) {
			text = strings.TrimSpace(text)
		}
		return text
	}
}

func htmlNodeTextValue(node *nethtml.Node) string {
	var builder strings.Builder
	htmlNodeTextRecursive(node, &builder)
	return builder.String()
}

func htmlNodeTextRecursive(node *nethtml.Node, builder *strings.Builder) {
	if node.Type == nethtml.TextNode {
		builder.WriteString(node.Data)
		return
	}
	if node.Type == nethtml.ElementNode {
		switch node.Data {
		case "script", "style", "noscript":
			return
		case "br", "p", "div", "h1", "h2", "h3", "h4", "h5", "h6", "li", "tr":
			if builder.Len() > 0 {
				builder.WriteString(" ")
			}
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		htmlNodeTextRecursive(child, builder)
	}
}

func renderHTMLNodeString(node *nethtml.Node) string {
	var buffer bytes.Buffer
	_ = nethtml.Render(&buffer, node)
	return buffer.String()
}

func htmlNodeAttr(node *nethtml.Node, key string) string {
	for _, attr := range node.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

func htmlNodeOptions(params map[string]any) map[string]any {
	if raw, ok := params["options"].(map[string]any); ok {
		return raw
	}
	return params
}

func resolveHTMLNodeURL(raw string, base string, attribute string) string {
	if raw == "" || base == "" {
		return raw
	}
	if attribute != "href" && attribute != "src" && attribute != "action" {
		return raw
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return raw
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return baseURL.ResolveReference(ref).String()
}

func sanitizeHTMLValue(raw string, policy string) string {
	switch strings.ToLower(policy) {
	case "strict":
		return bluemonday.StrictPolicy().Sanitize(raw)
	case "custom":
		p := bluemonday.NewPolicy()
		p.AllowElements("a", "b", "strong", "em", "i", "u", "s", "del", "h1", "h2", "h3", "h4", "h5", "h6", "p", "div", "span", "br", "ul", "ol", "li", "blockquote", "pre", "code", "table", "thead", "tbody", "tfoot", "tr", "th", "td", "img")
		p.AllowAttrs("href", "title", "rel").OnElements("a")
		p.AllowAttrs("src", "alt", "title", "width", "height").OnElements("img")
		p.AllowAttrs("class", "id").Globally()
		p.AllowURLSchemes("https", "http", "mailto")
		return p.Sanitize(raw)
	default:
		return bluemonday.UGCPolicy().Sanitize(raw)
	}
}

func cleanupHTML(raw string) string {
	document, err := nethtml.Parse(strings.NewReader(raw))
	if err != nil {
		return raw
	}
	return renderHTMLNodeString(document)
}

func firstNonNilHTMLValue(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func escapeHTMLString(value string) string {
	return htmlstd.EscapeString(value)
}
