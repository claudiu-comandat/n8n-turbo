package msteams

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func (n *Node) handleUser(ctx context.Context, cred *Credential, operation string, params map[string]any) ([]dataplane.Item, error) {
	switch operation {
	case "getAll", "list":
		return n.listUsers(ctx, cred, params)
	case "get":
		userID := stringValue(params, "userId")
		if userID == "" {
			userID = "me"
		}
		path := "/users/" + userID
		if userID == "me" {
			path = "/me"
		}
		return single(n.doJSON(ctx, cred, http.MethodGet, path, nil))
	case "sendMail":
		return single(n.sendMail(ctx, cred, params))
	default:
		return nil, fmt.Errorf("unknown user operation %s", operation)
	}
}

func (n *Node) listUsers(ctx context.Context, cred *Credential, params map[string]any) ([]dataplane.Item, error) {
	fields := stringValue(params, "fields")
	if fields == "" {
		fields = "id,displayName,mail,userPrincipalName,jobTitle,department"
	}
	limit := intValue(params, "limit")
	if limit <= 0 {
		limit = 100
	}
	query := url.Values{}
	query.Set("$select", fields)
	query.Set("$top", fmt.Sprint(limit))
	if filter := stringValue(params, "filter"); filter != "" {
		query.Set("$filter", filter)
	}
	result, err := n.doJSON(ctx, cred, http.MethodGet, "/users?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	items, err := itemsFromValue(result, nil)
	if err != nil {
		return nil, err
	}
	if boolValue(params, "returnAll", false) {
		return n.fetchAllGraphPages(ctx, cred, result, items)
	}
	return items, nil
}

func (n *Node) fetchAllGraphPages(ctx context.Context, cred *Credential, firstPage map[string]any, initial []dataplane.Item) ([]dataplane.Item, error) {
	results := append([]dataplane.Item(nil), initial...)
	current := firstPage
	for {
		next, _ := current["@odata.nextLink"].(string)
		if next == "" {
			return results, nil
		}
		parsed, err := url.Parse(next)
		if err != nil {
			return results, nil
		}
		path := strings.TrimPrefix(parsed.RequestURI(), "/v1.0")
		page, err := n.doJSON(ctx, cred, http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}
		items, err := itemsFromValue(page, nil)
		if err != nil {
			return nil, err
		}
		results = append(results, items...)
		current = page
	}
}

func (n *Node) sendMail(ctx context.Context, cred *Credential, params map[string]any) (map[string]any, error) {
	toEmail := stringValue(params, "toEmail")
	subject := stringValue(params, "subject")
	content := stringValue(params, "content", "message")
	if toEmail == "" || subject == "" || content == "" {
		return nil, fmt.Errorf("toEmail, subject, and content are required")
	}
	contentType := stringValue(params, "contentType")
	if contentType == "" {
		contentType = "html"
	}
	message := map[string]any{
		"subject": subject,
		"body":    map[string]any{"contentType": contentType, "content": content},
		"toRecipients": []map[string]any{{
			"emailAddress": map[string]any{"address": toEmail, "name": stringValue(params, "toName")},
		}},
	}
	if cc := stringValue(params, "ccEmail"); cc != "" {
		message["ccRecipients"] = []map[string]any{{"emailAddress": map[string]any{"address": cc}}}
	}
	sender := stringValue(params, "senderId")
	if sender == "" {
		sender = "me"
	}
	path := "/users/" + sender + "/sendMail"
	if sender == "me" {
		path = "/me/sendMail"
	}
	body := map[string]any{"message": message, "saveToSentItems": boolValue(params, "saveToSentItems", true)}
	return n.doJSON(ctx, cred, http.MethodPost, path, body)
}

func (n *Node) getUserIDByEmail(ctx context.Context, cred *Credential, email string) (string, error) {
	filter := fmt.Sprintf("mail eq '%s' or userPrincipalName eq '%s'", strings.ReplaceAll(email, "'", "''"), strings.ReplaceAll(email, "'", "''"))
	query := url.Values{}
	query.Set("$filter", filter)
	query.Set("$select", "id,mail,userPrincipalName,displayName")
	result, err := n.doJSON(ctx, cred, http.MethodGet, "/users?"+query.Encode(), nil)
	if err != nil {
		return "", err
	}
	values, _ := result["value"].([]any)
	if len(values) == 0 {
		return "", fmt.Errorf("user not found with email %s", email)
	}
	first, _ := values[0].(map[string]any)
	id, _ := first["id"].(string)
	if id == "" {
		return "", fmt.Errorf("user id not found")
	}
	return id, nil
}
