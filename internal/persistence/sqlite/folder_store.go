package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/persistence"
)

type FolderStore struct {
	db *sql.DB
}

func NewFolderStore(db *sql.DB) *FolderStore {
	return &FolderStore{db: db}
}

func (s *FolderStore) Init(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS folder (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			parent_folder_id TEXT,
			project_id TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_folder_project ON folder(project_id);
		CREATE INDEX IF NOT EXISTS idx_folder_parent ON folder(parent_folder_id);`)
	if err != nil {
		return fmt.Errorf("init folder table: %w", err)
	}
	return nil
}

func (s *FolderStore) Create(ctx context.Context, id, name, projectID string, parentFolderID *string) (*persistence.FolderRow, error) {
	if id == "" {
		id = newID()
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO folder (id, name, parent_folder_id, project_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		id, name, nullableString(parentFolderID), projectID, now, now)
	if err != nil {
		return nil, fmt.Errorf("create folder: %w", err)
	}
	return s.GetByID(ctx, id)
}

func (s *FolderStore) Rename(ctx context.Context, id, name string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE folder SET name = ?, updated_at = ? WHERE id = ?`,
		name, time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("rename folder: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return persistence.ErrNotFound
	}
	return nil
}

func (s *FolderStore) Move(ctx context.Context, id string, parentFolderID *string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE folder SET parent_folder_id = ?, updated_at = ? WHERE id = ?`,
		nullableString(parentFolderID), time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("move folder: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return persistence.ErrNotFound
	}
	return nil
}

func (s *FolderStore) Delete(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM folder WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete folder: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return persistence.ErrNotFound
	}
	return nil
}

func (s *FolderStore) GetByID(ctx context.Context, id string) (*persistence.FolderRow, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, parent_folder_id, project_id, created_at, updated_at FROM folder WHERE id = ?`, id)
	return scanFolder(row)
}

func (s *FolderStore) ListByProject(ctx context.Context, projectID string) ([]persistence.FolderRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, parent_folder_id, project_id, created_at, updated_at FROM folder WHERE project_id = ? ORDER BY name`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list folders: %w", err)
	}
	defer rows.Close()
	result := make([]persistence.FolderRow, 0)
	for rows.Next() {
		folder, err := scanFolderValues(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *folder)
	}
	return result, rows.Err()
}

func scanFolder(row scanner) (*persistence.FolderRow, error) {
	folder, err := scanFolderValues(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, persistence.ErrNotFound
		}
		return nil, err
	}
	return folder, nil
}

func scanFolderValues(row scanner) (*persistence.FolderRow, error) {
	var folder persistence.FolderRow
	var parentFolderID sql.NullString
	var createdAt, updatedAt string
	if err := row.Scan(&folder.ID, &folder.Name, &parentFolderID, &folder.ProjectID, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	if parentFolderID.Valid && parentFolderID.String != "" {
		value := parentFolderID.String
		folder.ParentFolderID = &value
	}
	var err error
	if folder.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return nil, fmt.Errorf("parse folder created_at: %w", err)
	}
	if folder.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt); err != nil {
		return nil, fmt.Errorf("parse folder updated_at: %w", err)
	}
	return &folder, nil
}

func nullableString(value *string) any {
	if value == nil || *value == "" {
		return nil
	}
	return *value
}
