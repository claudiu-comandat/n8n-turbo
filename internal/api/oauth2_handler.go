package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/n8n-io/n8n-turbo/internal/credentials"
	"github.com/n8n-io/n8n-turbo/internal/persistence"
)

type oauthState struct {
	CredentialID string `json:"credentialId"`
	RedirectURI  string `json:"redirectUri"`
	Nonce        string `json:"nonce"`
	CreatedAt    int64  `json:"createdAt"`
}

func (s *Server) handleOAuth2Auth(w http.ResponseWriter, r *http.Request) {
	credentialID := firstNonEmpty(r.URL.Query().Get("credentialId"), chi.URLParam(r, "id"))
	redirectURI := firstNonEmpty(r.URL.Query().Get("redirectUri"), s.oauthCallbackURL(r))
	if credentialID == "" {
		writeError(w, http.StatusBadRequest, "credentialId is required")
		return
	}
	row, data, err := s.oauthCredentialData(r.Context(), credentialID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	cred := oauth2CredentialFromData(data)
	if cred.PKCE && cred.CodeVerifier == "" {
		verifier, _, err := credentials.GeneratePKCEChallenge()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		cred.CodeVerifier = verifier
		data["codeVerifier"] = verifier
		encrypted, err := s.encryptCredentialData(data)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if _, err := s.credentialStore.Save(r.Context(), persistence.CredentialRow{ID: row.ID, Name: row.Name, Type: row.Type, Data: encrypted, OwnerID: row.OwnerID}); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	state, err := s.signOAuthState(oauthState{CredentialID: row.ID, RedirectURI: redirectURI, Nonce: randomHex(16), CreatedAt: time.Now().UTC().Unix()})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	authURL, err := cred.BuildAuthURL(redirectURI, state)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"url": authURL, "state": state, "credentialId": row.ID}})
}

func (s *Server) handleOAuth2Callback(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
	}
	code := firstNonEmpty(r.URL.Query().Get("code"), r.FormValue("code"))
	rawState := firstNonEmpty(r.URL.Query().Get("state"), r.FormValue("state"))
	redirectURI := firstNonEmpty(r.URL.Query().Get("redirectUri"), r.FormValue("redirectUri"))
	if code == "" || rawState == "" {
		writeError(w, http.StatusBadRequest, "code and state are required")
		return
	}
	state, err := s.verifyOAuthState(rawState)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if redirectURI == "" {
		redirectURI = state.RedirectURI
	}
	row, data, err := s.oauthCredentialData(r.Context(), state.CredentialID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	cred := oauth2CredentialFromData(data)
	token, err := cred.ExchangeCode(r.Context(), http.DefaultClient, code, redirectURI)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := s.saveOAuthToken(r.Context(), row, data, token); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"success": true, "credentialId": row.ID, "expiresAt": credentials.OAuth2ExpiresAt(token.ExpiresIn).Format(time.RFC3339Nano)}})
}

func (s *Server) handleOAuth2Refresh(w http.ResponseWriter, r *http.Request) {
	credentialID := chi.URLParam(r, "id")
	row, data, err := s.oauthCredentialData(r.Context(), credentialID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	cred := oauth2CredentialFromData(data)
	refreshToken := firstNonEmpty(stringFromMap(data, "refreshToken"), cred.RefreshToken)
	if refreshToken == "" {
		writeError(w, http.StatusBadRequest, "refresh token is missing")
		return
	}
	token, err := cred.Refresh(r.Context(), http.DefaultClient, refreshToken)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if token.RefreshToken == "" {
		token.RefreshToken = refreshToken
	}
	if err := s.saveOAuthToken(r.Context(), row, data, token); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"success": true, "credentialId": row.ID}})
}

func (s *Server) handleOAuth2ClientCredentials(w http.ResponseWriter, r *http.Request) {
	credentialID := chi.URLParam(r, "id")
	row, data, err := s.oauthCredentialData(r.Context(), credentialID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	cred := oauth2CredentialFromData(data)
	token, err := cred.ClientCredentials(r.Context(), http.DefaultClient)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := s.saveOAuthToken(r.Context(), row, data, token); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"success": true, "credentialId": row.ID}})
}

