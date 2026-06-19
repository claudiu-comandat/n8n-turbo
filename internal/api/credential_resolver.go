package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/n8n-io/n8n-turbo/internal/credentials"
	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/persistence"
)

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
	cred := oauth2CredentialFromData(data)
	refreshToken := firstNonEmpty(stringFromMap(data, "refreshToken"), cred.RefreshToken)
	var token *credentials.OAuth2TokenResponse
	var err error
	if refreshToken != "" {
		token, err = cred.Refresh(ctx, http.DefaultClient, refreshToken)
		if err == nil && token.RefreshToken == "" {
			token.RefreshToken = refreshToken
		}
	} else if strings.EqualFold(cred.GrantType, "clientCredentials") || strings.EqualFold(cred.GrantType, "client_credentials") {
		token, err = cred.ClientCredentials(ctx, http.DefaultClient)
	}
	if err != nil {
		return err
	}
	if token == nil {
		return nil
	}
	return s.saveOAuthToken(ctx, row, data, token)
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
