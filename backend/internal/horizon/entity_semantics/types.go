package entity_semantics

import (
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// EntityTypeKey identifies a semantic entity type for unified object-detail logic.
// GOV uses FragmentTypeOrganization with /gov/ path; CommunityDynastyTribe is a Community subtype.
type EntityTypeKey string

const (
	EntityTypeOrganization     EntityTypeKey = "org"
	EntityTypeGov              EntityTypeKey = "gov"
	EntityTypePerson           EntityTypeKey = "person"
	EntityTypeMarket           EntityTypeKey = "market"
	EntityTypeProduct          EntityTypeKey = "product"
	EntityTypeSite             EntityTypeKey = "site"
	EntityTypeGeo              EntityTypeKey = "geo"
	EntityTypeProject          EntityTypeKey = "project"
	EntityTypeCommunity        EntityTypeKey = "community"
	EntityTypeCommunityDynasty EntityTypeKey = "community_dynasty"
	EntityTypeCommunityTribe   EntityTypeKey = "community_tribe"
	EntityTypeDeposit          EntityTypeKey = "deposit"
	EntityTypeDecision         EntityTypeKey = "decision"
	EntityTypeMechanism        EntityTypeKey = "mechanism"
	EntityTypeDomain           EntityTypeKey = "domain"
	EntityTypeAffectedZone     EntityTypeKey = "affected_zone"
	EntityTypeSupplyLink       EntityTypeKey = "supply_link"
	EntityTypeMobileAsset      EntityTypeKey = "mobile_asset"
	EntityTypeGeneral          EntityTypeKey = "general"

	// Back-compat aliases inside runtime code; prompts and new logic should use
	// the AH-facing names above.
	EntityTypeFacility      = EntityTypeSite
	EntityTypeGeoRegion     = EntityTypeGeo
	EntityTypeProgram       = EntityTypeProject
	EntityTypeDecisionPoint = EntityTypeDecision
	EntityTypeTrack         = EntityTypeMobileAsset
)

// MissingInfoTrigger defines when a MissingInfoRule fires.
const (
	TriggerRelationMissing    = "relation_missing"
	TriggerFieldEmpty         = "field_empty"
	TriggerRelationCountBelow = "relation_count_below"
	TriggerSeed               = "seed"
)

// MissingInfoRule is a structured rule for generating missingInfo questions.
// Used by RuleBasedQuickValidation, synthetic seed, and fallback in extraction path.
type MissingInfoRule struct {
	Trigger          string   // relation_missing | field_empty | relation_count_below | seed
	RelationType     string   // for relation_missing (PRODUCES, HAS_MARKET_SEGMENT, ...)
	RelationTypes    []string // when set, rule fires when ALL listed types are missing (e.g. PRODUCES+SUPPLIES_TO)
	FieldPath        string   // for field_empty: Labels.en, MarketDomain, State./metric/market/tam_usd
	MinCount         int      // for relation_count_below
	DomainMode       string   // "" = general; "market" = only when DomainModeMarket active
	QuestionTemplate string   // fmt.Sprintf template, %s = targetCanonicalPath
}

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

	// MissingInfoRules: structured rules for deterministic missingInfo generation.
	// Used by RuleBasedQuickValidation, synthetic seed, and fallback in extraction path.
	MissingInfoRules []MissingInfoRule
}
