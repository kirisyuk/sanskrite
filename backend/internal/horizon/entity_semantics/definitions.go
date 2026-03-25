package entity_semantics

import (
	"fmt"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/graphenrich"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// All definitions preserve the full taxonomy and verified prompt fragments.
// Business goals are primary; no simplification.

func init() {
	registerAll()
	models.RegisterProfileSectionsResolver(profileSectionsResolverImpl)
}

func profileSectionsResolverImpl(fragmentType models.FragmentType, canonicalPath string, communityType string) []models.ProfileSectionSpec {
	return ProfileSectionsForType(fragmentType, canonicalPath, communityType)
}

func registerAll() {
	registry[EntityTypeOrganization] = &EntityTypeDefinition{
		Key: EntityTypeOrganization,
		RequiredAspects: []string{
			"products", "competitors", "investors", "owners", "markets", "deposits", "locations",
		},
		ProfileSections: []models.ProfileSectionSpec{
			{Key: "identity_operations", Title: "Identity & Operations", Description: "Who they are, where they operate, scale, footprint, how work is organized", Required: true, Order: 1},
			{Key: "business_moat", Title: "Business Model & Moat", Description: "How value is created, revenue streams, pricing power, defensibility", Required: true, Order: 2},
			{Key: "leadership_governance", Title: "Leadership & Governance", Description: "Key leaders, board, governance signals, leadership changes", Required: false, Order: 3},
			{Key: "financial_profile", Title: "Financial Profile", Description: "Revenue, margins, cash flow, capital structure, unit economics", Required: true, Order: 4},
			{Key: "market_position", Title: "Market Position & Competition", Description: "Competitive position, peers, share, differentiation", Required: true, Order: 5},
			{Key: "risks_constraints", Title: "Risks & Constraints", Description: "Regulatory, legal, operational, supply, cyber, concentration risks", Required: false, Order: 6},
			{Key: "trajectory_catalysts", Title: "Trajectory & Catalysts", Description: "Momentum, recent events, upcoming milestones, what changes the outlook", Required: false, Order: 7},
		},
		ExtractionBlock: `ENTITY TYPE: ORGANIZATION
- Required aspects to extract: products (PRODUCES), competitors (COMPETES_WITH), investors (INVESTS_IN), owners (OWNS/SUBSIDIARY_OF), markets (PARTICIPATES_IN), deposits (OPERATES_IN when extractive), locations (LOCATED_IN, OPERATES_IN).
- Identify critical infrastructure dependencies: transport chokepoints (straits, canals, pipelines), key ports, and logistics corridors through which this entity's supply chain flows. Create DEPENDS_ON edges to these infrastructure nodes.
- Taxonomically correct edge types:
  - Required/primary: PRODUCES, SUPPLIES_TO, COMPETES_WITH, INVESTS_IN, OWNS, SUBSIDIARY_OF, PARTICIPATES_IN, LOCATED_IN, OPERATES_IN
  - Allowed when evidence exists: DEPENDS_ON, SUPPLIES_TO, CONTROLS, MANAGES, PARTNERS_WITH, REGULATES
  - For leadership: MANAGES, EMPLOYED_BY, BOARD_MEMBER_OF, FOUNDED (PERSON targets)
- Missing info priorities:
  - Missing products and services.
  - Missing competitors and market participation.
  - Missing ownership/investor structure.
  - Missing locations and deposits (for extractive orgs).`,
		MissingInfoRules: []MissingInfoRule{
			{Trigger: TriggerFieldEmpty, FieldPath: "Labels.en", QuestionTemplate: "entity label/name in English for %s"},
			{Trigger: TriggerFieldEmpty, FieldPath: "Identifiers.legalName", QuestionTemplate: "official legal name for %s"},
			{Trigger: TriggerFieldEmpty, FieldPath: "MarketSegments", QuestionTemplate: "industry sector classification for %s"},
			{Trigger: TriggerRelationCountBelow, MinCount: 1, QuestionTemplate: "relationships with other entities for %s"},
			{Trigger: TriggerSeed, QuestionTemplate: "Who provides external assurance to %s? Auditor, certifier, or regulator. Use ASSURES(assurance_type:audit|certification|regulatory|inspection). Provide assurer and source."},
			{Trigger: TriggerSeed, QuestionTemplate: "[HIGH PRIORITY] What industry/sector does %s operate in? Return marketSegments as array of paths /segment/{domain}/{vertical}/{geo}."},
			{Trigger: TriggerSeed, QuestionTemplate: "[HIGH PRIORITY] What is the size of %s? Find employee count or revenue to determine orgSize: sme, le, enterprise, mega."},
		},
		ResearchBlock: `ENTITY TYPE: ORGANIZATION
- Required aspects: products, competitors, investors, owners, markets, deposits, locations.
- Taxonomically correct edge types: PRODUCES, SUPPLIES_TO, COMPETES_WITH, INVESTS_IN, OWNS, SUBSIDIARY_OF, PARTICIPATES_IN, LOCATED_IN, OPERATES_IN.
- Missing info priorities: products, competitors, ownership, markets, locations.`,
	}

	registry[EntityTypeGov] = &EntityTypeDefinition{
		Key: EntityTypeGov,
		RequiredAspects: []string{
			"programs", "decisions", "mechanisms", "hierarchy", "market_influence",
		},
		ProfileSections: []models.ProfileSectionSpec{
			{Key: "identity_jurisdiction", Title: "Identity & Jurisdiction", Description: "What the institution is, legal status, hierarchy/chain-of-command, geographic/functional jurisdiction, population/territory coverage", Required: true, Order: 1},
			{Key: "mandate_policy_domain", Title: "Mandate & Policy Domain", Description: "Formal mandate, powers, policy remit, key legal instruments, regulatory authority, international obligations if relevant", Required: true, Order: 2},
			{Key: "leadership_political_landscape", Title: "Leadership & Political Landscape", Description: "Leadership, appointments, political context, electoral cycle, coalition stability, key stakeholders, legitimacy signals", Required: false, Order: 3},
			{Key: "budget_resources", Title: "Budget & Resources", Description: "Budget size and trends, staffing/resources, funding sources, spending priorities, procurement and capability constraints", Required: false, Order: 4},
			{Key: "policy_agenda", Title: "Policy Agenda", Description: "Current initiatives, reforms, legislative pipeline, stated priorities, implementation status and timelines", Required: true, Order: 5},
			{Key: "regulatory_impact", Title: "Regulatory Impact", Description: "Regulations/enforcement actions, compliance posture, fines/sanctions/investigations, who is impacted and how", Required: false, Order: 6},
			{Key: "geopolitical_position", Title: "Geopolitical Position", Description: "International posture, alliances/organizations, sanctions regime, trade agreements, major external constraints", Required: false, Order: 7},
		},
		ExtractionBlock: `ENTITY TYPE: GOV (government body)
- Required execution order:
  1) Resolve jurisdiction correctness and govType.
  2) Place in administrative hierarchy via ADMINISTERS (parent -> child).
  3) Resolve regulatory perimeter via REGULATES / CONTROLS_ACCESS.
  4) Resolve ties to markets/programs/entities affected by this body.
- Taxonomically correct edge types:
  - Required/primary: ADMINISTERS, REGULATES
  - Allowed when evidence exists: CONTROLS_ACCESS, IMPOSES_CONDITION, USES_MECHANISM, APPLIES_TO
  - Avoid: organizational ownership semantics when governance semantics fit better.
- Missing info priorities:
  - Missing parent body or subordinate agencies.
  - Missing regulated entities and scope qualifiers.
  - Missing formal legal basis/events for powers.`,
		ResearchBlock: `ENTITY TYPE: GOV
- Required execution order:
  1) Resolve jurisdiction correctness and govType.
  2) Place in administrative hierarchy via ADMINISTERS (parent -> child).
  3) Resolve regulatory perimeter via REGULATES / CONTROLS_ACCESS.
  4) Resolve ties to markets/programs/entities affected by this body.
- Taxonomically correct edge types:
  - Required/primary: ADMINISTERS, REGULATES
  - Allowed when evidence exists: CONTROLS_ACCESS, IMPOSES_CONDITION, USES_MECHANISM, APPLIES_TO
  - Avoid: organizational ownership semantics when governance semantics fit better.
- Missing info priorities:
  - Missing parent body or subordinate agencies.
  - Missing regulated entities and scope qualifiers.
  - Missing formal legal basis/events for powers.`,
		MissingInfoRules: []MissingInfoRule{
			{Trigger: TriggerFieldEmpty, FieldPath: "Labels.en", QuestionTemplate: "entity label/name in English for %s"},
			{Trigger: TriggerFieldEmpty, FieldPath: "MarketSegments", QuestionTemplate: "industry sector classification for %s"},
			{Trigger: TriggerFieldEmpty, FieldPath: "OrgSize", QuestionTemplate: "What is the size of %s? Find employee count or revenue to determine orgSize: sme, le, enterprise, mega. For government: federal G20 ministries=enterprise/mega, regional=le/enterprise."},
			{Trigger: TriggerRelationCountBelow, MinCount: 1, QuestionTemplate: "relationships with other entities for %s"},
			{Trigger: TriggerSeed, QuestionTemplate: "Who provides external assurance to %s? Auditor, certifier, or regulator. Use ASSURES(assurance_type:audit|certification|regulatory|inspection). Provide assurer and source."},
			{Trigger: TriggerSeed, QuestionTemplate: "[HIGH PRIORITY] What industry/sector does %s operate in? Return marketSegments as array of paths /segment/{domain}/{vertical}/{geo}."},
			{Trigger: TriggerSeed, QuestionTemplate: "[HIGH PRIORITY] What is the size of %s? Find employee count or revenue to determine orgSize: sme, le, enterprise, mega. For government: federal G20 ministries=enterprise/mega, regional=le/enterprise."},
		},
	}

	registry[EntityTypePerson] = &EntityTypeDefinition{
		Key: EntityTypePerson,
		RequiredAspects: []string{
			"family_ties", "companies_managed", "companies_owned", "business_locations",
		},
		ProfileSections: []models.ProfileSectionSpec{
			{Key: "overview", Title: "Overview", Description: "Brief biography, role, affiliations", Required: true, Order: 1},
			{Key: "family", Title: "Family & Relations", Description: "Family ties, RELATIVE_OF, RELATED_TO", Required: false, Order: 2},
			{Key: "governance", Title: "Governance", Description: "Companies managed (MANAGES), owned (OWNS), board roles (BOARD_MEMBER_OF)", Required: false, Order: 3},
			{Key: "locations", Title: "Business Locations", Description: "Primary business locations, OPERATES_IN", Required: false, Order: 4},
		},
		ExtractionBlock: `ENTITY TYPE: PERSON
- Required aspects to extract: family ties (RELATIVE_OF, RELATED_TO), companies managed (MANAGES), companies owned (OWNS), business locations (OPERATES_IN, LOCATED_IN).
- Taxonomically correct edge types:
  - Required/primary: RELATIVE_OF, RELATED_TO, MANAGES, OWNS, BOARD_MEMBER_OF, EMPLOYED_BY, FOUNDED, INVESTS_IN
  - Allowed when evidence exists: MEMBER_OF, AFFILIATED_WITH, OPERATES_IN, LOCATED_IN
  - Avoid: OWNS where target is PERSON; use RELATIVE_OF/RELATED_TO for person-person.
- Missing info priorities:
  - Missing family/relative links.
  - Missing companies under management or ownership.
  - Missing primary business locations.`,
		ResearchBlock: `ENTITY TYPE: PERSON
- Required aspects: family ties, companies managed, companies owned, business locations.
- Taxonomically correct edge types: RELATIVE_OF, RELATED_TO, MANAGES, OWNS, BOARD_MEMBER_OF, EMPLOYED_BY, FOUNDED, INVESTS_IN.
- Missing info priorities: family links, governance ties, locations.`,
		MissingInfoRules: []MissingInfoRule{
			{Trigger: TriggerFieldEmpty, FieldPath: "Labels.en", QuestionTemplate: "entity label/name in English for %s"},
			{Trigger: TriggerRelationMissing, RelationType: "RELATIVE_OF", QuestionTemplate: "Who are the direct public relatives (spouse/children/parents/siblings) of %s? Output RELATIVE_OF links with sources."},
			{Trigger: TriggerRelationMissing, RelationType: "RELATED_TO", QuestionTemplate: "Who are the publicly known close associates of %s (business partners/friends/classmates)? Output RELATED_TO links with evidence and sources."},
			{Trigger: TriggerRelationCountBelow, MinCount: 1, QuestionTemplate: "relationships with other entities for %s"},
			{Trigger: TriggerSeed, QuestionTemplate: "Who are the direct public relatives (spouse/children/parents/siblings) of %s? Output RELATIVE_OF links with sources."},
			{Trigger: TriggerSeed, QuestionTemplate: "Who are the publicly known close associates of %s (business partners/friends/classmates)? Output RELATED_TO links with evidence and sources."},
		},
	}

	registry[EntityTypeMarket] = &EntityTypeDefinition{
		Key: EntityTypeMarket,
		RequiredAspects: []string{
			"characteristics", "players", "sub_segments", "parent_segments", "locations",
		},
		ProfileSections: []models.ProfileSectionSpec{
			{Key: "overview", Title: "Overview", Description: "Market definition, scope, key characteristics", Required: true, Order: 1},
			{Key: "size_metrics", Title: "Market Size", Description: "TAM, current size, historical growth", Required: false, Order: 2},
			{Key: "growth_outlook", Title: "Growth Outlook", Description: "CAGR, growth drivers, forecasts", Required: false, Order: 3},
			{Key: "competition", Title: "Competitive Landscape", Description: "Key players, market shares, concentration (HHI, CR4)", Required: false, Order: 4},
			{Key: "trends", Title: "Trends & Drivers", Description: "Key trends, technology shifts, regulatory changes", Required: false, Order: 5},
			{Key: "barriers", Title: "Barriers & Risks", Description: "Entry barriers, risks, challenges", Required: false, Order: 6},
			{Key: "regional", Title: "Regional Dynamics", Description: "Geographic breakdown, regional differences", Required: false, Order: 7},
		},
		ExtractionBlock: `ENTITY TYPE: MARKET
- Required execution order:
  1) Place this market in hierarchy (parent/children) via HAS_MARKET_SEGMENT.
  2) Resolve market structure: marketDomain, marketVertical, marketChannels, tags.
  3) Resolve players and leaders via PARTICIPATES_IN (leaders via qualifiers.marketRole=leader|dominant).
  4) Resolve traded products via TRADED_IN when the market explicitly trades, benchmarks, or price-discovers products/commodities.
  5) Resolve market metrics (TAM/CAGR/CR4/HHI) and concentration evidence.
- Identify critical infrastructure dependencies: transport chokepoints (straits, canals, pipelines) through which this market's flows pass. Create DEPENDS_ON edges to these infrastructure nodes.
- Taxonomically correct edge types:
  - Required/primary: HAS_MARKET_SEGMENT, PARTICIPATES_IN
  - Use TRADED_IN only for PRODUCT -> MARKET trading/price-discovery context.
  - Allowed when evidence exists: REGULATES, DEPENDS_ON, INFLUENCES
  - Avoid for market decomposition: generic RELATED_TO without concrete semantics.
- Missing info priorities:
  - Missing parent/children market nodes.
  - Missing top players and dominance shares.
  - Missing sizing/concentration metrics with units and dates.`,
		ResearchBlock: `ENTITY TYPE: MARKET
- Required execution order:
  1) Place this market in hierarchy (parent/children) via HAS_MARKET_SEGMENT.
  2) Resolve market structure: marketDomain, marketVertical, marketChannels, tags.
  3) Resolve players and leaders via PARTICIPATES_IN (leaders via qualifiers.marketRole=leader|dominant).
  4) Resolve traded products via TRADED_IN when the market explicitly trades, benchmarks, or price-discovers products/commodities.
  5) Resolve market metrics (TAM/CAGR/CR4/HHI) and concentration evidence.
- Identify critical infrastructure dependencies: transport chokepoints through which this market's flows pass. Create DEPENDS_ON edges.
- Taxonomically correct edge types:
  - Required/primary: HAS_MARKET_SEGMENT, PARTICIPATES_IN
  - Use TRADED_IN only for PRODUCT -> MARKET trading/price-discovery context.
  - Allowed when evidence exists: REGULATES, DEPENDS_ON, INFLUENCES
  - Avoid for market decomposition: generic RELATED_TO without concrete semantics.
- Missing info priorities:
  - Missing parent/children market nodes.
  - Missing top players and dominance shares.
  - Missing sizing/concentration metrics with units and dates.`,
		MissingInfoRules: []MissingInfoRule{
			{Trigger: TriggerRelationMissing, RelationType: "HAS_MARKET_SEGMENT", QuestionTemplate: "What are the key immediate sub-markets of %s? Return 5-10 /market/... canonical paths and HAS_MARKET_SEGMENT edges (broader -> narrower; child path = parent path + one segment)."},
			{Trigger: TriggerRelationMissing, RelationType: "PARTICIPATES_IN", QuestionTemplate: "Who are the key players in %s and who dominates? Provide top players and (if available) market shares."},
			{Trigger: TriggerRelationMissing, RelationType: "TRADED_IN", QuestionTemplate: "Which products or commodities are explicitly traded, benchmarked, or price-discovered in %s? Return PRODUCT -> MARKET TRADED_IN edges only when this market truly trades or prices the product."},
			{Trigger: TriggerFieldEmpty, FieldPath: "MarketDomain", QuestionTemplate: fmt.Sprintf("What is the market domain and vertical for %%s? Return marketDomain (%s) and marketVertical (specific vertical) with sources. Prefer graphenrich-aligned verticals such as: %s.", graphenrich.SupportedMarketDomainsForPrompt(), graphenrich.DomainVerticalHintsForPrompt())},
			{Trigger: TriggerFieldEmpty, FieldPath: "MarketVertical", QuestionTemplate: "What is the market domain and vertical for %s? Return marketDomain and marketVertical with sources."},
			{Trigger: TriggerFieldEmpty, FieldPath: "State./metric/market/tam_usd", QuestionTemplate: "What is the TAM (total addressable market) in USD for %s (latest available year)? Provide value (number), year, and sources."},
			{Trigger: TriggerFieldEmpty, FieldPath: "State./metric/market/cagr_percent", QuestionTemplate: "What is the market CAGR percent for %s (provide period, e.g. 2024-2029)? Provide percent and sources."},
			{Trigger: TriggerFieldEmpty, FieldPath: "State./metric/market/cr4_percent", QuestionTemplate: "What is the market concentration for %s? Provide CR4 (top-4 share percent) and/or HHI (0..10000) with sources."},
			{Trigger: TriggerRelationCountBelow, MinCount: 1, QuestionTemplate: "relationships with other entities for %s"},
			{Trigger: TriggerSeed, QuestionTemplate: "Comprehensive information about entity '%s' is missing. Identify its type, location, key metrics, description, and relationships."},
			{Trigger: TriggerSeed, QuestionTemplate: fmt.Sprintf("What is the market domain and vertical for %%s? Return marketDomain (%s) and marketVertical (specific vertical) with sources. Prefer graphenrich-aligned verticals such as: %s.", graphenrich.SupportedMarketDomainsForPrompt(), graphenrich.DomainVerticalHintsForPrompt())},
			{Trigger: TriggerSeed, QuestionTemplate: "What is the TAM (total addressable market) in USD for %s (latest available year)? Provide value (number), year, and sources."},
			{Trigger: TriggerSeed, QuestionTemplate: "What is the market CAGR percent for %s (provide period, e.g. 2024-2029)? Provide percent and sources."},
			{Trigger: TriggerSeed, QuestionTemplate: "What is the market concentration for %s? Provide CR4 (top-4 share percent) and/or HHI (0..10000) with sources."},
			{Trigger: TriggerSeed, QuestionTemplate: "Who are the key players in %s and who dominates? Provide top players and (if available) market shares."},
		},
	}

	registry[EntityTypeProduct] = &EntityTypeDefinition{
		Key: EntityTypeProduct,
		RequiredAspects: []string{
			"producers", "consumers", "transformations", "markets",
		},
		ProfileSections: []models.ProfileSectionSpec{
			{Key: "overview", Title: "Overview", Description: "Product description, category, manufacturer", Required: true, Order: 1},
			{Key: "specifications", Title: "Specifications", Description: "Technical specs, features, variants", Required: false, Order: 2},
			{Key: "market_position", Title: "Market Position", Description: "Market share, positioning, target segment", Required: false, Order: 3},
			{Key: "supply_chain", Title: "Supply Chain", Description: "Key inputs, suppliers, manufacturing locations", Required: false, Order: 4},
			{Key: "alternatives", Title: "Alternatives & Substitutes", Description: "Competing products, substitutes", Required: false, Order: 5},
			{Key: "pricing", Title: "Pricing", Description: "Price points, pricing trends", Required: false, Order: 6},
		},
		ExtractionBlock: `ENTITY TYPE: PRODUCT
- Required execution order:
  1) Resolve canonical product taxonomy and aliases.
  2) Build upstream/downstream product chain via TRANSFORMS_INTO, ASSEMBLES_INTO, and USED_IN where functional input semantics are explicit.
  3) Connect actors/facilities that PRODUCE/CONSUME this product.
  4) Fill key supply-chain evidence (volumes/capacity/contracts where available).
- Identify critical infrastructure dependencies: transport chokepoints (straits, canals, pipelines) through which this product's supply chain flows. Create DEPENDS_ON edges to these infrastructure nodes.
- Taxonomically correct edge types:
  - Required/primary: TRANSFORMS_INTO, ASSEMBLES_INTO, USED_IN, PRODUCES, SUPPLIES_TO
  - Allowed when evidence exists: SUPPLIES_TO, DEPENDS_ON, TRADED_IN
  - Avoid: OWNS for actor-product commercial flows.
  - Avoid: PRODUCT -> PRODUCT with PRODUCES; use TRANSFORMS_INTO for processing/refining, ASSEMBLES_INTO for direct assembly, and USED_IN for strong operational input semantics.
  - Avoid: APPLIES_TO unless source is an explicit /mechanism/... fragment.
- Missing info priorities:
  - Missing upstream raw inputs.
  - Missing downstream transformed or directly assembled products.
  - Missing producer/facility nodes and chain continuity gaps.`,
		ResearchBlock: `ENTITY TYPE: PRODUCT
- Required execution order:
  1) Resolve canonical product taxonomy and aliases.
  2) Build upstream/downstream product chain via TRANSFORMS_INTO, ASSEMBLES_INTO, and USED_IN where functional input semantics are explicit.
  3) Connect actors/facilities that PRODUCE/CONSUME this product.
  4) Fill key supply-chain evidence (volumes/capacity/contracts where available).
- Identify critical infrastructure dependencies: transport chokepoints through which this product's supply chain flows. Create DEPENDS_ON edges.
- Taxonomically correct edge types:
  - Required/primary: TRANSFORMS_INTO, ASSEMBLES_INTO, USED_IN, PRODUCES, SUPPLIES_TO
  - Allowed when evidence exists: SUPPLIES_TO, DEPENDS_ON, TRADED_IN
  - Avoid: OWNS for actor-product commercial flows.
  - Avoid: PRODUCT -> PRODUCT with PRODUCES; use TRANSFORMS_INTO for processing/refining, ASSEMBLES_INTO for direct assembly, and USED_IN for strong operational input semantics.
  - Avoid: APPLIES_TO unless source is an explicit /mechanism/... fragment.
- Missing info priorities:
  - Missing upstream raw inputs.
  - Missing downstream transformed or directly assembled products.
  - Missing producer/facility nodes and chain continuity gaps.`,
		MissingInfoRules: []MissingInfoRule{
			{Trigger: TriggerFieldEmpty, FieldPath: "Labels.en", QuestionTemplate: "entity label/name in English for %s"},
			{Trigger: TriggerRelationMissing, RelationType: "TRANSFORMS_INTO", QuestionTemplate: "What are the upstream materials and downstream derived, directly assembled, or functionally used-in products for %s (produced from / used to produce / assembled into / used in)? Return TRANSFORMS_INTO for processing/refining, ASSEMBLES_INTO for direct assembly, and USED_IN for strong operational input semantics between products."},
			{Trigger: TriggerRelationCountBelow, MinCount: 1, QuestionTemplate: "relationships with other entities for %s"},
			{Trigger: TriggerSeed, QuestionTemplate: "What products and services does %s produce/offer? Provide a canonical list of outputs suitable for /product/... paths."},
			{Trigger: TriggerSeed, QuestionTemplate: "What key inputs/materials/components does %s consume? Provide a canonical list of inputs suitable for /product/... paths."},
			{Trigger: TriggerSeed, QuestionTemplate: "What are the upstream materials and downstream derived, directly assembled, or functionally used-in products for %s (produced from / used to produce / assembled into / used in)? Return TRANSFORMS_INTO for processing/refining, ASSEMBLES_INTO for direct assembly, and USED_IN for strong operational input semantics between products."},
		},
	}

	registry[EntityTypeFacility] = &EntityTypeDefinition{
		Key: EntityTypeFacility,
		RequiredAspects: []string{
			"owner_operator", "products", "deposits", "location", "suppliers_consumers",
		},
		ProfileSections: []models.ProfileSectionSpec{
			{Key: "overview", Title: "Overview", Description: "Facility description, type, operator", Required: true, Order: 1},
			{Key: "io_profile", Title: "Inputs & Outputs", Description: "Products consumed and produced", Required: false, Order: 2},
			{Key: "location", Title: "Location", Description: "Geographic placement, LOCATED_IN", Required: false, Order: 3},
			{Key: "capacity", Title: "Capacity", Description: "Throughput, capacity metrics", Required: false, Order: 4},
		},
		ExtractionBlock: `ENTITY TYPE: FACILITY
- Required execution order:
  1) Resolve lifecycle and location validity.
  2) Resolve facility IO profile: SUPPLIES_TO (product→org) for inputs, PRODUCES for outputs.
  3) Connect to counterparties/markets via SUPPLIES_TO, DEPENDS_ON, PARTICIPATES_IN.
  4) Add capacity/throughput metrics where available.
- Taxonomically correct edge types:
  - Required/primary: LOCATED_IN/OPERATES_IN, PRODUCES, SUPPLIES_TO
  - Allowed when evidence exists: SUPPLIES_TO, DEPENDS_ON, PARTICIPATES_IN, EXTRACTS_FROM
  - Avoid: treating facility as legal owner in OWNS edges.
- Missing info priorities:
  - Missing output products and consumed materials.
  - Missing lifecycle dates and current stage.
  - Missing capacity/volume metrics.`,
		ResearchBlock: `ENTITY TYPE: FACILITY
- Required execution order:
  1) Resolve lifecycle and location validity.
  2) Resolve facility IO profile: SUPPLIES_TO (product→org) for inputs, PRODUCES for outputs.
  3) Connect to counterparties/markets via SUPPLIES_TO, DEPENDS_ON, PARTICIPATES_IN.
  4) Add capacity/throughput metrics where available.
- Taxonomically correct edge types:
  - Required/primary: LOCATED_IN/OPERATES_IN, PRODUCES, SUPPLIES_TO
  - Allowed when evidence exists: SUPPLIES_TO, DEPENDS_ON, PARTICIPATES_IN, EXTRACTS_FROM
  - Avoid: treating facility as legal owner in OWNS edges.
- Missing info priorities:
  - Missing output products and consumed materials.
  - Missing lifecycle dates and current stage.
  - Missing capacity/volume metrics.`,
		MissingInfoRules: []MissingInfoRule{
			{Trigger: TriggerFieldEmpty, FieldPath: "Labels.en", QuestionTemplate: "entity label/name in English for %s"},
			{Trigger: TriggerFieldEmpty, FieldPath: "HomeLocation", QuestionTemplate: "facility geographic location for %s"},
			{Trigger: TriggerRelationCountBelow, MinCount: 1, QuestionTemplate: "relationships with other entities for %s"},
			{Trigger: TriggerSeed, QuestionTemplate: "Who provides external assurance to %s? Auditor, certifier, or regulator. Use ASSURES(assurance_type:audit|certification|regulatory|inspection). Provide assurer and source."},
			{Trigger: TriggerSeed, QuestionTemplate: "[HIGH PRIORITY] What industry/sector does %s operate in? Return marketSegments as array of paths /segment/{domain}/{vertical}/{geo}."},
			{Trigger: TriggerSeed, QuestionTemplate: "[HIGH PRIORITY] What is the size of %s? Find employee count or revenue to determine orgSize: sme, le, enterprise, mega."},
		},
	}

	registry[EntityTypeGeoRegion] = &EntityTypeDefinition{
		Key: EntityTypeGeoRegion,
		RequiredAspects: []string{
			"organizations", "markets", "deposits", "regulatory_environment",
		},
		ProfileSections: []models.ProfileSectionSpec{
			{Key: "overview", Title: "Overview", Description: "Region description, boundaries, significance", Required: true, Order: 1},
			{Key: "actors", Title: "Key Actors", Description: "Organizations operating in region", Required: false, Order: 2},
			{Key: "markets", Title: "Markets", Description: "Markets present in region", Required: false, Order: 3},
			{Key: "resources", Title: "Resources & Deposits", Description: "Deposits, resources in region", Required: false, Order: 4},
			{Key: "regulation", Title: "Regulatory Environment", Description: "Programs, mechanisms, jurisdictions", Required: false, Order: 5},
		},
		ExtractionBlock: `ENTITY TYPE: GEO_REGION
- Required aspects to extract: organizations/communities/deposits/programs located or operating in region, and regulatory environment.
- Taxonomically correct edge types:
  - Required/primary: LOCATED_IN (entity -> this geo), OPERATES_IN (org/person -> this geo or facility/deposit)
  - Allowed when evidence exists: ADMINISTERS (gov/program hierarchy into geo), REGULATES (gov bodies acting within jurisdiction)
  - Use explicit source for entities discovered in this region: organization/community/deposit/program/person should usually be the SOURCE and this GEO_REGION should be the TARGET.
  - Avoid: GEO_REGION as source of OWNS, MEMBER_OF, HAS_SUBGROUP, or generic placement edges; use LOCATED_IN/OPERATES_IN for presence in region.
- Missing info priorities:
  - Missing organizations in region.
  - Missing communities, deposits, and programs in region.
  - Missing regulatory context.`,
		ResearchBlock: `ENTITY TYPE: GEO_REGION
- Required aspects: organizations, communities, deposits, programs, regulatory environment.
- Taxonomically correct edge types: LOCATED_IN, OPERATES_IN, ADMINISTERS, REGULATES.
- GEO_REGION is often the semantic target for placement facts: emit entity -> LOCATED_IN/OPERATES_IN -> this geo, not this geo -> entity.
- Missing info priorities: actors, communities, deposits, programs, regulation.`,
		MissingInfoRules: []MissingInfoRule{
			{Trigger: TriggerFieldEmpty, FieldPath: "Labels.en", QuestionTemplate: "entity label/name in English for %s"},
			{Trigger: TriggerRelationCountBelow, MinCount: 1, QuestionTemplate: "relationships with other entities for %s"},
			{Trigger: TriggerSeed, QuestionTemplate: "Comprehensive information about entity '%s' is missing. Identify its type, location, key metrics, description, and relationships."},
		},
	}

	registry[EntityTypeProgram] = &EntityTypeDefinition{
		Key: EntityTypeProgram,
		RequiredAspects: []string{
			"administrator", "participants", "geography", "mechanisms",
		},
		ProfileSections: []models.ProfileSectionSpec{
			{Key: "identity_scope", Title: "Identity & Scope", Description: "What the program is, sponsoring entity, objectives, geographic/sectoral scope, timeline, budget authorization, legal basis", Required: true, Order: 1},
			{Key: "funding_resources", Title: "Funding & Resources", Description: "Budget size, funding sources, disbursement schedule, resource allocation, procurement mechanisms, cost structure", Required: true, Order: 2},
			{Key: "implementation_status", Title: "Implementation Status", Description: "Current phase, milestones achieved, timeline adherence, key deliverables, contractors/implementers, operational structure", Required: true, Order: 3},
			{Key: "stakeholder_ecosystem", Title: "Stakeholder Ecosystem", Description: "Sponsors, implementing agencies, beneficiaries, partners, critics, oversight bodies, political support", Required: false, Order: 4},
			{Key: "impact_outcomes", Title: "Impact & Outcomes", Description: "KPIs, measured results, economic/social impact, success metrics, evaluation findings, beneficiary feedback", Required: false, Order: 5},
			{Key: "risks_constraints", Title: "Risks & Constraints", Description: "Political risks, funding risks, implementation challenges, dependencies, regulatory hurdles, external factors", Required: false, Order: 6},
			{Key: "trajectory_outlook", Title: "Trajectory & Outlook", Description: "Future phases, expansion plans, sunset provisions, successor programs, strategic direction, sustainability", Required: false, Order: 7},
		},
		ExtractionBlock: `ENTITY TYPE: PROGRAM
- Required execution order:
  1) Resolve jurisdiction and administering entities.
  2) Resolve beneficiaries/targets and mechanism links.
  3) Resolve funding/timeline/status metrics.
- Taxonomically correct edge types:
  - Required/primary: ADMINISTERS
  - Allowed when evidence exists: INVESTS_IN, USES_MECHANISM, REGULATES
  - APPLIES_TO is valid when source is MECHANISM; for program context use explicit mechanism source when needed.
- Missing info priorities:
  - Missing admin body, timeline, and scope boundaries.`,
		ResearchBlock: `ENTITY TYPE: PROGRAM
- Required execution order:
  1) Resolve jurisdiction and administering entities.
  2) Resolve beneficiaries/targets and mechanism links.
  3) Resolve funding/timeline/status metrics.
- Taxonomically correct edge types:
  - Required/primary: ADMINISTERS
  - Allowed when evidence exists: INVESTS_IN, USES_MECHANISM, REGULATES
  - APPLIES_TO is valid when source is MECHANISM; for program context use explicit mechanism source when needed.
- Missing info priorities:
  - Missing admin body, timeline, and scope boundaries.`,
		MissingInfoRules: []MissingInfoRule{
			{Trigger: TriggerFieldEmpty, FieldPath: "Labels.en", QuestionTemplate: "entity label/name in English for %s"},
			{Trigger: TriggerRelationMissing, RelationType: "ADMINISTERS", QuestionTemplate: "Which government body administers %s? Return ADMINISTERS edge."},
			{Trigger: TriggerRelationCountBelow, MinCount: 1, QuestionTemplate: "relationships with other entities for %s"},
			{Trigger: TriggerSeed, QuestionTemplate: "Comprehensive information about entity '%s' is missing. Identify its type, location, key metrics, description, and relationships."},
		},
	}

	registry[EntityTypeCommunity] = &EntityTypeDefinition{
		Key: EntityTypeCommunity,
		RequiredAspects: []string{
			"org_affiliations", "members", "hierarchy", "locations",
		},
		ProfileSections: []models.ProfileSectionSpec{
			{Key: "overview", Title: "Overview", Description: "Community description, type, purpose", Required: true, Order: 1},
			{Key: "structure", Title: "Structure", Description: "HAS_SUBGROUP hierarchy", Required: false, Order: 2},
			{Key: "affiliations", Title: "Affiliations", Description: "AFFILIATED_WITH organizations", Required: false, Order: 3},
			{Key: "channels", Title: "Channels", Description: "OPERATES_VIA media channels", Required: false, Order: 4},
		},
		ExtractionBlock: `ENTITY TYPE: COMMUNITY (general)
- Required aspects: org affiliations (AFFILIATED_WITH), members (MEMBER_OF), hierarchy (HAS_SUBGROUP), locations (LOCATED_IN), market participation (PARTICIPATES_IN).
- Taxonomically correct edge types:
  - Required/primary: HAS_SUBGROUP, MEMBER_OF, AFFILIATED_WITH, PARTICIPATES_IN (community → market), LOCATED_IN (community → region)
  - Allowed when evidence exists: CONTROLS, INFLUENCES, OPERATES_VIA, DEPENDS_ON
- PARTICIPATES_IN: community → market (direction: community is source, market is target). If reversed, policy auto-corrects.
- LOCATED_IN: community → geo region. AFFILIATED_WITH: community → organization.
- Missing info priorities:
  - Missing branch/sub-group hierarchy.
  - Missing canonical members.
  - Missing affiliated formal entities.
  - Missing market participation (PARTICIPATES_IN) for trade/industry-association communities.`,
		ResearchBlock: `ENTITY TYPE: COMMUNITY
- Required aspects: org affiliations, members, hierarchy, locations, market participation.
- Taxonomically correct edge types: HAS_SUBGROUP, MEMBER_OF, AFFILIATED_WITH, PARTICIPATES_IN, LOCATED_IN.
- Missing info priorities: hierarchy, members, affiliations, markets.`,
		MissingInfoRules: []MissingInfoRule{
			{Trigger: TriggerFieldEmpty, FieldPath: "Labels.en", QuestionTemplate: "entity label/name in English for %s"},
			{Trigger: TriggerFieldEmpty, FieldPath: "CommunityType", QuestionTemplate: "What type of community is %s? Return one of trade|activist|professional|criminal|religious|ethnic|interest|dynasty|tribe|representatives|industry-association|key-community."},
			{Trigger: TriggerRelationCountBelow, MinCount: 1, QuestionTemplate: "relationships with other entities for %s"},
			{Trigger: TriggerSeed, QuestionTemplate: "Comprehensive information about entity '%s' is missing. Identify its type, location, key metrics, description, and relationships."},
		},
	}

	registry[EntityTypeCommunityDynasty] = &EntityTypeDefinition{
		Key: EntityTypeCommunityDynasty,
		RequiredAspects: []string{
			"lineage", "members", "influence_org_gov_market", "affiliated_entities", "channels",
		},
		ProfileSections: []models.ProfileSectionSpec{
			{Key: "identity_lineage", Title: "Identity & Lineage", Description: "Founding history, lineage, generational depth, succession principles, key branches, historical significance", Required: true, Order: 1},
			{Key: "economic_footprint", Title: "Economic Footprint", Description: "Holdings (companies, investments, real estate), key industries, estimated wealth magnitude (where publicly documented), asset classes", Required: true, Order: 2},
			{Key: "leadership_succession", Title: "Leadership & Succession", Description: "Current patriarch/matriarch or decision-makers, succession pipeline, family governance structures, generational roles", Required: true, Order: 3},
			{Key: "influence_network", Title: "Influence Network", Description: "Political connections, economic influence, board positions, philanthropic networks, advisory roles, international ties", Required: false, Order: 4},
			{Key: "legacy_reputation", Title: "Legacy & Reputation", Description: "Public perception, philanthropic initiatives, cultural contributions, brand and reputation management", Required: false, Order: 5},
			{Key: "trajectory_catalysts", Title: "Trajectory & Catalysts", Description: "Generational transitions, strategic moves, upcoming milestones, potential succession events, emerging opportunities or challenges", Required: false, Order: 6},
		},
		ExtractionBlock: `ENTITY TYPE: COMMUNITY_DYNASTY
- Required execution order:
  1) Resolve lineage structure via HAS_SUBGROUP.
  2) Resolve key persons and membership links (MEMBER_OF).
  3) Resolve ownership/control/influence ties to org/gov/market.
  4) Resolve affiliated formal entities and channels used (AFFILIATED_WITH / OPERATES_VIA).
- Taxonomically correct edge types:
  - Required/primary: HAS_SUBGROUP, MEMBER_OF, AFFILIATED_WITH
  - Allowed when evidence exists: OWNS, CONTROLS, INFLUENCES, OPERATES_VIA, DEPENDS_ON
  - Use OWNS when beneficial/legal ownership is explicit; use CONTROLS or INFLUENCES for informal, political, or soft-power authority.
  - Avoid: ORG -> dynasty/tribe OWNS inversions and generic untyped association edges.
- Missing info priorities:
  - Missing branch/sub-clan hierarchy.
  - Missing canonical key members.
  - Missing concrete institutional influence vectors.`,
		ResearchBlock: `ENTITY TYPE: COMMUNITY_DYNASTY
- Required execution order:
  1) Resolve lineage structure via HAS_SUBGROUP.
  2) Resolve key persons and membership links (MEMBER_OF).
  3) Resolve ownership/control/influence ties to org/gov/market.
  4) Resolve affiliated formal entities and channels used (AFFILIATED_WITH / OPERATES_VIA).
- Taxonomically correct edge types:
  - Required/primary: HAS_SUBGROUP, MEMBER_OF, AFFILIATED_WITH
  - Allowed when evidence exists: OWNS, CONTROLS, INFLUENCES, OPERATES_VIA, DEPENDS_ON
  - Use OWNS when beneficial/legal ownership is explicit; use CONTROLS or INFLUENCES for informal, political, or soft-power authority.
  - Avoid: ORG -> dynasty/tribe OWNS inversions and generic untyped association edges.
- Missing info priorities:
  - Missing branch/sub-clan hierarchy.
  - Missing canonical key members.
  - Missing concrete institutional influence vectors.`,
	}

	registry[EntityTypeCommunityTribe] = &EntityTypeDefinition{
		Key: EntityTypeCommunityTribe,
		RequiredAspects: []string{
			"heritage", "governance", "economic_activities", "political_engagement", "alliances",
		},
		ProfileSections: []models.ProfileSectionSpec{
			{Key: "identity_heritage", Title: "Identity & Heritage", Description: "Origins, cultural identity, language/traditions, geographic homeland, population estimates, sub-tribes/clans, historical context", Required: true, Order: 1},
			{Key: "traditional_governance", Title: "Traditional Governance", Description: "Leadership structures (councils, chiefs, elders), decision-making processes, succession mechanisms, traditional laws/customs", Required: true, Order: 2},
			{Key: "economic_activities", Title: "Economic Activities", Description: "Traditional livelihoods, modern economic participation, key industries, business interests, economic challenges and opportunities", Required: true, Order: 3},
			{Key: "political_engagement", Title: "Political Engagement", Description: "Political representation, relationship with central government, autonomy status, historical agreements, current political role", Required: false, Order: 4},
			{Key: "alliance_network", Title: "Alliance Network", Description: "Relations with other tribes, confederations, historical alliances, regional partnerships, diaspora communities", Required: false, Order: 5},
			{Key: "contemporary_dynamics", Title: "Contemporary Dynamics", Description: "Current challenges and opportunities, urbanization/migration patterns, cultural preservation efforts, modernization initiatives", Required: false, Order: 6},
		},
		ExtractionBlock: `ENTITY TYPE: COMMUNITY_TRIBE
- Required execution order:
  1) Resolve lineage structure via HAS_SUBGROUP (sub-tribes, clans).
  2) Resolve key persons and membership links (MEMBER_OF).
  3) Resolve ownership/control/influence ties to org/gov/market.
  4) Resolve affiliated formal entities and channels used (AFFILIATED_WITH / OPERATES_VIA).
- Taxonomically correct edge types:
  - Required/primary: HAS_SUBGROUP, MEMBER_OF, AFFILIATED_WITH
  - Allowed when evidence exists: OWNS, CONTROLS, INFLUENCES, OPERATES_VIA, DEPENDS_ON
  - Use OWNS when beneficial/legal ownership is explicit; use CONTROLS or INFLUENCES for informal, political, or soft-power authority.
  - Avoid: ORG -> tribe OWNS inversions and generic untyped association edges.
- Missing info priorities:
  - Missing branch/sub-clan hierarchy.
  - Missing canonical key members.
  - Missing concrete institutional influence vectors.`,
		ResearchBlock: `ENTITY TYPE: COMMUNITY_TRIBE
- Required execution order:
  1) Resolve lineage structure via HAS_SUBGROUP.
  2) Resolve key persons and membership links (MEMBER_OF).
  3) Resolve ownership/control/influence ties to org/gov/market.
  4) Resolve affiliated formal entities and channels used (AFFILIATED_WITH / OPERATES_VIA).
- Taxonomically correct edge types:
  - Required/primary: HAS_SUBGROUP, MEMBER_OF, AFFILIATED_WITH
  - Allowed when evidence exists: OWNS, CONTROLS, INFLUENCES, OPERATES_VIA, DEPENDS_ON
  - Use OWNS when beneficial/legal ownership is explicit; use CONTROLS or INFLUENCES for informal, political, or soft-power authority.
- Missing info priorities:
  - Missing branch/sub-clan hierarchy.
  - Missing canonical key members.
  - Missing concrete institutional influence vectors.`,
	}

	registry[EntityTypeDeposit] = &EntityTypeDefinition{
		Key: EntityTypeDeposit,
		RequiredAspects: []string{
			"resource_class", "operators", "downstream_products", "reserves_metrics",
		},
		ProfileSections: []models.ProfileSectionSpec{
			{Key: "overview", Title: "Overview", Description: "Deposit description, resource type", Required: true, Order: 1},
			{Key: "operators", Title: "Operators", Description: "Facilities extracting (EXTRACTS_FROM)", Required: false, Order: 2},
			{Key: "reserves", Title: "Reserves & Production", Description: "Reserves, grade, metrics", Required: false, Order: 3},
		},
		ExtractionBlock: `ENTITY TYPE: DEPOSIT
- Required execution order:
  1) Resolve resource class + geospatial placement.
  2) Resolve extraction operators/facilities (EXTRACTS_FROM from facility to deposit).
  3) Resolve downstream products (deposit/resource -> produced flows).
  4) Fill reserves/grade/production metrics and temporal validity.
- Taxonomically correct edge types:
  - Required/primary: LOCATED_IN, EXTRACTS_FROM (facility -> deposit), PRODUCES
  - Allowed when evidence exists: OWNS (rights), DEPENDS_ON
  - Avoid: reversing EXTRACTS_FROM direction.
- Missing info priorities:
  - Missing operator facilities.
  - Missing reserves/grade/annual output.
  - Missing downstream resource transformation path.`,
		ResearchBlock: `ENTITY TYPE: DEPOSIT
- Required execution order:
  1) Resolve resource class + geospatial placement.
  2) Resolve extraction operators/facilities (EXTRACTS_FROM from facility to deposit).
  3) Resolve downstream products (deposit/resource -> produced flows).
  4) Fill reserves/grade/production metrics and temporal validity.
- Taxonomically correct edge types:
  - Required/primary: LOCATED_IN, EXTRACTS_FROM (facility -> deposit), PRODUCES
  - Allowed when evidence exists: OWNS (rights), DEPENDS_ON
  - Avoid: reversing EXTRACTS_FROM direction.
- Missing info priorities:
  - Missing operator facilities.
  - Missing reserves/grade/annual output.
  - Missing downstream resource transformation path.`,
		MissingInfoRules: []MissingInfoRule{
			{Trigger: TriggerFieldEmpty, FieldPath: "Labels.en", QuestionTemplate: "entity label/name in English for %s"},
			{Trigger: TriggerRelationCountBelow, MinCount: 1, QuestionTemplate: "relationships with other entities for %s"},
			{Trigger: TriggerSeed, QuestionTemplate: "Comprehensive information about entity '%s' is missing. Identify its type, location, key metrics, description, and relationships."},
		},
	}

	registry[EntityTypeDecisionPoint] = &EntityTypeDefinition{
		Key: EntityTypeDecisionPoint,
		RequiredAspects: []string{
			"gatekeepers", "mechanisms", "impacted_entities",
		},
		ProfileSections: []models.ProfileSectionSpec{
			{Key: "overview", Title: "Overview", Description: "Decision point description", Required: true, Order: 1},
			{Key: "gatekeepers", Title: "Gatekeepers", Description: "GATEKEEPS_DECISION actors", Required: false, Order: 2},
			{Key: "mechanisms", Title: "Mechanisms", Description: "USES_MECHANISM", Required: false, Order: 3},
			{Key: "scope", Title: "Scope", Description: "CONTROLS_ACCESS, IMPOSES_CONDITION targets", Required: false, Order: 4},
		},
		ExtractionBlock: `ENTITY TYPE: DECISION_POINT
- Required execution order:
  1) Resolve gatekeepers (GATEKEEPS_DECISION).
  2) Resolve mechanisms used (USES_MECHANISM).
  3) Resolve impacted entities (CONTROLS_ACCESS / IMPOSES_CONDITION) — downstream impact mandatory.
- Taxonomically correct edge types:
  - Required/primary: GATEKEEPS_DECISION, USES_MECHANISM, CONTROLS_ACCESS
  - Allowed when evidence exists: IMPOSES_CONDITION
- Downstream: Must have CONTROLS_ACCESS or IMPOSES_CONDITION to concrete entities. Do not create without affected targets.
- Missing info priorities:
  - Missing gatekeeper actor.
  - Missing explicit mechanism object.
  - Missing controlled/affected target scope.`,
		ResearchBlock: `ENTITY TYPE: DECISION_POINT
- Required execution order:
  1) Resolve gatekeepers (GATEKEEPS_DECISION).
  2) Resolve mechanisms used (USES_MECHANISM).
  3) Resolve impacted entities (CONTROLS_ACCESS / IMPOSES_CONDITION) — downstream impact is mandatory.
- Taxonomically correct edge types:
  - Required/primary: GATEKEEPS_DECISION, USES_MECHANISM, CONTROLS_ACCESS
  - Allowed when evidence exists: IMPOSES_CONDITION
- Downstream: Each decision MUST have CONTROLS_ACCESS or IMPOSES_CONDITION to concrete org/facility/product/market. Do not create without affected targets.
- Missing info priorities:
  - Missing gatekeeper actor.
  - Missing explicit mechanism object.
  - Missing controlled/affected target scope (what/who is affected).`,
	}

	registry[EntityTypeMechanism] = &EntityTypeDefinition{
		Key: EntityTypeMechanism,
		RequiredAspects: []string{
			"users", "applies_to", "control_object",
		},
		ProfileSections: []models.ProfileSectionSpec{
			{Key: "overview", Title: "Overview", Description: "Mechanism description, category", Required: true, Order: 1},
			{Key: "users", Title: "Users", Description: "USES_MECHANISM", Required: false, Order: 2},
			{Key: "scope", Title: "Scope", Description: "APPLIES_TO targets", Required: false, Order: 3},
		},
		ExtractionBlock: `ENTITY TYPE: MECHANISM
- Required execution order:
  1) Resolve concrete mechanism category semantics.
  2) Resolve users/holders of mechanism (USES_MECHANISM).
  3) Resolve affected scope (APPLIES_TO) with evidence — downstream impact mandatory.
- Taxonomically correct edge types:
  - Required/primary: USES_MECHANISM, APPLIES_TO
  - Allowed when evidence exists: IMPOSES_CONDITION
- Downstream: Must have APPLIES_TO to concrete entities. Do not create without affected targets.
- Missing info priorities:
  - Missing who uses mechanism.
  - Missing where mechanism applies.
  - Missing concrete object of control (not generic wording).`,
		ResearchBlock: `ENTITY TYPE: MECHANISM
- Required execution order:
  1) Resolve concrete mechanism category semantics.
  2) Resolve users/holders of mechanism (USES_MECHANISM).
  3) Resolve affected scope (APPLIES_TO) with evidence — downstream impact is mandatory.
- Taxonomically correct edge types:
  - Required/primary: USES_MECHANISM, APPLIES_TO
  - Allowed when evidence exists: IMPOSES_CONDITION
- Downstream: Each mechanism MUST have APPLIES_TO to concrete org/facility/product/market. Do not create without affected targets.
- Missing info priorities:
  - Missing who uses mechanism.
  - Missing where mechanism applies (what/who is affected).
  - Missing concrete object of control (not generic wording).`,
	}

	registry[EntityTypeDomain] = &EntityTypeDefinition{
		Key: EntityTypeDomain,
		RequiredAspects: []string{
			"anchors", "actor_scope", "geo_scope", "object_scope",
		},
		ProfileSections: []models.ProfileSectionSpec{
			{Key: "overview", Title: "Overview", Description: "Domain scope and purpose", Required: true, Order: 1},
			{Key: "anchors", Title: "Anchors", Description: "First-order entities", Required: false, Order: 2},
		},
		ExtractionBlock: `ENTITY TYPE: DOMAIN
- Required execution order:
  1) Expand domain into concrete first-order anchors.
  2) Map actor/geo/object scope with domain-safe relation types.
  3) Surface strategic overlays only after anchor map is coherent.
- Taxonomically correct edge types:
  - Required/primary: FOCUSES_ON, INVOLVES_ACTOR, IN_GEO_SCOPE, ABOUT, AFFECTS, DEPENDS_ON
  - Allowed when evidence exists: SURFACES, RELATED_DOMAIN, BROADENS_TO, NARROWS_TO
- Missing info priorities:
  - Missing anchor entities by each required scope dimension.
  - Missing domain->entity evidence for top-risk edges.`,
		ResearchBlock: `ENTITY TYPE: DOMAIN
- Required execution order:
  1) Expand domain into concrete first-order anchors.
  2) Map actor/geo/object scope with domain-safe relation types.
  3) Surface strategic overlays only after anchor map is coherent.
- Taxonomically correct edge types:
  - Required/primary: FOCUSES_ON, INVOLVES_ACTOR, IN_GEO_SCOPE, ABOUT, AFFECTS, DEPENDS_ON
  - Allowed when evidence exists: SURFACES, RELATED_DOMAIN, BROADENS_TO, NARROWS_TO
- Missing info priorities:
  - Missing anchor entities by each required scope dimension.
  - Missing domain->entity evidence for top-risk edges.`,
		MissingInfoRules: []MissingInfoRule{
			{Trigger: TriggerFieldEmpty, FieldPath: "Labels.en", QuestionTemplate: "entity label/name in English for %s"},
			{Trigger: TriggerRelationCountBelow, MinCount: 1, QuestionTemplate: "relationships with other entities for %s"},
			{Trigger: TriggerSeed, QuestionTemplate: "Comprehensive information about entity '%s' is missing. Identify its type, location, key metrics, description, and relationships."},
		},
	}

	registry[EntityTypeAffectedZone] = &EntityTypeDefinition{
		Key: EntityTypeAffectedZone,
		RequiredAspects: []string{
			"condition_type", "affected_entities", "trigger_event", "metrics", "description",
		},
		ProfileSections: []models.ProfileSectionSpec{
			{Key: "overview", Title: "Overview", Description: "Politically-neutral description of the zone, boundaries, and condition type", Required: true, Order: 1},
			{Key: "characteristics", Title: "Characteristics", Description: "zone_severity_score, population_affected_estimate, condition_type", Required: false, Order: 2},
			{Key: "affected_entities", Title: "Affected Entities", Description: "Facilities, routes, organizations impacted (AFFECTS edges)", Required: false, Order: 3},
			{Key: "trigger_event", Title: "Trigger Event", Description: "Event that caused the zone (TRIGGERED_BY → EVENT)", Required: false, Order: 4},
		},
		ExtractionBlock: `ENTITY TYPE: AFFECTED_ZONE
- Canonical path: /zone/{geo}/{condition_type}/{slug}. geo: ISO2, composite (tr-sy), or regional token. slug: descriptive place/region name (northern-rakhine, gaza-strip, donbas). NEVER use "na" as slug.
- Required aspects: condition_type (Qualifiers), affected entities (AFFECTS), parent geo (LOCATED_IN), trigger event (TRIGGERED_BY), radius_km (approximate zone radius in km when reliably known).
- State metrics (politically-neutral): zone_severity_score (0-10), population_affected_estimate. radiusKm: dedicated field for zone radius in km. Be conservative and evidence-based—avoid overly broad interpretations; omit if uncertain.
- Taxonomically correct edge types:
  - Required/primary: LOCATED_IN (zone → GEO_REGION), AFFECTS (zone → FACILITY|ORGANIZATION|GEO_REGION for routes)
  - Allowed when evidence exists: TRIGGERED_BY (zone → EVENT)
- Missing info priorities:
  - Missing condition_type and severity metrics.
  - Missing politically-neutral description.
  - Missing AFFECTS links to facilities/routes/orgs.`,
		MissingInfoRules: []MissingInfoRule{
			{Trigger: TriggerFieldEmpty, FieldPath: "Labels.en", QuestionTemplate: "entity label/name in English for %s"},
			{Trigger: TriggerFieldEmpty, FieldPath: "RadiusKm", QuestionTemplate: "What is the approximate radius of the affected zone in kilometers (evidence-based, conservative estimate)? Return radiusKm (numeric, km). Avoid overly broad interpretations."},
			{Trigger: TriggerRelationCountBelow, MinCount: 1, QuestionTemplate: "relationships with other entities for %s"},
		},
		ResearchBlock: `ENTITY TYPE: AFFECTED_ZONE
- ZONE vs EVENT: ZONE = geographic area (conflict zone, disaster zone); EVENT = discrete occurrence (earthquake, flood incident). TRIGGERED_BY: zone→event.
- slug: descriptive place/region name. NEVER "na".
- Required aspects: condition_type, affected entities, trigger event, metrics, description, radius_km (zone radius in km when reliably known; be conservative, omit if uncertain).
- Taxonomically correct edge types: LOCATED_IN, AFFECTS, TRIGGERED_BY.
- State: zone_severity_score, population_affected_estimate. Description: politically-neutral.
- Missing info priorities: metrics, affected entities, trigger event.`,
	}

	registry[EntityTypeSupplyLink] = &EntityTypeDefinition{
		Key: EntityTypeSupplyLink,
		RequiredAspects: []string{
			"supplier", "customer", "product", "volume", "route",
		},
		ProfileSections: []models.ProfileSectionSpec{
			{Key: "overview", Title: "Overview", Description: "Supply link description: supplier, product, customer", Required: true, Order: 1},
			{Key: "flow_metrics", Title: "Flow Metrics", Description: "Volume, capacity, frequency, contract terms", Required: false, Order: 2},
			{Key: "route", Title: "Route & Logistics", Description: "Transport mode, route, chokepoints, lead time", Required: false, Order: 3},
			{Key: "risks", Title: "Risks & Dependencies", Description: "Concentration risk, alternatives, disruption history", Required: false, Order: 4},
		},
		ExtractionBlock: `ENTITY TYPE: SUPPLY_LINK
- Canonical path: /supply-link/{tenant}/{supplier-slug}/{product-slug}/{customer-slug}. Tenant-scoped.
- Required execution order:
  1) Resolve supplier and customer entities (SUPPLIES_TO direction: supplier → customer).
  2) Resolve product being supplied (CARRIES or via-product reference).
  3) Resolve volume/capacity metrics and contract evidence.
  4) Resolve logistics route and chokepoints (DEPENDS_ON infrastructure).
- Taxonomically correct edge types:
  - Required/primary: SUPPLIES_TO (supplier → customer), CARRIES (link → product)
  - Allowed when evidence exists: DEPENDS_ON (link → infrastructure/chokepoint), LOCATED_IN
  - Avoid: PRODUCES/SUPPLIES_TO on the link itself; those belong on supplier/customer nodes.
- Missing info priorities:
  - Missing supplier or customer identity.
  - Missing product specification.
  - Missing volume/capacity metrics.
  - Missing route and infrastructure dependencies.`,
		ResearchBlock: `ENTITY TYPE: SUPPLY_LINK
- Canonical path: /supply-link/{tenant}/{supplier-slug}/{product-slug}/{customer-slug}. Tenant-scoped.
- Required execution order:
  1) Resolve supplier and customer entities.
  2) Resolve product being supplied.
  3) Resolve volume/capacity metrics.
  4) Resolve logistics route and chokepoints.
- Taxonomically correct edge types:
  - Required/primary: SUPPLIES_TO, CARRIES
  - Allowed when evidence exists: DEPENDS_ON, LOCATED_IN
- Missing info priorities: supplier/customer, product, volume, route.`,
		MissingInfoRules: []MissingInfoRule{
			{Trigger: TriggerFieldEmpty, FieldPath: "Labels.en", QuestionTemplate: "description/label for supply link %s"},
			{Trigger: TriggerRelationMissing, RelationType: "SUPPLIES_TO", QuestionTemplate: "Who is the supplier and who is the customer in %s? Return SUPPLIES_TO edge from supplier org/facility to customer org/facility."},
			{Trigger: TriggerRelationMissing, RelationType: "CARRIES", QuestionTemplate: "What product is transported/supplied in %s? Return CARRIES edge to the product canonical path."},
			{Trigger: TriggerRelationCountBelow, MinCount: 1, QuestionTemplate: "relationships with other entities for %s"},
			{Trigger: TriggerSeed, QuestionTemplate: "What are the volume, capacity, and logistics details for %s? Include transport mode, route, lead time, and any chokepoint dependencies."},
		},
	}

	registry[EntityTypeTrack] = &EntityTypeDefinition{
		Key: EntityTypeTrack,
		RequiredAspects: []string{
			"kind", "id_scheme", "operator", "vessel_type", "flag",
		},
		ProfileSections: []models.ProfileSectionSpec{
			{Key: "identity", Title: "Identity & Classification", Description: "Kind (sea/air/land), ID scheme (MMSI/IMO/ICAO), external track ID, vessel/vehicle type, flag state", Required: true, Order: 1},
			{Key: "operator", Title: "Operator & Ownership", Description: "Operating entity, owner, fleet affiliation", Required: true, Order: 2},
			{Key: "operations", Title: "Operations", Description: "Ports/facilities frequented, routes, cargo types carried", Required: false, Order: 3},
			{Key: "status", Title: "Current Status", Description: "Last known position, operational status, draught, speed", Required: false, Order: 4},
		},
		ExtractionBlock: `ENTITY TYPE: TRACK
- Canonical path: /track/{kind}/{geo}/{id_scheme}-{id}. kind: sea|air|land. id_scheme: mmsi|imo|icao. geo: ISO2 flag or region.
- Required fields: kind, idScheme, externalTrackId.
- Sub-types: bulk_carrier, tanker, cargo, ore_carrier, container.
- Required execution order:
  1) Resolve identity: kind, idScheme, externalTrackId, vesselType, flag.
  2) Resolve operator/owner via OPERATES_AT, OWNED_BY.
  3) Resolve cargo/goods carried via CARRIES.
- Taxonomically correct edge types:
  - Required/primary: OPERATES_AT (track → facility/port), CARRIES (track → product), OWNED_BY (track → org)
  - Allowed when evidence exists: LOCATED_IN, DEPENDS_ON
  - Avoid: PRODUCES/SUPPLIES_TO for tracks; use CARRIES for cargo association.
- Missing info priorities:
  - Missing operator/owner.
  - Missing vessel type and flag.
  - Missing cargo and route information.`,
		ResearchBlock: `ENTITY TYPE: TRACK
- Canonical path: /track/{kind}/{geo}/{id_scheme}-{id}. kind: sea|air|land. id_scheme: mmsi|imo|icao.
- Required fields: kind, idScheme, externalTrackId.
- Sub-types: bulk_carrier, tanker, cargo, ore_carrier, container.
- Taxonomically correct edge types:
  - Required/primary: OPERATES_AT, CARRIES, OWNED_BY
  - Allowed when evidence exists: LOCATED_IN, DEPENDS_ON
- Missing info priorities: operator/owner, vessel type, flag, cargo, routes.`,
		MissingInfoRules: []MissingInfoRule{
			{Trigger: TriggerFieldEmpty, FieldPath: "Labels.en", QuestionTemplate: "entity label/name in English for %s"},
			{Trigger: TriggerFieldEmpty, FieldPath: "Qualifiers.vesselType", QuestionTemplate: "What is the vessel/vehicle type for %s? Return one of bulk_carrier|tanker|cargo|ore_carrier|container or other specific type."},
			{Trigger: TriggerFieldEmpty, FieldPath: "Qualifiers.flag", QuestionTemplate: "What is the flag state (ISO2) for %s?"},
			{Trigger: TriggerFieldEmpty, FieldPath: "Qualifiers.operator", QuestionTemplate: "Who is the operator of %s? Return the operating entity name and canonical path."},
			{Trigger: TriggerRelationMissing, RelationType: "OPERATES_AT", QuestionTemplate: "Which ports or facilities does %s frequently operate at? Return OPERATES_AT edges with facility/port canonical paths."},
			{Trigger: TriggerRelationMissing, RelationType: "CARRIES", QuestionTemplate: "What cargo or goods does %s carry? Return CARRIES edges to product canonical paths."},
			{Trigger: TriggerRelationMissing, RelationType: "OWNED_BY", QuestionTemplate: "Who owns %s? Return OWNED_BY edge to the owning organization."},
			{Trigger: TriggerRelationCountBelow, MinCount: 1, QuestionTemplate: "relationships with other entities for %s"},
			{Trigger: TriggerSeed, QuestionTemplate: "Comprehensive information about tracked entity '%s' is missing. Identify its kind (sea/air/land), ID scheme (mmsi/imo/icao), vessel type, flag, operator, and relationships."},
		},
	}

	registry[EntityTypeGeneral] = &EntityTypeDefinition{
		Key:             EntityTypeGeneral,
		RequiredAspects: []string{},
		ProfileSections: []models.ProfileSectionSpec{
			{Key: "overview", Title: "Overview", Description: "General description and purpose", Required: true, Order: 1},
			{Key: "structure", Title: "Structure", Description: "Organizational structure, key divisions", Required: false, Order: 2},
			{Key: "mandate", Title: "Mandate & Functions", Description: "Legal mandate, responsibilities, functions", Required: false, Order: 3},
			{Key: "regulations", Title: "Regulations", Description: "Key regulations issued or enforced", Required: false, Order: 4},
			{Key: "recent_actions", Title: "Recent Actions", Description: "Recent decisions, announcements, enforcement actions", Required: false, Order: 5},
		},
		ExtractionBlock: "",
		ResearchBlock:   "",
	}
}

// supplyChainExtractionBlockOverrides: when domain mode is supply_chain, use these blocks instead.
// No DEPENDS_ON; only PRODUCES, SUPPLIES_TO, TRANSFORMS_INTO, ASSEMBLES_INTO, USED_IN, EXTRACTS_FROM, LOCATED_IN, OPERATES_IN, HAS_MARKET_SEGMENT.
var supplyChainExtractionBlockOverrides = map[EntityTypeKey]string{
	EntityTypeOrganization: `ENTITY TYPE: ORGANIZATION (supply chain scope)
- Required aspects: products (PRODUCES), customers/suppliers (SUPPLIES_TO), locations (LOCATED_IN, OPERATES_IN).
- Use PRODUCES for outputs; SUPPLIES_TO for supplier→customer flow. Do NOT use DEPENDS_ON.
- Taxonomically correct edge types: PRODUCES, SUPPLIES_TO, LOCATED_IN, OPERATES_IN, PARTICIPATES_IN (ORG→MARKET).
- For input products: use SUPPLIES_TO from supplier org to this org (supplier is source). Avoid generic DEPENDS_ON.
- Missing info priorities: products, facilities, supply chain counterparties.`,
	EntityTypeMarket: `ENTITY TYPE: MARKET (supply chain scope)
- Required: HAS_MARKET_SEGMENT (hierarchy), PARTICIPATES_IN (players: ORG/FACILITY→MARKET).
- Do NOT use DEPENDS_ON. Use PARTICIPATES_IN for org/facility participation (direction: ORG→MARKET).
- Taxonomically correct edge types: HAS_MARKET_SEGMENT, PARTICIPATES_IN only.
- Missing info priorities: market segments, key players.`,
	EntityTypeProduct: `ENTITY TYPE: PRODUCT (supply chain scope)
- Required: TRANSFORMS_INTO / ASSEMBLES_INTO / USED_IN (upstream/downstream/product-use), PRODUCES (actor→product), SUPPLIES_TO (flow).
- Do NOT use DEPENDS_ON. Use TRANSFORMS_INTO for processing/refining steps, ASSEMBLES_INTO for direct assembly steps, USED_IN for strong operational input semantics, and PRODUCES/SUPPLIES_TO for actor links.
- Taxonomically correct edge types: TRANSFORMS_INTO, ASSEMBLES_INTO, USED_IN, PRODUCES, SUPPLIES_TO.
- Do NOT use PARTICIPATES_IN for PRODUCT -> MARKET. If market trading context is explicitly needed, use TRADED_IN only outside the operational supply-chain contract.
- Missing info priorities: upstream inputs, downstream products, producers, consumers.`,
	EntityTypeFacility: `ENTITY TYPE: FACILITY (supply chain scope)
- Required: PRODUCES (outputs), SUPPLIES_TO (product→org for inputs, supplier→customer for flow), LOCATED_IN, OPERATES_IN.
- Do NOT use DEPENDS_ON. Use SUPPLIES_TO for flow and input products (product supplies to org).
- Taxonomically correct edge types: PRODUCES, SUPPLIES_TO, LOCATED_IN, OPERATES_IN, EXTRACTS_FROM.
- Missing info priorities: products, counterparties, capacity.`,
}
