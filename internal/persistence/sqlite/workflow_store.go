package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/persistence"
)

type WorkflowStore struct {
	db *sql.DB
}

func NewWorkflowStore(db *sql.DB) *WorkflowStore {
	return &WorkflowStore{db: db}
}

func (s *WorkflowStore) Init(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS workflow_entity (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			active INTEGER NOT NULL DEFAULT 0,
			nodes TEXT NOT NULL DEFAULT '[]',
			connections TEXT NOT NULL DEFAULT '{}',
			settings TEXT NOT NULL DEFAULT '{}',
			static_data TEXT,
			pin_data TEXT,
			version_id TEXT NOT NULL,
			meta TEXT,
			owner_id TEXT,
			parent_folder_id TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_workflow_active ON workflow_entity(active);
		CREATE INDEX IF NOT EXISTS idx_workflow_updated ON workflow_entity(updated_at);`)
	if err != nil {
		return fmt.Errorf("init workflow table: %w", err)
	}
	// Best-effort column add for databases created before folders existed.
	// Fresh databases already have the column, so this errors and is ignored.
	_, _ = s.db.ExecContext(ctx, `ALTER TABLE workflow_entity ADD COLUMN parent_folder_id TEXT`)
	_, _ = s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_workflow_parent_folder ON workflow_entity(parent_folder_id)`)
	return nil
}

// SetWorkflowFolder moves a workflow into a folder (nil parentFolderID -> project root).
func (s *WorkflowStore) SetWorkflowFolder(ctx context.Context, workflowID string, parentFolderID *string) error {
	var value any
	if parentFolderID != nil && *parentFolderID != "" {
		value = *parentFolderID
	}
	result, err := s.db.ExecContext(ctx, `UPDATE workflow_entity SET parent_folder_id = ?, updated_at = ? WHERE id = ?`,
		value, time.Now().UTC().Format(time.RFC3339Nano), workflowID)
	if err != nil {
		return fmt.Errorf("set workflow folder: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return persistence.ErrNotFound
	}
	return nil
}

// ParentFoldersFor batch-loads the folder id each workflow sits in (only workflows
// that are in a folder appear in the map).
func (s *WorkflowStore) ParentFoldersFor(ctx context.Context, ids []string) (map[string]string, error) {
	result := map[string]string{}
	if len(ids) == 0 {
		return result, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, parent_folder_id FROM workflow_entity WHERE id IN (`+strings.Join(placeholders, ",")+`) AND parent_folder_id IS NOT NULL AND parent_folder_id != ''`, args...)
	if err != nil {
		return nil, fmt.Errorf("parent folders for workflows: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, parent string
		if err := rows.Scan(&id, &parent); err != nil {
			return nil, err
		}
		result[id] = parent
	}
	return result, rows.Err()
}

// WorkflowIDsInFolder returns the workflows directly inside a folder.
func (s *WorkflowStore) WorkflowIDsInFolder(ctx context.Context, parentFolderID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM workflow_entity WHERE parent_folder_id = ?`, parentFolderID)
	if err != nil {
		return nil, fmt.Errorf("workflows in folder: %w", err)
	}
	defer rows.Close()
	result := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, rows.Err()
}

func (s *WorkflowStore) List(ctx context.Context, limit int) ([]persistence.WorkflowRow, error) {
	if limit <= 0 || limit > 250 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, active, nodes, connections, settings, static_data, pin_data, version_id, meta, owner_id, created_at, updated_at
		FROM workflow_entity ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list workflows: %w", err)
	}
	defer rows.Close()
	result := make([]persistence.WorkflowRow, 0, limit)
	for rows.Next() {
		workflow, err := scanWorkflowRows(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *workflow)
	}
	return result, rows.Err()
}

func (s *WorkflowStore) ListPage(ctx context.Context, limit int, before time.Time, beforeID string) (persistence.WorkflowPage, error) {
	if limit <= 0 || limit > 250 {
		limit = 50
	}
	queryLimit := limit + 1
	var rows *sql.Rows
	var err error
	if before.IsZero() || beforeID == "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, name, active, nodes, connections, settings, static_data, pin_data, version_id, meta, owner_id, created_at, updated_at
			FROM workflow_entity ORDER BY updated_at DESC, id DESC LIMIT ?`, queryLimit)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, name, active, nodes, connections, settings, static_data, pin_data, version_id, meta, owner_id, created_at, updated_at
			FROM workflow_entity
			WHERE updated_at < ? OR (updated_at = ? AND id < ?)
			ORDER BY updated_at DESC, id DESC LIMIT ?`, before.Format(time.RFC3339Nano), before.Format(time.RFC3339Nano), beforeID, queryLimit)
	}
	if err != nil {
		return persistence.WorkflowPage{}, fmt.Errorf("list workflow page: %w", err)
	}
	defer rows.Close()
	result := make([]persistence.WorkflowRow, 0, limit)
	for rows.Next() {
		workflow, err := scanWorkflowRows(rows)
		if err != nil {
			return persistence.WorkflowPage{}, err
		}
		result = append(result, *workflow)
	}
	if err := rows.Err(); err != nil {
		return persistence.WorkflowPage{}, err
	}
	page := persistence.WorkflowPage{}
	if len(result) > limit {
		next := result[limit-1]
		page.NextCursor = next.UpdatedAt.Format(time.RFC3339Nano) + "|" + next.ID
		result = result[:limit]
	}
	page.Rows = result
	return page, nil
}

