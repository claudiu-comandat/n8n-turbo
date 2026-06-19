package main

import (
	"context"
	"database/sql"
	"log"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/api"
	"github.com/n8n-io/n8n-turbo/internal/auth"
	"github.com/n8n-io/n8n-turbo/internal/config"
	"github.com/n8n-io/n8n-turbo/internal/credentials"
	"github.com/n8n-io/n8n-turbo/internal/migration"
	"github.com/n8n-io/n8n-turbo/internal/persistence/postgres"
	"github.com/n8n-io/n8n-turbo/internal/persistence/sqlite"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		if err := runMigrate(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	db, err := openDatabase(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	userStore := sqlite.NewUserStore(db)
	if err := userStore.Init(context.Background()); err != nil {
		log.Fatal(err)
	}

	settingsStore := sqlite.NewSettingsStore(db)
	if err := settingsStore.Init(context.Background()); err != nil {
		log.Fatal(err)
	}

	workflowStore := sqlite.NewWorkflowStore(db)
	if err := workflowStore.Init(context.Background()); err != nil {
		log.Fatal(err)
	}

	executionStore := sqlite.NewExecutionStore(db)
	if err := executionStore.Init(context.Background()); err != nil {
		log.Fatal(err)
	}
	cleanup := migration.NewStartupCleanup(db, slog.Default())
	if _, err := cleanup.CleanOrphanedExecutions(context.Background()); err != nil {
		log.Fatal(err)
	}

	credentialStore := sqlite.NewCredentialStore(db)
	if err := credentialStore.Init(context.Background()); err != nil {
		log.Fatal(err)
	}

	vault, err := credentials.NewVault(cfg.EncryptionKey)
	if err != nil {
		log.Fatal(err)
	}

	variableStore := sqlite.NewVariableStoreWithVault(db, vault)
	if err := variableStore.Init(context.Background()); err != nil {
		log.Fatal(err)
	}

	tagStore := sqlite.NewTagStore(db)
	if err := tagStore.Init(context.Background()); err != nil {
		log.Fatal(err)
	}

	auditStore := sqlite.NewAuditStore(db)
	if err := auditStore.Init(context.Background()); err != nil {
		log.Fatal(err)
	}
	insightsStore := sqlite.NewInsightsStore(db)

	authService := auth.NewService(userStore, cfg.EncryptionKey, cfg.Auth)

	server, err := api.NewServer(cfg, authService, userStore, settingsStore, workflowStore, executionStore, credentialStore, variableStore, tagStore, auditStore, insightsStore, vault)
	if err != nil {
		log.Fatal(err)
	}
	runtimeCtx, stopRuntime := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopRuntime()
	server.StartRuntime(runtimeCtx)
	startPprof()

	httpServer := &http.Server{
		Addr:              cfg.Listen.Address(),
		Handler:           server.Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	<-runtimeCtx.Done()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
}

func openDatabase(cfg config.Config) (*sql.DB, error) {
	switch cfg.Database.Type {
	case "postgres", "postgresdb":
		return postgres.Open(cfg.Database.PostgresDSN)
	default:
		return sqlite.Open(cfg.Database.SQLitePath)
	}
}
