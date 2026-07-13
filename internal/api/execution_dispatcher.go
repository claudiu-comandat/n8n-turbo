package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
	"github.com/n8n-io/n8n-turbo/internal/engine/distribution"
	"github.com/n8n-io/n8n-turbo/internal/push"
	"github.com/redis/go-redis/v9"
)

type executionDispatcher struct {
	server      *Server
	distributor distribution.JobDistributor
	pool        *distribution.WorkerPool
	pending     sync.Map
	startOnce   sync.Once
	stopOnce    sync.Once
	stopped     atomic.Bool
	done        chan error
}

type executionDispatchRequest struct {
	ExecutionID     string
	Workflow        dataplane.Workflow
	Mode            string
	Options         engine.ExecuteOptions
	StartData       map[string]any
	PinData         map[string][]dataplane.Item
	PushRef         string
	ErrorName       string
	Timeout         time.Duration
	CrashOnDeadline bool
	Done            chan executionDispatchResult
}

type executionDispatchResult struct {
	Result   *engine.Result
	Status   string
	StartErr error
	RunErr   error
	StoreErr error
}

type executionDispatchPayload struct {
	ExecutionID     string
	Workflow        dataplane.Workflow
	Mode            string
	Destination     *engine.DestinationNode
	Variables       map[string]any
	Secrets         map[string]map[string]string
	TriggerNode     string
	TriggerItems    []dataplane.Item
	InitialInputs   map[string]map[int][]dataplane.Item
	StartNodes      []string
	RunData         dataplane.RunData
	PinData         map[string][]dataplane.Item
	StartData       map[string]any
	RetryOf         string
	PushRef         string
	ScheduledTime   string
	ResumeURL       string
	ResumeFormURL   string
	ResumeToken     string
	ErrorName       string
	TimeoutMillis   int64
	CrashOnDeadline bool
}

func newExecutionDispatcher(server *Server) (*executionDispatcher, error) {
	workers := server.config.Execution.ConcurrencyLimit
	if workers <= 0 {
		workers = 10
	}
	distributor, err := newExecutionJobDistributor(server)
	if err != nil {
		return nil, err
	}
	dispatcher := &executionDispatcher{server: server, distributor: distributor, done: make(chan error, 1)}
	dispatcher.pool = distribution.NewWorkerPool(distributor, dispatcher.executeJob, distribution.PoolConfig{
		NumWorkers:       workers,
		MaxJobsPerWorker: 1,
		ShutdownTimeout:  30 * time.Second,
		JobTimeout:       server.executionJobTimeout(),
	})
	return dispatcher, nil
}

func newExecutionJobDistributor(server *Server) (distribution.JobDistributor, error) {
	cfg := server.config.Execution
	switch cfg.DispatcherMode {
	case "", "local":
		workers := cfg.ConcurrencyLimit
		if workers <= 0 {
			workers = 10
		}
		return distribution.NewLocalDistributor(distribution.LocalConfig{QueueSize: cfg.ConcurrencyQueueSize, MaxWorkers: workers}), nil
	case "redis":
		return distribution.NewRedisDistributorFromOptions(&redis.Options{
			Addr:     cfg.DispatcherRedisAddr,
			Password: cfg.DispatcherRedisPassword,
			DB:       cfg.DispatcherRedisDB,
		}, distribution.RedisConfig{
			StreamKey:     cfg.DispatcherStream,
			GroupName:     cfg.DispatcherConsumer,
			ConsumerID:    server.config.Instance.ID,
			ResultStream:  cfg.DispatcherStream + ":results",
			DeadLetterKey: cfg.DispatcherStream + ":dead",
			Block:         time.Second,
		})
	default:
		return nil, fmt.Errorf("unsupported execution dispatcher %q", cfg.DispatcherMode)
	}
}

func (d *executionDispatcher) Start(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	d.startOnce.Do(func() {
		go func() {
			d.done <- d.pool.Run(ctx)
		}()
	})
}

