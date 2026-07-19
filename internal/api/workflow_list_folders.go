package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/n8n-io/n8n-turbo/internal/persistence"
)

// usesFolderAwareList detects the n8n editor UI's list request (skip/take/filter/
// includeFolders) so we can serve the folder-interleaved, paginated envelope.
func (s *Server) usesFolderAwareList(r *http.Request) bool {
	q := r.URL.Query()
	return q.Has("take") || q.Has("skip") || q.Has("includeFolders") || strings.TrimSpace(q.Get("filter")) != ""
}

// handleListWorkflowsWithFolders serves GET /rest/workflows the way n8n 2.16 expects
// when folders are enabled: a single {data,count} page that interleaves folder items
// (resource:"folder") and workflow items (resource:"workflow") for the current level.
func (s *Server) handleListWorkflowsWithFolders(w http.ResponseWriter, r *http.Request) {
	filter := parseJSONQueryObject(r, "filter")
	projectID := stringFromAny(filter["projectId"])
	if projectID == "" {
		projectID = stringFromAny(s.personalProject(r)["id"])
	}
	includeFolders := boolQueryParam(r, "includeFolders")
	nameFilter := strings.ToLower(strings.TrimSpace(stringFromAny(filter["query"])))
	parentFilter, hasParent := filter["parentFolderId"].(string)
	var activeFilter *bool
	if value, ok := filter["active"].(bool); ok {
		activeFilter = &value
	}
	isArchivedFilter := false
	if value, ok := filter["isArchived"].(bool); ok {
		isArchivedFilter = value
	}
	tagFilter := stringsFromAnySlice(filter["tags"])

	rows := []persistence.WorkflowRow{}
	_ = s.eachWorkflowRow(r.Context(), func(row persistence.WorkflowRow) {
		rows = append(rows, row)
	})

	parentMap := map[string]string{}
	if store := s.workflowFolders(); store != nil {
		ids := make([]string, 0, len(rows))
		for i := range rows {
			ids = append(ids, rows[i].ID)
		}
		if loaded, err := store.ParentFoldersFor(r.Context(), ids); err == nil {
			parentMap = loaded
		}
	}

	allowedByTag := map[string]bool{}
	if len(tagFilter) > 0 && s.tagStore != nil {
		for _, tagID := range tagFilter {
			if ids, err := s.tagStore.WorkflowIDsByTag(r.Context(), tagID); err == nil {
				for _, id := range ids {
					allowedByTag[id] = true
				}
			}
		}
	}

	kept := make([]persistence.WorkflowRow, 0, len(rows))
	for _, row := range rows {
		if isArchivedFilter {
			// No workflow is archived (archive not modelled here), so the archived tab is empty.
			continue
		}
		parentID := parentMap[row.ID]
		if hasParent {
			if parentFilter == rootFolderSentinel {
				if parentID != "" {
					continue
				}
			} else if parentID != parentFilter {
				continue
			}
		}
		if activeFilter != nil && row.Active != *activeFilter {
			continue
		}
		if nameFilter != "" && !strings.Contains(strings.ToLower(row.Name), nameFilter) {
			continue
		}
		if len(tagFilter) > 0 && !allowedByTag[row.ID] {
			continue
		}
		kept = append(kept, row)
	}

	sortWorkflowRows(kept, r.URL.Query().Get("sortBy"))
	s.decorateWorkflowRowsForFrontend(r.Context(), kept)

	byID, childCount := s.loadFolderContext(r.Context(), projectID)

	items := []map[string]any{}
	if includeFolders && hasParent {
		level := parentFilter
		folders := make([]persistence.FolderRow, 0, len(byID))
		for _, folder := range byID {
			if level == rootFolderSentinel {
				if folder.ParentFolderID != nil {
					continue
				}
			} else if folder.ParentFolderID == nil || *folder.ParentFolderID != level {
				continue
			}
			folders = append(folders, folder)
		}
		sort.Slice(folders, func(i, j int) bool { return folders[i].Name < folders[j].Name })
		for _, folder := range folders {
			items = append(items, s.buildFolderResponse(r.Context(), r, folder, byID, childCount))
		}
	}
	for i := range kept {
		items = append(items, s.workflowListItem(r, kept[i], parentMap[kept[i].ID], byID))
	}

	total := len(items)
	writeJSON(w, http.StatusOK, map[string]any{"data": paginateMaps(items, r), "count": total})
}

