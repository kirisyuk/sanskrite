package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// geoAndSectorContext extracts geo and sector from fragment for prompt adaptation.
// Used to instruct LLM to adapt extraction to regional/industry-specific supply chain and governance patterns.
func geoAndSectorContext(frag *models.HorizonFragment) string {
	if frag == nil {
		return ""
	}
	var geo, sector []string
	// Geo: from canonical path (org/gov/market 2nd segment), homeLocation, or marketSegments
	cp := strings.TrimSpace(frag.CanonicalPath)
	if cp != "" {
		parts := strings.Split(strings.Trim(cp, "/"), "/")
		if len(parts) >= 2 {
			g := strings.ToLower(strings.TrimSpace(parts[1]))
			if g != "" && g != "na" && len(g) >= 2 {
				geo = append(geo, g)
			}
		}
	}
	if frag.HomeLocation != nil && frag.HomeLocation.ISO3166_1 != nil {
		g := strings.ToLower(strings.TrimSpace(*frag.HomeLocation.ISO3166_1))
		if g != "" && len(g) == 2 {
			geo = append(geo, g)
		}
	}
	for _, seg := range frag.MarketSegments {
		parts := strings.Split(strings.Trim(seg, "/"), "/")
		if len(parts) >= 3 {
			g := strings.ToLower(strings.TrimSpace(parts[2]))
			if g != "" && g != "global" {
				geo = append(geo, g)
			}
		}
	}
	// Sector: from SectorPath, EntityPath, or marketSegments (domain/vertical)
	if sp := strings.TrimSpace(frag.SectorPath); sp != "" {
		sector = append(sector, sp)
	}
	if ep := strings.TrimSpace(frag.EntityPath); ep != "" {
		sector = append(sector, ep)
	}
	for _, seg := range frag.MarketSegments {
		parts := strings.Split(strings.Trim(seg, "/"), "/")
		if len(parts) >= 2 {
			s := strings.Join(parts[:2], "/")
			if s != "" {
				sector = append(sector, s)
			}
		}
	}
	if len(geo) == 0 && len(sector) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("ENTITY CONTEXT (adapt extraction to this company and market):\n")
	if len(geo) > 0 {
		seen := make(map[string]struct{})
		var uniq []string
		for _, g := range geo {
			if _, ok := seen[g]; !ok {
				seen[g] = struct{}{}
				uniq = append(uniq, g)
			}
		}
		sb.WriteString(fmt.Sprintf("- Geo: %s\n", strings.Join(uniq, ", ")))
	}
	if len(sector) > 0 {
		seen := make(map[string]struct{})
		var uniq []string
		for _, s := range sector {
			if _, ok := seen[s]; !ok {
				seen[s] = struct{}{}
				uniq = append(uniq, s)
			}
		}
		sb.WriteString(fmt.Sprintf("- Sector: %s\n", strings.Join(uniq, ", ")))
	}
	sb.WriteString(`- ADAPT: Use your knowledge of the company, region, and industry to infer supply chain and governance patterns.
  Examples: Chinese EV/automotive (e.g., BYD) often use vertical integration; European OEMs often rely on partnership networks.
  When in doubt, infer from entity name, sector, and geo; output qualifiers.integration_type when the relationship nature is clear.
`)
	return sb.String()
}

func unitPhaseResearchGuardrailBlock(phase models.UnitPipelinePhase) string {
	switch phase {
	case models.UnitPipelinePhaseP5:
		return `UNIT PIPELINE PHASE: P5 (Unit Dive Execution)
- This run is an explicit unit dive. You may expand legal entities, facilities, and multi-hop chains within budget.
- Keep alignment to selected supplier_unit_ref and customer_unit_ref when present in context.`
	default:
		return `UNIT PIPELINE PHASE: P0-P3 (Broad/Semi-depth)
- Do NOT perform full legal-entity decomposition or SKU/contract-level reconstruction.
- Keep output at macro + unit contours with broad product/service buckets and clear qualifiers.
- If deep detail is needed, add targeted missingInfo questions instead of deep expansion.`
	}
}

