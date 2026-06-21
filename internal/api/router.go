package api

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/n8n-io/n8n-turbo/internal/audit"
	"github.com/n8n-io/n8n-turbo/internal/auth"
	"github.com/n8n-io/n8n-turbo/internal/binarydata"
	"github.com/n8n-io/n8n-turbo/internal/config"
	"github.com/n8n-io/n8n-turbo/internal/credentials"
	cronleader "github.com/n8n-io/n8n-turbo/internal/cron"
	"github.com/n8n-io/n8n-turbo/internal/engine"
	"github.com/n8n-io/n8n-turbo/internal/frontend"
	"github.com/n8n-io/n8n-turbo/internal/logstream"
	"github.com/n8n-io/n8n-turbo/internal/nodes"
	"github.com/n8n-io/n8n-turbo/internal/persistence"
	"github.com/n8n-io/n8n-turbo/internal/push"
	"github.com/n8n-io/n8n-turbo/internal/secrets"
)

type Server struct {
	config           config.Config
	runtimeCtx       context.Context
	authService      *auth.Service
	userStore        persistence.UserStore
	settingsStore    persistence.SettingsStore
	workflowStore    persistence.WorkflowStore
	executionStore   persistence.ExecutionStore
	credentialStore  persistence.CredentialStore
	variableStore    persistence.VariableStore
	tagStore         persistence.TagStore
	auditStore       persistence.AuditStore
	insightsStore    persistence.InsightsStore
	registry         engine.Registry
	evaluator        *engine.Evaluator
	activeExecutions *engine.ActiveExecutions
	dispatcher       *executionDispatcher
	scheduler        *scheduler
	pushHub          *push.Hub
	logStream        *logstream.Service
	binaryStore      binarydata.Store
	rateLimiter      *rateLimiter
	vault            *credentials.Vault
	secretsManager   *secrets.Manager
	router           http.Handler
}

type loginRequest struct {
	Email              string `json:"email"`
	EmailOrLdapLoginID string `json:"emailOrLdapLoginId"`
	Password           string `json:"password"`
}

