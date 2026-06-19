package sqlite

import (
	"context"
	"database/sql"
	"fmt"
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
	_, err := s.db.ExecContext(ctx, `DELETE FROM tag_entity WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete tag: %w", err)
	}
	return nil
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
