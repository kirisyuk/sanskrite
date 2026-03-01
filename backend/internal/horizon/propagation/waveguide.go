package propagation

import (
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// Waveguide boost multipliers per axis group.
// Physical/logistics axes get stronger waveguide because chains are rigid conduits
// for those signal types. Other axes receive a baseline boost.
const (
	waveguideBoostPhysicalFlow  = 1.25
	waveguideBoostLogistics     = 1.20
	waveguideBoostLegalOps      = 1.15
	waveguideBoostDefault       = 1.10
	waveguideCoherenceBonus     = 1.05
)

// EdgeArtifactKey extracts the artifact canonical key from an edge's qualifiers.
// Returns empty string if the edge is not part of a materialized artifact (chain).
func EdgeArtifactKey(edge *horizonmodels.DependencyEdge) string {
	if edge == nil || edge.Qualifiers == nil {
		return ""
	}
	if key, ok := edge.Qualifiers["artifactKey"].(string); ok {
		return strings.TrimSpace(key)
	}
	return ""
}

// WaveguideBoostForEdge returns a multiplier >= 1.0 that compensates for damping
// on edges belonging to a materialized artifact (chain waveguide effect).
// The boost is axis-dependent: physical/logistics axes get stronger waveguide
// because chains are rigid conduits for physical flow signals.
// Returns 1.0 (no boost) if the edge has no artifact key.
func WaveguideBoostForEdge(edge *horizonmodels.DependencyEdge, profile *horizonmodels.PropagationProfile) float64 {
	if EdgeArtifactKey(edge) == "" {
		return 1.0
	}
	if profile == nil {
		return waveguideBoostDefault
	}
	axis := strings.TrimSpace(profile.AxisKey)
	switch axis {
	case horizonmodels.AxisPhysicalFlowSupply, horizonmodels.AxisPhysicalFlowDemand:
		return waveguideBoostPhysicalFlow
	case horizonmodels.AxisLogisticsCapacity, horizonmodels.AxisLogisticsLatency:
		return waveguideBoostLogistics
	case horizonmodels.AxisLegalAccess, horizonmodels.AxisLegalCompliance, horizonmodels.AxisOperationalContinuity:
		return waveguideBoostLegalOps
	default:
		return waveguideBoostDefault
	}
}

// WaveguideCoherenceBonus returns a small amplitude bonus applied when a signal
// continues along the same chain (same artifactKey) from one hop to the next.
// This makes signals "prefer" to follow known chain paths rather than scatter.
func WaveguideCoherenceBonus(currentArtifactKey, neighborArtifactKey string) float64 {
	if currentArtifactKey != "" && currentArtifactKey == neighborArtifactKey {
		return waveguideCoherenceBonus
	}
	return 1.0
}