func NewServer(
	cfg config.Config,
	authService *auth.Service,
	userStore persistence.UserStore,
	settingsStore persistence.SettingsStore,
	workflowStore persistence.WorkflowStore,
	executionStore persistence.ExecutionStore,
	credentialStore persistence.CredentialStore,
	variableStore persistence.VariableStore,
	tagStore persistence.TagStore,
	auditStore persistence.AuditStore,
	insightsStore persistence.InsightsStore,
	vault *credentials.Vault,
) (*Server, error) {
	if err := authServiceBootstrap(authService); err != nil {
		return nil, err
	}

	frontendHandler, err := frontend.NewHandler(frontend.Config{
		UIPath: cfg.FrontendDir,
	})
	if err != nil {
		return nil, err
	}
	binaryStore, err := newBinaryStore(cfg)
	if err != nil {
		return nil, err
	}

	server := &Server{
		config:          cfg,
		runtimeCtx:      context.Background(),
		authService:     authService,
		userStore:       userStore,
		settingsStore:   settingsStore,
		workflowStore:   workflowStore,
		executionStore:  executionStore,
		credentialStore: credentialStore,
		variableStore:   variableStore,
		tagStore:        tagStore,
		auditStore:      auditStore,
		insightsStore:   insightsStore,
		vault:           vault,
		secretsManager:  secrets.NewManager(secrets.NewEnvProvider("")),
		logStream:       logstream.New(1000),
		binaryStore:     binaryStore,
		rateLimiter:     newRateLimiter(600, time.Minute),
	}
	registry := engine.NewRegistry()
	nodes.RegisterBuiltins(registry)
	server.registry = registry
	server.evaluator = engine.NewEvaluator(registry)
	concurrencyLimit := cfg.Execution.ConcurrencyLimit
	if concurrencyLimit < 0 {
		concurrencyLimit = 0
	}
	perWorkflowLimit := cfg.Execution.ConcurrencyPerWorkflowLimit
	if perWorkflowLimit < 0 {
		perWorkflowLimit = 0
	}
	queueSize := cfg.Execution.ConcurrencyQueueSize
	if queueSize < 0 {
		queueSize = 0
	}
	server.activeExecutions = engine.NewActiveExecutionsWithConfig(engine.ConcurrencyConfig{
		MaxGlobalConcurrent:      concurrencyLimit,
		MaxPerWorkflowConcurrent: perWorkflowLimit,
		QueueSize:                queueSize,
		AcquireTimeout:           cfg.Execution.ConcurrencyAcquireTimeout,
	})
	dispatcher, err := newExecutionDispatcher(server)
	if err != nil {
		return nil, err
	}
	server.dispatcher = dispatcher
	server.scheduler = newScheduler()
	if cfg.Scheduler.LeaderEnabled {
		server.scheduler.leader = cronleader.NewRedisLeader(cronleader.RedisLeaderConfig{
			Addr:       cfg.Scheduler.LeaderRedisAddr,
			Password:   cfg.Scheduler.LeaderRedisPassword,
			DB:         cfg.Scheduler.LeaderRedisDB,
			Key:        cfg.Scheduler.LeaderKey,
			InstanceID: cfg.Instance.ID,
			TTL:        cfg.Scheduler.LeaderTTL,
		})
	}
	server.pushHub = push.NewHub()

	router := chi.NewRouter()
	router.Use(recoverer)
	router.Use(requestID)
	router.Use(server.logging)
	router.Use(cors)
	router.Use(securityHeaders)
	router.Use(bodyLimit(50 << 20))
	router.Use(compression)
	router.Use(server.rateLimit)
	router.Get("/healthz", server.handleHealthz)
	router.Get("/rest/n8n-turbo/version", server.handleTurboVersion)
	router.Get("/metrics", server.handleMetrics)
	router.With(auth.OptionalMiddleware(authService, cfg.Auth)).Get("/rest/login", server.handleCurrentLogin)
	router.Post("/rest/login", server.handleLogin)
	router.Get("/rest/settings", server.handleSettings)
	router.Get("/types/nodes.json", server.handleNodeTypesJSON)
	router.Get("/types/credentials.json", server.handleCredentialTypesJSON)
	router.Get("/rest/license", server.handleLicenseInfo)
	router.Get("/rest/telemetry/rudderstack/sourceConfig/", server.handleTelemetrySourceConfig)
	router.Get("/rest/ph/static/array.js", server.handleTelemetryArrayJS)
	router.HandleFunc("/webhook/*", server.handleProductionWebhook)
	router.HandleFunc("/webhook-test/*", server.handleTestWebhook)
	router.HandleFunc("/form/*", server.handleProductionForm)
	router.HandleFunc("/form-test/*", server.handleTestForm)
	router.HandleFunc("/webhook-waiting/*", server.handleWaitingWebhook)
	router.HandleFunc("/form-waiting/*", server.handleWaitingForm)
	router.Handle("/push", server.pushHub)
	router.Handle("/rest/push", server.pushHub)
	router.Group(func(r chi.Router) {
		r.Use(auth.Middleware(authService, cfg.Auth))
		r.Use(server.auditMiddleware)
		r.Get("/rest/audit", server.handleListAuditEvents)
		r.Patch("/rest/settings", server.handleUpdateSettings)
		r.Get("/rest/me/settings", server.handleGetMeSettings)
		r.Patch("/rest/me/settings", server.handleUpdateMeSettings)
		r.Get("/rest/module-settings", server.handleModuleSettings)
		r.Get("/rest/projects/my-projects", server.handleMyProjects)
		r.Get("/rest/projects/personal", server.handlePersonalProject)
		r.Get("/rest/projects/count", server.handleProjectsCount)
		r.Get("/rest/projects/sharing-candidates", server.handleProjectSharingCandidates)
		r.Get("/rest/projects/{id}", server.handleProject)
		r.Get("/rest/roles", server.handleRoles)
		r.Get("/rest/favorites", server.handleListFavorites)
		r.Post("/rest/favorites", server.handleAddFavorite)
		r.Delete("/rest/favorites/{resourceType}/{resourceId}", server.handleRemoveFavorite)
		r.Get("/rest/log-streaming/events", server.handleLogStreamEvents)
		r.Get("/rest/logs", server.handleLogStreamEvents)
		r.Post("/rest/binary-data", server.handleUploadBinaryData)
		r.Get("/rest/binary-data/{id}", server.handleDownloadBinaryData)
		r.Delete("/rest/binary-data/{id}", server.handleDeleteBinaryData)
		r.Get("/rest/insights/summary", server.handleInsightsSummary)
		r.Get("/rest/insights/dashboard", server.handleInsightsDashboard)
		r.Get("/rest/workflow-statistics/{id}", server.handleWorkflowStats)
		r.Get("/rest/users/me", server.handleMe)
		r.Get("/rest/users", server.handleUsers)
		r.Get("/rest/workflows", server.handleListWorkflows)
		r.Post("/rest/workflows", server.handleSaveWorkflow)
		r.Post("/rest/workflows/import", server.handleImportWorkflow)
		r.Get("/rest/workflows/{id}", server.handleGetWorkflow)
		r.Get("/rest/workflows/{id}/exists", server.handleWorkflowExists)
		r.Get("/rest/workflows/{id}/collaboration/write-lock", server.handleWorkflowWriteLock)
		r.Patch("/rest/workflows/{id}", server.handleSaveWorkflow)
		r.Put("/rest/workflows/{id}", server.handleSaveWorkflow)
		r.Delete("/rest/workflows/{id}", server.handleDeleteWorkflow)
		r.Post("/rest/workflows/{id}/duplicate", server.handleDuplicateWorkflow)
		r.Get("/rest/workflows/{id}/export", server.handleExportWorkflow)
		r.Post("/rest/workflow-dependencies/counts", server.handleWorkflowDependenciesCounts)
		r.Get("/rest/workflows/{id}/executions/last-successful", server.handleLastSuccessfulWorkflowExecution)
		r.Post("/rest/workflows/{id}/activate", server.handleActivateWorkflow)
		r.Patch("/rest/workflows/{id}/activate", server.handleActivateWorkflow)
		r.Post("/rest/workflows/{id}/publish", server.handleActivateWorkflow)
		r.Patch("/rest/workflows/{id}/publish", server.handleActivateWorkflow)
		r.Post("/rest/workflows/{id}/deactivate", server.handleDeactivateWorkflow)
		r.Patch("/rest/workflows/{id}/deactivate", server.handleDeactivateWorkflow)
		r.Post("/rest/workflows/{id}/unpublish", server.handleDeactivateWorkflow)
		r.Patch("/rest/workflows/{id}/unpublish", server.handleDeactivateWorkflow)
		r.Post("/rest/workflows/{id}/run", server.handleRunWorkflow)
		r.Get("/rest/executions", server.handleListExecutions)
		r.Get("/rest/executions/active", server.handleActiveExecutions)
		r.Post("/rest/executions/delete", server.handleDeleteExecutions)
		r.Get("/rest/active-workflows", server.handleLegacyActiveWorkflows)
		r.Post("/rest/webhooks/find", server.handleFindWebhook)
		r.Get("/rest/executions/{id}", server.handleGetExecution)
		r.Delete("/rest/executions/{id}", server.handleDeleteExecution)
		r.Post("/rest/executions/{id}/stop", server.handleStopExecution)
		r.Post("/rest/executions/{id}/retry", server.handleRetryExecution)
		r.Get("/rest/node-types", server.handleNodeTypes)
		r.Get("/rest/node-types/{name}", server.handleNodeType)
		r.Get("/rest/credential-types", server.handleCredentialTypes)
		r.Get("/rest/credential-types/{name}", server.handleCredentialType)
		r.Get("/rest/oauth2-credential/auth", server.handleOAuth2Auth)
		r.Get("/rest/oauth2-credential/callback", server.handleOAuth2Callback)
		r.Post("/rest/oauth2-credential/callback", server.handleOAuth2Callback)
		r.Get("/rest/credentials", server.handleListCredentials)
		r.Get("/rest/credentials/new", server.handleNewCredentialName)
		r.Get("/rest/credentials/for-workflow", server.handleCredentialsForWorkflow)
		r.Post("/rest/credentials", server.handleSaveCredential)
		r.Post("/rest/credentials/test", server.handleTestCredential)
		r.Get("/rest/credentials/{id}", server.handleGetCredential)
		r.Patch("/rest/credentials/{id}", server.handleSaveCredential)
		r.Put("/rest/credentials/{id}", server.handleSaveCredential)
		r.Delete("/rest/credentials/{id}", server.handleDeleteCredential)
		r.Post("/rest/credentials/{id}/test", server.handleTestCredential)
		r.Post("/rest/credentials/{id}/oauth2/refresh", server.handleOAuth2Refresh)
		r.Post("/rest/credentials/{id}/oauth2/client-credentials", server.handleOAuth2ClientCredentials)
		r.Get("/rest/api-keys", server.handleListAPIKeys)
		r.Get("/rest/api-keys/exists", server.handleAPIKeyExists)
		r.Get("/rest/api-keys/{id}/exists", server.handleAPIKeyExists)
		r.Get("/rest/api-keys/scopes", server.handleAPIKeyScopes)
		r.Post("/rest/api-keys", server.handleCreateAPIKey)
		r.Patch("/rest/api-keys/{id}", server.handleUpdateAPIKey)
		r.Delete("/rest/api-keys/{id}", server.handleDeleteAPIKey)
		r.Get("/rest/variables", server.handleListVariables)
		r.Post("/rest/variables", server.handleSaveVariable)
		r.Get("/rest/variables/{id}", server.handleGetVariable)
		r.Patch("/rest/variables/{id}", server.handleSaveVariable)
		r.Delete("/rest/variables/{id}", server.handleDeleteVariable)
		r.Get("/rest/external-secrets/providers", server.handleExternalSecretProviders)
		r.Get("/rest/external-secrets/secrets", server.handleExternalSecretsList)
		r.Get("/rest/external-secrets/secrets/{provider}/{name}", server.handleExternalSecretLookup)
		r.Get("/rest/source-control/preferences", server.handleSourceControlPreferences)
		r.Post("/rest/source-control/preferences", server.handleSaveSourceControlPreferences)
		r.Patch("/rest/source-control/preferences", server.handleSaveSourceControlPreferences)
		r.Get("/rest/source-control/get-branches", server.handleSourceControlBranches)
		r.Get("/rest/source-control/status", server.handleSourceControlStatus)
		r.Get("/rest/source-control/get-status", server.handleSourceControlAggregatedStatus)
		r.Post("/rest/source-control/generate-key-pair", server.handleSourceControlGenerateKeyPair)
		r.Post("/rest/source-control/disconnect", server.handleSourceControlDisconnect)
		r.Post("/rest/source-control/push-workfolder", server.handleSourceControlPushWorkfolder)
		r.Post("/rest/source-control/pull-workfolder", server.handleSourceControlPullWorkfolder)
		r.Get("/rest/tags", server.handleListTags)
		r.Post("/rest/tags", server.handleSaveTag)
		r.Get("/rest/tags/{id}", server.handleGetTag)
		r.Patch("/rest/tags/{id}", server.handleSaveTag)
		r.Delete("/rest/tags/{id}", server.handleDeleteTag)
		r.Get("/rest/workflow-history/workflow/{id}/version/{versionId}", server.handleWorkflowHistoryVersion)
		r.Get("/rest/sso/provisioning/config", server.handleProvisioningConfig)
		r.Get("/rest/data-tables-global/limits", server.handleDataTablesGlobalLimits)
	})
	router.Mount("/", frontendHandler)
	server.router = router
	return server, nil
}

