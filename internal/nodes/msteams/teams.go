package msteams

import (
	"context"
	"fmt"
	"net/http"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func (n *Node) handleTeam(ctx context.Context, cred *Credential, operation string, params map[string]any) ([]dataplane.Item, error) {
	switch operation {
	case "getAll", "list":
		return itemsFromValue(n.doJSON(ctx, cred, http.MethodGet, "/me/joinedTeams", nil))
	case "get":
		teamID := stringValue(params, "teamId")
		if teamID == "" {
			return nil, fmt.Errorf("teamId is required")
		}
		return single(n.doJSON(ctx, cred, http.MethodGet, "/teams/"+teamID, nil))
	default:
		return nil, fmt.Errorf("unknown team operation %s", operation)
	}
}

func (n *Node) handleChannel(ctx context.Context, cred *Credential, operation string, params map[string]any) ([]dataplane.Item, error) {
	teamID := stringValue(params, "teamId")
	if teamID == "" {
		return nil, fmt.Errorf("teamId is required")
	}
	switch operation {
	case "getAll", "list":
		return itemsFromValue(n.doJSON(ctx, cred, http.MethodGet, fmt.Sprintf("/teams/%s/channels", teamID), nil))
	case "get":
		channelID := stringValue(params, "channelId")
		if channelID == "" {
			return nil, fmt.Errorf("channelId is required")
		}
		return single(n.doJSON(ctx, cred, http.MethodGet, fmt.Sprintf("/teams/%s/channels/%s", teamID, channelID), nil))
	case "create":
		return single(n.createChannel(ctx, cred, teamID, params))
	case "update":
		return single(n.updateChannel(ctx, cred, teamID, params))
	case "deleteChannel", "delete":
		channelID := stringValue(params, "channelId")
		if channelID == "" {
			return nil, fmt.Errorf("channelId is required")
		}
		return single(n.doJSON(ctx, cred, http.MethodDelete, fmt.Sprintf("/teams/%s/channels/%s", teamID, channelID), nil))
	default:
		return nil, fmt.Errorf("unknown channel operation %s", operation)
	}
}

func (n *Node) createChannel(ctx context.Context, cred *Credential, teamID string, params map[string]any) (map[string]any, error) {
	name := stringValue(params, "name", "displayName")
	if name == "" {
		return nil, fmt.Errorf("channel name is required")
	}
	membershipType := stringValue(params, "membershipType")
	if membershipType == "" {
		membershipType = "standard"
	}
	body := map[string]any{"displayName": name, "membershipType": membershipType}
	if description := stringValue(params, "description"); description != "" {
		body["description"] = description
	}
	if membershipType == "private" {
		owners := stringSlice(params, "ownerUserIds")
		if len(owners) == 0 {
			return nil, fmt.Errorf("private channels require at least one owner")
		}
		members := make([]map[string]any, 0, len(owners))
		for _, owner := range owners {
			members = append(members, conversationMember(owner))
		}
		body["members"] = members
	}
	return n.doJSON(ctx, cred, http.MethodPost, fmt.Sprintf("/teams/%s/channels", teamID), body)
}

func (n *Node) updateChannel(ctx context.Context, cred *Credential, teamID string, params map[string]any) (map[string]any, error) {
	channelID := stringValue(params, "channelId")
	if channelID == "" {
		return nil, fmt.Errorf("channelId is required")
	}
	body := map[string]any{}
	if name := stringValue(params, "name", "displayName"); name != "" {
		body["displayName"] = name
	}
	if description := stringValue(mapParam(params, "options"), "description"); description != "" {
		body["description"] = description
	} else if description := stringValue(params, "description"); description != "" {
		body["description"] = description
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("name or description is required")
	}
	if _, err := n.doJSON(ctx, cred, http.MethodPatch, fmt.Sprintf("/teams/%s/channels/%s", teamID, channelID), body); err != nil {
		return nil, err
	}
	return map[string]any{"success": true}, nil
}
