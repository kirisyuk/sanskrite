package agents

import (
	"fmt"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
	"sort"
	"strings"
)

// =============================================================================
// LEGACY HELPER PORTS
// =============================================================================
// During the AH taxonomy "Big Bang" migration, several helper functions and
// closed-vocabulary constants were removed from package `models` (their
// replacements live in agent-side prompts and the canonicalizer pipeline).
// Scout still needs the local concepts to drive prompt specialization,
// strategic-overlay critic gates, and relevance ranking, so we port the
// removed logic here under scout-package scope. These ports are intentionally
// faithful copies of the pre-removal definitions; see git history for
// `models/edge_semantic_groups.go`, `models/facility_semantic_contracts.go`,
// and `models/object_qualifier_contracts.go` for the original sources.

// -----------------------------------------------------------------------------
// Edge semantic groups (formerly models.SemanticGroup*).
// -----------------------------------------------------------------------------

const (
	SemanticGroupSupplyChain = "supply_chain"
	SemanticGroupGovernance  = "governance"
	SemanticGroupAssurance   = "assurance"
	SemanticGroupMarket      = "market"
	SemanticGroupStrategic   = "strategic"
	SemanticGroupDomain      = "domain"
	SemanticGroupPerson      = "person"
	SemanticGroupTransition  = "transition"
	SemanticGroupOther       = "other"
)

func SemanticGroupForEdgeType(t ah.RelationName) string {
	if g, ok := edgeTypeToSemanticGroup[t]; ok {
		return g
	}
	return SemanticGroupOther
}

var edgeTypeToSemanticGroup = map[ah.RelationName]string{
	ah.RelationNameProduces:         SemanticGroupSupplyChain,
	ah.RelationNameSupplies:         SemanticGroupSupplyChain,
	ah.RelationNameIncorporatedIn:   SemanticGroupSupplyChain,
	ah.RelationNameOperates:         SemanticGroupSupplyChain,

	ah.RelationNameBeneficiallyOwns: SemanticGroupGovernance,
	ah.RelationNameSubsidiaryOf:     SemanticGroupGovernance,
	ah.RelationNameControlsVia:      SemanticGroupGovernance,
	ah.RelationNameInvestedIn:       SemanticGroupGovernance,
	ah.RelationNameHoldsBoardSeat:  SemanticGroupGovernance,
	ah.RelationNameMemberOf:         SemanticGroupGovernance,
	ah.RelationNameInfluences:       SemanticGroupGovernance,

	ah.RelationNameAssures:  SemanticGroupAssurance,
	ah.RelationNameRegulates: SemanticGroupAssurance,

	ah.RelationNameListedOn: SemanticGroupMarket,

	ah.RelationNameImposes:   SemanticGroupStrategic,
	ah.RelationNameGatekeeps: SemanticGroupStrategic,
	ah.RelationNameWields:    SemanticGroupStrategic,

	ah.RelationNameDisrupts:      SemanticGroupDomain,
	ah.RelationNameConstrainedBy: SemanticGroupDomain,

	ah.RelationNameJointVentureWith: SemanticGroupPerson,

	ah.RelationNameMergedInto:        SemanticGroupTransition,
	ah.RelationNameSpunOut:           SemanticGroupTransition,
	ah.RelationNameAcquiredStakeFrom: SemanticGroupTransition,

	ah.RelationNameAlternateTo: SemanticGroupOther,
	ah.RelationNameManagedBy:   SemanticGroupOther,
}

// -----------------------------------------------------------------------------
// FacilitySemantic — closed business lane for facility scouting specialization.
// Ported from deleted models/facility_semantic_contracts.go.
// -----------------------------------------------------------------------------

type FacilitySemantic string

const (
	FacilitySemanticNone        FacilitySemantic = ""
	FacilitySemanticRoute       FacilitySemantic = "route"
	FacilitySemanticSupply      FacilitySemantic = "supply"
	FacilitySemanticProcurement FacilitySemantic = "procurement"
)

var (
	facilityRouteHints = []string{
		"route", "corridor", "pipeline", "shipping-lane",
		"logistics-lane", "transport-lane", "trade-lane", "canal",
	}
	facilitySupplyHints = []string{
		"supply", "supplier", "downstream", "upstream",
		"distribution", "fulfillment",
	}
	facilityProcurementHints = []string{
		"procurement", "sourcing", "purchasing", "tender", "vendor",
	}
)