func (s *Server) Router() http.Handler {
	return s.router
}

func authServiceBootstrap(service *auth.Service) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return service.BootstrapOwner(ctx)
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleTurboVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"build":                firstNonEmpty(os.Getenv("N8N_TURBO_BUILD"), "dev"),
			"image":                firstNonEmpty(os.Getenv("N8N_TURBO_IMAGE"), ""),
			"credentialMaskMarker": maskedCredentialValue(),
			"publishFallback":      true,
			"assetCache":           "no-store",
		},
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var payload loginRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	email := firstNonEmpty(payload.Email, payload.EmailOrLdapLoginID)
	result, err := s.authService.Login(r.Context(), email, payload.Password)
	if err != nil {
		s.logAudit(r, audit.Event{EventType: audit.EventUserLoginFailed, UserEmail: email, ResourceType: audit.ResourceUser})
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	s.logAudit(r, audit.Event{EventType: audit.EventUserLoggedIn, UserID: result.User.ID, UserEmail: result.User.Email, ResourceType: audit.ResourceUser, ResourceID: result.User.ID})

	auth.SetSessionCookie(w, result.Token, result.ExpiresAt, s.config.Auth)
	writeJSON(w, http.StatusOK, map[string]any{"data": userPayload(result.User.ID, result.User.Email, result.User.FirstName, result.User.LastName, result.User.Role, result.User.IsOwner)})
}

