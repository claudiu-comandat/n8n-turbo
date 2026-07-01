package metadata

import "encoding/json"

type NodeType struct {
	Raw                     map[string]any    `json:"-"`
	Name                    string            `json:"name"`
	DisplayName             string            `json:"displayName"`
	Description             string            `json:"description"`
	Version                 any               `json:"version"`
	DefaultVersion          any               `json:"defaultVersion,omitempty"`
	Subtitle                string            `json:"subtitle,omitempty"`
	Defaults                NodeDefaults      `json:"defaults"`
	Properties              []Property        `json:"properties"`
	Inputs                  any               `json:"inputs"`
	Outputs                 any               `json:"outputs"`
	OutputNames             []string          `json:"outputNames,omitempty"`
	Credentials             []CredentialUsage `json:"credentials,omitempty"`
	Webhooks                []Webhook         `json:"webhooks,omitempty"`
	Icon                    string            `json:"icon,omitempty"`
	IconURL                 string            `json:"iconUrl,omitempty"`
	IconColor               string            `json:"iconColor,omitempty"`
	Group                   []string          `json:"group"`
	Category                string            `json:"category,omitempty"`
	DocumentationURL        string            `json:"documentationUrl,omitempty"`
	MaxNodes                int               `json:"maxNodes,omitempty"`
	SupportsCORS            bool              `json:"supportsCORS,omitempty"`
	ActivationMessage       string            `json:"activationMessage,omitempty"`
	EventTriggerDescription string            `json:"eventTriggerDescription,omitempty"`
	TriggerPanel            map[string]any    `json:"triggerPanel,omitempty"`
	RequestDefaults         map[string]any    `json:"requestDefaults,omitempty"`
	BuilderHint             map[string]any    `json:"builderHint,omitempty"`
	Hints                   []map[string]any  `json:"hints,omitempty"`
	Codex                   *NodeCodex        `json:"codex,omitempty"`
}

func (node NodeType) MarshalJSON() ([]byte, error) {
	if node.Raw != nil {
		return json.Marshal(node.Raw)
	}
	type nodeTypeAlias NodeType
	return json.Marshal(nodeTypeAlias(node))
}

type Webhook struct {
	Name                       string `json:"name"`
	HTTPMethod                 any    `json:"httpMethod,omitempty"`
	IsFullPath                 bool   `json:"isFullPath,omitempty"`
	ResponseCode               any    `json:"responseCode,omitempty"`
	ResponseMode               any    `json:"responseMode,omitempty"`
	ResponseData               any    `json:"responseData,omitempty"`
	ResponseBinaryPropertyName any    `json:"responseBinaryPropertyName,omitempty"`
	ResponseContentType        any    `json:"responseContentType,omitempty"`
	ResponsePropertyName       any    `json:"responsePropertyName,omitempty"`
	ResponseHeaders            any    `json:"responseHeaders,omitempty"`
	Path                       any    `json:"path,omitempty"`
}

type NodeCodex struct {
	Categories    []string            `json:"categories,omitempty"`
	Subcategories map[string][]string `json:"subcategories,omitempty"`
	Resources     map[string]any      `json:"resources,omitempty"`
}

type NodeDefaults struct {
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

type Property struct {
	DisplayName      string            `json:"displayName"`
	Name             string            `json:"name"`
	Type             string            `json:"type"`
	TypeOptions      map[string]any    `json:"typeOptions,omitempty"`
	CredentialTypes  []string          `json:"credentialTypes,omitempty"`
	Default          any               `json:"default"`
	Required         bool              `json:"required,omitempty"`
	Description      string            `json:"description,omitempty"`
	Hint             string            `json:"hint,omitempty"`
	Placeholder      string            `json:"placeholder,omitempty"`
	Options          any               `json:"options,omitempty"`
	DisplayOptions   map[string]any    `json:"displayOptions,omitempty"`
	DisabledOptions  map[string]any    `json:"disabledOptions,omitempty"`
	Routing          map[string]any    `json:"routing,omitempty"`
	NoDataExpression bool              `json:"noDataExpression,omitempty"`
	ExtractValue     map[string]any    `json:"extractValue,omitempty"`
	Modes            []ParameterMode   `json:"modes,omitempty"`
	Documentation    map[string]string `json:"documentation,omitempty"`
	BuilderHint      map[string]any    `json:"builderHint,omitempty"`
}

type ParameterMode struct {
	Name        string         `json:"name"`
	DisplayName string         `json:"displayName"`
	Type        string         `json:"type"`
	Placeholder string         `json:"placeholder,omitempty"`
	TypeOptions map[string]any `json:"typeOptions,omitempty"`
}

type Option struct {
	Name        string     `json:"name"`
	Value       any        `json:"value,omitempty"`
	Description string     `json:"description,omitempty"`
	Action      string     `json:"action,omitempty"`
	DisplayName string     `json:"displayName,omitempty"`
	Values      []Property `json:"values,omitempty"`
}

type CredentialUsage struct {
	Name     string         `json:"name"`
	Required bool           `json:"required"`
	Display  map[string]any `json:"displayOptions,omitempty"`
}

type CredentialType struct {
	Name             string         `json:"name"`
	DisplayName      string         `json:"displayName"`
	DocumentationURL string         `json:"documentationUrl,omitempty"`
	Properties       []Property     `json:"properties"`
	Authenticate     map[string]any `json:"authenticate,omitempty"`
	Test             map[string]any `json:"test,omitempty"`
	GenericAuth      bool           `json:"genericAuth,omitempty"`
	IconURL          string         `json:"iconUrl,omitempty"`
	Extends          []string       `json:"extends,omitempty"`
}
