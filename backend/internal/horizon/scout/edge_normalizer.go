package scout

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/edgesemantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// =============================================================================
// EDGE POLICIES
// =============================================================================
// This file defines normalization and validation policies for dependency edges.
// Policies are enforced at creation time and during cleanup operations.

// =============================================================================
// Policy 1: Symmetric Edge Normalization
// =============================================================================
// Some edge types represent symmetric relationships where direction doesn't matter.
// To avoid duplicates (A->B and B->A), we normalize by always placing the
// lexicographically smaller ID as source.

// SymmetricEdgeTypes delegates to models.SymmetricEdgeTypes.
// Kept for backwards compatibility.
var SymmetricEdgeTypes = models.SymmetricEdgeTypes

// NormalizeSymmetricEdge delegates to models.NormalizeSymmetricEdge.
// Kept for backwards compatibility.
func NormalizeSymmetricEdge(sourceID, targetID string, edgeType models.DependencyType) (string, string, models.DependencyType, bool) {
	return models.NormalizeSymmetricEdge(sourceID, targetID, edgeType)
}

// =============================================================================
// Policy 2: Edge Direction Validation
// =============================================================================
// Certain edge types have semantically meaningful directions that must be enforced.
// When an edge violates direction rules, it should be reversed (not deleted).

// EdgeDirectionRule delegates to edgesemantics.EdgeDirectionRule.
// Kept for backwards compatibility.
type EdgeDirectionRule = edgesemantics.EdgeDirectionRule

// =============================================================================
// Policy 2.5: Edge Canonicalization (Type + Direction Rewrites)
// =============================================================================
// Some edge patterns are not just "wrong direction" but "wrong relation type".
// In those cases, reversing is not sufficient: we rewrite type and (optionally) swap.
//
// NOTE: Keep this conservative and explicit. This is used at creation time and during cleanup.

// CanonicalizeEdge rewrites an edge type (and optionally direction) based on endpoint fragment types.
// Returns: (newType, shouldSwapEndpoints, changed, reason).
func CanonicalizeEdge(edgeType models.DependencyType, sourceType, targetType models.FragmentType) (models.DependencyType, bool, bool, string) {
	// Keep the canonicalization logic in models to avoid import cycles (repo <-> scout).
	return models.CanonicalizeEdge(edgeType, sourceType, targetType)
}

// CanonicalizeEdgeWithPaths rewrites an edge type (and optionally direction) based on endpoint fragment types and canonical paths.
// This extends CanonicalizeEdge with path-based checks (e.g., for community subtypes).
// Returns: (newType, shouldSwapEndpoints, changed, reason).
func CanonicalizeEdgeWithPaths(edgeType models.DependencyType, sourceType, targetType models.FragmentType, sourcePath, targetPath string) (models.DependencyType, bool, bool, string) {
	// Delegate to models to avoid import cycles
	return models.CanonicalizeEdgeWithPaths(edgeType, sourceType, targetType, sourcePath, targetPath)
}

// EdgeDirectionPolicies delegates to edgesemantics.EdgeDirectionPolicies.
// Kept for backwards compatibility.
var EdgeDirectionPolicies = edgesemantics.EdgeDirectionPolicies

// ValidateEdgeDirection checks if an edge has correct direction according to policies.
// Returns: (isValid, shouldReverse, rule)
// - isValid: true if current direction is valid
// - shouldReverse: true if reversing would make it valid
// - rule: the applicable rule (nil if no rule defined)
func ValidateEdgeDirection(edgeType models.DependencyType, sourceType, targetType models.FragmentType) (bool, bool, *EdgeDirectionRule) {
	return edgesemantics.ValidateEdgeDirection(edgeType, sourceType, targetType)
}

// ShouldReverseEdge is a convenience function that returns true if the edge
// should be reversed to comply with direction policies.
func ShouldReverseEdge(edgeType models.DependencyType, sourceType, targetType models.FragmentType) bool {
	_, shouldReverse, _ := ValidateEdgeDirection(edgeType, sourceType, targetType)
	return shouldReverse
}

// =============================================================================
// Policy 3: Self-Loop Prevention
// =============================================================================
// Edges where source == target are typically errors and should be rejected.

// IsSelfLoop delegates to models.IsSelfLoop.
// Kept for backwards compatibility.
func IsSelfLoop(sourceID, targetID string) bool {
	return models.IsSelfLoop(sourceID, targetID)
}

// =============================================================================
// Policy 4: Edge Deduplication
// =============================================================================
// Duplicate edges (same source, target, type) should be merged, not duplicated.
// This is handled at the repository level, but the policy is documented here.

// EdgeKey generates a unique key for edge deduplication.
// For symmetric edges, the key is normalized to ensure A->B and B->A produce the same key.
func EdgeKey(sourceID, targetID string, edgeType models.DependencyType) string {
	if SymmetricEdgeTypes[edgeType] {
		if strings.Compare(sourceID, targetID) > 0 {
			sourceID, targetID = targetID, sourceID
		}
	}
	return sourceID + "|" + targetID + "|" + string(edgeType)
}
