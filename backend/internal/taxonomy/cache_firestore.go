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

	// Metric units (physical units only). Currency/index units are validated by pattern elsewhere.
	if len(t.AllowedMetricUnits) > 0 {
		rt.AllowedMetricUnits = map[string]struct{}{}
		for _, u := range t.AllowedMetricUnits {
			u = strings.ToLower(strings.TrimSpace(u))
			if u == "" {
				continue
			}
			rt.AllowedMetricUnits[u] = struct{}{}
		}
	}

	// Load signal types
	if len(t.AllowedSignalKinds) > 0 {
		rt.AllowedSignalKinds = map[string]struct{}{}
		for _, kind := range t.AllowedSignalKinds {
			kind = strings.ToLower(strings.TrimSpace(kind))
			if kind == "" {
				continue
			}
			rt.AllowedSignalKinds[kind] = struct{}{}
		}
	}
	if len(t.SignalTypes) > 0 {
		rt.SignalTypes = map[string]SignalTypeSpec{}
		for k, v := range t.SignalTypes {
			k = strings.ToLower(strings.TrimSpace(k))
			if k == "" {
				continue
			}
			rt.SignalTypes[k] = SignalTypeSpec{
				Kind:               strings.ToLower(strings.TrimSpace(v.Kind)),
				Label:              strings.TrimSpace(v.Label),
				DefaultMetric:      strings.TrimSpace(v.DefaultMetric),
				PropagationProfile: strings.TrimSpace(v.PropagationProfile),
			}
		}
	}
	if len(t.SignalTypeSynonyms) > 0 {
		rt.SignalTypeSynonyms = map[string]string{}
		for k, v := range t.SignalTypeSynonyms {
			k = strings.ToLower(strings.TrimSpace(k))
			v = strings.ToLower(strings.TrimSpace(v))
			if k == "" || v == "" {
				continue
			}
			rt.SignalTypeSynonyms[k] = v
		}
	}

	// Load product taxonomy
	if len(t.AllowedProductCategories) > 0 {
		rt.AllowedProductCategories = map[string]struct{}{}
		for _, cat := range t.AllowedProductCategories {
			cat = strings.ToLower(strings.TrimSpace(cat))
			if cat == "" {
				continue
			}
			rt.AllowedProductCategories[cat] = struct{}{}
		}
	}
	if len(t.AllowedProductTypes) > 0 {
		rt.AllowedProductTypes = map[string]struct{}{}
		for _, pt := range t.AllowedProductTypes {
			pt = strings.ToLower(strings.TrimSpace(pt))
			if pt == "" {
				continue
			}
			rt.AllowedProductTypes[pt] = struct{}{}
		}
	}
	if len(t.AllowedProductStages) > 0 {
		rt.AllowedProductStages = map[string]struct{}{}
		for _, stage := range t.AllowedProductStages {
			stage = strings.ToLower(strings.TrimSpace(stage))
			if stage == "" {
				continue
			}
			rt.AllowedProductStages[stage] = struct{}{}
		}
	}
	if len(t.ProductStageOrder) > 0 {
		rt.ProductStageOrder = make([]string, 0, len(t.ProductStageOrder))
		for _, stage := range t.ProductStageOrder {
			stage = strings.ToLower(strings.TrimSpace(stage))
			if stage == "" {
				continue
			}
			rt.ProductStageOrder = append(rt.ProductStageOrder, stage)
		}
	}
	if len(t.KnownProducts) > 0 {
		rt.KnownProducts = map[string]ProductSpec{}
		for k, v := range t.KnownProducts {
			k = strings.ToLower(strings.TrimSpace(k))
			if k == "" {
				continue
			}
			rt.KnownProducts[k] = ProductSpec{
				Category: strings.ToLower(strings.TrimSpace(v.Category)),
				Type:     strings.ToLower(strings.TrimSpace(v.Type)),
				Stage:    strings.ToLower(strings.TrimSpace(v.Stage)),
			}
		}
	}
	if len(t.ProductSynonyms) > 0 {
		rt.ProductSynonyms = map[string]string{}
		for k, v := range t.ProductSynonyms {
			k = strings.ToLower(strings.TrimSpace(k))
			v = strings.ToLower(strings.TrimSpace(v))
			if k == "" || v == "" {
				continue
			}
			rt.ProductSynonyms[k] = v
		}
	}

	// Load deposit resources
	if len(t.AllowedDepositResources) > 0 {
		rt.AllowedDepositResources = map[string]struct{}{}
		for _, r := range t.AllowedDepositResources {
			r = strings.ToLower(strings.TrimSpace(r))
			if r == "" {
				continue
			}
			rt.AllowedDepositResources[r] = struct{}{}
		}
	}

	// Load lifecycle stages
	if len(t.AllowedLifecycleStages) > 0 {
		rt.AllowedLifecycleStages = map[string]struct{}{}
		for _, s := range t.AllowedLifecycleStages {
			s = strings.ToLower(strings.TrimSpace(s))
			if s == "" {
				continue
			}
			rt.AllowedLifecycleStages[s] = struct{}{}
		}
	}
	if len(t.LifecycleStageAliases) > 0 {
		rt.LifecycleStageAliases = map[string]string{}
		for k, v := range t.LifecycleStageAliases {
			k = strings.ToLower(strings.TrimSpace(k))
			v = strings.ToLower(strings.TrimSpace(v))
			if k == "" || v == "" {
				continue
			}
			rt.LifecycleStageAliases[k] = v
		}
	}

	// Load edge metrics and modalities
	if len(t.AllowedEdgeMetrics) > 0 {
		rt.AllowedEdgeMetrics = map[string][]string{}
		for k, v := range t.AllowedEdgeMetrics {
			k = strings.ToUpper(strings.TrimSpace(k))
			if k == "" || len(v) == 0 {
				continue
			}
			metrics := make([]string, 0, len(v))
			for _, m := range v {
				m = strings.ToLower(strings.TrimSpace(m))
				if m != "" {
					metrics = append(metrics, m)
				}
			}
			if len(metrics) > 0 {
				rt.AllowedEdgeMetrics[k] = metrics
			}
		}
	}
	if len(t.AllowedEdgeModalities) > 0 {
		rt.AllowedEdgeModalities = map[string]struct{}{}
		for _, m := range t.AllowedEdgeModalities {
			m = strings.ToLower(strings.TrimSpace(m))
			if m == "" {
				continue
			}
			rt.AllowedEdgeModalities[m] = struct{}{}
		}
	}

	// Load service taxonomy
	if len(t.AllowedServiceDomains) > 0 {
		rt.AllowedServiceDomains = map[string]struct{}{}
		for _, d := range t.AllowedServiceDomains {
			d = strings.ToLower(strings.TrimSpace(d))
			if d == "" {
				continue
			}
			rt.AllowedServiceDomains[d] = struct{}{}
		}
	}
	if len(t.AllowedServiceSubdomains) > 0 {
		rt.AllowedServiceSubdomains = map[string][]string{}
		for k, v := range t.AllowedServiceSubdomains {
			k = strings.ToLower(strings.TrimSpace(k))
			if k == "" || len(v) == 0 {
				continue
			}
			subs := make([]string, 0, len(v))
			for _, s := range v {
				s = strings.ToLower(strings.TrimSpace(s))
				if s != "" {
					subs = append(subs, s)
				}
			}
			if len(subs) > 0 {
				rt.AllowedServiceSubdomains[k] = subs
			}
		}
	}
	if len(t.KnownServices) > 0 {
		rt.KnownServices = map[string]ServiceSpec{}
		for k, v := range t.KnownServices {
			k = strings.ToLower(strings.TrimSpace(k))
			if k == "" {
				continue
			}
			rt.KnownServices[k] = ServiceSpec{
				Domain:    strings.ToLower(strings.TrimSpace(v.Domain)),
				Subdomain: strings.ToLower(strings.TrimSpace(v.Subdomain)),
			}
		}
	}
	if len(t.ServiceSynonyms) > 0 {
		rt.ServiceSynonyms = map[string]string{}
		for k, v := range t.ServiceSynonyms {
			k = strings.ToLower(strings.TrimSpace(k))
			v = strings.ToLower(strings.TrimSpace(v))
			if k == "" || v == "" {
				continue
			}
			rt.ServiceSynonyms[k] = v
		}
	}

	// Load product models (optional)
	if len(t.KnownProductModels) > 0 {
		rt.KnownProductModels = map[string]ProductModelSpec{}
		for k, v := range t.KnownProductModels {
			k = strings.ToLower(strings.TrimSpace(k))
			if k == "" {
				continue
			}
			rt.KnownProductModels[k] = ProductModelSpec{
				GenericSlug:  strings.ToLower(strings.TrimSpace(v.GenericSlug)),
				Manufacturer: strings.ToLower(strings.TrimSpace(v.Manufacturer)),
			}
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
