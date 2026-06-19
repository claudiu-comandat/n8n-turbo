package secrets

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrSecretNotFound = errors.New("secret not found")
var ErrProviderNotConfigured = errors.New("provider not configured")
var ErrNotFound = ErrSecretNotFound

type Provider interface {
	Name() string
	GetSecret(ctx context.Context, name string) (string, error)
	GetAllSecrets(ctx context.Context) (map[string]string, error)
	Refresh(ctx context.Context) error
	IsConnected(ctx context.Context) (bool, error)
	GetMetadata() ProviderMetadata
}

type ProviderMetadata struct {
	Name         string    `json:"name"`
	Type         string    `json:"type"`
	Connected    bool      `json:"connected"`
	LastRefresh  time.Time `json:"lastRefresh,omitempty"`
	SecretsCount int       `json:"secretsCount"`
	Error        string    `json:"error,omitempty"`
}

type Manager struct {
	mu        sync.RWMutex
	providers map[string]Provider
	cancel    context.CancelFunc
}

func NewManager(providers ...Provider) *Manager {
	manager := &Manager{providers: map[string]Provider{}}
	for _, provider := range providers {
		manager.Register(provider)
	}
	return manager
}

func (m *Manager) Register(provider Provider) {
	if provider == nil || provider.Name() == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.providers[provider.Name()] = provider
}

func (m *Manager) Resolve(ctx context.Context) (map[string]map[string]string, error) {
	providers := m.snapshot()
	result := make(map[string]map[string]string, len(providers))
	for _, provider := range providers {
		values, err := provider.GetAllSecrets(ctx)
		if err != nil {
			return nil, err
		}
		result[provider.Name()] = values
	}
	return result, nil
}

func (m *Manager) GetSecret(ctx context.Context, providerName string, name string) (string, error) {
	m.mu.RLock()
	provider := m.providers[providerName]
	m.mu.RUnlock()
	if provider == nil {
		return "", ErrSecretNotFound
	}
	return provider.GetSecret(ctx, name)
}

func (m *Manager) Refresh(ctx context.Context) error {
	providers := m.snapshot()
	var failures []string
	for _, provider := range providers {
		if err := provider.Refresh(ctx); err != nil {
			failures = append(failures, provider.Name()+": "+err.Error())
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	return nil
}

func (m *Manager) StartRefresh(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	m.StopRefresh()
	runCtx, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	m.cancel = cancel
	m.mu.Unlock()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		_ = m.Refresh(runCtx)
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				_ = m.Refresh(runCtx)
			}
		}
	}()
}

func (m *Manager) StopRefresh() {
	m.mu.Lock()
	cancel := m.cancel
	m.cancel = nil
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (m *Manager) Metadata(ctx context.Context) []ProviderMetadata {
	providers := m.snapshot()
	result := make([]ProviderMetadata, 0, len(providers))
	for _, provider := range providers {
		result = append(result, provider.GetMetadata())
	}
	sort.Slice(result, func(i int, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

func (m *Manager) snapshot() []Provider {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]Provider, 0, len(m.providers))
	for _, provider := range m.providers {
		result = append(result, provider)
	}
	return result
}

type providerState struct {
	mu          sync.RWMutex
	cache       map[string]string
	lastRefresh time.Time
	lastError   error
}

func (s *providerState) set(cache map[string]string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		s.cache = cloneSecrets(cache)
		s.lastRefresh = time.Now().UTC()
	}
	s.lastError = err
}

func (s *providerState) get(name string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cache == nil {
		return "", ErrProviderNotConfigured
	}
	value, ok := s.cache[name]
	if !ok {
		return "", ErrSecretNotFound
	}
	return value, nil
}

func (s *providerState) all() (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cache == nil {
		return nil, ErrProviderNotConfigured
	}
	return cloneSecrets(s.cache), nil
}

func (s *providerState) metadata(name string, providerType string) ProviderMetadata {
	s.mu.RLock()
	defer s.mu.RUnlock()
	meta := ProviderMetadata{
		Name:         name,
		Type:         providerType,
		Connected:    s.lastError == nil && s.cache != nil,
		LastRefresh:  s.lastRefresh,
		SecretsCount: len(s.cache),
	}
	if s.lastError != nil {
		meta.Error = s.lastError.Error()
	}
	return meta
}

type EnvProvider struct {
	prefix string
	state  providerState
}

func NewEnvProvider(prefix string) *EnvProvider {
	if prefix == "" {
		prefix = "N8N_EXTERNAL_SECRETS_"
	}
	return &EnvProvider{prefix: prefix}
}

func (p *EnvProvider) Name() string {
	return "env"
}

func (p *EnvProvider) GetSecret(ctx context.Context, name string) (string, error) {
	if _, err := p.state.all(); err != nil {
		if err := p.Refresh(ctx); err != nil {
			return "", err
		}
	}
	value, err := p.state.get(name)
	if err == nil {
		return value, nil
	}
	return p.state.get(strings.ToUpper(name))
}

func (p *EnvProvider) GetAllSecrets(ctx context.Context) (map[string]string, error) {
	values, err := p.state.all()
	if err == nil {
		return values, nil
	}
	if err := p.Refresh(ctx); err != nil {
		return nil, err
	}
	return p.state.all()
}

func (p *EnvProvider) Refresh(ctx context.Context) error {
	select {
	case <-ctx.Done():
		p.state.set(nil, ctx.Err())
		return ctx.Err()
	default:
	}
	values := map[string]string{}
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || !strings.HasPrefix(key, p.prefix) {
			continue
		}
		name := strings.TrimPrefix(key, p.prefix)
		if name != "" {
			values[name] = value
		}
	}
	p.state.set(values, nil)
	return nil
}

