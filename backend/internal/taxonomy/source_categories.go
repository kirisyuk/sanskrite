package taxonomy

import "strings"

var sourceCategoryAllowlist = []string{
	"agriculture",
	"climate",
	"compliance",
	"cyber",
	"defense",
	"economy",
	"energy",
	"environment",
	"finance",
	"geopolitics",
	"health",
	"infrastructure",
	"legal",
	"logistics",
	"markets",
	"medicine",
	"policy",
	"science",
	"social",
	"technology",
	"transport",
}

var sourceCategorySet = func() map[string]struct{} {
	out := make(map[string]struct{}, len(sourceCategoryAllowlist))
	for _, c := range sourceCategoryAllowlist {
		out[c] = struct{}{}
	}
	return out
}()

// sourceCategorySegmentAliases keeps canonical L2/L3 slugs compact and stable.
// L1 remains strict allow-list; L2+ remain open but common aliases are normalized.
var sourceCategorySegmentAliases = map[string]string{
	"artificial-intelligence":             "ai",
	"machine-learning":                    "ai",
	"ml":                                  "ai",
	"ai-ml":                               "ai",
	"large-language-model":                "llm",
	"large-language-models":               "llm",
	"generative-ai":                       "genai",
	"semiconductor":                       "semiconductors",
	"semis":                               "semiconductors",
	"chip":                                "semiconductors",
	"chips":                               "semiconductors",
	"cloud-computing":                     "cloud",
	"dev-tools":                           "devtools",
	"developer-tools":                     "devtools",
	"regulatory":                          "regulation",
	"regulations":                         "regulation",
	"public-policy":                       "regulation",
	"foreign-exchange":                    "fx",
	"equity":                              "equities",
	"stocks":                              "equities",
	"bond":                                "fixed-income",
	"bonds":                               "fixed-income",
	"pharmaceutical":                      "pharma",
	"pharmaceuticals":                     "pharma",
	"bio-tech":                            "biotech",
	"drug-approvals":                      "approvals",
	"clinical-studies":                    "clinical-trials",
	"international-relations":             "diplomacy",
	"intl-relations":                      "diplomacy",
	"cyber-security":                      "security",
	"cyber-threat-intelligence":           "threat-intel",
	"critical-infrastructure":             "critical-infra",
	"supply-chain":                        "supply-chain-risk",
	"value-chain":                         "supply-chain-risk",
	"venture-capital":                     "vc",
	"private-equity":                      "pe",
	"payment":                             "payments",
	"anti-money-laundering":               "aml",
	"know-your-customer":                  "kyc",
	"aml-kyc":                             "aml-kyc",
	"environmental-social-governance":     "esg",
	"export-controls":                     "export-control",
	"electric-vehicles":                   "ev",
	"electric-vehicle":                    "ev",
	"nuclear-energy":                      "nuclear",
	"oil-and-gas":                         "oil-gas",
	"natural-gas":                         "gas",
	"asset-management":                    "asset-management",
	"earth-observation":                   "earth-observation",
	"public-healthcare":                   "public-health",
	"health-care":                         "healthcare-systems",
	"civil-society":                       "civil-society",
	"freight-forwarding":                  "freight",
	"ocean-shipping":                      "shipping",
	"maritime-shipping":                   "shipping",
	"rail-transport":                      "rail",
	"air-transport":                       "aviation",
	"road-transport":                      "road",
	"intelligence-analysis":               "intel-analysis",
	"standards-and-certification":         "standards",
	"standards-development":               "standards",
	"law-enforcement":                     "enforcement",
	"waste-management":                    "waste",
	"water-security":                      "water",
	"food-security":                       "food-security",
	"agricultural-technology":             "agtech",
	"fiscal-policy":                       "fiscal-policy",
	"monetary-policy":                     "monetary-policy",
	"research-and-development":            "r-and-d",
	"space-policy":                        "space-policy",
	"human-rights":                        "human-rights",
	"labor-market":                        "labor",
	"public-procurement":                  "public-procurement",
	"industrial-policy":                   "industrial-policy",
	"trade-policy":                        "trade-policy",
	"sanctions-compliance":                "sanctions-compliance",
	"privacy-and-data-protection":         "privacy",
	"critical-minerals":                   "critical-minerals",
	"critical-raw-materials":              "critical-minerals",
	"climate-change":                      "climate-risk",
	"conflict-analysis":                   "conflict",
	"international-security":              "security",
	"multilateral-organizations":          "multilateral",
	"international-organizations":         "multilateral",
	"transportation":                      "transport",
	"logistics-and-supply-chain":          "supply-chain-risk",
	"platform-regulation":                 "platform-regulation",
	"antitrust-and-competition":           "antitrust",
	"market-structure":                    "market-structure",
	"critical-national-infrastructure":    "critical-infra",
	"critical-information-infrastructure": "critical-infra",
	"critical-infrastructure-protection":  "critical-infra",
}

