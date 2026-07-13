package distribution

import (
	"testing"
	"time"
)

// The reclaim threshold must stay above the keepalive refresh window, otherwise
// a live job (whose entry is refreshed every KeepaliveInterval) could be
// reclaimed by the reaper and executed a second time.
func TestReclaimMinIdleExceedsKeepalive(t *testing.T) {
	cases := []RedisConfig{
		{},                                    // all defaults
		{KeepaliveInterval: 30 * time.Second}, // reclaim must be bumped to >= 90s
		{ReclaimMinIdle: time.Second},         // too-small reclaim gets raised
		{KeepaliveInterval: time.Minute, ReclaimMinIdle: time.Second}, // both adjusted
	}
	for _, cfg := range cases {
		got := normalizeRedisConfig(cfg)
		if got.ReclaimMinIdle < 3*got.KeepaliveInterval {
			t.Fatalf("ReclaimMinIdle(%v) must be >= 3*KeepaliveInterval(%v)", got.ReclaimMinIdle, got.KeepaliveInterval)
		}
		if got.KeepaliveInterval <= 0 || got.ReclaimInterval <= 0 {
			t.Fatalf("intervals must be positive: %+v", got)
		}
	}
}