func (d *executionDispatcher) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	d.stopped.Store(true)
	var err error
	d.stopOnce.Do(func() {
		if closeErr := d.distributor.Close(); closeErr != nil {
			err = closeErr
			return
		}
		err = d.pool.GracefulStop(ctx)
	})
	return err
}

func (d *executionDispatcher) RunSync(ctx context.Context, request executionDispatchRequest) executionDispatchResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if request.Done == nil {
		request.Done = make(chan executionDispatchResult, 1)
	}
	if err := d.Submit(ctx, request); err != nil {
		return executionDispatchResult{Status: "error", StartErr: err}
	}
	resultCh := make(chan distribution.JobResult, 1)
	if waiter, ok := d.distributor.(distribution.JobResultWaiter); ok {
		go func() {
			result, err := waiter.WaitResult(ctx, request.ExecutionID)
			if err != nil {
				resultCh <- distribution.JobResult{JobID: request.ExecutionID, WorkflowID: request.Workflow.ID, Success: false, Error: err.Error()}
				return
			}
			resultCh <- result
		}()
	}
	select {
	case result := <-request.Done:
		return result
	case result := <-resultCh:
		if result.Success {
			return executionDispatchResult{Status: "success"}
		}
		return executionDispatchResult{Status: "error", RunErr: errors.New(result.Error)}
	case <-ctx.Done():
		return executionDispatchResult{Status: "error", StartErr: ctx.Err()}
	}
}

func (d *executionDispatcher) Submit(ctx context.Context, request executionDispatchRequest) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if request.Done == nil {
		request.Done = make(chan executionDispatchResult, 1)
	}
	if d.stopped.Load() {
		return fmt.Errorf("execution dispatcher is shutting down")
	}
	dispatchPayload := dispatchPayloadFromRequest(request)
	payload, err := json.Marshal(dispatchPayload)
	if err != nil {
		return err
	}
	d.Start(d.server.runtimeCtx)
	d.pending.Store(request.ExecutionID, request.Done)
	err = d.distributor.Enqueue(ctx, dispatchJobFromRequest(request, payload, dispatchPayload.RetryOf))
	if err != nil {
		d.pending.Delete(request.ExecutionID)
		return err
	}
	return nil
}

func (d *executionDispatcher) executeJob(ctx context.Context, job distribution.Job) (distribution.JobResult, error) {
	var done chan executionDispatchResult
	if value, ok := d.pending.LoadAndDelete(job.ID); ok {
		done = value.(chan executionDispatchResult)
	}
	request, err := d.requestFromJob(job)
	if err != nil {
		result := executionDispatchResult{Status: "error", RunErr: err}
		if done != nil {
			select {
			case done <- result:
			default:
			}
		}
		return distribution.JobResult{JobID: job.ID, WorkflowID: job.WorkflowID, Success: false, Error: err.Error()}, nil
	}
	if done != nil {
		request.Done = done
	}
	result := d.server.executeWorkflowDirect(ctx, *request)
	select {
	case request.Done <- result:
	default:
	}
	text := ""
	if result.RunErr != nil {
		text = result.RunErr.Error()
	}
	if result.StoreErr != nil {
		text = result.StoreErr.Error()
	}
	return distribution.JobResult{JobID: job.ID, WorkflowID: job.WorkflowID, Success: result.Status == "success" || result.Status == "waiting", Error: text}, nil
}

func (d *executionDispatcher) requestFromJob(job distribution.Job) (*executionDispatchRequest, error) {
	if len(job.WorkflowData) == 0 {
		return nil, fmt.Errorf("execution payload not found")
	}
	var payload executionDispatchPayload
	if err := json.Unmarshal(job.WorkflowData, &payload); err != nil {
		return nil, err
	}
	return payload.toRequest(d.server, nil), nil
}

