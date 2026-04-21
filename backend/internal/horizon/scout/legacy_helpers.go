package scout

import (
	"fmt"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
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

func SemanticGroupForEdgeType(t DependencyType) string {
	if g, ok := edgeTypeToSemanticGroup[t]; ok {
		return g
	}
	return SemanticGroupOther
}

var edgeTypeToSemanticGroup = map[DependencyType]string{
	ah.RelationNameProduces:        SemanticGroupSupplyChain,
	ah.RelationNameSupplies:        SemanticGroupSupplyChain,
	DependencyType("transforms_into"): SemanticGroupSupplyChain,
	DependencyType("assembles_into"):  SemanticGroupSupplyChain,
	DependencyType("used_in"):         SemanticGroupSupplyChain,
	DependencyType("extracts_from"):   SemanticGroupSupplyChain,
	DependencyType("located_in"):      SemanticGroupSupplyChain,
	DependencyType("operates_in"):     SemanticGroupSupplyChain,

	ah.RelationNameBeneficiallyOwns: SemanticGroupGovernance,
	ah.RelationNameSubsidiaryOf:     SemanticGroupGovernance,
	ah.RelationNameControlsVia:      SemanticGroupGovernance,
	ah.RelationNameInvestedIn:       SemanticGroupGovernance,
	ah.RelationNameHoldsBoardSeat:   SemanticGroupGovernance,
	DependencyType("employed_by"):     SemanticGroupGovernance,
	DependencyType("founded"):       SemanticGroupGovernance,
	ah.RelationNameOperates:         SemanticGroupGovernance,

	ah.RelationNameAssures:    SemanticGroupAssurance,
	DependencyType("audited_by"): SemanticGroupAssurance,
	ah.RelationNameRegulates:  SemanticGroupAssurance,

	DependencyType("participates_in"): SemanticGroupMarket,
	ah.RelationNameListedOn:           SemanticGroupMarket,
	DependencyType("has_market_segment"): SemanticGroupMarket,
	DependencyType("dominates"):        SemanticGroupMarket,

	DependencyType("controls_access"): SemanticGroupStrategic,
	ah.RelationNameImposes:            SemanticGroupStrategic,
	ah.RelationNameGatekeeps:          SemanticGroupStrategic,
	ah.RelationNameWields:             SemanticGroupStrategic,
	DependencyType("applies_to"):      SemanticGroupStrategic,

	DependencyType("focuses_on"):      SemanticGroupDomain,
	DependencyType("domain_relation"):  SemanticGroupDomain,
	DependencyType("related_domain"):   SemanticGroupDomain,
	DependencyType("broadens_to"):      SemanticGroupDomain,
	DependencyType("narrows_to"):       SemanticGroupDomain,
	ah.RelationNameDisrupts:           SemanticGroupDomain,
	DependencyType("about"):           SemanticGroupDomain,
	DependencyType("surfaces"):        SemanticGroupDomain,
	DependencyType("involves_actor"):  SemanticGroupDomain,
	DependencyType("in_geo_scope"):    SemanticGroupDomain,

	DependencyType("relative_of"):      SemanticGroupPerson,
	DependencyType("associated_with"):  SemanticGroupPerson,
	DependencyType("related_to"):       SemanticGroupPerson,
	ah.RelationNameMemberOf:           SemanticGroupPerson,
	DependencyType("affiliated_with"): SemanticGroupPerson,
	ah.RelationNameJointVentureWith:   SemanticGroupPerson,
	DependencyType("works_for"):       SemanticGroupPerson,
	DependencyType("key_person"):      SemanticGroupPerson,
	DependencyType("has_key_person"):  SemanticGroupPerson,

	DependencyType("corporate_transition"): SemanticGroupTransition,
	DependencyType("renamed_to"):            SemanticGroupTransition,
	DependencyType("rebranded_to"):          SemanticGroupTransition,
	DependencyType("legal_successor_of"):  SemanticGroupTransition,
	ah.RelationNameMergedInto:             SemanticGroupTransition,
	DependencyType("spun_off_from"):        SemanticGroupTransition,
	DependencyType("acquired_by"):          SemanticGroupTransition,

	DependencyType("competes_with"): SemanticGroupOther,
	ah.RelationNameInfluences:       SemanticGroupOther,
	DependencyType("depends_on"):    SemanticGroupOther,
	DependencyType("manages"):       SemanticGroupOther,
	DependencyType("operates_via"):  SemanticGroupOther,
	DependencyType("has_subgroup"):  SemanticGroupOther,
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

// AllEdgeTypesForPromptList returns the legacy AGENTHORDE edge-type vocabulary
// as a sorted slice of upper-case wire labels.
func AllEdgeTypesForPromptList() []string {
	rels := []DependencyType{
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
		label := strings.ToUpper(string(r))
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
func AllowedObjectQualifierStringsForType(class FragmentType, canonicalPath string) []string {
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

// SymmetricEdgeTypes maps the relations whose source/target are
// interchangeable (the relation is reciprocal). Mirror of the legacy
// models.SymmetricEdgeTypes map.
var SymmetricEdgeTypes = map[DependencyType]bool{
	DependencyType("competes_with"):   true,
	DependencyType("relative_of"):     true,
	DependencyType("associated_with"): true,
	DependencyType("domain_relation"):  true,
}

// IsAllowedObjectQualifierForType reports whether qualifier `q` is in the
// allowed set for `class` (best-effort port; uses
// AllowedObjectQualifierStringsForType as the source of truth).
func IsAllowedObjectQualifierForType(class FragmentType, canonicalPath string, q ObjectQualifier) bool {
	if q == ObjectQualifierNone {
		return true
	}
	allowed := AllowedObjectQualifierStringsForType(class, canonicalPath)
	target := string(q)
	for _, s := range allowed {
		if s == target {
			return true
		}
	}
	return false
}

// IsDomainScaffoldRelationType reports whether `t` is a "scaffold"
// (DEPENDS_ON / domain) relation. Mirror of the legacy
// edgesemantics.IsDomainScaffoldRelationType helper.
func IsDomainScaffoldRelationType(t DependencyType) bool {
	switch t {
	case DependencyType("depends_on"), ah.RelationNameInfluences, DependencyType("related_to"),
		DependencyType("associated_with"), ah.RelationNameDisrupts, DependencyType("about"),
		DependencyType("surfaces"), DependencyType("involves_actor"), DependencyType("in_geo_scope"),
		DependencyType("focuses_on"), DependencyType("domain_relation"), DependencyType("related_domain"):
		return true
	}
	return false
}

// IsMarketRelationType reports whether `t` is a market-anchor relation.
// Mirror of the legacy edgesemantics.IsMarketRelationType helper.
func IsMarketRelationType(t DependencyType) bool {
	switch t {
	case DependencyType("participates_in"), ah.RelationNameListedOn,
		DependencyType("has_market_segment"), DependencyType("has_submarket"),
		DependencyType("dominates"), DependencyType("competes_with"):
		return true
	}
	return false
}

// ProductToProductEdgeTypeFromPaths selects the canonical product->product
// edge type. ASSEMBLES_INTO is reserved for nearest-step component /
// subassembly -> assembled product links; everything else stays
// TRANSFORMS_INTO. Mirror of the legacy models helper.
func ProductToProductEdgeTypeFromPaths(sourcePath, targetPath string) DependencyType {
	if shouldPreferAssemblesIntoByProductPaths(sourcePath, targetPath) {
		return DependencyType("assembles_into")
	}
	return DependencyType("transforms_into")
}

func shouldPreferAssemblesIntoByProductPaths(sourcePath, targetPath string) bool {
	src, srcOK := parseProductCanonicalParts(sourcePath)
	tgt, tgtOK := parseProductCanonicalParts(targetPath)
	if !srcOK || !tgtOK {
		return false
	}
	srcCategory := strings.ToLower(strings.TrimSpace(src.Category))
	tgtCategory := strings.ToLower(strings.TrimSpace(tgt.Category))
	srcStage := strings.ToLower(strings.TrimSpace(src.Stage))
	tgtStage := strings.ToLower(strings.TrimSpace(tgt.Stage))

	targetLooksAssembled := tgtCategory == "finished" || tgtStage == "assembly"
	sourceLooksSubassembly := srcCategory == "component" || srcStage == "assembly" || srcStage == "manufacturing" || srcCategory == "finished"
	if !targetLooksAssembled || !sourceLooksSubassembly {
		return false
	}
	if srcCategory == "raw" {
		return false
	}
	return true
}

func parseProductCanonicalParts(path string) (*taxonomy.ProductPathParts, bool) {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/product/") {
		return nil, false
	}
	parts, err := taxonomy.ParseCanonicalPathParts(path)
	if err != nil || parts == nil || parts.Product == nil {
		return nil, false
	}
	return parts.Product, true
}

// ProductStageRankFromCanonicalPath returns the stage-order rank for a
// product canonical path. Used by supply-chain assembly to compare upstream
// vs downstream products. Mirror of the legacy models helper.
func ProductStageRankFromCanonicalPath(path string) (int, bool) {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/product/") {
		return 0, false
	}
	parts, err := taxonomy.ParseCanonicalPathParts(path)
	if err != nil || parts == nil || parts.Product == nil {
		return 0, false
	}
	stage := strings.ToLower(strings.TrimSpace(parts.Product.Stage))
	if stage == "" {
		return 0, false
	}
	order := taxonomy.GetProductStageOrder()
	for i, s := range order {
		if strings.ToLower(strings.TrimSpace(s)) == stage {
			return i, true
		}
	}
	return 0, false
}

// IsProductTransitionEdgeType reports whether `edgeType` is a
// product-to-product transition (TRANSFORMS_INTO / ASSEMBLES_INTO /
// USED_IN) used by the supply-chain continuity layer. Mirror of the
// legacy IsProductTransitionEdgeType helper.
func IsProductTransitionEdgeType(edgeType DependencyType) bool {
	switch edgeType {
	case DependencyType("transforms_into"), DependencyType("assembles_into"), DependencyType("used_in"):
		return true
	default:
		return false
	}
}

// HasOwnershipEvidence returns true when evidence indicates legal/equity
// ownership. Mirror of the legacy models.HasOwnershipEvidence helper.
func HasOwnershipEvidence(reason string, qualifiers map[string]interface{}, metrics map[string]models.MetricValue) bool {
	ownershipMetricKeys := []string{
		"ownership_percent", "voting_percent", "equity_share",
		"stake_percent", "controlling_interest",
	}
	for _, k := range ownershipMetricKeys {
		if mv, ok := metrics[k]; ok && mv.Value > 0 {
			return true
		}
	}
	ownershipQualifierKeys := []string{
		"ownership_percent", "voting_percent", "equity_share",
		"stake_percent", "ownershipEvidence", "isOwner",
	}
	for _, k := range ownershipQualifierKeys {
		raw, ok := qualifiers[k]
		if !ok {
			continue
		}
		switch v := raw.(type) {
		case bool:
			if v {
				return true
			}
		case float64:
			if v > 0 {
				return true
			}
		case int:
			if v > 0 {
				return true
			}
		case string:
			s := strings.ToLower(strings.TrimSpace(v))
			if s == "" {
				continue
			}
			if s == "true" || s == "yes" || s == "owner" {
				return true
			}
		}
	}
	text := strings.ToLower(strings.TrimSpace(reason))
	if text == "" {
		return false
	}
	ownershipTerms := []string{
		"owns", "owned", "ownership", "stake", "equity",
		"acquire", "acquired", "subsidiary",
		"majority share", "controlling interest",
	}
	for _, term := range ownershipTerms {
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}