// sourceCategoryRecommendedTree is an expanded suggested L2/L3 vocabulary.
// It is advisory only; runtime keeps L2+ open.
var sourceCategoryRecommendedTree = map[string]map[string][]string{
	"agriculture": {
		"crops":         {"grains", "oilseeds", "yield"},
		"livestock":     {"dairy", "poultry", "feed"},
		"fertilizers":   {"nitrogen", "potash", "phosphate"},
		"agtech":        {"precision-farming", "drones", "irrigation"},
		"food-security": {"supply-shocks", "prices", "imports"},
	},
	"climate": {
		"climate-risk": {"extreme-weather", "transition-risk", "adaptation"},
		"emissions":    {"co2", "methane", "reporting"},
		"policy":       {"carbon-pricing", "net-zero", "cbam"},
	},
	"compliance": {
		"aml-kyc":              {"screening", "beneficial-ownership", "suspicious-activity"},
		"sanctions-compliance": {"screening", "enforcement", "secondary-sanctions"},
		"export-control":       {"dual-use", "licensing", "entity-lists"},
		"privacy":              {"gdpr", "cross-border-transfer", "data-governance"},
	},
	"cyber": {
		"threat-intel":      {"apt", "ransomware", "supply-chain"},
		"vulnerabilities":   {"zero-day", "cve", "patching"},
		"incident-response": {"breach", "forensics", "containment"},
		"policy":            {"national-strategy", "critical-infra", "reporting-rules"},
	},
	"defense": {
		"procurement":      {"contracts", "budgets", "platforms"},
		"military-tech":    {"drones", "missiles", "ew"},
		"defense-industry": {"prime-contractors", "supply-chain", "exports"},
	},
	"economy": {
		"macro":     {"growth", "recession", "productivity"},
		"inflation": {"cpi", "ppi", "core"},
		"labor":     {"employment", "wages", "unemployment"},
		"trade":     {"balance", "current-account", "exports"},
		"policy":    {"fiscal-policy", "monetary-policy", "industrial-policy"},
	},
	"energy": {
		"oil-gas":           {"upstream", "lng", "opec"},
		"power":             {"grid", "generation", "capacity"},
		"renewables":        {"solar", "wind", "storage"},
		"nuclear":           {"reactors", "fuel-cycle", "safety"},
		"critical-minerals": {"lithium", "nickel", "rare-earths"},
	},
	"environment": {
		"water":        {"scarcity", "quality", "infrastructure"},
		"pollution":    {"air", "soil", "industrial"},
		"waste":        {"recycling", "hazardous", "epr"},
		"biodiversity": {"conservation", "habitats", "deforestation"},
	},
	"finance": {
		"banking":          {"credit", "liquidity", "stress"},
		"payments":         {"card", "instant-payments", "settlement"},
		"insurance":        {"underwriting", "cat-risk", "claims"},
		"asset-management": {"fund-flows", "allocation", "aum"},
		"vc":               {"rounds", "valuation", "fundraising"},
		"pe":               {"buyouts", "deals", "exits"},
	},
	"geopolitics": {
		"conflict":     {"escalation", "ceasefire", "deterrence"},
		"sanctions":    {"packages", "enforcement", "evasion"},
		"diplomacy":    {"summits", "treaties", "mediation"},
		"alliances":    {"nato", "brics", "g7"},
		"intelligence": {"osint", "strategic-assessment", "risk-signals"},
	},
	"health": {
		"public-health":      {"outbreaks", "vaccination", "preparedness"},
		"healthcare-systems": {"capacity", "workforce", "financing"},
		"epidemiology":       {"surveillance", "trends", "risk-factors"},
		"policy":             {"reimbursement", "access", "regulation"},
	},
	"infrastructure": {
		"digital-infrastructure": {"datacenters", "subsea-cables", "cloud-regions"},
		"utilities":              {"water", "power", "wastewater"},
		"construction":           {"projects", "contractors", "permits"},
		"pipelines":              {"oil", "gas", "cross-border"},
	},
	"legal": {
		"legislation":     {"bills", "amendments", "frameworks"},
		"litigation":      {"civil", "commercial", "class-actions"},
		"court-decisions": {"precedents", "appeals", "rulings"},
		"antitrust":       {"merger-control", "abuse", "fines"},
	},
	"logistics": {
		"shipping": {"rates", "disruptions", "routes"},
		"ports":    {"congestion", "capacity", "throughput"},
		"customs":  {"clearance", "tariffs", "controls"},
		"freight":  {"air-freight", "rail-freight", "intermodal"},
	},
	"markets": {
		"commodities":  {"energy", "metals", "agri"},
		"equities":     {"earnings", "indices", "valuation"},
		"fixed-income": {"yields", "spreads", "issuance"},
		"fx":           {"dxy", "volatility", "intervention"},
		"derivatives":  {"options", "futures", "risk-hedging"},
	},
	"medicine": {
		"pharma":          {"pipeline", "approvals", "manufacturing"},
		"biotech":         {"platforms", "modalities", "research"},
		"medical-devices": {"diagnostics", "implants", "monitoring"},
		"clinical-trials": {"phase-1", "phase-2", "phase-3"},
	},
	"policy": {
		"regulation":         {"ai-regulation", "platform-regulation", "standards"},
		"public-procurement": {"defense", "health", "infrastructure"},
		"standards":          {"technical", "compliance", "certification"},
		"enforcement":        {"fines", "inspections", "orders"},
	},
	"science": {
		"materials":     {"semiconductors", "batteries", "composites"},
		"earth-science": {"climate", "seismic", "ocean"},
		"space":         {"launch", "satellites", "missions"},
	},
	"social": {
		"demography":   {"aging", "urbanization", "fertility"},
		"migration":    {"flows", "refugees", "policy"},
		"education":    {"skills", "access", "outcomes"},
		"labor-rights": {"collective-bargaining", "strikes", "standards"},
	},
	"technology": {
		"ai":             {"genai", "llm", "agents", "ai-safety"},
		"semiconductors": {"foundries", "equipment", "supply-chain"},
		"cloud":          {"regions", "outages", "multi-cloud"},
		"platforms":      {"marketplaces", "app-stores", "network-effects"},
		"devtools":       {"ci-cd", "observability", "security"},
		"startups":       {"funding", "unicorns", "accelerators", "layoffs"},
	},
	"transport": {
		"aviation":       {"airlines", "airports", "airspace"},
		"rail":           {"freight", "passenger", "infrastructure"},
		"maritime":       {"shipping", "ports", "chokepoints"},
		"road":           {"trucking", "ev", "autonomy"},
		"urban-mobility": {"public-transit", "micromobility", "policy"},
	},
}

