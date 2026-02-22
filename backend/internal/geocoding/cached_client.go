package geocoding

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

// CacheOptions controls geocoding cache behavior.
type CacheOptions struct {
	Enabled bool
	TTL     time.Duration
}

// CacheOptionsFromEnv reads cache settings.
//
// Env:
// - ENABLE_EXTERNAL_DATA_CACHE=true|1 (default: true)
// - EXTERNAL_DATA_CACHE_TTL_HOURS (default: 720 = 30 days)
// - GEOCODING_CACHE_TTL_HOURS (optional override)
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
	if v := strings.TrimSpace(os.Getenv("GEOCODING_CACHE_TTL_HOURS")); v != "" {
		if h, err := strconv.Atoi(v); err == nil && h > 0 {
			ttlHours = h
		}
	}
	return CacheOptions{Enabled: enabled, TTL: time.Duration(ttlHours) * time.Hour}
}

// CachedClient wraps a GeocodingClientInterface and adds Firestore-backed cache.
// Uses external_data_cache collection with service="google_geocoding".
type CachedClient struct {
	base  ontology.GeocodingClientInterface
	cache *repo.ExternalDataCacheRepository
	opts  CacheOptions
}

// NewCachedClient creates a cached geocoding client.
func NewCachedClient(base ontology.GeocodingClientInterface, cache *repo.ExternalDataCacheRepository, opts CacheOptions) *CachedClient {
	return &CachedClient{base: base, cache: cache, opts: opts}
}

// Geocode implements ontology.GeocodingClientInterface with cache-first behavior.
func (c *CachedClient) Geocode(ctx context.Context, address string) (*ontology.GeocodingResult, error) {
	if c == nil || c.base == nil {
		return nil, nil
	}
	addr := normalizeAddressForCache(address)
	if addr == "" {
		return nil, nil
	}

	cacheKey := "address=" + addr
	if c.opts.Enabled && c.cache != nil {
		docID := repo.MakeCacheDocID("google_geocoding", "geocoding.geocode", cacheKey)
		if hit := c.getCached(ctx, docID); hit != nil {
			return hit, nil
		}
	}

	resp, err := c.base.Geocode(ctx, address)
	if err != nil {
		return nil, err
	}
	if c.opts.Enabled && c.cache != nil && resp != nil {
		_ = c.putCached(ctx, cacheKey, resp)
	}
	return resp, nil
}

func (c *CachedClient) getCached(ctx context.Context, docID string) *ontology.GeocodingResult {
	e, err := c.cache.Get(ctx, docID)
	if err != nil || e == nil {
		return nil
	}
	if isExpired(e.ExpiresAt) {
		return nil
	}
	var out struct {
		Lat              float64 `json:"lat"`
		Lon              float64 `json:"lon"`
		FormattedAddress string  `json:"formattedAddress"`
		PlaceID          string  `json:"placeId"`
	}
	if err := json.Unmarshal(e.Payload, &out); err != nil {
		return nil
	}
	return &ontology.GeocodingResult{
		Lat:              out.Lat,
		Lon:              out.Lon,
		FormattedAddress: out.FormattedAddress,
		PlaceID:          out.PlaceID,
	}
}

func (c *CachedClient) putCached(ctx context.Context, key string, v *ontology.GeocodingResult) error {
	if c == nil || c.cache == nil || !c.opts.Enabled || v == nil {
		return nil
	}
	payload := map[string]interface{}{
		"lat":              v.Lat,
		"lon":              v.Lon,
		"formattedAddress": v.FormattedAddress,
		"placeId":          v.PlaceID,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	var exp *time.Time
	if c.opts.TTL > 0 {
		t := now.Add(c.opts.TTL)
		exp = &t
	}
	id := repo.MakeCacheDocID("google_geocoding", "geocoding.geocode", key)
	return c.cache.Upsert(ctx, &repo.ExternalDataCacheEntry{
		ID:          id,
		Service:     "google_geocoding",
		Type:        "geocoding.geocode",
		Types:       []string{"google_geocoding", "geocoding.geocode"},
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

// normalizeAddressForCache produces a stable cache key from an address.
func normalizeAddressForCache(addr string) string {
	s := strings.TrimSpace(addr)
	s = strings.Join(strings.Fields(s), " ")
	return s
}
