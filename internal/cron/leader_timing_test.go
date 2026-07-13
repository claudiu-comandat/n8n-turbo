package cron

import (
	"testing"
	"time"
)

// The dual-leadership window is closed only if a stalled renewal relinquishes
// leadership before the lease lapses, i.e. interval + callTimeout < ttl for
// every reachable configuration.
func TestLeaderRenewStaysWithinLease(t *testing.T) {
	cases := []struct {
		name     string
		ttl      time.Duration
		interval time.Duration // 0 = use the default derived from ttl
	}{
		{"default", 30 * time.Second, 0},
		{"misconfigured large interval", 10 * time.Second, 8 * time.Second},
		{"interval equals ttl", 10 * time.Second, 10 * time.Second},
		{"tiny ttl", 200 * time.Millisecond, 0},
		{"one second", time.Second, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := NewRedisLeaderWithClient(nil, "", "", tc.ttl)
			if tc.interval > 0 {
				l.interval = clampRenewInterval(tc.interval, l.ttl)
			}
			if l.interval <= 0 {
				t.Fatalf("interval must be positive, got %v", l.interval)
			}
			if timeout := l.callTimeout(); timeout <= 0 {
				t.Fatalf("callTimeout must be positive, got %v", timeout)
			}
			if got := l.interval + l.callTimeout(); got >= l.ttl {
				t.Fatalf("interval(%v)+callTimeout(%v)=%v must be < ttl(%v)", l.interval, l.callTimeout(), got, l.ttl)
			}
		})
	}
}
