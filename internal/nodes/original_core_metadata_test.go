package nodes

import (
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/metadata"
)

func TestCoreRuntimeOptionsMatchOriginalMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		nodeType string
		property string
		want     []string
	}{
		{"n8n-nodes-base.compression", "operation", []string{"compress", "decompress"}},
		{"n8n-nodes-base.convertToFile", "operation", []string{"csv", "html", "iCal", "ods", "rtf", "toBinary", "toJson", "toText", "xls", "xlsx"}},
		{"n8n-nodes-base.crypto", "action", []string{"generate", "hash", "hmac", "sign"}},
		{"n8n-nodes-base.dateTime", "operation", []string{"addToDate", "extractDate", "formatDate", "getCurrentDate", "getTimeBetweenDates", "roundDate", "subtractFromDate"}},
		{"n8n-nodes-base.extractFromFile", "operation", []string{"binaryToPropery", "csv", "fromIcs", "fromJson", "html", "ods", "pdf", "rtf", "text", "xls", "xlsx", "xml"}},
		{"n8n-nodes-base.html", "operation", []string{"convertToHtmlTable", "extractHtmlContent", "generateHtmlTemplate"}},
		{"n8n-nodes-base.markdown", "mode", []string{"htmlToMarkdown", "markdownToHtml"}},
		{"n8n-nodes-base.merge", "mode", []string{"append", "chooseBranch", "combine", "combineBySql"}},
		{"n8n-nodes-base.readWriteFile", "operation", []string{"read", "write"}},
		{"n8n-nodes-base.removeDuplicates", "operation", []string{"clearDeduplicationHistory", "removeDuplicateInputItems", "removeItemsSeenInPreviousExecutions"}},
		{"n8n-nodes-base.sort", "type", []string{"code", "random", "simple"}},
		{"n8n-nodes-base.switch", "mode", []string{"expression", "rules"}},
		{"n8n-nodes-base.xml", "mode", []string{"jsonToxml", "xmlToJson"}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.nodeType+"/"+test.property, func(t *testing.T) {
			t.Parallel()
			got := originalCoreOptionValues(t, test.nodeType, test.property)
			sort.Strings(test.want)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("original options changed: got %v want %v", got, test.want)
			}
		})
	}
}

func originalCoreOptionValues(t *testing.T, nodeType string, propertyName string) []string {
	t.Helper()
	node, ok := metadata.NodeTypeByName(nodeType, []string{nodeType})
	if !ok {
		t.Fatalf("%s metadata not found", nodeType)
	}
	properties, ok := node.Raw["properties"].([]any)
	if !ok {
		t.Fatalf("%s metadata properties not found", nodeType)
	}
	for _, rawProperty := range properties {
		property, ok := rawProperty.(map[string]any)
		if !ok || property["name"] != propertyName {
			continue
		}
		options, ok := property["options"].([]any)
		if !ok {
			t.Fatalf("%s.%s has no options", nodeType, propertyName)
		}
		values := make([]string, 0, len(options))
		for _, rawOption := range options {
			option, ok := rawOption.(map[string]any)
			if !ok {
				continue
			}
			values = append(values, fmt.Sprint(option["value"]))
		}
		sort.Strings(values)
		return values
	}
	t.Fatalf("%s.%s not found", nodeType, propertyName)
	return nil
}
