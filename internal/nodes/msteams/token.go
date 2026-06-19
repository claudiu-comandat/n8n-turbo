package msteams

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
}

func (n *Node) refreshToken(ctx context.Context, cred *Credential) error {
	tenantID := cred.TenantID
	if tenantID == "" {
		tenantID = "common"
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", cred.RefreshToken)
	form.Set("client_id", cred.ClientID)
	form.Set("client_secret", cred.ClientSecret)
	form.Set("scope", "https://graph.microsoft.com/.default offline_access")
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf(n.tokenURLPattern, tenantID), strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := n.client.Do(request)
	if err != nil {
		return fmt.Errorf("token refresh HTTP request: %w", err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("token refresh failed HTTP %d: %s", response.StatusCode, string(data))
	}
	var token TokenResponse
	if err := json.Unmarshal(data, &token); err != nil {
		return fmt.Errorf("decode token response: %w", err)
	}
	cred.AccessToken = token.AccessToken
	if token.RefreshToken != "" {
		cred.RefreshToken = token.RefreshToken
	}
	if token.ExpiresIn > 0 {
		cred.ExpiresAt = time.Now().Unix() + int64(token.ExpiresIn)
	}
	return nil
}
