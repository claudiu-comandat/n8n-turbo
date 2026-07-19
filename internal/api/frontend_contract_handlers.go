package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/n8n-io/n8n-turbo/internal/auth"
	"github.com/n8n-io/n8n-turbo/internal/metadata"
	"github.com/n8n-io/n8n-turbo/internal/persistence"
)

func (s *Server) handleNodeTypesJSON(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, metadata.NodeTypes(s.knownNodeTypes()))
}

func (s *Server) handleCredentialTypesJSON(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, metadata.CredentialTypes())
}

func (s *Server) handleLicenseInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"planName":          "",
			"activatedAt":       nil,
			"expirationDate":    nil,
			"isCloudDeployment": false,
			"licensed":          false,
		},
	})
}

func (s *Server) handleTelemetrySourceConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Server) handleTelemetryArrayJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	_, _ = w.Write([]byte("window.posthog=window.posthog||{};\n"))
}

func (s *Server) handleModuleSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"insights": map[string]any{
				"dashboard": true,
				"dateRanges": []map[string]any{
					{"key": "week", "licensed": true},
					{"key": "month", "licensed": true},
					{"key": "year", "licensed": true},
				},
			},
			"chat-hub": map[string]any{
				"enabled": true,
			},
			"external-secrets": map[string]any{
				"multipleConnections": false,
				"forProjects":         false,
			},
			"otel": map[string]any{
				"enabled": false,
			},
		},
	})
}

func (s *Server) handleProvisioningConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"scopesInstanceRoleClaimName":  "n8n_instance_role",
			"scopesName":                   "n8n",
			"scopesProjectsRolesClaimName": "n8n_projects",
			"scopesProvisionInstanceRole":  false,
			"scopesProvisionProjectRoles":  false,
			"scopesUseExpressionMapping":   false,
		},
	})
}

func (s *Server) handleDataTablesGlobalLimits(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"totalBytes":  0,
		"quotaStatus": "ok",
		"dataTables":  map[string]any{},
		"data": map[string]any{
			"totalBytes":  0,
			"quotaStatus": "ok",
			"dataTables":  map[string]any{},
		},
	})
}

func (s *Server) handleMyProjects(w http.ResponseWriter, r *http.Request) {
	project := s.personalProject(r)
	writeJSON(w, http.StatusOK, map[string]any{"data": []map[string]any{projectListItem(project)}})
}

func (s *Server) handlePersonalProject(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": s.personalProject(r)})
}

func (s *Server) handleProject(w http.ResponseWriter, r *http.Request) {
	project := s.personalProject(r)
	id := chi.URLParam(r, "id")
	if id != project["id"] && id != "personal" && !strings.HasPrefix(id, "personal-") {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": project})
}

func (s *Server) handleProjectsCount(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]int{"personal": 1, "team": 0, "public": 0}})
}

func (s *Server) handleProjectSharingCandidates(w http.ResponseWriter, r *http.Request) {
	project := projectListItem(s.personalProject(r))
	response := map[string]any{
		"data":  []map[string]any{project},
		"count": 1,
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleRoles(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
		"global":                    []map[string]any{},
		"project":                   []map[string]any{},
		"credential":                []map[string]any{},
		"workflow":                  []map[string]any{},
		"secretsProviderConnection": []map[string]any{},
	}})
}

