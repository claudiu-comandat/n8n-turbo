package api

import (
	"context"
	"net/http"
	"sort"
	"strings"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/nodes"
	"github.com/n8n-io/n8n-turbo/internal/persistence"
)

// processTrigger describes ONE way a workflow can start, regardless of type, so
// a human or an AI can see at a glance what fires each process — a webhook path,
// a cron schedule, a sub-workflow call, or a manual run.
type processTrigger struct {
	Kind     string `json:"kind"` // webhook | form | schedule | manual | subworkflow | error | poll | other
	NodeName string `json:"nodeName"`
	NodeType string `json:"nodeType"`
	Method   string `json:"method,omitempty"`   // webhook/form
	Path     string `json:"path,omitempty"`     // webhook/form registered path
	Schedule string `json:"schedule,omitempty"` // schedule: cron expression
	Timezone string `json:"timezone,omitempty"` // schedule: timezone
	Disabled bool   `json:"disabled,omitempty"`
}

// processCatalogEntry is one workflow ("process") plus how it is triggered and
// where it belongs. This is the flat, legible "table of contents" of the whole
// instance — the same view for people (via /catalog) and AI (via the MCP).
type processCatalogEntry struct {
	WorkflowID   string           `json:"workflowId"`
	WorkflowName string           `json:"workflowName"`
	Folder       string           `json:"folder"` // first path-like segment of the name, e.g. "off-site"
	Active       bool             `json:"active"`
	Tags         []string         `json:"tags"`
	Triggers     []processTrigger `json:"triggers"`
}

// nodeTriggerInfo reports whether a node is a trigger and describes it. Trigger
// detection mirrors the engine (scheduleTrigger drives cron, webhook/formTrigger
// drive HTTP) and falls back to the n8n "*Trigger" naming convention for the long
// tail of polling triggers (gmailTrigger, rssFeedReadTrigger, ...).
func nodeTriggerInfo(node dataplane.Node) (processTrigger, bool) {
	trigger := processTrigger{NodeName: node.Name, NodeType: node.Type, Disabled: node.Disabled}
	switch node.Type {
	case "n8n-nodes-base.webhook":
		trigger.Kind = "webhook"
		trigger.Path = webhookPath(node)
		trigger.Method = nodeWebhookMethod(node)
		return trigger, true
	case "n8n-nodes-base.formTrigger":
		trigger.Kind = "form"
		trigger.Path = webhookPath(node)
		trigger.Method = nodeWebhookMethod(node)
		return trigger, true
	case "n8n-nodes-base.scheduleTrigger":
		trigger.Kind = "schedule"
		if expr, err := nodes.BuildScheduleCronExpression(node.Parameters); err == nil {
			trigger.Schedule = expr
		}
		trigger.Timezone = nodes.ScheduleTimezone(node.Parameters)
		return trigger, true
	case "n8n-nodes-base.cron", "n8n-nodes-base.interval":
		trigger.Kind = "schedule"
		return trigger, true
	case "n8n-nodes-base.manualTrigger":
		trigger.Kind = "manual"
		return trigger, true
	case "n8n-nodes-base.executeWorkflowTrigger":
		trigger.Kind = "subworkflow"
		return trigger, true
	case "n8n-nodes-base.errorTrigger":
		trigger.Kind = "error"
		return trigger, true
	}
	if strings.HasSuffix(strings.ToLower(node.Type), "trigger") {
		trigger.Kind = "poll"
		return trigger, true
	}
	return processTrigger{}, false
}

// nameFolder derives a "virtual folder" from a workflow name so processes read
// like a tree even before real folders exist: "off-site / awb / issue" -> "off-site".
func nameFolder(name string) string {
	for _, sep := range []string{" / ", "/", " · ", "·", " - ", ":"} {
		if idx := strings.Index(name, sep); idx > 0 {
			return strings.TrimSpace(name[:idx])
		}
	}
	return ""
}

// collectProcessCatalog scans every workflow (active or not) and returns the full
// trigger-aware catalog. It reuses eachWorkflowRow — the same uncapped iterator the
// webhook router already runs — so results are always fresh, never a stale index.
func (s *Server) collectProcessCatalog(ctx context.Context) ([]processCatalogEntry, error) {
	type accumulator struct {
		name     string
		active   bool
		triggers []processTrigger
	}
	order := []string{}
	byID := map[string]*accumulator{}
	ids := []string{}
	err := s.eachWorkflowRow(ctx, func(row persistence.WorkflowRow) {
		workflow, err := workflowFromRow(&row)
		if err != nil {
			return
		}
		entry := &accumulator{name: row.Name, active: row.Active, triggers: []processTrigger{}}
		for _, node := range workflow.Nodes {
			if info, ok := nodeTriggerInfo(node); ok {
				entry.triggers = append(entry.triggers, info)
			}
		}
		byID[row.ID] = entry
		order = append(order, row.ID)
		ids = append(ids, row.ID)
	})
	if err != nil {
		return nil, err
	}
	tagsByWorkflow := map[string][]persistence.TagRow{}
	if s.tagStore != nil {
		if loaded, err := s.tagStore.TagsForWorkflows(ctx, ids); err == nil {
			tagsByWorkflow = loaded
		}
	}
	entries := make([]processCatalogEntry, 0, len(order))
	for _, id := range order {
		entry := byID[id]
		tags := []string{}
		for _, tag := range tagsByWorkflow[id] {
			tags = append(tags, tag.Name)
		}
		entries = append(entries, processCatalogEntry{
			WorkflowID:   id,
			WorkflowName: entry.name,
			Folder:       nameFolder(entry.name),
			Active:       entry.active,
			Tags:         tags,
			Triggers:     entry.triggers,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Folder != entries[j].Folder {
			return entries[i].Folder < entries[j].Folder
		}
		return entries[i].WorkflowName < entries[j].WorkflowName
	})
	return entries, nil
}

// handleProcessCatalog serves the whole catalog, with optional ?tag= and ?q=
// (name/path substring) filters. This is the AI/CLI "table of contents".
func (s *Server) handleProcessCatalog(w http.ResponseWriter, r *http.Request) {
	entries, err := s.collectProcessCatalog(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	tagFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("tag")))
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	filtered := make([]processCatalogEntry, 0, len(entries))
	for _, entry := range entries {
		if tagFilter != "" && !containsStringFold(entry.Tags, tagFilter) {
			continue
		}
		if query != "" && !catalogEntryMatches(entry, query) {
			continue
		}
		filtered = append(filtered, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": filtered, "count": len(filtered)})
}

func catalogEntryMatches(entry processCatalogEntry, query string) bool {
	if strings.Contains(strings.ToLower(entry.WorkflowName), query) {
		return true
	}
	for _, trigger := range entry.Triggers {
		if strings.Contains(strings.ToLower(trigger.Path), query) {
			return true
		}
	}
	return false
}

func containsStringFold(values []string, needle string) bool {
	for _, value := range values {
		if strings.EqualFold(value, needle) {
			return true
		}
	}
	return false
}