func (p *EnvProvider) IsConnected(ctx context.Context) (bool, error) {
	return true, nil
}

func (p *EnvProvider) GetMetadata() ProviderMetadata {
	return p.state.metadata(p.Name(), "env")
}

func (p *EnvProvider) Metadata(ctx context.Context) ProviderMetadata {
	return p.GetMetadata()
}

type VaultConfig struct {
	Address    string
	Token      string
	Namespace  string
	MountPath  string
	SecretPath string
	Engine     string
}

type VaultProvider struct {
	config VaultConfig
	client *http.Client
	state  providerState
}

func NewVaultProvider(cfg VaultConfig) *VaultProvider {
	return &VaultProvider{config: cfg, client: &http.Client{Timeout: 10 * time.Second}}
}

func (p *VaultProvider) Name() string {
	return "vault"
}

func (p *VaultProvider) GetSecret(ctx context.Context, name string) (string, error) {
	return cachedSecret(ctx, p, &p.state, name)
}

func (p *VaultProvider) GetAllSecrets(ctx context.Context) (map[string]string, error) {
	return cachedSecrets(ctx, p, &p.state)
}

func (p *VaultProvider) Refresh(ctx context.Context) error {
	values, err := p.fetch(ctx)
	p.state.set(values, err)
	return err
}

func (p *VaultProvider) IsConnected(ctx context.Context) (bool, error) {
	if p.config.Address == "" || p.config.Token == "" {
		return false, ErrProviderNotConfigured
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(p.config.Address, "/")+"/v1/sys/health", nil)
	if err != nil {
		return false, err
	}
	request.Header.Set("X-Vault-Token", p.config.Token)
	response, err := p.client.Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	return response.StatusCode >= 200 && response.StatusCode < 500, nil
}

func (p *VaultProvider) GetMetadata() ProviderMetadata {
	return p.state.metadata(p.Name(), "hashicorpVault")
}

func (p *VaultProvider) Metadata(ctx context.Context) ProviderMetadata {
	return p.GetMetadata()
}

