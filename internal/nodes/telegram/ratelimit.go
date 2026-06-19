package telegram

import (
	"context"
	"sync"
	"time"
)

type RateLimiter struct {
	mu         sync.Mutex
	globalNext time.Time
	perChat    map[string]time.Time
}

func NewRateLimiter() *RateLimiter {
	return &RateLimiter{perChat: map[string]time.Time{}}
}

func (r *RateLimiter) Wait(ctx context.Context, chatID string) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	now := time.Now()
	waitUntil := r.globalNext
	if chatID != "" && r.perChat[chatID].After(waitUntil) {
		waitUntil = r.perChat[chatID]
	}
	if now.After(r.globalNext) {
		r.globalNext = now.Add(time.Second / 30)
	} else {
		r.globalNext = r.globalNext.Add(time.Second / 30)
	}
	if chatID != "" {
		if now.After(r.perChat[chatID]) {
			r.perChat[chatID] = now.Add(time.Second)
		} else {
			r.perChat[chatID] = r.perChat[chatID].Add(time.Second)
		}
	}
	r.mu.Unlock()
	if waitUntil.IsZero() || !waitUntil.After(now) {
		return nil
	}
	timer := time.NewTimer(waitUntil.Sub(now))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
