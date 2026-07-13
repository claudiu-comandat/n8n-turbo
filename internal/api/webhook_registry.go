package api

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/persistence"
	"github.com/n8n-io/n8n-turbo/internal/webhook"
)

func isHTTPTriggerNode(node dataplane.Node) bool {
	return node.Type == "n8n-nodes-base.webhook" || node.Type == "n8n-nodes-base.formTrigger"
}

// nodeWebhookMethod returns the method token to index a webhook node under,
// matching how webhookMethod interprets it (empty/*/ANY mean match-all -> "ALL").
func nodeWebhookMethod(node dataplane.Node) string {
	method := strings.ToUpper(strings.TrimSpace(fmt.Sprint(node.Parameters["httpMethod"])))
	if method == "" || method == "<NIL>" {
		method = strings.ToUpper(strings.TrimSpace(fmt.Sprint(node.Parameters["method"])))
	}
	if method == "" || method == "<NIL>" || method == "*" || method == "ANY" {
		return "ALL"
	}
	return method
}

// registeredWebhooksForWorkflow builds the index rows (path -> workflow) for a
// workflow's production webhook/form nodes. It stores only routing metadata; the
// live workflow node is what actually executes, so no auth/HMAC/options here.
func registeredWebhooksForWorkflow(workflow dataplane.Workflow) []webhook.RegisteredWebhook {
	result := []webhook.RegisteredWebhook{}
	for _, node := range workflow.Nodes {
		if node.Disabled || !isHTTPTriggerNode(node) {
			continue
		}
		path := webhookPath(node)
		if path == "" {
			continue
		}
		result = append(result, webhook.RegisteredWebhook{
			WebhookID:  firstNonEmpty(node.WebhookID, node.ID, workflow.ID+"-"+node.Name),
			WorkflowID: workflow.ID,
			NodeID:     firstNonEmpty(node.ID, node.Name),
			NodeName:   node.Name,
			Path:       path,
			Method:     nodeWebhookMethod(node),
			IsTest:     false,
		})
	}
	return result
}

type workflowRowPager interface {
	ListPage(ctx context.Context, limit int, before time.Time, beforeID string) (persistence.WorkflowPage, error)
}

// eachWorkflowRow iterates every workflow uncapped. It paginates via ListPage
// when available (the real store), else falls back to a single List page.
func (s *Server) eachWorkflowRow(ctx context.Context, fn func(persistence.WorkflowRow)) error {
	pager, ok := s.workflowStore.(workflowRowPager)
	if !ok {
		rows, err := s.workflowStore.List(ctx, 250)
		if err != nil {
			return err
		}
		for i := range rows {
			fn(rows[i])
		}
		return nil
	}
	var before time.Time
	var beforeID string
	for {
		page, err := pager.ListPage(ctx, 250, before, beforeID)
		if err != nil {
			return err
		}
		for i := range page.Rows {
			fn(page.Rows[i])
		}
		if page.NextCursor == "" || len(page.Rows) == 0 {
			return nil
		}
		parts := strings.SplitN(page.NextCursor, "|", 2)
		if len(parts) != 2 {
			return nil
		}
		parsed, err := time.Parse(time.RFC3339Nano, parts[0])
		if err != nil {
			return nil
		}
		before, beforeID = parsed, parts[1]
	}
}

// reconcileWebhookRegistry rebuilds the webhook index from the active workflows.
// It is best-effort: the request-time lookup validates against the live workflow
// and falls back to a scan, so a stale or missing index entry never misroutes.
func (s *Server) reconcileWebhookRegistry(ctx context.Context) error {
	if s.webhookStore == nil {
		return nil
	}
	desired := map[string][]webhook.RegisteredWebhook{}
	if err := s.eachWorkflowRow(ctx, func(row persistence.WorkflowRow) {
		if !row.Active {
			return
		}
		workflow, err := workflowFromRow(&row)
		if err != nil {
			log.Printf("webhook registry: load workflow %s: %v", row.ID, err)
			return
		}
		if hooks := registeredWebhooksForWorkflow(workflow); len(hooks) > 0 {
			desired[workflow.ID] = hooks
		}
	}); err != nil {
		return err
	}
	for workflowID, hooks := range desired {
		if err := s.webhookStore.DeleteByWorkflow(ctx, workflowID); err != nil {
			log.Printf("webhook registry: clear %s: %v", workflowID, err)
			continue
		}
		for _, hook := range hooks {
			if err := s.webhookStore.Save(ctx, hook); err != nil {
				log.Printf("webhook registry: save %s: %v", workflowID, err)
			}
		}
	}
	stored, err := s.webhookStore.GetAll(ctx)
	if err != nil {
		return err
	}
	pruned := map[string]bool{}
	for _, hook := range stored {
		if _, ok := desired[hook.WorkflowID]; ok || pruned[hook.WorkflowID] {
			continue
		}
		pruned[hook.WorkflowID] = true
		if err := s.webhookStore.DeleteByWorkflow(ctx, hook.WorkflowID); err != nil {
			log.Printf("webhook registry: prune %s: %v", hook.WorkflowID, err)
		}
	}
	return nil
}

// lookupRegisteredWebhook resolves a request via the index (O(1)) and validates
// the hit against the live workflow. Returns false on any miss/stale entry so
// the caller falls back to the full scan.
func (s *Server) lookupRegisteredWebhook(ctx context.Context, path string, method string, isTest bool, nodeTypes map[string]bool) (*webhookMatch, bool) {
	reg := s.registeredWebhookByPath(ctx, path, method)
	if reg == nil {
		return nil, false
	}
	row, err := s.workflowStore.GetByID(ctx, reg.WorkflowID)
	if err != nil || row == nil {
		return nil, false
	}
	if !row.Active && !isTest {
		return nil, false
	}
	workflow, err := workflowFromRow(row)
	if err != nil {
		return nil, false
	}
	for _, node := range workflow.Nodes {
		if !nodeTypes[node.Type] || node.Disabled {
			continue
		}
		params, ok := matchWebhookPath(webhookPath(node), path)
		if ok && webhookMethod(node, method) {
			return &webhookMatch{Workflow: workflow, Node: node, Params: params}, true
		}
	}
	return nil, false
}

func (s *Server) registeredWebhookByPath(ctx context.Context, path string, method string) *webhook.RegisteredWebhook {
	if reg, err := s.webhookStore.GetByPath(ctx, path, method, false); err == nil && reg != nil {
		return reg
	}
	if strings.ToUpper(method) != "ALL" {
		if reg, err := s.webhookStore.GetByPath(ctx, path, "ALL", false); err == nil && reg != nil {
			return reg
		}
	}
	return nil
}
