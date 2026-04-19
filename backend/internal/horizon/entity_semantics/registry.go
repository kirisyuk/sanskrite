package entity_semantics

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

var registry = make(map[EntityTypeKey]*EntityTypeDefinition)

// ResolveByPath returns the entity type key for a canonical path.
// Path takes precedence over (Class, Subtype) extracted from the
// fragment, mirroring the legacy semantics: extraction may
// misclassify, but the canonical path is authoritative.
//
// `class` is an ah.FragmentClass hint used only when no path prefix
// matches. `subtype` further refines decisions for community
// (dynasty/tribe), affected_zone, deposit, supply_link, and other
// closed subtypes from the AH taxonomy.
func ResolveByPath(canonicalPath string, class ah.FragmentClass, subtype string, communityType string) EntityTypeKey {
	cp := strings.TrimSpace(strings.ToLower(canonicalPath))
	ct := strings.TrimSpace(strings.ToLower(communityType))
	subtype = strings.TrimSpace(strings.ToLower(subtype))
	if class == "" {
		class, subtype = inferFragmentClassFromPath(cp)
	}

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
		if ct == "dynasty" || subtype == "dynasty" || strings.Contains(cp, "/dynasty/") {
			return EntityTypeCommunityDynasty
		}
		if ct == "tribe" || subtype == "tribe" || strings.Contains(cp, "/tribe/") {
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
	if strings.HasPrefix(cp, "/decision/") || class == ah.FragmentClassDecision {
		return EntityTypeDecisionPoint
	}
	if strings.HasPrefix(cp, "/mechanism/") || class == ah.FragmentClassMechanism {
		return EntityTypeMechanism
	}
	// Legacy /domain/ paths fold into evidence under AH; we still
	// surface them as DOMAIN entity for prompt continuity.
	if strings.HasPrefix(cp, "/domain/") || (class == ah.FragmentClassEvidence && subtype == "domain_legacy") {
		return EntityTypeDomain
	}
	if strings.HasPrefix(cp, "/zone/") || (class == ah.FragmentClassGeo && subtype == "affected_zone") {
		return EntityTypeAffectedZone
	}
	if strings.HasPrefix(cp, "/org/") {
		return EntityTypeOrganization
	}

	switch class {
	case ah.FragmentClassMarket:
		return EntityTypeMarket
	case ah.FragmentClassProduct:
		return EntityTypeProduct
	case ah.FragmentClassSite:
		switch subtype {
		case "deposit":
			return EntityTypeDeposit
		case "supply_link":
			return EntityTypeSupplyLink
		}
		return EntityTypeFacility
	case ah.FragmentClassMobileAsset:
		return EntityTypeTrack
	case ah.FragmentClassCommunity:
		if ct == "dynasty" || subtype == "dynasty" || strings.Contains(cp, "/dynasty/") {
			return EntityTypeCommunityDynasty
		}
		if ct == "tribe" || subtype == "tribe" || strings.Contains(cp, "/tribe/") {
			return EntityTypeCommunityTribe
		}
		return EntityTypeCommunity
	case ah.FragmentClassPerson:
		return EntityTypePerson
	case ah.FragmentClassGeo:
		if subtype == "affected_zone" {
			return EntityTypeAffectedZone
		}
		return EntityTypeGeoRegion
	case ah.FragmentClassOrg:
		switch subtype {
		case "gov":
			return EntityTypeGov
		case "program":
			return EntityTypeProgram
		}
		return EntityTypeOrganization
	case ah.FragmentClassDecision:
		return EntityTypeDecisionPoint
	case ah.FragmentClassMechanism:
		return EntityTypeMechanism
	case ah.FragmentClassEvidence:
		if subtype == "domain_legacy" {
			return EntityTypeDomain
		}
	}
	return EntityTypeGeneral
}

// inferFragmentClassFromPath returns the (Class, subtype) implied by
// a canonical path prefix. Empty class means "unknown".
func inferFragmentClassFromPath(cp string) (ah.FragmentClass, string) {
	switch {
	case strings.HasPrefix(cp, "/market/"):
		return ah.FragmentClassMarket, ""
	case strings.HasPrefix(cp, "/product/"):
		return ah.FragmentClassProduct, ""
	case strings.HasPrefix(cp, "/fac/"):
		return ah.FragmentClassSite, ""
	case strings.HasPrefix(cp, "/deposit/"):
		return ah.FragmentClassSite, "deposit"
	case strings.HasPrefix(cp, "/supply-link/"):
		return ah.FragmentClassSite, "supply_link"
	case strings.HasPrefix(cp, "/track/"):
		return ah.FragmentClassMobileAsset, ""
	case strings.HasPrefix(cp, "/gov/"):
		return ah.FragmentClassOrg, "gov"
	case strings.HasPrefix(cp, "/org/"):
		return ah.FragmentClassOrg, ""
	case strings.HasPrefix(cp, "/community/"):
		return ah.FragmentClassCommunity, ""
	case strings.HasPrefix(cp, "/program/"):
		return ah.FragmentClassOrg, "program"
	case strings.HasPrefix(cp, "/domain/"):
		return ah.FragmentClassEvidence, "domain_legacy"
	case strings.HasPrefix(cp, "/decision/"):
		return ah.FragmentClassDecision, ""
	case strings.HasPrefix(cp, "/mechanism/"):
		return ah.FragmentClassMechanism, ""
	case strings.HasPrefix(cp, "/person/"):
		return ah.FragmentClassPerson, ""
	case strings.HasPrefix(cp, "/loc/"), strings.HasPrefix(cp, "/region/"), strings.HasPrefix(cp, "/geo/"):
		return ah.FragmentClassGeo, ""
	case strings.HasPrefix(cp, "/event/"):
		return ah.FragmentClassEvidence, "event"
	case strings.HasPrefix(cp, "/media/"):
		return ah.FragmentClassOrg, "media_channel"
	case strings.HasPrefix(cp, "/zone/"):
		return ah.FragmentClassGeo, "affected_zone"
	default:
		return "", ""
	}
}

// GetDefinition returns the entity type definition for the given key.
func GetDefinition(key EntityTypeKey) *EntityTypeDefinition {
	return registry[key]
}

// GetDefinitionByPath returns the definition for the entity at the
// given path. `class` and `subtype` are AH hints used when path
// prefix resolution doesn't bind.
func GetDefinitionByPath(canonicalPath string, class ah.FragmentClass, subtype string, communityType string) *EntityTypeDefinition {
	key := ResolveByPath(canonicalPath, class, subtype, communityType)
	return GetDefinition(key)
}

// ProfileSectionsForType returns profile section specs for a fragment
// class and path. Uses GOV-specific sections when path starts with
// /gov/.
func ProfileSectionsForType(class ah.FragmentClass, canonicalPath string, communityType string) []models.ProfileSectionSpec {
	def := GetDefinitionByPath(canonicalPath, class, "", communityType)
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

// ExtractionBlockForPath returns the type-specific extraction block.
func ExtractionBlockForPath(canonicalPath string, class ah.FragmentClass, subtype string, communityType string) string {
	return ExtractionBlockForPathAndDomainModes(canonicalPath, class, subtype, communityType, nil)
}

// ExtractionBlockForPathAndDomainModes returns the extraction block,
// with supply-chain-specific overrides when domain modes include
// supply_chain.
func ExtractionBlockForPathAndDomainModes(canonicalPath string, class ah.FragmentClass, subtype string, communityType string, domainModes []string) string {
	def := GetDefinitionByPath(canonicalPath, class, subtype, communityType)
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

// ResearchBlockForPath returns the type-specific research block.
func ResearchBlockForPath(canonicalPath string, class ah.FragmentClass, subtype string, communityType string) string {
	def := GetDefinitionByPath(canonicalPath, class, subtype, communityType)
	if def == nil {
		return ""
	}
	return def.ResearchBlock
}
