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
	if d := cadenceToDuration(ds); d != 10*time.Minute {
		t.Fatalf("<15m should be 10m, got %v", d)
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
	if d := cadenceToDuration(ds); d != 30*time.Minute {
		t.Fatalf("horizon_monitor_feed should default to 30m, got %v", d)
	}

	override := 15
	ds.Details.PollIntervalMinutes = &override
	if d := cadenceToDuration(ds); d != 15*time.Minute {
		t.Fatalf("pollIntervalMinutes override should be 15m, got %v", d)
	}
}

// (no-op)
