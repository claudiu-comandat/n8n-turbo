package descriptor

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestOfficialOperationAliases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		nodeType  string
		resource  string
		operation string
		want      string
	}{
		{"slack message post", "n8n-nodes-base.slack", "message", "post", "postMessage"},
		{"slack channel history", "n8n-nodes-base.slack", "channel", "history", "getChannelHistory"},
		{"slack channel archive", "n8n-nodes-base.slack", "channel", "archive", "archiveChannel"},
		{"slack channel replies", "n8n-nodes-base.slack", "channel", "replies", "getChannelReplies"},
		{"slack file get", "n8n-nodes-base.slack", "file", "get", "getFile"},
		{"slack reaction get", "n8n-nodes-base.slack", "reaction", "get", "getReaction"},
		{"slack star get all", "n8n-nodes-base.slack", "star", "getAll", "listStars"},
		{"slack user profile", "n8n-nodes-base.slack", "user", "getProfile", "getUserProfile"},
		{"github file edit", "n8n-nodes-base.github", "file", "edit", "createOrUpdateFile"},
		{"github file list", "n8n-nodes-base.github", "file", "list", "listFiles"},
		{"github issue comment", "n8n-nodes-base.github", "issue", "createComment", "createIssueComment"},
		{"github release get", "n8n-nodes-base.github", "release", "get", "getRelease"},
		{"github repository popular paths", "n8n-nodes-base.github", "repository", "listPopularPaths", "listPopularPaths"},
		{"github review list", "n8n-nodes-base.github", "review", "getAll", "listReviews"},
		{"github user issues", "n8n-nodes-base.github", "user", "getUserIssues", "listUserIssues"},
		{"github workflow dispatch", "n8n-nodes-base.github", "workflow", "dispatch", "dispatchWorkflow"},
		{"github workflow dispatch and wait", "n8n-nodes-base.github", "workflow", "dispatchAndWait", "dispatchWorkflow"},
		{"github workflow usage", "n8n-nodes-base.github", "workflow", "getUsage", "getWorkflowUsage"},
		{"gmail message send", "n8n-nodes-base.gmail", "message", "send", "sendMessage"},
		{"sheets spreadsheet create", "n8n-nodes-base.googleSheets", "spreadsheet", "create", "createSpreadsheet"},
		{"sheets sheet create", "n8n-nodes-base.googleSheets", "sheet", "create", "createSheet"},
		{"sheets sheet delete", "n8n-nodes-base.googleSheets", "sheet", "delete", "deleteDimension"},
		{"sheets sheet remove", "n8n-nodes-base.googleSheets", "sheet", "remove", "removeSheet"},
		{"airtable record search", "n8n-nodes-base.airtable", "record", "search", "listRecords"},
		{"jira issue get all", "n8n-nodes-base.jira", "issue", "getAll", "searchIssues"},
		{"hubspot contact upsert", "n8n-nodes-base.hubspot", "contact", "upsert", "updateContact"},
		{"notion block append", "n8n-nodes-base.notion", "block", "append", "appendBlockChildren"},
		{"notion page get", "n8n-nodes-base.notion", "page", "get", "getPage"},
		{"openai chat complete", "n8n-nodes-base.openAi", "chat", "complete", "chatCompletion"},
		{"stripe customer get all", "n8n-nodes-base.stripe", "customer", "getAll", "listCustomers"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			executor := Executor{descriptor: Descriptor{
				NodeType: tt.nodeType,
				Operations: map[string]Operation{
					tt.want: {Name: tt.want, Method: "GET", Path: "/"},
				},
			}}
			got := executor.operationName(map[string]any{"resource": tt.resource, "operation": tt.operation})
			if got != tt.want {
				t.Fatalf("operationName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOfficialOperationAliasesTargetExistingDescriptorOperations(t *testing.T) {
	t.Parallel()

	descriptors := map[string]Descriptor{}
	for _, descriptor := range Builtins() {
		descriptor.Normalize()
		descriptors[descriptor.NodeType] = descriptor
	}
	for nodeType, byResource := range officialOperationAliases {
		descriptor, ok := descriptors[nodeType]
		if !ok {
			continue
		}
		for resource, byOperation := range byResource {
			for operation, alias := range byOperation {
				if _, ok := descriptor.Operations[alias]; !ok {
					t.Errorf("%s %s/%s aliases to missing descriptor operation %q", nodeType, resource, operation, alias)
				}
			}
		}
	}
}

func TestOperationNameKeepsNativeOperation(t *testing.T) {
	t.Parallel()

	executor := Executor{descriptor: Descriptor{
		NodeType: "n8n-nodes-base.slack",
		Operations: map[string]Operation{
			"post":        {Name: "post", Method: "GET", Path: "/native"},
			"postMessage": {Name: "postMessage", Method: "GET", Path: "/alias"},
		},
	}}
	got := executor.operationName(map[string]any{"resource": "message", "operation": "post"})
	if got != "post" {
		t.Fatalf("operationName() = %q, want native operation", got)
	}
}

func TestOfficialParamAliases(t *testing.T) {
	t.Parallel()

	params := withOfficialParamAliases("n8n-nodes-base.airtable", map[string]any{
		"base":  map[string]any{"mode": "list", "value": "app123"},
		"table": map[string]any{"mode": "list", "value": "tbl123"},
	})
	baseID, ok := descriptorParamValue(params, Param{Name: "baseId"})
	if !ok || baseID != "app123" {
		t.Fatalf("baseId = %#v, %v; want app123", baseID, ok)
	}
	tableID, ok := descriptorParamValue(params, Param{Name: "tableIdOrName"})
	if !ok || tableID != "tbl123" {
		t.Fatalf("tableIdOrName = %#v, %v; want tbl123", tableID, ok)
	}
}

func TestAirtableRuntimeSupportsOriginalOperations(t *testing.T) {
	t.Parallel()

	got := originalDescriptorOperations(t, "n8n-nodes-base.airtable")
	want := map[string][]string{
		"base":   {"getMany", "getSchema"},
		"record": {"create", "deleteRecord", "get", "search", "update", "upsert"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Airtable original operations changed or runtime coverage is stale\n got: %#v\nwant: %#v", got, want)
	}

	descriptor, ok := descriptorByType("n8n-nodes-base.airtable")
	if !ok {
		t.Fatal("airtable descriptor is unavailable")
	}
	executor := Executor{descriptor: descriptor}
	for resource, operations := range got {
		for _, operation := range operations {
			resolved := executor.operationName(map[string]any{"resource": resource, "operation": operation})
			if _, ok := descriptor.Operations[resolved]; !ok {
				t.Errorf("airtable %s/%s resolves to missing descriptor operation %q", resource, operation, resolved)
			}
		}
	}
}

func TestGitHubOfficialParamAliases(t *testing.T) {
	t.Parallel()

	params := withOfficialParamAliases("n8n-nodes-base.github", map[string]any{
		"owner":             map[string]any{"value": "octo"},
		"repository":        map[string]any{"value": "hello"},
		"issueNumber":       7,
		"commitMessage":     "update file",
		"fileContent":       "hello",
		"releaseTag":        "v1",
		"pullRequestNumber": 3,
		"reviewId":          "42",
		"workflowId":        map[string]any{"value": "ci.yml"},
	})
	for _, tt := range []struct {
		param string
		want  any
	}{
		{"owner", "octo"},
		{"repo", "hello"},
		{"issue_number", 7},
		{"message", "update file"},
		{"content", "hello"},
		{"tag_name", "v1"},
		{"pull_request_number", 3},
		{"review_id", "42"},
		{"workflow_id", "ci.yml"},
	} {
		got, ok := descriptorParamValue(params, Param{Name: tt.param})
		if !ok || got != tt.want {
			t.Fatalf("%s = %#v, %v; want %#v", tt.param, got, ok, tt.want)
		}
	}
}

func TestGitHubRuntimeSupportsOriginalOperations(t *testing.T) {
	t.Parallel()

	got := originalDescriptorOperations(t, "n8n-nodes-base.github")
	want := map[string][]string{
		"file":         {"create", "delete", "edit", "get", "list"},
		"issue":        {"create", "createComment", "edit", "get", "lock"},
		"organization": {"getRepositories"},
		"release":      {"create", "delete", "get", "getAll", "update"},
		"repository": {
			"get", "getIssues", "getLicense", "getProfile", "getPullRequests", "listPopularPaths", "listReferrers",
		},
		"review":   {"create", "get", "getAll", "update"},
		"user":     {"getRepositories", "getUserIssues", "invite"},
		"workflow": {"disable", "dispatch", "dispatchAndWait", "enable", "get", "getUsage", "list"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GitHub original operations changed or runtime coverage is stale\n got: %#v\nwant: %#v", got, want)
	}

	descriptor, ok := descriptorByType("n8n-nodes-base.github")
	if !ok {
		t.Fatal("github descriptor is unavailable")
	}
	executor := Executor{descriptor: descriptor}
	for resource, operations := range got {
		for _, operation := range operations {
			resolved := executor.operationName(map[string]any{"resource": resource, "operation": operation})
			if _, ok := descriptor.Operations[resolved]; !ok {
				t.Errorf("github %s/%s resolves to missing descriptor operation %q", resource, operation, resolved)
			}
		}
	}
}

func TestTemplateValuesUseResourceLocatorValue(t *testing.T) {
	t.Parallel()

	got := replaceTemplateValues("/repos/{{.owner}}/{{.repo}}", map[string]any{
		"owner": map[string]any{"value": "octo org"},
		"repo":  map[string]any{"value": "hello/world"},
	}, nil)
	if got != "/repos/octo%20org/hello%2Fworld" {
		t.Fatalf("template = %q", got)
	}
}

func TestGmailEmailTypeMapsToIsHTML(t *testing.T) {
	t.Parallel()

	params := withOfficialParamAliases("n8n-nodes-base.gmail", map[string]any{
		"sendTo":    "ana@example.test",
		"message":   "<b>Salut</b>",
		"emailType": "html",
	})
	if params["to"] != "ana@example.test" || params["body"] != "<b>Salut</b>" || params["isHtml"] != true {
		t.Fatalf("unexpected gmail params: %#v", params)
	}
}

func TestGmailRuntimeSupportsOriginalOperations(t *testing.T) {
	t.Parallel()

	got := originalDescriptorOperations(t, "n8n-nodes-base.gmail")
	want := map[string][]string{
		"draft":   {"create", "delete", "get", "getAll"},
		"label":   {"create", "delete", "get", "getAll"},
		"message": {"addLabels", "delete", "get", "getAll", "markAsRead", "markAsUnread", "removeLabels", "reply", "send", "sendAndWait"},
		"thread":  {"addLabels", "delete", "get", "getAll", "removeLabels", "reply", "trash", "untrash"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Gmail original operations changed or runtime coverage is stale\n got: %#v\nwant: %#v", got, want)
	}

	descriptor, ok := descriptorByType("n8n-nodes-base.gmail")
	if !ok {
		t.Fatal("gmail descriptor is unavailable")
	}
	executor := Executor{descriptor: descriptor}
	for resource, operations := range got {
		for _, operation := range operations {
			resolved := executor.operationName(map[string]any{"resource": resource, "operation": operation})
			if _, ok := descriptor.Operations[resolved]; !ok {
				t.Errorf("gmail %s/%s resolves to missing descriptor operation %q", resource, operation, resolved)
			}
		}
	}
}

func TestGmailOfficialOptionsAliases(t *testing.T) {
	t.Parallel()

	params := withOfficialParamAliases("n8n-nodes-base.gmail", map[string]any{
		"resource":  "thread",
		"operation": "addLabels",
		"labelIds":  []any{"STARRED"},
		"filters": map[string]any{
			"q":                "from:ana@example.test",
			"includeSpamTrash": true,
		},
		"options": map[string]any{
			"ccList":    "cc@example.test",
			"bccList":   "bcc@example.test",
			"fromAlias": "me@example.test",
			"replyTo":   "reply@example.test",
			"sendTo":    "ana@example.test",
		},
		"limit": 12,
	})
	if params["addLabelIds"] == nil || params["q"] != "from:ana@example.test" || params["includeSpamTrash"] != true || params["maxResults"] != 12 {
		t.Fatalf("gmail filter/label params = %#v", params)
	}
	if params["to"] != "ana@example.test" || params["cc"] != "cc@example.test" || params["bcc"] != "bcc@example.test" || params["from"] != "me@example.test" || params["replyTo"] != "reply@example.test" {
		t.Fatalf("gmail email option params = %#v", params)
	}
}

func TestGoogleSheetsRuntimeSupportsOriginalOperations(t *testing.T) {
	t.Parallel()

	got := originalDescriptorOperations(t, "n8n-nodes-base.googleSheets")
	want := map[string][]string{
		"sheet":       {"append", "appendOrUpdate", "clear", "create", "delete", "read", "remove", "update"},
		"spreadsheet": {"create", "deleteSpreadsheet"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Google Sheets original operations changed or runtime coverage is stale\n got: %#v\nwant: %#v", got, want)
	}

	descriptor, ok := descriptorByType("n8n-nodes-base.googleSheets")
	if !ok {
		t.Fatal("google sheets descriptor is unavailable")
	}
	executor := Executor{descriptor: descriptor}
	for resource, operations := range got {
		for _, operation := range operations {
			resolved := executor.operationName(map[string]any{"resource": resource, "operation": operation})
			if _, ok := descriptor.Operations[resolved]; !ok {
				t.Errorf("google sheets %s/%s resolves to missing descriptor operation %q", resource, operation, resolved)
			}
		}
	}
}

func TestGoogleSheetsOfficialParamAliases(t *testing.T) {
	t.Parallel()

	params := withOfficialParamAliases("n8n-nodes-base.googleSheets", map[string]any{
		"documentId": map[string]any{"value": "spreadsheet-1"},
		"sheetName":  map[string]any{"value": "Orders"},
		"columns": map[string]any{
			"value": map[string]any{"status": "paid"},
		},
	})
	spreadsheetID, ok := descriptorParamValue(params, Param{Name: "spreadsheetId"})
	if !ok || spreadsheetID != "spreadsheet-1" || params["range"] != "Orders!A:Z" {
		t.Fatalf("sheets params = %#v", params)
	}
	objects, ok := params["objects"].([]any)
	if !ok || len(objects) != 1 {
		t.Fatalf("sheets objects = %#v", params["objects"])
	}
}

func TestGoogleSheetsDeleteSpreadsheetUsesDriveURL(t *testing.T) {
	t.Parallel()

	executor := Executor{descriptor: Descriptor{BaseURL: "https://sheets.googleapis.com/v4/spreadsheets"}}
	endpoint, err := executor.endpoint(Operation{
		Path: "https://www.googleapis.com/drive/v3/files/{{.spreadsheetId}}",
		Params: []Param{
			{Name: "spreadsheetId", In: "path", Required: true, Type: "string"},
		},
	}, map[string]any{"spreadsheetId": "abc 123"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if endpoint != "https://www.googleapis.com/drive/v3/files/abc%20123" {
		t.Fatalf("endpoint = %q", endpoint)
	}
}

func TestRemainingDescriptorNodesSupportOriginalOperations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		nodeType string
		want     map[string][]string
	}{
		{"n8n-nodes-base.jira", map[string][]string{
			"issue":           {"changelog", "create", "delete", "get", "getAll", "notify", "transitions", "update"},
			"issueAttachment": {"add", "get", "getAll", "remove"},
			"issueComment":    {"add", "get", "getAll", "remove", "update"},
			"user":            {"create", "delete", "get"},
		}},
		{"n8n-nodes-base.hubspot", map[string][]string{
			"company":     {"create", "delete", "get", "getAll", "getRecentlyCreatedUpdated", "searchByDomain", "update"},
			"contact":     {"delete", "get", "getAll", "getRecentlyCreatedUpdated", "search", "upsert"},
			"contactList": {"add", "remove"},
			"deal":        {"create", "delete", "get", "getAll", "getRecentlyCreatedUpdated", "search", "update"},
			"engagement":  {"create", "delete", "get", "getAll"},
			"ticket":      {"create", "delete", "get", "getAll", "update"},
		}},
		{"n8n-nodes-base.notion", map[string][]string{
			"block":        {"append", "getAll"},
			"database":     {"get", "get", "getAll", "getAll", "search"},
			"databasePage": {"create", "create", "get", "getAll", "getAll", "update", "update"},
			"page":         {"archive", "create", "create", "get", "search", "search"},
			"user":         {"get", "getAll"},
		}},
		{"n8n-nodes-base.openAi", map[string][]string{
			"chat":  {"complete"},
			"image": {"create"},
			"text":  {"complete", "edit", "moderate"},
		}},
		{"n8n-nodes-base.stripe", map[string][]string{
			"balance":      {"get"},
			"charge":       {"create", "get", "getAll", "update"},
			"coupon":       {"create", "getAll"},
			"customer":     {"create", "delete", "get", "getAll", "update"},
			"customerCard": {"add", "get", "remove"},
			"meterEvent":   {"create"},
			"source":       {"create", "delete", "get"},
			"token":        {"create"},
		}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.nodeType, func(t *testing.T) {
			t.Parallel()
			got := originalDescriptorOperations(t, tt.nodeType)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("%s original operations changed or runtime coverage is stale\n got: %#v\nwant: %#v", tt.nodeType, got, tt.want)
			}
			descriptor, ok := descriptorByType(tt.nodeType)
			if !ok {
				t.Fatalf("%s descriptor is unavailable", tt.nodeType)
			}
			executor := Executor{descriptor: descriptor}
			for resource, operations := range got {
				for _, operation := range operations {
					resolved := executor.operationName(map[string]any{"resource": resource, "operation": operation})
					if _, ok := descriptor.Operations[resolved]; !ok {
						t.Errorf("%s %s/%s resolves to missing descriptor operation %q", tt.nodeType, resource, operation, resolved)
					}
				}
			}
		})
	}
}

func TestSlackOfficialParamAliases(t *testing.T) {
	t.Parallel()

	params := withOfficialParamAliases("n8n-nodes-base.slack", map[string]any{
		"channelId": map[string]any{"value": "C123"},
		"fileId":    "F123",
		"userId":    "U123",
	})
	for _, tt := range []struct {
		param string
		want  string
	}{
		{"channel", "C123"},
		{"file", "F123"},
		{"user", "U123"},
	} {
		got, ok := descriptorParamValue(params, Param{Name: tt.param})
		if !ok || got != tt.want {
			t.Fatalf("%s = %#v, %v; want %s", tt.param, got, ok, tt.want)
		}
	}
}

func TestSlackRuntimeSupportsOriginalOperations(t *testing.T) {
	t.Parallel()

	got := originalDescriptorOperations(t, "n8n-nodes-base.slack")
	want := map[string][]string{
		"channel": {
			"archive", "close", "create", "get", "getAll", "history", "invite", "join", "kick", "leave", "member", "open", "rename", "replies", "setPurpose", "setTopic", "unarchive",
		},
		"file":     {"get", "getAll", "upload"},
		"message":  {"delete", "getPermalink", "post", "search", "sendAndWait", "update"},
		"reaction": {"add", "get", "remove"},
		"star":     {"add", "delete", "getAll"},
		"user":     {"getAll", "getPresence", "getProfile", "info", "updateProfile"},
		"userGroup": {
			"create", "disable", "enable", "getAll", "getUsers", "update", "updateUsers",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Slack original operations changed or runtime coverage is stale\n got: %#v\nwant: %#v", got, want)
	}

	descriptor, ok := descriptorByType("n8n-nodes-base.slack")
	if !ok {
		t.Fatal("slack descriptor is unavailable")
	}
	executor := Executor{descriptor: descriptor}
	for resource, operations := range got {
		for _, operation := range operations {
			resolved := executor.operationName(map[string]any{"resource": resource, "operation": operation})
			if _, ok := descriptor.Operations[resolved]; !ok {
				t.Errorf("slack %s/%s resolves to missing descriptor operation %q", resource, operation, resolved)
			}
		}
	}
}

func TestSlackOfficialOptionsAliases(t *testing.T) {
	t.Parallel()

	open := withOfficialParamAliases("n8n-nodes-base.slack", map[string]any{
		"resource":  "channel",
		"operation": "open",
		"options": map[string]any{
			"users":    "U1,U2",
			"returnIm": true,
		},
	})
	if open["users"] != "U1,U2" || open["return_im"] != true {
		t.Fatalf("channel open params = %#v", open)
	}

	sendAndWait := withOfficialParamAliases("n8n-nodes-base.slack", map[string]any{
		"resource":  "message",
		"operation": "sendAndWait",
		"select":    "user",
		"user":      map[string]any{"value": "U1"},
		"message":   "approve?",
	})
	if !reflect.DeepEqual(sendAndWait["channel"], map[string]any{"value": "U1"}) || sendAndWait["text"] != "approve?" {
		t.Fatalf("sendAndWait params = %#v", sendAndWait)
	}

	search := withOfficialParamAliases("n8n-nodes-base.slack", map[string]any{
		"resource":  "message",
		"operation": "search",
		"query":     "hello",
		"limit":     25,
	})
	if search["count"] != 25 {
		t.Fatalf("search params = %#v", search)
	}
}

func descriptorByType(nodeType string) (Descriptor, bool) {
	for _, descriptor := range Builtins() {
		descriptor.Normalize()
		if descriptor.NodeType == nodeType {
			return descriptor, true
		}
	}
	return Descriptor{}, false
}

func originalDescriptorOperations(t *testing.T, nodeType string) map[string][]string {
	t.Helper()

	node := originalDescriptorNode(t, nodeType)
	if node == nil {
		t.Fatalf("%s original metadata is unavailable", nodeType)
	}
	properties, ok := node["properties"].([]any)
	if !ok {
		t.Fatalf("%s metadata has no properties", nodeType)
	}
	result := map[string][]string{}
	for _, raw := range properties {
		prop, ok := raw.(map[string]any)
		if !ok || prop["name"] != "operation" {
			continue
		}
		display, _ := prop["displayOptions"].(map[string]any)
		show, _ := display["show"].(map[string]any)
		options, _ := prop["options"].([]any)
		for _, resource := range descriptorStringList(show["resource"]) {
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

func originalDescriptorNode(t *testing.T, nodeType string) map[string]any {
	t.Helper()

	data, err := os.ReadFile("../metadata/original_nodes_generated.go")
	if err != nil {
		t.Fatalf("read original metadata: %v", err)
	}
	line := ""
	for _, candidate := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(candidate, "const originalNodeDescriptionsJSON = ") {
			line = strings.TrimSpace(strings.TrimPrefix(candidate, "const originalNodeDescriptionsJSON = "))
			break
		}
	}
	if line == "" {
		t.Fatal("originalNodeDescriptionsJSON const not found")
	}
	raw, err := strconv.Unquote(line)
	if err != nil {
		t.Fatalf("unquote original metadata: %v", err)
	}
	var nodes map[string]map[string]any
	if err := json.Unmarshal([]byte(raw), &nodes); err != nil {
		t.Fatalf("decode original metadata: %v", err)
	}
	return nodes[nodeType]
}

func descriptorStringList(value any) []string {
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
