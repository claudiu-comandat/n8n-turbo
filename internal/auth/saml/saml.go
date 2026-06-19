package saml

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type Config struct {
	MetadataURL   string `json:"metadataUrl"`
	EntityID      string `json:"entityId"`
	CertPEM       string `json:"certPem"`
	PrivKeyPEM    string `json:"privateKeyPem"`
	CallbackURL   string `json:"callbackUrl"`
	EmailAttr     string `json:"emailAttribute"`
	NameAttr      string `json:"nameAttribute"`
	FirstNameAttr string `json:"firstNameAttribute"`
	LastNameAttr  string `json:"lastNameAttribute"`
}

type User struct {
	Email     string   `json:"email"`
	FirstName string   `json:"firstName"`
	LastName  string   `json:"lastName"`
	Groups    []string `json:"groups"`
	NameID    string   `json:"nameId"`
}

type ServiceProvider struct {
	config Config
}

func NewServiceProvider(cfg Config) (*ServiceProvider, error) {
	if cfg.EntityID == "" || cfg.CallbackURL == "" {
		return nil, fmt.Errorf("saml entityId and callbackUrl are required")
	}
	return &ServiceProvider{config: cfg}, nil
}

func (sp *ServiceProvider) LoginURL(relayState string) (string, error) {
	if sp.config.MetadataURL == "" {
		return "", fmt.Errorf("saml metadataUrl is required")
	}
	target, err := url.Parse(sp.config.MetadataURL)
	if err != nil {
		return "", err
	}
	query := target.Query()
	query.Set("SAMLRequest", sp.authnRequest())
	if relayState != "" {
		query.Set("RelayState", relayState)
	}
	target.RawQuery = query.Encode()
	return target.String(), nil
}

func (sp *ServiceProvider) HandleCallback(r *http.Request) (*User, error) {
	if err := r.ParseForm(); err != nil {
		return nil, err
	}
	raw := r.FormValue("SAMLResponse")
	if raw == "" {
		return nil, fmt.Errorf("missing SAMLResponse")
	}
	return sp.ParseResponse(raw)
}

func (sp *ServiceProvider) ParseResponse(encoded string) (*User, error) {
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	var response assertionResponse
	if err := xml.Unmarshal(decoded, &response); err != nil {
		return nil, err
	}
	attrs := map[string][]string{}
	for _, statement := range response.Assertion.AttributeStatements {
		for _, attr := range statement.Attributes {
			values := make([]string, 0, len(attr.Values))
			for _, value := range attr.Values {
				values = append(values, strings.TrimSpace(value.Value))
			}
			attrs[attr.Name] = values
		}
	}
	user := &User{NameID: response.Assertion.Subject.NameID.Value}
	user.Email = firstAttr(attrs, sp.config.EmailAttr, "email", "mail", "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress")
	user.FirstName = firstAttr(attrs, sp.config.FirstNameAttr, "firstName", "givenName", "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/givenname")
	user.LastName = firstAttr(attrs, sp.config.LastNameAttr, "lastName", "sn", "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/surname")
	user.Groups = attrs["groups"]
	if len(user.Groups) == 0 {
		user.Groups = attrs["memberOf"]
	}
	if user.Email == "" {
		user.Email = user.NameID
	}
	if user.Email == "" {
		return nil, fmt.Errorf("saml email is missing")
	}
	return user, nil
}

func (sp *ServiceProvider) Metadata() ([]byte, error) {
	entityID := xmlEscape(sp.config.EntityID)
	acs := xmlEscape(sp.config.CallbackURL)
	return []byte(fmt.Sprintf(`<EntityDescriptor entityID="%s" xmlns="urn:oasis:names:tc:SAML:2.0:metadata"><SPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol"><AssertionConsumerService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST" Location="%s" index="1"/></SPSSODescriptor></EntityDescriptor>`, entityID, acs)), nil
}

func (sp *ServiceProvider) authnRequest() string {
	request := fmt.Sprintf(`<samlp:AuthnRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" AssertionConsumerServiceURL="%s" EntityID="%s"/>`, xmlEscape(sp.config.CallbackURL), xmlEscape(sp.config.EntityID))
	var buffer bytes.Buffer
	writer, _ := flate.NewWriter(&buffer, flate.DefaultCompression)
	_, _ = writer.Write([]byte(request))
	_ = writer.Close()
	return base64.StdEncoding.EncodeToString(buffer.Bytes())
}

type assertionResponse struct {
	Assertion assertion `xml:"Assertion"`
}

type assertion struct {
	Subject             subject              `xml:"Subject"`
	AttributeStatements []attributeStatement `xml:"AttributeStatement"`
}

type subject struct {
	NameID nameID `xml:"NameID"`
}

type nameID struct {
	Value string `xml:",chardata"`
}

type attributeStatement struct {
	Attributes []attribute `xml:"Attribute"`
}

type attribute struct {
	Name   string           `xml:"Name,attr"`
	Values []attributeValue `xml:"AttributeValue"`
}

type attributeValue struct {
	Value string `xml:",chardata"`
}

func firstAttr(attrs map[string][]string, keys ...string) string {
	for _, key := range keys {
		if key == "" {
			continue
		}
		values := attrs[key]
		if len(values) > 0 && values[0] != "" {
			return values[0]
		}
	}
	return ""
}

func xmlEscape(value string) string {
	var buffer strings.Builder
	_ = xml.EscapeText(&buffer, []byte(value))
	return buffer.String()
}

func InflateRequest(encoded string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	reader := flate.NewReader(bytes.NewReader(data))
	defer reader.Close()
	inflated, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return string(inflated), nil
}
