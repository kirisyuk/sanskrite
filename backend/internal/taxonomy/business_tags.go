package taxonomy

import "strings"

const (
	bizTagPrefix = "/biz/"
)

var bizTagFamilies = map[string]struct{}{
	"function":       {},
	"economic-role":  {},
	"risk-exposure":  {},
	"value-pool":     {},
	"replaceability": {},
}

// BusinessTagRulesPrompt defines canonical business-relevance tag taxonomy.
// These tags are stored in fragment.tags as /biz/... paths.
func BusinessTagRulesPrompt() string {
	return `Business relevance tags (HIGH PRIORITY for ORG/FAC/GOV/PRODUCT/MARKET):
- Use canonical path tags in fragment.tags with this format:
  - /biz/function/{slug}
  - /biz/economic-role/{slug}
  - /biz/risk-exposure/{slug}
  - /biz/value-pool/{slug}
  - /biz/replaceability/{low|medium|high|variable}
- Add 3-6 tags when evidence exists; prefer stable business semantics over marketing wording.
- Examples:
  - /biz/function/regulation
  - /biz/economic-role/public-institution
  - /biz/risk-exposure/financial-counterparty
  - /biz/value-pool/energy-supply`
}

// NormalizeBusinessTagPaths normalizes /biz/... paths and drops invalid business tags.
func NormalizeBusinessTagPaths(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(tags))
	for _, raw := range tags {
		p, ok := NormalizeBusinessTagPath(raw)
		if !ok {
			continue
		}
		if _, exists := seen[p]; exists {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// NormalizeBusinessTagPath normalizes one /biz/{family}/{value} path.
func NormalizeBusinessTagPath(in string) (string, bool) {
	s := strings.TrimSpace(in)
	if s == "" {
		return "", false
	}
	if !strings.HasPrefix(s, "/") {
		s = "/" + s
	}
	parts := strings.Split(strings.Trim(s, "/"), "/")
	if len(parts) != 3 || parts[0] != "biz" {
		return "", false
	}
	family := Slugify(parts[1])
	if _, ok := bizTagFamilies[family]; !ok {
		return "", false
	}
	value := Slugify(parts[2])
	if value == "" {
		return "", false
	}
	return "/biz/" + family + "/" + value, true
}

// NormalizeFragmentTagsWithBusiness keeps existing tags, normalizes market tags for MARKET,
// and injects deterministic /biz/... business tags derived from fragment data.
func NormalizeFragmentTagsWithBusiness(
	tags []string,
	fragmentType string,
	canonicalPath string,
	marketSegments []string,
	orgSize string,
	marketDomain string,
) []string {
	nonBiz := make([]string, 0, len(tags))
	for _, raw := range tags {
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(t), bizTagPrefix) {
			continue
		}
		nonBiz = append(nonBiz, t)
	}

	if strings.EqualFold(strings.TrimSpace(fragmentType), "MARKET") {
		nonBiz = NormalizeMarketTags(nonBiz)
	} else {
		seen := map[string]struct{}{}
		out := make([]string, 0, len(nonBiz))
		for _, t := range nonBiz {
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			out = append(out, t)
		}
		nonBiz = out
	}

	existingBiz := NormalizeBusinessTagPaths(tags)
	derivedBiz := deriveBusinessTagPaths(fragmentType, canonicalPath, marketSegments, orgSize, marketDomain)
	return mergeTags(nonBiz, existingBiz, derivedBiz)
}

func deriveBusinessTagPaths(fragmentType string, canonicalPath string, marketSegments []string, orgSize string, marketDomain string) []string {
	out := make([]string, 0, 10)
	add := func(path string) {
		if p, ok := NormalizeBusinessTagPath(path); ok {
			out = append(out, p)
		}
	}

	ft := strings.ToUpper(strings.TrimSpace(fragmentType))
	cp := strings.TrimSpace(strings.ToLower(canonicalPath))
	switch ft {
	case "ORGANIZATION", "ORG":
		add("/biz/function/operator")
		add("/biz/economic-role/corporate-entity")
		if strings.HasPrefix(cp, "/gov/") {
			add("/biz/function/regulation")
			add("/biz/economic-role/public-institution")
			add("/biz/risk-exposure/policy-regulatory")
		}
	case "FACILITY", "SITE":
		add("/biz/function/production-node")
		add("/biz/economic-role/infrastructure-asset")
		add("/biz/risk-exposure/operational-disruption")
	case "PRODUCT":
		add("/biz/function/value-offering")
		add("/biz/economic-role/tradable-output")
		add("/biz/replaceability/variable")
	case "MARKET":
		add("/biz/function/demand-pool")
		add("/biz/economic-role/market-space")
		add("/biz/value-pool/market-liquidity")
	case "PROGRAM", "PROJECT":
		add("/biz/function/policy-instrument")
		add("/biz/economic-role/public-program")
	}

	switch NormalizeOrgSize(orgSize) {
	case "mega", "enterprise":
		add("/biz/replaceability/low")
	case "le":
		add("/biz/replaceability/medium")
	case "sme":
		add("/biz/replaceability/high")
	}

	domains := map[string]struct{}{}
	if d := Slugify(strings.TrimSpace(marketDomain)); d != "" {
		domains[d] = struct{}{}
	}
	for _, seg := range NormalizeSegmentPaths(marketSegments) {
		parts := strings.Split(strings.Trim(seg, "/"), "/")
		if len(parts) == 4 && parts[0] == "segment" {
			domains[parts[1]] = struct{}{}
		}
	}
	for d := range domains {
		switch d {
		case "finance":
			add("/biz/risk-exposure/financial-counterparty")
			add("/biz/value-pool/financial-services")
		case "technology":
			add("/biz/risk-exposure/technology-cyber")
			add("/biz/value-pool/digital-systems")
		case "industrial":
			add("/biz/risk-exposure/industrial-capacity")
			add("/biz/value-pool/manufacturing-output")
		case "consumer":
			add("/biz/risk-exposure/demand-volatility")
			add("/biz/value-pool/consumer-demand")
		case "healthcare":
			add("/biz/risk-exposure/regulatory-clinical")
			add("/biz/value-pool/health-services")
		case "energy":
			add("/biz/risk-exposure/commodity-price")
			add("/biz/value-pool/energy-supply")
		case "materials":
			add("/biz/risk-exposure/commodity-price")
			add("/biz/value-pool/materials-supply")
		case "telecom":
			add("/biz/risk-exposure/infrastructure-outage")
			add("/biz/value-pool/connectivity")
		case "real-estate":
			add("/biz/risk-exposure/financing-rate")
			add("/biz/value-pool/asset-rent")
		case "public-sector":
			add("/biz/function/regulation")
			add("/biz/economic-role/public-institution")
			add("/biz/risk-exposure/policy-regulatory")
			add("/biz/value-pool/public-services")
		case "professional-services":
			add("/biz/risk-exposure/talent-concentration")
			add("/biz/value-pool/professional-fee")
		}
	}

	return dedupeTags(out)
}

func mergeTags(groups ...[]string) []string {
	out := make([]string, 0, 16)
	seen := map[string]struct{}{}
	for _, g := range groups {
		for _, t := range g {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func dedupeTags(tags []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}
