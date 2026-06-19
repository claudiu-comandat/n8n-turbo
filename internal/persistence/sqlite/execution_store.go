package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/flatted"
	"github.com/n8n-io/n8n-turbo/internal/persistence"
)

type ExecutionStore struct {
	db *sql.DB
}

func NewExecutionStore(db *sql.DB) *ExecutionStore {
	return &ExecutionStore{db: db}
}

func (s *ExecutionStore) Init(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS execution_entity (
			id TEXT PRIMARY KEY,
			workflow_id TEXT NOT NULL,
			finished INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL,
			mode TEXT NOT NULL,
			retry_of TEXT,
			retry_success_id TEXT,
			started_at TEXT NOT NULL,
			stopped_at TEXT,
			wait_till TEXT,
			deleted_at TEXT,
			custom_data TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS execution_data (
			execution_id TEXT PRIMARY KEY,
			workflow_data TEXT NOT NULL,
			data TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_execution_workflow ON execution_entity(workflow_id);
		CREATE INDEX IF NOT EXISTS idx_execution_status ON execution_entity(status);
		CREATE INDEX IF NOT EXISTS idx_execution_finished ON execution_entity(finished, started_at);
		CREATE INDEX IF NOT EXISTS idx_execution_created ON execution_entity(created_at);`)
	if err != nil {
		return fmt.Errorf("init execution tables: %w", err)
	}
	for _, statement := range []string{
		`ALTER TABLE execution_entity ADD COLUMN finished INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE execution_entity ADD COLUMN retry_of TEXT`,
		`ALTER TABLE execution_entity ADD COLUMN retry_success_id TEXT`,
		`ALTER TABLE execution_entity ADD COLUMN wait_till TEXT`,
		`ALTER TABLE execution_entity ADD COLUMN deleted_at TEXT`,
		`ALTER TABLE execution_entity ADD COLUMN custom_data TEXT NOT NULL DEFAULT '{}'`,
	} {
		_, _ = s.db.ExecContext(ctx, statement)
	}
	return nil
}

func (s *ExecutionStore) Create(ctx context.Context, workflow dataplane.Workflow, mode string) (*persistence.ExecutionRow, error) {
	id := newID()
	now := time.Now().UTC()
	workflowData := mustJSON(workflow, "{}")
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO execution_entity (id, workflow_id, finished, status, mode, started_at, created_at, custom_data)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id,
		workflow.ID,
		0,
		"running",
		mode,
		now.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
		"{}",
	)
	if err != nil {
		return nil, fmt.Errorf("create execution: %w", err)
	}
	initialData, err := flattedExecutionData(dataplane.RunExecutionData{})
	if err != nil {
		return nil, err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO execution_data (execution_id, workflow_data, data)
		VALUES (?, ?, ?)`, id, workflowData, initialData)
	if err != nil {
		return nil, fmt.Errorf("create execution data: %w", err)
	}
	return s.GetByID(ctx, id)
}

func (s *ExecutionStore) Finish(ctx context.Context, id string, status string, stoppedAt time.Time, data dataplane.RunExecutionData) error {
	payload, err := flattedExecutionData(data)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE execution_entity SET status = ?, finished = 1, stopped_at = ?, wait_till = NULL WHERE id = ? AND deleted_at IS NULL`, status, stoppedAt.UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("finish execution: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return persistence.ErrNotFound
	}
	_, err = s.db.ExecContext(ctx, `UPDATE execution_data SET data = ? WHERE execution_id = ?`, payload, id)
	if err != nil {
		return fmt.Errorf("finish execution data: %w", err)
	}
	return nil
}

func (s *ExecutionStore) MarkWaiting(ctx context.Context, id string, waitTill time.Time, data dataplane.RunExecutionData) error {
	payload, err := flattedExecutionData(data)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE execution_entity SET status = ?, finished = 0, stopped_at = NULL, wait_till = ? WHERE id = ? AND deleted_at IS NULL`, "waiting", waitTill.UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("mark execution waiting: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return persistence.ErrNotFound
	}
	_, err = s.db.ExecContext(ctx, `UPDATE execution_data SET data = ? WHERE execution_id = ?`, payload, id)
	if err != nil {
		return fmt.Errorf("mark execution waiting data: %w", err)
	}
	return nil
}

func (s *ExecutionStore) SetRetryOf(ctx context.Context, id string, retryOf string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE execution_entity SET retry_of = ? WHERE id = ? AND deleted_at IS NULL`, retryOf, id)
	if err != nil {
		return fmt.Errorf("set execution retry_of: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return persistence.ErrNotFound
	}
	return nil
}

func (s *ExecutionStore) GetByID(ctx context.Context, id string) (*persistence.ExecutionRow, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT e.id, e.workflow_id, e.status, e.mode, e.started_at, e.stopped_at, e.wait_till, e.retry_of, e.retry_success_id, d.workflow_data, d.data, e.created_at
		FROM execution_entity e
		JOIN execution_data d ON d.execution_id = e.id
		WHERE e.id = ? AND e.deleted_at IS NULL`, id)
	return scanExecution(row)
}

