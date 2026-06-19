package api

import (
	"context"
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/cron"
	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
	"github.com/n8n-io/n8n-turbo/internal/nodes"
)

type scheduler struct {
	mu      sync.Mutex
	jobs    map[string]scheduledJob
	running map[string]bool
	leader  schedulerLeader
}

type schedulerLeader interface {
	Start(ctx context.Context)
	Stop(ctx context.Context) error
	IsLeader() bool
}

type scheduledJob struct {
	cancel   context.CancelFunc
	version  string
	workflow string
	node     string
}

type scheduledCandidate struct {
	workflow dataplane.Workflow
	node     dataplane.Node
}

func newScheduler() *scheduler {
	return &scheduler{jobs: make(map[string]scheduledJob), running: make(map[string]bool)}
}

func (s *Server) StartRuntime(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.runtimeCtx = ctx
	if s.secretsManager != nil {
		s.secretsManager.StartRefresh(ctx, 5*time.Minute)
	}
	if s.dispatcher != nil {
		s.dispatcher.Start(ctx)
	}
	if s.scheduler != nil && s.scheduler.leader != nil {
		s.scheduler.leader.Start(ctx)
	}
	go s.runtimeLoop(ctx)
}

func (s *Server) runtimeLoop(ctx context.Context) {
	if err := s.syncScheduledWorkflows(ctx); err != nil {
		log.Printf("sync scheduled workflows: %v", err)
	}
	if err := s.resumeDueWaitingExecutions(ctx, time.Now().UTC()); err != nil {
		log.Printf("resume waiting executions: %v", err)
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			if s.secretsManager != nil {
				s.secretsManager.StopRefresh()
			}
			if s.dispatcher != nil {
				stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				if err := s.dispatcher.Stop(stopCtx); err != nil {
					log.Printf("stop execution dispatcher: %v", err)
				}
				cancel()
			}
			if s.scheduler != nil && s.scheduler.leader != nil {
				stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				if err := s.scheduler.leader.Stop(stopCtx); err != nil {
					log.Printf("stop scheduler leader: %v", err)
				}
				cancel()
			}
			s.stopScheduledJobs()
			s.activeExecutions.StopAll()
			return
		case <-ticker.C:
			if err := s.syncScheduledWorkflows(ctx); err != nil {
				log.Printf("sync scheduled workflows: %v", err)
			}
			if err := s.resumeDueWaitingExecutions(ctx, time.Now().UTC()); err != nil {
				log.Printf("resume waiting executions: %v", err)
			}
		}
	}
}

func (s *Server) syncScheduledWorkflows(ctx context.Context) error {
	rows, err := s.workflowStore.List(ctx, 250)
	if err != nil {
		return err
	}
	desired := make(map[string]scheduledCandidate)
	for _, row := range rows {
		if !row.Active {
			continue
		}
		workflow, err := workflowFromRow(&row)
		if err != nil {
			log.Printf("load active workflow %s: %v", row.ID, err)
			continue
		}
		for _, node := range workflow.Nodes {
			if node.Disabled || node.Type != "n8n-nodes-base.scheduleTrigger" {
				continue
			}
			desired[scheduledKey(workflow.ID, node.Name)] = scheduledCandidate{workflow: workflow, node: node}
		}
	}
	s.scheduler.mu.Lock()
	defer s.scheduler.mu.Unlock()
	for key, job := range s.scheduler.jobs {
		candidate, ok := desired[key]
		if !ok || candidate.workflow.VersionID != job.version {
			job.cancel()
			delete(s.scheduler.jobs, key)
		}
	}
	for key, candidate := range desired {
		if _, ok := s.scheduler.jobs[key]; ok {
			continue
		}
		jobCtx, cancel := context.WithCancel(ctx)
		s.scheduler.jobs[key] = scheduledJob{cancel: cancel, version: candidate.workflow.VersionID, workflow: candidate.workflow.ID, node: candidate.node.Name}
		go s.scheduledLoop(jobCtx, candidate.workflow, candidate.node)
	}
	return nil
}

func (s *Server) startWorkflowSchedule(ctx context.Context, workflow dataplane.Workflow) {
	candidates := make(map[string]scheduledCandidate)
	for _, node := range workflow.Nodes {
		if node.Disabled || node.Type != "n8n-nodes-base.scheduleTrigger" {
			continue
		}
		candidates[scheduledKey(workflow.ID, node.Name)] = scheduledCandidate{workflow: workflow, node: node}
	}
	s.scheduler.mu.Lock()
	defer s.scheduler.mu.Unlock()
	for key, candidate := range candidates {
		if job, ok := s.scheduler.jobs[key]; ok {
			if job.version == workflow.VersionID {
				continue
			}
			job.cancel()
			delete(s.scheduler.jobs, key)
		}
		jobCtx, cancel := context.WithCancel(ctx)
		s.scheduler.jobs[key] = scheduledJob{cancel: cancel, version: workflow.VersionID, workflow: workflow.ID, node: candidate.node.Name}
		go s.scheduledLoop(jobCtx, workflow, candidate.node)
	}
}

