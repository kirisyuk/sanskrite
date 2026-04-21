package signals

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

func TestLocationSeedCapForSignal(t *testing.T) {
	if got := locationSeedCapForSignal(&horizonmodels.SignalDescriptor{Axis: horizonmodels.AxisGeoConflict}); got != 18 {
		t.Fatalf("geo/conflict cap: got %d want 18", got)
	}
	if got := locationSeedCapForSignal(&horizonmodels.SignalDescriptor{Axis: horizonmodels.AxisLogisticsCapacity}); got != 16 {
		t.Fatalf("logistics/capacity cap: got %d want 16", got)
	}
	if got := locationSeedCapForSignal(&horizonmodels.SignalDescriptor{Axis: horizonmodels.AxisOperationalContinuity}); got != 20 {
		t.Fatalf("operational/continuity cap: got %d want 20", got)
	}
}

func TestLocationFallbackPriorityForAxisConflictPrefersBusinessSeedsOverGov(t *testing.T) {
	signal := &horizonmodels.SignalDescriptor{
		Axis:         horizonmodels.AxisGeoConflict,
		LocationPath: "/loc/ir",
	}
	loc := &horizonmodels.HorizonFragment{CanonicalPath: "/loc/ir", Class: FragmentTypeGeoRegion}
	fac := &horizonmodels.HorizonFragment{CanonicalPath: "/fac/ir/bandar-abbas-port", Class: FragmentTypeFacility}
	market := &horizonmodels.HorizonFragment{CanonicalPath: "/market/ir/transportation/port-services", Class: FragmentTypeMarket}
	gov := &horizonmodels.HorizonFragment{CanonicalPath: "/gov/ir/ministry-of-defense", Class: FragmentTypeOrganization}

	locScore := locationFallbackPriorityForAxis(signal, loc)
	facScore := locationFallbackPriorityForAxis(signal, fac)
	marketScore := locationFallbackPriorityForAxis(signal, market)
	govScore := locationFallbackPriorityForAxis(signal, gov)

	if locScore <= govScore {
		t.Fatalf("expected loc score > gov score, got %.2f <= %.2f", locScore, govScore)
	}
	if facScore <= govScore {
		t.Fatalf("expected facility score > gov score, got %.2f <= %.2f", facScore, govScore)
	}
	if marketScore >= facScore {
		t.Fatalf("expected generic conflict market score < facility score, got %.2f >= %.2f", marketScore, facScore)
	}
	if marketScore >= locScore {
		t.Fatalf("expected generic conflict market score < location score, got %.2f >= %.2f", marketScore, locScore)
	}
}

func TestLocationFallbackPriorityForAxisConflictBoostsEnergyMarketsWithTheme(t *testing.T) {
	signal := &horizonmodels.SignalDescriptor{
		Axis:         horizonmodels.AxisGeoConflict,
		LocationPath: "/loc/ir",
		SeedNames:    []string{"Strait of Hormuz", "Iran energy", "Iran port"},
	}
	fac := &horizonmodels.HorizonFragment{CanonicalPath: "/fac/ir/bandar-abbas-port", Class: FragmentTypeFacility}
	market := &horizonmodels.HorizonFragment{CanonicalPath: "/market/ir/energy/natural-gas", Class: FragmentTypeMarket}
	gov := &horizonmodels.HorizonFragment{CanonicalPath: "/gov/ir/ministry-of-defense", Class: FragmentTypeOrganization}
	deposit := &horizonmodels.HorizonFragment{CanonicalPath: "/deposit/ir/south-pars", Class: ah.FragmentClassSite, Subtype: "deposit"}

	marketScore := locationFallbackPriorityForAxis(signal, market)
	facScore := locationFallbackPriorityForAxis(signal, fac)
	govScore := locationFallbackPriorityForAxis(signal, gov)
	depositScore := locationFallbackPriorityForAxis(signal, deposit)

	if marketScore <= govScore {
		t.Fatalf("expected themed market score > gov score, got %.2f <= %.2f", marketScore, govScore)
	}
	if depositScore <= govScore {
		t.Fatalf("expected themed deposit score > gov score, got %.2f <= %.2f", depositScore, govScore)
	}
	if facScore <= marketScore {
		t.Fatalf("expected facility to remain competitive under themed conflict, got %.2f <= %.2f", facScore, marketScore)
	}
}

func TestMergeSeedFragmentIDsPreservesPriorityAndDedupes(t *testing.T) {
	merged := mergeSeedFragmentIDs(5,
		[]string{"seed-path-1", "seed-path-2"},
		[]string{"seed-path-2", "semantic-1"},
		[]string{"location-1", "semantic-1", "location-2"},
	)
	want := []string{"seed-path-1", "seed-path-2", "semantic-1", "location-1", "location-2"}
	if len(merged) != len(want) {
		t.Fatalf("len mismatch: got %v want %v", merged, want)
	}
	for i := range want {
		if merged[i] != want[i] {
			t.Fatalf("priority mismatch at %d: got %v want %v", i, merged, want)
		}
	}
}

func TestSeedPathSearchNameOnlyUsesParallelPairs(t *testing.T) {
	signal := &horizonmodels.SignalDescriptor{
		SeedPaths: []string{"/loc/ir", "/loc/qa"},
		SeedNames: []string{"United Kingdom air defense", "Iran air defense", "Qatar air defense"},
	}
	if got := seedPathSearchName(signal, 0); got != "" {
		t.Fatalf("expected empty search name for non-parallel arrays, got %q", got)
	}
	paired := &horizonmodels.SignalDescriptor{
		SeedPaths: []string{"/loc/ir", "/loc/qa"},
		SeedNames: []string{"Iran", "Qatar"},
	}
	if got := seedPathSearchName(paired, 1); got != "Qatar" {
		t.Fatalf("expected aligned search name, got %q", got)
	}
}

