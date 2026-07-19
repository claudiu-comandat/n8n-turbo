package api

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/n8n-io/n8n-turbo/internal/auth"
)

const apiKeyHeader = "X-N8N-API-KEY"

// authGuard authenticates a request by EITHER a valid API key (X-N8N-API-KEY,
// checked against the keys minted in Settings → n8n API) OR the normal session
// cookie / bearer JWT. The API-key path is what lets a CLI or MCP drive a hosted
// instance with a long-lived token instead of a 7-day login JWT.
func (s *Server) authGuard(next http.Handler) http.Handler {
	jwtGuard := auth.Middleware(s.authService, s.config.Auth)(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if presented := strings.TrimSpace(r.Header.Get(apiKeyHeader)); presented != "" {
			user, ok := s.validateAPIKey(r, presented)
			if !ok {
				writeError(w, http.StatusUnauthorized, "invalid api key")
				return
			}
			next.ServeHTTP(w, r.WithContext(auth.ContextWithUser(r.Context(), user)))
			return
		}
		jwtGuard.ServeHTTP(w, r)
	})
}

// validateAPIKey constant-time compares the presented key against every stored
// key and, on a match, resolves the owning user so downstream handlers see a
// normal authenticated user.
func (s *Server) validateAPIKey(r *http.Request, presented string) (auth.User, bool) {
	state, err := s.loadAPIKeys(r)
	if err != nil {
		return auth.User{}, false
	}
	presentedBytes := []byte(presented)
	for _, key := range state.Keys {
		if key.APIKey == "" {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(key.APIKey), presentedBytes) == 1 {
			return s.apiKeyUser(r.Context(), key), true
		}
	}
	return auth.User{}, false
}

func (s *Server) apiKeyUser(ctx context.Context, key publicAPIKey) auth.User {
	id, _ := key.Owner["id"].(string)
	email, _ := key.Owner["email"].(string)
	if s.userStore != nil && id != "" {
		if user, err := s.userStore.GetByID(ctx, id); err == nil && user != nil {
			return auth.User{
				ID:        user.ID,
				Email:     user.Email,
				FirstName: user.FirstName,
				LastName:  user.LastName,
				Role:      user.Role,
				IsOwner:   user.Role == "global:owner",
			}
		}
	}
	return auth.User{
		ID:      firstNonEmpty(id, "owner"),
		Email:   firstNonEmpty(email, "owner@n8n.local"),
		Role:    "global:owner",
		IsOwner: true,
	}
}
