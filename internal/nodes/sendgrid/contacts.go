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
		return itemsFromMaps(n.getContacts(ctx, cred, params))
	case "create", "update", "upsert":
		return single(n.upsertContact(ctx, cred, params))
	case "delete":
		return single(n.deleteContact(ctx, cred, params))
	default:
		return nil, fmt.Errorf("unknown contact operation %s", operation)
	}
}

func (n *Node) getContact(ctx context.Context, cred Credential, params map[string]any) (map[string]any, error) {
	if stringParam(params, "by") == "id" {
		id := stringParam(params, "contactId", "id")
		if id == "" {
			return nil, fmt.Errorf("contactId is required")
		}
		return n.doJSON(ctx, cred, http.MethodGet, "/marketing/contacts/"+url.PathEscape(id), nil)
	}
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

func (n *Node) getContacts(ctx context.Context, cred Credential, params map[string]any) ([]map[string]any, error) {
	filters := nestedMap(params, "filters")
	query := stringParam(filters, "query")
	var (
		result map[string]any
		err    error
	)
	if query != "" {
		result, err = n.doJSON(ctx, cred, http.MethodPost, "/marketing/contacts/search", map[string]any{"query": query})
	} else {
		result, err = n.doJSON(ctx, cred, http.MethodGet, "/marketing/contacts", nil)
	}
	if err != nil {
		return nil, err
	}
	contacts := listFrom(result, "result")
	if !boolParam(params, "returnAll") {
		limit := intParam(params, "limit")
		if limit > 0 && len(contacts) > limit {
			contacts = contacts[:limit]
		}
	}
	return contacts, nil
}

func (n *Node) upsertContact(ctx context.Context, cred Credential, params map[string]any) (map[string]any, error) {
	additional := nestedMap(params, "additionalFields")
	address := nestedMap(nestedMap(additional, "addressUi"), "addressValues")
	contact := Contact{
		Email:            stringParam(params, "email", "contactEmail"),
		FirstName:        firstText(stringParam(params, "firstName", "first_name"), stringParam(additional, "firstName")),
		LastName:         firstText(stringParam(params, "lastName", "last_name"), stringParam(additional, "lastName")),
		AddressLine1:     firstText(stringParam(params, "addressLine1", "address_line_1"), stringParam(address, "address1")),
		AddressLine2:     firstText(stringParam(params, "addressLine2", "address_line_2"), stringParam(address, "address2")),
		City:             firstText(stringParam(params, "city"), stringParam(additional, "city")),
		StateProvince:    firstText(stringParam(params, "stateProvince", "state_province_region"), stringParam(additional, "stateProvinceRegion")),
		Country:          firstText(stringParam(params, "country"), stringParam(additional, "country")),
		PostalCode:       firstText(stringParam(params, "postalCode", "postal_code"), stringParam(additional, "postalCode")),
		PhoneNumber:      stringParam(params, "phoneNumber", "phone_number"),
		UniqueExternalID: stringParam(params, "uniqueExternalId", "unique_name"),
		AlternateEmails:  stringSlice(additional, "alternateEmails"),
	}
	if contact.Email == "" {
		return nil, fmt.Errorf("email is required")
	}
	if custom, err := stringMapParam(params, "customFields"); err != nil {
		return nil, err
	} else {
		contact.CustomFields = custom
	}
	if len(contact.CustomFields) == 0 {
		contact.CustomFields = customFieldsFromUI(additional)
	}
	body := map[string]any{"contacts": []Contact{contact}}
	listIDs := stringSlice(params, "listIds")
	if len(listIDs) == 0 {
		listIDs = nestedStringSlice(nestedMap(additional, "listIdsUi"), "listIdValues", "listIds")
	}
	if len(listIDs) > 0 {
		body["list_ids"] = listIDs
	}
	return n.doJSON(ctx, cred, http.MethodPut, "/marketing/contacts", body)
}

func (n *Node) deleteContact(ctx context.Context, cred Credential, params map[string]any) (map[string]any, error) {
	if ids := strings.Join(stringSlice(params, "ids"), ","); ids != "" {
		query := url.Values{"ids": {strings.ReplaceAll(ids, " ", "")}}
		if boolParam(params, "deleteAll") {
			query.Set("delete_all_contacts", "true")
		}
		return n.doJSON(ctx, cred, http.MethodDelete, "/marketing/contacts?"+query.Encode(), nil)
	}
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

func customFieldsFromUI(params map[string]any) map[string]string {
	raw, ok := nestedMap(params, "customFieldsUi")["customFieldValues"].([]any)
	if !ok {
		return nil
	}
	out := map[string]string{}
	for _, item := range raw {
		field, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id := stringValue(field, "fieldId")
		if id != "" {
			out[id] = stringValue(field, "fieldValue")
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
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
