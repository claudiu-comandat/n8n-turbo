package oidc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Config struct {
	ClientID       string   `json:"clientId"`
	ClientSecret   string   `json:"clientSecret"`
	DiscoveryURL   string   `json:"discoveryUrl"`
	RedirectURL    string   `json:"redirectUrl"`
	Scopes         []string `json:"scopes"`
	EmailClaim     string   `json:"emailClaim"`
	FirstNameClaim string   `json:"firstNameClaim"`
	LastNameClaim  string   `json:"lastNameClaim"`
}

type User struct {
	Subject   string `json:"subject"`
	Email     string `json:"email"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
}

type Provider struct {
	config    Config
	client    *http.Client
	discovery discoveryDocument
	mu        sync.Mutex
	states    map[string]time.Time
}

type discoveryDocument struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserInfoEndpoint      string `json:"userinfo_endpoint"`
	Issuer                string `json:"issuer"`
}

func NewProvider(ctx context.Context, cfg Config) (*Provider, error) {
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{"openid", "email", "profile"}
	}
	provider := &Provider{config: cfg, client: &http.Client{Timeout: 10 * time.Second}, states: map[string]time.Time{}}
	if err := provider.discover(ctx); err != nil {
		return nil, err
	}
	return provider, nil
}

func (p *Provider) AuthURL(state string) string {
	now := time.Now().UTC()
	p.mu.Lock()
	for key, expiry := range p.states {
		if now.After(expiry) {
			delete(p.states, key)
		}
	}
	p.states[state] = now.Add(10 * time.Minute)
	p.mu.Unlock()
	values := url.Values{}
	values.Set("client_id", p.config.ClientID)
	values.Set("redirect_uri", p.config.RedirectURL)
	values.Set("response_type", "code")
	values.Set("scope", strings.Join(p.config.Scopes, " "))
	values.Set("state", state)
	return p.discovery.AuthorizationEndpoint + "?" + values.Encode()
}

func (p *Provider) HandleCallback(ctx context.Context, state string, code string) (*User, error) {
	if err := p.consumeState(state); err != nil {
		return nil, err
	}
	token, err := p.exchange(ctx, code)
	if err != nil {
		return nil, err
	}
	claims := map[string]any{}
	if idToken, _ := token["id_token"].(string); idToken != "" {
		parsed, err := parseJWTClaims(idToken)
		if err == nil {
			for key, value := range parsed {
				claims[key] = value
			}
		}
	}
	if accessToken, _ := token["access_token"].(string); accessToken != "" && p.discovery.UserInfoEndpoint != "" {
		userInfo, err := p.userInfo(ctx, accessToken)
		if err == nil {
			for key, value := range userInfo {
				claims[key] = value
			}
		}
	}
	user := &User{
		Subject:   stringClaim(claims, "sub"),
		Email:     stringClaim(claims, firstNonEmpty(p.config.EmailClaim, "email")),
		FirstName: stringClaim(claims, firstNonEmpty(p.config.FirstNameClaim, "given_name")),
		LastName:  stringClaim(claims, firstNonEmpty(p.config.LastNameClaim, "family_name")),
	}
	if user.Email == "" {
		return nil, fmt.Errorf("oidc email is missing")
	}
	return user, nil
}

func (p *Provider) discover(ctx context.Context) error {
	base := strings.TrimRight(p.config.DiscoveryURL, "/")
	url := base
	if !strings.HasSuffix(url, "/.well-known/openid-configuration") {
		url += "/.well-known/openid-configuration"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := p.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("oidc discovery status %d", response.StatusCode)
	}
	return json.NewDecoder(response.Body).Decode(&p.discovery)
}

func (p *Provider) exchange(ctx context.Context, code string) (map[string]any, error) {
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("code", code)
	values.Set("redirect_uri", p.config.RedirectURL)
	values.Set("client_id", p.config.ClientID)
	if p.config.ClientSecret != "" {
		values.Set("client_secret", p.config.ClientSecret)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.discovery.TokenEndpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := p.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(response.Body)
		return nil, fmt.Errorf("oidc token status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	var token map[string]any
	if err := json.NewDecoder(response.Body).Decode(&token); err != nil {
		return nil, err
	}
	return token, nil
}

func (p *Provider) userInfo(ctx context.Context, accessToken string) (map[string]any, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.discovery.UserInfoEndpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	response, err := p.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("oidc userinfo status %d", response.StatusCode)
	}
	var claims map[string]any
	if err := json.NewDecoder(response.Body).Decode(&claims); err != nil {
		return nil, err
	}
	return claims, nil
}

func (p *Provider) consumeState(state string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	expiry, ok := p.states[state]
	delete(p.states, state)
	if !ok || time.Now().UTC().After(expiry) {
		return fmt.Errorf("oidc state is invalid or expired")
	}
	return nil
}

func parseJWTClaims(token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid jwt")
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var claims map[string]any
	if err := json.Unmarshal(data, &claims); err != nil {
		return nil, err
	}
	return claims, nil
}

func stringClaim(claims map[string]any, key string) string {
	value, _ := claims[key].(string)
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
