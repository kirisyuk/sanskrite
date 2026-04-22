package taxonomy

import "strings"

var promptSupplyChainEdgeTypes = []string{"produces", "supplies", "invested_in", "joint_venture_with", "member_of", "wields"}
var promptServiceChainEdgeTypes = []string{"produces", "supplies", "imposes", "wields"}
var promptGovernanceEdgeTypes = []string{"assures", "beneficially_owns", "controls_via", "holds_board_seat", "invested_in", "managed_by", "member_of", "operates", "subsidiary_of", "wields"}
var promptAssuranceEdgeTypes = []string{"assures", "regulates"}
var promptEcosystemEdgeTypes = []string{"beneficially_owns", "controls_via", "influences", "invested_in", "joint_venture_with", "member_of", "regulates", "subsidiary_of", "supplies", "wields"}
var promptMarketEdgeTypes = []string{"listed_on", "member_of", "wields"}
var promptStrategicEdgeTypes = []string{"gatekeeps", "imposes", "member_of", "wields"}
var promptPolicyImpactEdgeTypes = []string{"disrupts", "gatekeeps", "imposes", "influences", "listed_on", "member_of", "operates", "regulates", "wields"}

// DomainMode determines which Scout phases, Research strategies, and Critic validators run.
// Source of truth: intent-resolved domains (contract types) and steering profile mapping.
const (
	DomainModeSupplyChain  = "supply_chain"
	DomainModeServiceChain = "service_chain"
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
		case DomainModeSupplyChain, DomainModeServiceChain, DomainModeGovernance, DomainModeAssurance, DomainModeMarket, DomainModeStrategic, DomainModePolicyImpact, DomainModeEcosystem, DomainModeGeneral:
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
	"supply_chain":             DomainModeSupplyChain,
	"supply-chain":             DomainModeSupplyChain,
	"service_chain":            DomainModeServiceChain,
	"service-chain":            DomainModeServiceChain,
	"service_delivery_network": DomainModeServiceChain,
	"service-delivery-network": DomainModeServiceChain,
	"governance":               DomainModeGovernance,
	"assurance":                DomainModeAssurance,
	"audit":                    DomainModeAssurance,
	"market":                   DomainModeMarket,
	"strategic":                DomainModeStrategic,
	"policy_impact":            DomainModePolicyImpact,
	"policy-impact":            DomainModePolicyImpact,
	"general":                  DomainModeGeneral,
	"ecosystem":                DomainModeEcosystem,
	"equity":                   DomainModeGovernance, // specialty equity -> governance mode
}

// EntityTypesForDomainMode returns the entity types string for Research/Critic prompts.
// Shared across composable Research strategies and Critic validators.
func EntityTypesForDomainMode(mode string) string {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case DomainModeSupplyChain:
		return "site (/fac/; primary actor for produces/supplies), org (/org/ and /gov/), product, geo, deposit (site subtype), community (informal trade), project"
	case DomainModeServiceChain:
		return "site (/fac/ service delivery site, data center, repair depot, network hub), org (provider, reseller, managed-service operator), product (including service capabilities), market, geo, community (informal operators), project"
	case DomainModeGovernance:
		return "org (including /gov/ government bodies), site, deposit, person, community (dynasty/tribe), project, media channel orgs"
	case DomainModeAssurance:
		return "org (audit firms, certifiers, regulators as assurers), site, product, project"
	case DomainModeMarket:
		return "org, site, community, person, project, market, media-channel orgs"
	case DomainModeStrategic:
		return "org (including /gov/), site, product, market, project, person, community, media-channel orgs, decision, mechanism"
	case DomainModePolicyImpact:
		return "org (including /gov/), site, product, project, market, community, person, media-channel orgs, geo, decision, mechanism"
	case DomainModeEcosystem:
		return "org (including /gov/), site, person, community, project, market, geo, product, media-channel orgs"
	default:
		return ""
	}
}

