package edgesemantics

import (
	"fmt"
	"sort"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// AllRelationsForPrompt returns every canonical AH relation as a sorted
// `"a" | "b" | ...` enum suitable for LLM prompt schemas.
//
// The wire alphabet on graph-intent + scout outputs is intentionally
// the AH SoT itself (lowercase snake_case). Legacy UPPER_SNAKE aliases
// remain accepted on the inbound boundary via models.NormalizeRelationName,
// but new prompts MUST emit the AH names.
func AllRelationsForPrompt() string {
	relations := AllRelationsList()
	quoted := make([]string, 0, len(relations))
	for _, r := range relations {
		quoted = append(quoted, `"`+r+`"`)
	}
	return strings.Join(quoted, " | ")
}

// AllRelationsList returns every canonical AH relation as a sorted slice
// of lowercase snake_case strings.
func AllRelationsList() []string {
	out := make([]string, 0, len(EdgeDirectionPolicies))
	for r := range EdgeDirectionPolicies {
		out = append(out, string(r))
	}
	sort.Strings(out)
	return out
}

// RelationAllowlist is a simple set of AH relation names. nil means
// "no restriction" (allow every AH relation).
type RelationAllowlist []ah.RelationName

// AllowlistContains reports whether allow is nil (allow all) or the
// relation appears in the allowlist.
func AllowlistContains(allow RelationAllowlist, rel ah.RelationName) bool {
	if allow == nil {
		return true
	}
	for _, r := range allow {
		if r == rel {
			return true
		}
	}
	return false
}

// AllowlistContainsLegacy normalises the (possibly legacy, possibly upper
// case) wire string to the AH relation and delegates to AllowlistContains.
// Returns true for unknown wire strings only when allow is nil.
func AllowlistContainsLegacy(allow RelationAllowlist, wire string) bool {
	if allow == nil {
		return true
	}
	dec, ok := models.NormalizeRelationName(wire)
	if !ok || dec.Dropped || dec.Relation == "" {
		return false
	}
	return AllowlistContains(allow, dec.Relation)
}

// domainScaffoldRelations are the small-set of structural relations used
// when DOMAIN_MODE_GENERAL needs explicit "scaffold" relations to keep
// graphs cohesive. Under the new taxonomy DOMAIN/SPHERE classes are
// dropped, so the legacy DOMAIN-rooted scaffolds (FOCUSES_ON, ABOUT,
// IN_GEO_SCOPE, ...) are gone. The remaining structural fallbacks live
// in member_of / influences and are surfaced as the canonical scaffold.
var domainScaffoldRelations = []ah.RelationName{
	ah.RelationNameInfluences,
	ah.RelationNameMemberOf,
}

// DomainScaffoldRelations returns the canonical relations that DOMAIN
// expansion prompts and domain-anchor sanitization must allow even when
// no domain-mode allowlist is in effect.
func DomainScaffoldRelations() []ah.RelationName {
	out := make([]ah.RelationName, len(domainScaffoldRelations))
	copy(out, domainScaffoldRelations)
	return out
}

// DomainScaffoldRelationsString returns a prompt-ready comma-separated
// list of scaffold relations (lowercase AH names).
func DomainScaffoldRelationsString() string {
	parts := make([]string, 0, len(domainScaffoldRelations))
	for _, r := range domainScaffoldRelations {
		parts = append(parts, string(r))
	}
	return strings.Join(parts, ", ")
}

// IsDomainScaffoldRelation reports whether rel belongs to the DOMAIN
// expansion scaffold relation set.
func IsDomainScaffoldRelation(rel ah.RelationName) bool {
	for _, r := range domainScaffoldRelations {
		if r == rel {
			return true
		}
	}
	return false
}

// marketAnchorRelations are AH relations that cluster around /market
// fragments (listed_on, priced_under) and supply-chain market anchors
// (supplies actor_to_market kind row).
var marketAnchorRelations = []ah.RelationName{
	ah.RelationNameListedOn,
	ah.RelationNamePricedUnder,
	ah.RelationNamePublishesBenchmark,
	ah.RelationNameBenchmarkedTo,
}

// IsMarketAnchorRelation reports whether the relation belongs to the
// canonical market-anchor cluster.
func IsMarketAnchorRelation(rel ah.RelationName) bool {
	for _, r := range marketAnchorRelations {
		if r == rel {
			return true
		}
	}
	return false
}

// allowlistFromTaxonomyWire converts a []string of legacy UPPER_SNAKE
// relation names (drawn from internal/taxonomy domain-mode lists) into
// the deduplicated set of canonical AH relations. Unknown / dropped
// wire names are skipped (callers see a tighter allowlist).
func allowlistFromTaxonomyWire(wire []string) RelationAllowlist {
	if len(wire) == 0 {
		return nil
	}
	seen := make(map[ah.RelationName]struct{}, len(wire))
	out := make(RelationAllowlist, 0, len(wire))
	for _, w := range wire {
		if dec, ok := models.NormalizeRelationName(w); ok && !dec.Dropped && dec.Relation != "" {
			if _, dup := seen[dec.Relation]; dup {
				continue
			}
			seen[dec.Relation] = struct{}{}
			out = append(out, dec.Relation)
		}
	}
	return out
}

// domainModeAllowlists pins the canonical AH relation allowlist per
// domain mode. Built at init() from the legacy taxonomy.*EdgeTypes lists
// (which still hold the UPPER_SNAKE alphabet driving prompts) so adding
// a wire alias in taxonomy/domain_mode.go automatically refreshes the
// runtime allowlist.
var domainModeAllowlists = map[string]RelationAllowlist{}

// extraDomainModeRelations augments the wire-derived allowlist with the
// AH relations that have no legacy equivalent but which the domain mode
// must allow (e.g. /decision and /mechanism interactions for strategic).
var extraDomainModeRelations = map[string][]ah.RelationName{
	taxonomy.DomainModeStrategic: {
		ah.RelationNameGatekeeps,
		ah.RelationNameWields,
		ah.RelationNameImposes,
		ah.RelationNameEnactedVia,
	},
	taxonomy.DomainModePolicyImpact: {
		ah.RelationNameImposes,
		ah.RelationNameWields,
		ah.RelationNameSubjectTo,
		ah.RelationNameExposesTo,
		ah.RelationNameMitigatedBy,
	},
	taxonomy.DomainModeAssurance: {
		ah.RelationNameAssures,
		ah.RelationNameRegulates,
	},
}

func init() {
	domainModeAllowlists[taxonomy.DomainModeSupplyChain] = mergeAllowlists(
		allowlistFromTaxonomyWire(taxonomy.SupplyChainEdgeTypes),
		nil,
	)
	domainModeAllowlists[taxonomy.DomainModeServiceChain] = mergeAllowlists(
		allowlistFromTaxonomyWire(taxonomy.ServiceChainEdgeTypes),
		nil,
	)
	domainModeAllowlists[taxonomy.DomainModeGovernance] = mergeAllowlists(
		allowlistFromTaxonomyWire(taxonomy.GovernanceEdgeTypes),
		nil,
	)
	domainModeAllowlists[taxonomy.DomainModeAssurance] = mergeAllowlists(
		allowlistFromTaxonomyWire(taxonomy.AssuranceEdgeTypes),
		extraDomainModeRelations[taxonomy.DomainModeAssurance],
	)
	domainModeAllowlists[taxonomy.DomainModeMarket] = mergeAllowlists(
		allowlistFromTaxonomyWire(taxonomy.MarketEdgeTypes),
		marketAnchorRelations,
	)
	domainModeAllowlists[taxonomy.DomainModeStrategic] = mergeAllowlists(
		allowlistFromTaxonomyWire(taxonomy.StrategicEdgeTypes),
		extraDomainModeRelations[taxonomy.DomainModeStrategic],
	)
	domainModeAllowlists[taxonomy.DomainModePolicyImpact] = mergeAllowlists(
		allowlistFromTaxonomyWire(taxonomy.PolicyImpactEdgeTypes),
		extraDomainModeRelations[taxonomy.DomainModePolicyImpact],
	)
	domainModeAllowlists[taxonomy.DomainModeEcosystem] = mergeAllowlists(
		allowlistFromTaxonomyWire(taxonomy.EcosystemEdgeTypes),
		nil,
	)
}

func mergeAllowlists(base RelationAllowlist, extras []ah.RelationName) RelationAllowlist {
	seen := make(map[ah.RelationName]struct{}, len(base)+len(extras))
	out := make(RelationAllowlist, 0, len(base)+len(extras))
	for _, r := range base {
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		out = append(out, r)
	}
	for _, r := range extras {
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		out = append(out, r)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// EdgeAllowlistByDomainMode returns the canonical AH relation allowlist
// for the given domain mode. Returns nil for "general" / unknown modes
// (no restriction).
func EdgeAllowlistByDomainMode(mode string) RelationAllowlist {
	mode = strings.TrimSpace(strings.ToLower(mode))
	if mode == taxonomy.DomainModeGeneral || mode == "" {
		return nil
	}
	if list, ok := domainModeAllowlists[mode]; ok {
		out := make(RelationAllowlist, len(list))
		copy(out, list)
		return out
	}
	return nil
}

// EdgeAllowlistByDomainModes returns the deduplicated union of allowlists
// for the given modes. Returns nil (no restriction) when any mode is
// general or the input is empty.
func EdgeAllowlistByDomainModes(modes []string) RelationAllowlist {
	if len(modes) == 0 {
		return nil
	}
	seen := make(map[ah.RelationName]struct{})
	for _, mode := range modes {
		mode = strings.TrimSpace(strings.ToLower(mode))
		if mode == taxonomy.DomainModeGeneral {
			return nil
		}
		for _, rel := range EdgeAllowlistByDomainMode(mode) {
			seen[rel] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make(RelationAllowlist, 0, len(seen))
	for rel := range seen {
		out = append(out, rel)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// EdgeAllowlistStrings returns the lowercase AH relation names for the
// given domain modes, sorted. Returns nil when no restriction applies.
func EdgeAllowlistStrings(modes []string) []string {
	allow := EdgeAllowlistByDomainModes(modes)
	if allow == nil {
		return nil
	}
	out := make([]string, 0, len(allow))
	for _, rel := range allow {
		out = append(out, string(rel))
	}
	sort.Strings(out)
	return out
}

// EdgeDirectionValidationRulesForPrompt generates a prompt block listing
// source/target class constraints for every AH relation that has a closed
// endpoint catalog. Sourced from EdgeDirectionPolicies (which is itself
// derived from ah.RelationEndpointKinds).
func EdgeDirectionValidationRulesForPrompt() string {
	relations := AllRelationsList()
	var sb strings.Builder
	sb.WriteString("EDGE DIRECTION VALIDATION RULES (edges violating these will be REJECTED or REVERSED):\n")
	for _, name := range relations {
		rule := EdgeDirectionPolicies[ah.RelationName(name)]
		src := joinClasses(rule.ValidSourceClasses)
		tgt := joinClasses(rule.ValidTargetClasses)
		desc := strings.TrimSpace(rule.Description)
		if desc != "" {
			sb.WriteString(fmt.Sprintf("  - %s: source MUST be %s, target MUST be %s. %s\n", name, src, tgt, desc))
		} else {
			sb.WriteString(fmt.Sprintf("  - %s: source MUST be %s, target MUST be %s.\n", name, src, tgt))
		}
	}
	return sb.String()
}

func joinClasses(classes []ah.FragmentClass) string {
	if len(classes) == 0 {
		return "*"
	}
	parts := make([]string, 0, len(classes))
	for _, c := range classes {
		parts = append(parts, string(c))
	}
	return strings.Join(parts, " | ")
}
