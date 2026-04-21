package agents

import (
	"github.com/igorkirisyuk/sanskrite/internal/horizon/entity_semantics"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// ResearchProfile defines specialized orchestration behavior for fragment types.
// Deprecated: use entity_semantics.ResolveByPath and entity_semantics.ResearchBlockForPath.
type ResearchProfile string

const (
	ResearchProfileGeneral               ResearchProfile = "GENERAL"
	ResearchProfileMarket                ResearchProfile = "MARKET"
	ResearchProfileProduct               ResearchProfile = "PRODUCT"
	ResearchProfileFacility              ResearchProfile = "FACILITY"
	ResearchProfileDeposit               ResearchProfile = "deposit"
	ResearchProfileGov                   ResearchProfile = "GOV"
	ResearchProfileCommunityDynastyTribe ResearchProfile = "COMMUNITY_DYNASTY_TRIBE"
	ResearchProfileProgram               ResearchProfile = "PROGRAM"
	ResearchProfileDomain                ResearchProfile = "DOMAIN"
	ResearchProfileDecisionPoint         ResearchProfile = "DECISION_POINT"
	ResearchProfileMechanism             ResearchProfile = "MECHANISM"
)

func ResolveResearchProfile(fragmentType ah.FragmentClass, canonicalPath string, communityType string) ResearchProfile {
	key := entity_semantics.ResolveByPath(canonicalPath, fragmentType, "", communityType)
	return entityTypeKeyToResearchProfile(key)
}

func entityTypeKeyToResearchProfile(key entity_semantics.EntityTypeKey) ResearchProfile {
	switch key {
	case entity_semantics.EntityTypeMarket:
		return ResearchProfileMarket
	case entity_semantics.EntityTypeProduct:
		return ResearchProfileProduct
	case entity_semantics.EntityTypeFacility:
		return ResearchProfileFacility
	case entity_semantics.EntityTypeDeposit:
		return ResearchProfileDeposit
	case entity_semantics.EntityTypeGov:
		return ResearchProfileGov
	case entity_semantics.EntityTypeCommunityDynasty, entity_semantics.EntityTypeCommunityTribe:
		return ResearchProfileCommunityDynastyTribe
	case entity_semantics.EntityTypeProgram:
		return ResearchProfileProgram
	case entity_semantics.EntityTypeDomain:
		return ResearchProfileDomain
	case entity_semantics.EntityTypeDecisionPoint:
		return ResearchProfileDecisionPoint
	case entity_semantics.EntityTypeMechanism:
		return ResearchProfileMechanism
	default:
		return ResearchProfileGeneral
	}
}

func BuildResearchProfilePromptBlock(profile ResearchProfile) string {
	// Delegate to entity_semantics for single source of truth.
	key := researchProfileToEntityTypeKey(profile)
	def := entity_semantics.GetDefinition(key)
	if def != nil && def.ResearchBlock != "" {
		return def.ResearchBlock
	}
	return ""
}

func researchProfileToEntityTypeKey(profile ResearchProfile) entity_semantics.EntityTypeKey {
	switch profile {
	case ResearchProfileMarket:
		return entity_semantics.EntityTypeMarket
	case ResearchProfileProduct:
		return entity_semantics.EntityTypeProduct
	case ResearchProfileFacility:
		return entity_semantics.EntityTypeFacility
	case ResearchProfileDeposit:
		return entity_semantics.EntityTypeDeposit
	case ResearchProfileGov:
		return entity_semantics.EntityTypeGov
	case ResearchProfileCommunityDynastyTribe:
		return entity_semantics.EntityTypeCommunityDynasty
	case ResearchProfileProgram:
		return entity_semantics.EntityTypeProgram
	case ResearchProfileDomain:
		return entity_semantics.EntityTypeDomain
	case ResearchProfileDecisionPoint:
		return entity_semantics.EntityTypeDecisionPoint
	case ResearchProfileMechanism:
		return entity_semantics.EntityTypeMechanism
	default:
		return entity_semantics.EntityTypeGeneral
	}
}
