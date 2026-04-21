package agents

import (
	"fmt"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/contractsemantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/entity_semantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/graphenrich"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
	"strings"
)

func buildExtractionSpecializationModulesBlock(specialties []string, domainModes []string) string {
	return buildSpecializationModulesBlock(specialties, domainModes, true)
}

func buildResearchSpecializationModulesBlock(specialties []string) string {
	return buildSpecializationModulesBlock(specialties, nil, false)
}

func buildSpecializationModulesBlock(specialties []string, domainModes []string, includeDomainModes bool) string {
	specialties = taxonomy.NormalizeSpecialties(specialties)
	domainModes = contractsemantics.SortDomainModesForExecution(domainModes)

	var b strings.Builder
	b.WriteString("CORE + SPECIALIZATION MODULES:\n")
	b.WriteString("- Core supply_chain is always active.\n")
	if len(specialties) > 0 {
		b.WriteString(fmt.Sprintf("- requestedSpecialties: %s\n", strings.Join(specialties, ", ")))
	}
	if includeDomainModes && len(domainModes) > 0 {
		b.WriteString(fmt.Sprintf("- requestedDomainModes: %s\n", strings.Join(domainModes, ", ")))
	}
	b.WriteString("\n")

	if taxonomy.HasSpecialty(specialties, taxonomy.SpecialtyManagement) {
		b.WriteString("MODULE: management (executive leadership / board / founders)\n")
		b.WriteString("- Extract leadership and governance actors: CEO/CFO/COO/CTO/CIO/CMO/CHRO, President/Managing Director, Chair, founders.\n")
		b.WriteString("- Represent people as targetType=PERSON with canonicalPath /person/{slug}.\n")
		b.WriteString("- Use relation types MANAGES / EMPLOYED_BY / BOARD_MEMBER_OF / FOUNDED with grounded evidence.\n\n")
	}
	if taxonomy.HasSpecialty(specialties, taxonomy.SpecialtyProductsAndServices) {
		b.WriteString("MODULE: products_and_services (outputs + inputs + transformations)\n")
		b.WriteString("- For ORG/FAC: outputs via PRODUCES, inputs via SUPPLIES_TO(product→org).\n")
		b.WriteString("- For PRODUCT: upstream/downstream value-chain via transforms_into, plus direct final assembly via assembles_into.\n")
		b.WriteString("- Use canonical /product/{category}/{type}/{stage}/{slug} paths consistently.\n")
		b.WriteString("- Product/material names: prefer singular canonical terms; avoid brand prefixes unless officially part of the name.\n\n")
	}
	if taxonomy.HasSpecialty(specialties, taxonomy.SpecialtyEquity) {
		b.WriteString("MODULE: equity (ownership & control)\n")
		b.WriteString("- Prioritize ownership/control discovery: OWNS / SUBSIDIARY_OF / CONTROLS / INVESTS_IN.\n")
		b.WriteString("- Capture stake/voting/investment evidence when available.\n\n")
	}

	b.WriteString("MODULE: supply_chain (core business graph)\n")
	b.WriteString("- Discover complete supply chains as ordered node sequences.\n")
	b.WriteString("- Chain pattern: supplier -> product -> processor -> product -> customer -> ...\n")
	b.WriteString("- Nodes should alternate actor/product; include facilities where transformation occurs.\n")
	b.WriteString("- Include transport/logistics hints (transport_via, route waypoints, corridor facilities) when available.\n")
	b.WriteString("- For cross-border or chokepoint-sensitive chains, treat transport_via as expected unless route is genuinely unknown.\n")
	b.WriteString("- Prefer canonical ports/terminals/pipelines/straits/canals over vague logistics wording.\n")
	b.WriteString("- Always emit individual relations alongside chain-level output.\n\n")
	return b.String()
}

type facilityTaskSemantic string

const (
	facilityTaskSemanticNone        facilityTaskSemantic = ""
	facilityTaskSemanticRoute       facilityTaskSemantic = "route"
	facilityTaskSemanticSupply      facilityTaskSemantic = "supply"
	facilityTaskSemanticProcurement facilityTaskSemantic = "procurement"
)