func (s *Server) handleCurrentLogin(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"data": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": userPayload(user.ID, user.Email, user.FirstName, user.LastName, user.Role, user.IsOwner)})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing user")
		return
	}
	dbUser, err := s.authService.GetUser(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	payload := userPayload(dbUser.ID, dbUser.Email, dbUser.FirstName, dbUser.LastName, dbUser.Role, dbUser.Role == "global:owner")
	payload["settings"] = s.currentUserSettings(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"data": payload})
}

func userPayload(id string, email string, firstName string, lastName string, role string, isOwner bool) map[string]any {
	globalScopes := []string{}
	if isOwner || role == "global:owner" {
		globalScopes = ownerGlobalScopes()
	}
	return map[string]any{
		"id":               id,
		"email":            email,
		"firstName":        firstName,
		"lastName":         lastName,
		"role":             role,
		"isOwner":          isOwner,
		"isPending":        false,
		"mfaEnabled":       false,
		"mfaAuthenticated": true,
		"settings": map[string]any{
			"userActivated":     true,
			"dismissedCallouts": map[string]any{},
		},
		"featureFlags": map[string]any{},
		"globalScopes": globalScopes,
	}
}

func ownerGlobalScopes() []string {
	return []string{
		"auditLogs:manage",
		"community:register",
		"credential:create",
		"credential:read",
		"credential:update",
		"credential:delete",
		"credential:list",
		"credential:share",
		"credential:unshare",
		"credential:shareGlobally",
		"credential:move",
		"apiKey:manage",
		"apiKey:list",
		"apiKey:create",
		"apiKey:delete",
		"apiKey:update",
		"aiAssistant:manage",
		"chatHub:manage",
		"encryptionKey:manage",
		"dataTable:create",
		"dataTable:delete",
		"dataTable:read",
		"dataTable:update",
		"dataTable:list",
		"dataTable:listProject",
		"dataTable:readRow",
		"dataTable:writeRow",
		"dataTable:readColumn",
		"dataTable:writeColumn",
		"execution:reveal",
		"externalSecret:list",
		"externalSecretsProvider:create",
		"externalSecretsProvider:read",
		"externalSecretsProvider:update",
		"externalSecretsProvider:delete",
		"externalSecretsProvider:list",
		"externalSecretsProvider:sync",
		"ldap:manage",
		"otel:manage",
		"folder:create",
		"folder:read",
		"folder:update",
		"folder:delete",
		"folder:list",
		"folder:move",
		"insights:list",
		"insights:read",
		"logStreaming:manage",
		"project:create",
		"project:read",
		"project:update",
		"project:delete",
		"project:list",
		"role:manage",
		"projectVariable:create",
		"projectVariable:read",
		"projectVariable:update",
		"projectVariable:delete",
		"projectVariable:list",
		"sourceControl:manage",
		"sourceControl:pull",
		"sourceControl:push",
		"securitySettings:manage",
		"tag:create",
		"tag:read",
		"tag:update",
		"tag:delete",
		"tag:list",
		"user:create",
		"user:read",
		"user:update",
		"user:delete",
		"user:list",
		"workersView:manage",
		"variable:create",
		"variable:read",
		"variable:update",
		"variable:delete",
		"variable:list",
		"workflow:create",
		"workflow:read",
		"workflow:export",
		"workflow:import",
		"workflow:update",
		"workflow:publish",
		"workflow:unpublish",
		"workflow:delete",
		"workflow:list",
		"workflow:share",
		"workflow:unshare",
		"workflow:execute",
		"workflow:execute-chat",
		"workflow:move",
		"workflow:enableRedaction",
		"workflow:disableRedaction",
	}
}

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.userStore.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"items": users,
			"count": len(users),
		},
	})
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	settings := s.defaultSettings()
	if err := s.applyStoredSettings(r.Context(), settings); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": settings})
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid settings body")
		return
	}
	if len(payload) == 0 {
		writeError(w, http.StatusBadRequest, "settings body is empty")
		return
	}
	overrides, err := s.storedSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	deepMerge(overrides, payload)
	data, err := json.Marshal(overrides)
	if err != nil {
		writeError(w, http.StatusBadRequest, "settings body is not serializable")
		return
	}
	if err := s.settingsStore.Set(r.Context(), "settings.overrides", string(data)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	settings := s.defaultSettings()
	deepMerge(settings, overrides)
	writeJSON(w, http.StatusOK, map[string]any{"data": settings})
}

