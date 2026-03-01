package taxonomy

import (
	"fmt"
	"strings"
)

// CanonicalRules defines the shared canonical path contract used across
// prompts, validation, and policy checks.
type CanonicalRules struct {
	Org     OrgCanonicalRules
	Gov     GovCanonicalRules
	Product ProductCanonicalRules
	Geo     GeoCanonicalRules
}

type OrgCanonicalRules struct {
	Format     string
	LegalForms []string
	Example    string
}

type GovCanonicalRules struct {
	Format  string
	Example string
}

type ProductCanonicalRules struct {
	Format     string
	Categories []string
	Types      []string
	Stages     []string
	Example    string
}

type GeoCanonicalRules struct {
	Format  string
	Example string
	Notes   []string
}

// LegalForms returns the canonical list of legal form tokens for org canonical paths.
// Single source of truth for legal_form suffix parsing, prompts, and validation.
func LegalForms() []string {
	return append([]string(nil), DefaultCanonicalRules().Org.LegalForms...)
}

// MarketExamplePathsForPrompt returns canonical market path examples for prompts and validation.
// Single source of truth: L1 (domain), L2 (vertical), L3 (sub-vertical) — sector-diverse (agro, energy, tech, finance).
func MarketExamplePathsForPrompt() []string {
	return []string{
		"/market/iq/agriculture",                     // L1
		"/market/iq/agriculture/crop-production",      // L2
		"/market/ae/energy/oil-gas",                    // L2
		"/market/global/technology/semiconductors",    // L2
		"/market/sa/finance/banking",                 // L2
		"/market/sa/finance/banking/islamic-banking", // L3
		"/market/eu/finance/banking",                 // regional geo
		"/market/brics/energy/oil-gas",               // regional geo
	}
}

