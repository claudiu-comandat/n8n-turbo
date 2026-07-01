package nodes

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type xmlNodeOptions struct {
	AttributePrefix    string
	TextNodeKey        string
	ForceArray         bool
	ParseNumbers       bool
	ParseBooleans      bool
	ExplicitRoot       bool
	RootName           string
	XMLDeclaration     bool
	XMLVersion         string
	XMLEncoding        string
	AttributeChar      string
	CDATAKey           string
	PreserveNamespaces bool
	IgnoreNamespaces   bool
	IgnoreAttrs        bool
	MergeAttrs         bool
	NormalizeTags      bool
	Trim               bool
}

func executeXMLNode(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	operation := normalizeXMLOperation(firstNonEmptyNode(stringParam(in.Node.Parameters, "mode"), stringParam(in.Node.Parameters, "operation"), "xmlToJson"))
	property := firstNonEmptyNode(stringParam(in.Node.Parameters, "dataPropertyName", "dataProperty", "fieldName"), "data")
	options := newXMLNodeOptions(in.Node.Parameters)
	items := firstInput(in.InputData)
	output := make([]dataplane.Item, 0, len(items))
	for index, item := range items {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		switch operation {
		case "xmlToJson":
			value, ok := item.JSON[property]
			if !ok {
				return nil, fmt.Errorf("xml item %d: field %s not found", index, property)
			}
			converted, err := xmlToJSON(fmt.Sprint(value), options)
			if err != nil {
				return nil, fmt.Errorf("xml xmlToJson item %d: %w", index, err)
			}
			output = append(output, dataplane.Item{JSON: converted})
		case "jsonToxml":
			converted, err := jsonToXML(item.JSON, options)
			if err != nil {
				return nil, fmt.Errorf("xml jsonToxml item %d: %w", index, err)
			}
			output = append(output, dataplane.Item{JSON: map[string]any{property: converted}, PairedItem: &dataplane.PairedItem{Item: index}})
		case "validate":
			next := cloneItem(item)
			value, ok := item.JSON[property]
			if !ok {
				return nil, fmt.Errorf("xml item %d: field %s not found", index, property)
			}
			valid, errors := validateXMLString(fmt.Sprint(value))
			next.JSON["isValid"] = valid
			next.JSON["errors"] = errors
			output = append(output, next)
		default:
			return nil, fmt.Errorf("xml: unsupported operation %s", operation)
		}
	}
	return dataplane.MainOutput(output), nil
}

func normalizeXMLOperation(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "xmltojson", "tojson":
		return "xmlToJson"
	case "jsontoxml", "fromjson":
		return "jsonToxml"
	default:
		return value
	}
}

func newXMLNodeOptions(params map[string]any) xmlNodeOptions {
	options := xmlOptionsMap(params)
	headless := boolParam(options, "headless", false)
	return xmlNodeOptions{
		AttributePrefix:    firstNonEmptyNode(stringParam(options, "attributePrefix"), stringParam(options, "attrkey"), stringParam(params, "attributePrefix"), ""),
		TextNodeKey:        firstNonEmptyNode(stringParam(options, "textNodeKey"), stringParam(options, "charkey"), stringParam(params, "textNodeKey"), "_"),
		ForceArray:         boolParam(options, "forceArray", boolParam(options, "explicitArray", boolParam(params, "forceArray", false))),
		ParseNumbers:       boolParam(options, "parseNumbers", boolParam(params, "parseNumbers", false)),
		ParseBooleans:      boolParam(options, "parseBooleans", boolParam(params, "parseBooleans", false)),
		ExplicitRoot:       boolParam(options, "explicitRoot", boolParam(params, "explicitRoot", true)),
		RootName:           firstNonEmptyNode(stringParam(options, "rootName"), stringParam(params, "rootName"), "root"),
		XMLDeclaration:     boolParam(options, "xmlDeclaration", boolParam(params, "xmlDeclaration", !headless)),
		XMLVersion:         firstNonEmptyNode(stringParam(options, "xmlVersion"), stringParam(params, "xmlVersion"), "1.0"),
		XMLEncoding:        firstNonEmptyNode(stringParam(options, "xmlEncoding"), stringParam(params, "xmlEncoding"), "UTF-8"),
		AttributeChar:      firstNonEmptyNode(stringParam(options, "attributeChar"), stringParam(options, "attrkey"), stringParam(params, "attributeChar"), "$"),
		CDATAKey:           firstNonEmptyNode(stringParam(options, "cdataKey"), stringParam(params, "cdataKey"), "#cdata"),
		PreserveNamespaces: boolParam(options, "preserveNamespaces", boolParam(params, "preserveNamespaces", false)),
		IgnoreNamespaces:   boolParam(options, "ignoreNamespaces", boolParam(params, "ignoreNamespaces", false)),
		IgnoreAttrs:        boolParam(options, "ignoreAttrs", false),
		MergeAttrs:         boolParam(options, "mergeAttrs", true),
		NormalizeTags:      boolParam(options, "normalizeTags", false),
		Trim:               boolParam(options, "trim", boolParam(options, "normalize", false)),
	}
}

