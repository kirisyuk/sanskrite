package pathquery

import (
	"context"
	"fmt"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/metriccore"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

type memFrags struct {
	byID map[string]*horizonmodels.HorizonFragment
	byCP map[string]*horizonmodels.HorizonFragment
}

func (m *memFrags) Get(ctx context.Context, id string) (*horizonmodels.HorizonFragment, error) {
	if m == nil {
		return nil, nil
	}
	return m.byID[id], nil
}

func (m *memFrags) GetByCanonicalPathForTenant(ctx context.Context, tenantID, canonicalPath string) (*horizonmodels.HorizonFragment, error) {
	if m == nil {
		return nil, nil
	}
	return m.byCP[canonicalPath], nil
}

func (m *memFrags) GetByCanonicalPath(ctx context.Context, canonicalPath string) (*horizonmodels.HorizonFragment, error) {
	if m == nil {
		return nil, nil
	}
	return m.byCP[canonicalPath], nil
}

type memMat struct {
	byNode map[string][]*horizonmodels.MaterializedArtifact
	byID   map[string]*horizonmodels.MaterializedArtifact
}

func (m *memMat) FindByNodeRef(ctx context.Context, tenantID, path string, kind horizonmodels.MaterializedKind) ([]*horizonmodels.MaterializedArtifact, error) {
	if m == nil || m.byNode == nil {
		return nil, nil
	}
	return m.byNode[path], nil
}

func (m *memMat) Get(ctx context.Context, id string) (*horizonmodels.MaterializedArtifact, error) {
	if m == nil || m.byID == nil {
		return nil, nil
	}
	return m.byID[id], nil
}

type memEdges struct {
	out map[string][]*horizonmodels.DependencyEdge
	in  map[string][]*horizonmodels.DependencyEdge
}

func (m *memEdges) ListBySource(ctx context.Context, tenantID, sourceFragmentID string, edgeType *ah.RelationName, limit int) ([]*horizonmodels.DependencyEdge, error) {
	if m == nil || m.out == nil {
		return nil, nil
	}
	return m.out[sourceFragmentID], nil
}

func (m *memEdges) ListByTarget(ctx context.Context, tenantID, targetFragmentID string, edgeType *ah.RelationName, limit int) ([]*horizonmodels.DependencyEdge, error) {
	if m == nil || m.in == nil {
		return nil, nil
	}
	return m.in[targetFragmentID], nil
}

func TestQueryPaths_materializedTwoAnchors(t *testing.T) {
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
	s := NewService(fr, nil, mat, nil, nil)
	res, err := s.QueryPaths(context.Background(), Query{
		Tenant:    "t",
		From:      Endpoint{Kind: EndpointCanonicalPath, Value: "/org/a"},
		To:        &Endpoint{Kind: EndpointCanonicalPath, Value: "/org/b"},
		CacheMode: CacheModeDisabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Paths) != 1 || res.Paths[0].ID != "m1" {
		t.Fatalf("paths=%v", res.Paths)
	}
	if res.Diagnostics.MaterializedHits != 1 {
		t.Fatalf("diag=%+v", res.Diagnostics)
	}
}

func TestQueryPaths_cacheHitMiss(t *testing.T) {
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
	res1, err := s.QueryPaths(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if res1.FromCache {
		t.Fatal("first query should not be cached")
	}
	if metriccore.PathqueryCacheMisses() != 1 || metriccore.PathqueryCacheHits() != 0 {
		t.Fatalf("after miss: hits=%d misses=%d", metriccore.PathqueryCacheHits(), metriccore.PathqueryCacheMisses())
	}
	res2, err := s.QueryPaths(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if !res2.FromCache {
		t.Fatal("second query should hit cache")
	}
	if len(res2.Paths) != 1 || res2.Paths[0].ID != "m1" {
		t.Fatalf("paths=%v", res2.Paths)
	}
	if metriccore.PathqueryCacheHits() != 1 || metriccore.PathqueryCacheMisses() != 1 {
		t.Fatalf("after hit: hits=%d misses=%d", metriccore.PathqueryCacheHits(), metriccore.PathqueryCacheMisses())
	}
}

func TestInvalidateByNode_memPathCache(t *testing.T) {
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
	ctx := context.Background()
	q := Query{
		Tenant: "t",
		From:   Endpoint{Kind: EndpointCanonicalPath, Value: "/org/a"},
		To:     &Endpoint{Kind: EndpointCanonicalPath, Value: "/org/b"},
	}
	if _, err := s.QueryPaths(ctx, q); err != nil {
		t.Fatal(err)
	}
	if len(cache.docs) != 1 {
		t.Fatalf("expected 1 cached doc, got %d", len(cache.docs))
	}
	if err := s.InvalidateByNode(ctx, "t", "/org/a"); err != nil {
		t.Fatal(err)
	}
	if len(cache.docs) != 0 {
		t.Fatalf("after invalidate expected 0 docs, got %d %#v", len(cache.docs), cache.docs)
	}
	res, err := s.QueryPaths(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if res.FromCache {
		t.Fatal("expected cache cleared after InvalidateByNode")
	}
}

func TestBfsReachable_withEdge(t *testing.T) {
	fr := &memFrags{}
	rel := ah.RelationNameSupplies
	edges := &memEdges{
		out: map[string][]*horizonmodels.DependencyEdge{
			"fa": {{SourceFragmentID: "fa", TargetFragmentID: "fb", Relation: rel}},
		},
	}
	s := NewService(fr, edges, &memMat{}, nil, nil)
	nq := &normalizedQuery{
		Query:          Query{Tenant: "t"},
		ToPresent:      true,
		FromFragmentID: "fa",
		ToFragmentID:   "fb",
		MaxHops:        4,
		RelationKinds: []ah.RelationName{rel},
	}
	d := Diagnostics{}
	if !s.bfsReachable(context.Background(), nq, &d) {
		t.Fatalf("expected reachable, diag=%+v", d)
	}
}

func TestQueryPaths_openEndedCap(t *testing.T) {
	fr := &memFrags{byCP: map[string]*horizonmodels.HorizonFragment{
		"/org/a": {ID: "fa", CanonicalPath: "/org/a"},
	}}
	var arts []*horizonmodels.MaterializedArtifact
	for i := 0; i < 40; i++ {
		leg := horizonmodels.SupplyChainPathLeg{Index: 0, SupplierRef: "/org/a", CustomerRef: "/org/z"}
		payload := horizonmodels.BuildSupplyChainPathPayloadDetailed(nil, nil, []horizonmodels.SupplyChainPathLeg{leg}, nil, nil, nil, nil)
		arts = append(arts, &horizonmodels.MaterializedArtifact{
			ID:       fmt.Sprintf("m%d", i),
			TenantID: "t",
			Kind:     horizonmodels.MaterializedKindSupplyChainPath,
			NodeRefs: []string{"/org/a"},
			Payload:  payload,
		})
	}
	mat := &memMat{byNode: map[string][]*horizonmodels.MaterializedArtifact{"/org/a": arts}}
	s := NewService(fr, nil, mat, nil, nil)
	res, err := s.QueryPaths(context.Background(), Query{
		Tenant:    "t",
		From:      Endpoint{Kind: EndpointCanonicalPath, Value: "/org/a"},
		CacheMode: CacheModeDisabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Paths) != maxOpenEndedPaths {
		t.Fatalf("len=%d want %d", len(res.Paths), maxOpenEndedPaths)
	}
	if !res.Diagnostics.TruncatedResultCap {
		t.Fatal("expected truncated cap")
	}
}

func TestQueryPaths_fromFragmentID(t *testing.T) {
	fr := &memFrags{
		byID: map[string]*horizonmodels.HorizonFragment{
			"frag1": {ID: "frag1", CanonicalPath: "/org/a", TenantID: "t"},
		},
	}
	leg := horizonmodels.SupplyChainPathLeg{Index: 0, SupplierRef: "/org/a", ProductRef: "/product/p"}
	payload := horizonmodels.BuildSupplyChainPathPayloadDetailed(nil, nil, []horizonmodels.SupplyChainPathLeg{leg}, nil, nil, nil, nil)
	art := &horizonmodels.MaterializedArtifact{
		ID:       "m1",
		TenantID: "t",
		Kind:     horizonmodels.MaterializedKindSupplyChainPath,
		NodeRefs: []string{"/org/a"},
		Payload:  payload,
	}
	mat := &memMat{byNode: map[string][]*horizonmodels.MaterializedArtifact{"/org/a": {art}}}
	s := NewService(fr, nil, mat, nil, nil)
	res, err := s.QueryPaths(context.Background(), Query{
		Tenant:    "t",
		From:      Endpoint{Kind: EndpointFragmentID, Value: "frag1"},
		CacheMode: CacheModeDisabled,
		ProductAxis: "/product/p",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Paths) != 1 {
		t.Fatalf("%d paths", len(res.Paths))
	}
	if !res.Diagnostics.ProductAxisFiltered {
		t.Fatal("expected product filter flag")
	}
}

func TestQueryPaths_includeStress(t *testing.T) {
	fr := &memFrags{byCP: map[string]*horizonmodels.HorizonFragment{
		"/org/a": {ID: "fa", CanonicalPath: "/org/a"},
	}}
	leg := horizonmodels.SupplyChainPathLeg{Index: 0, SupplierRef: "/org/a", Mode: "sea"}
	payload := horizonmodels.BuildSupplyChainPathPayloadDetailed(nil, nil, []horizonmodels.SupplyChainPathLeg{leg}, nil, nil, nil, nil)
	art := &horizonmodels.MaterializedArtifact{
		ID:       "m1",
		TenantID: "t",
		Kind:     horizonmodels.MaterializedKindSupplyChainPath,
		NodeRefs: []string{"/org/a"},
		Payload:  payload,
	}
	mat := &memMat{byNode: map[string][]*horizonmodels.MaterializedArtifact{"/org/a": {art}}}
	s := NewService(fr, nil, mat, nil, nil)
	res, err := s.QueryPaths(context.Background(), Query{
		Tenant:        "t",
		From:          Endpoint{Kind: EndpointCanonicalPath, Value: "/org/a"},
		CacheMode:     CacheModeDisabled,
		IncludeStress: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := horizonmodels.ParseSupplyChainPathPayload(res.Paths[0].Payload)
	if err != nil || parsed == nil || parsed.Stress == nil {
		t.Fatal("stress missing")
	}
}

func TestExpandNeighbors_multiRel(t *testing.T) {
	rel1 := ah.RelationNameSupplies
	rel2 := ah.RelationNameProduces
	edges := &memEdges{
		out: map[string][]*horizonmodels.DependencyEdge{
			"x": {
				{SourceFragmentID: "x", TargetFragmentID: "y", Relation: rel1},
			},
		},
	}
	s := NewService(nil, edges, nil, nil, nil)
	d := Diagnostics{}
	n := s.expandNeighbors(context.Background(), "t", "x", []ah.RelationName{rel1, rel2}, &d, 10)
	if len(n) != 1 {
		t.Fatalf("%v", n)
	}
}
