package scout

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/contractsemantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/edgesemantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// EdgeAllowlistByDomainMode delegates to contract semantics as the source of truth
// for runtime contract isolation.
func EdgeAllowlistByDomainMode(mode string) []models.DependencyType {
	profile := contractsemantics.ProfileForDomainMode(mode)
	if len(profile.AllowedEdges) == 0 {
		return nil
	}
	out := make([]models.DependencyType, 0, len(profile.AllowedEdges))
	for _, edgeType := range profile.AllowedEdges {
		out = append(out, edgeType)
	}
	return out
}

// EdgeAllowlistByDomainModes returns the deduplicated union of allowed edges across
// normalized domain modes, preserving contract semantics as the source of truth.
func EdgeAllowlistByDomainModes(modes []string) []models.DependencyType {
	composed := contractsemantics.ComposeDomainModes(modes)
	if len(composed.AllowedEdges) == 0 {
		return nil
	}
	out := make([]models.DependencyType, 0, len(composed.AllowedEdges))
	for _, edgeType := range composed.AllowedEdges {
		out = append(out, edgeType)
	}
	return out
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

// IsAllowedSupplyChainRuntimeRelation applies the runtime supply-chain gate with
// the same domain/market scaffold exceptions used by relation pre-filtering.
func IsAllowedSupplyChainRuntimeRelation(sourcePath, targetPath string, relationType models.DependencyType) bool {
	sourcePath = strings.TrimSpace(sourcePath)
	targetPath = strings.TrimSpace(targetPath)
	relationType = models.DependencyType(strings.TrimSpace(taxonomy.NormalizeRelationType(string(relationType))))
	if relationType == "" {
		return false
	}
	if IsSupplyChainEdgeType(string(relationType)) {
		return true
	}
	if relationType == models.DependencyTypeOwns || relationType == models.DependencyTypeControls {
		return allowSupplyChainOwnershipOrControlByPath(sourcePath, targetPath)
	}
	if strings.HasPrefix(sourcePath, "/domain/") && IsDomainScaffoldRelationType(relationType) {
		return true
	}
	if strings.HasPrefix(sourcePath, "/market/") && (IsDomainScaffoldRelationType(relationType) || IsMarketRelationType(relationType)) {
		return true
	}
	return false
}

// IsAllowedSupplyChainPersistRelation keeps final persist gates aligned with the
// runtime relation contract, including scaffold exceptions from domain/market anchors.
func IsAllowedSupplyChainPersistRelation(sourcePath, targetPath string, relationType models.DependencyType) bool {
	return IsAllowedSupplyChainRuntimeRelation(sourcePath, targetPath, relationType)
}
