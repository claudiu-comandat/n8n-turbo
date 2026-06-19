package nodes

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	md "github.com/JohannesKaufmann/html-to-markdown"
	mdplugin "github.com/JohannesKaufmann/html-to-markdown/plugin"
	frontmatter "github.com/adrg/frontmatter"
	"github.com/microcosm-cc/bluemonday"
	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
	"github.com/yuin/goldmark"
	emoji "github.com/yuin/goldmark-emoji"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	goldhtml "github.com/yuin/goldmark/renderer/html"
)

type markdownNodeOptions struct {
	Flavor             string
	Tables             bool
	Strikethrough      bool
	Autolinks          bool
	TaskListItems      bool
	Emoji              bool
	Sanitize           bool
	BreakLines         bool
	PreserveLinks      bool
	ConvertTables      bool
	BulletChar         string
	HeadingStyle       string
	CodeBlockFence     string
	ExtractFrontMatter bool
}

func executeMarkdownNode(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	operation := strings.ToLower(firstNonEmptyNode(stringParam(in.Node.Parameters, "operation"), "toHtml"))
	property := firstNonEmptyNode(stringParam(in.Node.Parameters, "dataPropertyName", "dataProperty", "fieldName"), "data")
	options := newMarkdownOptions(in.Node.Parameters)
	items := firstInput(in.InputData)
	output := make([]dataplane.Item, 0, len(items))
	for index, item := range items {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		value, ok := item.JSON[property]
		if !ok {
			return nil, fmt.Errorf("markdown item %d: field %s not found", index, property)
		}
		content := fmt.Sprint(value)
		next := cloneItem(item)
		switch operation {
		case "tohtml", "markdown":
			if options.ExtractFrontMatter {
				front, rest := extractMarkdownFrontMatter(content)
				if front != nil {
					next.JSON["frontMatter"] = front
					content = rest
				}
			}
			converted, err := markdownToHTML(content, options)
			if err != nil {
				return nil, fmt.Errorf("markdown toHtml item %d: %w", index, err)
			}
			next.JSON[property] = converted
		case "fromhtml", "tomarkdown":
			converted, err := htmlToMarkdown(content, options)
			if err != nil {
				return nil, fmt.Errorf("markdown fromHtml item %d: %w", index, err)
			}
			next.JSON[property] = converted
		default:
			return nil, fmt.Errorf("markdown: unsupported operation %s", operation)
		}
		output = append(output, next)
	}
	return dataplane.MainOutput(output), nil
}

func newMarkdownOptions(params map[string]any) markdownNodeOptions {
	options := markdownOptionsMap(params)
	return markdownNodeOptions{
		Flavor:             firstNonEmptyNode(stringParam(options, "flavor"), stringParam(params, "flavor"), "gfm"),
		Tables:             boolParam(options, "tables", boolParam(params, "tables", false)),
		Strikethrough:      boolParam(options, "strikethrough", boolParam(params, "strikethrough", false)),
		Autolinks:          boolParam(options, "autolinks", boolParam(params, "autolinks", false)),
		TaskListItems:      boolParam(options, "taskListItems", boolParam(params, "taskListItems", false)),
		Emoji:              boolParam(options, "emoji", boolParam(params, "emoji", false)),
		Sanitize:           boolParam(options, "sanitize", boolParam(params, "sanitize", false)),
		BreakLines:         boolParam(options, "breakLines", boolParam(params, "breakLines", false)),
		PreserveLinks:      boolParam(options, "preserveLinks", boolParam(params, "preserveLinks", true)),
		ConvertTables:      boolParam(options, "convertTables", boolParam(params, "convertTables", false)),
		BulletChar:         firstNonEmptyNode(stringParam(options, "bulletChar"), stringParam(params, "bulletChar"), "-"),
		HeadingStyle:       firstNonEmptyNode(stringParam(options, "headingStyle"), stringParam(params, "headingStyle"), "atx"),
		CodeBlockFence:     firstNonEmptyNode(stringParam(options, "codeBlockFence"), stringParam(params, "codeBlockFence"), "```"),
		ExtractFrontMatter: boolParam(options, "extractFrontMatter", boolParam(params, "extractFrontMatter", false)),
	}
}