func (s *Server) dispatchWorkflowSync(ctx context.Context, request executionDispatchRequest) executionDispatchResult {
	if s.dispatcher == nil {
		return s.executeWorkflowDirect(ctx, request)
	}
	return s.dispatcher.RunSync(ctx, request)
}

func (s *Server) dispatchWorkflowAsync(ctx context.Context, request executionDispatchRequest) error {
	if s.dispatcher == nil {
		go s.executeWorkflowDirect(context.Background(), request)
		return nil
	}
	return s.dispatcher.Submit(ctx, request)
}

func (s *Server) executeWorkflowDirect(ctx context.Context, request executionDispatchRequest) executionDispatchResult {
	if ctx == nil {
		ctx = context.Background()
	}
	// Idempotency guard: if this execution already reached a terminal state (e.g. a
	// reclaimed job whose original run finished before it could be acknowledged),
	// don't run it again and repeat its side effects.
	if request.ExecutionID != "" {
		if row, err := s.executionStore.GetByID(ctx, request.ExecutionID); err == nil && row != nil && isTerminalExecutionStatus(row.Status) {
			return executionDispatchResult{Status: row.Status}
		}
	}
	request.Options = s.hydrateExecutionOptions(request.Options)
	baseCtx, cancelExecution := s.executionContext(ctx)
	defer cancelExecution()
	if request.Timeout > 0 {
		var cancelTimeout context.CancelFunc
		baseCtx, cancelTimeout = context.WithTimeout(baseCtx, request.Timeout)
		defer cancelTimeout()
	}
	execCtx, err := s.activeExecutions.Add(baseCtx, request.ExecutionID, request.Workflow.ID, request.Workflow.Name, request.Mode)
	if err != nil {
		return executionDispatchResult{Status: "error", StartErr: err}
	}
	defer s.activeExecutions.Remove(request.ExecutionID)
	options := request.Options
	options = s.prepareResumeOptions(request.ExecutionID, options)
	options.Mode = request.Mode
	result, runErr := s.evaluator.ExecuteWithOptions(execCtx, request.Workflow, request.ExecutionID, options)
	status := "success"
	var executionError *dataplane.ExecutionError
	if suspend, ok := engine.AsSuspendError(runErr); ok {
		data := dataplane.RunExecutionData{
			StartData:   request.StartData,
			ResumeToken: options.ResumeToken,
			ResultData: dataplane.ResultData{
				RunData:          resultRunData(result),
				PinData:          request.PinData,
				LastNodeExecuted: resultLastNode(result),
			},
		}
		waitCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		storeErr := s.executionStore.MarkWaiting(waitCtx, request.ExecutionID, suspend.ResumeAt, data)
		if s.pushHub != nil {
			s.pushHub.BroadcastToExecution(request.ExecutionID, push.ExecutionWaiting(request.ExecutionID))
		}
		return executionDispatchResult{Result: result, Status: "waiting", RunErr: nil, StoreErr: storeErr}
	}
	if runErr != nil {
		status = "error"
		if request.CrashOnDeadline && errors.Is(runErr, context.DeadlineExceeded) {
			status = "crashed"
		}
		errorName := request.ErrorName
		if errorName == "" {
			errorName = "WorkflowExecutionError"
		}
		executionError = &dataplane.ExecutionError{Name: errorName, Message: runErr.Error(), Timestamp: time.Now().UTC().UnixMilli()}
	}
	data := dataplane.RunExecutionData{
		StartData:   request.StartData,
		ResumeToken: options.ResumeToken,
		ResultData: dataplane.ResultData{
			RunData:          resultRunData(result),
			PinData:          request.PinData,
			LastNodeExecuted: resultLastNode(result),
			Error:            executionError,
		},
	}
	stoppedAt := time.Now().UTC()
	if result != nil {
		stoppedAt = result.StoppedAt
	}
	finishCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	storeErr := s.executionStore.Finish(finishCtx, request.ExecutionID, status, stoppedAt, data)
	if storeErr == nil && (status == "error" || status == "crashed") {
		s.launchErrorWorkflow(context.Background(), request, result, executionError)
	}
	return executionDispatchResult{Result: result, Status: status, RunErr: runErr, StoreErr: storeErr}
}

