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
		return "FACILITY (primary actor for supplies/produces), ORGANIZATION (OWNS, LOCATED_IN — supply flow endpoints are FACILITY), PRODUCT, GEO_REGION, DEPOSIT, COMMUNITY (informal trade), PROGRAM (procurement)"
	case DomainModeGovernance:
		return "ORGANIZATION (including /gov/ government bodies), FACILITY, DEPOSIT, PERSON, COMMUNITY (dynasty/tribe can OWNS), PROGRAM (ADMINISTERS target), MEDIA_CHANNEL (community control/mobilization channel)"
	case DomainModeAssurance:
		return "ORGANIZATION (audit firms, certifiers, regulators as assurers), FACILITY, PRODUCT, PROGRAM (assurees)"
	case DomainModeMarket:
		return "ORGANIZATION, FACILITY, COMMUNITY, PERSON, PROGRAM, MARKET, MEDIA_CHANNEL"
	case DomainModeStrategic:
		return "ORGANIZATION (including /gov/), FACILITY, PRODUCT, MARKET, PROGRAM, PERSON, COMMUNITY, MEDIA_CHANNEL, DECISION_POINT, MECHANISM"
	case DomainModePolicyImpact:
		return "ORGANIZATION (including /gov/), FACILITY, PRODUCT, PROGRAM, MARKET, COMMUNITY, PERSON, MEDIA_CHANNEL, GEO_REGION, DECISION_POINT, MECHANISM"
	case DomainModeEcosystem:
		return "ORGANIZATION (including /gov/), FACILITY, PERSON, COMMUNITY, PROGRAM, MARKET, GEO_REGION, PRODUCT, MEDIA_CHANNEL"
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
		return "ORGANIZATION, FACILITY, COMMUNITY, PERSON, PROGRAM"
	case DomainModePolicyImpact:
		return "ORGANIZATION (including /gov/), PROGRAM, COMMUNITY, PERSON"
	case DomainModeStrategic:
		return "ORGANIZATION (including /gov/), PROGRAM, COMMUNITY, PERSON, DECISION_POINT"
	case DomainModeEcosystem:
		return "ORGANIZATION, PERSON, COMMUNITY, PROGRAM"
	default:
		return ""
	}
}

// SupplyChainEdgeTypes is the allowlist for supply_chain mode (filtering, gap analyzer).
// Operational edges only; DEPENDS_ON excluded to avoid generic dependency proliferation.
// CONSUMES removed: use SUPPLIES_TO(product→org) for input flow. Single source of truth.
// Must stay in sync with edgesemantics.domainModeAllowlists[supply_chain].
var SupplyChainEdgeTypes = []string{
	"PRODUCES", "SUPPLIES_TO", "TRANSFORMS_INTO", "EXTRACTS_FROM",
	"LOCATED_IN", "OPERATES_IN", "PARTICIPATES_IN",
	"HAS_MARKET_SEGMENT",
}

// SupplyChainEdgeTypesString returns comma-separated list for prompts. Derives from SupplyChainEdgeTypes.
func SupplyChainEdgeTypesString() string {
	return strings.Join(SupplyChainEdgeTypes, ", ")
}

// SupplyChainEdgeTypesForPrompt returns prompt enum string for supply-chain-only schemas:
// `"PRODUCES" | "SUPPLIES_TO" | ...`. Use in supply-chain contract prompts instead of AllEdgeTypesForPrompt.
func SupplyChainEdgeTypesForPrompt() string {
	quoted := make([]string, 0, len(SupplyChainEdgeTypes))
	for _, t := range SupplyChainEdgeTypes {
		quoted = append(quoted, `"`+t+`"`)
	}
	return strings.Join(quoted, " | ")
}

// supplyChainEdgeTypesSet is initialized at startup for O(1) allowlist checks.
var supplyChainEdgeTypesSet map[string]struct{}

func init() {
	supplyChainEdgeTypesSet = make(map[string]struct{}, len(SupplyChainEdgeTypes))
	for _, t := range SupplyChainEdgeTypes {
		supplyChainEdgeTypesSet[strings.TrimSpace(strings.ToUpper(t))] = struct{}{}
	}
}

// IsSupplyChainEdgeType returns true if t is in SupplyChainEdgeTypes (case-insensitive).
// Use for runtime guards before materialization.
func IsSupplyChainEdgeType(t string) bool {
	_, ok := supplyChainEdgeTypesSet[strings.TrimSpace(strings.ToUpper(t))]
	return ok
}

// SupplyChainOperationalEdgeTypes are the only edge types for supply chain structural links (fac→fac, fac→product).
// Use for "Use ONLY these" in supply chain prompts. Subset of SupplyChainEdgeTypes.
var SupplyChainOperationalEdgeTypes = []string{
	"PRODUCES", "SUPPLIES_TO", "TRANSFORMS_INTO", "EXTRACTS_FROM",
}

// SupplyChainOperationalEdgeTypesString returns comma-separated list for operational-link prompts.
func SupplyChainOperationalEdgeTypesString() string {
	return "PRODUCES, SUPPLIES_TO, TRANSFORMS_INTO, EXTRACTS_FROM"
}