func containsAnyHintToken(text string, hints []string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return false
	}
	for _, h := range hints {
		if strings.Contains(text, h) {
			return true
		}
	}
	return false
}

func valueContainsFacilityHint(raw interface{}, hints []string) bool {
	switch v := raw.(type) {
	case string:
		return containsAnyHintToken(v, hints)
	case []string:
		for _, item := range v {
			if containsAnyHintToken(item, hints) {
				return true
			}
		}
	case []interface{}:
		for _, item := range v {
			if valueContainsFacilityHint(item, hints) {
				return true
			}
		}
	case map[string]interface{}:
		for k, item := range v {
			if containsAnyHintToken(k, hints) {
				return true
			}
			if valueContainsFacilityHint(item, hints) {
				return true
			}
		}
	case map[string]string:
		for k, item := range v {
			if containsAnyHintToken(k, hints) || containsAnyHintToken(item, hints) {
				return true
			}
		}
	default:
		return containsAnyHintToken(fmt.Sprint(raw), hints)
	}
	return false
}

func firstObjectQualifierRawFromMaps(qualifiers map[string]interface{}, identifiers map[string]string) string {
	if identifiers != nil {
		for _, key := range []string{"objectQualifier", "object_qualifier", "qualifier", "businessQualifier", "business_qualifier"} {
			if v, ok := identifiers[key]; ok {
				s := strings.TrimSpace(v)
				if s != "" {
					return s
				}
			}
		}
	}
	if qualifiers != nil {
		for _, key := range []string{"objectQualifier", "object_qualifier", "qualifier", "businessQualifier", "business_qualifier"} {
			if v, ok := qualifiers[key]; ok {
				if s, ok := v.(string); ok {
					s = strings.TrimSpace(s)
					if s != "" {
						return s
					}
				}
			}
		}
	}
	return ""
}

// FacilitySemanticFromObjectQualifier maps closed object qualifiers to
// facility semantic lanes.
func FacilitySemanticFromObjectQualifier(q ObjectQualifier) FacilitySemantic {
	switch q {
	case ObjectQualifierRoute:
		return FacilitySemanticRoute
	case ObjectQualifierSupply:
		return FacilitySemanticSupply
	case ObjectQualifierProcurement, ObjectQualifierProcurer:
		return FacilitySemanticProcurement
	default:
		return FacilitySemanticNone
	}
}

// DetectFacilitySemantic resolves facility business lane using the same
// contract for runtime and prompts.
func DetectFacilitySemantic(canonicalPath string, qualifiers map[string]interface{}, identifiers map[string]string, labels map[string]string) FacilitySemantic {
	cp := strings.TrimSpace(strings.ToLower(canonicalPath))
	if !strings.HasPrefix(cp, "/fac/") {
		return FacilitySemanticNone
	}

	if raw := firstObjectQualifierRawFromMaps(qualifiers, identifiers); raw != "" {
		if sem := FacilitySemanticFromObjectQualifier(NormalizeObjectQualifier(raw)); sem != FacilitySemanticNone {
			return sem
		}
	}

	if containsAnyHintToken(cp, facilityRouteHints) {
		return FacilitySemanticRoute
	}
	if containsAnyHintToken(cp, facilityProcurementHints) {
		return FacilitySemanticProcurement
	}
	if containsAnyHintToken(cp, facilitySupplyHints) {
		return FacilitySemanticSupply
	}

	for k, v := range qualifiers {
		if containsAnyHintToken(k, facilityRouteHints) || valueContainsFacilityHint(v, facilityRouteHints) {
			return FacilitySemanticRoute
		}
		if containsAnyHintToken(k, facilityProcurementHints) || valueContainsFacilityHint(v, facilityProcurementHints) {
			return FacilitySemanticProcurement
		}
		if containsAnyHintToken(k, facilitySupplyHints) || valueContainsFacilityHint(v, facilitySupplyHints) {
			return FacilitySemanticSupply
		}
	}
	for k, v := range identifiers {
		if containsAnyHintToken(k, facilityRouteHints) || containsAnyHintToken(v, facilityRouteHints) {
			return FacilitySemanticRoute
		}
		if containsAnyHintToken(k, facilityProcurementHints) || containsAnyHintToken(v, facilityProcurementHints) {
			return FacilitySemanticProcurement
		}
		if containsAnyHintToken(k, facilitySupplyHints) || containsAnyHintToken(v, facilitySupplyHints) {
			return FacilitySemanticSupply
		}
	}
	for _, v := range labels {
		if containsAnyHintToken(v, facilityRouteHints) {
			return FacilitySemanticRoute
		}
		if containsAnyHintToken(v, facilityProcurementHints) {
			return FacilitySemanticProcurement
		}
		if containsAnyHintToken(v, facilitySupplyHints) {
			return FacilitySemanticSupply
		}
	}
	return FacilitySemanticNone
}

