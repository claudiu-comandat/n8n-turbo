package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/persistence"
)

func TestLastSuccessfulExecutionUsesFrontendExecutionShape(t *testing.T) {
	t.Parallel()

	server := &Server{executionStore: fakeExecutionStore{
		rows: []persistence.ExecutionRow{{
			ID:           "exec-1",
			WorkflowID:   "workflow-1",
			Status:       "success",
			Mode:         "manual",
			StartedAt:    time.Now().UTC(),
			WorkflowData: json.RawMessage(`{}`),
			Data:         json.RawMessage(`{}`),
			CreatedAt:    time.Now().UTC(),
		}},
	}}
	request := httptest.NewRequest(http.MethodGet, "/rest/workflows/workflow-1/executions/last-successful", nil)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("id", "workflow-1")
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))
	recorder := httptest.NewRecorder()

	server.handleLastSuccessfulWorkflowExecution(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	rawData, ok := response.Data["data"].(string)
	if !ok {
		t.Fatalf("execution data should use frontend flatted string shape, got %#v", response.Data["data"])
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(rawData), &root); err != nil {
		t.Fatal(err)
	}
	resultData := root["resultData"].(map[string]any)
	if _, ok := resultData["runData"].(map[string]any); !ok {
		t.Fatalf("resultData.runData missing: %#v", resultData)
	}
}

type fakeExecutionStore struct {
	rows []persistence.ExecutionRow
}

func (f fakeExecutionStore) Init(context.Context) error { return nil }

func (f fakeExecutionStore) Create(context.Context, dataplane.Workflow, string) (*persistence.ExecutionRow, error) {
	return nil, persistence.ErrNotFound
}

func (f fakeExecutionStore) Finish(context.Context, string, string, time.Time, dataplane.RunExecutionData) error {
	return persistence.ErrNotFound
}

func (f fakeExecutionStore) MarkWaiting(context.Context, string, time.Time, dataplane.RunExecutionData) error {
	return persistence.ErrNotFound
}

func (f fakeExecutionStore) ListDueWaiting(context.Context, time.Time, int) ([]persistence.ExecutionRow, error) {
	return nil, nil
}

func (f fakeExecutionStore) GetByID(context.Context, string) (*persistence.ExecutionRow, error) {
	return nil, persistence.ErrNotFound
}

func (f fakeExecutionStore) List(context.Context, string, int) ([]persistence.ExecutionRow, error) {
	return f.rows, nil
}

func (f fakeExecutionStore) Delete(context.Context, string) error { return persistence.ErrNotFound }

func (f fakeExecutionStore) DeleteOlderThan(context.Context, time.Time) (int, error) { return 0, nil }

func (f fakeExecutionStore) PrunePerWorkflow(context.Context, int) (int, error) { return 0, nil }
