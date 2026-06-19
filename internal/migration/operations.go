package migration

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

type StartupCleanup struct {
	db     *sql.DB
	logger *slog.Logger
}

type BackupResult struct {
	Files     []string  `json:"files"`
	CreatedAt time.Time `json:"createdAt"`
}

func NewStartupCleanup(db *sql.DB, logger *slog.Logger) *StartupCleanup {
	if logger == nil {
		logger = slog.Default()
	}
	return &StartupCleanup{db: db, logger: logger}
}

func (c *StartupCleanup) CleanOrphanedExecutions(ctx context.Context) (int64, error) {
	checker := NewChecker(c.db, c.logger)
	exists, err := checker.tableExists(ctx, "execution_entity")
	if err != nil || !exists {
		return 0, err
	}
	columns, err := checker.columns(ctx, "execution_entity")
	if err != nil {
		return 0, err
	}
	stoppedColumn := firstColumn(columns, "stoppedAt", "stopped_at")
	waitColumn := firstColumn(columns, "waitTill", "wait_till")
	if stoppedColumn == "" || waitColumn == "" || !hasAnyColumn(columns, "finished") || !hasAnyColumn(columns, "status") {
		return 0, nil
	}
	query := fmt.Sprintf(`
		UPDATE execution_entity
		SET finished = 1, status = 'error', %s = ?
		WHERE finished = 0 AND status IN ('running', 'new') AND %s IS NULL`,
		quoteIdent(stoppedColumn),
		quoteIdent(waitColumn),
	)
	result, err := c.db.ExecContext(ctx, query, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, fmt.Errorf("clean orphaned executions: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected > 0 {
		c.logger.Warn("orphaned executions marked as error", "count", affected)
	}
	return affected, nil
}

func BackupSQLite(ctx context.Context, db *sql.DB, dbPath string, backupDir string, now time.Time) (*BackupResult, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("database path is required")
	}
	if backupDir == "" {
		backupDir = filepath.Join(filepath.Dir(dbPath), "backups")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return nil, fmt.Errorf("create backup dir: %w", err)
	}
	if db != nil {
		if _, err := db.ExecContext(ctx, "PRAGMA wal_checkpoint(FULL)"); err != nil {
			return nil, fmt.Errorf("checkpoint sqlite wal: %w", err)
		}
	}
	base := filepath.Join(backupDir, "n8n_backup_"+now.Format("20060102_150405")+".sqlite")
	if err := copyFile(dbPath, base); err != nil {
		return nil, err
	}
	result := &BackupResult{Files: []string{base}, CreatedAt: now.UTC()}
	for _, suffix := range []string{"-wal", "-shm"} {
		source := dbPath + suffix
		if _, err := os.Stat(source); err == nil {
			target := base + suffix
			if err := copyFile(source, target); err != nil {
				return nil, err
			}
			result.Files = append(result.Files, target)
		}
	}
	return result, nil
}

func copyFile(source string, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open backup source %s: %w", source, err)
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create backup target %s: %w", target, err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy backup %s: %w", source, err)
	}
	if err := out.Sync(); err != nil {
		return fmt.Errorf("sync backup %s: %w", target, err)
	}
	return nil
}