func (s *Server) oauthCredentialData(ctx context.Context, credentialID string) (*persistence.CredentialRow, map[string]any, error) {
	row, err := s.credentialStore.GetByID(ctx, credentialID)
	if err != nil {
		return nil, nil, err
	}
	data, err := s.decryptCredentialData(row.Data)
	if err != nil {
		return nil, nil, err
	}
	return row, data, nil
}

func oauth2CredentialFromData(data map[string]any) credentials.OAuth2Credential {
	return credentials.OAuth2Credential{
		GrantType:           stringFromMap(data, "grantType"),
		ClientID:            stringFromMap(data, "clientId"),
		ClientSecret:        stringFromMap(data, "clientSecret"),
		AuthURL:             firstNonEmpty(stringFromMap(data, "authUrl"), stringFromMap(data, "authorizationUrl")),
		AuthorizationURL:    stringFromMap(data, "authorizationUrl"),
		AccessTokenURL:      stringFromMap(data, "accessTokenUrl"),
		Scope:               stringFromMap(data, "scope"),
		Authentication:      stringFromMap(data, "authentication"),
		AuthQueryParameters: stringFromMap(data, "authQueryParameters"),
		CodeVerifier:        stringFromMap(data, "codeVerifier"),
		PKCE:                boolFromAny(data["pkce"]),
		AccessToken:         stringFromMap(data, "accessToken"),
		RefreshToken:        stringFromMap(data, "refreshToken"),
		TokenType:           stringFromMap(data, "tokenType"),
		ExpiresAt:           stringFromMap(data, "expiresAt"),
	}
}

func (s *Server) saveOAuthToken(ctx context.Context, row *persistence.CredentialRow, data map[string]any, token *credentials.OAuth2TokenResponse) error {
	expiresAt := credentials.OAuth2ExpiresAt(token.ExpiresIn).Format(time.RFC3339Nano)
	data["accessToken"] = token.AccessToken
	data["tokenType"] = token.TokenType
	data["expiresIn"] = token.ExpiresIn
	data["expiresAt"] = expiresAt
	if token.RefreshToken != "" {
		data["refreshToken"] = token.RefreshToken
	}
	if token.Scope != "" {
		data["scope"] = token.Scope
	}
	if token.IDToken != "" {
		data["idToken"] = token.IDToken
	}
	data["oauthTokenData"] = map[string]any{"access_token": token.AccessToken, "refresh_token": data["refreshToken"], "token_type": token.TokenType, "expires_in": token.ExpiresIn, "expires_at": expiresAt, "scope": token.Scope}
	encrypted, err := s.encryptCredentialData(data)
	if err != nil {
		return err
	}
	_, err = s.credentialStore.Save(ctx, persistence.CredentialRow{ID: row.ID, Name: row.Name, Type: row.Type, Data: encrypted, OwnerID: row.OwnerID})
	return err
}

func (s *Server) signOAuthState(state oauthState) (string, error) {
	payload, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(s.config.EncryptionKey))
	mac.Write([]byte(encodedPayload))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encodedPayload + "." + signature, nil
}

func (s *Server) verifyOAuthState(raw string) (oauthState, error) {
	parts := strings.SplitN(raw, ".", 2)
	if len(parts) != 2 {
		return oauthState{}, fmt.Errorf("invalid oauth state")
	}
	mac := hmac.New(sha256.New, []byte(s.config.EncryptionKey))
	mac.Write([]byte(parts[0]))
	expected := mac.Sum(nil)
	actual, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(expected, actual) {
		return oauthState{}, fmt.Errorf("invalid oauth state signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return oauthState{}, err
	}
	var state oauthState
	if err := json.Unmarshal(payload, &state); err != nil {
		return oauthState{}, err
	}
	if time.Now().UTC().Unix()-state.CreatedAt > 15*60 {
		return oauthState{}, fmt.Errorf("oauth state expired")
	}
	return state, nil
}

func (s *Server) oauthCallbackURL(r *http.Request) string {
	protocol := "http"
	if r.TLS != nil {
		protocol = "https"
	}
	return protocol + "://" + r.Host + "/rest/oauth2-credential/callback"
}

func randomHex(size int) string {
	bytes := make([]byte, size)
	_, _ = rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func boolFromAny(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(typed, "true") || typed == "1"
	default:
		return false
	}
}

func stringFromMap(data map[string]any, key string) string {
	value, ok := data[key]
	if !ok || value == nil {
		return ""
	}
	return fmt.Sprint(value)
}
