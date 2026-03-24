package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

const criticValidationOutputSchema = `STRICT OUTPUT SCHEMA (types are mandatory):
{
  "hasConflicts": boolean,
  "conflicts": [
    {
      "field": "string",
      "extractedValue": any,
      "sourceValue": any,
      "reason": "string",
      "severity": "low" | "medium" | "high"
    }
  ],
  "confidence": number,           // float 0..1, MUST be numeric (not string)
  "isValid": boolean,
  "missingInfo": ["string"]       // array of strings only
}
Rules:
- missingInfo items MUST be plain strings, not objects.
- Return exactly one JSON object and no extra prose.`

func (a *CriticAgent) runValidationPrompt(ctx context.Context, prompt string) (*ValidationResult, error) {
	if a == nil || a.executor == nil {
		return nil, nil
	}
	verifier := ai.NewSimpleJSONVerifier()
	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentGeneration,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       7,
			MinContextWindow:         12000,
			MaxOutputSize:            4096,
		},
		Verifier: verifier,
	}
	result, err := a.executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("failed to validate data: %w", err)
	}
	jsonData, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}
	validationResult, err := parseValidationResultJSON(jsonData)
	if err != nil {
		return nil, err
	}
	return validationResult, nil
}

func (a *CriticAgent) buildCoreValidationPrompt(input CriticValidatorInput) string {
	fragmentJSON, _ := json.MarshalIndent(input.Fragment, "", "  ")
	relationsJSON, _ := json.MarshalIndent(input.Relations, "", "  ")
	graphBlock := ""
	if strings.TrimSpace(input.GraphContext) != "" {
		graphBlock = "GRAPH CONTEXT:\n" + strings.TrimSpace(input.GraphContext) + "\n\n"
	}
	return fmt.Sprintf(`You are a Critic validator for core entity integrity.

%s
EXTRACTED FRAGMENT:
%s

EXTRACTED RELATIONS:
%s

Validate:
1. canonicalPath correctness and basic type-path consistency;
2. obvious conflicts between fragment fields and source context;
3. structural completeness at broad level only;
4. missing evidence questions needed for next non-deep pass.

Return JSON:
{
  "hasConflicts": boolean,
  "conflicts": [{"field":"string","extractedValue":"any","sourceValue":"any","reason":"string","severity":"low|medium|high"}],
  "confidence": 0.0,
  "isValid": boolean,
  "missingInfo": ["string"]
}

%s

Return ONLY valid JSON.`, graphBlock, string(fragmentJSON), string(relationsJSON), criticValidationOutputSchema)
}

func (a *CriticAgent) buildSupplyChainValidationPrompt(input CriticValidatorInput) string {
	fragmentJSON, _ := json.MarshalIndent(input.Fragment, "", "  ")
	relationsJSON, _ := json.MarshalIndent(input.Relations, "", "  ")
	chainsJSON, _ := json.MarshalIndent(input.SupplyChains, "", "  ")
	scopeJSON, _ := json.MarshalIndent(input.WorkingProductScope, "", "  ")
	graphBlock := ""
	if strings.TrimSpace(input.GraphContext) != "" {
		graphBlock = "GRAPH CONTEXT:\n" + strings.TrimSpace(input.GraphContext) + "\n\n"
	}
	return fmt.Sprintf(`You are a Critic validator for SUPPLY CHAIN mode.

Allowed relation types: %s.
Allowed actor types: %s.
Canonical path rules:
%s

%s
FRAGMENT:
%s

RELATIONS:
%s

WORKING PRODUCT SCOPE:
%s

SUPPLY CHAINS:
%s

Validate:
1. chain continuity and direction consistency;
2. relation taxonomy compliance (type-direction-entity compatibility) and canonical path shape;
3. DEPENDS_ON vs SUPPLIES_TO semantics:
   - DEPENDS_ON is consumer-side dependency;
   - SUPPLIES_TO is supplier-side flow;
   - avoid contradictory duplicates unless source evidence explicitly supports both directions;
4. transmission/logistics qualifiers quality for supply dependencies when present (facet, throughput_share, capacity_constraint, reroute_* best-effort);
5. supplyChains node alternation actor→product and facility transformation hops;
6. org→fac→product→fac→org tree coverage (child orgs, facilities, products, org-recipients);
7. qualifier quality (confidence, source hints, transport qualifiers when applicable);
8. productCandidates must be genuinely new scope additions, not duplicates or aliases of products already in workingProductScope or nodes[];
9. carrierProductObservations must stay reusable actor×product facts, linkObservations must stay customer/destination-specific, routeObservations must stay transport/path-specific;
10. do not smear multi-product truth into one SupplyLink. Each link and path leg must stay one-product.
11. when a chain has concrete carrier + product evidence and reusable baseline metrics are evidenced or strongly implied, missing carrierProductObservations must be reported as a capability-evidence gap.

%s

Return ONLY valid JSON.`, taxonomy.SupplyChainEdgeTypesString(), taxonomy.EntityActorTypesForDomainMode(taxonomy.DomainModeSupplyChain), taxonomy.ExtendedCanonicalPathRulesPrompt(), graphBlock, string(fragmentJSON), string(relationsJSON), string(scopeJSON), string(chainsJSON), criticValidationOutputSchema)
}

