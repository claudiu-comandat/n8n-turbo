package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/credentials"
	"github.com/n8n-io/n8n-turbo/internal/persistence"
)

type VariableStore struct {
	db     *sql.DB
	vault  *credentials.Vault
	mu     sync.RWMutex
	cache  map[string]persistence.VariableRow
	byKey  map[string]string
	loaded bool
}

func NewVariableStore(db *sql.DB) *VariableStore {
	return NewVariableStoreWithVault(db, nil)
}

func NewVariableStoreWithVault(db *sql.DB, vault *credentials.Vault) *VariableStore {
	return &VariableStore{
		db:    db,
		vault: vault,
		cache: map[string]persistence.VariableRow{},
		byKey: map[string]string{},
	}
}

func (s *VariableStore) Init(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS variables (
			id TEXT PRIMARY KEY,
			key TEXT NOT NULL UNIQUE,
			type TEXT NOT NULL DEFAULT 'string',
			value TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_variables_key ON variables(key);`)
	if err != nil {
		return fmt.Errorf("init variables table: %w", err)
	}
	return nil
}

func (s *VariableStore) List(ctx context.Context) ([]persistence.VariableRow, error) {
	if err := s.ensureLoaded(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]persistence.VariableRow, 0, len(s.cache))
	for _, variable := range s.cache {
		result = append(result, variable)
	}
	sort.Slice(result, func(i int, j int) bool {
		return result[i].Key < result[j].Key
	})
	return result, nil
}

func (s *VariableStore) GetByID(ctx context.Context, id string) (*persistence.VariableRow, error) {
	if err := s.ensureLoaded(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	variable, ok := s.cache[id]
	if !ok {
		return nil, persistence.ErrNotFound
	}
	return cloneVariable(variable), nil
}

func (s *VariableStore) GetByKey(ctx context.Context, key string) (*persistence.VariableRow, error) {
	if err := s.ensureLoaded(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.byKey[key]
	if !ok {
		return nil, persistence.ErrNotFound
	}
	variable := s.cache[id]
	return cloneVariable(variable), nil
}

func (s *VariableStore) Save(ctx context.Context, variable persistence.VariableRow) (*persistence.VariableRow, error) {
	if err := s.ensureLoaded(ctx); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, exists := s.cache[variable.ID]
	if exists {
		if variable.Key == "" {
			variable.Key = existing.Key
		}
		if variable.Type == "" {
			variable.Type = existing.Type
		}
		if variable.Value == "" && existing.Type == "secret" && strings.EqualFold(variable.Type, "secret") {
			variable.Value = existing.Value
		}
		variable.CreatedAt = existing.CreatedAt
	} else {
		if variable.ID == "" {
			variable.ID = newID()
		}
		variable.CreatedAt = now
	}
	if variable.Type == "" {
		variable.Type = "string"
	}
	variable.Type = strings.ToLower(variable.Type)
	if err := validateVariable(variable, exists); err != nil {
		return nil, err
	}
	if ownerID, ok := s.byKey[variable.Key]; ok && ownerID != variable.ID {
		return nil, fmt.Errorf("variable key already exists")
	}
	storedValue, err := s.storedValue(variable.Type, variable.Value)
	if err != nil {
		return nil, err
	}
	variable.Value = storedValue
	variable.UpdatedAt = now
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO variables (id, key, type, value, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			key = excluded.key,
			type = excluded.type,
			value = excluded.value,
			updated_at = excluded.updated_at`,
		variable.ID,
		variable.Key,
		variable.Type,
		variable.Value,
		variable.CreatedAt.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, fmt.Errorf("save variable: %w", err)
	}
	if exists && existing.Key != variable.Key {
		delete(s.byKey, existing.Key)
	}
	s.cache[variable.ID] = variable
	s.byKey[variable.Key] = variable.ID
	return cloneVariable(variable), nil
}

func (s *VariableStore) Delete(ctx context.Context, id string) error {
	if err := s.ensureLoaded(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, exists := s.cache[id]
	if !exists {
		return persistence.ErrNotFound
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM variables WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete variable: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return persistence.ErrNotFound
	}
	delete(s.cache, id)
	delete(s.byKey, existing.Key)
	return nil
}

func (s *VariableStore) Resolve(ctx context.Context) (map[string]any, error) {
	if err := s.ensureLoaded(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]any, len(s.cache))
	for _, variable := range s.cache {
		value, err := s.resolvedValue(variable)
		if err != nil {
			return nil, err
		}
		result[variable.Key] = value
	}
	return result, nil
}