// MarketExamplePathsString returns comma-separated market examples with level hints for prompts.
func MarketExamplePathsString() string {
	ex := MarketExamplePathsForPrompt()
	// Format: path (L1), path (L2), path (L3) — keep it compact
	hints := map[string]string{
		"/market/iq/agriculture":                     "L1",
		"/market/iq/agriculture/crop-production":     "L2",
		"/market/ae/energy/oil-gas":                   "L2",
		"/market/global/technology/semiconductors":    "L2",
		"/market/sa/finance/banking":                 "L2",
		"/market/sa/finance/banking/islamic-banking": "L3",
		"/market/eu/finance/banking":                 "regional",
		"/market/brics/energy/oil-gas":               "regional",
	}
	parts := make([]string, 0, len(ex))
	for _, p := range ex {
		if h, ok := hints[p]; ok {
			parts = append(parts, p+" ("+h+")")
		} else {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ", ")
}

// MechanismSlugExamples returns canonical mechanism slug examples for prompts.
// Single source of truth: sector-diverse (banking, mining, export, pharma).
func MechanismSlugExamples() []string {
	return []string{
		"banking-license", "mining-license", "euv-export-control",
		"entity-list", "pharmaceutical-approval", "export-control",
	}
}

// MechanismSlugExamplesString returns comma-separated mechanism examples for prompts.
func MechanismSlugExamplesString() string {
	return strings.Join(MechanismSlugExamples(), ", ")
}

// MarketGeographyAndHierarchyPromptBlock returns shared prompt block for MARKET geography and hierarchy.
func MarketGeographyAndHierarchyPromptBlock() string {
	return `- MARKET GEOGRAPHY INHERITANCE (CRITICAL):
  - When the query mentions a specific country/region AND a market/industry together:
    - The MARKET anchor MUST inherit that country's ISO2 code as geo
    - Do NOT default to "global" when geographic context is present in the query
  - Examples:
    - "Saudi Arabia Event marketing market" → /market/sa/professional-services/event-marketing (NOT /market/global/...)
    - "German automotive industry" → /market/de/industrial/automotive (NOT /market/global/automotive)
    - "UAE banking sector" → /market/ae/finance/banking (NOT /market/global/banking)
    - "Kazakhstan grain exports" → /market/kz/industrial/agriculture (NOT /market/global/agriculture)
    - "Taiwan semiconductor foundries" → /market/tw/technology/semiconductors (NOT /market/global/semiconductors)
  - Use "global" as geo ONLY when NO geographic context is provided in the query
  - You CAN also include a separate GEO_REGION anchor for the country - that is fine and often useful

- MARKET HIERARCHY (best-effort):
  - Markets form hierarchies: broader → narrower (e.g., marketing → digital-marketing → event-marketing)
  - When creating a MARKET anchor, consider if there's an obvious parent market
  - In decompositionHints.subMarkets, list narrower segments that belong to this market
  - Example hierarchy (segment-first): /market/sa/{domain}/marketing → /market/sa/{domain}/digital-marketing → /market/sa/{domain}/event-marketing`
}

// DecompositionHintsPromptBlock returns shared prompt block for decomposition hints.
func DecompositionHintsPromptBlock() string {
	return `DECOMPOSITION HINTS (REQUIRED for MARKET/PROGRAM anchors):
- If any anchor has type MARKET or PROGRAM, you MUST provide decompositionHints for that anchor.
- decompositionHints help downstream research know HOW to explore abstract anchors.
- For MARKET anchors:
  - entityTypes: what entities to discover (typically ["ORGANIZATION", "PRODUCT", "FACILITY"])
  - subMarkets: market segments to explore (e.g., ["upstream", "midstream"] for energy; ["commercial", "retail"] for finance; ["foundry", "design"] for semiconductors)
  - playerCategories: types of players (e.g., ["operators", "traders", "regulators"] for energy; ["local banks", "fintechs"] for finance; ["fabless", "IDMs"] for semiconductors)
  - searchQueries: 3-5 concrete search queries in English for grounded research
    - searchQueries MUST be designed to yield REAL-WORLD entities (company/org/regulator names) that we can anchor as /org/... etc.
    - Avoid "market size" / "market share" queries that only return abstract numbers.
      - If you include a market-share query, phrase it as "top X by market share" so the intent is to extract PLAYERS.
    - Examples (sector-specific):
      - "Kazakhstan grain exports": ["top grain exporters Kazakhstan 2024", "Kazakhstan wheat traders list", "Kazakhstan Ministry of Agriculture grain statistics"]
      - "Saudi B2B SME banking": ["top SME banks in Saudi Arabia 2024", "Saudi Arabia SME banking providers list", "Saudi Arabian Monetary Authority SME banking regulations"]
      - "Taiwan semiconductor foundries": ["largest semiconductor foundries Taiwan 2024", "TSMC competitors list", "Taiwan semiconductor industry associations"]
- For PROGRAM anchors:
  - entityTypes: what entities participate (e.g., ["ORGANIZATION", "FACILITY", "PRODUCT"])
  - playerCategories: types of participants (e.g., ["implementing agencies", "contractors", "beneficiaries"])
  - searchQueries: 3-5 concrete search queries about the program
- searchQueries MUST be in English and actionable for web search.`
}

// MarketGeographyAndDecompositionPromptBlock returns both blocks concatenated (for anchor_resolver).
func MarketGeographyAndDecompositionPromptBlock() string {
	return MarketGeographyAndHierarchyPromptBlock() + "\n\n" + DecompositionHintsPromptBlock()
}

// DefaultCanonicalRules returns the default, shared canonical rules.
func DefaultCanonicalRules() CanonicalRules {
	return CanonicalRules{
		Org: OrgCanonicalRules{
			Format: "/org/{iso2|na|int|regional}/{slug}-{legal_form}",
			// NOTE: legal_form tokens are a compact allowlist used by:
			// - LLM prompts (canonicalization + enforcement)
			// - deterministic suffix parsing in canonical_parts.go
			//
			// This is not meant to be exhaustive, but it must cover common national corporate forms.
			LegalForms: []string{
				// US/UK & generic
				"inc", "llc", "corp", "ltd", "plc", "llp", "lp",
				// Civil law / EU common
				"gmbh", "ag", "sarl", "srl", "sa", "sas", "sasu", "spa", "bv", "nv",
				// Luxembourg / partnerships / funds (common in holding structures)
				"scs", "sca", "snc", "senc",
				// Nordics
				"as", "asa", "ab", // NO/SE/DK common
				// Norway cooperative / association forms are covered by "sa" above (often written "... SA")
				// CEE/EEU / post-soviet
				"jsc", "pjsc", "ao", "oao", "zao", "ooo",
				// APAC
				"kk", "oy", "oyj", "pteltd",
				// LATAM / other
				"ltda", "sdnbhd", "ptyltd", "spzoo",
				// Ireland / common offshore-ish corporate suffixes
				"dac", "teo", "ulc",
			},
			Example: "/org/us/tesla-inc",
		},
		Gov: GovCanonicalRules{
			Format:  "/gov/{iso2|na|regional}/{slug}",
			Example: "/gov/us/department-of-state",
		},
		Product: ProductCanonicalRules{
			Format:     "/product/{category}/{type}/{stage}/{slug}",
			Categories: ProductContractCategories(),
			Types:      ProductContractTypes(),
			Stages:     ProductContractStages(),
			Example:    "/product/raw/mineral/refining/lithium-carbonate",
		},
		Geo: GeoCanonicalRules{
			Format:  "/loc/{cc}/{admin1}/{city}",
			Example: "/loc/us/ca/san-francisco",
			Notes: []string{
				"cc: ISO3166-1 alpha2 (2 chars), composite (e.g. tr-sy, ae-sa-om), or regional token",
				"regional tokens: " + strings.Join(RegionalGeoTokenList(), ", "),
				"extend via HORIZON_REGIONAL_GEO_TOKENS env",
				"admin1: GeoNames admin code or \"na\"",
				"city: lowercase slug or \"na\"",
				"country-only: /loc/{cc}/na/na",
			},
		},
	}
}

// CanonicalPathRulesPrompt returns a shared prompt block for canonical path rules.
func CanonicalPathRulesPrompt() string {
	rules := DefaultCanonicalRules()
	var sb strings.Builder
	sb.WriteString("CANONICAL PATH RULES (REQUIRED):\n")
	sb.WriteString(fmt.Sprintf("- ORGANIZATION: %s (example: %s)\n", rules.Org.Format, rules.Org.Example))
	sb.WriteString(fmt.Sprintf("  - geo: ISO2 (us, sa), composite (tr-sy, ae-sa-om), na, int, or regional (%s). EU/ASEAN/BRICS-level orgs use eu/asean/brics\n", strings.Join(RegionalGeoTokenList(), ", ")))
	sb.WriteString(fmt.Sprintf("  - legal_form in {%s}\n", strings.Join(rules.Org.LegalForms, ", ")))
	sb.WriteString(fmt.Sprintf("- GOVERNMENT: %s (example: %s)\n", rules.Gov.Format, rules.Gov.Example))
	sb.WriteString(fmt.Sprintf("  - geo: ISO2, composite, na, or regional (%s). EU/ASEAN-level bodies use eu/asean\n", strings.Join(RegionalGeoTokenList(), ", ")))
	sb.WriteString(fmt.Sprintf("- PRODUCT: %s\n", rules.Product.Format))
	sb.WriteString(fmt.Sprintf("  - category: %s\n", strings.Join(rules.Product.Categories, " | ")))
	sb.WriteString(fmt.Sprintf("  - type: %s\n", strings.Join(rules.Product.Types, " | ")))
	sb.WriteString(fmt.Sprintf("  - stage: %s\n", strings.Join(rules.Product.Stages, " | ")))
	sb.WriteString("  - for services: use category=service; map service domain/subdomain into {type}/{stage}\n")
	sb.WriteString(fmt.Sprintf("  - example: %s\n", rules.Product.Example))
	sb.WriteString(fmt.Sprintf("- GEO_REGION: %s (example: %s)\n", rules.Geo.Format, rules.Geo.Example))
	for _, note := range rules.Geo.Notes {
		sb.WriteString(fmt.Sprintf("  - %s\n", note))
	}
	return sb.String()
}

// ExtendedCanonicalPathRulesPrompt returns canonical path rules for all entity types used in Research/Critic.
// Includes FACILITY, PERSON, MARKET, COMMUNITY, DEPOSIT, EVENT, PROGRAM, DECISION_POINT, MECHANISM.
func ExtendedCanonicalPathRulesPrompt() string {
	regionalList := strings.Join(RegionalGeoTokenList(), ", ")
	var sb strings.Builder
	sb.WriteString(CanonicalPathRulesPrompt())
	sb.WriteString("- FACILITY: /fac/{orgSlug}/{facilitySlug}. If org is unknown, use orgSlug=na. Example: /fac/tsmc/fab-21, /fac/na/fab-21\n")
	sb.WriteString("- PERSON: /person/{slug}. Example: /person/elon-musk\n")
	sb.WriteString(fmt.Sprintf("- MARKET: L1 /market/{geo}/{domain}, L2 /market/{geo}/{domain}/{vertical}, L3+ parent+one segment. NEVER repeat a segment. geo: ISO2, composite (tr-sy), int, or regional (%s). Examples: %s. HAS_MARKET_SEGMENT: child = parent + one segment.\n", regionalList, MarketExamplePathsString()))
	sb.WriteString(fmt.Sprintf("- COMMUNITY: /community/{geo}/{type}/{slug}. geo: ISO2, composite, or regional (%s). type: trade|activist|professional|criminal|religious|ethnic|interest|dynasty|tribe|representatives|industry-association|key-community. Example: /community/ae/trade/hawala-dubai-network, /community/sa/dynasty/house-of-saud, /community/af/tribe/pashtun, /community/eu/industry-association/steel-producers\n", regionalList))
	sb.WriteString("- DEPOSIT: /deposit/{resource}/{slug}. resource: oil, natural-gas, iron-ore, lithium-ore, etc. Example: /deposit/oil/permian-basin. Fallback: /deposit/other/{slug}\n")
	sb.WriteString("- EVENT: /event/{slug}. Example: /event/funding-round-series-a\n")
	sb.WriteString(fmt.Sprintf("- PROGRAM: /program/{jurisdiction}/{slug}. If jurisdiction is unknown, use global. jurisdiction: ISO2, int, global, or regional (%s). Example: /program/kazakhstan/alem-ai, /program/global/alem-ai\n", regionalList))
	sb.WriteString("- DECISION_POINT: /decision/{domain}/{slug}. Example: /decision/export/grant-license\n")
	sb.WriteString(fmt.Sprintf("- MECHANISM: /mechanism/{category}/{slug}. category: regulatory, economic, technological, contractual, political. Examples: %s.\n", MechanismSlugExamplesString()))
	sb.WriteString(fmt.Sprintf("- AFFECTED_ZONE: /zone/{geo}/{condition_type}/{slug}. geo: ISO2, composite (tr-sy), or regional (%s). condition_type: military_conflict, natural_disaster, political_tension, humanitarian_crisis, earthquake_affected, flood_zone. slug: descriptive place/region name (northern-syria, donbas, gaza-strip, northern-rakhine). NEVER use \"na\" as slug — use region name or condition-specific name (e.g. myanmar-crisis-area). Example: /zone/tr-sy/military_conflict/northern-syria\n", regionalList))
	return sb.String()
}

// ZoneVsEventDistinctionPrompt returns explicit rules to distinguish AFFECTED_ZONE from EVENT.
// Use in research prompts when both zone and event entities may be discovered.
func ZoneVsEventDistinctionPrompt() string {
	return `ZONE vs EVENT (CRITICAL — do not confuse):
- AFFECTED_ZONE = geographic AREA/region affected by a condition (conflict zone, disaster zone, earthquake-affected area). Has a LOCATION. Path: /zone/{geo}/{condition_type}/{slug}. slug = place/region name (northern-rakhine, gaza-strip, donbas). NEVER use "na" as zone slug.
- EVENT = discrete OCCURRENCE (incident) — something that HAPPENED at a point in time. Path: /event/{slug}. Examples: /event/2023-turkey-syria-earthquake, /event/2024-flood-xyz, /event/conflict-outbreak-2022.
- TRIGGERED_BY: zone → event (the zone was CAUSED BY the event). Zone = affected area; Event = the incident that caused it.
- When in doubt: if it has a geographic extent and persists as an area → ZONE. If it is a one-time incident → EVENT.`
}

// GraphIntentCanonicalPathRulesPrompt returns canonical path rules for Graph Intent planner.
// Covers all allowed node types (GOVERNMENT_AGENCY, GEO_REGION, DOMAIN, ORGANIZATION, etc.)
// and includes DOMAIN path format.
func GraphIntentCanonicalPathRulesPrompt() string {
	regionalList := strings.Join(RegionalGeoTokenList(), ", ")
	domainTypes := strings.Join(DomainContractTypes(), ", ")
	var sb strings.Builder
	sb.WriteString("CANONICAL PATH RULES (REQUIRED for every node):\n")
	sb.WriteString("Every node MUST have canonicalPathHint in the exact format below. No other formats allowed.\n\n")
	sb.WriteString("Forbidden namespaces/forms: /policy/*, /geo/*, /economy/*, /society/*, /org/gov/*.\n\n")
	sb.WriteString("- GOVERNMENT_AGENCY: /gov/{iso2}/{slug}. Example: /gov/ro/ministry-of-finance, /gov/us/department-of-state\n")
	sb.WriteString(fmt.Sprintf("  geo: ISO2 (ro, us, sa), composite (tr-sy), or regional (%s). EU/ASEAN bodies use eu/asean\n\n", regionalList))
	sb.WriteString("- GEO_REGION: /loc/{cc}/{admin1}/{city}. Country-only: /loc/{cc}/na/na. Example: /loc/ro/na/na (Romania), /loc/us/ca/san-francisco\n")
	sb.WriteString(fmt.Sprintf("  cc: ISO2, composite (tr-sy), or regional (%s). admin1/city: GeoNames code or \"na\"\n\n", regionalList))
	sb.WriteString("- DOMAIN: /domain/v1/{contractType}/{facetKey}/{facetValue}... or /domain/v1/other/{custom-slug}/geo/{cc}\n")
	sb.WriteString(fmt.Sprintf("  contractType (allowlist): %s\n", domainTypes))
	sb.WriteString("  Examples: /domain/v1/regulatory-impact/geo/ro/framework/investment, /domain/v1/policy-and-regulation/geo/ro, /domain/v1/other/foreign-investment-framework/geo/ro\n\n")
	sb.WriteString("- ORGANIZATION: /org/{geo}/{slug}-{legal_form}. Example: /org/us/tesla-inc, /org/ro/invest-romania-na\n")
	sb.WriteString(fmt.Sprintf("  geo: ISO2, composite, na, int, or regional (%s). legal_form: inc, ltd, gmbh, srl, etc.\n\n", regionalList))
	sb.WriteString("- PRODUCT: /product/{category}/{type}/{stage}/{slug}. Example: /product/raw/mineral/refining/lithium-carbonate\n")
	sb.WriteString("  For services, use category=service and map service domain/subdomain into {type}/{stage}.\n\n")
	sb.WriteString("- FACILITY: /fac/{orgSlug}/{facilitySlug}. If org is unknown, use orgSlug=na. Example: /fac/tsmc/fab-21, /fac/na/fab-21\n\n")
	sb.WriteString("- PERSON: /person/{slug}. Example: /person/elon-musk\n\n")
	sb.WriteString(fmt.Sprintf("- MARKET: L1 /market/{geo}/{domain}, L2 /market/{geo}/{domain}/{vertical}, L3+ parent+one segment. NEVER repeat a segment. geo: ISO2, composite, or regional (%s). Examples: %s. HAS_MARKET_SEGMENT: child = parent + one segment.\n\n", regionalList, MarketExamplePathsString()))
	sb.WriteString("- COMMUNITY: /community/{geo}/{type}/{slug}. type: trade|activist|professional|dynasty|tribe|interest|representatives|industry-association|key-community. Example: /community/ro/interest/investors-association, /community/eu/industry-association/steel-producers\n\n")
	sb.WriteString("- DEPOSIT: /deposit/{resource}/{slug}. Example: /deposit/oil/permian-basin\n\n")
	sb.WriteString("- EVENT: /event/{slug}. Example: /event/funding-round-series-a\n\n")
	sb.WriteString("- PROGRAM: /program/{jurisdiction}/{slug}. Example: /program/ro/invest-romania-program\n\n")
	sb.WriteString("- DECISION_POINT: /decision/{domain}/{slug}. Example: /decision/export/grant-license\n")
	sb.WriteString(fmt.Sprintf("- MECHANISM: /mechanism/{category}/{slug}. category: regulatory, economic, technological, contractual, political. Examples: %s.\n", MechanismSlugExamplesString()))
	sb.WriteString(fmt.Sprintf("- AFFECTED_ZONE: /zone/{geo}/{condition_type}/{slug}. geo: ISO2, composite, or regional (%s). condition_type: military_conflict, natural_disaster, political_tension, humanitarian_crisis, earthquake_affected, flood_zone. slug: descriptive place/region name. NEVER \"na\". Example: /zone/tr-sy/military_conflict/northern-syria\n", regionalList))
	return sb.String()
}
