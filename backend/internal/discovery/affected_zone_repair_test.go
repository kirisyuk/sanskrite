package discovery

import (
	"testing"

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
