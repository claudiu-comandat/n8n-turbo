package metadata

import (
	"sort"

	"github.com/n8n-io/n8n-turbo/internal/descriptor"
)

func CredentialTypes() []CredentialType {
	types := []CredentialType{
		genericCredential(credential("httpBasicAuth", "Basic Auth", text("User", "user", ""), secret("Password", "password"))),
		genericCredential(credential("httpBearerAuth", "Bearer Auth", secret("Access Token", "accessToken"))),
		genericCredential(credential("httpCustomAuth", "Custom Auth", text("Header Name", "headerName", "Authorization"), secret("Header Value", "headerValue"))),
		genericCredential(credential("httpDigestAuth", "Digest Auth", text("User", "user", ""), secret("Password", "password"))),
		genericCredential(credential("httpHeaderAuth", "Header Auth", text("Name", "name", "Authorization"), secret("Value", "value"))),
		genericCredential(credential("httpQueryAuth", "Query Auth", text("Query Parameter Name", "queryParameterName", "access_token"), secret("Access Token", "accessToken"))),
		credential("jwtAuth", "JWT Auth", secret("Secret", "secret"), selectProp("Key Type", "keyType", "passphrase", []Option{{Name: "Passphrase", Value: "passphrase"}, {Name: "PEM Key", Value: "pemKey"}}), text("Header Name", "headerName", "Authorization")),
		genericCredential(credential("oAuth1Api", "OAuth1 API", text("Consumer Key", "consumerKey", ""), secret("Consumer Secret", "consumerSecret"), text("Access Token", "accessToken", ""), secret("Access Token Secret", "accessTokenSecret"))),
		genericCredential(credential("oAuth2Api", "OAuth2 API", text("Client ID", "clientId", ""), secret("Client Secret", "clientSecret"), text("Auth URL", "authUrl", ""), text("Access Token URL", "accessTokenUrl", ""), text("Scope", "scope", ""), secret("Access Token", "accessToken"))),
		credential("slackApi", "Slack API", secret("Access Token", "accessToken")),
		credential("githubApi", "GitHub API", secret("Access Token", "accessToken")),
		credential("gmailOAuth2Api", "Gmail OAuth2 API", text("Client ID", "clientId", ""), secret("Client Secret", "clientSecret"), secret("Access Token", "accessToken"), secret("Refresh Token", "refreshToken")),
		credential("googleOAuth2Api", "Google OAuth2 API", text("Client ID", "clientId", ""), secret("Client Secret", "clientSecret"), secret("Access Token", "accessToken"), secret("Refresh Token", "refreshToken")),
		credential("googleSheetsOAuth2Api", "Google Sheets OAuth2 API", text("Client ID", "clientId", ""), secret("Client Secret", "clientSecret"), secret("Access Token", "accessToken"), secret("Refresh Token", "refreshToken")),
		credential("googleApi", "Google Service Account API", text("Email", "email", ""), secret("Private Key", "privateKey")),
		credential("notionApi", "Notion API", secret("Internal Integration Token", "apiKey")),
		credential("airtableApi", "Airtable API", secret("Access Token", "accessToken")),
		credential("jiraSoftwareCloudApi", "Jira Software Cloud API", text("Email", "email", ""), secret("API Token", "apiToken"), text("Domain", "domain", "")),
		credential("hubspotApi", "HubSpot API", secret("Access Token", "accessToken")),
		credential("hubspotPrivateAppApi", "HubSpot Private App", secret("Private App Token", "accessToken")),
		credential("stripeApi", "Stripe API", secret("Secret Key", "secretKey")),
		credential("openAiApi", "OpenAI API", secret("API Key", "apiKey"), text("Organization ID", "organizationId", ""), text("Base URL", "baseUrl", "https://api.openai.com/v1")),
		credential("telegramApi", "Telegram API", secret("Access Token", "accessToken")),
		credential("discordBotApi", "Discord Bot API", secret("Bot Token", "botToken")),
		credential("twilioApi", "Twilio API", text("Account SID", "accountSid", ""), secret("Auth Token", "authToken")),
		credential("sendGridApi", "SendGrid API", secret("API Key", "apiKey")),
		credential("shopifyAccessTokenApi", "Shopify Access Token API", text("Shop Subdomain", "shopSubdomain", ""), secret("Access Token", "accessToken"), text("API Version", "apiVersion", "2024-10")),
		credential("microsoftTeamsOAuth2Api", "Microsoft Teams OAuth2 API", text("Client ID", "clientId", ""), secret("Client Secret", "clientSecret"), text("Tenant ID", "tenantId", ""), secret("Access Token", "accessToken")),
		credential("trelloApi", "Trello API", secret("API Key", "apiKey"), secret("Token", "token")),
		credential("postgres", "Postgres", text("Host", "host", "localhost"), numberProp("Port", "port", 5432), text("Database", "database", ""), text("User", "user", ""), secret("Password", "password"), booleanProp("SSL", "ssl", false)),
		credential("mySql", "MySQL", text("Host", "host", "localhost"), numberProp("Port", "port", 3306), text("Database", "database", ""), text("User", "user", ""), secret("Password", "password")),
		credential("redis", "Redis", text("Host", "host", "localhost"), numberProp("Port", "port", 6379), secret("Password", "password"), numberProp("Database Number", "databaseNumber", 0), booleanProp("SSL", "ssl", false), booleanProp("TLS Insecure", "tlsInsecure", false)),
		credential("mongoDb", "MongoDB", text("Connection String", "connectionString", "mongodb://localhost:27017"), text("Database", "database", ""), text("Authentication Database", "authenticationDatabase", ""), booleanProp("TLS", "tls", false), booleanProp("TLS Insecure", "tlsInsecure", false)),
	}
	enrichCredentialTypes(types)
	sort.Slice(types, func(i, j int) bool { return types[i].Name < types[j].Name })
	return types
}