func (s *Server) defaultSettings() map[string]any {
	timezone := firstNonEmpty(s.config.Instance.Timezone, "Europe/Bucharest")
	locale := firstNonEmpty(s.config.Instance.Locale, "en")
	logLevel := firstNonEmpty(s.config.Instance.LogLevel, "info")
	binaryMode := firstNonEmpty(s.config.BinaryData.Mode, "filesystem")
	instanceID := s.config.Instance.ID
	return map[string]any{
		"n8nVersion":                  "0.0.1-go-bootstrap",
		"n8nMetadata":                 map[string]any{"instanceId": instanceID},
		"executionMode":               "regular",
		"executionTimeout":            s.config.Execution.TimeoutSeconds,
		"maxExecutionTimeout":         s.config.Execution.MaxTimeoutSeconds,
		"timezone":                    timezone,
		"defaultLocale":               locale,
		"saveDataErrorExecution":      s.config.Execution.SaveOnError,
		"saveDataSuccessExecution":    s.config.Execution.SaveOnSuccess,
		"saveManualExecutions":        s.config.Execution.SaveManual,
		"saveExecutionProgress":       s.config.Execution.SaveProgress,
		"hiringBannerEnabled":         false,
		"versionNotificationsEnabled": true,
		"templatesEnabled":            true,
		"templatesHost":               "https://api.n8n.io",
		"onboardingCallPromptEnabled": false,
		"publicApi": map[string]any{
			"enabled":       true,
			"latestVersion": 1,
			"path":          "api",
			"swaggerUi":     map[string]any{"enabled": true},
		},
		"mfa":      map[string]any{"enabled": false, "enforced": false},
		"folders":  map[string]any{"enabled": false},
		"security": map[string]any{"blockFileAccessToN8nFiles": false},
		"userManagement": map[string]any{
			"enabled":              true,
			"showSetupOnFirstLoad": false,
			"smtpSetup":            false,
			"authenticationMethod": "email",
			"loginLabel":           "Email",
			"loginEnabled":         true,
			"passwordMinLength":    8,
			"passwordHashing":      map[string]any{"includePasswordsInLogs": false},
		},
		"saml": map[string]any{
			"enabled":      false,
			"loginLabel":   "Sign in with SAML SSO",
			"loginEnabled": false,
			"managedByEnv": false,
		},
		"sso": map[string]any{
			"enabled":      false,
			"loginLabel":   "SSO",
			"loginEnabled": false,
			"managedByEnv": false,
		},
		"ldap": map[string]any{
			"enabled":      false,
			"loginLabel":   "Sign in with LDAP",
			"loginEnabled": false,
			"managedByEnv": false,
		},
		"isNpmAvailable": isNpmAvailable(),
		"allowedModules": map[string]any{
			"builtins":        []string{},
			"externalModules": []string{},
		},
		"enterprise": map[string]any{
			"advancedExecutionFilters": true,
			"advancedPermissions":      true,
			"auditLogs":                true,
			"customRoles":              true,
			"dataRedaction":            true,
			"debugInEditor":            true,
			"externalSecrets":          true,
			"logStreaming":             true,
			"mfaEnforcement":           true,
			"namedVersions":            true,
			"provisioning":             true,
			"sharing":                  true,
			"sourceControl":            true,
			"variables":                true,
			"workerView":               true,
			"projects": map[string]any{
				"team": map[string]any{
					"limit": -1,
				},
			},
		},
		"variables":            map[string]any{"limit": 1000000},
		"workflowTagsDisabled": false,
		"logLevel":             logLevel,
		"deployment":           map[string]any{"type": "default"},
		"telemetry":            map[string]any{"enabled": false},
		"posthog":              map[string]any{"enabled": false, "apiKey": "", "apiHost": "https://eu.posthog.com", "featureFlagsPollingInterval": 3600000},
		"diagnostics":          map[string]any{"enabled": false, "backendEnabled": false, "frontendEnabled": false},
		"license": map[string]any{
			"environment":       "development",
			"consumerId":        "unknown",
			"planName":          "Community",
			"active":            false,
			"activatedAt":       nil,
			"expirationDate":    nil,
			"isCloudDeployment": false,
		},
		"banners": map[string]any{
			"dismissed": []string{},
			"endpoint":  "",
		},
		"versionNotifications": map[string]any{
			"enabled":          false,
			"endpoint":         "",
			"infoUrl":          "",
			"whatsNewEnabled":  false,
			"whatsNewEndpoint": "",
		},
		"dynamicBanners": map[string]any{
			"enabled":  false,
			"endpoint": "https://api.n8n.io/api/banners",
		},
		"externalSecretsEnabled":            true,
		"externalSecrets":                   map[string]any{"providers": []string{"env"}},
		"authCookie":                        map[string]any{"secure": s.config.Auth.CookieSecure, "sameSite": s.config.Auth.CookieSameSite},
		"bruteForceProtectionEnabled":       false,
		"workflowCallerPolicyDefaultOption": "any",
		"binaryDataMode":                    binaryMode,
		"community":                         map[string]any{"packagesEnabled": false},
		"communityNodesEnabled":             false,
		"unverifiedCommunityNodesEnabled":   false,
		"ai":                                map[string]any{"enabled": false, "provider": "", "errorDebugging": false},
		"aiAssistant":                       map[string]any{"enabled": false, "setup": false},
		"aiBuilder":                         map[string]any{"enabled": false, "setup": false},
		"askAi":                             map[string]any{"enabled": false},
		"aiCredits":                         map[string]any{"enabled": false, "setup": false, "credits": 0},
		"aiGateway":                         map[string]any{"enabled": false, "budget": 0},
		"instanceId":                        instanceID,
		"partnerUrl":                        "",
		"pushBackend":                       "websocket",
		"activeModules": []string{
			"data-table",
			"insights",
			"chat-hub",
			"external-secrets",
		},
		"featureFlags": map[string]any{
			"askAi":                    false,
			"ndv.v2":                   true,
			"canvas.v2":                true,
			"workflow.sharing":         true,
			"debugInEditor":            false,
			"advancedExecutionFilters": true,
			"concurrentExecution":      true,
			"binaryDataS3":             binaryMode == "s3",
		},
		"endpointWebhook":        "webhook",
		"endpointWebhookTest":    "webhook-test",
		"endpointWebhookWaiting": "webhook-waiting",
		"endpointForm":           "form",
		"endpointFormTest":       "form-test",
		"endpointFormWaiting":    "form-waiting",
		"endpointMcp":            "mcp",
		"endpointMcpTest":        "mcp-test",
		"endpointHealth":         "/healthz",
		"urlBaseWebhook":         firstNonEmpty(s.config.WebhookBaseURL, fmt.Sprintf("%s://%s/", s.config.Listen.Protocol, s.config.Listen.Address())),
		"urlBaseEditor":          firstNonEmpty(s.config.EditorBaseURL, fmt.Sprintf("%s://%s/", s.config.Listen.Protocol, s.config.Listen.Address())),
		"versionCli":             runtime.Version(),
		"nodeJsVersion":          "",
		"nodeEnv":                "production",
		"concurrency":            s.config.Execution.ConcurrencyLimit,
		"pruning":                map[string]any{"isEnabled": false},
		"instanceType":           runtime.GOOS,
		"editorBaseUrl":          firstNonEmpty(s.config.EditorBaseURL, fmt.Sprintf("%s://%s", s.config.Listen.Protocol, s.config.Listen.Address())),
		"templates":              map[string]any{"enabled": true, "host": "https://api.n8n.io"},
		"frontendDir":            s.config.FrontendDir,
		"frontendDirExists":      pathExists(s.config.FrontendDir),
		"binaryDataPath":         s.config.BinaryData.Path,
		"osUser":                 os.Getenv("USERNAME"),
	}
}