func unitPhaseSupplyDepthGuidance(phase models.UnitPipelinePhase) string {
	if phase == models.UnitPipelinePhaseP5 {
		return "Depth target: 2+ tiers upstream/downstream when evidence is available and within dive budget."
	}
	return "Depth target: broad/semi-depth. Prioritize direct links and, at most, one additional tier per side when confidence is strong."
}

// SupplyChainResearchStrategy enriches for supply chain only (PRODUCES, CONSUMES, SUPPLIES_TO, etc.).
type SupplyChainResearchStrategy struct {
	agent *ResearchAgent
}

func (s *SupplyChainResearchStrategy) DomainMode() string { return taxonomy.DomainModeSupplyChain }

func (s *SupplyChainResearchStrategy) Enrich(ctx context.Context, input ResearchStrategyInput) (*ResearchStrategyOutput, error) {
	if s.agent == nil || s.agent.executor == nil {
		return nil, nil
	}
	prompt := s.agent.buildSupplyChainEnrichmentPrompt(input)
	out, err := s.agent.runResearchPrompt(ctx, prompt)
	if err != nil || out == nil {
		return nil, err
	}
	return out, nil
}

// GovernanceResearchStrategy enriches for ownership, board, key persons only.
type GovernanceResearchStrategy struct {
	agent *ResearchAgent
}

func (s *GovernanceResearchStrategy) DomainMode() string { return taxonomy.DomainModeGovernance }

func (s *GovernanceResearchStrategy) Enrich(ctx context.Context, input ResearchStrategyInput) (*ResearchStrategyOutput, error) {
	if s.agent == nil || s.agent.executor == nil {
		return nil, nil
	}
	prompt := s.agent.buildGovernanceEnrichmentPrompt(input)
	out, err := s.agent.runResearchPrompt(ctx, prompt)
	if err != nil || out == nil {
		return nil, err
	}
	return out, nil
}

// MarketResearchStrategy enriches for market participation only (PARTICIPATES_IN, HAS_SUBMARKET).
type MarketResearchStrategy struct {
	agent *ResearchAgent
}

func (s *MarketResearchStrategy) DomainMode() string { return taxonomy.DomainModeMarket }

func (s *MarketResearchStrategy) Enrich(ctx context.Context, input ResearchStrategyInput) (*ResearchStrategyOutput, error) {
	if s.agent == nil || s.agent.executor == nil {
		return nil, nil
	}
	prompt := s.agent.buildMarketEnrichmentPrompt(input)
	out, err := s.agent.runResearchPrompt(ctx, prompt)
	if err != nil || out == nil {
		return nil, err
	}
	return out, nil
}

// StrategicResearchStrategy enriches for strategic layer (CONTROLS_ACCESS, USES_MECHANISM, etc.).
type StrategicResearchStrategy struct {
	agent *ResearchAgent
}

func (s *StrategicResearchStrategy) DomainMode() string { return taxonomy.DomainModeStrategic }

func (s *StrategicResearchStrategy) Enrich(ctx context.Context, input ResearchStrategyInput) (*ResearchStrategyOutput, error) {
	if s.agent == nil || s.agent.executor == nil {
		return nil, nil
	}
	prompt := s.agent.buildStrategicEnrichmentPrompt(input)
	out, err := s.agent.runResearchPrompt(ctx, prompt)
	if err != nil || out == nil {
		return nil, err
	}
	return out, nil
}

