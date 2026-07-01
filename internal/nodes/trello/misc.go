package trello

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func (n *Node) handleMember(ctx context.Context, cred Credential, operation string, params map[string]any) ([]dataplane.Item, error) {
	boardID := stringValue(params, "boardId", "id")
	cardID := stringValue(params, "cardId", "idCard")
	switch operation {
	case "getAll", "list":
		query := url.Values{}
		query.Set("fields", "id,username,fullName,avatarUrl,email")
		if boardID != "" {
			return itemsFromArray(n.doArray(ctx, cred, http.MethodGet, "/boards/"+boardID+"/members?"+query.Encode(), nil))
		}
		if cardID != "" {
			return itemsFromArray(n.doArray(ctx, cred, http.MethodGet, "/cards/"+cardID+"/members?"+query.Encode(), nil))
		}
		return nil, fmt.Errorf("boardId or cardId is required")
	case "add":
		memberID := stringValue(params, "memberId", "idMember")
		if boardID == "" || memberID == "" {
			return nil, fmt.Errorf("boardId and idMember are required")
		}
		query := url.Values{}
		memberType := stringValue(params, "type")
		if memberType == "" {
			memberType = "normal"
		}
		query.Set("type", memberType)
		if extra, err := mapParam(params, "additionalFields"); err != nil {
			return nil, err
		} else {
			for key, value := range extra {
				query.Set(key, textValue(value))
			}
		}
		return single(n.doJSON(ctx, cred, http.MethodPut, "/boards/"+boardID+"/members/"+memberID+"?"+query.Encode(), nil))
	case "invite":
		email := stringValue(params, "email")
		if boardID == "" || email == "" {
			return nil, fmt.Errorf("boardId and email are required")
		}
		body := map[string]any{"email": email}
		if extra, err := mapParam(params, "additionalFields"); err != nil {
			return nil, err
		} else {
			for key, value := range extra {
				body[key] = value
			}
		}
		return single(n.doJSON(ctx, cred, http.MethodPut, "/boards/"+boardID+"/members", body))
	case "remove":
		memberID := stringValue(params, "memberId", "idMember")
		if boardID == "" || memberID == "" {
			return nil, fmt.Errorf("boardId and idMember are required")
		}
		_, err := n.doRaw(ctx, cred, http.MethodDelete, "/boards/"+boardID+"/members/"+memberID, nil, "application/json")
		return single(map[string]any{"success": true}, err)
	default:
		return nil, fmt.Errorf("unknown member operation %s", operation)
	}
}

