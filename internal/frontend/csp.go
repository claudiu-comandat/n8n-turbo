package frontend

import (
	"fmt"
	"net/http"
	"strings"
)

type CSPConfig struct {
	Enabled    bool
	ReportOnly bool
	ConnectSrc []string
	FontSrc    []string
	ImgSrc     []string
	ScriptSrc  []string
	StyleSrc   []string
}

func DefaultCSP(baseURL string) CSPConfig {
	connect := []string{"'self'"}
	if baseURL != "" {
		connect = append(connect, baseURL, "wss://"+stripScheme(baseURL))
	}
	return CSPConfig{
		Enabled:    true,
		ConnectSrc: connect,
		FontSrc:    []string{"'self'", "data:"},
		ImgSrc:     []string{"'self'", "data:", "blob:", "https:"},
		ScriptSrc:  []string{"'self'", "'unsafe-inline'", "'unsafe-eval'", "'wasm-unsafe-eval'", "data:"},
		StyleSrc:   []string{"'self'", "'unsafe-inline'"},
	}
}

func ApplyCSP(cfg CSPConfig, w http.ResponseWriter) {
	if !cfg.Enabled {
		return
	}
	parts := []string{
		"default-src 'self'",
		fmt.Sprintf("connect-src %s", strings.Join(nonEmpty(cfg.ConnectSrc, "'self'"), " ")),
		fmt.Sprintf("font-src %s", strings.Join(nonEmpty(cfg.FontSrc, "'self'"), " ")),
		fmt.Sprintf("img-src %s", strings.Join(nonEmpty(cfg.ImgSrc, "'self'"), " ")),
		fmt.Sprintf("script-src %s", strings.Join(nonEmpty(cfg.ScriptSrc, "'self'"), " ")),
		fmt.Sprintf("style-src %s", strings.Join(nonEmpty(cfg.StyleSrc, "'self'"), " ")),
		"worker-src 'self' blob:",
	}
	header := "Content-Security-Policy"
	if cfg.ReportOnly {
		header = "Content-Security-Policy-Report-Only"
	}
	w.Header().Set(header, strings.Join(parts, "; "))
}

func stripScheme(value string) string {
	value = strings.TrimPrefix(value, "https://")
	value = strings.TrimPrefix(value, "http://")
	return strings.TrimRight(value, "/")
}

func nonEmpty(values []string, fallback string) []string {
	if len(values) == 0 {
		return []string{fallback}
	}
	return values
}
