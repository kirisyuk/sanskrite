package horizon

import (
	"testing"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

func TestAnnotateSupplyChainTraceArtifact_DirectAnchorAndStress(t *testing.T) {
	artifact := &horizonmodels.MaterializedArtifact{
		ID:        "chain-1",
		TenantID:  "global",
		Kind:      horizonmodels.MaterializedKindSupplyChainPath,
		Status:    "active",
		UpdatedAt: time.Now(),
		NodeRefs:  []string{"/fac/qa/ras-laffan-industrial-city", "/org/int/global-lng-buyer"},
		FlowRefs:  []string{"flow:strait-of-hormuz"},
		Payload: horizonmodels.BuildSupplyChainPathPayload(
			[]horizonmodels.SupplyChainPathNode{
				{Class: ah.FragmentClassSite, Ref: "/fac/qa/ras-laffan-industrial-city"},
				{Class: ah.FragmentClassProduct, Ref: "/product/lng"},
				{Class: ah.FragmentClassOrg, Ref: "/org/int/global-lng-buyer"},
			},
			nil,
			[]horizonmodels.SupplyChainPathLeg{
				{
					Index:             0,
					SupplierRef:       "/fac/qa/ras-laffan-industrial-city",
					CustomerRef:       "/org/int/global-lng-buyer",
					BindingConfidence: 0.82,
				},
			},
			&horizonmodels.SupplyChainPathStress{
				PathScore:          0.74,
				BottleneckLegIndex: 0,
			},
			nil,
		),
	}

	annotateSupplyChainTraceArtifact(artifact, "/fac/qa/ras-laffan-industrial-city")

	if artifact.TraceRole != "direct_anchor" {
		t.Fatalf("TraceRole = %q, want direct_anchor", artifact.TraceRole)
	}
	if artifact.TracePriority <= 0 {
		t.Fatalf("TracePriority = %d, want > 0", artifact.TracePriority)
	}
	if artifact.LegCount != 1 {
		t.Fatalf("LegCount = %d, want 1", artifact.LegCount)
	}
	if artifact.BottleneckLeg != 0 {
		t.Fatalf("BottleneckLeg = %d, want 0", artifact.BottleneckLeg)
	}
	if artifact.PathStressScore < 0.7 {
		t.Fatalf("PathStressScore = %v, want >= 0.7", artifact.PathStressScore)
	}
	if artifact.TraceReason == "" {
		t.Fatal("TraceReason empty")
	}
}

func TestHydrateSupplyChainTraceEndpoints_ResolvesCounterpartNames(t *testing.T) {
	artifact := &horizonmodels.MaterializedArtifact{
		ID:             "chain-2",
		TenantID:       "global",
		Kind:           horizonmodels.MaterializedKindSupplyChainPath,
		AnchorSupplier: "/fac/de/ludwigshafen-pharma-ingredients",
		AnchorCustomer: "/org/de/basf-pharma-distribution",
		Payload: horizonmodels.BuildSupplyChainPathPayload(
			[]horizonmodels.SupplyChainPathNode{
				{Class: ah.FragmentClassSite, Ref: "/fac/de/ludwigshafen-pharma-ingredients"},
				{Class: ah.FragmentClassProduct, Ref: "/product/excipients"},
				{Class: ah.FragmentClassOrg, Ref: "/org/de/basf-pharma-distribution"},
			},
			nil,
			[]horizonmodels.SupplyChainPathLeg{
				{
					Index:       0,
					SupplierRef: "/fac/de/ludwigshafen-pharma-ingredients",
					CustomerRef: "/org/de/basf-pharma-distribution",
					ProductRef:  "/product/excipients",
				},
			},
			nil,
			nil,
		),
	}

	hydrateSupplyChainTraceEndpoints(artifact, "/fac/de/ludwigshafen-pharma-ingredients", map[string]*horizonmodels.HorizonFragment{
		"/fac/de/ludwigshafen-pharma-ingredients": {
			CanonicalPath: "/fac/de/ludwigshafen-pharma-ingredients",
			Class:         FragmentTypeFacility,
			Labels:        map[string]string{"en": "Ludwigshafen Pharma Ingredients"},
		},
		"/org/de/basf-pharma-distribution": {
			CanonicalPath: "/org/de/basf-pharma-distribution",
			Class:         FragmentTypeOrganization,
			Labels:        map[string]string{"en": "BASF Pharma Distribution"},
		},
	})

	if artifact.TraceEndpoints == nil {
		t.Fatal("TraceEndpoints nil")
	}
	if artifact.TraceEndpoints.Downstream == nil || artifact.TraceEndpoints.Downstream.Label != "BASF Pharma Distribution" {
		t.Fatalf("Downstream = %#v, want BASF Pharma Distribution", artifact.TraceEndpoints.Downstream)
	}
	if artifact.TraceEndpoints.Supplier == nil || artifact.TraceEndpoints.Supplier.Label != "Ludwigshafen Pharma Ingredients" {
		t.Fatalf("Supplier = %#v, want Ludwigshafen Pharma Ingredients", artifact.TraceEndpoints.Supplier)
	}
}

func TestBuildFragmentTraceSupplyChainStats_BucketsRoles(t *testing.T) {
	chains := []*horizonmodels.MaterializedArtifact{
		{IsPinned: true, TraceRole: "direct_anchor", PathStressScore: 0.6, BottleneckLeg: 0, BindingScore: 0.8},
		{IsPinned: false, TraceRole: "route_overlap", PathStressScore: 0.4, BottleneckLeg: -1, BindingScore: 0.7},
		{IsPinned: false, TraceRole: "affected_zone_overlap", PathStressScore: 0.2, BottleneckLeg: -1, BindingScore: 0.2},
	}

	stats := buildFragmentTraceSupplyChainStats(chains)

	if stats.PinnedCount != 1 || stats.DiscoveredCount != 2 {
		t.Fatalf("unexpected pinned/discovered counts: %+v", stats)
	}
	if stats.DirectCount != 1 || stats.RouteCount != 1 || stats.AffectedCount != 1 {
		t.Fatalf("unexpected role counts: %+v", stats)
	}
	if stats.HighStressCount != 1 || stats.BottleneckCount != 1 || stats.StrongBindingCnt != 2 {
		t.Fatalf("unexpected stress/binding counts: %+v", stats)
	}
}
