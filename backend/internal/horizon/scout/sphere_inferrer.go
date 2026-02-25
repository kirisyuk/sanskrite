package scout

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// InferSpherePaths infers sphere paths from canonical path, fragment type, and existing qualifiers.
// Returns nil if nothing can be inferred. Caller should set f.SpherePaths only if empty.
func InferSpherePaths(cp string, fragType models.FragmentType, govType string, qualifiers map[string]interface{}) []string {
	cp = strings.TrimSpace(strings.ToLower(cp))
	if cp == "" {
		return nil
	}

	var out []string

	switch fragType {
	case models.FragmentTypeOrganization:
		if strings.HasPrefix(cp, "/gov/") {
			out = inferGovSpherePaths(cp, govType)
		} else {
			out = inferOrgSpherePaths(cp, qualifiers)
		}
	case models.FragmentTypeMarket:
		out = inferMarketSpherePaths(qualifiers)
	case models.FragmentTypeProduct:
		out = inferProductSpherePaths(cp)
	case models.FragmentTypeFacility:
		out = inferFacilitySpherePaths(cp, qualifiers)
	case models.FragmentTypeGeoRegion:
		out = inferLocSpherePaths(cp, qualifiers)
	case models.FragmentTypeCommunity:
		out = inferCommunitySpherePaths(qualifiers)
	case models.FragmentTypeDecisionPoint, models.FragmentTypeMechanism:
		out = inferDomainSpherePaths(cp)
	default:
		return nil
	}

	if len(out) == 0 {
		return nil
	}
	return taxonomy.NormalizeSpherePaths(out)
}

func inferGovSpherePaths(cp, _ string) []string {
	slug := extractLastSegment(cp)
	if slug == "" {
		return []string{"/sphere/governance"}
	}
	if strings.Contains(slug, "defense") || strings.Contains(slug, "armed-forces") || strings.Contains(slug, "military") {
		return []string{"/sphere/defense"}
	}
	if strings.Contains(slug, "finance") || strings.Contains(slug, "central-bank") || strings.Contains(slug, "treasury") {
		return []string{"/sphere/finance"}
	}
	if strings.Contains(slug, "energy") || strings.Contains(slug, "oil") || strings.Contains(slug, "mining") {
		return []string{"/sphere/energy"}
	}
	if strings.Contains(slug, "health") || strings.Contains(slug, "fda") || strings.Contains(slug, "sfda") {
		return []string{"/sphere/health"}
	}
	if strings.Contains(slug, "trade") || strings.Contains(slug, "commerce") {
		return []string{"/sphere/trade"}
	}
	return []string{"/sphere/governance"}
}

