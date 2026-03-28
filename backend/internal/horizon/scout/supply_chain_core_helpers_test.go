package scout

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestNormalizeAndDedupSupplyLinkSegments_PreservesDistinctRouteVariants(t *testing.T) {
	segments := normalizeAndDedupSupplyLinkSegments([]*SupplyLinkSegment{
		{
			SupplierRef:      "/fac/jp/a",
			ProductRef:       "/product/component/electronic/assembly/ev-battery-cell",
			CustomerRef:      "/org/us/tesla",
			TransportViaRefs: []string{"/fac/us/port-long-beach"},
			SegmentClass:     "external_product_flow",
			Confidence:       0.6,
			State: map[string]models.MetricValue{
				"volume": {Value: 10},
			},
		},
		{
			SupplierRef:      "/fac/jp/a",
			ProductRef:       "/product/component/electronic/assembly/ev-battery-cell",
			CustomerRef:      "/org/us/tesla",
			TransportViaRefs: []string{"/fac/us/port-long-beach", "/fac/us/port-oakland"},
			SegmentClass:     "external_product_flow",
			Confidence:       0.9,
			State: map[string]models.MetricValue{
				"share": {Value: 0.4},
			},
		},
	})
	if len(segments) != 2 {
		t.Fatalf("segments=%d, want 2", len(segments))
	}
	if got := len(segments[0].TransportViaRefs); got != 1 {
		t.Fatalf("first route via=%v, want 1 exact via ref", segments[0].TransportViaRefs)
	}
	if got := len(segments[1].TransportViaRefs); got != 2 {
		t.Fatalf("second route via=%v, want preserved ordered variant", segments[1].TransportViaRefs)
	}
}

func TestNormalizeAndDedupSupplyLinkSegments_MergesExactDuplicates(t *testing.T) {
	segments := normalizeAndDedupSupplyLinkSegments([]*SupplyLinkSegment{
		{
			SupplierRef:      "/fac/jp/a",
			ProductRef:       "/product/component/electronic/assembly/ev-battery-cell",
			CustomerRef:      "/org/us/tesla",
			TransportViaRefs: []string{"/fac/us/port-long-beach"},
			SegmentClass:     "external_product_flow",
			Confidence:       0.6,
			State: map[string]models.MetricValue{
				"volume": {Value: 10},
			},
		},
		{
			SupplierRef:      "/fac/jp/a",
			ProductRef:       "/product/component/electronic/assembly/ev-battery-cell",
			CustomerRef:      "/org/us/tesla",
			TransportViaRefs: []string{"/fac/us/port-long-beach"},
			SegmentClass:     "external_product_flow",
			Confidence:       0.9,
			State: map[string]models.MetricValue{
				"share": {Value: 0.4},
			},
		},
	})
	if len(segments) != 1 {
		t.Fatalf("segments=%d, want 1", len(segments))
	}
	seg := segments[0]
	if seg.Confidence != 0.9 {
		t.Fatalf("confidence=%v, want 0.9", seg.Confidence)
	}
	if len(seg.TransportViaRefs) != 1 {
		t.Fatalf("transportViaRefs=%v, want exact single route", seg.TransportViaRefs)
	}
	if _, ok := seg.State["volume"]; !ok {
		t.Fatalf("expected merged volume metric")
	}
	if _, ok := seg.State["share"]; !ok {
		t.Fatalf("expected merged share metric")
	}
}

func TestBuildOperationalEdgesFromSegments_CompletesTriad(t *testing.T) {
	edges := buildOperationalEdgesFromSegments([]*SupplyLinkSegment{
		{
			SupplierRef:      "/fac/jp/panasonic-giga-nevada-lines",
			ProductRef:       "/product/component/electronic/assembly/ev-battery-cell",
			CustomerRef:      "/org/us/tesla",
			TransportViaRefs: []string{"/fac/us/port-long-beach"},
			SegmentClass:     "external_product_flow",
			Confidence:       0.88,
		},
	})
	if len(edges) != 3 {
		t.Fatalf("edges=%d, want 3", len(edges))
	}
	var hasProduces, hasProductSupply, hasActorSupply bool
	for _, edge := range edges {
		if edge == nil {
			continue
		}
		switch {
		case edge.RelationType == string(models.DependencyTypeProduces) &&
			edge.SourceCanonicalPath == "/fac/jp/panasonic-giga-nevada-lines" &&
			edge.TargetCanonicalPath == "/product/component/electronic/assembly/ev-battery-cell":
			hasProduces = true
		case edge.RelationType == string(models.DependencyTypeSuppliesTo) &&
			edge.SourceCanonicalPath == "/product/component/electronic/assembly/ev-battery-cell" &&
			edge.TargetCanonicalPath == "/org/us/tesla":
			hasProductSupply = true
			if got := extractProductQualifier(edge.Qualifiers); got != "/product/component/electronic/assembly/ev-battery-cell" {
				t.Fatalf("product->customer qualifier=%q", got)
			}
		case edge.RelationType == string(models.DependencyTypeSuppliesTo) &&
			edge.SourceCanonicalPath == "/fac/jp/panasonic-giga-nevada-lines" &&
			edge.TargetCanonicalPath == "/org/us/tesla":
			hasActorSupply = true
			if got := extractProductQualifier(edge.Qualifiers); got != "/product/component/electronic/assembly/ev-battery-cell" {
				t.Fatalf("actor->actor qualifier=%q", got)
			}
		}
	}
	if !hasProduces || !hasProductSupply || !hasActorSupply {
		t.Fatalf("hasProduces=%v hasProductSupply=%v hasActorSupply=%v", hasProduces, hasProductSupply, hasActorSupply)
	}
}