func detectFacilityTaskSemantic(canonicalPath string, qualifiers map[string]interface{}, identifiers map[string]string) facilityTaskSemantic {
	switch DetectFacilitySemantic(canonicalPath, qualifiers, identifiers, nil) {
	case FacilitySemanticRoute:
		return facilityTaskSemanticRoute
	case FacilitySemanticProcurement:
		return facilityTaskSemanticProcurement
	case FacilitySemanticSupply:
		return facilityTaskSemanticSupply
	default:
		return facilityTaskSemanticNone
	}
}

func inferPromptFragmentType(canonicalPath string, fragmentType ah.FragmentClass) ah.FragmentClass {
	if strings.TrimSpace(string(fragmentType)) != "" {
		return fragmentType
	}
	cp := strings.TrimSpace(strings.ToLower(canonicalPath))
	switch {
	case strings.HasPrefix(cp, "/org/"), strings.HasPrefix(cp, "/gov/"):
		return ah.FragmentClassOrg
	case strings.HasPrefix(cp, "/fac/"):
		return ah.FragmentClassSite
	case strings.HasPrefix(cp, "/product/"):
		return ah.FragmentClassProduct
	case strings.HasPrefix(cp, "/market/"):
		return ah.FragmentClassMarket
	case strings.HasPrefix(cp, "/deposit/"):
		return ah.FragmentClassSite
	case strings.HasPrefix(cp, "/program/"):
		return ah.FragmentClassOrg
	case strings.HasPrefix(cp, "/loc/"), strings.HasPrefix(cp, "/region/"), strings.HasPrefix(cp, "/geo/"):
		return ah.FragmentClassGeo
	default:
		return ""
	}
}

func buildObjectQualifierContractBlock(canonicalPath string, fragmentType ah.FragmentClass, communityType string) string {
	_ = communityType
	return buildObjectQualifierContractBlockWithHints(canonicalPath, fragmentType, nil, nil, nil)
}

func buildObjectQualifierContractBlockWithHints(canonicalPath string, fragmentType ah.FragmentClass, qualifiers map[string]interface{}, identifiers map[string]string, _ map[string]string) string {
	fragmentType = inferPromptFragmentType(canonicalPath, fragmentType)
	allQualifiers := AllObjectQualifierStrings()
	allowedQualifiers := AllowedObjectQualifierStringsForType(fragmentType, canonicalPath)

	var b strings.Builder
	b.WriteString("OBJECT QUALIFIER CONTRACT (STRICT):\n")
	b.WriteString(fmt.Sprintf("- objectQualifier CLOSED ENUM (global): %s\n", strings.Join(allQualifiers, ", ")))
	if len(allowedQualifiers) > 0 {
		b.WriteString(fmt.Sprintf("- objectQualifier allowed for this target (%s, %s): %s\n", string(fragmentType), strings.TrimSpace(canonicalPath), strings.Join(allowedQualifiers, ", ")))
	} else {
		b.WriteString(fmt.Sprintf("- objectQualifier allowed for this target (%s, %s): none (omit objectQualifier)\n", string(fragmentType), strings.TrimSpace(canonicalPath)))
	}
	b.WriteString("- Never invent objectQualifier values outside CLOSED ENUM; if unsupported for this type/path, leave it unset.\n")

	switch detectFacilityTaskSemantic(canonicalPath, qualifiers, identifiers) {
	case facilityTaskSemanticRoute:
		b.WriteString("- Runtime-aligned facility lane detection: route.\n")
	case facilityTaskSemanticSupply:
		b.WriteString("- Runtime-aligned facility lane detection: supply.\n")
	case facilityTaskSemanticProcurement:
		b.WriteString("- Runtime-aligned facility lane detection: procurement.\n")
	}

	return b.String() + "\n"
}

func buildObjectTypeTaskSpecializationBlock(canonicalPath string, fragmentType ah.FragmentClass, communityType string) string {
	return buildObjectTypeTaskSpecializationBlockWithHints(canonicalPath, fragmentType, communityType, nil, nil, nil)
}

