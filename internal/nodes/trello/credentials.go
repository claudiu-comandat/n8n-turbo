package trello

import (
	"fmt"
	"net/url"
)

const NodeType = "n8n-nodes-base.trello"

type Credential struct {
	APIKey   string `json:"apiKey"`
	APIToken string `json:"apiToken"`
}

func (c Credential) Validate() error {
	if c.APIKey == "" {
		return fmt.Errorf("trello apiKey is required")
	}
	if c.APIToken == "" {
		return fmt.Errorf("trello apiToken is required")
	}
	return nil
}

func (c Credential) AuthParams() url.Values {
	values := url.Values{}
	values.Set("key", c.APIKey)
	values.Set("token", c.APIToken)
	return values
}

func extractCredential(credentials map[string]map[string]any) (Credential, error) {
	for _, key := range []string{"trelloApi", "trello", "credentials"} {
		if credential, ok := credentials[key]; ok {
			cred := Credential{APIKey: stringValue(credential, "apiKey", "key"), APIToken: stringValue(credential, "apiToken", "token")}
			if err := cred.Validate(); err == nil {
				return cred, nil
			}
		}
	}
	for _, credential := range credentials {
		cred := Credential{APIKey: stringValue(credential, "apiKey", "key"), APIToken: stringValue(credential, "apiToken", "token")}
		if err := cred.Validate(); err == nil {
			return cred, nil
		}
	}
	return Credential{}, fmt.Errorf("trelloApi credentials are required")
}