func (a *CriticAgent) buildGovernanceValidationPrompt(input CriticValidatorInput) string {
	fragmentJSON, _ := json.MarshalIndent(input.Fragment, "", "  ")
	relationsJSON, _ := json.MarshalIndent(input.Relations, "", "  ")
	return fmt.Sprintf(`You are a Critic validator for GOVERNANCE mode.

Allowed relation types: %s.
Allowed actor types: %s.

FRAGMENT:
%s

RELATIONS:
%s

	Validate ownership and control coherence:
1. parent/subsidiary direction consistency;
2. beneficial-owner completeness: call out when UBO, nominee/trustee vehicle, or indirect holding layer appears missing;
3. voting-rights vs equity divergence: call out when OWNS exists but voting/control evidence is thin or contradictory;
4. board and executive coverage at practical broad level;
5. community/dynasty lineage completeness when COMMUNITY nodes materially explain control;
6. government hierarchy consistency when /gov/ entities appear;
7. assurance linkage quality when auditors/certifiers are present (ASSURES + assurance_type);
8. missing information that should be researched before deep dive.

%s

Return ONLY valid JSON.`, relationTypesPromptForDomainMode(taxonomy.DomainModeGovernance), taxonomy.EntityActorTypesForDomainMode(taxonomy.DomainModeGovernance), string(fragmentJSON), string(relationsJSON), criticValidationOutputSchema)
}

func (a *CriticAgent) buildAssuranceValidationPrompt(input CriticValidatorInput) string {
	fragmentJSON, _ := json.MarshalIndent(input.Fragment, "", "  ")
	relationsJSON, _ := json.MarshalIndent(input.Relations, "", "  ")
	return fmt.Sprintf(`You are a Critic validator for ASSURANCE mode (Third Line: audit, certification, regulatory oversight).

Allowed relation types: %s (for ASSURES keep qualifiers.assurance_type: audit|certification|regulatory|inspection).
Allowed actor types: %s.

FRAGMENT:
%s

RELATIONS:
%s

Validate:
1. ASSURES direction: assurer (source) → assuree (target). Do not use AUDITED_BY legacy inverse.
2. external audit coverage for significant orgs/facilities;
3. certification and regulatory oversight when relevant to sector;
4. missing assurance information that should be researched.

%s

Return ONLY valid JSON.`, relationTypesPromptForDomainMode(taxonomy.DomainModeAssurance), taxonomy.EntityActorTypesForDomainMode(taxonomy.DomainModeAssurance), string(fragmentJSON), string(relationsJSON), criticValidationOutputSchema)
}

func (a *CriticAgent) buildMarketValidationPrompt(input CriticValidatorInput) string {
	fragmentJSON, _ := json.MarshalIndent(input.Fragment, "", "  ")
	relationsJSON, _ := json.MarshalIndent(input.Relations, "", "  ")
	graphBlock := ""
	if strings.TrimSpace(input.GraphContext) != "" {
		graphBlock = "GRAPH CONTEXT:\n" + strings.TrimSpace(input.GraphContext) + "\n\n"
	}
	return fmt.Sprintf(`You are a Critic validator for MARKET mode.

Allowed relation types: %s.
Allowed actor types: %s.

%sFRAGMENT:
%s

RELATIONS:
%s

Validate:
1. market hierarchy placement and immediate parent/children coverage;
2. major participant coverage and market share plausibility when available;
3. required market metrics presence at broad level (tam, cagr, concentration).

%s

Return ONLY valid JSON.`, relationTypesPromptForDomainMode(taxonomy.DomainModeMarket), taxonomy.EntityActorTypesForDomainMode(taxonomy.DomainModeMarket), graphBlock, string(fragmentJSON), string(relationsJSON), criticValidationOutputSchema)
}

