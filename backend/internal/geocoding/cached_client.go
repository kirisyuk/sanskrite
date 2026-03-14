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
	base    ontology.GeocodingClientInterface
	cache   *repo.ExternalDataCacheRepository
	opts    CacheOptions
	service string
}

// NewCachedClient creates a cached geocoding client.
func NewCachedClient(base ontology.GeocodingClientInterface, cache *repo.ExternalDataCacheRepository, opts CacheOptions) *CachedClient {
	return NewCachedClientWithService(base, cache, opts, ontology.GeocodingProviderGoogle)
}

// NewCachedClientWithService creates a cached geocoding client with explicit provider identity.
func NewCachedClientWithService(base ontology.GeocodingClientInterface, cache *repo.ExternalDataCacheRepository, opts CacheOptions, service string) *CachedClient {
	svc := strings.TrimSpace(service)
	if svc == "" {
		svc = ontology.GeocodingProviderGoogle
	}
	return &CachedClient{base: base, cache: cache, opts: opts, service: svc}
}

// Geocode implements ontology.GeocodingClientInterface with cache-first behavior.
// Cache lookup is tolerant to minor address variations (e.g. "Moscow, RU" vs "Moscow, Russia")
// via normalized keys and multi-variant lookup.
func (c *CachedClient) Geocode(ctx context.Context, address string) (*ontology.GeocodingResult, error) {
	if c == nil || c.base == nil {
		return nil, nil
	}
	if !IsPlausibleAddress(address) {
		return nil, nil
	}
	addr := normalizeAddressForCache(address)
	if addr == "" {
		return nil, nil
	}

	if c.opts.Enabled && c.cache != nil {
		for _, variant := range addressVariantsForCacheLookup(addr) {
			cacheKey := "address=" + variant
			docID := repo.MakeCacheDocID(c.service, "geocoding.geocode", cacheKey)
			if hit, ok := c.getCached(ctx, docID); ok {
				return hit, nil
			}
		}
	}

	resp, err := c.base.Geocode(ctx, address)
	if err != nil {
		return nil, err
	}
	if c.opts.Enabled && c.cache != nil {
		cacheKey := "address=" + addr
		_ = c.putCached(ctx, cacheKey, resp)
	}
	return resp, nil
}

func (c *CachedClient) getCached(ctx context.Context, docID string) (*ontology.GeocodingResult, bool) {
	e, err := c.cache.Get(ctx, docID)
	if err != nil || e == nil {
		return nil, false
	}
	if isExpired(e.ExpiresAt) {
		return nil, false
	}
	var out struct {
		Found            *bool   `json:"found,omitempty"`
		Lat              float64 `json:"lat"`
		Lon              float64 `json:"lon"`
		FormattedAddress string  `json:"formattedAddress"`
		PlaceID          string  `json:"placeId"`
		Provider         string  `json:"provider,omitempty"`
	}
	if err := json.Unmarshal(e.Payload, &out); err != nil {
		return nil, false
	}
	if out.Found != nil && !*out.Found {
		return nil, true
	}
	return &ontology.GeocodingResult{
		Lat:              out.Lat,
		Lon:              out.Lon,
		FormattedAddress: out.FormattedAddress,
		PlaceID:          out.PlaceID,
		Provider:         firstNonEmptyProvider(out.Provider, c.service),
	}, true
}

func (c *CachedClient) putCached(ctx context.Context, key string, v *ontology.GeocodingResult) error {
	if c == nil || c.cache == nil || !c.opts.Enabled {
		return nil
	}
	payload := map[string]interface{}{
		"found": v != nil,
	}
	if v != nil {
		payload["lat"] = v.Lat
		payload["lon"] = v.Lon
		payload["formattedAddress"] = v.FormattedAddress
		payload["placeId"] = v.PlaceID
		payload["provider"] = firstNonEmptyProvider(v.Provider, c.service)
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	var exp *time.Time
	ttl := c.opts.TTL
	if v == nil {
		ttl = negativeCacheTTL(c.opts.TTL)
	}
	if ttl > 0 {
		t := now.Add(ttl)
		exp = &t
	}
	id := repo.MakeCacheDocID(c.service, "geocoding.geocode", key)
	return c.cache.Upsert(ctx, &repo.ExternalDataCacheEntry{
		ID:          id,
		Service:     c.service,
		Type:        "geocoding.geocode",
		Types:       []string{c.service, "geocoding.geocode"},
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

func negativeCacheTTL(baseTTL time.Duration) time.Duration {
	const maxNegativeTTL = 24 * time.Hour
	switch {
	case baseTTL <= 0:
		return maxNegativeTTL
	case baseTTL < maxNegativeTTL:
		return baseTTL
	default:
		return maxNegativeTTL
	}
}

func firstNonEmptyProvider(values ...string) string {
	for _, value := range values {
		if s := strings.TrimSpace(value); s != "" {
			return s
		}
	}
	return ""
}

// normalizeAddressForCache produces a stable cache key from an address.
// Applies: trim/collapse spaces, lowercase, country part normalization (e.g. Russia→ru, RU→ru).
func normalizeAddressForCache(addr string) string {
	s := strings.TrimSpace(addr)
	s = strings.Join(strings.Fields(s), " ")
	s = strings.ToLower(s)
	if s == "" {
		return ""
	}
	parts := splitAddressParts(s)
	if len(parts) >= 2 {
		last := normalizeCountryPart(parts[len(parts)-1])
		rest := strings.Join(parts[:len(parts)-1], ", ")
		return rest + ", " + last
	}
	return s
}