func TestSemanticSeedQueryGeoBonusPrefersCountryMatch(t *testing.T) {
	qaFrag := &horizonmodels.HorizonFragment{
		CanonicalPath: "/org/qa/qatarenergy",
		Class:         FragmentTypeOrganization,
	}
	egFrag := &horizonmodels.HorizonFragment{
		CanonicalPath: "/gov/eg/egyptian-air-defense-forces",
		Class:         FragmentTypeOrganization,
	}
	if got := semanticSeedQueryGeoBonus("Qatar air defense", qaFrag); got <= 0 {
		t.Fatalf("expected positive geo bonus for Qatar match, got %.2f", got)
	}
	if got := semanticSeedQueryGeoBonus("Qatar air defense", egFrag); got >= 0 {
		t.Fatalf("expected negative geo bonus for Egypt mismatch, got %.2f", got)
	}
	if !semanticSeedCountryMismatch("Qatar air defense", egFrag) {
		t.Fatal("expected explicit Qatar query to reject Egypt fragment")
	}
	if semanticSeedCountryMismatch("Qatar air defense", qaFrag) {
		t.Fatal("expected Qatar fragment to pass explicit country filter")
	}
}

func TestSemanticSeedCountryFromQuery_AmbiguityPolicy(t *testing.T) {
	if got := semanticSeedCountryFromQuery("Port of Savannah, Georgia"); got != "" {
		t.Fatalf("expected georgia state query to stay ambiguous, got %q", got)
	}
	if got := semanticSeedCountryFromQuery("Michael Jordan retail business"); got != "" {
		t.Fatalf("expected person query to stay ambiguous, got %q", got)
	}
	if got := semanticSeedCountryFromQuery("Jordan air defense"); got != "jo" {
		t.Fatalf("expected Jordan country in defense context, got %q", got)
	}
	if got := semanticSeedCountryFromQuery("Mexico navy modernization"); got != "mx" {
		t.Fatalf("expected Mexico to resolve in defense query, got %q", got)
	}
	if got := semanticSeedCountryFromQuery("Morocco LNG terminal"); got != "ma" {
		t.Fatalf("expected Morocco to resolve in logistics query, got %q", got)
	}
}

func TestSemanticSeedFragmentAllowedRejectsSupplyLinksAndAbstractHubs(t *testing.T) {
	signal := &horizonmodels.SignalDescriptor{Axis: horizonmodels.AxisGeoConflict}
	cases := []struct {
		name string
		frag *horizonmodels.HorizonFragment
		want bool
	}{
		{
			name: "facility allowed",
			frag: &horizonmodels.HorizonFragment{
				CanonicalPath: "/fac/ir/bandar-abbas-port",
				Class:         FragmentTypeFacility,
			},
			want: true,
		},
		{
			name: "supply link rejected",
			frag: &horizonmodels.HorizonFragment{
				CanonicalPath: "/supply-link/ir/bandar-abbas-to-doha",
				Class:         ah.FragmentClassSite,
				Subtype:       "supply_link",
			},
			want: false,
		},
		{
			name: "generic loc rejected",
			frag: &horizonmodels.HorizonFragment{
				CanonicalPath: "/loc/qa/na",
				Class:         FragmentTypeGeoRegion,
			},
			want: false,
		},
		{
			name: "sphere hub rejected",
			frag: &horizonmodels.HorizonFragment{
				CanonicalPath: "/sphere/geopolitics",
				Class:         ah.FragmentClassEvidence,
				Subtype:       "domain_legacy",
			},
			want: false,
		},
		{
			name: "product hub rejected",
			frag: &horizonmodels.HorizonFragment{
				CanonicalPath: "/product/energy/natural-gas",
				Class:         FragmentTypeProduct,
			},
			want: false,
		},
	}
	for _, tc := range cases {
		if got := semanticSeedFragmentAllowed(signal, tc.frag); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestShouldSearchSemanticSeedQueryFiltersPoliticalNoise(t *testing.T) {
	signal := &horizonmodels.SignalDescriptor{Axis: horizonmodels.AxisGeoConflict}
	for _, bad := range []string{
		"President Donald Trump",
		"Trump Administration",
		"White House",
		"Plants and Production",
		"The Associated Press",
		"American and Israeli",
		"Patriot and THAAD",
		"Air Force",
		"Iran government",
		"The OSMP",
		"TOP SECRET",
		"HAND DISTRIBUTION ONLY",
		"CODEX LEVEL II PRINTED BY",
	} {
		if shouldSearchSemanticSeedQuery(signal, bad) {
			t.Fatalf("expected query %q to be filtered", bad)
		}
	}
	if !shouldSearchSemanticSeedQuery(signal, "Israel Defense Forces") {
		t.Fatal("expected operational military actor to be allowed")
	}
	if !shouldSearchSemanticSeedQuery(signal, "House Armed Services Committee") {
		t.Fatal("expected meaningful institutional actor to remain allowed")
	}
}

func TestSemanticSeedCapForSignal(t *testing.T) {
	if got := semanticSeedCapForSignal(&horizonmodels.SignalDescriptor{Axis: horizonmodels.AxisGeoConflict}); got != 3 {
		t.Fatalf("geo/conflict semantic cap: got %d want 3", got)
	}
	if got := semanticSeedCapForSignal(&horizonmodels.SignalDescriptor{Axis: horizonmodels.AxisPhysicalFlowSupply}); got != 4 {
		t.Fatalf("physical_flow/supply semantic cap: got %d want 4", got)
	}
}
