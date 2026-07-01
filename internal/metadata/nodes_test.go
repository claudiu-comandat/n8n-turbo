package metadata

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAllKnownNodesUseOriginalDescriptionsAndExactFileIconURLs(t *testing.T) {
	t.Parallel()

	known := []string{
		"@n8n/n8n-nodes-langchain.agent",
		"@n8n/n8n-nodes-langchain.lmChatGoogleGemini",
		"n8n-nodes-base.aggregate",
		"n8n-nodes-base.airtable",
		"n8n-nodes-base.code",
		"n8n-nodes-base.compression",
		"n8n-nodes-base.convertToFile",
		"n8n-nodes-base.crypto",
		"n8n-nodes-base.dateTime",
		"n8n-nodes-base.discord",
		"n8n-nodes-base.editFields",
		"n8n-nodes-base.errorTrigger",
		"n8n-nodes-base.executeCommand",
		"n8n-nodes-base.executeWorkflow",
		"n8n-nodes-base.executeWorkflowTrigger",
		"n8n-nodes-base.extractFromFile",
		"n8n-nodes-base.filter",
		"n8n-nodes-base.formTrigger",
		"n8n-nodes-base.function",
		"n8n-nodes-base.functionItem",
		"n8n-nodes-base.github",
		"n8n-nodes-base.gmail",
		"n8n-nodes-base.googleSheets",
		"n8n-nodes-base.html",
		"n8n-nodes-base.httpRequest",
		"n8n-nodes-base.hubspot",
		"n8n-nodes-base.if",
		"n8n-nodes-base.jira",
		"n8n-nodes-base.limit",
		"n8n-nodes-base.loopOverItems",
		"n8n-nodes-base.manualTrigger",
		"n8n-nodes-base.markdown",
		"n8n-nodes-base.merge",
		"n8n-nodes-base.microsoftTeams",
		"n8n-nodes-base.mongoDb",
		"n8n-nodes-base.mySql",
		"n8n-nodes-base.noOp",
		"n8n-nodes-base.notion",
		"n8n-nodes-base.openAi",
		"n8n-nodes-base.postgres",
		"n8n-nodes-base.readWriteFile",
		"n8n-nodes-base.redis",
		"n8n-nodes-base.removeDuplicates",
		"n8n-nodes-base.respondToWebhook",
		"n8n-nodes-base.scheduleTrigger",
		"n8n-nodes-base.sendGrid",
		"n8n-nodes-base.set",
		"n8n-nodes-base.shopify",
		"n8n-nodes-base.slack",
		"n8n-nodes-base.sort",
		"n8n-nodes-base.splitInBatches",
		"n8n-nodes-base.splitOut",
		"n8n-nodes-base.start",
		"n8n-nodes-base.stickyNote",
		"n8n-nodes-base.stripe",
		"n8n-nodes-base.summarize",
		"n8n-nodes-base.switch",
		"n8n-nodes-base.telegram",
		"n8n-nodes-base.trello",
		"n8n-nodes-base.twilio",
		"n8n-nodes-base.wait",
		"n8n-nodes-base.webhook",
		"n8n-nodes-base.xml",
	}
	original := originalNodeDescriptions()
	for _, node := range NodeTypes(known) {
		raw, ok := original[node.Name]
		if !ok {
			t.Fatalf("%s is not backed by an original n8n node description", node.Name)
		}
		if node.Raw == nil {
			t.Fatalf("%s should expose original raw metadata, not generated fallback metadata", node.Name)
		}
		icon := fileIconName(raw["icon"])
		if icon == "" {
			continue
		}
		expected := builtinNodeIconURL(node.Name, "file:"+icon)
		if expected == "" {
			t.Errorf("%s uses original file icon %q but has no exact iconUrl mapping", node.Name, icon)
			continue
		}
		if got := raw["iconUrl"]; got != expected {
			t.Errorf("%s iconUrl = %#v, want exact original path %q", node.Name, got, expected)
		}
	}
}

func fileIconName(value any) string {
	switch typed := value.(type) {
	case string:
		if len(typed) > len("file:") && typed[:len("file:")] == "file:" {
			return typed[len("file:"):]
		}
		return ""
	case map[string]any:
		if light := fileIconName(typed["light"]); light != "" {
			return light
		}
		return fileIconName(typed["dark"])
	default:
		return ""
	}
}

