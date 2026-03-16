package discovery

import (
	"strings"
	"testing"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestHasAffectedZoneIdentityDrift_DetectsCorruptedLegacyZone(t *testing.T) {
	cn := "CN"
	f := &horizonmodels.HorizonFragment{
		CanonicalPath: "/zone/cn/military_conflict/china",
		Type:          horizonmodels.FragmentTypeAffectedZone,
		Identifiers: map[string]string{
			"legalName":    "strait of hormuz (kinetic operations)",
			"merged_paths": "/zone/cn/military_conflict/strait-of-hormuz",
		},
		HomeLocation: &horizonmodels.Location{
			Name:      "strait of hormuz (kinetic operations)",
			ISO3166_1: &cn,
		},
		Qualifiers: map[string]interface{}{
			"zoneInferenceScore": "0",
			"geoPrecision":       "approximate_country",
			"sourceCountryCode":  "CN",
			"sourceTitle":        "'What is the game plan?': The Iran war is unsettling China and its ambitions",
			"sourceSummary":      "China is not feeling the shock of war in the Middle East - yet. But it is feeling the ripples.",
		},
	}
	if !hasAffectedZoneIdentityDrift(f) {
		t.Fatal("expected identity drift to be detected")
	}
}

func TestHasAffectedZoneIdentityDrift_RejectsSyntheticApproximatePlaceholder(t *testing.T) {
	f := &horizonmodels.HorizonFragment{
		CanonicalPath: "/zone/za/humanitarian_crisis/south-africa",
		Type:          horizonmodels.FragmentTypeAffectedZone,
		Qualifiers: map[string]interface{}{
			"zoneInferenceScore": "0",
			"geoPrecision":       "approximate_country",
			"sourceCountryCode":  "ZA",
			"sourceTitle":        "South Africa (mass displacement) — mass displacement",
		},
	}
	if !hasAffectedZoneIdentityDrift(f) {
		t.Fatal("expected synthetic placeholder zone to be treated as identity drift")
	}
}

func TestHasAffectedZoneIdentityDrift_DoesNotIgnoreMergedPathMismatchAtMediumScore(t *testing.T) {
	cy := "CY"
	f := &horizonmodels.HorizonFragment{
		CanonicalPath: "/zone/cy/military_conflict/northern-cyprus",
		Type:          horizonmodels.FragmentTypeAffectedZone,
		Identifiers: map[string]string{
			"legalName":    "North Sea (kinetic operations)",
			"merged_paths": "/zone/fr/military_conflict/north-sea,/zone/fr/military_conflict/france",
		},
		HomeLocation: &horizonmodels.Location{
			Name:      "North Sea (kinetic operations)",
			ISO3166_1: &cy,
		},
		Qualifiers: map[string]interface{}{
			"zoneInferenceScore": "4.606",
			"geoPrecision":       "precise",
			"sourceCountryCode":  "FR",
			"sourceTitle":        "Turkey sends six F-16 fighters to Northern Cyprus amid Iran missile threats",
		},
	}
	if !hasAffectedZoneIdentityDrift(f) {
		t.Fatal("expected merged-path and source-country mismatch to be treated as identity drift")
	}
}

func TestIsSyntheticAffectedZoneNarrative_RecognizesCyberOutagePlaceholder(t *testing.T) {
	if !isSyntheticAffectedZoneNarrative("United States (cyber outage) — cyber outage") {
		t.Fatal("expected cyber outage placeholder to be treated as synthetic affected zone narrative")
	}
}

func TestRefreshAffectedZoneStateForRepair_ReplaysNewerStoredSource(t *testing.T) {
	lastMatch := time.Date(2026, 3, 14, 9, 22, 37, 0, time.UTC)
	now := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	st := affectedZoneState{
		Score:         45.0,
		Hits1h:        8.0,
		Hits24h:       20.0,
		TotalMentions: 7,
		LastMatchedAt: lastMatch,
		LastUpdatedAt: now,
	}
	event := &horizonMonitorFeedEvent{
		Title:      "UAE's Fujairah stops some oil loading operations after drone attack - Reuters",
		Summary:    "Fujairah operations impacted after drone attack.",
		OccurredAt: time.Date(2026, 3, 15, 6, 43, 0, 0, time.UTC),
		Severity:   "high",
	}
	cls := horizonMonitorNewsClassification{
		Category: "infrastructure",
		Level:    "high",
	}
	candidate := &affectedZoneCandidate{
		ConditionType:        "infrastructure_disruption",
		GeoPrecision:         "precise",
		EventConfidence:      0.86,
		GeoConfidence:        1.0,
		ResolutionConfidence: 0.9188,
		GeoResolutionMethod:  "precise",
		Label:                "Fujairah (systems disruption)",
	}

	refreshed, changed := refreshAffectedZoneStateForRepair(st, now, event, cls, candidate)
	if !changed {
		t.Fatal("expected newer stored source to refresh zone state")
	}
	if !refreshed.LastMatchedAt.Equal(event.OccurredAt.UTC()) {
		t.Fatalf("expected lastMatchedAt %s, got %s", event.OccurredAt.UTC(), refreshed.LastMatchedAt)
	}
	if refreshed.Score <= st.Score {
		t.Fatalf("expected score to increase, before=%.2f after=%.2f", st.Score, refreshed.Score)
	}
	if activityStatus(refreshed.Score) != "hot" && activityStatus(refreshed.Score) != "active" {
		t.Fatalf("expected refreshed zone to become visible-hot or active, got %.2f (%s)", refreshed.Score, activityStatus(refreshed.Score))
	}
}

func TestApplyAffectedZoneIdentity_OverridesLegacyCountryPlaceholder(t *testing.T) {
	cc := "AE"
	fragment := &horizonmodels.HorizonFragment{
		CanonicalPath: "/zone/ae/infrastructure_disruption/fujairah",
		Identifiers: map[string]string{
			"legalName":    "United Arab Emirates (systems disruption)",
			"shortName":    "United Arab Emirates (systems disruption)",
			"merged_paths": "/zone/ae/infrastructure_disruption/united-arab-emirates",
		},
		HomeLocation: &horizonmodels.Location{
			Name:      "United Arab Emirates (systems disruption)",
			ISO3166_1: &cc,
		},
	}
	candidate := &affectedZoneCandidate{
		Label:       "Fujairah (systems disruption)",
		CountryCode: "AE",
	}

	changed := applyAffectedZoneIdentity(fragment, candidate)
	if !changed {
		t.Fatal("expected identity fields to refresh")
	}
	if got := fragment.Identifiers["legalName"]; got != candidate.Label {
		t.Fatalf("expected legalName %q, got %q", candidate.Label, got)
	}
	if got := fragment.HomeLocation.Name; got != candidate.Label {
		t.Fatalf("expected home location %q, got %q", candidate.Label, got)
	}
	if got := fragment.Identifiers["merged_paths"]; !strings.Contains(got, fragment.CanonicalPath) {
		t.Fatalf("expected merged_paths to include %q, got %q", fragment.CanonicalPath, got)
	}
}

func TestHasAffectedZoneIdentityDrift_AllowsPreciseConcreteRetargetWithLegacyMergedPaths(t *testing.T) {
	f := &horizonmodels.HorizonFragment{
		CanonicalPath: "/zone/ae/infrastructure_disruption/fujairah",
		Type:          horizonmodels.FragmentTypeAffectedZone,
		Identifiers: map[string]string{
			"merged_paths": "/zone/ae/infrastructure_disruption/united-arab-emirates",
		},
		Qualifiers: map[string]interface{}{
			"geoPrecision":           "precise",
			"zoneInferenceScore":     "1.22",
			"sourceCountryCode":      "AE",
			"sourceTitle":            "UAE's Fujairah stops some oil loading operations after drone attack - Reuters",
			"sourceSummary":          "Fujairah operations impacted after drone attack.",
			"classificationCategory": "infrastructure",
			"classificationLevel":    "high",
		},
	}
	if hasAffectedZoneIdentityDrift(f) {
		t.Fatal("expected precise concrete retarget to survive legacy merged_paths residue")
	}
}