// runResearchPrompt runs a research prompt and parses result into ResearchStrategyOutput.
func (a *ResearchAgent) runResearchPrompt(ctx context.Context, prompt string) (*ResearchStrategyOutput, error) {
	if a == nil || a.executor == nil {
		return nil, nil
	}
	verifier := ai.NewSimpleJSONVerifier()
	task := &ai.AITask{
		TaskType:     ai.TaskTypeKnowledgeSynthesis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability:        true,
			SearchGroundingCapability:       true,
			InternalDataGroundingCapability: true,
			MinCapabilityScore:              7,
			MinContextWindow:                16000,
			MaxOutputSize:                   16000,
		},
		Verifier: verifier,
	}
	result, _, err := a.executor.ExecuteStructuredTaskFull(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("research prompt: %w", err)
	}
	jsonData, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	var res researchResult
	if err := json.Unmarshal(jsonData, &res); err != nil {
		return nil, fmt.Errorf("unmarshal research result: %w", err)
	}
	out := &ResearchStrategyOutput{
		Relations:    res.Relations,
		SupplyChains: res.SupplyChains,
	}
	if res.Fragment != nil {
		fragBytes, _ := json.Marshal(res.Fragment)
		var partial models.HorizonFragment
		if err := json.Unmarshal(fragBytes, &partial); err == nil {
			out.FragmentPartial = &partial
		}
	}
	normalizeDiscoveredRelationsNames(out.Relations)
	return out, nil
}

// buildSupplyChainEnrichmentPrompt builds a prompt for supply-chain enrichment (value chains, logistics, extractive).
func (a *ResearchAgent) buildSupplyChainEnrichmentPrompt(input ResearchStrategyInput) string {
	frag := input.Fragment
	if frag == nil {
		frag = &models.HorizonFragment{}
	}
	fragmentJSON, _ := json.MarshalIndent(frag, "", "  ")
	questions := strings.Join(input.MissingInfo, "\n- ")
	if questions == "" {
		questions = "Identify upstream suppliers and downstream customers; products produced or consumed; facility locations; extractive links to deposits."
	}
	geoContext := geoAndSectorContext(frag)
	phaseGuardrails := unitPhaseResearchGuardrailBlock(input.UnitPipelinePhase)
	return fmt.Sprintf(`You are a research agent. Enrich the entity for SUPPLY CHAIN context. Build coherent chains with phase-aware depth.

Use ONLY these relation types (canonical): DEPENDS_ON, PRODUCES, CONSUMES, SUPPLIES_TO, TRANSFORMS_INTO, EXTRACTS_FROM, LOCATED_IN, OPERATES_IN.
Do NOT use RELATED_TO, AFFILIATED_WITH, PARTNERS_WITH, OWNS, COMPETES_WITH, or other non-supply-chain types.

ENTITY TYPES in supply chain: %s. Use correct targetType for each relation.

CANONICAL PATH RULES (targetCanonicalPath MUST follow; supplyChains nodes too):
%s

%s
%s

WHAT TO FIND (prioritize completeness):
1. Upstream: direct suppliers, then one next tier when confidence allows. Use DEPENDS_ON (consumer side) or SUPPLIES_TO (supplier side) consistently. Use PRODUCES/CONSUMES (org, facility, program→product). For raw materials: trace to extraction/mining when relevant.
2. Downstream: direct customers, then one next tier when confidence allows. Include distribution channels and end users when known.
3. Products: PRODUCES (org/facility/program→product), CONSUMES (org/facility/program→product). Value chain: PRODUCT TRANSFORMS_INTO PRODUCT (upstream→downstream). Include capacity/volume in metrics when you find figures (production_capacity, annual_volume).
4. Facilities & geography: LOCATED_IN (org/facility/person→GEO_REGION), OPERATES_IN (org/facility→region/deposit). For each facility: what it produces, capacity if known.
5. Extractive (mining, O&G): EXTRACTS_FROM (FACILITY→DEPOSIT). Add reserves/production to fragment.state (/metric/reserves_volume, /metric/production_annual).
6. Supply chains: Build end-to-end chains with detail appropriate to phase. Output supplyChains with nodes [{canonicalPath, name, type, role?}].
   - CRITICAL: Include FACILITY nodes where transformation happens. Pattern: product → facility → product (e.g. lithium-ore → refinery → lithium-carbonate).
   - For extractive: subsidiary → facility → deposit → product (e.g. KMG → mine → /deposit/lithium/salares → lithium-ore).
   - Nodes MUST alternate: actor → product → actor → product. Use /fac/{org}/{slug} for transformation sites.
   - transportVia (optional): array of facility paths the product passes through (ports, terminals, pipelines, warehouses). Same canonicalization rules.
   - For SUPPLIES_TO relations: add qualifiers.transport_via when transport route is known.
   - Return entities (orgs, facilities, products, deposits, communities) that should be explored further (child jobs will recurse).

INTEGRATION_TYPE qualifier (for SUPPLIES_TO): When the relationship nature is clear, add qualifiers.integration_type:
- "vertical": supplier is owned/controlled by the same parent (e.g., BYD subsidiaries supplying BYD)
- "partnership": long-term partnership, JV, or strategic alliance (e.g., European OEM–tier-1 networks)
- "arm's-length": spot or short-term commercial relationship

CURRENT FRAGMENT:
%s

QUESTIONS TO ANSWER (from Critic; prioritize these):
- %s

Return a JSON object:
{
  "fragment": { updated fields if found: homeLocation, state (e.g. /metric/reserves_volume, /metric/production_annual for extractive) },
  "relations": [ { "targetName", "targetType", "targetCanonicalPath", "relationType", "reason", "confidence", "qualifiers": { "integration_type"?: "vertical|partnership|arm's-length", "transport_via"?: ["/fac/port/...", "/fac/terminal/..."] } } ],
  "supplyChains": [ { "nodes": [...], "confidence", "reason", "transportVia"?: ["/fac/port/...", "/fac/terminal/..."] } ]
}
Nodes MUST alternate actor→product. Include FACILITY where transformation happens (product→facility→product).
For SUPPLIES_TO: add qualifiers.transport_via when transport route known. transportVia in chains = facilities product passes through.

%s

Return ONLY valid JSON. No markdown.`, taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeSupplyChain), taxonomy.ExtendedCanonicalPathRulesPrompt(), geoContext, phaseGuardrails, string(fragmentJSON), questions, unitPhaseSupplyDepthGuidance(input.UnitPipelinePhase))
}

