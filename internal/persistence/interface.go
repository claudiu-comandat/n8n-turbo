package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/audit"
	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/insights"
)

var ErrNotFound = errors.New("not found")

type UserRow struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	FirstName string    `json:"firstName"`
	LastName  string    `json:"lastName"`
	Password  *string   `json:"-"`
	Role      string    `json:"role"`
	Disabled  bool      `json:"disabled"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type UserStore interface {
	Init(ctx context.Context) error
	GetByEmail(ctx context.Context, email string) (*UserRow, error)
	GetByID(ctx context.Context, id string) (*UserRow, error)
	List(ctx context.Context) ([]UserRow, error)
	Insert(ctx context.Context, user UserRow) error
	HasAny(ctx context.Context) (bool, error)
	UpdatePassword(ctx context.Context, userID, passwordHash string) error
}

type SettingsStore interface {
	Init(ctx context.Context) error
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string) error
}

type WorkflowRow struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Active      bool            `json:"active"`
	Nodes       json.RawMessage `json:"nodes"`
	Connections json.RawMessage `json:"connections"`
	Settings    json.RawMessage `json:"settings"`
	StaticData  json.RawMessage `json:"staticData"`
	PinData     json.RawMessage `json:"pinData"`
	VersionID   string          `json:"versionId"`
	Checksum    string          `json:"checksum,omitempty"`
	Meta        json.RawMessage `json:"meta"`
	Scopes      []string        `json:"scopes,omitempty"`
	OwnerID     string          `json:"-"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
}

type WorkflowStore interface {
	Init(ctx context.Context) error
	List(ctx context.Context, limit int) ([]WorkflowRow, error)
	GetByID(ctx context.Context, id string) (*WorkflowRow, error)
	Save(ctx context.Context, workflow dataplane.Workflow, ownerID string) (*WorkflowRow, error)
	SetActive(ctx context.Context, id string, active bool) error
	Delete(ctx context.Context, id string) error
}

type WorkflowPage struct {
	Rows       []WorkflowRow
	NextCursor string
}

type ExecutionRow struct {
	ID             string          `json:"id"`
	WorkflowID     string          `json:"workflowId"`
	Status         string          `json:"status"`
	Mode           string          `json:"mode"`
	StartedAt      time.Time       `json:"startedAt"`
	StoppedAt      *time.Time      `json:"stoppedAt,omitempty"`
	WaitTill       *time.Time      `json:"waitTill,omitempty"`
	RetryOf        *string         `json:"retryOf,omitempty"`
	RetrySuccessID *string         `json:"retrySuccessId,omitempty"`
	WorkflowData   json.RawMessage `json:"workflowData"`
	Data           json.RawMessage `json:"data"`
	CreatedAt      time.Time       `json:"createdAt"`
}

type ExecutionStore interface {
	Init(ctx context.Context) error
	Create(ctx context.Context, workflow dataplane.Workflow, mode string) (*ExecutionRow, error)
	Finish(ctx context.Context, id string, status string, stoppedAt time.Time, data dataplane.RunExecutionData) error
	MarkWaiting(ctx context.Context, id string, waitTill time.Time, data dataplane.RunExecutionData) error
	ListDueWaiting(ctx context.Context, now time.Time, limit int) ([]ExecutionRow, error)
	GetByID(ctx context.Context, id string) (*ExecutionRow, error)
	List(ctx context.Context, workflowID string, limit int) ([]ExecutionRow, error)
	Delete(ctx context.Context, id string) error
	DeleteOlderThan(ctx context.Context, cutoff time.Time) (int, error)
	PrunePerWorkflow(ctx context.Context, maxCount int) (int, error)
}

type ExecutionPage struct {
	Rows       []ExecutionRow
	NextCursor string
}

type CredentialRow struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data,omitempty"`
	OwnerID   string          `json:"-"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

type CredentialStore interface {
	Init(ctx context.Context) error
	List(ctx context.Context, ownerID string, limit int) ([]CredentialRow, error)
	GetByID(ctx context.Context, id string) (*CredentialRow, error)
	Save(ctx context.Context, credential CredentialRow) (*CredentialRow, error)
	Delete(ctx context.Context, id string) error
}

type VariableRow struct {
	ID        string    `json:"id"`
	Key       string    `json:"key"`
	Type      string    `json:"type"`
	Value     string    `json:"value"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type VariableStore interface {
	Init(ctx context.Context) error
	List(ctx context.Context) ([]VariableRow, error)
	GetByID(ctx context.Context, id string) (*VariableRow, error)
	GetByKey(ctx context.Context, key string) (*VariableRow, error)
	Save(ctx context.Context, variable VariableRow) (*VariableRow, error)
	Delete(ctx context.Context, id string) error
	Resolve(ctx context.Context) (map[string]any, error)
	MaxVariables() int
	InvalidateCache()
}

type TagRow struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type TagStore interface {
	Init(ctx context.Context) error
	List(ctx context.Context) ([]TagRow, error)
	GetByID(ctx context.Context, id string) (*TagRow, error)
	Save(ctx context.Context, tag TagRow) (*TagRow, error)
	Delete(ctx context.Context, id string) error
}

type AuditStore interface {
	Init(ctx context.Context) error
	Log(ctx context.Context, event audit.Event) (*audit.Event, error)
	List(ctx context.Context, filter audit.Filter) ([]audit.Event, int, error)
}

type InsightsStore interface {
	Summary(ctx context.Context, query insights.Query) (insights.SummaryData, error)
	Dashboard(ctx context.Context, query insights.Query) (insights.DashboardData, error)
	WorkflowStats(ctx context.Context, workflowID string, query insights.Query) (insights.WorkflowStat, error)
}
