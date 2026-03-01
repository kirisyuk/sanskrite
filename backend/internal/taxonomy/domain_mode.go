package taxonomy

import "strings"

// DomainMode determines which Scout phases, Research strategies, and Critic validators run.
// Source of truth: intent-resolved domains (contract types) and steering profile mapping.
const (
	DomainModeSupplyChain  = "supply_chain"
	DomainModeGovernance   = "governance"
	DomainModeAssurance    = "assurance"
	DomainModeMarket       = "market"
	DomainModeStrategic    = "strategic"
	DomainModePolicyImpact = "policy_impact"
	DomainModeEcosystem    = "ecosystem"
	DomainModeGeneral      = "general"
)

// DomainMode is a string type for domain execution mode.
type DomainMode string

// NormalizeDomainModes returns a deduplicated list of canonical domain modes.
// Unknown values are ignored. Order is preserved (first occurrence wins).
func NormalizeDomainModes(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(strings.ToLower(s))
		if s == "" {
			continue
		}
		switch s {
		case DomainModeSupplyChain, DomainModeGovernance, DomainModeAssurance, DomainModeMarket, DomainModeStrategic, DomainModePolicyImpact, DomainModeEcosystem, DomainModeGeneral:
			if _, ok := seen[s]; !ok {
				seen[s] = struct{}{}
				out = append(out, s)
			}
		default:
			// allow synonyms
			if canon, ok := domainModeSynonyms[s]; ok && canon != "" {
				if _, ok := seen[canon]; !ok {
					seen[canon] = struct{}{}
					out = append(out, canon)
				}
			}
		}
	}
	return out
}

var domainModeSynonyms = map[string]string{
	"supply_chain":  DomainModeSupplyChain,
	"supply-chain":  DomainModeSupplyChain,
	"governance":    DomainModeGovernance,
	"assurance":     DomainModeAssurance,
	"audit":         DomainModeAssurance,
	"market":        DomainModeMarket,
	"strategic":     DomainModeStrategic,
	"policy_impact": DomainModePolicyImpact,
	"policy-impact": DomainModePolicyImpact,
	"general":       DomainModeGeneral,
	"ecosystem":     DomainModeEcosystem,
	"equity":        DomainModeGovernance, // specialty equity -> governance mode
}

// EntityTypesForDomainMode returns the entity types string for Research/Critic prompts.
// Shared across composable Research strategies and Critic validators.
func EntityTypesForDomainMode(mode string) string {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case DomainModeSupplyChain:
		return "ORGANIZATION, FACILITY, PRODUCT, GEO_REGION, DEPOSIT, COMMUNITY (informal trade networks), PROGRAM (procurement/subsidy programs)"
	case DomainModeGovernance:
		return "ORGANIZATION (including /gov/ government bodies), PERSON, COMMUNITY (dynasty/tribe can OWNS), PROGRAM (ADMINISTERS target)"
	case DomainModeAssurance:
		return "ORGANIZATION (audit firms, certifiers, regulators as assurers), FACILITY, PROGRAM (assurees)"
	case DomainModeMarket:
		return "ORGANIZATION, FACILITY, COMMUNITY, PROGRAM, MARKET"
	case DomainModeStrategic:
		return "ORGANIZATION (including /gov/), PERSON, COMMUNITY, DECISION_POINT, MECHANISM"
	case DomainModePolicyImpact:
		return "ORGANIZATION (including /gov/), PROGRAM, MARKET, COMMUNITY, GEO_REGION, DECISION_POINT, MECHANISM"
	case DomainModeEcosystem:
		return "ORGANIZATION (including /gov/), FACILITY, PERSON, COMMUNITY, PROGRAM, MARKET, GEO_REGION, PRODUCT"
	default:
		return ""
	}
}

// EntityActorTypesForDomainMode returns actor entity types (sources of relations) for domain validation.
// Used by Critic validators for "allowed actors" in chain/ownership/market checks.
func EntityActorTypesForDomainMode(mode string) string {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case DomainModeSupplyChain:
		return "ORGANIZATION, FACILITY, COMMUNITY (informal trade), PROGRAM (procurement)"
	case DomainModeGovernance:
		return "ORGANIZATION, PERSON, COMMUNITY (dynasty/tribe)"
	case DomainModeAssurance:
		return "ORGANIZATION (audit firms, certifiers, regulators)"
	case DomainModeMarket:
		return "ORGANIZATION, FACILITY, COMMUNITY, PROGRAM"
	case DomainModePolicyImpact:
		return "ORGANIZATION (including /gov/), PROGRAM, COMMUNITY"
	case DomainModeEcosystem:
		return "ORGANIZATION, PERSON, COMMUNITY, PROGRAM"
	default:
		return ""
	}
}