// buildGovernanceEnrichmentPrompt builds a prompt for ownership, board, key persons, gov hierarchy (canonical types).
func (a *ResearchAgent) buildGovernanceEnrichmentPrompt(input ResearchStrategyInput) string {
	frag := input.Fragment
	if frag == nil {
		frag = &models.HorizonFragment{}
	}
	fragmentJSON, _ := json.MarshalIndent(frag, "", "  ")
	questions := strings.Join(input.MissingInfo, "\n- ")
	if questions == "" {
		questions = "Identify owners, subsidiaries, board members, key executives; ownership percentages; government hierarchy (ADMINISTERS)."
	}
	geoContext := geoAndSectorContext(frag)
	phaseGuardrails := unitPhaseResearchGuardrailBlock(input.UnitPipelinePhase)
	return fmt.Sprintf(`You are a research agent. Enrich the entity for GOVERNANCE context. Aim for COMPLETE ownership and control structure:

ENTITY TYPES in governance: %s. Use correct targetType for each relation.

CANONICAL PATH RULES (targetCanonicalPath MUST follow):
%s

%s
%s

Use ONLY these relation types (canonical): OWNS, SUBSIDIARY_OF, CONTROLS, INVESTS_IN, BOARD_MEMBER_OF, EMPLOYED_BY, FOUNDED, ADMINISTERS.
Do NOT use RELATED_TO, AFFILIATED_WITH, KEY_PERSON, HAS_KEY_PERSON, or other non-governance types.

WHAT TO FIND (prioritize completeness):
1. Ownership chain: Trace to ultimate parent or beneficial owner. OWNS (parent→subsidiary): ORG/PERSON/COMMUNITY→ORG. SUBSIDIARY_OF (subsidiary→parent). Include intermediate holding companies. metrics.ownership_percent when known. COMMUNITY (dynasty, tribe) can OWNS ORGANIZATION.
2. Control: CONTROLS for state control, voting rights, contractual control. Who controls at each level?
3. Full board: BOARD_MEMBER_OF (PERSON→ORG). All board members for significant organizations.
4. Full C-suite: EMPLOYED_BY (PERSON→ORG) for CEO, CFO, COO. FOUNDED (PERSON→ORG) for founders.
5. Investment: INVESTS_IN (investor→target). Include metrics (investment_amount, round, valuation) when known.
6. Government hierarchy: ADMINISTERS (GOV/ORG with /gov/ path → subordinate GOV or PROGRAM). Full hierarchy to leaf agencies.
7. Return entities (parents, subsidiaries, board members, investors, gov bodies, programs) that should be explored further (child jobs will recurse).

INTEGRATION_TYPE qualifier (for OWNS): When the ownership fits a known pattern, add qualifiers.integration_type:
- "vertical": subsidiary is part of vertically integrated supply chain (e.g., BYD owning battery/cell producers)
- "partnership": JV, strategic stake, or alliance structure (e.g., European OEM–supplier equity ties)
- "arm's-length": pure financial investment, no operational integration

CURRENT FRAGMENT:
%s

QUESTIONS TO ANSWER (from Critic; prioritize these):
- %s

Return a JSON object:
{
  "fragment": { updated fields if any },
  "relations": [ { "targetName", "targetType", "targetCanonicalPath", "relationType", "reason", "confidence", "metrics": { "ownership_percent", "investment_amount", ... }, "qualifiers": { "integration_type"?: "vertical|partnership|arm's-length" } } ],
  "supplyChains": []
}

Return ONLY valid JSON. No markdown.`, taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeGovernance), taxonomy.ExtendedCanonicalPathRulesPrompt(), geoContext, phaseGuardrails, string(fragmentJSON), questions)
}

