package discord

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type Bucket struct {
	mu        sync.Mutex
	remaining int
	resetAt   time.Time
	limit     int
}

func (b *Bucket) Wait(ctx context.Context) error {
	b.mu.Lock()
	wait := b.remaining <= 0 && time.Now().Before(b.resetAt)
	resetAt := b.resetAt
	b.mu.Unlock()
	if !wait {
		return nil
	}
	timer := time.NewTimer(time.Until(resetAt))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (b *Bucket) Update(response *http.Response) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if value := response.Header.Get("X-RateLimit-Remaining"); value != "" {
		b.remaining, _ = strconv.Atoi(value)
	}
	if value := response.Header.Get("X-RateLimit-Limit"); value != "" {
		b.limit, _ = strconv.Atoi(value)
	}
	if value := response.Header.Get("X-RateLimit-Reset"); value != "" {
		seconds, _ := strconv.ParseFloat(value, 64)
		whole := int64(seconds)
		b.resetAt = time.Unix(whole, int64((seconds-float64(whole))*1e9))
	}
}

type RateLimiter struct {
	mu      sync.RWMutex
	buckets map[string]*Bucket
	global  <-chan time.Time
}

func NewRateLimiter() *RateLimiter {
	ticker := time.NewTicker(time.Second / 50)
	return &RateLimiter{buckets: map[string]*Bucket{}, global: ticker.C}
}

func (r *RateLimiter) Wait(ctx context.Context, bucketID string) error {
	if r == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.global:
	}
	return r.bucket(bucketID).Wait(ctx)
}

func (r *RateLimiter) Update(bucketID string, response *http.Response) {
	if r == nil || response == nil {
		return
	}
	if real := response.Header.Get("X-RateLimit-Bucket"); real != "" {
		bucketID = real
	}
	r.bucket(bucketID).Update(response)
}

func (r *RateLimiter) bucket(bucketID string) *Bucket {
	r.mu.RLock()
	bucket, ok := r.buckets[bucketID]
	r.mu.RUnlock()
	if ok {
		return bucket
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if bucket, ok = r.buckets[bucketID]; ok {
		return bucket
	}
	bucket = &Bucket{remaining: 5, limit: 5}
	r.buckets[bucketID] = bucket
	return bucket
}
