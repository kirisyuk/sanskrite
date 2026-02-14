package discovery

import (
	"strings"
	"testing"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

func TestNormalizeHorizonMonitorIngestMode(t *testing.T) {
	ds := &coremodels.DataSource{}
	if got := normalizeHorizonMonitorIngestMode(ds); got != wmIngestModeDocumentFeed {
		t.Fatalf("expected default mode %s, got %s", wmIngestModeDocumentFeed, got)
	}

	v := wmIngestModeOntologyFact
	ds.Details.HorizonMonitorIngestMode = &v
	if got := normalizeHorizonMonitorIngestMode(ds); got != wmIngestModeOntologyFact {
		t.Fatalf("expected mode %s, got %s", wmIngestModeOntologyFact, got)
	}

	bad := "unknown_mode"
	ds.Details.HorizonMonitorIngestMode = &bad
	if got := normalizeHorizonMonitorIngestMode(ds); got != wmIngestModeDocumentFeed {
		t.Fatalf("expected fallback mode %s, got %s", wmIngestModeDocumentFeed, got)
	}
}

func TestSignalTypeFromFeedType(t *testing.T) {
	cases := map[string]string{
		"gdelt_geo_protest": "risk:protest",
		"opensky_flights":   "risk:military_flight",
		"ais_snapshot":      "risk:ais_disruption",
		"service_status":    "risk:service_outage",
	}
	for in, want := range cases {
		if got := signalTypeFromFeedType(in); got != want {
			t.Fatalf("feedType=%s: want %s got %s", in, want, got)
		}
	}
}

func TestHorizonMonitorBlueprintFromSignal(t *testing.T) {
	bp, ok := horizonMonitorBlueprintFromSignal("risk:maritime_hazard")
	if !ok {
		t.Fatalf("expected blueprint for risk:maritime_hazard")
	}
	if bp.Axis == "" || bp.Mechanism == "" {
		t.Fatalf("blueprint should include axis and mechanism")
	}
}

func TestClassifyHorizonMonitorNewsKeyword(t *testing.T) {
	cls := classifyHorizonMonitorNewsKeyword("Missile attack reported after military escalation")
	if cls.Level != "high" && cls.Level != "critical" {
		t.Fatalf("expected high/critical level, got %s", cls.Level)
	}
	if cls.Category == "general" {
		t.Fatalf("expected specific category, got %s", cls.Category)
	}
}

func TestMergeAndAggregate(t *testing.T) {
	p := NewHorizonMonitorPipeline(nil, nil, nil, nil, nil, nil, nil)
	now := time.Now().UTC()
	countries, cells := p.mergeAndAggregate([]horizonMonitorSignalObservation{
		{
			CountryCode:    "US",
			CountryName:    "United States",
			SignalType:     "risk:service_outage",
			Severity:       "high",
			Timestamp:      now,
			Lat:            37.7,
			Lon:            -122.4,
			HasCoordinates: true,
			SourceID:       "src-1",
		},
		{
			CountryCode:    "US",
			CountryName:    "United States",
			SignalType:     "risk:protest",
			Severity:       "medium",
			Timestamp:      now,
			Lat:            37.8,
			Lon:            -122.3,
			HasCoordinates: true,
			SourceID:       "src-2",
		},
	})

	us, ok := countries["US"]
	if !ok {
		t.Fatalf("expected US country aggregate")
	}
	if us.TotalSignals != 2 {
		t.Fatalf("expected totalSignals=2, got %d", us.TotalSignals)
	}
	if us.DistinctSignalTypes != 2 {
		t.Fatalf("expected distinctSignalTypes=2, got %d", us.DistinctSignalTypes)
	}
	if us.ConvergenceScore <= 0 {
		t.Fatalf("expected positive convergence score")
	}
	if len(cells) == 0 {
		t.Fatalf("expected at least one grid cell aggregate")
	}
}

func TestTrackCanonicalPath(t *testing.T) {
	e := &horizonMonitorFeedEvent{
		SourceID:       "src-opensky",
		ExternalID:     "evt-1",
		Title:          "RCH123",
		CountryCode:    "US",
		Lat:            37.6213,
		Lon:            -122.3790,
		HasCoordinates: true,
	}
	tr := horizonTrackObservation{
		Kind:            horizonmodels.TrackKindAir,
		ObjectType:      "military_aircraft",
		ExternalTrackID: "abc123",
		DisplayName:     "RCH123",
	}

	got := trackCanonicalPath(e, tr)
	if !strings.HasPrefix(got, "/track/air/us/") {
		t.Fatalf("expected /track/air/us prefix, got %q", got)
	}
	if !strings.Contains(got, "rch123-") {
		t.Fatalf("expected slugged display name in canonical path, got %q", got)
	}
}

func TestExtractEventIdentifierCandidatesTrackLike(t *testing.T) {
	e := &horizonMonitorFeedEvent{
		FeedType: "ais_snapshot",
		Raw: map[string]interface{}{
			"mmsi":           "123456789",
			"sourceObjectId": "wm:port:alpha",
		},
	}
	candidates := extractEventIdentifierCandidates(e)
	hasTrackID := false
	hasSourceObjectID := false
	for _, c := range candidates {
		if c.Key == "externalTrackId" && c.Value == "123456789" {
			hasTrackID = true
		}
		if c.Key == "sourceObjectId" && c.Value == "wm:port:alpha" {
			hasSourceObjectID = true
		}
	}
	if !hasTrackID {
		t.Fatalf("expected externalTrackId candidate from mmsi")
	}
	if !hasSourceObjectID {
		t.Fatalf("expected sourceObjectId candidate from sourceObjectId")
	}
}

func TestScoreEventFragmentCandidateRejectsFarDistance(t *testing.T) {
	e := &horizonMonitorFeedEvent{
		FeedType:       "ais_snapshot",
		Lat:            25.0,
		Lon:            55.0,
		HasCoordinates: true,
		SignalType:     "risk:military_vessel",
		Confidence:     0.8,
		ExternalID:     "evt-geo-1",
		SourceID:       "src-ais",
	}
	f := &horizonmodels.HorizonFragment{
		CanonicalPath: "/track/sea/global/test-vessel",
		Type:          horizonmodels.FragmentTypeTrack,
		Track: &horizonmodels.TrackFragmentData{
			ObjectType: "vessel",
			Lat:        -33.8688,
			Lon:        151.2093,
		},
	}
	if _, ok := scoreEventFragmentCandidate(e, f, 1.0); ok {
		t.Fatalf("expected far-away fragment to be rejected")
	}
}
