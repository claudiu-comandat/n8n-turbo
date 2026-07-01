package dataplane

import (
	"encoding/json"
	"fmt"
	"time"
)

type WorkflowID string

type ExecutionID string

type Workflow struct {
	ID            string            `json:"id,omitempty"`
	Name          string            `json:"name"`
	Active        bool              `json:"active"`
	Nodes         []Node            `json:"nodes"`
	Connections   Connections       `json:"connections"`
	Settings      map[string]any    `json:"settings,omitempty"`
	PinData       map[string][]Item `json:"pinData,omitempty"`
	StaticData    map[string]any    `json:"staticData,omitempty"`
	Meta          map[string]any    `json:"meta,omitempty"`
	VersionID     string            `json:"versionId,omitempty"`
	CreatedAt     *time.Time        `json:"createdAt,omitempty"`
	UpdatedAt     *time.Time        `json:"updatedAt,omitempty"`
	presentFields map[string]bool
	Raw           map[string]json.RawMessage `json:"-"`
}

func (w *Workflow) UnmarshalJSON(data []byte) error {
	type workflowAlias Workflow
	var alias workflowAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(data, &raw)
	present := make(map[string]bool, len(raw))
	for key := range raw {
		present[key] = true
	}
	for _, key := range []string{"id", "name", "active", "nodes", "connections", "settings", "pinData", "staticData", "meta", "versionId", "createdAt", "updatedAt"} {
		delete(raw, key)
	}
	*w = Workflow(alias)
	w.presentFields = present
	w.Raw = raw
	return nil
}

func (w Workflow) MarshalJSON() ([]byte, error) {
	type workflowAlias Workflow
	alias := workflowAlias(w)
	alias.Raw = nil
	known, err := json.Marshal(alias)
	if err != nil {
		return nil, err
	}
	merged := copyRawMap(w.Raw)
	if err := overlayJSON(merged, known); err != nil {
		return nil, err
	}
	preserveExplicitObject(merged, w.presentFields, "settings", w.Settings)
	preserveExplicitObject(merged, w.presentFields, "pinData", w.PinData)
	preserveExplicitObject(merged, w.presentFields, "staticData", w.StaticData)
	preserveExplicitObject(merged, w.presentFields, "meta", w.Meta)
	return json.Marshal(merged)
}

func (w *Workflow) PreserveFields(keys ...string) {
	if w.presentFields == nil {
		w.presentFields = map[string]bool{}
	}
	for _, key := range keys {
		w.presentFields[key] = true
	}
}

type Node struct {
	ID                    string                   `json:"id,omitempty"`
	Name                  string                   `json:"name"`
	Type                  string                   `json:"type"`
	TypeVersion           float64                  `json:"typeVersion,omitempty"`
	Position              []float64                `json:"position,omitempty"`
	Parameters            map[string]any           `json:"parameters"`
	Credentials           map[string]CredentialRef `json:"credentials,omitempty"`
	Disabled              bool                     `json:"disabled,omitempty"`
	ContinueOnFail        bool                     `json:"continueOnFail,omitempty"`
	OnError               string                   `json:"onError,omitempty"`
	AlwaysOutputData      bool                     `json:"alwaysOutputData,omitempty"`
	ExecuteOnce           bool                     `json:"executeOnce,omitempty"`
	RetryOnFail           bool                     `json:"retryOnFail,omitempty"`
	MaxTries              int                      `json:"maxTries,omitempty"`
	WaitBetweenTries      int                      `json:"waitBetweenTries,omitempty"`
	UseExponentialBackoff bool                     `json:"useExponentialBackoff,omitempty"`
	Notes                 string                   `json:"notes,omitempty"`
	NotesInFlow           bool                     `json:"notesInFlow,omitempty"`
	Color                 string                   `json:"color,omitempty"`
	WebhookID             string                   `json:"webhookId,omitempty"`
	presentFields         map[string]bool
	Raw                   map[string]json.RawMessage `json:"-"`
}

func (n *Node) UnmarshalJSON(data []byte) error {
	type nodeAlias Node
	var alias nodeAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(data, &raw)
	present := make(map[string]bool, len(raw))
	for key := range raw {
		present[key] = true
	}
	for _, key := range []string{"id", "name", "type", "typeVersion", "position", "parameters", "credentials", "disabled", "continueOnFail", "onError", "alwaysOutputData", "executeOnce", "retryOnFail", "maxTries", "waitBetweenTries", "useExponentialBackoff", "notes", "notesInFlow", "color", "webhookId"} {
		delete(raw, key)
	}
	*n = Node(alias)
	n.presentFields = present
	n.Raw = raw
	return nil
}

func (n Node) MarshalJSON() ([]byte, error) {
	type nodeAlias Node
	alias := nodeAlias(n)
	alias.Raw = nil
	known, err := json.Marshal(alias)
	if err != nil {
		return nil, err
	}
	merged := copyRawMap(n.Raw)
	if err := overlayJSON(merged, known); err != nil {
		return nil, err
	}
	for _, field := range []struct {
		jsonKey string
		value   bool
	}{
		{"disabled", n.Disabled},
		{"continueOnFail", n.ContinueOnFail},
		{"alwaysOutputData", n.AlwaysOutputData},
		{"executeOnce", n.ExecuteOnce},
		{"retryOnFail", n.RetryOnFail},
		{"useExponentialBackoff", n.UseExponentialBackoff},
		{"notesInFlow", n.NotesInFlow},
	} {
		if field.value || n.presentFields[field.jsonKey] {
			merged[field.jsonKey] = json.RawMessage(fmt.Sprintf("%t", field.value))
		}
	}
	return json.Marshal(merged)
}

