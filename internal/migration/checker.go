package migration

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
)

type Checker struct {
	db     *sql.DB
	logger *slog.Logger
}

func NewChecker(db *sql.DB, logger *slog.Logger) *Checker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Checker{db: db, logger: logger}
}

func (c *Checker) Check(ctx context.Context) (*Result, error) {
	result := &Result{
		Compatible: true,
		TableStats: make(map[string]int64),
		CheckedAt:  time.Now().UTC(),
	}
	if err := c.checkRequiredTables(ctx, result); err != nil {
		return nil, fmt.Errorf("check tables: %w", err)
	}
	if err := c.collectStats(ctx, result); err != nil {
		return nil, fmt.Errorf("collect stats: %w", err)
	}
	if err := c.checkExecutionData(ctx, result); err != nil {
		return nil, fmt.Errorf("check execution data: %w", err)
	}
	if err := c.checkExecutionStatus(ctx, result); err != nil {
		return nil, fmt.Errorf("check execution status: %w", err)
	}
	if err := c.checkCredentials(ctx, result); err != nil {
		return nil, fmt.Errorf("check credentials: %w", err)
	}
	if err := c.checkUsers(ctx, result); err != nil {
		return nil, fmt.Errorf("check users: %w", err)
	}
	if err := c.checkVariables(ctx, result); err != nil {
		return nil, fmt.Errorf("check variables: %w", err)
	}
	if err := c.checkWebhookDuplicates(ctx, result); err != nil {
		return nil, fmt.Errorf("check webhooks: %w", err)
	}
	if len(result.Errors) > 0 {
		result.Compatible = false
	}
	return result, nil
}

func (c *Checker) checkRequiredTables(ctx context.Context, result *Result) error {
	checks := []struct {
		table    string
		critical [][]string
		warn     [][]string
	}{
		{"workflow_entity", [][]string{{"id"}, {"name"}, {"active"}, {"nodes"}, {"connections"}, {"settings"}}, [][]string{{"staticData", "static_data"}, {"pinData", "pin_data"}, {"versionId", "version_id"}, {"createdAt", "created_at"}, {"updatedAt", "updated_at"}}},
		{"execution_entity", [][]string{{"id"}, {"finished"}, {"mode"}, {"status"}}, [][]string{{"workflowId", "workflow_id"}, {"retryOf", "retry_of"}, {"retrySuccessId", "retry_success_id"}, {"startedAt", "started_at"}, {"stoppedAt", "stopped_at"}, {"waitTill", "wait_till"}}},
		{"execution_data", [][]string{{"data"}}, [][]string{{"executionId", "execution_id"}, {"workflowData", "workflow_data"}}},
		{"credentials_entity", [][]string{{"id"}, {"name"}, {"type"}, {"data"}}, [][]string{{"nodesAccess", "nodes_access"}, {"createdAt", "created_at"}, {"updatedAt", "updated_at"}}},
		{"user", [][]string{{"id"}, {"email"}, {"password"}, {"role"}}, [][]string{{"firstName", "first_name"}, {"lastName", "last_name"}, {"createdAt", "created_at"}, {"updatedAt", "updated_at"}}},
		{"webhook_entity", [][]string{{"method"}, {"webhookPath", "webhook_path"}}, [][]string{{"workflowId", "workflow_id"}, {"webhookId", "webhook_id"}}},
		{"settings", [][]string{{"key"}, {"value"}}, nil},
	}
	for _, check := range checks {
		exists, err := c.tableExists(ctx, check.table)
		if err != nil {
			return err
		}
		tableCheck := TableCheck{Table: check.table, Exists: exists}
		if !exists {
			result.Errors = append(result.Errors, fmt.Sprintf("missing required table %q", check.table))
			result.TableChecks = append(result.TableChecks, tableCheck)
			continue
		}
		columns, err := c.columns(ctx, check.table)
		if err != nil {
			return err
		}
		for _, group := range check.critical {
			if !hasAnyColumn(columns, group...) {
				tableCheck.MissingColumns = append(tableCheck.MissingColumns, strings.Join(group, " or "))
			}
		}
		for _, group := range check.warn {
			if !hasAnyColumn(columns, group...) {
				tableCheck.Warnings = append(tableCheck.Warnings, "missing optional compatibility column "+strings.Join(group, " or "))
			}
		}
		if len(tableCheck.MissingColumns) > 0 {
			result.Errors = append(result.Errors, fmt.Sprintf("%s missing columns: %s", check.table, strings.Join(tableCheck.MissingColumns, ", ")))
		}
		for _, warning := range tableCheck.Warnings {
			result.Warnings = append(result.Warnings, check.table+": "+warning)
		}
		result.TableChecks = append(result.TableChecks, tableCheck)
	}
	return nil
}