func TestOriginalLightDarkFileIconsGetIconURL(t *testing.T) {
	t.Parallel()

	nodes := NodeTypes([]string{
		"n8n-nodes-base.convertToFile",
		"n8n-nodes-base.extractFromFile",
		"n8n-nodes-base.html",
		"n8n-nodes-base.httpRequest",
		"n8n-nodes-base.markdown",
		"n8n-nodes-base.webhook",
	})
	want := map[string]string{
		"n8n-nodes-base.convertToFile":   "icons/n8n-nodes-base/dist/nodes/Files/ConvertToFile/convertToFile.svg",
		"n8n-nodes-base.extractFromFile": "icons/n8n-nodes-base/dist/nodes/Files/ExtractFromFile/extractFromFile.svg",
		"n8n-nodes-base.html":            "icons/n8n-nodes-base/dist/nodes/Html/html.svg",
		"n8n-nodes-base.httpRequest":     "icons/n8n-nodes-base/dist/nodes/HttpRequest/httprequest.svg",
		"n8n-nodes-base.markdown":        "icons/n8n-nodes-base/dist/nodes/Markdown/markdown.svg",
		"n8n-nodes-base.webhook":         "icons/n8n-nodes-base/dist/nodes/Webhook/webhook.svg",
	}
	for _, node := range nodes {
		expected, ok := want[node.Name]
		if !ok {
			continue
		}
		if got := node.Raw["iconUrl"]; got != expected {
			t.Fatalf("%s iconUrl = %#v, want %q", node.Name, got, expected)
		}
	}
}

func TestOriginalFontAwesomeIconsDoNotGetFallbackIconURL(t *testing.T) {
	t.Parallel()

	node, ok := NodeTypeByName("@n8n/n8n-nodes-langchain.agent", nil)
	if !ok {
		t.Fatal("expected AI Agent node metadata")
	}
	if got := node.Raw["icon"]; got != "fa:robot" {
		t.Fatalf("agent icon = %#v, want original fa:robot", got)
	}
	if got, ok := node.Raw["iconUrl"]; ok {
		t.Fatalf("agent should use original FontAwesome icon without fallback iconUrl, got %#v", got)
	}
}

func TestExposedNodeMetadataStaysOriginalExceptMigrationCompat(t *testing.T) {
	t.Parallel()

	original := originalNodeDescriptions()
	for _, node := range NodeTypes(nil) {
		raw, ok := original[node.Name]
		if !ok {
			t.Fatalf("%s is exposed without original n8n metadata", node.Name)
		}
		if metadataCompatNode(node.Name) {
			continue
		}
		if !reflect.DeepEqual(node.Raw, raw) {
			t.Fatalf("%s metadata drifted from original n8n description", node.Name)
		}
	}
}

func metadataCompatNode(name string) bool {
	switch name {
	case "@n8n/n8n-nodes-langchain.agent",
		"n8n-nodes-base.code",
		"n8n-nodes-base.filter",
		"n8n-nodes-base.n8n",
		"n8n-nodes-base.webhook":
		return true
	default:
		return false
	}
}

func TestAIAgentExposesOriginalV2VersionsForMigratedWorkflows(t *testing.T) {
	t.Parallel()

	node, ok := NodeTypeByName("@n8n/n8n-nodes-langchain.agent", nil)
	if !ok {
		t.Fatal("expected AI Agent node metadata")
	}
	versions, ok := node.Raw["version"].([]any)
	if !ok {
		t.Fatalf("agent version should be a list, got %#v", node.Raw["version"])
	}
	want := map[float64]bool{2: true, 2.1: true, 2.2: true, 3: true, 3.1: true}
	for _, version := range versions {
		if number, ok := version.(float64); ok {
			delete(want, number)
		}
		if number, ok := version.(int); ok {
			delete(want, float64(number))
		}
	}
	if len(want) > 0 {
		t.Fatalf("agent metadata is missing migrated workflow versions: %#v", want)
	}
}

func TestWebhookOptionsExposeAllowedOriginsForCopyPasteParity(t *testing.T) {
	t.Parallel()

	node, ok := NodeTypeByName("n8n-nodes-base.webhook", nil)
	if !ok {
		t.Fatal("expected Webhook node metadata")
	}
	options := collectionOptions(node.Raw, "options")
	if len(options) == 0 {
		t.Fatal("expected Webhook options collection")
	}
	for _, option := range options {
		if option["name"] != "allowedOrigins" {
			continue
		}
		if option["default"] != "*" {
			t.Fatalf("allowedOrigins default = %#v, want *", option["default"])
		}
		return
	}
	t.Fatal("Webhook options should expose allowedOrigins like original n8n")
}

func TestCodeNodeExposesGoLanguageForTurboRunner(t *testing.T) {
	t.Parallel()

	node, ok := NodeTypeByName("n8n-nodes-base.code", nil)
	if !ok {
		t.Fatal("expected Code node metadata")
	}
	language := testRawProperty(node.Raw, "language", "")
	if language == nil {
		t.Fatal("Code node should expose language selector")
	}
	if !propertyHasOption(language, "go") {
		t.Fatal("Code node language selector should expose Go")
	}
	if testRawProperty(node.Raw, "goCode", "runOnceForAllItems") == nil {
		t.Fatal("Code node should expose Go editor in all-items mode")
	}
	if testRawProperty(node.Raw, "goCode", "runOnceForEachItem") == nil {
		t.Fatal("Code node should expose Go editor in per-item mode")
	}
}