// DetectFacilitySemanticFromFragment applies DetectFacilitySemantic to a fragment.
func DetectFacilitySemanticFromFragment(fragment *models.HorizonFragment) FacilitySemantic {
	if fragment == nil {
		return FacilitySemanticNone
	}
	cp := strings.TrimSpace(fragment.CanonicalPath)
	if !strings.HasPrefix(strings.ToLower(cp), "/fac/") && fragment.Class != ah.FragmentClassSite {
		return FacilitySemanticNone
	}
	return DetectFacilitySemantic(cp, fragment.Qualifiers, fragment.Identifiers, fragment.Labels)
}

// -----------------------------------------------------------------------------
// ObjectQualifier — closed business qualifier vocabulary.
// Ported from deleted models/object_qualifier_contracts.go.
// -----------------------------------------------------------------------------

type ObjectQualifier string

const (
	ObjectQualifierNone ObjectQualifier = ""

	// Facility-oriented
	ObjectQualifierRoute       ObjectQualifier = "route"
	ObjectQualifierSupply      ObjectQualifier = "supply"
	ObjectQualifierProcurement ObjectQualifier = "procurement"
	ObjectQualifierHub         ObjectQualifier = "hub"
	ObjectQualifierWarehouse   ObjectQualifier = "warehouse"
	ObjectQualifierProduction  ObjectQualifier = "production"

	// Organization-oriented
	ObjectQualifierHolding     ObjectQualifier = "holding"
	ObjectQualifierOperator    ObjectQualifier = "operator"
	ObjectQualifierDistributor ObjectQualifier = "distributor"
	ObjectQualifierContractor  ObjectQualifier = "contractor"

	// Market-oriented
	ObjectQualifierRegulated       ObjectQualifier = "regulated"
	ObjectQualifierConcentrated    ObjectQualifier = "concentrated"
	ObjectQualifierImportDependent ObjectQualifier = "import_dependent"

	// Product-oriented
	ObjectQualifierCriticalInput ObjectQualifier = "critical_input"
	ObjectQualifierRegulatedGood ObjectQualifier = "regulated_good"

	// Government-oriented
	ObjectQualifierRegulator     ObjectQualifier = "regulator"
	ObjectQualifierProcurer      ObjectQualifier = "procurer"
	ObjectQualifierOwnerOperator ObjectQualifier = "owner_operator"

	// Deposit-oriented
	ObjectQualifierGreenfield ObjectQualifier = "greenfield"
	ObjectQualifierBrownfield ObjectQualifier = "brownfield"
	ObjectQualifierStrategic  ObjectQualifier = "strategic"
)

var objectQualifierAliases = map[string]ObjectQualifier{
	"route":            ObjectQualifierRoute,
	"corridor":         ObjectQualifierRoute,
	"pipeline":         ObjectQualifierRoute,
	"supply":           ObjectQualifierSupply,
	"supplier":         ObjectQualifierSupply,
	"distribution":     ObjectQualifierDistributor,
	"distributor":      ObjectQualifierDistributor,
	"procurement":      ObjectQualifierProcurement,
	"sourcing":         ObjectQualifierProcurement,
	"purchasing":       ObjectQualifierProcurement,
	"tender":           ObjectQualifierProcurement,
	"hub":              ObjectQualifierHub,
	"warehouse":        ObjectQualifierWarehouse,
	"production":       ObjectQualifierProduction,
	"holding":          ObjectQualifierHolding,
	"operator":         ObjectQualifierOperator,
	"contractor":       ObjectQualifierContractor,
	"regulated":        ObjectQualifierRegulated,
	"concentrated":     ObjectQualifierConcentrated,
	"import_dependent": ObjectQualifierImportDependent,
	"critical_input":   ObjectQualifierCriticalInput,
	"regulated_good":   ObjectQualifierRegulatedGood,
	"regulator":        ObjectQualifierRegulator,
	"procurer":         ObjectQualifierProcurer,
	"owner_operator":   ObjectQualifierOwnerOperator,
	"greenfield":       ObjectQualifierGreenfield,
	"brownfield":       ObjectQualifierBrownfield,
	"strategic":        ObjectQualifierStrategic,
}

