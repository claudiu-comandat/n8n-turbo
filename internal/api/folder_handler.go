package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/n8n-io/n8n-turbo/internal/persistence"
)

// rootFolderSentinel is n8n's literal "0" meaning "project root" (no parent folder).
const rootFolderSentinel = "0"

// workflowFolderStore is the optional slice of the workflow store used for folders.
type workflowFolderStore interface {
	SetWorkflowFolder(ctx context.Context, workflowID string, parentFolderID *string) error
	ParentFoldersFor(ctx context.Context, ids []string) (map[string]string, error)
	WorkflowIDsInFolder(ctx context.Context, parentFolderID string) ([]string, error)
}

func (s *Server) workflowFolders() workflowFolderStore {
	if store, ok := s.workflowStore.(workflowFolderStore); ok {
		return store
	}
	return nil
}

func (s *Server) homeProjectSummary(r *http.Request) map[string]any {
	project := s.personalProject(r)
	return map[string]any{
		"id":   project["id"],
		"name": project["name"],
		"type": project["type"],
		"icon": project["icon"],
	}
}

func normalizeParentFolderID(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" || trimmed == rootFolderSentinel {
		return nil
	}
	return &trimmed
}

func strPtrOrNil(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

// loadFolderContext loads all folders for a project into an id->folder map plus a
// direct-child count per folder id. Folder counts are small, so in-memory is fine.
func (s *Server) loadFolderContext(ctx context.Context, projectID string) (map[string]persistence.FolderRow, map[string]int) {
	byID := map[string]persistence.FolderRow{}
	childCount := map[string]int{}
	if s.folderStore == nil {
		return byID, childCount
	}
	folders, err := s.folderStore.ListByProject(ctx, projectID)
	if err != nil {
		return byID, childCount
	}
	for _, folder := range folders {
		byID[folder.ID] = folder
		if folder.ParentFolderID != nil {
			childCount[*folder.ParentFolderID]++
		}
	}
	return byID, childCount
}

func (s *Server) workflowCountInFolder(ctx context.Context, folderID string) int {
	if store := s.workflowFolders(); store != nil {
		if ids, err := store.WorkflowIDsInFolder(ctx, folderID); err == nil {
			return len(ids)
		}
	}
	return 0
}

// folderAndDescendants returns the set of folder ids rooted at rootID (inclusive).
func (s *Server) folderAndDescendants(byID map[string]persistence.FolderRow, rootID string) map[string]bool {
	set := map[string]bool{}
	if rootID == "" {
		return set
	}
	if _, ok := byID[rootID]; !ok {
		return set
	}
	children := map[string][]string{}
	for id, folder := range byID {
		if folder.ParentFolderID != nil {
			children[*folder.ParentFolderID] = append(children[*folder.ParentFolderID], id)
		}
	}
	stack := []string{rootID}
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if set[current] {
			continue
		}
		set[current] = true
		stack = append(stack, children[current]...)
	}
	return set
}

// buildFolderResponse renders the n8n 2.16 folder object shape.
func (s *Server) buildFolderResponse(ctx context.Context, r *http.Request, folder persistence.FolderRow, byID map[string]persistence.FolderRow, childCount map[string]int) map[string]any {
	var parentFolder any
	if folder.ParentFolderID != nil {
		if parent, ok := byID[*folder.ParentFolderID]; ok {
			parentFolder = map[string]any{"id": parent.ID, "name": parent.Name, "parentFolderId": strPtrOrNil(parent.ParentFolderID)}
		} else {
			parentFolder = map[string]any{"id": *folder.ParentFolderID, "name": "", "parentFolderId": nil}
		}
	}
	return map[string]any{
		"resource":       "folder",
		"id":             folder.ID,
		"name":           folder.Name,
		"parentFolderId": strPtrOrNil(folder.ParentFolderID),
		"parentFolder":   parentFolder,
		"homeProject":    s.homeProjectSummary(r),
		"tags":           []map[string]any{},
		"workflowCount":  s.workflowCountInFolder(ctx, folder.ID),
		"subFolderCount": childCount[folder.ID],
		"createdAt":      folder.CreatedAt.Format(time.RFC3339Nano),
		"updatedAt":      folder.UpdatedAt.Format(time.RFC3339Nano),
	}
}