func buildObjectTypeTaskSpecializationBlockWithHints(canonicalPath string, fragmentType ah.FragmentClass, communityType string, qualifiers map[string]interface{}, identifiers map[string]string, spherePaths []string) string {
	key := entity_semantics.ResolveByPath(canonicalPath, fragmentType, "", communityType)
	def := entity_semantics.GetDefinition(key)

	var b strings.Builder
	b.WriteString("OBJECT-TYPE TASK SPECIALIZATION (single source: entity_semantics):\n")
	if def != nil && len(def.RequiredAspects) > 0 {
		b.WriteString(fmt.Sprintf("- Required aspects: %s\n", strings.Join(def.RequiredAspects, ", ")))
	}

	switch key {
	case entity_semantics.EntityTypeGov:
		b.WriteString("- Resolve govType + parentGovPath and enforce hierarchy edges via ADMINISTERS (superior -> subordinate).\n")
		b.WriteString("- Prioritize regulatory perimeter and affected entities: REGULATES for domestic jurisdiction oversight, CONTROLS_ACCESS for sanctions/extra-territorial restrictions, APPLIES_TO via mechanism.\n")
	case entity_semantics.EntityTypeOrganization:
		b.WriteString("- Separate operating business vs holding structure early; for holdings prioritize ownership map before product map.\n")
		b.WriteString("- For operating orgs, prioritize products/services, child orgs + their facilities, org-recipients, and market participation.\n")
	case entity_semantics.EntityTypeFacility:
		b.WriteString("- Prioritize lifecycle, location validity, IO profile (SUPPLIES_TO -> PRODUCES), and throughput/capacity signals.\n")
		switch detectFacilityTaskSemantic(canonicalPath, qualifiers, identifiers) {
		case facilityTaskSemanticRoute:
			b.WriteString("- Route-qualified FAC: resolve route regions and waypoint coordinates first (prefer Google geocoding-backed matches).\n")
			b.WriteString("- Route-qualified FAC: prioritize suppliers/logistics operators/customers by business flow semantics (DEPENDS_ON facet=supply|logistics, SUPPLIES_TO when flow is explicit).\n")
			b.WriteString("- Route-qualified FAC: avoid non-business detours (biography/family/person-centric discovery) and avoid route->route recursion by default.\n")
		case facilityTaskSemanticSupply:
			b.WriteString("- Supply-qualified FAC: prioritize producing facilities, org-recipients, products THIS fac produces, and transport chain constraints.\n")
			b.WriteString("- Supply-qualified FAC: prioritize ORG/FAC/PRODUCT/GEO targets with flow evidence; avoid person-centric and social-noise entities.\n")
		case facilityTaskSemanticProcurement:
			b.WriteString("- Procurement-qualified FAC: prioritize vendors, sourcing channels, procurement service providers, and sourcing regions.\n")
			b.WriteString("- Procurement-qualified FAC: prioritize business contract/dependency semantics (DEPENDS_ON/SUPPLIES_TO/PARTNERS_WITH) and supplier concentration signals.\n")
		default:
			b.WriteString("- If route semantics appear (route/route_path/route_waypoints), resolve route regions and waypoint coordinates (prefer Google geocoding-backed matches).\n")
			b.WriteString("- For route facilities, prioritize suppliers and logistics companies by business semantics (DEPENDS_ON facet=supply/logistics, SUPPLIES_TO where flow is explicit).\n")
		}
	case entity_semantics.EntityTypeProduct:
		b.WriteString("- Prioritize canonical product taxonomy, then upstream/downstream transformations (transforms_into), direct assembly steps (assembles_into), and actor links.\n")
	case entity_semantics.EntityTypeMarket:
		b.WriteString("- Prioritize market hierarchy (HAS_MARKET_SEGMENT), key players (PARTICIPATES_IN), and concentration/sizing metrics.\n")
	case entity_semantics.EntityTypeGeoRegion:
		b.WriteString("- GEO anchor: treat THIS fragment as the semantic target for placement. Discover entities in the region, then emit entity -> LOCATED_IN/OPERATES_IN -> THIS GEO using explicit source fields when needed.\n")
		b.WriteString("- Avoid expressing regional presence via OWNS, MEMBER_OF, HAS_SUBGROUP, or other social/governance edge types.\n")
	case entity_semantics.EntityTypeCommunity, entity_semantics.EntityTypeCommunityDynasty, entity_semantics.EntityTypeCommunityTribe:
		b.WriteString("- Prioritize community type, subgroup hierarchy (HAS_SUBGROUP), channels (OPERATES_VIA), and formal affiliations (AFFILIATED_WITH).\n")
	case entity_semantics.EntityTypeDeposit:
		b.WriteString("- Prioritize extraction operators/facilities (EXTRACTS_FROM), resource metrics, and downstream transformation chain.\n")
	case entity_semantics.EntityTypeProgram:
		b.WriteString("- Prioritize administrator hierarchy, mechanism links, beneficiaries, and timeline/status/funding facts.\n")
	case entity_semantics.EntityTypeDecisionPoint:
		b.WriteString("- Decision points must have gatekeepers, mechanisms, and downstream controlled targets.\n")
	case entity_semantics.EntityTypeMechanism:
		b.WriteString("- Mechanisms must have users (USES_MECHANISM) and affected entities (APPLIES_TO).\n")
	case entity_semantics.EntityTypeDomain:
		b.WriteString("- Expand domain into first-order concrete anchors before adding overlays.\n")
	}
	if len(spherePaths) > 0 {
		b.WriteString(fmt.Sprintf("- Active spheres: %s\n", strings.Join(spherePaths, ", ")))
		b.WriteString("- Prioritize discovery of DECISION_POINT and MECHANISM entities within these spheres.\n")
		b.WriteString("- For subordinate entities (ADMINISTERS targets, subsidiaries), prefer those operating in the same spheres.\n")
	}
	return b.String() + "\n"
}

