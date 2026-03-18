package scout

import (
	"testing"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestComputeSupplyLinkRiskRollup_PicksInventoryVulnerability(t *testing.T) {
	frag := &models.HorizonFragment{
		CanonicalPath: "/supply-link/global/test",
		State: map[string]models.MetricValue{
			"lead_time_days":                      {Value: 48, Confidence: 0.8},
			"inventory_cover_days_at_destination": {Value: 6, Confidence: 0.8},
			"share":                               {Value: 0.24, Confidence: 0.7},
			"customs_clearance_days":              {Value: 2, Confidence: 0.7},
			"technical_fit":                       {Value: 0.84, Confidence: 0.7},
			"regulatory_fit":                      {Value: 0.82, Confidence: 0.7},
		},
		Qualifiers: map[string]interface{}{},
	}
	seg := &SupplyLinkSegment{
		SupplierRef:      "/org/kz/supplier-a",
		ProductRef:       "/product/material/lithium-chloride",
		CustomerRef:      "/org/cn/customer-b",
		TransportViaRefs: []string{"/fac/kz/port/aktau"},
	}

	rollup := computeSupplyLinkRiskRollup(frag, seg, nil)
	if rollup == nil {
		t.Fatalf("rollup=nil")
	}
	if got := rollup.PrimaryMechanism.Key; got != "inventory_vulnerability" {
		t.Fatalf("primary mechanism=%q, want inventory_vulnerability", got)
	}
	if rollup.Criticality < 0.55 {
		t.Fatalf("criticality=%.3f, want >= 0.55", rollup.Criticality)
	}
	applySupplyLinkRiskRollup(frag, rollup)
	if got := frag.Qualifiers["primaryRiskMechanism"]; got != "inventory_vulnerability" {
		t.Fatalf("qualifier primaryRiskMechanism=%v", got)
	}
	if got := frag.State["supply_chain_criticality"].Value; got < 0.55 {
		t.Fatalf("supply_chain_criticality=%.3f, want >= 0.55", got)
	}
}

func TestBuildCapabilityProfileRollup_MergesWithExistingProfile(t *testing.T) {
	unit := "metric_tons_per_year"
	now := time.Now().UTC()
	existing := &models.CapabilityProfile{
		CarrierRef: "/org/kz/supplier-a",
		ProductRef: "/product/material/lithium-chloride",
		Metrics: map[string]models.MetricValue{
			"annual_capacity": {
				Value:      1200,
				Unit:       &unit,
				Confidence: 0.92,
				ObservedAt: now.Add(-2 * time.Hour),
			},
		},
		Qualifiers: map[string]interface{}{
			"carrierType": "org",
		},
		LastObservedAt: now.Add(-2 * time.Hour),
		LastRolledUpAt: now.Add(-2 * time.Hour),
	}
	frag := &models.HorizonFragment{
		TenantID: "global",
		State: map[string]models.MetricValue{
			"annual_capacity":             {Value: 900, Unit: &unit, Confidence: 0.61, ObservedAt: now},
			"lead_time_days_baseline":     {Value: 18, Confidence: 0.74, ObservedAt: now},
			"technical_substitutability":  {Value: 0.42, Confidence: 0.7, ObservedAt: now},
			"regulatory_substitutability": {Value: 0.51, Confidence: 0.7, ObservedAt: now},
		},
		Qualifiers: map[string]interface{}{},
	}
	seg := &SupplyLinkSegment{
		SupplierRef:  "/org/kz/supplier-a",
		ProductRef:   "/product/material/lithium-chloride",
		CustomerRef:  "/org/cn/customer-b",
		SegmentClass: "validated",
	}

	profile := buildCapabilityProfileRollup(existing, frag, seg)
	if profile == nil {
		t.Fatalf("profile=nil")
	}
	if got := profile.Metrics["annual_capacity"].Value; got != 1200 {
		t.Fatalf("annual_capacity=%.2f, want existing 1200", got)
	}
	if got := profile.Metrics["lead_time_days_baseline"].Value; got != 18 {
		t.Fatalf("lead_time_days_baseline=%.2f, want 18", got)
	}
	if got := profile.CoverageStatus; got != "partial" {
		t.Fatalf("coverageStatus=%q, want partial", got)
	}
}

func TestBuildCapabilityProfileRollup_BootstrapsBaselineFromLinkMetrics(t *testing.T) {
	now := time.Now().UTC()
	frag := &models.HorizonFragment{
		TenantID: "global",
		State: map[string]models.MetricValue{
			"lead_time_days": {Value: 21, Confidence: 0.71, ObservedAt: now},
		},
	}
	seg := &SupplyLinkSegment{
		SupplierRef: "/org/qa/qatarenergy",
		ProductRef:  "/product/energy/hydrocarbon/liquefaction/lng",
		CustomerRef: "/fac/gb/south-hook",
	}

	profile := buildCapabilityProfileRollup(nil, frag, seg)
	if profile == nil {
		t.Fatalf("profile=nil")
	}
	if got := profile.Metrics["lead_time_days_baseline"].Value; got != 21 {
		t.Fatalf("lead_time_days_baseline=%.2f, want 21", got)
	}
	if got := profile.Qualifiers["bootstrapSource"]; got != "supply_link" {
		t.Fatalf("bootstrapSource=%v, want supply_link", got)
	}
}
