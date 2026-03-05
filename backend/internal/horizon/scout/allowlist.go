package scout

import (
	"github.com/igorkirisyuk/sanskrite/internal/horizon/edgesemantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// EdgeAllowlistByDomainMode delegates to edgesemantics (single source of truth).
func EdgeAllowlistByDomainMode(mode string) []models.DependencyType {
	return edgesemantics.EdgeAllowlistByDomainMode(mode)
}

// EdgeAllowlistByDomainModes delegates to edgesemantics (single source of truth).
func EdgeAllowlistByDomainModes(modes []string) []models.DependencyType {
	return edgesemantics.EdgeAllowlistByDomainModes(modes)
}

// AllowlistContains delegates to edgesemantics (single source of truth).
func AllowlistContains(allowlist []models.DependencyType, t models.DependencyType) bool {
	return edgesemantics.AllowlistContains(allowlist, t)
}

// DomainScaffoldRelationTypesString delegates to edgesemantics (single source of truth).
func DomainScaffoldRelationTypesString() string {
	return edgesemantics.DomainScaffoldRelationTypesString()
}

// IsDomainScaffoldRelationType delegates to edgesemantics (single source of truth).
func IsDomainScaffoldRelationType(t models.DependencyType) bool {
	return edgesemantics.IsDomainScaffoldRelationType(t)
}

// IsMarketRelationType delegates to edgesemantics (single source of truth).
func IsMarketRelationType(t models.DependencyType) bool {
	return edgesemantics.IsMarketRelationType(t)
}

// SupplyChainEdgeTypesForPrompt returns prompt enum string for supply-chain-only schemas.
// Use in supply-chain contract prompts instead of AllEdgeTypesForPrompt.
func SupplyChainEdgeTypesForPrompt() string {
	return taxonomy.SupplyChainEdgeTypesForPrompt()
}

// IsSupplyChainEdgeType returns true if t is in SupplyChainEdgeTypes (case-insensitive).
// Use for runtime guards before materialization.
func IsSupplyChainEdgeType(t string) bool {
	return taxonomy.IsSupplyChainEdgeType(t)
}
