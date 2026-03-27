package simscope

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestFilterRecognitionObjectsDropsOffScopeToyActors(t *testing.T) {
	refs := horizonmodels.SimulationBaselineRefs{
		ScopePaths: []string{
			"/domain/v1/supply-chain/geo/kz/product/toys",
			"/market/kz/consumer-goods/toys",
			"/fac/kz/khorgos-gateway",
			"/gov/kz/state-revenue-committee",
		},
	}
	objects := []map[string]any{
		{"kind": "domain", "path": "/domain/v1/supply-chain/geo/kz/product/toys", "score": 2.2, "sources": []any{"lens.domainQuery"}},
		{"kind": "market", "path": "/market/kz/consumer-goods/toys", "score": 2.1, "sources": []any{"lens.scope"}},
		{"kind": "fac", "path": "/fac/kz/khorgos-gateway", "score": 1.2, "sources": []any{"lens.scope"}},
		{"kind": "org", "path": "/org/dk/lego-group", "score": 1.2, "sources": []any{"lens.scope"}},
		{"kind": "org", "path": "/org/de/uniper", "score": 1.05, "sources": []any{"baseline.scope"}},
		{"kind": "org", "path": "/org/de/volkswagen", "score": 1.05, "sources": []any{"baseline.fragment"}},
	}
	profile := BuildProfile("Toy imports into Kazakhstan face tighter customs scrutiny and child-safety enforcement.", &horizonmodels.Lens{
		Scope: []string{
			"/domain/v1/supply-chain/geo/kz/product/toys",
			"/market/kz/consumer-goods/toys",
			"/fac/kz/khorgos-gateway",
			"/gov/kz/state-revenue-committee",
			"/org/dk/lego-group",
		},
		DomainQueries: []horizonmodels.LensDomainQuery{{CanonicalPath: "/domain/v1/supply-chain/geo/kz/product/toys"}},
	}, refs, nil)
	filtered := FilterRecognitionObjects(objects, profile, 12)
	got := map[string]bool{}
	for _, object := range filtered {
		got[stringValue(object, "path")] = true
	}
	if !got["/org/dk/lego-group"] {
		t.Fatalf("expected lego group to remain in filtered recognition: %v", got)
	}
	if got["/org/de/uniper"] {
		t.Fatalf("expected unrelated uniper actor to be filtered out: %v", got)
	}
	if got["/org/de/volkswagen"] {
		t.Fatalf("expected unrelated volkswagen actor to be filtered out: %v", got)
	}
}

func TestArtifactMatchesProfileRejectsOffTopicPathFamilies(t *testing.T) {
	profile := BuildProfile("Toy imports into Kazakhstan face tighter customs scrutiny.", &horizonmodels.Lens{
		DomainQueries: []horizonmodels.LensDomainQuery{{CanonicalPath: "/domain/v1/supply-chain/geo/kz/product/toys"}},
		Scope: []string{
			"/domain/v1/supply-chain/geo/kz/product/toys",
			"/market/kz/consumer-goods/toys",
			"/fac/kz/khorgos-gateway",
		},
	}, horizonmodels.SimulationBaselineRefs{
		ScopePaths: []string{
			"/domain/v1/supply-chain/geo/kz/product/toys",
			"/market/kz/consumer-goods/toys",
			"/fac/kz/khorgos-gateway",
		},
	}, map[string]any{
		"objectCandidates": []map[string]any{
			{"kind": "domain", "path": "/domain/v1/supply-chain/geo/kz/product/toys", "score": 2.2, "sources": []string{"lens.domainQuery"}},
			{"kind": "fac", "path": "/fac/kz/khorgos-gateway", "score": 1.2, "sources": []string{"lens.scope"}},
		},
	})
	relevant := &horizonmodels.MaterializedArtifact{
		Kind:           horizonmodels.MaterializedKindSupplyChainPath,
		ProductAxis:    "/product/finished/mechanical/assembly/toys",
		AnchorCustomer: "/fac/kz/khorgos-gateway",
		NodeRefs: []string{
			"/fac/cn/alpha-group-shantou-plant",
			"/fac/kz/khorgos-gateway",
		},
	}
	irrelevant := &horizonmodels.MaterializedArtifact{
		Kind:           horizonmodels.MaterializedKindSupplyChainPath,
		ProductAxis:    "/product/raw/hydrocarbon/extraction/natural-gas",
		AnchorCustomer: "/org/de/uniper",
		NodeRefs: []string{
			"/fac/qa/north-field-production-complex",
			"/org/de/uniper",
		},
	}
	if !ArtifactMatchesProfile(relevant, profile) {
		t.Fatalf("expected toy artifact to match profile")
	}
	if ArtifactMatchesProfile(irrelevant, profile) {
		t.Fatalf("expected natural gas artifact to be rejected")
	}
}

