package scout

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

func TestNormalizeAffectedZoneIdentitySlug_StripsGenericSuffixes(t *testing.T) {
	tests := map[string]string{
		"strait-of-hormuz-transit-corridor": "strait-of-hormuz",
		"preah-vihear-border-sector":        "preah-vihear-border",
		"kharg-island-vicinity":             "kharg-island",
		"southern-red-sea":                  "southern-red-sea",
	}
	for in, want := range tests {
		if got := NormalizeAffectedZoneIdentitySlug(in); got != want {
			t.Fatalf("NormalizeAffectedZoneIdentitySlug(%q)=%q want=%q", in, got, want)
		}
	}
}

func TestSelectAffectedZoneTwinReplacement_PrefersRicherSibling(t *testing.T) {
	incoming := &models.HorizonFragment{
		ID:            "dup",
		Class:         ah.FragmentClassGeo,
		Subtype:       "affected_zone",
		CanonicalPath: "/zone/sea/military_conflict/strait-of-hormuz-transit-corridor",
		Metadata:      models.FragmentMetadata{EpistemicStatus: models.EpistemicStatusLowConfidence},
	}
	rich := &models.HorizonFragment{
		ID:            "rich",
		Class:         ah.FragmentClassGeo,
		Subtype:       "affected_zone",
		CanonicalPath: "/zone/sea/military_conflict/strait-of-hormuz",
		State: map[string]models.MetricValue{
			models.MetricPathImportanceStrategic:           {Value: 0.53},
			models.MetricPathImportanceBridge:              {Value: 0.66},
			models.MetricPathImportanceBusinessBlastRadius: {Value: 0.50},
			models.MetricPathImportanceConflictBlastRadius: {Value: 0.70},
		},
	}
	got, reason := SelectAffectedZoneTwinReplacement(incoming, []*models.HorizonFragment{rich})
	if got == nil || got.ID != rich.ID {
		t.Fatalf("expected richer sibling replacement, got %#v", got)
	}
	if reason == "" {
		t.Fatal("expected non-empty replacement reason")
	}
}

func TestSelectAffectedZoneTwinReplacement_DoesNotMergeCrossFamily(t *testing.T) {
	incoming := &models.HorizonFragment{
		ID:            "dup",
		Class:         ah.FragmentClassGeo,
		Subtype:       "affected_zone",
		CanonicalPath: "/zone/central-asia/earthquake_affected/fergana-valley",
		Metadata:      models.FragmentMetadata{EpistemicStatus: models.EpistemicStatusLowConfidence},
	}
	rich := &models.HorizonFragment{
		ID:            "rich",
		Class:         ah.FragmentClassGeo,
		Subtype:       "affected_zone",
		CanonicalPath: "/zone/central-asia/political_tension/fergana-valley",
		State: map[string]models.MetricValue{
			models.MetricPathImportanceStrategic:           {Value: 0.53},
			models.MetricPathImportanceBusinessBlastRadius: {Value: 0.78},
			models.MetricPathImportanceConflictBlastRadius: {Value: 0.74},
		},
	}
	got, _ := SelectAffectedZoneTwinReplacement(incoming, []*models.HorizonFragment{rich})
	if got != nil {
		t.Fatalf("expected no replacement across condition families, got %s", got.CanonicalPath)
	}
}

func TestShouldAutoMergeAffectedZoneTwin_RequiresSkeletalDuplicate(t *testing.T) {
	primary := &models.HorizonFragment{
		ID:            "primary",
		Class:         ah.FragmentClassGeo,
		Subtype:       "affected_zone",
		CanonicalPath: "/zone/sea/political_tension/southern-red-sea",
		State: map[string]models.MetricValue{
			models.MetricPathImportanceStrategic:           {Value: 0.71},
			models.MetricPathImportanceBridge:              {Value: 0.79},
			models.MetricPathImportanceBusinessBlastRadius: {Value: 0.61},
			models.MetricPathImportanceConflictBlastRadius: {Value: 0.89},
		},
	}
	duplicate := &models.HorizonFragment{
		ID:            "dup",
		Class:         ah.FragmentClassGeo,
		Subtype:       "affected_zone",
		CanonicalPath: "/zone/sea/military_conflict/southern-red-sea",
		State: map[string]models.MetricValue{
			models.MetricPathImportanceStrategic: {Value: 0.17},
			models.MetricPathImportanceBridge:    {Value: 0.31},
		},
		Metadata: models.FragmentMetadata{EpistemicStatus: models.EpistemicStatusLowConfidence},
	}
	ok, _ := ShouldAutoMergeAffectedZoneTwin(primary, duplicate)
	if !ok {
		t.Fatal("expected skeletal duplicate to be auto-mergeable")
	}
}
