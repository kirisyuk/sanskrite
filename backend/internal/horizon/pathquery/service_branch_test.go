package pathquery

import (
	"context"
	"testing"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestService_QueryPaths_missingFragmentsRepo(t *testing.T) {
	s := NewService(nil, nil, nil, nil, nil)
	_, err := s.QueryPaths(context.Background(), Query{
		Tenant: "t",
		From:   Endpoint{Kind: EndpointCanonicalPath, Value: "/org/x"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCollectSupplyChainsForLens_nilInputs(t *testing.T) {
	a, b, err := CollectSupplyChainsForLens(context.Background(), nil, &horizonmodels.Lens{ID: "l"}, 10)
	if err != nil || a != nil || b != nil {
		t.Fatalf("got %v %v %v", a, b, err)
	}
}

func TestCollectFragmentTraceSupplyChains_nil(t *testing.T) {
	a, err := CollectFragmentTraceSupplyChains(context.Background(), nil, nil, &horizonmodels.HorizonFragment{ID: "f"}, "")
	if err != nil || a != nil {
		t.Fatalf("got %v err=%v", a, err)
	}
}

func TestSearchSupplyChainAnchors_nilMat(t *testing.T) {
	a, err := SearchSupplyChainAnchors(context.Background(), nil, nil, "t", []string{"/p"}, "", 10)
	if err != nil || a != nil {
		t.Fatalf("got %v err=%v", a, err)
	}
}

func TestService_Invalidate_noDB(t *testing.T) {
	s := NewService(nil, nil, nil, nil, nil)
	if err := s.InvalidatePathCache(context.Background(), "pq_x"); err != nil {
		t.Fatal(err)
	}
	if err := s.InvalidateByNode(context.Background(), "t", "/p"); err != nil {
		t.Fatal(err)
	}
}

func TestBfsReachable_noEdges(t *testing.T) {
	s := NewService(nil, nil, nil, nil, nil)
	nq := &normalizedQuery{
		ToPresent:      true,
		FromFragmentID: "a",
		ToFragmentID:   "b",
		MaxHops:        2,
		RelationKinds:  defaultRelationKinds,
	}
	d := Diagnostics{}
	if s.bfsReachable(context.Background(), nq, &d) {
		t.Fatal("expected false without edges repo")
	}
}

func TestExtendWithPathbuilder_nil(t *testing.T) {
	s := NewService(nil, nil, nil, nil, nil)
	nq := &normalizedQuery{
		ToPresent:    true,
		ToCanonical:  "/org/b",
		FromCanonical: "/org/a",
		Query: Query{
			Tenant: "t",
		},
	}
	d := Diagnostics{}
	if got := s.extendWithPathbuilder(context.Background(), nq, &d); len(got) != 0 {
		t.Fatalf("got %d", len(got))
	}
}

func TestHydratePathsFromCache_emptyIDs(t *testing.T) {
	s := NewService(nil, nil, nil, nil, nil)
	_, err := s.hydratePathsFromCache(context.Background(), "t", nil)
	if err == nil {
		t.Fatal("expected error without mat repo")
	}
}

func TestCacheWrite_nilDB(t *testing.T) {
	s := NewService(nil, nil, nil, nil, nil)
	if err := s.cacheWrite(context.Background(), "k", pathCacheDocument{}); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeQuery_toEmpty(t *testing.T) {
	to := Endpoint{Kind: EndpointCanonicalPath, Value: ""}
	_, err := normalizeQuery(Query{Tenant: "t", From: Endpoint{Kind: EndpointCanonicalPath, Value: "/a"}, To: &to})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestNormalizeQuery_cacheRefresh(t *testing.T) {
	q := Query{
		Tenant:    "t",
		From:      Endpoint{Kind: EndpointCanonicalPath, Value: "/a"},
		CacheMode: CacheModeRefresh,
	}
	nq, err := normalizeQuery(q)
	if err != nil {
		t.Fatal(err)
	}
	if nq.CacheRead || !nq.CacheWrite {
		t.Fatalf("refresh mode: read=%v write=%v", nq.CacheRead, nq.CacheWrite)
	}
}

func TestAsOfFromQuery(t *testing.T) {
	q := Query{
		Tenant: "t",
		From:   Endpoint{Kind: EndpointCanonicalPath, Value: "/a"},
		Context: SearchContext{
			AsOf: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		},
	}
	k := computeCacheKey(q, EndpointCanonicalPath, "/a", false, 0, "", 8, defaultRelationKinds)
	if k == "" {
		t.Fatal("empty key")
	}
}
