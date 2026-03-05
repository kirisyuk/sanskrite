package models

import (
	"fmt"
	"strings"
)

// Standard qualifier keys for influence edges
const (
	// QualifierVia specifies the channel/mechanism through which influence is exerted.
	// Value: canonical path to a fragment (e.g., "/product/.../natural-gas", "/org/swift", "/fac/tr/bosphorus-strait")
	QualifierVia = "via"

	// QualifierMechanism describes the type of influence mechanism.
	// Values: "energy_dependency", "debt_trap", "sanctions", "trade_leverage", "infrastructure_control", etc.
	QualifierMechanism = "mechanism"

	// QualifierDirection indicates the direction/effect of influence.
	// Values: "positive" (beneficial), "negative" (harmful/pressuring), "neutral"
	QualifierDirection = "direction"

	// QualifierReversible indicates if the influence relationship is reversible.
	// Value: boolean (true/false)
	QualifierReversible = "reversible"

	// QualifierConditionality specifies conditions/triggers for the influence.
	// Value: array of strings (e.g., ["winter", "low_storage"])
	QualifierConditionality = "conditionality"

	// QualifierStrength indicates the strength/intensity of influence (0..1).
	// Value: float64
	QualifierStrength = "strength"
)

// InfluenceQualifiers represents standardized qualifiers for INFLUENCES edges.
type InfluenceQualifiers struct {
	Via           *string   `json:"via,omitempty"`           // Canonical path to fragment
	Mechanism     *string   `json:"mechanism,omitempty"`     // Type of mechanism
	Direction     *string   `json:"direction,omitempty"`     // positive, negative, neutral
	Reversible    *bool     `json:"reversible,omitempty"`   // Is reversible
	Conditionality []string `json:"conditionality,omitempty"` // Conditions/triggers
	Strength      *float64  `json:"strength,omitempty"`      // 0..1
}

// ExtractInfluenceQualifiers extracts standardized qualifiers from a DependencyEdge.
func ExtractInfluenceQualifiers(edge *DependencyEdge) (*InfluenceQualifiers, error) {
	if edge == nil {
		return nil, fmt.Errorf("edge is nil")
	}
	if edge.Type != DependencyTypeInfluences {
		return nil, fmt.Errorf("edge type is not INFLUENCES")
	}

	qualifiers := &InfluenceQualifiers{}

	if edge.Qualifiers == nil {
		return qualifiers, nil
	}

	// Extract via
	if viaRaw, ok := edge.Qualifiers[QualifierVia]; ok {
		if viaStr, ok := viaRaw.(string); ok {
			qualifiers.Via = &viaStr
		}
	}

	// Extract mechanism
	if mechRaw, ok := edge.Qualifiers[QualifierMechanism]; ok {
		if mechStr, ok := mechRaw.(string); ok {
			qualifiers.Mechanism = &mechStr
		}
	}

	// Extract direction
	if dirRaw, ok := edge.Qualifiers[QualifierDirection]; ok {
		if dirStr, ok := dirRaw.(string); ok {
			qualifiers.Direction = &dirStr
		}
	}

	// Extract reversible
	if revRaw, ok := edge.Qualifiers[QualifierReversible]; ok {
		if revBool, ok := revRaw.(bool); ok {
			qualifiers.Reversible = &revBool
		}
	}

	// Extract conditionality
	if condRaw, ok := edge.Qualifiers[QualifierConditionality]; ok {
		switch v := condRaw.(type) {
		case []string:
			if len(v) > 0 {
				qualifiers.Conditionality = v
			}
		case []interface{}:
			conditions := make([]string, 0, len(v))
			for _, item := range v {
				if condStr, ok := item.(string); ok {
					conditions = append(conditions, condStr)
				}
			}
			if len(conditions) > 0 {
				qualifiers.Conditionality = conditions
			}
		}
	}

	// Extract strength
	if strRaw, ok := edge.Qualifiers[QualifierStrength]; ok {
		if strFloat, ok := toFloat64(strRaw); ok {
			qualifiers.Strength = &strFloat
		}
	}

	return qualifiers, nil
}

// SetInfluenceQualifiers sets standardized qualifiers on a DependencyEdge.
func SetInfluenceQualifiers(edge *DependencyEdge, qualifiers *InfluenceQualifiers) error {
	if edge == nil {
		return fmt.Errorf("edge is nil")
	}
	if edge.Type != DependencyTypeInfluences {
		return fmt.Errorf("edge type is not INFLUENCES")
	}

	if edge.Qualifiers == nil {
		edge.Qualifiers = make(map[string]interface{})
	}

	if qualifiers == nil {
		return nil
	}

	if qualifiers.Via != nil {
		edge.Qualifiers[QualifierVia] = *qualifiers.Via
	}
	if qualifiers.Mechanism != nil {
		edge.Qualifiers[QualifierMechanism] = *qualifiers.Mechanism
	}
	if qualifiers.Direction != nil {
		edge.Qualifiers[QualifierDirection] = *qualifiers.Direction
	}
	if qualifiers.Reversible != nil {
		edge.Qualifiers[QualifierReversible] = *qualifiers.Reversible
	}
	if len(qualifiers.Conditionality) > 0 {
		edge.Qualifiers[QualifierConditionality] = qualifiers.Conditionality
	}
	if qualifiers.Strength != nil {
		edge.Qualifiers[QualifierStrength] = *qualifiers.Strength
	}

	return nil
}

// ValidateInfluenceQualifiers validates influence qualifiers.
func ValidateInfluenceQualifiers(qualifiers *InfluenceQualifiers) error {
	if qualifiers == nil {
		return nil
	}

	// Validate via (must be a canonical path)
	if qualifiers.Via != nil {
		via := strings.TrimSpace(*qualifiers.Via)
		if via == "" {
			return fmt.Errorf("via qualifier cannot be empty")
		}
		if !strings.HasPrefix(via, "/") {
			return fmt.Errorf("via qualifier must be a canonical path (starts with '/')")
		}
	}

	// Validate direction
	if qualifiers.Direction != nil {
		dir := strings.ToLower(strings.TrimSpace(*qualifiers.Direction))
		if dir != "positive" && dir != "negative" && dir != "neutral" {
			return fmt.Errorf("direction qualifier must be one of: positive, negative, neutral")
		}
	}

	// Validate strength (0..1)
	if qualifiers.Strength != nil {
		strength := *qualifiers.Strength
		if strength < 0 || strength > 1 {
			return fmt.Errorf("strength qualifier must be between 0 and 1")
		}
	}

	return nil
}

// IsInfluenceEdge checks if an edge is an influence edge.
func IsInfluenceEdge(edge *DependencyEdge) bool {
	return edge != nil && edge.Type == DependencyTypeInfluences
}

// GetViaQualifier extracts the "via" qualifier from an edge.
func GetViaQualifier(edge *DependencyEdge) (string, bool) {
	if edge == nil || edge.Qualifiers == nil {
		return "", false
	}
	viaRaw, ok := edge.Qualifiers[QualifierVia]
	if !ok {
		return "", false
	}
	viaStr, ok := viaRaw.(string)
	if !ok {
		return "", false
	}
	return viaStr, true
}

