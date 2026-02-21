package entity_semantics

import (
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
- Taxonomically correct edge types:
  - Required/primary: PRODUCES, CONSUMES, COMPETES_WITH, INVESTS_IN, OWNS, SUBSIDIARY_OF, PARTICIPATES_IN, LOCATED_IN, OPERATES_IN
  - Allowed when evidence exists: DEPENDS_ON, SUPPLIES_TO, CONTROLS, MANAGES, PARTNERS_WITH, REGULATES
  - For leadership: MANAGES, EMPLOYED_BY, BOARD_MEMBER_OF, FOUNDED (PERSON targets)
- Missing info priorities:
  - Missing products and services.
  - Missing competitors and market participation.
  - Missing ownership/investor structure.
  - Missing locations and deposits (for extractive orgs).`,
		ResearchBlock: `ENTITY TYPE: ORGANIZATION
- Required aspects: products, competitors, investors, owners, markets, deposits, locations.
- Taxonomically correct edge types: PRODUCES, CONSUMES, COMPETES_WITH, INVESTS_IN, OWNS, SUBSIDIARY_OF, PARTICIPATES_IN, LOCATED_IN, OPERATES_IN.
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
  3) Resolve players and leaders via PARTICIPATES_IN / DOMINATES.
  4) Resolve market metrics (TAM/CAGR/CR4/HHI) and concentration evidence.
- Taxonomically correct edge types:
  - Required/primary: HAS_MARKET_SEGMENT, PARTICIPATES_IN, DOMINATES
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
  3) Resolve players and leaders via PARTICIPATES_IN / DOMINATES.
  4) Resolve market metrics (TAM/CAGR/CR4/HHI) and concentration evidence.
- Taxonomically correct edge types:
  - Required/primary: HAS_MARKET_SEGMENT, PARTICIPATES_IN, DOMINATES
  - Allowed when evidence exists: REGULATES, DEPENDS_ON, INFLUENCES
  - Avoid for market decomposition: generic RELATED_TO without concrete semantics.
- Missing info priorities:
  - Missing parent/children market nodes.
  - Missing top players and dominance shares.
  - Missing sizing/concentration metrics with units and dates.`,
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
  2) Build upstream/downstream product chain via TRANSFORMS_INTO.
  3) Connect actors/facilities that PRODUCE/CONSUME this product.
  4) Fill key supply-chain evidence (volumes/capacity/contracts where available).
- Taxonomically correct edge types:
  - Required/primary: TRANSFORMS_INTO, PRODUCES, CONSUMES
  - Allowed when evidence exists: SUPPLIES_TO, DEPENDS_ON, PARTICIPATES_IN
  - Avoid: OWNS for actor-product commercial flows.
  - Avoid: PRODUCT -> PRODUCT with PRODUCES; use TRANSFORMS_INTO.
  - Avoid: APPLIES_TO unless source is an explicit /mechanism/... fragment.
- Missing info priorities:
  - Missing upstream raw inputs.
  - Missing downstream transformed products.
  - Missing producer/facility nodes and chain continuity gaps.`,
		ResearchBlock: `ENTITY TYPE: PRODUCT
- Required execution order:
  1) Resolve canonical product taxonomy and aliases.
  2) Build upstream/downstream product chain via TRANSFORMS_INTO.
  3) Connect actors/facilities that PRODUCE/CONSUME this product.
  4) Fill key supply-chain evidence (volumes/capacity/contracts where available).
- Taxonomically correct edge types:
  - Required/primary: TRANSFORMS_INTO, PRODUCES, CONSUMES
  - Allowed when evidence exists: SUPPLIES_TO, DEPENDS_ON, PARTICIPATES_IN
  - Avoid: OWNS for actor-product commercial flows.
  - Avoid: PRODUCT -> PRODUCT with PRODUCES; use TRANSFORMS_INTO.
  - Avoid: APPLIES_TO unless source is an explicit /mechanism/... fragment.
- Missing info priorities:
  - Missing upstream raw inputs.
  - Missing downstream transformed products.
  - Missing producer/facility nodes and chain continuity gaps.`,
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
  2) Resolve facility IO profile: CONSUMES inputs, PRODUCES outputs.
  3) Connect to counterparties/markets via SUPPLIES_TO, DEPENDS_ON, PARTICIPATES_IN.
  4) Add capacity/throughput metrics where available.
- Taxonomically correct edge types:
  - Required/primary: LOCATED_IN/OPERATES_IN, PRODUCES, CONSUMES
  - Allowed when evidence exists: SUPPLIES_TO, DEPENDS_ON, PARTICIPATES_IN, EXTRACTS_FROM
  - Avoid: treating facility as legal owner in OWNS edges.
- Missing info priorities:
  - Missing output products and consumed materials.
  - Missing lifecycle dates and current stage.
  - Missing capacity/volume metrics.`,
		ResearchBlock: `ENTITY TYPE: FACILITY
