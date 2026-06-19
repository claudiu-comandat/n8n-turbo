package sendgrid

import (
	"fmt"
	"strings"
)

type Credential struct {
	APIKey string `json:"apiKey"`
}

func (c Credential) Validate() error {
	if strings.TrimSpace(c.APIKey) == "" {
		return fmt.Errorf("sendgrid apiKey is required")
	}
	if !strings.HasPrefix(c.APIKey, "SG.") {
		return fmt.Errorf("sendgrid invalid API key format")
	}
	return nil
}

func (c Credential) AuthHeader() string {
	return "Bearer " + c.APIKey
}

func extractCredential(credentials map[string]map[string]any) (Credential, error) {
	for _, key := range []string{"sendGridApi", "sendgrid", "credentials"} {
		if credential, ok := credentials[key]; ok {
			cred := Credential{APIKey: stringValue(credential, "apiKey", "token")}
			if err := cred.Validate(); err == nil {
				return cred, nil
			}
		}
	}
	for _, credential := range credentials {
		cred := Credential{APIKey: stringValue(credential, "apiKey", "token")}
		if err := cred.Validate(); err == nil {
			return cred, nil
		}
	}
	return Credential{}, fmt.Errorf("sendGridApi apiKey is required")
}
