package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"

	"github.com/n8n-io/n8n-turbo/internal/persistence"
	"github.com/n8n-io/n8n-turbo/internal/sourcecontrol"
)

// serverImportTarget lets the git Service write pulled resources back into the DB.
type serverImportTarget struct{ server *Server }

func (t serverImportTarget) ApplyWorkflow(ctx context.Context, row persistence.WorkflowRow) error {
	return t.server.applyImportedWorkflow(ctx, row)
}

func (t serverImportTarget) ApplyVariable(ctx context.Context, row persistence.VariableRow) error {
	if t.server.variableStore == nil {
		return nil
	}
	_, err := t.server.variableStore.Save(ctx, row)
	return err
}

func (s *Server) applyImportedWorkflow(ctx context.Context, row persistence.WorkflowRow) error {
	workflow, err := workflowFromRow(&row)
	if err != nil {
		return err
	}
	workflow.VersionID = "" // authoritative overwrite from git
	// ponytail: never hot-activate on pull — preserve the DB's current active flag so a
	// pull can't silently switch on a live marketplace webhook. New workflows land inactive.
	if existing, err := s.workflowStore.GetByID(ctx, workflow.ID); err == nil {
		workflow.Active = existing.Active
	} else {
		workflow.Active = false
	}
	_, err = s.workflowStore.Save(ctx, workflow, "")
	return err
}

func (s *Server) sourceControlPreferences() map[string]any {
	prefs := map[string]any{
		"branchName":       "",
		"branches":         []string{},
		"branchReadOnly":   false,
		"branchColor":      "#5296D6",
		"connected":        false,
		"repositoryUrl":    "",
		"publicKey":        "",
		"keyGeneratorType": "ed25519",
		"connectionType":   "https",
		"currentBranch":    "",
	}
	if s.sourceControl == nil {
		return prefs
	}
	if cfg := s.sourceControl.CurrentConfig(); cfg != nil {
		prefs["connected"] = cfg.Active
		prefs["repositoryUrl"] = cfg.RepoURL
		prefs["branchName"] = cfg.Branch
		prefs["currentBranch"] = cfg.Branch
		prefs["branches"] = []string{cfg.Branch}
		if cfg.PrivateKey != "" {
			prefs["connectionType"] = "ssh"
		}
	}
	return prefs
}

func (s *Server) handleSourceControlPreferences(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": s.sourceControlPreferences()})
}

