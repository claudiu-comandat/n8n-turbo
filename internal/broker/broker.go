package broker

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/n8n-io/n8n-turbo/internal/push"
)

var (
	ErrUnauthorized    = errors.New("runner authentication failed")
	ErrInvalidGrant    = errors.New("runner grant token is invalid or expired")
	ErrRunnerNotFound  = errors.New("runner not found")
	ErrTaskNotFound    = errors.New("task not found")
	ErrNoTaskAvailable = errors.New("no task available")
	ErrTaskFinished    = errors.New("task is already finished")
)

type Config struct {
	AuthToken      string
	GrantTTL       time.Duration
	RequestTimeout time.Duration
	TaskTimeout    time.Duration
}

type Broker struct {
	hub     *push.Hub
	config  Config
	mu      sync.Mutex
	grants  map[string]time.Time
	runners map[string]*Runner
	tasks   map[string]*Task
}

type Message = push.Message
type EventType = push.EventType

type Runner struct {
	ID          string    `json:"id"`
	Name        string    `json:"name,omitempty"`
	TaskTypes   []string  `json:"taskTypes"`
	LastSeen    time.Time `json:"lastSeen"`
	ActiveTasks int       `json:"activeTasks"`
}

type Task struct {
	ID          string          `json:"id"`
	RequesterID string          `json:"requesterId"`
	RunnerID    string          `json:"runnerId,omitempty"`
	TaskType    string          `json:"taskType"`
	Status      string          `json:"status"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
	Error       string          `json:"error,omitempty"`
	CreatedAt   time.Time       `json:"createdAt"`
	StartedAt   *time.Time      `json:"startedAt,omitempty"`
	StoppedAt   *time.Time      `json:"stoppedAt,omitempty"`
	ExpiresAt   time.Time       `json:"expiresAt"`
}

func New() *Broker {
	return NewWithConfig(Config{})
}

func NewWithConfig(config Config) *Broker {
	if config.GrantTTL <= 0 {
		config.GrantTTL = 30 * time.Second
	}
	if config.RequestTimeout <= 0 {
		config.RequestTimeout = 60 * time.Second
	}
	if config.TaskTimeout <= 0 {
		config.TaskTimeout = 5 * time.Minute
	}
	return &Broker{
		hub:     push.NewHub(),
		config:  config,
		grants:  map[string]time.Time{},
		runners: map[string]*Runner{},
		tasks:   map[string]*Task{},
	}
}

func NewWithHub(hub *push.Hub) *Broker {
	if hub == nil {
		hub = push.NewHub()
	}
	broker := New()
	broker.hub = hub
	return broker
}

func (b *Broker) Authenticate(token string) (string, error) {
	if b.config.AuthToken != "" && !sameToken(token, b.config.AuthToken) {
		return "", ErrUnauthorized
	}
	grant, err := randomToken()
	if err != nil {
		return "", err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pruneLocked(time.Now().UTC())
	b.grants[grant] = time.Now().UTC().Add(b.config.GrantTTL)
	return grant, nil
}

func (b *Broker) RegisterRunner(runner Runner, grantToken string) (*Runner, error) {
	now := time.Now().UTC()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pruneLocked(now)
	if b.config.AuthToken != "" {
		expires, ok := b.grants[grantToken]
		if !ok || expires.Before(now) {
			return nil, ErrInvalidGrant
		}
		delete(b.grants, grantToken)
	}
	if strings.TrimSpace(runner.ID) == "" {
		runner.ID = uuid.NewString()
	}
	runner.LastSeen = now
	runner.TaskTypes = uniqueNonEmpty(runner.TaskTypes)
	copy := runner
	b.runners[runner.ID] = &copy
	return cloneRunner(&copy), nil
}

func (b *Broker) DeregisterRunner(runnerID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.runners, runnerID)
	now := time.Now().UTC()
	for _, task := range b.tasks {
		if task.RunnerID == runnerID && task.Status == "running" {
			task.Status = "failed"
			task.Error = "runner disconnected"
			task.StoppedAt = &now
		}
	}
}

func (b *Broker) Heartbeat(runnerID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	runner, ok := b.runners[runnerID]
	if !ok {
		return ErrRunnerNotFound
	}
	runner.LastSeen = time.Now().UTC()
	return nil
}

func (b *Broker) SubmitTask(requesterID string, taskType string, payload json.RawMessage) (*Task, error) {
	taskType = strings.TrimSpace(taskType)
	if taskType == "" {
		return nil, errors.New("task type is required")
	}
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	now := time.Now().UTC()
	task := &Task{
		ID:          uuid.NewString(),
		RequesterID: requesterID,
		TaskType:    taskType,
		Status:      "queued",
		Payload:     cloneRaw(payload),
		CreatedAt:   now,
		ExpiresAt:   now.Add(b.config.RequestTimeout),
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pruneLocked(now)
	b.tasks[task.ID] = task
	return cloneTask(task), nil
}

func (b *Broker) ClaimTask(runnerID string) (*Task, error) {
	now := time.Now().UTC()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pruneLocked(now)
	runner, ok := b.runners[runnerID]
	if !ok {
		return nil, ErrRunnerNotFound
	}
	for _, task := range b.tasks {
		if task.Status != "queued" || task.ExpiresAt.Before(now) || !runnerSupports(runner, task.TaskType) {
			continue
		}
		task.Status = "running"
		task.RunnerID = runnerID
		task.StartedAt = &now
		task.ExpiresAt = now.Add(b.config.TaskTimeout)
		runner.LastSeen = now
		runner.ActiveTasks++
		return cloneTask(task), nil
	}
	return nil, ErrNoTaskAvailable
}

func (b *Broker) CompleteTask(runnerID string, taskID string, result json.RawMessage) (*Task, error) {
	return b.finishTask(runnerID, taskID, "succeeded", result, "")
}

func (b *Broker) FailTask(runnerID string, taskID string, reason string) (*Task, error) {
	return b.finishTask(runnerID, taskID, "failed", nil, reason)
}

func (b *Broker) Task(taskID string) (*Task, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pruneLocked(time.Now().UTC())
	task, ok := b.tasks[taskID]
	if !ok {
		return nil, ErrTaskNotFound
	}
	return cloneTask(task), nil
}

func (b *Broker) Runners() []Runner {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pruneLocked(time.Now().UTC())
	result := make([]Runner, 0, len(b.runners))
	for _, runner := range b.runners {
		result = append(result, *cloneRunner(runner))
	}
	return result
}

func (b *Broker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/healthz":
		writeBrokerJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	case r.Method == http.MethodPost && r.URL.Path == "/runners/auth":
		b.handleAuth(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/runners/register":
		b.handleRegisterRunner(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/runners/heartbeat":
		b.handleHeartbeat(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/runners":
		writeBrokerJSON(w, http.StatusOK, map[string]any{"data": b.Runners()})
	case r.Method == http.MethodPost && r.URL.Path == "/tasks":
		b.handleSubmitTask(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/tasks/claim":
		b.handleClaimTask(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/tasks/"):
		b.handleGetTask(w, r)
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/tasks/") && strings.HasSuffix(r.URL.Path, "/complete"):
		b.handleCompleteTask(w, r)
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/tasks/") && strings.HasSuffix(r.URL.Path, "/fail"):
		b.handleFailTask(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (b *Broker) PushHTTP(w http.ResponseWriter, r *http.Request) {
	b.hub.ServeHTTP(w, r)
}

func (b *Broker) Publish(message Message) {
	b.hub.Publish(message)
}

func (b *Broker) BroadcastToUser(userID string, message Message) {
	b.hub.BroadcastToUser(userID, message)
}

func (b *Broker) BroadcastToExecution(executionID string, message Message) {
	b.hub.BroadcastToExecution(executionID, message)
}

func (b *Broker) Count() int {
	return b.hub.Count()
}

func (b *Broker) Hub() *push.Hub {
	return b.hub
}

func (b *Broker) finishTask(runnerID string, taskID string, status string, result json.RawMessage, reason string) (*Task, error) {
	now := time.Now().UTC()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pruneLocked(now)
	task, ok := b.tasks[taskID]
	if !ok {
		return nil, ErrTaskNotFound
	}
	if task.Status == "succeeded" || task.Status == "failed" {
		return nil, ErrTaskFinished
	}
	if task.RunnerID != runnerID {
		return nil, ErrRunnerNotFound
	}
	task.Status = status
	task.Result = cloneRaw(result)
	task.Error = reason
	task.StoppedAt = &now
	if runner, ok := b.runners[runnerID]; ok && runner.ActiveTasks > 0 {
		runner.ActiveTasks--
		runner.LastSeen = now
	}
	return cloneTask(task), nil
}

func (b *Broker) pruneLocked(now time.Time) {
	for token, expires := range b.grants {
		if expires.Before(now) {
			delete(b.grants, token)
		}
	}
	for _, task := range b.tasks {
		if (task.Status == "queued" || task.Status == "running") && task.ExpiresAt.Before(now) {
			task.Status = "failed"
			task.Error = "task timed out"
			stopped := now
			task.StoppedAt = &stopped
			if runner, ok := b.runners[task.RunnerID]; ok && runner.ActiveTasks > 0 {
				runner.ActiveTasks--
			}
		}
	}
}

func (b *Broker) handleAuth(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeBrokerError(w, http.StatusBadRequest, "invalid auth body")
		return
	}
	grant, err := b.Authenticate(payload.Token)
	if err != nil {
		writeBrokerError(w, http.StatusForbidden, err.Error())
		return
	}
	writeBrokerJSON(w, http.StatusOK, map[string]any{"grantToken": grant})
}

func (b *Broker) handleRegisterRunner(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ID         string   `json:"id"`
		Name       string   `json:"name"`
		TaskTypes  []string `json:"taskTypes"`
		GrantToken string   `json:"grantToken"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeBrokerError(w, http.StatusBadRequest, "invalid runner body")
		return
	}
	runner, err := b.RegisterRunner(Runner{ID: payload.ID, Name: payload.Name, TaskTypes: payload.TaskTypes}, payload.GrantToken)
	if err != nil {
		writeBrokerError(w, http.StatusForbidden, err.Error())
		return
	}
	writeBrokerJSON(w, http.StatusOK, map[string]any{"data": runner})
}