func xmlOptionsMap(params map[string]any) map[string]any {
	if options, ok := params["options"].(map[string]any); ok {
		return options
	}
	return map[string]any{}
}

func xmlToJSON(raw string, options xmlNodeOptions) (map[string]any, error) {
	decoder := xml.NewDecoder(strings.NewReader(raw))
	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				return nil, fmt.Errorf("empty XML document")
			}
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		value, err := parseXMLNodeElement(decoder, start, options)
		if err != nil {
			return nil, err
		}
		rootName := xmlElementName(start.Name, options)
		if options.ExplicitRoot {
			return map[string]any{rootName: value}, nil
		}
		if mapped, ok := value.(map[string]any); ok {
			return mapped, nil
		}
		return map[string]any{rootName: value}, nil
	}
}

func parseXMLNodeElement(decoder *xml.Decoder, start xml.StartElement, options xmlNodeOptions) (any, error) {
	result := map[string]any{}
	if !options.IgnoreAttrs {
		for _, attr := range start.Attr {
			name := xmlName(attr.Name, options)
			if options.NormalizeTags {
				name = strings.ToLower(name)
			}
			if options.MergeAttrs {
				result[name] = convertXMLTextValue(attr.Value, options)
			} else {
				result[options.AttributePrefix+name] = convertXMLTextValue(attr.Value, options)
			}
		}
	}
	var text strings.Builder
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			childName := xmlElementName(typed.Name, options)
			if options.NormalizeTags {
				childName = strings.ToLower(childName)
			}
			childValue, err := parseXMLNodeElement(decoder, typed, options)
			if err != nil {
				return nil, err
			}
			addXMLJSONValue(result, childName, childValue, options)
		case xml.CharData:
			text.Write([]byte(typed))
		case xml.EndElement:
			content := text.String()
			if options.Trim {
				content = strings.TrimSpace(content)
			}
			if len(result) == 0 {
				return convertXMLTextValue(content, options), nil
			}
			if content != "" {
				result[options.TextNodeKey] = convertXMLTextValue(content, options)
			}
			return result, nil
		}
	}
}

func addXMLJSONValue(result map[string]any, key string, value any, options xmlNodeOptions) {
	if existing, ok := result[key]; ok {
		if values, ok := existing.([]any); ok {
			result[key] = append(values, value)
		} else {
			result[key] = []any{existing, value}
		}
		return
	}
	if options.ForceArray {
		result[key] = []any{value}
	} else {
		result[key] = value
	}
}

func xmlElementName(name xml.Name, options xmlNodeOptions) string {
	return xmlName(name, options)
}

func xmlName(name xml.Name, options xmlNodeOptions) string {
	if options.IgnoreNamespaces || name.Space == "" {
		return name.Local
	}
	if options.PreserveNamespaces {
		return name.Space + ":" + name.Local
	}
	return name.Local
}

func convertXMLTextValue(value string, options xmlNodeOptions) any {
	if options.ParseBooleans {
		if strings.EqualFold(value, "true") {
			return true
		}
		if strings.EqualFold(value, "false") {
			return false
		}
	}
	if options.ParseNumbers {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			return parsed
		}
		if parsed, err := strconv.ParseFloat(value, 64); err == nil {
			return parsed
		}
	}
	return value
}

