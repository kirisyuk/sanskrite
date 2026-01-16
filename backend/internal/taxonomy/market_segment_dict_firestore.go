package taxonomy

import (
	"context"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/models"
)

// MarketSegmentDictionary is the in-memory operational view of the Firestore dictionary.
type MarketSegmentDictionary struct {
	Version string

	ChannelDomains   map[string]struct{}
	DomainAliases    map[string]string
	VerticalAliases  map[string]string
	VerticalFamilies map[string]string
	NormRules        map[string]SegmentNormRule // key: "/segment/{domain}/{vertical}"

	// Segment-first hierarchy support
	VerticalParents  map[string]string // childVertical -> parentVertical
	VerticalSynonyms map[string]string // rawVertical -> canonicalVertical
}

var msMu sync.RWMutex
var msDict *MarketSegmentDictionary

func SetMarketSegmentDictionary(d *MarketSegmentDictionary) {
	msMu.Lock()
	defer msMu.Unlock()
	msDict = d
}

func GetMarketSegmentDictionary() *MarketSegmentDictionary {
	msMu.RLock()
	defer msMu.RUnlock()
	return msDict
}

// FirestoreMarketSegmentDictCache caches market segment dictionary in-memory with TTL.
type FirestoreMarketSegmentDictCache struct {
	db      *firestore.Client
	version string
	ttl     time.Duration

	mu       sync.Mutex
	cached   *MarketSegmentDictionary
	loadedAt time.Time
}

func NewFirestoreMarketSegmentDictCache(db *firestore.Client, version string, ttl time.Duration) *FirestoreMarketSegmentDictCache {
	if strings.TrimSpace(version) == "" {
		version = "v1"
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &FirestoreMarketSegmentDictCache{
		db:      db,
		version: version,
		ttl:     ttl,
	}
}

func (c *FirestoreMarketSegmentDictCache) isFreshLocked(now time.Time) bool {
	if c.cached == nil || c.loadedAt.IsZero() {
		return false
	}
	return now.Sub(c.loadedAt) < c.ttl
}

func (c *FirestoreMarketSegmentDictCache) Get(ctx context.Context) (*MarketSegmentDictionary, error) {
	if c == nil || c.db == nil {
		return nil, nil
	}
	now := time.Now().UTC()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isFreshLocked(now) {
		return c.cached, nil
	}

	snap, err := c.db.Collection("horizon_market_segment_dict").Doc(c.version).Get(ctx)
	if err != nil || snap == nil || !snap.Exists() {
		// Keep previous cached version if we have it.
		if c.cached != nil {
			return c.cached, nil
		}
		return nil, err
	}
	var doc models.MarketSegmentDictionary
	if err := snap.DataTo(&doc); err != nil {
		if c.cached != nil {
			return c.cached, nil
		}
		return nil, err
	}

	out := &MarketSegmentDictionary{Version: c.version}

	out.ChannelDomains = map[string]struct{}{}
	for _, d := range doc.SegmentChannelDomains {
		d = strings.ToLower(strings.TrimSpace(d))
		if d == "" {
			continue
		}
		out.ChannelDomains[d] = struct{}{}
	}

	out.DomainAliases = map[string]string{}
	for k, v := range doc.SegmentDomainAliases {
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.ToLower(strings.TrimSpace(v))
		if k == "" || v == "" {
			continue
		}
		out.DomainAliases[k] = v
	}

	out.VerticalAliases = map[string]string{}
	for k, v := range doc.SegmentVerticalAliases {
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.ToLower(strings.TrimSpace(v))
		if k == "" || v == "" {
			continue
		}
		out.VerticalAliases[k] = v
	}

	out.VerticalFamilies = map[string]string{}
	for k, v := range doc.SegmentVerticalFamilies {
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.ToLower(strings.TrimSpace(v))
		if k == "" || v == "" {
			continue
		}
		out.VerticalFamilies[k] = v
	}

	out.NormRules = map[string]SegmentNormRule{}
	for k, v := range doc.SegmentNormRules {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		nd := strings.ToLower(strings.TrimSpace(v.NormDomain))
		nv := strings.ToLower(strings.TrimSpace(v.NormVertical))
		ch := strings.ToLower(strings.TrimSpace(v.Channel))
		if nd == "" {
			continue
		}
		out.NormRules[k] = SegmentNormRule{NormDomain: nd, NormVertical: nv, Channel: ch}
	}

	// Optional: vertical hierarchy + synonyms
	out.VerticalParents = map[string]string{}
	for k, v := range doc.SegmentVerticalParents {
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.ToLower(strings.TrimSpace(v))
		if k == "" || v == "" {
			continue
		}
		out.VerticalParents[k] = v
	}
	out.VerticalSynonyms = map[string]string{}
	for k, v := range doc.SegmentVerticalSynonyms {
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.ToLower(strings.TrimSpace(v))
		if k == "" || v == "" {
			continue
		}
		out.VerticalSynonyms[k] = v
	}

	c.cached = out
	c.loadedAt = now
	return out, nil
}

func (c *FirestoreMarketSegmentDictCache) StartAutoRefresh(ctx context.Context, interval time.Duration, onError func(error)) {
	if c == nil || c.db == nil {
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
				d, err := c.Get(context.Background())
				if err != nil {
					if onError != nil {
						onError(err)
					}
					continue
				}
				if d != nil {
					SetMarketSegmentDictionary(d)
				}
			}
		}
	}()
}

var msCacheMu = &sync.Mutex{}
var msCache *FirestoreMarketSegmentDictCache

// InitMarketSegmentDictionaryCacheFromFirestore initializes the global cached loader.
func InitMarketSegmentDictionaryCacheFromFirestore(db *firestore.Client, version string, ttl time.Duration) {
	if db == nil {
		return
	}
	msCacheMu.Lock()
	defer msCacheMu.Unlock()
	msCache = NewFirestoreMarketSegmentDictCache(db, version, ttl)
}

// RefreshMarketSegmentDictionary loads dict through cache and installs it globally.
func RefreshMarketSegmentDictionary(ctx context.Context) error {
	msCacheMu.Lock()
	c := msCache
	msCacheMu.Unlock()
	if c == nil {
		return nil
	}
	d, err := c.Get(ctx)
	if err != nil {
		return err
	}
	if d != nil {
		SetMarketSegmentDictionary(d)
	}
	return nil
}

// StartMarketSegmentDictionaryAutoRefresh starts periodic refresh in background.
func StartMarketSegmentDictionaryAutoRefresh(ctx context.Context, interval time.Duration, onError func(error)) {
	msCacheMu.Lock()
	c := msCache
	msCacheMu.Unlock()
	if c == nil {
		return
	}
	c.StartAutoRefresh(ctx, interval, onError)
}
