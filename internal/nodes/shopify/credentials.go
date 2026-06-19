package shopify

import (
	"fmt"
	"strings"
)

type Credential struct {
	ShopSubdomain string `json:"shopSubdomain"`
	AccessToken   string `json:"accessToken"`
	APIVersion    string `json:"apiVersion"`
}

func (c *Credential) Validate() error {
	c.ShopSubdomain = normalizeShop(c.ShopSubdomain)
	if c.ShopSubdomain == "" {
		return fmt.Errorf("shopify shopSubdomain is required")
	}
	if strings.TrimSpace(c.AccessToken) == "" {
		return fmt.Errorf("shopify accessToken is required")
	}
	if c.APIVersion == "" {
		c.APIVersion = "2024-10"
	}
	return nil
}

func (c Credential) BaseURL() string {
	return fmt.Sprintf("https://%s.myshopify.com/admin/api/%s", c.ShopSubdomain, c.APIVersion)
}

func (c Credential) AuthHeader() string {
	return c.AccessToken
}

func extractCredential(credentials map[string]map[string]any) (Credential, error) {
	for _, key := range []string{"shopifyAccessTokenApi", "shopifyApi", "credentials"} {
		if credential, ok := credentials[key]; ok {
			cred := Credential{
				ShopSubdomain: stringValue(credential, "shopSubdomain", "shop", "subdomain"),
				AccessToken:   stringValue(credential, "accessToken", "token"),
				APIVersion:    stringValue(credential, "apiVersion"),
			}
			if err := cred.Validate(); err == nil {
				return cred, nil
			}
		}
	}
	for _, credential := range credentials {
		cred := Credential{
			ShopSubdomain: stringValue(credential, "shopSubdomain", "shop", "subdomain"),
			AccessToken:   stringValue(credential, "accessToken", "token"),
			APIVersion:    stringValue(credential, "apiVersion"),
		}
		if err := cred.Validate(); err == nil {
			return cred, nil
		}
	}
	return Credential{}, fmt.Errorf("shopifyAccessTokenApi credentials are required")
}

func normalizeShop(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "https://")
	value = strings.TrimPrefix(value, "http://")
	value = strings.Trim(value, "/")
	value = strings.TrimSuffix(value, ".myshopify.com")
	if index := strings.Index(value, ".myshopify.com/"); index >= 0 {
		value = value[:index]
	}
	return value
}