func (n *Node) handleChecklist(ctx context.Context, cred Credential, operation string, params map[string]any) ([]dataplane.Item, error) {
	switch operation {
	case "getAll", "list":
		cardID := stringValue(params, "cardId", "idCard")
		if cardID == "" {
			return nil, fmt.Errorf("cardId is required")
		}
		query := url.Values{}
		if extra, err := mapParam(params, "additionalFields"); err != nil {
			return nil, err
		} else {
			for key, value := range extra {
				query.Set(key, textValue(value))
			}
		}
		path := "/cards/" + cardID + "/checklists"
		if encoded := query.Encode(); encoded != "" {
			path += "?" + encoded
		}
		return itemsFromArray(n.doArray(ctx, cred, http.MethodGet, path, nil))
	case "get":
		id := stringValue(params, "checklistId", "id")
		if id == "" {
			return nil, fmt.Errorf("checklistId is required")
		}
		return single(n.doJSON(ctx, cred, http.MethodGet, "/checklists/"+id+"?checkItems=all&checkItem_fields=name,state,pos", nil))
	case "create":
		cardID := stringValue(params, "cardId", "idCard")
		if cardID == "" {
			return nil, fmt.Errorf("cardId is required")
		}
		name := stringValue(params, "name")
		if name == "" {
			name = "Checklist"
		}
		body := map[string]any{"idCard": cardID, "name": name}
		setString(body, "pos", stringValue(params, "pos"))
		setString(body, "idChecklistSource", stringValue(params, "idChecklistSource"))
		return single(n.doJSON(ctx, cred, http.MethodPost, "/checklists", body))
	case "delete":
		id := stringValue(params, "checklistId", "id")
		if id == "" {
			return nil, fmt.Errorf("checklistId is required")
		}
		_, err := n.doRaw(ctx, cred, http.MethodDelete, "/checklists/"+id, nil, "application/json")
		return single(map[string]any{"success": true}, err)
	case "createCheckItem":
		id := stringValue(params, "checklistId", "id")
		name := stringValue(params, "name")
		if id == "" || name == "" {
			return nil, fmt.Errorf("checklistId and name are required")
		}
		body := map[string]any{"name": name, "checked": boolValue(params, "checked", false)}
		setString(body, "pos", stringValue(params, "pos"))
		return single(n.doJSON(ctx, cred, http.MethodPost, "/checklists/"+id+"/checkItems", body))
	case "deleteCheckItem":
		id := stringValue(params, "checklistId", "idChecklist")
		checkItemID := stringValue(params, "checkItemId", "idCheckItem")
		if id == "" || checkItemID == "" {
			return nil, fmt.Errorf("checklistId and checkItemId are required")
		}
		_, err := n.doRaw(ctx, cred, http.MethodDelete, "/checklists/"+id+"/checkItems/"+checkItemID, nil, "application/json")
		return single(map[string]any{"success": true}, err)
	case "getCheckItem":
		id := stringValue(params, "checklistId", "idChecklist")
		checkItemID := stringValue(params, "checkItemId", "idCheckItem")
		if id == "" || checkItemID == "" {
			return nil, fmt.Errorf("checklistId and checkItemId are required")
		}
		return single(n.doJSON(ctx, cred, http.MethodGet, "/checklists/"+id+"/checkItems/"+checkItemID, nil))
	case "updateCheckItem":
		cardID := stringValue(params, "cardId", "idCard")
		checkItemID := stringValue(params, "checkItemId", "idCheckItem")
		if cardID == "" || checkItemID == "" {
			return nil, fmt.Errorf("cardId and checkItemId are required")
		}
		body := map[string]any{}
		if fields, err := mapParam(params, "additionalFields"); err != nil {
			return nil, err
		} else {
			for key, value := range fields {
				body[key] = value
			}
		}
		setString(body, "name", stringValue(params, "name"))
		setString(body, "state", stringValue(params, "state"))
		if len(body) == 0 {
			return nil, fmt.Errorf("at least one check item field is required")
		}
		return single(n.doJSON(ctx, cred, http.MethodPut, "/cards/"+cardID+"/checkItem/"+checkItemID, body))
	case "completedCheckItems", "completeCheckItems":
		cardID := stringValue(params, "cardId", "idCard")
		if cardID == "" {
			return nil, fmt.Errorf("cardId is required")
		}
		return itemsFromArray(n.doArray(ctx, cred, http.MethodGet, "/cards/"+cardID+"/checkItemStates", nil))
	default:
		return nil, fmt.Errorf("unknown checklist operation %s", operation)
	}
}