// buildComposedValidationPrompt merges Core + all non-strategic domain validators into a single
// LLM prompt. This eliminates N sequential LLM calls (one per validator) and replaces them with
// a single call that covers all active domain modes. Strategic mode is excluded because it uses
// a structurally different prompt (ValidateStrategicOverlay).
func (a *CriticAgent) buildComposedValidationPrompt(input CriticValidatorInput, domainModes []string) string {
	fragmentJSON, _ := json.MarshalIndent(input.Fragment, "", "  ")
	relationsJSON, _ := json.MarshalIndent(input.Relations, "", "  ")

	graphBlock := ""
	if strings.TrimSpace(input.GraphContext) != "" {
		graphBlock = "GRAPH CONTEXT:\n" + strings.TrimSpace(input.GraphContext) + "\n\n"
	}

	var sections []string

	// Core validation (always included)
	sections = append(sections, `== CORE VALIDATION ==
Validate:
1. canonicalPath correctness and basic type-path consistency;
2. obvious conflicts between fragment fields and source context;
3. structural completeness at broad level only;
4. missing evidence questions needed for next non-deep pass.`)

	for _, m := range domainModes {
		m = strings.TrimSpace(strings.ToLower(m))
		switch m {
		case taxonomy.DomainModeSupplyChain:
			chainsJSON, _ := json.MarshalIndent(input.SupplyChains, "", "  ")
			scopeJSON, _ := json.MarshalIndent(input.WorkingProductScope, "", "  ")
			sections = append(sections, fmt.Sprintf(`== SUPPLY CHAIN VALIDATION ==
Allowed relation types: %s.
Allowed actor types: %s.
Canonical path rules:
%s

WORKING PRODUCT SCOPE:
%s

SUPPLY CHAINS:
%s

Validate:
1. chain continuity and direction consistency;
2. relation taxonomy compliance (type-direction-entity compatibility) and canonical path shape;
3. DEPENDS_ON vs SUPPLIES_TO semantics;
4. transmission/logistics qualifiers quality when present;
5. supplyChains node alternation actor→product and facility transformation hops;
6. org→fac→product→fac→org tree coverage (child orgs, facilities, products, org-recipients);
7. qualifier quality (confidence, source hints, transport qualifiers when applicable);
8. productCandidates must be genuinely new scope additions, not duplicates or aliases of workingProductScope or nodes[];
9. carrierProductObservations vs linkObservations vs routeObservations must remain correctly scoped;
10. do not merge multi-product truth into one SupplyLink or one path leg;
11. when reusable actor×product metrics are evidenced, missing carrierProductObservations is a gap and should be called out explicitly.`,
				taxonomy.SupplyChainEdgeTypesString(),
				taxonomy.EntityActorTypesForDomainMode(taxonomy.DomainModeSupplyChain),
				taxonomy.ExtendedCanonicalPathRulesPrompt(),
				string(scopeJSON),
				string(chainsJSON)))

		case taxonomy.DomainModeGovernance:
			sections = append(sections, fmt.Sprintf(`== GOVERNANCE VALIDATION ==
Allowed relation types: %s.
Allowed actor types: %s.

Validate ownership and control coherence:
1. parent/subsidiary direction consistency;
2. beneficial-owner completeness: call out when UBO, nominee/trustee vehicle, or indirect holding layer appears missing;
3. voting-rights vs equity divergence: call out when OWNS exists but voting/control evidence is thin or contradictory;
4. board and executive coverage at practical broad level;
5. community/dynasty lineage completeness when COMMUNITY nodes materially explain control;
6. government hierarchy consistency when /gov/ entities appear;
7. assurance linkage quality when auditors/certifiers are present;
8. missing information that should be researched before deep dive.`,
				relationTypesPromptForDomainMode(taxonomy.DomainModeGovernance),
				taxonomy.EntityActorTypesForDomainMode(taxonomy.DomainModeGovernance)))

		case taxonomy.DomainModeAssurance:
			sections = append(sections, fmt.Sprintf(`== ASSURANCE VALIDATION ==
Allowed relation types: %s (for ASSURES keep qualifiers.assurance_type: audit|certification|regulatory|inspection).
Allowed actor types: %s.

Validate:
1. ASSURES direction: assurer (source) → assuree (target);
2. external audit coverage for significant orgs/facilities;
3. certification and regulatory oversight when relevant to sector;
4. missing assurance information that should be researched.`,
				relationTypesPromptForDomainMode(taxonomy.DomainModeAssurance),
				taxonomy.EntityActorTypesForDomainMode(taxonomy.DomainModeAssurance)))

		case taxonomy.DomainModeMarket:
			sections = append(sections, fmt.Sprintf(`== MARKET VALIDATION ==
Allowed relation types: %s.
Allowed actor types: %s.

Validate:
1. market hierarchy placement and immediate parent/children coverage;
2. major participant coverage and market share plausibility when available;
3. required market metrics presence at broad level (tam, cagr, concentration).`,
				relationTypesPromptForDomainMode(taxonomy.DomainModeMarket),
				taxonomy.EntityActorTypesForDomainMode(taxonomy.DomainModeMarket)))

		case taxonomy.DomainModePolicyImpact:
			sections = append(sections, fmt.Sprintf(`== POLICY IMPACT VALIDATION ==
Allowed relation types: %s.
Allowed actor types: %s.
Canonical path rules:
%s

Validate:
1. policy channel consistency: gatekeeper/regulator/admin/mechanism links are directionally coherent and use the correct semantic source;
2. directed role contract: core policy-impact edges should resolve to canonical role pairs
   gatekeeper->decision_point, mechanism_user->mechanism, controller->access_subject, imposer->constrained_entity,
   mechanism->applicable_entity, administrator->administered_entity, scope_owner->geo_scope, impact_source->affected_entity;
3. impact graph quality: affected segments are explicit and connected;
4. geo/community/media/program completeness: flag when a policy graph lacks explicit geo scope, downstream community/media transmission, or program execution layer where those are materially relevant;
5. relation taxonomy compliance;
6. scenario modeling readiness (quantitative qualifiers on edges);
7. observability completeness;
8. five-layer coverage check: Policy Source, Mechanism, Scope/Exposure, Transmission/Response, Observability.`,
				relationTypesPromptForDomainMode(taxonomy.DomainModePolicyImpact),
				taxonomy.EntityActorTypesForDomainMode(taxonomy.DomainModePolicyImpact),
				taxonomy.ExtendedCanonicalPathRulesPrompt()))

		case taxonomy.DomainModeEcosystem:
			sections = append(sections, fmt.Sprintf(`== ECOSYSTEM VALIDATION ==
Allowed relation types: %s.
Allowed actor types: %s.

Validate:
1. ecosystem actor coverage: all significant actors are represented (organizations, gov bodies, investors, communities);
2. relationship diversity: not just one relation type dominates; ownership, supply, competition, regulation, partnership should all be present where applicable;
3. directional consistency: OWNS goes owner->owned, SUBSIDIARY_OF goes child->parent, REGULATES goes regulator->regulated (same jurisdiction only);
4. REGULATES jurisdiction check: if source is a government body from country X and target is a foreign entity from country Y, REGULATES is incorrect — flag as missingInfo and suggest CONTROLS_ACCESS or IMPOSES_CONDITION instead;
5. power dynamics coverage: key influencers, gatekeepers, and bottleneck nodes are identified;
6. geographic and sectoral coherence: actors and relations are consistent with the ecosystem's geography and industry.`,
				relationTypesPromptForDomainMode(taxonomy.DomainModeEcosystem),
				taxonomy.EntityActorTypesForDomainMode(taxonomy.DomainModeEcosystem)))
		}
	}

	return fmt.Sprintf(`You are a Critic validator. Validate the entity across multiple dimensions in a single pass.

%s%s

EXTRACTED FRAGMENT:
%s

EXTRACTED RELATIONS:
%s

%s

Return ONLY valid JSON.`, graphBlock, strings.Join(sections, "\n\n"), string(fragmentJSON), string(relationsJSON), criticValidationOutputSchema)
}

