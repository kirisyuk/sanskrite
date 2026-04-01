package focusobject

import (
	"testing"
	"time"
)

func TestAnalyzeFixtureBuildsWatchCompatibleSnapshot(t *testing.T) {
	fixture, err := LoadFixture("testdata/focus_battery_graph.json")
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	snapshot, err := NewAnalyzer().Analyze(AnalyzeRequest{
		Focus:     fixture.Focus,
		Fragments: fixture.Fragments,
		Edges:     fixture.Edges,
		Artifacts: fixture.Artifacts,
		Facts:     fixture.Facts,
		Signals:   fixture.Signals,
		Now:       time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if snapshot.Focus.AnchorCanonicalPath != "/org/us/acme-batteries" {
		t.Fatalf("unexpected anchor: %q", snapshot.Focus.AnchorCanonicalPath)
	}
	if len(snapshot.Focus.UniversePaths) < 4 {
		t.Fatalf("expected universe paths, got %v", snapshot.Focus.UniversePaths)
	}
	if len(snapshot.PathExposures) == 0 {
		t.Fatal("expected matched path exposures")
	}
	if len(snapshot.Threats) == 0 {
		t.Fatal("expected top threats")
	}
	if len(snapshot.TodayMentions) == 0 {
		t.Fatal("expected today mentions")
	}
	if len(snapshot.Propagation) == 0 {
		t.Fatal("expected propagation audit")
	}
	if len(snapshot.EndpointHits) == 0 {
		t.Fatal("expected endpoint hits")
	}
	if len(snapshot.EndpointPackets) == 0 {
		t.Fatal("expected endpoint interpretation packets")
	}
}

func TestAnalyzeBuildsEndpointHitsAndPacketContext(t *testing.T) {
	fixture, err := LoadFixture("testdata/focus_battery_graph.json")
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	snapshot, err := NewAnalyzer().Analyze(AnalyzeRequest{
		Focus:     fixture.Focus,
		Fragments: fixture.Fragments,
		Edges:     fixture.Edges,
		Artifacts: fixture.Artifacts,
		Facts:     fixture.Facts,
		Signals:   fixture.Signals,
		Now:       time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	foundFactory := false
	foundPacket := false
	for _, item := range snapshot.EndpointHits {
		if item.CanonicalPath == "/fac/us/acme-nevada-gigafactory" {
			foundFactory = true
			if item.PeakScore <= 0 {
				t.Fatalf("expected positive factory impact score, got %#v", item)
			}
		}
	}
	for _, item := range snapshot.EndpointPackets {
		if item.CanonicalPath == "/org/de/euro-mobility" && len(item.Context) > 0 {
			foundPacket = true
		}
	}
	if !foundFactory {
		t.Fatalf("expected factory endpoint hit in snapshot: %#v", snapshot.EndpointHits)
	}
	if !foundPacket {
		t.Fatalf("expected endpoint packet context in snapshot: %#v", snapshot.EndpointPackets)
	}
	foundSignalMatch := false
	for _, item := range snapshot.PathExposures {
		if item.ArtifactCanonicalKey == "/artifact/supply-chain/global/acme-lfp-europe" && len(item.MatchedSignals) > 0 {
			foundSignalMatch = true
		}
	}
	if !foundSignalMatch {
		t.Fatalf("expected path exposure to retain matched signals: %#v", snapshot.PathExposures)
	}
}

func TestAnalyzeBuildsPropagationAuditForLongerCascade(t *testing.T) {
	fixture, err := LoadFixture("testdata/focus_battery_graph.json")
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	snapshot, err := NewAnalyzer().Analyze(AnalyzeRequest{
		Focus:     fixture.Focus,
		Fragments: fixture.Fragments,
		Edges:     fixture.Edges,
		Artifacts: fixture.Artifacts,
		Facts:     fixture.Facts,
		Signals:   fixture.Signals,
		Now:       time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	foundAudit := false
	foundReached := false
	for _, audit := range snapshot.Propagation {
		if audit.SignalID != "signal-red-sea" {
			continue
		}
		foundAudit = true
		for _, item := range audit.Reached {
			if item.CanonicalPath == "/org/fr/citybus-fleet" {
				foundReached = true
				if item.Hops < 3 {
					t.Fatalf("expected long-shoulder propagation depth, got %#v", item)
				}
			}
		}
	}
	if !foundAudit {
		t.Fatalf("expected red sea propagation audit: %#v", snapshot.Propagation)
	}
	if !foundReached {
		t.Fatalf("expected long-shoulder target in propagation audit: %#v", snapshot.Propagation)
	}
}

func TestAnalyzeReachesRemoteEndpointForHormuzFixture(t *testing.T) {
	fixture, err := LoadFixture("testdata/focus_equinor_hormuz_graph.json")
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	snapshot, err := NewAnalyzer().Analyze(AnalyzeRequest{
		Focus:     fixture.Focus,
		Fragments: fixture.Fragments,
		Edges:     fixture.Edges,
		Artifacts: fixture.Artifacts,
		Facts:     fixture.Facts,
		Signals:   fixture.Signals,
		Now:       time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	foundEndpoint := false
	for _, item := range snapshot.EndpointHits {
		if item.CanonicalPath == "/org/no/equinor" {
			foundEndpoint = true
			if item.PeakScore <= 0 {
				t.Fatalf("expected positive remote endpoint peak score, got %#v", item)
			}
		}
	}
	if !foundEndpoint {
		t.Fatalf("expected Equinor endpoint hit in snapshot: %#v", snapshot.EndpointHits)
	}
}

func TestAnalyzeDemandOpportunityPropagatesUpstreamLongShoulder(t *testing.T) {
	fixture, err := LoadFixture("testdata/focus_ev_demand_graph.json")
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	snapshot, err := NewAnalyzer().Analyze(AnalyzeRequest{
		Focus:     fixture.Focus,
		Fragments: fixture.Fragments,
		Edges:     fixture.Edges,
		Artifacts: fixture.Artifacts,
		Facts:     fixture.Facts,
		Signals:   fixture.Signals,
		Now:       time.Date(2026, 3, 31, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if snapshot.Position.OpportunityPressure <= 0.30 {
		t.Fatalf("expected upstream demand opportunity pressure, got %#v", snapshot.Position)
	}
	foundEndpoint := false
	for _, item := range snapshot.EndpointHits {
		if item.CanonicalPath == "/org/cl/andes-lithium" {
			foundEndpoint = true
			if item.MaxHops < 4 {
				t.Fatalf("expected long upstream shoulder to beneficiary, got %#v", item)
			}
		}
	}
	if !foundEndpoint {
		t.Fatalf("expected Andes Lithium endpoint hit in snapshot: %#v", snapshot.EndpointHits)
	}
}

func TestAnalyzeLegalAccessShockTransformsIntoSupplyConsequence(t *testing.T) {
	fixture, err := LoadFixture("testdata/focus_legal_access_graph.json")
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	snapshot, err := NewAnalyzer().Analyze(AnalyzeRequest{
		Focus:     fixture.Focus,
		Fragments: fixture.Fragments,
		Edges:     fixture.Edges,
		Artifacts: fixture.Artifacts,
		Facts:     fixture.Facts,
		Signals:   fixture.Signals,
		Now:       time.Date(2026, 3, 31, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	foundCustomer := false
	foundAltSupplierContext := false
	foundTransform := false
	for _, item := range snapshot.EndpointHits {
		switch item.CanonicalPath {
		case "/org/de/euro-mobility":
			foundCustomer = true
		}
	}
	for _, item := range snapshot.EndpointPackets {
		if item.CanonicalPath != "/org/de/euro-mobility" {
			continue
		}
		for _, ctx := range item.Context {
			if ctx.CanonicalPath == "/org/pl/baltic-cathodes" && ctx.Relationship == "alternate_supplier_candidate" {
				foundAltSupplierContext = true
				break
			}
		}
	}
	for _, audit := range snapshot.Propagation {
		if audit.SignalID != "signal-eu-passport-enforcement" {
			continue
		}
		for _, step := range audit.Steps {
			if step.ToPath == "/org/de/euro-mobility" && step.AxisOut == "physical_flow/supply" && step.TransformReason == "legal_access_to_supply_shortfall" {
				foundTransform = true
				break
			}
		}
	}
	if !foundCustomer {
		t.Fatalf("expected Euro Mobility downstream endpoint hit: %#v", snapshot.EndpointHits)
	}
	if !foundAltSupplierContext {
		t.Fatalf("expected Baltic Cathodes alternate supplier context: %#v", snapshot.EndpointPackets)
	}
	if !foundTransform {
		t.Fatalf("expected legal-access to supply-shortfall transform in propagation audit: %#v", snapshot.Propagation)
	}
}
