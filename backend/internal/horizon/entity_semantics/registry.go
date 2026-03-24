package entity_semantics

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

var registry = make(map[EntityTypeKey]*EntityTypeDefinition)

// ResolveByPath returns the entity type key for a canonical path.
// Path takes precedence over fragment type (e.g. /market/ -> MARKET even when extraction returns ORGANIZATION).
// This prevents taxonomy/extraction misclassification from yielding wrong MissingInfoRules.
func ResolveByPath(canonicalPath string, fragmentType models.FragmentType, communityType string) EntityTypeKey {
	cp := strings.TrimSpace(strings.ToLower(canonicalPath))
	ct := strings.TrimSpace(strings.ToLower(communityType))
	ft := fragmentType
	if ft == "" {
		ft = inferFragmentTypeFromPath(cp)
	}

	// Path precedence: canonical path overrides fragment type from extraction.
	if strings.HasPrefix(cp, "/gov/") {
		return EntityTypeGov
	}
	if strings.HasPrefix(cp, "/market/") {
		return EntityTypeMarket
	}
	if strings.HasPrefix(cp, "/product/") {
		return EntityTypeProduct
	}
	if strings.HasPrefix(cp, "/person/") {
		return EntityTypePerson
	}
	if strings.HasPrefix(cp, "/fac/") {
		return EntityTypeFacility
	}
	if strings.HasPrefix(cp, "/deposit/") {
		return EntityTypeDeposit
	}
	if strings.HasPrefix(cp, "/supply-link/") {
		return EntityTypeSupplyLink
	}
	if strings.HasPrefix(cp, "/track/") {
		return EntityTypeTrack
	}
	if strings.HasPrefix(cp, "/community/") {
		if ct == "dynasty" || strings.Contains(cp, "/dynasty/") {
			return EntityTypeCommunityDynasty
		}
		if ct == "tribe" || strings.Contains(cp, "/tribe/") {
			return EntityTypeCommunityTribe
		}
		return EntityTypeCommunity
	}
	if strings.HasPrefix(cp, "/program/") {
		return EntityTypeProgram
	}
	if strings.HasPrefix(cp, "/loc/") || strings.HasPrefix(cp, "/region/") || strings.HasPrefix(cp, "/geo/") {
		return EntityTypeGeoRegion
	}
	if strings.HasPrefix(cp, "/decision/") || ft == models.FragmentTypeDecisionPoint {
		return EntityTypeDecisionPoint
	}
	if strings.HasPrefix(cp, "/mechanism/") || ft == models.FragmentTypeMechanism {
		return EntityTypeMechanism
	}
	if strings.HasPrefix(cp, "/domain/") || ft == models.FragmentTypeDomain {
		return EntityTypeDomain
	}
	if strings.HasPrefix(cp, "/zone/") || ft == models.FragmentTypeAffectedZone {
		return EntityTypeAffectedZone
	}
	if strings.HasPrefix(cp, "/org/") {
		return EntityTypeOrganization
	}

	switch ft {
	case models.FragmentTypeMarket:
		return EntityTypeMarket
	case models.FragmentTypeProduct:
		return EntityTypeProduct
	case models.FragmentTypeFacility:
		return EntityTypeFacility
	case models.FragmentTypeDeposit:
		return EntityTypeDeposit
	case models.FragmentTypeSupplyLink:
		return EntityTypeSupplyLink
	case models.FragmentTypeTrack:
		return EntityTypeTrack
	case models.FragmentTypeProgram:
		return EntityTypeProgram
	case models.FragmentTypeCommunity:
		if ct == "dynasty" || strings.Contains(cp, "/dynasty/") {
			return EntityTypeCommunityDynasty
		}
		if ct == "tribe" || strings.Contains(cp, "/tribe/") {
			return EntityTypeCommunityTribe
		}
		return EntityTypeCommunity
	case models.FragmentTypePerson:
		return EntityTypePerson
	case models.FragmentTypeGeoRegion:
		return EntityTypeGeoRegion
	case models.FragmentTypeAffectedZone:
		return EntityTypeAffectedZone
	case models.FragmentTypeOrganization:
		return EntityTypeOrganization
	}
	return EntityTypeGeneral
}

