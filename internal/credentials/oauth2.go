package credentials

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type OAuth2Credential struct {
	GrantType           string `json:"grantType"`
	ClientID            string `json:"clientId"`
	ClientSecret        string `json:"clientSecret"`
	AuthURL             string `json:"authUrl"`
	AuthorizationURL    string `json:"authorizationUrl"`
	AccessTokenURL      string `json:"accessTokenUrl"`
	Scope               string `json:"scope"`
	Authentication      string `json:"authentication"`
	AuthQueryParameters string `json:"authQueryParameters"`
	IgnoreSSLIssues     bool   `json:"ignoreSSLIssues"`
	PKCE                bool   `json:"pkce"`
	CodeVerifier        string `json:"codeVerifier"`
	AccessToken         string `json:"accessToken"`
	RefreshToken        string `json:"refreshToken"`
	TokenType           string `json:"tokenType"`
	ExpiresIn           int    `json:"expiresIn"`
	ExpiresAt           string `json:"expiresAt"`
	OAuthTokenData      any    `json:"oauthTokenData"`
}

type OAuth2TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
	Scope        string `json:"scope,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
}

func (c OAuth2Credential) AuthorizationEndpoint() string {
	if c.AuthorizationURL != "" {
		return c.AuthorizationURL
	}
	return c.AuthURL
}

func (c OAuth2Credential) TokenEndpoint() string {
	return c.AccessTokenURL
}

func (c OAuth2Credential) BuildAuthURL(redirectURI string, state string) (string, error) {
	endpoint := c.AuthorizationEndpoint()
	if endpoint == "" {
		return "", fmt.Errorf("authorization URL is required")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	query.Set("response_type", "code")
	query.Set("client_id", c.ClientID)
	query.Set("redirect_uri", redirectURI)
	if c.Scope != "" {
		query.Set("scope", c.Scope)
	}
	if state != "" {
		query.Set("state", state)
	}
	if c.PKCE && c.CodeVerifier != "" {
		query.Set("code_challenge", OAuth2PKCEChallenge(c.CodeVerifier))
		query.Set("code_challenge_method", "S256")
	}
	if c.AuthQueryParameters != "" {
		extra, _ := url.ParseQuery(c.AuthQueryParameters)
		for key, values := range extra {
			for _, value := range values {
				query.Add(key, value)
			}
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func (c OAuth2Credential) ExchangeCode(ctx context.Context, client *http.Client, code string, redirectURI string) (*OAuth2TokenResponse, error) {
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("code", code)
	values.Set("redirect_uri", redirectURI)
	if c.PKCE && c.CodeVerifier != "" {
		values.Set("code_verifier", c.CodeVerifier)
	}
	return c.requestToken(ctx, client, values)
}

func (c OAuth2Credential) Refresh(ctx context.Context, client *http.Client, refreshToken string) (*OAuth2TokenResponse, error) {
	values := url.Values{}
	values.Set("grant_type", "refresh_token")
	values.Set("refresh_token", refreshToken)
	if c.Scope != "" {
		values.Set("scope", c.Scope)
	}
	return c.requestToken(ctx, client, values)
}

func (c OAuth2Credential) ClientCredentials(ctx context.Context, client *http.Client) (*OAuth2TokenResponse, error) {
	values := url.Values{}
	values.Set("grant_type", "client_credentials")
	if c.Scope != "" {
		values.Set("scope", c.Scope)
	}
	return c.requestToken(ctx, client, values)
}

func (c OAuth2Credential) requestToken(ctx context.Context, client *http.Client, values url.Values) (*OAuth2TokenResponse, error) {
	endpoint := c.TokenEndpoint()
	if endpoint == "" {
		return nil, fmt.Errorf("access token URL is required")
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	if strings.EqualFold(c.Authentication, "body") {
		values.Set("client_id", c.ClientID)
		values.Set("client_secret", c.ClientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if !strings.EqualFold(c.Authentication, "body") {
		req.SetBasicAuth(c.ClientID, c.ClientSecret)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
		return nil, fmt.Errorf("oauth2 token endpoint returned %d: %s", resp.StatusCode, string(body))
	}
	var token OAuth2TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, err
	}
	if token.AccessToken == "" {
		return nil, fmt.Errorf("oauth2 token response missing access_token")
	}
	if token.TokenType == "" {
		token.TokenType = "Bearer"
	}
	return &token, nil
}

func GeneratePKCEChallenge() (string, string, error) {
	bytes := make([]byte, 64)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", err
	}
	verifier := base64.RawURLEncoding.EncodeToString(bytes)
	return verifier, OAuth2PKCEChallenge(verifier), nil
}

func OAuth2PKCEChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func InjectOAuth2Token(req *http.Request, token string, tokenType string, placement string) {
	if tokenType == "" {
		tokenType = "Bearer"
	}
	if strings.EqualFold(placement, "query") {
		query := req.URL.Query()
		query.Set("access_token", token)
		req.URL.RawQuery = query.Encode()
		return
	}
	req.Header.Set("Authorization", strings.TrimSpace(tokenType+" "+token))
}

func OAuth2ExpiresAt(expiresIn int) time.Time {
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	return time.Now().UTC().Add(time.Duration(expiresIn) * time.Second)
}

func OAuth2TokenExpired(expiresAt string) bool {
	if expiresAt == "" {
		return true
	}
	parsed, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		return true
	}
	return time.Now().UTC().Add(60 * time.Second).After(parsed)
}