func TestFilterExposesV1ConditionsForMigratedWorkflows(t *testing.T) {
	t.Parallel()

	node, ok := NodeTypeByName("n8n-nodes-base.filter", nil)
	if !ok {
		t.Fatal("expected Filter node metadata")
	}
	versions, ok := node.Raw["version"].([]any)
	if !ok {
		t.Fatalf("filter version should be a list, got %#v", node.Raw["version"])
	}
	hasV1 := false
	for _, version := range versions {
		if version == 1 {
			hasV1 = true
		}
	}
	if !hasV1 {
		t.Fatal("Filter metadata should include v1 for migrated workflows")
	}
	for _, option := range collectionOptions(node.Raw, "conditions") {
		if option["name"] == "string" {
			return
		}
	}
	t.Fatal("Filter v1 conditions should expose string conditions")
}

func TestUnknownKnownNodeIsNotExposedAsFallbackNode(t *testing.T) {
	t.Parallel()

	if _, ok := NodeTypeByName("n8n-nodes-base.notInOriginal", []string{"n8n-nodes-base.notInOriginal"}); ok {
		t.Fatal("unknown node should not be exposed without original n8n metadata")
	}
}

func TestN8nNodeUsesOriginalIconAndCredential(t *testing.T) {
	t.Parallel()

	node, ok := NodeTypeByName("n8n-nodes-base.n8n", nil)
	if !ok {
		t.Fatal("expected n8n node metadata")
	}
	if node.Icon != "file:n8n.svg" {
		t.Fatalf("n8n icon = %q, want original file:n8n.svg", node.Icon)
	}
	if node.IconURL != "icons/n8n-nodes-base/dist/nodes/N8n/n8n.svg" {
		t.Fatalf("n8n iconUrl = %q", node.IconURL)
	}
	if len(node.Credentials) != 1 || node.Credentials[0].Name != "n8nApi" || !node.Credentials[0].Required {
		t.Fatalf("unexpected n8n credentials: %#v", node.Credentials)
	}
}

func TestMigrationWorkflowTopLevelParametersExistInOriginalMetadata(t *testing.T) {
	t.Parallel()

	dir := filepath.FromSlash("C:/Users/titam/Documents/migrare_n8n")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("migration workflow folder is unavailable: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		var workflow struct {
			Nodes []struct {
				Name       string         `json:"name"`
				Type       string         `json:"type"`
				Parameters map[string]any `json:"parameters"`
			} `json:"nodes"`
		}
		if err := json.Unmarshal(raw, &workflow); err != nil {
			t.Fatalf("parse %s: %v", entry.Name(), err)
		}
		for _, node := range workflow.Nodes {
			props := originalTopLevelParameterNames(node.Type)
			if len(props) == 0 {
				t.Errorf("%s / %s uses %s, but no original metadata exists", entry.Name(), node.Name, node.Type)
				continue
			}
			for key := range node.Parameters {
				if props[key] || migrationInternalParameter(key) {
					continue
				}
				t.Errorf("%s / %s (%s) uses parameter %q that is not in original metadata", entry.Name(), node.Name, node.Type, key)
			}
		}
	}
}

func originalTopLevelParameterNames(nodeType string) map[string]bool {
	raw := originalNodeDescriptions()[nodeType]
	properties, ok := raw["properties"].([]any)
	if !ok {
		return nil
	}
	names := map[string]bool{}
	for _, entry := range properties {
		prop, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if name, ok := prop["name"].(string); ok {
			names[name] = true
		}
	}
	return names
}

func testRawProperty(raw map[string]any, name string, mode string) map[string]any {
	properties, ok := raw["properties"].([]any)
	if !ok {
		return nil
	}
	for _, entry := range properties {
		prop, ok := entry.(map[string]any)
		if !ok || prop["name"] != name {
			continue
		}
		if mode == "" || propertyShowsMode(prop, mode) {
			return prop
		}
	}
	return nil
}

func propertyShowsMode(prop map[string]any, mode string) bool {
	displayOptions, _ := prop["displayOptions"].(map[string]any)
	show, _ := displayOptions["show"].(map[string]any)
	modes, _ := show["mode"].([]any)
	for _, value := range modes {
		if value == mode {
			return true
		}
	}
	return false
}

func propertyHasOption(prop map[string]any, value string) bool {
	options, _ := prop["options"].([]any)
	for _, entry := range options {
		option, ok := entry.(map[string]any)
		if ok && option["value"] == value {
			return true
		}
	}
	return false
}

func collectionOptions(raw map[string]any, name string) []map[string]any {
	properties, ok := raw["properties"].([]any)
	if !ok {
		return nil
	}
	for _, entry := range properties {
		prop, ok := entry.(map[string]any)
		if !ok || prop["name"] != name {
			continue
		}
		rawOptions, _ := prop["options"].([]any)
		options := make([]map[string]any, 0, len(rawOptions))
		for _, rawOption := range rawOptions {
			option, ok := rawOption.(map[string]any)
			if ok {
				options = append(options, option)
			}
		}
		return options
	}
	return nil
}

func migrationInternalParameter(key string) bool {
	switch key {
	case "height", "width", "requestOptions":
		return true
	default:
		return false
	}
}