func inferFragmentTypeFromPath(cp string) models.FragmentType {
	switch {
	case strings.HasPrefix(cp, "/market/"):
		return models.FragmentTypeMarket
	case strings.HasPrefix(cp, "/product/"):
		return models.FragmentTypeProduct
	case strings.HasPrefix(cp, "/fac/"):
		return models.FragmentTypeFacility
	case strings.HasPrefix(cp, "/deposit/"):
		return models.FragmentTypeDeposit
	case strings.HasPrefix(cp, "/supply-link/"):
		return models.FragmentTypeSupplyLink
	case strings.HasPrefix(cp, "/track/"):
		return models.FragmentTypeTrack
	case strings.HasPrefix(cp, "/gov/"), strings.HasPrefix(cp, "/org/"):
		return models.FragmentTypeOrganization
	case strings.HasPrefix(cp, "/community/"):
		return models.FragmentTypeCommunity
	case strings.HasPrefix(cp, "/program/"):
		return models.FragmentTypeProgram
	case strings.HasPrefix(cp, "/domain/"):
		return models.FragmentTypeDomain
	case strings.HasPrefix(cp, "/decision/"):
		return models.FragmentTypeDecisionPoint
	case strings.HasPrefix(cp, "/mechanism/"):
		return models.FragmentTypeMechanism
	case strings.HasPrefix(cp, "/person/"):
		return models.FragmentTypePerson
	case strings.HasPrefix(cp, "/loc/"), strings.HasPrefix(cp, "/region/"), strings.HasPrefix(cp, "/geo/"):
		return models.FragmentTypeGeoRegion
	case strings.HasPrefix(cp, "/event/"):
		return models.FragmentTypeEvent
	case strings.HasPrefix(cp, "/media/"):
		return models.FragmentTypeMediaChannel
	case strings.HasPrefix(cp, "/zone/"):
		return models.FragmentTypeAffectedZone
	default:
		return ""
	}
}

// GetDefinition returns the entity type definition for the given key.
// Returns nil for EntityTypeGeneral or unknown keys.
func GetDefinition(key EntityTypeKey) *EntityTypeDefinition {
	return registry[key]
}

// GetDefinitionByPath returns the definition for the entity at the given path.
func GetDefinitionByPath(canonicalPath string, fragmentType models.FragmentType, communityType string) *EntityTypeDefinition {
	key := ResolveByPath(canonicalPath, fragmentType, communityType)
	return GetDefinition(key)
}

// ProfileSectionsForType returns profile section specs for a fragment type and path.
// Uses GOV-specific sections when path starts with /gov/.
func ProfileSectionsForType(fragmentType models.FragmentType, canonicalPath string, communityType string) []models.ProfileSectionSpec {
	def := GetDefinitionByPath(canonicalPath, fragmentType, communityType)
	if def == nil || len(def.ProfileSections) == 0 {
		return defaultProfileSections()
	}
	return def.ProfileSections
}

func defaultProfileSections() []models.ProfileSectionSpec {
	return []models.ProfileSectionSpec{
		{Key: "overview", Title: "Overview", Description: "General description and purpose", Required: true, Order: 1},
		{Key: "structure", Title: "Structure", Description: "Organizational structure, key divisions", Required: false, Order: 2},
		{Key: "mandate", Title: "Mandate & Functions", Description: "Legal mandate, responsibilities, functions", Required: false, Order: 3},
		{Key: "regulations", Title: "Regulations", Description: "Key regulations issued or enforced", Required: false, Order: 4},
		{Key: "recent_actions", Title: "Recent Actions", Description: "Recent decisions, announcements, enforcement actions", Required: false, Order: 5},
	}
}

// ExtractionBlockForPath returns the type-specific extraction block (object detail).
// Empty string if no block for this type.
func ExtractionBlockForPath(canonicalPath string, fragmentType models.FragmentType, communityType string) string {
	return ExtractionBlockForPathAndDomainModes(canonicalPath, fragmentType, communityType, nil)
}

// ExtractionBlockForPathAndDomainModes returns the extraction block, with supply-chain-specific overrides
// when domain modes include supply_chain. Supply chain contract: use only PRODUCES, SUPPLIES_TO, TRANSFORMS_INTO, ASSEMBLES_INTO,
// USED_IN, EXTRACTS_FROM, LOCATED_IN, OPERATES_IN, HAS_MARKET_SEGMENT (no DEPENDS_ON).
func ExtractionBlockForPathAndDomainModes(canonicalPath string, fragmentType models.FragmentType, communityType string, domainModes []string) string {
	def := GetDefinitionByPath(canonicalPath, fragmentType, communityType)
	if def == nil {
		return ""
	}
	if taxonomy.HasDomainMode(domainModes, taxonomy.DomainModeSupplyChain) {
		if override := supplyChainExtractionBlockOverrides[def.Key]; override != "" {
			return override
		}
	}
	return def.ExtractionBlock
}

// ResearchBlockForPath returns the type-specific research block (object detail).
// Empty string if no block for this type.
func ResearchBlockForPath(canonicalPath string, fragmentType models.FragmentType, communityType string) string {
	def := GetDefinitionByPath(canonicalPath, fragmentType, communityType)
	if def == nil {
		return ""
	}
	return def.ResearchBlock
}
