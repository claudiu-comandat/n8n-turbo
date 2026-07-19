package descriptor

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type AuthInjector struct {
	tokenCache map[string]cachedToken
	cacheMu    sync.RWMutex
	httpClient *http.Client
}

type cachedToken struct {
	AccessToken  string
	ExpiresAt    time.Time
	RefreshToken string
}

func NewAuthInjector() *AuthInjector {
	return &AuthInjector{
		tokenCache: make(map[string]cachedToken),
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (a *AuthInjector) Inject(req *http.Request, credential map[string]any, authType string, authConfig map[string]string) error {
	if authConfig == nil {
		authConfig = map[string]string{}
	}
	switch authType {
	case "", "noAuth":
		return nil
	case "bearer":
		return a.injectBearer(req, credential, authConfig)
	case "apiKey":
		return a.injectAPIKey(req, credential, authConfig)
	case "basic":
		return a.injectBasic(req, credential, authConfig)
	case "headerAuth":
		return a.injectHeaderAuth(req, credential, authConfig)
	case "oauth2":
		return a.injectOAuth2(req, credential, authConfig)
	case "query":
		return a.injectQuery(req, credential, authConfig)
	case "digestAuth":
		return a.injectDigest(req, credential, authConfig)
	default:
		return fmt.Errorf("unknown auth type %s", authType)
	}
}

func (a *AuthInjector) injectBearer(req *http.Request, credential map[string]any, authConfig map[string]string) error {
	tokenField := firstConfig(authConfig, "tokenField", "accessToken")
	token := credentialString(credential, tokenField, "accessToken", "token", "apiKey")
	if token == "" {
		return fmt.Errorf("missing credential field %s", tokenField)
	}
	prefix := firstConfig(authConfig, "tokenPrefix", "Bearer")
	if prefix == "" {
		req.Header.Set("Authorization", token)
	} else {
		req.Header.Set("Authorization", prefix+" "+token)
	}
	if organizationID := credentialString(credential, "organizationId", "organization"); organizationID != "" && req.Header.Get("OpenAI-Organization") == "" {
		req.Header.Set("OpenAI-Organization", organizationID)
	}
	return nil
}

func (a *AuthInjector) injectAPIKey(req *http.Request, credential map[string]any, authConfig map[string]string) error {
	keyField := firstConfig(authConfig, "keyField", "apiKey")
	apiKey := credentialString(credential, keyField, "apiKey", "token", "accessToken")
	if apiKey == "" {
		return fmt.Errorf("missing credential field %s", keyField)
	}
	keyName := firstConfig(authConfig, "keyName", "X-API-Key")
	value := authConfig["keyPrefix"] + apiKey
	switch firstConfig(authConfig, "keyIn", "header") {
	case "header":
		req.Header.Set(keyName, value)
	case "query":
		query := req.URL.Query()
		query.Set(keyName, value)
		req.URL.RawQuery = query.Encode()
	default:
		return fmt.Errorf("invalid apiKey location %s", authConfig["keyIn"])
	}
	return nil
}

func (a *AuthInjector) injectBasic(req *http.Request, credential map[string]any, authConfig map[string]string) error {
	userField := firstConfig(authConfig, "usernameField", "user")
	passwordField := firstConfig(authConfig, "passwordField", "password")
	username := credentialString(credential, userField, "user", "username", "email")
	password := credentialString(credential, passwordField, "password", "apiToken", "token", "authToken")
	if username == "" {
		return fmt.Errorf("missing credential field %s", userField)
	}
	if password == "" {
		return fmt.Errorf("missing credential field %s", passwordField)
	}
	req.SetBasicAuth(username, password)
	return nil
}

func (a *AuthInjector) injectHeaderAuth(req *http.Request, credential map[string]any, authConfig map[string]string) error {
	headerName := authConfig["headerName"]
	if headerName == "" {
		headerName = credentialString(credential, "name", "headerName")
	}
	if headerName == "" {
		return fmt.Errorf("missing auth header name")
	}
	valueField := firstConfig(authConfig, "valueField", "value")
	value := credentialString(credential, valueField, "value", "headerValue", "token", "apiKey")
	if value == "" {
		return fmt.Errorf("missing credential field %s", valueField)
	}
	req.Header.Set(headerName, authConfig["valuePrefix"]+value)
	return nil
}

func (a *AuthInjector) injectOAuth2(req *http.Request, credential map[string]any, authConfig map[string]string) error {
	data, ok := extractOAuthData(credential, authConfig)
	if !ok {
		return fmt.Errorf("oauth2 credential data missing")
	}
	cacheKey := data.cacheKey(authConfig)
	token, err := a.validOAuthToken(req.Context(), data, authConfig, cacheKey)
	if err != nil {
		return err
	}
	injectOAuthToken(req, token, firstConfig(authConfig, "tokenPrefix", "Bearer"), firstConfig(authConfig, "tokenPlacement", "header"))
	return nil
}

func (a *AuthInjector) injectDigest(req *http.Request, credential map[string]any, authConfig map[string]string) error {
	username := credentialString(credential, firstConfig(authConfig, "usernameField", "user"), "user", "username")
	password := credentialString(credential, firstConfig(authConfig, "passwordField", "password"), "password")
	realm := authConfig["realm"]
	nonce := authConfig["nonce"]
	if realm == "" || nonce == "" {
		challenge, err := parseDigestChallenge(firstNonEmptyAuth(authConfig["wwwAuthenticate"], authConfig["challenge"]))
		if err == nil {
			realm = challenge.Realm
			nonce = challenge.Nonce
			if authConfig["qop"] == "" {
				authConfig["qop"] = challenge.QOP
			}
			if authConfig["opaque"] == "" {
				authConfig["opaque"] = challenge.Opaque
			}
		}
	}
	if username == "" || password == "" || realm == "" || nonce == "" {
		return fmt.Errorf("digestAuth requires username, password, realm and nonce")
	}
	uri := req.URL.RequestURI()
	if uri == "" {
		uri = "/"
	}
	qop := firstConfig(authConfig, "qop", "auth")
	nc := firstConfig(authConfig, "nc", "00000001")
	cnonce := firstConfig(authConfig, "cnonce", "n8nturbo")
	ha1 := md5Hex(username + ":" + realm + ":" + password)
	ha2 := md5Hex(req.Method + ":" + uri)
	response := md5Hex(ha1 + ":" + nonce + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2)
	if qop == "" {
		response = md5Hex(ha1 + ":" + nonce + ":" + ha2)
	}
	parts := []string{
		fmt.Sprintf(`username="%s"`, quoteDigest(username)),
		fmt.Sprintf(`realm="%s"`, quoteDigest(realm)),
		fmt.Sprintf(`nonce="%s"`, quoteDigest(nonce)),
		fmt.Sprintf(`uri="%s"`, quoteDigest(uri)),
		fmt.Sprintf(`response="%s"`, response),
	}
	if qop != "" {
		parts = append(parts, "qop="+qop, "nc="+nc, fmt.Sprintf(`cnonce="%s"`, quoteDigest(cnonce)))
	}
	if opaque := authConfig["opaque"]; opaque != "" {
		parts = append(parts, fmt.Sprintf(`opaque="%s"`, quoteDigest(opaque)))
	}
	req.Header.Set("Authorization", "Digest "+strings.Join(parts, ", "))
	return nil
}

func (a *AuthInjector) injectQuery(req *http.Request, credential map[string]any, authConfig map[string]string) error {
	keyField := firstConfig(authConfig, "keyField", "apiKey")
	tokenField := firstConfig(authConfig, "tokenField", "token")
	keyName := firstConfig(authConfig, "keyName", "key")
	tokenName := firstConfig(authConfig, "tokenName", "token")
	query := req.URL.Query()
	if key := credentialString(credential, keyField, "apiKey", "key"); key != "" {
		query.Set(keyName, key)
	}
	if token := credentialString(credential, tokenField, "token", "apiToken", "accessToken"); token != "" {
		query.Set(tokenName, token)
	}
	if query.Get(keyName) == "" && query.Get(tokenName) == "" {
		return fmt.Errorf("query auth requires key or token")
	}
	req.URL.RawQuery = query.Encode()
	return nil
}

type OAuthData struct {
	TokenURL     string
	ClientID     string
	ClientSecret string
	Scope        string
	AccessToken  string
	RefreshToken string
	TokenType    string
	ExpiresAt    time.Time
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	ExpiresAt    string `json:"expires_at"`
	ExpiryDate   any    `json:"expiry_date"`
}

func (a *AuthInjector) validOAuthToken(ctx context.Context, data OAuthData, authConfig map[string]string, cacheKey string) (string, error) {
	a.cacheMu.RLock()
	cached, ok := a.tokenCache[cacheKey]
	a.cacheMu.RUnlock()
	if ok && time.Now().Add(60*time.Second).Before(cached.ExpiresAt) {
		return cached.AccessToken, nil
	}
	if data.AccessToken != "" && (data.ExpiresAt.IsZero() || time.Now().Add(60*time.Second).Before(data.ExpiresAt)) {
		return data.AccessToken, nil
	}
	grantType := firstConfig(authConfig, "grantType", "refreshToken")
	next, err := a.requestOAuthToken(ctx, data, authConfig, grantType)
	if err != nil {
		if ok && cached.AccessToken != "" {
			return cached.AccessToken, nil
		}
		if data.AccessToken != "" {
			return data.AccessToken, nil
		}
		return "", err
	}
	a.cacheMu.Lock()
	a.tokenCache[cacheKey] = next
	a.cacheMu.Unlock()
	return next.AccessToken, nil
}

func (a *AuthInjector) requestOAuthToken(ctx context.Context, data OAuthData, authConfig map[string]string, grantType string) (cachedToken, error) {
	tokenURL := firstNonEmptyAuth(authConfig["tokenURL"], data.TokenURL)
	if tokenURL == "" {
		return cachedToken{}, fmt.Errorf("oauth2 tokenURL missing")
	}
	values := url.Values{}
	switch grantType {
	case "clientCredentials", "client_credentials":
		values.Set("grant_type", "client_credentials")
	case "refreshToken", "refresh_token":
		if data.RefreshToken == "" {
			return cachedToken{}, fmt.Errorf("oauth2 refresh token missing")
		}
		values.Set("grant_type", "refresh_token")
		values.Set("refresh_token", data.RefreshToken)
	default:
		return cachedToken{}, fmt.Errorf("unknown oauth2 grantType %s", grantType)
	}
	if data.ClientID != "" {
		values.Set("client_id", data.ClientID)
	}
	if data.ClientSecret != "" {
		values.Set("client_secret", data.ClientSecret)
	}
	if data.Scope != "" {
		values.Set("scope", data.Scope)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return cachedToken{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if strings.EqualFold(authConfig["authentication"], "basic") && data.ClientID != "" {
		req.SetBasicAuth(data.ClientID, data.ClientSecret)
	}
	return a.doOAuthTokenRequest(req, data.RefreshToken)
}

func (a *AuthInjector) doOAuthTokenRequest(req *http.Request, previousRefreshToken string) (cachedToken, error) {
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return cachedToken{}, fmt.Errorf("oauth2 token request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return cachedToken{}, fmt.Errorf("oauth2 token endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var decoded tokenResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return cachedToken{}, fmt.Errorf("oauth2 token response: %w", err)
	}
	if decoded.AccessToken == "" {
		return cachedToken{}, fmt.Errorf("oauth2 token response missing access_token")
	}
	expiresAt := oauthExpiry(decoded.ExpiresIn, decoded.ExpiresAt, decoded.ExpiryDate)
	if expiresAt.IsZero() {
		expiresAt = time.Now().Add(time.Hour)
	}
	refreshToken := decoded.RefreshToken
	if refreshToken == "" {
		refreshToken = previousRefreshToken
	}
	return cachedToken{AccessToken: decoded.AccessToken, RefreshToken: refreshToken, ExpiresAt: expiresAt}, nil
}

func extractOAuthData(credential map[string]any, authConfig map[string]string) (OAuthData, bool) {
	source := credential
	if nested, ok := credential["oauthTokenData"].(map[string]any); ok {
		source = mergeAuthMaps(credential, nested)
	}
	data := OAuthData{
		TokenURL:     firstNonEmptyAuth(authConfig["tokenURL"], credentialString(source, "tokenURL", "accessTokenURL", "token_uri")),
		ClientID:     credentialString(source, "clientID", "clientId", "client_id"),
		ClientSecret: credentialString(source, "clientSecret", "client_secret"),
		Scope:        credentialString(source, "scope"),
		AccessToken:  credentialString(source, firstConfig(authConfig, "tokenField", "accessToken"), "accessToken", "access_token", "token"),
		RefreshToken: credentialString(source, firstConfig(authConfig, "refreshTokenField", "refreshToken"), "refreshToken", "refresh_token"),
		TokenType:    credentialString(source, "tokenType", "token_type"),
	}
	data.ExpiresAt = oauthTimeValue(source[firstConfig(authConfig, "expiresAtField", "expiresAt")])
	if data.ExpiresAt.IsZero() {
		data.ExpiresAt = oauthTimeValue(source["expiry_date"])
	}
	if data.ExpiresAt.IsZero() {
		data.ExpiresAt = oauthTimeValue(source["expires"])
	}
	return data, data.AccessToken != "" || data.RefreshToken != "" || (data.ClientID != "" && data.ClientSecret != "")
}

func mergeAuthMaps(base map[string]any, nested map[string]any) map[string]any {
	merged := make(map[string]any, len(base)+len(nested))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range nested {
		merged[key] = value
	}
	return merged
}

func injectOAuthToken(req *http.Request, token string, prefix string, placement string) {
	if strings.EqualFold(placement, "query") {
		query := req.URL.Query()
		query.Set("access_token", token)
		req.URL.RawQuery = query.Encode()
		return
	}
	if prefix == "" {
		req.Header.Set("Authorization", token)
		return
	}
	req.Header.Set("Authorization", prefix+" "+token)
}

func (d OAuthData) cacheKey(authConfig map[string]string) string {
	return strings.Join([]string{
		firstNonEmptyAuth(authConfig["tokenURL"], d.TokenURL),
		d.ClientID,
		d.Scope,
		d.RefreshToken,
		firstConfig(authConfig, "grantType", "refreshToken"),
	}, "|")
}

func oauthExpiry(expiresIn int64, expiresAt string, expiryDate any) time.Time {
	if expiresAt != "" {
		if parsed, err := time.Parse(time.RFC3339, expiresAt); err == nil {
			return parsed
		}
	}
	if parsed := oauthTimeValue(expiryDate); !parsed.IsZero() {
		return parsed
	}
	if expiresIn > 0 {
		return time.Now().Add(time.Duration(expiresIn) * time.Second)
	}
	return time.Time{}
}

func oauthTimeValue(value any) time.Time {
	switch typed := value.(type) {
	case time.Time:
		return typed
	case string:
		if parsed, err := time.Parse(time.RFC3339, typed); err == nil {
			return parsed
		}
		if number, err := strconvParseInt(typed); err == nil {
			return oauthUnixTime(number)
		}
	case int64:
		return oauthUnixTime(typed)
	case int:
		return oauthUnixTime(int64(typed))
	case float64:
		return oauthUnixTime(int64(typed))
	case json.Number:
		if number, err := typed.Int64(); err == nil {
			return oauthUnixTime(number)
		}
	}
	return time.Time{}
}

func oauthUnixTime(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	if value > 100000000000 {
		return time.UnixMilli(value)
	}
	return time.Unix(value, 0)
}

type DigestChallenge struct {
	Realm     string
	Nonce     string
	Opaque    string
	Algorithm string
	QOP       string
}

func parseDigestChallenge(header string) (DigestChallenge, error) {
	header = strings.TrimSpace(header)
	header = strings.TrimPrefix(header, "Digest ")
	if header == "" {
		return DigestChallenge{}, fmt.Errorf("digest challenge missing")
	}
	result := DigestChallenge{}
	for _, part := range strings.Split(header, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"`)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "realm":
			result.Realm = value
		case "nonce":
			result.Nonce = value
		case "opaque":
			result.Opaque = value
		case "algorithm":
			result.Algorithm = value
		case "qop":
			result.QOP = strings.TrimSpace(strings.Split(value, ",")[0])
		}
	}
	if result.Realm == "" || result.Nonce == "" {
		return DigestChallenge{}, fmt.Errorf("digest challenge requires realm and nonce")
	}
	return result, nil
}

func firstConfig(config map[string]string, key string, fallback string) string {
	if value := config[key]; value != "" {
		return value
	}
	return fallback
}

func tokenExpiry(credential map[string]any) (time.Time, bool) {
	for _, key := range []string{"expiresAt", "expiry", "expires"} {
		value := credentialString(credential, key)
		if value == "" {
			continue
		}
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func firstNonEmptyAuth(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func strconvParseInt(value string) (int64, error) {
	var result int64
	_, err := fmt.Sscan(strings.TrimSpace(value), &result)
	return result, err
}

func md5Hex(value string) string {
	sum := md5.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

func quoteDigest(value string) string {
	return strings.ReplaceAll(value, `"`, `\"`)
}

func credentialFromDescriptor(descriptor Descriptor, credentials map[string]map[string]any) map[string]any {
	if descriptor.CredentialType != "" {
		credential := credentialByType(credentials, descriptor.CredentialType)
		if credential != nil {
			return credential
		}
		if descriptor.CredentialType == "gmailOAuth2" {
			return credentialByType(credentials, "googleOAuth2Api")
		}
		if descriptor.CredentialType == "hubspotPrivateAppApi" {
			return credentialByType(credentials, "hubspotApi")
		}
	}
	if len(credentials) == 1 {
		for _, credential := range credentials {
			return credential
		}
	}
	return nil
}

func appendQuery(raw string, values url.Values) string {
	if len(values) == 0 {
		return raw
	}
	if raw == "" {
		return values.Encode()
	}
	return raw + "&" + values.Encode()
}
