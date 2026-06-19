package sourcecontrol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/n8n-io/n8n-turbo/internal/persistence"
)

type Importer struct {
	repoPath string
}

func NewImporter(repoPath string) *Importer {
	return &Importer{repoPath: repoPath}
}

func (i *Importer) ImportWorkflows() ([]persistence.WorkflowRow, []SourceControlledFile, error) {
	dir := filepath.Join(i.repoPath, WorkflowsDir)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	rows := []persistence.WorkflowRow{}
	files := []SourceControlledFile{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, err
		}
		var exported WorkflowExport
		if err := json.Unmarshal(data, &exported); err != nil {
			return nil, nil, err
		}
		row := persistence.WorkflowRow{
			ID:          exported.ID,
			Name:        exported.Name,
			Active:      exported.Active,
			Nodes:       exported.Nodes,
			Connections: exported.Connections,
			Settings:    exported.Settings,
			StaticData:  exported.StaticData,
			PinData:     exported.PinData,
			Meta:        exported.Meta,
			VersionID:   exported.VersionID,
		}
		rows = append(rows, row)
		files = append(files, SourceControlledFile{
			File: filepath.ToSlash(filepath.Join(WorkflowsDir, entry.Name())),
			ID:   exported.ID,
			Name: exported.Name,
			Type: "workflow",
		})
	}
	return rows, files, nil
}

func (i *Importer) ImportVariables() ([]persistence.VariableRow, []SourceControlledFile, error) {
	path := filepath.Join(i.repoPath, VariablesFile)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var exported []VariableExport
	if err := json.Unmarshal(data, &exported); err != nil {
		return nil, nil, err
	}
	rows := make([]persistence.VariableRow, 0, len(exported))
	files := make([]SourceControlledFile, 0, len(exported))
	for _, variable := range exported {
		rows = append(rows, persistence.VariableRow{ID: variable.ID, Key: variable.Key, Type: variable.Type, Value: variable.Value})
		files = append(files, SourceControlledFile{File: VariablesFile, ID: variable.ID, Name: variable.Key, Type: "variable"})
	}
	return rows, files, nil
}

func (i *Importer) ImportAll() (*PullResult, error) {
	_, workflowFiles, err := i.ImportWorkflows()
	if err != nil {
		return nil, err
	}
	_, variableFiles, err := i.ImportVariables()
	if err != nil {
		return nil, err
	}
	files := append(workflowFiles, variableFiles...)
	return &PullResult{StatusCode: "pulled", Files: files}, nil
}
