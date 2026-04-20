package pathquery

import (
	"testing"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

func TestComputeCacheKey_stable(t *testing.T) {
	q := Query{
		Tenant:  "t1",
		Project: "p1",
		From:    Endpoint{Kind: EndpointCanonicalPath, Value: "/org/a"},
		To:      &Endpoint{Kind: EndpointCanonicalPath, Value: "/org/b"},
		Context: SearchContext{LensID: "L", Scope: "s", AsOf: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)},
		RelationKinds: []ah.RelationName{
			ah.RelationNameProduces,
			ah.RelationNameSupplies,
		},
		IncludeStress: true,
		IncludeGeo:    false,
	}
	k1 := computeCacheKey(q, EndpointCanonicalPath, "/org/a", true, EndpointCanonicalPath, "/org/b", 8, q.RelationKinds)
	k2 := computeCacheKey(q, EndpointCanonicalPath, "/org/a", true, EndpointCanonicalPath, "/org/b", 8, []ah.RelationName{ah.RelationNameSupplies, ah.RelationNameProduces})
	if k1 != k2 {
		t.Fatalf("relation order should not affect key: %s vs %s", k1, k2)
	}
	if len(k1) < 10 || k1[:3] != "pq_" {
		t.Fatalf("unexpected key: %s", k1)
	}
}

func TestAsOfCacheKey(t *testing.T) {
	if asOfCacheKey(time.Time{}) != "live" {
		t.Fatalf("zero asOf")
	}
	if got := asOfCacheKey(time.Date(2026, 4, 19, 15, 0, 0, 0, time.UTC)); got != "2026-04-19" {
		t.Fatalf("got %q", got)
	}
}