func (b *Broker) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		RunnerID string `json:"runnerId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeBrokerError(w, http.StatusBadRequest, "invalid heartbeat body")
		return
	}
	if err := b.Heartbeat(payload.RunnerID); err != nil {
		writeBrokerError(w, http.StatusNotFound, err.Error())
		return
	}
	writeBrokerJSON(w, http.StatusOK, map[string]any{"data": true})
}

func (b *Broker) handleSubmitTask(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		RequesterID string          `json:"requesterId"`
		TaskType    string          `json:"taskType"`
		Payload     json.RawMessage `json:"payload"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeBrokerError(w, http.StatusBadRequest, "invalid task body")
		return
	}
	task, err := b.SubmitTask(payload.RequesterID, payload.TaskType, payload.Payload)
	if err != nil {
		writeBrokerError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeBrokerJSON(w, http.StatusAccepted, map[string]any{"data": task})
}

func (b *Broker) handleClaimTask(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		RunnerID string `json:"runnerId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeBrokerError(w, http.StatusBadRequest, "invalid claim body")
		return
	}
	task, err := b.ClaimTask(payload.RunnerID)
	if err != nil {
		status := http.StatusNotFound
		if errors.Is(err, ErrNoTaskAvailable) {
			status = http.StatusNoContent
		}
		writeBrokerError(w, status, err.Error())
		return
	}
	writeBrokerJSON(w, http.StatusOK, map[string]any{"data": task})
}

func (b *Broker) handleGetTask(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimPrefix(r.URL.Path, "/tasks/")
	taskID = strings.Trim(taskID, "/")
	if strings.Contains(taskID, "/") {
		http.NotFound(w, r)
		return
	}
	task, err := b.Task(taskID)
	if err != nil {
		writeBrokerError(w, http.StatusNotFound, err.Error())
		return
	}
	writeBrokerJSON(w, http.StatusOK, map[string]any{"data": task})
}

func (b *Broker) handleCompleteTask(w http.ResponseWriter, r *http.Request) {
	taskID := taskIDFromAction(r.URL.Path, "/complete")
	var payload struct {
		RunnerID string          `json:"runnerId"`
		Result   json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeBrokerError(w, http.StatusBadRequest, "invalid task completion body")
		return
	}
	task, err := b.CompleteTask(payload.RunnerID, taskID, payload.Result)
	if err != nil {
		writeBrokerError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeBrokerJSON(w, http.StatusOK, map[string]any{"data": task})
}

func (b *Broker) handleFailTask(w http.ResponseWriter, r *http.Request) {
	taskID := taskIDFromAction(r.URL.Path, "/fail")
	var payload struct {
		RunnerID string `json:"runnerId"`
		Error    string `json:"error"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeBrokerError(w, http.StatusBadRequest, "invalid task failure body")
		return
	}
	task, err := b.FailTask(payload.RunnerID, taskID, payload.Error)
	if err != nil {
		writeBrokerError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeBrokerJSON(w, http.StatusOK, map[string]any{"data": task})
}

