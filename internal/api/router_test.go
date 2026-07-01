package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRateLimitExemptPath(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/assets/app.js",
		"/static/n8n-turbo-compat.js",
		"/schemas/n8n-nodes-base.postgres/2.6.0/database/executeQuery.json",
		"/rest/push",
		"/push",
		"/healthz",
	} {
		if !rateLimitExemptPath(path) {
			t.Fatalf("%s should be exempt from rate limiting", path)
		}
	}

	if rateLimitExemptPath("/rest/projects/personal") {
		t.Fatal("API routes should stay rate limited")
	}
}

func TestHandleNodeSchemaReturnsEmptyJSON(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	(&Server{}).handleNodeSchema(recorder, httptest.NewRequest(http.MethodGet, "/schemas/n8n-nodes-base.postgres/2.6.0/database/executeQuery.json", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if recorder.Body.String() != "{}\n" {
		t.Fatalf("schema body = %q, want empty object", recorder.Body.String())
	}
}