func (a *CriticAgent) buildPolicyImpactValidationPrompt(input CriticValidatorInput) string {
	fragmentJSON, _ := json.MarshalIndent(input.Fragment, "", "  ")
	relationsJSON, _ := json.MarshalIndent(input.Relations, "", "  ")
	graphBlock := ""
	if strings.TrimSpace(input.GraphContext) != "" {
		graphBlock = "GRAPH CONTEXT:\n" + strings.TrimSpace(input.GraphContext) + "\n\n"
	}
	return fmt.Sprintf(`You are a Critic validator for POLICY IMPACT mode.

Allowed relation types: %s.
Allowed actor types: %s.
Canonical path rules:
%s

%sFRAGMENT:
%s

RELATIONS:
%s

Validate:
1. policy channel consistency: gatekeeper/regulator/admin/mechanism links are directionally coherent and use the correct semantic source;
2. DIRECTED ROLE CONTRACT (critical):
   a. GATEKEEPS_DECISION must read gatekeeper -> decision_point;
   b. USES_MECHANISM must read mechanism_user -> mechanism;
   c. CONTROLS_ACCESS must read controller -> access_subject;
   d. IMPOSES_CONDITION must read imposer -> constrained_entity;
   e. APPLIES_TO must read mechanism -> applicable_entity;
   f. ADMINISTERS must read administrator -> administered_entity;
   g. IN_GEO_SCOPE must read scope_owner -> geo_scope;
   h. AFFECTS must read impact_source -> affected_entity;
   If the extracted relation implies one of these but reverses the semantic source, mark invalid and add a specific missingInfo question.
3. impact graph quality: affected segments are explicit and connected through interpretable channels;
4. geo/community/media/program transmission completeness:
   a. is there an explicit geo scope when jurisdiction or local applicability matters?
   b. are communities or programs connected when they materially transmit adoption/compliance/evasion?
   c. if a community or org uses a media channel to transmit the effect, is the media endpoint represented?
5. relation taxonomy compliance (type-direction-entity compatibility);
6. SCENARIO MODELING READINESS (critical for policy-impact):
   a. do REGULATES/IMPOSES_CONDITION edges carry quantitative qualifiers (rate_from, rate_to, effective_date)?
   b. do IN_GEO_SCOPE edges carry scope basis when inferable?
   c. do exposure edges carry pass_through_estimate, elasticity_hint, or sector_sensitivity?
   d. do AFFECTS/INFLUENCES edges specify channel_type, lag_quarters, observability?
   e. flag edges lacking quantitative qualifiers as missingInfo with specific questions (e.g., "What is the estimated pass-through rate for VAT increase on food retail?");
7. OBSERVABILITY COMPLETENESS: are there edges or segments with no plausible measurement source?
   Flag segments where effect measurement is unclear as missingInfo;
8. five-layer coverage check: Policy Source, Mechanism, Scope/Exposure, Transmission/Response, Observability.
   Report which layers are missing or thin for the current fragment.

%s

Return ONLY valid JSON.`, relationTypesPromptForDomainMode(taxonomy.DomainModePolicyImpact), taxonomy.EntityActorTypesForDomainMode(taxonomy.DomainModePolicyImpact), taxonomy.ExtendedCanonicalPathRulesPrompt(), graphBlock, string(fragmentJSON), string(relationsJSON), criticValidationOutputSchema)
}

