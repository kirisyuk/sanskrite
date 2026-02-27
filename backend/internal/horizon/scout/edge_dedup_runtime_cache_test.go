package scout

import (
	"testing"
	"time"
)

func TestEdgeRecentDedupCache_BasicTTL(t *testing.T) {
	engine := &ScoutEngine{
		edgeRecentDedup:           map[string]time.Time{},
		edgeRecentDedupTTL:        20 * time.Millisecond,
		edgeRecentDedupMaxEntries: 100,
	}
	key := "edge:src:dst:DEPENDS_ON"
	if engine.seenEdgeDedupRecently(key) {
		t.Fatalf("expected miss before remember")
	}
	engine.rememberEdgeDedupRecently(key)
	if !engine.seenEdgeDedupRecently(key) {
		t.Fatalf("expected hit after remember")
	}
	time.Sleep(30 * time.Millisecond)
	if engine.seenEdgeDedupRecently(key) {
		t.Fatalf("expected ttl expiry")
	}
}
