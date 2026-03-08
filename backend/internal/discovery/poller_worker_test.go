package discovery

import (
	"testing"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/models"
)

func TestCadenceToDuration(t *testing.T) {
	now := time.Now().UTC()
	ds := &models.DataSource{}
	// default
	d := cadenceToDuration(ds)
	if d != 30*time.Minute {
		t.Fatalf("expected default 30m, got %v", d)
	}
	ago := func(dur time.Duration) *time.Time { t := now.Add(-dur); return &t }

	ds.LastFoundAt = ago(10 * time.Minute)
	if d := cadenceToDuration(ds); d != 30*time.Minute {
		t.Fatalf("default policy should not poll more often than 30m without explicit source policy, got %v", d)
	}
	ds.LastFoundAt = ago(1 * time.Hour)
	if d := cadenceToDuration(ds); d != 30*time.Minute {
		t.Fatalf("<2h should be 30m, got %v", d)
	}
	ds.LastFoundAt = ago(3 * time.Hour)
	if d := cadenceToDuration(ds); d != 2*time.Hour {
		t.Fatalf("<24h should be 2h, got %v", d)
	}
	ds.LastFoundAt = ago(30 * time.Hour)
	if d := cadenceToDuration(ds); d != 6*time.Hour {
		t.Fatalf(">=24h should be 6h, got %v", d)
	}

	ds.Kind = models.SourceKindHorizonFeed
	ds.LastFoundAt = ago(72 * time.Hour)
	if d := cadenceToDuration(ds); d != 6*time.Hour {
		t.Fatalf("stale horizon_monitor_feed should back off to 6h, got %v", d)
	}

	override := 60
	ds.Details.PollIntervalMinutes = &override
	ds.Kind = models.SourceKindRSS
	ds.LastFoundAt = ago(10 * time.Minute)
	if d := cadenceToDuration(ds); d != 60*time.Minute {
		t.Fatalf("pollIntervalMinutes should act as a 60m floor, got %v", d)
	}
	ds.LastFoundAt = ago(48 * time.Hour)
	if d := cadenceToDuration(ds); d != 6*time.Hour {
		t.Fatalf("adaptive cadence should still back off beyond the floor, got %v", d)
	}
}

// (no-op)