func inferOrgSpherePaths(_ string, qualifiers map[string]interface{}) []string {
	// From MarketSegmentNormDomains / MarketSegmentNormVerticals if in qualifiers
	if qualifiers != nil {
		if domains, ok := qualifiers["marketSegmentNormDomains"].([]string); ok && len(domains) > 0 {
			var out []string
			for _, d := range domains {
				sp := domainToSphere(d)
				if sp != "" {
					out = append(out, sp)
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	// Fallback: infer from industry paths in qualifiers
	if qualifiers != nil {
		if paths, ok := qualifiers["industryPaths"].([]string); ok && len(paths) > 0 {
			for _, p := range paths {
				if strings.Contains(p, "energy") || strings.Contains(p, "oil") {
					return []string{"/sphere/energy"}
				}
				if strings.Contains(p, "defense") || strings.Contains(p, "aerospace") {
					return []string{"/sphere/defense"}
				}
				if strings.Contains(p, "technology") || strings.Contains(p, "semiconductor") {
					return []string{"/sphere/technology"}
				}
				if strings.Contains(p, "finance") || strings.Contains(p, "banking") {
					return []string{"/sphere/finance"}
				}
			}
		}
	}
	return nil
}

func domainToSphere(domain string) string {
	d := strings.ToLower(strings.TrimSpace(domain))
	switch {
	case strings.Contains(d, "finance") || strings.Contains(d, "banking"):
		return "/sphere/finance"
	case strings.Contains(d, "energy") || strings.Contains(d, "oil") || strings.Contains(d, "gas"):
		return "/sphere/energy"
	case strings.Contains(d, "technology") || strings.Contains(d, "tech"):
		return "/sphere/technology"
	case strings.Contains(d, "defense") || strings.Contains(d, "aerospace"):
		return "/sphere/defense"
	case strings.Contains(d, "health"):
		return "/sphere/health"
	case strings.Contains(d, "logistics") || strings.Contains(d, "transport"):
		return "/sphere/logistics"
	case strings.Contains(d, "trade"):
		return "/sphere/trade"
	default:
		return ""
	}
}

func inferMarketSpherePaths(qualifiers map[string]interface{}) []string {
	if qualifiers == nil {
		return nil
	}
	if d, ok := qualifiers["marketDomain"].(string); ok && d != "" {
		sp := domainToSphere(d)
		if sp != "" {
			return []string{sp}
		}
	}
	return nil
}

func inferProductSpherePaths(cp string) []string {
	cp = strings.ToLower(cp)
	if strings.Contains(cp, "/product/raw/mineral") || strings.Contains(cp, "/product/raw") {
		return []string{"/sphere/energy", "/sphere/technology"}
	}
	if strings.Contains(cp, "/product/finished/electronics") || strings.Contains(cp, "/product/component/electronic") {
		return []string{"/sphere/technology"}
	}
	if strings.Contains(cp, "/product/raw/hydrocarbon") || strings.Contains(cp, "/product/raw/energy") {
		return []string{"/sphere/energy"}
	}
	if strings.Contains(cp, "/product/service") {
		return []string{"/sphere/technology"}
	}
	return nil
}

func inferFacilitySpherePaths(cp string, _ map[string]interface{}) []string {
	cp = strings.ToLower(cp)
	if strings.Contains(cp, "refinery") || strings.Contains(cp, "terminal") || strings.Contains(cp, "pipeline") {
		return []string{"/sphere/energy"}
	}
	if strings.Contains(cp, "port") || strings.Contains(cp, "harbor") {
		return []string{"/sphere/logistics/maritime"}
	}
	if strings.Contains(cp, "military") || strings.Contains(cp, "base") {
		return []string{"/sphere/defense"}
	}
	return nil
}

func inferLocSpherePaths(cp string, qualifiers map[string]interface{}) []string {
	if qualifiers != nil {
		if gt, ok := qualifiers["geoFeatureType"].(string); ok {
			gt = strings.ToLower(strings.TrimSpace(gt))
			switch gt {
			case "strait", "canal":
				return []string{"/sphere/logistics/maritime"}
			case "pass", "border-crossing":
				return []string{"/sphere/logistics"}
			case "pipeline-corridor":
				return []string{"/sphere/energy/oil-gas"}
			case "port":
				return []string{"/sphere/logistics/maritime"}
			}
		}
	}
	cp = strings.ToLower(cp)
	if strings.Contains(cp, "hormuz") || strings.Contains(cp, "suez") || strings.Contains(cp, "malacca") ||
		strings.Contains(cp, "panama") || strings.Contains(cp, "bab-el-mandeb") || strings.Contains(cp, "bosphorus") {
		return []string{"/sphere/logistics/maritime"}
	}
	return nil
}

func inferCommunitySpherePaths(qualifiers map[string]interface{}) []string {
	if qualifiers == nil {
		return nil
	}
	if ct, ok := qualifiers["communityType"].(string); ok {
		ct = strings.ToLower(strings.TrimSpace(ct))
		switch ct {
		case "trade":
			return []string{"/sphere/trade"}
		case "religious":
			return []string{"/sphere/social/religious-affairs"}
		case "professional", "industry-association":
			return []string{"/sphere/social"}
		case "dynasty", "tribe":
			return []string{"/sphere/governance"}
		}
	}
	return nil
}

func inferDomainSpherePaths(cp string) []string {
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) >= 2 {
		domain := strings.ToLower(parts[1])
		sp := domainToSphere(domain)
		if sp != "" {
			return []string{sp}
		}
	}
	return nil
}

func extractLastSegment(path string) string {
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		return ""
	}
	return strings.ToLower(parts[len(parts)-1])
}

// InferResolutionHint infers resolution from fragment type, gov type, path, and parent.
// Returns "" if unspecified. Values: "macro" | "unit" | "detail".
func InferResolutionHint(fragType models.FragmentType, govType string, cp string, parentPath string) string {
	cp = strings.TrimSpace(cp)
	parentPath = strings.TrimSpace(parentPath)
	govType = strings.ToLower(strings.TrimSpace(govType))

	switch fragType {
	case models.FragmentTypeOrganization:
		if strings.HasPrefix(cp, "/gov/") {
			return inferGovResolution(govType)
		}
		if parentPath != "" {
			return "unit"
		}
		return "macro"
	case models.FragmentTypeMarket:
		if parentPath == "" {
			return "macro"
		}
		depth := pathDepth(cp)
		if depth <= 3 {
			return "unit"
		}
		return "detail"
	case models.FragmentTypeProduct:
		depth := pathDepth(cp)
		if depth <= 3 {
			return "macro"
		}
		if depth == 4 {
			return "unit"
		}
		return "detail"
	case models.FragmentTypeFacility:
		return "detail"
	case models.FragmentTypeGeoRegion:
		depth := pathDepth(cp)
		if depth <= 1 {
			return "macro"
		}
		if depth == 2 {
			return "unit"
		}
		return "detail"
	case models.FragmentTypeCommunity:
		if parentPath != "" {
			return "unit"
		}
		return "macro"
	case models.FragmentTypePerson:
		return "detail"
	case models.FragmentTypeDecisionPoint, models.FragmentTypeMechanism:
		return "detail"
	default:
		return ""
	}
}

func inferGovResolution(govType string) string {
	switch govType {
	case "government", "ministry", "authority":
		return "macro"
	case "department", "agency", "committee", "council", "commission":
		return "unit"
	case "service", "bureau", "office":
		return "detail"
	default:
		return "macro"
	}
}

func pathDepth(path string) int {
	path = strings.Trim(path, "/")
	if path == "" {
		return 0
	}
	return len(strings.Split(path, "/"))
}
