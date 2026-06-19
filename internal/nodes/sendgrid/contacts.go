package sendgrid

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func (n *Node) handleContact(ctx context.Context, cred Credential, operation string, params map[string]any) ([]dataplane.Item, error) {
	switch operation {
	case "get":
		return single(n.getContact(ctx, cred, params))
	case "getAll", "list":
		return single(n.exportContacts(ctx, cred, params))
	case "create", "update", "upsert":
		return single(n.upsertContact(ctx, cred, params))
	case "delete":
		return single(n.deleteContact(ctx, cred, params))
	default:
		return nil, fmt.Errorf("unknown contact operation %s", operation)
	}
}

func (n *Node) getContact(ctx context.Context, cred Credential, params map[string]any) (map[string]any, error) {
	email := stringParam(params, "email", "contactEmail")
	if email == "" {
		return nil, fmt.Errorf("email is required")
	}
	id, contact, err := n.findContactByEmail(ctx, cred, email)
	if err != nil {
		return nil, err
	}
	if id != "" {
		contact["id"] = id
	}
	return contact, nil
}

func (n *Node) exportContacts(ctx context.Context, cred Credential, params map[string]any) (map[string]any, error) {
	body := map[string]any{}
	if listIDs := stringSlice(params, "listIds"); len(listIDs) > 0 {
		body["list_ids"] = listIDs
	}
	return n.doJSON(ctx, cred, http.MethodPost, "/marketing/contacts/exports", body)
}

func (n *Node) upsertContact(ctx context.Context, cred Credential, params map[string]any) (map[string]any, error) {
	contact := Contact{
		Email:            stringParam(params, "email", "contactEmail"),
		FirstName:        stringParam(params, "firstName", "first_name"),
		LastName:         stringParam(params, "lastName", "last_name"),
		AddressLine1:     stringParam(params, "addressLine1", "address_line_1"),
		AddressLine2:     stringParam(params, "addressLine2", "address_line_2"),
		City:             stringParam(params, "city"),
		StateProvince:    stringParam(params, "stateProvince", "state_province_region"),
		Country:          stringParam(params, "country"),
		PostalCode:       stringParam(params, "postalCode", "postal_code"),
		PhoneNumber:      stringParam(params, "phoneNumber", "phone_number"),
		UniqueExternalID: stringParam(params, "uniqueExternalId", "unique_name"),
	}
	if contact.Email == "" {
		return nil, fmt.Errorf("email is required")
	}
	if custom, err := stringMapParam(params, "customFields"); err != nil {
		return nil, err
	} else {
		contact.CustomFields = custom
	}
	body := map[string]any{"contacts": []Contact{contact}}
	if listIDs := stringSlice(params, "listIds"); len(listIDs) > 0 {
		body["list_ids"] = listIDs
	}
	return n.doJSON(ctx, cred, http.MethodPut, "/marketing/contacts", body)
}

func (n *Node) deleteContact(ctx context.Context, cred Credential, params map[string]any) (map[string]any, error) {
	id := stringParam(params, "contactId", "id")
	if id == "" {
		email := stringParam(params, "email", "contactEmail")
		if email == "" {
			return nil, fmt.Errorf("contactId or email is required")
		}
		found, _, err := n.findContactByEmail(ctx, cred, email)
		if err != nil {
			return nil, err
		}
		id = found
	}
	if id == "" {
		return nil, fmt.Errorf("contact not found")
	}
	return n.doJSON(ctx, cred, http.MethodDelete, "/marketing/contacts?ids="+url.QueryEscape(id), nil)
}

func (n *Node) findContactByEmail(ctx context.Context, cred Credential, email string) (string, map[string]any, error) {
	body := map[string]any{"query": "email LIKE '" + strings.ReplaceAll(email, "'", "\\'") + "'"}
	result, err := n.doJSON(ctx, cred, http.MethodPost, "/marketing/contacts/search", body)
	if err != nil {
		return "", nil, err
	}
	results, _ := result["result"].([]any)
	if len(results) == 0 {
		return "", nil, fmt.Errorf("contact not found")
	}
	contact, ok := results[0].(map[string]any)
	if !ok {
		return "", nil, fmt.Errorf("invalid contact result")
	}
	id := stringValue(contact, "id")
	return id, contact, nil
}