func (c *Checker) collectStats(ctx context.Context, result *Result) error {
	tables := []string{"workflow_entity", "execution_entity", "credentials_entity", "user", "variables", "webhook_entity", "tag_entity", "workflows_tags", "settings", "installed_nodes", "installed_packages", "event_destinations", "migrations"}
	for _, table := range tables {
		exists, err := c.tableExists(ctx, table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		var count int64
		if err := c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quoteIdent(table)).Scan(&count); err != nil {
			return err
		}
		result.TableStats[table] = count
	}
	return nil
}

func (c *Checker) checkExecutionData(ctx context.Context, result *Result) error {
	exists, err := c.tableExists(ctx, "execution_data")
	if err != nil || !exists {
		return err
	}
	query := `
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN typeof(data) = 'text' AND substr(data, 1, 1) = '[' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN typeof(data) = 'text' AND data IS NOT NULL AND substr(data, 1, 1) != '[' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN data IS NULL THEN 1 ELSE 0 END), 0)
		FROM execution_data`
	err = c.db.QueryRowContext(ctx, query).Scan(&result.ExecutionStats.Total, &result.ExecutionStats.FlattedValid, &result.ExecutionStats.Invalid, &result.ExecutionStats.NullData)
	if err != nil {
		return err
	}
	if result.ExecutionStats.Invalid > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("%d execution_data rows are not flatted-compatible", result.ExecutionStats.Invalid))
	}
	if result.ExecutionStats.NullData > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("%d execution_data rows have null data", result.ExecutionStats.NullData))
	}
	return nil
}

func (c *Checker) checkExecutionStatus(ctx context.Context, result *Result) error {
	exists, err := c.tableExists(ctx, "execution_entity")
	if err != nil || !exists {
		return err
	}
	rows, err := c.db.QueryContext(ctx, `
		SELECT COALESCE(status, ''), COALESCE(finished, 0), COUNT(*)
		FROM execution_entity
		GROUP BY status, finished
		ORDER BY status, finished`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var stat StatusStat
		var finished int
		if err := rows.Scan(&stat.Status, &finished, &stat.Count); err != nil {
			return err
		}
		stat.Finished = finished != 0
		result.ExecutionStatus = append(result.ExecutionStatus, stat)
		if stat.Finished && !isFinishedStatus(stat.Status) {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%d finished executions have status %q", stat.Count, stat.Status))
		}
		if !stat.Finished && !isOpenStatus(stat.Status) {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%d unfinished executions have status %q", stat.Count, stat.Status))
		}
	}
	return rows.Err()
}

func (c *Checker) checkCredentials(ctx context.Context, result *Result) error {
	exists, err := c.tableExists(ctx, "credentials_entity")
	if err != nil || !exists {
		return err
	}
	rows, err := c.db.QueryContext(ctx, `
		SELECT COALESCE(type, ''), COUNT(*), COALESCE(SUM(CASE WHEN json_valid(data) THEN 1 ELSE 0 END), 0)
		FROM credentials_entity
		GROUP BY type
		ORDER BY COUNT(*) DESC`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var stat CredentialStat
		if err := rows.Scan(&stat.Type, &stat.Count, &stat.JSONValid); err != nil {
			return err
		}
		result.CredentialStats = append(result.CredentialStats, stat)
		if stat.JSONValid < stat.Count {
			result.Warnings = append(result.Warnings, fmt.Sprintf("credential type %q has %d invalid JSON payloads", stat.Type, stat.Count-stat.JSONValid))
		}
	}
	return rows.Err()
}

