package msteams

import (
	"fmt"
	"time"
)

const NodeType = "n8n-nodes-base.microsoftTeams"

type Credential struct {
	TenantID     string `json:"tenantId"`
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresAt    int64  `json:"expiresAt"`
}

func (c *Credential) Validate() error {
	if c.AccessToken == "" && c.RefreshToken == "" {
		return fmt.Errorf("msteams no valid tokens available")
	}
	if c.TenantID == "" {
		c.TenantID = "common"
	}
	return nil
}

func (c Credential) IsExpired() bool {
	if c.ExpiresAt == 0 {
		return false
	}
	return time.Now().Unix() >= c.ExpiresAt-60
}

func (c Credential) AuthHeader() string {
	return "Bearer " + c.AccessToken
}

func extractCredential(credentials map[string]map[string]any) (Credential, error) {
	for _, key := range []string{"microsoftTeamsOAuth2Api", "microsoftTeams", "oAuth2Api", "credentials"} {
		if credential, ok := credentials[key]; ok {
			cred := Credential{
				TenantID:     stringValue(credential, "tenantId"),
				ClientID:     stringValue(credential, "clientId"),
				ClientSecret: stringValue(credential, "clientSecret"),
				AccessToken:  stringValue(credential, "accessToken"),
				RefreshToken: stringValue(credential, "refreshToken"),
				ExpiresAt:    int64Value(credential, "expiresAt", "expires_at"),
			}
			if err := cred.Validate(); err == nil {
				return cred, nil
			}
		}
	}
	for _, credential := range credentials {
		cred := Credential{
			TenantID:     stringValue(credential, "tenantId"),
			ClientID:     stringValue(credential, "clientId"),
			ClientSecret: stringValue(credential, "clientSecret"),
			AccessToken:  stringValue(credential, "accessToken"),
			RefreshToken: stringValue(credential, "refreshToken"),
			ExpiresAt:    int64Value(credential, "expiresAt", "expires_at"),
		}
		if err := cred.Validate(); err == nil {
			return cred, nil
		}
	}
	return Credential{}, fmt.Errorf("microsoftTeamsOAuth2Api credentials are required")
}
