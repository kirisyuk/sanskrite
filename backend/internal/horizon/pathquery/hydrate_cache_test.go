package pathquery

import (
	"context"
	"testing"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/metriccore"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestHydratePathsFromCache_noMat(t *testing.T) {
	s := NewService(nil, nil, nil, nil, nil)
	_, err := s.hydratePathsFromCache(context.Background(), "t", []string{"x"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHydratePathsFromCache_skipsWrongTenant(t *testing.T) {
	fr := &memFrags{byCP: map[string]*horizonmodels.HorizonFragment{
		"/org/a": {ID: "fa", CanonicalPath: "/org/a", TenantID: "t"},
	}}
	leg := horizonmodels.SupplyChainPathLeg{Index: 0, SupplierRef: "/org/a", CustomerRef: "/org/z"}
	payload := horizonmodels.BuildSupplyChainPathPayloadDetailed(nil, nil, []horizonmodels.SupplyChainPathLeg{leg}, nil, nil, nil, nil)
	art := &horizonmodels.MaterializedArtifact{
		ID:       "m1",
		TenantID: "other",
		Kind:     horizonmodels.MaterializedKindSupplyChainPath,
		NodeRefs: []string{"/org/a"},
		Payload:  payload,
	}
	mat := &memMat{byID: map[string]*horizonmodels.MaterializedArtifact{"m1": art}}
	s := NewService(fr, nil, mat, nil, nil)
	out, err := s.hydratePathsFromCache(context.Background(), "t", []string{"m1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Fatalf("%v", out)
	}
}

func TestQueryPaths_cacheDocHydrateEmptyFallsBackToMaterialized(t *testing.T) {
	metriccore.ResetPathqueryMetricsForTest()
	fr := &memFrags{byCP: map[string]*horizonmodels.HorizonFragment{
		"/org/a": {ID: "fa", CanonicalPath: "/org/a", TenantID: "t"},
		"/org/b": {ID: "fb", CanonicalPath: "/org/b", TenantID: "t"},
	}}
	leg := horizonmodels.SupplyChainPathLeg{Index: 0, SupplierRef: "/org/a", CustomerRef: "/org/b"}
	payload := horizonmodels.BuildSupplyChainPathPayloadDetailed(nil, nil, []horizonmodels.SupplyChainPathLeg{leg}, nil, nil, nil, nil)
	art := &horizonmodels.MaterializedArtifact{
		ID:       "m1",
		TenantID: "t",
		Kind:     horizonmodels.MaterializedKindSupplyChainPath,
		NodeRefs: []string{"/org/a", "/org/b"},
		Payload:  payload,
	}
	mat := &memMat{
		byNode: map[string][]*horizonmodels.MaterializedArtifact{"/org/a": {art}},
		byID:   map[string]*horizonmodels.MaterializedArtifact{"m1": art},
	}
	cache := newMemPathCache()
	s := NewService(fr, nil, mat, nil, nil).WithPathCacheForTest(cache)
	q := Query{
		Tenant: "t",
		From:   Endpoint{Kind: EndpointCanonicalPath, Value: "/org/a"},
		To:     &Endpoint{Kind: EndpointCanonicalPath, Value: "/org/b"},
	}
	ctx := context.Background()
	nq, err := normalizeQuery(q)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.resolveEndpoints(ctx, &nq); err != nil {
		t.Fatal(err)
	}
	key := computeCacheKey(q, nq.FromKind, nq.FromValue, nq.ToPresent, nq.ToKind, nq.ToValue, nq.MaxHops, nq.RelationKinds)
	gen := time.Now().UTC()
	_ = cache.setDoc(ctx, key, pathCacheDocument{
		CacheKey:    key,
		Tenant:      "t",
		PathIDs:     []string{"missing-id"},
		GeneratedAt: gen,
		ExpiresAt:   gen.Add(time.Hour),
	})
	res, err := s.QueryPaths(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if res.FromCache {
		t.Fatal("stale path ids should not produce cache hit")
	}
	if len(res.Paths) != 1 || res.Paths[0].ID != "m1" {
		t.Fatalf("%v", res.Paths)
	}
	if metriccore.PathqueryCacheMisses() < 1 {
		t.Fatal("expected cache miss when hydrate empty")
	}
}

func TestQueryPaths_cacheRefreshSkipsStaleSeed(t *testing.T) {
	fr := &memFrags{byCP: map[string]*horizonmodels.HorizonFragment{
		"/org/a": {ID: "fa", CanonicalPath: "/org/a", TenantID: "t"},
		"/org/b": {ID: "fb", CanonicalPath: "/org/b", TenantID: "t"},
	}}
	leg := horizonmodels.SupplyChainPathLeg{Index: 0, SupplierRef: "/org/a", CustomerRef: "/org/b"}
	payload := horizonmodels.BuildSupplyChainPathPayloadDetailed(nil, nil, []horizonmodels.SupplyChainPathLeg{leg}, nil, nil, nil, nil)
	art := &horizonmodels.MaterializedArtifact{
		ID:       "m1",
		TenantID: "t",
		Kind:     horizonmodels.MaterializedKindSupplyChainPath,
		NodeRefs: []string{"/org/a", "/org/b"},
		Payload:  payload,
	}
	mat := &memMat{
		byNode: map[string][]*horizonmodels.MaterializedArtifact{"/org/a": {art}},
		byID:   map[string]*horizonmodels.MaterializedArtifact{"m1": art},
	}
	cache := newMemPathCache()
	s := NewService(fr, nil, mat, nil, nil).WithPathCacheForTest(cache)
	q := Query{
		Tenant:    "t",
		From:      Endpoint{Kind: EndpointCanonicalPath, Value: "/org/a"},
		To:        &Endpoint{Kind: EndpointCanonicalPath, Value: "/org/b"},
		CacheMode: CacheModeRefresh,
	}
	ctx := context.Background()
	nq, err := normalizeQuery(q)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.resolveEndpoints(ctx, &nq); err != nil {
		t.Fatal(err)
	}
	key := computeCacheKey(Query{Tenant: "t", From: q.From, To: q.To, CacheMode: CacheModeDefault}, nq.FromKind, nq.FromValue, nq.ToPresent, nq.ToKind, nq.ToValue, nq.MaxHops, nq.RelationKinds)
	gen := time.Now().UTC()
	_ = cache.setDoc(ctx, key, pathCacheDocument{
		CacheKey:    key,
		Tenant:      "t",
		PathIDs:     []string{"stale"},
		GeneratedAt: gen,
		ExpiresAt:   gen.Add(time.Hour),
		NodeRefs:    []string{"/org/a"},
	})
	res, err := s.QueryPaths(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if res.FromCache {
		t.Fatal("refresh must not read cache")
	}
	if len(res.Paths) != 1 || res.Paths[0].ID != "m1" {
		t.Fatalf("%v", res.Paths)
	}
}