package nodes

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	credpkg "github.com/n8n-io/n8n-turbo/internal/credentials"
	"golang.org/x/sync/singleflight"
)

type nodeOAuth2CachedToken struct {
	AccessToken string
	TokenType   string
	ExpiresAt   time.Time
}

var nodeOAuth2Cache = struct {
	sync.Mutex
	tokens map[string]nodeOAuth2CachedToken
}{tokens: map[string]nodeOAuth2CachedToken{}}

var nodeOAuth2Fetch singleflight.Group

func credentialByType(credentials map[string]map[string]any, names ...string) map[string]any {
	for _, name := range names {
		if credential, ok := credentials[name]; ok {
			return credential
		}
	}
	for _, credential := range credentials {
		credentialType := fmt.Sprint(credential["type"])
		for _, name := range names {
			if credentialType == name {
				return credential
			}
		}
	}
	return nil
}

func applyCredentialAuth(ctx context.Context, req *http.Request, credentials map[string]map[string]any) (bool, error) {
	if credential := credentialByType(credentials, "oAuth2Api", "googleOAuth2Api", "googleSheetsOAuth2Api", "microsoftTeamsOAuth2Api"); credential != nil {
		token := firstNonEmptyNode(credentialString(credential, "accessToken", "token"), oauth2TokenDataString(credential, "access_token"))
		tokenType := firstNonEmptyNode(credentialString(credential, "tokenType"), oauth2TokenDataString(credential, "token_type"))
		if token != "" {
			credpkg.InjectOAuth2Token(req, token, tokenType, credentialString(credential, "tokenPlacement", "authPlacement"))
			return true, nil
		}
		fetched, fetchedType, err := nodeOAuth2ClientCredentialsToken(ctx, credential)
		if err != nil {
			return false, err
		}
		if fetched != "" {
			credpkg.InjectOAuth2Token(req, fetched, fetchedType, credentialString(credential, "tokenPlacement", "authPlacement"))
			return true, nil
		}
	}
	if credential := credentialByType(credentials, "httpHeaderAuth"); credential != nil {
		name := credentialString(credential, "name", "headerName")
		value := credentialString(credential, "value", "headerValue")
		if name != "" {
			req.Header.Set(name, value)
			return true, nil
		}
	}
	if credential := credentialByType(credentials, "httpBearerAuth"); credential != nil {
		if token := credentialString(credential, "accessToken", "token", "value"); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
			return true, nil
		}
	}
	if credential := credentialByType(credentials, "httpQueryAuth"); credential != nil {
		if token := credentialString(credential, "accessToken", "token", "value"); token != "" {
			key := credentialString(credential, "name", "queryParameterName", "key")
			if key == "" {
				key = "access_token"
			}
			q := req.URL.Query()
			q.Set(key, token)
			req.URL.RawQuery = q.Encode()
			return true, nil
		}
	}
	if credential := credentialByType(credentials, "httpBasicAuth"); credential != nil {
		req.SetBasicAuth(credentialString(credential, "user", "username"), credentialString(credential, "password"))
		return true, nil
	}
	if credential := credentialByType(credentials, "githubApi", "slackApi", "notionApi", "openAiApi", "stripeApi", "telegramApi", "sendGridApi", "airtableApi", "hubspotApi"); credential != nil {
		if token := credentialString(credential, "accessToken", "apiKey", "token", "secretKey"); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
			return true, nil
		}
	}
	return false, nil
}

func nodeOAuth2ClientCredentialsToken(ctx context.Context, credential map[string]any) (string, string, error) {
	grantType := strings.ToLower(firstNonEmptyNode(credentialString(credential, "grantType"), "authorizationCode"))
	if grantType != "clientcredentials" && grantType != "client_credentials" {
		return "", "", nil
	}
	cred := nodeOAuth2Credential(credential)
	key := nodeOAuth2CacheKey(cred)
	if cached, ok := nodeOAuth2CachedTokenFor(key); ok {
		return cached.AccessToken, cached.TokenType, nil
	}
	result, err, _ := nodeOAuth2Fetch.Do(key, func() (any, error) {
		if cached, ok := nodeOAuth2CachedTokenFor(key); ok {
			return cached, nil
		}
		token, err := cred.ClientCredentials(ctx, nodeOAuth2HTTPClient(cred))
		if err != nil {
			return nodeOAuth2CachedToken{}, err
		}
		entry := nodeOAuth2CachedToken{AccessToken: token.AccessToken, TokenType: token.TokenType, ExpiresAt: credpkg.OAuth2ExpiresAt(token.ExpiresIn)}
		nodeOAuth2Cache.Lock()
		nodeOAuth2Cache.tokens[key] = entry
		nodeOAuth2Cache.Unlock()
		return entry, nil
	})
	if err != nil {
		return "", "", err
	}
	entry := result.(nodeOAuth2CachedToken)
	return entry.AccessToken, entry.TokenType, nil
}

func nodeOAuth2CachedTokenFor(key string) (nodeOAuth2CachedToken, bool) {
	nodeOAuth2Cache.Lock()
	defer nodeOAuth2Cache.Unlock()
	cached, ok := nodeOAuth2Cache.tokens[key]
	if ok && time.Now().Add(60*time.Second).Before(cached.ExpiresAt) {
		return cached, true
	}
	return nodeOAuth2CachedToken{}, false
}

func nodeOAuth2Credential(credential map[string]any) credpkg.OAuth2Credential {
	data, _ := json.Marshal(credential)
	var result credpkg.OAuth2Credential
	_ = json.Unmarshal(data, &result)
	return result
}

func nodeOAuth2HTTPClient(credential credpkg.OAuth2Credential) *http.Client {
	if !credential.IgnoreSSLIssues {
		return http.DefaultClient
	}
	return &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
}

func nodeOAuth2CacheKey(credential credpkg.OAuth2Credential) string {
	return strings.Join([]string{credential.ClientID, credential.AccessTokenURL, credential.Scope, credential.Authentication}, "\x00")
}

func oauth2TokenDataString(credential map[string]any, key string) string {
	tokenData, ok := rawObject(credential["oauthTokenData"])
	if !ok {
		return ""
	}
	return stringParam(tokenData, key)
}

func credentialString(credential map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := credential[key]
		if !ok || value == nil {
			continue
		}
		text := fmt.Sprint(value)
		if strings.TrimSpace(text) != "" && text != "<nil>" {
			return text
		}
	}
	return ""
}
