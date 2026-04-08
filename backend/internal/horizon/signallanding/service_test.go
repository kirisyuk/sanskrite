package signallanding

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/ontology"
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

func TestDeriveAffectedZoneIdentityFromLocation(t *testing.T) {
	frag := &horizonmodels.HorizonFragment{
		CanonicalPath: "/loc/ae/fujairah/fujairah",
		Labels:        map[string]string{"en": "Fujairah"},
	}
	geo, slug, label, precision := deriveAffectedZoneIdentity(frag)
	if geo != "ae" || slug != "fujairah" || label != "Fujairah" || precision != "city" {
		t.Fatalf("unexpected identity geo=%q slug=%q label=%q precision=%q", geo, slug, label, precision)
	}
}

func TestDeriveAffectedZoneIdentityFromFacility(t *testing.T) {
	frag := &horizonmodels.HorizonFragment{
		CanonicalPath: "/fac/ae/port-of-fujairah",
		Labels:        map[string]string{"en": "Port of Fujairah"},
	}
	geo, slug, _, precision := deriveAffectedZoneIdentity(frag)
	if geo != "ae" || slug != "fujairah" || precision != "facility" {
		t.Fatalf("unexpected facility identity geo=%q slug=%q precision=%q", geo, slug, precision)
	}
}

func TestInferSignalLandingConditionTypePrefersMilitaryConflict(t *testing.T) {
	claim := &horizonmodels.SignalImpactClaim{
		Axis:            "conflict/escalation",
		Mechanism:       "missile_strike",
		ClaimLabels:     []string{"Drone attack near Fujairah"},
		SourceTextSpans: []string{"Missile and drone attack around port infrastructure."},
	}
	if got := inferSignalLandingConditionType(claim); got != "military_conflict" {
		t.Fatalf("expected military_conflict, got %q", got)
	}
}

func TestSessionIsRetroAttachActiveUsesWindow(t *testing.T) {
	now := time.Now().UTC()
	session := &horizonmodels.SignalLandingSession{
		ID:          "s1",
		TenantID:    "global",
		Status:      "active",
		CreatedAt:   now.Add(-2 * time.Hour),
		UpdatedAt:   now.Add(-30 * time.Minute),
		ActiveUntil: now.Add(4 * time.Hour),
	}
	if !sessionIsRetroAttachActive(session, now) {
		t.Fatalf("expected session to remain active")
	}
	session.ActiveUntil = now.Add(-1 * time.Minute)
	if sessionIsRetroAttachActive(session, now) {
		t.Fatalf("expected expired session to be inactive")
	}
}

func TestRetroAttachScoreMatchesMissingCandidateAndCountry(t *testing.T) {
	svc := &Service{}
	ua := "UA"
	fragment := &horizonmodels.HorizonFragment{
		Type:          horizonmodels.FragmentTypeFacility,
		CanonicalPath: "/fac/ua/port-of-odessa",
		Labels:        map[string]string{"en": "Port of Odesa"},
		HomeLocation:  &horizonmodels.Location{Name: "Odesa", ISO3166_1: &ua},
	}
	claim := &horizonmodels.SignalImpactClaim{
		CountryHints:  []string{"/loc/ua"},
		FacilityHints: []string{"Odesa port", "Black Sea port"},
		ClaimLabels:   []string{"Drone attack around Odesa port"},
	}
	candidate := &horizonmodels.SignalMissingAnchorCandidate{
		ID:          "cand1",
		Label:       "Port of Odesa",
		Status:      "open",
		HintCountry: "/loc/ua",
	}
	score, reasons, resolved := svc.retroAttachScore(fragment, claim, []*horizonmodels.SignalMissingAnchorCandidate{candidate}, nil)
	if score < 0.56 {
		t.Fatalf("expected retro attach score to pass threshold, got %.3f", score)
	}
	if len(resolved) != 1 || resolved[0].ID != "cand1" {
		t.Fatalf("expected missing candidate to resolve, got %+v", resolved)
	}
	if len(reasons) == 0 {
		t.Fatalf("expected non-empty attach reasons")
	}
}

func TestBackfillAffectedZoneCoordinatesUsesGeocoding(t *testing.T) {
	svc := (&Service{}).WithGeocodingClient(mockGeocoder{
		byQuery: map[string]ontology.GeocodingResult{
			"Strait of Hormuz": {Lat: 26.5667, Lon: 56.2500},
		},
	})
	zone := &horizonmodels.HorizonFragment{
		Type:          horizonmodels.FragmentTypeAffectedZone,
		CanonicalPath: "/zone/int/military_conflict/strait-of-hormuz",
		Labels:        map[string]string{"en": "Strait of Hormuz (armed tension)"},
		Qualifiers: map[string]interface{}{
			"geoPrecision": "region",
		},
	}
	changed, err := svc.BackfillAffectedZoneCoordinates(context.Background(), zone)
	if err != nil {
		t.Fatalf("BackfillAffectedZoneCoordinates returned error: %v", err)
	}
	if !changed {
		t.Fatalf("expected coordinates to be hydrated")
	}
	if zone.Coordinates == nil || !horizonmodels.IsRealGeoCoordinate(zone.Coordinates.Lat, zone.Coordinates.Lon) {
		t.Fatalf("expected valid coordinates, got %+v", zone.Coordinates)
	}
	if zone.HomeLocation == nil || zone.HomeLocation.ISO6709 == nil || *zone.HomeLocation.ISO6709 == "" {
		t.Fatalf("expected ISO6709 to be filled, got %+v", zone.HomeLocation)
	}
}

func TestApplySignalLandingZoneActivityMarksZoneVisible(t *testing.T) {
	now := time.Now().UTC()
	session := &horizonmodels.SignalLandingSession{
		ID:        "sess-1",
		TenantID:  "global",
		UpdatedAt: now,
	}
	claim := &horizonmodels.SignalImpactClaim{
		Severity:   0.82,
		Confidence: 0.88,
		Axis:       "conflict/escalation",
		Mechanism:  "missile_strike",
		UpdatedAt:  now,
	}
	zone := &horizonmodels.HorizonFragment{
		Type:          horizonmodels.FragmentTypeAffectedZone,
		CanonicalPath: "/zone/ir/military_conflict/kharg-island-crude-oil-export-terminal",
		Qualifiers:    map[string]interface{}{},
	}

	applySignalLandingZoneActivity(zone, session, claim, now)

	if zone.State == nil {
		t.Fatalf("expected zone state to be initialized")
	}
	if mv, ok := zone.State[signalLandingZoneMetricHypeScore]; !ok || mv.Value < 26 {
		t.Fatalf("expected visible hype score, got %+v", zone.State[signalLandingZoneMetricHypeScore])
	}
	if got := strings.TrimSpace(fmt.Sprint(zone.Qualifiers["hypeStatus"])); got == "" || got == "inactive" {
		t.Fatalf("expected visible hypeStatus, got %q", got)
	}
	if got := strings.TrimSpace(fmt.Sprint(zone.Qualifiers["lastMatchedAt"])); got == "" {
		t.Fatalf("expected lastMatchedAt qualifier to be written")
	}
}

type mockGeocoder struct {
	byQuery map[string]ontology.GeocodingResult
}

func (m mockGeocoder) Geocode(_ context.Context, address string) (*ontology.GeocodingResult, error) {
	if result, ok := m.byQuery[address]; ok {
		copy := result
		return &copy, nil
	}
	return nil, nil
}
