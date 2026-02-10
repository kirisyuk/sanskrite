package agents

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// ResearchProfile defines specialized orchestration behavior for fragment types.
type ResearchProfile string

const (
	ResearchProfileGeneral               ResearchProfile = "GENERAL"
	ResearchProfileMarket                ResearchProfile = "MARKET"
	ResearchProfileProduct               ResearchProfile = "PRODUCT"
	ResearchProfileFacility              ResearchProfile = "FACILITY"
	ResearchProfileDeposit               ResearchProfile = "DEPOSIT"
	ResearchProfileGov                   ResearchProfile = "GOV"
	ResearchProfileCommunityDynastyTribe ResearchProfile = "COMMUNITY_DYNASTY_TRIBE"
	ResearchProfileProgram               ResearchProfile = "PROGRAM"
	ResearchProfileDomain                ResearchProfile = "DOMAIN"
	ResearchProfileDecisionPoint         ResearchProfile = "DECISION_POINT"
	ResearchProfileMechanism             ResearchProfile = "MECHANISM"
)

func ResolveResearchProfile(fragmentType models.FragmentType, canonicalPath string, communityType string) ResearchProfile {
	cp := strings.TrimSpace(strings.ToLower(canonicalPath))
	ct := strings.TrimSpace(strings.ToLower(communityType))
	ft := fragmentType
	if ft == "" {
		ft = inferFragmentTypeFromPath(cp)
	}

	if strings.HasPrefix(cp, "/gov/") {
		return ResearchProfileGov
	}
	if strings.HasPrefix(cp, "/decision/") || ft == models.FragmentTypeDecisionPoint {
		return ResearchProfileDecisionPoint
	}
	if strings.HasPrefix(cp, "/mechanism/") || ft == models.FragmentTypeMechanism {
		return ResearchProfileMechanism
	}
	if strings.HasPrefix(cp, "/domain/") || ft == models.FragmentTypeDomain {
		return ResearchProfileDomain
	}

	switch ft {
	case models.FragmentTypeMarket:
		return ResearchProfileMarket
	case models.FragmentTypeProduct:
		return ResearchProfileProduct
	case models.FragmentTypeFacility:
		return ResearchProfileFacility
	case models.FragmentTypeDeposit:
		return ResearchProfileDeposit
	case models.FragmentTypeProgram:
		return ResearchProfileProgram
	case models.FragmentTypeCommunity:
		if ct == "dynasty" || ct == "tribe" || strings.Contains(cp, "/dynasty/") || strings.Contains(cp, "/tribe/") {
			return ResearchProfileCommunityDynastyTribe
		}
	}
	return ResearchProfileGeneral
}

func inferFragmentTypeFromPath(cp string) models.FragmentType {
	switch {
	case strings.HasPrefix(cp, "/market/"):
		return models.FragmentTypeMarket
	case strings.HasPrefix(cp, "/product/"):
		return models.FragmentTypeProduct
	case strings.HasPrefix(cp, "/fac/"):
		return models.FragmentTypeFacility
	case strings.HasPrefix(cp, "/deposit/"):
		return models.FragmentTypeDeposit
	case strings.HasPrefix(cp, "/gov/"), strings.HasPrefix(cp, "/org/"):
		return models.FragmentTypeOrganization
	case strings.HasPrefix(cp, "/community/"):
		return models.FragmentTypeCommunity
	case strings.HasPrefix(cp, "/program/"):
		return models.FragmentTypeProgram
	case strings.HasPrefix(cp, "/domain/"):
		return models.FragmentTypeDomain
	case strings.HasPrefix(cp, "/decision/"):
		return models.FragmentTypeDecisionPoint
	case strings.HasPrefix(cp, "/mechanism/"):
		return models.FragmentTypeMechanism
	default:
		return ""
	}
}

