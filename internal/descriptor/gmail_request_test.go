package descriptor

import "testing"

func TestGmailCreateDraftBodyWrapsRawMessage(t *testing.T) {
	t.Parallel()

	body, ok, err := gmailRequestBody(Operation{Name: "createDraft"}, map[string]any{
		"subject":  "Draft",
		"body":     "Salut",
		"isHtml":   false,
		"threadId": "thread-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("gmailRequestBody did not handle createDraft")
	}
	message, ok := body["message"].(map[string]any)
	if !ok || message["raw"] == "" || message["threadId"] != "thread-1" {
		t.Fatalf("draft body = %#v", body)
	}
}
