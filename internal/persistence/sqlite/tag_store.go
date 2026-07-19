package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/persistence"
)

type TagStore struct {
	db *sql.DB
}

func NewTagStore(db *sql.DB) *TagStore {
	return &TagStore{db: db}
}

func (s *TagStore) Init(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS tag_entity (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS workflows_tags (
			workflow_id TEXT NOT NULL,
			tag_id TEXT NOT NULL,
			PRIMARY KEY (workflow_id, tag_id)
		);`)
	if err != nil {
		return fmt.Errorf("init tag tables: %w", err)
	}
	return nil
}

func (s *TagStore) List(ctx context.Context) ([]persistence.TagRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, created_at, updated_at FROM tag_entity ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	defer rows.Close()
	result := make([]persistence.TagRow, 0)
	for rows.Next() {
		tag, err := scanTagRows(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *tag)
	}
	return result, rows.Err()
}

func (s *TagStore) GetByID(ctx context.Context, id string) (*persistence.TagRow, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, created_at, updated_at FROM tag_entity WHERE id = ?`, id)
	return scanTag(row)
}

func (s *TagStore) Save(ctx context.Context, tag persistence.TagRow) (*persistence.TagRow, error) {
	now := time.Now().UTC()
	if tag.ID == "" {
		tag.ID = newID()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tag_entity (id, name, created_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			updated_at = excluded.updated_at`,
		tag.ID,
		tag.Name,
		now.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, fmt.Errorf("save tag: %w", err)
	}
	return s.GetByID(ctx, tag.ID)
}

func (s *TagStore) Delete(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM workflows_tags WHERE tag_id = ?`, id); err != nil {
		return fmt.Errorf("delete tag associations: %w", err)
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM tag_entity WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete tag: %w", err)
	}
	return nil
}

// SetWorkflowTags replaces the full tag set for a workflow. Passing nil clears it.
func (s *TagStore) SetWorkflowTags(ctx context.Context, workflowID string, tagIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("set workflow tags: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM workflows_tags WHERE workflow_id = ?`, workflowID); err != nil {
		return fmt.Errorf("clear workflow tags: %w", err)
	}
	seen := map[string]bool{}
	for _, tagID := range tagIDs {
		tagID = strings.TrimSpace(tagID)
		if tagID == "" || seen[tagID] {
			continue
		}
		seen[tagID] = true
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO workflows_tags (workflow_id, tag_id) VALUES (?, ?)`, workflowID, tagID); err != nil {
			return fmt.Errorf("insert workflow tag: %w", err)
		}
	}
	return tx.Commit()
}

// ListTagsForWorkflow returns the tags associated with a single workflow.
func (s *TagStore) ListTagsForWorkflow(ctx context.Context, workflowID string) ([]persistence.TagRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.name, t.created_at, t.updated_at
		FROM tag_entity t
		JOIN workflows_tags wt ON wt.tag_id = t.id
		WHERE wt.workflow_id = ?
		ORDER BY t.name`, workflowID)
	if err != nil {
		return nil, fmt.Errorf("list workflow tags: %w", err)
	}
	defer rows.Close()
	result := make([]persistence.TagRow, 0)
	for rows.Next() {
		tag, err := scanTagRows(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *tag)
	}
	return result, rows.Err()
}

// TagsForWorkflows batch-loads tags for many workflows (id -> tags), for list views.
func (s *TagStore) TagsForWorkflows(ctx context.Context, workflowIDs []string) (map[string][]persistence.TagRow, error) {
	result := map[string][]persistence.TagRow{}
	if len(workflowIDs) == 0 {
		return result, nil
	}
	placeholders := make([]string, len(workflowIDs))
	args := make([]any, len(workflowIDs))
	for i, id := range workflowIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	query := `
		SELECT wt.workflow_id, t.id, t.name, t.created_at, t.updated_at
		FROM workflows_tags wt
		JOIN tag_entity t ON t.id = wt.tag_id
		WHERE wt.workflow_id IN (` + strings.Join(placeholders, ",") + `)
		ORDER BY t.name`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("tags for workflows: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var workflowID string
		var tag persistence.TagRow
		var createdAt, updatedAt string
		if err := rows.Scan(&workflowID, &tag.ID, &tag.Name, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		tag.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		tag.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
		result[workflowID] = append(result[workflowID], tag)
	}
	return result, rows.Err()
}

// WorkflowIDsByTag returns the workflow ids tagged with a given tag id.
func (s *TagStore) WorkflowIDsByTag(ctx context.Context, tagID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT workflow_id FROM workflows_tags WHERE tag_id = ?`, tagID)
	if err != nil {
		return nil, fmt.Errorf("workflow ids by tag: %w", err)
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

func scanTag(row scanner) (*persistence.TagRow, error) {
	tag, err := scanTagValues(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, persistence.ErrNotFound
		}
		return nil, err
	}
	return tag, nil
}

func scanTagRows(rows *sql.Rows) (*persistence.TagRow, error) {
	return scanTagValues(rows)
}

func scanTagValues(row scanner) (*persistence.TagRow, error) {
	var tag persistence.TagRow
	var createdAt string
	var updatedAt string
	err := row.Scan(&tag.ID, &tag.Name, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	parsedCreatedAt, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse tag created_at: %w", err)
	}
	tag.CreatedAt = parsedCreatedAt
	parsedUpdatedAt, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse tag updated_at: %w", err)
	}
	tag.UpdatedAt = parsedUpdatedAt
	return &tag, nil
}
