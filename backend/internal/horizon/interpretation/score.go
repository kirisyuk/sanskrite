package interpretation

import (
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// ScoreComputer computes aggregate risk/opportunity scores from node metrics.
type ScoreComputer struct {
	axisWeights map[string]float64 // Optional axis-specific weights
}

// NewScoreComputer creates a new ScoreComputer.
func NewScoreComputer() *ScoreComputer {
	return &ScoreComputer{
		axisWeights: make(map[string]float64),
	}
}

// SetAxisWeight sets a weight for a specific axis.
func (s *ScoreComputer) SetAxisWeight(axis string, weight float64) {
	s.axisWeights[axis] = weight
}

// ComputeScore computes an aggregate score for a fragment by polarity.
// Returns score in [0.0, 1.0].
func (s *ScoreComputer) ComputeScore(fragment *horizonmodels.HorizonFragment, polarity string) float64 {
	if fragment == nil || fragment.State == nil {
		return 0.0
	}

	if polarity != "risk" && polarity != "opportunity" {
		return 0.0
	}

	score := 0.0
	for metricPath, value := range fragment.State {
		if !s.hasPolarity(metricPath, polarity) {
			continue
		}

		axis := s.extractAxis(metricPath)
		if axis == "" {
			continue
		}

		weight := s.getAxisWeight(axis)
		score += value.Value * weight
	}

	// Cap at 1.0
	if score > 1.0 {
		score = 1.0
	}

	return score
}

// hasPolarity checks if a metric path has the given polarity.
func (s *ScoreComputer) hasPolarity(metricPath, polarity string) bool {
	// Metric path format: /metric/{polarity}/{axis}/{mechanism?}
	// Check if path starts with /metric/{polarity}/
	prefix := "/metric/" + polarity + "/"
	return len(metricPath) >= len(prefix) && metricPath[:len(prefix)] == prefix
}

// extractAxis extracts the axis from a metric path.
func (s *ScoreComputer) extractAxis(metricPath string) string {
	// Metric path format: /metric/{polarity}/{axis}/{mechanism?}
	parts := splitPath(metricPath)
	if len(parts) < 3 {
		return ""
	}
	if parts[0] != "metric" {
		return ""
	}
	// parts[1] is polarity, parts[2] is axis
	return parts[2]
}

// getAxisWeight returns the weight for an axis (default 1.0).
func (s *ScoreComputer) getAxisWeight(axis string) float64 {
	if weight, ok := s.axisWeights[axis]; ok {
		return weight
	}
	return 1.0 // Default weight
}

// splitPath splits a path string into parts.
func splitPath(path string) []string {
	if path == "" {
		return []string{}
	}
	// Remove leading slash
	if len(path) > 0 && path[0] == '/' {
		path = path[1:]
	}
	if path == "" {
		return []string{}
	}
	parts := make([]string, 0)
	current := ""
	for _, r := range path {
		if r == '/' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(r)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}
