package webhook

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrNotFound = errors.New("webhook not found")

type Store interface {
	Init(ctx context.Context) error
	Get(ctx context.Context, webhookID string) (*RegisteredWebhook, error)
	GetByPath(ctx context.Context, path string, method string, isTest bool) (*RegisteredWebhook, error)
	GetByWorkflow(ctx context.Context, workflowID string) ([]RegisteredWebhook, error)
	GetAll(ctx context.Context) ([]RegisteredWebhook, error)
	Save(ctx context.Context, webhook RegisteredWebhook) error
	Delete(ctx context.Context, webhookID string) error
	DeleteByWorkflow(ctx context.Context, workflowID string) error
	Exists(ctx context.Context, path string, method string, isTest bool) (bool, error)
}

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(db *sql.DB) (*SQLiteStore, error) {
	store := &SQLiteStore{db: db}
	if err := store.Init(context.Background()); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) Init(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS webhook_entity (
			webhookPath TEXT NOT NULL,
			method TEXT NOT NULL,
			node TEXT NOT NULL,
			node_name TEXT,
			workflowId TEXT NOT NULL,
			webhookId TEXT,
			pathLength INTEGER,
			is_test INTEGER NOT NULL DEFAULT 0,
			auth_mode TEXT NOT NULL DEFAULT 'none',
			hmac_secret TEXT,
			hmac_header TEXT,
			hmac_algo TEXT DEFAULT 'sha256',
			response_mode TEXT NOT NULL DEFAULT 'onReceived',
			options TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL,
			PRIMARY KEY (webhookPath, method, is_test)
		);
		CREATE INDEX IF NOT EXISTS idx_webhook_entity_workflow ON webhook_entity(workflowId);
		CREATE INDEX IF NOT EXISTS idx_webhook_entity_id ON webhook_entity(webhookId);`)
	if err != nil {
		return fmt.Errorf("init webhook store: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Get(ctx context.Context, webhookID string) (*RegisteredWebhook, error) {
	row := s.db.QueryRowContext(ctx, webhookSelectSQL()+` WHERE webhookId = ?`, webhookID)
	return scanWebhook(row)
}

func (s *SQLiteStore) GetByPath(ctx context.Context, path string, method string, isTest bool) (*RegisteredWebhook, error) {
	row := s.db.QueryRowContext(ctx, webhookSelectSQL()+` WHERE webhookPath = ? AND method = ? AND is_test = ?`, cleanPath(path), cleanMethod(method), boolInt(isTest))
	return scanWebhook(row)
}

func (s *SQLiteStore) GetByWorkflow(ctx context.Context, workflowID string) ([]RegisteredWebhook, error) {
	rows, err := s.db.QueryContext(ctx, webhookSelectSQL()+` WHERE workflowId = ? ORDER BY webhookPath, method`, workflowID)
	if err != nil {
		return nil, fmt.Errorf("get workflow webhooks: %w", err)
	}
	defer rows.Close()
	return scanWebhooks(rows)
}

func (s *SQLiteStore) GetAll(ctx context.Context) ([]RegisteredWebhook, error) {
	rows, err := s.db.QueryContext(ctx, webhookSelectSQL()+` ORDER BY workflowId, webhookPath, method`)
	if err != nil {
		return nil, fmt.Errorf("get all webhooks: %w", err)
	}
	defer rows.Close()
	return scanWebhooks(rows)
}

func (s *SQLiteStore) Save(ctx context.Context, webhook RegisteredWebhook) error {
	webhook.Path = cleanPath(webhook.Path)
	webhook.Method = cleanMethod(webhook.Method)
	if webhook.AuthMode == "" {
		webhook.AuthMode = "none"
	}
	if webhook.HMACAlgo == "" {
		webhook.HMACAlgo = "sha256"
	}
	if webhook.ResponseMode == "" {
		webhook.ResponseMode = ResponseModeOnReceived
	}
	if webhook.CreatedAt.IsZero() {
		webhook.CreatedAt = time.Now().UTC()
	}
	options, err := json.Marshal(webhook.Options)
	if err != nil {
		return fmt.Errorf("marshal webhook options: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO webhook_entity
			(webhookPath, method, node, node_name, workflowId, webhookId, pathLength, is_test, auth_mode, hmac_secret, hmac_header, hmac_algo, response_mode, options, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(webhookPath, method, is_test) DO UPDATE SET
			node = excluded.node,
			node_name = excluded.node_name,
			workflowId = excluded.workflowId,
			webhookId = excluded.webhookId,
			pathLength = excluded.pathLength,
			auth_mode = excluded.auth_mode,
			hmac_secret = excluded.hmac_secret,
			hmac_header = excluded.hmac_header,
			hmac_algo = excluded.hmac_algo,
			response_mode = excluded.response_mode,
			options = excluded.options`,
		webhook.Path,
		webhook.Method,
		webhook.NodeID,
		webhook.NodeName,
		webhook.WorkflowID,
		webhook.WebhookID,
		pathLength(webhook.Path),
		boolInt(webhook.IsTest),
		webhook.AuthMode,
		nullEmpty(webhook.HMACSecret),
		nullEmpty(webhook.HMACHeader),
		nullEmpty(webhook.HMACAlgo),
		string(webhook.ResponseMode),
		string(options),
		webhook.CreatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("save webhook: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Delete(ctx context.Context, webhookID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM webhook_entity WHERE webhookId = ?`, webhookID)
	if err != nil {
		return fmt.Errorf("delete webhook: %w", err)
	}
	affected, err := result.RowsAffected()
	if err == nil && affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) DeleteByWorkflow(ctx context.Context, workflowID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM webhook_entity WHERE workflowId = ?`, workflowID)
	if err != nil {
		return fmt.Errorf("delete workflow webhooks: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Exists(ctx context.Context, path string, method string, isTest bool) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM webhook_entity WHERE webhookPath = ? AND method = ? AND is_test = ?`, cleanPath(path), cleanMethod(method), boolInt(isTest)).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func webhookSelectSQL() string {
	return `SELECT webhookId, webhookPath, method, node, node_name, workflowId, is_test, auth_mode, hmac_secret, hmac_header, hmac_algo, response_mode, options, created_at FROM webhook_entity`
}

func scanWebhook(row interface {
	Scan(dest ...any) error
}) (*RegisteredWebhook, error) {
	var webhook RegisteredWebhook
	var webhookID sql.NullString
	var nodeName sql.NullString
	var hmacSecret sql.NullString
	var hmacHeader sql.NullString
	var hmacAlgo sql.NullString
	var isTest int
	var responseMode string
	var options string
	var createdAt string
	err := row.Scan(&webhookID, &webhook.Path, &webhook.Method, &webhook.NodeID, &nodeName, &webhook.WorkflowID, &isTest, &webhook.AuthMode, &hmacSecret, &hmacHeader, &hmacAlgo, &responseMode, &options, &createdAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan webhook: %w", err)
	}
	webhook.WebhookID = webhookID.String
	webhook.NodeName = nodeName.String
	webhook.IsTest = isTest == 1
	webhook.HMACSecret = hmacSecret.String
	webhook.HMACHeader = hmacHeader.String
	webhook.HMACAlgo = hmacAlgo.String
	webhook.ResponseMode = ResponseMode(responseMode)
	_ = json.Unmarshal([]byte(options), &webhook.Options)
	if parsed, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
		webhook.CreatedAt = parsed
	}
	return &webhook, nil
}

func scanWebhooks(rows *sql.Rows) ([]RegisteredWebhook, error) {
	result := []RegisteredWebhook{}
	for rows.Next() {
		webhook, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *webhook)
	}
	return result, rows.Err()
}

func cleanPath(path string) string {
	return strings.Trim(path, "/")
}

func cleanMethod(method string) string {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		return "ALL"
	}
	return method
}

func pathLength(path string) int {
	path = cleanPath(path)
	if path == "" {
		return 0
	}
	return len(strings.Split(path, "/"))
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