func isNpmAvailable() bool {
	name := "npm"
	if runtime.GOOS == "windows" {
		name = "npm.cmd"
	}
	_, err := exec.LookPath(name)
	return err == nil
}

func (s *Server) applyStoredSettings(ctx context.Context, settings map[string]any) error {
	overrides, err := s.storedSettings(ctx)
	if err != nil {
		return err
	}
	deepMerge(settings, overrides)
	return nil
}

func (s *Server) storedSettings(ctx context.Context) (map[string]any, error) {
	if s.settingsStore == nil {
		return map[string]any{}, nil
	}
	raw, err := s.settingsStore.Get(ctx, "settings.overrides")
	if err != nil {
		if errors.Is(err, persistence.ErrNotFound) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	result := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, err
	}
	return result, nil
}

func deepMerge(target map[string]any, patch map[string]any) {
	for key, value := range patch {
		valueMap, valueIsMap := value.(map[string]any)
		targetMap, targetIsMap := target[key].(map[string]any)
		if valueIsMap && targetIsMap {
			deepMerge(targetMap, valueMap)
			continue
		}
		target[key] = value
	}
}

func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = randomRequestID()
		}
		w.Header().Set("X-Request-ID", id)
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDContextKey{}, id)))
	})
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With, X-Request-ID, Accept")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID")
		w.Header().Set("Access-Control-Max-Age", "86400")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type requestIDContextKey struct{}