// GovernanceEdgeTypes returns canonical edge types for governance/organizational management context.
// Used by: Scout GovernanceResearchStrategy, frontend layout (GOVERNANCE_EDGE_SET), ObjectInspector.
// Single source of truth for "орган управления" semantics: ownership, control, board, management, founders,
// and external assurance actors (auditors/certifiers).
var GovernanceEdgeTypes = []string{
	"OWNS", "SUBSIDIARY_OF", "CONTROLS", "MANAGES", "INVESTS_IN",
	"BOARD_MEMBER_OF", "EXECUTIVE_OF", "EMPLOYED_BY", "FOUNDED", "ADMINISTERS",
	"ASSURES", "HAS_SUBGROUP", "MEMBER_OF", "AFFILIATED_WITH", "OPERATES_VIA",
}

// GovernanceEdgeTypesString returns comma-separated list for prompts.
// Must stay in sync with GovernanceEdgeTypes.
func GovernanceEdgeTypesString() string {
	return strings.Join(GovernanceEdgeTypes, ", ")
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
	"HAS_SUBGROUP", "MEMBER_OF", "AFFILIATED_WITH", "OPERATES_VIA",
}

// EcosystemEdgeTypesString returns comma-separated list for prompts.
func EcosystemEdgeTypesString() string {
	return strings.Join(EcosystemEdgeTypes, ", ")
}

// MarketEdgeTypes is the allowlist for market mode.
var MarketEdgeTypes = []string{
	"PARTICIPATES_IN", "HAS_MARKET_SEGMENT",
	"TRADED_IN", "HAS_SUBGROUP", "MEMBER_OF", "AFFILIATED_WITH", "OPERATES_VIA",
}

// MarketEdgeTypesString returns comma-separated list for prompts (market/competitive analysis).
func MarketEdgeTypesString() string {
	return strings.Join(MarketEdgeTypes, ", ")
}

// StrategicEdgeTypes is the allowlist for strategic mode.
var StrategicEdgeTypes = []string{
	"CONTROLS_ACCESS", "IMPOSES_CONDITION", "GATEKEEPS_DECISION", "USES_MECHANISM", "APPLIES_TO",
	"HAS_SUBGROUP", "MEMBER_OF", "AFFILIATED_WITH", "OPERATES_VIA",
}

// StrategicEdgeTypesString returns comma-separated list for prompts (strategic/geopolitical analysis).
func StrategicEdgeTypesString() string {
	return strings.Join(StrategicEdgeTypes, ", ")
}

// PolicyImpactEdgeTypes is the allowlist for policy_impact mode.
var PolicyImpactEdgeTypes = []string{
	"REGULATES", "CONTROLS_ACCESS", "IMPOSES_CONDITION", "GATEKEEPS_DECISION", "USES_MECHANISM", "APPLIES_TO", "INFLUENCES", "DEPENDS_ON", "PARTICIPATES_IN", "TRADED_IN", "LOCATED_IN", "OPERATES_IN", "ADMINISTERS",
	"HAS_SUBGROUP", "MEMBER_OF", "AFFILIATED_WITH", "OPERATES_VIA",
}

// PolicyImpactEdgeTypesString returns comma-separated list for prompts (regulatory impact).
func PolicyImpactEdgeTypesString() string {
	return strings.Join(PolicyImpactEdgeTypes, ", ")
}

// AllowedEntityTypesForDomainMode returns the strict programmatic list of allowed entity types for a given domain mode.
func AllowedEntityTypesForDomainMode(mode string) []string {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case DomainModeSupplyChain:
		return []string{"ORGANIZATION", "FACILITY", "PRODUCT", "GEO_REGION", "DEPOSIT", "COMMUNITY", "PROGRAM"}
	case DomainModeGovernance:
		return []string{"ORGANIZATION", "FACILITY", "DEPOSIT", "PERSON", "COMMUNITY", "PROGRAM", "MEDIA_CHANNEL"}
	case DomainModeAssurance:
		return []string{"ORGANIZATION", "FACILITY", "PRODUCT", "PROGRAM"}
	case DomainModeMarket:
		return []string{"ORGANIZATION", "FACILITY", "COMMUNITY", "PERSON", "PROGRAM", "MARKET", "MEDIA_CHANNEL"}
	case DomainModeStrategic:
		return []string{"ORGANIZATION", "FACILITY", "PRODUCT", "MARKET", "PROGRAM", "PERSON", "COMMUNITY", "MEDIA_CHANNEL", "DECISION_POINT", "MECHANISM"}
	case DomainModePolicyImpact:
		return []string{"ORGANIZATION", "FACILITY", "PRODUCT", "PROGRAM", "MARKET", "COMMUNITY", "PERSON", "MEDIA_CHANNEL", "GEO_REGION", "DECISION_POINT", "MECHANISM"}
	case DomainModeEcosystem:
		return []string{"ORGANIZATION", "FACILITY", "PERSON", "COMMUNITY", "PROGRAM", "MARKET", "GEO_REGION", "PRODUCT", "MEDIA_CHANNEL"}
	default:
		return nil
	}
}

// AllowedEdgeTypesForDomainMode returns the strict programmatic list of allowed edge types for a given domain mode.
func AllowedEdgeTypesForDomainMode(mode string) []string {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case DomainModeSupplyChain:
		return SupplyChainEdgeTypes
	case DomainModeGovernance:
		return GovernanceEdgeTypes
	case DomainModeAssurance:
		return AssuranceEdgeTypes
	case DomainModeMarket:
		return MarketEdgeTypes
	case DomainModeStrategic:
		return StrategicEdgeTypes
	case DomainModePolicyImpact:
		return PolicyImpactEdgeTypes
	case DomainModeEcosystem:
		return EcosystemEdgeTypes
	default:
		return nil
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
