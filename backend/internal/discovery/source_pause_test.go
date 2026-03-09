package discovery

import (
	"testing"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/models"
)

func TestShouldAutoPauseSource_UsesDefaultThreshold(t *testing.T) {
	ds := &models.DataSource{}
	state := &models.SourceRuntimeState{ConsecutiveErrors: 10}
	if !shouldAutoPauseSource(ds, state) {
		t.Fatalf("expected default policy to auto pause after 10 consecutive errors")
	}
}

func TestShouldAutoPauseSource_CanBeDisabled(t *testing.T) {
	disabled := 0
	ds := &models.DataSource{
		Details: models.SourceDetails{
			CadencePolicy: &models.SourceCadencePolicy{
				AutoPauseAfterErrors: &disabled,
			},
		},
	}
	state := &models.SourceRuntimeState{ConsecutiveErrors: 100}
	if shouldAutoPauseSource(ds, state) {
		t.Fatalf("expected auto pause to be disabled when threshold is 0")
	}
}

func TestComputeAutoPauseUntil_UsesConfiguredDuration(t *testing.T) {
	durationMinutes := 90
	ds := &models.DataSource{
		Details: models.SourceDetails{
			CadencePolicy: &models.SourceCadencePolicy{
				AutoPauseDurationMinutes: &durationMinutes,
			},
		},
	}
	now := time.Date(2026, time.March, 9, 0, 0, 0, 0, time.UTC)
	until := computeAutoPauseUntil(ds, now)
	if until == nil {
		t.Fatalf("expected auto pause window")
	}
	if got := until.Sub(now); got != 90*time.Minute {
		t.Fatalf("expected 90m auto pause duration, got %v", got)
	}
}
