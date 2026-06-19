package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/audit"
)

type AuditStore struct {
	db *sql.DB
}

func NewAuditStore(db *sql.DB) *AuditStore {
	return &AuditStore{db: db}
}

func (s *AuditStore) Init(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS audit_log (
			id TEXT PRIMARY KEY,
			timestamp TEXT NOT NULL,
			event_type TEXT NOT NULL,
			user_id TEXT,
			user_email TEXT,
			resource_type TEXT,
			resource_id TEXT,
			resource_name TEXT,
			metadata TEXT,
			user_agent TEXT,
			ip TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_log(timestamp);
		CREATE INDEX IF NOT EXISTS idx_audit_event_type ON audit_log(event_type);
		CREATE INDEX IF NOT EXISTS idx_audit_resource ON audit_log(resource_type, resource_id);`)
	if err != nil {
		return fmt.Errorf("init audit table: %w", err)
	}
	return nil
}

func (s *AuditStore) Log(ctx context.Context, event audit.Event) (*audit.Event, error) {
	if event.ID == "" {
		event.ID = newID()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	metadata := ""
	if event.Metadata != nil {
		data, err := json.Marshal(event.Metadata)
		if err != nil {
			return nil, fmt.Errorf("marshal audit metadata: %w", err)
		}
		metadata = string(data)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_log (id, timestamp, event_type, user_id, user_email, resource_type, resource_id, resource_name, metadata, user_agent, ip)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID,
		event.Timestamp.Format(time.RFC3339Nano),
		string(event.EventType),
		nullString(event.UserID),
		nullString(event.UserEmail),
		nullString(string(event.ResourceType)),
		nullString(event.ResourceID),
		nullString(event.ResourceName),
		nullString(metadata),
		nullString(event.UserAgent),
		nullString(event.IP),
	)
	if err != nil {
		return nil, fmt.Errorf("insert audit event: %w", err)
	}
	return &event, nil
}

func (s *AuditStore) List(ctx context.Context, filter audit.Filter) ([]audit.Event, int, error) {
	where, args := auditWhere(filter)
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 250 {
		limit = 250
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	query := `SELECT id, timestamp, event_type, user_id, user_email, resource_type, resource_id, resource_name, metadata, user_agent, ip FROM audit_log` + where + ` ORDER BY timestamp DESC LIMIT ? OFFSET ?`
	queryArgs := append(append([]any{}, args...), limit, offset)
	rows, err := s.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list audit events: %w", err)
	}
	defer rows.Close()
	events := make([]audit.Event, 0, limit)
	for rows.Next() {
		event, err := scanAuditEvent(rows)
		if err != nil {
			return nil, 0, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var total int
	countQuery := `SELECT COUNT(*) FROM audit_log` + where
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count audit events: %w", err)
	}
	return events, total, nil
}

func auditWhere(filter audit.Filter) (string, []any) {
	parts := make([]string, 0)
	args := make([]any, 0)
	if len(filter.EventTypes) > 0 {
		placeholders := make([]string, 0, len(filter.EventTypes))
		for _, eventType := range filter.EventTypes {
			placeholders = append(placeholders, "?")
			args = append(args, string(eventType))
		}
		parts = append(parts, "event_type IN ("+strings.Join(placeholders, ",")+")")
	}
	if filter.StartDate != nil {
		parts = append(parts, "timestamp >= ?")
		args = append(args, filter.StartDate.UTC().Format(time.RFC3339Nano))
	}
	if filter.EndDate != nil {
		parts = append(parts, "timestamp <= ?")
		args = append(args, filter.EndDate.UTC().Format(time.RFC3339Nano))
	}
	if filter.UserID != "" {
		parts = append(parts, "user_id = ?")
		args = append(args, filter.UserID)
	}
	if filter.ResourceType != "" {
		parts = append(parts, "resource_type = ?")
		args = append(args, string(filter.ResourceType))
	}
	if filter.ResourceID != "" {
		parts = append(parts, "resource_id = ?")
		args = append(args, filter.ResourceID)
	}
	if len(parts) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(parts, " AND "), args
}

func scanAuditEvent(row scanner) (audit.Event, error) {
	var event audit.Event
	var timestamp string
	var eventType string
	var userID sql.NullString
	var userEmail sql.NullString
	var resourceType sql.NullString
	var resourceID sql.NullString
	var resourceName sql.NullString
	var metadata sql.NullString
	var userAgent sql.NullString
	var ip sql.NullString
	err := row.Scan(&event.ID, &timestamp, &eventType, &userID, &userEmail, &resourceType, &resourceID, &resourceName, &metadata, &userAgent, &ip)
	if err != nil {
		return event, err
	}
	parsed, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return event, fmt.Errorf("parse audit timestamp: %w", err)
	}
	event.Timestamp = parsed
	event.EventType = audit.EventType(eventType)
	event.UserID = userID.String
	event.UserEmail = userEmail.String
	event.ResourceType = audit.ResourceType(resourceType.String)
	event.ResourceID = resourceID.String
	event.ResourceName = resourceName.String
	event.UserAgent = userAgent.String
	event.IP = ip.String
	if metadata.Valid && metadata.String != "" {
		event.Metadata = map[string]any{}
		_ = json.Unmarshal([]byte(metadata.String), &event.Metadata)
	}
	return event, nil
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
