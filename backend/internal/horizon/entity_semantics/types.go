package entity_semantics

import (
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// EntityTypeKey identifies a semantic entity type for unified object-detail logic.
// GOV uses FragmentTypeOrganization with /gov/ path; CommunityDynastyTribe is a Community subtype.
type EntityTypeKey string

const (
	EntityTypeOrganization         EntityTypeKey = "ORGANIZATION"
	EntityTypeGov                  EntityTypeKey = "GOV"
	EntityTypePerson               EntityTypeKey = "PERSON"
	EntityTypeMarket               EntityTypeKey = "MARKET"
	EntityTypeProduct              EntityTypeKey = "PRODUCT"
	EntityTypeFacility              EntityTypeKey = "FACILITY"
	EntityTypeGeoRegion             EntityTypeKey = "GEO_REGION"
	EntityTypeProgram               EntityTypeKey = "PROGRAM"
	EntityTypeCommunity       EntityTypeKey = "COMMUNITY"
	EntityTypeCommunityDynasty EntityTypeKey = "COMMUNITY_DYNASTY"
	EntityTypeCommunityTribe  EntityTypeKey = "COMMUNITY_TRIBE"
	EntityTypeDeposit               EntityTypeKey = "DEPOSIT"
	EntityTypeDecisionPoint         EntityTypeKey = "DECISION_POINT"
	EntityTypeMechanism             EntityTypeKey = "MECHANISM"
	EntityTypeDomain                EntityTypeKey = "DOMAIN"
	EntityTypeGeneral               EntityTypeKey = "GENERAL"
)

// EntityTypeDefinition is the single source of truth for object-detail logic per entity type.
// Domain modes and specialties define SCOPE; this defines how to detail each object.
type EntityTypeDefinition struct {
	Key EntityTypeKey

	// RequiredAspects: business-oriented aspects to request (documentation + validation).
	RequiredAspects []string

	// ProfileSections: section specs for profile generation.
	ProfileSections []models.ProfileSectionSpec

	// ExtractionBlock: type-specific prompt block for extraction (object detail).
	// Preserves full taxonomy rules; appended to extraction prompt.
	ExtractionBlock string

	// ResearchBlock: type-specific prompt block for research/critic (object detail).
	// Same content as ExtractionBlock for consistency; used by research_agent and critic_agent.
	ResearchBlock string
}