func markdownOptionsMap(params map[string]any) map[string]any {
	if options, ok := params["options"].(map[string]any); ok {
		return options
	}
	return map[string]any{}
}

func markdownToHTML(input string, options markdownNodeOptions) (string, error) {
	if input == "" {
		return "", nil
	}
	extensions := []goldmark.Extender{}
	if options.Flavor == "gfm" {
		extensions = append(extensions, extension.GFM)
	} else {
		if options.Tables {
			extensions = append(extensions, extension.Table)
		}
		if options.Strikethrough {
			extensions = append(extensions, extension.Strikethrough)
		}
		if options.TaskListItems {
			extensions = append(extensions, extension.TaskList)
		}
		if options.Autolinks {
			extensions = append(extensions, extension.Linkify)
		}
	}
	if options.Emoji {
		extensions = append(extensions, emoji.Emoji)
	}
	rendererOptions := []renderer.Option{goldhtml.WithUnsafe(), goldhtml.WithXHTML()}
	if options.BreakLines {
		rendererOptions = append(rendererOptions, goldhtml.WithHardWraps())
	}
	converter := goldmark.New(
		goldmark.WithExtensions(extensions...),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		goldmark.WithRendererOptions(rendererOptions...),
	)
	var buffer bytes.Buffer
	if err := converter.Convert([]byte(input), &buffer); err != nil {
		return "", err
	}
	output := strings.TrimSpace(buffer.String())
	if options.Sanitize {
		policy := bluemonday.UGCPolicy()
		policy.AllowAttrs("class").OnElements("code", "pre", "span")
		policy.AllowAttrs("id").Globally()
		output = policy.Sanitize(output)
	}
	return strings.TrimSpace(output), nil
}

func htmlToMarkdown(input string, options markdownNodeOptions) (string, error) {
	if input == "" {
		return "", nil
	}
	bullet := options.BulletChar
	if bullet != "-" && bullet != "*" && bullet != "+" {
		bullet = "-"
	}
	heading := options.HeadingStyle
	if heading != "setext" {
		heading = "atx"
	}
	fence := options.CodeBlockFence
	if fence != "~~~" {
		fence = "```"
	}
	linkStyle := "inlined"
	if !options.PreserveLinks {
		linkStyle = "referenced"
	}
	converter := md.NewConverter("", true, &md.Options{
		HeadingStyle:     heading,
		BulletListMarker: bullet,
		CodeBlockStyle:   "fenced",
		Fence:            fence,
		LinkStyle:        linkStyle,
	})
	if options.ConvertTables || options.Flavor == "gfm" {
		converter.Use(mdplugin.Table())
	}
	if options.TaskListItems || options.Flavor == "gfm" {
		converter.Use(mdplugin.TaskListItems())
	}
	if options.Strikethrough || options.Flavor == "gfm" {
		converter.Use(mdplugin.Strikethrough(""))
	}
	output, err := converter.ConvertString(input)
	if err != nil {
		return "", err
	}
	lines := strings.Split(output, "\n")
	for index, line := range lines {
		lines[index] = strings.TrimRight(line, " \t")
	}
	output = strings.Join(lines, "\n")
	for strings.Contains(output, "\n\n\n") {
		output = strings.ReplaceAll(output, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(output), nil
}

func extractMarkdownFrontMatter(input string) (map[string]any, string) {
	var data map[string]any
	rest, err := frontmatter.Parse(strings.NewReader(input), &data)
	if err != nil || data == nil {
		return nil, input
	}
	return data, strings.TrimSpace(string(rest))
}
