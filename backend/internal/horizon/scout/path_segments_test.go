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
	})
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
	segments := buildSegmentsFromParsed(parsed, nil)
	if len(segments) != 0 {
		t.Fatalf("segments=%d, want 0", len(segments))
	}
}