func (c *Checker) checkUsers(ctx context.Context, result *Result) error {
	exists, err := c.tableExists(ctx, "user")
	if err != nil || !exists {
		return err
	}
	return c.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN password LIKE '$2a$%' OR password LIKE '$2b$%' OR password LIKE '$2y$%' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN password IS NULL OR password = '' THEN 1 ELSE 0 END), 0)
		FROM user`).Scan(&result.UserStats.Total, &result.UserStats.BcryptValid, &result.UserStats.NoPassword)
}

func (c *Checker) checkVariables(ctx context.Context, result *Result) error {
	exists, err := c.tableExists(ctx, "variables")
	if err != nil || !exists {
		return err
	}
	if has, err := c.hasColumn(ctx, "variables", "type"); err != nil || !has {
		return err
	}
	rows, err := c.db.QueryContext(ctx, `
		SELECT COALESCE(type, ''), COUNT(*)
		FROM variables
		GROUP BY type
		ORDER BY type`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var stat VariableStat
		if err := rows.Scan(&stat.Type, &stat.Count); err != nil {
			return err
		}
		result.VariableStats = append(result.VariableStats, stat)
		if !validVariableType(stat.Type) {
			result.Warnings = append(result.Warnings, fmt.Sprintf("variables table contains non-standard type %q", stat.Type))
		}
	}
	return rows.Err()
}

func (c *Checker) checkWebhookDuplicates(ctx context.Context, result *Result) error {
	exists, err := c.tableExists(ctx, "webhook_entity")
	if err != nil || !exists {
		return err
	}
	columns, err := c.columns(ctx, "webhook_entity")
	if err != nil {
		return err
	}
	pathColumn := firstColumn(columns, "webhookPath", "webhook_path")
	methodColumn := firstColumn(columns, "method")
	if pathColumn == "" || methodColumn == "" {
		return nil
	}
	query := fmt.Sprintf(`
		SELECT COALESCE(%s, ''), COALESCE(%s, ''), COUNT(*) AS cnt
		FROM webhook_entity
		GROUP BY %s, %s
		HAVING cnt > 1`, quoteIdent(methodColumn), quoteIdent(pathColumn), quoteIdent(methodColumn), quoteIdent(pathColumn))
	rows, err := c.db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var duplicate WebhookDuplicate
		if err := rows.Scan(&duplicate.Method, &duplicate.Path, &duplicate.Count); err != nil {
			return err
		}
		result.WebhookDuplicates = append(result.WebhookDuplicates, duplicate)
		result.Errors = append(result.Errors, fmt.Sprintf("duplicate webhook %s %s appears %d times", duplicate.Method, duplicate.Path, duplicate.Count))
	}
	return rows.Err()
}

func (c *Checker) tableExists(ctx context.Context, table string) (bool, error) {
	var count int
	err := c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count)
	return count > 0, err
}

func (c *Checker) columns(ctx context.Context, table string) (map[string]bool, error) {
	rows, err := c.db.QueryContext(ctx, "PRAGMA table_info("+quoteIdent(table)+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name string
		var typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	return columns, rows.Err()
}

func (c *Checker) hasColumn(ctx context.Context, table string, names ...string) (bool, error) {
	columns, err := c.columns(ctx, table)
	if err != nil {
		return false, err
	}
	return hasAnyColumn(columns, names...), nil
}

func hasAnyColumn(columns map[string]bool, names ...string) bool {
	for _, name := range names {
		if columns[name] {
			return true
		}
	}
	return false
}

func firstColumn(columns map[string]bool, names ...string) string {
	for _, name := range names {
		if columns[name] {
			return name
		}
	}
	return ""
}

func quoteIdent(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func isFinishedStatus(status string) bool {
	switch status {
	case "success", "error", "canceled", "crashed":
		return true
	default:
		return false
	}
}

func isOpenStatus(status string) bool {
	switch status {
	case "new", "running", "waiting", "unknown":
		return true
	default:
		return false
	}
}

func validVariableType(value string) bool {
	switch value {
	case "string", "number", "boolean", "secret":
		return true
	default:
		return false
	}
}

func sortedStats(stats map[string]int64) []string {
	keys := make([]string, 0, len(stats))
	for key := range stats {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
