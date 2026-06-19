package twilio

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
)

var accountSidRegex = regexp.MustCompile(`(?i)^AC[0-9a-f]{32}$`)
var authTokenRegex = regexp.MustCompile(`(?i)^[0-9a-f]{32}$`)

type Credential struct {
	AccountSid string `json:"accountSid"`
	AuthToken  string `json:"authToken"`
}

func (c Credential) Validate() error {
	if !accountSidRegex.MatchString(c.AccountSid) {
		return fmt.Errorf("twilio invalid AccountSid format")
	}
	if !authTokenRegex.MatchString(c.AuthToken) {
		return fmt.Errorf("twilio invalid AuthToken format")
	}
	return nil
}

func (c Credential) BasicAuth() string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(c.AccountSid+":"+c.AuthToken))
}

func (c Credential) BaseURL() string {
	return "https://api.twilio.com/2010-04-01/Accounts/" + c.AccountSid
}

func (c Credential) LookupURL() string {
	return "https://lookups.twilio.com/v1/PhoneNumbers"
}

func (c Credential) VerifyURL(serviceSid string) string {
	return "https://verify.twilio.com/v2/Services/" + serviceSid
}

func extractCredential(credentials map[string]map[string]any) (Credential, error) {
	for _, key := range []string{"twilioApi", "twilio", "credentials"} {
		if credential, ok := credentials[key]; ok {
			cred := Credential{AccountSid: stringValue(credential, "accountSid"), AuthToken: stringValue(credential, "authToken", "password")}
			if err := cred.Validate(); err == nil {
				return cred, nil
			}
		}
	}
	for _, credential := range credentials {
		cred := Credential{AccountSid: stringValue(credential, "accountSid"), AuthToken: stringValue(credential, "authToken", "password")}
		if err := cred.Validate(); err == nil {
			return cred, nil
		}
	}
	return Credential{}, fmt.Errorf("twilioApi accountSid/authToken are required")
}

func stringValue(params map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := params[key]; ok {
			text := strings.TrimSpace(fmt.Sprint(value))
			if text != "" && text != "<nil>" {
				return text
			}
		}
	}
	return ""
}