- Required execution order:
  1) Resolve lifecycle and location validity.
  2) Resolve facility IO profile: CONSUMES inputs, PRODUCES outputs.
  3) Connect to counterparties/markets via SUPPLIES_TO, DEPENDS_ON, PARTICIPATES_IN.
  4) Add capacity/throughput metrics where available.
- Taxonomically correct edge types:
  - Required/primary: LOCATED_IN/OPERATES_IN, PRODUCES, CONSUMES
  - Allowed when evidence exists: SUPPLIES_TO, DEPENDS_ON, PARTICIPATES_IN, EXTRACTS_FROM
  - Avoid: treating facility as legal owner in OWNS edges.
- Missing info priorities:
  - Missing output products and consumed materials.
  - Missing lifecycle dates and current stage.
  - Missing capacity/volume metrics.`,
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
- Required aspects to extract: organizations operating (OPERATES_IN, LOCATED_IN), markets present (PARTICIPATES_IN geo), deposits (DEPOSIT LOCATED_IN), regulatory environment (REGULATES, PROGRAM).
- Taxonomically correct edge types:
  - Required/primary: LOCATED_IN (entities in region), OPERATES_IN (orgs operating in region)
  - Allowed when evidence exists: REGULATES (gov bodies in region), PARTICIPATES_IN (markets)
  - Avoid: GEO_REGION as source of OWNS; use LOCATED_IN/OPERATES_IN for placement.
- Missing info priorities:
  - Missing organizations in region.
  - Missing markets and deposits.
  - Missing regulatory context.`,
		ResearchBlock: `ENTITY TYPE: GEO_REGION
- Required aspects: organizations, markets, deposits, regulatory environment.
- Taxonomically correct edge types: LOCATED_IN, OPERATES_IN, REGULATES, PARTICIPATES_IN.
- Missing info priorities: actors, markets, deposits, regulation.`,
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
  3) Resolve influence ties to org/gov/market (prefer CONTROLS/INFLUENCES over OWNS when appropriate).
  4) Resolve affiliated formal entities and channels used (AFFILIATED_WITH / OPERATES_VIA).
- Taxonomically correct edge types:
  - Required/primary: HAS_SUBGROUP, MEMBER_OF, AFFILIATED_WITH
  - Allowed when evidence exists: CONTROLS, INFLUENCES, OPERATES_VIA, DEPENDS_ON
  - Avoid: ORG -> dynasty/tribe OWNS inversions and generic untyped association edges.
- Missing info priorities:
  - Missing branch/sub-clan hierarchy.
  - Missing canonical key members.
  - Missing concrete institutional influence vectors.`,
		ResearchBlock: `ENTITY TYPE: COMMUNITY_DYNASTY
- Required execution order:
  1) Resolve lineage structure via HAS_SUBGROUP.
  2) Resolve key persons and membership links (MEMBER_OF).
  3) Resolve influence ties to org/gov/market (prefer CONTROLS/INFLUENCES over OWNS when appropriate).
  4) Resolve affiliated formal entities and channels used (AFFILIATED_WITH / OPERATES_VIA).
- Taxonomically correct edge types:
  - Required/primary: HAS_SUBGROUP, MEMBER_OF, AFFILIATED_WITH
  - Allowed when evidence exists: CONTROLS, INFLUENCES, OPERATES_VIA, DEPENDS_ON
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
  3) Resolve influence ties to org/gov/market (prefer CONTROLS/INFLUENCES over OWNS when appropriate).
  4) Resolve affiliated formal entities and channels used (AFFILIATED_WITH / OPERATES_VIA).
- Taxonomically correct edge types:
  - Required/primary: HAS_SUBGROUP, MEMBER_OF, AFFILIATED_WITH
  - Allowed when evidence exists: CONTROLS, INFLUENCES, OPERATES_VIA, DEPENDS_ON
  - Avoid: ORG -> tribe OWNS inversions and generic untyped association edges.
- Missing info priorities:
  - Missing branch/sub-clan hierarchy.
  - Missing canonical key members.
  - Missing concrete institutional influence vectors.`,
		ResearchBlock: `ENTITY TYPE: COMMUNITY_TRIBE
- Required execution order:
  1) Resolve lineage structure via HAS_SUBGROUP.
  2) Resolve key persons and membership links (MEMBER_OF).
  3) Resolve influence ties to org/gov/market (prefer CONTROLS/INFLUENCES over OWNS when appropriate).
  4) Resolve affiliated formal entities and channels used (AFFILIATED_WITH / OPERATES_VIA).
- Taxonomically correct edge types:
  - Required/primary: HAS_SUBGROUP, MEMBER_OF, AFFILIATED_WITH
  - Allowed when evidence exists: CONTROLS, INFLUENCES, OPERATES_VIA, DEPENDS_ON
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
