package lens

import (
	"testing"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestLensInternalKnowledgeTemperatureReady(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	lens := &models.Lens{
		ViewConfig: map[string]interface{}{
			"internalKnowledge": map[string]interface{}{
				"status":                "queued",
				"availableTemperatures": []interface{}{"hot"},
				"hotReadyAt":            now,
			},
		},
	}
	if !lensInternalKnowledgeTemperatureReady(lens, "hot") {
		t.Fatalf("expected hot temperature to be ready")
	}
	if lensInternalKnowledgeTemperatureReady(lens, "warm") {
		t.Fatalf("did not expect warm temperature to be ready")
	}
}

func TestLensDesiredWarmupTemperature(t *testing.T) {
	if got := lensDesiredWarmupTemperature("watch_signal_landing_delta"); got != "hot" {
		t.Fatalf("expected hot for watch delta, got %q", got)
	}
	if got := lensDesiredWarmupTemperature("supplier_refresh"); got != "warm" {
		t.Fatalf("expected warm for supplier-oriented reason, got %q", got)
	}
	if got := lensDesiredWarmupTemperature("manual_backfill"); got != "full" {
		t.Fatalf("expected full for generic reason, got %q", got)
	}
}
