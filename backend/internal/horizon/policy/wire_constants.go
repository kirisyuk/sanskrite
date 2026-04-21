package policy

import (
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// Package-local relation and class names for validation/canonicalization policy.
// Wire-only snake_case values have no AH edge equivalent; policy uses them for quarantine/fold rules.

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

	relAuditedBy        ah.RelationName = "audited_by"
	relDependsOn        ah.RelationName = "depends_on"
	relParticipatesIn   ah.RelationName = "participates_in"
	relHasMarketSegment ah.RelationName = "has_market_segment"
	relCompetesWith     ah.RelationName = "competes_with"
	relInvolvesActor    ah.RelationName = "involves_actor"
	relFocusesOn        ah.RelationName = "focuses_on"
	relInGeoScope       ah.RelationName = "in_geo_scope"
	relAppliesTo        ah.RelationName = "applies_to"
	relControlsAccess   ah.RelationName = "controls_access"
	relOperatesVia      ah.RelationName = "operates_via"
	relAffects          ah.RelationName = "affects"
	relPartnersWith     ah.RelationName = "partners_with"
)

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
	classProgram      = ah.FragmentClassOrg
	classDeposit      = ah.FragmentClassSite
	classSupplyLink   = ah.FragmentClassSite
	classSphere       = ah.FragmentClassEvidence
	classDomain       = ah.FragmentClassEvidence
)

const (
	subtypeGov        = "gov"
	subtypeProgram    = "program"
	subtypeDeposit    = "deposit"
	subtypeSupplyLink = "supply_link"
)

func canonicalRelationFromWire(s string) ah.RelationName {
	if rel, ok := horizonmodels.NormalizeWireRelationStrict(s); ok {
		return rel
	}
	return ""
}

func canonicalClassFromWire(s string) ah.FragmentClass {
	if cls, _, ok := horizonmodels.NormalizeFragmentClassName(s, ""); ok {
		return cls
	}
	return ""
}