func CredentialTypeByName(name string) (CredentialType, bool) {
	for _, credential := range CredentialTypes() {
		if credential.Name == name {
			return credential, true
		}
	}
	return CredentialType{}, false
}

func credential(name string, display string, props ...Property) CredentialType {
	for index := range props {
		if props[index].Default == "" && !optionalCredentialProperty(props[index].Name) {
			props[index].Required = true
		}
	}
	return CredentialType{
		Name:             name,
		DisplayName:      display,
		DocumentationURL: "https://docs.n8n.io/integrations/builtin/credentials/" + name,
		Properties:       props,
		Test:             map[string]any{"request": map[string]any{}},
	}
}

func genericCredential(credential CredentialType) CredentialType {
	credential.GenericAuth = true
	return credential
}

func enrichCredentialTypes(types []CredentialType) {
	descriptorByCredential := map[string]descriptor.Descriptor{}
	for _, desc := range descriptor.Builtins() {
		if desc.CredentialType != "" {
			descriptorByCredential[desc.CredentialType] = desc
		}
	}
	for index := range types {
		desc, ok := descriptorByCredential[types[index].Name]
		if !ok {
			continue
		}
		types[index].Authenticate = map[string]any{"type": desc.AuthType, "properties": desc.AuthConfig}
		types[index].IconURL = desc.IconURL
		if types[index].IconURL == "" {
			types[index].IconURL = "file:" + desc.Name + ".svg"
		}
		if operation, ok := desc.Operations["default"]; ok {
			types[index].Test = map[string]any{
				"request": map[string]any{
					"baseURL": desc.BaseURL,
					"url":     operation.Path,
					"method":  operation.Method,
					"headers": desc.DefaultHeaders,
				},
			}
		}
	}
}

func optionalCredentialProperty(name string) bool {
	switch name {
	case "organizationId", "baseUrl", "scope", "refreshToken":
		return true
	default:
		return false
	}
}

func secret(display string, name string) Property {
	prop := text(display, name, "")
	prop.TypeOptions = map[string]any{"password": true}
	return prop
}

func booleanProp(display string, name string, def bool) Property {
	return Property{DisplayName: display, Name: name, Type: "boolean", Default: def}
}