func (s *Server) stopWorkflowSchedule(workflowID string) {
	s.scheduler.mu.Lock()
	defer s.scheduler.mu.Unlock()
	for key, job := range s.scheduler.jobs {
		if job.workflow != workflowID {
			continue
		}
		job.cancel()
		delete(s.scheduler.jobs, key)
	}
}

func (s *Server) stopScheduledJobs() {
	s.scheduler.mu.Lock()
	defer s.scheduler.mu.Unlock()
	for key, job := range s.scheduler.jobs {
		job.cancel()
		delete(s.scheduler.jobs, key)
	}
}

func (s *Server) scheduledLoop(ctx context.Context, workflow dataplane.Workflow, node dataplane.Node) {
	expr, err := nodes.BuildScheduleCronExpression(node.Parameters)
	if err != nil {
		log.Printf("invalid schedule trigger for workflow %s node %s: %v", workflow.ID, node.Name, err)
		return
	}
	location := time.UTC
	timezone := nodes.ScheduleTimezone(node.Parameters)
	if timezone != "" {
		loaded, err := time.LoadLocation(timezone)
		if err != nil {
			log.Printf("invalid schedule timezone for workflow %s node %s: %v", workflow.ID, node.Name, err)
		} else {
			location = loaded
		}
	}
	schedule, err := cron.Parse(expr, location)
	if err != nil {
		log.Printf("invalid schedule expression for workflow %s node %s: %v", workflow.ID, node.Name, err)
		return
	}
	after := time.Now().In(location)
	for {
		next := schedule.Next(after)
		if next.IsZero() {
			log.Printf("schedule has no next run for workflow %s node %s", workflow.ID, node.Name)
			return
		}
		wait := time.Until(next)
		if wait < 0 {
			wait = 0
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			s.runScheduledWorkflow(ctx, workflow, node, next)
			after = time.Now().In(location)
		}
	}
}

func (s *Server) runScheduledWorkflow(ctx context.Context, workflow dataplane.Workflow, node dataplane.Node, at time.Time) {
	if !s.scheduler.canRunAsLeader() {
		log.Printf("skip scheduled execution for workflow %s: instance is scheduler follower", workflow.ID)
		return
	}
	if !s.scheduler.markWorkflowRunning(workflow) {
		log.Printf("skip scheduled execution for workflow %s: already running", workflow.ID)
		return
	}
	defer s.scheduler.clearWorkflowRunning(workflow.ID)
	mode := engine.ExecutionModeScheduled.String()
	execution, err := s.executionStore.Create(ctx, workflow, mode)
	if err != nil {
		log.Printf("create scheduled execution for workflow %s: %v", workflow.ID, err)
		return
	}
	variables, err := s.resolvedVariablesContext(ctx)
	if err != nil {
		log.Printf("resolve variables for scheduled execution %s: %v", execution.ID, err)
		variables = map[string]any{}
	}
	secrets, err := s.resolvedSecrets(ctx)
	if err != nil {
		log.Printf("resolve external secrets for scheduled execution %s: %v", execution.ID, err)
		secrets = map[string]map[string]string{}
	}
	dispatchResult := s.dispatchWorkflowSync(ctx, executionDispatchRequest{
		ExecutionID: execution.ID,
		Workflow:    workflow,
		Mode:        mode,
		Options: engine.ExecuteOptions{
			Variables:     variables,
			Secrets:       secrets,
			BinaryStore:   s.binaryStore,
			Credentials:   s.resolveNodeCredentials,
			TriggerNode:   node.Name,
			TriggerItems:  []dataplane.Item{scheduledItem(node, at)},
			ScheduledTime: at.UTC(),
			OnStarted:     s.pushExecutionStarted,
			OnNodeAfter:   s.pushNodeAfter,
			OnFinished:    s.pushExecutionFinished,
		},
		StartData:       map[string]any{"triggerNode": node.Name, "scheduledTime": at.UTC().Format(time.RFC3339Nano)},
		ErrorName:       "ScheduledExecutionError",
		Timeout:         scheduledExecutionTimeout(workflow),
		CrashOnDeadline: true,
	})
	if dispatchResult.StartErr != nil {
		log.Printf("start scheduled execution %s: %v", execution.ID, dispatchResult.StartErr)
		return
	}
	if dispatchResult.StoreErr != nil {
		log.Printf("finish scheduled execution %s: %v", execution.ID, dispatchResult.StoreErr)
	}
}