func (s *WorkflowStore) GetByID(ctx context.Context, id string) (*persistence.WorkflowRow, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, active, nodes, connections, settings, static_data, pin_data, version_id, meta, owner_id, created_at, updated_at
		FROM workflow_entity WHERE id = ?`, id)
	return scanWorkflow(row)
}

func (s *WorkflowStore) Save(ctx context.Context, workflow dataplane.Workflow, ownerID string) (*persistence.WorkflowRow, error) {
	now := time.Now().UTC()
	if workflow.ID == "" {
		workflow.ID = newID()
	}
	baseVersion := workflow.VersionID
	workflow.VersionID = newID()
	nodes := mustJSON(workflow.Nodes, "[]")
	connections := mustJSON(workflow.Connections, "{}")
	settings := mustJSON(workflow.Settings, "{}")
	staticData := mustJSON(workflow.StaticData, "{}")
	pinData := mustJSON(workflow.PinData, "{}")
	meta := mustJSON(workflow.Meta, "{}")

	// Optimistic concurrency: overwrite only if the stored version still matches.
	if baseVersion != "" {
		result, err := s.db.ExecContext(ctx, `
			UPDATE workflow_entity SET
				name = ?, active = ?, nodes = ?, connections = ?, settings = ?,
				static_data = ?, pin_data = ?, version_id = ?, meta = ?, owner_id = ?, updated_at = ?
			WHERE id = ? AND version_id = ?`,
			workflow.Name, boolToInt(workflow.Active), nodes, connections, settings,
			staticData, pinData, workflow.VersionID, meta, ownerID,
			now.Format(time.RFC3339Nano), workflow.ID, baseVersion)
		if err != nil {
			return nil, fmt.Errorf("save workflow: %w", err)
		}
		if affected, _ := result.RowsAffected(); affected > 0 {
			return s.GetByID(ctx, workflow.ID)
		}
		if _, err := s.GetByID(ctx, workflow.ID); err == nil {
			return nil, persistence.ErrVersionConflict
		} else if err != persistence.ErrNotFound {
			return nil, err
		}
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO workflow_entity (id, name, active, nodes, connections, settings, static_data, pin_data, version_id, meta, owner_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			active = excluded.active,
			nodes = excluded.nodes,
			connections = excluded.connections,
			settings = excluded.settings,
			static_data = excluded.static_data,
			pin_data = excluded.pin_data,
			version_id = excluded.version_id,
			meta = excluded.meta,
			owner_id = excluded.owner_id,
			updated_at = excluded.updated_at`,
		workflow.ID,
		workflow.Name,
		boolToInt(workflow.Active),
		nodes,
		connections,
		settings,
		staticData,
		pinData,
		workflow.VersionID,
		meta,
		ownerID,
		now.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, fmt.Errorf("save workflow: %w", err)
	}
	return s.GetByID(ctx, workflow.ID)
}

func (s *WorkflowStore) SetActive(ctx context.Context, id string, active bool) error {
	result, err := s.db.ExecContext(ctx, `UPDATE workflow_entity SET active = ?, updated_at = ? WHERE id = ?`, boolToInt(active), time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("set workflow active: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return persistence.ErrNotFound
	}
	return nil
}

func (s *WorkflowStore) Delete(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM workflow_entity WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete workflow: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return persistence.ErrNotFound
	}
	return nil
}

func scanWorkflow(row scanner) (*persistence.WorkflowRow, error) {
	workflow, err := scanWorkflowValues(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, persistence.ErrNotFound
		}
		return nil, err
	}
	return workflow, nil
}

func scanWorkflowRows(rows *sql.Rows) (*persistence.WorkflowRow, error) {
	return scanWorkflowValues(rows)
}

func scanWorkflowValues(row scanner) (*persistence.WorkflowRow, error) {
	var workflow persistence.WorkflowRow
	var active int
	var staticData sql.NullString
	var pinData sql.NullString
	var meta sql.NullString
	var ownerID sql.NullString
	var createdAt string
	var updatedAt string
	var nodes string
	var connections string
	var settings string
	err := row.Scan(&workflow.ID, &workflow.Name, &active, &nodes, &connections, &settings, &staticData, &pinData, &workflow.VersionID, &meta, &ownerID, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	workflow.Active = active == 1
	workflow.Nodes = json.RawMessage(nodes)
	workflow.Connections = json.RawMessage(connections)
	workflow.Settings = json.RawMessage(settings)
	workflow.StaticData = nullableJSON(staticData, "{}")
	workflow.PinData = nullableJSON(pinData, "{}")
	workflow.Meta = nullableJSON(meta, "{}")
	workflow.Checksum = workflow.VersionID
	if ownerID.Valid {
		workflow.OwnerID = ownerID.String
	}
	workflow.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse workflow created_at: %w", err)
	}
	workflow.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse workflow updated_at: %w", err)
	}
	return &workflow, nil
}

func mustJSON(value any, fallback string) string {
	bytes, err := json.Marshal(value)
	if err != nil || string(bytes) == "null" {
		return fallback
	}
	return string(bytes)
}

func nullableJSON(value sql.NullString, fallback string) json.RawMessage {
	if value.Valid && value.String != "" {
		return json.RawMessage(value.String)
	}
	return json.RawMessage(fallback)
}
