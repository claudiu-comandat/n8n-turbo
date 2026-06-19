package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/n8n-io/n8n-turbo/internal/persistence"
)

type SettingsStore struct {
	db *sql.DB
}

func NewSettingsStore(db *sql.DB) *SettingsStore {
	return &SettingsStore{db: db}
}

func (s *SettingsStore) Init(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);`)
	if err != nil {
		return fmt.Errorf("init settings table: %w", err)
	}
	return nil
}

func (s *SettingsStore) Get(ctx context.Context, key string) (string, error) {
	row := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key)
	var value string
	if err := row.Scan(&value); err != nil {
		if err == sql.ErrNoRows {
			return "", persistence.ErrNotFound
		}
		return "", fmt.Errorf("get setting: %w", err)
	}
	return value, nil
}

func (s *SettingsStore) Set(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key,
		value,
	)
	if err != nil {
		return fmt.Errorf("set setting: %w", err)
	}
	return nil
}
