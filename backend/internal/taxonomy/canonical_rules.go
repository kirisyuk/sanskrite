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
	Fallback   string
}

type GeoCanonicalRules struct {
	Format  string
	Example string
	Notes   []string
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
			Categories: []string{"raw", "component", "finished", "service"},
			// NOTE: Keep in sync with Firestore seed taxonomy (cmd/tools/horizon_seed_taxonomy).
			// This list must be operationally sufficient; it is not intended to be an exhaustive ontology of matter.
			Types: []string{
				"mineral",
				"agricultural",
				"chemical",
				"electronic",
				"mechanical",
				"energy",
				"hydrocarbon",
				"fuel",
				"petrochemical",
				"healthcare",
				"consumer",
				// Common supply-chain product types that appear in extraction logs.
				"metal",
				"magnetic",
			},
			Stages: []string{
				"extraction",
				"refining",
				// Common industrial stages that appear in supply-chain graphs.
				"smelting",
				"liquefaction",
				"cracking",
				"processing",
				"manufacturing",
				"assembly",
			},
			Example:  "/product/raw/mineral/refining/lithium-carbonate",
			Fallback: "/product/{slug}",
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
	sb.WriteString(fmt.Sprintf("- PRODUCT: %s (fallback: %s)\n", rules.Product.Format, rules.Product.Fallback))
	sb.WriteString(fmt.Sprintf("  - category: %s\n", strings.Join(rules.Product.Categories, " | ")))
	sb.WriteString(fmt.Sprintf("  - type: %s\n", strings.Join(rules.Product.Types, " | ")))
	sb.WriteString(fmt.Sprintf("  - stage: %s\n", strings.Join(rules.Product.Stages, " | ")))
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
	sb.WriteString("- FACILITY: /fac/{orgSlug}/{facilitySlug} (preferred) or /fac/{facilitySlug}. Example: /fac/tsmc/fab-21\n")
	sb.WriteString("- PERSON: /person/{slug}. Example: /person/elon-musk\n")
	sb.WriteString(fmt.Sprintf("- MARKET: /market/{geo}/{domain}/{vertical} (preferred) or /market/{geo}/{slug}. geo: ISO2, composite (tr-sy), int, or regional (%s). Example: /market/sa/finance/banking\n", regionalList))
	sb.WriteString(fmt.Sprintf("- COMMUNITY: /community/{geo}/{type}/{slug}. geo: ISO2, composite, or regional (%s). type: trade|activist|professional|criminal|religious|ethnic|interest|dynasty|tribe. Example: /community/ae/trade/hawala-dubai-network, /community/sa/dynasty/house-of-saud, /community/af/tribe/pashtun\n", regionalList))
	sb.WriteString("- DEPOSIT: /deposit/{resource}/{slug}. resource: oil, natural-gas, iron-ore, lithium-ore, etc. Example: /deposit/oil/permian-basin. Fallback: /deposit/other/{slug}\n")
	sb.WriteString("- EVENT: /event/{slug}. Example: /event/funding-round-series-a\n")
	sb.WriteString(fmt.Sprintf("- PROGRAM: /program/{jurisdiction}/{slug} (preferred) or /program/{slug}. jurisdiction: ISO2, int, or regional (%s). Example: /program/kazakhstan/alem-ai\n", regionalList))
	sb.WriteString("- DECISION_POINT: /decision/{domain}/{slug}. Example: /decision/export/grant-license\n")
	sb.WriteString("- MECHANISM: /mechanism/{category}/{slug}. category: regulatory, economic, technological, contractual, political. Example: /mechanism/regulatory/entity-list\n")
	return sb.String()
}
