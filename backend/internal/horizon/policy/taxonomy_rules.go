package policy

import (
	"fmt"
	"sort"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/canonical"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/edgesemantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// =============================================================================
// TAXONOMY RULES
// =============================================================================
// These rules are used by the Policy Checker (both LLM and deterministic)
// to validate canonical paths, edge directions, and entity structures.

// TaxonomyRules contains all taxonomy rules for policy enforcement.
type TaxonomyRules struct {
	Geo            GeoTaxonomyRules      `json:"geo" yaml:"geo"`
	Org            OrgTaxonomyRules      `json:"org" yaml:"org"`
	Gov            GovTaxonomyRules      `json:"gov" yaml:"gov"`
	Person         PersonTaxonomyRules   `json:"person" yaml:"person"`
	Product        ProductTaxonomyRules  `json:"product" yaml:"product"`
	Facility       FacilityTaxonomyRules `json:"facility" yaml:"facility"`
	Market         MarketTaxonomyRules   `json:"market" yaml:"market"`
	EdgeDirection  EdgeDirectionRulesLLM `json:"edgeDirection" yaml:"edgeDirection"`
	SymmetricEdges []string              `json:"symmetricEdges" yaml:"symmetricEdges"`
}

// GeoTaxonomyRules defines rules for GEO_REGION canonical paths.
type GeoTaxonomyRules struct {
	Format       string   `json:"format" yaml:"format"`
	Admin1Source string   `json:"admin1Source" yaml:"admin1Source"`
	Examples     Examples `json:"examples" yaml:"examples"`
	Resolution   string   `json:"resolution" yaml:"resolution"`
}

// OrgTaxonomyRules defines rules for ORGANIZATION canonical paths.
type OrgTaxonomyRules struct {
	Format   string   `json:"format" yaml:"format"`
	Examples Examples `json:"examples" yaml:"examples"`
}

// GovTaxonomyRules defines rules for government body canonical paths.
type GovTaxonomyRules struct {
	Format   string   `json:"format" yaml:"format"`
	Examples Examples `json:"examples" yaml:"examples"`
}

// PersonTaxonomyRules defines rules for PERSON canonical paths.
type PersonTaxonomyRules struct {
	Format     string   `json:"format" yaml:"format"`
	Honorifics []string `json:"honorifics" yaml:"honorifics"`
	NoGeoToken bool     `json:"noGeoToken" yaml:"noGeoToken"`
	Reason     string   `json:"reason" yaml:"reason"`
	Examples   Examples `json:"examples" yaml:"examples"`
}

// ProductTaxonomyRules defines rules for PRODUCT canonical paths.
type ProductTaxonomyRules struct {
	Format     string   `json:"format" yaml:"format"`
	Categories []string `json:"categories" yaml:"categories"`
	Types      []string `json:"types" yaml:"types"`
	Stages     []string `json:"stages" yaml:"stages"`
	Examples   Examples `json:"examples" yaml:"examples"`
}

// FacilityTaxonomyRules defines rules for FACILITY canonical paths.
type FacilityTaxonomyRules struct {
	Format   string   `json:"format" yaml:"format"`
	Examples Examples `json:"examples" yaml:"examples"`
}

// MarketTaxonomyRules defines rules for MARKET canonical paths.
type MarketTaxonomyRules struct {
	Format   string   `json:"format" yaml:"format"`
	Domains  []string `json:"domains" yaml:"domains"`
	Examples Examples `json:"examples" yaml:"examples"`
}

// EdgeDirectionRulesLLM defines edge direction rules for LLM context.
type EdgeDirectionRulesLLM struct {
	Rules []EdgeDirectionRuleLLM `json:"rules" yaml:"rules"`
}

// EdgeDirectionRuleLLM defines a single edge direction rule for LLM context (string-based).
// Note: This is different from EdgeDirectionRule in validation_policy.go which uses FragmentType.
type EdgeDirectionRuleLLM struct {
	EdgeType    string   `json:"edgeType" yaml:"edgeType"`
	Direction   string   `json:"direction" yaml:"direction"` // e.g., "PERSON → ORGANIZATION"
	Description string   `json:"description" yaml:"description"`
	ValidSource []string `json:"validSource" yaml:"validSource"`
	ValidTarget []string `json:"validTarget" yaml:"validTarget"`
}

// Examples contains correct and incorrect examples for a rule.
type Examples struct {
	Correct   []string `json:"correct" yaml:"correct"`
	Incorrect []string `json:"incorrect" yaml:"incorrect"`
}

// DefaultTaxonomyRules returns the default taxonomy rules for policy enforcement.
func DefaultTaxonomyRules() *TaxonomyRules {
	rules := taxonomy.DefaultCanonicalRules()
	tr := &TaxonomyRules{
		Geo: GeoTaxonomyRules{
			Format:       rules.Geo.Format,
			Admin1Source: "geonames",
			Examples: Examples{
				Correct: []string{
					"/loc/ae/01/dubai",   // Dubai emirate = admin1 code "01"
					"/loc/sa/01/riyadh",  // Riyadh province = "01"
					"/loc/ae",            // Country-level UAE
					"/loc/us/tx/austin",  // Texas, Austin
					"/loc/ru/mow/moscow", // Moscow city
				},
				Incorrect: []string{
					"/loc/ae/du/dubai",    // "du" is not GeoNames admin code
					"/loc/ae/dubai/dubai", // city name in admin1 slot
					"/loc/uae",            // "uae" is not valid ISO2
					"/loc/saudi-arabia",   // country name instead of ISO2
				},
			},
			Resolution: "Use GeoNames API to resolve deeper geo levels when available. Country-level locations use /loc/{cc}.",
		},
		Org: OrgTaxonomyRules{
			Format: rules.Org.Format,
			Examples: Examples{
				Correct: []string{
					"/org/sa/saudi-aramco",
					"/org/ae/emirates",
					"/org/us/tesla",
					"/org/de/siemens",
					"/org/int/united-nations",
					"/org/int/unesco",
					"/org/eu/european-commission", // regional token for EU-level orgs
				},
				Incorrect: []string{
					"/org/saudi-aramco",       // missing iso2
					"/org/aramco",             // missing iso2
					"/org/na/unknown-company", // "na" only when jurisdiction unknown
				},
			},
		},
		Gov: GovTaxonomyRules{
			Format: rules.Gov.Format,
			Examples: Examples{
				Correct: []string{
					"/gov/sa/sfda",
					"/gov/ae/adnoc",
					"/gov/us/sec",
					"/gov/ae-sa-om/gcc-council",   // composite geo for intergovernmental
					"/gov/eu/european-commission", // regional token for EU-level bodies
					"/gov/asean/asean-secretariat",
				},
				Incorrect: []string{
					"/gov/ministry-of-finance", // missing iso2
					"/org/sa/sfda",             // gov body in /org/ namespace
				},
			},
		},
		Person: PersonTaxonomyRules{
			Format:     "/person/{honorific?}-{name-slug}",
			Honorifics: []string{"sheikh", "hrh", "he", "dr", "gen", "col", "prof", "sir", "dame", "lord", "lady"},
			NoGeoToken: true,
			Reason:     "GDPR compliance and privacy - people move, have multiple citizenships",
			Examples: Examples{
				Correct: []string{
					"/person/elon-musk",
					"/person/sheikh-tahnoon-bin-zayed",
					"/person/hrh-mohammed-bin-salman",
					"/person/dr-tedros-adhanom",
				},
				Incorrect: []string{
					"/person/us/elon-musk", // geo token not allowed
					"/person/ae/tahnoon",   // geo token not allowed
				},
			},
		},
		Product: ProductTaxonomyRules{
			Format:     rules.Product.Format,
			Categories: rules.Product.Categories,
			Types:      rules.Product.Types,
			Stages:     rules.Product.Stages,
			Examples: Examples{
				Correct: []string{
					"/product/raw/mineral/extraction/lithium-ore",
					"/product/finished/mechanical/assembly/electric-vehicle",
					"/product/component/electronic/manufacturing/battery-cells",
				},
				Incorrect: []string{
					"/product/lithium",           // missing taxonomy segments
					"/product/org/tesla/model-s", // org prefix not allowed
				},
			},
		},
		Facility: FacilityTaxonomyRules{
			Format: "/fac/{geo}/{facility-slug}",
			Examples: Examples{
				Correct: []string{
					"/fac/sa/abqaiq-refinery",
					"/fac/us/gigafactory-texas",
					"/fac/ae/ruwais-refinery",
				},
				Incorrect: []string{
					"/fac/abqaiq",             // missing geo segment
					"/facility/aramco/abqaiq", // wrong namespace
				},
			},
		},
		Market: MarketTaxonomyRules{
			Format:  "L1 /market/{geo}/{domain}, L2 /market/{geo}/{domain}/{vertical}, L3+ parent path + one segment",
			Domains: []string{"finance", "technology", "industrial", "consumer", "healthcare", "energy", "materials", "telecom", "real-estate", "public-sector"},
			Examples: Examples{
				Correct: taxonomy.MarketExamplePathsForPrompt(),
				Incorrect: []string{
					"/market/banking",                    // missing geo and domain
					"/market/sa/banking",                 // missing domain segment
					"/market/iq/agriculture/agriculture", // repeated segment INVALID
				},
			},
		},
		EdgeDirection: EdgeDirectionRulesLLM{},
		SymmetricEdges: nil,
	}
	// Keep prompt contract aligned with runtime edge semantics source-of-truth.
	tr.EdgeDirection.Rules = buildEdgeDirectionRulesFromSemantics()
	tr.SymmetricEdges = buildSymmetricEdgesFromSemantics()
	return tr
}

func buildEdgeDirectionRulesFromSemantics() []EdgeDirectionRuleLLM {
	types := make([]string, 0, len(edgesemantics.EdgeDirectionPolicies))
	for t := range edgesemantics.EdgeDirectionPolicies {
		types = append(types, string(t))
	}
	sort.Strings(types)

	out := make([]EdgeDirectionRuleLLM, 0, len(types))
	for _, t := range types {
		rule := edgesemantics.EdgeDirectionPolicies[models.DependencyType(t)]
		out = append(out, EdgeDirectionRuleLLM{
			EdgeType:    t,
			Direction:   fmt.Sprintf("%s → %s", joinFragmentTypes(rule.ValidSourceTypes), joinFragmentTypes(rule.ValidTargetTypes)),
			Description: rule.Description,
			ValidSource: fragmentTypesToStrings(rule.ValidSourceTypes),
			ValidTarget: fragmentTypesToStrings(rule.ValidTargetTypes),
		})
	}
	return out
}

func buildSymmetricEdgesFromSemantics() []string {
	out := make([]string, 0, len(models.SymmetricEdgeTypes))
	for edgeType := range models.SymmetricEdgeTypes {
		out = append(out, string(edgeType))
	}
	sort.Strings(out)
	return out
}

func fragmentTypesToStrings(types []models.FragmentType) []string {
	if len(types) == 0 {
		return nil
	}
	out := make([]string, 0, len(types))
	for _, t := range types {
		out = append(out, string(t))
	}
	return out
}

func joinFragmentTypes(types []models.FragmentType) string {
	if len(types) == 0 {
		return "*"
	}
	parts := fragmentTypesToStrings(types)
	return strings.Join(parts, "|")
}

// ToLLMContext converts taxonomy rules to a string suitable for LLM prompts.
func (r *TaxonomyRules) ToLLMContext() string {
	var sb strings.Builder

	sb.WriteString("## Taxonomy Rules for Policy Enforcement\n\n")

	// Geo rules
	sb.WriteString("### GEO_REGION Paths\n")
	sb.WriteString(fmt.Sprintf("Format: `%s`\n", r.Geo.Format))
	sb.WriteString(fmt.Sprintf("Admin1 source: %s\n", r.Geo.Admin1Source))
	sb.WriteString("Correct examples:\n")
	for _, ex := range r.Geo.Examples.Correct {
		sb.WriteString(fmt.Sprintf("  - `%s`\n", ex))
	}
	sb.WriteString("Incorrect examples:\n")
	for _, ex := range r.Geo.Examples.Incorrect {
		sb.WriteString(fmt.Sprintf("  - `%s` (WRONG)\n", ex))
	}
	sb.WriteString(fmt.Sprintf("Resolution: %s\n\n", r.Geo.Resolution))

	// Org rules
	sb.WriteString("### ORGANIZATION Paths\n")
	sb.WriteString(fmt.Sprintf("Format: `%s`\n", r.Org.Format))
	sb.WriteString("Do NOT include legal form (Inc, Ltd, LLC, etc.) in slug.\n")
	sb.WriteString("Correct examples:\n")
	for _, ex := range r.Org.Examples.Correct {
		sb.WriteString(fmt.Sprintf("  - `%s`\n", ex))
	}
	sb.WriteString("Incorrect examples:\n")
	for _, ex := range r.Org.Examples.Incorrect {
		sb.WriteString(fmt.Sprintf("  - `%s` (WRONG)\n", ex))
	}
	sb.WriteString("\n")

	// Gov rules
	sb.WriteString("### Government Body Paths\n")
	sb.WriteString(fmt.Sprintf("Format: `%s`\n", r.Gov.Format))
	sb.WriteString("Correct examples:\n")
	for _, ex := range r.Gov.Examples.Correct {
		sb.WriteString(fmt.Sprintf("  - `%s`\n", ex))
	}
	sb.WriteString("\n")

	// Person rules
	sb.WriteString("### PERSON Paths\n")
	sb.WriteString(fmt.Sprintf("Format: `%s`\n", r.Person.Format))
	sb.WriteString(fmt.Sprintf("NO geographic token allowed (reason: %s)\n", r.Person.Reason))
	sb.WriteString(fmt.Sprintf("Honorifics: %s\n", strings.Join(r.Person.Honorifics, ", ")))
	sb.WriteString("Correct examples:\n")
	for _, ex := range r.Person.Examples.Correct {
		sb.WriteString(fmt.Sprintf("  - `%s`\n", ex))
	}
	sb.WriteString("\n")

	// Edge direction rules
	sb.WriteString("### Edge Direction Rules\n")
	sb.WriteString("These edges MUST follow the specified direction:\n")
	for _, rule := range r.EdgeDirection.Rules {
		sb.WriteString(fmt.Sprintf("  - `%s`: %s\n", rule.EdgeType, rule.Direction))
	}
	sb.WriteString("\n")

	// Symmetric edges
	sb.WriteString("### Symmetric Edges\n")
	sb.WriteString("These edges are bidirectional (order doesn't matter, but normalized to lexicographic order):\n")
	for _, edge := range r.SymmetricEdges {
		sb.WriteString(fmt.Sprintf("  - `%s`\n", edge))
	}

	return sb.String()
}

// ValidateGeoPath validates a GEO_REGION canonical path format.
func (r *TaxonomyRules) ValidateGeoPath(path string) (bool, string) {
	if !strings.HasPrefix(path, "/loc/") {
		return false, "GEO_REGION path must start with /loc/"
	}

	cc, level1, _, ok := taxonomy.ParseLocCanonicalPath(path)
	if !ok {
		return false, "GEO_REGION path must match /loc/{country}[/{level1}[/{level2}]]"
	}
	if !taxonomy.IsValidLocCountryToken(cc) {
		return false, fmt.Sprintf("invalid geo code: %s", cc)
	}
	// Keep a soft sanity check for clearly overlong first subdivision tokens.
	if level1 != "" && len(level1) > 48 {
		return false, fmt.Sprintf("first geo subdivision token is too long: %s", level1)
	}

	return true, ""
}

// ValidateOrgPath validates an ORGANIZATION canonical path format.
func (r *TaxonomyRules) ValidateOrgPath(path string) (bool, string) {
	if !strings.HasPrefix(path, "/org/") {
		return false, "ORGANIZATION path must start with /org/"
	}

	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 3 {
		return false, "ORGANIZATION path must have at least 3 segments (/org/{iso2}/{slug})"
	}
	if canonical.OrgSlugForbiddenPlaceholder(strings.ToLower(strings.TrimSpace(parts[len(parts)-1]))) {
		return false, "ORGANIZATION path slug must not be a synthetic placeholder like 'na'"
	}

	return true, ""
}

// ValidateGovPath validates a GOVERNMENT canonical path format.
func (r *TaxonomyRules) ValidateGovPath(path string) (bool, string) {
	if !strings.HasPrefix(path, "/gov/") {
		return false, "GOVERNMENT path must start with /gov/"
	}

	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 3 {
		return false, "GOVERNMENT path must have exactly 3 segments (/gov/{iso2|na}/{slug})"
	}

	geo := strings.ToLower(strings.TrimSpace(parts[1]))
	if geo == "" {
		return false, "GOVERNMENT path missing jurisdiction segment"
	}
	// Allow: na, ISO2 (2 chars), composite (tr-sy, ae-sa-om), or regional token (eu, asean, brics, gcc, etc.)
	if !taxonomy.IsValidGeoForOrgGovFac(geo) {
		return false, fmt.Sprintf("Invalid jurisdiction (expected %s): %s", taxonomy.ISO2GeoDescriptionForPrompts(), parts[1])
	}
	if canonical.GovSlugForbiddenPlaceholder(strings.ToLower(strings.TrimSpace(parts[2]))) {
		return false, "GOVERNMENT path slug must not be a synthetic placeholder like 'na'"
	}

	return true, ""
}

// ValidateProductPath validates a PRODUCT canonical path format.
func (r *TaxonomyRules) ValidateProductPath(path string) (bool, string) {
	if !strings.HasPrefix(path, "/product/") {
		return false, "PRODUCT path must start with /product/"
	}

	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 5 {
		return false, fmt.Sprintf("PRODUCT path must be /product/{category}/{type}/{stage}/{slug}, got %d segments", len(parts))
	}

	if !containsString(r.Product.Categories, parts[1]) {
		return false, fmt.Sprintf("Invalid product category: %s", parts[1])
	}
	// Service products use the same 5-part contract; type/stage are service domain/subdomain.
	if parts[1] == "service" {
		return true, ""
	}
	if !containsString(r.Product.Types, parts[2]) {
		return false, fmt.Sprintf("Invalid product type: %s", parts[2])
	}
	if !containsString(r.Product.Stages, parts[3]) {
		return false, fmt.Sprintf("Invalid product stage: %s", parts[3])
	}

	return true, ""
}

// ValidatePersonPath validates a PERSON canonical path format.
func (r *TaxonomyRules) ValidatePersonPath(path string) (bool, string) {
	if !strings.HasPrefix(path, "/person/") {
		return false, "PERSON path must start with /person/"
	}

	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 {
		return false, fmt.Sprintf("PERSON path must have exactly 2 segments (/person/{slug}), got %d", len(parts))
	}

	return true, ""
}

// ValidateEdgeDirection checks if an edge has correct direction.
func (r *TaxonomyRules) ValidateEdgeDirection(edgeType models.DependencyType, sourceType, targetType models.FragmentType) (bool, bool, string) {
	valid, shouldReverse, rule := edgesemantics.ValidateEdgeDirection(edgeType, sourceType, targetType)
	if valid {
		return true, false, ""
	}

	description := ""
	if rule != nil {
		description = strings.TrimSpace(rule.Description)
	}
	if shouldReverse {
		if description == "" {
			description = fmt.Sprintf("edge %s should be reversed: %s → %s, not %s → %s", edgeType, targetType, sourceType, sourceType, targetType)
		}
		return false, true, description
	}

	if description == "" {
		description = fmt.Sprintf("edge %s has invalid endpoint types: source=%s, target=%s", edgeType, sourceType, targetType)
	}
	return false, false, description
}

// IsSymmetricEdge checks if an edge type is symmetric.
func (r *TaxonomyRules) IsSymmetricEdge(edgeType models.DependencyType) bool {
	for _, sym := range r.SymmetricEdges {
		if sym == string(edgeType) {
			return true
		}
	}
	return false
}

func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}
