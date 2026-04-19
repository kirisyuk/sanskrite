package horizon

import (
	"testing"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestFragmentToGeoItem_IncludesAffectedZoneWithCoordinates(t *testing.T) {
	now := time.Date(2026, 3, 15, 8, 3, 0, 0, time.UTC)
	fragment := &models.HorizonFragment{
		ID:             "zone-1",
		CanonicalPath:  "/zone/sea/infrastructure_disruption/strait-of-hormuz",
		Class:         models.ClassSubtypeAffectedZone.Class,
		Subtype:       models.ClassSubtypeAffectedZone.Subtype,
		Labels:         map[string]string{"en": "Strait of Hormuz (critical infra)"},
		Coordinates:    &models.Coordinates{Lat: 26.5667, Lon: 56.2500},
		RadiusKm:       180,
		HasCoordinates: true,
		Qualifiers: map[string]interface{}{
			"zoneType":        "infrastructure_disruption",
			"zoneSubType":     "systems_disruption",
			"geoPrecision":    "precise",
			"sourceLayer":     "source_mechanics",
			"processingLayer": "heuristic_keyword",
			"hypeIndicator":   2,
			"hypeStatus":      "active",
			"lastMatchedAt":   now.Format(time.RFC3339),
		},
		Governance: models.Governance{Status: string(models.FragmentStatusActive)},
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	item, ok := fragmentToGeoItem(fragment, true)
	if !ok {
		t.Fatal("expected geo item")
	}
	if item.CanonicalPath != fragment.CanonicalPath {
		t.Fatalf("unexpected canonical path %q", item.CanonicalPath)
	}
	if item.Coordinates == nil || item.Coordinates.Lat != 26.5667 || item.Coordinates.Lon != 56.2500 {
		t.Fatalf("unexpected coordinates %+v", item.Coordinates)
	}
	if item.GeoPrecision != "precise" || item.ZoneType != "infrastructure_disruption" {
		t.Fatalf("unexpected affected-zone metadata %+v", item)
	}
}

func TestFragmentToGeoItem_ConflictLinkedInfraIsExposedToMap(t *testing.T) {
	now := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	fragment := &models.HorizonFragment{
		ID:             "zone-fujairah",
		CanonicalPath:  "/zone/ae/infrastructure_disruption/fujairah",
		Class:         models.ClassSubtypeAffectedZone.Class,
		Subtype:       models.ClassSubtypeAffectedZone.Subtype,
		Labels:         map[string]string{"en": "Fujairah (systems disruption)"},
		Description:    "Fujairah (systems disruption) - systems disruption",
		Coordinates:    &models.Coordinates{Lat: 25.1288, Lon: 56.3265},
		RadiusKm:       120,
		HasCoordinates: true,
		Qualifiers: map[string]interface{}{
			"conditionType":   "infrastructure_disruption",
			"zoneType":        "infrastructure_disruption",
			"zoneSubType":     "systems_disruption",
			"geoPrecision":    "precise",
			"hypeIndicator":   3,
			"hypeStatus":      "watch",
			"lastMatchedAt":   now.Format(time.RFC3339),
			"sourceTitle":     "UAE's Fujairah stops some oil loading operations after drone attack - Reuters",
			"sourceSummary":   "Fujairah operations impacted after drone attack.",
			"processingLayer": "heuristic",
		},
		Governance: models.Governance{Status: string(models.FragmentStatusActive)},
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	item, ok := fragmentToGeoItem(fragment, true)
	if !ok {
		t.Fatal("expected geo item")
	}
	if !item.ConflictLinked {
		t.Fatalf("expected conflict-linked map item, got %+v", item)
	}
	if item.ThreatClass != "conflict-linked" {
		t.Fatalf("unexpected threat class %q", item.ThreatClass)
	}
}

func TestReadZoneActivitySnapshot_FallsBackToSignalLandingQualifiers(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 30, 0, 0, time.UTC)
	lastMatched := now.Add(-2 * time.Hour)
	fragment := &models.HorizonFragment{
		ID:            "zone-kharg",
		CanonicalPath: "/zone/ir/military_conflict/kharg-island-crude-oil-export-terminal",
		Class:         models.ClassSubtypeAffectedZone.Class,
		Subtype:       models.ClassSubtypeAffectedZone.Subtype,
		Coordinates:   &models.Coordinates{Lat: 29.25, Lon: 50.32},
		RadiusKm:      12,
		Qualifiers: map[string]interface{}{
			"signalLandingGenerated": true,
			"conditionType":          "military_conflict",
			"signalLandingUpdatedAt": lastMatched.Format(time.RFC3339),
		},
		UpdatedAt: lastMatched,
	}

	score, indicator, status, matchedAt := readZoneActivitySnapshot(fragment, now)
	if score <= 0 {
		t.Fatalf("expected positive fallback score, got %.2f", score)
	}
	if indicator <= 0 {
		t.Fatalf("expected positive hype indicator, got %d", indicator)
	}
	if status != "active" && status != "hot" && status != "watch" {
		t.Fatalf("expected visible activity status, got %q", status)
	}
	if matchedAt == nil || !matchedAt.Equal(lastMatched) {
		t.Fatalf("expected fallback lastMatchedAt %s, got %+v", lastMatched.Format(time.RFC3339), matchedAt)
	}
}
