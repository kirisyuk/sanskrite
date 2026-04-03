package artifactrewrite

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathbuilder"
)

func TestMergeRewrittenArtifactPayload_PreservesLegIdentityAndProduct(t *testing.T) {
	parsed := &models.SupplyChainPathPayload{
		Nodes: []models.SupplyChainPathNode{
			{Type: "fac", Ref: "/fac/sa/abqaiq-plant"},
			{Type: "product", Ref: "/product/raw/hydrocarbon/crude-oil"},
			{Type: "fac", Ref: "/fac/sa/yasref-refinery"},
		},
		Edges: []models.SupplyChainPathEdge{
			{
				Type:       "SUPPLY_LINK",
				From:       0,
				To:         2,
				ProductRef: "/product/raw/hydrocarbon/crude-oil",
			},
		},
		Legs: []models.SupplyChainPathLeg{
			{
				Index:                   0,
				SupplierRef:             "/fac/sa/abqaiq-plant",
				ProductRef:              "/product/raw/hydrocarbon/crude-oil",
				CustomerRef:             "/fac/sa/yasref-refinery",
				SupplyLinkCanonicalPath: "/supply-link/global/abqaiq/crude-oil/yasref",
				RouteVariantKey:         "variant-a",
				RouteCoordinates:        []models.Coordinates{{Lat: -20.0, Lon: 121.0}},
				RouteGeometrySource:     "fragmentary_waypoint_sequence",
				FlowRefs:                []string{"sea_us_gulf_jebel_ali_via_suez"},
			},
		},
		Notes: map[string]interface{}{"artifactKind": "SUPPLY_CHAIN_PATH"},
	}
	draft := &pathbuilder.SupplyChainMaterializationDraft{
		Legs: []models.SupplyChainPathLeg{
			{
				Index:                   0,
				SupplierRef:             "/fac/sa/abqaiq-plant",
				ProductRef:              "/product/raw/hydrocarbon/crude-oil",
				CustomerRef:             "/fac/sa/yasref-refinery",
				SupplyLinkCanonicalPath: "/supply-link/global/abqaiq/crude-oil/yasref",
				RouteVariantKey:         "variant-a",
				TransportViaRefs:        []string{"/fac/sa/east-west-pipeline"},
				RouteCoordinates: []models.Coordinates{
					{Lat: 24.95, Lon: 43.85},
					{Lat: 23.97, Lon: 38.20},
				},
				RouteGeometrySource: "fragmentary_waypoint_sequence",
				FlowRefs:            []string{"land_sa_east_west_pipeline"},
				AlternateFlowRefs:   []string{"sea_us_gulf_jebel_ali_via_suez"},
				Mode:                "pipeline",
				BindingConfidence:   0.88,
				SegmentConfidence:   0.77,
			},
		},
	}

	mergedLegs, _, _, ok := mergeRewrittenArtifactPayload(parsed, draft)
	if !ok {
		t.Fatal("expected merge to succeed")
	}
	if len(mergedLegs) != 1 {
		t.Fatalf("legs=%d, want 1", len(mergedLegs))
	}
	if mergedLegs[0].ProductRef != "/product/raw/hydrocarbon/crude-oil" {
		t.Fatalf("productRef=%q, want preserved crude-oil", mergedLegs[0].ProductRef)
	}
	if mergedLegs[0].SupplyLinkCanonicalPath != "/supply-link/global/abqaiq/crude-oil/yasref" {
		t.Fatalf("supplyLinkCanonicalPath=%q", mergedLegs[0].SupplyLinkCanonicalPath)
	}
	if len(mergedLegs[0].RouteCoordinates) != 2 || mergedLegs[0].RouteCoordinates[0].Lat != 24.95 {
		t.Fatalf("routeCoordinates=%+v", mergedLegs[0].RouteCoordinates)
	}
	if len(mergedLegs[0].TransportViaRefs) != 1 || mergedLegs[0].TransportViaRefs[0] != "/fac/sa/east-west-pipeline" {
		t.Fatalf("transportViaRefs=%v", mergedLegs[0].TransportViaRefs)
	}
	if len(mergedLegs[0].FlowRefs) != 1 || mergedLegs[0].FlowRefs[0] != "land_sa_east_west_pipeline" {
		t.Fatalf("flowRefs=%v", mergedLegs[0].FlowRefs)
	}
	if len(mergedLegs[0].AlternateFlowRefs) != 1 || mergedLegs[0].AlternateFlowRefs[0] != "sea_us_gulf_jebel_ali_via_suez" {
		t.Fatalf("alternateFlowRefs=%v", mergedLegs[0].AlternateFlowRefs)
	}
}
