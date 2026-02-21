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
		case DomainModeSupplyChain, DomainModeGovernance, DomainModeAssurance, DomainModeMarket, DomainModeStrategic, DomainModePolicyImpact, DomainModeGeneral:
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
	default:
		return ""
	}
}

// GovernanceEdgeTypes returns the canonical edge types for governance/organ management context.
// Used by: Scout GovernanceResearchStrategy, frontend layout (GOVERNANCE_EDGE_SET), ObjectInspector.
// Single source of truth for "орган управления" semantics: ownership, control, board, management, founders.
var GovernanceEdgeTypes = []string{
	"OWNS", "SUBSIDIARY_OF", "CONTROLS", "MANAGES", "INVESTS_IN",
	"BOARD_MEMBER_OF", "EXECUTIVE_OF", "EMPLOYED_BY", "FOUNDED", "ADMINISTERS",
}

// GovernanceEdgeTypesString returns comma-separated list for prompts.
// Must stay in sync with GovernanceEdgeTypes (EXECUTIVE_OF excluded — display alias for EMPLOYED_BY).
func GovernanceEdgeTypesString() string {
	return "OWNS, SUBSIDIARY_OF, CONTROLS, MANAGES, INVESTS_IN, BOARD_MEMBER_OF, EMPLOYED_BY, FOUNDED, ADMINISTERS"
}

// AssuranceEdgeTypes — Third Line: independent audit, certification, regulatory approval, inspection.
// Single source of truth for "assurance & oversight" semantics.
var AssuranceEdgeTypes = []string{
	"ASSURES", "AUDITED_BY", "REGULATES",
}

// AssuranceEdgeTypesString returns comma-separated list for prompts.
func AssuranceEdgeTypesString() string {
	return "ASSURES, AUDITED_BY, REGULATES"
}

// AssuranceTypeQualifierValues — allowed values for qualifiers.assurance_type on ASSURES edges.
const (
	AssuranceTypeAudit         = "audit"
	AssuranceTypeCertification = "certification"
	AssuranceTypeRegulatory    = "regulatory"
	AssuranceTypeInspection    = "inspection"
)

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
