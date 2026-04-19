package policy

import (
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// Package-local aliases that retain the legacy DependencyType / FragmentType
// vocabulary used throughout the validation, canonicalization, and direction
// autofix policies. Values map to canonical AgentHorde identifiers per
// docs/taxonomy/migration_map.yaml.
//
// Where a legacy concept has no direct AH equivalent (e.g. PARTICIPATES_IN,
// HAS_MARKET_SEGMENT, COMPETES_WITH), the constant retains its legacy snake
// form. Such relations are NOT canonical AH; downstream layers MUST treat
// them as wire-only fallbacks. Policy code routes them through fold-to-
// attributes / drop dispositions rather than emitting them as edges.

// Relation aliases (ah.RelationName-typed strings).
const (
	relSuppliesTo        = ah.RelationNameSupplies
	relProduces          = ah.RelationNameProduces
	relRegulates         = ah.RelationNameRegulates
	relAssures           = ah.RelationNameAssures
	relInfluences        = ah.RelationNameInfluences
	relDisrupts          = ah.RelationNameDisrupts
	relControls          = ah.RelationNameControlsVia
	relOwns              = ah.RelationNameBeneficiallyOwns
	relInvestsIn         = ah.RelationNameInvestedIn
	relSubsidiaryOf      = ah.RelationNameSubsidiaryOf
	relGatekeepsDecision = ah.RelationNameGatekeeps
	relUsesMechanism     = ah.RelationNameWields
	relImposesCondition  = ah.RelationNameImposes
	relAdministers       = ah.RelationNameOperates
	relMemberOf          = ah.RelationNameMemberOf
	relTradedIn          = ah.RelationNameListedOn
	relJointVentureWith  = ah.RelationNameJointVentureWith
	relEnactedVia        = ah.RelationNameEnactedVia
	relSubjectTo         = ah.RelationNameSubjectTo
	relConstrainedBy     = ah.RelationNameConstrainedBy
	relManagedBy         = ah.RelationNameManagedBy
	relHoldsBoardSeat    = ah.RelationNameHoldsBoardSeat
	relSupportedBy       = ah.RelationNameSupportedBy
	relCharters          = ah.RelationNameCharters

	// Legacy wire-only relation labels that have NO direct AH equivalent.
	// They are folded/dropped per migration_map.yaml. Kept here only so legacy
	// inbound payloads can be matched and translated/quarantined.
	relAuditedBy        ah.RelationName = "audited_by"        // → ASSURES (reversed)
	relDependsOn        ah.RelationName = "depends_on"        // per-pair routing
	relParticipatesIn   ah.RelationName = "participates_in"   // dropped
	relHasMarketSegment ah.RelationName = "has_market_segment" // folded to attributes
	relCompetesWith     ah.RelationName = "competes_with"     // dropped
	relInvolvesActor    ah.RelationName = "involves_actor"    // dropped
	relFocusesOn        ah.RelationName = "focuses_on"        // dropped
	relInGeoScope       ah.RelationName = "in_geo_scope"      // folded to attributes
	relAppliesTo        ah.RelationName = "applies_to"        // dropped
	relControlsAccess   ah.RelationName = "controls_access"   // → IMPOSES via mechanism
	relOperatesVia      ah.RelationName = "operates_via"      // → wields
	relAffects          ah.RelationName = "affects"           // → disrupts
	relPartnersWith     ah.RelationName = "partners_with"     // → joint_venture_with
)

// Fragment class aliases (ah.FragmentClass-typed strings).
const (
	classOrganization = ah.FragmentClassOrg
	classFacility     = ah.FragmentClassSite
	classGeoRegion    = ah.FragmentClassGeo
	classMarket       = ah.FragmentClassMarket
	classProduct      = ah.FragmentClassProduct
	classCommunity    = ah.FragmentClassCommunity
	classPerson       = ah.FragmentClassPerson
	classMechanism    = ah.FragmentClassMechanism
	classDecision     = ah.FragmentClassDecision
	classMobileAsset  = ah.FragmentClassMobileAsset
	classEvidence     = ah.FragmentClassEvidence
	classProgram      = ah.FragmentClassOrg     // subtype=program (open enum)
	classDeposit      = ah.FragmentClassSite    // subtype=deposit (closed enum)
	classSupplyLink   = ah.FragmentClassSite    // subtype=supply_link (transitional)
	classSphere       = ah.FragmentClassEvidence // legacy strategic overlay
	classDomain       = ah.FragmentClassEvidence // legacy strategic overlay
)

// Track which legacy fragment-type tokens map into specific subtypes of the
// AH class above, used by string-keyed comparisons (e.g. when relationType
// arrives as wire string and we need to confirm "is this a /gov/ org?").
const (
	subtypeGov        = "gov"
	subtypeProgram    = "program"
	subtypeDeposit    = "deposit"
	subtypeSupplyLink = "supply_link"
)

// canonicalRelationFromWire takes a raw wire-format relation string
// (e.g. "OWNS", "supplies_to") and returns the corresponding ah.RelationName,
// preferring the strict AH normalization first, then falling back to a
// best-effort lowercase match against the legacy aliases above.
//
// Returns the empty relation if no mapping is possible.
func canonicalRelationFromWire(s string) ah.RelationName {
	if rel, ok := horizonmodels.NormalizeWireRelationStrict(s); ok {
		return rel
	}
	return ""
}

// canonicalClassFromWire normalizes a legacy FragmentType wire string
// (e.g. "ORGANIZATION", "FACILITY") into an ah.FragmentClass. Returns
// the empty class if no mapping is possible.
func canonicalClassFromWire(s string) ah.FragmentClass {
	if cls, _, ok := horizonmodels.NormalizeFragmentClassName(s, ""); ok {
		return cls
	}
	return ""
}
