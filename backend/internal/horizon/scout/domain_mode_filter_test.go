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
	operational := []string{"PRODUCES", "SUPPLIES_TO", "TRANSFORMS_INTO", "ASSEMBLES_INTO", "USED_IN", "EXTRACTS_FROM"}
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

func TestIsAllowedSupplyChainRuntimeRelation_DomainAnchorKeepsScaffold(t *testing.T) {
	if !IsAllowedSupplyChainRuntimeRelation(
		"/domain/v1/supply-chain/geo/kz/subject/samsung-electronics",
		"/org/kr/samsung-electronics",
		models.DependencyTypeFocusesOn,
	) {
		t.Fatal("domain supply-chain anchor must allow FOCUSES_ON at runtime")
	}
}

func TestIsAllowedSupplyChainPersistRelation_DomainAnchorKeepsScaffold(t *testing.T) {
	if !IsAllowedSupplyChainPersistRelation(
		"/domain/v1/supply-chain/geo/kz/subject/samsung-electronics",
		"/org/kr/samsung-electronics",
		models.DependencyTypeFocusesOn,
	) {
		t.Fatal("domain supply-chain anchor must allow FOCUSES_ON at final persist gate")
	}
}

func TestIsAllowedSupplyChainRuntimeRelation_MarketAnchorKeepsMarketRelation(t *testing.T) {
	if !IsAllowedSupplyChainRuntimeRelation(
		"/market/kz/consumer-electronics/retail",
		"/market/kz/consumer-electronics/wholesale",
		models.DependencyType("HAS_MARKET_SEGMENT"),
	) {
		t.Fatal("market supply-chain anchor must allow market relations at runtime")
	}
}

func TestIsAllowedSupplyChainRuntimeRelation_OrgSourceStillRejectsFocusesOn(t *testing.T) {
	if IsAllowedSupplyChainRuntimeRelation(
		"/org/kr/samsung-electronics",
		"/market/kz/consumer-electronics/retail",
		models.DependencyTypeFocusesOn,
	) {
		t.Fatal("non-domain/non-market source must not allow FOCUSES_ON in supply-chain runtime gate")
	}
}