func TestBuildSegmentsFromParsed_DedupesRepeatedSegments(t *testing.T) {
	parsed := &ParsedChainResult{
		SupplyLinkSegments: []*SupplyLinkSegment{
			{
				SupplierRef:  "/fac/jp/a",
				ProductRef:   "/product/x",
				CustomerRef:  "/org/us/b",
				SegmentClass: "external_product_flow",
				Confidence:   0.7,
			},
			{
				SupplierRef:  "/fac/jp/a",
				ProductRef:   "/product/x",
				CustomerRef:  "/org/us/b",
				SegmentClass: "external_product_flow",
				Confidence:   0.8,
			},
		},
	}
	segments := buildSegmentsFromParsed(parsed, nil, nil)
	if len(segments) != 1 {
		t.Fatalf("segments=%d, want 1", len(segments))
	}
	if segments[0].Confidence != 0.8 {
		t.Fatalf("confidence=%v, want 0.8", segments[0].Confidence)
	}
}

func TestDeriveSupplyLinkSegmentsFromEdges_DirectActorSupply(t *testing.T) {
	segments := deriveSupplyLinkSegmentsFromEdges([]*ChainEdge{
		{
			SourceCanonicalPath: "/fac/kr/wroclaw-plant",
			TargetCanonicalPath: "/org/be/umicore",
			RelationType:        string(models.DependencyTypeSuppliesTo),
			Confidence:          0.72,
			Qualifiers: map[string]interface{}{
				"product_ref":                "/product/component/electronic/assembly/lithium-ion-battery-cells",
				models.QualifierTransportVia: []string{"/fac/pl/port-gdansk"},
			},
		},
	})
	if len(segments) != 1 {
		t.Fatalf("segments=%d, want 1", len(segments))
	}
	seg := segments[0]
	if seg.SupplierRef != "/fac/kr/wroclaw-plant" || seg.ProductRef != "/product/component/electronic/assembly/lithium-ion-battery-cells" || seg.CustomerRef != "/org/be/umicore" {
		t.Fatalf("segment=%+v", seg)
	}
	if len(seg.TransportViaRefs) != 1 || seg.TransportViaRefs[0] != "/fac/pl/port-gdansk" {
		t.Fatalf("via=%v", seg.TransportViaRefs)
	}
}

func TestDeriveSupplyLinkSegmentsFromEdges_ComposesFromTriadEdges(t *testing.T) {
	segments := deriveSupplyLinkSegmentsFromEdges([]*ChainEdge{
		{
			SourceCanonicalPath: "/fac/fr/billy-berclau-douvrin-gigafactory",
			TargetCanonicalPath: "/product/component/electronic/assembly/battery-cell",
			RelationType:        string(models.DependencyTypeProduces),
			Confidence:          0.61,
		},
		{
			SourceCanonicalPath: "/product/component/electronic/assembly/battery-cell",
			TargetCanonicalPath: "/org/be/umicore",
			RelationType:        string(models.DependencyTypeSuppliesTo),
			Confidence:          0.77,
			Qualifiers: map[string]interface{}{
				models.QualifierTransportVia: []string{"/fac/be/port-of-antwerp-bruges"},
			},
		},
	})
	if len(segments) != 1 {
		t.Fatalf("segments=%d, want 1", len(segments))
	}
	seg := segments[0]
	if seg.SupplierRef != "/fac/fr/billy-berclau-douvrin-gigafactory" {
		t.Fatalf("supplier=%q", seg.SupplierRef)
	}
	if seg.ProductRef != "/product/component/electronic/assembly/battery-cell" {
		t.Fatalf("product=%q", seg.ProductRef)
	}
	if seg.CustomerRef != "/org/be/umicore" {
		t.Fatalf("customer=%q", seg.CustomerRef)
	}
	if seg.Confidence != 0.77 {
		t.Fatalf("confidence=%v, want 0.77", seg.Confidence)
	}
	if len(seg.TransportViaRefs) != 1 || seg.TransportViaRefs[0] != "/fac/be/port-of-antwerp-bruges" {
		t.Fatalf("via=%v", seg.TransportViaRefs)
	}
}

func TestRewriteChainEdgeWithPathRewrites_RewritesQualifierRefs(t *testing.T) {
	edge := rewriteChainEdgeWithPathRewrites(&ChainEdge{
		SourceCanonicalPath: "/org/jp/yazaki",
		TargetCanonicalPath: "/fac/toyota-motor-corp/toyota-city-plant",
		RelationType:        string(models.DependencyTypeSuppliesTo),
		Confidence:          0.7,
		Qualifiers: map[string]interface{}{
			"product_ref": "/product/component/electrical/manufacturing/automotive-wire-harness",
			"productRef":  "/product/component/electrical/manufacturing/automotive-wire-harness",
		},
	}, map[string]string{
		"/org/jp/yazaki": "/org/jp/yazaki-corp",
		"/product/component/electrical/manufacturing/automotive-wire-harness": "/product/finished/mechanical/assembly/automotive-wire-harness",
	})
	if edge == nil {
		t.Fatal("rewriteChainEdgeWithPathRewrites returned nil")
	}
	if edge.SourceCanonicalPath != "/org/jp/yazaki-corp" {
		t.Fatalf("source=%q", edge.SourceCanonicalPath)
	}
	if got := extractProductQualifier(edge.Qualifiers); got != "/product/finished/mechanical/assembly/automotive-wire-harness" {
		t.Fatalf("product qualifier=%q", got)
	}
}