func isTerminalExecutionStatus(status string) bool {
	switch status {
	case "success", "error", "crashed":
		return true
	default:
		return false
	}
}

func dispatchPayloadFromRequest(request executionDispatchRequest) executionDispatchPayload {
	return executionDispatchPayload{
		ExecutionID:     request.ExecutionID,
		Workflow:        request.Workflow,
		Mode:            request.Mode,
		Destination:     request.Options.Destination,
		Variables:       request.Options.Variables,
		Secrets:         request.Options.Secrets,
		TriggerNode:     request.Options.TriggerNode,
		TriggerItems:    request.Options.TriggerItems,
		InitialInputs:   request.Options.InitialInputs,
		StartNodes:      request.Options.StartNodes,
		RunData:         request.Options.RunData,
		PinData:         request.Options.PinData,
		StartData:       request.StartData,
		RetryOf:         retryOfFromStartData(request.StartData),
		PushRef:         request.PushRef,
		ScheduledTime:   formatOptionalTime(request.Options.ScheduledTime),
		ResumeURL:       request.Options.ResumeURL,
		ResumeFormURL:   request.Options.ResumeFormURL,
		ResumeToken:     request.Options.ResumeToken,
		ErrorName:       request.ErrorName,
		TimeoutMillis:   request.Timeout.Milliseconds(),
		CrashOnDeadline: request.CrashOnDeadline,
	}
}

func (p executionDispatchPayload) toRequest(server *Server, done chan executionDispatchResult) *executionDispatchRequest {
	startData := p.StartData
	if p.RetryOf != "" {
		if startData == nil {
			startData = map[string]any{}
		} else {
			copied := make(map[string]any, len(startData)+1)
			for key, value := range startData {
				copied[key] = value
			}
			startData = copied
		}
		startData["retryOf"] = p.RetryOf
	}
	options := engine.ExecuteOptions{
		Destination:   p.Destination,
		Variables:     p.Variables,
		Secrets:       p.Secrets,
		TriggerNode:   p.TriggerNode,
		TriggerItems:  p.TriggerItems,
		InitialInputs: p.InitialInputs,
		StartNodes:    p.StartNodes,
		RunData:       p.RunData,
		PinData:       p.PinData,
		Mode:          p.Mode,
		ScheduledTime: parseOptionalTime(p.ScheduledTime),
		ResumeURL:     p.ResumeURL,
		ResumeFormURL: p.ResumeFormURL,
		ResumeToken:   p.ResumeToken,
	}
	if server != nil {
		options = server.hydrateExecutionOptions(options)
		if p.PushRef != "" {
			options.OnNodeAfter = func(event engine.NodeAfterEvent) {
				server.pushNodeAfterToSession(p.PushRef, event)
			}
			options.OnFinished = func(event engine.ExecutionFinishedEvent) {
				server.pushExecutionFinishedToSession(p.PushRef, event)
			}
		}
	}
	return &executionDispatchRequest{
		ExecutionID:     p.ExecutionID,
		Workflow:        p.Workflow,
		Mode:            p.Mode,
		Options:         options,
		StartData:       startData,
		PinData:         p.PinData,
		PushRef:         p.PushRef,
		ErrorName:       p.ErrorName,
		Timeout:         time.Duration(p.TimeoutMillis) * time.Millisecond,
		CrashOnDeadline: p.CrashOnDeadline,
		Done:            done,
	}
}

func dispatchJobFromRequest(request executionDispatchRequest, payload []byte, retryOf string) distribution.Job {
	return distribution.Job{
		ID:           request.ExecutionID,
		WorkflowID:   request.Workflow.ID,
		WorkflowData: payload,
		Mode:         request.Mode,
		Priority:     dispatchPriority(request.Mode),
		RetryOf:      retryOf,
	}
}

