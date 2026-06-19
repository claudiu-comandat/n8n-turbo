package security

import "net/http"

var Headers = map[string]string{
	"X-Content-Type-Options":       "nosniff",
	"X-Frame-Options":              "SAMEORIGIN",
	"Referrer-Policy":              "strict-origin-when-cross-origin",
	"Permissions-Policy":           "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()",
	"Cross-Origin-Resource-Policy": "same-origin",
	"Cross-Origin-Opener-Policy":   "same-origin",
}

func ApplyHeaders(header http.Header, csp string, hsts bool) {
	for key, value := range Headers {
		header.Set(key, value)
	}
	if csp != "" {
		header.Set("Content-Security-Policy", csp)
	}
	if hsts {
		header.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	}
}
