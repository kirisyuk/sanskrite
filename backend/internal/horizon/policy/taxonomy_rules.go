package policy

import (
	"fmt"
	"strings"

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
	Format     string   `json:"format" yaml:"format"`
	LegalForms []string `json:"legalForms" yaml:"legalForms"`
	Examples   Examples `json:"examples" yaml:"examples"`
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
	return &TaxonomyRules{
		Geo: GeoTaxonomyRules{
			Format:       rules.Geo.Format,
			Admin1Source: "geonames",
			Examples: Examples{
				Correct: []string{
					"/loc/ae/01/dubai",   // Dubai emirate = admin1 code "01"
					"/loc/sa/01/riyadh",  // Riyadh province = "01"
					"/loc/ae/na/na",      // Country-level UAE
					"/loc/us/tx/austin",  // Texas, Austin
					"/loc/ru/mow/moscow", // Moscow city
				},
				Incorrect: []string{
					"/loc/ae/du/dubai",        // "du" is not GeoNames admin code
					"/loc/ae/dubai/dubai",     // city name in admin1 slot
					"/loc/uae/na/na",          // "uae" is not valid ISO2
					"/loc/saudi-arabia/na/na", // country name instead of ISO2
				},
			},
			Resolution: "Use GeoNames API to resolve admin1 code. Country-level locations use /loc/{cc}/na/na.",
		},
		Org: OrgTaxonomyRules{
			Format:     rules.Org.Format,
			LegalForms: rules.Org.LegalForms,
			Examples: Examples{
				Correct: []string{
					"/org/sa/saudi-aramco-jsc",
					"/org/ae/emirates-llc",
					"/org/us/tesla-inc",
					"/org/de/siemens-ag",
					"/org/int/united-nations",
					"/org/int/unesco",
				},
				Incorrect: []string{
					"/org/saudi-aramco",       // missing iso2 and legal form
					"/org/sa/aramco",          // missing legal form
					"/org/aramco-jsc",         // missing iso2
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
					"/gov/ae-sa-om/gcc-council", // composite geo for intergovernmental
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
			Format: "/fac/{org-slug}/{facility-slug}",
			Examples: Examples{
				Correct: []string{
					"/fac/saudi-aramco/abqaiq-refinery",
					"/fac/tesla/gigafactory-texas",
					"/fac/adnoc/ruwais-refinery",
				},
				Incorrect: []string{
					"/fac/abqaiq",             // missing org prefix
					"/facility/aramco/abqaiq", // wrong namespace
				},
			},
		},
		Market: MarketTaxonomyRules{
			Format:  "/market/{geo}/{domain}/{vertical}",
			Domains: []string{"finance", "technology", "industrial", "consumer", "healthcare", "energy", "materials", "telecom", "real-estate", "public-sector"},
			Examples: Examples{
				Correct: []string{
					"/market/sa/finance/banking",
					"/market/ae/energy/oil-gas",
					"/market/global/technology/semiconductors",
				},
				Incorrect: []string{
					"/market/banking",    // missing geo and domain
					"/market/sa/banking", // missing domain segment
				},
			},
		},
		EdgeDirection: EdgeDirectionRulesLLM{
			Rules: []EdgeDirectionRuleLLM{
				{EdgeType: "EMPLOYED_BY", Direction: "PERSON → ORGANIZATION", ValidSource: []string{"PERSON"}, ValidTarget: []string{"ORGANIZATION"}},
				{EdgeType: "BOARD_MEMBER_OF", Direction: "PERSON → ORGANIZATION", ValidSource: []string{"PERSON"}, ValidTarget: []string{"ORGANIZATION"}},
				{EdgeType: "KEY_PERSON", Direction: "PERSON → ORGANIZATION", ValidSource: []string{"PERSON"}, ValidTarget: []string{"ORGANIZATION"}},
				{EdgeType: "FOUNDED", Direction: "PERSON|ORG → ORGANIZATION|PROGRAM", ValidSource: []string{"PERSON", "ORGANIZATION"}, ValidTarget: []string{"ORGANIZATION", "PROGRAM"}},
				{EdgeType: "OWNS", Direction: "ORG|PERSON → ORG|FACILITY|PRODUCT|DEPOSIT", ValidSource: []string{"ORGANIZATION", "PERSON"}, ValidTarget: []string{"ORGANIZATION", "FACILITY", "PRODUCT", "DEPOSIT"}},
				// COMMUNITY can also be an actor (e.g., dynasty) that controls organizations/facilities.
				{EdgeType: "CONTROLS", Direction: "ORG|PERSON|COMMUNITY → ORG|FACILITY", ValidSource: []string{"ORGANIZATION", "PERSON", "COMMUNITY"}, ValidTarget: []string{"ORGANIZATION", "FACILITY"}},
				{EdgeType: "REGULATES", Direction: "ORGANIZATION → *", ValidSource: []string{"ORGANIZATION"}, ValidTarget: []string{"ORGANIZATION", "FACILITY", "PRODUCT", "GEO_REGION", "MARKET"}},
				// PROGRAM can also be located in a GEO region (e.g., incubator/government program operating in a city).
				{EdgeType: "LOCATED_IN", Direction: "* → GEO_REGION", ValidSource: []string{"FACILITY", "ORGANIZATION", "PERSON", "PRODUCT", "PROGRAM", "GEO_REGION", "DEPOSIT", "COMMUNITY"}, ValidTarget: []string{"GEO_REGION"}},
				{EdgeType: "OPERATES_IN", Direction: "ORGANIZATION|PERSON → GEO_REGION|FACILITY|DEPOSIT", ValidSource: []string{"ORGANIZATION", "PERSON"}, ValidTarget: []string{"GEO_REGION", "FACILITY", "DEPOSIT"}},
				{EdgeType: "SUPPLIES_TO", Direction: "ORG|FACILITY → ORG|FACILITY", ValidSource: []string{"ORGANIZATION", "FACILITY"}, ValidTarget: []string{"ORGANIZATION", "FACILITY"}},
				{EdgeType: "PRODUCES", Direction: "ORG|FACILITY|DEPOSIT → PRODUCT", ValidSource: []string{"ORGANIZATION", "FACILITY", "DEPOSIT"}, ValidTarget: []string{"PRODUCT"}},
				{EdgeType: "CONSUMES", Direction: "ORG|FACILITY|PRODUCT → PRODUCT", ValidSource: []string{"ORGANIZATION", "FACILITY", "PRODUCT"}, ValidTarget: []string{"PRODUCT"}},
				{EdgeType: "INVESTS_IN", Direction: "ORG|PERSON → ORG|PROGRAM|PRODUCT", ValidSource: []string{"ORGANIZATION", "PERSON"}, ValidTarget: []string{"ORGANIZATION", "PROGRAM", "PRODUCT"}},
				// PROGRAM can participate in markets (e.g., government programs active in a sector/segment).
				{EdgeType: "PARTICIPATES_IN", Direction: "ORG|FACILITY|COMMUNITY|PRODUCT|PROGRAM → MARKET", ValidSource: []string{"ORGANIZATION", "FACILITY", "COMMUNITY", "PRODUCT", "PROGRAM"}, ValidTarget: []string{"MARKET"}},
				{EdgeType: "DOMINATES", Direction: "ORG|FACILITY|COMMUNITY → MARKET", ValidSource: []string{"ORGANIZATION", "FACILITY", "COMMUNITY"}, ValidTarget: []string{"MARKET"}},
				{EdgeType: "HAS_SUBMARKET", Direction: "MARKET → MARKET", ValidSource: []string{"MARKET"}, ValidTarget: []string{"MARKET"}},
				{EdgeType: "ADMINISTERS", Direction: "ORGANIZATION|GEO_REGION → ORGANIZATION|PROGRAM|GEO_REGION", ValidSource: []string{"ORGANIZATION", "GEO_REGION"}, ValidTarget: []string{"ORGANIZATION", "PROGRAM", "GEO_REGION"}, Description: "Government hierarchy"},
				{EdgeType: "EXTRACTS_FROM", Direction: "FACILITY → DEPOSIT", ValidSource: []string{"FACILITY"}, ValidTarget: []string{"DEPOSIT"}},
				{EdgeType: "MEMBER_OF", Direction: "PERSON → COMMUNITY", ValidSource: []string{"PERSON"}, ValidTarget: []string{"COMMUNITY"}},
				{EdgeType: "HAS_SUBGROUP", Direction: "COMMUNITY → COMMUNITY", ValidSource: []string{"COMMUNITY"}, ValidTarget: []string{"COMMUNITY"}},
				{EdgeType: "OPERATES_VIA", Direction: "COMMUNITY|ORG → MEDIA_CHANNEL", ValidSource: []string{"COMMUNITY", "ORGANIZATION"}, ValidTarget: []string{"MEDIA_CHANNEL"}},
				{EdgeType: "AFFILIATED_WITH", Direction: "COMMUNITY|PERSON → ORGANIZATION", ValidSource: []string{"COMMUNITY", "PERSON"}, ValidTarget: []string{"ORGANIZATION"}},

				// --- DOMAIN edges (knowledge contracts) ---
				// Domain → Entity/Domain (generic “focus” link).
				{EdgeType: "FOCUSES_ON", Direction: "DOMAIN → *", ValidSource: []string{"DOMAIN"}, ValidTarget: []string{"DOMAIN", "ORGANIZATION", "PERSON", "MARKET", "PRODUCT", "PROGRAM", "FACILITY", "GEO_REGION", "COMMUNITY", "MEDIA_CHANNEL", "DEPOSIT"}},
				// Domain → Actor (principal entities involved in a domain).
				{EdgeType: "INVOLVES_ACTOR", Direction: "DOMAIN → ORG|PERSON|COMMUNITY", ValidSource: []string{"DOMAIN"}, ValidTarget: []string{"ORGANIZATION", "PERSON", "COMMUNITY"}},
				// Domain → Geo scope.
				{EdgeType: "IN_GEO_SCOPE", Direction: "DOMAIN → GEO_REGION", ValidSource: []string{"DOMAIN"}, ValidTarget: []string{"GEO_REGION"}},
				// Domain → About (topic/subject matter).
				{EdgeType: "ABOUT", Direction: "DOMAIN → *", ValidSource: []string{"DOMAIN"}, ValidTarget: []string{"DOMAIN", "ORGANIZATION", "PERSON", "MARKET", "PRODUCT", "PROGRAM", "FACILITY", "GEO_REGION", "COMMUNITY", "MEDIA_CHANNEL", "DEPOSIT"}},
				// Domain → Domain (contract relationships).
				{EdgeType: "RELATED_DOMAIN", Direction: "DOMAIN → DOMAIN", ValidSource: []string{"DOMAIN"}, ValidTarget: []string{"DOMAIN"}},
				{EdgeType: "BROADENS_TO", Direction: "DOMAIN → DOMAIN", ValidSource: []string{"DOMAIN"}, ValidTarget: []string{"DOMAIN"}},
				{EdgeType: "NARROWS_TO", Direction: "DOMAIN → DOMAIN", ValidSource: []string{"DOMAIN"}, ValidTarget: []string{"DOMAIN"}},
				{EdgeType: "AFFECTS", Direction: "DOMAIN → DOMAIN", ValidSource: []string{"DOMAIN"}, ValidTarget: []string{"DOMAIN"}},
				// DEPENDS_ON is broadly used; allow DOMAIN and existing entity usage.
				{EdgeType: "DEPENDS_ON", Direction: "* → *", ValidSource: []string{"DOMAIN", "ORGANIZATION", "PERSON", "MARKET", "PRODUCT", "PROGRAM", "FACILITY", "GEO_REGION", "COMMUNITY", "MEDIA_CHANNEL", "DEPOSIT"}, ValidTarget: []string{"DOMAIN", "ORGANIZATION", "PERSON", "MARKET", "PRODUCT", "PROGRAM", "FACILITY", "GEO_REGION", "COMMUNITY", "MEDIA_CHANNEL", "DEPOSIT"}},

				// --- STRATEGIC DECISION LAYER edges ---
				// SURFACES: domain crystallizes/surfaces decision points and mechanisms during Phase 2 strategic overlay.
				{EdgeType: "SURFACES", Direction: "DOMAIN → DECISION_POINT|MECHANISM", ValidSource: []string{"DOMAIN"}, ValidTarget: []string{"DECISION_POINT", "MECHANISM"}, Description: "Domain surfaces strategic decision points"},
				// CONTROLS_ACCESS: strategic gatekeeping (block/enable/delay/monitor).
				// Source: DECISION_POINT or GOV/ORG (gatekeeper). Target: entity being controlled.
				{EdgeType: "CONTROLS_ACCESS", Direction: "DECISION_POINT|ORG → ORG|FACILITY|PRODUCT|MARKET|PROGRAM", ValidSource: []string{"DECISION_POINT", "ORGANIZATION"}, ValidTarget: []string{"ORGANIZATION", "FACILITY", "PRODUCT", "MARKET", "PROGRAM"}, Description: "Strategic gatekeeping relation"},
				// IMPOSES_CONDITION: soft power / compliance requirements.
				{EdgeType: "IMPOSES_CONDITION", Direction: "ORG|DECISION_POINT → ORG|FACILITY|PRODUCT", ValidSource: []string{"ORGANIZATION", "DECISION_POINT"}, ValidTarget: []string{"ORGANIZATION", "FACILITY", "PRODUCT"}, Description: "Imposing rules/costs/compliance"},
				// USES_MECHANISM: agent possesses/uses a control mechanism.
				{EdgeType: "USES_MECHANISM", Direction: "DECISION_POINT|ORG → MECHANISM", ValidSource: []string{"DECISION_POINT", "ORGANIZATION"}, ValidTarget: []string{"MECHANISM"}, Description: "Agent uses control mechanism"},
				// APPLIES_TO: mechanism scope.
				{EdgeType: "APPLIES_TO", Direction: "MECHANISM → ORG|FACILITY|PRODUCT|MARKET", ValidSource: []string{"MECHANISM"}, ValidTarget: []string{"ORGANIZATION", "FACILITY", "PRODUCT", "MARKET"}, Description: "Mechanism applies to entity"},
			},
		},
		SymmetricEdges: []string{
			"COMPETES_WITH",
			"PARTNERS_WITH",
			"RELATIVE_OF",
			"RELATED_TO",
		},
	}
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
	sb.WriteString(fmt.Sprintf("Legal forms: %s\n", strings.Join(r.Org.LegalForms, ", ")))
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

	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 4 {
		return false, fmt.Sprintf("GEO_REGION path must have 4 segments (/loc/{cc}/{admin1}/{city}), got %d", len(parts))
	}

	cc := parts[1]
	if len(cc) != 2 && !strings.Contains(cc, "-") && !taxonomy.IsRegionalGeoToken(cc) { // Allow composite like "ae-sa" and region tokens
		return false, fmt.Sprintf("Invalid geo code: %s", cc)
	}

	// Basic admin1 validation (should not be a city name)
	admin1 := parts[2]
	if admin1 != "na" && len(admin1) > 10 {
		return false, fmt.Sprintf("Admin1 code '%s' looks like a city name, not an admin code", admin1)
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

	// Special-case: international organizations (/org/int/...) do not require a legal form suffix.
	// Rationale: many intergovernmental / international bodies are not incorporated as companies.
	if strings.EqualFold(strings.TrimSpace(parts[1]), "int") {
		return true, ""
	}

	// Check for legal form suffix
	slug := parts[len(parts)-1]
	hasLegalForm := false
	for _, lf := range r.Org.LegalForms {
		if strings.HasSuffix(slug, "-"+lf) {
			hasLegalForm = true
			break
		}
	}

	if !hasLegalForm {
		return false, fmt.Sprintf("ORGANIZATION path should include legal form suffix (e.g., -llc, -jsc): %s", path)
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

	geo := strings.TrimSpace(parts[1])
	if geo == "" {
		return false, "GOVERNMENT path missing jurisdiction segment"
	}
	if geo != "na" && len(geo) != 2 && !strings.Contains(geo, "-") {
		return false, fmt.Sprintf("Invalid ISO2 jurisdiction code: %s", geo)
	}

	return true, ""
}

// ValidateProductPath validates a PRODUCT canonical path format.
func (r *TaxonomyRules) ValidateProductPath(path string) (bool, string) {
	if !strings.HasPrefix(path, "/product/") {
		return false, "PRODUCT path must start with /product/"
	}

	parts := strings.Split(strings.Trim(path, "/"), "/")
	// /product/{slug}
	if len(parts) == 2 {
		return true, ""
	}
	// /product/{category}/{type}/{stage}/{slug}
	if len(parts) != 5 {
		return false, fmt.Sprintf("PRODUCT path must be /product/{slug} or /product/{category}/{type}/{stage}/{slug}, got %d segments", len(parts))
	}

	if !containsString(r.Product.Categories, parts[1]) {
		return false, fmt.Sprintf("Invalid product category: %s", parts[1])
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
	edgeTypeStr := string(edgeType)

	for _, rule := range r.EdgeDirection.Rules {
		if rule.EdgeType == edgeTypeStr {
			sourceValid := containsString(rule.ValidSource, string(sourceType))
			targetValid := containsString(rule.ValidTarget, string(targetType))

			if sourceValid && targetValid {
				return true, false, ""
			}

			// Check if reversed would be valid
			reversedSourceValid := containsString(rule.ValidSource, string(targetType))
			reversedTargetValid := containsString(rule.ValidTarget, string(sourceType))

			if reversedSourceValid && reversedTargetValid {
				return false, true, fmt.Sprintf("Edge %s should be reversed: %s → %s, not %s → %s",
					edgeTypeStr, targetType, sourceType, sourceType, targetType)
			}

			return false, false, fmt.Sprintf("Edge %s has invalid endpoint types: source=%s, target=%s",
				edgeTypeStr, sourceType, targetType)
		}
	}

	// No rule found = valid
	return true, false, ""
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
