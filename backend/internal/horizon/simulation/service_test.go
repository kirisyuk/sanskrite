package simulation

import (
	"testing"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type testNestedEvent struct {
	Step    int                `json:"step"`
	Reason  string             `json:"reason,omitempty"`
	Factors map[string]float64 `json:"factors,omitempty"`
}

func TestSanitizeSimulationDocumentMap_JSONSafeNestedPayload(t *testing.T) {
	now := time.Date(2026, 3, 23, 10, 30, 0, 0, time.UTC)
	input := map[string]any{
		"seedFragmentIds":    []string{"frag-1"},
		"previewGeneratedAt": now,
		"propagationPaths": [][]string{
			{"a", "b", "c"},
		},
		"trace": []testNestedEvent{
			{
				Step:   1,
				Reason: "seed",
				Factors: map[string]float64{
					"damping": 0.8,
				},
			},
		},
	}

	got := sanitizeSimulationDocumentMap(input)
	if got == nil {
		t.Fatalf("sanitizeSimulationDocumentMap returned nil")
	}
	if got["previewGeneratedAt"] == nil {
		t.Fatalf("expected previewGeneratedAt to survive sanitization")
	}
	trace, ok := got["trace"].([]any)
	if !ok || len(trace) != 1 {
		t.Fatalf("expected trace to be []any with one item, got %#v", got["trace"])
	}
	event, ok := trace[0].(map[string]any)
	if !ok {
		t.Fatalf("expected trace event to be map[string]any, got %#v", trace[0])
	}
	if event["reason"] != "seed" {
		t.Fatalf("expected trace reason to round-trip, got %#v", event["reason"])
	}
	paths, ok := got["propagationPaths"].([]any)
	if !ok || len(paths) != 1 {
		t.Fatalf("expected propagationPaths to be []any with one item, got %#v", got["propagationPaths"])
	}
	pathObject, ok := paths[0].(map[string]any)
	if !ok {
		t.Fatalf("expected propagation path to be wrapped object, got %#v", paths[0])
	}
	items, ok := pathObject["items"].([]any)
	if !ok || len(items) != 3 {
		t.Fatalf("expected wrapped nested path items, got %#v", pathObject["items"])
	}
}

func TestRecognitionPrimaryGeoCodeUsesDomainGeoFacet(t *testing.T) {
	rec := map[string]any{
		"objectCandidates": []map[string]any{
			{"path": "/domain/v1/supply-chain/geo/kz/product/toys", "kind": "domain", "score": 2.0},
			{"path": "/market/kz/consumer-goods/toys", "kind": "market", "score": 1.5},
		},
	}
	if got := recognitionPrimaryGeoCode(rec); got != "kz" {
		t.Fatalf("expected kz, got %q", got)
	}
}

func TestArtifactShoulderCountriesUsesAnchorsAndLegsInsteadOfNoisyNodeRefs(t *testing.T) {
	artifact := &horizonmodels.MaterializedArtifact{
		Kind:           horizonmodels.MaterializedKindSupplyChainPath,
		AnchorSupplier: "/fac/cn/lego-jiaxing-factory",
		AnchorCustomer: "/fac/kz/marwin-almaty-distribution-center",
		NodeRefs: []string{
			"/fac/cn/lego-jiaxing-factory",
			"/fac/kz/marwin-almaty-distribution-center",
			"/fac/de/port-of-hamburg",
			"/fac/us/port-of-oakland",
		},
		Payload: horizonmodels.BuildSupplyChainPathPayloadDetailed(
			nil,
			nil,
			[]horizonmodels.SupplyChainPathLeg{
				{
					Index:            0,
					SupplierRef:      "/fac/cn/lego-jiaxing-factory",
					CustomerRef:      "/fac/kz/marwin-almaty-distribution-center",
					TransportViaRefs: []string{"/fac/cn/u-rumqi-rail-hub", "/fac/kz/khorgos-gateway"},
					GeoRefs:          []string{"/loc/cn", "/loc/kz"},
				},
			},
			nil,
			nil,
			nil,
			nil,
		),
	}
	got := artifactShoulderCountries(artifact, "kz")
	if len(got) != 1 || got[0] != "cn" {
		t.Fatalf("artifactShoulderCountries()=%v, want [cn]", got)
	}
}

func TestArtifactShoulderCountriesIgnoreTransitGeoNoise(t *testing.T) {
	artifact := &horizonmodels.MaterializedArtifact{
		Kind:           horizonmodels.MaterializedKindSupplyChainPath,
		AnchorSupplier: "/fac/cn/lego-jiaxing-factory",
		AnchorCustomer: "/fac/kz/marwin-meloman-almaty-distribution-center",
		NodeRefs: []string{
			"/fac/cn/lego-jiaxing-factory",
			"/fac/de/port-of-hamburg",
			"/fac/us/port-of-oakland",
			"/fac/kz/marwin-meloman-almaty-distribution-center",
		},
		Payload: horizonmodels.BuildSupplyChainPathPayloadDetailed(
			nil,
			nil,
			[]horizonmodels.SupplyChainPathLeg{
				{
					Index:            0,
					SupplierRef:      "/fac/cn/lego-jiaxing-factory",
					CustomerRef:      "/fac/kz/marwin-meloman-almaty-distribution-center",
					TransportViaRefs: []string{"/fac/de/port-of-hamburg", "/fac/us/port-of-oakland"},
					GeoRefs:          []string{"/loc/cn", "/loc/de", "/loc/us", "/loc/kz"},
				},
			},
			nil,
			nil,
			nil,
			nil,
		),
	}
	got := artifactShoulderCountries(artifact, "kz")
	if len(got) != 1 || got[0] != "cn" {
		t.Fatalf("artifactShoulderCountries()=%v, want [cn]", got)
	}
}

func TestBuildArtifactShoulderPreviewSuppressesWeakTailShoulders(t *testing.T) {
	preview := buildArtifactShoulderPreview(map[string]*artifactShoulderAggregate{
		"cn": {code: "cn", pathCount: 75, score: 49.78, maxStress: 0.513},
		"ru": {code: "ru", pathCount: 6, score: 4.273, maxStress: 0.462},
		"de": {code: "de", pathCount: 5, score: 3.692, maxStress: 0.460},
		"hu": {code: "hu", pathCount: 1, score: 0.704, maxStress: 0.449},
	})
	if len(preview) != 2 {
		t.Fatalf("expected 2 management-relevant shoulders, got %#v", preview)
	}
	if preview[0]["code"] != "cn" || preview[1]["code"] != "ru" {
		t.Fatalf("unexpected shoulder order/content: %#v", preview)
	}
}

func TestBuildArtifactChainPreviewKeepsOnlyAllowedShoulders(t *testing.T) {
	chains := buildArtifactChainPreview(map[string]*artifactChainAggregate{
		"cn::/product/finished/consumer/goods/toys": {code: "cn", path: "/product/finished/consumer/goods/toys", pathCount: 16, score: 10.81, maxStress: 0.513},
		"ru::/product/finished/consumer/goods/toys": {code: "ru", path: "/product/finished/consumer/goods/toys", pathCount: 6, score: 4.27, maxStress: 0.46},
		"de::/product/finished/consumer/goods/toys": {code: "de", path: "/product/finished/consumer/goods/toys", pathCount: 5, score: 3.69, maxStress: 0.46},
	}, map[string]struct{}{"cn": {}, "ru": {}})
	if len(chains) != 2 {
		t.Fatalf("expected 2 allowed chains, got %#v", chains)
	}
	for _, item := range chains {
		code, _ := item["countryCode"].(string)
		if code != "cn" && code != "ru" {
			t.Fatalf("unexpected chain country retained: %#v", item)
		}
	}
}
