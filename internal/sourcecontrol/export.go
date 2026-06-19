package sourcecontrol

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/persistence"
)

const (
	WorkflowsDir   = "workflows"
	CredentialsDir = "credentials"
	VariablesFile  = "variable_placeholders.json"
	TagsFile       = "tags.json"
)

type Exporter struct {
	repoPath string
}

func NewExporter(repoPath string) *Exporter {
	return &Exporter{repoPath: repoPath}
}

func (e *Exporter) ExportAll(deps PushDependencies) ([]string, error) {
	files := []string{}
	workflowFiles, err := e.ExportWorkflows(deps.Workflows)
	if err != nil {
		return nil, err
	}
	files = append(files, workflowFiles...)
	credentialFiles, err := e.ExportCredentials(deps.Credentials)
	if err != nil {
		return nil, err
	}
	files = append(files, credentialFiles...)
	if err := e.ExportVariables(deps.Variables); err != nil {
		return nil, err
	}
	files = append(files, VariablesFile)
	if err := e.ExportTags(deps.Tags); err != nil {
		return nil, err
	}
	files = append(files, TagsFile)
	return files, nil
}

func (e *Exporter) ExportWorkflows(workflows []persistence.WorkflowRow) ([]string, error) {
	dir := filepath.Join(e.repoPath, WorkflowsDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	files := []string{}
	for _, workflow := range workflows {
		name := workflow.Name
		if name == "" {
			name = workflow.ID
		}
		relative := filepath.ToSlash(filepath.Join(WorkflowsDir, sanitizeFilename(name)+"."+workflow.ID+".json"))
		export := WorkflowExport{
			ID:          workflow.ID,
			Name:        workflow.Name,
			Active:      workflow.Active,
			Nodes:       workflow.Nodes,
			Connections: workflow.Connections,
			Settings:    workflow.Settings,
			StaticData:  workflow.StaticData,
			PinData:     workflow.PinData,
			Meta:        workflow.Meta,
			VersionID:   workflow.VersionID,
			ExportedAt:  time.Now().UTC().Format(time.RFC3339),
		}
		if err := writeJSONFile(filepath.Join(e.repoPath, filepath.FromSlash(relative)), export); err != nil {
			return nil, fmt.Errorf("export workflow %s: %w", workflow.ID, err)
		}
		files = append(files, relative)
	}
	return files, nil
}

func (e *Exporter) ExportCredentials(credentials []persistence.CredentialRow) ([]string, error) {
	dir := filepath.Join(e.repoPath, CredentialsDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	files := []string{}
	for _, credential := range credentials {
		name := credential.Name
		if name == "" {
			name = credential.ID
		}
		relative := filepath.ToSlash(filepath.Join(CredentialsDir, sanitizeFilename(name)+"."+credential.ID+".json"))
		export := CredentialExport{ID: credential.ID, Name: credential.Name, Type: credential.Type, Data: credential.Data}
		if err := writeJSONFile(filepath.Join(e.repoPath, filepath.FromSlash(relative)), export); err != nil {
			return nil, fmt.Errorf("export credential %s: %w", credential.ID, err)
		}
		files = append(files, relative)
	}
	return files, nil
}

func (e *Exporter) ExportVariables(variables []persistence.VariableRow) error {
	placeholders := make([]VariableExport, 0, len(variables))
	for _, variable := range variables {
		placeholder := VariableExport{ID: variable.ID, Key: variable.Key, Type: variable.Type}
		if strings.ToLower(variable.Type) != "secret" {
			placeholder.Value = variable.Value
		}
		placeholders = append(placeholders, placeholder)
	}
	return writeJSONFile(filepath.Join(e.repoPath, VariablesFile), placeholders)
}

func (e *Exporter) ExportTags(tags []persistence.TagRow) error {
	return writeJSONFile(filepath.Join(e.repoPath, TagsFile), tags)
}

func writeJSONFile(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

var invalidFileCharacters = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1F]+`)

func sanitizeFilename(name string) string {
	clean := invalidFileCharacters.ReplaceAllString(strings.TrimSpace(name), "_")
	clean = strings.Join(strings.Fields(clean), "_")
	clean = strings.Trim(clean, "._- ")
	if clean == "" {
		return "resource"
	}
	return clean
}
