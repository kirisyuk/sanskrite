package scout

import (
	"context"
	"strings"
	"time"
)

const edgeDedupRedisPrefix = "h:ededup:"
const edgeDedupRedisTimeout = 70 * time.Millisecond

func (e *ScoutEngine) seenEdgeDedupRecently(key string) bool {
	if e == nil {
		return false
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	ttl := e.edgeRecentDedupTTL
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	now := time.Now().UTC()
	e.edgeRecentDedupMu.Lock()
	defer e.edgeRecentDedupMu.Unlock()
	e.pruneEdgeDedupCacheLocked(now, ttl)
	if e.edgeRecentDedup == nil {
		e.edgeRecentDedup = make(map[string]time.Time)
	}
	expiresAt, ok := e.edgeRecentDedup[key]
	if ok && !now.After(expiresAt) {
		return true
	}
	if ok && now.After(expiresAt) {
		delete(e.edgeRecentDedup, key)
	}
	if e.redisClient != nil {
		rctx, cancel := context.WithTimeout(context.Background(), edgeDedupRedisTimeout)
		val, err := e.redisClient.Get(rctx, edgeDedupRedisPrefix+key).Result()
		cancel()
		if err == nil && val == "1" {
			e.edgeRecentDedup[key] = now.Add(ttl)
			return true
		}
	}
	return false
}

func (e *ScoutEngine) rememberEdgeDedupRecently(key string) {
	if e == nil {
		return
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	ttl := e.edgeRecentDedupTTL
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	maxEntries := e.edgeRecentDedupMaxEntries
	if maxEntries <= 0 {
		maxEntries = 20000
	}
	now := time.Now().UTC()
	e.edgeRecentDedupMu.Lock()
	defer e.edgeRecentDedupMu.Unlock()
	if e.edgeRecentDedup == nil {
		e.edgeRecentDedup = make(map[string]time.Time)
	}
	e.pruneEdgeDedupCacheLocked(now, ttl)
	if len(e.edgeRecentDedup) >= maxEntries {
		e.edgeRecentDedup = make(map[string]time.Time, maxEntries/2)
	}
	e.edgeRecentDedup[key] = now.Add(ttl)
	if e.redisClient != nil {
		rctx, cancel := context.WithTimeout(context.Background(), edgeDedupRedisTimeout)
		_ = e.redisClient.Set(rctx, edgeDedupRedisPrefix+key, "1", ttl).Err()
		cancel()
	}
}

func (e *ScoutEngine) pruneEdgeDedupCacheLocked(now time.Time, ttl time.Duration) {
	if e == nil || e.edgeRecentDedup == nil || len(e.edgeRecentDedup) == 0 {
		return
	}
	_ = ttl
	for k, expiresAt := range e.edgeRecentDedup {
		if now.After(expiresAt) {
			delete(e.edgeRecentDedup, k)
		}
	}
}