func (s *Server) personalProject(r *http.Request) map[string]any {
	user, ok := auth.UserFromContext(r.Context())
	userID := "owner"
	email := "owner@n8n.local"
	firstName := "Owner"
	lastName := "User"
	if ok {
		userID = user.ID
		email = user.Email
		firstName = user.FirstName
		lastName = user.LastName
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return map[string]any{
		"id":          "personal-" + userID,
		"name":        "Personal",
		"icon":        map[string]any{"type": "icon", "value": "home"},
		"type":        "personal",
		"description": "",
		"createdAt":   now,
		"updatedAt":   now,
		"relations": []map[string]any{
			{
				"id":        userID,
				"email":     email,
				"firstName": firstName,
				"lastName":  lastName,
				"role":      "project:personalOwner",
			},
		},
		"scopes":              personalProjectScopes(),
		"customTelemetryTags": []map[string]any{},
	}
}

func projectListItem(project map[string]any) map[string]any {
	return map[string]any{
		"id":          project["id"],
		"name":        project["name"],
		"icon":        project["icon"],
		"type":        project["type"],
		"description": project["description"],
		"createdAt":   project["createdAt"],
		"updatedAt":   project["updatedAt"],
		"role":        "project:personalOwner",
		"scopes":      project["scopes"],
	}
}

func personalProjectScopes() []string {
	return []string{
		"credential:create",
		"credential:read",
		"credential:update",
		"credential:delete",
		"credential:list",
		"credential:move",
		"credential:unshare",
		"dataTable:create",
		"dataTable:delete",
		"dataTable:read",
		"dataTable:update",
		"dataTable:listProject",
		"dataTable:readRow",
		"dataTable:writeRow",
		"dataTable:readColumn",
		"dataTable:writeColumn",
		"execution:reveal",
		"folder:create",
		"folder:read",
		"folder:update",
		"folder:delete",
		"folder:list",
		"folder:move",
		"project:list",
		"project:read",
		"projectVariable:create",
		"projectVariable:read",
		"projectVariable:update",
		"projectVariable:delete",
		"projectVariable:list",
		"workflow:create",
		"workflow:read",
		"workflow:export",
		"workflow:import",
		"workflow:update",
		"workflow:publish",
		"workflow:delete",
		"workflow:list",
		"workflow:execute",
		"workflow:execute-chat",
		"workflow:move",
		"workflow:unpublish",
		"workflow:unshare",
		"workflow:enableRedaction",
		"workflow:disableRedaction",
	}
}

func (s *Server) handleListFavorites(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": []map[string]any{}})
}

func (s *Server) handleLegacyActiveWorkflows(w http.ResponseWriter, r *http.Request) {
	s.handleActiveExecutions(w, r)
}

func (s *Server) handleCredentialsForWorkflow(w http.ResponseWriter, r *http.Request) {
	rows := []map[string]any{}
	user, ok := auth.UserFromContext(r.Context())
	if ok {
		credentials, err := s.credentialStore.List(r.Context(), user.ID, queryInt(r, "limit", 100))
		if err == nil {
			rows = make([]map[string]any, 0, len(credentials))
			for _, row := range credentials {
				rows = append(rows, s.credentialMeta(r, row))
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": rows})
}

func (s *Server) handleGetMeSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": s.currentUserSettings(r.Context())})
}

func (s *Server) handleUpdateMeSettings(w http.ResponseWriter, r *http.Request) {
	var patch map[string]any
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeError(w, http.StatusBadRequest, "invalid user settings body")
		return
	}
	if patch == nil {
		patch = map[string]any{}
	}
	settings := s.currentUserSettings(r.Context())
	deepMerge(settings, patch)
	if err := s.saveCurrentUserSettings(r.Context(), settings); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": settings})
}

func (s *Server) currentUserSettings(ctx context.Context) map[string]any {
	settings := map[string]any{
		"userActivated":     true,
		"dismissedCallouts": map[string]any{},
	}
	if s.settingsStore == nil {
		return settings
	}
	raw, err := s.settingsStore.Get(ctx, s.currentUserSettingsKey(ctx))
	if err != nil {
		if errors.Is(err, persistence.ErrNotFound) {
			return settings
		}
		return settings
	}
	stored := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return settings
	}
	deepMerge(settings, stored)
	return settings
}

func (s *Server) saveCurrentUserSettings(ctx context.Context, settings map[string]any) error {
	if s.settingsStore == nil {
		return nil
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	return s.settingsStore.Set(ctx, s.currentUserSettingsKey(ctx), string(raw))
}

func (s *Server) currentUserSettingsKey(ctx context.Context) string {
	user, ok := auth.UserFromContext(ctx)
	userID := "owner"
	if ok && strings.TrimSpace(user.ID) != "" {
		userID = user.ID
	}
	return "user.settings." + userID
}

func (s *Server) handleWorkflowWriteLock(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": nil})
}

func (s *Server) handleWorkflowHistoryVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"id":        chi.URLParam(r, "versionId"),
			"authors":   []map[string]any{},
			"createdAt": time.Now().UTC().Format(time.RFC3339Nano),
			"nodes":     []map[string]any{},
		},
	})
}

func (s *Server) handleAddFavorite(w http.ResponseWriter, r *http.Request) {
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid favorite body")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"id":           1,
			"userId":       "owner",
			"resourceId":   payload["resourceId"],
			"resourceType": payload["resourceType"],
			"resourceName": "",
		},
	})
}

func (s *Server) handleRemoveFavorite(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": true})
}

