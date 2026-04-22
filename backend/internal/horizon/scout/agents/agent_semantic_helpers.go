package agents

import (
	"fmt"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
	"sort"
	"strings"
)

// Agent-side edge semantic groups and facility-specialization helpers.

// -----------------------------------------------------------------------------
// Edge semantic groups
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

func normalizeAgentRelationToken(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if rel, ok := ah.NormalizeRelation(raw); ok {
		return string(rel)
	}
	if dec, ok := models.NormalizeRelationName(raw); ok {
		if dec.Dropped || dec.Relation == "" {
			return ""
		}
		return string(dec.Relation)
	}
	return ""
}

var edgeTypeToSemanticGroup = map[ah.RelationName]string{
	ah.RelationNameProduces:       SemanticGroupSupplyChain,
	ah.RelationNameSupplies:       SemanticGroupSupplyChain,
	ah.RelationNameIncorporatedIn: SemanticGroupSupplyChain,
	ah.RelationNameOperates:       SemanticGroupSupplyChain,

	ah.RelationNameBeneficiallyOwns: SemanticGroupGovernance,
	ah.RelationNameSubsidiaryOf:     SemanticGroupGovernance,
	ah.RelationNameControlsVia:      SemanticGroupGovernance,
	ah.RelationNameInvestedIn:       SemanticGroupGovernance,
	ah.RelationNameHoldsBoardSeat:   SemanticGroupGovernance,
	ah.RelationNameMemberOf:         SemanticGroupGovernance,
	ah.RelationNameInfluences:       SemanticGroupGovernance,

	ah.RelationNameAssures:   SemanticGroupAssurance,
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
// Facility scouting lane classifier.
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
// Qualifier contracts for agent prompts.
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

// Category B — LLM prompt boundary: Scout prompts emit the canonical AH
// snake_case relation alphabet directly.

// AllEdgeTypesForPromptList returns the canonical AH relation vocabulary as a
// sorted slice of snake_case relation names.
func AllEdgeTypesForPromptList() []string {
	out := make([]string, 0, len(ah.RelationNames))
	for _, r := range ah.RelationNames {
		name := strings.TrimSpace(string(r))
		if name == "" {
			continue
		}
		out = append(out, name)
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

// DomainScaffoldRelationTypesString returns the closed AH relation subset used
// when a DOMAIN contract is expanded into evidence-/regime-like overlays.
func DomainScaffoldRelationTypesString() string {
	rels := []string{
		"influences", "disrupts", "subject_to", "exposes_to",
		"supported_by", "asserted_by",
	}
	out := make([]string, 0, len(rels))
	for _, r := range rels {
		out = append(out, fmt.Sprintf("%q", r))
	}
	return strings.Join(out, " | ")
}

// AllEdgeTypesList returns the closed list of canonical prompt relation labels.
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