func jsonToXML(value any, options xmlNodeOptions) (string, error) {
	normalized, err := normalizeJSONLike(value)
	if err != nil {
		return "", err
	}
	var builder strings.Builder
	if options.XMLDeclaration {
		builder.WriteString(`<?xml version="`)
		builder.WriteString(options.XMLVersion)
		builder.WriteString(`" encoding="`)
		builder.WriteString(options.XMLEncoding)
		builder.WriteString(`"?>`)
		builder.WriteString("\n")
	}
	rootName := options.RootName
	if rootName == "" {
		rootName = "root"
	}
	if err := writeXMLElement(&builder, rootName, normalized, options, 0); err != nil {
		return "", err
	}
	return builder.String(), nil
}

func normalizeJSONLike(value any) (any, error) {
	switch typed := value.(type) {
	case string:
		var decoded any
		if err := json.Unmarshal([]byte(typed), &decoded); err == nil {
			return decoded, nil
		}
		return typed, nil
	case map[string]any, []any, nil, bool, float64, int, int64:
		return typed, nil
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return typed, nil
		}
		var decoded any
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			return typed, nil
		}
		return decoded, nil
	}
}

func writeXMLElement(builder *strings.Builder, name string, value any, options xmlNodeOptions, depth int) error {
	indent := strings.Repeat("  ", depth)
	switch typed := value.(type) {
	case map[string]any:
		return writeXMLMapElement(builder, name, typed, options, depth, indent)
	case []any:
		for _, item := range typed {
			if err := writeXMLElement(builder, name, item, options, depth); err != nil {
				return err
			}
		}
	case nil:
		builder.WriteString(indent)
		builder.WriteString("<")
		builder.WriteString(name)
		builder.WriteString("/>\n")
	default:
		builder.WriteString(indent)
		builder.WriteString("<")
		builder.WriteString(name)
		builder.WriteString(">")
		writeEscapedXMLText(builder, fmt.Sprint(typed))
		builder.WriteString("</")
		builder.WriteString(name)
		builder.WriteString(">\n")
	}
	return nil
}

func writeXMLMapElement(builder *strings.Builder, name string, data map[string]any, options xmlNodeOptions, depth int, indent string) error {
	attributes := map[string]string{}
	children := map[string]any{}
	var textValue any
	var cdataValue any
	for key, value := range data {
		switch {
		case strings.HasPrefix(key, options.AttributeChar):
			attributes[strings.TrimPrefix(key, options.AttributeChar)] = fmt.Sprint(value)
		case key == options.TextNodeKey || key == "#text":
			textValue = value
		case key == options.CDATAKey:
			cdataValue = value
		default:
			children[key] = value
		}
	}
	builder.WriteString(indent)
	builder.WriteString("<")
	builder.WriteString(name)
	for _, key := range sortedMapKeys(attributes) {
		builder.WriteString(" ")
		builder.WriteString(key)
		builder.WriteString(`="`)
		writeEscapedXMLText(builder, attributes[key])
		builder.WriteString(`"`)
	}
	if len(children) == 0 && textValue == nil && cdataValue == nil {
		builder.WriteString("/>\n")
		return nil
	}
	builder.WriteString(">")
	if textValue != nil {
		writeEscapedXMLText(builder, fmt.Sprint(textValue))
	}
	if cdataValue != nil {
		builder.WriteString("<![CDATA[")
		builder.WriteString(strings.ReplaceAll(fmt.Sprint(cdataValue), "]]>", "]]]]><![CDATA[>"))
		builder.WriteString("]]>")
	}
	if len(children) > 0 {
		builder.WriteString("\n")
		for _, key := range sortedAnyMapKeys(children) {
			if err := writeXMLElement(builder, key, children[key], options, depth+1); err != nil {
				return err
			}
		}
		builder.WriteString(indent)
	}
	builder.WriteString("</")
	builder.WriteString(name)
	builder.WriteString(">\n")
	return nil
}

func writeEscapedXMLText(builder *strings.Builder, value string) {
	_ = xml.EscapeText(builder, []byte(value))
}

func validateXMLString(raw string) (bool, []string) {
	decoder := xml.NewDecoder(strings.NewReader(raw))
	for {
		_, err := decoder.Token()
		if err == io.EOF {
			return true, []string{}
		}
		if err != nil {
			return false, []string{err.Error()}
		}
	}
}

func sortedMapKeys(input map[string]string) []string {
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedAnyMapKeys(input map[string]any) []string {
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
