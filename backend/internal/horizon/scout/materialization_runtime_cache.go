package scout

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

const scoutRedisCacheTimeout = 70 * time.Millisecond

type supplyChainFragmentRef struct {
	ID            string `json:"id"`
	CanonicalPath string `json:"canonicalPath"`
	Type          string `json:"type"`
	TenantID      string `json:"tenantId"`
}

func scoutRedisTTL(key string, def time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return def
	}
	return d
}

func (e *ScoutEngine) getSupplyChainJobFragmentRef(ctx context.Context, jobID, canonicalPath string) *supplyChainFragmentRef {
	if e == nil || e.redisClient == nil {
		return nil
	}
	jobID = strings.TrimSpace(jobID)
	canonicalPath = strings.TrimSpace(canonicalPath)
	if jobID == "" || canonicalPath == "" {
		return nil
	}
	rctx, cancel := context.WithTimeout(ctx, scoutRedisCacheTimeout)
	defer cancel()
	val, err := e.redisClient.Get(rctx, "h:scout:job:"+jobID+":pathToId:"+engineSha1Hex(canonicalPath)).Bytes()
	if err != nil || len(val) == 0 {
		return nil
	}
	var ref supplyChainFragmentRef
	if err := json.Unmarshal(val, &ref); err != nil {
		return nil
	}
	return &ref
}

func (e *ScoutEngine) setSupplyChainJobFragmentRef(ctx context.Context, jobID string, fragment *models.HorizonFragment) {
	if e == nil || e.redisClient == nil || fragment == nil {
		return
	}
	jobID = strings.TrimSpace(jobID)
	canonicalPath := strings.TrimSpace(fragment.CanonicalPath)
	if jobID == "" || canonicalPath == "" {
		return
	}
	payload, err := json.Marshal(supplyChainFragmentRef{
		ID:            strings.TrimSpace(fragment.ID),
		CanonicalPath: canonicalPath,
		Type:          string(fragment.Type),
		TenantID:      strings.TrimSpace(fragment.TenantID),
	})
	if err != nil {
		return
	}
	rctx, cancel := context.WithTimeout(ctx, scoutRedisCacheTimeout)
	defer cancel()
	_ = e.redisClient.Set(rctx, "h:scout:job:"+jobID+":pathToId:"+engineSha1Hex(canonicalPath), payload, scoutRedisTTL("HORIZON_REDIS_JOB_RUNTIME_TTL", 2*time.Hour)).Err()
}

func (e *ScoutEngine) getSupplyLinkRefCached(ctx context.Context, lookupKey string) *supplyLinkMaterializationRef {
	if e == nil || e.redisClient == nil {
		return nil
	}
	lookupKey = strings.TrimSpace(lookupKey)
	if lookupKey == "" {
		return nil
	}
	rctx, cancel := context.WithTimeout(ctx, scoutRedisCacheTimeout)
	defer cancel()
	val, err := e.redisClient.Get(rctx, "h:supplylink:key:"+engineSha1Hex(lookupKey)).Bytes()
	if err != nil || len(val) == 0 {
		return nil
	}
	var ref supplyLinkMaterializationRef
	if err := json.Unmarshal(val, &ref); err != nil {
		return nil
	}
	return &ref
}

func (e *ScoutEngine) setSupplyLinkRefCached(ctx context.Context, lookupKey string, ref supplyLinkMaterializationRef) {
	if e == nil || e.redisClient == nil {
		return
	}
	lookupKey = strings.TrimSpace(lookupKey)
	if lookupKey == "" || strings.TrimSpace(ref.CanonicalPath) == "" {
		return
	}
	payload, err := json.Marshal(ref)
	if err != nil {
		return
	}
	rctx, cancel := context.WithTimeout(ctx, scoutRedisCacheTimeout)
	defer cancel()
	_ = e.redisClient.Set(rctx, "h:supplylink:key:"+engineSha1Hex(lookupKey), payload, scoutRedisTTL("HORIZON_REDIS_SUPPLY_CACHE_TTL", 30*time.Minute)).Err()
}

func (e *ScoutEngine) hasSupplyChainEdgeSeen(ctx context.Context, dedupKey string) bool {
	if e == nil || e.redisClient == nil {
		return false
	}
	dedupKey = strings.TrimSpace(dedupKey)
	if dedupKey == "" {
		return false
	}
	rctx, cancel := context.WithTimeout(ctx, scoutRedisCacheTimeout)
	defer cancel()
	val, err := e.redisClient.Exists(rctx, "h:edge:dedup:"+engineSha1Hex(dedupKey)).Result()
	return err == nil && val > 0
}

func (e *ScoutEngine) markSupplyChainEdgeSeen(ctx context.Context, dedupKey string) {
	if e == nil || e.redisClient == nil {
		return
	}
	dedupKey = strings.TrimSpace(dedupKey)
	if dedupKey == "" {
		return
	}
	rctx, cancel := context.WithTimeout(ctx, scoutRedisCacheTimeout)
	defer cancel()
	_ = e.redisClient.Set(rctx, "h:edge:dedup:"+engineSha1Hex(dedupKey), "1", scoutRedisTTL("HORIZON_REDIS_EDGE_DEDUP_TTL", 30*time.Minute)).Err()
}
