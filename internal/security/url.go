package security

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

func ValidatePublicHTTPURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("URL scheme %q is not allowed", parsed.Scheme)
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("URL host is required")
	}
	if isBlockedHostname(host) {
		return fmt.Errorf("access to private or local host is not allowed")
	}
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateIP(ip) {
			return fmt.Errorf("access to private or local IP is not allowed")
		}
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil
	}
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return fmt.Errorf("host resolves to private or local IP")
		}
	}
	return nil
}

func isBlockedHostname(host string) bool {
	value := strings.ToLower(strings.TrimSuffix(host, "."))
	return value == "localhost" || strings.HasSuffix(value, ".localhost")
}

func isPrivateIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}
