package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/persistence"
)

// workflowTags returns the frontend-shaped tag list for a single workflow.
func (s *Server) workflowTags(ctx context.Context, workflowID string) []map[string]any {
	out := []map[string]any{}
	if s.tagStore == nil || workflowID == "" || workflowID == "new" || workflowID == "demo" {
		return out
	}
	tags, err := s.tagStore.ListTagsForWorkflow(ctx, workflowID)
	if err != nil {
		return out
	}
	for _, tag := range tags {
		out = append(out, map[string]any{
			"id":        tag.ID,
			"name":      tag.Name,
			"createdAt": tag.CreatedAt.Format(time.RFC3339Nano),
			"updatedAt": tag.UpdatedAt.Format(time.RFC3339Nano),
		})
	}
	return out
}

// resolveTagPayload maps a workflow-save `tags` payload to tag ids. The payload
// may be an array of ids, an array of names, or an array of {id,name} objects.
// Unknown names are created on the fly so tagging stays "brain-dead simple" for
// both the UI and AI callers.
func (s *Server) resolveTagPayload(ctx context.Context, raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || s.tagStore == nil {
		return nil, nil
	}
	existing, err := s.tagStore.List(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[string]string{}
	byName := map[string]string{}
	for _, tag := range existing {
		byID[tag.ID] = tag.ID
		byName[strings.ToLower(tag.Name)] = tag.ID
	}

	type tagToken struct {
		ID   string
		Name string
	}
	tokens := []tagToken{}
	var asStrings []string
	if err := json.Unmarshal(raw, &asStrings); err == nil {
		for _, value := range asStrings {
			tokens = append(tokens, tagToken{ID: value})
		}
	} else {
		var asObjects []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(raw, &asObjects); err != nil {
			return nil, nil
		}
		for _, object := range asObjects {
			tokens = append(tokens, tagToken{ID: object.ID, Name: object.Name})
		}
	}

	result := []string{}
	seen := map[string]bool{}
	for _, token := range tokens {
		id := ""
		switch {
		case token.ID != "" && byID[token.ID] != "":
			id = token.ID
		case token.Name != "" && byName[strings.ToLower(token.Name)] != "":
			id = byName[strings.ToLower(token.Name)]
		default:
			name := strings.TrimSpace(firstNonEmpty(token.Name, token.ID))
			if name == "" {
				continue
			}
			if existingID := byName[strings.ToLower(name)]; existingID != "" {
				id = existingID
			} else {
				created, err := s.tagStore.Save(ctx, persistence.TagRow{Name: name})
				if err != nil {
					return nil, err
				}
				id = created.ID
				byID[id] = id
				byName[strings.ToLower(created.Name)] = id
			}
		}
		if id != "" && !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}
	return result, nil
}

// filterWorkflowRowsByTag narrows rows to those tagged with any of the tags in
// the `tags`/`tagIds`/`tag` query param. Tokens may be tag ids or tag names.
func (s *Server) filterWorkflowRowsByTag(ctx context.Context, rows []persistence.WorkflowRow, r *http.Request) []persistence.WorkflowRow {
	tagFilter := strings.TrimSpace(firstNonEmpty(r.URL.Query().Get("tags"), r.URL.Query().Get("tagIds"), r.URL.Query().Get("tag")))
	if tagFilter == "" || s.tagStore == nil {
		return rows
	}
	existing, err := s.tagStore.List(ctx)
	if err != nil {
		return rows
	}
	idSet := map[string]bool{}
	nameToID := map[string]string{}
	for _, tag := range existing {
		idSet[tag.ID] = true
		nameToID[strings.ToLower(tag.Name)] = tag.ID
	}
	allowed := map[string]bool{}
	for _, token := range splitQueryValues(tagFilter) {
		id := token
		if !idSet[token] {
			mapped := nameToID[strings.ToLower(token)]
			if mapped == "" {
				continue
			}
			id = mapped
		}
		ids, err := s.tagStore.WorkflowIDsByTag(ctx, id)
		if err != nil {
			continue
		}
		for _, workflowID := range ids {
			allowed[workflowID] = true
		}
	}
	result := make([]persistence.WorkflowRow, 0, len(rows))
	for _, row := range rows {
		if allowed[row.ID] {
			result = append(result, row)
		}
	}
	return result
}