func (s *ExecutionStore) ListDueWaiting(ctx context.Context, now time.Time, limit int) ([]persistence.ExecutionRow, error) {
	if limit <= 0 || limit > 250 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.workflow_id, e.status, e.mode, e.started_at, e.stopped_at, e.wait_till, e.retry_of, e.retry_success_id, d.workflow_data, d.data, e.created_at
		FROM execution_entity e
		JOIN execution_data d ON d.execution_id = e.id
		WHERE e.deleted_at IS NULL AND e.status = 'waiting' AND e.wait_till IS NOT NULL AND e.wait_till <= ?
		ORDER BY e.wait_till ASC LIMIT ?`, now.UTC().Format(time.RFC3339Nano), limit)
	if err != nil {
		return nil, fmt.Errorf("list due waiting executions: %w", err)
	}
	defer rows.Close()
	result := make([]persistence.ExecutionRow, 0, limit)
	for rows.Next() {
		execution, err := scanExecutionRows(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *execution)
	}
	return result, rows.Err()
}

func (s *ExecutionStore) List(ctx context.Context, workflowID string, limit int) ([]persistence.ExecutionRow, error) {
	if limit <= 0 || limit > 250 {
		limit = 50
	}
	query := `
		SELECT e.id, e.workflow_id, e.status, e.mode, e.started_at, e.stopped_at, e.wait_till, e.retry_of, e.retry_success_id, d.workflow_data, d.data, e.created_at
		FROM execution_entity e
		JOIN execution_data d ON d.execution_id = e.id`
	args := []any{}
	if workflowID != "" {
		query += ` WHERE e.deleted_at IS NULL AND e.workflow_id = ?`
		args = append(args, workflowID)
	} else {
		query += ` WHERE e.deleted_at IS NULL`
	}
	query += ` ORDER BY e.created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list executions: %w", err)
	}
	defer rows.Close()
	result := make([]persistence.ExecutionRow, 0, limit)
	for rows.Next() {
		execution, err := scanExecutionRows(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *execution)
	}
	return result, rows.Err()
}

func (s *ExecutionStore) ListPage(ctx context.Context, workflowID string, limit int, before time.Time, beforeID string) (persistence.ExecutionPage, error) {
	if limit <= 0 || limit > 250 {
		limit = 50
	}
	queryLimit := limit + 1
	query := `
		SELECT e.id, e.workflow_id, e.status, e.mode, e.started_at, e.stopped_at, e.wait_till, e.retry_of, e.retry_success_id, d.workflow_data, d.data, e.created_at
		FROM execution_entity e
		JOIN execution_data d ON d.execution_id = e.id
		WHERE e.deleted_at IS NULL`
	args := []any{}
	if workflowID != "" {
		query += ` AND e.workflow_id = ?`
		args = append(args, workflowID)
	}
	if !before.IsZero() && beforeID != "" {
		cursor := before.UTC().Format(time.RFC3339Nano)
		query += ` AND (e.started_at < ? OR (e.started_at = ? AND e.id < ?))`
		args = append(args, cursor, cursor, beforeID)
	}
	query += ` ORDER BY e.started_at DESC, e.id DESC LIMIT ?`
	args = append(args, queryLimit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return persistence.ExecutionPage{}, fmt.Errorf("list execution page: %w", err)
	}
	defer rows.Close()
	result := make([]persistence.ExecutionRow, 0, limit)
	for rows.Next() {
		execution, err := scanExecutionRows(rows)
		if err != nil {
			return persistence.ExecutionPage{}, err
		}
		result = append(result, *execution)
	}
	if err := rows.Err(); err != nil {
		return persistence.ExecutionPage{}, err
	}
	page := persistence.ExecutionPage{}
	if len(result) > limit {
		next := result[limit-1]
		page.NextCursor = next.StartedAt.Format(time.RFC3339Nano) + "|" + next.ID
		result = result[:limit]
	}
	page.Rows = result
	return page, nil
}

