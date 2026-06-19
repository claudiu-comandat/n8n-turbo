package telegram

import (
	"fmt"
	"regexp"
	"strings"
)

var tokenRegex = regexp.MustCompile(`^\d+:[A-Za-z0-9_-]{35,}$`)

type Credential struct {
	AccessToken string `json:"accessToken"`
	baseURL     string
	fileBaseURL string
}

func NewCredential(token string) *Credential {
	return &Credential{AccessToken: strings.TrimSpace(token)}
}

func (c *Credential) Validate() error {
	if !tokenRegex.MatchString(c.AccessToken) {
		return fmt.Errorf("invalid Telegram bot token format")
	}
	return nil
}

func (c *Credential) BaseURL() string {
	if c.baseURL != "" {
		return strings.TrimRight(c.baseURL, "/") + "/bot" + c.AccessToken
	}
	return "https://api.telegram.org/bot" + c.AccessToken
}

func (c *Credential) FileURL() string {
	if c.fileBaseURL != "" {
		return strings.TrimRight(c.fileBaseURL, "/") + "/file/bot" + c.AccessToken
	}
	return "https://api.telegram.org/file/bot" + c.AccessToken
}

func extractCredential(credentials map[string]map[string]any, baseURL string) (*Credential, error) {
	for _, key := range []string{"telegramApi", "telegram", "credentials"} {
		if credential, ok := credentials[key]; ok {
			token := stringValue(credential, "accessToken", "token")
			if token != "" {
				return credentialWithBase(token, baseURL), nil
			}
		}
	}
	for _, credential := range credentials {
		token := stringValue(credential, "accessToken", "token")
		if token != "" {
			return credentialWithBase(token, baseURL), nil
		}
	}
	return nil, fmt.Errorf("telegramApi accessToken is required")
}

func credentialWithBase(token string, baseURL string) *Credential {
	credential := NewCredential(token)
	credential.baseURL = baseURL
	credential.fileBaseURL = baseURL
	return credential
}
