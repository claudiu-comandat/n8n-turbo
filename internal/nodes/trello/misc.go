package trello

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func (n *Node) handleMember(ctx context.Context, cred Credential, operation string, params map[string]any) ([]dataplane.Item, error) {
	if operation != "getAll" && operation != "list" {
		return nil, fmt.Errorf("unknown member operation %s", operation)
	}
	boardID := stringValue(params, "boardId")
	cardID := stringValue(params, "cardId")
	query := url.Values{}
	query.Set("fields", "id,username,fullName,avatarUrl,email")
	if boardID != "" {
		return itemsFromArray(n.doArray(ctx, cred, http.MethodGet, "/boards/"+boardID+"/members?"+query.Encode(), nil))
	}
	if cardID != "" {
		return itemsFromArray(n.doArray(ctx, cred, http.MethodGet, "/cards/"+cardID+"/members?"+query.Encode(), nil))
	}
	return nil, fmt.Errorf("boardId or cardId is required")
}

func (n *Node) handleChecklist(ctx context.Context, cred Credential, operation string, params map[string]any) ([]dataplane.Item, error) {
	switch operation {
	case "get":
		id := stringValue(params, "checklistId")
		if id == "" {
			return nil, fmt.Errorf("checklistId is required")
		}
		return single(n.doJSON(ctx, cred, http.MethodGet, "/checklists/"+id+"?checkItems=all&checkItem_fields=name,state,pos", nil))
	case "create":
		cardID := stringValue(params, "cardId")
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
		id := stringValue(params, "checklistId")
		if id == "" {
			return nil, fmt.Errorf("checklistId is required")
		}
		_, err := n.doRaw(ctx, cred, http.MethodDelete, "/checklists/"+id, nil, "application/json")
		return single(map[string]any{"success": true}, err)
	case "createCheckItem":
		id := stringValue(params, "checklistId")
		name := stringValue(params, "name")
		if id == "" || name == "" {
			return nil, fmt.Errorf("checklistId and name are required")
		}
		body := map[string]any{"name": name, "checked": boolValue(params, "checked", false)}
		setString(body, "pos", stringValue(params, "pos"))
		return single(n.doJSON(ctx, cred, http.MethodPost, "/checklists/"+id+"/checkItems", body))
	default:
		return nil, fmt.Errorf("unknown checklist operation %s", operation)
	}
}

func (n *Node) handleLabel(ctx context.Context, cred Credential, operation string, params map[string]any) ([]dataplane.Item, error) {
	switch operation {
	case "getAll", "list":
		boardID := stringValue(params, "boardId")
		if boardID == "" {
			return nil, fmt.Errorf("boardId is required")
		}
		return itemsFromArray(n.doArray(ctx, cred, http.MethodGet, "/boards/"+boardID+"/labels?fields=id,name,color,idBoard", nil))
	case "create":
		boardID := stringValue(params, "boardId")
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
		id := stringValue(params, "labelId")
		if id == "" {
			return nil, fmt.Errorf("labelId is required")
		}
		_, err := n.doRaw(ctx, cred, http.MethodDelete, "/labels/"+id, nil, "application/json")
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
	cardID := stringValue(params, "cardId")
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
