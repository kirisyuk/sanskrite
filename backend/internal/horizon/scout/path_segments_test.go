package scout

import "testing"

func TestBuildSegmentsFromParsed_RewritesTransportViaRefs(t *testing.T) {
	parsed := &ParsedChainResult{
		SupplyLinkSegments: []*SupplyLinkSegment{
			{
				SupplierRef:      "/fac/ae/jebel-ali-terminal",
				ProductRef:       "/product/raw/hydrocarbon/distribution/lng",
				CustomerRef:      "/fac/nl/port-of-rotterdam",
				TransportViaRefs: []string{"/fac/na/strait-of-hormuz-terminal", "/fac/na/suez-terminal"},
				SegmentClass:     "external_product_flow",
				Confidence:       0.81,
			},
		},
	}
	segments := buildSegmentsFromParsed(parsed, map[string]string{
		"/fac/na/strait-of-hormuz-terminal": "/fac/sea/strait-of-hormuz-terminal",
		"/fac/na/suez-terminal":             "/fac/eg/suez-terminal",
	}, nil)
	if len(segments) != 1 {
		t.Fatalf("segments=%d, want 1", len(segments))
	}
	if got, want := len(segments[0].TransportViaRefs), 2; got != want {
		t.Fatalf("via refs=%d, want %d", got, want)
	}
	if segments[0].TransportViaRefs[0] != "/fac/sea/strait-of-hormuz-terminal" {
		t.Fatalf("first via=%q", segments[0].TransportViaRefs[0])
	}
	if segments[0].TransportViaRefs[1] != "/fac/eg/suez-terminal" {
		t.Fatalf("second via=%q", segments[0].TransportViaRefs[1])
	}
}

func TestBuildSegmentsFromParsed_SkipsInternalSupportSegment(t *testing.T) {
	parsed := &ParsedChainResult{
		SupplyLinkSegments: []*SupplyLinkSegment{
			{
				SupplierRef:  "/fac/qa/ras-laffan-port",
				ProductRef:   "/product/services/logistics/port-operations",
				CustomerRef:  "/org/qa/qatarenergy",
				SegmentClass: "internal_support",
				Confidence:   0.68,
			},
		},
	}
	segments := buildSegmentsFromParsed(parsed, nil, nil)
	if len(segments) != 0 {
		t.Fatalf("segments=%d, want 0", len(segments))
	}
}

func TestBuildSegmentsFromParsed_FallsBackToEdgeDerivedSegments(t *testing.T) {
	parsed := &ParsedChainResult{
		Edges: []*ChainEdge{
			{
				SourceCanonicalPath: "/fac/jp/yazaki",
				TargetCanonicalPath: "/product/component/electrical/manufacturing/automotive-wire-harness",
				RelationType:        "produces",
				Confidence:          0.63,
			},
			{
				SourceCanonicalPath: "/product/component/electrical/manufacturing/automotive-wire-harness",
				TargetCanonicalPath: "/fac/toyota-motor-corp/toyota-city-plant",
				RelationType:        "supplies",
				Confidence:          0.74,
			},
		},
	}
	segments := buildSegmentsFromParsed(parsed, map[string]string{
		"/fac/jp/yazaki": "/org/jp/yazaki-corp",
		"/product/component/electrical/manufacturing/automotive-wire-harness": "/product/finished/mechanical/assembly/automotive-wire-harness",
	}, nil)
	if len(segments) != 1 {
		t.Fatalf("segments=%d, want 1", len(segments))
	}
	seg := segments[0]
	if seg.SupplierRef != "/org/jp/yazaki-corp" {
		t.Fatalf("supplier=%q", seg.SupplierRef)
	}
	if seg.ProductRef != "/product/finished/mechanical/assembly/automotive-wire-harness" {
		t.Fatalf("product=%q", seg.ProductRef)
	}
	if seg.CustomerRef != "/fac/toyota-motor-corp/toyota-city-plant" {
		t.Fatalf("customer=%q", seg.CustomerRef)
	}
}