func BuildResearchProfilePromptBlock(profile ResearchProfile) string {
	switch profile {
	case ResearchProfileMarket:
		return `SPECIALIZED RESEARCH PROFILE: MARKET
- Required execution order:
  1) Place this market in hierarchy (parent/children) via HAS_SUBMARKET.
  2) Resolve market structure: marketDomain, marketVertical, marketChannels, tags.
  3) Resolve players and leaders via PARTICIPATES_IN / DOMINATES.
  4) Resolve market metrics (TAM/CAGR/CR4/HHI) and concentration evidence.
- Taxonomically correct edge types:
  - Required/primary: HAS_SUBMARKET, PARTICIPATES_IN, DOMINATES
  - Allowed when evidence exists: REGULATES, DEPENDS_ON, INFLUENCES
  - Avoid for market decomposition: generic RELATED_TO without concrete semantics.
- Missing info priorities:
  - Missing parent/children market nodes.
  - Missing top players and dominance shares.
  - Missing sizing/concentration metrics with units and dates.`
	case ResearchProfileProduct:
		return `SPECIALIZED RESEARCH PROFILE: PRODUCT
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
  - Missing producer/facility nodes and chain continuity gaps.`
	case ResearchProfileFacility:
		return `SPECIALIZED RESEARCH PROFILE: FACILITY
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
  - Missing capacity/volume metrics.`
	case ResearchProfileDeposit:
		return `SPECIALIZED RESEARCH PROFILE: DEPOSIT
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
  - Missing downstream resource transformation path.`
	case ResearchProfileGov:
		return `SPECIALIZED RESEARCH PROFILE: GOV
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
  - Missing formal legal basis/events for powers.`
	case ResearchProfileCommunityDynastyTribe:
		return `SPECIALIZED RESEARCH PROFILE: COMMUNITY_DYNASTY_TRIBE
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
  - Missing concrete institutional influence vectors.`
	case ResearchProfileProgram:
		return `SPECIALIZED RESEARCH PROFILE: PROGRAM
- Required execution order:
  1) Resolve jurisdiction and administering entities.
  2) Resolve beneficiaries/targets and mechanism links.
  3) Resolve funding/timeline/status metrics.
- Taxonomically correct edge types:
  - Required/primary: ADMINISTERS
  - Allowed when evidence exists: INVESTS_IN, USES_MECHANISM, REGULATES
  - APPLIES_TO is valid when source is MECHANISM; for program context use explicit mechanism source when needed.
- Missing info priorities:
  - Missing admin body, timeline, and scope boundaries.`
	case ResearchProfileDecisionPoint:
		return `SPECIALIZED RESEARCH PROFILE: DECISION_POINT
- Required execution order:
  1) Resolve gatekeepers (GATEKEEPS_DECISION).
  2) Resolve mechanisms used (USES_MECHANISM).
  3) Resolve impacted entities (CONTROLS_ACCESS / IMPOSES_CONDITION).
- Taxonomically correct edge types:
  - Required/primary: GATEKEEPS_DECISION, USES_MECHANISM, CONTROLS_ACCESS
  - Allowed when evidence exists: IMPOSES_CONDITION
- Missing info priorities:
  - Missing gatekeeper actor.
  - Missing explicit mechanism object.
  - Missing controlled/affected target scope.`
	case ResearchProfileMechanism:
		return `SPECIALIZED RESEARCH PROFILE: MECHANISM
- Required execution order:
  1) Resolve concrete mechanism category semantics.
  2) Resolve users/holders of mechanism (USES_MECHANISM).
  3) Resolve affected scope (APPLIES_TO) with evidence.
- Taxonomically correct edge types:
  - Required/primary: USES_MECHANISM, APPLIES_TO
  - Allowed when evidence exists: IMPOSES_CONDITION
- Missing info priorities:
  - Missing who uses mechanism.
  - Missing where mechanism applies.
  - Missing concrete object of control (not generic wording).`
	case ResearchProfileDomain:
		return `SPECIALIZED RESEARCH PROFILE: DOMAIN
- Required execution order:
  1) Expand domain into concrete first-order anchors.
  2) Map actor/geo/object scope with domain-safe relation types.
  3) Surface strategic overlays only after anchor map is coherent.
- Taxonomically correct edge types:
  - Required/primary: FOCUSES_ON, INVOLVES_ACTOR, IN_GEO_SCOPE, ABOUT, AFFECTS, DEPENDS_ON
  - Allowed when evidence exists: SURFACES, RELATED_DOMAIN, BROADENS_TO, NARROWS_TO
- Missing info priorities:
  - Missing anchor entities by each required scope dimension.
  - Missing domain->entity evidence for top-risk edges.`
	default:
		return ""
	}
}