func (n *Node) handleLabel(ctx context.Context, cred Credential, operation string, params map[string]any) ([]dataplane.Item, error) {
	switch operation {
	case "getAll", "list":
		boardID := stringValue(params, "boardId", "idBoard")
		if boardID == "" {
			return nil, fmt.Errorf("boardId is required")
		}
		return itemsFromArray(n.doArray(ctx, cred, http.MethodGet, "/boards/"+boardID+"/labels?fields=id,name,color,idBoard", nil))
	case "create":
		boardID := stringValue(params, "boardId", "idBoard")
		name := stringValue(params, "name")
		if boardID == "" {
			return nil, fmt.Errorf("boardId is required")
		}
		color := stringValue(params, "color")
		if color == "" {
			color = "null"
		}
		if !validLabelColor(color) {
			return nil, fmt.Errorf("invalid label color %s", color)
		}
		return single(n.doJSON(ctx, cred, http.MethodPost, "/labels", map[string]any{"idBoard": boardID, "name": name, "color": color}))
	case "delete":
		id := stringValue(params, "labelId", "id")
		if id == "" {
			return nil, fmt.Errorf("labelId is required")
		}
		_, err := n.doRaw(ctx, cred, http.MethodDelete, "/labels/"+id, nil, "application/json")
		return single(map[string]any{"success": true}, err)
	case "get":
		id := stringValue(params, "labelId", "id")
		if id == "" {
			return nil, fmt.Errorf("labelId is required")
		}
		return single(n.doJSON(ctx, cred, http.MethodGet, "/labels/"+id, nil))
	case "update":
		id := stringValue(params, "labelId", "id")
		if id == "" {
			return nil, fmt.Errorf("labelId is required")
		}
		body := map[string]any{}
		if fields, err := mapParam(params, "updateFields"); err != nil {
			return nil, err
		} else {
			for key, value := range fields {
				body[key] = value
			}
		}
		setString(body, "name", stringValue(params, "name"))
		setString(body, "color", stringValue(params, "color"))
		return single(n.doJSON(ctx, cred, http.MethodPut, "/labels/"+id, body))
	case "addLabel":
		cardID := stringValue(params, "cardId", "idCard")
		labelID := stringValue(params, "labelId", "id")
		if cardID == "" || labelID == "" {
			return nil, fmt.Errorf("cardId and label id are required")
		}
		return single(n.doJSON(ctx, cred, http.MethodPost, "/cards/"+cardID+"/idLabels", map[string]any{"value": labelID}))
	case "removeLabel":
		cardID := stringValue(params, "cardId", "idCard")
		labelID := stringValue(params, "labelId", "id")
		if cardID == "" || labelID == "" {
			return nil, fmt.Errorf("cardId and label id are required")
		}
		_, err := n.doRaw(ctx, cred, http.MethodDelete, "/cards/"+cardID+"/idLabels/"+labelID, nil, "application/json")
		return single(map[string]any{"success": true}, err)
	default:
		return nil, fmt.Errorf("unknown label operation %s", operation)
	}
}

func validLabelColor(color string) bool {
	switch color {
	case "yellow", "purple", "blue", "red", "green", "orange", "black", "sky", "pink", "lime", "null":
		return true
	default:
		return false
	}
}

func (n *Node) handleComment(ctx context.Context, cred Credential, operation string, params map[string]any) ([]dataplane.Item, error) {
	cardID := stringValue(params, "cardId", "idCard")
	if cardID == "" {
		return nil, fmt.Errorf("cardId is required")
	}
	switch operation {
	case "getAll", "list":
		return itemsFromArray(n.doArray(ctx, cred, http.MethodGet, "/cards/"+cardID+"/actions?filter=commentCard", nil))
	case "create":
		text := stringValue(params, "text", "comment")
		if text == "" {
			return nil, fmt.Errorf("text is required")
		}
		return single(n.doJSON(ctx, cred, http.MethodPost, "/cards/"+cardID+"/actions/comments", map[string]any{"text": text}))
	case "update":
		commentID := stringValue(params, "commentId", "actionId")
		text := stringValue(params, "text", "comment")
		if commentID == "" || text == "" {
			return nil, fmt.Errorf("commentId and text are required")
		}
		return single(n.doJSON(ctx, cred, http.MethodPut, "/cards/"+cardID+"/actions/"+commentID+"/comments", map[string]any{"text": text}))
	case "delete":
		commentID := stringValue(params, "commentId", "actionId")
		if commentID == "" {
			return nil, fmt.Errorf("commentId is required")
		}
		_, err := n.doRaw(ctx, cred, http.MethodDelete, "/cards/"+cardID+"/actions/"+commentID+"/comments", nil, "application/json")
		return single(map[string]any{"success": true}, err)
	default:
		return nil, fmt.Errorf("unknown comment operation %s", operation)
	}
}