func TestIsAllowedSupplyChainPersistRelation_OrgSourceStillRejectsFocusesOn(t *testing.T) {
	if IsAllowedSupplyChainPersistRelation(
		"/org/kr/samsung-electronics",
		"/market/kz/consumer-electronics/retail",
		models.DependencyTypeFocusesOn,
	) {
		t.Fatal("non-domain/non-market source must not allow FOCUSES_ON at final persist gate")
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

func TestFilterDiscoveredRelationsByDomainModes_SupplyChainAdministersAllowedForRelevantPaths(t *testing.T) {
	govFrag := &models.HorizonFragment{CanonicalPath: "/gov/us/department-of-the-navy"}
	in := []agents.DiscoveredRelation{
		{
			RelationType:        "ADMINISTERS",
			TargetCanonicalPath: "/gov/us/navy",
		},
		{
			RelationType:        "ADMINISTERS",
			TargetCanonicalPath: "/fac/us/norfolk-naval-shipyard",
		},
		{
			RelationType:        "ADMINISTERS",
			TargetCanonicalPath: "/market/us/defense/naval-systems",
		},
	}

	out := filterDiscoveredRelationsByDomainModesWithDomainAnchor(govFrag, in, []string{taxonomy.DomainModeSupplyChain})
	if len(out) != 2 {
		t.Fatalf("expected 2 supply-relevant ADMINISTERS relations, got %d", len(out))
	}
	for _, r := range out {
		if r.RelationType != "ADMINISTERS" {
			t.Fatalf("expected ADMINISTERS relation, got %s", r.RelationType)
		}
		if r.TargetCanonicalPath == "/market/us/defense/naval-systems" {
			t.Fatalf("market target must not pass supply-chain ADMINISTERS predicate")
		}
	}
}

func TestFilterDiscoveredRelationsByDomainModes_SupplyChainSubsidiaryOfAllowedForRelevantPaths(t *testing.T) {
	orgFrag := &models.HorizonFragment{CanonicalPath: "/org/jp/toyota-industries"}
	in := []agents.DiscoveredRelation{
		{
			RelationType:        "SUBSIDIARY_OF",
			TargetCanonicalPath: "/org/jp/toyota-group",
		},
		{
			RelationType:        "SUBSIDIARY_OF",
			TargetCanonicalPath: "/gov/jp/ministry-of-economy-trade-and-industry",
		},
		{
			RelationType:        "SUBSIDIARY_OF",
			TargetCanonicalPath: "/market/jp/automotive/oem",
		},
	}

	out := filterDiscoveredRelationsByDomainModesWithDomainAnchor(orgFrag, in, []string{taxonomy.DomainModeSupplyChain})
	if len(out) != 2 {
		t.Fatalf("expected 2 supply-relevant SUBSIDIARY_OF relations, got %d", len(out))
	}
	for _, r := range out {
		if r.RelationType != "SUBSIDIARY_OF" {
			t.Fatalf("expected SUBSIDIARY_OF relation, got %s", r.RelationType)
		}
		if r.TargetCanonicalPath == "/market/jp/automotive/oem" {
			t.Fatalf("market target must not pass supply-chain SUBSIDIARY_OF predicate")
		}
	}
}

func TestIsAllowedSupplyChainRuntimeRelation_AllowsRelevantAdministersAndSubsidiaryOf(t *testing.T) {
	if !IsAllowedSupplyChainRuntimeRelation(
		"/gov/us/department-of-the-navy",
		"/gov/us/navy",
		models.DependencyTypeAdministers,
	) {
		t.Fatal("supply-chain runtime gate must allow ADMINISTERS for relevant governance overlays")
	}
	if !IsAllowedSupplyChainPersistRelation(
		"/org/jp/toyota-industries",
		"/org/jp/toyota-group",
		models.DependencyTypeSubsidiaryOf,
	) {
		t.Fatal("supply-chain persist gate must allow SUBSIDIARY_OF for relevant ownership overlays")
	}
	if !IsAllowedSupplyChainRuntimeRelation(
		"/program/ae/saber",
		"/gov/ae/ministry-of-industry-and-advanced-technology",
		models.DependencyTypeAdministers,
	) {
		t.Fatal("runtime gate must keep reversible PROGRAM -> GOV ADMINISTERS edges so canonicalization can swap them")
	}
	if IsAllowedSupplyChainRuntimeRelation(
		"/gov/us/department-of-the-navy",
		"/market/us/defense/naval-systems",
		models.DependencyTypeAdministers,
	) {
		t.Fatal("market target must remain out of contract for supply-chain ADMINISTERS")
	}
}

func TestIsAllowedSupplyChainRuntimeRelation_AllowsRelevantManagesAndAffects(t *testing.T) {
	if !IsAllowedSupplyChainRuntimeRelation(
		"/org/kz/damu-entrepreneurship-development-fund",
		"/program/kz/business-roadmap-2025",
		models.DependencyTypeManages,
	) {
		t.Fatal("supply-chain runtime gate must allow MANAGES for org->program overlays")
	}
	if !IsAllowedSupplyChainPersistRelation(
		"/org/kz/damu-entrepreneurship-development-fund",
		"/market/kz/logistics/cross-border-trade",
		models.DependencyTypeAffects,
	) {
		t.Fatal("supply-chain persist gate must allow AFFECTS for org->market overlays")
	}
	if !IsAllowedSupplyChainRuntimeRelation(
		"/program/ae/saber",
		"/community/global/professional/conformity-assessment-bodies",
		models.DependencyTypeAffects,
	) {
		t.Fatal("supply-chain runtime gate must allow AFFECTS for program->community overlays")
	}
	if IsAllowedSupplyChainRuntimeRelation(
		"/product/finished/consumer/footwear",
		"/market/kz/logistics/cross-border-trade",
		models.DependencyTypeAffects,
	) {
		t.Fatal("product source must remain out of contract for supply-chain AFFECTS overlays")
	}
}

func TestIsAllowedSupplyChainRuntimeRelation_AllowsRelevantPolicyImpactOverlays(t *testing.T) {
	cases := []struct {
		name   string
		source string
		target string
		typ    models.DependencyType
	}{
		{
			name:   "influences program to market pricing",
			source: "/program/kz/digital-footwear-labeling",
			target: "/market/kz/retail/apparel/footwear/pricing",
			typ:    models.DependencyTypeInfluences,
		},
		{
			name:   "gatekeeps decision gov to decision",
			source: "/gov/kz/ministry-of-industry-and-construction",
			target: "/decision/trade/mandatory-labeling-enforcement",
			typ:    models.DependencyTypeGatekeepsDecision,
		},
		{
			name:   "uses mechanism decision to mechanism",
			source: "/decision/trade/mandatory-labeling-enforcement",
			target: "/mechanism/regulatory/compulsory-data-matrix-marking",
			typ:    models.DependencyTypeUsesMechanism,
		},
		{
			name:   "regulates gov to market",
			source: "/gov/kz/ministry-of-industry-and-construction",
			target: "/market/kz/retail/footwear",
			typ:    models.DependencyTypeRegulates,
		},
		{
			name:   "imposes condition mechanism to market",
			source: "/mechanism/technological/is-mtt-traceability-system",
			target: "/market/kz/retail/footwear",
			typ:    models.DependencyTypeImposesCondition,
		},
		{
			name:   "controls access org to market",
			source: "/org/kz/kazakhtelecom",
			target: "/market/kz/retail/footwear",
			typ:    models.DependencyTypeControlsAccess,
		},
		{
			name:   "in geo scope program to geo",
			source: "/program/ae/saber",
			target: "/loc/ae",
			typ:    models.DependencyTypeInGeoScope,
		},
		{
			name:   "operates via mechanism to mechanism",
			source: "/mechanism/technological/is-mtt-traceability-system",
			target: "/mechanism/regulatory/compulsory-data-matrix-marking",
			typ:    models.DependencyTypeOperatesVia,
		},
		{
			name:   "direct competitor overlay on organizations",
			source: "/org/de/infineon-technologies",
			target: "/org/nl/nxp-semiconductors",
			typ:    models.DependencyTypeCompetesWith,
		},
		{
			name:   "partnership overlay from org to program",
			source: "/org/be/imec",
			target: "/program/eu/european-chips-act",
			typ:    models.DependencyTypePartnersWith,
		},
		{
			name:   "operates via overlay from org to facility",
			source: "/org/us/wolfspeed",
			target: "/fac/de/ensdorf-fab",
			typ:    models.DependencyTypeOperatesVia,
		},
		{
			name:   "membership overlay from org to program",
			source: "/org/eu/chips-joint-undertaking",
			target: "/program/eu/european-chips-act",
			typ:    models.DependencyTypeMemberOf,
		},
		{
			name:   "investment overlay from gov to org",
			source: "/gov/eu/european-commission",
			target: "/org/de/infineon",
			typ:    models.DependencyTypeInvestsIn,
		},
	}

	for _, tc := range cases {
		if !IsAllowedSupplyChainRuntimeRelation(tc.source, tc.target, tc.typ) {
			t.Fatalf("%s: runtime gate rejected %s -> %s (%s)", tc.name, tc.source, tc.target, tc.typ)
		}
		if !IsAllowedSupplyChainPersistRelation(tc.source, tc.target, tc.typ) {
			t.Fatalf("%s: persist gate rejected %s -> %s (%s)", tc.name, tc.source, tc.target, tc.typ)
		}
	}

	if IsAllowedSupplyChainRuntimeRelation(
		"/product/finished/consumer/footwear",
		"/decision/trade/mandatory-labeling-enforcement",
		models.DependencyTypeGatekeepsDecision,
	) {
		t.Fatal("product source must remain out of contract for GATEKEEPS_DECISION")
	}
	if IsAllowedSupplyChainRuntimeRelation(
		"/decision/trade/mandatory-labeling-enforcement",
		"/market/kz/retail/footwear",
		models.DependencyTypeUsesMechanism,
	) {
		t.Fatal("USES_MECHANISM must still require a mechanism target")
	}
	if IsAllowedSupplyChainRuntimeRelation(
		"/product/finished/consumer/footwear",
		"/mechanism/regulatory/compulsory-data-matrix-marking",
		models.DependencyTypeOperatesVia,
	) {
		t.Fatal("OPERATES_VIA must still reject product sources")
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

func TestFilterDomainRelations_CommunitySpecificEdgesAllowedInInfluenceModes(t *testing.T) {
	tests := []struct {
		mode string
	}{
		{mode: taxonomy.DomainModeGovernance},
		{mode: taxonomy.DomainModeMarket},
		{mode: taxonomy.DomainModeStrategic},
		{mode: taxonomy.DomainModePolicyImpact},
		{mode: taxonomy.DomainModeEcosystem},
	}

	in := []agents.DiscoveredRelation{
		{
			RelationType:        "HAS_SUBGROUP",
			SourceCanonicalPath: "/community/sa/dynasty/house-of-saud",
			TargetCanonicalPath: "/community/sa/dynasty/sudairi-seven",
		},
		{
			RelationType:        "MEMBER_OF",
			SourceCanonicalPath: "/person/sa/mohammed-bin-salman",
			TargetCanonicalPath: "/community/sa/dynasty/house-of-saud",
		},
		{
			RelationType:        "AFFILIATED_WITH",
			SourceCanonicalPath: "/community/ae/trade/hawala-network",
			TargetCanonicalPath: "/org/ae/exchange-house",
		},
		{
			RelationType:        "OPERATES_VIA",
			SourceCanonicalPath: "/community/ru/criminal/kazan-dealers",
			TargetCanonicalPath: "/media/telegram/kazan-zakaz",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.mode, func(t *testing.T) {
			out, dropped := agents.FilterDomainRelations(context.Background(), tc.mode, in)
			if dropped != 0 || len(out) != len(in) {
				t.Fatalf("%s should keep all community-specific contract edges, dropped=%d out=%d", tc.mode, dropped, len(out))
			}
		})
	}
}

func TestFilterDiscoveredRelationsByDomainModes_SupplyChainDropsNonContractTypes(t *testing.T) {
	// Contract isolation: ASSOCIATED_WITH must be dropped in supply-chain mode.
	// COMPETES_WITH, PARTNERS_WITH, OPERATES_VIA, MEMBER_OF, and INVESTS_IN are allowed as
	// supply-chain overlays. SUBSIDIARY_OF may survive here via canonicalized ownership semantics,
	// while PARTICIPATES_IN is in allowlist (market→org scaffold); SUPPLIES_TO passes; DEPENDS_ON excluded.
	in := []agents.DiscoveredRelation{
		{RelationType: "SUPPLIES_TO", TargetCanonicalPath: "/org/us/acme"},
		{RelationType: "PARTNERS_WITH", SourceCanonicalPath: "/org/tw/tsmc", TargetCanonicalPath: "/org/eu/partner"},
		{RelationType: "COMPETES_WITH", SourceCanonicalPath: "/org/de/infineon-technologies", TargetCanonicalPath: "/org/us/competitor"},
		{RelationType: "OPERATES_VIA", SourceCanonicalPath: "/org/us/wolfspeed", TargetCanonicalPath: "/fac/de/ops-site"},
		{RelationType: "MEMBER_OF", SourceCanonicalPath: "/org/eu/chips-joint-undertaking", TargetCanonicalPath: "/program/eu/european-chips-act"},
		{RelationType: "INVESTS_IN", SourceCanonicalPath: "/gov/eu/european-commission", TargetCanonicalPath: "/org/de/infineon"},
		{RelationType: "SUBSIDIARY_OF", SourceCanonicalPath: "/org/us/sub", TargetCanonicalPath: "/org/us/parent"},
		{RelationType: "ASSOCIATED_WITH", TargetCanonicalPath: "/org/uk/affiliate"},
		{RelationType: "PARTICIPATES_IN", TargetCanonicalPath: "/market/global/electronics"},
		{RelationType: "DEPENDS_ON", TargetCanonicalPath: "/org/cn/supplier"},
	}
	out := filterDiscoveredRelationsByDomainModes(in, []string{taxonomy.DomainModeSupplyChain})
	types := make(map[string]bool)
	for _, r := range out {
		types[r.RelationType] = true
	}
	if !types["SUPPLIES_TO"] || !types["PARTICIPATES_IN"] || !types["COMPETES_WITH"] || !types["PARTNERS_WITH"] || !types["OPERATES_VIA"] || !types["MEMBER_OF"] || !types["INVESTS_IN"] {
		t.Fatalf("expected SUPPLIES_TO, PARTICIPATES_IN, COMPETES_WITH, PARTNERS_WITH, OPERATES_VIA, MEMBER_OF, and INVESTS_IN to pass, got %v", types)
	}
	if types["DEPENDS_ON"] {
		t.Fatalf("DEPENDS_ON must be excluded in supply_chain; got %v", types)
	}
	for _, bad := range []string{"ASSOCIATED_WITH"} {
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
	for _, bad := range []string{"SUBSIDIARY_OF", "ASSOCIATED_WITH", "DEPENDS_ON"} {
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