// buildMarketEnrichmentPrompt builds a prompt for market participation, sizing, concentration, submarkets.
func (a *ResearchAgent) buildMarketEnrichmentPrompt(input ResearchStrategyInput) string {
	frag := input.Fragment
	if frag == nil {
		frag = &models.HorizonFragment{}
	}
	fragmentJSON, _ := json.MarshalIndent(frag, "", "  ")
	questions := strings.Join(input.MissingInfo, "\n- ")
	if questions == "" {
		questions = "Identify market participation, sub-markets, key players, market sizing (TAM, CAGR, HHI, CR4), geographic concentration."
	}
	phaseGuardrails := unitPhaseResearchGuardrailBlock(input.UnitPipelinePhase)
	return fmt.Sprintf(`You are a research agent. Enrich the entity for MARKET context. Aim for COMPLETE market picture:

ENTITY TYPES in market: %s. PARTICIPATES_IN source can be any of these. Use correct targetType for each relation.

CANONICAL PATH RULES (targetCanonicalPath MUST follow):
%s

%s

Use ONLY these relation types: PARTICIPATES_IN (with marketRole and metrics.market_share), HAS_SUBMARKET.
Do NOT use DOMINATES, COMPETES_WITH, or other non-market types.

WHAT TO FIND (prioritize completeness):
1. Market participation: PARTICIPATES_IN (entity→market). Source: ORGANIZATION, FACILITY, COMMUNITY, or PROGRAM. metrics.market_share (0..1 or percent). marketRole: participant, leader, dominant.
2. Full market hierarchy: HAS_SUBMARKET (MARKET→MARKET, parent→narrower). Build 3 levels when possible (e.g. market→sub-market→segment).
3. Top 10 players: For MARKET entities, find PARTICIPATES_IN edges FROM top 10 players (orgs, facilities, communities, programs) by market share. Include shares when known.
4. Full sizing: fragment.state: /metric/market/tam_usd, /metric/market/cagr_percent, /metric/market/hhi, /metric/market/cr4_percent. Only if you find reliable data; do NOT fabricate.
5. Geographic split: fragment.state with geo distribution (e.g. /metric/market/geo_share_us, /metric/market/geo_share_eu) when relevant.
6. Return key players (orgs, facilities, communities, programs) that should be explored further (child jobs will recurse).

CURRENT FRAGMENT:
%s

QUESTIONS TO ANSWER (from Critic; prioritize these):
- %s

Return a JSON object:
{
  "fragment": { updated fields: state with /metric/market/tam_usd, cagr_percent, hhi, cr4_percent when found },
  "relations": [ { "targetName", "targetType", "targetCanonicalPath", "relationType", "reason", "confidence", "metrics": { "market_share": ... } } ],
  "supplyChains": []
}

Return ONLY valid JSON. No markdown.`, taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeMarket), taxonomy.ExtendedCanonicalPathRulesPrompt(), phaseGuardrails, string(fragmentJSON), questions)
}