func (s *Server) workflowListItem(r *http.Request, row persistence.WorkflowRow, parentID string, byID map[string]persistence.FolderRow) map[string]any {
	data, _ := json.Marshal(row)
	item := map[string]any{}
	_ = json.Unmarshal(data, &item)
	item["resource"] = "workflow"
	item["isArchived"] = false
	item["homeProject"] = s.homeProjectSummary(r)
	item["parentFolder"] = s.parentFolderSummary(parentID, byID)
	return item
}

func (s *Server) parentFolderSummary(parentID string, byID map[string]persistence.FolderRow) any {
	if parentID == "" {
		return nil
	}
	if folder, ok := byID[parentID]; ok {
		return map[string]any{"id": folder.ID, "name": folder.Name, "parentFolderId": strPtrOrNil(folder.ParentFolderID)}
	}
	return map[string]any{"id": parentID, "name": "", "parentFolderId": nil}
}

func sortWorkflowRows(rows []persistence.WorkflowRow, sortBy string) {
	field, direction := "updatedAt", "desc"
	if parts := strings.SplitN(strings.TrimSpace(sortBy), ":", 2); parts[0] != "" {
		field = parts[0]
		if len(parts) == 2 && parts[1] != "" {
			direction = strings.ToLower(parts[1])
		}
	}
	less := func(i, j int) bool {
		switch field {
		case "name":
			return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name)
		case "createdAt":
			return rows[i].CreatedAt.Before(rows[j].CreatedAt)
		default:
			return rows[i].UpdatedAt.Before(rows[j].UpdatedAt)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if direction == "asc" {
			return less(i, j)
		}
		return less(j, i)
	})
}

func boolQueryParam(r *http.Request, key string) bool {
	return strings.EqualFold(strings.TrimSpace(r.URL.Query().Get(key)), "true")
}

func stringsFromAnySlice(value any) []string {
	result := []string{}
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				result = append(result, text)
			}
		}
	case string:
		if strings.TrimSpace(typed) != "" {
			result = append(result, typed)
		}
	}
	return result
}

// applyWorkflowParentFolder persists a workflow's folder from a save/patch payload's
// parentFolderId ("0"/null -> project root).
func (s *Server) applyWorkflowParentFolder(r *http.Request, workflowID string, raw json.RawMessage) {
	store := s.workflowFolders()
	if store == nil {
		return
	}
	var value *string
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		value = normalizeParentFolderID(&text)
	}
	_ = store.SetWorkflowFolder(r.Context(), workflowID, value)
}

// workflowParentFolder returns the {id,name,parentFolderId} location of a workflow, or nil.
func (s *Server) workflowParentFolder(r *http.Request, workflowID string) any {
	store := s.workflowFolders()
	if store == nil || workflowID == "" || workflowID == "new" || workflowID == "demo" {
		return nil
	}
	loaded, err := store.ParentFoldersFor(r.Context(), []string{workflowID})
	if err != nil {
		return nil
	}
	parentID, ok := loaded[workflowID]
	if !ok || parentID == "" {
		return nil
	}
	if s.folderStore != nil {
		if folder, err := s.folderStore.GetByID(r.Context(), parentID); err == nil {
			return map[string]any{"id": folder.ID, "name": folder.Name, "parentFolderId": strPtrOrNil(folder.ParentFolderID)}
		}
	}
	return map[string]any{"id": parentID, "name": "", "parentFolderId": nil}
}
