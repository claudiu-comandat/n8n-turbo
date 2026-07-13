package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/persistence"
)

type CredentialStore struct {
	db *sql.DB
}

func NewCredentialStore(db *sql.DB) *CredentialStore {
	return &CredentialStore{db: db}
}

func (s *CredentialStore) Init(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS credentials_entity (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			data TEXT NOT NULL DEFAULT '{}',
			owner_id TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_credential_type ON credentials_entity(type);
		CREATE INDEX IF NOT EXISTS idx_credential_owner ON credentials_entity(owner_id);
		CREATE TABLE IF NOT EXISTS credential_sharing (
			credential_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'owner',
			PRIMARY KEY (credential_id, user_id)
		);
		CREATE INDEX IF NOT EXISTS idx_credential_sharing_user ON credential_sharing(user_id);`)
	if err != nil {
		return fmt.Errorf("init credentials table: %w", err)
	}
	return nil
}

func (s *CredentialStore) List(ctx context.Context, ownerID string, limit int) ([]persistence.CredentialRow, error) {
	if limit <= 0 || limit > 250 {
		limit = 100
	}
	query := `
		SELECT id, name, type, data, owner_id, created_at, updated_at
		FROM credentials_entity`
	args := []any{}
	if ownerID != "" {
		query += ` WHERE owner_id = ? OR EXISTS (
			SELECT 1 FROM credential_sharing
			WHERE credential_sharing.credential_id = credentials_entity.id
			AND credential_sharing.user_id = ?
		)`
		args = append(args, ownerID, ownerID)
	}
	query += ` ORDER BY updated_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list credentials: %w", err)
	}
	defer rows.Close()
	result := make([]persistence.CredentialRow, 0, limit)
	for rows.Next() {
		row, err := scanCredentialRows(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *row)
	}
	return result, rows.Err()
}

func (s *CredentialStore) GetByID(ctx context.Context, id string) (*persistence.CredentialRow, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, type, data, owner_id, created_at, updated_at
		FROM credentials_entity WHERE id = ?`, id)
	return scanCredential(row)
}

func (s *CredentialStore) Save(ctx context.Context, credential persistence.CredentialRow) (*persistence.CredentialRow, error) {
	now := time.Now().UTC()
	if credential.ID == "" {
		credential.ID = newID()
	}
	if len(credential.Data) == 0 {
		credential.Data = json.RawMessage("{}")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("save credential: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO credentials_entity (id, name, type, data, owner_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			type = excluded.type,
			data = excluded.data,
			owner_id = excluded.owner_id,
			updated_at = excluded.updated_at`,
		credential.ID,
		credential.Name,
		credential.Type,
		string(credential.Data),
		credential.OwnerID,
		now.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
	); err != nil {
		return nil, fmt.Errorf("save credential: %w", err)
	}
	if credential.OwnerID != "" {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO credential_sharing (credential_id, user_id, role)
			VALUES (?, ?, 'owner')
			ON CONFLICT(credential_id, user_id) DO UPDATE SET role = excluded.role`,
			credential.ID,
			credential.OwnerID,
		); err != nil {
			return nil, fmt.Errorf("save credential sharing: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("save credential: %w", err)
	}
	return s.GetByID(ctx, credential.ID)
}

func (s *CredentialStore) Delete(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("delete credential: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM credential_sharing WHERE credential_id = ?`, id); err != nil {
		return fmt.Errorf("delete credential sharing: %w", err)
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM credentials_entity WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete credential: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return persistence.ErrNotFound
	}
	return tx.Commit()
}

func scanCredential(row scanner) (*persistence.CredentialRow, error) {
	credential, err := scanCredentialValues(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, persistence.ErrNotFound
		}
		return nil, err
	}
	return credential, nil
}

func scanCredentialRows(rows *sql.Rows) (*persistence.CredentialRow, error) {
	return scanCredentialValues(rows)
}

func scanCredentialValues(row scanner) (*persistence.CredentialRow, error) {
	var credential persistence.CredentialRow
	var data string
	var ownerID sql.NullString
	var createdAt string
	var updatedAt string
	err := row.Scan(&credential.ID, &credential.Name, &credential.Type, &data, &ownerID, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	credential.Data = json.RawMessage(data)
	if ownerID.Valid {
		credential.OwnerID = ownerID.String
	}
	parsedCreatedAt, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse credential created_at: %w", err)
	}
	credential.CreatedAt = parsedCreatedAt
	parsedUpdatedAt, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse credential updated_at: %w", err)
	}
	credential.UpdatedAt = parsedUpdatedAt
	return &credential, nil
}
