package auth

import (
	"net/http"
	"strings"

	"github.com/n8n-io/n8n-turbo/internal/config"
)

func Middleware(service *Service, cfg config.AuthConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, err := tokenFromRequest(r, cfg)
			if err != nil {
				http.Error(w, ErrNoToken.Error(), http.StatusUnauthorized)
				return
			}
			claims, err := service.ValidateToken(token)
			if err != nil {
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r.WithContext(withUser(r.Context(), userFromClaims(claims))))
		})
	}
}

func OptionalMiddleware(service *Service, cfg config.AuthConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, err := tokenFromRequest(r, cfg)
			if err == nil && token != "" {
				if claims, err := service.ValidateToken(token); err == nil {
					r = r.WithContext(withUser(r.Context(), userFromClaims(claims)))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func tokenFromRequest(r *http.Request, cfg config.AuthConfig) (string, error) {
	if cookie, err := r.Cookie(cookieName); err == nil && strings.TrimSpace(cookie.Value) != "" {
		return cookie.Value, nil
	}
	raw := r.Header.Get(cfg.JWTHeader)
	if strings.TrimSpace(raw) == "" {
		return "", ErrNoToken
	}
	prefix := cfg.JWTPrefix + " "
	if strings.HasPrefix(raw, prefix) {
		return strings.TrimSpace(strings.TrimPrefix(raw, prefix)), nil
	}
	return strings.TrimSpace(raw), nil
}