func (s *Server) handleCreateFolder(w http.ResponseWriter, r *http.Request) {
	if s.folderStore == nil {
		writeError(w, http.StatusNotImplemented, "folders unavailable")
		return
	}
	projectID := chi.URLParam(r, "id")
	var body struct {
		Name           string  `json:"name"`
		ParentFolderID *string `json:"parentFolderId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid folder body")
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "folder name is required")
		return
	}
	folder, err := s.folderStore.Create(r.Context(), "", name, projectID, normalizeParentFolderID(body.ParentFolderID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	byID, childCount := s.loadFolderContext(r.Context(), projectID)
	writeJSON(w, http.StatusOK, map[string]any{"data": s.buildFolderResponse(r.Context(), r, *folder, byID, childCount)})
}

func (s *Server) handleListFolders(w http.ResponseWriter, r *http.Request) {
	if s.folderStore == nil {
		writeJSON(w, http.StatusOK, map[string]any{"data": []map[string]any{}, "count": 0})
		return
	}
	projectID := chi.URLParam(r, "id")
	byID, childCount := s.loadFolderContext(r.Context(), projectID)
	filter := parseJSONQueryObject(r, "filter")
	parentFilter, hasParent := filter["parentFolderId"].(string)
	nameFilter := strings.ToLower(strings.TrimSpace(stringFromAny(filter["name"])))
	exclude := stringFromAny(filter["excludeFolderIdAndDescendants"])
	excludeSet := s.folderAndDescendants(byID, exclude)

	folders := make([]persistence.FolderRow, 0, len(byID))
	for _, folder := range byID {
		folders = append(folders, folder)
	}
	sort.Slice(folders, func(i, j int) bool { return folders[i].Name < folders[j].Name })

	items := []map[string]any{}
	for _, folder := range folders {
		if hasParent {
			if parentFilter == rootFolderSentinel {
				if folder.ParentFolderID != nil {
					continue
				}
			} else if folder.ParentFolderID == nil || *folder.ParentFolderID != parentFilter {
				continue
			}
		}
		if nameFilter != "" && !strings.Contains(strings.ToLower(folder.Name), nameFilter) {
			continue
		}
		if excludeSet[folder.ID] {
			continue
		}
		items = append(items, s.buildFolderResponse(r.Context(), r, folder, byID, childCount))
	}
	total := len(items)
	writeJSON(w, http.StatusOK, map[string]any{"data": paginateMaps(items, r), "count": total})
}

func (s *Server) handleFolderTree(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	folderID := chi.URLParam(r, "folderId")
	byID, _ := s.loadFolderContext(r.Context(), projectID)
	chain := []persistence.FolderRow{}
	current, ok := byID[folderID]
	for ok {
		chain = append([]persistence.FolderRow{current}, chain...)
		if current.ParentFolderID == nil {
			break
		}
		current, ok = byID[*current.ParentFolderID]
	}
	var node map[string]any
	for i := len(chain) - 1; i >= 0; i-- {
		children := []map[string]any{}
		if node != nil {
			children = []map[string]any{node}
		}
		node = map[string]any{"id": chain[i].ID, "name": chain[i].Name, "children": children}
	}
	data := []map[string]any{}
	if node != nil {
		data = []map[string]any{node}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

func (s *Server) handleFolderContent(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	folderID := chi.URLParam(r, "folderId")
	byID, _ := s.loadFolderContext(r.Context(), projectID)
	descendants := s.folderAndDescendants(byID, folderID)
	totalSubFolders := 0
	if len(descendants) > 0 {
		totalSubFolders = len(descendants) - 1
	}
	totalWorkflows := 0
	for id := range descendants {
		totalWorkflows += s.workflowCountInFolder(r.Context(), id)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
		"totalSubFolders": totalSubFolders,
		"totalWorkflows":  totalWorkflows,
	}})
}

func (s *Server) handleUpdateFolder(w http.ResponseWriter, r *http.Request) {
	if s.folderStore == nil {
		writeError(w, http.StatusNotImplemented, "folders unavailable")
		return
	}
	projectID := chi.URLParam(r, "id")
	folderID := chi.URLParam(r, "folderId")
	var body struct {
		Name           *string  `json:"name"`
		ParentFolderID *string  `json:"parentFolderId"`
		TagIds         []string `json:"tagIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid folder body")
		return
	}
	if body.Name != nil {
		if err := s.folderStore.Rename(r.Context(), folderID, strings.TrimSpace(*body.Name)); err != nil {
			writeStoreError(w, err)
			return
		}
	}
	if body.ParentFolderID != nil {
		if err := s.folderStore.Move(r.Context(), folderID, normalizeParentFolderID(body.ParentFolderID)); err != nil {
			writeStoreError(w, err)
			return
		}
	}
	folder, err := s.folderStore.GetByID(r.Context(), folderID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	byID, childCount := s.loadFolderContext(r.Context(), projectID)
	writeJSON(w, http.StatusOK, map[string]any{"data": s.buildFolderResponse(r.Context(), r, *folder, byID, childCount)})
}

