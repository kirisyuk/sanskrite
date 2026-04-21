package repo

import (
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// Relation and class constants for the persistence boundary (Firestore wire shapes).

const (
	relSuppliesTo        = ah.RelationNameSupplies
	relProduces          = ah.RelationNameProduces
	relOwns              = ah.RelationNameBeneficiallyOwns
	relSubsidiaryOf      = ah.RelationNameSubsidiaryOf
	relControls          = ah.RelationNameControlsVia
	relInvestsIn         = ah.RelationNameInvestedIn
	relRegulates         = ah.RelationNameRegulates
	relAssures           = ah.RelationNameAssures
	relInfluences        = ah.RelationNameInfluences
	relGatekeepsDecision = ah.RelationNameGatekeeps
	relUsesMechanism     = ah.RelationNameWields
	relImposesCondition  = ah.RelationNameImposes
	relAdministers       = ah.RelationNameOperates
	relMemberOf          = ah.RelationNameMemberOf
	relTradedIn          = ah.RelationNameListedOn
	relPartnersWith      = ah.RelationNameJointVentureWith
	relAffects           = ah.RelationNameDisrupts

	relAuditedBy      ah.RelationName = "audited_by"
	relAssemblesInto  ah.RelationName = "assembles_into"
	relUsedIn         ah.RelationName = "used_in"
	relTransformsInto ah.RelationName = "transforms_into"
	relDependsOn      ah.RelationName = "depends_on"
	relAssociatedWith ah.RelationName = "associated_with"
	relOperatesIn     ah.RelationName = "operates_in"
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
)
