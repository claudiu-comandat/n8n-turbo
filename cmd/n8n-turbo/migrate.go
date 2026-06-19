package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/config"
	"github.com/n8n-io/n8n-turbo/internal/credentials"
	"github.com/n8n-io/n8n-turbo/internal/migration"
	"github.com/n8n-io/n8n-turbo/internal/persistence/sqlite"
)

func runMigrate(args []string) error {
	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	jsonOutput := flags.Bool("json", false, "print JSON result")
	backup := flags.Bool("backup", false, "create SQLite backup before checking")
	backupDir := flags.String("backup-dir", "", "backup directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if strings.ToLower(cfg.Database.Type) != "sqlite" {
		return fmt.Errorf("migrate currently supports sqlite databases only")
	}
	db, err := sqlite.Open(cfg.Database.SQLitePath)
	if err != nil {
		return err
	}
	defer db.Close()
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if *backup {
		result, err := migration.BackupSQLite(ctx, db, cfg.Database.SQLitePath, *backupDir, time.Now().UTC())
		if err != nil {
			return err
		}
		if !*jsonOutput {
			fmt.Println("Backup created:")
			for _, file := range result.Files {
				fmt.Println("  " + file)
			}
			fmt.Println()
		}
	}
	checker := migration.NewChecker(db, logger)
	result, err := checker.Check(ctx)
	if err != nil {
		return err
	}
	if cfg.EncryptionKey != "" {
		if warning := credentialWarning(ctx, db, cfg.EncryptionKey); warning != "" {
			result.Warnings = append(result.Warnings, warning)
		}
	}
	if *jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(result); err != nil {
			return err
		}
	} else {
		printMigrationResult(result)
	}
	if !result.Compatible {
		return fmt.Errorf("database is not compatible")
	}
	return nil
}

func printMigrationResult(result *migration.Result) {
	fmt.Println("=== n8n-turbo migration check ===")
	fmt.Println()
	fmt.Println("Tables:")
	keys := make([]string, 0, len(result.TableStats))
	for key := range result.TableStats {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Printf("  %-28s %d rows\n", key, result.TableStats[key])
	}
	fmt.Println()
	fmt.Printf("Execution data: total=%d flatted=%d invalid=%d null=%d\n", result.ExecutionStats.Total, result.ExecutionStats.FlattedValid, result.ExecutionStats.Invalid, result.ExecutionStats.NullData)
	if result.UserStats.Total > 0 {
		fmt.Printf("Users: total=%d bcrypt=%d no_password=%d\n", result.UserStats.Total, result.UserStats.BcryptValid, result.UserStats.NoPassword)
	}
	if len(result.CredentialStats) > 0 {
		fmt.Println("Credentials:")
		for _, stat := range result.CredentialStats {
			fmt.Printf("  %-28s %d rows, %d JSON-valid\n", stat.Type, stat.Count, stat.JSONValid)
		}
	}
	if len(result.Warnings) > 0 {
		fmt.Println()
		fmt.Println("Warnings:")
		for _, warning := range result.Warnings {
			fmt.Println("  WARN: " + warning)
		}
	}
	if len(result.Errors) > 0 {
		fmt.Println()
		fmt.Println("Errors:")
		for _, problem := range result.Errors {
			fmt.Println("  ERROR: " + problem)
		}
	}
	fmt.Println()
	if result.Compatible {
		fmt.Println("OK: database is compatible with the n8n-turbo migration checks.")
	} else {
		fmt.Println("FAIL: database is not compatible. Fix the errors before starting n8n-turbo.")
	}
}

func credentialWarning(ctx context.Context, db *sql.DB, encryptionKey string) string {
	var encrypted string
	if err := db.QueryRowContext(ctx, "SELECT data FROM credentials_entity LIMIT 1").Scan(&encrypted); err != nil {
		return ""
	}
	if strings.HasPrefix(strings.TrimSpace(encrypted), "{") || strings.HasPrefix(strings.TrimSpace(encrypted), "[") {
		return ""
	}
	vault, err := credentials.NewVault(encryptionKey)
	if err != nil {
		return "credential decrypt test skipped: " + err.Error()
	}
	plain, err := vault.Decrypt(encrypted)
	if err != nil {
		return "first credential cannot be decrypted with N8N_ENCRYPTION_KEY: " + err.Error()
	}
	var decoded any
	if err := json.Unmarshal([]byte(plain), &decoded); err != nil {
		return "first credential decrypted but payload is not JSON: " + err.Error()
	}
	return ""
}
