package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/n8n-io/n8n-turbo/internal/auth"
	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
	"github.com/n8n-io/n8n-turbo/internal/flatted"
	"github.com/n8n-io/n8n-turbo/internal/metadata"
	"github.com/n8n-io/n8n-turbo/internal/persistence"
)

type workflowRunRequest struct {
	DestinationNode json.RawMessage             `json:"destinationNode"`
	TriggerToStart  json.RawMessage             `json:"triggerToStartFrom"`
	WorkflowData    *dataplane.Workflow         `json:"workflowData,omitempty"`
	PinData         map[string][]dataplane.Item `json:"pinData,omitempty"`
	RunData         dataplane.RunData           `json:"runData,omitempty"`
	StartNodes      []workflowStartNode         `json:"startNodes,omitempty"`
	DirtyNodeNames  []string                    `json:"dirtyNodeNames,omitempty"`
	PushRef         string                      `json:"pushRef,omitempty"`
}

type workflowStartNode struct {
	Name string `json:"name"`
}

func (s *Server) handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	if s.usesFolderAwareList(r) {
		s.handleListWorkflowsWithFolders(w, r)
		return
	}
	limit := queryInt(r, "limit", 100)
	if pageStore, ok := s.workflowStore.(workflowPageStore); ok {
		before, beforeID, err := parseTimeIDCursor(r.URL.Query().Get("cursor"), "workflow")
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		page, err := pageStore.ListPage(r.Context(), limit, before, beforeID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		page.Rows = filterWorkflowRows(page.Rows, r)
		page.Rows = s.filterWorkflowRowsByTag(r.Context(), page.Rows, r)
		s.decorateWorkflowRowsForFrontend(r.Context(), page.Rows)
		response := map[string]any{"data": page.Rows}
		if page.NextCursor != "" {
			response["nextCursor"] = encodeTimeIDCursor(page.NextCursor)
		}
		writeJSON(w, http.StatusOK, response)
		return
	}
	workflows, err := s.workflowStore.List(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	workflows = filterWorkflowRows(workflows, r)
	workflows = s.filterWorkflowRowsByTag(r.Context(), workflows, r)
	s.decorateWorkflowRowsForFrontend(r.Context(), workflows)
	writeJSON(w, http.StatusOK, map[string]any{"data": workflows})
}

type workflowPageStore interface {
	ListPage(ctx context.Context, limit int, before time.Time, beforeID string) (persistence.WorkflowPage, error)
}

type executionPageStore interface {
	ListPage(ctx context.Context, workflowID string, limit int, before time.Time, beforeID string) (persistence.ExecutionPage, error)
}

type executionRetryStore interface {
	SetRetryOf(ctx context.Context, id string, retryOf string) error
}

func (s *Server) handleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "new" {
		workflow := s.newWorkflowPlaceholder(r)
		writeJSON(w, http.StatusOK, map[string]any{"data": workflow})
		return
	}
	if id == "demo" {
		workflow := s.newWorkflowPlaceholder(r)
		workflow.ID = "demo"
		workflow.Name = "Demo workflow"
		writeJSON(w, http.StatusOK, map[string]any{"data": workflow})
		return
	}
	row, err := s.workflowStore.GetByID(r.Context(), id)
	if err != nil {
		status := http.StatusInternalServerError
		if err == persistence.ErrNotFound {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	workflow, err := workflowFromRow(row)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.decorateWorkflowForFrontend(r, &workflow)
	writeJSON(w, http.StatusOK, map[string]any{"data": workflow})
}

func (s *Server) handleWorkflowExists(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "demo" {
		writeJSON(w, http.StatusOK, map[string]any{
			"exists": true,
			"data": map[string]any{
				"exists": true,
			},
		})
		return
	}
	_, err := s.workflowStore.GetByID(r.Context(), id)
	exists := err == nil
	if err != nil && err != persistence.ErrNotFound {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"exists": exists,
		"data": map[string]any{
			"exists": exists,
		},
	})
}

func (s *Server) handleSaveWorkflow(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workflow body")
		return
	}
	var workflow dataplane.Workflow
	if err := json.Unmarshal(body, &workflow); err != nil {
		writeError(w, http.StatusBadRequest, "invalid workflow body")
		return
	}
	if id := chi.URLParam(r, "id"); id != "" {
		workflow.ID = id
	}
	// Partial update: a PATCH may send only a few fields (e.g. n8n's "move to folder"
	// sends {parentFolderId, versionId}). Restore every field the body omitted from the
	// stored workflow so a metadata patch never wipes nodes/connections/name.
	if workflow.ID != "" {
		existing, err := s.workflowStore.GetByID(r.Context(), workflow.ID)
		if err == nil {
			existingWorkflow, convErr := workflowFromRow(existing)
			if convErr == nil {
				if !jsonObjectHasKey(body, "name") {
					workflow.Name = existingWorkflow.Name
				}
				if !jsonObjectHasKey(body, "nodes") {
					workflow.Nodes = existingWorkflow.Nodes
				}
				if !jsonObjectHasKey(body, "connections") {
					workflow.Connections = existingWorkflow.Connections
				}
				if !jsonObjectHasKey(body, "settings") {
					workflow.Settings = existingWorkflow.Settings
				}
				if !jsonObjectHasKey(body, "staticData") {
					workflow.StaticData = existingWorkflow.StaticData
				}
				if !jsonObjectHasKey(body, "pinData") {
					workflow.PinData = existingWorkflow.PinData
				}
				if !jsonObjectHasKey(body, "meta") {
					workflow.Meta = existingWorkflow.Meta
				}
				if !jsonObjectHasKey(body, "active") {
					workflow.Active = existingWorkflow.Active
				}
			}
		} else if err != persistence.ErrNotFound {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if workflow.Name == "" {
		workflow.Name = "My workflow"
	}
	normalizeWorkflowDefaults(&workflow)
	normalizeNodeDefaults(workflow.Nodes)
	if err := validateWorkflow(workflow); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	user, _ := auth.UserFromContext(r.Context())
	saved, err := s.workflowStore.Save(r.Context(), workflow, user.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if rawTags, ok := workflow.Raw["tags"]; ok && s.tagStore != nil {
		if tagIDs, tagErr := s.resolveTagPayload(r.Context(), rawTags); tagErr == nil {
			_ = s.tagStore.SetWorkflowTags(r.Context(), saved.ID, tagIDs)
		}
	}
	if rawParent, ok := workflow.Raw["parentFolderId"]; ok {
		s.applyWorkflowParentFolder(r, saved.ID, rawParent)
	}
	savedWorkflow, err := workflowFromRow(saved)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.decorateWorkflowForFrontend(r, &savedWorkflow)
	writeJSON(w, http.StatusOK, map[string]any{"data": savedWorkflow})
}

func (s *Server) handleDeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s.stopWorkflowSchedule(id)
	if err := s.workflowStore.Delete(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	if s.tagStore != nil {
		_ = s.tagStore.SetWorkflowTags(r.Context(), id, nil)
	}
	if s.webhookStore != nil {
		_ = s.webhookStore.DeleteByWorkflow(r.Context(), id)
	}
	s.pushWorkflowDeactivated(id)
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"deleted": true, "id": id}})
}

