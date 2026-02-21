package edgesemantics

import (
	"fmt"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// EdgeDirectionRule defines valid source and target fragment types for an edge type.
type EdgeDirectionRule struct {
	ValidSourceTypes []models.FragmentType
	ValidTargetTypes []models.FragmentType
	Description      string
}

// EdgeDirectionPolicies is the single source of truth for edge direction semantics.
var EdgeDirectionPolicies = map[models.DependencyType]EdgeDirectionRule{
	models.DependencyTypeRegulates: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypeOrganization},
		ValidTargetTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeFacility,
			models.FragmentTypeProduct,
			models.FragmentTypeGeoRegion,
			models.FragmentTypeMarket,
		},
		Description: "Regulator (org) regulates regulated entity",
	},
	models.DependencyTypeOwns: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypePerson,
		},
		ValidTargetTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeFacility,
			models.FragmentTypeProduct,
			models.FragmentTypeDeposit,
			models.FragmentTypeMediaChannel,
			models.FragmentTypeCommunity,
		},
		Description: "Owner owns asset/subsidiary",
	},
	models.DependencyTypeControls: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypePerson,
			models.FragmentTypeCommunity,
		},
		ValidTargetTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeFacility,
		},
		Description: "Controller controls entity (non-equity or indirect control)",
	},
	models.DependencyTypeManages: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypePerson,
		},
		ValidTargetTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeFacility,
			models.FragmentTypeProgram,
		},
		Description: "Manager manages entity (formal authority, not ownership)",
	},
	models.DependencyTypeEmployedBy: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypePerson},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeOrganization},
		Description:      "Person is employed by organization",
	},
	models.DependencyTypeBoardMemberOf: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypePerson},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeOrganization},
		Description:      "Person is board member of organization",
	},
	models.DependencyTypeWorksFor: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypePerson},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeOrganization},
		Description:      "Person works for organization (legacy)",
	},
	models.DependencyTypeKeyPerson: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypePerson},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeOrganization},
		Description:      "Person is key person of organization",
	},
	models.DependencyTypeHasKeyPerson: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypePerson},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeOrganization},
		Description:      "Legacy alias; normalize to KEY_PERSON (person -> org)",
	},
	models.DependencyTypeFounded: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypePerson,
			models.FragmentTypeOrganization,
		},
		ValidTargetTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeProgram,
			models.FragmentTypeCommunity,
		},
		Description: "Founder (person/org) founded organization/program/community",
	},
	models.DependencyTypeInvestsIn: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypePerson,
		},
		ValidTargetTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeProgram,
			models.FragmentTypeProduct,
		},
		Description: "Investor invests in investee",
	},
	models.DependencyTypePartnersWith: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeFacility,
		},
		ValidTargetTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeFacility,
		},
		Description: "Partners collaborate (symmetric)",
	},
	models.DependencyTypeCompetesWith: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeFacility,
			models.FragmentTypeProduct,
		},
		ValidTargetTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeFacility,
			models.FragmentTypeProduct,
		},
		Description: "Competitors compete in the same arena (symmetric)",
	},
	models.DependencyTypeProduces: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeFacility,
			models.FragmentTypeDeposit,
		},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeProduct},
		Description:      "Producer produces product",
	},
	models.DependencyTypeConsumes: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeFacility,
			models.FragmentTypeProduct,
		},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeProduct},
		Description:      "Consumer consumes product",
	},
	models.DependencyTypeLocatedIn: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeFacility,
			models.FragmentTypeOrganization,
			models.FragmentTypePerson,
			models.FragmentTypeProduct,
			models.FragmentTypeProgram,
			models.FragmentTypeGeoRegion,
			models.FragmentTypeDeposit,
			models.FragmentTypeCommunity,
		},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeGeoRegion},
		Description:      "Entity is located within region",
	},
	models.DependencyTypeOperatesIn: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypePerson,
		},
		ValidTargetTypes: []models.FragmentType{
			models.FragmentTypeGeoRegion,
			models.FragmentTypeFacility,
			models.FragmentTypeDeposit,
		},
		Description: "Organization operates in location",
	},
	models.DependencyTypeSuppliesTo: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeFacility,
		},
		ValidTargetTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeFacility,
		},
		Description: "Supplier supplies to customer",
	},
	models.DependencyTypeDependsOn: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeDomain,
			models.FragmentTypeOrganization,
			models.FragmentTypePerson,
			models.FragmentTypeProgram,
			models.FragmentTypeFacility,
			models.FragmentTypeProduct,
			models.FragmentTypeCommunity,
			models.FragmentTypeMediaChannel,
			models.FragmentTypeDeposit,
			models.FragmentTypeMarket,
		},
		ValidTargetTypes: []models.FragmentType{
			models.FragmentTypeDomain,
			models.FragmentTypeOrganization,
			models.FragmentTypePerson,
			models.FragmentTypeMarket,
			models.FragmentTypeProgram,
			models.FragmentTypeFacility,
			models.FragmentTypeProduct,
			models.FragmentTypeGeoRegion,
			models.FragmentTypeMediaChannel,
			models.FragmentTypeCommunity,
			models.FragmentTypeDeposit,
		},
		Description: "Dependent entity depends on dependency",
	},
	models.DependencyTypeTransformsInto: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypeProduct},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeProduct},
		Description:      "Product transforms into downstream product",
	},
	models.DependencyTypeExtractsFrom: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypeFacility},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeDeposit},
		Description:      "Facility extracts from deposit",
	},
	models.DependencyTypeAssures: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypeOrganization},
		ValidTargetTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeFacility,
			models.FragmentTypeProgram,
		},
		Description: "Assurer provides independent assurance to entity. Qualifier assurance_type: audit | certification | regulatory | inspection",
	},
	models.DependencyTypeAuditedBy: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeFacility,
			models.FragmentTypeProgram,
		},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeOrganization},
		Description:      "Entity is audited by an external audit firm (legacy; prefer ASSURES for new data)",
	},
	models.DependencyTypeInfluences: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypePerson,
			models.FragmentTypeGeoRegion,
			models.FragmentTypeFacility,
			models.FragmentTypeProduct,
			models.FragmentTypeProgram,
		},
		ValidTargetTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeGeoRegion,
			models.FragmentTypeFacility,
			models.FragmentTypeProduct,
			models.FragmentTypeProgram,
			models.FragmentTypeMarket,
			models.FragmentTypeSphere,
		},
		Description: "Source entity influences target entity (political/economic influence)",
	},
	models.DependencyTypeRelativeOf: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypePerson},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypePerson},
		Description:      "Person is relative of person (symmetric)",
	},
	models.DependencyTypeRelatedTo: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypePerson},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypePerson},
		Description:      "Person is related to person (symmetric)",
	},
	models.DependencyTypeParticipatesIn: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeFacility,
			models.FragmentTypeCommunity,
			models.FragmentTypeProduct,
			models.FragmentTypeProgram,
			models.FragmentTypePerson,
		},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeMarket},
		Description:      "Entity participates in market",
	},
	models.DependencyTypeDominates: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeFacility,
			models.FragmentTypeCommunity,
		},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeMarket},
		Description:      "Entity dominates market (leader)",
	},
	models.DependencyTypeHasMarketSegment: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypeMarket},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeMarket},
		Description:      "Market has sub-market (hierarchy)",
	},
	models.DependencyTypeHasSubgroup: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypeCommunity},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeCommunity},
		Description:      "Community has sub-group (hierarchy)",
	},
	models.DependencyTypeAdministers: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeGeoRegion,
		},
		ValidTargetTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeProgram,
			models.FragmentTypeGeoRegion,
		},
		Description: "Government body administers subordinate body (hierarchy)",
	},
	models.DependencyTypeOperatesVia: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeCommunity,
			models.FragmentTypeOrganization,
		},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeMediaChannel},
		Description:      "Entity operates via media channel",
	},
	models.DependencyTypeMemberOf: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypePerson},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeCommunity},
		Description:      "Person is member of community",
	},
	models.DependencyTypeAffiliatedWith: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeCommunity,
			models.FragmentTypePerson,
		},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeOrganization},
		Description:      "Entity is affiliated with organization",
	},
	models.DependencyTypeRenamedTo: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypeOrganization},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeOrganization},
		Description:      "Organization renamed to organization (corporate transition)",
	},
	models.DependencyTypeRebrandedTo: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypeOrganization},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeOrganization},
		Description:      "Organization rebranded to organization (corporate transition)",
	},
	models.DependencyTypeLegalSuccessorOf: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypeOrganization},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeOrganization},
		Description:      "Organization is legal successor of organization",
	},
	models.DependencyTypeMergedInto: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypeOrganization},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeOrganization},
		Description:      "Organization merged into organization",
	},
	models.DependencyTypeSpunOffFrom: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypeOrganization},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeOrganization},
		Description:      "Organization spun off from organization",
	},
	models.DependencyTypeAcquiredBy: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypeOrganization},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeOrganization},
		Description:      "Organization acquired by organization",
	},
	models.DependencyTypeFocusesOn: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypeDomain},
		ValidTargetTypes: []models.FragmentType{
			models.FragmentTypeDomain,
			models.FragmentTypeOrganization,
			models.FragmentTypePerson,
			models.FragmentTypeMarket,
			models.FragmentTypeProduct,
			models.FragmentTypeProgram,
			models.FragmentTypeFacility,
			models.FragmentTypeGeoRegion,
			models.FragmentTypeCommunity,
			models.FragmentTypeMediaChannel,
			models.FragmentTypeDeposit,
		},
		Description: "Domain focuses on an entity/domain",
	},
	models.DependencyTypeInvolvesActor: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypeDomain},
		ValidTargetTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypePerson,
			models.FragmentTypeCommunity,
		},
		Description: "Domain involves principal actor",
	},
	models.DependencyTypeInGeoScope: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypeDomain},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeGeoRegion},
		Description:      "Domain is in geo scope",
	},
	models.DependencyTypeAbout: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypeDomain},
		ValidTargetTypes: []models.FragmentType{
			models.FragmentTypeDomain,
			models.FragmentTypeOrganization,
			models.FragmentTypePerson,
			models.FragmentTypeMarket,
			models.FragmentTypeProduct,
			models.FragmentTypeProgram,
			models.FragmentTypeFacility,
			models.FragmentTypeGeoRegion,
			models.FragmentTypeCommunity,
			models.FragmentTypeMediaChannel,
			models.FragmentTypeDeposit,
		},
		Description: "Domain describes subject entities",
	},
	models.DependencyTypeRelatedDomain: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypeDomain},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeDomain},
		Description:      "Related domains (symmetric)",
	},
	models.DependencyTypeBroadensTo: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypeDomain},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeDomain},
		Description:      "Domain broadens to another domain",
	},
	models.DependencyTypeNarrowsTo: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypeDomain},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeDomain},
		Description:      "Domain narrows to another domain",
	},
	models.DependencyTypeAffects: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypeDomain},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeDomain},
		Description:      "Domain affects another domain",
	},
	models.DependencyTypeSurfaces: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypeDomain},
		ValidTargetTypes: []models.FragmentType{
			models.FragmentTypeDecisionPoint,
			models.FragmentTypeMechanism,
		},
		Description: "Domain surfaces strategic entities",
	},
	models.DependencyTypeControlsAccess: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeDecisionPoint,
			models.FragmentTypeOrganization,
			models.FragmentTypeCommunity,
		},
		ValidTargetTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeFacility,
			models.FragmentTypeProduct,
			models.FragmentTypeMarket,
			models.FragmentTypeProgram,
		},
		Description: "Strategic gatekeeping relation",
	},
	models.DependencyTypeImposesCondition: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeDecisionPoint,
			models.FragmentTypeCommunity,
		},
		ValidTargetTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeFacility,
			models.FragmentTypeProduct,
		},
		Description: "Imposes compliance/cost constraints",
	},
	models.DependencyTypeGatekeepsDecision: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeCommunity,
			models.FragmentTypePerson,
		},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeDecisionPoint},
		Description:      "Actor gatekeeps a strategic decision",
	},
	models.DependencyTypeUsesMechanism: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeDecisionPoint,
			models.FragmentTypeOrganization,
			models.FragmentTypeCommunity,
			models.FragmentTypeProgram,
		},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeMechanism},
		Description:      "Agent uses control mechanism",
	},
	models.DependencyTypeAppliesTo: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypeMechanism},
		ValidTargetTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeFacility,
			models.FragmentTypeProduct,
			models.FragmentTypeMarket,
		},
		Description: "Mechanism applies to entity",
	},
}

// ValidateEdgeDirection checks whether direction is semantically valid.
func ValidateEdgeDirection(edgeType models.DependencyType, sourceType, targetType models.FragmentType) (bool, bool, *EdgeDirectionRule) {
	rule, exists := EdgeDirectionPolicies[edgeType]
	if !exists {
		return false, false, &EdgeDirectionRule{Description: fmt.Sprintf("unknown edge type %s: missing direction policy", edgeType)}
	}

	sourceValid := containsFragmentType(rule.ValidSourceTypes, sourceType)
	targetValid := containsFragmentType(rule.ValidTargetTypes, targetType)
	if sourceValid && targetValid {
		return true, false, &rule
	}

	reversedSourceValid := containsFragmentType(rule.ValidSourceTypes, targetType)
	reversedTargetValid := containsFragmentType(rule.ValidTargetTypes, sourceType)
	if reversedSourceValid && reversedTargetValid {
		return false, true, &rule
	}

	return false, false, &rule
}

func containsFragmentType(types []models.FragmentType, t models.FragmentType) bool {
	for _, vt := range types {
		if vt == t {
			return true
		}
	}
	return false
}