func sameToken(a string, b string) bool {
	ab := []byte(a)
	bb := []byte(b)
	if len(ab) != len(bb) {
		return false
	}
	return subtle.ConstantTimeCompare(ab, bb) == 1
}

func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func runnerSupports(runner *Runner, taskType string) bool {
	if len(runner.TaskTypes) == 0 {
		return true
	}
	for _, supported := range runner.TaskTypes {
		if supported == taskType || supported == "*" {
			return true
		}
	}
	return false
}

func uniqueNonEmpty(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func cloneRunner(runner *Runner) *Runner {
	if runner == nil {
		return nil
	}
	copy := *runner
	copy.TaskTypes = append([]string(nil), runner.TaskTypes...)
	return &copy
}

func cloneTask(task *Task) *Task {
	if task == nil {
		return nil
	}
	copy := *task
	copy.Payload = cloneRaw(task.Payload)
	copy.Result = cloneRaw(task.Result)
	if task.StartedAt != nil {
		started := *task.StartedAt
		copy.StartedAt = &started
	}
	if task.StoppedAt != nil {
		stopped := *task.StoppedAt
		copy.StoppedAt = &stopped
	}
	return &copy
}

func cloneRaw(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func taskIDFromAction(path string, suffix string) string {
	value := strings.TrimSuffix(strings.TrimPrefix(path, "/tasks/"), suffix)
	return strings.Trim(value, "/")
}

func writeBrokerJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeBrokerError(w http.ResponseWriter, status int, message string) {
	writeBrokerJSON(w, status, map[string]any{"message": message})
}

func AwaitTask(ctx context.Context, broker *Broker, taskID string, interval time.Duration) (*Task, error) {
	if interval <= 0 {
		interval = 25 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		task, err := broker.Task(taskID)
		if err != nil {
			return nil, err
		}
		if task.Status == "succeeded" || task.Status == "failed" {
			return task, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}
