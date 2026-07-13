package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/n8n-io/n8n-turbo/internal/credentials"
	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/persistence"
	"golang.org/x/sync/singleflight"
)

var oauthRefreshGroup singleflight.Group

func (s *Server) resolveNodeCredentials(ctx context.Context, node dataplane.Node) (map[string]map[string]any, error) {
	result := make(map[string]map[string]any, len(node.Credentials))
	for name, ref := range node.Credentials {
		if ref.ID == "" {
			continue
		}
		row, err := s.credentialStore.GetByID(ctx, ref.ID)
		if err != nil {
			return nil, fmt.Errorf("credential %s for node %s: %w", ref.ID, node.Name, err)
		}
		data, err := s.decryptCredentialData(row.Data)
		if err != nil {
			return nil, fmt.Errorf("decrypt credential %s for node %s: %w", ref.ID, node.Name, err)
		}
		data["id"] = row.ID
		data["name"] = row.Name
		data["type"] = row.Type
		if err := s.refreshNodeCredentialIfNeeded(ctx, row, data); err != nil {
			return nil, fmt.Errorf("refresh credential %s for node %s: %w", ref.ID, node.Name, err)
		}
		data["id"] = row.ID
		data["name"] = row.Name
		data["type"] = row.Type
		result[name] = data
		if row.Type != "" {
			result[row.Type] = data
		}
	}
	return result, nil
}

func (s *Server) refreshNodeCredentialIfNeeded(ctx context.Context, row *persistence.CredentialRow, data map[string]any) error {
	if !isOAuthCredential(row.Type, data) {
		return nil
	}
	accessToken := stringFromMap(data, "accessToken")
	expiresAt := stringFromMap(data, "expiresAt")
	if accessToken != "" && !credentials.OAuth2TokenExpired(expiresAt) {
		return nil
	}
	refreshed, err, _ := oauthRefreshGroup.Do(row.ID, func() (any, error) {
		return s.refreshOAuthTokenFields(ctx, row)
	})
	if err != nil {
		return err
	}
	if fields, ok := refreshed.(map[string]any); ok {
		for key, value := range fields {
			data[key] = value
		}
	}
	return nil
}

func (s *Server) refreshOAuthTokenFields(ctx context.Context, row *persistence.CredentialRow) (map[string]any, error) {
	fresh, err := s.credentialStore.GetByID(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	data, err := s.decryptCredentialData(fresh.Data)
	if err != nil {
		return nil, err
	}
	data["id"] = fresh.ID
	data["name"] = fresh.Name
	data["type"] = fresh.Type
	if token := stringFromMap(data, "accessToken"); token != "" && !credentials.OAuth2TokenExpired(stringFromMap(data, "expiresAt")) {
		return oauthTokenFields(data), nil
	}
	cred := oauth2CredentialFromData(data)
	refreshToken := firstNonEmpty(stringFromMap(data, "refreshToken"), cred.RefreshToken)
	var token *credentials.OAuth2TokenResponse
	if refreshToken != "" {
		token, err = cred.Refresh(ctx, http.DefaultClient, refreshToken)
		if err == nil && token.RefreshToken == "" {
			token.RefreshToken = refreshToken
		}
	} else if strings.EqualFold(cred.GrantType, "clientCredentials") || strings.EqualFold(cred.GrantType, "client_credentials") {
		token, err = cred.ClientCredentials(ctx, http.DefaultClient)
	}
	if err != nil {
		return nil, err
	}
	if token == nil {
		return oauthTokenFields(data), nil
	}
	if err := s.saveOAuthToken(ctx, fresh, data, token); err != nil {
		return nil, err
	}
	return oauthTokenFields(data), nil
}

func oauthTokenFields(data map[string]any) map[string]any {
	fields := make(map[string]any, 8)
	for _, key := range []string{"accessToken", "tokenType", "expiresIn", "expiresAt", "refreshToken", "scope", "idToken", "oauthTokenData"} {
		if value, ok := data[key]; ok {
			fields[key] = value
		}
	}
	return fields
}

func isOAuthCredential(credentialType string, data map[string]any) bool {
	if stringFromMap(data, "accessTokenUrl") != "" {
		return true
	}
	switch credentialType {
	case "oAuth2Api", "googleOAuth2Api", "googleSheetsOAuth2Api", "microsoftTeamsOAuth2Api":
		return true
	default:
		return false
	}
}