func (s *ExecutionStore) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int, error) {
	rows, err := s.executionIDs(ctx, `WHERE finished = 1 AND deleted_at IS NULL AND started_at < ?`, cutoff.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return s.deleteMany(ctx, rows)
}

func (s *ExecutionStore) PrunePerWorkflow(ctx context.Context, maxCount int) (int, error) {
	if maxCount <= 0 {
		return 0, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		WITH ranked AS (
			SELECT id, ROW_NUMBER() OVER (PARTITION BY workflow_id ORDER BY started_at DESC) AS rn
			FROM execution_entity
			WHERE finished = 1 AND deleted_at IS NULL
		)
		SELECT id FROM ranked WHERE rn > ?`, maxCount)
	if err != nil {
		return 0, fmt.Errorf("prune executions: %w", err)
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return s.deleteMany(ctx, ids)
}

func (s *ExecutionStore) executionIDs(ctx context.Context, where string, args ...any) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM execution_entity `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("list execution ids: %w", err)
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *ExecutionStore) deleteMany(ctx context.Context, ids []string) (int, error) {
	deleted := 0
	for _, id := range ids {
		if err := s.Delete(ctx, id); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

func (s *ExecutionStore) Delete(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM execution_data WHERE execution_id = ?`, id); err != nil {
		return fmt.Errorf("delete execution data: %w", err)
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM execution_entity WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete execution: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return persistence.ErrNotFound
	}
	return nil
}

func scanExecution(row scanner) (*persistence.ExecutionRow, error) {
	execution, err := scanExecutionValues(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, persistence.ErrNotFound
		}
		return nil, err
	}
	return execution, nil
}

func scanExecutionRows(rows *sql.Rows) (*persistence.ExecutionRow, error) {
	return scanExecutionValues(rows)
}

func scanExecutionValues(row scanner) (*persistence.ExecutionRow, error) {
	var execution persistence.ExecutionRow
	var stoppedAt sql.NullString
	var waitTill sql.NullString
	var retryOf sql.NullString
	var retrySuccessID sql.NullString
	var startedAt string
	var createdAt string
	var workflowData string
	var data string
	err := row.Scan(&execution.ID, &execution.WorkflowID, &execution.Status, &execution.Mode, &startedAt, &stoppedAt, &waitTill, &retryOf, &retrySuccessID, &workflowData, &data, &createdAt)
	if err != nil {
		return nil, err
	}
	execution.WorkflowData = json.RawMessage(workflowData)
	execution.Data = json.RawMessage(data)
	parsedStartedAt, err := time.Parse(time.RFC3339Nano, startedAt)
	if err != nil {
		return nil, fmt.Errorf("parse execution started_at: %w", err)
	}
	execution.StartedAt = parsedStartedAt
	if stoppedAt.Valid {
		parsedStoppedAt, err := time.Parse(time.RFC3339Nano, stoppedAt.String)
		if err != nil {
			return nil, fmt.Errorf("parse execution stopped_at: %w", err)
		}
		execution.StoppedAt = &parsedStoppedAt
	}
	if waitTill.Valid {
		parsedWaitTill, err := time.Parse(time.RFC3339Nano, waitTill.String)
		if err != nil {
			return nil, fmt.Errorf("parse execution wait_till: %w", err)
		}
		execution.WaitTill = &parsedWaitTill
	}
	if retryOf.Valid && retryOf.String != "" {
		execution.RetryOf = &retryOf.String
	}
	if retrySuccessID.Valid && retrySuccessID.String != "" {
		execution.RetrySuccessID = &retrySuccessID.String
	}
	execution.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse execution created_at: %w", err)
	}
	return &execution, nil
}

func flattedExecutionData(data dataplane.RunExecutionData) (string, error) {
	if data.ResultData.RunData == nil {
		data.ResultData.RunData = dataplane.RunData{}
	}
	payload, err := flatted.SimpleStringify(data)
	if err != nil {
		return "", fmt.Errorf("encode execution data: %w", err)
	}
	return payload, nil
}
