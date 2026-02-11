package models

import "strings"

// HasOwnershipEvidence returns true when evidence indicates legal/equity ownership.
func HasOwnershipEvidence(reason string, qualifiers map[string]interface{}, metrics map[string]MetricValue) bool {
	ownershipMetricKeys := []string{
		"ownership_percent",
		"voting_percent",
		"equity_share",
		"stake_percent",
		"controlling_interest",
	}
	for _, k := range ownershipMetricKeys {
		if mv, ok := metrics[k]; ok && mv.Value > 0 {
			return true
		}
	}

	ownershipQualifierKeys := []string{
		"ownership_percent",
		"voting_percent",
		"equity_share",
		"stake_percent",
		"ownershipEvidence",
		"isOwner",
	}
	for _, k := range ownershipQualifierKeys {
		raw, ok := qualifiers[k]
		if !ok {
			continue
		}
		switch v := raw.(type) {
		case bool:
			if v {
				return true
			}
		case float64:
			if v > 0 {
				return true
			}
		case int:
			if v > 0 {
				return true
			}
		case string:
			s := strings.ToLower(strings.TrimSpace(v))
			if s == "" {
				continue
			}
			if s == "true" || s == "yes" || s == "owner" {
				return true
			}
		}
	}

	text := strings.ToLower(strings.TrimSpace(reason))
	if text == "" {
		return false
	}
	ownershipTerms := []string{
		"owns",
		"owned",
		"ownership",
		"stake",
		"equity",
		"acquire",
		"acquired",
		"subsidiary",
		"majority share",
		"controlling interest",
	}
	for _, term := range ownershipTerms {
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}

// CanonicalizationPenalty returns a confidence penalty for weak canonical rewrites.
func CanonicalizationPenalty(oldType, newType DependencyType, sourceType, targetType FragmentType) float64 {
	switch {
	case oldType == DependencyTypeProduces &&
		newType == DependencyTypeSuppliesTo &&
		sourceType == FragmentTypeOrganization &&
		targetType == FragmentTypeOrganization:
		return 0.10
	case oldType == DependencyTypeInvestsIn &&
		newType == DependencyTypeOperatesIn &&
		sourceType == FragmentTypeOrganization &&
		targetType == FragmentTypeFacility:
		return 0.10
	default:
		return 0
	}
}