// EntityActorTypesForDomainMode returns actor entity types (sources of relations) for domain validation.
// Used by Critic validators for "allowed actors" in chain/ownership/market checks.
func EntityActorTypesForDomainMode(mode string) string {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case DomainModeSupplyChain:
		return "org, site, community (informal trade), project"
	case DomainModeServiceChain:
		return "org, site, community (informal operators), project"
	case DomainModeGovernance:
		return "org, person, community (dynasty/tribe)"
	case DomainModeAssurance:
		return "org (audit firms, certifiers, regulators)"
	case DomainModeMarket:
		return "org, site, community, person, project"
	case DomainModePolicyImpact:
		return "org (including /gov/), project, community, person"
	case DomainModeStrategic:
		return "org (including /gov/), project, community, person, decision"
	case DomainModeEcosystem:
		return "org, person, community, project"
	default:
		return ""
	}
}

// SupplyChainEdgeTypes is the allowlist for supply_chain mode (filtering, gap analyzer).
// Keep operational path-carried edges plus a small set of safe overlays useful for
// supply-chain context. DEPENDS_ON stays excluded to avoid generic dependency proliferation.
// CONSUMES removed: use SUPPLIES_TO(product→org) for input flow. Single source of truth.
// Must stay in sync with edgesemantics.domainModeAllowlists[supply_chain].
var SupplyChainEdgeTypes = []string{
	"PRODUCES", "SUPPLIES_TO", "transforms_into", "assembles_into", "USED_IN", "EXTRACTS_FROM",
	"LOCATED_IN", "OPERATES_IN", "PARTICIPATES_IN",
	"HAS_MARKET_SEGMENT", "COMPETES_WITH", "PARTNERS_WITH", "OPERATES_VIA", "MEMBER_OF", "INVESTS_IN",
	// AH RelationName aliases (lowercase) — admitted alongside the legacy
	// uppercase entries so that scenario seeders and runtime callers that
	// have already migrated to ah.RelationName remain inside the allow-list.
	// Source-of-truth mapping: docs/archive/taxonomy-migration-2026/migration_map.yaml.
	"produces", "supplies", "transforms_into", "assembles_into", "used_in", "extracts_from",
	"located_in", "operates_in", "participates_in",
	"has_market_segment", "competes_with", "partners_with", "operates_via", "member_of", "invests_in",
}

// ServiceChainEdgeTypes is the allowlist for service_chain mode.
// Keep this tighter than ecosystem/governance, but broad enough to express
// concrete service delivery dependencies and access/control gates.
var ServiceChainEdgeTypes = []string{
	"PRODUCES", "SUPPLIES_TO", "transforms_into", "assembles_into", "USED_IN",
	"DEPENDS_ON", "CONTROLS_ACCESS", "OPERATES_VIA",
	"LOCATED_IN", "OPERATES_IN", "PARTICIPATES_IN",
	"HAS_MARKET_SEGMENT",
}

// SupplyChainEdgeTypesString returns comma-separated list for prompts. Derives from SupplyChainEdgeTypes.
func SupplyChainEdgeTypesString() string {
	return strings.Join(promptSupplyChainEdgeTypes, ", ")
}

// ServiceChainEdgeTypesString returns comma-separated list for prompts. Derives from ServiceChainEdgeTypes.
func ServiceChainEdgeTypesString() string {
	return strings.Join(promptServiceChainEdgeTypes, ", ")
}

