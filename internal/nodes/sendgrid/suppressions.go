package sendgrid

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func (n *Node) handleSuppression(ctx context.Context, cred Credential, operation string, params map[string]any) ([]dataplane.Item, error) {
	switch operation {
	case "getAll", "list":
		return itemsFromMaps(n.getSuppressions(ctx, cred, params))
	case "add", "create":
		return single(n.addSuppression(ctx, cred, params))
	case "delete", "remove":
		return single(n.deleteSuppression(ctx, cred, params))
	default:
		return nil, fmt.Errorf("unknown suppression operation %s", operation)
	}
}

func (n *Node) getSuppressions(ctx context.Context, cred Credential, params map[string]any) ([]map[string]any, error) {
	limit := intParam(params, "limit")
	offset := intParam(params, "offset")
	query := url.Values{}
	if limit > 0 {
		query.Set("limit", fmt.Sprint(limit))
	}
	if offset > 0 {
		query.Set("offset", fmt.Sprint(offset))
	}
	path := "/suppression/unsubscribes"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	result, err := n.doJSON(ctx, cred, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	return listFrom(result, "result"), nil
}

func (n *Node) addSuppression(ctx context.Context, cred Credential, params map[string]any) (map[string]any, error) {
	emails := stringSlice(params, "emails")
	if len(emails) == 0 {
		email := stringParam(params, "email")
		if email != "" {
			emails = []string{email}
		}
	}
	if len(emails) == 0 {
		return nil, fmt.Errorf("email or emails is required")
	}
	return n.doJSON(ctx, cred, http.MethodPost, "/asm/suppressions/global", map[string]any{"recipient_emails": emails})
}

func (n *Node) deleteSuppression(ctx context.Context, cred Credential, params map[string]any) (map[string]any, error) {
	email := stringParam(params, "email")
	if email == "" {
		return nil, fmt.Errorf("email is required")
	}
	return n.doJSON(ctx, cred, http.MethodDelete, "/suppression/unsubscribes/"+url.PathEscape(email), nil)
}
