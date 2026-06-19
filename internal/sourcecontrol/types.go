package sourcecontrol

import (
	"encoding/json"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/persistence"
)

type Config struct {
	ID                   string    `json:"id"`
	Active               bool      `json:"connected"`
	RepoURL              string    `json:"repositoryUrl"`
	Branch               string    `json:"branchName"`
	PrivateKey           string    `json:"privateKey,omitempty"`
	PrivateKeyPassphrase string    `json:"privateKeyPassphrase,omitempty"`
	Username             string    `json:"username,omitempty"`
	Password             string    `json:"password,omitempty"`
	AuthorName           string    `json:"authorName"`
	AuthorEmail          string    `json:"authorEmail"`
	SyncOnStart          bool      `json:"syncOnStart"`
	CreatedAt            time.Time `json:"createdAt"`
	UpdatedAt            time.Time `json:"updatedAt"`
}

type SourceControlledFile struct {
	File      string `json:"file"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	Conflict  bool   `json:"conflict"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

type PushOptions struct {
	Message   string   `json:"message"`
	Force     bool     `json:"force"`
	FileNames []string `json:"fileNames,omitempty"`
}

type PushResult struct {
	Status string   `json:"status"`
	Files  []string `json:"files"`
	Commit string   `json:"commit,omitempty"`
}

type PullResult struct {
	StatusCode string                 `json:"statusCode"`
	Files      []SourceControlledFile `json:"files"`
	Conflicts  []string               `json:"conflicts,omitempty"`
}

type StatusResult struct {
	Ahead      int                    `json:"ahead"`
	Behind     int                    `json:"behind"`
	Modified   []SourceControlledFile `json:"modified"`
	Added      []SourceControlledFile `json:"added"`
	Deleted    []SourceControlledFile `json:"deleted"`
	Untracked  []SourceControlledFile `json:"untracked"`
	Conflicted []SourceControlledFile `json:"conflicted"`
}

type PushDependencies struct {
	Workflows   []persistence.WorkflowRow
	Credentials []persistence.CredentialRow
	Variables   []persistence.VariableRow
	Tags        []persistence.TagRow
}

type WorkflowExport struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Active      bool            `json:"active"`
	Nodes       json.RawMessage `json:"nodes"`
	Connections json.RawMessage `json:"connections"`
	Settings    json.RawMessage `json:"settings,omitempty"`
	StaticData  json.RawMessage `json:"staticData,omitempty"`
	PinData     json.RawMessage `json:"pinData,omitempty"`
	Meta        json.RawMessage `json:"meta,omitempty"`
	VersionID   string          `json:"versionId,omitempty"`
	ExportedAt  string          `json:"exportedAt"`
}

type CredentialExport struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

type VariableExport struct {
	ID    string `json:"id"`
	Key   string `json:"key"`
	Type  string `json:"type"`
	Value string `json:"value,omitempty"`
}