func scheduledItem(node dataplane.Node, at time.Time) dataplane.Item {
	return nodes.ScheduledTriggerItem(node, at)
}

func (s *scheduler) markWorkflowRunning(workflow dataplane.Workflow) bool {
	mode := scheduledConcurrencyMode(workflow)
	if mode == "parallel" {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running[workflow.ID] {
		return false
	}
	s.running[workflow.ID] = true
	return true
}

func (s *scheduler) clearWorkflowRunning(workflowID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.running, workflowID)
}

func (s *scheduler) canRunAsLeader() bool {
	if s == nil || s.leader == nil {
		return true
	}
	return s.leader.IsLeader()
}

func scheduledConcurrencyMode(workflow dataplane.Workflow) string {
	if workflow.Settings == nil {
		return "skip"
	}
	mode := stringValue(workflow.Settings["scheduledConcurrency"])
	if mode == "" {
		mode = stringValue(workflow.Settings["concurrency"])
	}
	if mode == "" {
		switch stringValue(workflow.Settings["executionOrder"]) {
		case "v0":
			mode = "parallel"
		case "v1":
			mode = "skip"
		}
	}
	if mode != "parallel" && mode != "queue" {
		mode = "skip"
	}
	return mode
}

func scheduledExecutionTimeout(workflow dataplane.Workflow) time.Duration {
	if workflow.Settings == nil {
		return 30 * time.Minute
	}
	for _, key := range []string{"executionTimeout", "timeout", "maxExecutionTime"} {
		value := numberValue(workflow.Settings[key])
		if value > 0 {
			return time.Duration(value * float64(time.Second))
		}
	}
	return 30 * time.Minute
}

func scheduledKey(workflowID string, nodeName string) string {
	return fmt.Sprintf("%s\x00%s", workflowID, nodeName)
}

func scheduleInterval(parameters map[string]any) time.Duration {
	if interval, ok := parameters["interval"]; ok {
		if duration := intervalRulesDuration(interval); duration > 0 {
			return clampScheduleInterval(duration)
		}
	}
	if duration := intervalRuleDuration(parameters); duration > 0 {
		return clampScheduleInterval(duration)
	}
	return time.Minute
}

func intervalRulesDuration(value any) time.Duration {
	switch typed := value.(type) {
	case []any:
		for _, entry := range typed {
			if duration := intervalRuleDuration(anyMap(entry)); duration > 0 {
				return duration
			}
		}
	case map[string]any:
		return intervalRuleDuration(typed)
	}
	return 0
}

func intervalRuleDuration(rule map[string]any) time.Duration {
	if len(rule) == 0 {
		return 0
	}
	field := stringValue(rule["field"])
	if field == "" {
		field = stringValue(rule["unit"])
	}
	if field == "" {
		field = "seconds"
	}
	keys := []string{"amount", "every", "interval"}
	switch field {
	case "second", "seconds":
		keys = append([]string{"secondsInterval"}, keys...)
	case "minute", "minutes":
		keys = append([]string{"minutesInterval"}, keys...)
	case "hour", "hours":
		keys = append([]string{"hoursInterval", "hourInterval"}, keys...)
	case "day", "days":
		keys = append([]string{"daysInterval"}, keys...)
	case "week", "weeks":
		keys = append([]string{"weeksInterval"}, keys...)
	}
	amount := 0.0
	for _, key := range keys {
		amount = numberValue(rule[key])
		if amount > 0 {
			break
		}
	}
	if amount <= 0 {
		return 0
	}
	switch field {
	case "millisecond", "milliseconds":
		return time.Duration(amount * float64(time.Millisecond))
	case "second", "seconds":
		return time.Duration(amount * float64(time.Second))
	case "minute", "minutes":
		return time.Duration(amount * float64(time.Minute))
	case "hour", "hours":
		return time.Duration(amount * float64(time.Hour))
	case "day", "days":
		return time.Duration(amount * float64(24*time.Hour))
	case "week", "weeks":
		return time.Duration(amount * float64(7*24*time.Hour))
	}
	return time.Duration(amount * float64(time.Second))
}

func clampScheduleInterval(duration time.Duration) time.Duration {
	if duration < time.Second {
		return time.Second
	}
	return duration
}

func anyMap(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	default:
		return map[string]any{}
	}
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	default:
		return fmt.Sprint(value)
	}
}

func numberValue(value any) float64 {
	switch typed := value.(type) {
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return 0
		}
		return typed
	case jsonNumber:
		parsed, _ := typed.Float64()
		return parsed
	default:
		return 0
	}
}

type jsonNumber interface {
	Float64() (float64, error)
}
