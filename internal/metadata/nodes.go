package metadata

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"

	"github.com/n8n-io/n8n-turbo/internal/descriptor"
)

func NodeTypes(known []string) []NodeType {
	byName := map[string]NodeType{}
	for _, node := range builtinNodeTypes() {
		byName[node.Name] = node
	}
	for _, desc := range descriptor.Builtins() {
		byName[desc.NodeType] = descriptorNodeType(desc)
	}
	original := originalNodeDescriptions()
	for _, name := range known {
		if _, ok := byName[name]; ok {
			continue
		}
		if _, ok := original[name]; ok {
			byName[name] = genericNodeType(name)
		}
	}
	applyOriginalNodeDescriptions(byName)
	result := make([]NodeType, 0, len(byName))
	for _, node := range byName {
		result = append(result, node)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func NodeTypeByName(name string, known []string) (NodeType, bool) {
	for _, node := range NodeTypes(known) {
		if node.Name == name {
			return node, true
		}
	}
	return NodeType{}, false
}

func applyOriginalNodeDescriptions(byName map[string]NodeType) {
	for name, raw := range originalNodeDescriptions() {
		node, ok := byName[name]
		if !ok {
			continue
		}
		raw = cloneRawMap(raw)
		applyIconURL(raw, name)
		applyNodeMetadataCompat(raw, name)
		node.Raw = raw
		byName[name] = node
	}
}

func applyNodeMetadataCompat(raw map[string]any, name string) {
	if name == "n8n-nodes-base.n8n" {
		ensureProperty(raw, map[string]any{
			"displayName": "Request Options",
			"name":        "requestOptions",
			"type":        "collection",
			"default":     map[string]any{},
			"placeholder": "Add option",
			"options":     []any{},
		})
	}
	if name == "n8n-nodes-base.webhook" {
		ensureCollectionOption(raw, "options", map[string]any{
			"displayName": "Allowed Origins (CORS)",
			"name":        "allowedOrigins",
			"type":        "string",
			"default":     "*",
			"description": "Comma-separated list of URLs allowed for cross-origin non-preflight requests. Use * (default) to allow all origins.",
		})
	}
	if name == "n8n-nodes-base.extractFromFile" {
		ensureCollectionOption(raw, "options", map[string]any{
			"displayName": "Keep Source",
			"name":        "keepSource",
			"type":        "options",
			"default":     "json",
			"description": "Whether to keep source data from the incoming item on extracted rows.",
			"options": []any{
				map[string]any{"name": "JSON", "value": "json", "description": "Keep incoming JSON fields and remove the processed binary field"},
				map[string]any{"name": "Binary", "value": "binary", "description": "Keep only the source binary data"},
				map[string]any{"name": "Both", "value": "both", "description": "Keep both incoming JSON fields and source binary data"},
				map[string]any{"name": "None", "value": "none", "description": "Keep only extracted data"},
			},
		})
	}
	if name == "n8n-nodes-base.code" {
		ensureCodeGoSupport(raw)
	}
	if name == "n8n-nodes-base.filter" {
		raw["version"] = []any{1, 2, 2.1, 2.2, 2.3}
		showPropertyForVersion(raw, "conditions", "filter", map[string]any{"@version": []any{map[string]any{"_cnd": map[string]any{"gte": 2}}}})
		showPropertyForVersion(raw, "options", "collection", map[string]any{"@version": []any{map[string]any{"_cnd": map[string]any{"gte": 2}}}})
		ensureVersionedProperty(raw, filterV1ConditionsProperty())
		ensureVersionedProperty(raw, filterV1CombineConditionsProperty())
	}
	if name == "@n8n/n8n-nodes-langchain.agent" {
		raw["version"] = []any{2, 2.1, 2.2, 3, 3.1}
	}
}

func ensureCodeGoSupport(raw map[string]any) {
	raw["description"] = "Runs JavaScript, Python, or Go code"
	ensurePropertyOption(raw, "language", map[string]any{"name": "Go", "value": "go"})
	for _, property := range codeNodeProps() {
		if property.Name != "goCode" && !propertyShowsLanguage(property, "go") {
			continue
		}
		ensureRawProperty(raw, rawProperty(property))
	}
}

func propertyShowsLanguage(property Property, language string) bool {
	show, _ := property.DisplayOptions["show"].(map[string][]any)
	for _, value := range show["language"] {
		if value == language {
			return true
		}
	}
	return false
}

func ensureProperty(raw map[string]any, property map[string]any) {
	name, _ := property["name"].(string)
	if name == "" {
		return
	}
	properties, _ := raw["properties"].([]any)
	for _, entry := range properties {
		object, ok := entry.(map[string]any)
		if ok && object["name"] == name {
			return
		}
	}
	raw["properties"] = append(properties, property)
}

func ensureRawProperty(raw map[string]any, property map[string]any) {
	name, _ := property["name"].(string)
	propertyType, _ := property["type"].(string)
	if name == "" || propertyType == "" {
		return
	}
	properties, _ := raw["properties"].([]any)
	for _, entry := range properties {
		existing, ok := entry.(map[string]any)
		if !ok || existing["name"] != name || existing["type"] != propertyType {
			continue
		}
		if reflect.DeepEqual(existing["displayOptions"], property["displayOptions"]) {
			return
		}
	}
	raw["properties"] = append(properties, property)
}

func ensurePropertyOption(raw map[string]any, propertyName string, option map[string]any) {
	optionValue := option["value"]
	properties, _ := raw["properties"].([]any)
	for _, entry := range properties {
		property, ok := entry.(map[string]any)
		if !ok || property["name"] != propertyName {
			continue
		}
		options, _ := property["options"].([]any)
		for _, entry := range options {
			existing, ok := entry.(map[string]any)
			if ok && existing["value"] == optionValue {
				return
			}
		}
		property["options"] = append(options, option)
		return
	}
}

func rawProperty(property Property) map[string]any {
	bytes, err := json.Marshal(property)
	if err != nil {
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal(bytes, &raw); err != nil {
		return nil
	}
	return raw
}

func showPropertyForVersion(raw map[string]any, name string, propertyType string, show map[string]any) {
	properties, _ := raw["properties"].([]any)
	for _, entry := range properties {
		property, ok := entry.(map[string]any)
		if !ok || property["name"] != name || property["type"] != propertyType {
			continue
		}
		if _, exists := property["displayOptions"]; exists {
			return
		}
		property["displayOptions"] = map[string]any{"show": show}
		return
	}
}

func ensureVersionedProperty(raw map[string]any, property map[string]any) {
	name, _ := property["name"].(string)
	propertyType, _ := property["type"].(string)
	if name == "" || propertyType == "" {
		return
	}
	properties, _ := raw["properties"].([]any)
	for _, entry := range properties {
		existing, ok := entry.(map[string]any)
		if ok && existing["name"] == name && existing["type"] == propertyType {
			return
		}
	}
	raw["properties"] = append([]any{property}, properties...)
}

func ensureCollectionOption(raw map[string]any, collectionName string, option map[string]any) {
	optionName, _ := option["name"].(string)
	if optionName == "" {
		return
	}
	properties, _ := raw["properties"].([]any)
	for _, entry := range properties {
		collection, ok := entry.(map[string]any)
		if !ok || collection["name"] != collectionName {
			continue
		}
		options, _ := collection["options"].([]any)
		for _, existing := range options {
			existingOption, ok := existing.(map[string]any)
			if ok && existingOption["name"] == optionName {
				return
			}
		}
		collection["options"] = append([]any{option}, options...)
		return
	}
}

func filterV1ConditionsProperty() map[string]any {
	return map[string]any{
		"displayName": "Conditions",
		"name":        "conditions",
		"placeholder": "Add Condition",
		"type":        "fixedCollection",
		"typeOptions": map[string]any{"multipleValues": true, "sortable": true},
		"description": "The type of values to compare",
		"default":     map[string]any{},
		"displayOptions": map[string]any{
			"show": map[string]any{"@version": []any{1}},
		},
		"options": []any{
			map[string]any{
				"name":        "string",
				"displayName": "String",
				"values": []any{
					map[string]any{"displayName": "Value 1", "name": "value1", "type": "string", "default": "", "description": "The value to compare with the second one"},
					map[string]any{
						"displayName":      "Operation",
						"name":             "operation",
						"type":             "options",
						"noDataExpression": true,
						"default":          "equal",
						"description":      "Operation to decide where the data should be mapped to",
						"options": []any{
							map[string]any{"name": "Contains", "value": "contains"},
							map[string]any{"name": "Not Contains", "value": "notContains"},
							map[string]any{"name": "Ends With", "value": "endsWith"},
							map[string]any{"name": "Not Ends With", "value": "notEndsWith"},
							map[string]any{"name": "Equal", "value": "equal"},
							map[string]any{"name": "Not Equal", "value": "notEqual"},
							map[string]any{"name": "Regex Match", "value": "regex"},
							map[string]any{"name": "Regex Not Match", "value": "notRegex"},
							map[string]any{"name": "Starts With", "value": "startsWith"},
							map[string]any{"name": "Not Starts With", "value": "notStartsWith"},
							map[string]any{"name": "Is Empty", "value": "isEmpty"},
							map[string]any{"name": "Is Not Empty", "value": "isNotEmpty"},
						},
					},
					map[string]any{
						"displayName":    "Value 2",
						"name":           "value2",
						"type":           "string",
						"default":        "",
						"description":    "The value to compare with the first one",
						"displayOptions": map[string]any{"hide": map[string]any{"operation": []any{"isEmpty", "isNotEmpty", "regex", "notRegex"}}},
					},
					map[string]any{
						"displayName":    "Regex",
						"name":           "value2",
						"type":           "string",
						"default":        "",
						"placeholder":    "/text/i",
						"description":    "The regex which has to match",
						"displayOptions": map[string]any{"show": map[string]any{"operation": []any{"regex", "notRegex"}}},
					},
				},
			},
		},
	}
}

func filterV1CombineConditionsProperty() map[string]any {
	return map[string]any{
		"displayName": "Combine Conditions",
		"name":        "combineConditions",
		"type":        "options",
		"default":     "AND",
		"description": "How to combine the conditions: AND requires all conditions to be true, OR requires at least one condition to be true",
		"displayOptions": map[string]any{
			"show": map[string]any{"@version": []any{1}},
		},
		"options": []any{
			map[string]any{"name": "AND", "description": "Items are passed to the next node only if they meet all the conditions", "value": "AND"},
			map[string]any{"name": "OR", "description": "Items are passed to the next node if they meet at least one condition", "value": "OR"},
		},
	}
}

func cloneRawMap(raw map[string]any) map[string]any {
	cloned := make(map[string]any, len(raw))
	for key, value := range raw {
		cloned[key] = cloneRawValue(value)
	}
	return cloned
}

func cloneRawValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneRawMap(typed)
	case []any:
		cloned := make([]any, len(typed))
		for index, entry := range typed {
			cloned[index] = cloneRawValue(entry)
		}
		return cloned
	default:
		return value
	}
}

func applyIconURL(raw map[string]any, nodeName string) {
	if iconURL, ok := raw["iconUrl"].(string); ok {
		raw["iconUrl"] = strings.TrimPrefix(iconURL, "/")
		return
	}
	icon := iconFileName(raw["icon"])
	if icon == "" {
		return
	}
	raw["iconUrl"] = nodeIconURL(nodeName, icon)
}

func iconFileName(value any) string {
	switch typed := value.(type) {
	case string:
		if !strings.HasPrefix(typed, "file:") {
			return ""
		}
		return strings.TrimPrefix(typed, "file:")
	case map[string]any:
		if light := iconFileName(typed["light"]); light != "" {
			return light
		}
		return iconFileName(typed["dark"])
	default:
		return ""
	}
}

func nodeIconURL(nodeName string, icon string) string {
	return builtinNodeIconURL(nodeName, "file:"+icon)
}

func builtinNodeTypes() []NodeType {
	return []NodeType{
		trigger("n8n-nodes-base.manualTrigger", "Manual Trigger", "Runs the workflow manually", "manualTrigger"),
		trigger("n8n-nodes-base.start", "Start", "Legacy manual start node", "manualTrigger"),
		stickyNoteNode(),
		trigger("n8n-nodes-base.webhook", "Webhook", "Starts the workflow from an HTTP request", "webhook",
			option("HTTP Method", "httpMethod", "options", "GET", options("GET", "POST", "PUT", "PATCH", "DELETE", "HEAD")),
			textPlaceholder("Path", "path", "", "webhook"),
			selectProp("Authentication", "authentication", "none", []Option{
				{Name: "Basic Auth", Value: "basicAuth", Description: "Require HTTP basic auth"},
				{Name: "Header Auth", Value: "headerAuth", Description: "Require a matching HTTP header"},
				{Name: "JWT Auth", Value: "jwtAuth", Description: "Require a valid JWT"},
				{Name: "None", Value: "none", Description: "Do not require authentication"},
			}),
			selectProp("Respond", "responseMode", "onReceived", []Option{
				{Name: "Immediately", Value: "onReceived", Description: "As soon as this node executes"},
				{Name: "When Last Node Finishes", Value: "lastNode", Description: "Returns data of the last-executed node"},
				{Name: "Using 'Respond to Webhook' Node", Value: "responseNode", Description: "Response defined in that node"},
				{Name: "Streaming", Value: "streaming", Description: "Return data in real time from streaming enabled nodes"},
			}),
			selectProp("Response Data", "responseData", "firstEntryJson", []Option{
				{Name: "All Entries", Value: "allEntries", Description: "Returns all the entries of the last node. Always returns an array."},
				{Name: "First Entry JSON", Value: "firstEntryJson", Description: "Returns the JSON data of the first entry of the last node. Always returns a JSON object."},
				{Name: "First Entry Binary", Value: "firstEntryBinary", Description: "Returns the binary data of the first entry of the last node. Always returns a binary file."},
				{Name: "No Response Body", Value: "noData", Description: "Returns without a body"},
			}),
			webhookOptionsCollection()).
			withVersions(1, 1.1, 2, 2.1).
			withWebhookContract().
			withCredentialDisplay("httpBasicAuth", false, "authentication", "basicAuth").
			withCredentialDisplay("httpHeaderAuth", false, "authentication", "headerAuth").
			withCredentialDisplay("jwtAuth", false, "authentication", "jwtAuth"),
		trigger("n8n-nodes-base.formTrigger", "Form Trigger", "Starts the workflow when a form is submitted", "form",
			text("Path", "path", ""),
			text("Form Title", "formTitle", "Form"),
			textArea("Form Description", "formDescription", ""),
			fixedCollection("Form Elements", "formFields", []Property{text("Field Name", "fieldName", ""), text("Label", "fieldLabel", ""), selectProp("Type", "fieldType", "text", options("text", "email", "number", "textarea", "dropdown", "checkbox", "radio", "file"))})).withCredential("httpBasicAuth", false),
		trigger("n8n-nodes-base.errorTrigger", "Error Trigger", "Starts an error workflow after another workflow fails", "errorTrigger"),
		trigger("n8n-nodes-base.executeWorkflowTrigger", "Execute Workflow Trigger", "Starts when called by another workflow", "sub-workflow-trigger",
			selectProp("Input Data Mode", "inputSource", "passthrough", []Option{{Name: "Accept All Data", Value: "passthrough"}, {Name: "Define Using Fields Below", Value: "workflowInputs"}, {Name: "Define Using JSON Example", Value: "jsonExample"}}),
			fixedCollection("Workflow Input Schema", "workflowInputs", []Property{text("Name", "name", ""), selectProp("Type", "type", "string", options("string", "number", "boolean", "object", "array"))})),
		trigger("n8n-nodes-base.scheduleTrigger", "Schedule Trigger", "Starts the workflow on a schedule", "scheduleTrigger",
			fixedCollection("Interval", "interval", []Property{
				selectProp("Field", "field", "seconds", options("seconds", "minutes", "hours", "days", "weeks")),
				numberProp("Seconds Interval", "secondsInterval", 60),
				numberProp("Minutes Interval", "minutesInterval", 1),
				numberProp("Hours Interval", "hoursInterval", 1),
			})),
		action("n8n-nodes-base.respondToWebhook", "Respond to Webhook", "Returns a response for a webhook execution", "webhook",
			respondToWebhookProps()...).
			withVersions(1, 1.4).
			withCredentialDisplay("jwtAuth", true, "respondWith", "jwt"),
		action("n8n-nodes-base.noOp", "No Operation", "Passes input data through unchanged", "transform"),
		action("n8n-nodes-base.set", "Set", "Adds or edits item fields", "transform",
			selectProp("Mode", "mode", "manual", []Option{{Name: "Manual", Value: "manual"}, {Name: "JSON", Value: "json"}}),
			option("Include Other Fields", "includeOtherFields", "boolean", true, nil),
			fixedCollection("Fields", "fields", []Property{text("Name", "name", ""), selectProp("Type", "type", "string", options("string", "number", "boolean", "array", "object")), text("Value", "value", "")}),
			fixedCollection("Assignments", "assignments", []Property{text("Name", "name", ""), selectProp("Type", "type", "string", options("string", "number", "boolean", "array", "object")), text("Value", "value", "")}),
			jsonProp("JSON Output", "jsonOutput", "{}")).withVersions(1, 2, 3, 3.1, 3.2, 3.3, 3.4),
		action("n8n-nodes-base.editFields", "Edit Fields", "Adds or edits item fields", "transform",
			selectProp("Mode", "mode", "manual", []Option{{Name: "Manual", Value: "manual"}, {Name: "JSON", Value: "json"}}),
			option("Include Other Fields", "includeOtherFields", "boolean", true, nil),
			fixedCollection("Fields", "fields", []Property{text("Name", "name", ""), selectProp("Type", "type", "string", options("string", "number", "boolean", "array", "object")), text("Value", "value", "")}),
			fixedCollection("Assignments", "assignments", []Property{text("Name", "name", ""), selectProp("Type", "type", "string", options("string", "number", "boolean", "array", "object")), text("Value", "value", "")}),
			jsonProp("JSON Output", "jsonOutput", "{}")),
		action("n8n-nodes-base.if", "If", "Routes items to true or false outputs", "transform",
			conditionProps()...).withVersions(1, 2, 2.1, 2.2).withOutputs("main", "main"),
		action("n8n-nodes-base.switch", "Switch", "Routes items to multiple outputs", "transform",
			selectProp("Mode", "mode", "rules", []Option{{Name: "Rules", Value: "rules"}, {Name: "Expression", Value: "expression"}}),
			selectProp("Data Type", "dataType", "number", []Option{{Name: "Number", Value: "number"}, {Name: "String", Value: "string"}, {Name: "Boolean", Value: "boolean"}}),
			selectProp("Output", "output", "single", []Option{{Name: "Single", Value: "single"}, {Name: "Multiple", Value: "multiple"}}),
			text("Value", "value", "={{ $json }}"),
			numberProp("Fallback Output", "fallbackOutput", 0),
			fixedCollection("Rules", "rules", []Property{text("Value 1", "value1", "={{ $json.value }}"), text("Value 2", "value2", ""), selectProp("Operation", "operation", "equal", []Option{{Name: "Equals", Value: "equal"}, {Name: "Not Equal", Value: "notEqual"}, {Name: "Contains", Value: "contains"}, {Name: "Not Contains", Value: "notContains"}, {Name: "Starts With", Value: "startsWith"}, {Name: "Ends With", Value: "endsWith"}, {Name: "Matches Regex", Value: "matchesRegex"}, {Name: "Larger", Value: "larger"}, {Name: "Larger Or Equal", Value: "largerEqual"}, {Name: "Smaller", Value: "smaller"}, {Name: "Smaller Or Equal", Value: "smallerEqual"}, {Name: "Exists", Value: "exists"}, {Name: "Is Empty", Value: "isEmpty"}, {Name: "Date After", Value: "dateAfter"}, {Name: "Date Before", Value: "dateBefore"}}), numberProp("Output Index", "outputIndex", 0), option("Case Sensitive", "caseSensitive", "boolean", false, nil)})),
		action("n8n-nodes-base.filter", "Filter", "Keeps only matching items", "transform", conditionProps()...),
		action("n8n-nodes-base.merge", "Merge", "Merges items from multiple branches", "transform",
			selectProp("Mode", "mode", "append", []Option{{Name: "Append", Value: "append"}, {Name: "Combine By Position", Value: "combineByPosition"}, {Name: "Combine By Fields", Value: "combineByFields"}, {Name: "Choose Branch", Value: "chooseBranch"}, {Name: "Multiplex", Value: "multiplex"}, {Name: "Pass Through", Value: "passThrough"}}),
			selectProp("Join Mode", "joinMode", "keepMatches", []Option{{Name: "Keep Matches", Value: "keepMatches"}, {Name: "Keep Non Matches", Value: "keepNonMatches"}, {Name: "Enrich Input 1", Value: "enrichInput1"}, {Name: "Enrich Input 2", Value: "enrichInput2"}}),
			fixedCollection("Fields To Match", "fieldsToMatch", []Property{text("Input 1 Field", "field1", "id"), text("Input 2 Field", "field2", "id")}),
			numberProp("Choose Branch Input", "chooseBranchInput", 0),
			option("Include Unpaired", "includeUnpaired", "boolean", false, nil),
			selectProp("Multiple Matches", "multipleMatches", "all", []Option{{Name: "All", Value: "all"}, {Name: "First", Value: "first"}})),
		action("n8n-nodes-base.limit", "Limit", "Limits the number of items", "transform",
			numberProp("Max Items", "maxItems", 1),
			selectProp("Keep", "keep", "firstItems", []Option{{Name: "First Items", Value: "firstItems"}, {Name: "Last Items", Value: "lastItems"}})),
		action("n8n-nodes-base.splitInBatches", "Split In Batches", "Splits items into batches", "transform", loopBatchProps()...).withVersions(1, 2, 3).withOutputs("main", "main"),
		action("n8n-nodes-base.loopOverItems", "Loop Over Items", "Processes items in batches", "transform", loopBatchProps()...).withVersions(1, 2, 3).withOutputs("main", "main"),
		action("n8n-nodes-base.wait", "Wait", "Pauses execution", "transform",
			selectProp("Resume", "resume", "timeInterval", []Option{{Name: "After Time Interval", Value: "timeInterval"}, {Name: "At Specified Time", Value: "specificTime"}, {Name: "Webhook Call", Value: "webhook"}, {Name: "Form Submitted", Value: "form"}}),
			numberProp("Amount", "amount", 1),
			selectProp("Unit", "unit", "seconds", options("milliseconds", "seconds", "minutes", "hours", "days", "weeks")),
			text("Date Time", "dateTime", ""),
			text("Webhook Suffix", "webhookSuffix", ""),
			selectProp("HTTP Method", "httpMethod", "POST", options("GET", "POST", "PUT", "PATCH", "DELETE")),
			option("Limit Wait Time", "limitWaitTime", "boolean", false, nil),
			numberProp("Limit Amount", "limitAmount", 1),
			selectProp("Limit Unit", "limitUnit", "hours", options("seconds", "minutes", "hours", "days", "weeks")),
			fixedCollection("Form Fields", "formFields", []Property{text("Field Name", "fieldName", ""), text("Label", "fieldLabel", ""), selectProp("Type", "fieldType", "text", options("text", "email", "number", "textarea", "dropdown", "checkbox", "radio", "file"))})),
		action("n8n-nodes-base.sort", "Sort", "Sorts items", "transform",
			selectProp("Type", "type", "simple", []Option{{Name: "Simple", Value: "simple"}, {Name: "Random", Value: "random"}, {Name: "Expression", Value: "expression"}}),
			text("Field", "field", ""),
			fixedCollection("Sort Fields", "sortFieldsUi", []Property{
				text("Field Name", "fieldName", ""),
				selectProp("Order", "order", "ascending", []Option{{Name: "Ascending", Value: "ascending"}, {Name: "Descending", Value: "descending"}}),
			}),
			fixedCollection("Options", "options", []Property{
				option("Case Sensitive", "caseSensitive", "boolean", false, nil),
				selectProp("Nulls Position", "nullsPosition", "last", []Option{{Name: "First", Value: "first"}, {Name: "Last", Value: "last"}}),
				option("Numeric Sort", "numericSort", "boolean", false, nil),
				text("Locale Compare", "localeCompare", ""),
				option("Stable Sort", "stableSort", "boolean", true, nil),
				option("Disable Dot Notation", "disableDotNotation", "boolean", false, nil),
			})),
		action("n8n-nodes-base.removeDuplicates", "Remove Duplicates", "Removes duplicate items", "transform",
			selectProp("Compare", "compare", "all-fields", []Option{{Name: "All Fields", Value: "all-fields"}, {Name: "Selected Fields", Value: "selected-fields"}}),
			text("Fields To Compare", "fieldsToCompare", ""),
			text("Disabled Fields", "disabledFields", ""),
			selectProp("Keep Mode", "keepMode", "first", []Option{{Name: "First", Value: "first"}, {Name: "Last", Value: "last"}, {Name: "All If Different", Value: "all-if-different"}}),
			option("Case Sensitive", "caseSensitive", "boolean", true, nil),
			option("Remove Blank Values", "removeBlankValues", "boolean", false, nil),
			option("Fuzzy Matching", "fuzzyMatching", "boolean", false, nil),
			numberProp("Fuzzy Threshold", "fuzzyThreshold", 0.8),
			option("Sort Before Dedup", "sortBeforeDedup", "boolean", false, nil)),
		action("n8n-nodes-base.splitOut", "Split Out", "Splits arrays into separate items", "transform",
			text("Field To Split Out", "fieldToSplitOut", ""),
			selectProp("Include", "include", "noOtherFields", []Option{{Name: "No Other Fields", Value: "noOtherFields"}, {Name: "Selected Other Fields", Value: "selectedOtherFields"}, {Name: "All Other Fields", Value: "allOtherFields"}}),
			text("Fields To Include", "fieldsToInclude", ""),
			text("Destination Field Name", "destinationFieldName", "")),
		action("n8n-nodes-base.aggregate", "Aggregate", "Aggregates items", "transform", aggregateProps()...),
		action("n8n-nodes-base.summarize", "Summarize", "Summarizes items", "transform", summarizeProps()...),
		action("n8n-nodes-base.dateTime", "Date & Time", "Transforms date and time values", "transform",
			selectProp("Action", "action", "format", []Option{{Name: "Format", Value: "format"}, {Name: "Get", Value: "get"}, {Name: "Calculate", Value: "calculate"}, {Name: "Round", Value: "round"}, {Name: "Convert", Value: "convert"}, {Name: "Now", Value: "now"}}),
			text("Value", "value", "={{ $json.date }}"),
			text("Output Field Name", "outputFieldName", "outputDate"),
			option("Include Input", "includeInput", "boolean", true, nil),
			text("Format String", "formatString", "yyyy-MM-dd'T'HH:mm:ssZZZ"),
			selectProp("Get Part", "getPart", "year", []Option{{Name: "Year", Value: "year"}, {Name: "Month", Value: "month"}, {Name: "Day", Value: "day"}, {Name: "Hour", Value: "hour"}, {Name: "Minute", Value: "minute"}, {Name: "Second", Value: "second"}, {Name: "Millisecond", Value: "millisecond"}, {Name: "Weekday", Value: "weekday"}, {Name: "Weekday Name", Value: "weekdayName"}, {Name: "Day Of Year", Value: "dayOfYear"}, {Name: "Week", Value: "week"}, {Name: "ISO Week Year", Value: "isoWeekYear"}, {Name: "Quarter", Value: "quarter"}, {Name: "Timestamp", Value: "timestamp"}, {Name: "Timestamp Ms", Value: "timestampMs"}}),
			selectProp("Calculation Operation", "calculationOperation", "add", []Option{{Name: "Add", Value: "add"}, {Name: "Subtract", Value: "subtract"}}),
			numberProp("Duration", "duration", 1),
			selectProp("Unit", "unit", "day", []Option{{Name: "Year", Value: "year"}, {Name: "Month", Value: "month"}, {Name: "Week", Value: "week"}, {Name: "Day", Value: "day"}, {Name: "Hour", Value: "hour"}, {Name: "Minute", Value: "minute"}, {Name: "Second", Value: "second"}, {Name: "Millisecond", Value: "millisecond"}}),
			selectProp("Round To", "roundTo", "day", []Option{{Name: "Year", Value: "year"}, {Name: "Month", Value: "month"}, {Name: "Week", Value: "week"}, {Name: "Day", Value: "day"}, {Name: "Hour", Value: "hour"}, {Name: "Minute", Value: "minute"}, {Name: "Second", Value: "second"}}),
			text("From Timezone", "fromTimezone", ""),
			text("To Timezone", "toTimezone", "UTC"),
			fixedCollection("Options", "options", []Property{
				text("Timezone", "timezone", "UTC"),
				option("ISO", "iso", "boolean", false, nil),
			})),
		action("n8n-nodes-base.crypto", "Crypto", "Hashes and signs values", "transform",
			selectProp("Action", "action", "hash", []Option{{Name: "Hash", Value: "hash"}, {Name: "HMAC", Value: "hmac"}, {Name: "Sign", Value: "sign"}, {Name: "Verify", Value: "verify"}, {Name: "Generate Key Pair", Value: "generateKeyPair"}, {Name: "Encrypt", Value: "encrypt"}, {Name: "Decrypt", Value: "decrypt"}}),
			text("Value", "value", "={{ $json.value }}"),
			selectProp("Algorithm", "algorithm", "SHA256", []Option{{Name: "MD5", Value: "MD5"}, {Name: "SHA1", Value: "SHA1"}, {Name: "SHA224", Value: "SHA224"}, {Name: "SHA256", Value: "SHA256"}, {Name: "SHA384", Value: "SHA384"}, {Name: "SHA512", Value: "SHA512"}}),
			selectProp("Encoding", "encoding", "hex", []Option{{Name: "Hex", Value: "hex"}, {Name: "Base64", Value: "base64"}, {Name: "Latin1", Value: "latin1"}}),
			text("Secret Key", "secretKey", ""),
			text("Private Key", "privateKey", ""),
			text("Public Key", "publicKey", ""),
			text("Signature", "signature", ""),
			selectProp("Signature Encoding", "signatureEncoding", "hex", []Option{{Name: "Hex", Value: "hex"}, {Name: "Base64", Value: "base64"}}),
			selectProp("Key Type", "keyType", "RSA", []Option{{Name: "RSA", Value: "RSA"}, {Name: "ECDSA", Value: "ECDSA"}}),
			numberProp("RSA Bit Length", "rsaBitLength", 2048),
			selectProp("ECDSA Curve", "ecdsaCurve", "P-256", []Option{{Name: "P-256", Value: "P-256"}, {Name: "P-384", Value: "P-384"}, {Name: "P-521", Value: "P-521"}}),
			text("AES Key", "aesKey", ""),
			selectProp("AES Algorithm", "aesAlgorithm", "aes-256-gcm", []Option{{Name: "AES-128-GCM", Value: "aes-128-gcm"}, {Name: "AES-192-GCM", Value: "aes-192-gcm"}, {Name: "AES-256-GCM", Value: "aes-256-gcm"}, {Name: "AES-128-CBC", Value: "aes-128-cbc"}, {Name: "AES-192-CBC", Value: "aes-192-cbc"}, {Name: "AES-256-CBC", Value: "aes-256-cbc"}}),
			text("AES IV", "aesIv", "")),
		action("n8n-nodes-base.code", "Code", "Runs JavaScript or Python code", "transform", codeNodeProps()...).withVersions(1, 2),
		action("n8n-nodes-base.function", "Function", "Legacy JavaScript function node", "transform", legacyFunctionProps("return items;")...),
		action("n8n-nodes-base.functionItem", "Function Item", "Legacy JavaScript function item node", "transform", legacyFunctionProps("return item;")...),
		action("n8n-nodes-base.executeCommand", "Execute Command", "Runs a shell command", "utility", executeCommandProps()...),
		action("n8n-nodes-base.executeWorkflow", "Execute Workflow", "Runs another workflow and optionally waits for its output", "flow",
			selectProp("Source", "source", "database", []Option{{Name: "Database", Value: "database"}, {Name: "Current Workflow", Value: "currentWorkflow"}}),
			text("Workflow ID", "workflowId", ""),
			selectProp("Mode", "mode", "once", []Option{{Name: "Run Once with All Items", Value: "once"}, {Name: "Run Once for Each Item", Value: "each"}}),
			fixedCollection("Options", "options", []Property{option("Wait For Sub-Workflow Completion", "waitForSubWorkflow", "boolean", true, nil)})),
		action("n8n-nodes-base.readWriteFile", "Read/Write Files", "Reads or writes local files", "utility",
			selectProp("Operation", "operation", "read", []Option{{Name: "Read", Value: "read"}, {Name: "Write", Value: "write"}, {Name: "Delete", Value: "delete"}, {Name: "Copy", Value: "copy"}, {Name: "Move", Value: "move"}, {Name: "List", Value: "list"}}),
			text("File Path", "filePath", ""),
			text("New Path", "newPath", ""),
			selectProp("Write To File", "writeToFile", "binary", []Option{{Name: "Binary", Value: "binary"}, {Name: "Text", Value: "text"}}),
			text("Text Content", "textContent", ""),
			text("Data Property Name", "dataPropertyName", "data"),
			option("Append To File", "appendToFile", "boolean", false, nil),
			fixedCollection("Options", "options", []Property{text("Return Object Type", "returnObjType", "binary"), text("Output Property Name", "dataPropertyName", "data"), text("Allowed Paths", "allowedPaths", ""), numberProp("Max File Size", "maxFileSize", 52428800)})),
		action("n8n-nodes-base.compression", "Compression", "Compresses or extracts data", "utility",
			compressionProps()...).
			withVersions(1, 1.1),
		action("n8n-nodes-base.html", "HTML", "Extracts or generates HTML", "transform",
			selectProp("Operation", "operation", "generateHtml", []Option{{Name: "Generate HTML", Value: "generateHtml"}, {Name: "Extract HTML Content", Value: "extractHtmlContent"}}),
			textArea("HTML", "html", ""),
			text("Data Property", "dataProperty", "html"),
			selectProp("Source Data", "sourceData", "json", []Option{{Name: "JSON", Value: "json"}, {Name: "Binary", Value: "binary"}}),
			text("Binary Property Name", "binaryPropertyName", "data"),
			text("Output Field Name", "outputFieldName", "html"),
			option("Sanitize", "sanitize", "boolean", false, nil),
			selectProp("Sanitize Policy", "sanitizePolicy", "ugc", []Option{{Name: "UGC", Value: "ugc"}, {Name: "Strict", Value: "strict"}, {Name: "Custom", Value: "custom"}}),
			fixedCollection("Extraction Values", "extractionValues", []Property{
				text("Key", "key", ""),
				text("CSS Selector", "cssSelector", ""),
				selectProp("Return Value", "returnValue", "text", []Option{{Name: "Text", Value: "text"}, {Name: "HTML", Value: "html"}, {Name: "Inner HTML", Value: "innerHTML"}, {Name: "Value", Value: "value"}, {Name: "Attribute", Value: "attribute"}}),
				text("Attribute", "attribute", ""),
				option("Return Array", "returnArray", "boolean", false, nil),
			}),
			fixedCollection("Options", "options", []Property{
				option("Trim Whitespace", "trimWhitespace", "boolean", true, nil),
				option("Cleanup HTML", "cleanupHTML", "boolean", false, nil),
				option("Unfurl Links", "unfurlLinks", "boolean", false, nil),
				text("Base URL", "baseURL", ""),
			})),
		action("n8n-nodes-base.xml", "XML", "Converts XML and JSON", "transform",
			selectProp("Operation", "operation", "toJson", []Option{{Name: "To JSON", Value: "toJson"}, {Name: "From JSON", Value: "fromJson"}, {Name: "Validate", Value: "validate"}}),
			text("Data Property Name", "dataPropertyName", "data"),
			textArea("XML", "xml", ""),
			fixedCollection("Options", "options", []Property{
				text("Attribute Prefix", "attributePrefix", "@"),
				text("Text Node Key", "textNodeKey", "#text"),
				option("Force Array", "forceArray", "boolean", false, nil),
				option("Parse Numbers", "parseNumbers", "boolean", false, nil),
				option("Parse Booleans", "parseBooleans", "boolean", false, nil),
				option("Explicit Root", "explicitRoot", "boolean", true, nil),
				text("Root Name", "rootName", "root"),
				option("XML Declaration", "xmlDeclaration", "boolean", false, nil),
				text("XML Version", "xmlVersion", "1.0"),
				text("XML Encoding", "xmlEncoding", "UTF-8"),
				text("Attribute Char", "attributeChar", "@"),
				text("CDATA Key", "cdataKey", "#cdata"),
				option("Preserve Namespaces", "preserveNamespaces", "boolean", false, nil),
				option("Ignore Namespaces", "ignoreNamespaces", "boolean", false, nil),
			})),
		action("n8n-nodes-base.markdown", "Markdown", "Converts Markdown and HTML", "transform",
			selectProp("Operation", "operation", "toHtml", []Option{{Name: "To HTML", Value: "toHtml"}, {Name: "From HTML", Value: "fromHtml"}}),
			text("Data Property Name", "dataPropertyName", "data"),
			textArea("Markdown", "markdown", ""),
			fixedCollection("Options", "options", []Property{
				selectProp("Flavor", "flavor", "gfm", []Option{{Name: "GitHub Flavored", Value: "gfm"}, {Name: "CommonMark", Value: "commonmark"}}),
				option("Tables", "tables", "boolean", false, nil),
				option("Strikethrough", "strikethrough", "boolean", false, nil),
				option("Autolinks", "autolinks", "boolean", false, nil),
				option("Task List Items", "taskListItems", "boolean", false, nil),
				option("Emoji", "emoji", "boolean", false, nil),
				option("Sanitize", "sanitize", "boolean", false, nil),
				option("Break Lines", "breakLines", "boolean", false, nil),
				option("Preserve Links", "preserveLinks", "boolean", true, nil),
				option("Convert Tables", "convertTables", "boolean", false, nil),
				selectProp("Bullet Char", "bulletChar", "-", []Option{{Name: "Dash", Value: "-"}, {Name: "Asterisk", Value: "*"}, {Name: "Plus", Value: "+"}}),
				selectProp("Heading Style", "headingStyle", "atx", []Option{{Name: "ATX", Value: "atx"}, {Name: "Setext", Value: "setext"}}),
				selectProp("Code Block Fence", "codeBlockFence", "```", []Option{{Name: "Backticks", Value: "```"}, {Name: "Tildes", Value: "~~~"}}),
				option("Extract Front Matter", "extractFrontMatter", "boolean", false, nil),
			})),
		action("n8n-nodes-base.extractFromFile", "Extract From File", "Extracts structured data from binary files", "utility",
			selectProp("Operation", "operation", "auto", []Option{{Name: "Auto", Value: "auto"}, {Name: "XLSX", Value: "xlsx"}, {Name: "ODS", Value: "ods"}, {Name: "CSV", Value: "csv"}, {Name: "HTML", Value: "html"}, {Name: "iCal", Value: "ical"}, {Name: "Text", Value: "text"}, {Name: "Binary", Value: "binary"}}),
			selectProp("HTML Operation", "htmlOperation", "fullText", []Option{{Name: "Full Text", Value: "fullText"}, {Name: "Extract Text", Value: "extractText"}, {Name: "Extract Table", Value: "extractTable"}, {Name: "Extract Links", Value: "extractLinks"}, {Name: "Extract Images", Value: "extractImages"}, {Name: "CSS Selector", Value: "cssSelector"}, {Name: "Structured Data", Value: "structuredData"}}),
			text("Binary Property", "binaryProperty", "data"),
			selectProp("Delimiter", "delimiter", "auto", []Option{{Name: "Auto", Value: "auto"}, {Name: "Comma", Value: ","}, {Name: "Semicolon", Value: ";"}, {Name: "Tab", Value: "\\t"}, {Name: "Pipe", Value: "|"}, {Name: "Colon", Value: ":"}}),
			text("Quote Character", "quoteChar", "\""),
			text("Escape Character", "escapeChar", "\""),
			text("Comment Character", "commentChar", "#"),
			selectProp("Encoding", "encoding", "auto", []Option{{Name: "Auto", Value: "auto"}, {Name: "UTF-8", Value: "utf8"}, {Name: "UTF-16 LE", Value: "utf16le"}, {Name: "UTF-16 BE", Value: "utf16be"}, {Name: "Latin-1", Value: "latin1"}, {Name: "Windows-1252", Value: "windows1252"}}),
			text("Sheet Name", "sheetName", ""),
			numberProp("Sheet Index", "sheetIndex", 0),
			text("Range", "range", ""),
			option("Header Row", "headerRow", "boolean", true, nil),
			numberProp("Header Row Index", "headerRowIndex", 0),
			numberProp("Skip Rows", "skipRows", 0),
			option("Trim Leading Space", "trimLeadingSpace", "boolean", true, nil),
			selectProp("Output Format", "outputFormat", "objects", []Option{{Name: "Objects", Value: "objects"}, {Name: "Arrays", Value: "arrays"}, {Name: "Structured", Value: "structured"}, {Name: "Reference", Value: "reference"}}),
			option("Convert Types", "convertTypes", "boolean", true, nil),
			text("Date Format", "dateFormat", ""),
			selectProp("Empty Values", "emptyValues", "null", []Option{{Name: "Null", Value: "null"}, {Name: "Skip", Value: "skip"}, {Name: "Empty String", Value: "empty-string"}}),
			numberProp("Stream Threshold", "streamThreshold", 104857600),
			text("Output Field Name", "outputFieldName", "data"),
			text("Line Output Field", "lineOutputField", "line"),
			option("Trim Whitespace", "trimWhitespace", "boolean", false, nil),
			option("Split Into Items", "splitIntoItems", "boolean", false, nil),
			text("Selector", "selector", ""),
			option("Return All", "returnAll", "boolean", false, nil),
			numberProp("Table Index", "tableIndex", 0),
			text("Link Base", "linkBase", ""),
			option("Only Internal", "onlyInternal", "boolean", false, nil),
			option("Only With Alt", "onlyWithAlt", "boolean", false, nil),
			text("Component Types", "componentTypes", "VEVENT"),
			text("Timezone", "timezone", ""),
			option("Include Metadata", "includeMetadata", "boolean", false, nil),
			option("Include Input Fields", "includeInputFields", "boolean", false, nil)),
		action("n8n-nodes-base.convertToFile", "Convert to File", "Converts item data to binary files", "utility",
			selectProp("Operation", "operation", "json", []Option{{Name: "CSV", Value: "csv"}, {Name: "XLSX", Value: "xlsx"}, {Name: "HTML", Value: "html"}, {Name: "Text", Value: "text"}, {Name: "JSON", Value: "json"}, {Name: "Binary", Value: "binary"}}),
			text("Output File Name", "outputFileName", "export.json"),
			text("Binary Property Name", "binaryPropertyName", "data"),
			text("Source Binary Property Name", "sourceBinaryPropertyName", "data"),
			selectProp("Mime Type", "mimeType", "application/json", []Option{{Name: "JSON", Value: "application/json"}, {Name: "CSV", Value: "text/csv"}, {Name: "XLSX", Value: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"}, {Name: "HTML", Value: "text/html"}, {Name: "Text", Value: "text/plain"}, {Name: "Binary", Value: "application/octet-stream"}}),
			text("Delimiter", "delimiter", ","),
			option("Include Header", "includeHeader", "boolean", true, nil),
			option("BOM", "bom", "boolean", false, nil),
			option("Quote All Fields", "quoteAllFields", "boolean", false, nil),
			text("Line Terminator", "lineTerminator", "\\n"),
			text("Empty Field Value", "emptyFieldValue", ""),
			selectProp("Encoding", "encoding", "utf8", []Option{{Name: "UTF-8", Value: "utf8"}, {Name: "UTF-16 LE", Value: "utf16le"}, {Name: "UTF-16 BE", Value: "utf16be"}, {Name: "Latin-1", Value: "latin1"}, {Name: "Windows-1252", Value: "windows1252"}}),
			text("Sheet Name", "sheetName", "Sheet1"),
			option("Header Row", "headerRow", "boolean", true, nil),
			option("Auto Filter", "autoFilter", "boolean", false, nil),
			option("Freeze Panes", "freezePanes", "boolean", false, nil),
			text("Title", "title", "Export"),
			text("Table Class", "tableClass", "n8n-table"),
			textArea("Template", "template", ""),
			text("Field Name", "fieldName", ""),
			text("Separator", "separator", "\\n"),
			option("Indent", "indent", "boolean", false, nil),
			option("Wrap In Array", "wrapInArray", "boolean", true, nil)),
		action("n8n-nodes-base.httpRequest", "HTTP Request", "Makes HTTP requests", "action", httpRequestProps()...).withVersions(1, 2, 3, 4, 4.1, 4.2, 4.3, 4.4).withCredentialDisplay("httpSslAuth", true, "provideSslCertificates", true),
		n8nNode(),
		aiAgentNode(),
		googleGeminiChatModelNode(),
		deepSeekChatModelNode(),
		openRouterChatModelNode(),
		sqlNode("n8n-nodes-base.postgres", "Postgres", "postgres"),
		sqlNode("n8n-nodes-base.mySql", "MySQL", "mySql"),
		action("n8n-nodes-base.redis", "Redis", "Reads and writes Redis data", "database",
			selectProp("Operation", "operation", "get", []Option{{Name: "Get", Value: "get"}, {Name: "Set", Value: "set"}, {Name: "Delete", Value: "delete"}, {Name: "Exists", Value: "exists"}, {Name: "Expire", Value: "expire"}, {Name: "TTL", Value: "ttl"}, {Name: "Increment", Value: "increment"}, {Name: "Increment By", Value: "incrby"}, {Name: "Decrement", Value: "decrement"}, {Name: "Keys", Value: "keys"}, {Name: "Scan", Value: "scan"}, {Name: "Hash Set", Value: "hset"}, {Name: "Hash Get", Value: "hget"}, {Name: "Hash Get All", Value: "hgetall"}, {Name: "Hash Delete", Value: "hdel"}, {Name: "Hash Exists", Value: "hexists"}, {Name: "Hash Keys", Value: "hkeys"}, {Name: "Hash Values", Value: "hvals"}, {Name: "List Push Left", Value: "lpush"}, {Name: "List Push Right", Value: "rpush"}, {Name: "List Pop Left", Value: "lpop"}, {Name: "List Pop Right", Value: "rpop"}, {Name: "List Range", Value: "lrange"}, {Name: "List Length", Value: "llen"}, {Name: "Set Add", Value: "sadd"}, {Name: "Set Members", Value: "smembers"}, {Name: "Set Is Member", Value: "sismember"}, {Name: "Set Remove", Value: "srem"}, {Name: "Set Cardinality", Value: "scard"}, {Name: "Sorted Set Add", Value: "zadd"}, {Name: "Sorted Set Range", Value: "zrange"}, {Name: "Sorted Set Score", Value: "zscore"}, {Name: "Publish", Value: "publish"}, {Name: "Type", Value: "type"}, {Name: "Rename", Value: "rename"}, {Name: "Persist", Value: "persist"}, {Name: "Command", Value: "command"}}),
			text("Host", "host", "localhost"),
			numberProp("Port", "port", 6379),
			secret("Password", "password"),
			numberProp("Database Number", "databaseNumber", 0),
			option("SSL", "ssl", "boolean", false, nil),
			option("TLS Insecure", "tlsInsecure", "boolean", false, nil),
			text("Key", "key", ""),
			text("New Key", "newKey", ""),
			text("Field", "field", ""),
			text("Fields", "fields", ""),
			text("Value", "value", ""),
			text("Member", "member", ""),
			numberProp("Score", "score", 0),
			numberProp("TTL", "ttl", 0),
			numberProp("Start", "start", 0),
			numberProp("Stop", "stop", -1),
			numberProp("Count", "count", 100),
			text("Channel", "channel", ""),
			text("Pattern", "pattern", "*"),
			text("Command", "command", ""),
			fixedCollection("Options", "options", []Property{
				selectProp("Expire Mode", "expireMode", "seconds", []Option{{Name: "Seconds", Value: "seconds"}, {Name: "Milliseconds", Value: "milliseconds"}, {Name: "Unix Timestamp", Value: "unixTimestamp"}}),
				selectProp("Get Value As", "getValueAs", "string", []Option{{Name: "String", Value: "string"}, {Name: "JSON", Value: "json"}, {Name: "Number", Value: "number"}, {Name: "Auto", Value: "auto"}}),
				selectProp("Set Value As", "setValueAs", "auto", []Option{{Name: "Auto", Value: "auto"}, {Name: "String", Value: "string"}, {Name: "JSON", Value: "json"}, {Name: "Number", Value: "number"}}),
				selectProp("Set Mode", "setMode", "", []Option{{Name: "Always", Value: ""}, {Name: "Only If Missing", Value: "nx"}, {Name: "Only If Exists", Value: "xx"}}),
			})).withCredential("redis", false),
		action("n8n-nodes-base.mongoDb", "MongoDB", "Reads and writes MongoDB documents", "database",
			selectProp("Operation", "operation", "find", []Option{{Name: "Find", Value: "find"}, {Name: "Find One", Value: "findOne"}, {Name: "Insert One", Value: "insertOne"}, {Name: "Insert Many", Value: "insertMany"}, {Name: "Update One", Value: "updateOne"}, {Name: "Update Many", Value: "updateMany"}, {Name: "Find One And Update", Value: "findOneAndUpdate"}, {Name: "Delete One", Value: "deleteOne"}, {Name: "Delete Many", Value: "deleteMany"}, {Name: "Find One And Delete", Value: "findOneAndDelete"}, {Name: "Aggregate", Value: "aggregate"}, {Name: "Count", Value: "countDocuments"}}),
			text("Connection String", "connectionString", "mongodb://localhost:27017"),
			text("Database", "database", ""),
			text("Authentication Database", "authenticationDatabase", ""),
			option("TLS", "tls", "boolean", false, nil),
			option("TLS Insecure", "tlsInsecure", "boolean", false, nil),
			text("Collection", "collection", ""),
			jsonProp("Query", "query", "{}"),
			jsonProp("Filter", "filter", "{}"),
			jsonProp("Document", "document", "{}"),
			textArea("Documents", "documents", "[]"),
			jsonProp("Update", "update", "{}"),
			jsonProp("Projection", "projection", "{}"),
			jsonProp("Sort", "sort", "{}"),
			textArea("Pipeline", "pipeline", "[]"),
			numberProp("Limit", "limit", 50),
			numberProp("Skip", "skip", 0),
			option("Upsert", "upsert", "boolean", false, nil),
			fixedCollection("Options", "options", []Property{
				option("Ordered", "ordered", "boolean", true, nil),
				option("Allow Disk Use", "allowDiskUse", "boolean", false, nil),
				selectProp("Return Documents", "returnDocuments", "before", []Option{{Name: "Before Update", Value: "before"}, {Name: "Updated Document", Value: "updated"}}),
			})).withCredential("mongoDb", false),
	}
}

func descriptorNodeType(desc descriptor.Descriptor) NodeType {
	ops := make([]Option, 0, len(desc.Operations))
	for name, operation := range desc.Operations {
		display := operation.DisplayName
		if display == "" {
			display = title(name)
		}
		ops = append(ops, Option{Name: display, Value: name, Description: firstNonEmptyText(operation.Description, operation.Method+" "+operation.Path)})
	}
	sort.Slice(ops, func(i, j int) bool { return ops[i].Name < ops[j].Name })
	defaultOperation := "default"
	if _, ok := desc.Operations[defaultOperation]; !ok && len(ops) > 0 {
		if value, ok := ops[0].Value.(string); ok {
			defaultOperation = value
		}
	}
	properties := []Property{
		selectProp("Operation", "operation", defaultOperation, ops),
		text("Base URL", "baseUrl", desc.BaseURL),
	}
	properties[0].Required = true
	properties[0].NoDataExpression = true
	for name, operation := range desc.Operations {
		operationName := operation.Name
		if operationName == "" {
			operationName = name
		}
		for _, param := range operation.Params {
			prop := descriptorParamProperty(param, operationName)
			if prop.Name != "" {
				properties = append(properties, prop)
			}
		}
	}
	node := action(desc.NodeType, desc.DisplayName, firstNonEmptyText(desc.Description, desc.DisplayName+" API operations"), "integration", properties...)
	node.IconColor = categoryToColor(desc.Category)
	node.RequestDefaults = map[string]any{"baseURL": desc.BaseURL, "headers": desc.DefaultHeaders}
	node.Credentials = descriptorCredentials(desc)
	node.Codex = codexForAppCategory(desc.Category, node.DocumentationURL)
	return node
}

func descriptorParamProperty(param descriptor.Param, operationName string) Property {
	if param.In == "credential" {
		return Property{}
	}
	display := param.DisplayName
	if display == "" {
		display = title(param.Name)
	}
	prop := Property{
		DisplayName:      display,
		Name:             param.Name,
		Type:             descriptorParamType(param.Type),
		Default:          param.Default,
		Required:         param.Required,
		Description:      param.Description,
		Options:          descriptorOptions(param.Options),
		DisplayOptions:   map[string]any{"show": map[string][]any{"operation": []any{operationName}}},
		NoDataExpression: param.In == "path",
		Routing:          map[string]any{"send": map[string]any{"type": param.In, "property": param.Name}},
	}
	if prop.Default == nil {
		prop.Default = descriptorDefault(prop.Type)
	}
	if isSensitiveName(param.Name) {
		prop.TypeOptions = map[string]any{"password": true}
	}
	return prop
}

func descriptorParamType(paramType string) string {
	switch paramType {
	case "number":
		return "number"
	case "boolean":
		return "boolean"
	case "json", "array", "object":
		return "json"
	default:
		return "string"
	}
}

func descriptorDefault(propType string) any {
	switch propType {
	case "number":
		return 0
	case "boolean":
		return false
	case "json":
		return "{}"
	default:
		return ""
	}
}

func descriptorOptions(options []descriptor.Option) []Option {
	result := make([]Option, 0, len(options))
	for _, option := range options {
		result = append(result, Option{Name: option.Name, Value: option.Value})
	}
	return result
}

func descriptorCredentials(desc descriptor.Descriptor) []CredentialUsage {
	if desc.CredentialType != "" {
		result := []CredentialUsage{{Name: desc.CredentialType, Required: true}}
		for _, credential := range descriptorCredentialFallbacks(desc.NodeType) {
			if credential.Name != desc.CredentialType {
				result = append(result, credential)
			}
		}
		return result
	}
	return descriptorCredentialFallbacks(desc.NodeType)
}

func descriptorCredentialFallbacks(nodeType string) []CredentialUsage {
	switch nodeType {
	case "n8n-nodes-base.slack":
		return []CredentialUsage{{Name: "slackApi", Required: false}}
	case "n8n-nodes-base.github":
		return []CredentialUsage{{Name: "githubApi", Required: false}}
	case "n8n-nodes-base.gmail":
		return []CredentialUsage{{Name: "gmailOAuth2Api", Required: false}, {Name: "googleOAuth2Api", Required: false}}
	case "n8n-nodes-base.googleSheets":
		return []CredentialUsage{{Name: "googleSheetsOAuth2Api", Required: false}, {Name: "googleApi", Required: false}}
	case "n8n-nodes-base.notion":
		return []CredentialUsage{{Name: "notionApi", Required: false}}
	case "n8n-nodes-base.airtable":
		return []CredentialUsage{{Name: "airtableApi", Required: false}, {Name: "oAuth2Api", Required: false}}
	case "n8n-nodes-base.jira":
		return []CredentialUsage{{Name: "jiraSoftwareCloudApi", Required: false}}
	case "n8n-nodes-base.hubspot":
		return []CredentialUsage{{Name: "hubspotPrivateAppApi", Required: false}, {Name: "hubspotApi", Required: false}}
	case "n8n-nodes-base.stripe":
		return []CredentialUsage{{Name: "stripeApi", Required: false}}
	case "n8n-nodes-base.openAi":
		return []CredentialUsage{{Name: "openAiApi", Required: false}}
	case "n8n-nodes-base.telegram":
		return []CredentialUsage{{Name: "telegramApi", Required: false}}
	case "n8n-nodes-base.discord":
		return []CredentialUsage{{Name: "discordBotApi", Required: false}}
	case "n8n-nodes-base.twilio":
		return []CredentialUsage{{Name: "twilioApi", Required: false}}
	case "n8n-nodes-base.sendGrid":
		return []CredentialUsage{{Name: "sendGridApi", Required: false}}
	case "n8n-nodes-base.shopify":
		return []CredentialUsage{{Name: "shopifyAccessTokenApi", Required: false}}
	case "n8n-nodes-base.microsoftTeams":
		return []CredentialUsage{{Name: "microsoftTeamsOAuth2Api", Required: false}}
	case "n8n-nodes-base.trello":
		return []CredentialUsage{{Name: "trelloApi", Required: false}}
	default:
		return nil
	}
}

func categoryToColor(category string) string {
	switch category {
	case "Communication":
		return "#1A82E2"
	case "Development":
		return "#24292F"
	case "Productivity":
		return "#EA4335"
	case "Data & Storage":
		return "#44B678"
	case "Project Management":
		return "#0052CC"
	case "Marketing & CRM":
		return "#FF6D5A"
	case "Finance":
		return "#635BFF"
	case "AI":
		return "#10A37F"
	default:
		return "#4467ff"
	}
}

func isSensitiveName(name string) bool {
	lower := strings.ToLower(name)
	for _, token := range []string{"password", "secret", "token", "key", "credential"} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

func firstNonEmptyText(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func genericNodeType(name string) NodeType {
	display := strings.TrimPrefix(name, "n8n-nodes-base.")
	return NodeType{
		Name:             name,
		DisplayName:      title(display),
		Description:      "Registered node",
		Version:          1,
		Subtitle:         "={{$parameter.operation || ''}}",
		Defaults:         NodeDefaults{Name: title(display), Color: "#4467ff"},
		Inputs:           []string{"main"},
		Outputs:          []string{"main"},
		Group:            []string{"transform"},
		Category:         "utility",
		DocumentationURL: "https://docs.n8n.io/integrations/builtin/core-nodes/" + strings.TrimPrefix(name, "n8n-nodes-base."),
		Codex:            codexForCategory("utility", "https://docs.n8n.io/integrations/builtin/core-nodes/"+strings.TrimPrefix(name, "n8n-nodes-base.")),
	}
}

func builtinNodeIcon(name string, fallback string) string {
	if icon, ok := map[string]string{
		"n8n-nodes-base.aggregate":              "node:aggregate",
		"n8n-nodes-base.code":                   "node:code",
		"n8n-nodes-base.compression":            "node:compression",
		"n8n-nodes-base.convertToFile":          "node:convert-to-file",
		"n8n-nodes-base.crypto":                 "node:crypto",
		"n8n-nodes-base.dateTime":               "node:date-and-time",
		"n8n-nodes-base.editFields":             "node:edit-fields",
		"n8n-nodes-base.errorTrigger":           "node:error-trigger",
		"n8n-nodes-base.executeCommand":         "node:execute-command",
		"n8n-nodes-base.executeWorkflow":        "node:execute-sub-workflow",
		"n8n-nodes-base.executeWorkflowTrigger": "node:sub-workflow-trigger",
		"n8n-nodes-base.extractFromFile":        "node:extract-from-file",
		"n8n-nodes-base.filter":                 "node:filter",
		"n8n-nodes-base.formTrigger":            "node:form-trigger",
		"n8n-nodes-base.function":               "fa:code",
		"n8n-nodes-base.functionItem":           "fa:code",
		"n8n-nodes-base.html":                   "node:html",
		"n8n-nodes-base.httpRequest":            "node:http-request",
		"n8n-nodes-base.if":                     "node:if",
		"n8n-nodes-base.limit":                  "node:limit",
		"n8n-nodes-base.loopOverItems":          "node:loop-over-items",
		"n8n-nodes-base.manualTrigger":          "node:manual-trigger",
		"n8n-nodes-base.markdown":               "node:markdown",
		"n8n-nodes-base.merge":                  "node:merge",
		"n8n-nodes-base.mongoDb":                "file:mongodb.svg",
		"n8n-nodes-base.mySql":                  "file:mysql.svg",
		"n8n-nodes-base.noOp":                   "node:no-operation",
		"n8n-nodes-base.postgres":               "file:postgres.svg",
		"n8n-nodes-base.readWriteFile":          "node:read-write-files-from-disk",
		"n8n-nodes-base.redis":                  "file:redis.svg",
		"n8n-nodes-base.removeDuplicates":       "node:remove-duplicates",
		"n8n-nodes-base.respondToWebhook":       "node:respond-to-webhook",
		"n8n-nodes-base.scheduleTrigger":        "node:schedule-trigger",
		"n8n-nodes-base.set":                    "node:edit-fields",
		"n8n-nodes-base.sort":                   "node:sort",
		"n8n-nodes-base.splitInBatches":         "node:loop-over-items",
		"n8n-nodes-base.splitOut":               "node:split-out",
		"n8n-nodes-base.summarize":              "node:summarize",
		"n8n-nodes-base.switch":                 "node:switch",
		"n8n-nodes-base.wait":                   "node:wait",
		"n8n-nodes-base.webhook":                "node:webhook",
		"n8n-nodes-base.xml":                    "node:xml",
	}[name]; ok {
		return icon
	}
	if strings.Contains(fallback, ":") {
		return fallback
	}
	if fallback == "" {
		return ""
	}
	return "fa:" + fallback
}

func builtinNodeIconURL(name string, icon string) string {
	if iconURL, ok := map[string]string{
		"@n8n/n8n-nodes-langchain.lmChatDeepSeek":     "icons/@n8n/n8n-nodes-langchain/dist/nodes/llms/LmChatDeepSeek/deepseek.svg",
		"@n8n/n8n-nodes-langchain.lmChatGoogleGemini": "icons/@n8n/n8n-nodes-langchain/dist/nodes/llms/LmChatGoogleGemini/google.svg",
		"@n8n/n8n-nodes-langchain.lmChatOpenRouter":   "icons/@n8n/n8n-nodes-langchain/dist/nodes/llms/LmChatOpenRouter/openrouter.svg",
		"n8n-nodes-base.aggregate":                    "icons/n8n-nodes-base/dist/nodes/Transform/Aggregate/aggregate.svg",
		"n8n-nodes-base.n8n":                          "icons/n8n-nodes-base/dist/nodes/N8n/n8n.svg",
		"n8n-nodes-base.airtable":                     "icons/n8n-nodes-base/dist/nodes/Airtable/airtable.svg",
		"n8n-nodes-base.code":                         "icons/n8n-nodes-base/dist/nodes/Code/code.svg",
		"n8n-nodes-base.convertToFile":                "icons/n8n-nodes-base/dist/nodes/Files/ConvertToFile/convertToFile.svg",
		"n8n-nodes-base.discord":                      "icons/n8n-nodes-base/dist/nodes/Discord/discord.svg",
		"n8n-nodes-base.extractFromFile":              "icons/n8n-nodes-base/dist/nodes/Files/ExtractFromFile/extractFromFile.svg",
		"n8n-nodes-base.formTrigger":                  "icons/n8n-nodes-base/dist/nodes/Form/form.svg",
		"n8n-nodes-base.github":                       "icons/n8n-nodes-base/dist/nodes/Github/github.svg",
		"n8n-nodes-base.gmail":                        "icons/n8n-nodes-base/dist/nodes/Google/Gmail/gmail.svg",
		"n8n-nodes-base.googleSheets":                 "icons/n8n-nodes-base/dist/nodes/Google/Sheet/googleSheets.svg",
		"n8n-nodes-base.html":                         "icons/n8n-nodes-base/dist/nodes/Html/html.svg",
		"n8n-nodes-base.hubspot":                      "icons/n8n-nodes-base/dist/nodes/Hubspot/hubspot.svg",
		"n8n-nodes-base.httpRequest":                  "icons/n8n-nodes-base/dist/nodes/HttpRequest/httprequest.svg",
		"n8n-nodes-base.jira":                         "icons/n8n-nodes-base/dist/nodes/Jira/jira.svg",
		"n8n-nodes-base.limit":                        "icons/n8n-nodes-base/dist/nodes/Transform/Limit/limit.svg",
		"n8n-nodes-base.markdown":                     "icons/n8n-nodes-base/dist/nodes/Markdown/markdown.svg",
		"n8n-nodes-base.merge":                        "icons/n8n-nodes-base/dist/nodes/Merge/merge.svg",
		"n8n-nodes-base.microsoftTeams":               "icons/n8n-nodes-base/dist/nodes/Microsoft/Teams/teams.svg",
		"n8n-nodes-base.mongoDb":                      "icons/n8n-nodes-base/dist/nodes/MongoDb/mongodb.svg",
		"n8n-nodes-base.mySql":                        "icons/n8n-nodes-base/dist/nodes/MySql/mysql.svg",
		"n8n-nodes-base.notion":                       "icons/n8n-nodes-base/dist/nodes/Notion/notion.svg",
		"n8n-nodes-base.openAi":                       "icons/n8n-nodes-base/dist/nodes/OpenAi/openAi.svg",
		"n8n-nodes-base.postgres":                     "icons/n8n-nodes-base/dist/nodes/Postgres/postgres.svg",
		"n8n-nodes-base.readWriteFile":                "icons/n8n-nodes-base/dist/nodes/Files/ReadWriteFile/readWriteFile.svg",
		"n8n-nodes-base.redis":                        "icons/n8n-nodes-base/dist/nodes/Redis/redis.svg",
		"n8n-nodes-base.removeDuplicates":             "icons/n8n-nodes-base/dist/nodes/Transform/RemoveDuplicates/removeDuplicates.svg",
		"n8n-nodes-base.respondToWebhook":             "icons/n8n-nodes-base/dist/nodes/RespondToWebhook/webhook.svg",
		"n8n-nodes-base.sendGrid":                     "icons/n8n-nodes-base/dist/nodes/SendGrid/sendGrid.svg",
		"n8n-nodes-base.shopify":                      "icons/n8n-nodes-base/dist/nodes/Shopify/shopify.svg",
		"n8n-nodes-base.slack":                        "icons/n8n-nodes-base/dist/nodes/Slack/slack.svg",
		"n8n-nodes-base.sort":                         "icons/n8n-nodes-base/dist/nodes/Transform/Sort/sort.svg",
		"n8n-nodes-base.splitOut":                     "icons/n8n-nodes-base/dist/nodes/Transform/SplitOut/splitOut.svg",
		"n8n-nodes-base.stripe":                       "icons/n8n-nodes-base/dist/nodes/Stripe/stripe.svg",
		"n8n-nodes-base.summarize":                    "icons/n8n-nodes-base/dist/nodes/Transform/Summarize/summarize.svg",
		"n8n-nodes-base.telegram":                     "icons/n8n-nodes-base/dist/nodes/Telegram/telegram.svg",
		"n8n-nodes-base.trello":                       "icons/n8n-nodes-base/dist/nodes/Trello/trello.svg",
		"n8n-nodes-base.twilio":                       "icons/n8n-nodes-base/dist/nodes/Twilio/twilio.svg",
		"n8n-nodes-base.webhook":                      "icons/n8n-nodes-base/dist/nodes/Webhook/webhook.svg",
	}[name]; ok {
		return iconURL
	}
	return ""
}

func stickyNoteNode() NodeType {
	node := base("n8n-nodes-base.stickyNote", "Sticky Note", "Adds a note to the canvas", []string{"transform"}, "utility", "node:n8n",
		textArea("Content", "content", ""),
	)
	node.Inputs = []string{}
	node.Outputs = []string{}
	node.Subtitle = ""
	node.DocumentationURL = "https://docs.n8n.io/workflows/components/sticky-notes/"
	node.Codex = codexForCategory("utility", node.DocumentationURL)
	return node
}

func n8nNode() NodeType {
	workflowFilter := Property{
		DisplayName: "Workflow",
		Name:        "workflowId",
		Type:        "resourceLocator",
		Default:     map[string]any{"mode": "list", "value": ""},
		Description: "Workflow to filter the executions by",
		Modes: []ParameterMode{
			{
				DisplayName: "From List",
				Name:        "list",
				Type:        "list",
				Placeholder: "Select a Workflow...",
				TypeOptions: map[string]any{
					"searchListMethod":     "searchWorkflows",
					"searchFilterRequired": false,
					"searchable":           true,
				},
			},
			{
				DisplayName: "By URL",
				Name:        "url",
				Type:        "string",
				Placeholder: "https://myinstance.app.n8n.cloud/workflow/1",
			},
			{
				DisplayName: "ID",
				Name:        "id",
				Type:        "string",
				Placeholder: "1",
			},
		},
		Routing: map[string]any{
			"send": map[string]any{
				"type":     "query",
				"property": "workflowId",
				"value":    "={{ $value || undefined }}",
			},
		},
	}
	filters := showProp(collection("Filters", "filters", []Property{
		workflowFilter,
		{
			DisplayName: "Status",
			Name:        "status",
			Type:        "options",
			Default:     "success",
			Description: "Status to filter the executions by",
			Options: []Option{
				{Name: "Error", Value: "error"},
				{Name: "Success", Value: "success"},
				{Name: "Waiting", Value: "waiting"},
			},
			Routing: map[string]any{
				"send": map[string]any{
					"type":     "query",
					"property": "status",
					"value":    "={{ $value }}",
				},
			},
		},
	}), map[string][]any{"resource": []any{"execution"}, "operation": []any{"getAll"}})
	filters.Placeholder = "Add Filter"
	options := showProp(collection("Options", "options", []Property{
		{
			DisplayName: "Include Execution Details",
			Name:        "activeWorkflows",
			Type:        "boolean",
			Default:     false,
			Description: "Whether to include the detailed execution data",
			Routing: map[string]any{
				"send": map[string]any{
					"type":     "query",
					"property": "includeData",
					"value":    "={{ $value }}",
				},
			},
		},
	}), map[string][]any{"resource": []any{"execution"}, "operation": []any{"getAll"}})
	requestOptions := collection("Request Options", "requestOptions", []Property{})
	node := base("n8n-nodes-base.n8n", "n8n", "Handle events and perform actions on your n8n instance", []string{"transform"}, "integration", "file:n8n.svg",
		Property{
			DisplayName:      "Resource",
			Name:             "resource",
			Type:             "options",
			NoDataExpression: true,
			Default:          "workflow",
			Options: []Option{
				{Name: "Audit", Value: "audit"},
				{Name: "Credential", Value: "credential"},
				{Name: "Execution", Value: "execution"},
				{Name: "Workflow", Value: "workflow"},
			},
		},
		showProp(selectProp("Operation", "operation", "getAll", []Option{
			{Name: "Get", Value: "get", Action: "Get an execution"},
			{Name: "Get Many", Value: "getAll", Action: "Get many executions"},
			{Name: "Delete", Value: "delete", Action: "Delete an execution"},
		}), map[string][]any{"resource": []any{"execution"}}),
		showProp(text("Execution ID", "executionId", ""), map[string][]any{"resource": []any{"execution"}, "operation": []any{"get", "delete"}}),
		showProp(option("Return All", "returnAll", "boolean", false, nil), map[string][]any{"resource": []any{"execution"}, "operation": []any{"getAll"}}),
		showProp(numberProp("Limit", "limit", 100), map[string][]any{"resource": []any{"execution"}, "operation": []any{"getAll"}, "returnAll": []any{false}}),
		filters,
		options,
		requestOptions,
	)
	node.Subtitle = `={{$parameter["operation"] + ": " + $parameter["resource"]}}`
	node.Credentials = []CredentialUsage{{Name: "n8nApi", Required: true}}
	node.RequestDefaults = map[string]any{
		"returnFullResponse": true,
		"baseURL":            `={{ $credentials.baseUrl.replace(new RegExp("/$"), "") }}`,
		"headers": map[string]any{
			"Accept":       "application/json",
			"Content-Type": "application/json",
		},
	}
	node.DocumentationURL = "https://docs.n8n.io/integrations/builtin/core-nodes/n8n-nodes-base.n8n/"
	node.Codex = codexForAppCategory("App Nodes", node.DocumentationURL)
	return node
}

func aiAgentNode() NodeType {
	node := base("@n8n/n8n-nodes-langchain.agent", "AI Agent", "Generates an action plan and executes it using connected AI tools", []string{"transform"}, "utility", "fa:robot",
		Property{
			DisplayName: "Tip: Get a feel for agents with our quick <a href=\"https://docs.n8n.io/advanced-ai/intro-tutorial/\" target=\"_blank\">tutorial</a> or see an <a href=\"/workflows/templates/1954\" target=\"_blank\">example</a> of how this node works",
			Name:        "aiAgentStarterCallout",
			Type:        "callout",
			Default:     "",
		},
		Property{
			DisplayName: "Source for Prompt (User Message)",
			Name:        "promptType",
			Type:        "options",
			Default:     "auto",
			Options: []Option{
				{
					Name:        "Connected Chat Trigger Node",
					Value:       "auto",
					Description: "Looks for an input field called 'chatInput' that is coming from a directly connected Chat Trigger",
				},
				{
					Name:        "Connected Guardrails Node",
					Value:       "guardrails",
					Description: "Looks for an input field called 'guardrailsInput' that is coming from a directly connected Guardrails Node",
				},
				{
					Name:        "Define below",
					Value:       "define",
					Description: "Use an expression to reference data in previous nodes or enter static text",
				},
			},
			DisplayOptions: map[string]any{"show": map[string]any{"@version": []any{map[string]any{"_cnd": map[string]any{"lt": 3.1}}}}},
		},
		Property{
			DisplayName: "Source for Prompt (User Message)",
			Name:        "promptType",
			Type:        "options",
			Default:     "auto",
			Options: []Option{
				{
					Name:        "Connected Chat Trigger Node",
					Value:       "auto",
					Description: "Looks for an input field called 'chatInput' that is coming from a directly connected Chat Trigger",
				},
				{
					Name:        "Define below",
					Value:       "define",
					Description: "Use an expression to reference data in previous nodes or enter static text",
				},
			},
			DisplayOptions: map[string]any{"show": map[string]any{"@version": []any{map[string]any{"_cnd": map[string]any{"gte": 3.1}}}}},
		},
		Property{
			DisplayName: "Prompt (User Message)",
			Name:        "text",
			Type:        "string",
			Default:     "={{ $json.guardrailsInput }}",
			Required:    true,
			TypeOptions: map[string]any{"rows": 2},
			DisplayOptions: map[string]any{
				"show": map[string][]any{
					"promptType": []any{"guardrails"},
				},
			},
		},
		Property{
			DisplayName: "Prompt (User Message)",
			Name:        "text",
			Type:        "string",
			Default:     "={{ $json.chatInput }}",
			Required:    true,
			TypeOptions: map[string]any{"rows": 2},
			DisplayOptions: map[string]any{
				"show": map[string][]any{
					"promptType": []any{"auto"},
				},
			},
		},
		Property{
			DisplayName: "Prompt (User Message)",
			Name:        "text",
			Type:        "string",
			Default:     "",
			Required:    true,
			Placeholder: "e.g. Hello, how can you help me?",
			TypeOptions: map[string]any{"rows": 2},
			DisplayOptions: map[string]any{
				"show": map[string][]any{
					"promptType": []any{"define"},
				},
			},
		},
		Property{
			DisplayName:      "Require Specific Output Format",
			Name:             "hasOutputParser",
			Type:             "boolean",
			Default:          false,
			NoDataExpression: true,
		},
		option("Connect an <a data-action='openSelectiveNodeCreator' data-action-parameter-connectiontype='ai_outputParser'>output parser</a> on the canvas to specify the output format you require", "notice", "notice", "", nil),
		Property{
			DisplayName:      "Enable Fallback Model",
			Name:             "needsFallback",
			Type:             "boolean",
			Default:          false,
			NoDataExpression: true,
		},
		option("Connect an additional language model on the canvas to use it as a fallback if the main model fails", "fallbackNotice", "notice", "", nil),
		collection("Options", "options", []Property{
			textArea("System Message", "systemMessage", "You are a helpful assistant"),
			numberProp("Max Iterations", "maxIterations", 10),
			option("Return Intermediate Steps", "returnIntermediateSteps", "boolean", false, nil),
			option("Automatically Passthrough Binary Images", "passthroughBinaryImages", "boolean", true, nil),
			fixedCollectionGroup("Tracing Metadata", "tracingMetadata", "values", "Metadata", true, []Property{
				text("Key", "key", ""),
				{
					DisplayName: "Type",
					Name:        "type",
					Type:        "options",
					Default:     "stringValue",
					Description: "The field value type",
					Options: []Option{
						{Name: "Array", Value: "arrayValue"},
						{Name: "Boolean", Value: "booleanValue"},
						{Name: "Number", Value: "numberValue"},
						{Name: "Object", Value: "objectValue"},
						{Name: "String", Value: "stringValue"},
					},
				},
				text("Value", "stringValue", ""),
				text("Value", "numberValue", ""),
				option("Value", "booleanValue", "options", "true", []Option{{Name: "True", Value: "true"}, {Name: "False", Value: "false"}}),
				text("Value", "arrayValue", ""),
				jsonProp("Value", "objectValue", "={}"),
			}),
			option("Enable Streaming", "enableStreaming", "boolean", true, nil),
			collection("Batch Processing", "batching", []Property{
				numberProp("Batch Size", "batchSize", 1),
				numberProp("Delay Between Batches", "delayBetweenBatches", 0),
			}),
		}),
	)
	node.Version = []float64{1, 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7, 1.8, 1.9, 2, 2.1, 2.2, 2.3, 3, 3.1}
	node.DefaultVersion = 3.1
	node.Defaults.Color = "#404040"
	node.IconColor = ""
	node.Inputs = aiAgentInputsExpression()
	node.Outputs = []string{"main"}
	for index := range node.Properties {
		switch node.Properties[index].Name {
		case "notice":
			node.Properties[index].DisplayOptions = map[string]any{"show": map[string][]any{"hasOutputParser": []any{true}}}
		case "fallbackNotice":
			node.Properties[index].DisplayOptions = map[string]any{"show": map[string][]any{"needsFallback": []any{true}}}
		}
	}
	node.DocumentationURL = "https://docs.n8n.io/integrations/builtin/cluster-nodes/root-nodes/n8n-nodes-langchain.agent/"
	node.Codex = codexForAppCategory("AI", node.DocumentationURL)
	return node
}

func aiAgentInputsExpression() string {
	return `={{
	((hasOutputParser, needsFallback) => {
		const getInputData = (inputs) => inputs.map(({ type, filter, displayName, required }) => {
			const input = {
				type,
				displayName,
				required,
				maxConnections: ['ai_languageModel', 'ai_memory', 'ai_outputParser'].includes(type) ? 1 : undefined,
			};
			if (filter) input.filter = filter;
			return input;
		});
		let specialInputs = [
			{ type: 'ai_languageModel', displayName: 'Chat Model', required: true, filter: { excludedNodes: ['@n8n/n8n-nodes-langchain.lmCohere', '@n8n/n8n-nodes-langchain.lmOllama', 'n8n/n8n-nodes-langchain.lmOpenAi', '@n8n/n8n-nodes-langchain.lmOpenHuggingFaceInference'] } },
			{ type: 'ai_languageModel', displayName: 'Fallback Model', required: true, filter: { excludedNodes: ['@n8n/n8n-nodes-langchain.lmCohere', '@n8n/n8n-nodes-langchain.lmOllama', 'n8n/n8n-nodes-langchain.lmOpenAi', '@n8n/n8n-nodes-langchain.lmOpenHuggingFaceInference'] } },
			{ type: 'ai_memory', displayName: 'Memory' },
			{ type: 'ai_tool', displayName: 'Tool' },
			{ type: 'ai_outputParser', displayName: 'Output Parser' },
		];
		if (hasOutputParser === false) specialInputs = specialInputs.filter((input) => input.type !== 'ai_outputParser');
		if (needsFallback === false) specialInputs = specialInputs.filter((input) => input.displayName !== 'Fallback Model');
		return ['main', ...getInputData(specialInputs)];
	})($parameter.hasOutputParser === undefined || $parameter.hasOutputParser === true, $parameter.needsFallback !== undefined && $parameter.needsFallback === true)
}}`
}

func googleGeminiChatModelNode() NodeType {
	node := base("@n8n/n8n-nodes-langchain.lmChatGoogleGemini", "Google Gemini Chat Model", "Provides a Google Gemini chat model for AI workflows", []string{"transform"}, "utility", "file:google.svg",
		Property{
			DisplayName: "This node must be connected to an AI chain. <a data-action='openSelectiveNodeCreator' data-action-parameter-creatorview='AI'>Insert one</a>",
			Name:        "notice",
			Type:        "notice",
			Default:     "",
			TypeOptions: map[string]any{"containerClass": "ndv-connection-hint-notice"},
		},
		Property{
			DisplayName: "Model",
			Name:        "modelName",
			Type:        "options",
			Default:     "models/gemini-2.5-flash",
			Description: "The model which will generate the completion. <a href=\"https://developers.generativeai.google/api/rest/generativelanguage/models/list\">Learn more</a>.",
			TypeOptions: map[string]any{
				"loadOptions": map[string]any{
					"routing": map[string]any{
						"request": map[string]any{"method": "GET", "url": "/v1beta/models"},
						"output": map[string]any{"postReceive": []any{
							map[string]any{"type": "rootProperty", "properties": map[string]any{"property": "models"}},
							map[string]any{"type": "filter", "properties": map[string]any{"pass": "={{ !$responseItem.name.includes('embedding') }}"}},
							map[string]any{"type": "setKeyValue", "properties": map[string]any{
								"name":        "={{$responseItem.name}}",
								"value":       "={{$responseItem.name}}",
								"description": "={{$responseItem.description}}",
							}},
							map[string]any{"type": "sort", "properties": map[string]any{"key": "name"}},
						}},
					},
				},
			},
		},
		collection("Options", "options", []Property{
			numberProp("Maximum Number of Tokens", "maxOutputTokens", 2048),
			numberProp("Sampling Temperature", "temperature", 0.4),
			numberProp("Top K", "topK", 32),
			numberProp("Top P", "topP", 1),
			fixedCollectionGroup("Safety Settings", "safetySettings", "values", "Safety Settings", true, []Property{
				option("Safety Category", "category", "options", "HARM_CATEGORY_UNSPECIFIED", []Option{
					{Name: "HARM_CATEGORY_HARASSMENT", Value: "HARM_CATEGORY_HARASSMENT", Description: "Harassment content"},
					{Name: "HARM_CATEGORY_HATE_SPEECH", Value: "HARM_CATEGORY_HATE_SPEECH", Description: "Hate speech and content"},
					{Name: "HARM_CATEGORY_SEXUALLY_EXPLICIT", Value: "HARM_CATEGORY_SEXUALLY_EXPLICIT", Description: "Sexually explicit content"},
					{Name: "HARM_CATEGORY_DANGEROUS_CONTENT", Value: "HARM_CATEGORY_DANGEROUS_CONTENT", Description: "Dangerous content"},
				}),
				option("Safety Threshold", "threshold", "options", "HARM_BLOCK_THRESHOLD_UNSPECIFIED", []Option{
					{Name: "HARM_BLOCK_THRESHOLD_UNSPECIFIED", Value: "HARM_BLOCK_THRESHOLD_UNSPECIFIED", Description: "Threshold is unspecified"},
					{Name: "BLOCK_LOW_AND_ABOVE", Value: "BLOCK_LOW_AND_ABOVE", Description: "Content with NEGLIGIBLE will be allowed"},
					{Name: "BLOCK_MEDIUM_AND_ABOVE", Value: "BLOCK_MEDIUM_AND_ABOVE", Description: "Content with NEGLIGIBLE and LOW will be allowed"},
					{Name: "BLOCK_ONLY_HIGH", Value: "BLOCK_ONLY_HIGH", Description: "Content with NEGLIGIBLE, LOW, and MEDIUM will be allowed"},
					{Name: "BLOCK_NONE", Value: "BLOCK_NONE", Description: "All content will be allowed"},
				}),
			}),
		}),
	)
	node.Version = 1
	node.DefaultVersion = nil
	node.Inputs = []string{}
	node.Outputs = []string{"ai_languageModel"}
	node.OutputNames = []string{"Model"}
	node.Credentials = []CredentialUsage{{Name: "googlePalmApi", Required: true}}
	node.RequestDefaults = map[string]any{
		"ignoreHttpStatusErrors": true,
		"baseURL":                "={{ $credentials.host }}",
	}
	node.DocumentationURL = "https://docs.n8n.io/integrations/builtin/cluster-nodes/sub-nodes/n8n-nodes-langchain.lmchatgooglegemini/"
	node.Codex = codexForAppCategory("AI", node.DocumentationURL)
	return node
}

func deepSeekChatModelNode() NodeType {
	return openAICompatibleChatModelNode(openAICompatibleChatModelConfig{
		Name:             "@n8n/n8n-nodes-langchain.lmChatDeepSeek",
		DisplayName:      "DeepSeek Chat Model",
		Icon:             "file:deepseek.svg",
		Credential:       "deepSeekApi",
		DefaultModel:     "deepseek-chat",
		ModelDescription: "The model which will generate the completion. <a href=\"https://api-docs.deepseek.com/quick_start/pricing\">Learn more</a>.",
		DocumentationURL: "https://docs.n8n.io/integrations/builtin/cluster-nodes/sub-nodes/n8n-nodes-langchain.lmchatdeepseek/",
	})
}

func openRouterChatModelNode() NodeType {
	return openAICompatibleChatModelNode(openAICompatibleChatModelConfig{
		Name:             "@n8n/n8n-nodes-langchain.lmChatOpenRouter",
		DisplayName:      "OpenRouter Chat Model",
		Icon:             "file:openrouter.svg",
		Credential:       "openRouterApi",
		DefaultModel:     "openai/gpt-4.1-mini",
		ModelDescription: "The model which will generate the completion. <a href=\"https://openrouter.ai/docs/models\">Learn more</a>.",
		DocumentationURL: "https://docs.n8n.io/integrations/builtin/cluster-nodes/sub-nodes/n8n-nodes-langchain.lmchatopenrouter/",
	})
}

type openAICompatibleChatModelConfig struct {
	Name             string
	DisplayName      string
	Icon             string
	Credential       string
	DefaultModel     string
	ModelDescription string
	DocumentationURL string
}

func openAICompatibleChatModelNode(config openAICompatibleChatModelConfig) NodeType {
	node := base(config.Name, config.DisplayName, "For advanced usage with an AI chain", []string{"transform"}, "utility", config.Icon,
		Property{
			DisplayName: "This node must be connected to an AI chain. <a data-action='openSelectiveNodeCreator' data-action-parameter-creatorview='AI'>Insert one</a>",
			Name:        "notice",
			Type:        "notice",
			Default:     "",
			TypeOptions: map[string]any{"containerClass": "ndv-connection-hint-notice"},
		},
		Property{
			DisplayName: "If using JSON response format, you must include word \"json\" in the prompt in your chain or agent. Also, make sure to select latest models released post November 2023.",
			Name:        "notice",
			Type:        "notice",
			Default:     "",
			DisplayOptions: map[string]any{
				"show": map[string][]any{
					"/options.responseFormat": []any{"json_object"},
				},
			},
		},
		Property{
			DisplayName: "Model",
			Name:        "model",
			Type:        "options",
			Default:     config.DefaultModel,
			Description: config.ModelDescription,
			TypeOptions: map[string]any{
				"loadOptions": map[string]any{
					"routing": map[string]any{
						"request": map[string]any{"method": "GET", "url": "/models"},
						"output": map[string]any{"postReceive": []any{
							map[string]any{"type": "rootProperty", "properties": map[string]any{"property": "data"}},
							map[string]any{"type": "setKeyValue", "properties": map[string]any{
								"name":  "={{$responseItem.id}}",
								"value": "={{$responseItem.id}}",
							}},
							map[string]any{"type": "sort", "properties": map[string]any{"key": "name"}},
						}},
					},
				},
			},
		},
		collection("Options", "options", []Property{
			openAICompatibleNumberOption("Frequency Penalty", "frequencyPenalty", 0, -2, 2, 1),
			openAICompatibleNumberOption("Maximum Number of Tokens", "maxTokens", -1, 0, 32768, 0),
			selectProp("Response Format", "responseFormat", "text", []Option{
				{Name: "Text", Value: "text"},
				{Name: "JSON", Value: "json_object"},
			}),
			openAICompatibleNumberOption("Presence Penalty", "presencePenalty", 0, -2, 2, 1),
			openAICompatibleNumberOption("Sampling Temperature", "temperature", 0.7, 0, 2, 1),
			numberProp("Timeout", "timeout", 360000),
			numberProp("Max Retries", "maxRetries", 2),
			openAICompatibleNumberOption("Top P", "topP", 1, 0, 1, 1),
		}),
	)
	node.Version = 1
	node.DefaultVersion = nil
	node.Defaults.Name = config.DisplayName
	node.Inputs = []string{}
	node.Outputs = []string{"ai_languageModel"}
	node.OutputNames = []string{"Model"}
	node.Credentials = []CredentialUsage{{Name: config.Credential, Required: true}}
	node.RequestDefaults = map[string]any{
		"baseURL": "={{ $credentials?.url }}",
	}
	node.DocumentationURL = config.DocumentationURL
	node.Codex = codexForAppCategory("AI", node.DocumentationURL)
	return node
}

func openAICompatibleNumberOption(display string, name string, def float64, min float64, max float64, precision float64) Property {
	prop := numberProp(display, name, def)
	options := map[string]any{}
	if max > min {
		options["minValue"] = min
		options["maxValue"] = max
	}
	if precision > 0 {
		options["numberPrecision"] = precision
	}
	if len(options) > 0 {
		prop.TypeOptions = options
	}
	return prop
}

func trigger(name string, display string, description string, icon string, props ...Property) NodeType {
	node := base(name, display, description, []string{"trigger"}, "trigger", icon, props...)
	node.Inputs = []string{}
	node.Outputs = []string{"main"}
	node.TriggerPanel = map[string]any{"header": display}
	node.Codex = codexForCategory("trigger", node.DocumentationURL)
	return node
}

func action(name string, display string, description string, category string, props ...Property) NodeType {
	node := base(name, display, description, []string{"transform"}, category, "node", props...)
	node.Codex = codexForCategory(category, node.DocumentationURL)
	return node
}

func sqlNode(name string, display string, credential string) NodeType {
	schema := hideProp(resourceLocator("Schema", "schema", "public", "schemaSearch", true), map[string][]any{"operation": []any{"executeQuery"}})
	table := hideProp(resourceLocator("Table", "table", "", "tableSearch", true), map[string][]any{"operation": []any{"executeQuery"}})
	props := []Property{
		hiddenProp("Resource", "resource", "database"),
		selectProp("Operation", "operation", "insert", []Option{
			{Name: "Delete", Value: "deleteTable", Description: "Delete an entire table or rows in a table", Action: "Delete table or rows"},
			{Name: "Execute Query", Value: "executeQuery", Description: "Execute an SQL query", Action: "Execute a SQL query"},
			{Name: "Insert", Value: "insert", Description: "Insert rows in a table", Action: "Insert rows in a table"},
			{Name: "Insert or Update", Value: "upsert", Description: "Insert or update rows in a table", Action: "Insert or update rows in a table"},
			{Name: "Select", Value: "select", Description: "Select rows from a table", Action: "Select rows from a table"},
			{Name: "Update", Value: "update", Description: "Update rows in a table", Action: "Update rows in a table"},
		}),
		schema,
		table,
	}
	props = append(props, sqlExecuteQueryProps()...)
	props = append(props, sqlDeleteTableProps()...)
	props = append(props, sqlInsertProps()...)
	props = append(props, sqlSelectProps()...)
	props = append(props, sqlUpdateProps()...)
	props = append(props, sqlUpsertProps()...)
	node := action(name, display, "Get, add and update data in "+display, "database", props...)
	node.Credentials = []CredentialUsage{{Name: credential, Required: true}}
	node = node.withVersions(2, 2.1, 2.2, 2.3, 2.4, 2.5, 2.6)
	node.DefaultVersion = 2.6
	return node
}

func sqlExecuteQueryProps() []Property {
	query := showProp(codeEditor("Query", "query", "SELECT 1", "sqlEditor", "PostgreSQL", true, true, "e.g. SELECT id, name FROM product WHERE quantity > $1 AND price <= $2"), map[string][]any{"operation": []any{"executeQuery"}})
	options := showProp(collection("Options", "options", sqlOptionsCollection("executeQuery")), map[string][]any{"operation": []any{"executeQuery"}})
	return []Property{
		query,
		options,
	}
}

func sqlSelectProps() []Property {
	limit := numberProp("Limit", "limit", 50)
	limit.DisplayOptions = map[string]any{"show": map[string][]any{"returnAll": []any{false}}}
	returnAll := showProp(option("Return All", "returnAll", "boolean", false, nil), map[string][]any{"operation": []any{"select"}})
	where := showProp(sqlWhereFixedCollection(), map[string][]any{"operation": []any{"select"}})
	combine := showProp(sqlCombineConditionsCollection(), map[string][]any{"operation": []any{"select"}})
	sort := showProp(sqlSortFixedCollection(), map[string][]any{"operation": []any{"select"}})
	options := showProp(collection("Options", "options", sqlOptionsCollection("select")), map[string][]any{"operation": []any{"select"}})
	return []Property{
		returnAll,
		limit,
		where,
		combine,
		sort,
		options,
	}
}

func sqlDeleteTableProps() []Property {
	command := showProp(selectProp("Command", "deleteCommand", "truncate", []Option{
		{Name: "Truncate", Value: "truncate", Description: "Only removes the table's data and preserves the table's structure"},
		{Name: "Delete", Value: "delete", Description: "Delete the rows that match the 'Select Rows' conditions below. If no selection is made, all rows in the table are deleted."},
		{Name: "Drop", Value: "drop", Description: "Deletes the table's data and also the table's structure permanently"},
	}), map[string][]any{"operation": []any{"deleteTable"}})
	restart := showProp(option("Restart Sequences", "restartSequences", "boolean", false, nil), map[string][]any{"deleteCommand": []any{"truncate"}})
	where := showProp(sqlWhereFixedCollection(), map[string][]any{"deleteCommand": []any{"delete"}})
	combine := showProp(sqlCombineConditionsCollection(), map[string][]any{"deleteCommand": []any{"delete"}})
	options := showProp(collection("Options", "options", sqlOptionsCollection("deleteTable")), map[string][]any{"operation": []any{"deleteTable"}})
	return []Property{
		command,
		restart,
		where,
		combine,
		options,
	}
}

func sqlInsertProps() []Property {
	columns := showProp(resourceMapper("Columns", "columns", "add", "getMappingColumns", "column", "columns"), map[string][]any{"operation": []any{"insert"}, "@version": []any{map[string]any{"_cnd": map[string]any{"gte": 2.2}}}})
	options := showProp(collection("Options", "options", sqlOptionsCollection("insert")), map[string][]any{"operation": []any{"insert"}})
	return []Property{
		columns,
		options,
	}
}

func sqlUpdateProps() []Property {
	columns := showProp(resourceMapper("Columns", "columns", "update", "getMappingColumns", "column", "columns"), map[string][]any{"operation": []any{"update"}, "@version": []any{map[string]any{"_cnd": map[string]any{"gte": 2.2}}}})
	options := showProp(collection("Options", "options", sqlOptionsCollection("update")), map[string][]any{"operation": []any{"update"}})
	return []Property{
		columns,
		options,
	}
}

func sqlUpsertProps() []Property {
	columns := showProp(resourceMapper("Columns", "columns", "upsert", "getMappingColumns", "column", "columns"), map[string][]any{"operation": []any{"upsert"}, "@version": []any{map[string]any{"_cnd": map[string]any{"gte": 2.2}}}})
	options := showProp(collection("Options", "options", sqlOptionsCollection("upsert")), map[string][]any{"operation": []any{"upsert"}})
	return []Property{
		columns,
		options,
	}
}

func sqlWhereFixedCollection() Property {
	return Property{
		DisplayName: "Select Rows",
		Name:        "where",
		Type:        "fixedCollection",
		Default:     map[string]any{},
		Description: "If not set, all rows will be selected",
		Placeholder: "Add Condition",
		TypeOptions: map[string]any{"multipleValues": true},
		Options: []Option{{
			Name:        "values",
			DisplayName: "Values",
			Values: []Property{
				{DisplayName: "Column", Name: "column", Type: "options", Default: "", Description: "Choose from the list, or specify an ID using an <a href=\"https://docs.n8n.io/code/expressions/\" target=\"_blank\">expression</a>", TypeOptions: map[string]any{"loadOptionsMethod": "getColumns", "loadOptionsDependsOn": []string{"schema.value", "table.value"}}},
				{DisplayName: "Operator", Name: "condition", Type: "options", Default: "equal", Description: "The operator to check the column against. When using 'LIKE' operator percent sign ( %) matches zero or more characters, underscore ( _) matches any single character.", Options: []Option{
					{Name: "Equal", Value: "equal"},
					{Name: "Not Equal", Value: "!="},
					{Name: "Like", Value: "LIKE"},
					{Name: "Greater Than", Value: ">"},
					{Name: "Less Than", Value: "<"},
					{Name: "Greater Than Or Equal", Value: ">="},
					{Name: "Less Than Or Equal", Value: "<="},
					{Name: "Is Null", Value: "IS NULL"},
					{Name: "Is Not Null", Value: "IS NOT NULL"},
				}},
				{DisplayName: "Value", Name: "value", Type: "string", Default: ""},
			},
		}},
	}
}

func sqlSortFixedCollection() Property {
	return Property{
		DisplayName: "Sort",
		Name:        "sort",
		Type:        "fixedCollection",
		Default:     map[string]any{},
		Placeholder: "Add Sort Rule",
		TypeOptions: map[string]any{"multipleValues": true},
		Options: []Option{{
			Name:        "values",
			DisplayName: "Values",
			Values: []Property{
				{DisplayName: "Column", Name: "column", Type: "options", Default: "", Description: "Choose from the list, or specify an ID using an <a href=\"https://docs.n8n.io/code/expressions/\" target=\"_blank\">expression</a>", TypeOptions: map[string]any{"loadOptionsMethod": "getColumns", "loadOptionsDependsOn": []string{"schema.value", "table.value"}}},
				{DisplayName: "Direction", Name: "direction", Type: "options", Default: "ASC", Options: []Option{{Name: "ASC", Value: "ASC"}, {Name: "DESC", Value: "DESC"}}},
			},
		}},
	}
}

func sqlCombineConditionsCollection() Property {
	return selectProp("Combine Conditions", "combineConditions", "AND", []Option{
		{Name: "AND", Value: "AND", Description: "Only rows that meet all the conditions are selected"},
		{Name: "OR", Value: "OR", Description: "Rows that meet at least one condition are selected"},
	})
}

func sqlOptionsCollection(operation string) []Property {
	options := []Property{
		numberProp("Connection Timeout", "connectionTimeout", 30),
		numberProp("Delay Closing Idle Connection", "delayClosingIdleConnection", 0),
		selectProp("Query Batching", "queryBatching", "single", []Option{
			{Name: "Single Query", Value: "single", Description: "A single query for all incoming items"},
			{Name: "Independent", Value: "independently", Description: "Execute one query per incoming item of the run"},
			{Name: "Transaction", Value: "transaction", Description: "Execute all queries in a transaction, if a failure occurs, all changes are rolled back"},
		}),
		text("Query Parameters", "queryReplacement", ""),
		option("Treat query parameters in single quotes as text", "treatQueryParametersInSingleQuotesAsText", "boolean", false, nil),
		multiOptions("Output Columns", "outputColumns", nil),
		selectProp("Output Large-Format Numbers As", "largeNumbersOutput", "text", []Option{{Name: "Numbers", Value: "numbers"}, {Name: "Text", Value: "text", Description: "Use this if you expect numbers longer than 16 digits (otherwise numbers may be incorrect)"}}),
		option("Skip on Conflict", "skipOnConflict", "boolean", false, nil),
		option("Replace Empty Strings with NULL", "replaceEmptyStrings", "boolean", false, nil),
	}
	if operation == "deleteTable" {
		options = append([]Property{option("Cascade", "cascade", "boolean", false, nil)}, options...)
	}
	return options
}

func base(name string, display string, description string, group []string, category string, icon string, props ...Property) NodeType {
	properties := make([]Property, 0, len(props))
	properties = append(properties, props...)
	iconValue := builtinNodeIcon(name, icon)
	return NodeType{
		Name:             name,
		DisplayName:      display,
		Description:      description,
		Version:          1,
		Subtitle:         "={{$parameter.operation || ''}}",
		Defaults:         NodeDefaults{Name: display, Color: "#4467ff"},
		Properties:       properties,
		Inputs:           []string{"main"},
		Outputs:          []string{"main"},
		Icon:             iconValue,
		IconURL:          builtinNodeIconURL(name, iconValue),
		Group:            group,
		Category:         category,
		DocumentationURL: "https://docs.n8n.io/integrations/builtin/core-nodes/" + strings.TrimPrefix(name, "n8n-nodes-base."),
	}
}

func (node NodeType) withVersions(versions ...float64) NodeType {
	if len(versions) == 0 {
		return node
	}
	if len(versions) == 1 {
		node.Version = versions[0]
		node.DefaultVersion = versions[0]
		return node
	}
	node.Version = versions
	node.DefaultVersion = versions[len(versions)-1]
	return node
}

func (node NodeType) withOutputs(outputs ...string) NodeType {
	if len(outputs) == 0 {
		return node
	}
	node.Outputs = append([]string(nil), outputs...)
	return node
}

func (node NodeType) withWebhookContract() NodeType {
	node.SupportsCORS = true
	node.EventTriggerDescription = "Waiting for you to call the Test URL"
	node.ActivationMessage = "You can now make calls to your production webhook URL."
	node.TriggerPanel = map[string]any{
		"header": "",
		"executionsHelp": map[string]any{
			"inactive": "Webhooks have two modes: test and production.",
			"active":   "Webhooks have two modes: test and production.",
		},
		"activationHint": "Once you've finished building your workflow, run it without having to click this button by using the production webhook URL.",
	}
	node.Webhooks = []Webhook{{
		Name:                       "default",
		HTTPMethod:                 `={{$parameter["httpMethod"] || "GET"}}`,
		IsFullPath:                 true,
		ResponseCode:               `={{$parameter["responseCode"] || $parameter["options"]?.["responseCode"]?.["values"]?.["responseCode"] || 200}}`,
		ResponseMode:               `={{$parameter["responseMode"]}}`,
		ResponseData:               `={{$parameter["responseData"]}}`,
		ResponseBinaryPropertyName: `={{$parameter["responseBinaryPropertyName"] || $parameter["options"]?.["binaryPropertyName"]}}`,
		ResponseContentType:        `={{$parameter["options"]?.["responseContentType"]}}`,
		ResponsePropertyName:       `={{$parameter["options"]?.["responsePropertyName"]}}`,
		ResponseHeaders:            `={{$parameter["options"]?.["responseHeaders"]}}`,
		Path:                       `={{$parameter["path"]}}`,
	}}
	return node
}

func codexForCategory(category string, documentationURL string) *NodeCodex {
	switch category {
	case "integration":
		return codexForAppCategory("App Nodes", documentationURL)
	case "database":
		return codexForAppCategory("Data & Storage", documentationURL)
	}
	codex := &NodeCodex{
		Categories: []string{"Core Nodes"},
		Subcategories: map[string][]string{
			"Core Nodes": []string{subcategoryForCategory(category)},
		},
	}
	if documentationURL != "" {
		codex.Resources = map[string]any{
			"primaryDocumentation": []map[string]any{{"url": documentationURL}},
		}
	}
	return codex
}

func codexForAppCategory(category string, documentationURL string) *NodeCodex {
	if strings.TrimSpace(category) == "" {
		category = "App Nodes"
	}
	codex := &NodeCodex{
		Categories: []string{category},
	}
	if documentationURL != "" {
		codex.Resources = map[string]any{
			"primaryDocumentation": []map[string]any{{"url": documentationURL}},
		}
	}
	return codex
}

func subcategoryForCategory(category string) string {
	switch category {
	case "trigger":
		return "Other Trigger Nodes"
	case "transform":
		return "Data Transformation"
	case "flow":
		return "Flow"
	case "utility":
		return "Helpers"
	case "database":
		return "Helpers"
	case "integration":
		return "Helpers"
	default:
		return "Helpers"
	}
}

func (n NodeType) withCredential(name string, required bool) NodeType {
	n.Credentials = append(n.Credentials, CredentialUsage{Name: name, Required: required})
	return n
}

func (n NodeType) withCredentialDisplay(name string, required bool, field string, values ...any) NodeType {
	n.Credentials = append(n.Credentials, CredentialUsage{
		Name:     name,
		Required: required,
		Display:  map[string]any{"show": map[string][]any{field: values}},
	})
	return n
}

func text(display string, name string, def string) Property {
	return Property{DisplayName: display, Name: name, Type: "string", Default: def}
}

func textPlaceholder(display string, name string, def string, placeholder string) Property {
	prop := text(display, name, def)
	prop.Placeholder = placeholder
	return prop
}

func textArea(display string, name string, def string) Property {
	prop := text(display, name, def)
	prop.TypeOptions = map[string]any{"rows": 5}
	return prop
}

func codeProp(display string, name string, def string) Property {
	return codeEditorProp(display, name, def, "javaScript", nil)
}

func codeEditorProp(display string, name string, def string, language string, displayOptions map[string]any) Property {
	prop := Property{
		DisplayName:      display,
		Name:             name,
		Type:             "string",
		Default:          def,
		NoDataExpression: true,
		TypeOptions: map[string]any{
			"editor":         "codeNodeEditor",
			"editorLanguage": language,
		},
	}
	if displayOptions != nil {
		prop.DisplayOptions = displayOptions
	}
	return prop
}

func codeEditor(display string, name string, def string, editor string, language string, required bool, noDataExpression bool, placeholder string) Property {
	prop := Property{
		DisplayName:      display,
		Name:             name,
		Type:             "string",
		Default:          def,
		Required:         required,
		NoDataExpression: noDataExpression,
		Placeholder:      placeholder,
		TypeOptions:      map[string]any{"editor": editor, "sqlDialect": language, "rows": 10},
	}
	return prop
}

func hiddenProp(display string, name string, def any) Property {
	return Property{DisplayName: display, Name: name, Type: "hidden", Default: def, NoDataExpression: true}
}

func resourceLocator(display string, name string, def string, searchListMethod string, required bool) Property {
	return Property{
		DisplayName: display,
		Name:        name,
		Type:        "resourceLocator",
		Default:     map[string]any{"mode": "list", "value": def},
		Required:    required,
		Placeholder: def,
		TypeOptions: map[string]any{
			"multipleValues": false,
		},
		Modes: []ParameterMode{
			{
				Name:        "list",
				DisplayName: "From List",
				Type:        "list",
				TypeOptions: map[string]any{"searchListMethod": searchListMethod},
			},
			{
				Name:        "name",
				DisplayName: "By Name",
				Type:        "string",
			},
		},
	}
}

func resourceMapper(display string, name string, mode string, method string, singular string, plural string) Property {
	return Property{
		DisplayName:      display,
		Name:             name,
		Type:             "resourceMapper",
		Default:          map[string]any{"mappingMode": "defineBelow", "value": nil},
		Required:         true,
		NoDataExpression: true,
		TypeOptions: map[string]any{
			"loadOptionsDependsOn": []string{"table.value", "operation"},
			"resourceMapper": map[string]any{
				"resourceMapperMethod": method,
				"mode":                 mode,
				"fieldWords": map[string]any{
					"singular": singular,
					"plural":   plural,
				},
				"addAllFields":  true,
				"multiKeyMatch": true,
			},
		},
	}
}

func multiOptions(display string, name string, def []string) Property {
	opts := make([]Option, 0)
	if def == nil {
		def = []string{}
	}
	return Property{DisplayName: display, Name: name, Type: "multiOptions", Default: def, Options: opts}
}

func numberProp(display string, name string, def float64) Property {
	return Property{DisplayName: display, Name: name, Type: "number", Default: def}
}

func selectProp(display string, name string, def any, opts []Option) Property {
	return Property{DisplayName: display, Name: name, Type: "options", Default: def, Options: opts}
}

func option(display string, name string, propType string, def any, opts []Option) Property {
	prop := Property{DisplayName: display, Name: name, Type: propType, Default: def}
	if opts != nil {
		prop.Options = opts
	}
	return prop
}

func showProp(prop Property, conditions map[string][]any) Property {
	prop.DisplayOptions = map[string]any{"show": conditions}
	return prop
}

func hideProp(prop Property, conditions map[string][]any) Property {
	prop.DisplayOptions = map[string]any{"hide": conditions}
	return prop
}

func jsonProp(display string, name string, def string) Property {
	return Property{DisplayName: display, Name: name, Type: "json", Default: def, TypeOptions: map[string]any{"rows": 5}}
}

func collection(display string, name string, values []Property) Property {
	return Property{DisplayName: display, Name: name, Type: "collection", Default: map[string]any{}, Placeholder: "Add option", Options: values}
}

func fixedCollection(display string, name string, values []Property) Property {
	return Property{DisplayName: display, Name: name, Type: "fixedCollection", Default: map[string]any{}, TypeOptions: map[string]any{"multipleValues": true}, Options: propertyOptions(values)}
}

func fixedCollectionGroup(display string, name string, groupName string, groupDisplay string, multiple bool, values []Property) Property {
	prop := Property{
		DisplayName: display,
		Name:        name,
		Type:        "fixedCollection",
		Default:     map[string]any{},
		Options: []Option{{
			Name:        groupName,
			DisplayName: groupDisplay,
			Values:      values,
		}},
	}
	if multiple {
		prop.TypeOptions = map[string]any{"multipleValues": true}
	}
	return prop
}

func webhookOptionsCollection() Property {
	responseCode := selectProp("Response Code", "responseCode", 200, []Option{
		{Name: "100 - Continue", Value: 100},
		{Name: "200 - OK", Value: 200},
		{Name: "201 - Created", Value: 201},
		{Name: "202 - Accepted", Value: 202},
		{Name: "204 - No Content", Value: 204},
		{Name: "301 - Moved Permanently", Value: 301},
		{Name: "302 - Found", Value: 302},
		{Name: "400 - Bad Request", Value: 400},
		{Name: "401 - Unauthorized", Value: 401},
		{Name: "403 - Forbidden", Value: 403},
		{Name: "404 - Not Found", Value: 404},
		{Name: "409 - Conflict", Value: 409},
		{Name: "429 - Too Many Requests", Value: 429},
		{Name: "500 - Internal Server Error", Value: 500},
		{Name: "Custom Code", Value: "customCode"},
	})
	responseCodeCollection := fixedCollectionGroup("Response Code", "responseCode", "values", "Values", false, []Property{
		responseCode,
		numberProp("Custom Code", "customCode", 200),
	})
	responseCodeCollection.Default = map[string]any{"values": map[string]any{"responseCode": 200}}
	prop := collection("Options", "options", []Property{
		text("Allowed Origins (CORS)", "allowedOrigins", "*"),
		text("Field Name for Binary Data", "binaryPropertyName", "data"),
		option("Ignore Bots", "ignoreBots", "boolean", false, nil),
		text("IP(s) Allowlist", "ipWhitelist", ""),
		option("Raw Body", "rawBody", "boolean", false, nil),
		responseCodeCollection,
		fixedCollectionGroup("Response Headers", "responseHeaders", "entries", "Entries", true, []Property{
			text("Name", "name", ""),
			text("Value", "value", ""),
		}),
	})
	prop.Default = map[string]any{"allowedOrigins": "*"}
	return prop
}

func respondToWebhookProps() []Property {
	responseBody := jsonProp("Response Body", "responseBody", "{\n  \"status\": \"success\"\n}")
	responseBody.DisplayOptions = map[string]any{"show": map[string][]any{"respondWith": []any{"json"}}}
	textBody := textArea("Response Body", "responseBody", "")
	textBody.DisplayOptions = map[string]any{"show": map[string][]any{"respondWith": []any{"text"}}}
	payload := jsonProp("Payload", "payload", "{\n  \"myField\": \"value\"\n}")
	payload.DisplayOptions = map[string]any{"show": map[string][]any{"respondWith": []any{"jwt"}}}
	redirectURL := text("Redirect URL", "redirectURL", "")
	redirectURL.DisplayOptions = map[string]any{"show": map[string][]any{"respondWith": []any{"redirect"}}}
	responseDataSource := selectProp("Response Data Source", "responseDataSource", "automatically", []Option{
		{Name: "Choose Automatically From Input", Value: "automatically", Description: "Use if input data will contain a single piece of binary data"},
		{Name: "Specify Myself", Value: "set", Description: "Enter the name of the input field the binary data will be in"},
	})
	responseDataSource.DisplayOptions = map[string]any{"show": map[string][]any{"respondWith": []any{"binary"}}}
	inputFieldName := text("Input Field Name", "inputFieldName", "data")
	inputFieldName.DisplayOptions = map[string]any{"show": map[string][]any{"respondWith": []any{"binary"}, "responseDataSource": []any{"set"}}}

	responseHeaders := fixedCollectionGroup("Response Headers", "responseHeaders", "entries", "Entries", true, []Property{
		text("Name", "name", ""),
		text("Value", "value", ""),
	})
	responseHeaders.Placeholder = "Add Response Header"
	responseKey := text("Put Response in Field", "responseKey", "")
	responseKey.Placeholder = "e.g. data"
	responseKey.DisplayOptions = map[string]any{"show": map[string][]any{"/respondWith": []any{"allIncomingItems", "firstIncomingItem"}}}
	optionsProp := collection("Options", "options", []Property{
		numberProp("Response Code", "responseCode", 200),
		responseHeaders,
		responseKey,
	})
	optionsProp.Default = map[string]any{}

	return []Property{
		selectProp("Respond With", "respondWith", "json", []Option{
			{Name: "All Incoming Items", Value: "allIncomingItems", Description: "Respond with all input JSON items"},
			{Name: "Binary File", Value: "binary", Description: "Respond with incoming file binary data"},
			{Name: "First Incoming Item", Value: "firstIncomingItem", Description: "Respond with the first input JSON item"},
			{Name: "JSON", Value: "json", Description: "Respond with a custom JSON body"},
			{Name: "JWT Token", Value: "jwt", Description: "Respond with a JWT token"},
			{Name: "No Data", Value: "noData", Description: "Respond with an empty body"},
			{Name: "Redirect", Value: "redirect", Description: "Respond with a redirect to a given URL"},
			{Name: "Text", Value: "text", Description: "Respond with a simple text message body"},
		}),
		responseBody,
		textBody,
		payload,
		redirectURL,
		responseDataSource,
		inputFieldName,
		optionsProp,
	}
}

func compressionProps() []Property {
	inputFields := text("Input Binary Field(s)", "binaryPropertyName", "data")
	inputFields.Description = "The name of the input binary field(s) containing the file(s) to compress or decompress"
	outputPrefix := text("Output Prefix", "outputPrefix", "file_")
	outputPrefix.DisplayOptions = map[string]any{"show": map[string][]any{"operation": []any{"decompress"}}}

	return []Property{
		selectProp("Operation", "operation", "decompress", []Option{
			{Name: "Compress", Value: "compress", Description: "Compress files into a zip or gzip archive"},
			{Name: "Decompress", Value: "decompress", Description: "Decompress zip or gzip archives"},
		}),
		inputFields,
		outputPrefix,
	}
}

func propertyOptions(values []Property) []Option {
	result := make([]Option, 0, len(values))
	for _, value := range values {
		result = append(result, Option{Name: value.DisplayName, Value: value.Name})
	}
	return result
}

func conditionProps() []Property {
	return []Property{
		text("Left Value", "leftValue", "={{ $json.value }}"),
		selectProp("Operation", "operation", "equal", []Option{
			{Name: "Equals", Value: "equal"},
			{Name: "Not Equal", Value: "notEqual"},
			{Name: "Contains", Value: "contains"},
			{Name: "Not Contains", Value: "notContains"},
			{Name: "Starts With", Value: "startsWith"},
			{Name: "Ends With", Value: "endsWith"},
			{Name: "Matches Regex", Value: "matchesRegex"},
			{Name: "Does Not Match Regex", Value: "notMatchesRegex"},
			{Name: "Exists", Value: "exists"},
			{Name: "Does Not Exist", Value: "notExists"},
			{Name: "Is Empty", Value: "isEmpty"},
			{Name: "Is Not Empty", Value: "isNotEmpty"},
			{Name: "Larger", Value: "larger"},
			{Name: "Larger Or Equal", Value: "largerEqual"},
			{Name: "Smaller", Value: "smaller"},
			{Name: "Smaller Or Equal", Value: "smallerEqual"},
			{Name: "Date After", Value: "dateAfter"},
			{Name: "Date Before", Value: "dateBefore"},
			{Name: "Is True", Value: "isTrue"},
			{Name: "Is False", Value: "isFalse"},
		}),
		text("Right Value", "rightValue", ""),
	}
}

func aggregateProps() []Property {
	operations := []Option{
		{Name: "Sum", Value: "sum"},
		{Name: "Count", Value: "count"},
		{Name: "Count Unique", Value: "countUnique"},
		{Name: "Min", Value: "min"},
		{Name: "Max", Value: "max"},
		{Name: "Mean", Value: "mean"},
		{Name: "First", Value: "first"},
		{Name: "Last", Value: "last"},
		{Name: "Append", Value: "append"},
		{Name: "Append Unique", Value: "appendUnique"},
		{Name: "Concatenate", Value: "concatenate"},
		{Name: "Concatenate Unique", Value: "concatenateUnique"},
	}
	return []Property{
		selectProp("Aggregate", "aggregate", "aggregateAllItemData", []Option{{Name: "All Item Data", Value: "aggregateAllItemData"}, {Name: "Individual Fields", Value: "aggregateIndividualFields"}}),
		text("Field To Aggregate", "fieldToAggregate", ""),
		text("Destination Field Name", "destinationFieldName", "data"),
		fixedCollection("Fields To Aggregate", "fieldsToAggregate", []Property{text("Field", "aggregateField", ""), text("Rename Field", "renameField", ""), selectProp("Aggregation", "aggregation", "append", operations), text("Separator", "separatorForConcatenate", ",")}),
		option("Keep Missing Values", "keepMissingValues", "boolean", false, nil),
		text("Sort Field", "sortField", ""),
		selectProp("Sort Order", "sortOrder", "ascending", []Option{{Name: "Ascending", Value: "ascending"}, {Name: "Descending", Value: "descending"}}),
	}
}

func summarizeProps() []Property {
	operations := []Option{
		{Name: "Append", Value: "append"},
		{Name: "Average", Value: "average"},
		{Name: "Concatenate", Value: "concatenate"},
		{Name: "Count", Value: "count"},
		{Name: "Count Unique", Value: "countUnique"},
		{Name: "Max", Value: "max"},
		{Name: "Min", Value: "min"},
		{Name: "Sum", Value: "sum"},
		{Name: "First", Value: "first"},
		{Name: "Last", Value: "last"},
		{Name: "List Unique", Value: "listUnique"},
		{Name: "Median", Value: "median"},
		{Name: "Variance", Value: "variance"},
		{Name: "Std Dev", Value: "stdDev"},
		{Name: "Count Truthy", Value: "countTruthy"},
	}
	return []Property{
		fixedCollection("Fields to Summarize", "fieldsToSummarize", []Property{
			selectProp("Aggregation", "aggregation", "count", operations),
			text("Field", "field", ""),
			text("New Field Name", "newFieldName", ""),
			option("Include Empty Values", "includeEmpty", "boolean", false, nil),
			selectProp("Separator", "separateBy", ",", []Option{{Name: "Comma", Value: ","}, {Name: "Comma and Space", Value: ", "}, {Name: "New Line", Value: "\n"}, {Name: "None", Value: ""}, {Name: "Space", Value: " "}, {Name: "Other", Value: "other"}}),
			text("Custom Separator", "customSeparator", ""),
		}),
		text("Fields to Split By", "fieldsToSplitBy", ""),
		fixedCollection("Options", "options", []Property{
			option("Continue if Field Not Found", "continueIfFieldNotFound", "boolean", false, nil),
			option("Disable Dot Notation", "disableDotNotation", "boolean", false, nil),
			selectProp("Output Format", "outputFormat", "separateItems", []Option{{Name: "Each Split in a Separate Item", Value: "separateItems"}, {Name: "All Splits in a Single Item", Value: "singleItem"}}),
			option("Ignore Blank Values", "ignoreBlankValues", "boolean", false, nil),
			option("Skip Empty Split Fields", "skipEmptySplitFields", "boolean", false, nil),
		}),
	}
}

func codeNodeProps() []Property {
	return []Property{
		selectProp("Mode", "mode", "runOnceForAllItems", []Option{{Name: "Run Once for All Items", Value: "runOnceForAllItems"}, {Name: "Run Once for Each Item", Value: "runOnceForEachItem"}}),
		showProp(selectProp("Language", "language", "javaScript", []Option{{Name: "JavaScript", Value: "javaScript"}, {Name: "Python", Value: "pythonNative"}, {Name: "Go", Value: "go"}}), map[string][]any{"@version": []any{2}}),
		showProp(hiddenProp("Language", "language", "javaScript"), map[string][]any{"@version": []any{1}}),
		codeEditorProp("JavaScript", "jsCode", "", "javaScript", map[string]any{"show": map[string][]any{"@version": []any{1}, "mode": []any{"runOnceForAllItems"}}}),
		codeEditorProp("JavaScript", "jsCode", "", "javaScript", map[string]any{"show": map[string][]any{"@version": []any{1}, "mode": []any{"runOnceForEachItem"}}}),
		codeEditorProp("JavaScript", "jsCode", "", "javaScript", map[string]any{"show": map[string][]any{"@version": []any{2}, "language": []any{"javaScript"}, "mode": []any{"runOnceForAllItems"}}}),
		codeEditorProp("JavaScript", "jsCode", "", "javaScript", map[string]any{"show": map[string][]any{"@version": []any{2}, "language": []any{"javaScript"}, "mode": []any{"runOnceForEachItem"}}}),
		codeEditorProp("Python", "pythonCode", "", "python", map[string]any{"show": map[string][]any{"language": []any{"python", "pythonNative"}, "mode": []any{"runOnceForAllItems"}}}),
		codeEditorProp("Python", "pythonCode", "", "python", map[string]any{"show": map[string][]any{"language": []any{"python", "pythonNative"}, "mode": []any{"runOnceForEachItem"}}}),
		codeEditorProp("Go", "goCode", "", "go", map[string]any{"show": map[string][]any{"language": []any{"go"}, "mode": []any{"runOnceForAllItems"}}}),
		codeEditorProp("Go", "goCode", "", "go", map[string]any{"show": map[string][]any{"language": []any{"go"}, "mode": []any{"runOnceForEachItem"}}}),
		showProp(option("Type <code>$</code> for a list of <a target=\"_blank\" href=\"https://docs.n8n.io/code-examples/methods-variables-reference/\">special vars/methods</a>. Debug by using <code>console.log()</code> statements and viewing their output in the browser console.", "notice", "notice", "", nil), map[string][]any{"language": []any{"javaScript"}}),
		showProp(option("Debug by using <code>print()</code> statements and viewing their output in the browser console.<br><br>The Python option does not support <code>_</code> syntax and helpers, except for <code>_items</code> in all-items mode and <code>_item</code> in per-item mode.", "notice", "notice", "", nil), map[string][]any{"language": []any{"python", "pythonNative"}}),
		showProp(option("Go runs native snippets in-process style through a Go runner. Use Go identifiers like <code>items</code>, <code>item</code>, <code>jsonData</code>, <code>binary</code>, <code>itemIndex</code>, <code>node</code>, <code>input</code>, <code>now</code>, and <code>today</code>. Return n8n-style objects such as <code>[]map[string]any{{\"json\": map[string]any{\"ok\": true}}}</code>.", "notice", "notice", "", nil), map[string][]any{"language": []any{"go"}}),
	}
}

func httpRequestProps() []Property {
	return []Property{
		{
			DisplayName: "",
			Name:        "curlImport",
			Type:        "curlImport",
			Default:     "",
		},
		selectProp("Method", "method", "GET", []Option{
			{Name: "DELETE", Value: "DELETE"},
			{Name: "GET", Value: "GET"},
			{Name: "HEAD", Value: "HEAD"},
			{Name: "OPTIONS", Value: "OPTIONS"},
			{Name: "PATCH", Value: "PATCH"},
			{Name: "POST", Value: "POST"},
			{Name: "PUT", Value: "PUT"},
		}),
		text("URL", "url", ""),
		selectProp("Authentication", "authentication", "none", []Option{
			{Name: "None", Value: "none"},
			{Name: "Predefined Credential Type", Value: "predefinedCredentialType"},
			{Name: "Generic Credential Type", Value: "genericCredentialType"},
		}),
		{
			DisplayName: "Credential Type",
			Name:        "nodeCredentialType",
			Type:        "credentialsSelect",
			Default:     "",
			Required:    true,
			CredentialTypes: []string{
				"extends:oAuth2Api",
				"extends:oAuth1Api",
				"has:authenticate",
			},
			DisplayOptions: map[string]any{
				"show": map[string][]any{
					"authentication": []any{"predefinedCredentialType"},
				},
			},
			NoDataExpression: true,
		},
		showProp(Property{
			DisplayName: "Generic Auth Type",
			Name:        "genericAuthType",
			Type:        "credentialsSelect",
			Default:     "",
			Required:    true,
			CredentialTypes: []string{
				"has:genericAuth",
			},
			DisplayOptions: map[string]any{
				"show": map[string][]any{
					"authentication": []any{"genericCredentialType"},
				},
			},
		}, map[string][]any{"authentication": []any{"genericCredentialType"}}),
		option("Send Query Parameters", "sendQuery", "boolean", false, nil),
		showProp(selectProp("Specify Query Parameters", "specifyQuery", "keypair", []Option{{Name: "Using Fields Below", Value: "keypair"}, {Name: "Using JSON", Value: "json"}}), map[string][]any{"sendQuery": []any{true}}),
		httpNamedValueCollection("Query Parameters", "queryParameters", "Query Parameter", "Add Query Parameter", map[string]any{
			"show": map[string][]any{
				"sendQuery":    []any{true},
				"specifyQuery": []any{"keypair"},
			},
		}),
		showProp(jsonProp("JSON", "jsonQuery", ""), map[string][]any{"sendQuery": []any{true}, "specifyQuery": []any{"json"}}),
		option("Send Headers", "sendHeaders", "boolean", false, nil),
		showProp(selectProp("Specify Headers", "specifyHeaders", "keypair", []Option{{Name: "Using Fields Below", Value: "keypair"}, {Name: "Using JSON", Value: "json"}}), map[string][]any{"sendHeaders": []any{true}}),
		httpNamedValueCollection("Headers", "headerParameters", "Header", "Add Header", map[string]any{
			"show": map[string][]any{
				"sendHeaders":    []any{true},
				"specifyHeaders": []any{"keypair"},
			},
		}),
		showProp(jsonProp("JSON", "jsonHeaders", ""), map[string][]any{"sendHeaders": []any{true}, "specifyHeaders": []any{"json"}}),
		option("Send Body", "sendBody", "boolean", false, nil),
		showProp(selectProp("Body Content Type", "contentType", "json", []Option{
			{Name: "Form URL Encoded", Value: "form-urlencoded"},
			{Name: "Form-Data", Value: "multipart-form-data"},
			{Name: "JSON", Value: "json"},
			{Name: "n8n Binary File", Value: "binaryData"},
			{Name: "Raw", Value: "raw"},
		}), map[string][]any{"sendBody": []any{true}}),
		showProp(selectProp("Specify Body", "specifyBody", "keypair", []Option{{Name: "Using Fields Below", Value: "keypair"}, {Name: "Using JSON", Value: "json"}}), map[string][]any{"sendBody": []any{true}, "contentType": []any{"json"}}),
		httpNamedValueCollection("Body Parameters", "bodyParameters", "Body Field", "Add Body Field", map[string]any{
			"show": map[string][]any{
				"sendBody":    []any{true},
				"contentType": []any{"json"},
				"specifyBody": []any{"keypair"},
			},
		}),
		showProp(jsonProp("JSON", "jsonBody", ""), map[string][]any{"sendBody": []any{true}, "contentType": []any{"json"}, "specifyBody": []any{"json"}}),
		httpMultipartCollection(),
		showProp(selectProp("Specify Body", "specifyBody", "keypair", []Option{{Name: "Using Fields Below", Value: "keypair"}, {Name: "Using Single Field", Value: "string"}}), map[string][]any{"sendBody": []any{true}, "contentType": []any{"form-urlencoded"}}),
		httpNamedValueCollection("Body Fields", "bodyParameters", "Field", "Add Field", map[string]any{
			"show": map[string][]any{
				"sendBody":    []any{true},
				"contentType": []any{"form-urlencoded"},
				"specifyBody": []any{"keypair"},
			},
		}),
		showProp(textPlaceholder("Body", "body", "", "field1=value1&field2=value2"), map[string][]any{"sendBody": []any{true}, "specifyBody": []any{"string"}}),
		showProp(text("Input Data Field Name", "inputDataFieldName", ""), map[string][]any{"sendBody": []any{true}, "contentType": []any{"binaryData"}}),
		showProp(textPlaceholder("Content Type", "rawContentType", "", "text/html"), map[string][]any{"sendBody": []any{true}, "contentType": []any{"raw"}}),
		showProp(textArea("Body", "body", ""), map[string][]any{"sendBody": []any{true}, "contentType": []any{"raw"}}),
		httpRequestOptionsCollection(),
		option("You can view the raw requests this node makes in your browser's developer console", "infoMessage", "notice", "", nil),
	}
}

func httpNamedValueCollection(display string, name string, itemDisplay string, placeholder string, displayOptions map[string]any) Property {
	return Property{
		DisplayName: display,
		Name:        name,
		Type:        "fixedCollection",
		Default: map[string]any{
			"parameters": []map[string]any{{
				"name":  "",
				"value": "",
			}},
		},
		Placeholder: placeholder,
		TypeOptions: map[string]any{
			"multipleValues": true,
			"fixedCollection": map[string]any{
				"itemTitle": "={{ $collection.item.value.name }}",
			},
		},
		DisplayOptions: displayOptions,
		Options: []Option{{
			Name:        "parameters",
			DisplayName: itemDisplay,
			Values: []Property{
				text("Name", "name", ""),
				text("Value", "value", ""),
			},
		}},
	}
}

func httpMultipartCollection() Property {
	return Property{
		DisplayName: "Body",
		Name:        "bodyParameters",
		Type:        "fixedCollection",
		Default: map[string]any{
			"parameters": []map[string]any{{
				"parameterType": "formData",
				"name":          "",
				"value":         "",
			}},
		},
		Placeholder: "Add Body Field",
		TypeOptions: map[string]any{
			"multipleValues": true,
			"fixedCollection": map[string]any{
				"itemTitle": "={{ $collection.item.value.name }}",
			},
		},
		DisplayOptions: map[string]any{
			"show": map[string][]any{
				"sendBody":    []any{true},
				"contentType": []any{"multipart-form-data"},
			},
		},
		Options: []Option{{
			Name:        "parameters",
			DisplayName: "Body Field",
			Values: []Property{
				selectProp("Type", "parameterType", "formData", []Option{{Name: "n8n Binary File", Value: "formBinaryData"}, {Name: "Form Data", Value: "formData"}}),
				text("Name", "name", ""),
				text("Value", "value", ""),
				text("Input Data Field Name", "inputDataFieldName", ""),
			},
		}},
	}
}

func httpRequestOptionsCollection() Property {
	return Property{
		DisplayName: "Options",
		Name:        "options",
		Type:        "collection",
		Default:     map[string]any{},
		Placeholder: "Add option",
		Options: []Property{
			{
				DisplayName: "Batching",
				Name:        "batching",
				Type:        "fixedCollection",
				Placeholder: "Add Batching",
				Default:     map[string]any{"batch": map[string]any{}},
				TypeOptions: map[string]any{"multipleValues": false},
				Options: []Option{{
					Name:        "batch",
					DisplayName: "Batching",
					Values: []Property{
						numberProp("Items per Batch", "batchSize", 50),
						numberProp("Batch Interval (ms)", "batchInterval", 1000),
					},
				}},
			},
			option("Ignore SSL Issues (Insecure)", "allowUnauthorizedCerts", "boolean", false, nil),
			selectProp("Array Format in Query Parameters", "queryParameterArrays", "brackets", []Option{{Name: "No Brackets", Value: "repeat"}, {Name: "Brackets Only", Value: "brackets"}, {Name: "Brackets with Indices", Value: "indices"}}),
			option("Lowercase Headers", "lowercaseHeaders", "boolean", true, nil),
			{
				DisplayName: "Redirects",
				Name:        "redirect",
				Type:        "fixedCollection",
				Placeholder: "Add Redirect",
				Default:     map[string]any{"redirect": map[string]any{}},
				TypeOptions: map[string]any{"multipleValues": false},
				Options: []Option{{
					Name:        "redirect",
					DisplayName: "Redirect",
					Values: []Property{
						option("Follow Redirects", "followRedirects", "boolean", true, nil),
						numberProp("Max Redirects", "maxRedirects", 21),
					},
				}},
			},
			{
				DisplayName: "Response",
				Name:        "response",
				Type:        "fixedCollection",
				Placeholder: "Add response",
				Default:     map[string]any{"response": map[string]any{}},
				TypeOptions: map[string]any{"multipleValues": false},
				Options: []Option{{
					Name:        "response",
					DisplayName: "Response",
					Values: []Property{
						option("Include Response Headers and Status", "fullResponse", "boolean", false, nil),
						option("Never Error", "neverError", "boolean", false, nil),
						selectProp("Response Format", "responseFormat", "autodetect", []Option{{Name: "Autodetect", Value: "autodetect"}, {Name: "File", Value: "file"}, {Name: "JSON", Value: "json"}, {Name: "Text", Value: "text"}}),
						text("Put Output in Field", "outputPropertyName", "data"),
					},
				}},
			},
			httpPaginationOption(),
			textPlaceholder("Proxy", "proxy", "", "e.g. http://myproxy:3128"),
			numberProp("Timeout", "timeout", 10000),
			option("Send Credentials on Cross-Origin Redirect", "sendCredentialsOnCrossOriginRedirect", "boolean", false, nil),
		},
	}
}

func httpPaginationOption() Property {
	return Property{
		DisplayName: "Pagination",
		Name:        "pagination",
		Type:        "fixedCollection",
		Placeholder: "Add pagination",
		Default:     map[string]any{"pagination": map[string]any{}},
		TypeOptions: map[string]any{"multipleValues": false},
		Options: []Option{{
			Name:        "pagination",
			DisplayName: "Pagination",
			Values: []Property{
				selectProp("Pagination Mode", "paginationMode", "updateAParameterInEachRequest", []Option{{Name: "Off", Value: "off"}, {Name: "Update a Parameter in Each Request", Value: "updateAParameterInEachRequest"}, {Name: "Response Contains Next URL", Value: "responseContainsNextURL"}}),
				option("Use the $response variables to access the data of the previous response. Refer to the docs for more info about pagination.", "webhookNotice", "notice", "", nil),
				text("Next URL", "nextURL", ""),
				{
					DisplayName: "Parameters",
					Name:        "parameters",
					Type:        "fixedCollection",
					Placeholder: "Add Parameter",
					Default: map[string]any{"parameters": []map[string]any{{
						"type":  "qs",
						"name":  "",
						"value": "",
					}}},
					TypeOptions: map[string]any{
						"multipleValues": true,
						"noExpression":   true,
						"fixedCollection": map[string]any{
							"itemTitle": "={{ $collection.item.value.name }}",
						},
					},
					Options: []Option{{
						Name:        "parameters",
						DisplayName: "Parameter",
						Values: []Property{
							selectProp("Type", "type", "qs", []Option{{Name: "Body", Value: "body"}, {Name: "Header", Value: "headers"}, {Name: "Query", Value: "qs"}}),
							textPlaceholder("Name", "name", "", "e.g page"),
							text("Value", "value", ""),
						},
					}},
				},
				selectProp("Pagination Complete When", "paginationCompleteWhen", "responseIsEmpty", []Option{{Name: "Response Is Empty", Value: "responseIsEmpty"}, {Name: "Receive Specific Status Code(s)", Value: "receiveSpecificStatusCodes"}, {Name: "Other", Value: "other"}}),
				text("Status Code(s) when Complete", "statusCodesWhenComplete", ""),
				text("Complete Expression", "completeExpression", ""),
				option("Limit Pages Fetched", "limitPagesFetched", "boolean", false, nil),
				numberProp("Max Pages", "maxRequests", 100),
				numberProp("Interval Between Requests (ms)", "requestInterval", 0),
			},
		}},
	}
}

func legacyFunctionProps(defaultCode string) []Property {
	return []Property{
		selectProp("Mode", "mode", "runOnceForAllItems", []Option{{Name: "Run Once for All Items", Value: "runOnceForAllItems"}, {Name: "Run Once for Each Item", Value: "runOnceForEachItem"}}),
		codeProp("JavaScript", "functionCode", defaultCode),
		numberProp("Timeout Seconds", "timeoutSeconds", 10),
	}
}

func executeCommandProps() []Property {
	return []Property{
		text("Command", "command", ""),
		option("Execute Once", "executeOnce", "boolean", false, nil),
		fixedCollection("Options", "options", []Property{
			text("Working Directory", "workingDirectory", ""),
			numberProp("Timeout", "timeout", 60000),
			numberProp("Max Output Size", "maxOutputSize", 10485760),
			fixedCollection("Environment Variables", "environmentVariables", []Property{text("Name", "name", ""), text("Value", "value", "")}),
		}),
	}
}

func loopBatchProps() []Property {
	return []Property{
		numberProp("Batch Size", "batchSize", 1),
		option("Reset", "reset", "boolean", false, nil),
	}
}

func options(values ...string) []Option {
	result := make([]Option, 0, len(values))
	for _, value := range values {
		result = append(result, Option{Name: title(value), Value: value})
	}
	return result
}

func title(value string) string {
	value = strings.ReplaceAll(value, "_", " ")
	value = strings.ReplaceAll(value, "-", " ")
	if value == "" {
		return ""
	}
	return strings.ToUpper(value[:1]) + value[1:]
}