func (s *Server) handleDeleteFolder(w http.ResponseWriter, r *http.Request) {
	if s.folderStore == nil {
		writeError(w, http.StatusNotImplemented, "folders unavailable")
		return
	}
	projectID := chi.URLParam(r, "id")
	folderID := chi.URLParam(r, "folderId")
	transferTo := strings.TrimSpace(r.URL.Query().Get("transferToFolderId"))
	byID, _ := s.loadFolderContext(r.Context(), projectID)
	if _, ok := byID[folderID]; !ok {
		writeError(w, http.StatusNotFound, "folder not found")
		return
	}
	folders := s.workflowFolders()

	if transferTo != "" {
		var dest *string
		if transferTo != rootFolderSentinel {
			dest = &transferTo
		}
		if folders != nil {
			if ids, err := folders.WorkflowIDsInFolder(r.Context(), folderID); err == nil {
				for _, id := range ids {
					_ = folders.SetWorkflowFolder(r.Context(), id, dest)
				}
			}
		}
		for _, folder := range byID {
			if folder.ParentFolderID != nil && *folder.ParentFolderID == folderID {
				_ = s.folderStore.Move(r.Context(), folder.ID, dest)
			}
		}
	} else {
		// ponytail: cascade-delete moves contained workflows to project root instead of
		// deleting them — never destroy the user's workflows as a side effect of a folder delete.
		descendants := s.folderAndDescendants(byID, folderID)
		if folders != nil {
			for id := range descendants {
				if ids, err := folders.WorkflowIDsInFolder(r.Context(), id); err == nil {
					for _, workflowID := range ids {
						_ = folders.SetWorkflowFolder(r.Context(), workflowID, nil)
					}
				}
			}
		}
		for id := range descendants {
			if id == folderID {
				continue
			}
			_ = s.folderStore.Delete(r.Context(), id)
		}
	}
	if err := s.folderStore.Delete(r.Context(), folderID); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": true})
}

func (s *Server) handleTransferFolder(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": true})
}

func (s *Server) handleFolderCredentials(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": []map[string]any{}})
}

// parseJSONQueryObject reads a query param that the n8n client JSON.stringify'd
// (e.g. filter={"parentFolderId":"0"}).
func parseJSONQueryObject(r *http.Request, key string) map[string]any {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return map[string]any{}
	}
	return out
}

func stringFromAny(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func paginateMaps(items []map[string]any, r *http.Request) []map[string]any {
	skip := queryInt(r, "skip", 0)
	take := queryInt(r, "take", 0)
	if skip < 0 {
		skip = 0
	}
	if skip >= len(items) {
		return []map[string]any{}
	}
	items = items[skip:]
	if take > 0 && take < len(items) {
		items = items[:take]
	}
	return items
}