// handleSaveSourceControlPreferences connects (or reconnects) to a git remote. It
// accepts HTTPS+PAT (username/token) or SSH (privateKey). Sending no repositoryUrl
// just echoes the current preferences.
func (s *Server) handleSaveSourceControlPreferences(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		RepositoryURL  string `json:"repositoryUrl"`
		BranchName     string `json:"branchName"`
		AuthorName     string `json:"authorName"`
		AuthorEmail    string `json:"authorEmail"`
		Username       string `json:"username"`
		Password       string `json:"password"`
		Token          string `json:"token"`
		PrivateKey     string `json:"privateKey"`
		ConnectionType string `json:"connectionType"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid source control preferences body")
		return
	}
	if payload.RepositoryURL == "" || s.sourceControl == nil {
		writeJSON(w, http.StatusOK, map[string]any{"data": s.sourceControlPreferences()})
		return
	}
	cfg := sourcecontrol.Config{
		RepoURL:     payload.RepositoryURL,
		Branch:      firstNonEmpty(payload.BranchName, "main"),
		AuthorName:  firstNonEmpty(payload.AuthorName, "n8n Turbo"),
		AuthorEmail: firstNonEmpty(payload.AuthorEmail, "n8n-turbo@local"),
		Username:    payload.Username,
		Password:    firstNonEmpty(payload.Password, payload.Token),
		PrivateKey:  payload.PrivateKey,
	}
	if err := s.sourceControl.Connect(r.Context(), cfg); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": s.sourceControlPreferences()})
}

func (s *Server) handleSourceControlBranches(w http.ResponseWriter, r *http.Request) {
	branch := ""
	if s.sourceControl != nil {
		if cfg := s.sourceControl.CurrentConfig(); cfg != nil {
			branch = cfg.Branch
		}
	}
	branches := []string{}
	if branch != "" {
		branches = []string{branch}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"branches": branches, "currentBranch": branch}})
}

func (s *Server) handleSourceControlStatus(w http.ResponseWriter, r *http.Request) {
	if s.sourceControl != nil {
		if result, err := s.sourceControl.Status(r.Context()); err == nil {
			writeJSON(w, http.StatusOK, map[string]any{"data": sourceControlStatusMap(result)})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": emptySourceControlStatus()})
}

func (s *Server) handleSourceControlAggregatedStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": []map[string]any{}})
}

func (s *Server) handleSourceControlGenerateKeyPair(w http.ResponseWriter, r *http.Request) {
	key := make([]byte, 24)
	_, _ = rand.Read(key)
	publicKey := "ssh-ed25519 " + base64.StdEncoding.EncodeToString(key) + " n8n-turbo"
	writeJSON(w, http.StatusOK, map[string]any{"data": publicKey})
}

func (s *Server) handleSourceControlDisconnect(w http.ResponseWriter, r *http.Request) {
	if s.sourceControl != nil {
		_ = s.sourceControl.Disconnect(r.Context())
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": "disconnected"})
}

func (s *Server) handleSourceControlPushWorkfolder(w http.ResponseWriter, r *http.Request) {
	if s.sourceControl == nil {
		writeError(w, http.StatusNotImplemented, "source control unavailable")
		return
	}
	var opts struct {
		Message   string   `json:"message"`
		Force     bool     `json:"force"`
		FileNames []string `json:"fileNames"`
	}
	_ = json.NewDecoder(r.Body).Decode(&opts)
	deps, err := s.sourceControlPushDeps(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	result, err := s.sourceControl.Push(r.Context(), deps, sourcecontrol.PushOptions{Message: opts.Message, Force: opts.Force, FileNames: opts.FileNames})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{"files": result.Files, "commit": result.Commit, "status": result.Status},
	})
}

func (s *Server) handleSourceControlPullWorkfolder(w http.ResponseWriter, r *http.Request) {
	if s.sourceControl == nil {
		writeError(w, http.StatusNotImplemented, "source control unavailable")
		return
	}
	var opts struct {
		Force bool `json:"force"`
	}
	_ = json.NewDecoder(r.Body).Decode(&opts)
	result, err := s.sourceControl.Pull(r.Context(), opts.Force, serverImportTarget{server: s})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Imported workflows take effect immediately: refresh the webhook index and schedules.
	_ = s.reconcileWebhookRegistry(r.Context())
	if err := s.syncScheduledWorkflows(r.Context()); err != nil {
		// non-fatal; the 30s reconcile loop will retry
		_ = err
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result.Files, "statusCode": result.StatusCode})
}

func (s *Server) sourceControlPushDeps(ctx context.Context) (sourcecontrol.PushDependencies, error) {
	deps := sourcecontrol.PushDependencies{}
	if err := s.eachWorkflowRow(ctx, func(row persistence.WorkflowRow) {
		deps.Workflows = append(deps.Workflows, row)
	}); err != nil {
		return deps, err
	}
	if s.tagStore != nil {
		if tags, err := s.tagStore.List(ctx); err == nil {
			deps.Tags = tags
		}
	}
	if s.variableStore != nil {
		if variables, err := s.variableStore.List(ctx); err == nil {
			deps.Variables = variables
		}
	}
	// ponytail: credentials are deliberately NOT exported — their data is secret and
	// must never land in a git repo. Only workflows/variables/tags are versioned.
	return deps, nil
}

func sourceControlStatusMap(result *sourcecontrol.StatusResult) map[string]any {
	files := []map[string]any{}
	appendFiles := func(list []sourcecontrol.SourceControlledFile) {
		for _, file := range list {
			files = append(files, map[string]any{
				"file": file.File, "id": file.ID, "name": file.Name,
				"type": file.Type, "status": file.Status, "conflict": file.Conflict,
			})
		}
	}
	appendFiles(result.Modified)
	appendFiles(result.Added)
	appendFiles(result.Deleted)
	appendFiles(result.Untracked)
	appendFiles(result.Conflicted)
	status := emptySourceControlStatus()
	status["ahead"] = result.Ahead
	status["behind"] = result.Behind
	status["files"] = files
	return status
}

func emptySourceControlStatus() map[string]any {
	return map[string]any{
		"ahead":      0,
		"behind":     0,
		"conflicted": []string{},
		"created":    []string{},
		"current":    "",
		"deleted":    []string{},
		"detached":   false,
		"files":      []map[string]any{},
		"modified":   []string{},
		"not_added":  []string{},
		"renamed":    []string{},
		"staged":     []string{},
		"tracking":   nil,
	}
}
