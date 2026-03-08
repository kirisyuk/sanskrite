package scout

import (
	"context"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func TestEdgeAllowlistByDomainMode_SupplyChainIncludesMarketAnchors(t *testing.T) {
	allow := EdgeAllowlistByDomainMode(taxonomy.DomainModeSupplyChain)
	operational := []string{"PRODUCES", "SUPPLIES_TO", "TRANSFORMS_INTO", "EXTRACTS_FROM"}
	for _, ot := range operational {
		if !AllowlistContains(allow, models.DependencyType(ot)) {
			t.Fatalf("supply_chain allowlist must include %s", ot)
		}
	}
	if !AllowlistContains(allow, models.DependencyType("HAS_MARKET_SEGMENT")) {
		t.Fatalf("supply_chain allowlist must include HAS_MARKET_SEGMENT")
	}
	if AllowlistContains(allow, models.DependencyType("DEPENDS_ON")) {
		t.Fatalf("supply_chain allowlist must exclude DEPENDS_ON (use SUPPLIES_TO, PARTICIPATES_IN instead)")
	}
}

func TestEdgeAllowlistByDomainMode_PolicyImpactIncludesInfluences(t *testing.T) {
	allow := EdgeAllowlistByDomainMode(taxonomy.DomainModePolicyImpact)
	if !AllowlistContains(allow, "INFLUENCES") {
		t.Fatalf("policy_impact allowlist must include INFLUENCES")
	}
}

func TestEdgeAllowlistByDomainMode_GovernanceIncludesAssurance(t *testing.T) {
	allow := EdgeAllowlistByDomainMode(taxonomy.DomainModeGovernance)
	if !AllowlistContains(allow, "ASSURES") {
		t.Fatalf("governance allowlist must include ASSURES for auditor/certifier governance links")
	}
}

func TestEdgeAllowlistByDomainMode_AssuranceExcludesLegacyAuditedBy(t *testing.T) {
	allow := EdgeAllowlistByDomainMode(taxonomy.DomainModeAssurance)
	if AllowlistContains(allow, "AUDITED_BY") {
		t.Fatalf("assurance allowlist must not include legacy AUDITED_BY output")
	}
}

func TestEdgeAllowlistByDomainModes_ComposedFromContractSemantics(t *testing.T) {
	allow := EdgeAllowlistByDomainModes([]string{taxonomy.DomainModeGovernance, taxonomy.DomainModeSupplyChain})
	for _, want := range []models.DependencyType{
		models.DependencyTypeProduces,
		models.DependencyTypeSuppliesTo,
		models.DependencyTypeOwns,
		models.DependencyTypeBoardMemberOf,
	} {
		if !AllowlistContains(allow, want) {
			t.Fatalf("composed allowlist must include %s", want)
		}
	}
	if AllowlistContains(allow, models.DependencyTypeInfluences) {
		t.Fatalf("composed allowlist must not leak interpretive INFLUENCES into supply+governance runtime contract")
	}
}

func TestFilterDiscoveredRelationsByDomainModes_SupplyChain(t *testing.T) {
	in := []agents.DiscoveredRelation{
		{RelationType: "SUPPLIES_TO"},
		{RelationType: "depends_on"},
		{RelationType: "PRODUCES"},
		{RelationType: "OWNS"},
	}
	out := filterDiscoveredRelationsByDomainModes(in, []string{taxonomy.DomainModeSupplyChain})
	// DEPENDS_ON excluded; OWNS excluded. Only SUPPLIES_TO and PRODUCES pass.
	if len(out) != 2 {
		t.Fatalf("expected 2 relations after supply_chain filter, got %d", len(out))
	}
	types := map[string]bool{}
	for _, r := range out {
		types[r.RelationType] = true
	}
	if !types["SUPPLIES_TO"] || !types["PRODUCES"] {
		t.Fatalf("expected SUPPLIES_TO, PRODUCES; got %v", types)
	}
	if types["DEPENDS_ON"] || types["OWNS"] {
		t.Fatalf("DEPENDS_ON and OWNS must be excluded in supply_chain; got %v", types)
	}
}

func TestFilterDiscoveredRelationsByDomainModes_DomainAnchorKeepsScaffold(t *testing.T) {
	domainFrag := &models.HorizonFragment{CanonicalPath: "/domain/v1/supply-chain/geo/tr/market/tr/food-beverage/soft-drinks"}
	in := []agents.DiscoveredRelation{
		{RelationType: "FOCUSES_ON", TargetCanonicalPath: "/org/us/coca-cola"},
		{RelationType: "RELATED_DOMAIN", TargetCanonicalPath: "/market/tr/beverages"},
		{RelationType: "OWNS", TargetCanonicalPath: "/org/us/pepsi"},
	}
	out := filterDiscoveredRelationsByDomainModesWithDomainAnchor(domainFrag, in, []string{taxonomy.DomainModeSupplyChain})
	// FOCUSES_ON and RELATED_DOMAIN pass (domain-scaffold from domain); OWNS excluded
	if len(out) != 2 {
		t.Fatalf("expected 2 relations (domain-anchor scaffold), got %d", len(out))
	}
	types := map[string]bool{}
	for _, r := range out {
		types[r.RelationType] = true
	}
	if !types["FOCUSES_ON"] || !types["RELATED_DOMAIN"] {
		t.Fatalf("expected FOCUSES_ON and RELATED_DOMAIN from domain anchor, got %v", types)
	}
	if types["OWNS"] {
		t.Fatalf("OWNS must remain excluded in supply_chain mode even from domain")
	}
}

func TestFilterDiscoveredRelationsByDomainModes_MarketAnchorKeepsMarketTypes(t *testing.T) {
	marketFrag := &models.HorizonFragment{CanonicalPath: "/market/za/fmcg/soft-drinks"}
	in := []agents.DiscoveredRelation{
		{RelationType: "DEPENDS_ON", TargetCanonicalPath: "/org/za/ccbsa"},
		{RelationType: "HAS_MARKET_SEGMENT", TargetCanonicalPath: "/market/za/fmcg/carbonated-beverages"},
		{RelationType: "PARTICIPATES_IN", TargetCanonicalPath: "/org/za/ccbsa"},
		{RelationType: "OWNS", TargetCanonicalPath: "/org/za/pepsi"},
	}
	out := filterDiscoveredRelationsByDomainModesWithDomainAnchor(marketFrag, in, []string{taxonomy.DomainModeSupplyChain})
	// DEPENDS_ON blocked in supply chain; HAS_MARKET_SEGMENT and PARTICIPATES_IN pass; OWNS excluded
	if len(out) != 2 {
		t.Fatalf("expected 2 relations (market-anchor), got %d", len(out))
	}
	types := map[string]bool{}
	for _, r := range out {
		types[r.RelationType] = true
	}
	if !types["HAS_MARKET_SEGMENT"] || !types["PARTICIPATES_IN"] {
		t.Fatalf("expected HAS_MARKET_SEGMENT and PARTICIPATES_IN from market anchor, got %v", types)
	}
	if types["DEPENDS_ON"] || types["OWNS"] {
		t.Fatalf("DEPENDS_ON and OWNS must remain excluded in supply_chain mode; got %v", types)
	}
}

func TestFilterDiscoveredRelationsByDomainModes_SupplyChainOwnsAllowedForRelevantPaths(t *testing.T) {
	orgFrag := &models.HorizonFragment{CanonicalPath: "/org/jp/canon"}
	in := []agents.DiscoveredRelation{
		{
			RelationType:        "OWNS",
			TargetCanonicalPath: "/org/jp/canon-semiconductor-manufacturing",
		},
		{
			RelationType:        "OWNS",
			TargetCanonicalPath: "/fac/jp/oita-plant",
		},
		{
			RelationType:        "OWNS",
			TargetCanonicalPath: "/market/jp/electronics/imaging",
		},
	}

	out := filterDiscoveredRelationsByDomainModesWithDomainAnchor(orgFrag, in, []string{taxonomy.DomainModeSupplyChain})
	if len(out) != 2 {
		t.Fatalf("expected 2 supply-relevant OWNS relations, got %d", len(out))
	}
	for _, r := range out {
		if r.RelationType != "OWNS" {
			t.Fatalf("expected OWNS relation, got %s", r.RelationType)
		}
		if r.TargetCanonicalPath == "/market/jp/electronics/imaging" {
			t.Fatalf("market target must not pass supply-chain OWNS predicate")
		}
	}
}

func TestFilterDiscoveredRelationsByDomainModes_SupplyChainControlsAllowedForRelevantPaths(t *testing.T) {
	govFrag := &models.HorizonFragment{CanonicalPath: "/gov/us/department-of-energy"}
	in := []agents.DiscoveredRelation{
		{
			RelationType:        "CONTROLS",
			TargetCanonicalPath: "/fac/us/strategic-reserve-terminal",
		},
		{
			RelationType:        "CONTROLS",
			TargetCanonicalPath: "/org/us/federal-energy-regulatory-commission",
		},
		{
			RelationType:        "CONTROLS",
			TargetCanonicalPath: "/market/us/energy/oil-gas",
		},
	}

	out := filterDiscoveredRelationsByDomainModesWithDomainAnchor(govFrag, in, []string{taxonomy.DomainModeSupplyChain})
	if len(out) != 2 {
		t.Fatalf("expected 2 supply-relevant CONTROLS relations, got %d", len(out))
	}
	for _, r := range out {
		if r.RelationType != "CONTROLS" {
			t.Fatalf("expected CONTROLS relation, got %s", r.RelationType)
		}
		if r.TargetCanonicalPath == "/market/us/energy/oil-gas" {
			t.Fatalf("market target must not pass supply-chain CONTROLS predicate")
		}
	}
}

func TestFilterDomainRelations_GovernanceAllowsFacilityTargets(t *testing.T) {
	in := []agents.DiscoveredRelation{{
		RelationType:        "OWNS",
		SourceCanonicalPath: "/org/de/infineon",
		TargetCanonicalPath: "/fac/de/dresden-fab",
	}}
	out, dropped := agents.FilterDomainRelations(context.Background(), taxonomy.DomainModeGovernance, in)
	if dropped != 0 || len(out) != 1 {
		t.Fatalf("governance should keep org->facility OWNS, dropped=%d out=%d", dropped, len(out))
	}
}

func TestFilterDomainRelations_StrategicAllowsAffectedOperationalTargets(t *testing.T) {
	in := []agents.DiscoveredRelation{
		{
			RelationType:        "CONTROLS_ACCESS",
			SourceCanonicalPath: "/decision/export-controls/chip-license-gate",
			TargetCanonicalPath: "/fac/tw/fab-21",
		},
		{
			RelationType:        "APPLIES_TO",
			SourceCanonicalPath: "/mechanism/regulatory/export-license-requirement",
			TargetCanonicalPath: "/product/finished/electronic/assembly/gpu",
		},
	}
	out, dropped := agents.FilterDomainRelations(context.Background(), taxonomy.DomainModeStrategic, in)
	if dropped != 0 || len(out) != 2 {
		t.Fatalf("strategic should keep operational targets, dropped=%d out=%d", dropped, len(out))
	}
}

func TestFilterDomainRelations_PolicyImpactAllowsFacilityAndProductTargets(t *testing.T) {
	in := []agents.DiscoveredRelation{
		{
			RelationType:        "IMPOSES_CONDITION",
			SourceCanonicalPath: "/gov/eu/european-commission",
			TargetCanonicalPath: "/fac/de/basf-ludwigshafen",
		},
		{
			RelationType:        "REGULATES",
			SourceCanonicalPath: "/gov/us/fda",
			TargetCanonicalPath: "/product/finished/chemical/processing/api",
		},
	}
	out, dropped := agents.FilterDomainRelations(context.Background(), taxonomy.DomainModePolicyImpact, in)
	if dropped != 0 || len(out) != 2 {
		t.Fatalf("policy_impact should keep facility/product targets, dropped=%d out=%d", dropped, len(out))
	}
}

func TestFilterDiscoveredRelationsByDomainModes_SupplyChainDropsNonContractTypes(t *testing.T) {
	// Contract isolation: PARTNERS_WITH, COMPETES_WITH, SUBSIDIARY_OF, ASSOCIATED_WITH
	// must be dropped in supply-chain mode (not in SupplyChainEdgeTypes).
	// PARTICIPATES_IN is in allowlist (market→org scaffold); SUPPLIES_TO passes; DEPENDS_ON excluded.
	in := []agents.DiscoveredRelation{
		{RelationType: "SUPPLIES_TO", TargetCanonicalPath: "/org/us/acme"},
		{RelationType: "PARTNERS_WITH", TargetCanonicalPath: "/org/eu/partner"},
		{RelationType: "COMPETES_WITH", TargetCanonicalPath: "/org/us/competitor"},
		{RelationType: "SUBSIDIARY_OF", TargetCanonicalPath: "/org/us/parent"},
		{RelationType: "ASSOCIATED_WITH", TargetCanonicalPath: "/org/uk/affiliate"},
		{RelationType: "PARTICIPATES_IN", TargetCanonicalPath: "/market/global/electronics"},
		{RelationType: "DEPENDS_ON", TargetCanonicalPath: "/org/cn/supplier"},
	}
	out := filterDiscoveredRelationsByDomainModes(in, []string{taxonomy.DomainModeSupplyChain})
	types := make(map[string]bool)
	for _, r := range out {
		types[r.RelationType] = true
	}
	if !types["SUPPLIES_TO"] || !types["PARTICIPATES_IN"] {
		t.Fatalf("expected SUPPLIES_TO and PARTICIPATES_IN to pass, got %v", types)
	}
	if types["DEPENDS_ON"] {
		t.Fatalf("DEPENDS_ON must be excluded in supply_chain; got %v", types)
	}
	for _, bad := range []string{"PARTNERS_WITH", "COMPETES_WITH", "SUBSIDIARY_OF", "ASSOCIATED_WITH"} {
		if types[bad] {
			t.Fatalf("supply-chain filter must drop %s (out-of-contract)", bad)
		}
	}
}

func TestIsSupplyChainEdgeType(t *testing.T) {
	for _, allowed := range taxonomy.SupplyChainEdgeTypes {
		if !IsSupplyChainEdgeType(allowed) {
			t.Fatalf("IsSupplyChainEdgeType(%q) must be true", allowed)
		}
	}
	for _, bad := range []string{"PARTNERS_WITH", "COMPETES_WITH", "SUBSIDIARY_OF", "ASSOCIATED_WITH", "INVESTS_IN", "DEPENDS_ON"} {
		if IsSupplyChainEdgeType(bad) {
			t.Fatalf("IsSupplyChainEdgeType(%q) must be false (out-of-contract)", bad)
		}
	}
}

func TestSupplyChainsEnabled(t *testing.T) {
	if !supplyChainsEnabled("/org/us/test", "", []string{taxonomy.DomainModeSupplyChain}, nil) {
		t.Fatalf("supply chains must be enabled in core mode")
	}
	if !supplyChainsEnabled("/org/us/test", "", []string{taxonomy.DomainModeGovernance}, []string{taxonomy.SpecialtySupplyChain}) {
		t.Fatalf("supply chains must remain enabled with specialty/domain overrides")
	}
	if !supplyChainsEnabled("/org/us/test", "", []string{taxonomy.DomainModeGovernance}, nil) {
		t.Fatalf("supply chains must be enabled even outside explicit supply_chain mode/specialty")
	}
	if supplyChainsEnabled("/person/john-doe", "", []string{taxonomy.DomainModeSupplyChain}, nil) {
		t.Fatalf("supply chains must be disabled for /person paths")
	}
	if supplyChainsEnabled("/org/us/test", "PERSON", []string{taxonomy.DomainModeSupplyChain}, nil) {
		t.Fatalf("supply chains must be disabled for PERSON type")
	}
}