func copyRawMap(raw map[string]json.RawMessage) map[string]json.RawMessage {
	merged := make(map[string]json.RawMessage, len(raw))
	for key, value := range raw {
		merged[key] = value
	}
	return merged
}

func overlayJSON(target map[string]json.RawMessage, source []byte) error {
	var known map[string]json.RawMessage
	if err := json.Unmarshal(source, &known); err != nil {
		return err
	}
	for key, value := range known {
		if string(value) == "null" {
			continue
		}
		target[key] = value
	}
	return nil
}

func preserveExplicitObject(target map[string]json.RawMessage, present map[string]bool, key string, value any) {
	if !present[key] {
		return
	}
	data, err := json.Marshal(value)
	if err != nil || string(data) == "null" {
		target[key] = json.RawMessage("{}")
		return
	}
	target[key] = data
}

type OnErrorBehavior string

const (
	OnErrorStopWorkflow          OnErrorBehavior = "stopWorkflow"
	OnErrorContinueRegularOutput OnErrorBehavior = "continueRegularOutput"
	OnErrorContinueErrorOutput   OnErrorBehavior = "continueErrorOutput"
)

type RetryConfig struct {
	Enabled               bool
	MaxTries              int
	WaitBetweenTries      int
	UseExponentialBackoff bool
}

func (n Node) EffectiveOnError() OnErrorBehavior {
	if n.OnError != "" {
		return OnErrorBehavior(n.OnError)
	}
	if n.ContinueOnFail {
		return OnErrorContinueRegularOutput
	}
	return OnErrorStopWorkflow
}

func (n Node) RetryConfig() RetryConfig {
	cfg := RetryConfig{
		Enabled:               n.RetryOnFail,
		MaxTries:              n.MaxTries,
		WaitBetweenTries:      n.WaitBetweenTries,
		UseExponentialBackoff: n.UseExponentialBackoff,
	}
	if cfg.MaxTries <= 0 {
		cfg.MaxTries = 3
	}
	if cfg.MaxTries > 10 {
		cfg.MaxTries = 10
	}
	if cfg.MaxTries < 1 {
		cfg.MaxTries = 1
	}
	if cfg.WaitBetweenTries <= 0 {
		cfg.WaitBetweenTries = 1000
	}
	return cfg
}

type CredentialRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Connection struct {
	Node  string `json:"node"`
	Type  string `json:"type"`
	Index int    `json:"index"`
}

type Connections map[string]map[string][][]Connection

type InverseConnection struct {
	Node      string `json:"node"`
	OutputIdx int    `json:"outputIndex"`
}

type InvertedConnections map[string]map[string][][]InverseConnection

type Item struct {
	JSON       map[string]any    `json:"json"`
	Binary     map[string]Binary `json:"binary,omitempty"`
	PairedItem *PairedItem       `json:"pairedItem,omitempty"`
	Error      *NodeError        `json:"error,omitempty"`
}

type Binary struct {
	ID            string `json:"id,omitempty"`
	Data          string `json:"data,omitempty"`
	MimeType      string `json:"mimeType,omitempty"`
	FileType      string `json:"fileType,omitempty"`
	FileName      string `json:"fileName,omitempty"`
	FileSize      int64  `json:"fileSize,omitempty"`
	FileExtension string `json:"fileExtension,omitempty"`
	Directory     string `json:"directory,omitempty"`
}

type PairedItem struct {
	Item  int  `json:"item"`
	Input *int `json:"input,omitempty"`
}

type Output [][]Item

type NodeExecutionData map[string][][]Item

type RunData map[string][]TaskData

type TaskData struct {
	StartTime       int64             `json:"startTime"`
	ExecutionTime   int64             `json:"executionTime"`
	ExecutionStatus string            `json:"executionStatus,omitempty"`
	ExecutionIndex  int               `json:"executionIndex,omitempty"`
	Source          []any             `json:"source"`
	Data            NodeExecutionData `json:"data"`
	Error           *ExecutionError   `json:"error,omitempty"`
}

type NodeError struct {
	Name        string         `json:"name"`
	Message     string         `json:"message"`
	Description string         `json:"description,omitempty"`
	Level       string         `json:"level,omitempty"`
	Context     map[string]any `json:"context,omitempty"`
	Stack       string         `json:"stack,omitempty"`
	HTTPCode    int            `json:"httpCode,omitempty"`
	Timestamp   int64          `json:"timestamp,omitempty"`
}

func (e *NodeError) Error() string {
	if e.Description != "" {
		return fmt.Sprintf("%s: %s (%s)", e.Name, e.Message, e.Description)
	}
	return fmt.Sprintf("%s: %s", e.Name, e.Message)
}

type ExecutionError struct {
	Name        string         `json:"name"`
	Message     string         `json:"message"`
	Description string         `json:"description,omitempty"`
	NodeType    string         `json:"nodeType,omitempty"`
	Node        string         `json:"node,omitempty"`
	Stack       string         `json:"stack,omitempty"`
	Timestamp   int64          `json:"timestamp,omitempty"`
	Context     map[string]any `json:"context,omitempty"`
}

func (e *ExecutionError) Error() string {
	return fmt.Sprintf("%s: %s", e.Name, e.Message)
}

type RunExecutionData struct {
	StartData   map[string]any `json:"startData"`
	ResultData  ResultData     `json:"resultData"`
	ResumeToken string         `json:"resumeToken,omitempty"`
}

type ResultData struct {
	RunData          RunData           `json:"runData"`
	PinData          map[string][]Item `json:"pinData,omitempty"`
	LastNodeExecuted string            `json:"lastNodeExecuted,omitempty"`
	Error            *ExecutionError   `json:"error,omitempty"`
}

func MainOutput(items []Item) Output {
	return Output{items}
}

func EmptyOutput() Output {
	return Output{[]Item{}}
}
