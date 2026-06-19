package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/persistence"
)

type UserStore struct {
	db *sql.DB
}

func NewUserStore(db *sql.DB) *UserStore {
	return &UserStore{db: db}
}

func (s *UserStore) Init(ctx context.Context) error {
	query := `
	CREATE TABLE IF NOT EXISTS user (
		id TEXT PRIMARY KEY,
		email TEXT NOT NULL UNIQUE,
		first_name TEXT NOT NULL,
		last_name TEXT NOT NULL,
		password TEXT,
		role TEXT NOT NULL,
		disabled INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);`
	_, err := s.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("init user table: %w", err)
	}
	return nil
}

func (s *UserStore) GetByEmail(ctx context.Context, email string) (*persistence.UserRow, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, email, first_name, last_name, password, role, disabled, created_at, updated_at
		FROM user WHERE lower(email) = lower(?)`, strings.TrimSpace(email))
	return scanUser(row)
}

func (s *UserStore) GetByID(ctx context.Context, id string) (*persistence.UserRow, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, email, first_name, last_name, password, role, disabled, created_at, updated_at
		FROM user WHERE id = ?`, id)
	return scanUser(row)
}

func (s *UserStore) List(ctx context.Context) ([]persistence.UserRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, email, first_name, last_name, password, role, disabled, created_at, updated_at
		FROM user ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	result := make([]persistence.UserRow, 0, 8)
	for rows.Next() {
		user, err := scanUserRows(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *user)
	}
	return result, rows.Err()
}

func (s *UserStore) Insert(ctx context.Context, user persistence.UserRow) error {
	if user.ID == "" {
		user.ID = newID()
	}
	now := time.Now().UTC()
	if user.CreatedAt.IsZero() {
		user.CreatedAt = now
	}
	if user.UpdatedAt.IsZero() {
		user.UpdatedAt = now
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user (id, email, first_name, last_name, password, role, disabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		user.ID,
		user.Email,
		user.FirstName,
		user.LastName,
		user.Password,
		user.Role,
		boolToInt(user.Disabled),
		user.CreatedAt.Format(time.RFC3339Nano),
		user.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}

func (s *UserStore) HasAny(ctx context.Context) (bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM user`)
	var count int
	if err := row.Scan(&count); err != nil {
		return false, fmt.Errorf("count users: %w", err)
	}
	return count > 0, nil
}

func (s *UserStore) UpdatePassword(ctx context.Context, userID, passwordHash string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE user SET password = ?, updated_at = ? WHERE id = ?`,
		passwordHash,
		time.Now().UTC().Format(time.RFC3339Nano),
		userID,
	)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanUser(row scanner) (*persistence.UserRow, error) {
	user, err := scanUserValues(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, persistence.ErrNotFound
		}
		return nil, err
	}
	return user, nil
}

func scanUserRows(rows *sql.Rows) (*persistence.UserRow, error) {
	return scanUserValues(rows)
}

func scanUserValues(row scanner) (*persistence.UserRow, error) {
	var user persistence.UserRow
	var password sql.NullString
	var disabled int
	var createdAt string
	var updatedAt string
	err := row.Scan(
		&user.ID,
		&user.Email,
		&user.FirstName,
		&user.LastName,
		&password,
		&user.Role,
		&disabled,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return nil, err
	}
	user.Disabled = disabled == 1
	if password.Valid {
		user.Password = &password.String
	}
	user.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	user.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse updated_at: %w", err)
	}
	return &user, nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func newID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("user-%d", time.Now().UTC().UnixNano())
	}
	return hex.EncodeToString(buffer)
}