// buildStrategicEnrichmentPrompt builds a prompt for strategic layer (decision points, mechanisms, gatekeepers).
func (a *ResearchAgent) buildStrategicEnrichmentPrompt(input ResearchStrategyInput) string {
	frag := input.Fragment
	if frag == nil {
		frag = &models.HorizonFragment{}
	}
	fragmentJSON, _ := json.MarshalIndent(frag, "", "  ")
	questions := strings.Join(input.MissingInfo, "\n- ")
	if questions == "" {
		questions = "Identify decision points, mechanisms, gatekeepers, control/access relations, export controls, sanctions, licensing."
	}
	phaseGuardrails := unitPhaseResearchGuardrailBlock(input.UnitPipelinePhase)
	return fmt.Sprintf(`You are a research agent. Enrich the entity for STRATEGIC context only.

ENTITY TYPES in strategic: %s. Gatekeepers can be org, gov, community, or person. Use correct targetType for each relation.

CANONICAL PATH RULES (targetCanonicalPath, sourceCanonicalPath MUST follow):
%s

%s

Use ONLY these relation types: CONTROLS_ACCESS, IMPOSES_CONDITION, GATEKEEPS_DECISION, USES_MECHANISM, APPLIES_TO.
Do NOT use generic relation types.

WHAT TO FIND:
1. Decision points: DECISION_POINT /decision/{domain}/{slug} — concrete decisions (Grant License, Block Export, Approve). CONTROLS_ACCESS (decision→entity it controls), IMPOSES_CONDITION (decision→entity). GATEKEEPS_DECISION (org/gov/community/person→decision: who decides).
2. Mechanisms: MECHANISM /mechanism/{category}/{slug} — instruments (Entity List, Banking License, Export Control). USES_MECHANISM (org/gov/decision→mechanism: who uses it). APPLIES_TO (mechanism→entity: what it affects). Categories: regulatory, economic, technological, contractual, political.
3. Control/access: CONTROLS_ACCESS — who blocks/enables access to what. IMPOSES_CONDITION — who imposes rules/costs on whom.
4. Gatekeepers: GATEKEEPS_DECISION — ORGANIZATION (incl. /gov/), PERSON, or COMMUNITY as source, DECISION_POINT as target. Who has authority over this decision?

CURRENT FRAGMENT:
%s

QUESTIONS TO ANSWER (from Critic; prioritize these):
- %s

Return a JSON object:
{
  "fragment": { updated fields if any },
  "relations": [ { "targetName", "targetType", "targetCanonicalPath", "relationType", "reason", "confidence", "sourceCanonicalPath?", "sourceType?", "sourceName?" } ],
  "supplyChains": []
}

For GATEKEEPS_DECISION and USES_MECHANISM: set sourceCanonicalPath, sourceType, sourceName when the SOURCE is not the current fragment (e.g. gatekeeper org, mechanism user).
Return ONLY valid JSON. No markdown.`, taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeStrategic), taxonomy.ExtendedCanonicalPathRulesPrompt(), phaseGuardrails, string(fragmentJSON), questions)
}
