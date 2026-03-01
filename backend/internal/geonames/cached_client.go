package geonames

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ontology"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

type CacheOptions struct {
	Enabled bool
	TTL     time.Duration
}

// CacheOptionsFromEnv reads cache settings for external providers.
//
// Env:
// - ENABLE_EXTERNAL_DATA_CACHE=true|1 (default: true)
// - EXTERNAL_DATA_CACHE_TTL_HOURS (default: 720 = 30 days)
// - GEONAMES_CACHE_TTL_HOURS (optional override for GeoNames)
func CacheOptionsFromEnv() CacheOptions {
	enabledRaw := strings.ToLower(strings.TrimSpace(os.Getenv("ENABLE_EXTERNAL_DATA_CACHE")))
	enabled := true
	if enabledRaw != "" {
		enabled = enabledRaw == "1" || enabledRaw == "true" || enabledRaw == "yes" || enabledRaw == "y" || enabledRaw == "on"
	}

	ttlHours, _ := strconv.Atoi(strings.TrimSpace(os.Getenv("EXTERNAL_DATA_CACHE_TTL_HOURS")))
	if ttlHours <= 0 {
		ttlHours = 24 * 30
	}
	if v := strings.TrimSpace(os.Getenv("GEONAMES_CACHE_TTL_HOURS")); v != "" {
		if h, err := strconv.Atoi(v); err == nil && h > 0 {
			ttlHours = h
		}
	}
	return CacheOptions{Enabled: enabled, TTL: time.Duration(ttlHours) * time.Hour}
}

// CachedClient wraps a GeoNamesClientInterface and adds Firestore-backed cache-first behavior.
// Cache key is deterministic and scoped by service+type+key.
type CachedClient struct {
	base  ontology.GeoNamesClientInterface
	cache *repo.ExternalDataCacheRepository
	opts  CacheOptions
}

func NewCachedClient(base ontology.GeoNamesClientInterface, cache *repo.ExternalDataCacheRepository, opts CacheOptions) *CachedClient {
	return &CachedClient{base: base, cache: cache, opts: opts}
}

func (c *CachedClient) Search(ctx context.Context, query string, maxResults int, opts ontology.GeoNamesSearchOptions) (*ontology.GeoNamesSearchResponse, error) {
	if c == nil || c.base == nil {
		return nil, nil
	}
	// Mirror upstream clamping to keep cache keys stable.
	if maxResults <= 0 {
		maxResults = 10
	}
	if maxResults > 50 {
		maxResults = 50
	}
	cacheKey := buildGeoNamesSearchKey(query, maxResults, opts)
	if c.opts.Enabled && c.cache != nil && cacheKey != "" {
		docID := repo.MakeCacheDocID("geonames", "geonames.search", cacheKey)
		if hit := c.getCachedSearch(ctx, docID); hit != nil {
			return hit, nil
		}
	}

	resp, err := c.base.Search(ctx, query, maxResults, opts)
	if err != nil {
		return nil, err
	}
	if c.opts.Enabled && c.cache != nil && cacheKey != "" && resp != nil {
		_ = c.putCached(ctx, "geonames.search", []string{"geonames", "geonames.search"}, cacheKey, resp)
	}
	return resp, nil
}

func (c *CachedClient) GetPlace(ctx context.Context, geonameID int) (*ontology.GeoNamesPlace, error) {
	if c == nil || c.base == nil {
		return nil, nil
	}
	cacheKey := "geonameId=" + strconv.Itoa(geonameID)
	if c.opts.Enabled && c.cache != nil && geonameID > 0 {
		docID := repo.MakeCacheDocID("geonames", "geonames.place", cacheKey)
		if hit := c.getCachedPlace(ctx, docID); hit != nil {
			return hit, nil
		}
	}

	resp, err := c.base.GetPlace(ctx, geonameID)
	if err != nil {
		return nil, err
	}
	if c.opts.Enabled && c.cache != nil && geonameID > 0 && resp != nil {
		_ = c.putCached(ctx, "geonames.place", []string{"geonames", "geonames.place"}, cacheKey, resp)
	}
	return resp, nil
}

func (c *CachedClient) GetHierarchy(ctx context.Context, geonameID int) (*ontology.GeoNamesHierarchyResponse, error) {
	if c == nil || c.base == nil {
		return nil, nil
	}
	cacheKey := "geonameId=" + strconv.Itoa(geonameID)
	if c.opts.Enabled && c.cache != nil && geonameID > 0 {
		docID := repo.MakeCacheDocID("geonames", "geonames.hierarchy", cacheKey)
		if hit := c.getCachedHierarchy(ctx, docID); hit != nil {
			return hit, nil
		}
	}

	resp, err := c.base.GetHierarchy(ctx, geonameID)
	if err != nil {
		return nil, err
	}
	if c.opts.Enabled && c.cache != nil && geonameID > 0 && resp != nil {
		_ = c.putCached(ctx, "geonames.hierarchy", []string{"geonames", "geonames.hierarchy"}, cacheKey, resp)
	}
	return resp, nil
}

func (c *CachedClient) GetCountryCode(ctx context.Context, lat, lon float64) (string, error) {
	if c == nil || c.base == nil {
		return "", nil
	}
	return c.base.GetCountryCode(ctx, lat, lon)
}

func (c *CachedClient) getCachedSearch(ctx context.Context, docID string) *ontology.GeoNamesSearchResponse {
	e, err := c.cache.Get(ctx, docID)
	if err != nil || e == nil {
		return nil
	}
	if isExpired(e.ExpiresAt) {
		return nil
	}
	var out ontology.GeoNamesSearchResponse
	if err := json.Unmarshal(e.Payload, &out); err != nil {
		return nil
	}
	return &out
}

func (c *CachedClient) getCachedPlace(ctx context.Context, docID string) *ontology.GeoNamesPlace {
	e, err := c.cache.Get(ctx, docID)
	if err != nil || e == nil {
		return nil
	}
	if isExpired(e.ExpiresAt) {
		return nil
	}
	var out ontology.GeoNamesPlace
	if err := json.Unmarshal(e.Payload, &out); err != nil {
		return nil
	}
	return &out
}

func (c *CachedClient) getCachedHierarchy(ctx context.Context, docID string) *ontology.GeoNamesHierarchyResponse {
	e, err := c.cache.Get(ctx, docID)
	if err != nil || e == nil {
		return nil
	}
	if isExpired(e.ExpiresAt) {
		return nil
	}
	var out ontology.GeoNamesHierarchyResponse
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
	id := repo.MakeCacheDocID("geonames", typ, key)
	return c.cache.Upsert(ctx, &repo.ExternalDataCacheEntry{
		ID:          id,
		Service:     "geonames",
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

func buildGeoNamesSearchKey(query string, maxResults int, opts ontology.GeoNamesSearchOptions) string {
	q := strings.TrimSpace(query)
	if q == "" {
		return ""
	}
	cc := strings.ToUpper(strings.TrimSpace(opts.Country))
	admin := strings.TrimSpace(opts.AdminCode1)
	fcl := strings.ToUpper(strings.TrimSpace(opts.FeatureClass))
	fcode := strings.ToUpper(strings.TrimSpace(opts.FeatureCode))
	nameEquals := opts.NameEquals
	return strings.Join([]string{
		"q=" + q,
		"max=" + strconv.Itoa(maxResults),
		"country=" + cc,
		"adminCode1=" + admin,
		"featureClass=" + fcl,
		"featureCode=" + fcode,
		"nameEquals=" + strconv.FormatBool(nameEquals),
	}, "|")
}