func TestArtifactMatchesProfileRejectsToyArtifactThatNeverTouchesPrimaryGeo(t *testing.T) {
	profile := BuildProfile("Toy imports into Kazakhstan face tighter customs scrutiny.", &horizonmodels.Lens{
		DomainQueries: []horizonmodels.LensDomainQuery{{CanonicalPath: "/domain/v1/supply-chain/geo/kz/product/toys"}},
		Scope: []string{
			"/domain/v1/supply-chain/geo/kz/product/toys",
			"/market/kz/consumer-goods/toys",
			"/fac/kz/khorgos-gateway",
		},
	}, horizonmodels.SimulationBaselineRefs{
		ScopePaths: []string{
			"/domain/v1/supply-chain/geo/kz/product/toys",
			"/market/kz/consumer-goods/toys",
			"/fac/kz/khorgos-gateway",
		},
	}, map[string]any{
		"objectCandidates": []map[string]any{
			{"kind": "domain", "path": "/domain/v1/supply-chain/geo/kz/product/toys", "score": 2.2, "sources": []string{"lens.domainQuery"}},
			{"kind": "fac", "path": "/fac/kz/khorgos-gateway", "score": 1.2, "sources": []string{"lens.scope"}},
		},
	})
	offScopeToy := &horizonmodels.MaterializedArtifact{
		Kind:           horizonmodels.MaterializedKindSupplyChainPath,
		ProductAxis:    "/product/finished/mechanical/assembly/construction-toys",
		AnchorSupplier: "/fac/cn/jiaxing-factory",
		AnchorCustomer: "/fac/us/port-of-long-beach",
		NodeRefs: []string{
			"/fac/cn/jiaxing-factory",
			"/fac/cn/port-of-ningbo-zhoushan",
			"/fac/us/port-of-long-beach",
		},
		Payload: horizonmodels.BuildSupplyChainPathPayloadDetailed(
			nil,
			nil,
			[]horizonmodels.SupplyChainPathLeg{
				{
					Index:            0,
					SupplierRef:      "/fac/cn/jiaxing-factory",
					CustomerRef:      "/fac/us/port-of-long-beach",
					TransportViaRefs: []string{"/fac/cn/port-of-ningbo-zhoushan"},
					GeoRefs:          []string{"/loc/cn", "/loc/us"},
				},
			},
			nil,
			nil,
			nil,
			nil,
		),
	}
	if ArtifactMatchesProfile(offScopeToy, profile) {
		t.Fatalf("expected toy artifact without Kazakhstan touch to be rejected")
	}
}

func TestArtifactMatchesProfileRejectsTransitToyArtifactThatOnlyPassesThroughPrimaryGeo(t *testing.T) {
	profile := BuildProfile("Toy imports into Kazakhstan face tighter customs scrutiny.", &horizonmodels.Lens{
		DomainQueries: []horizonmodels.LensDomainQuery{{CanonicalPath: "/domain/v1/supply-chain/geo/kz/product/toys"}},
		Scope: []string{
			"/domain/v1/supply-chain/geo/kz/product/toys",
			"/market/kz/consumer-goods/toys",
			"/fac/kz/khorgos-gateway",
		},
	}, horizonmodels.SimulationBaselineRefs{
		ScopePaths: []string{
			"/domain/v1/supply-chain/geo/kz/product/toys",
			"/market/kz/consumer-goods/toys",
			"/fac/kz/khorgos-gateway",
		},
	}, map[string]any{
		"objectCandidates": []map[string]any{
			{"kind": "domain", "path": "/domain/v1/supply-chain/geo/kz/product/toys", "score": 2.2, "sources": []string{"lens.domainQuery"}},
			{"kind": "fac", "path": "/fac/kz/khorgos-gateway", "score": 1.2, "sources": []string{"lens.scope"}},
		},
	})
	transitToy := &horizonmodels.MaterializedArtifact{
		Kind:           horizonmodels.MaterializedKindSupplyChainPath,
		ProductAxis:    "/product/finished/mechanical/assembly/toys",
		AnchorSupplier: "/fac/cn/lego-jiaxing-factory",
		AnchorCustomer: "/fac/de/duisport-terminal-duisburg",
		NodeRefs: []string{
			"/fac/cn/lego-jiaxing-factory",
			"/fac/cn/u-rumqi-rail-hub",
			"/fac/kz/khorgos-gateway",
			"/fac/de/duisport-terminal-duisburg",
		},
		Payload: horizonmodels.BuildSupplyChainPathPayloadDetailed(
			nil,
			nil,
			[]horizonmodels.SupplyChainPathLeg{
				{
					Index:            0,
					SupplierRef:      "/fac/cn/lego-jiaxing-factory",
					CustomerRef:      "/fac/kz/khorgos-gateway",
					TransportViaRefs: []string{"/fac/cn/u-rumqi-rail-hub"},
					GeoRefs:          []string{"/loc/cn", "/loc/kz"},
				},
				{
					Index:       1,
					SupplierRef: "/fac/kz/khorgos-gateway",
					CustomerRef: "/fac/de/duisport-terminal-duisburg",
					GeoRefs:     []string{"/loc/kz", "/loc/de"},
				},
			},
			nil,
			nil,
			nil,
			nil,
		),
	}
	if ArtifactMatchesProfile(transitToy, profile) {
		t.Fatalf("expected transit-only Kazakhstan touch to be rejected")
	}
}