// SupplyChainEdgeTypesForPrompt returns prompt enum string for supply-chain-only schemas:
// `"PRODUCES" | "SUPPLIES_TO" | ...`. Use in supply-chain contract prompts instead of AllEdgeTypesForPrompt.
func SupplyChainEdgeTypesForPrompt() string {
	quoted := make([]string, 0, len(promptSupplyChainEdgeTypes))
	for _, t := range promptSupplyChainEdgeTypes {
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
	"PRODUCES", "SUPPLIES_TO", "transforms_into", "assembles_into", "USED_IN", "EXTRACTS_FROM",
}

// SupplyChainOperationalEdgeTypesString returns comma-separated list for operational-link prompts.
func SupplyChainOperationalEdgeTypesString() string {
	return strings.Join(SupplyChainOperationalEdgeTypes, ", ")
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
	return strings.Join(promptGovernanceEdgeTypes, ", ")
}

// AssuranceEdgeTypes — Third Line: independent audit, certification, regulatory approval, inspection.
// Single source of truth for "assurance & oversight" semantics.
var AssuranceEdgeTypes = []string{
	"ASSURES", "REGULATES",
}

// AssuranceEdgeTypesString returns comma-separated list for prompts.
func AssuranceEdgeTypesString() string {
	return strings.Join(promptAssuranceEdgeTypes, ", ")
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
	return strings.Join(promptEcosystemEdgeTypes, ", ")
}

// MarketEdgeTypes is the allowlist for market mode.
var MarketEdgeTypes = []string{
	"PARTICIPATES_IN", "HAS_MARKET_SEGMENT",
	"TRADED_IN", "HAS_SUBGROUP", "MEMBER_OF", "AFFILIATED_WITH", "OPERATES_VIA",
}

// MarketEdgeTypesString returns comma-separated list for prompts (market/competitive analysis).
func MarketEdgeTypesString() string {
	return strings.Join(promptMarketEdgeTypes, ", ")
}

// StrategicEdgeTypes is the allowlist for strategic mode.
var StrategicEdgeTypes = []string{
	"CONTROLS_ACCESS", "IMPOSES_CONDITION", "GATEKEEPS_DECISION", "USES_MECHANISM", "APPLIES_TO",
	"HAS_SUBGROUP", "MEMBER_OF", "AFFILIATED_WITH", "OPERATES_VIA",
}

// StrategicEdgeTypesString returns comma-separated list for prompts (strategic/geopolitical analysis).
func StrategicEdgeTypesString() string {
	return strings.Join(promptStrategicEdgeTypes, ", ")
}

// PolicyImpactEdgeTypes is the allowlist for policy_impact mode.
// Keep this as the single source of truth for prompt/runtime contract.
var PolicyImpactEdgeTypes = []string{
	"REGULATES", "CONTROLS_ACCESS", "IMPOSES_CONDITION", "GATEKEEPS_DECISION", "USES_MECHANISM", "APPLIES_TO", "IN_GEO_SCOPE", "AFFECTS", "ADMINISTERS",
	"INFLUENCES", "DEPENDS_ON", "PARTICIPATES_IN", "TRADED_IN", "LOCATED_IN", "OPERATES_IN",
	"HAS_SUBGROUP", "MEMBER_OF", "AFFILIATED_WITH", "OPERATES_VIA",
	// AH RelationName aliases (lowercase) — admitted alongside the legacy
	// uppercase entries so that scenario seeders and runtime callers that
	// have already migrated to ah.RelationName remain inside the allow-list.
	// Source-of-truth mapping: docs/archive/taxonomy-migration-2026/migration_map.yaml.
	"regulates", "controls_access", "imposes", "gatekeeps", "wields", "applies_to", "in_geo_scope", "disrupts", "operates",
	"influences", "depends_on", "participates_in", "listed_on", "located_in", "operates_in",
	"has_subgroup", "member_of", "affiliated_with", "operates_via",
}

// PolicyImpactEdgeTypesString returns comma-separated list for prompts (regulatory impact).
func PolicyImpactEdgeTypesString() string {
	return strings.Join(promptPolicyImpactEdgeTypes, ", ")
}

// AllowedEntityTypesForDomainMode returns the strict programmatic list of allowed entity types for a given domain mode.
func AllowedEntityTypesForDomainMode(mode string) []string {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case DomainModeSupplyChain:
		return []string{"org", "site", "product", "geo", "community", "project"}
	case DomainModeServiceChain:
		return []string{"org", "site", "product", "market", "geo", "community", "project"}
	case DomainModeGovernance:
		return []string{"org", "site", "person", "community", "project"}
	case DomainModeAssurance:
		return []string{"org", "site", "product", "project"}
	case DomainModeMarket:
		return []string{"org", "site", "community", "person", "project", "market"}
	case DomainModeStrategic:
		return []string{"org", "site", "product", "market", "project", "person", "community", "decision", "mechanism"}
	case DomainModePolicyImpact:
		return []string{"org", "site", "product", "project", "market", "community", "person", "geo", "decision", "mechanism"}
	case DomainModeEcosystem:
		return []string{"org", "site", "person", "community", "project", "market", "geo", "product"}
	default:
		return nil
	}
}

// AllowedEdgeTypesForDomainMode returns the strict programmatic list of allowed edge types for a given domain mode.
func AllowedEdgeTypesForDomainMode(mode string) []string {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case DomainModeSupplyChain:
		return SupplyChainEdgeTypes
	case DomainModeServiceChain:
		return ServiceChainEdgeTypes
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