func requestIDFromContext(ctx context.Context) string {
	if value, ok := ctx.Value(requestIDContextKey{}).(string); ok {
		return value
	}
	return ""
}

func randomRequestID() string {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(data)
}

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf("panic recovered method=%s path=%s requestId=%s error=%v", r.Method, r.URL.Path, requestIDFromContext(r.Context()), recovered)
				writeError(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

func bodyLimit(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > limit {
				writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
				return
			}
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
			next.ServeHTTP(w, r)
		})
	}
}

var gzipPool = sync.Pool{
	New: func() any {
		writer, _ := gzip.NewWriterLevel(io.Discard, gzip.DefaultCompression)
		return writer
	},
}

func compression(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(strings.ToLower(r.Header.Get("Accept-Encoding")), "gzip") || r.Header.Get("Upgrade") != "" {
			next.ServeHTTP(w, r)
			return
		}
		writer := gzipPool.Get().(*gzip.Writer)
		defer gzipPool.Put(writer)
		writer.Reset(w)
		defer writer.Close()
		w.Header().Add("Vary", "Accept-Encoding")
		next.ServeHTTP(&gzipResponseWriter{ResponseWriter: w, writer: writer}, r)
	})
}

type gzipResponseWriter struct {
	http.ResponseWriter
	writer      *gzip.Writer
	wroteHeader bool
	passthrough bool
}

func (w *gzipResponseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	if status == http.StatusNoContent || status == http.StatusNotModified || status < http.StatusOK {
		w.passthrough = true
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Del("Content-Length")
	w.ResponseWriter.WriteHeader(status)
}

func (w *gzipResponseWriter) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if w.passthrough {
		return w.ResponseWriter.Write(data)
	}
	return w.writer.Write(data)
}

