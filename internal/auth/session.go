package auth

import (
	"net/http"
	"strings"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/config"
)

func SetSessionCookie(w http.ResponseWriter, token string, expiresAt time.Time, cfg config.AuthConfig) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   cfg.CookieSecure,
		SameSite: sameSiteMode(cfg.CookieSameSite),
		Expires:  expiresAt,
	})
}

func ClearSessionCookie(w http.ResponseWriter, cfg config.AuthConfig) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   cfg.CookieSecure,
		SameSite: sameSiteMode(cfg.CookieSameSite),
		MaxAge:   -1,
		Expires:  time.Unix(0, 0).UTC(),
	})
}

func sameSiteMode(value string) http.SameSite {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "strict":
		return http.SameSiteStrictMode
	case "none":
		return http.SameSiteNoneMode
	default:
		return http.SameSiteLaxMode
	}
}
