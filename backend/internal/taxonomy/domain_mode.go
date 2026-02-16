package taxonomy

import "strings"

// DomainMode determines which Scout phases, Research strategies, and Critic validators run.
// Source of truth: intent-resolved domains (contract types) and steering profile mapping.
const (
	DomainModeSupplyChain = "supply_chain"
	DomainModeGovernance  = "governance"
	DomainModeMarket      = "market"
	DomainModeStrategic   = "strategic"
	DomainModeGeneral     = "general"
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
		case DomainModeSupplyChain, DomainModeGovernance, DomainModeMarket, DomainModeStrategic, DomainModeGeneral:
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
	"supply_chain": DomainModeSupplyChain,
	"supply-chain": DomainModeSupplyChain,
	"governance":   DomainModeGovernance,
	"market":       DomainModeMarket,
	"strategic":    DomainModeStrategic,
	"general":      DomainModeGeneral,
	"equity":       DomainModeGovernance, // specialty equity -> governance mode
}

// EntityTypesForDomainMode returns the entity types string for Research/Critic prompts.
// Shared across composable Research strategies and Critic validators.
func EntityTypesForDomainMode(mode string) string {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case DomainModeSupplyChain:
		return "ORGANIZATION, FACILITY, PRODUCT, GEO_REGION, DEPOSIT, COMMUNITY (informal trade networks), PROGRAM (procurement/subsidy programs)"
	case DomainModeGovernance:
		return "ORGANIZATION (including /gov/ government bodies), PERSON, COMMUNITY (dynasty/tribe can OWNS), PROGRAM (ADMINISTERS target)"
	case DomainModeMarket:
		return "ORGANIZATION, FACILITY, COMMUNITY, PROGRAM, MARKET"
	case DomainModeStrategic:
		return "ORGANIZATION (including /gov/), PERSON, COMMUNITY, DECISION_POINT, MECHANISM"
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
	case DomainModeMarket:
		return "ORGANIZATION, FACILITY, COMMUNITY, PROGRAM"
	default:
		return ""
	}
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
