package signallanding

import (
	"context"
	"testing"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

func TestLandEventRequiresPreciseZoneForShockSignals(t *testing.T) {
	svc := NewService(nil, nil, nil, nil, nil, nil)
	event := &coremodels.OntologyFact{
		BaseOntologyFields: coremodels.BaseOntologyFields{
			ID:           "evt-1",
			TenantID:     "global",
			LocationPath: strptr("/loc/qa"),
		},
		Kind:       "event",
		Type:       "infrastructure_disruption",
		Title:      "Major strike disrupts flows in Qatar",
		Summary:    "A major strike disrupted export infrastructure in Qatar.",
		Confidence: 0.9,
	}
	signal := &horizonmodels.SignalDescriptor{
		ID:           "sig-1",
		Axis:         "logistics/capacity",
		Mechanism:    "port_closure",
		Amplitude:    0.8,
		Polarity:     "risk",
		LocationPath: "/loc/qa",
		Confidence:   0.85,
		ObservedAt:   time.Now().UTC(),
		Constraints: horizonmodels.SignalConstraints{
			Impact:      0.8,
			TimeHorizon: horizonmodels.SignalTimeHorizonShortTerm,
			Modality:    horizonmodels.SignalModalityDisruption,
		},
	}

	result, err := svc.LandEvent(context.Background(), event, []*horizonmodels.SignalDescriptor{signal})
	if err != nil {
		t.Fatalf("LandEvent returned error: %v", err)
	}
	if len(result.Claims) != 1 {
		t.Fatalf("expected 1 claim, got %d", len(result.Claims))
	}
	if result.Claims[0].ZoneEligibility != horizonmodels.SignalZoneEligibilityRequired {
		t.Fatalf("expected zone eligibility required, got %q", result.Claims[0].ZoneEligibility)
	}
	if !result.Claims[0].MissingPreciseZone {
		t.Fatalf("expected missing precise zone marker to be set")
	}
	if len(result.Candidates) == 0 {
		t.Fatalf("expected missing anchor candidate for precise zone")
	}
}

func TestLandEventUsesExistingAffectedZoneWithoutMissingCandidate(t *testing.T) {
	svc := NewService(nil, nil, nil, nil, nil, nil)
	event := &coremodels.OntologyFact{
		BaseOntologyFields: coremodels.BaseOntologyFields{
			ID:             "evt-2",
			TenantID:       "global",
			LocationPath:   strptr("/loc/sea"),
			CanonicalPaths: []string{"/zone/sea/infrastructure_disruption/strait-of-hormuz"},
		},
		Kind:       "event",
		Type:       "military_conflict",
		Title:      "Traffic through the Strait of Hormuz is disrupted",
		Summary:    "The Strait of Hormuz disruption affects maritime traffic.",
		Confidence: 0.95,
	}
	signal := &horizonmodels.SignalDescriptor{
		ID:         "sig-2",
		Axis:       "logistics/capacity",
		Mechanism:  "maritime_disruption",
		Amplitude:  0.9,
		Polarity:   "risk",
		SeedPaths:  []string{"/zone/sea/infrastructure_disruption/strait-of-hormuz"},
		SeedNames:  []string{"Strait of Hormuz"},
		Confidence: 0.9,
		ObservedAt: time.Now().UTC(),
		Constraints: horizonmodels.SignalConstraints{
			Impact:      0.9,
			TimeHorizon: horizonmodels.SignalTimeHorizonImmediate,
			Modality:    horizonmodels.SignalModalityDisruption,
		},
	}

	result, err := svc.LandEvent(context.Background(), event, []*horizonmodels.SignalDescriptor{signal})
	if err != nil {
		t.Fatalf("LandEvent returned error: %v", err)
	}
	if len(result.Claims) != 1 {
		t.Fatalf("expected 1 claim, got %d", len(result.Claims))
	}
	if result.Claims[0].MissingPreciseZone {
		t.Fatalf("did not expect missing precise zone when zone ref already exists")
	}
	if len(result.Candidates) != 0 {
		t.Fatalf("expected no missing candidates, got %d", len(result.Candidates))
	}
	zoneHitFound := false
	for _, hit := range result.Hits {
		if hit != nil && hit.CanonicalRef == "/zone/sea/infrastructure_disruption/strait-of-hormuz" {
			zoneHitFound = true
			break
		}
	}
	if !zoneHitFound {
		t.Fatalf("expected landed zone hit for Hormuz")
	}
}

func strptr(v string) *string {
	return &v
}