func (s *VariableStore) MaxVariables() int {
	return -1
}

func (s *VariableStore) InvalidateCache() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache = map[string]persistence.VariableRow{}
	s.byKey = map[string]string{}
	s.loaded = false
}

func (s *VariableStore) ensureLoaded(ctx context.Context) error {
	s.mu.RLock()
	if s.loaded {
		s.mu.RUnlock()
		return nil
	}
	s.mu.RUnlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, key, type, value, created_at, updated_at FROM variables ORDER BY key`)
	if err != nil {
		return fmt.Errorf("list variables: %w", err)
	}
	defer rows.Close()
	cache := map[string]persistence.VariableRow{}
	byKey := map[string]string{}
	for rows.Next() {
		variable, err := scanVariableRows(rows)
		if err != nil {
			return err
		}
		cache[variable.ID] = *variable
		byKey[variable.Key] = variable.ID
	}
	if err := rows.Err(); err != nil {
		return err
	}
	s.cache = cache
	s.byKey = byKey
	s.loaded = true
	return nil
}

func scanVariable(row scanner) (*persistence.VariableRow, error) {
	variable, err := scanVariableValues(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, persistence.ErrNotFound
		}
		return nil, err
	}
	return variable, nil
}

func scanVariableRows(rows *sql.Rows) (*persistence.VariableRow, error) {
	return scanVariableValues(rows)
}

func scanVariableValues(row scanner) (*persistence.VariableRow, error) {
	var variable persistence.VariableRow
	var value sql.NullString
	var createdAt string
	var updatedAt string
	err := row.Scan(&variable.ID, &variable.Key, &variable.Type, &value, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	if value.Valid {
		variable.Value = value.String
	}
	parsedCreatedAt, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse variable created_at: %w", err)
	}
	variable.CreatedAt = parsedCreatedAt
	parsedUpdatedAt, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse variable updated_at: %w", err)
	}
	variable.UpdatedAt = parsedUpdatedAt
	return &variable, nil
}

func resolveVariableValue(variable persistence.VariableRow) any {
	switch strings.ToLower(variable.Type) {
	case "number":
		value, err := strconv.ParseFloat(variable.Value, 64)
		if err == nil {
			return value
		}
	case "boolean":
		return strings.EqualFold(variable.Value, "true") || variable.Value == "1"
	}
	return variable.Value
}

func (s *VariableStore) resolvedValue(variable persistence.VariableRow) (any, error) {
	if variable.Type == "secret" && s.vault != nil && variable.Value != "" {
		value, err := s.vault.Decrypt(variable.Value)
		if err != nil {
			return nil, err
		}
		variable.Value = value
	}
	return resolveVariableValue(variable), nil
}

func (s *VariableStore) storedValue(variableType string, value string) (string, error) {
	if variableType != "secret" || s.vault == nil || value == "" {
		return value, nil
	}
	if credentials.IsEncryptedValue(value) {
		if _, err := s.vault.Decrypt(value); err == nil {
			return value, nil
		}
	}
	encrypted, err := s.vault.Encrypt(value)
	if err != nil {
		return "", fmt.Errorf("encrypt variable: %w", err)
	}
	return encrypted, nil
}

func cloneVariable(variable persistence.VariableRow) *persistence.VariableRow {
	copy := variable
	return &copy
}

func validateVariable(variable persistence.VariableRow, update bool) error {
	if strings.TrimSpace(variable.Key) == "" {
		return fmt.Errorf("variable key is required")
	}
	if len(variable.Key) > 100 {
		return fmt.Errorf("variable key is too long")
	}
	for _, char := range variable.Key {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '_' || char == '-') {
			return fmt.Errorf("variable key contains invalid characters")
		}
	}
	switch variable.Type {
	case "string", "number", "boolean", "secret":
	default:
		return fmt.Errorf("invalid variable type")
	}
	if !update && variable.Value == "" && variable.Type == "secret" {
		return fmt.Errorf("secret variable value is required")
	}
	return nil
}