func (w *gzipResponseWriter) Flush() {
	if !w.passthrough {
		_ = w.writer.Flush()
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.rateLimiter == nil {
			next.ServeHTTP(w, r)
			return
		}
		allowed, remaining, retryAfter := s.rateLimiter.Take(clientIP(r))
		w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", s.rateLimiter.limit))
		w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
		if !allowed {
			w.Header().Set("Retry-After", fmt.Sprintf("%.0f", retryAfter.Seconds()))
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type rateLimiter struct {
	limit      int
	window     time.Duration
	burst      float64
	refillRate float64
	mu         sync.Mutex
	buckets    map[string]rateBucket
	lastClean  time.Time
}

type rateBucket struct {
	tokens     float64
	lastRefill time.Time
	lastSeen   time.Time
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	if limit <= 0 {
		limit = 1
	}
	if window <= 0 {
		window = time.Minute
	}
	now := time.Now()
	return &rateLimiter{
		limit:      limit,
		window:     window,
		burst:      float64(limit),
		refillRate: float64(limit) / window.Seconds(),
		buckets:    make(map[string]rateBucket),
		lastClean:  now,
	}
}

func (r *rateLimiter) Allow(key string) bool {
	allowed, _, _ := r.Take(key)
	return allowed
}

func (r *rateLimiter) Take(key string) (bool, int, time.Duration) {
	if key == "" {
		key = "unknown"
	}
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	bucket := r.buckets[key]
	if bucket.lastRefill.IsZero() {
		bucket = rateBucket{tokens: r.burst, lastRefill: now}
	}
	elapsed := now.Sub(bucket.lastRefill).Seconds()
	if elapsed > 0 {
		bucket.tokens = minFloat(r.burst, bucket.tokens+elapsed*r.refillRate)
		bucket.lastRefill = now
	}
	bucket.lastSeen = now
	allowed := bucket.tokens >= 1
	retryAfter := time.Duration(0)
	if allowed {
		bucket.tokens--
	} else {
		missing := 1 - bucket.tokens
		retryAfter = time.Duration(missing/r.refillRate*float64(time.Second) + float64(time.Second-1))
		retryAfter = retryAfter.Truncate(time.Second)
		if retryAfter < time.Second {
			retryAfter = time.Second
		}
	}
	r.buckets[key] = bucket
	if now.Sub(r.lastClean) > 5*time.Minute {
		r.cleanup(now)
		r.lastClean = now
	}
	return allowed, int(bucket.tokens), retryAfter
}

func (r *rateLimiter) cleanup(now time.Time) {
	for key, bucket := range r.buckets {
		if now.Sub(bucket.lastSeen) > 10*time.Minute {
			delete(r.buckets, key)
		}
	}
}

func minFloat(a float64, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		duration := time.Since(started)
		log.Printf("http request method=%s path=%s status=%d duration=%s bytes=%d ip=%s requestId=%s", r.Method, r.URL.Path, recorder.status, duration, recorder.bytes, clientIP(r), requestIDFromContext(r.Context()))
		if s.logStream == nil {
			return
		}
		event := s.logStream.Emit(logstream.EventHTTPRequest, map[string]any{
			"method":     r.Method,
			"path":       r.URL.Path,
			"status":     recorder.status,
			"durationMs": duration.Milliseconds(),
			"bytes":      recorder.bytes,
			"ip":         clientIP(r),
			"requestId":  requestIDFromContext(r.Context()),
		})
		if s.pushHub != nil {
			s.pushHub.Publish(push.Message{Type: push.EventLogStream, Data: event})
		}
	})
}

func (s *Server) auditMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		if s.auditStore == nil || recorder.status >= 400 || !auditMethod(r.Method) || !strings.HasPrefix(r.URL.Path, "/rest/") {
			return
		}
		eventType, resourceType, resourceID := auditEventForRequest(r)
		if eventType == "" {
			return
		}
		user, _ := auth.UserFromContext(r.Context())
		s.logAudit(r, audit.Event{
			EventType:    eventType,
			UserID:       user.ID,
			UserEmail:    user.Email,
			ResourceType: resourceType,
			ResourceID:   resourceID,
			Metadata:     map[string]any{"method": r.Method, "path": r.URL.Path, "status": recorder.status},
		})
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status      int
	bytes       int64
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.wroteHeader {
		return
	}
	r.wroteHeader = true
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(data []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	written, err := r.ResponseWriter.Write(data)
	r.bytes += int64(written)
	return written, err
}

func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("hijacker is not available")
	}
	return hijacker.Hijack()
}

func (r *statusRecorder) Push(target string, opts *http.PushOptions) error {
	pusher, ok := r.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, opts)
}

func auditMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func auditEventForRequest(r *http.Request) (audit.EventType, audit.ResourceType, string) {
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] != "rest" {
		return "", "", ""
	}
	resourceID := ""
	if len(parts) >= 3 {
		resourceID = parts[2]
	}
	switch parts[1] {
	case "workflows":
		if len(parts) >= 4 && parts[3] == "activate" {
			return audit.EventWorkflowActivated, audit.ResourceWorkflow, resourceID
		}
		if len(parts) >= 4 && parts[3] == "deactivate" {
			return audit.EventWorkflowDeactivated, audit.ResourceWorkflow, resourceID
		}
		if r.Method == http.MethodPost {
			return audit.EventWorkflowCreated, audit.ResourceWorkflow, resourceID
		}
		return audit.EventWorkflowUpdated, audit.ResourceWorkflow, resourceID
	case "credentials":
		if r.Method == http.MethodDelete {
			return audit.EventCredentialDeleted, audit.ResourceCredential, resourceID
		}
		if r.Method == http.MethodPost {
			return audit.EventCredentialCreated, audit.ResourceCredential, resourceID
		}
		return audit.EventCredentialUpdated, audit.ResourceCredential, resourceID
	case "variables":
		if r.Method == http.MethodDelete {
			return audit.EventVariableDeleted, audit.ResourceVariable, resourceID
		}
		if r.Method == http.MethodPost {
			return audit.EventVariableCreated, audit.ResourceVariable, resourceID
		}
		return audit.EventVariableUpdated, audit.ResourceVariable, resourceID
	case "executions":
		if len(parts) >= 4 && parts[3] == "stop" {
			return audit.EventExecutionError, audit.ResourceExecution, resourceID
		}
	}
	return "", "", ""
}

func (s *Server) logAudit(r *http.Request, event audit.Event) {
	if s.auditStore == nil {
		return
	}
	event.UserAgent = r.UserAgent()
	event.IP = clientIP(r)
	_, _ = s.auditStore.Log(r.Context(), event)
}

func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		return strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		return realIP
	}
	return r.RemoteAddr
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
