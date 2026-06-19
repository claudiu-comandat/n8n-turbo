package descriptor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Descriptor struct {
	Name           string            `json:"name,omitempty"`
	NodeType       string            `json:"nodeType,omitempty"`
	DisplayName    string            `json:"displayName,omitempty"`
	Version        int               `json:"version,omitempty"`
	Description    string            `json:"description,omitempty"`
	BaseURL        string            `json:"baseUrl,omitempty"`
	AuthType       string            `json:"authType,omitempty"`
	AuthConfig     map[string]string `json:"authConfig,omitempty"`
	DefaultHeaders map[string]string `json:"defaultHeaders,omitempty"`
	ErrorCheck     *ErrorCheck       `json:"errorCheck,omitempty"`
	Pagination     *Pagination       `json:"pagination,omitempty"`
	CredentialType string            `json:"credentialType,omitempty"`
	IconURL        string            `json:"iconUrl,omitempty"`
	Category       string            `json:"category,omitempty"`
	Tags           []string          `json:"tags,omitempty"`
	Operations     map[string]Operation
}

type Operation struct {
	Name        string            `json:"name,omitempty"`
	DisplayName string            `json:"displayName,omitempty"`
	Description string            `json:"description,omitempty"`
	Method      string            `json:"method"`
	Path        string            `json:"path"`
	BodyType    string            `json:"bodyType,omitempty"`
	Params      []Param           `json:"params,omitempty"`
	Pagination  *Pagination       `json:"pagination,omitempty"`
	Transform   string            `json:"transform,omitempty"`
	ResponseMap string            `json:"responseMap,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
}

type Param struct {
	Name        string   `json:"name"`
	DisplayName string   `json:"displayName,omitempty"`
	In          string   `json:"in"`
	Required    bool     `json:"required,omitempty"`
	Type        string   `json:"type,omitempty"`
	Default     any      `json:"default,omitempty"`
	Description string   `json:"description,omitempty"`
	Options     []Option `json:"options,omitempty"`
}

type Option struct {
	Name  string `json:"name"`
	Value any    `json:"value"`
}

type Pagination struct {
	Type         string `json:"type,omitempty"`
	LimitParam   string `json:"limitParam,omitempty"`
	OffsetParam  string `json:"offsetParam,omitempty"`
	CursorParam  string `json:"cursorParam,omitempty"`
	PageParam    string `json:"pageParam,omitempty"`
	PerPageParam string `json:"perPageParam,omitempty"`
	CursorPath   string `json:"cursorPath,omitempty"`
	NextPagePath string `json:"nextPagePath,omitempty"`
	DataPath     string `json:"dataPath,omitempty"`
	MaxItems     int    `json:"maxItems,omitempty"`
	DefaultLimit int    `json:"defaultLimit,omitempty"`
	StopOnEmpty  bool   `json:"stopOnEmpty,omitempty"`
}

type ErrorCheck struct {
	Field        string `json:"field"`
	FalseIsError bool   `json:"falseIsError,omitempty"`
	MessageField string `json:"messageField,omitempty"`
	CodeField    string `json:"codeField,omitempty"`
}

type NodeDescriptor = Descriptor
type OperationDescriptor = Operation
type ParamDescriptor = Param
type PaginationConfig = Pagination
type ErrorCheckConfig = ErrorCheck

func (d *Descriptor) UnmarshalJSON(data []byte) error {
	type alias Descriptor
	var raw struct {
		alias
		Operations json.RawMessage `json:"operations"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*d = Descriptor(raw.alias)
	if len(raw.Operations) > 0 && string(raw.Operations) != "null" {
		if err := d.unmarshalOperations(raw.Operations); err != nil {
			return err
		}
	}
	d.Normalize()
	return nil
}

func (d *Descriptor) Normalize() {
	if d.NodeType == "" && d.Name != "" {
		if strings.HasPrefix(d.Name, "n8n-nodes-base.") {
			d.NodeType = d.Name
		} else {
			d.NodeType = "n8n-nodes-base." + d.Name
		}
	}
	if d.Name == "" {
		d.Name = strings.TrimPrefix(d.NodeType, "n8n-nodes-base.")
	}
	if d.Operations == nil {
		d.Operations = map[string]Operation{}
	}
	for name, operation := range d.Operations {
		if operation.Name == "" {
			operation.Name = name
		}
		if operation.Method == "" {
			operation.Method = "GET"
		}
		d.Operations[name] = operation
	}
}

func (d *Descriptor) unmarshalOperations(data []byte) error {
	var operations map[string]Operation
	if err := json.Unmarshal(data, &operations); err == nil {
		d.Operations = operations
		return nil
	}
	var list []Operation
	if err := json.Unmarshal(data, &list); err != nil {
		return fmt.Errorf("descriptor operations: %w", err)
	}
	operations = make(map[string]Operation, len(list))
	for _, operation := range list {
		if operation.Name == "" {
			return fmt.Errorf("descriptor operation missing name")
		}
		operations[operation.Name] = operation
	}
	d.Operations = operations
	return nil
}

func LoadJSON(data []byte) (Descriptor, error) {
	var descriptor Descriptor
	if err := json.Unmarshal(data, &descriptor); err != nil {
		return Descriptor{}, err
	}
	descriptor.Normalize()
	return descriptor, nil
}

func LoadFile(path string) (Descriptor, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Descriptor{}, err
	}
	return LoadJSON(data)
}

func LoadDir(path string) ([]Descriptor, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	descriptors := []Descriptor{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		descriptor, err := LoadFile(filepath.Join(path, entry.Name()))
		if err != nil {
			return nil, err
		}
		descriptors = append(descriptors, descriptor)
	}
	return descriptors, nil
}
