package pathquery

import (
	"testing"
	"time"
)

func TestNormalizeQuery_defaults(t *testing.T) {
	q := Query{Tenant: "t", From: Endpoint{Kind: EndpointCanonicalPath, Value: "/p"}}
	nq, err := normalizeQuery(q)
	if err != nil {
		t.Fatal(err)
	}
	if nq.MaxHops != defaultMaxHops {
		t.Fatalf("maxHops %d", nq.MaxHops)
	}
	if len(nq.RelationKinds) != 3 {
		t.Fatalf("rels %v", nq.RelationKinds)
	}
	if !nq.CacheRead || !nq.CacheWrite {
		t.Fatal("expected default cache read/write")
	}
	if nq.EffectiveTTL != DefaultCacheTTL {
		t.Fatalf("ttl %v", nq.EffectiveTTL)
	}
}

func TestNormalizeQuery_cacheDisabled(t *testing.T) {
	q := Query{Tenant: "t", From: Endpoint{Kind: EndpointCanonicalPath, Value: "/p"}, CacheMode: CacheModeDisabled}
	nq, err := normalizeQuery(q)
	if err != nil {
		t.Fatal(err)
	}
	if nq.CacheRead || nq.CacheWrite {
		t.Fatal("cache should be off")
	}
	if nq.EffectiveTTL != 0 {
		t.Fatalf("ttl %v", nq.EffectiveTTL)
	}
}

func TestNormalizeQuery_negativeTTL(t *testing.T) {
	q := Query{Tenant: "t", From: Endpoint{Kind: EndpointCanonicalPath, Value: "/p"}, CacheTTL: -1 * time.Hour}
	_, err := normalizeQuery(q)
	if err == nil {
		t.Fatal("expected error")
	}
}
