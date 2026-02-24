package edgesemantics

import (
	"fmt"
	"sort"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// AllEdgeTypesForPrompt returns all canonical edge types from EdgeDirectionPolicies
// formatted as a prompt enum: `"TYPE_A" | "TYPE_B" | ...`.
// This is the single source of truth for LLM prompt schemas.
func AllEdgeTypesForPrompt() string {
	types := make([]string, 0, len(EdgeDirectionPolicies))
	for t := range EdgeDirectionPolicies {
		types = append(types, string(t))
	}
	sort.Strings(types)

	quoted := make([]string, 0, len(types))
	for _, t := range types {
		quoted = append(quoted, `"`+t+`"`)
	}
	return strings.Join(quoted, " | ")
}

// AllEdgeTypesList returns all canonical edge types from EdgeDirectionPolicies as a sorted string slice.
func AllEdgeTypesList() []string {
	types := make([]string, 0, len(EdgeDirectionPolicies))
	for t := range EdgeDirectionPolicies {
		types = append(types, string(t))
	}
	sort.Strings(types)
	return types
}

// EdgeAllowlistByDomainMode returns dependency types allowed for the given domain mode.
// Returns nil for general mode (no restriction: all types allowed).
var domainModeAllowlists = map[string][]models.DependencyType{
	taxonomy.DomainModeSupplyChain: {
		models.DependencyTypeDependsOn,
		models.DependencyTypeProduces,
		models.DependencyTypeConsumes,
		models.DependencyTypeSuppliesTo,
		models.DependencyTypeTransformsInto,
		models.DependencyTypeExtractsFrom,
		models.DependencyTypeLocatedIn,
		models.DependencyTypeOperatesIn,
	},
	taxonomy.DomainModeGovernance: {
		models.DependencyTypeOwns,
		models.DependencyTypeSubsidiaryOf,
		models.DependencyTypeControls,
		models.DependencyTypeManages,
		models.DependencyTypeInvestsIn,
		models.DependencyTypeBoardMemberOf,
		models.DependencyTypeEmployedBy,
		models.DependencyTypeFounded,
		models.DependencyTypeAdministers,
	},
	taxonomy.DomainModeAssurance: {
		models.DependencyTypeAssures,
		models.DependencyTypeAuditedBy,
		models.DependencyTypeRegulates,
	},
	taxonomy.DomainModeMarket: {
		models.DependencyTypeParticipatesIn,
		models.DependencyTypeHasMarketSegment,
	},
	taxonomy.DomainModeStrategic: {
		models.DependencyTypeControlsAccess,
		models.DependencyTypeImposesCondition,
		models.DependencyTypeGatekeepsDecision,
		models.DependencyTypeUsesMechanism,
		models.DependencyTypeAppliesTo,
	},
	taxonomy.DomainModePolicyImpact: {
		models.DependencyTypeRegulates,
		models.DependencyTypeControlsAccess,
		models.DependencyTypeImposesCondition,
		models.DependencyTypeUsesMechanism,
		models.DependencyTypeAppliesTo,
		models.DependencyTypeInfluences,
		models.DependencyTypeDependsOn,
		models.DependencyTypeParticipatesIn,
		models.DependencyTypeLocatedIn,
		models.DependencyTypeOperatesIn,
		models.DependencyTypeAdministers,
	},
}

func EdgeAllowlistByDomainMode(mode string) []models.DependencyType {
	mode = strings.TrimSpace(strings.ToLower(mode))
	if mode == taxonomy.DomainModeGeneral || mode == "" {
		return nil
	}
	if list, ok := domainModeAllowlists[mode]; ok {
		out := make([]models.DependencyType, len(list))
		copy(out, list)
		return out
	}
	return nil
}

// EdgeAllowlistByDomainModes returns the union of allowlists for the given modes (deduplicated).
// Returns nil (no restriction) if any mode is general or the list is empty.
func EdgeAllowlistByDomainModes(modes []string) []models.DependencyType {
	if len(modes) == 0 {
		return nil
	}
	seen := make(map[models.DependencyType]struct{})
	for _, mode := range modes {
		mode = strings.TrimSpace(strings.ToLower(mode))
		if mode == taxonomy.DomainModeGeneral {
			return nil
		}
		for _, dt := range EdgeAllowlistByDomainMode(mode) {
			seen[dt] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]models.DependencyType, 0, len(seen))
	for dt := range seen {
		out = append(out, dt)
	}
	return out
}

// AllowlistContains returns true if the allowlist is nil (allow all) or contains the given type.
func AllowlistContains(allowlist []models.DependencyType, t models.DependencyType) bool {
	if allowlist == nil {
		return true
	}
	for _, a := range allowlist {
		if a == t {
			return true
		}
	}
	return false
}

// EdgeDirectionValidationRulesForPrompt generates a prompt block listing
// source/target type constraints for every edge type, derived from
// EdgeDirectionPolicies. This replaces all hand-maintained direction rule
// lists in LLM prompts.
func EdgeDirectionValidationRulesForPrompt() string {
	types := make([]string, 0, len(EdgeDirectionPolicies))
	for t := range EdgeDirectionPolicies {
		types = append(types, string(t))
	}
	sort.Strings(types)

	var sb strings.Builder
	sb.WriteString("EDGE DIRECTION VALIDATION RULES (edges violating these will be REJECTED or REVERSED):\n")
	for _, t := range types {
		rule := EdgeDirectionPolicies[models.DependencyType(t)]
		src := joinFT(rule.ValidSourceTypes)
		tgt := joinFT(rule.ValidTargetTypes)
		desc := strings.TrimSpace(rule.Description)
		if desc != "" {
			sb.WriteString(fmt.Sprintf("  - %s: source MUST be %s, target MUST be %s. %s\n", t, src, tgt, desc))
		} else {
			sb.WriteString(fmt.Sprintf("  - %s: source MUST be %s, target MUST be %s.\n", t, src, tgt))
		}
	}
	return sb.String()
}

func joinFT(types []models.FragmentType) string {
	if len(types) == 0 {
		return "*"
	}
	parts := make([]string, 0, len(types))
	for _, t := range types {
		parts = append(parts, string(t))
	}
	return strings.Join(parts, " | ")
}

// EdgeAllowlistStrings returns the string representations of allowed edge types
// for the given domain modes. Returns nil if no restriction.
func EdgeAllowlistStrings(modes []string) []string {
	allowlist := EdgeAllowlistByDomainModes(modes)
	if allowlist == nil {
		return nil
	}
	out := make([]string, 0, len(allowlist))
	for _, dt := range allowlist {
		out = append(out, string(dt))
	}
	sort.Strings(out)
	return out
}