func TestArtifactMatchesProfileKeepsKazakhstanLeakageRoute(t *testing.T) {
	profile := BuildProfile("Toy imports into Kazakhstan face tighter customs scrutiny and informal-market leakage.", &horizonmodels.Lens{
		DomainQueries: []horizonmodels.LensDomainQuery{{CanonicalPath: "/domain/v1/supply-chain/geo/kz/product/toys"}},
		Scope: []string{
			"/domain/v1/supply-chain/geo/kz/product/toys",
			"/market/kz/consumer-goods/toys",
			"/fac/kz/khorgos-gateway",
		},
	}, horizonmodels.SimulationBaselineRefs{
		ScopePaths: []string{
			"/domain/v1/supply-chain/geo/kz/product/toys",
			"/market/kz/consumer-goods/toys",
			"/fac/kz/khorgos-gateway",
		},
	}, map[string]any{
		"objectCandidates": []map[string]any{
			{"kind": "domain", "path": "/domain/v1/supply-chain/geo/kz/product/toys", "score": 2.2, "sources": []string{"lens.domainQuery"}},
			{"kind": "fac", "path": "/fac/kz/khorgos-gateway", "score": 1.2, "sources": []string{"lens.scope"}},
		},
	})
	leakageRoute := &horizonmodels.MaterializedArtifact{
		Kind:           horizonmodels.MaterializedKindSupplyChainPath,
		ProductAxis:    "/product/finished/consumer/distribution/toys-and-games",
		AnchorSupplier: "/fac/kz/marwin-almaty-distribution-center",
		AnchorCustomer: "/fac/ru/detskiy-mir-moscow-dc",
		NodeRefs: []string{
			"/fac/kz/marwin-almaty-distribution-center",
			"/fac/ru/detskiy-mir-moscow-dc",
		},
	}
	if !ArtifactMatchesProfile(leakageRoute, profile) {
		t.Fatalf("expected Kazakhstan leakage route to remain in scope")
	}
}

func TestBuildProfileDoesNotExpandToyDomainFromOffTopicScopeProducts(t *testing.T) {
	profile := BuildProfile("Toy imports into Kazakhstan face tighter customs scrutiny.", &horizonmodels.Lens{
		DomainQueries: []horizonmodels.LensDomainQuery{{CanonicalPath: "/domain/v1/supply-chain/geo/kz/product/toys"}},
		Scope: []string{
			"/domain/v1/supply-chain/geo/kz/product/toys",
			"/market/kz/consumer-goods/toys",
			"/fac/kz/khorgos-gateway",
		},
	}, horizonmodels.SimulationBaselineRefs{
		ScopePaths: []string{
			"/domain/v1/supply-chain/geo/kz/product/toys",
			"/market/kz/consumer-goods/toys",
			"/fac/kz/khorgos-gateway",
			"/product/component/mechanical/assembly/automotive-kits",
			"/product/finished/mechanical/assembly/nuclear-fuel-assembly",
		},
	}, map[string]any{
		"objectCandidates": []map[string]any{
			{"kind": "domain", "path": "/domain/v1/supply-chain/geo/kz/product/toys", "score": 2.2, "sources": []string{"lens.domainQuery"}},
			{"kind": "market", "path": "/market/kz/consumer-goods/toys", "score": 2.1, "sources": []string{"lens.scope"}},
		},
	})
	if !tokenMatchInSet("/product/finished/mechanical/assembly/toys", makeSet(profile.DomainProductTokens)) {
		t.Fatalf("expected toy tokens to remain in domain product tokens: %v", profile.DomainProductTokens)
	}
	if tokenMatchInSet("/product/component/mechanical/assembly/automotive-kits", makeSet(profile.DomainProductTokens)) {
		t.Fatalf("expected off-topic automotive product to stay out of toy domain tokens: %v", profile.DomainProductTokens)
	}
	if tokenMatchInSet("/product/finished/mechanical/assembly/nuclear-fuel-assembly", makeSet(profile.DomainProductTokens)) {
		t.Fatalf("expected off-topic nuclear product to stay out of toy domain tokens: %v", profile.DomainProductTokens)
	}
}
