package descriptor

import "testing"

func TestGitHubCreateFileBodyEncodesOfficialContent(t *testing.T) {
	t.Parallel()

	body, ok, err := githubRequestBody(Operation{Name: "createOrUpdateFile"}, map[string]any{
		"message":     "add readme",
		"content":     "hello",
		"fileContent": "hello",
	})
	if err != nil {
		t.Fatalf("githubRequestBody: %v", err)
	}
	if !ok || body["message"] != "add readme" || body["content"] != "aGVsbG8=" {
		t.Fatalf("body = %#v, %v", body, ok)
	}
}

func TestGitHubCreateIssueBodyFlattensOfficialCollections(t *testing.T) {
	t.Parallel()

	body, ok, err := githubRequestBody(Operation{Name: "createIssue"}, map[string]any{
		"title":     "Bug",
		"labels":    []any{map[string]any{"label": "bug"}},
		"assignees": []any{map[string]any{"assignee": "octo"}},
	})
	if err != nil {
		t.Fatalf("githubRequestBody: %v", err)
	}
	labels := body["labels"].([]string)
	assignees := body["assignees"].([]string)
	if !ok || labels[0] != "bug" || assignees[0] != "octo" {
		t.Fatalf("body = %#v, %v", body, ok)
	}
}

func TestGitHubCreateReviewBodyMatchesAPI(t *testing.T) {
	t.Parallel()

	body, ok, err := githubRequestBody(Operation{Name: "createReview"}, map[string]any{
		"event":            "requestChanges",
		"body":             "Please update",
		"additionalFields": map[string]any{"commit_id": "abc"},
	})
	if err != nil {
		t.Fatalf("githubRequestBody: %v", err)
	}
	if !ok || body["event"] != "REQUEST_CHANGES" || body["body"] != "Please update" || body["commit_id"] != "abc" {
		t.Fatalf("body = %#v, %v", body, ok)
	}
}

func TestGitHubDispatchWorkflowBodyParsesInputs(t *testing.T) {
	t.Parallel()

	body, ok, err := githubRequestBody(Operation{Name: "dispatchWorkflow"}, map[string]any{
		"ref":    "main",
		"inputs": `{"name":"Ana"}`,
	})
	if err != nil {
		t.Fatalf("githubRequestBody: %v", err)
	}
	inputs, _ := body["inputs"].(map[string]any)
	if !ok || body["ref"] != "main" || inputs["name"] != "Ana" {
		t.Fatalf("body = %#v, %v", body, ok)
	}
}

func TestGitHubUpdateReleaseUsesAdditionalFieldsAsBody(t *testing.T) {
	t.Parallel()

	body, ok, err := githubRequestBody(Operation{Name: "updateRelease"}, map[string]any{
		"additionalFields": map[string]any{"name": "v1"},
	})
	if err != nil {
		t.Fatalf("githubRequestBody: %v", err)
	}
	if !ok || body["name"] != "v1" {
		t.Fatalf("body = %#v, %v", body, ok)
	}
}
