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
			Format: "/org/{iso2|na}/{slug}-{legal_form}",
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
			Format:  "/gov/{iso2|na}/{slug}",
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
				"cc must be ISO3166-1 alpha2 lowercase (exactly 2 chars)",
				"regional geo tokens are allowed when ISO2 is insufficient (e.g., asean, brics, gcc, eu, eaeu, opec, oecd, g7, g20)",
				"regional geo tokens can be extended via HORIZON_REGIONAL_GEO_TOKENS",
				"admin1 must be a GeoNames admin code or \"na\"",
				"city must be a lowercase slug or \"na\"",
				"country-only locations use /loc/{cc}/na/na",
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
	sb.WriteString(fmt.Sprintf("  - legal_form in {%s}\n", strings.Join(rules.Org.LegalForms, ", ")))
	sb.WriteString(fmt.Sprintf("- GOVERNMENT: %s (example: %s)\n", rules.Gov.Format, rules.Gov.Example))
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
