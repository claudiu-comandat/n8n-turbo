package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/credentials"
	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

const gmailTriggerType = "n8n-nodes-base.gmailTrigger"

var gmailClient = &http.Client{Timeout: 30 * time.Second}

// gmailPollState tracks, per (workflow,node), where the last poll left off.
// ponytail: in-memory only — on process restart it re-baselines to "now", so
// emails that arrive during downtime are skipped. Upgrade path: persist
// sinceUnix into workflow.StaticData if at-least-once delivery is required.
type gmailPollState struct {
	sinceUnix int64
	seen      map[string]bool
}

func (s *Server) gmailPollStateFor(workflowID string, nodeName string) *gmailPollState {
	key := scheduledKey(workflowID, nodeName)
	s.scheduler.mu.Lock()
	defer s.scheduler.mu.Unlock()
	state := s.scheduler.gmailState[key]
	if state == nil {
		state = &gmailPollState{seen: map[string]bool{}}
		s.scheduler.gmailState[key] = state
	}
	return state
}

func (s *Server) pollGmailTrigger(ctx context.Context, workflow dataplane.Workflow, node dataplane.Node) {
	if !s.scheduler.canRunAsLeader() {
		return
	}
	// Reserve the per-workflow concurrency slot BEFORE fetching. Advancing the dedup
	// cursor/seen-set and then finding the slot busy would mark messages consumed
	// without ever dispatching them; skipping before any fetch avoids that data loss.
	if !s.scheduler.markWorkflowRunning(workflow) {
		log.Printf("skip gmail trigger for workflow %s: already running", workflow.ID)
		return
	}
	defer s.scheduler.clearWorkflowRunning(workflow.ID)
	items, err := s.fetchGmailMessages(ctx, workflow, node)
	if err != nil {
		log.Printf("gmail trigger poll for workflow %s node %s: %v", workflow.ID, node.Name, err)
		return
	}
	if len(items) == 0 {
		return
	}
	s.dispatchTriggerItems(ctx, workflow, node, items)
}

func (s *Server) fetchGmailMessages(ctx context.Context, workflow dataplane.Workflow, node dataplane.Node) ([]dataplane.Item, error) {
	creds, err := s.resolveNodeCredentials(ctx, node)
	if err != nil {
		return nil, err
	}
	cred := gmailCredentialData(creds)
	if cred == nil {
		return nil, fmt.Errorf("gmailOAuth2 credential not found on node")
	}
	refreshToken := gmailRefreshToken(cred)
	if refreshToken == "" {
		return nil, fmt.Errorf("gmail credential missing refresh token (reconnect OAuth)")
	}
	oauth := credentials.OAuth2Credential{
		ClientID:       credField(cred, "clientId"),
		ClientSecret:   credField(cred, "clientSecret"),
		AccessTokenURL: "https://oauth2.googleapis.com/token",
		Authentication: "body",
	}
	token, err := oauth.Refresh(ctx, gmailClient, refreshToken)
	if err != nil {
		return nil, fmt.Errorf("refresh gmail token: %w", err)
	}

	state := s.gmailPollStateFor(workflow.ID, node.Name)
	now := time.Now().Unix()
	if state.sinceUnix == 0 {
		// First poll after (re)start: baseline to now; only emails arriving later are emitted.
		state.sinceUnix = now
		return nil, nil
	}

	query := gmailQuery(node.Parameters)
	// 2s overlap so boundary emails are not missed; the seen-set dedups the overlap.
	scoped := strings.TrimSpace(query + " after:" + strconv.FormatInt(state.sinceUnix-2, 10))
	ids, err := s.gmailListMessageIDs(ctx, token.AccessToken, scoped)
	if err != nil {
		return nil, err
	}
	items := make([]dataplane.Item, 0, len(ids))
	failed := false
	for _, id := range ids {
		if state.seen[id] {
			continue
		}
		message, err := s.gmailGetMessage(ctx, token.AccessToken, id)
		if err != nil {
			log.Printf("gmail get message %s: %v", id, err)
			failed = true
			continue
		}
		state.seen[id] = true
		items = append(items, dataplane.Item{JSON: message})
	}
	// Only advance the cursor when every listed message was fetched; otherwise keep the
	// old cursor so the overlap query re-lists the failed ones on the next poll.
	// ponytail: a message that fails to fetch forever would stall the cursor — acceptable
	// since a listed message is essentially always fetchable; transient errors self-heal.
	if !failed {
		state.sinceUnix = now
	}
	boundGmailSeen(state, ids)
	return items, nil
}

