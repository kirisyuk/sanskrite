package taxonomy

import (
	"testing"
)

func TestFacilityKindFromSlug(t *testing.T) {
	tests := []struct {
		slug string
		want FacilityKind
	}{
		{"port-of-houston", FacilityKindSeaport},
		{"seaport-dubai", FacilityKindSeaport},
		{"harbor-rotterdam", FacilityKindSeaport},
		{"jfk-airport", FacilityKindAirport},
		{"pipeline-nord-stream", FacilityKindPipeline},
		{"submarine-cable-flags", FacilityKindCable},
		{"sea-me-we-cable", FacilityKindCable},
		{"lithium-mine", FacilityKindMine},
		{"refinery-ras-tanura", FacilityKindRefinery},
		{"gigafactory-texas", FacilityKindFactory},
		{"tsmc-fab-21", FacilityKindFactory},
		{"lng-terminal-zeebrugge", FacilityKindTerminal},
		{"datacenter-ashburn", FacilityKindDatacenter},
		{"power-plant-belchatow", FacilityKindPowerPlant},
		{"pepsico-cork-concentrate-plant", FacilityKindFactory},
		{"marwin-distribution-center", FacilityKindWarehouse},
		{"munich-airport-cargo-terminal", FacilityKindAirport},
		{"unknown-facility-xyz", FacilityKindOther},
	}
	for _, tt := range tests {
		got := FacilityKindFromSlug(tt.slug)
		if got != tt.want {
			t.Errorf("FacilityKindFromSlug(%q) = %q, want %q", tt.slug, got, tt.want)
		}
	}
}

func TestFacilityKindFromCanonicalPath(t *testing.T) {
	if got := FacilityKindFromCanonicalPath("/fac/us/port-long-beach"); got != FacilityKindSeaport {
		t.Errorf("got %q, want seaport", got)
	}
	if got := FacilityKindFromCanonicalPath("/fac/cn/submarine-cable-apcn"); got != FacilityKindCable {
		t.Errorf("got %q, want cable", got)
	}
}

func TestNormalizeFacilityKind(t *testing.T) {
	if got := NormalizeFacilityKind("  Seaport  "); got != FacilityKindSeaport {
		t.Errorf("got %q, want seaport", got)
	}
	if got := NormalizeFacilityKind("submarine-cable"); got != FacilityKindCable {
		t.Errorf("got %q, want cable", got)
	}
}

func TestInferFacilityKind(t *testing.T) {
	got := InferFacilityKind(
		"/fac/ie/pepsico-cork-concentrate-plant",
		"PepsiCo Cork Plant",
		"Concentrate manufacturing facility",
		"",
		"",
	)
	if got != FacilityKindFactory {
		t.Fatalf("InferFacilityKind(...) = %q, want %q", got, FacilityKindFactory)
	}

	got = InferFacilityKind(
		"/fac/kz/marwin-distribution-center",
		"Marwin Distribution Center",
		"Logistics hub",
		"",
		"",
	)
	if got != FacilityKindWarehouse {
		t.Fatalf("InferFacilityKind(...) = %q, want %q", got, FacilityKindWarehouse)
	}

	got = InferFacilityKind("/fac/na/northvolt-dwa", "Northvolt Dwa")
	if got != FacilityKindOther {
		t.Fatalf("InferFacilityKind(...) = %q, want %q", got, FacilityKindOther)
	}
}