// GovEntityRelationRulesBlock returns composable prompt text explaining
// GOV (/gov/...) vs non-GOV ORG relation type selection for MARKET/ORG targets.
// Included in any domain mode prompt where REGULATES and PARTICIPATES_IN are both available.
func GovEntityRelationRulesBlock() string {
	return `   - GOV vs ORG distinction (CRITICAL): entities with canonicalPath /gov/... are government bodies.
     For GOV -> MARKET (domestic): use REGULATES (not PARTICIPATES_IN, not DEPENDS_ON).
     For GOV -> ORG (same jurisdiction, regulatory oversight): use REGULATES when regulatory, ADMINISTERS when hierarchical.
     For GOV -> foreign ORG/MARKET (sanctions, trade restrictions, extra-territorial enforcement): use CONTROLS_ACCESS or IMPOSES_CONDITION, NOT REGULATES.
     For ORG -> MARKET: use PARTICIPATES_IN.
`
}

// PromptGovEntityRelationRulesBlock exposes the shared GOV-vs-ORG relation prompt rules
// so external planners can stay aligned with the composable Scout prompt layer.
func PromptGovEntityRelationRulesBlock() string {
	return GovEntityRelationRulesBlock()
}

func buildCoreBusinessTaxonomyBlock() string {
	domainContract := graphenrich.SupportedMarketDomainsForPrompt()
	verticalHints := graphenrich.DomainVerticalHintsForPrompt()
	return `BUSINESS TAXONOMY CONTRACT (single source for typical business objects):
- ORG/FAC/GOV:
  - marketSegments: /segment/{domain}/{vertical}/{geo} (geo = ISO2 or global/regional token)
  - marketDomain must use graphenrich canonical domains: ` + domainContract + `
  - marketVertical should align with canonical graphenrich verticals where inferable. Hints: ` + verticalHints + `
  - orgSize: sme | le | enterprise | mega | na
  - tags: 3-6 canonical /biz/... paths
- GOV (canonicalPath starts with /gov/):
  - govType: government | ministry | department | agency | authority | regulator | committee | service | bureau | office | council | commission
  - parentGovPath and ADMINISTERS hierarchy edges (superior -> subordinate)
- MARKET:
  - marketDomain, marketVertical, marketChannels, parentMarketPath
  - tags: 5-8 canonical slugs for dedup/search
  - key metrics (when available): /metric/market/tam_usd, /metric/market/cagr_percent, /metric/market/cr4_percent, /metric/market/hhi
- COMMUNITY:
  - communityType, communityPlatforms, communityEstimatedSize, affiliatedOrgPath
  - hierarchy via HAS_SUBGROUP (broader -> narrower), channels via OPERATES_VIA, market links via PARTICIPATES_IN`
}

// PromptCoreBusinessTaxonomyBlock exposes the shared business taxonomy contract
// used by Scout extraction/research prompts.
func PromptCoreBusinessTaxonomyBlock() string {
	return buildCoreBusinessTaxonomyBlock()
}