func (a *CriticAgent) buildEcosystemValidationPrompt(input CriticValidatorInput) string {
	fragmentJSON, _ := json.MarshalIndent(input.Fragment, "", "  ")
	relationsJSON, _ := json.MarshalIndent(input.Relations, "", "  ")
	graphBlock := ""
	if strings.TrimSpace(input.GraphContext) != "" {
		graphBlock = "GRAPH CONTEXT:\n" + strings.TrimSpace(input.GraphContext) + "\n\n"
	}
	return fmt.Sprintf(`You are a Critic validator for ECOSYSTEM mode.

Allowed relation types: %s.
Allowed actor types: %s.

%sFRAGMENT:
%s

RELATIONS:
%s

Validate:
1. ecosystem actor coverage: all significant actors are represented (organizations, gov bodies, investors, communities);
2. relationship diversity: not just one relation type dominates; ownership, supply, competition, regulation, partnership should all be present where applicable;
3. directional consistency: OWNS goes owner->owned, SUBSIDIARY_OF goes child->parent, REGULATES goes regulator->regulated (same jurisdiction only);
4. REGULATES jurisdiction check: if source is a government body from country X and target is a foreign entity from country Y, REGULATES is incorrect — flag and suggest CONTROLS_ACCESS or IMPOSES_CONDITION;
5. power dynamics coverage: key influencers, gatekeepers, and bottleneck nodes are identified;
6. geographic and sectoral coherence: actors and relations are consistent with the ecosystem geography and industry.

%s

Return ONLY valid JSON.`, relationTypesPromptForDomainMode(taxonomy.DomainModeEcosystem), taxonomy.EntityActorTypesForDomainMode(taxonomy.DomainModeEcosystem), graphBlock, string(fragmentJSON), string(relationsJSON), criticValidationOutputSchema)
}
