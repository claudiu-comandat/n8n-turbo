package sendgrid

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func (n *Node) handleList(ctx context.Context, cred Credential, operation string, params map[string]any) ([]dataplane.Item, error) {
	switch operation {
	case "get":
		return single(n.getList(ctx, cred, params))
	case "getAll", "list":
		return itemsFromMaps(n.getLists(ctx, cred, params))
	case "create":
		return single(n.createList(ctx, cred, params))
	case "update":
		return single(n.updateList(ctx, cred, params))
	case "addContacts", "addContact":
		return single(n.addContactsToList(ctx, cred, params))
	case "delete":
		return single(n.deleteList(ctx, cred, params))
	default:
		return nil, fmt.Errorf("unknown list operation %s", operation)
	}
}

func (n *Node) getList(ctx context.Context, cred Credential, params map[string]any) (map[string]any, error) {
	id := stringParam(params, "listId", "id")
	if id == "" {
		return nil, fmt.Errorf("listId is required")
	}
	path := "/marketing/lists/" + url.PathEscape(id)
	if boolParam(params, "contactSample") {
		path += "?contact_sample=true"
	}
	return n.doJSON(ctx, cred, http.MethodGet, path, nil)
}

func (n *Node) getLists(ctx context.Context, cred Credential, params map[string]any) ([]map[string]any, error) {
	pageSize := intParam(params, "pageSize")
	if pageSize <= 0 {
		pageSize = intParam(params, "limit")
		if pageSize <= 0 || boolParam(params, "returnAll") {
			pageSize = 1000
		}
	}
	result, err := n.doJSON(ctx, cred, http.MethodGet, fmt.Sprintf("/marketing/lists?page_size=%d", pageSize), nil)
	if err != nil {
		return nil, err
	}
	lists := listFrom(result, "result")
	if !boolParam(params, "returnAll") {
		limit := intParam(params, "limit")
		if limit > 0 && len(lists) > limit {
			lists = lists[:limit]
		}
	}
	return lists, nil
}

func (n *Node) createList(ctx context.Context, cred Credential, params map[string]any) (map[string]any, error) {
	name := stringParam(params, "name", "listName")
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	return n.doJSON(ctx, cred, http.MethodPost, "/marketing/lists", map[string]any{"name": name})
}

func (n *Node) updateList(ctx context.Context, cred Credential, params map[string]any) (map[string]any, error) {
	id := stringParam(params, "listId", "id")
	if id == "" {
		return nil, fmt.Errorf("listId is required")
	}
	name := stringParam(params, "name", "listName")
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	return n.doJSON(ctx, cred, http.MethodPatch, "/marketing/lists/"+url.PathEscape(id), map[string]any{"name": name})
}

func (n *Node) addContactsToList(ctx context.Context, cred Credential, params map[string]any) (map[string]any, error) {
	listID := stringParam(params, "listId", "id")
	if listID == "" {
		return nil, fmt.Errorf("listId is required")
	}
	emails := stringSlice(params, "emails")
	if len(emails) == 0 {
		email := stringParam(params, "email", "contactEmail")
		if email != "" {
			emails = []string{email}
		}
	}
	if len(emails) == 0 {
		return nil, fmt.Errorf("email or emails is required")
	}
	contacts := make([]Contact, 0, len(emails))
	for _, email := range emails {
		contacts = append(contacts, Contact{Email: email})
	}
	body := map[string]any{"list_ids": []string{listID}, "contacts": contacts}
	return n.doJSON(ctx, cred, http.MethodPut, "/marketing/contacts", body)
}

func (n *Node) deleteList(ctx context.Context, cred Credential, params map[string]any) (map[string]any, error) {
	id := stringParam(params, "listId", "id")
	if id == "" {
		return nil, fmt.Errorf("listId is required")
	}
	path := "/marketing/lists/" + url.PathEscape(id)
	if boolParam(params, "deleteContacts") {
		path += "?delete_contacts=true"
	}
	return n.doJSON(ctx, cred, http.MethodDelete, path, nil)
}