func (p *VaultProvider) fetch(ctx context.Context) (map[string]string, error) {
	if p.config.Address == "" || p.config.Token == "" {
		return nil, ErrProviderNotConfigured
	}
	mount := firstNonEmpty(p.config.MountPath, "secret")
	address := strings.TrimRight(p.config.Address, "/")
	path := strings.Trim(p.config.SecretPath, "/")
	url := address + "/v1/" + strings.Trim(mount, "/") + "/" + path
	if p.config.Engine != "kv-v1" {
		url = address + "/v1/" + strings.Trim(mount, "/") + "/data/" + path
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("X-Vault-Token", p.config.Token)
	if p.config.Namespace != "" {
		request.Header.Set("X-Vault-Namespace", p.config.Namespace)
	}
	response, err := p.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return map[string]string{}, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(response.Body)
		return nil, fmt.Errorf("vault status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}
	data := mapFrom(payload["data"])
	if p.config.Engine != "kv-v1" {
		data = mapFrom(data["data"])
	}
	return stringifyMap(data), nil
}

type AWSConfig struct {
	Endpoint        string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	SecretPrefix    string
}

type AWSProvider struct {
	config AWSConfig
	client *http.Client
	state  providerState
}

func NewAWSProvider(cfg AWSConfig) *AWSProvider {
	return &AWSProvider{config: cfg, client: &http.Client{Timeout: 10 * time.Second}}
}

func (p *AWSProvider) Name() string {
	return "aws"
}

func (p *AWSProvider) GetSecret(ctx context.Context, name string) (string, error) {
	return cachedSecret(ctx, p, &p.state, name)
}

func (p *AWSProvider) GetAllSecrets(ctx context.Context) (map[string]string, error) {
	return cachedSecrets(ctx, p, &p.state)
}

func (p *AWSProvider) Refresh(ctx context.Context) error {
	values, err := p.fetch(ctx)
	p.state.set(values, err)
	return err
}

func (p *AWSProvider) IsConnected(ctx context.Context) (bool, error) {
	return p.config.Endpoint != "", nil
}

func (p *AWSProvider) GetMetadata() ProviderMetadata {
	return p.state.metadata(p.Name(), "awsSecretsManager")
}

func (p *AWSProvider) Metadata(ctx context.Context) ProviderMetadata {
	return p.GetMetadata()
}

func (p *AWSProvider) fetch(ctx context.Context) (map[string]string, error) {
	if p.config.Endpoint == "" {
		return nil, ErrProviderNotConfigured
	}
	listPayload, err := p.awsRequest(ctx, "secretsmanager.ListSecrets", map[string]any{"MaxResults": 100})
	if err != nil {
		return nil, err
	}
	secrets, _ := listPayload["SecretList"].([]any)
	result := map[string]string{}
	for _, item := range secrets {
		meta := mapFrom(item)
		name, _ := meta["Name"].(string)
		if name == "" || (p.config.SecretPrefix != "" && !strings.HasPrefix(name, p.config.SecretPrefix)) {
			continue
		}
		valuePayload, err := p.awsRequest(ctx, "secretsmanager.GetSecretValue", map[string]any{"SecretId": name})
		if err != nil {
			return nil, err
		}
		value, _ := valuePayload["SecretString"].(string)
		key := strings.TrimPrefix(name, p.config.SecretPrefix)
		result[key] = value
	}
	return result, nil
}

func (p *AWSProvider) awsRequest(ctx context.Context, target string, body map[string]any) (map[string]any, error) {
	data, _ := json.Marshal(body)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.config.Endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/x-amz-json-1.1")
	request.Header.Set("X-Amz-Target", target)
	response, err := p.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(response.Body)
		return nil, fmt.Errorf("aws status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload, nil
}

type InfisicalConfig struct {
	Address      string
	Token        string
	WorkspaceID  string
	Environment  string
	SecretPath   string
	ProviderName string
}

type InfisicalProvider struct {
	config InfisicalConfig
	client *http.Client
	state  providerState
}

func NewInfisicalProvider(cfg InfisicalConfig) *InfisicalProvider {
	if cfg.ProviderName == "" {
		cfg.ProviderName = "infisical"
	}
	return &InfisicalProvider{config: cfg, client: &http.Client{Timeout: 10 * time.Second}}
}

func (p *InfisicalProvider) Name() string {
	return p.config.ProviderName
}

func (p *InfisicalProvider) GetSecret(ctx context.Context, name string) (string, error) {
	return cachedSecret(ctx, p, &p.state, name)
}

func (p *InfisicalProvider) GetAllSecrets(ctx context.Context) (map[string]string, error) {
	return cachedSecrets(ctx, p, &p.state)
}

func (p *InfisicalProvider) Refresh(ctx context.Context) error {
	values, err := p.fetch(ctx)
	p.state.set(values, err)
	return err
}

func (p *InfisicalProvider) IsConnected(ctx context.Context) (bool, error) {
	return p.config.Address != "" && p.config.Token != "", nil
}

func (p *InfisicalProvider) GetMetadata() ProviderMetadata {
	return p.state.metadata(p.Name(), "infisical")
}

func (p *InfisicalProvider) Metadata(ctx context.Context) ProviderMetadata {
	return p.GetMetadata()
}

func (p *InfisicalProvider) fetch(ctx context.Context) (map[string]string, error) {
	if p.config.Address == "" || p.config.Token == "" {
		return nil, ErrProviderNotConfigured
	}
	url := strings.TrimRight(p.config.Address, "/") + "/api/v3/secrets/raw"
	query := []string{}
	if p.config.WorkspaceID != "" {
		query = append(query, "workspaceId="+p.config.WorkspaceID)
	}
	if p.config.Environment != "" {
		query = append(query, "environment="+p.config.Environment)
	}
	if p.config.SecretPath != "" {
		query = append(query, "secretPath="+p.config.SecretPath)
	}
	if len(query) > 0 {
		url += "?" + strings.Join(query, "&")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+p.config.Token)
	response, err := p.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(response.Body)
		return nil, fmt.Errorf("infisical status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}
	secrets, _ := payload["secrets"].([]any)
	result := map[string]string{}
	for _, item := range secrets {
		entry := mapFrom(item)
		key, _ := entry["secretKey"].(string)
		value, _ := entry["secretValue"].(string)
		if key != "" {
			result[key] = value
		}
	}
	return result, nil
}

func cachedSecret(ctx context.Context, provider Provider, state *providerState, name string) (string, error) {
	if _, err := state.all(); err != nil {
		if err := provider.Refresh(ctx); err != nil {
			return "", err
		}
	}
	return state.get(name)
}

func cachedSecrets(ctx context.Context, provider Provider, state *providerState) (map[string]string, error) {
	values, err := state.all()
	if err == nil {
		return values, nil
	}
	if err := provider.Refresh(ctx); err != nil {
		return nil, err
	}
	return state.all()
}

func cloneSecrets(values map[string]string) map[string]string {
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func mapFrom(value any) map[string]any {
	result, _ := value.(map[string]any)
	if result == nil {
		return map[string]any{}
	}
	return result
}

func stringifyMap(values map[string]any) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		switch typed := value.(type) {
		case string:
			result[key] = typed
		default:
			result[key] = fmt.Sprint(typed)
		}
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
