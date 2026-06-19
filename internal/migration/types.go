package migration

import "time"

type Result struct {
	Compatible        bool               `json:"compatible"`
	Warnings          []string           `json:"warnings,omitempty"`
	Errors            []string           `json:"errors,omitempty"`
	TableStats        map[string]int64   `json:"tableStats"`
	TableChecks       []TableCheck       `json:"tableChecks,omitempty"`
	ExecutionStats    ExecutionStats     `json:"executionStats"`
	ExecutionStatus   []StatusStat       `json:"executionStatus,omitempty"`
	CredentialStats   []CredentialStat   `json:"credentialStats,omitempty"`
	UserStats         UserStats          `json:"userStats"`
	VariableStats     []VariableStat     `json:"variableStats,omitempty"`
	WebhookDuplicates []WebhookDuplicate `json:"webhookDuplicates,omitempty"`
	CheckedAt         time.Time          `json:"checkedAt"`
}

type TableCheck struct {
	Table          string   `json:"table"`
	Exists         bool     `json:"exists"`
	MissingColumns []string `json:"missingColumns,omitempty"`
	Warnings       []string `json:"warnings,omitempty"`
}

type ExecutionStats struct {
	Total        int64 `json:"total"`
	FlattedValid int64 `json:"flattedValid"`
	Invalid      int64 `json:"invalid"`
	NullData     int64 `json:"nullData"`
}

type StatusStat struct {
	Status   string `json:"status"`
	Finished bool   `json:"finished"`
	Count    int64  `json:"count"`
}

type CredentialStat struct {
	Type      string `json:"type"`
	Count     int64  `json:"count"`
	JSONValid int64  `json:"jsonValid"`
}

type UserStats struct {
	Total       int64 `json:"total"`
	BcryptValid int64 `json:"bcryptValid"`
	NoPassword  int64 `json:"noPassword"`
}

type VariableStat struct {
	Type  string `json:"type"`
	Count int64  `json:"count"`
}

type WebhookDuplicate struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Count  int64  `json:"count"`
}
