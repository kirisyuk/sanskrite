package taxonomy

import (
	"context"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// FirestoreRuntimeTaxonomyCache caches runtime taxonomy in-memory with TTL.
// It prevents hitting Firestore on every normalization call (NormalizeEventType/NormalizeSpherePaths).
type FirestoreRuntimeTaxonomyCache struct {
	repo    *repo.HorizonTaxonomyRepository
	version string
	ttl     time.Duration

	mu       sync.Mutex
	cached   *RuntimeTaxonomy
	loadedAt time.Time
}

func NewFirestoreRuntimeTaxonomyCache(db *firestore.Client, version string, ttl time.Duration) *FirestoreRuntimeTaxonomyCache {
	if strings.TrimSpace(version) == "" {
		version = "v1"
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &FirestoreRuntimeTaxonomyCache{
		repo:    repo.NewHorizonTaxonomyRepository(db),
		version: version,
		ttl:     ttl,
	}
}

func (c *FirestoreRuntimeTaxonomyCache) isFreshLocked(now time.Time) bool {
	if c.cached == nil {
		return false
	}
	if c.loadedAt.IsZero() {
		return false
	}
	return now.Sub(c.loadedAt) < c.ttl
}

func (c *FirestoreRuntimeTaxonomyCache) Get(ctx context.Context) (*RuntimeTaxonomy, error) {
	if c == nil || c.repo == nil {
		return nil, nil
	}
	now := time.Now().UTC()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isFreshLocked(now) {
		return c.cached, nil
	}

	t, err := c.repo.Get(ctx, c.version)
	if err != nil || t == nil {
		// Keep previous cached version if we have it.
		if c.cached != nil {
			return c.cached, nil
		}
		return nil, err
	}

	rt := &RuntimeTaxonomy{Version: c.version}

	if len(t.AllowedSpherePaths) > 0 {
		rt.AllowedSpherePaths = map[string]struct{}{}
		for _, p := range t.AllowedSpherePaths {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			rt.AllowedSpherePaths[p] = struct{}{}
		}
	}
	if len(t.SphereSynonyms) > 0 {
		rt.SphereSynonyms = map[string]string{}
		for k, v := range t.SphereSynonyms {
			k = strings.ToLower(strings.TrimSpace(k))
			v = strings.TrimSpace(v)
			if k == "" || v == "" {
				continue
			}
			rt.SphereSynonyms[k] = v
		}
	}
	if len(t.AllowedEventTypes) > 0 {
		rt.AllowedEventTypes = map[string]struct{}{}
		for _, et := range t.AllowedEventTypes {
			et = strings.ToLower(strings.TrimSpace(et))
			if et == "" {
				continue
			}
			rt.AllowedEventTypes[et] = struct{}{}
		}
	}
	if len(t.EventTypeSynonyms) > 0 {
		rt.EventTypeSynonyms = map[string]string{}
		for k, v := range t.EventTypeSynonyms {
			k = strings.ToLower(strings.TrimSpace(k))
			v = strings.ToLower(strings.TrimSpace(v))
			if k == "" || v == "" {
				continue
			}
			rt.EventTypeSynonyms[k] = v
		}
	}

	if len(t.AllowedRelationTypes) > 0 {
		rt.AllowedRelationTypes = map[string]struct{}{}
		for _, rtName := range t.AllowedRelationTypes {
			rtName = strings.ToUpper(strings.TrimSpace(rtName))
			if rtName == "" {
				continue
			}
			rt.AllowedRelationTypes[rtName] = struct{}{}
		}
	}

	c.cached = rt
	c.loadedAt = now
	return rt, nil
}

func (c *FirestoreRuntimeTaxonomyCache) StartAutoRefresh(ctx context.Context, interval time.Duration, onError func(error)) {
	if c == nil || c.repo == nil {
		return
	}
	if interval <= 0 {
		interval = c.ttl / 2
		if interval < 30*time.Second {
			interval = 30 * time.Second
		}
	}
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				rt, err := c.Get(context.Background())
				if err != nil {
					if onError != nil {
						onError(err)
					}
					continue
				}
				if rt != nil {
					SetRuntimeTaxonomy(rt)
				}
			}
		}
	}()
}