// AllowedSourceCategories returns the curated L1 list for /source/{category}/... taxonomy.
func AllowedSourceCategories() []string {
	out := make([]string, len(sourceCategoryAllowlist))
	copy(out, sourceCategoryAllowlist)
	return out
}

// RecommendedSourceCategoryTree returns suggested L2/L3 vocabulary for source paths.
// This tree is advisory and does not enforce strict validation for L2/L3.
func RecommendedSourceCategoryTree() map[string]map[string][]string {
	out := make(map[string]map[string][]string, len(sourceCategoryRecommendedTree))
	for l1, l2map := range sourceCategoryRecommendedTree {
		l2copy := make(map[string][]string, len(l2map))
		for l2, l3 := range l2map {
			l3copy := make([]string, len(l3))
			copy(l3copy, l3)
			l2copy[l2] = l3copy
		}
		out[l1] = l2copy
	}
	return out
}

func normalizeSourceCategorySegment(segment string) string {
	if segment == "" {
		return ""
	}
	if canonical, ok := sourceCategorySegmentAliases[segment]; ok {
		return canonical
	}
	return segment
}

// NormalizeSourceCategoryPath canonicalizes source category path into:
// /source/{category}/{subcategory}[/{topic}...]
//
// Inputs may be:
// - full path: /source/medicine/pharma
// - partial path: source/medicine/pharma
// - shorthand: medicine/pharma
//
// L1 category is closed allowlist (AllowedSourceCategories), L2+ are open.
// Common L2/L3 aliases are normalized to canonical slugs.
// If subcategory is missing, "general" is used.
func NormalizeSourceCategoryPath(in string) (string, bool) {
	in = strings.TrimSpace(strings.ReplaceAll(in, "\\", "/"))
	if in == "" {
		return "", false
	}
	if !strings.HasPrefix(in, "/") {
		in = "/" + in
	}
	partsRaw := strings.Split(strings.Trim(in, "/"), "/")
	parts := make([]string, 0, len(partsRaw))
	for _, p := range partsRaw {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		parts = append(parts, p)
	}
	if len(parts) == 0 {
		return "", false
	}

	if strings.EqualFold(parts[0], "source") {
		parts = parts[1:]
	}
	if len(parts) == 0 {
		return "", false
	}

	category := Slugify(parts[0])
	if category == "" {
		return "", false
	}
	if _, ok := sourceCategorySet[category]; !ok {
		return "", false
	}

	segments := []string{"source", category}
	for _, p := range parts[1:] {
		s := Slugify(p)
		if s == "" {
			continue
		}
		segments = append(segments, normalizeSourceCategorySegment(s))
	}
	if len(segments) == 2 {
		segments = append(segments, "general")
	}
	return JoinPath(segments...), true
}

// NormalizeSourceCategoryPaths normalizes and deduplicates source category paths.
func NormalizeSourceCategoryPaths(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		p, ok := NormalizeSourceCategoryPath(raw)
		if !ok {
			continue
		}
		if _, exists := seen[p]; exists {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}
