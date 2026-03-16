package repo

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestBuildStrategicRecommendationDedupeKeyStableAcrossOrderingAndWhitespace(t *testing.T) {
	gotA := BuildStrategicRecommendationDedupeKey(
		"tenant-a",
		"/zone/qa/military_conflict/ras-laffan",
		horizonmodels.StrategicRecommendationTypeStrategicGap,
		[]string{" MRI ", "cryogenics", "mri", " Cryogenics "},
	)
	gotB := BuildStrategicRecommendationDedupeKey(
		"tenant-a",
		"/zone/qa/military_conflict/ras-laffan",
		horizonmodels.StrategicRecommendationTypeStrategicGap,
		[]string{"cryogenics", "mri"},
	)

	if gotA != gotB {
		t.Fatalf("expected stable dedupe key, got %q != %q", gotA, gotB)
	}
}

func TestStrategicRecommendationIDStableForSameDedupeKey(t *testing.T) {
	dedupeKey := BuildStrategicRecommendationDedupeKey(
		"tenant-a",
		"/zone/sea/military_conflict/bab-el-mandeb",
		horizonmodels.StrategicRecommendationTypeStrategicGap,
		[]string{"suez", "maritime_latency"},
	)
	gotA := StrategicRecommendationID(dedupeKey)
	gotB := StrategicRecommendationID(dedupeKey)

	if gotA == "" {
		t.Fatalf("expected non-empty recommendation id")
	}
	if gotA != gotB {
		t.Fatalf("expected stable recommendation id, got %q != %q", gotA, gotB)
	}
}
