package msteams

import (
	"context"
	"fmt"
	"net/http"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

func (n *Node) handleTask(ctx context.Context, cred *Credential, operation string, params map[string]any) ([]dataplane.Item, error) {
	switch operation {
	case "create":
		return single(n.createTask(ctx, cred, params))
	case "get":
		taskID := stringValue(params, "taskId")
		if taskID == "" {
			return nil, fmt.Errorf("taskId is required")
		}
		return single(n.doJSON(ctx, cred, http.MethodGet, "/planner/tasks/"+taskID, nil))
	case "getAll", "list":
		return itemsFromValue(n.getTasks(ctx, cred, params))
	case "update":
		return single(n.updateTask(ctx, cred, params))
	case "deleteTask", "delete":
		return single(n.deleteTask(ctx, cred, params))
	default:
		return nil, fmt.Errorf("unknown task operation %s", operation)
	}
}

func (n *Node) createTask(ctx context.Context, cred *Credential, params map[string]any) (map[string]any, error) {
	body := map[string]any{
		"planId":   stringValue(params, "planId"),
		"bucketId": stringValue(params, "bucketId"),
		"title":    stringValue(params, "title"),
	}
	if body["planId"] == "" || body["bucketId"] == "" || body["title"] == "" {
		return nil, fmt.Errorf("planId, bucketId, and title are required")
	}
	applyTaskOptions(body, mapParam(params, "options"))
	return n.doJSON(ctx, cred, http.MethodPost, "/planner/tasks", body)
}

func (n *Node) getTasks(ctx context.Context, cred *Credential, params map[string]any) (map[string]any, error) {
	if stringValue(params, "tasksFor") == "plan" {
		planID := stringValue(params, "planId")
		if planID == "" {
			return nil, fmt.Errorf("planId is required")
		}
		return n.doJSON(ctx, cred, http.MethodGet, "/planner/plans/"+planID+"/tasks", nil)
	}
	me, err := n.doJSON(ctx, cred, http.MethodGet, "/me", nil)
	if err != nil {
		return nil, err
	}
	userID := stringValue(me, "id")
	if userID == "" {
		return nil, fmt.Errorf("current user id not found")
	}
	return n.doJSON(ctx, cred, http.MethodGet, "/users/"+userID+"/planner/tasks", nil)
}

func (n *Node) updateTask(ctx context.Context, cred *Credential, params map[string]any) (map[string]any, error) {
	taskID := stringValue(params, "taskId")
	if taskID == "" {
		return nil, fmt.Errorf("taskId is required")
	}
	body := map[string]any{}
	applyTaskOptions(body, mapParam(params, "updateFields"))
	if len(body) == 0 {
		return nil, fmt.Errorf("updateFields is required")
	}
	task, err := n.doJSON(ctx, cred, http.MethodGet, "/planner/tasks/"+taskID, nil)
	if err != nil {
		return nil, err
	}
	etag := stringValue(task, "@odata.etag")
	if _, err := n.doJSONWithHeaders(ctx, cred, http.MethodPatch, "/planner/tasks/"+taskID, body, map[string]string{"If-Match": etag}); err != nil {
		return nil, err
	}
	return map[string]any{"success": true}, nil
}

func (n *Node) deleteTask(ctx context.Context, cred *Credential, params map[string]any) (map[string]any, error) {
	taskID := stringValue(params, "taskId")
	if taskID == "" {
		return nil, fmt.Errorf("taskId is required")
	}
	task, err := n.doJSON(ctx, cred, http.MethodGet, "/planner/tasks/"+taskID, nil)
	if err != nil {
		return nil, err
	}
	etag := stringValue(task, "@odata.etag")
	return n.doJSONWithHeaders(ctx, cred, http.MethodDelete, "/planner/tasks/"+taskID, nil, map[string]string{"If-Match": etag})
}

func applyTaskOptions(body map[string]any, options map[string]any) {
	for key, value := range options {
		switch key {
		case "assignedTo":
			assignedTo := textValue(value)
			if assignedTo != "" {
				body["assignments"] = map[string]any{
					assignedTo: map[string]any{"@odata.type": "microsoft.graph.plannerAssignment", "orderHint": " !"},
				}
			}
		case "groupId":
			continue
		case "planId", "bucketId":
			if text := textValue(value); text != "" {
				body[key] = text
			}
		default:
			body[key] = value
		}
	}
}
