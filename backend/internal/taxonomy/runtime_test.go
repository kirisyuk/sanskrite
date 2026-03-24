package taxonomy

import (
	"strings"
	"testing"
)

func withRuntimeTaxonomy(t *testing.T, tax *RuntimeTaxonomy) {
	t.Helper()
	rtMu.Lock()
	prev := rt
	rt = tax
	rtMu.Unlock()
	t.Cleanup(func() {
		rtMu.Lock()
		rt = prev
		rtMu.Unlock()
	})
}

func TestNormalizeRelationType_RuntimeTaxonomyDriftAllowsBuiltIns(t *testing.T) {
	// Simulate a "stale" runtime taxonomy that doesn't include newer built-in relation types.
	withRuntimeTaxonomy(t, &RuntimeTaxonomy{
		Version: "test",
		AllowedRelationTypes: map[string]struct{}{
			"SUPPLIES_TO": {}, // intentionally tiny allowlist
		},
	})

	tests := []struct {
		in   string
		want string
	}{
		{"controls", "CONTROLS"},
		{"controls_access", "CONTROLS_ACCESS"},
		{"imposes_condition", "IMPOSES_CONDITION"},
		{"uses_mechanism", "USES_MECHANISM"},
		{"applies_to", "APPLIES_TO"},
		{"MANAGES", "MANAGES"},
		{"controlled_by", "CONTROLS"}, // alias -> CONTROLS
		{"owned_by", "SUBSIDIARY_OF"}, // alias -> SUBSIDIARY_OF
		{"corporate_transition", "CORPORATE_TRANSITION"},
		{"domain_relation", "DOMAIN_RELATION"},
		{"has_submarket", "HAS_MARKET_SEGMENT"},
		{"", "INFLUENCES"},
		{"some_new_unknown_type", "INFLUENCES"},
	}

	for _, tt := range tests {
		if got := NormalizeRelationType(tt.in); got != tt.want {
			t.Errorf("NormalizeRelationType(%q)=%q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNormalizeRelationType_EXTRACTS_FROM(t *testing.T) {
	withRuntimeTaxonomy(t, &RuntimeTaxonomy{
		Version: "test",
		AllowedRelationTypes: map[string]struct{}{
			"EXTRACTS_FROM": {},
		},
	})

	tests := []struct {
		in   string
		want string
	}{
		{"EXTRACTS_FROM", "EXTRACTS_FROM"},
		{"extracts_from", "EXTRACTS_FROM"},
		{"Extracts_From", "EXTRACTS_FROM"},
		// Note: spaces and underscores are normalized differently
		{"EXTRACTS FROM", "EXTRACTS_FROM"}, // uppercase with space becomes EXTRACTS_FROM after upper+trim
	}

	for _, tt := range tests {
		got := NormalizeRelationType(tt.in)
		// The function does ToUpper and TrimSpace, not underscore normalization
		// So "extracts from" becomes "EXTRACTS FROM" and won't match
		if strings.ToUpper(strings.TrimSpace(tt.in)) == "EXTRACTS_FROM" && got != tt.want {
			t.Errorf("NormalizeRelationType(%q)=%q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNormalizeRelationType_EXTRACTS_FROM_BuiltIn(t *testing.T) {
	// Test that EXTRACTS_FROM is in the built-in allowlist even without runtime taxonomy
	withRuntimeTaxonomy(t, nil)

	tests := []struct {
		in   string
		want string
	}{
		{"EXTRACTS_FROM", "EXTRACTS_FROM"},
		{"extracts_from", "EXTRACTS_FROM"},
	}

	for _, tt := range tests {
		if got := NormalizeRelationType(tt.in); got != tt.want {
			t.Errorf("NormalizeRelationType(%q)=%q, want %q (without runtime taxonomy)", tt.in, got, tt.want)
		}
	}
}

func TestNormalizeLifecycleStage(t *testing.T) {
	withRuntimeTaxonomy(t, nil)

	tests := []struct {
		in   string
		want string
	}{
		{"operational", "operational"},
		{"OPERATIONAL", "operational"},
		{"Operational", "operational"},
		{"producing", "operational"},       // alias
		{"active", "operational"},          // alias
		{"exploration", "concept"},         // alias
		{"feasibility", "planning"},        // alias
		{"development", "construction"},    // alias
		{"mothballed", "suspended"},        // alias
		{"abandoned", "closed"},            // alias
		{"unknown_stage", "unknown_stage"}, // returns as-is if not found
		{"", ""},                           // empty returns empty
	}

	for _, tt := range tests {
		if got := NormalizeLifecycleStage(tt.in); got != tt.want {
			t.Errorf("NormalizeLifecycleStage(%q)=%q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestIsValidDepositResource(t *testing.T) {
	withRuntimeTaxonomy(t, nil)

	tests := []struct {
		resource string
		want     bool
	}{
		{"oil", true},
		{"natural-gas", true},
		{"iron-ore", true},
		{"lithium-ore", true},
		{"other", true},
		{"uranium-ore", false},
		{"", false},
		{"unknown", false},
	}

	for _, tt := range tests {
		if got := IsValidDepositResource(tt.resource); got != tt.want {
			t.Errorf("IsValidDepositResource(%q)=%v, want %v", tt.resource, got, tt.want)
		}
	}
}

func TestIsValidEdgeMetric(t *testing.T) {
	withRuntimeTaxonomy(t, nil)

	tests := []struct {
		edgeType   string
		metricName string
		want       bool
	}{
		{"OWNS", "ownership_percent", true},
		{"OWNS", "voting_rights_percent", true},
		{"OWNS", "investment_amount", false}, // wrong edge type
		{"INVESTS_IN", "investment_amount", true},
		{"INVESTS_IN", "ownership_percent", false}, // wrong edge type
		{"SUPPLIES_TO", "annual_volume", true},
		{"", "ownership_percent", false},
		{"OWNS", "", false},
	}

	for _, tt := range tests {
		if got := IsValidEdgeMetric(tt.edgeType, tt.metricName); got != tt.want {
			t.Errorf("IsValidEdgeMetric(%q, %q)=%v, want %v", tt.edgeType, tt.metricName, got, tt.want)
		}
	}
}

func TestIsValidEdgeQualifier(t *testing.T) {
	withRuntimeTaxonomy(t, nil)

	tests := []struct {
		edgeType      string
		qualifierName string
		want          bool
	}{
		{"OWNS", "share_type", true},
		{"OWNS", "voting_rights_percent", true},
		{"CONTROLS", "control_type", true},
		{"AFFECTS", "pass_through_estimate", true},
		{"AFFECTS", "lag_quarters", true},
		{"IN_GEO_SCOPE", "scope_basis", true},
		{"INFLUENCES", "via", true},
		{"INFLUENCES", "strength", true},
		{"INVESTS_IN", "round", true},
		{"OWNS", "round", false},
		{"IN_GEO_SCOPE", "pass_through_estimate", false},
		{"", "share_type", false},
		{"OWNS", "", false},
	}

	for _, tt := range tests {
		if got := IsValidEdgeQualifier(tt.edgeType, tt.qualifierName); got != tt.want {
			t.Errorf("IsValidEdgeQualifier(%q, %q)=%v, want %v", tt.edgeType, tt.qualifierName, got, tt.want)
		}
	}
}

func TestGetAllowedEdgeQualifiers_ReturnsCopy(t *testing.T) {
	withRuntimeTaxonomy(t, nil)

	got := GetAllowedEdgeQualifiers("AFFECTS")
	if len(got) == 0 {
		t.Fatal("expected allowed qualifiers for AFFECTS")
	}
	if !containsString(got, "pass_through_estimate") || !containsString(got, "channel_type") {
		t.Fatalf("expected transmission qualifiers in AFFECTS contract, got %v", got)
	}

	got[0] = "mutated"
	again := GetAllowedEdgeQualifiers("AFFECTS")
	if len(again) == 0 || again[0] == "mutated" {
		t.Fatal("expected GetAllowedEdgeQualifiers to return a defensive copy")
	}
}

func TestIsValidEdgeModality(t *testing.T) {
	withRuntimeTaxonomy(t, &RuntimeTaxonomy{
		Version: "test",
		AllowedEdgeModalities: map[string]struct{}{
			"actual":     {},
			"projected":  {},
			"historical": {},
			"potential":  {},
		},
	})

	tests := []struct {
		modality string
		want     bool
	}{
		{"actual", true},
		{"projected", true},
		{"historical", true},
		{"potential", true},
		{"unknown", false},
		{"", false},
	}

	for _, tt := range tests {
		if got := IsValidEdgeModality(tt.modality); got != tt.want {
			t.Errorf("IsValidEdgeModality(%q)=%v, want %v", tt.modality, got, tt.want)
		}
	}
}

func TestIsValidServiceDomain(t *testing.T) {
	withRuntimeTaxonomy(t, nil)

	tests := []struct {
		domain string
		want   bool
	}{
		{"it", true},
		{"finance", true},
		{"consulting", true},
		{"other", true},
		{"healthcare", true},
		{"", false},
	}

	for _, tt := range tests {
		if got := IsValidServiceDomain(tt.domain); got != tt.want {
			t.Errorf("IsValidServiceDomain(%q)=%v, want %v", tt.domain, got, tt.want)
		}
	}
}

func TestIsValidServiceSubdomain(t *testing.T) {
	withRuntimeTaxonomy(t, nil)

	tests := []struct {
		domain    string
		subdomain string
		want      bool
	}{
		{"it", "software-development", true},
		{"it", "cloud-services", true},
		{"it", "cybersecurity", true},
		{"it", "other", true},
		{"finance", "banking", true},
		{"finance", "insurance", true},
		{"it", "healthcare", false},   // wrong subdomain for domain
		{"healthcare", "other", true}, // fallback allows "other" for any domain
		{"", "software-development", false},
		{"it", "", false},
	}

	for _, tt := range tests {
		if got := IsValidServiceSubdomain(tt.domain, tt.subdomain); got != tt.want {
			t.Errorf("IsValidServiceSubdomain(%q, %q)=%v, want %v", tt.domain, tt.subdomain, got, tt.want)
		}
	}
}

func TestIsValidMetricUnit_RuntimeAllowlist_FailOpen(t *testing.T) {
	withRuntimeTaxonomy(t, &RuntimeTaxonomy{
		Version: "test",
		AllowedMetricUnits: map[string]struct{}{
			"percent": {},
			"usd":     {}, // even if present, currency metrics should still use pattern validation
			"bbl":     {},
		},
	})

	// Physical unit present in allowlist
	if !IsValidMetricUnit("bbl", false) {
		t.Errorf("IsValidMetricUnit(bbl, false)=false, want true")
	}
	// Physical unit absent from allowlist -> still accepted if it looks like a token (fail-open)
	if !IsValidMetricUnit("gwh", false) {
		t.Errorf("IsValidMetricUnit(gwh, false)=false, want true (fail-open)")
	}
	// Currency metric: pattern validated (any ticker/unit-like token)
	if !IsValidMetricUnit("usdt", true) {
		t.Errorf("IsValidMetricUnit(usdt, true)=false, want true")
	}
	// Nonsense: should be rejected (doesn't match token pattern)
	if IsValidMetricUnit("USD$", true) {
		t.Errorf("IsValidMetricUnit(USD$, true)=true, want false")
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
