package discord

import (
	"fmt"
	"strings"
)

type Credential struct {
	BotToken string `json:"botToken"`
	ClientID string `json:"clientId"`
}

func (c Credential) Validate() error {
	if strings.TrimSpace(c.BotToken) == "" {
		return fmt.Errorf("discord botToken is required")
	}
	return nil
}

func (c Credential) AuthHeader() string {
	token := strings.TrimSpace(c.BotToken)
	if strings.HasPrefix(strings.ToLower(token), "bot ") || strings.HasPrefix(strings.ToLower(token), "bearer ") {
		return token
	}
	return "Bot " + token
}

func extractCredential(credentials map[string]map[string]any) (Credential, error) {
	for _, key := range []string{"discordBotApi", "discord", "credentials"} {
		if credential, ok := credentials[key]; ok {
			cred := Credential{BotToken: stringValue(credential, "botToken", "token", "accessToken"), ClientID: stringValue(credential, "clientId")}
			if err := cred.Validate(); err == nil {
				return cred, nil
			}
		}
	}
	for _, credential := range credentials {
		cred := Credential{BotToken: stringValue(credential, "botToken", "token", "accessToken"), ClientID: stringValue(credential, "clientId")}
		if err := cred.Validate(); err == nil {
			return cred, nil
		}
	}
	return Credential{}, fmt.Errorf("discordBotApi botToken is required")
}