func NormalizeObjectQualifier(raw string) ObjectQualifier {
	s := strings.TrimSpace(strings.ToLower(raw))
	if s == "" {
		return ObjectQualifierNone
	}
	if q, ok := objectQualifierAliases[s]; ok {
		return q
	}
	return ObjectQualifierNone
}

// AllObjectQualifierStrings returns the closed list of qualifier slugs
// (sorted for stable prompt rendering).
func AllObjectQualifierStrings() []string {
	out := make([]string, 0, len(objectQualifierAliases))
	seen := map[string]struct{}{}
	for _, v := range objectQualifierAliases {
		s := string(v)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// dependencyTypeWireLabelOverrides maps scout DependencyType values
// (which under AH point at canonical RelationName slugs like
// "beneficially_owns", "joint_venture_with", "operates", …) back to
// the pre-migration UPPER_SNAKE wire label that the LLM-facing scout
// agent prompts pin on. Without this map the prompt would emit
// "BENEFICIALLY_OWNS" / "JOINT_VENTURE_WITH" / "OPERATES" instead of
// "OWNS" / "PARTNERS_WITH" / "ADMINISTERS", breaking back-compat with
// historical agent prompts and downstream scout tests.
//
// MIGRATION NOTE (AH taxonomy / docs/taxonomy/migration_map.yaml +
// AGENTS.md): the prompt is an LLM contract; per the AGENTS.md
// "agent emits human-readable names" rule we keep the legacy
// UPPER_SNAKE wire labels in the relation enumeration and rely on the
// graph_intent decoder (legacyRelationAliases) to fold the LLM's
// answers back to canonical AH RelationName slugs at parse time.
var dependencyTypeWireLabelOverrides = map[ah.RelationName]string{
	ah.RelationNameBeneficiallyOwns:              "OWNS",
	ah.RelationNameSubsidiaryOf:      "SUBSIDIARY_OF",
	ah.RelationNameControlsVia:          "CONTROLS",
	ah.RelationNameInvestedIn:         "INVESTS_IN",
	ah.RelationNameHoldsBoardSeat:     "BOARD_MEMBER_OF",
	ah.RelationNameOperates:       "ADMINISTERS",
	ah.RelationNameJointVentureWith:      "PARTNERS_WITH",
	ah.RelationNameMergedInto:        "MERGED_INTO",
	ah.RelationNameSupplies:        "SUPPLIES_TO",
	ah.RelationNameProduces:          "PRODUCES",
	ah.RelationNameRegulates:         "REGULATES",
	ah.RelationNameAssures:           "ASSURES",
	ah.RelationNameImposes:  "IMPOSES_CONDITION",
	ah.RelationNameGatekeeps: "GATEKEEPS_DECISION",
	ah.RelationNameWields:     "USES_MECHANISM",
	ah.RelationNameDisrupts:           "AFFECTS",
	ah.RelationNameMemberOf:          "MEMBER_OF",
	ah.RelationNameListedOn:          "TRADED_IN",
	ah.RelationNameInfluences:        "INFLUENCES",
}

// wireLabelForDependencyType returns the UPPER_SNAKE wire label that
// scout LLM prompts use to advertise a DependencyType. Falls back to
// strings.ToUpper(string(rel)) for legacy snake_case literals that have
// no AH RelationName indirection (e.g. focuses_on, has_market_segment,
// extracts_from).
func wireLabelForDependencyType(rel ah.RelationName) string {
	if label, ok := dependencyTypeWireLabelOverrides[rel]; ok {
		return label
	}
	return strings.ToUpper(string(rel))
}

// AllEdgeTypesForPromptList returns the legacy AGENTHORDE edge-type vocabulary
// as a sorted slice of upper-case wire labels.
func AllEdgeTypesForPromptList() []string {
	rels := []ah.RelationName{
		ah.RelationNameBeneficiallyOwns, ah.RelationNameSubsidiaryOf, ah.RelationNameControlsVia,
		ah.RelationNameInvestedIn, ah.RelationNameHoldsBoardSeat, ah.RelationNameMemberOf,
		ah.RelationNameInfluences, ah.RelationNameOperates, ah.RelationNameManagedBy,
		ah.RelationNameMemberOf, ah.RelationNameJointVentureWith, ah.RelationNameJointVentureWith,
		ah.RelationNameJointVentureWith, ah.RelationNameInfluences, ah.RelationNameInfluences,
		ah.RelationNameInfluences, ah.RelationNameMemberOf, ah.RelationNameInfluences,

		ah.RelationNameSupplies, ah.RelationNameProduces, ah.RelationNameProduces,
		ah.RelationNameProduces, ah.RelationNameSupplies, ah.RelationNameProduces,
		ah.RelationNameOperates, ah.RelationNameWields, ah.RelationNameIncorporatedIn,
		ah.RelationNameSupplies,

		ah.RelationNameRegulates, ah.RelationNameAssures, ah.RelationNameAssures,
		ah.RelationNameImposes, ah.RelationNameGatekeeps,
		ah.RelationNameWields, ah.RelationNameImposes, ah.RelationNameImposes,

		ah.RelationNameListedOn, ah.RelationNameMemberOf, ah.RelationNameInfluences,
		ah.RelationNameInfluences, ah.RelationNameInfluences, ah.RelationNameAlternateTo,

		ah.RelationNameInfluences, ah.RelationNameInfluences, ah.RelationNameInfluences,
		ah.RelationNameInfluences, ah.RelationNameInfluences,
		ah.RelationNameDisrupts, ah.RelationNameInfluences, ah.RelationNameInfluences,
		ah.RelationNameInfluences, ah.RelationNameConstrainedBy, ah.RelationNameInfluences,

		ah.RelationNameMergedInto, ah.RelationNameInfluences, ah.RelationNameInfluences,
		ah.RelationNameMergedInto, ah.RelationNameMergedInto, ah.RelationNameSpunOut,
		ah.RelationNameAcquiredStakeFrom,
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(rels))
	for _, r := range rels {
		label := wireLabelForDependencyType(r)
		if label == "" {
			continue
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		out = append(out, label)
	}
	sort.Strings(out)
	return out
}

// AllEdgeTypesForPrompt renders the relation vocabulary as a quoted,
// pipe-separated enum string (matches the pre-removal edgesemantics format).
func AllEdgeTypesForPrompt() string {
	parts := AllEdgeTypesForPromptList()
	quoted := make([]string, 0, len(parts))
	for _, p := range parts {
		quoted = append(quoted, fmt.Sprintf("%q", p))
	}
	return strings.Join(quoted, " | ")
}

// DomainScaffoldRelationTypesString returns the closed list of "scaffold"
// relations (DEPENDS_ON family) as a quoted pipe-separated enum.
func DomainScaffoldRelationTypesString() string {
	rels := []string{
		"DEPENDS_ON", "INFLUENCES", "RELATED_TO", "ASSOCIATED_WITH",
		"AFFECTS", "ABOUT", "SURFACES", "INVOLVES_ACTOR", "IN_GEO_SCOPE",
	}
	out := make([]string, 0, len(rels))
	for _, r := range rels {
		out = append(out, fmt.Sprintf("%q", r))
	}
	return strings.Join(out, " | ")
}

// AllEdgeTypesList returns the closed list of upper-case relation labels.
func AllEdgeTypesList() []string { return AllEdgeTypesForPromptList() }

// AllowedObjectQualifierStringsForType returns the qualifier slugs that
// apply to a given fragment class. Best-effort port of the removed
// AllowedObjectQualifierStringsForType: returns the union per
// class lane plus "strategic" for deposits.
func AllowedObjectQualifierStringsForType(class ah.FragmentClass, canonicalPath string) []string {
	_ = canonicalPath
	var out []string
	switch class {
	case ah.FragmentClassSite:
		out = []string{
			string(ObjectQualifierRoute), string(ObjectQualifierSupply),
			string(ObjectQualifierProcurement), string(ObjectQualifierHub),
			string(ObjectQualifierWarehouse), string(ObjectQualifierProduction),
		}
	case ah.FragmentClassOrg:
		out = []string{
			string(ObjectQualifierHolding), string(ObjectQualifierOperator),
			string(ObjectQualifierDistributor), string(ObjectQualifierContractor),
			string(ObjectQualifierRegulator), string(ObjectQualifierProcurer),
			string(ObjectQualifierOwnerOperator),
		}
	case ah.FragmentClassMarket:
		out = []string{
			string(ObjectQualifierRegulated), string(ObjectQualifierConcentrated),
			string(ObjectQualifierImportDependent),
		}
	case ah.FragmentClassProduct:
		out = []string{
			string(ObjectQualifierCriticalInput), string(ObjectQualifierRegulatedGood),
		}
	default:
		return nil
	}
	sort.Strings(out)
	return out
}

// First version was a non-string forward decl; remove it now.