func (s *Server) handleDuplicateWorkflow(w http.ResponseWriter, r *http.Request) {
	row, err := s.workflowStore.GetByID(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	workflow, err := workflowFromRow(row)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var payload struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	workflow.ID = ""
	workflow.Active = false
	workflow.VersionID = ""
	workflow.CreatedAt = nil
	workflow.UpdatedAt = nil
	if strings.TrimSpace(payload.Name) != "" {
		workflow.Name = payload.Name
	} else {
		workflow.Name = workflow.Name + " copy"
	}
	user, _ := auth.UserFromContext(r.Context())
	saved, err := s.workflowStore.Save(r.Context(), workflow, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": saved})
}

func (s *Server) handleExportWorkflow(w http.ResponseWriter, r *http.Request) {
	row, err := s.workflowStore.GetByID(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	workflow, err := workflowFromRow(row)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": workflow})
}

func (s *Server) handleLastSuccessfulWorkflowExecution(w http.ResponseWriter, r *http.Request) {
	workflowID := chi.URLParam(r, "id")
	executions, err := s.executionStore.List(r.Context(), workflowID, 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, execution := range executions {
		if execution.Status == "success" {
			writeJSON(w, http.StatusOK, map[string]any{"data": lastSuccessfulExecutionResponse(execution)})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": nil})
}

func (s *Server) handleWorkflowDependenciesCounts(w http.ResponseWriter, r *http.Request) {
	var payload map[string]any
	_ = json.NewDecoder(r.Body).Decode(&payload)
	writeJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"counts": map[string]any{},
		},
	})
}

func (s *Server) handleImportWorkflow(w http.ResponseWriter, r *http.Request) {
	var payload map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid workflow import body")
		return
	}
	raw := payload["data"]
	if len(raw) == 0 {
		raw = payload["workflow"]
	}
	if len(raw) == 0 {
		raw, _ = json.Marshal(payload)
	}
	var workflow dataplane.Workflow
	if err := json.Unmarshal(raw, &workflow); err != nil {
		writeError(w, http.StatusBadRequest, "invalid workflow payload")
		return
	}
	if workflow.Name == "" {
		workflow.Name = "Imported workflow"
	}
	normalizeWorkflowDefaults(&workflow)
	normalizeNodeDefaults(workflow.Nodes)
	if err := validateWorkflow(workflow); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	workflow.ID = ""
	workflow.Active = false
	user, _ := auth.UserFromContext(r.Context())
	saved, err := s.workflowStore.Save(r.Context(), workflow, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": saved})
}

func (s *Server) handleActivateWorkflow(w http.ResponseWriter, r *http.Request) {
	s.setWorkflowActive(w, r, true)
}

func (s *Server) handleDeactivateWorkflow(w http.ResponseWriter, r *http.Request) {
	s.setWorkflowActive(w, r, false)
}

func (s *Server) setWorkflowActive(w http.ResponseWriter, r *http.Request, active bool) {
	id := chi.URLParam(r, "id")
	if err := s.workflowStore.SetActive(r.Context(), id, active); err != nil {
		writeStoreError(w, err)
		return
	}
	row, err := s.workflowStore.GetByID(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if active {
		workflow, err := workflowFromRow(row)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.startWorkflowSchedule(s.runtimeCtx, workflow)
		s.pushWorkflowActivated(workflow.ID, workflow.Name)
	} else {
		s.stopWorkflowSchedule(id)
		s.pushWorkflowDeactivated(id)
	}
	updated, err := workflowFromRow(row)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.decorateWorkflowForFrontend(r, &updated)
	writeJSON(w, http.StatusOK, map[string]any{"data": updated})
}

func (s *Server) handleRunWorkflow(w http.ResponseWriter, r *http.Request) {
	request, err := decodeWorkflowRunRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	workflow, err := s.manualRunWorkflow(r, request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	execution, err := s.executionStore.Create(r.Context(), workflow, "manual")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	destination, err := parseDestinationNode(request.DestinationNode)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	variables, err := s.resolvedVariables(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	secrets, err := s.resolvedSecretsRequest(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	pinData := request.effectivePinData(workflow)
	pushRef := request.pushRef(r)
	onNodeAfter := s.pushNodeAfter
	onFinished := s.pushExecutionFinished
	if pushRef != "" {
		onNodeAfter = func(event engine.NodeAfterEvent) {
			s.pushNodeAfterToSession(pushRef, event)
		}
		onFinished = func(event engine.ExecutionFinishedEvent) {
			s.pushExecutionFinishedToSession(pushRef, event)
		}
	}
	dispatchResult := s.dispatchWorkflowSync(r.Context(), executionDispatchRequest{
		ExecutionID: execution.ID,
		Workflow:    workflow,
		Mode:        "manual",
		Options: engine.ExecuteOptions{
			Destination: destination,
			Variables:   variables,
			Secrets:     secrets,
			BinaryStore: s.binaryStore,
			Credentials: s.resolveNodeCredentials,
			TriggerNode: request.triggerNodeName(),
			StartNodes:  request.startNodeNames(),
			RunData:     request.RunData,
			PinData:     pinData,
			OnStarted:   s.pushExecutionStarted,
			OnNodeAfter: onNodeAfter,
			OnFinished:  onFinished,
		},
		StartData: request.startData(destination),
		PinData:   pinData,
		PushRef:   pushRef,
		ErrorName: "WorkflowExecutionError",
	})
	if dispatchResult.StartErr != nil {
		writeError(w, http.StatusTooManyRequests, dispatchResult.StartErr.Error())
		return
	}
	if dispatchResult.StoreErr != nil {
		writeError(w, http.StatusInternalServerError, dispatchResult.StoreErr.Error())
		return
	}
	finished, err := s.executionStore.GetByID(r.Context(), execution.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": executionResponse(*finished)})
}

func (s *Server) handleListExecutions(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 100)
	filters := executionListFiltersFromRequest(r)
	workflowID := filters.workflowIDForStore()
	if pageStore, ok := s.executionStore.(executionPageStore); ok {
		before, beforeID, err := parseTimeIDCursor(r.URL.Query().Get("cursor"), "execution")
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		page, err := pageStore.ListPage(r.Context(), workflowID, limit, before, beforeID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		page.Rows = filterExecutionRows(page.Rows, filters)
		writeExecutionListResponse(w, page.Rows, page.NextCursor)
		return
	}
	executions, err := s.executionStore.List(r.Context(), workflowID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	executions = filterExecutionRows(executions, filters)
	writeExecutionListResponse(w, executions, "")
}

func (s *Server) handleGetExecution(w http.ResponseWriter, r *http.Request) {
	execution, err := s.executionStore.GetByID(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		status := http.StatusInternalServerError
		if err == persistence.ErrNotFound {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"data": executionResponse(*execution)})
}

func (s *Server) handleStopExecution(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !s.activeExecutions.Stop(id) {
		writeError(w, http.StatusNotFound, "execution is not active")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"stopped": true, "executionId": id}})
}

func (s *Server) handleDeleteExecution(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if s.activeExecutions != nil {
		_ = s.activeExecutions.Stop(id)
	}
	if err := s.executionStore.Delete(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"deleted": true, "id": id}})
}

func (s *Server) handleDeleteExecutions(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		IDs          []string `json:"ids"`
		DeleteBefore string   `json:"deleteBefore"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid execution delete body")
		return
	}
	deletedIDs := make([]string, 0, len(payload.IDs))
	deletedCount := 0
	for _, id := range payload.IDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if s.activeExecutions != nil {
			_ = s.activeExecutions.Stop(id)
		}
		if err := s.executionStore.Delete(r.Context(), id); err != nil {
			writeStoreError(w, err)
			return
		}
		deletedIDs = append(deletedIDs, id)
		deletedCount++
	}
	if strings.TrimSpace(payload.DeleteBefore) != "" {
		deleteBefore, err := time.Parse(time.RFC3339Nano, payload.DeleteBefore)
		if err != nil {
			if parsed, fallbackErr := time.Parse(time.RFC3339, payload.DeleteBefore); fallbackErr == nil {
				deleteBefore = parsed
			} else {
				writeError(w, http.StatusBadRequest, "deleteBefore must be an RFC3339 timestamp")
				return
			}
		}
		count, err := s.executionStore.DeleteOlderThan(r.Context(), deleteBefore)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		deletedCount += count
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"deleted": deletedCount,
			"ids":     deletedIDs,
		},
		"deleted": deletedCount,
		"ids":     deletedIDs,
	})
}

func (s *Server) handleRetryExecution(w http.ResponseWriter, r *http.Request) {
	previous, err := s.executionStore.GetByID(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var workflow dataplane.Workflow
	if err := json.Unmarshal(previous.WorkflowData, &workflow); err != nil {
		writeError(w, http.StatusBadRequest, "execution workflow data is invalid")
		return
	}
	retry, err := s.executionStore.Create(r.Context(), workflow, "retry")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if retryStore, ok := s.executionStore.(executionRetryStore); ok {
		if err := retryStore.SetRetryOf(r.Context(), retry.ID, previous.ID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		retry, err = s.executionStore.GetByID(r.Context(), retry.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if s.evaluator == nil || s.activeExecutions == nil {
		writeJSON(w, http.StatusOK, map[string]any{"data": executionResponse(*retry)})
		return
	}
	variables, _ := s.resolvedVariables(r)
	secrets, _ := s.resolvedSecretsRequest(r)
	pushRef := requestPushRef(r, "")
	onNodeAfter := s.pushNodeAfter
	onFinished := s.pushExecutionFinished
	if pushRef != "" {
		onNodeAfter = func(event engine.NodeAfterEvent) {
			s.pushNodeAfterToSession(pushRef, event)
		}
		onFinished = func(event engine.ExecutionFinishedEvent) {
			s.pushExecutionFinishedToSession(pushRef, event)
		}
	}
	dispatchResult := s.dispatchWorkflowSync(r.Context(), executionDispatchRequest{
		ExecutionID: retry.ID,
		Workflow:    workflow,
		Mode:        "retry",
		Options: engine.ExecuteOptions{
			Variables:   variables,
			Secrets:     secrets,
			BinaryStore: s.binaryStore,
			Credentials: s.resolveNodeCredentials,
			OnStarted:   s.pushExecutionStarted,
			OnNodeAfter: onNodeAfter,
			OnFinished:  onFinished,
		},
		StartData: map[string]any{"retryOf": previous.ID},
		PushRef:   pushRef,
		ErrorName: "RetryExecutionError",
	})
	if dispatchResult.StartErr != nil {
		writeError(w, http.StatusTooManyRequests, dispatchResult.StartErr.Error())
		return
	}
	if dispatchResult.StoreErr != nil {
		writeError(w, http.StatusInternalServerError, dispatchResult.StoreErr.Error())
		return
	}
	finished, err := s.executionStore.GetByID(r.Context(), retry.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": executionResponse(*finished)})
}

func (s *Server) handleActiveExecutions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": s.activeExecutions.List()})
}

func (s *Server) handleNodeTypes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": metadata.NodeTypes(s.knownNodeTypes())})
}

func (s *Server) handleNodeType(w http.ResponseWriter, r *http.Request) {
	node, ok := metadata.NodeTypeByName(chi.URLParam(r, "name"), s.knownNodeTypes())
	if !ok {
		writeError(w, http.StatusNotFound, "node type not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": node})
}

func (s *Server) knownNodeTypes() []string {
	if s.registry == nil {
		return nil
	}
	return s.registry.KnownTypes()
}

func workflowFromRow(row *persistence.WorkflowRow) (dataplane.Workflow, error) {
	workflow := dataplane.Workflow{
		ID:        row.ID,
		Name:      row.Name,
		Active:    row.Active,
		VersionID: row.VersionID,
		CreatedAt: &row.CreatedAt,
		UpdatedAt: &row.UpdatedAt,
	}
	if err := json.Unmarshal(row.Nodes, &workflow.Nodes); err != nil {
		return workflow, err
	}
	normalizeNodeDefaults(workflow.Nodes)
	if err := json.Unmarshal(row.Connections, &workflow.Connections); err != nil {
		return workflow, err
	}
	_ = json.Unmarshal(row.Settings, &workflow.Settings)
	_ = json.Unmarshal(row.StaticData, &workflow.StaticData)
	_ = json.Unmarshal(row.PinData, &workflow.PinData)
	_ = json.Unmarshal(row.Meta, &workflow.Meta)
	workflow.PreserveFields("settings", "pinData", "meta")
	normalizeWorkflowDefaults(&workflow)
	return workflow, nil
}

func normalizeNodeDefaults(nodes []dataplane.Node) {
	for index := range nodes {
		if nodes[index].Parameters == nil {
			nodes[index].Parameters = map[string]any{}
		}
		switch nodes[index].Type {
		case "n8n-nodes-base.webhook":
			if nodes[index].WebhookID == "" {
				nodes[index].WebhookID = firstNonEmpty(nodes[index].ID, stableNodeID(nodes[index].Name))
			}
			if strings.TrimSpace(parameterText(nodes[index].Parameters, "path")) == "" {
				nodes[index].Parameters["path"] = nodes[index].WebhookID
			}
		}
	}
}

func stableNodeID(seed string) string {
	seed = strings.TrimSpace(seed)
	if seed == "" {
		return "webhook"
	}
	return strings.NewReplacer(" ", "-", "/", "-", "\\", "-", ":", "-").Replace(strings.ToLower(seed))
}

func (s *Server) decorateWorkflowForFrontend(r *http.Request, workflow *dataplane.Workflow) {
	normalizeWorkflowDefaults(workflow)
	if workflow.Raw == nil {
		workflow.Raw = map[string]json.RawMessage{}
	}
	setWorkflowRaw(workflow, "settings", workflow.Settings)
	setWorkflowRaw(workflow, "pinData", workflow.PinData)
	setWorkflowRaw(workflow, "staticData", workflow.StaticData)
	setWorkflowRaw(workflow, "meta", workflow.Meta)
	setWorkflowRaw(workflow, "scopes", frontendWorkflowScopes())
	setWorkflowRaw(workflow, "homeProject", projectListItem(s.personalProject(r)))
	setWorkflowRaw(workflow, "checksum", workflowChecksum(*workflow))
	setWorkflowRaw(workflow, "sharedWithProjects", []map[string]any{})
	setWorkflowRaw(workflow, "tags", s.workflowTags(r.Context(), workflow.ID))
	setWorkflowRaw(workflow, "usedCredentials", []map[string]any{})
	setWorkflowRaw(workflow, "isArchived", false)
	setWorkflowRaw(workflow, "parentFolder", s.workflowParentFolder(r, workflow.ID))
	if workflow.Active && workflow.VersionID != "" {
		setWorkflowRaw(workflow, "activeVersionId", workflow.VersionID)
		setWorkflowRaw(workflow, "activeVersion", workflowActiveVersion(*workflow))
	} else {
		setWorkflowRaw(workflow, "activeVersionId", nil)
		setWorkflowRaw(workflow, "activeVersion", nil)
	}
}

func workflowActiveVersion(workflow dataplane.Workflow) map[string]any {
	createdAt := time.Now().UTC()
	if workflow.UpdatedAt != nil {
		createdAt = *workflow.UpdatedAt
	} else if workflow.CreatedAt != nil {
		createdAt = *workflow.CreatedAt
	}
	return map[string]any{
		"workflowId":             workflow.ID,
		"versionId":              workflow.VersionID,
		"name":                   nil,
		"description":            nil,
		"createdAt":              createdAt.Format(time.RFC3339Nano),
		"workflowPublishHistory": []map[string]any{},
	}
}

func (s *Server) newWorkflowPlaceholder(r *http.Request) dataplane.Workflow {
	now := time.Now().UTC()
	workflow := dataplane.Workflow{
		ID:        "new",
		Name:      "My workflow",
		Active:    false,
		VersionID: "new",
		CreatedAt: &now,
		UpdatedAt: &now,
	}
	s.decorateWorkflowForFrontend(r, &workflow)
	return workflow
}

func normalizeWorkflowDefaults(workflow *dataplane.Workflow) {
	if workflow.Nodes == nil {
		workflow.Nodes = []dataplane.Node{}
	}
	if workflow.Connections == nil {
		workflow.Connections = dataplane.Connections{}
	}
	if workflow.Settings == nil {
		workflow.Settings = map[string]any{}
	}
	if workflow.PinData == nil {
		workflow.PinData = map[string][]dataplane.Item{}
	}
	if workflow.StaticData == nil {
		workflow.StaticData = map[string]any{}
	}
	if workflow.Meta == nil {
		workflow.Meta = map[string]any{}
	}
}

func workflowChecksum(workflow dataplane.Workflow) string {
	if strings.TrimSpace(workflow.VersionID) != "" {
		return workflow.VersionID
	}
	if strings.TrimSpace(workflow.ID) != "" {
		return workflow.ID
	}
	return "new"
}

func (s *Server) decorateWorkflowRowsForFrontend(ctx context.Context, rows []persistence.WorkflowRow) {
	scopes := frontendWorkflowScopes()
	tagsByWorkflow := map[string][]persistence.TagRow{}
	if s.tagStore != nil && len(rows) > 0 {
		ids := make([]string, 0, len(rows))
		for i := range rows {
			ids = append(ids, rows[i].ID)
		}
		if loaded, err := s.tagStore.TagsForWorkflows(ctx, ids); err == nil {
			tagsByWorkflow = loaded
		}
	}
	for index := range rows {
		rows[index].Scopes = scopes
		rows[index].Tags = tagsByWorkflow[rows[index].ID]
		if rows[index].Tags == nil {
			rows[index].Tags = []persistence.TagRow{}
		}
		rows[index].Checksum = rows[index].VersionID
		if rows[index].Active && strings.TrimSpace(rows[index].VersionID) != "" {
			versionID := rows[index].VersionID
			rows[index].ActiveVersionID = &versionID
			rows[index].ActiveVersion = workflowActiveVersion(dataplane.Workflow{
				ID:        rows[index].ID,
				Name:      rows[index].Name,
				VersionID: rows[index].VersionID,
				CreatedAt: &rows[index].CreatedAt,
				UpdatedAt: &rows[index].UpdatedAt,
			})
		} else {
			rows[index].ActiveVersionID = nil
			rows[index].ActiveVersion = nil
		}
	}
}

func frontendWorkflowScopes() []string {
	return []string{
		"workflow:create",
		"workflow:read",
		"workflow:update",
		"workflow:publish",
		"workflow:delete",
		"workflow:list",
		"workflow:execute",
		"workflow:move",
		"workflow:share",
		"workflow:unshare",
		"workflow:unpublish",
		"workflow:activate",
		"workflow:deactivate",
	}
}

func jsonObjectHasKey(data []byte, key string) bool {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return false
	}
	_, ok := raw[key]
	return ok
}

func setWorkflowRaw(workflow *dataplane.Workflow, key string, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	workflow.Raw[key] = data
}

func validateWorkflow(workflow dataplane.Workflow) error {
	if strings.TrimSpace(workflow.Name) == "" {
		return errInvalidWorkflow("workflow name is required")
	}
	names := map[string]bool{}
	for _, node := range workflow.Nodes {
		if strings.TrimSpace(node.Name) == "" {
			return errInvalidWorkflow("workflow node name is required")
		}
		if names[node.Name] {
			return errInvalidWorkflow("workflow node names must be unique")
		}
		names[node.Name] = true
	}
	return nil
}

type errInvalidWorkflow string

func (e errInvalidWorkflow) Error() string {
	return string(e)
}

func queryInt(r *http.Request, key string, fallback int) int {
	value := r.URL.Query().Get(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func parseTimeIDCursor(cursor string, label string) (time.Time, string, error) {
	if strings.TrimSpace(cursor) == "" {
		return time.Time{}, "", nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", errInvalidWorkflow("invalid " + label + " cursor")
	}
	parts := strings.SplitN(string(decoded), "|", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return time.Time{}, "", errInvalidWorkflow("invalid " + label + " cursor")
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", errInvalidWorkflow("invalid " + label + " cursor")
	}
	return updatedAt, parts[1], nil
}

func encodeTimeIDCursor(cursor string) string {
	if cursor == "" {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(cursor))
}

func filterWorkflowRows(workflows []persistence.WorkflowRow, r *http.Request) []persistence.WorkflowRow {
	activeFilter := r.URL.Query().Get("active")
	nameFilter := strings.ToLower(strings.TrimSpace(firstNonEmpty(r.URL.Query().Get("name"), r.URL.Query().Get("search"))))
	if activeFilter == "" && nameFilter == "" {
		return workflows
	}
	result := make([]persistence.WorkflowRow, 0, len(workflows))
	for _, workflow := range workflows {
		if activeFilter != "" {
			active := strings.EqualFold(activeFilter, "true") || activeFilter == "1"
			if workflow.Active != active {
				continue
			}
		}
		if nameFilter != "" && !strings.Contains(strings.ToLower(workflow.Name), nameFilter) {
			continue
		}
		result = append(result, workflow)
	}
	return result
}

type executionListFilters struct {
	WorkflowIDs []string
	Statuses    []string
	Modes       []string
}

func executionListFiltersFromRequest(r *http.Request) executionListFilters {
	query := r.URL.Query()
	filters := executionListFilters{
		WorkflowIDs: splitQueryValues(query.Get("workflowId")),
		Statuses:    splitQueryValues(query.Get("status")),
		Modes:       splitQueryValues(query.Get("mode")),
	}
	if raw := strings.TrimSpace(query.Get("filter")); raw != "" {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
			filters.WorkflowIDs = appendUniqueStrings(filters.WorkflowIDs, stringsFromFilterValue(parsed["workflowId"])...)
			filters.WorkflowIDs = appendUniqueStrings(filters.WorkflowIDs, stringsFromFilterValue(parsed["workflowIds"])...)
			filters.Statuses = appendUniqueStrings(filters.Statuses, stringsFromFilterValue(parsed["status"])...)
			filters.Modes = appendUniqueStrings(filters.Modes, stringsFromFilterValue(parsed["mode"])...)
		}
	}
	return filters
}

func (filters executionListFilters) workflowIDForStore() string {
	if len(filters.WorkflowIDs) == 1 {
		return filters.WorkflowIDs[0]
	}
	return ""
}

func filterExecutionRows(executions []persistence.ExecutionRow, filters executionListFilters) []persistence.ExecutionRow {
	if len(filters.WorkflowIDs) == 0 && len(filters.Statuses) == 0 && len(filters.Modes) == 0 {
		return executions
	}
	result := make([]persistence.ExecutionRow, 0, len(executions))
	for _, execution := range executions {
		if len(filters.WorkflowIDs) > 0 && !containsString(filters.WorkflowIDs, execution.WorkflowID) {
			continue
		}
		if len(filters.Statuses) > 0 && !containsString(filters.Statuses, execution.Status) {
			continue
		}
		if len(filters.Modes) > 0 && !containsString(filters.Modes, execution.Mode) {
			continue
		}
		result = append(result, execution)
	}
	return result
}

func writeExecutionListResponse(w http.ResponseWriter, executions []persistence.ExecutionRow, nextCursor string) {
	w.Header().Set("Cache-Control", "no-store")
	count := len(executions)
	encodedCursor := ""
	if nextCursor != "" {
		encodedCursor = encodeTimeIDCursor(nextCursor)
		count++
	}
	results := make([]map[string]any, 0, len(executions))
	for _, execution := range executions {
		results = append(results, executionResponse(execution))
	}
	data := map[string]any{
		"results":                   results,
		"count":                     count,
		"estimated":                 false,
		"concurrentExecutionsCount": 0,
	}
	if encodedCursor != "" {
		data["nextCursor"] = encodedCursor
	}
	response := map[string]any{
		"data": data,
		// Keep top-level fields too for frontend paths using full API responses.
		"results":                   results,
		"count":                     data["count"],
		"estimated":                 data["estimated"],
		"concurrentExecutionsCount": data["concurrentExecutionsCount"],
	}
	if encodedCursor != "" {
		response["nextCursor"] = encodedCursor
	}
	writeJSON(w, http.StatusOK, response)
}

func executionResponse(execution persistence.ExecutionRow) map[string]any {
	finished := execution.Status != "new" && execution.Status != "running" && execution.Status != "waiting"
	response := map[string]any{
		"id":             execution.ID,
		"executionId":    execution.ID,
		"workflowId":     execution.WorkflowID,
		"status":         execution.Status,
		"mode":           execution.Mode,
		"startedAt":      execution.StartedAt,
		"workflowData":   execution.WorkflowData,
		"data":           executionDataForFrontend(execution.Data),
		"createdAt":      execution.CreatedAt,
		"finished":       finished,
		"stoppedAt":      execution.StoppedAt,
		"waitTill":       execution.WaitTill,
		"retryOf":        execution.RetryOf,
		"retrySuccessId": execution.RetrySuccessID,
	}
	return response
}

func lastSuccessfulExecutionResponse(execution persistence.ExecutionRow) map[string]any {
	response := executionResponse(execution)
	response["data"] = executionDataObjectForFrontend(execution.Data)
	return response
}

func executionDataObjectForFrontend(raw json.RawMessage) map[string]any {
	if parsed, err := flatted.Parse(string(raw)); err == nil {
		normalizeExecutionDataShape(parsed)
		if root, ok := parsed.(map[string]any); ok {
			return root
		}
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		root = map[string]any{}
	}
	normalizeExecutionDataShape(root)
	return root
}

func executionDataForFrontend(raw json.RawMessage) string {
	text := string(raw)
	parsed, err := flatted.Parse(text)
	if err != nil {
		var plain any
		if jsonErr := json.Unmarshal(raw, &plain); jsonErr == nil {
			normalizeExecutionDataShape(plain)
			if converted, jsonErr := json.Marshal(plain); jsonErr == nil {
				return string(converted)
			}
		}
		return text
	}
	normalizeExecutionDataShape(parsed)
	converted, err := flatted.Stringify(parsed)
	if err != nil {
		return text
	}
	return converted
}

func normalizeExecutionDataShape(value any) {
	root, ok := value.(map[string]any)
	if !ok {
		return
	}
	resultData, ok := root["resultData"].(map[string]any)
	if !ok || resultData == nil {
		resultData = map[string]any{}
		root["resultData"] = resultData
	}
	if _, ok := resultData["runData"].(map[string]any); !ok {
		resultData["runData"] = map[string]any{}
	}
	if _, ok := resultData["pinData"].(map[string]any); !ok {
		resultData["pinData"] = map[string]any{}
	}
	if startData, ok := root["startData"].(map[string]any); !ok || startData == nil {
		root["startData"] = map[string]any{}
	}
}

func isLegacyFlattedRoot(raw json.RawMessage) bool {
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil || len(values) == 0 {
		return false
	}
	var root string
	return json.Unmarshal(values[0], &root) == nil
}

func splitQueryValues(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values
}

func stringsFromFilterValue(value any) []string {
	switch typed := value.(type) {
	case string:
		return splitQueryValues(typed)
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			values = appendUniqueStrings(values, stringsFromFilterValue(item)...)
		}
		return values
	case map[string]any:
		values := make([]string, 0, len(typed))
		for _, key := range []string{"id", "value", "workflowId"} {
			values = appendUniqueStrings(values, stringsFromFilterValue(typed[key])...)
		}
		return values
	default:
		return nil
	}
}

func appendUniqueStrings(values []string, additions ...string) []string {
	for _, addition := range additions {
		addition = strings.TrimSpace(addition)
		if addition == "" || containsString(values, addition) {
			continue
		}
		values = append(values, addition)
	}
	return values
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func resultRunData(result *engine.Result) dataplane.RunData {
	if result == nil {
		return dataplane.RunData{}
	}
	return result.RunData
}

func resultLastNode(result *engine.Result) string {
	if result == nil {
		return ""
	}
	return result.LastNodeExecuted
}

func decodeWorkflowRunRequest(r *http.Request) (workflowRunRequest, error) {
	var request workflowRunRequest
	if r.Body == nil {
		return request, nil
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil && err != io.EOF {
		return workflowRunRequest{}, err
	}
	return request, nil
}

func (r workflowRunRequest) pushRef(request *http.Request) string {
	return requestPushRef(request, r.PushRef)
}

func requestPushRef(r *http.Request, bodyValue string) string {
	if value := strings.TrimSpace(bodyValue); value != "" {
		return value
	}
	if value := strings.TrimSpace(r.Header.Get("push-ref")); value != "" {
		return value
	}
	return strings.TrimSpace(r.URL.Query().Get("pushRef"))
}

func (s *Server) manualRunWorkflow(r *http.Request, request workflowRunRequest) (dataplane.Workflow, error) {
	if request.WorkflowData != nil {
		workflow := *request.WorkflowData
		if workflow.ID == "" {
			workflow.ID = chi.URLParam(r, "id")
		}
		if workflow.Name == "" {
			workflow.Name = "Manual workflow"
		}
		if err := validateWorkflow(workflow); err != nil {
			return dataplane.Workflow{}, err
		}
		return workflow, nil
	}
	row, err := s.workflowStore.GetByID(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		return dataplane.Workflow{}, err
	}
	workflow, err := workflowFromRow(row)
	if err != nil {
		return dataplane.Workflow{}, err
	}
	return workflow, nil
}

func (r workflowRunRequest) effectivePinData(workflow dataplane.Workflow) map[string][]dataplane.Item {
	if len(r.PinData) > 0 {
		return r.PinData
	}
	return workflow.PinData
}

func (r workflowRunRequest) startNodeNames() []string {
	names := make([]string, 0, len(r.StartNodes))
	for _, node := range r.StartNodes {
		if strings.TrimSpace(node.Name) != "" {
			names = append(names, node.Name)
		}
	}
	return names
}

func (r workflowRunRequest) triggerNodeName() string {
	trimmed := bytes.TrimSpace(r.TriggerToStart)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ""
	}
	var name string
	if err := json.Unmarshal(trimmed, &name); err == nil {
		return strings.TrimSpace(name)
	}
	var payload struct {
		Name     string `json:"name"`
		NodeName string `json:"nodeName"`
		Node     struct {
			Name string `json:"name"`
		} `json:"node"`
	}
	if err := json.Unmarshal(trimmed, &payload); err != nil {
		return ""
	}
	if name := strings.TrimSpace(payload.Name); name != "" {
		return name
	}
	if name := strings.TrimSpace(payload.NodeName); name != "" {
		return name
	}
	return strings.TrimSpace(payload.Node.Name)
}

func (r workflowRunRequest) startData(destination *engine.DestinationNode) map[string]any {
	data := map[string]any{}
	if destination != nil {
		data["destinationNode"] = destination.NodeName
		data["destinationMode"] = destination.Mode
	}
	if triggerNode := r.triggerNodeName(); triggerNode != "" {
		data["triggerToStartFrom"] = triggerNode
	}
	if names := r.startNodeNames(); len(names) > 0 {
		data["startNodes"] = names
	}
	if len(r.DirtyNodeNames) > 0 {
		data["dirtyNodeNames"] = r.DirtyNodeNames
	}
	return data
}

func parseDestinationNode(raw json.RawMessage) (*engine.DestinationNode, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	var name string
	if err := json.Unmarshal(trimmed, &name); err == nil {
		if name == "" {
			return nil, nil
		}
		return &engine.DestinationNode{NodeName: name, Mode: engine.DestinationInclusive}, nil
	}
	var payload struct {
		NodeName string `json:"nodeName"`
		Name     string `json:"name"`
		Mode     string `json:"mode"`
	}
	if err := json.Unmarshal(trimmed, &payload); err != nil {
		return nil, err
	}
	if payload.NodeName == "" {
		payload.NodeName = payload.Name
	}
	if payload.NodeName == "" {
		return nil, io.ErrUnexpectedEOF
	}
	mode := engine.DestinationMode(payload.Mode)
	if mode == "" {
		mode = engine.DestinationInclusive
	}
	return &engine.DestinationNode{NodeName: payload.NodeName, Mode: mode}, nil
}
