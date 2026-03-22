package main

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"time"
)

const productSynonymRedisTimeout = 150 * time.Millisecond

type pairKnowledgeCacheDoc struct {
	ExactDocIDs        []string          `json:"exactDocIds"`
	MappedSynonymPaths map[string]string `json:"mappedSynonymPaths"`
}

type cachedFragmentInfo struct {
	Exists bool                `json:"exists"`
	Info   currentFragmentInfo `json:"info"`
}

func productSynonymRedisTTL() time.Duration {
	raw := strings.TrimSpace(os.Getenv("HORIZON_PRODUCT_SYNONYM_REDIS_TTL"))
	if raw == "" {
		return 2 * time.Hour
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 2 * time.Hour
	}
	return d
}

func pairKnowledgeRedisKey() string {
	tenant := strings.TrimSpace(*tenantID)
	if tenant == "" {
		tenant = "global"
	}
	return "h:product-synonyms:pair-knowledge:" + tenant + ":" + strings.TrimSpace(*pairCollection)
}

func fragmentInfoRedisKey(fragmentID string) string {
	return "h:product-synonyms:fragment-info:" + strings.TrimSpace(fragmentID)
}

func getCachedPairKnowledge(ctx context.Context) (pairKnowledge, bool) {
	if toolRedisClient == nil {
		return pairKnowledge{}, false
	}
	rctx, cancel := context.WithTimeout(ctx, productSynonymRedisTimeout)
	defer cancel()
	raw, err := toolRedisClient.Get(rctx, pairKnowledgeRedisKey()).Bytes()
	if err != nil || len(raw) == 0 {
		return pairKnowledge{}, false
	}
	var cached pairKnowledgeCacheDoc
	if err := json.Unmarshal(raw, &cached); err != nil {
		return pairKnowledge{}, false
	}
	out := pairKnowledge{
		exactDocIDs:        map[string]struct{}{},
		mappedSynonymPaths: map[string]string{},
		reviewedPairIDs:    map[string]struct{}{},
	}
	for _, docID := range cached.ExactDocIDs {
		docID = strings.TrimSpace(docID)
		if docID != "" {
			out.exactDocIDs[docID] = struct{}{}
		}
	}
	for k, v := range cached.MappedSynonymPaths {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k != "" && v != "" {
			out.mappedSynonymPaths[k] = v
		}
	}
	return out, true
}

func setCachedPairKnowledge(ctx context.Context, knowledge pairKnowledge) {
	if toolRedisClient == nil {
		return
	}
	exactDocIDs := make([]string, 0, len(knowledge.exactDocIDs))
	for docID := range knowledge.exactDocIDs {
		exactDocIDs = append(exactDocIDs, docID)
	}
	sort.Strings(exactDocIDs)
	payload, err := json.Marshal(pairKnowledgeCacheDoc{
		ExactDocIDs:        exactDocIDs,
		MappedSynonymPaths: knowledge.mappedSynonymPaths,
	})
	if err != nil {
		return
	}
	rctx, cancel := context.WithTimeout(ctx, productSynonymRedisTimeout)
	defer cancel()
	_ = toolRedisClient.Set(rctx, pairKnowledgeRedisKey(), payload, productSynonymRedisTTL()).Err()
}

func getCachedFragmentInfo(ctx context.Context, fragmentID string) (cachedFragmentInfo, bool) {
	if toolRedisClient == nil {
		return cachedFragmentInfo{}, false
	}
	fragmentID = strings.TrimSpace(fragmentID)
	if fragmentID == "" {
		return cachedFragmentInfo{}, false
	}
	rctx, cancel := context.WithTimeout(ctx, productSynonymRedisTimeout)
	defer cancel()
	raw, err := toolRedisClient.Get(rctx, fragmentInfoRedisKey(fragmentID)).Bytes()
	if err != nil || len(raw) == 0 {
		return cachedFragmentInfo{}, false
	}
	var cached cachedFragmentInfo
	if err := json.Unmarshal(raw, &cached); err != nil {
		return cachedFragmentInfo{}, false
	}
	return cached, true
}

func setCachedFragmentInfo(ctx context.Context, fragmentID string, cached cachedFragmentInfo) {
	if toolRedisClient == nil {
		return
	}
	fragmentID = strings.TrimSpace(fragmentID)
	if fragmentID == "" {
		return
	}
	payload, err := json.Marshal(cached)
	if err != nil {
		return
	}
	rctx, cancel := context.WithTimeout(ctx, productSynonymRedisTimeout)
	defer cancel()
	_ = toolRedisClient.Set(rctx, fragmentInfoRedisKey(fragmentID), payload, productSynonymRedisTTL()).Err()
}
