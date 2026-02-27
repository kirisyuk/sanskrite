package wikidata

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/ontology"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

type CacheOptions struct {
	Enabled bool
	TTL     time.Duration
}

// CacheOptionsFromEnv reads cache settings for Wikidata.
//
// Env:
//   - ENABLE_EXTERNAL_DATA_CACHE=true|1 (default: true)
//   - EXTERNAL_DATA_CACHE_TTL_HOURS (default: 168 = 7 days)
//   - WIKIDATA_CACHE_TTL_HOURS (optional override)
func CacheOptionsFromEnv() CacheOptions {
	enabledRaw := strings.ToLower(strings.TrimSpace(os.Getenv("ENABLE_EXTERNAL_DATA_CACHE")))
	enabled := true
	if enabledRaw != "" {
		enabled = enabledRaw == "1" || enabledRaw == "true" || enabledRaw == "yes" || enabledRaw == "y" || enabledRaw == "on"
	}

	ttlHours, _ := strconv.Atoi(strings.TrimSpace(os.Getenv("EXTERNAL_DATA_CACHE_TTL_HOURS")))
	if ttlHours <= 0 {
		ttlHours = 24 * 7
	}
	if v := strings.TrimSpace(os.Getenv("WIKIDATA_CACHE_TTL_HOURS")); v != "" {
		if h, err := strconv.Atoi(v); err == nil && h > 0 {
			ttlHours = h
		}
	}
	return CacheOptions{Enabled: enabled, TTL: time.Duration(ttlHours) * time.Hour}
}

// CachedClient wraps a WikidataClientInterface and adds cache-first behavior
// via ExternalDataCacheRepository (Firestore + optional Redis L1).
type CachedClient struct {
	base  ontology.WikidataClientInterface
	cache *repo.ExternalDataCacheRepository
	opts  CacheOptions
}

func NewCachedClient(base ontology.WikidataClientInterface, cache *repo.ExternalDataCacheRepository, opts CacheOptions) *CachedClient {
	return &CachedClient{base: base, cache: cache, opts: opts}
}

func (c *CachedClient) GetEntityData(ctx context.Context, qID string) (*ontology.WikidataEntityData, error) {
	if c == nil || c.base == nil {
		return nil, nil
	}
	qID = strings.TrimSpace(qID)
	if qID == "" {
		return nil, nil
	}
	cacheKey := "qid=" + qID
	if c.opts.Enabled && c.cache != nil {
		docID := repo.MakeCacheDocID("wikidata", "wikidata.entity", cacheKey)
		if hit := c.getCachedEntity(ctx, docID); hit != nil {
			return hit, nil
		}
	}

	resp, err := c.base.GetEntityData(ctx, qID)
	if err != nil {
		return nil, err
	}
	if c.opts.Enabled && c.cache != nil && resp != nil {
		_ = c.putCached(ctx, "wikidata.entity", []string{"wikidata", "wikidata.entity"}, cacheKey, resp)
	}
	return resp, nil
}

func (c *CachedClient) Search(ctx context.Context, query string, maxResults int) (*ontology.WikidataSearchResponse, error) {
	if c == nil || c.base == nil {
		return nil, nil
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if maxResults <= 0 {
		maxResults = 5
	}
	cacheKey := "q=" + query + "|max=" + strconv.Itoa(maxResults)
	if c.opts.Enabled && c.cache != nil {
		docID := repo.MakeCacheDocID("wikidata", "wikidata.search", cacheKey)
		if hit := c.getCachedSearch(ctx, docID); hit != nil {
			return hit, nil
		}
	}

	resp, err := c.base.Search(ctx, query, maxResults)
	if err != nil {
		return nil, err
	}
	if c.opts.Enabled && c.cache != nil && resp != nil {
		_ = c.putCached(ctx, "wikidata.search", []string{"wikidata", "wikidata.search"}, cacheKey, resp)
	}
	return resp, nil
}

// ConvertToEntity is a local conversion — no caching needed.
func (c *CachedClient) ConvertToEntity(ctx context.Context, entityData *ontology.WikidataEntityData, tenantID string) (*models.Entity, error) {
	if c == nil || c.base == nil {
		return nil, nil
	}
	return c.base.ConvertToEntity(ctx, entityData, tenantID)
}

func (c *CachedClient) getCachedEntity(ctx context.Context, docID string) *ontology.WikidataEntityData {
	e, err := c.cache.Get(ctx, docID)
	if err != nil || e == nil {
		return nil
	}
	if isExpired(e.ExpiresAt) {
		return nil
	}
	var out ontology.WikidataEntityData
	if err := json.Unmarshal(e.Payload, &out); err != nil {
		return nil
	}
	return &out
}

func (c *CachedClient) getCachedSearch(ctx context.Context, docID string) *ontology.WikidataSearchResponse {
	e, err := c.cache.Get(ctx, docID)
	if err != nil || e == nil {
		return nil
	}
	if isExpired(e.ExpiresAt) {
		return nil
	}
	var out ontology.WikidataSearchResponse
	if err := json.Unmarshal(e.Payload, &out); err != nil {
		return nil
	}
	return &out
}

func (c *CachedClient) putCached(ctx context.Context, typ string, types []string, key string, v any) error {
	if c == nil || c.cache == nil || !c.opts.Enabled {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	var exp *time.Time
	if c.opts.TTL > 0 {
		t := now.Add(c.opts.TTL)
		exp = &t
	}
	id := repo.MakeCacheDocID("wikidata", typ, key)
	return c.cache.Upsert(ctx, &repo.ExternalDataCacheEntry{
		ID:          id,
		Service:     "wikidata",
		Type:        typ,
		Types:       types,
		Key:         key,
		ContentType: "application/json",
		Payload:     b,
		CreatedAt:   now,
		UpdatedAt:   now,
		ExpiresAt:   exp,
	})
}

func isExpired(t *time.Time) bool {
	if t == nil {
		return false
	}
	return time.Now().UTC().After(*t)
}
