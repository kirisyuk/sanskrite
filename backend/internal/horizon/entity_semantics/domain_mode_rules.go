package entity_semantics

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// DomainModeMissingInfoRules maps each domain mode to its relation_missing rules.
// Used when domain modes are active; these rules are additive to type-specific rules.
var DomainModeMissingInfoRules = map[string][]MissingInfoRule{
	taxonomy.DomainModeSupplyChain: {
		{Trigger: TriggerRelationMissing, RelationTypes: []string{"PRODUCES", "CONSUMES"}, DomainMode: taxonomy.DomainModeSupplyChain, QuestionTemplate: "products produced or consumed by %s"},
		{Trigger: TriggerRelationMissing, RelationTypes: []string{"LOCATED_IN", "OPERATES_IN"}, DomainMode: taxonomy.DomainModeSupplyChain, QuestionTemplate: "facility locations and operating regions for %s"},
		{Trigger: TriggerRelationMissing, RelationType: "EXTRACTS_FROM", DomainMode: taxonomy.DomainModeSupplyChain, QuestionTemplate: "extractive source links (deposits, mines) for %s"}, // only when path suggests extractive
		{Trigger: TriggerRelationCountBelow, MinCount: 3, DomainMode: taxonomy.DomainModeSupplyChain, QuestionTemplate: "child orgs, facilities, products, and org-recipients for %s"},
	},
	taxonomy.DomainModeGovernance: {
		// Seed: always ask upstream (who owns/controls) — critical for org/gov/fac.
		{Trigger: TriggerSeed, DomainMode: taxonomy.DomainModeGovernance, QuestionTemplate: "Who owns %s (shareholders, parent, UBO)? Who controls it? Who invests in it? Return OWNS/CONTROLS/INVESTS_IN with explicit sourceCanonicalPath when owner/controller is not current fragment. What subsidiaries does it own/control?"},
		{Trigger: TriggerRelationMissing, RelationType: "OWNS", DomainMode: taxonomy.DomainModeGovernance, QuestionTemplate: "ownership chain and ultimate parent entity for %s"},
		{Trigger: TriggerRelationMissing, RelationType: "SUBSIDIARY_OF", DomainMode: taxonomy.DomainModeGovernance, QuestionTemplate: "ownership chain and ultimate parent entity for %s"},
		{Trigger: TriggerRelationMissing, RelationType: "BOARD_MEMBER_OF", DomainMode: taxonomy.DomainModeGovernance, QuestionTemplate: "board composition and key executives for %s"},
		{Trigger: TriggerRelationMissing, RelationType: "CONTROLS", DomainMode: taxonomy.DomainModeGovernance, QuestionTemplate: "control structure and voting rights for %s"},
	},
	taxonomy.DomainModeMarket: {
		{Trigger: TriggerRelationMissing, RelationType: "PARTICIPATES_IN", DomainMode: taxonomy.DomainModeMarket, QuestionTemplate: "major market participants and market share for %s"},
		{Trigger: TriggerRelationMissing, RelationType: "HAS_MARKET_SEGMENT", DomainMode: taxonomy.DomainModeMarket, QuestionTemplate: "market hierarchy and segment breakdown for %s"},
	},
	taxonomy.DomainModeAssurance: {
		{Trigger: TriggerRelationMissing, RelationType: "ASSURES", DomainMode: taxonomy.DomainModeAssurance, QuestionTemplate: "audit and certification relationships for %s"},
	},
	taxonomy.DomainModePolicyImpact: {
		{Trigger: TriggerRelationMissing, RelationType: "REGULATES", DomainMode: taxonomy.DomainModePolicyImpact, QuestionTemplate: "regulatory relationships and oversight bodies for %s"},
		{Trigger: TriggerRelationMissing, RelationType: "IMPOSES_CONDITION", DomainMode: taxonomy.DomainModePolicyImpact, QuestionTemplate: "policy impact channels and conditions for %s"},
	},
}

// DomainModeRulesForModes returns MissingInfoRules for the given domain modes.
// Deduplicates by relation type within each mode (supply chain has PRODUCES+CONSUMES as one logical check).
func DomainModeRulesForModes(domainModes []string) []MissingInfoRule {
	domainModes = taxonomy.NormalizeDomainModes(domainModes)
	seen := make(map[string]struct{})
	var out []MissingInfoRule
	for _, m := range domainModes {
		m = strings.TrimSpace(strings.ToLower(m))
		if m == "" || m == taxonomy.DomainModeGeneral {
			continue
		}
		rules, ok := DomainModeMissingInfoRules[m]
		if !ok {
			continue
		}
		for _, r := range rules {
			key := r.DomainMode + ":" + r.Trigger + ":" + r.RelationType
			if len(r.RelationTypes) > 0 {
				key = r.DomainMode + ":" + r.Trigger + ":" + strings.Join(r.RelationTypes, ",")
			}
			if r.Trigger == TriggerRelationCountBelow {
				key = r.DomainMode + ":" + r.Trigger
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, r)
		}
	}
	return out
}