func retryOfFromStartData(startData map[string]any) string {
	value, ok := startData["retryOf"]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseOptionalTime(value string) time.Time {
	if strings.TrimSpace(value) == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func (s *Server) hydrateExecutionOptions(options engine.ExecuteOptions) engine.ExecuteOptions {
	if options.BinaryStore == nil {
		options.BinaryStore = s.binaryStore
	}
	if options.Credentials == nil && s.credentialStore != nil {
		options.Credentials = s.resolveNodeCredentials
	}
	if options.OnStarted == nil {
		options.OnStarted = s.pushExecutionStarted
	}
	if options.OnNodeAfter == nil {
		options.OnNodeAfter = s.pushNodeAfter
	}
	if options.OnFinished == nil {
		options.OnFinished = s.pushExecutionFinished
	}
	if options.Hooks == nil {
		options.Hooks = s.lifecycleHooks()
	}
	if options.SubWorkflow == nil && s.workflowStore != nil && s.executionStore != nil && s.evaluator != nil {
		options.SubWorkflow = s.executeSubWorkflow
	}
	return options
}

func (s *Server) prepareResumeOptions(executionID string, options engine.ExecuteOptions) engine.ExecuteOptions {
	if options.ResumeToken == "" {
		options.ResumeToken = randomResumeToken()
	}
	if options.ResumeURL == "" {
		options.ResumeURL = resumeURL(s.resumeBaseURL(), "webhook-waiting", executionID, options.ResumeToken)
	}
	if options.ResumeFormURL == "" {
		options.ResumeFormURL = resumeURL(s.resumeBaseURL(), "form-waiting", executionID, options.ResumeToken)
	}
	return options
}

func (s *Server) resumeBaseURL() string {
	if base := strings.TrimRight(s.config.WebhookBaseURL, "/"); base != "" {
		return base
	}
	if base := strings.TrimRight(s.config.EditorBaseURL, "/"); base != "" {
		return base
	}
	protocol := firstNonEmpty(s.config.Listen.Protocol, "http")
	host := firstNonEmpty(s.config.Listen.Host, "127.0.0.1")
	port := s.config.Listen.Port
	if port <= 0 {
		port = 5678
	}
	return fmt.Sprintf("%s://%s:%d", protocol, host, port)
}

func resumeURL(base string, endpoint string, executionID string, token string) string {
	raw := strings.TrimRight(base, "/") + "/" + strings.Trim(endpoint, "/") + "/" + url.PathEscape(executionID)
	if token == "" {
		return raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw + "?signature=" + url.QueryEscape(token)
	}
	values := parsed.Query()
	values.Set("signature", token)
	parsed.RawQuery = values.Encode()
	return parsed.String()
}

func randomResumeToken() string {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(bytes[:])
}

func (s *Server) executionJobTimeout() time.Duration {
	seconds := s.config.Execution.TimeoutSeconds
	if seconds <= 0 {
		seconds = s.config.Execution.MaxTimeoutSeconds
	}
	if seconds <= 0 {
		return 30 * time.Minute
	}
	return time.Duration(seconds) * time.Second
}

func dispatchPriority(mode string) int {
	switch engine.NormalizeExecutionMode(mode) {
	case engine.ExecutionModeWebhook, engine.ExecutionModeTrigger, engine.ExecutionModeScheduled, engine.ExecutionModeForm:
		return int(engine.PriorityHigh)
	case engine.ExecutionModeManual, engine.ExecutionModeRetry, engine.ExecutionModeWebhookTest, engine.ExecutionModeFormTest:
		return int(engine.PriorityNormal)
	default:
		return int(engine.PriorityLow)
	}
}

func executionStoreError(result executionDispatchResult) error {
	if result.StoreErr != nil {
		return result.StoreErr
	}
	if result.Status == "" {
		return fmt.Errorf("execution did not run")
	}
	return nil
}