func (s *Server) gmailListMessageIDs(ctx context.Context, accessToken string, query string) ([]string, error) {
	ids := []string{}
	pageToken := ""
	// Follow nextPageToken so a burst larger than one page is fully listed, not truncated
	// to the newest page (which would silently drop the older tail). ponytail: cap at 20
	// pages (~2000 messages) as a runaway guard for an unexpectedly huge window.
	for page := 0; page < 20; page++ {
		endpoint := "https://gmail.googleapis.com/gmail/v1/users/me/messages?maxResults=100"
		if query != "" {
			endpoint += "&q=" + url.QueryEscape(query)
		}
		if pageToken != "" {
			endpoint += "&pageToken=" + url.QueryEscape(pageToken)
		}
		body, err := gmailGet(ctx, accessToken, endpoint)
		if err != nil {
			return nil, err
		}
		var parsed struct {
			Messages []struct {
				ID string `json:"id"`
			} `json:"messages"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, fmt.Errorf("parse gmail message list: %w", err)
		}
		for _, message := range parsed.Messages {
			if message.ID != "" {
				ids = append(ids, message.ID)
			}
		}
		if parsed.NextPageToken == "" {
			break
		}
		pageToken = parsed.NextPageToken
	}
	return ids, nil
}

func (s *Server) gmailGetMessage(ctx context.Context, accessToken string, id string) (map[string]any, error) {
	endpoint := "https://gmail.googleapis.com/gmail/v1/users/me/messages/" + url.PathEscape(id) + "?format=full"
	body, err := gmailGet(ctx, accessToken, endpoint)
	if err != nil {
		return nil, err
	}
	var message map[string]any
	if err := json.Unmarshal(body, &message); err != nil {
		return nil, fmt.Errorf("parse gmail message: %w", err)
	}
	return message, nil
}

func gmailGet(ctx context.Context, accessToken string, endpoint string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Accept", "application/json")
	response, err := gmailClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 16*1024*1024))
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("gmail API %d: %s", response.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, nil
}

// dispatchTriggerItems runs one execution with polled items as the trigger node's output.
// The caller (pollGmailTrigger) already holds the per-workflow concurrency slot.
func (s *Server) dispatchTriggerItems(ctx context.Context, workflow dataplane.Workflow, node dataplane.Node, items []dataplane.Item) {
	mode := engine.ExecutionModeTrigger.String()
	execution, err := s.executionStore.Create(ctx, workflow, mode)
	if err != nil {
		log.Printf("create trigger execution for workflow %s: %v", workflow.ID, err)
		return
	}
	variables, err := s.resolvedVariablesContext(ctx)
	if err != nil {
		variables = map[string]any{}
	}
	secrets, err := s.resolvedSecrets(ctx)
	if err != nil {
		secrets = map[string]map[string]string{}
	}
	dispatchResult := s.dispatchWorkflowSync(ctx, executionDispatchRequest{
		ExecutionID: execution.ID,
		Workflow:    workflow,
		Mode:        mode,
		Options: engine.ExecuteOptions{
			Variables:    variables,
			Secrets:      secrets,
			BinaryStore:  s.binaryStore,
			Credentials:  s.resolveNodeCredentials,
			TriggerNode:  node.Name,
			TriggerItems: items,
			OnStarted:    s.pushExecutionStarted,
			OnNodeAfter:  s.pushNodeAfter,
			OnFinished:   s.pushExecutionFinished,
		},
		StartData:       map[string]any{"triggerNode": node.Name},
		ErrorName:       "TriggerExecutionError",
		Timeout:         scheduledExecutionTimeout(workflow),
		CrashOnDeadline: true,
	})
	if dispatchResult.StartErr != nil {
		log.Printf("start trigger execution %s: %v", execution.ID, dispatchResult.StartErr)
		return
	}
	if dispatchResult.StoreErr != nil {
		log.Printf("finish trigger execution %s: %v", execution.ID, dispatchResult.StoreErr)
	}
}

func gmailQuery(parameters map[string]any) string {
	filters, ok := parameters["filters"].(map[string]any)
	if !ok {
		return ""
	}
	if q, ok := filters["q"].(string); ok {
		return strings.TrimSpace(q)
	}
	return ""
}

func gmailCredentialData(creds map[string]map[string]any) map[string]any {
	if cred, ok := creds["gmailOAuth2"]; ok {
		return cred
	}
	if cred, ok := creds["googleOAuth2Api"]; ok {
		return cred
	}
	for _, cred := range creds {
		return cred
	}
	return nil
}

func gmailRefreshToken(cred map[string]any) string {
	if token, ok := cred["oauthTokenData"].(map[string]any); ok {
		if value := stringFromAny(token["refresh_token"]); value != "" {
			return value
		}
	}
	for _, key := range []string{"refreshToken", "refresh_token"} {
		if value := stringFromAny(cred[key]); value != "" {
			return value
		}
	}
	return ""
}

func credField(cred map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringFromAny(cred[key]); value != "" {
			return value
		}
	}
	return ""
}

// boundGmailSeen keeps the seen-set from growing unbounded while preserving overlap
// dedup: it retains only the ids listed in the current poll that were already emitted
// (state.seen[id] == true). The current window is a superset of the next poll's 2s
// overlap band, so retained ids still dedup it; ids that failed to fetch this poll are
// dropped (seen[id] false) so the overlap re-lists and retries them; out-of-window ids
// are dropped because they cannot be re-listed by a strictly newer after: query.
func boundGmailSeen(state *gmailPollState, ids []string) {
	retained := make(map[string]bool, len(ids))
	for _, id := range ids {
		if state.seen[id] {
			retained[id] = true
		}
	}
	state.seen = retained
}
