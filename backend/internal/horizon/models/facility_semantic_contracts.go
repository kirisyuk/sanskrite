package models

import (
	"fmt"
	"strings"
)

// FacilitySemantic is a closed business lane for facility scouting specialization.
type FacilitySemantic string

const (
	FacilitySemanticNone        FacilitySemantic = ""
	FacilitySemanticRoute       FacilitySemantic = "route"
	FacilitySemanticSupply      FacilitySemantic = "supply"
	FacilitySemanticProcurement FacilitySemantic = "procurement"
)

var (
	facilityRouteHints = []string{
		"route",
		"corridor",
		"pipeline",
		"shipping-lane",
		"logistics-lane",
		"transport-lane",
		"trade-lane",
		"canal",
	}
	facilitySupplyHints = []string{
		"supply",
		"supplier",
		"downstream",
		"upstream",
		"distribution",
		"fulfillment",
	}
	facilityProcurementHints = []string{
		"procurement",
		"sourcing",
		"purchasing",
		"tender",
		"vendor",
	}
)

func containsAnyHintToken(text string, hints []string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return false
	}
	for _, h := range hints {
		if strings.Contains(text, h) {
			return true
		}
	}
	return false
}

func valueContainsFacilityHint(raw interface{}, hints []string) bool {
	switch v := raw.(type) {
	case string:
		return containsAnyHintToken(v, hints)
	case []string:
		for _, item := range v {
			if containsAnyHintToken(item, hints) {
				return true
			}
		}
	case []interface{}:
		for _, item := range v {
			if valueContainsFacilityHint(item, hints) {
				return true
			}
		}
	case map[string]interface{}:
		for k, item := range v {
			if containsAnyHintToken(k, hints) {
				return true
			}
			if valueContainsFacilityHint(item, hints) {
				return true
			}
		}
	case map[string]string:
		for k, item := range v {
			if containsAnyHintToken(k, hints) || containsAnyHintToken(item, hints) {
				return true
			}
		}
	default:
		return containsAnyHintToken(fmt.Sprint(raw), hints)
	}
	return false
}

func firstObjectQualifierRawFromMaps(qualifiers map[string]interface{}, identifiers map[string]string) string {
	if identifiers != nil {
		for _, key := range []string{"objectQualifier", "object_qualifier", "qualifier", "businessQualifier", "business_qualifier"} {
			if v, ok := identifiers[key]; ok {
				s := strings.TrimSpace(v)
				if s != "" {
					return s
				}
			}
		}
	}
	if qualifiers != nil {
		for _, key := range []string{"objectQualifier", "object_qualifier", "qualifier", "businessQualifier", "business_qualifier"} {
			if v, ok := qualifiers[key]; ok {
				if s, ok := v.(string); ok {
					s = strings.TrimSpace(s)
					if s != "" {
						return s
					}
				}
			}
		}
	}
	return ""
}

// FacilitySemanticFromObjectQualifier maps closed object qualifiers to facility semantic lanes.
func FacilitySemanticFromObjectQualifier(q ObjectQualifier) FacilitySemantic {
	switch q {
	case ObjectQualifierRoute:
		return FacilitySemanticRoute
	case ObjectQualifierSupply:
		return FacilitySemanticSupply
	case ObjectQualifierProcurement, ObjectQualifierProcurer:
		return FacilitySemanticProcurement
	default:
		return FacilitySemanticNone
	}
}

// DetectFacilitySemantic resolves facility business lane using the same contract for runtime and prompts.
// The detector never returns non-facility semantics for non-/fac/ paths.
func DetectFacilitySemantic(canonicalPath string, qualifiers map[string]interface{}, identifiers map[string]string, labels map[string]string) FacilitySemantic {
	cp := strings.TrimSpace(strings.ToLower(canonicalPath))
	if !strings.HasPrefix(cp, "/fac/") {
		return FacilitySemanticNone
	}

	if raw := firstObjectQualifierRawFromMaps(qualifiers, identifiers); raw != "" {
		if sem := FacilitySemanticFromObjectQualifier(NormalizeObjectQualifier(raw)); sem != FacilitySemanticNone {
			return sem
		}
	}

	if containsAnyHintToken(cp, facilityRouteHints) {
		return FacilitySemanticRoute
	}
	if containsAnyHintToken(cp, facilityProcurementHints) {
		return FacilitySemanticProcurement
	}
	if containsAnyHintToken(cp, facilitySupplyHints) {
		return FacilitySemanticSupply
	}

	for k, v := range qualifiers {
		if containsAnyHintToken(k, facilityRouteHints) || valueContainsFacilityHint(v, facilityRouteHints) {
			return FacilitySemanticRoute
		}
		if containsAnyHintToken(k, facilityProcurementHints) || valueContainsFacilityHint(v, facilityProcurementHints) {
			return FacilitySemanticProcurement
		}
		if containsAnyHintToken(k, facilitySupplyHints) || valueContainsFacilityHint(v, facilitySupplyHints) {
			return FacilitySemanticSupply
		}
	}
	for k, v := range identifiers {
		if containsAnyHintToken(k, facilityRouteHints) || containsAnyHintToken(v, facilityRouteHints) {
			return FacilitySemanticRoute
		}
		if containsAnyHintToken(k, facilityProcurementHints) || containsAnyHintToken(v, facilityProcurementHints) {
			return FacilitySemanticProcurement
		}
		if containsAnyHintToken(k, facilitySupplyHints) || containsAnyHintToken(v, facilitySupplyHints) {
			return FacilitySemanticSupply
		}
	}
	for _, v := range labels {
		if containsAnyHintToken(v, facilityRouteHints) {
			return FacilitySemanticRoute
		}
		if containsAnyHintToken(v, facilityProcurementHints) {
			return FacilitySemanticProcurement
		}
		if containsAnyHintToken(v, facilitySupplyHints) {
			return FacilitySemanticSupply
		}
	}
	return FacilitySemanticNone
}

// DetectFacilitySemanticFromFragment applies DetectFacilitySemantic to a fragment.
func DetectFacilitySemanticFromFragment(fragment *HorizonFragment) FacilitySemantic {
	if fragment == nil {
		return FacilitySemanticNone
	}
	cp := strings.TrimSpace(fragment.CanonicalPath)
	if !strings.HasPrefix(strings.ToLower(cp), "/fac/") && fragment.Type != FragmentTypeFacility {
		return FacilitySemanticNone
	}
	return DetectFacilitySemantic(cp, fragment.Qualifiers, fragment.Identifiers, fragment.Labels)
}
