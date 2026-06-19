package trello

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func (n *Node) handleBoard(ctx context.Context, cred Credential, operation string, params map[string]any) ([]dataplane.Item, error) {
	switch operation {
	case "get":
		boardID := stringValue(params, "boardId")
		if boardID == "" {
			return nil, fmt.Errorf("boardId is required")
		}
		return single(n.doJSON(ctx, cred, http.MethodGet, "/boards/"+boardID+"?fields=all&lists=open", nil))
	case "getAll", "list":
		memberID := stringValue(params, "memberId")
		if memberID == "" {
			memberID = "me"
		}
		query := url.Values{}
		filter := stringValue(params, "filter")
		if filter == "" {
			filter = "open"
		}
		query.Set("filter", filter)
		query.Set("fields", "id,name,desc,url,shortUrl,closed,dateLastActivity")
		return itemsFromArray(n.doArray(ctx, cred, http.MethodGet, fmt.Sprintf("/members/%s/boards?%s", memberID, query.Encode()), nil))
	case "create":
		name := stringValue(params, "name")
		if name == "" {
			return nil, fmt.Errorf("name is required")
		}
		body := map[string]any{"name": name, "defaultLists": boolValue(params, "defaultLists", true), "defaultLabels": boolValue(params, "defaultLabels", true)}
		setString(body, "desc", stringValue(params, "description", "desc"))
		setString(body, "prefs_background", stringValue(params, "background"))
		setString(body, "prefs_voting", stringValue(params, "voting"))
		setString(body, "prefs_comments", stringValue(params, "comments"))
		setString(body, "idOrganization", stringValue(params, "organizationId"))
		return single(n.doJSON(ctx, cred, http.MethodPost, "/boards", body))
	default:
		return nil, fmt.Errorf("unknown board operation %s", operation)
	}
}

func (n *Node) handleList(ctx context.Context, cred Credential, operation string, params map[string]any) ([]dataplane.Item, error) {
	switch operation {
	case "get":
		listID := stringValue(params, "listId")
		if listID == "" {
			return nil, fmt.Errorf("listId is required")
		}
		return single(n.doJSON(ctx, cred, http.MethodGet, "/lists/"+listID, nil))
	case "getAll", "list":
		boardID := stringValue(params, "boardId")
		if boardID == "" {
			return nil, fmt.Errorf("boardId is required")
		}
		filter := stringValue(params, "filter")
		if filter == "" {
			filter = "open"
		}
		return itemsFromArray(n.doArray(ctx, cred, http.MethodGet, fmt.Sprintf("/boards/%s/lists?filter=%s", boardID, url.QueryEscape(filter)), nil))
	case "create":
		name := stringValue(params, "name")
		boardID := stringValue(params, "boardId")
		if name == "" || boardID == "" {
			return nil, fmt.Errorf("name and boardId are required")
		}
		body := map[string]any{"name": name, "idBoard": boardID}
		pos := stringValue(params, "pos")
		if pos == "" {
			pos = "bottom"
		}
		body["pos"] = pos
		return single(n.doJSON(ctx, cred, http.MethodPost, "/lists", body))
	case "update":
		listID := stringValue(params, "listId")
		if listID == "" {
			return nil, fmt.Errorf("listId is required")
		}
		body := map[string]any{}
		setString(body, "name", stringValue(params, "name"))
		setString(body, "pos", stringValue(params, "pos"))
		return single(n.doJSON(ctx, cred, http.MethodPut, "/lists/"+listID, body))
	case "archive":
		listID := stringValue(params, "listId")
		if listID == "" {
			return nil, fmt.Errorf("listId is required")
		}
		return single(n.doJSON(ctx, cred, http.MethodPut, "/lists/"+listID, map[string]any{"closed": true}))
	default:
		return nil, fmt.Errorf("unknown list operation %s", operation)
	}
}
