package trello

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func (n *Node) handleCard(ctx context.Context, cred Credential, operation string, params map[string]any, item dataplane.Item) ([]dataplane.Item, error) {
	switch operation {
	case "get":
		cardID := stringValue(params, "cardId", "id")
		if cardID == "" {
			return nil, fmt.Errorf("cardId is required")
		}
		query := url.Values{}
		fields := stringValue(params, "fields")
		if fields == "" {
			fields = "id,name,desc,due,dueComplete,closed,idList,idBoard,idMembers,labels,url,shortUrl"
		}
		query.Set("fields", fields)
		if boolValue(params, "includeMembers", false) {
			query.Set("members", "true")
		}
		if boolValue(params, "includeAttachments", false) {
			query.Set("attachments", "true")
		}
		if boolValue(params, "includeChecklists", false) {
			query.Set("checklists", "all")
		}
		return single(n.doJSON(ctx, cred, http.MethodGet, fmt.Sprintf("/cards/%s?%s", cardID, query.Encode()), nil))
	case "getAll", "list":
		return n.listCards(ctx, cred, params)
	case "create":
		return single(n.createCard(ctx, cred, params, item))
	case "update":
		return single(n.updateCard(ctx, cred, params))
	case "delete":
		cardID := stringValue(params, "cardId", "id")
		if cardID == "" {
			return nil, fmt.Errorf("cardId is required")
		}
		_, err := n.doRaw(ctx, cred, http.MethodDelete, "/cards/"+cardID, nil, "application/json")
		return single(map[string]any{"success": true, "id": cardID}, err)
	case "move":
		return single(n.moveCard(ctx, cred, params))
	case "archive":
		cardID := stringValue(params, "cardId", "id")
		if cardID == "" {
			return nil, fmt.Errorf("cardId is required")
		}
		return single(n.doJSON(ctx, cred, http.MethodPut, "/cards/"+cardID, map[string]any{"closed": boolValue(params, "archive", true)}))
	default:
		return nil, fmt.Errorf("unknown card operation %s", operation)
	}
}

func (n *Node) listCards(ctx context.Context, cred Credential, params map[string]any) ([]dataplane.Item, error) {
	listID := stringValue(params, "listId", "idList")
	boardID := stringValue(params, "boardId", "idBoard")
	if listID == "" && boardID == "" {
		return nil, fmt.Errorf("either listId or boardId is required")
	}
	path := "/lists/" + listID + "/cards"
	if boardID != "" {
		query := url.Values{}
		filter := stringValue(params, "filter")
		if filter == "" {
			filter = "open"
		}
		query.Set("filter", filter)
		path = "/boards/" + boardID + "/cards?" + query.Encode()
	}
	return itemsFromArray(n.doArray(ctx, cred, http.MethodGet, path, nil))
}

func (n *Node) createCard(ctx context.Context, cred Credential, params map[string]any, item dataplane.Item) (map[string]any, error) {
	body := map[string]any{}
	if raw, err := mapParam(params, "card"); err != nil {
		return nil, err
	} else if raw != nil {
		body = raw
	}
	if body["idList"] == nil {
		body["idList"] = stringValue(params, "listId", "idList")
	}
	if body["name"] == nil {
		body["name"] = stringValue(params, "name")
	}
	if body["name"] == "" || body["idList"] == "" {
		return nil, fmt.Errorf("listId and name are required")
	}
	setString(body, "desc", stringValue(params, "description", "desc"))
	setString(body, "due", stringValue(params, "due"))
	setString(body, "pos", stringValue(params, "pos"))
	setString(body, "idLabels", stringValue(params, "idLabels"))
	setString(body, "idMembers", stringValue(params, "idMembers"))
	setString(body, "urlSource", stringValue(params, "urlSource"))
	return n.doJSON(ctx, cred, http.MethodPost, "/cards", body)
}

func (n *Node) updateCard(ctx context.Context, cred Credential, params map[string]any) (map[string]any, error) {
	cardID := stringValue(params, "cardId", "id")
	if cardID == "" {
		return nil, fmt.Errorf("cardId is required")
	}
	body := map[string]any{}
	if raw, err := mapParam(params, "updateFields"); err != nil {
		return nil, err
	} else if raw != nil {
		body = raw
	}
	for _, key := range []string{"name", "desc", "due", "dueComplete", "pos", "closed", "idList", "idBoard"} {
		if value, ok := params[key]; ok {
			body[key] = value
		}
	}
	return n.doJSON(ctx, cred, http.MethodPut, "/cards/"+cardID, body)
}

func (n *Node) moveCard(ctx context.Context, cred Credential, params map[string]any) (map[string]any, error) {
	cardID := stringValue(params, "cardId", "id")
	newListID := stringValue(params, "newListId", "idList")
	if cardID == "" || newListID == "" {
		return nil, fmt.Errorf("cardId and newListId are required")
	}
	body := map[string]any{"idList": newListID}
	pos := stringValue(params, "pos")
	if pos == "" {
		pos = "bottom"
	}
	body["pos"] = pos
	setString(body, "idBoard", stringValue(params, "newBoardId"))
	return n.doJSON(ctx, cred, http.MethodPut, "/cards/"+cardID, body)
}
