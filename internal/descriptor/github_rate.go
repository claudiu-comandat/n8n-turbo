package descriptor

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type GitHubRateLimiter struct {
	mu        sync.Mutex
	remaining int
	resetAt   time.Time
}

func NewGitHubRateLimiter() *GitHubRateLimiter {
	return &GitHubRateLimiter{remaining: 5000, resetAt: time.Now().Add(time.Hour)}
}

func (r *GitHubRateLimiter) UpdateFromHeaders(headers http.Header) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if remaining := headers.Get("X-RateLimit-Remaining"); remaining != "" {
		if value, err := strconv.Atoi(remaining); err == nil {
			r.remaining = value
		}
	}
	if reset := headers.Get("X-RateLimit-Reset"); reset != "" {
		if value, err := strconv.ParseInt(reset, 10, 64); err == nil {
			r.resetAt = time.Unix(value, 0)
		}
	}
}

func (r *GitHubRateLimiter) WaitIfNeeded(ctx context.Context) error {
	r.mu.Lock()
	remaining := r.remaining
	resetAt := r.resetAt
	r.mu.Unlock()
	if remaining > 10 || resetAt.IsZero() {
		return nil
	}
	wait := time.Until(resetAt) + 5*time.Second
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (r *GitHubRateLimiter) Status() (int, time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.remaining, r.resetAt
}

func HandleGitHubSecondaryRateLimit(resp *http.Response) (bool, time.Duration) {
	if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusTooManyRequests {
		return false, 0
	}
	if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
		if seconds, err := strconv.ParseInt(retryAfter, 10, 64); err == nil {
			return true, time.Duration(seconds) * time.Second
		}
	}
	if reset := resp.Header.Get("X-RateLimit-Reset"); reset != "" && resp.Header.Get("X-RateLimit-Remaining") == "0" {
		if value, err := strconv.ParseInt(reset, 10, 64); err == nil {
			wait := time.Until(time.Unix(value, 0))
			if wait > 0 {
				return true, wait
			}
		}
	}
	return false, 0
}

func GitHubRateLimitError(wait time.Duration) error {
	if wait < 0 {
		wait = 0
	}
	return fmt.Errorf("github rate limited; retry after %s", wait.Round(time.Second))
}

var defaultGitHubRateLimiter = NewGitHubRateLimiter()