// SupplyChainEdgeTypes returns canonical edge types for supply chain context.
// Used by: Scout SupplyChainResearchStrategy, Research agent supply chain enrichment.
// Single source of truth for supply chain relation semantics.
var SupplyChainEdgeTypes = []string{
	"DEPENDS_ON", "PRODUCES", "CONSUMES", "SUPPLIES_TO",
	"TRANSFORMS_INTO", "EXTRACTS_FROM", "LOCATED_IN", "OPERATES_IN",
	"REGULATES", "PARTICIPATES_IN", "ADMINISTERS",
}

// SupplyChainEdgeTypesString returns comma-separated list for prompts.
func SupplyChainEdgeTypesString() string {
	return "DEPENDS_ON, PRODUCES, CONSUMES, SUPPLIES_TO, TRANSFORMS_INTO, EXTRACTS_FROM, LOCATED_IN, OPERATES_IN, REGULATES, PARTICIPATES_IN, ADMINISTERS"
}

// GovernanceEdgeTypes returns canonical edge types for governance/organizational management context.
// Used by: Scout GovernanceResearchStrategy, frontend layout (GOVERNANCE_EDGE_SET), ObjectInspector.
// Single source of truth for "орган управления" semantics: ownership, control, board, management, founders,
// and external assurance actors (auditors/certifiers).
var GovernanceEdgeTypes = []string{
	"OWNS", "SUBSIDIARY_OF", "CONTROLS", "MANAGES", "INVESTS_IN",
	"BOARD_MEMBER_OF", "EXECUTIVE_OF", "EMPLOYED_BY", "FOUNDED", "ADMINISTERS",
	"ASSURES",
}

// GovernanceEdgeTypesString returns comma-separated list for prompts.
// Must stay in sync with GovernanceEdgeTypes (EXECUTIVE_OF excluded — display alias for EMPLOYED_BY).
func GovernanceEdgeTypesString() string {
	return "OWNS, SUBSIDIARY_OF, CONTROLS, MANAGES, INVESTS_IN, BOARD_MEMBER_OF, EMPLOYED_BY, FOUNDED, ADMINISTERS, ASSURES"
}

// AssuranceEdgeTypes — Third Line: independent audit, certification, regulatory approval, inspection.
// Single source of truth for "assurance & oversight" semantics.
var AssuranceEdgeTypes = []string{
	"ASSURES", "REGULATES",
}

// AssuranceEdgeTypesString returns comma-separated list for prompts.
func AssuranceEdgeTypesString() string {
	return "ASSURES, REGULATES"
}

// AssuranceTypeQualifierValues — allowed values for qualifiers.assurance_type on ASSURES edges.
const (
	AssuranceTypeAudit         = "audit"
	AssuranceTypeCertification = "certification"
	AssuranceTypeRegulatory    = "regulatory"
	AssuranceTypeInspection    = "inspection"
)

// EcosystemEdgeTypes returns canonical edge types for ecosystem/stakeholder mapping context.
var EcosystemEdgeTypes = []string{
	"PARTICIPATES_IN", "OWNS", "SUBSIDIARY_OF", "CONTROLS", "SUPPLIES_TO",
	"DEPENDS_ON", "COMPETES_WITH", "REGULATES", "INFLUENCES", "INVESTS_IN",
	"PARTNERS_WITH", "LOCATED_IN", "OPERATES_IN",
}

// EcosystemEdgeTypesString returns comma-separated list for prompts.
func EcosystemEdgeTypesString() string {
	return "PARTICIPATES_IN, OWNS, SUBSIDIARY_OF, CONTROLS, SUPPLIES_TO, DEPENDS_ON, COMPETES_WITH, REGULATES, INFLUENCES, INVESTS_IN, PARTNERS_WITH, LOCATED_IN, OPERATES_IN"
}

// MarketEdgeTypesString returns comma-separated list for prompts (market/competitive analysis).
func MarketEdgeTypesString() string {
	return "PARTICIPATES_IN, HAS_MARKET_SEGMENT"
}

// StrategicEdgeTypesString returns comma-separated list for prompts (strategic/geopolitical analysis).
func StrategicEdgeTypesString() string {
	return "CONTROLS_ACCESS, IMPOSES_CONDITION, GATEKEEPS_DECISION, USES_MECHANISM, APPLIES_TO"
}

// PolicyImpactEdgeTypesString returns comma-separated list for prompts (regulatory impact).
func PolicyImpactEdgeTypesString() string {
	return "REGULATES, CONTROLS_ACCESS, IMPOSES_CONDITION, GATEKEEPS_DECISION, USES_MECHANISM, APPLIES_TO, INFLUENCES, DEPENDS_ON, PARTICIPATES_IN, LOCATED_IN, OPERATES_IN, ADMINISTERS"
}

// HasDomainMode returns true if modes contain the given mode (after normalizing the target).
func HasDomainMode(modes []string, mode string) bool {
	mode = strings.TrimSpace(strings.ToLower(mode))
	if canon, ok := domainModeSynonyms[mode]; ok {
		mode = canon
	}
	for _, m := range modes {
		if strings.TrimSpace(strings.ToLower(m)) == mode {
			return true
		}
	}
	return false
}
