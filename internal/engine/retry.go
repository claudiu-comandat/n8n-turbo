package engine

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

type RetryExhaustedError struct {
	NodeName  string
	Attempts  int
	LastError error
}

func (e *RetryExhaustedError) Error() string {
	return fmt.Sprintf("node %s failed after %d attempt(s): %v", e.NodeName, e.Attempts, e.LastError)
}

func (e *RetryExhaustedError) Unwrap() error {
	return e.LastError
}

func executeNodeWithRetry(ctx context.Context, executor NodeExecutor, input ExecuteInput) (dataplane.Output, error) {
	cfg := input.Node.RetryConfig()
	if !cfg.Enabled || cfg.MaxTries <= 1 {
		return executor.Execute(ctx, input)
	}
	var lastErr error
	attempts := 0
	for attempt := 1; attempt <= cfg.MaxTries; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		attempts = attempt
		attemptInput := input
		attemptInput.RunIndex = attempt - 1
		output, err := executor.Execute(ctx, attemptInput)
		if err == nil {
			return output, nil
		}
		lastErr = err
		if !isRetriableError(err) || attempt >= cfg.MaxTries {
			break
		}
		timer := time.NewTimer(retryWait(cfg, attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, &RetryExhaustedError{NodeName: input.Node.Name, Attempts: attempts, LastError: lastErr}
}

func retryWait(cfg dataplane.RetryConfig, attempt int) time.Duration {
	wait := time.Duration(cfg.WaitBetweenTries) * time.Millisecond
	if cfg.UseExponentialBackoff {
		wait *= time.Duration(1 << max(0, attempt-1))
	}
	if wait > 5*time.Minute {
		return 5 * time.Minute
	}
	return wait
}

func isRetriableError(err error) bool {
	if err == nil || isContextError(err) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	return true
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
