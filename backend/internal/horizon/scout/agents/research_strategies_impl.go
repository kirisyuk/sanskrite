package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/entity_semantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/naming"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

const (
	maxResearchChainContextChars = 4000
	maxResearchEntityLineChars   = 220
	maxResearchRawContentChars   = 7000
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

// researchPreferExistingEntitiesBlock returns guidance to use existing entities from grounding instead of creating duplicates.
// Applied to all research prompts to reduce duplicates from minor naming differences.
func researchPreferExistingEntitiesBlock() string {
	return `PREFER EXISTING ENTITIES (avoid duplicates):
Your search has access to existing entities in the knowledge base (grounding/datastore results).
When you find an entity that matches an existing one — even with minor naming differences (e.g., "Tesla Inc" vs "Tesla Inc.", "JPMorgan" vs "JPMorgan Chase", "Ministry of Finance" vs "MOF", abbreviations vs full names) — use the EXISTING canonical path from the grounding context.
Do NOT create duplicate entities. Only create new entities when no suitable existing match is found in your search results.

DIRECTION ROLE CONTRACT (mandatory when applicable):
- For OWNS/SUBSIDIARY_OF/INVESTS_IN/ASSURES/CONTROLS (ORG↔ORG), DEPENDS_ON (actor↔actor), and SUPPLIES_TO (ORG/FACILITY↔ORG/FACILITY), ALWAYS include qualifiers.source_role and qualifiers.target_role.
- Use role pairs: owner->owned_entity, subsidiary->parent_owner, investor->investee, assurer->assuree, controller->controlled_entity, dependent->dependency, supplier->customer.
- Also include qualifiers.direction_basis when direction is explicit (owner_to_owned, investor_to_investee, dependent_to_dependency, supplier_to_customer).
- If direction is unclear, omit the relation instead of guessing.

ZONE GEO CONTRACT:
- In /zone/{geo}/{condition_type}/{slug}, geo: ` + taxonomy.ISO2GeoDescriptionForPrompts() + `
- Sea-area zones are valid and must use geo=sea (example: /zone/sea/political_tension/spratly-islands).
- Do not invent natural-feature geo tokens like ocean/gulf/strait for {geo}; keep specific place naming in slug.
`
}

func strategySupplyChainMetricsVocabularyBlock() string {
	return `SUPPLY CHAIN METRICS (use ONLY these units):
volume: gwh_per_year | metric_tons_per_year | kt_per_year | bpd | mbpd | mcf_per_day | units_per_year | cells
share: percent (0-100) | ratio (0-1)
contract_value: usd | eur | rub
lead_time_days: days
on_time_delivery: ratio (0-1)

Format per segment: { "volume": { "value": 15, "unit": "gwh_per_year", "confidence": 0.8 }, "share": { "value": 40, "unit": "percent", "confidence": 0.7 } }
Choose unit by product type: batteries/energy → gwh; ores/chemicals → metric_tons; oil/gas → bbl/bcf; discrete → units/cells.
`
}

func strategyMetricValueFormatBlock() string {
	return `METRIC VALUE FORMAT (STRICT):
- For fragment.state and relations[].metrics, each metric entry MUST be either:
  1) scalar number / numeric string (e.g., 0.065, "6.5%", "10M"), OR
  2) MetricValue object: { "value": <number>, "unit"?: "...", "confidence"?: 0..1, "observedAt"?: "YYYY-MM-DD|RFC3339" }.
- value MUST be numeric (JSON number after expansion). Never place narrative text in metric values.
- Put explanations in "reason" or "fragment.description", not in metrics.
- INVALID metric example:
  "market_share_percent": "Estimated ~6.5% share of total food retail ... 30%+ of modern retail segment."
- VALID metric example:
  "market_share_percent": { "value": 6.5, "unit": "percent", "confidence": 0.7 }
`
}

const supplyChainOperationalModelBlock = `CRITICAL — Supply chain operational model (org→fac→product→fac→org):
1. Full chain: ORGANIZATION → its FACILITIES → PRODUCTS they produce → receiving FACILITIES → their ORGANIZATIONS.
2. Chain nodes use their real types: /org/ for organizations, /fac/ for facilities, /product/ for products.
3. When a specific facility is unknown, use /fac/{geo}/{orgSlug}-general-production.
4. Product = catalog entity. One product type, one node. Do not duplicate products for transport steps. Use transportVia array on the supplies edge. A product must NOT appear more than once in a single supply chain path — no pseudo-cycles.
5. Use only PRODUCES, SUPPLIES_TO, TRANSFORMS_INTO, EXTRACTS_FROM.`

const supplyChainProductGranularityBlock = `PRODUCT GRANULARITY (what counts as a product node):
- Tech/consumer products: use MODEL LINE / product family level. NOT specific SKU or version.
  - Correct: iPhone, MacBook, Claude, Claude Code (model line for tech). Wrong: iPhone 17, MacBook Pro M4, Claude Opus.
  - One product node = one model line. Avoid version/year suffixes (iPhone 17, Tesla Model Y 2024).
- Natural resources, ores, chemicals: follow existing taxonomy (/product/raw/mineral/..., /product/finished/...). Keep appropriate granularity (lithium-carbonate, copper-anode) — do NOT over-generalize to "minerals" or "chemicals".
- When in doubt: prefer product family over specific SKU; prefer concrete commodity over vague category.`

const supplyChainSpecialScenariosBlock = `SPECIAL SCENARIOS:
- Contract manufacturing: org owns IP; facility (contract manufacturer) produces. Link: fac_produces_product; facility may be /fac/{geo}/{contractor}-general-production.
- Toll manufacturing: raw from fac A, processing at fac B, result to A or C. Segments: fac_A→product→fac_B→product→fac_C.
- Distributors/resellers: fac with type distribution/sales; supplies without produces.
- Software vendors: fac = /fac/{geo}/{vendor}-general-production, product = software product line (e.g. Claude Code), not specific release.
- Co-product/by-product: one process (fac)→multiple product. Multiple PRODUCES edges from one fac.`

// holisticOutputSchemaBlock returns the full JSON schema for holistic/refine prompts (fragment partial merge + relations).
func holisticOutputSchemaBlock() string {
	return `Return JSON object (STRICT SCHEMA):
{
  "fragment": {
    "description"?: "string (max 500 chars)",
    "state"?: { "/metric/...": { "value": number, "confidence": 0..1, "observedAt": "RFC3339", "provenance": { "sourceIds": ["url"], "method": "llm_metric_inference" } } },
    "homeLocation"?: { "name": "string", "iso3166_1": "ISO2" },
    "qualifiers"?: { "key": "value" },
    "identifiers"?: { "legalName"?: "string", "shortName"?: "string" },
    "labels"?: { "en": "string", ... },
    "nameHistory"?: [ { "name": "string", "kind": "short|legal|brand|aka", "validFrom"?: "YYYY-MM-DD", "validTo"?: "YYYY-MM-DD" } ]
  },
  "relations": [
    {
      "targetName": "string (REQUIRED)",
      "targetType": "ORGANIZATION|FACILITY|PRODUCT|GEO_REGION|... (REQUIRED)",
      "targetCanonicalPath": "string (REQUIRED, canonical path)",
      "relationType": "string (REQUIRED)",
      "reason": "string",
      "confidence": 0.0,
      "sourceCanonicalPath"?: "string (when this fragment is semantic target)",
      "sourceType"?: "string",
      "sourceName"?: "string",
      "qualifiers"?: { "key": "value" }
    }
  ]
}`
}

const supplyChainMandatoryBlock = `MANDATORY — SUPPLY CHAIN RULES (NO EXCEPTIONS):
- Chain model: org → fac → product → fac → org. Organizations at the ends, facilities in the middle, products between facilities.
- Nodes: /org/ for organizations, /fac/ for facilities, /product/ for products, /deposit/ for deposits.
- Edge types: PRODUCES, SUPPLIES_TO, TRANSFORMS_INTO, EXTRACTS_FROM.
- Product: one per type in path. No duplicates. No "Freight" or "delivery" as product unless that IS the facility's service.
- If you cannot satisfy these rules, omit the relation. Do NOT output invalid links.`

func strategySupplyChainsJSONContractBlock() string {
	return `SUPPLY CHAINS JSON CONTRACT (STRICT):
- "supplyChains" MUST be an array of objects.
- Each chain object MUST have:
  - "nodes": REQUIRED array of objects (NOT string and NOT shorthand)
  - "confidence": REQUIRED number in 0..1
  - "reason": REQUIRED short evidence string
  - "transportVia": OPTIONAL array of facility canonical paths (/fac/...)
  - "segmentMetrics": OPTIONAL array of { fromNodeIndex, toNodeIndex, productIndex, volume: { value, unit, confidence }, share, period, confidence }
  - (no "alternatives" field — model is org→fac→product→fac→org trees, not alternative suppliers)
- Node object schema (each nodes[i]):
  - "canonicalPath": REQUIRED string
  - "name": REQUIRED string
  - "type": REQUIRED enum: ORGANIZATION|FACILITY|PRODUCT|DEPOSIT
  - "facilityType": OPTIONAL supply chain role: ` + taxonomy.FacilityRolesForPrompts() + `
  - "facilityKind": OPTIONAL physical type: ` + taxonomy.FacilityKindsForPrompts() + ` (seaport, factory, cable, pipeline, mine, etc.)
  - "position": OPTIONAL (upstream|midstream|downstream)
  - "role": OPTIONAL string
- Use /org/ for organizations, /fac/ for facilities, /product/ for products, /deposit/ for deposits. When facility unknown: /fac/{geo}/{orgSlug}-general-production.
- INVALID shape example: "nodes": "A -> B -> C"
- VALID shape example (org→fac→product→fac→org):
{
  "supplyChains": [{
    "nodes": [
      { "canonicalPath": "/org/us/albemarle-corp", "name": "Albemarle Corporation", "type": "ORGANIZATION" },
      { "canonicalPath": "/fac/us/silver-peak", "name": "Silver Peak Lithium", "type": "FACILITY", "facilityType": "extraction", "facilityKind": "mine", "position": "upstream" },
      { "canonicalPath": "/product/raw/mineral/refining/lithium-carbonate", "name": "Lithium carbonate", "type": "PRODUCT" },
      { "canonicalPath": "/fac/cn/catl-ningde", "name": "CATL Ningde", "type": "FACILITY", "facilityType": "manufacturing", "facilityKind": "factory", "position": "downstream" },
      { "canonicalPath": "/org/cn/catl", "name": "CATL", "type": "ORGANIZATION" }
    ],
    "confidence": 0.85,
    "reason": "Industry reports and filings.",
    "transportVia": ["/fac/us/port-long-beach"],
    "segmentMetrics": [{ "fromNodeIndex": 1, "toNodeIndex": 3, "productIndex": 2, "volume": { "value": 10, "unit": "kt_per_year", "confidence": 0.7 }, "period": "2023" }]
  }]
}`
}

// SupplyChainResearchStrategy enriches for supply chain only (PRODUCES, CONSUMES, SUPPLIES_TO, etc.).
type SupplyChainResearchStrategy struct {
	agent *ResearchAgent
}

func (s *SupplyChainResearchStrategy) DomainMode() string { return taxonomy.DomainModeSupplyChain }

func (s *SupplyChainResearchStrategy) Enrich(ctx context.Context, input ResearchStrategyInput) (*ResearchStrategyOutput, error) {
	if s.agent == nil || s.agent.executor == nil {
		return nil, fmt.Errorf("supply-chain strategy is not configured")
	}
	prompt := s.agent.buildSupplyChainEnrichmentPrompt(input)
	out, err := s.agent.runResearchPrompt(ctx, prompt, input.Fragment)
	if err != nil || out == nil {
		return nil, err
	}
	out.Relations, _ = FilterDomainRelations(ctx, s.DomainMode(), out.Relations)
	out.SupplyChains, _ = FilterDomainChains(ctx, s.DomainMode(), out.SupplyChains)
	return out, nil
}

// GovernanceResearchStrategy enriches for ownership, board, key persons only.
type GovernanceResearchStrategy struct {
	agent *ResearchAgent
}

func (s *GovernanceResearchStrategy) DomainMode() string { return taxonomy.DomainModeGovernance }

func (s *GovernanceResearchStrategy) Enrich(ctx context.Context, input ResearchStrategyInput) (*ResearchStrategyOutput, error) {
	if s.agent == nil || s.agent.executor == nil {
		return nil, fmt.Errorf("governance strategy is not configured")
	}
	prompt := s.agent.buildGovernanceEnrichmentPrompt(input)
	out, err := s.agent.runResearchPrompt(ctx, prompt, input.Fragment)
	if err != nil || out == nil {
		return nil, err
	}
	out.Relations, _ = FilterDomainRelations(ctx, s.DomainMode(), out.Relations)
	out.SupplyChains, _ = FilterDomainChains(ctx, s.DomainMode(), out.SupplyChains)
	return out, nil
}

// AssuranceResearchStrategy enriches for Third Line: audit, certification, regulatory oversight.
type AssuranceResearchStrategy struct {
	agent *ResearchAgent
}

func (s *AssuranceResearchStrategy) DomainMode() string { return taxonomy.DomainModeAssurance }

func (s *AssuranceResearchStrategy) Enrich(ctx context.Context, input ResearchStrategyInput) (*ResearchStrategyOutput, error) {
	if s.agent == nil || s.agent.executor == nil {
		return nil, fmt.Errorf("assurance strategy is not configured")
	}
	prompt := s.agent.buildAssuranceEnrichmentPrompt(input)
	out, err := s.agent.runResearchPrompt(ctx, prompt, input.Fragment)
	if err != nil || out == nil {
		return nil, err
	}
	out.Relations, _ = FilterDomainRelations(ctx, s.DomainMode(), out.Relations)
	out.SupplyChains, _ = FilterDomainChains(ctx, s.DomainMode(), out.SupplyChains)
	return out, nil
}

// MarketResearchStrategy enriches for market participation only (PARTICIPATES_IN, HAS_MARKET_SEGMENT).
type MarketResearchStrategy struct {
	agent *ResearchAgent
}

func (s *MarketResearchStrategy) DomainMode() string { return taxonomy.DomainModeMarket }

func (s *MarketResearchStrategy) Enrich(ctx context.Context, input ResearchStrategyInput) (*ResearchStrategyOutput, error) {
	if s.agent == nil || s.agent.executor == nil {
		return nil, fmt.Errorf("market strategy is not configured")
	}
	prompt := s.agent.buildMarketEnrichmentPrompt(input)
	out, err := s.agent.runResearchPrompt(ctx, prompt, input.Fragment)
	if err != nil || out == nil {
		return nil, err
	}
	out.Relations, _ = FilterDomainRelations(ctx, s.DomainMode(), out.Relations)
	out.SupplyChains, _ = FilterDomainChains(ctx, s.DomainMode(), out.SupplyChains)
	return out, nil
}

// StrategicResearchStrategy enriches for strategic layer (CONTROLS_ACCESS, USES_MECHANISM, etc.).
type StrategicResearchStrategy struct {
	agent *ResearchAgent
}

func (s *StrategicResearchStrategy) DomainMode() string { return taxonomy.DomainModeStrategic }

func (s *StrategicResearchStrategy) Enrich(ctx context.Context, input ResearchStrategyInput) (*ResearchStrategyOutput, error) {
	if s.agent == nil || s.agent.executor == nil {
		return nil, fmt.Errorf("strategic strategy is not configured")
	}
	prompt := s.agent.buildStrategicEnrichmentPrompt(input)
	out, err := s.agent.runResearchPrompt(ctx, prompt, input.Fragment)
	if err != nil || out == nil {
		return nil, err
	}
	out.Relations, _ = FilterDomainRelations(ctx, s.DomainMode(), out.Relations)
	out.SupplyChains, _ = FilterDomainChains(ctx, s.DomainMode(), out.SupplyChains)
	return out, nil
}

// PolicyImpactResearchStrategy enriches policy impact channels (regulatory instrument -> segment -> impact).
type PolicyImpactResearchStrategy struct {
	agent *ResearchAgent
}

func (s *PolicyImpactResearchStrategy) DomainMode() string { return taxonomy.DomainModePolicyImpact }

func (s *PolicyImpactResearchStrategy) Enrich(ctx context.Context, input ResearchStrategyInput) (*ResearchStrategyOutput, error) {
	if s.agent == nil || s.agent.executor == nil {
		return nil, fmt.Errorf("policy-impact strategy is not configured")
	}
	prompt := s.agent.buildPolicyImpactEnrichmentPrompt(input)
	out, err := s.agent.runResearchPrompt(ctx, prompt, input.Fragment)
	if err != nil || out == nil {
		return nil, err
	}
	out.Relations, _ = FilterDomainRelations(ctx, s.DomainMode(), out.Relations)
	out.SupplyChains, _ = FilterDomainChains(ctx, s.DomainMode(), out.SupplyChains)
	return out, nil
}

// EcosystemResearchStrategy enriches for ecosystem/stakeholder mapping (multi-actor relationships).
type EcosystemResearchStrategy struct {
	agent *ResearchAgent
}

func (s *EcosystemResearchStrategy) DomainMode() string { return taxonomy.DomainModeEcosystem }

func (s *EcosystemResearchStrategy) Enrich(ctx context.Context, input ResearchStrategyInput) (*ResearchStrategyOutput, error) {
	if s.agent == nil || s.agent.executor == nil {
		return nil, fmt.Errorf("ecosystem strategy is not configured")
	}
	prompt := s.agent.buildEcosystemEnrichmentPrompt(input)
	out, err := s.agent.runResearchPrompt(ctx, prompt, input.Fragment)
	if err != nil || out == nil {
		return nil, err
	}
	out.Relations, _ = FilterDomainRelations(ctx, s.DomainMode(), out.Relations)
	out.SupplyChains, _ = FilterDomainChains(ctx, s.DomainMode(), out.SupplyChains)
	return out, nil
}

// GeneralResearchStrategy enriches in broad mode with fragment-type specialization.
type GeneralResearchStrategy struct {
	agent *ResearchAgent
}

func (s *GeneralResearchStrategy) DomainMode() string { return taxonomy.DomainModeGeneral }

func (s *GeneralResearchStrategy) Enrich(ctx context.Context, input ResearchStrategyInput) (*ResearchStrategyOutput, error) {
	if s.agent == nil || s.agent.executor == nil {
		return nil, fmt.Errorf("general strategy is not configured")
	}
	prompt := s.agent.buildGeneralEnrichmentPrompt(input)
	out, err := s.agent.runResearchPrompt(ctx, prompt, input.Fragment)
	if err != nil || out == nil {
		return nil, err
	}
	out.Relations, _ = FilterDomainRelations(ctx, s.DomainMode(), out.Relations)
	out.SupplyChains, _ = FilterDomainChains(ctx, s.DomainMode(), out.SupplyChains)
	return out, nil
}

// runResearchPrompt runs a research prompt and parses result into ResearchStrategyOutput.
// Returns grounding evidence from Vertex AI Search when available.
// inputFragment is used as fallback when LLM omits fragment or returns invalid structure.
func (a *ResearchAgent) runResearchPrompt(ctx context.Context, prompt string, inputFragment *models.HorizonFragment) (*ResearchStrategyOutput, error) {
	if a == nil || a.executor == nil {
		return nil, fmt.Errorf("research agent executor is not configured")
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
	result, fullResult, err := a.executor.ExecuteStructuredTaskFull(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("research prompt: %w", err)
	}
	jsonData, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	// Always sanitize before unmarshal: the sanitizer normalizes field names
	// (e.g. LLM uses "participants" instead of "nodes"), coerces types, and
	// filters broken entries. Without this, json.Unmarshal silently produces
	// zero-value SupplyChain structs when field names don't match tags.
	sanitized, sanitizeErr := sanitizeResearchResultForUnmarshal(jsonData)
	unmarshalPath := "sanitized"
	unmarshalInput := sanitized
	if sanitizeErr != nil {
		unmarshalPath = "direct"
		unmarshalInput = jsonData
	}
	var res researchResult
	if err := json.Unmarshal(unmarshalInput, &res); err != nil {
		if unmarshalPath == "sanitized" {
			// Sanitized input failed too — try raw as last resort
			if err2 := json.Unmarshal(jsonData, &res); err2 != nil {
				return nil, fmt.Errorf("unmarshal research result: %w (sanitize: %v)", err2, sanitizeErr)
			}
			unmarshalPath = "direct_fallback"
		} else {
			return nil, fmt.Errorf("unmarshal research result: %w", err)
		}
	}

	// Diagnostic: log chain quality after unmarshal (before repair)
	for i, sc := range res.SupplyChains {
		nodesWithCP := 0
		for _, n := range sc.Nodes {
			if strings.TrimSpace(n.CanonicalPath) != "" {
				nodesWithCP++
			}
		}
		log.Printf("[ResearchParse] chain[%d]: nodesTotal=%d nodesWithCP=%d confidence=%.2f unmarshalPath=%s jsonBytes=%d",
			i, len(sc.Nodes), nodesWithCP, sc.Confidence, unmarshalPath, len(jsonData))
	}
	if len(res.SupplyChains) == 0 {
		rawWhat := logRawSupplyChainsFromJSON(jsonData)
		log.Printf("[ResearchParse] 0 supply chains in response unmarshalPath=%s jsonBytes=%d raw=%s", unmarshalPath, len(jsonData), rawWhat)
	}
	repairSupplyChainNodePaths(res.SupplyChains)
	out := &ResearchStrategyOutput{
		Relations:         res.Relations,
		SupplyChains:      res.SupplyChains,
		GroundingEvidence: nil,
	}
	if fullResult != nil {
		out.GroundingEvidence = fullResult.GroundingEvidence
		out.RawContent = compactResearchRawContent(fullResult.Content)
	}
	if res.Fragment != nil {
		fragBytes, _ := json.Marshal(res.Fragment)
		var partial models.HorizonFragment
		if err := json.Unmarshal(fragBytes, &partial); err == nil {
			out.FragmentPartial = &partial
		}
	}
	// When LLM omits fragment or returns invalid structure, preserve input fragment so we don't lose data.
	if out.FragmentPartial == nil && inputFragment != nil {
		out.FragmentPartial = inputFragment
	}
	normalizeDiscoveredRelationsNames(out.Relations)
	return out, nil
}

// sanitizeResearchResultForUnmarshal repairs common shape/type drifts in LLM output so
// a single malformed field does not drop the whole research response.
func sanitizeResearchResultForUnmarshal(data []byte) ([]byte, error) {
	var root map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}

	if fragmentRaw, ok := root["fragment"]; ok {
		if fragmentMap, mapOK := fragmentRaw.(map[string]interface{}); mapOK {
			sanitizeMetricMapField(fragmentMap, "state")
		} else {
			// LLM returned fragment as null, string, array, etc. Replace with empty object
			// so we preserve structure instead of losing fragment entirely.
			root["fragment"] = map[string]interface{}{}
		}
	}

	sanitizeResearchRelations(root)
	sanitizeResearchSupplyChains(root)

	return json.Marshal(root)
}

func sanitizeResearchRelations(root map[string]interface{}) {
	relsRaw, exists := root["relations"]
	if !exists {
		return
	}
	rels, ok := relsRaw.([]interface{})
	if !ok {
		delete(root, "relations")
		return
	}
	clean := make([]interface{}, 0, len(rels))
	for _, relRaw := range rels {
		relMap, ok := relRaw.(map[string]interface{})
		if !ok {
			continue
		}
		sanitizeMetricMapField(relMap, "metrics")
		coerceFloatField(relMap, "confidence", 0.5)
		ensureStringField(relMap, "targetName")
		ensureStringField(relMap, "targetType")
		ensureStringField(relMap, "targetCanonicalPath")
		ensureStringField(relMap, "relationType")
		ensureStringField(relMap, "reason")
		ensureStringField(relMap, "sourceCanonicalPath")
		ensureStringField(relMap, "sourceType")
		ensureStringField(relMap, "sourceName")
		sanitizeMapFieldOrDelete(relMap, "qualifiers")
		clean = append(clean, relMap)
	}
	root["relations"] = clean
}

func sanitizeResearchSupplyChains(root map[string]interface{}) {
	chainsRaw, exists := root["supplyChains"]
	if !exists {
		return
	}
	chains := toInterfaceSlice(chainsRaw)
	if chains == nil {
		delete(root, "supplyChains")
		return
	}

	cleanChains := make([]interface{}, 0, len(chains))
	for _, chainRaw := range chains {
		chainMap, ok := chainRaw.(map[string]interface{})
		if !ok {
			continue
		}
		nodes := extractNodesMultiKey(chainMap)
		if len(nodes) == 0 {
			logChainKeysOnEmpty(chainMap)
			continue
		}
		chainMap["nodes"] = nodes
		coerceFloatFieldMultiKey(chainMap, 0.5, "confidence", "conf", "reliability", "score")
		ensureStringFieldMultiKey(chainMap, "reason", "reason", "explanation", "description", "evidence")
		if via := sanitizeStringArrayField(chainMap["transportVia"]); len(via) > 0 {
			chainMap["transportVia"] = via
		} else {
			delete(chainMap, "transportVia")
		}
		if metrics := sanitizeSupplyChainSegmentMetricsField(chainMap["segmentMetrics"]); len(metrics) > 0 {
			chainMap["segmentMetrics"] = metrics
		} else {
			delete(chainMap, "segmentMetrics")
		}
		if alts := sanitizeSupplyChainAlternativesField(chainMap["alternatives"]); len(alts) > 0 {
			chainMap["alternatives"] = alts
		} else {
			delete(chainMap, "alternatives")
		}
		cleanChains = append(cleanChains, chainMap)
	}
	root["supplyChains"] = cleanChains
}

func sanitizeSupplyChainSegmentMetricsField(raw interface{}) []interface{} {
	items := toInterfaceSlice(raw)
	if items == nil {
		return nil
	}
	out := make([]interface{}, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		fromIdx, okFrom := coerceIndexValue(firstValue(m, "fromNodeIndex", "from_node_index", "from"))
		toIdx, okTo := coerceIndexValue(firstValue(m, "toNodeIndex", "to_node_index", "to"))
		productIdx, okProduct := coerceIndexValue(firstValue(m, "productIndex", "product_index", "product"))
		if !okFrom || !okTo || !okProduct {
			continue
		}
		entry := map[string]interface{}{
			"fromNodeIndex": fromIdx,
			"toNodeIndex":   toIdx,
			"productIndex":  productIdx,
		}
		if v := sanitizeMetricWithConfidenceField(firstValue(m, "volume")); v != nil {
			entry["volume"] = v
		}
		if v := sanitizeMetricWithConfidenceField(firstValue(m, "share")); v != nil {
			entry["share"] = v
		}
		if s := strings.TrimSpace(firstStringValue(m, "period")); s != "" {
			entry["period"] = s
		}
		if s := strings.TrimSpace(firstStringValue(m, "start")); s != "" {
			entry["start"] = s
		}
		if s := strings.TrimSpace(firstStringValue(m, "end")); s != "" {
			entry["end"] = s
		}
		coerceFloatField(m, "confidence", 0.5)
		entry["confidence"] = m["confidence"]
		out = append(out, entry)
	}
	return out
}

func sanitizeSupplyChainAlternativesField(raw interface{}) []interface{} {
	items := toInterfaceSlice(raw)
	if items == nil {
		return nil
	}
	out := make([]interface{}, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		customerIdx, okCustomer := coerceIndexValue(firstValue(m, "customerNodeIndex", "customer_node_index", "customerIndex"))
		productIdx, okProduct := coerceIndexValue(firstValue(m, "productNodeIndex", "product_node_index", "productIndex"))
		if !okCustomer || !okProduct {
			continue
		}
		suppliers := toInterfaceSlice(firstValue(m, "suppliers"))
		if len(suppliers) == 0 {
			continue
		}
		cleanSuppliers := make([]interface{}, 0, len(suppliers))
		for _, supplierRaw := range suppliers {
			supplierMap, ok := supplierRaw.(map[string]interface{})
			if !ok {
				continue
			}
			supplierRef := firstStringValue(supplierMap, "supplierRef", "supplier", "canonicalPath", "path")
			if strings.TrimSpace(supplierRef) == "" {
				continue
			}
			entry := map[string]interface{}{
				"supplierRef": strings.TrimSpace(supplierRef),
			}
			coerceFloatField(supplierMap, "technicalFit", 0.5)
			coerceFloatField(supplierMap, "regulatoryFit", 0.5)
			coerceFloatField(supplierMap, "confidence", 0.5)
			entry["technicalFit"] = supplierMap["technicalFit"]
			entry["regulatoryFit"] = supplierMap["regulatoryFit"]
			entry["confidence"] = supplierMap["confidence"]
			if v := sanitizeMetricWithConfidenceField(firstValue(supplierMap, "availableCapacity")); v != nil {
				entry["availableCapacity"] = v
			}
			if v := sanitizeMetricWithConfidenceField(firstValue(supplierMap, "leadTimeToSwitch")); v != nil {
				entry["leadTimeToSwitch"] = v
			}
			if v := sanitizeMetricWithConfidenceField(firstValue(supplierMap, "costToSwitch")); v != nil {
				entry["costToSwitch"] = v
			}
			cleanSuppliers = append(cleanSuppliers, entry)
		}
		if len(cleanSuppliers) == 0 {
			continue
		}
		out = append(out, map[string]interface{}{
			"customerNodeIndex": customerIdx,
			"productNodeIndex":  productIdx,
			"suppliers":         cleanSuppliers,
		})
	}
	return out
}

func sanitizeMetricWithConfidenceField(raw interface{}) map[string]interface{} {
	m, ok := raw.(map[string]interface{})
	if !ok || m == nil {
		return nil
	}
	out := map[string]interface{}{}
	coerceFloatField(m, "value", 0.0)
	out["value"] = m["value"]
	if u := strings.TrimSpace(firstStringValue(m, "unit")); u != "" {
		out["unit"] = u
	}
	coerceFloatField(m, "confidence", 0.5)
	out["confidence"] = m["confidence"]
	return out
}

func firstValue(holder map[string]interface{}, keys ...string) interface{} {
	for _, key := range keys {
		if v, ok := holder[key]; ok && v != nil {
			return v
		}
	}
	return nil
}

func coerceIndexValue(raw interface{}) (int, bool) {
	switch v := raw.(type) {
	case int:
		if v < 0 {
			return 0, false
		}
		return v, true
	case int32:
		if v < 0 {
			return 0, false
		}
		return int(v), true
	case int64:
		if v < 0 {
			return 0, false
		}
		return int(v), true
	case float64:
		if v < 0 {
			return 0, false
		}
		return int(v), true
	case float32:
		if v < 0 {
			return 0, false
		}
		return int(v), true
	case json.Number:
		if i, err := v.Int64(); err == nil && i >= 0 {
			return int(i), true
		}
		if f, err := v.Float64(); err == nil && f >= 0 {
			return int(f), true
		}
		return 0, false
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return 0, false
		}
		if i, err := strconv.Atoi(s); err == nil && i >= 0 {
			return i, true
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil && f >= 0 {
			return int(f), true
		}
		return 0, false
	default:
		return 0, false
	}
}

// extractNodesMultiKey looks for the nodes array under multiple possible field names
// that the LLM may use instead of the expected "nodes".
func extractNodesMultiKey(chainMap map[string]interface{}) []interface{} {
	for _, key := range []string{"nodes", "node_list", "chain_nodes", "participants", "steps", "stages", "chain", "entities", "members", "elements", "links"} {
		if raw, ok := chainMap[key]; ok && raw != nil {
			nodes := sanitizeSupplyChainNodesField(raw)
			if len(nodes) > 0 {
				return nodes
			}
		}
	}
	return nil
}

// coerceFloatFieldMultiKey looks for a float value under multiple keys and normalizes it
// into the first key name. Used because LLM may return "confidence" as "reliability", etc.
func coerceFloatFieldMultiKey(m map[string]interface{}, defaultVal float64, keys ...string) {
	if len(keys) == 0 {
		return
	}
	canonicalKey := keys[0]
	for _, key := range keys {
		if raw, ok := m[key]; ok && raw != nil {
			if key != canonicalKey {
				m[canonicalKey] = raw
				delete(m, key)
			}
			coerceFloatField(m, canonicalKey, defaultVal)
			return
		}
	}
	m[canonicalKey] = defaultVal
}

// ensureStringFieldMultiKey looks for a string value under multiple keys and normalizes it.
func ensureStringFieldMultiKey(m map[string]interface{}, canonicalKey string, keys ...string) {
	for _, key := range keys {
		if raw, ok := m[key]; ok && raw != nil {
			if s, ok := raw.(string); ok && strings.TrimSpace(s) != "" {
				if key != canonicalKey {
					m[canonicalKey] = s
					delete(m, key)
				}
				return
			}
		}
	}
	ensureStringField(m, canonicalKey)
}

// logChainKeysOnEmpty logs the actual keys of a chain object when no nodes were found,
// to help diagnose what field names the LLM used.
func logChainKeysOnEmpty(chainMap map[string]interface{}) {
	keys := make([]string, 0, len(chainMap))
	for k := range chainMap {
		keys = append(keys, k)
	}
	log.Printf("[ResearchParse] WARN: chain with 0 nodes dropped. keys=%v", keys)
}

// logRawSupplyChainsFromJSON extracts supplyChains from raw LLM JSON and returns a short
// diagnostic string (missing|null|[]|len=N,sample=...) so logs show exactly what the LLM returned.
func logRawSupplyChainsFromJSON(jsonData []byte) string {
	var raw map[string]interface{}
	if err := json.Unmarshal(jsonData, &raw); err != nil {
		return "parse_err"
	}
	v, exists := raw["supplyChains"]
	if !exists {
		return "missing"
	}
	if v == nil {
		return "null"
	}
	arr, ok := v.([]interface{})
	if !ok {
		return fmt.Sprintf("type=%T", v)
	}
	if len(arr) == 0 {
		return "[]"
	}
	// Log structure of first chain: keys and len(nodes/steps/etc)
	sample := ""
	if len(arr) > 0 {
		if m, ok := arr[0].(map[string]interface{}); ok {
			keys := make([]string, 0, len(m))
			for k, val := range m {
				if s, ok := val.([]interface{}); ok {
					keys = append(keys, fmt.Sprintf("%s[len=%d]", k, len(s)))
				} else {
					keys = append(keys, k)
				}
			}
			sample = strings.Join(keys, ",")
			if len(sample) > 200 {
				sample = sample[:200] + "..."
			}
		}
	}
	return fmt.Sprintf("len=%d first=%s", len(arr), sample)
}

func sanitizeSupplyChainNodesField(raw interface{}) []interface{} {
	nodes := toInterfaceSlice(raw)
	if nodes == nil {
		return nil
	}
	clean := make([]interface{}, 0, len(nodes))
	for _, nodeRaw := range nodes {
		nodeMap := sanitizeSupplyChainNode(nodeRaw)
		if nodeMap == nil {
			continue
		}
		clean = append(clean, nodeMap)
	}
	return clean
}

func sanitizeSupplyChainNode(raw interface{}) map[string]interface{} {
	switch v := raw.(type) {
	case map[string]interface{}:
		cp := firstStringValue(v, "canonicalPath", "canonical_path", "path")
		name := firstStringValue(v, "name", "step", "entity", "label", "id")
		nodeType := firstStringValue(v, "type")
		if cp == "" && name == "" {
			return nil
		}
		if cp != "" && name == "" {
			name = deriveNameFromCanonicalPath(cp)
		}
		if cp != "" && nodeType == "" {
			nodeType = inferSupplyChainNodeTypeFromPath(cp)
		}
		if nodeType == "" {
			nodeType = "ORGANIZATION"
		}
		nodeType = normalizeSupplyChainNodeType(nodeType)
		if cp == "" && name != "" {
			cp = inferCanonicalPathForSupplyChainActor(name, nodeType)
		}
		node := map[string]interface{}{
			"canonicalPath": cp,
			"name":          name,
			"type":          nodeType,
		}
		if s := firstStringValue(v, "role"); s != "" {
			node["role"] = s
		}
		if s := firstStringValue(v, "facilityType", "facility_type"); s != "" {
			node["facilityType"] = s
		}
		if s := firstStringValue(v, "facilityKind", "facility_kind"); s != "" {
			node["facilityKind"] = s
		}
		return node
	case string:
		return sanitizeSupplyChainNodeString(v)
	default:
		return nil
	}
}

func sanitizeSupplyChainNodeString(raw string) map[string]interface{} {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil
	}
	parsedPath := normalizeNodePathToken(s)
	if strings.HasPrefix(parsedPath, "/") {
		return map[string]interface{}{
			"canonicalPath": parsedPath,
			"name":          deriveNameFromCanonicalPath(parsedPath),
			"type":          inferSupplyChainNodeTypeFromPath(parsedPath),
		}
	}
	nodeType := "ORGANIZATION"
	cp := inferCanonicalPathForSupplyChainActor(s, nodeType)
	return map[string]interface{}{
		"canonicalPath": cp,
		"name":          s,
		"type":          nodeType,
	}
}

func sanitizeMapFieldOrDelete(holder map[string]interface{}, field string) {
	raw, ok := holder[field]
	if !ok {
		return
	}
	if _, isMap := raw.(map[string]interface{}); !isMap {
		delete(holder, field)
	}
}

func ensureStringField(holder map[string]interface{}, field string) {
	raw, ok := holder[field]
	if !ok || raw == nil {
		return
	}
	switch v := raw.(type) {
	case string:
		holder[field] = strings.TrimSpace(v)
	default:
		holder[field] = strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func firstStringValue(holder map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		raw, ok := holder[key]
		if !ok || raw == nil {
			continue
		}
		switch v := raw.(type) {
		case string:
			if s := strings.TrimSpace(v); s != "" {
				return s
			}
		default:
			s := strings.TrimSpace(fmt.Sprintf("%v", v))
			if s != "" {
				return s
			}
		}
	}
	return ""
}

func coerceFloatField(holder map[string]interface{}, field string, fallback float64) {
	raw, ok := holder[field]
	if !ok || raw == nil {
		holder[field] = fallback
		return
	}
	switch v := raw.(type) {
	case float64:
		holder[field] = v
	case float32:
		holder[field] = float64(v)
	case int:
		holder[field] = float64(v)
	case int64:
		holder[field] = float64(v)
	case json.Number:
		if f, err := v.Float64(); err == nil {
			holder[field] = f
			return
		}
		holder[field] = fallback
	case string:
		s := strings.TrimSpace(v)
		s = strings.TrimSuffix(s, "%")
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			holder[field] = f
			return
		}
		holder[field] = fallback
	default:
		holder[field] = fallback
	}
}

func sanitizeStringArrayField(raw interface{}) []string {
	arr := toInterfaceSlice(raw)
	if arr == nil {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		switch v := item.(type) {
		case string:
			s := strings.TrimSpace(v)
			if s != "" {
				out = append(out, s)
			}
		default:
			s := strings.TrimSpace(fmt.Sprintf("%v", v))
			if s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func toInterfaceSlice(raw interface{}) []interface{} {
	switch v := raw.(type) {
	case []interface{}:
		return v
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return nil
		}
		var arr []interface{}
		if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") && json.Unmarshal([]byte(s), &arr) == nil {
			return arr
		}
		for _, token := range splitSupplyChainNodeString(s) {
			arr = append(arr, token)
		}
		return arr
	default:
		return nil
	}
}

func splitSupplyChainNodeString(raw string) []string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil
	}
	repl := strings.NewReplacer("→", "->", "=>", "->", "\n", "->", ";", "->")
	s = repl.Replace(s)
	parts := strings.Split(s, "->")
	if len(parts) == 1 {
		parts = strings.Split(s, ",")
	}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		t := strings.TrimSpace(part)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

func normalizeNodePathToken(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "/") {
		return s
	}
	lowered := strings.ToLower(s)
	for _, prefix := range []string{"org/", "fac/", "product/", "market/", "program/", "loc/", "deposit/", "region/"} {
		if strings.HasPrefix(lowered, prefix) {
			return "/" + s
		}
	}
	return s
}

func deriveNameFromCanonicalPath(cp string) string {
	trimmed := strings.TrimSpace(cp)
	if trimmed == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	last := strings.TrimSpace(parts[len(parts)-1])
	if last == "" && len(parts) > 1 {
		last = strings.TrimSpace(parts[len(parts)-2])
	}
	last = strings.ReplaceAll(last, "-", " ")
	return last
}

// inferCanonicalPathFromNameAndType generates a best-effort canonical path
// when the LLM provides a name and type but omits canonicalPath.
// Paths use /discovered/ as the intermediate segment to signal auto-generation;
// the downstream canonicalizer resolves them to proper stable paths.
func inferCanonicalPathFromNameAndType(name, nodeType string) string {
	slug := slugifyNodeName(name)
	if slug == "" {
		return ""
	}
	prefix := nodeTypeToPathPrefix(strings.ToUpper(strings.TrimSpace(nodeType)))
	return prefix + slug
}

// inferCanonicalPathForSupplyChainActor is used when repairing supply chain nodes.
// For ORGANIZATION: returns /org/discovered/{slug} (org stays as org in supply chain).
// For other types: delegates to inferCanonicalPathFromNameAndType.
func inferCanonicalPathForSupplyChainActor(name, nodeType string) string {
	slug := slugifyNodeName(name)
	if slug == "" {
		return ""
	}
	nt := strings.ToUpper(strings.TrimSpace(nodeType))
	if nt == "ORGANIZATION" || nt == "ORG" {
		return "/org/discovered/" + slug
	}
	return inferCanonicalPathFromNameAndType(name, nodeType)
}

func nodeTypeToPathPrefix(nodeType string) string {
	switch nodeType {
	case "FACILITY":
		return "/fac/discovered/"
	case "PRODUCT":
		return "/product/discovered/"
	case "PROGRAM":
		return "/program/discovered/"
	case "MARKET":
		return "/market/discovered/"
	case "GEO_REGION":
		return "/loc/discovered/"
	case "DEPOSIT":
		return "/deposit/discovered/"
	case "COMMUNITY":
		return "/community/discovered/"
	default:
		return "/org/discovered/"
	}
}

func slugifyNodeName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	if s == "" {
		return ""
	}
	var buf strings.Builder
	buf.Grow(len(s))
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			buf.WriteRune(r)
			prevDash = false
		case r == ' ', r == '_', r == '.', r == ',', r == '(', r == ')', r == '&':
			if !prevDash && buf.Len() > 0 {
				buf.WriteByte('-')
				prevDash = true
			}
		case r == '-':
			if !prevDash && buf.Len() > 0 {
				buf.WriteByte('-')
				prevDash = true
			}
		}
	}
	result := strings.TrimRight(buf.String(), "-")
	if len(result) > 80 {
		result = result[:80]
		result = strings.TrimRight(result, "-")
	}
	return result
}

// repairSupplyChainNodePaths fills in empty CanonicalPath fields on supply chain
// nodes that survived direct json.Unmarshal but were returned by the LLM without
// a canonical path. Without this repair they would be dropped by the parser.
func repairSupplyChainNodePaths(chains []SupplyChain) {
	for i := range chains {
		for j := range chains[i].Nodes {
			n := &chains[i].Nodes[j]
			if strings.TrimSpace(n.CanonicalPath) != "" {
				continue
			}
			name := strings.TrimSpace(n.Name)
			if name == "" {
				continue
			}
			nodeType := strings.TrimSpace(n.Type)
			if nodeType == "" {
				nodeType = "ORGANIZATION"
			}
			n.CanonicalPath = inferCanonicalPathForSupplyChainActor(name, normalizeSupplyChainNodeType(nodeType))
		}
	}
}

func inferSupplyChainNodeTypeFromPath(cp string) string {
	path := strings.TrimSpace(strings.ToLower(cp))
	switch {
	case strings.HasPrefix(path, "/org/"):
		return "ORGANIZATION"
	case strings.HasPrefix(path, "/fac/"):
		return "FACILITY"
	case strings.HasPrefix(path, "/product/"):
		return "PRODUCT"
	case strings.HasPrefix(path, "/program/"):
		return "PROGRAM"
	case strings.HasPrefix(path, "/market/"):
		return "MARKET"
	case strings.HasPrefix(path, "/loc/"), strings.HasPrefix(path, "/region/"):
		return "GEO_REGION"
	case strings.HasPrefix(path, "/deposit/"):
		return "DEPOSIT"
	case strings.HasPrefix(path, "/community/"):
		return "COMMUNITY"
	default:
		return "ORGANIZATION"
	}
}

// normalizeSupplyChainNodeType maps arbitrary LLM output to runtime-supported enum.
// Prevents contract drift when LLM returns "COMMUNITY", "network", "group" etc.
func normalizeSupplyChainNodeType(raw string) string {
	u := strings.ToUpper(strings.TrimSpace(raw))
	switch u {
	case "ORGANIZATION", "ORG":
		return "ORGANIZATION"
	case "FACILITY", "FAC":
		return "FACILITY"
	case "PRODUCT":
		return "PRODUCT"
	case "PROGRAM":
		return "PROGRAM"
	case "MARKET":
		return "MARKET"
	case "GEO_REGION", "GEO", "LOCATION", "LOC", "REGION":
		return "GEO_REGION"
	case "DEPOSIT":
		return "DEPOSIT"
	case "COMMUNITY", "NETWORK", "GROUP", "COLLECTIVE":
		return "COMMUNITY"
	default:
		return "ORGANIZATION"
	}
}

// sanitizeMetricMapField removes malformed metric maps from the holder field.
// This allows strategy output to remain usable when a single metric value is emitted as narrative text.
func sanitizeMetricMapField(holder map[string]interface{}, field string) {
	raw, ok := holder[field]
	if !ok {
		return
	}
	metricMap, ok := raw.(map[string]interface{})
	if !ok {
		delete(holder, field)
		return
	}

	clean := make(map[string]interface{}, len(metricMap))
	for key, value := range metricMap {
		if isValidMetricValueJSON(value) {
			clean[key] = value
		}
	}

	if len(clean) == 0 {
		delete(holder, field)
		return
	}
	holder[field] = clean
}

func buildResearchFocusContextBlock(focus string, spherePaths []string, keywords []string, originalQuery string) string {
	focus = strings.TrimSpace(focus)
	originalQuery = strings.TrimSpace(originalQuery)
	spheresLine := strings.Join(dedupNonEmptyStrings(spherePaths), ", ")
	keywordsLine := strings.Join(dedupNonEmptyStrings(keywords), ", ")

	if focus == "" && spheresLine == "" && keywordsLine == "" && originalQuery == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString("FOCUS CONTEXT (use to steer grounded search):\n")
	if originalQuery != "" {
		b.WriteString(fmt.Sprintf("- originalQuery: %q\n", originalQuery))
	}
	if focus != "" {
		b.WriteString(fmt.Sprintf("- focus: %s\n", focus))
	}
	if spheresLine != "" {
		b.WriteString(fmt.Sprintf("- spherePaths: %s\n", spheresLine))
	}
	if keywordsLine != "" {
		b.WriteString(fmt.Sprintf("- keywords: %s\n", keywordsLine))
	}
	return b.String()
}

func buildResearchChainContextBlock(chainContext string) string {
	chainContext = strings.TrimSpace(chainContext)
	if chainContext == "" {
		return ""
	}
	chainContext = truncateTextHeadTail(chainContext, maxResearchChainContextChars)
	return "SUPPLY CHAIN CONTEXT:\n" + chainContext
}

func buildResearchExistingEntitiesBlock(existingEntities []string) string {
	if len(existingEntities) == 0 {
		return ""
	}
	ents := existingEntities
	if len(ents) > 50 {
		ents = ents[:50]
	}
	var b strings.Builder
	b.WriteString("EXISTING ENTITIES (reuse instead of creating duplicates):\n")
	for _, e := range ents {
		if strings.TrimSpace(e) == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("- %s\n", truncateTextHeadTail(e, maxResearchEntityLineChars)))
	}
	b.WriteString("- Prefer existing canonical paths when entity identity matches.\n")
	return b.String()
}

func truncateTextHeadTail(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 || len(text) <= limit {
		return text
	}
	if limit <= 32 {
		return text[:limit]
	}
	head := (limit * 2) / 3
	tail := limit - head - len("\n...[truncated]...\n")
	if tail < 0 {
		tail = 0
	}
	if head > len(text) {
		head = len(text)
	}
	if tail > len(text)-head {
		tail = len(text) - head
	}
	return text[:head] + "\n...[truncated]...\n" + text[len(text)-tail:]
}

func compactResearchRawContent(raw string) string {
	return truncateTextHeadTail(raw, maxResearchRawContentChars)
}

func isDomainFragment(fragment *models.HorizonFragment) bool {
	if fragment == nil {
		return false
	}
	cp := strings.TrimSpace(strings.ToLower(fragment.CanonicalPath))
	return fragment.Type == models.FragmentTypeDomain || strings.HasPrefix(cp, "/domain/")
}

func buildGeneralResearchDomainExpansionBlock(fragment *models.HorizonFragment) string {
	if !isDomainFragment(fragment) {
		return ""
	}
	return `DOMAIN EXPANSION GUIDANCE (CRITICAL OVERRIDE):
- This fragment is a DOMAIN contract. Your primary goal is to expand it into first-order entities.
- Use ONLY domain relation types: ` + domainScaffoldRelationTypesPromptString() + `.
- Target canonicalPath MUST match targetType:
  ORGANIZATION->/org or /gov, PERSON->/person, PRODUCT->/product, PROGRAM->/program, MARKET->/market, FACILITY->/fac, GEO_REGION->/loc, EVENT->/event, MEDIA_CHANNEL->/media, COMMUNITY->/community, DOMAIN->/domain, AFFECTED_ZONE->/zone
- If the user asks about a market, use /market/... as a TARGET (anchor), NOT as a DOMAIN.
- Do NOT output placeholder names like NA/N-A/Unknown. Use fewer entities instead.
- For AFFECTED_ZONE: slug MUST be descriptive place/region name (northern-rakhine, gaza-strip, donbas). NEVER "na".
- ZONE vs EVENT: ZONE = geographic area; EVENT = discrete occurrence. TRIGGERED_BY: zone→event.`
}

func generalResearchCanonicalSupplementBlock() string {
	return `- /segment/{domain}/{vertical}/{geo} for org marketSegments; /media/{platform}/{slug} for media channels
- Product: use a single canonical format /product/{category}/{type}/{stage}/{slug}[/{variant-slug}]
- For services, use category=service and map service domain/subdomain into {type}/{stage}
- Markets: prefer /market/{geo}/{domain}/{vertical}; for HAS_MARKET_SEGMENT child path = parent path + one segment
- Communities: /community/{geo}/{type}/{slug}; use HAS_SUBGROUP for parent->narrower subgroup
- Programs: prefer /program/{jurisdiction}/{slug} when jurisdiction is known
- AFFECTED_ZONE: /zone/{geo}/{condition_type}/{slug}. geo: ` + taxonomy.ISO2GeoDescriptionForPrompts() + `. condition_type: military_conflict, natural_disaster, political_tension, humanitarian_crisis, earthquake_affected, flood_zone. slug: descriptive place/region name (northern-syria, donbas, gaza-strip, northern-rakhine). NEVER use "na" as slug — use region name or condition-specific name (e.g. myanmar-crisis-area).
- NEVER use "/discovered/*" (not allowed)
- canonicalPath MUST match expected prefix for targetType; if unsure, still output your best stable canonicalPath`
}

func buildGeneralResearchRelationTaxonomyBlock(isDomain bool) string {
	if isDomain {
		return `ALLOWED RELATION TAXONOMY (DOMAIN OVERRIDE):
- Use ONLY: ` + domainScaffoldRelationTypesPromptString() + `.
- Do not emit non-domain relations for DOMAIN fragments.
- Domain is configuration, not a graph node for non-domain relation families.`
	}
	return "ALLOWED RELATION TAXONOMY:\n" + extractionGeneralRelationRulesBlock()
}

func generalResearchProductCanonicalizationBlock() string {
	return `Product Canonicalization:
- If you can resolve a more specific product name/path, update fragment.canonicalPath.
- Prefer /product/{category}/{type}/{stage}/{slug} consistently.
- For services, use category=service and map service domain/subdomain into {type}/{stage}.
- If the current path is flat or acronym-like, expand to full product name and align relation targets.
- Product naming rules:
  - Use SINGULAR nouns by default (e.g., "battery cell", "lithium carbonate"), not invented plurals.
  - Use plural only when plural is standard industry term.
  - Do NOT include company/brand prefix unless it is clearly official and widely used.`
}

func generalResearchNameHistoryBlock() string {
	return `Name History (ORG/PERSON - rebrand/rename/formerly-known-as):
- If former names are found, output fragment.nameHistory[] with:
  { "name": "...", "kind": "short|legal|brand|aka", "validFrom"?: "YYYY-MM-DD", "validTo"?: "YYYY-MM-DD" }
- Do NOT include current name in nameHistory.
- Search cues: "formerly known as", "renamed", "rebrand", "previous name".`
}

func generalResearchExplicitSourceRulesBlock() string {
	return `EXPLICIT SOURCE RULES (CRITICAL for strict direction semantics):
- Use ROLE-FIRST direction for OWNS/SUBSIDIARY_OF/INVESTS_IN/ASSURES/CONTROLS (ORG<->ORG): source is defined by semantic role, not by CURRENT FRAGMENT.
- Use sourceCanonicalPath/sourceType/sourceName when semantic source is different.
- OWNS: direction is owner -> owned entity. If THIS FRAGMENT is owned by X, set sourceCanonicalPath/sourceType/sourceName = X and keep targetCanonicalPath = THIS FRAGMENT.
- Never emit reversed OWNS (THIS FRAGMENT -> its owner).
- INVESTS_IN: direction is investor -> investee. If THIS FRAGMENT is invested in by X, set sourceCanonicalPath/sourceType/sourceName = X and keep targetCanonicalPath = THIS FRAGMENT.
- Never emit reversed INVESTS_IN (THIS FRAGMENT -> its investor).
- GATEKEEPS_DECISION: source must be gatekeeper (ORG/GOV/COMMUNITY), target is DECISION_POINT.
- USES_MECHANISM: source is decision/org/gov/community/program, target is MECHANISM.
- APPLIES_TO: source is MECHANISM, target is affected entity.
- CONTROLS_ACCESS / IMPOSES_CONDITION: source is controller/imposer, target is affected entity.
- For actor↔actor DEPENDS_ON and ORG/FACILITY SUPPLIES_TO, include qualifiers.source_role/target_role (dependent->dependency, supplier->customer).
- For phase-1 business edges (DEPENDS_ON, SUPPLIES_TO, LOCATED_IN, etc.), keep default source unless evidence explicitly requires otherwise.
- Exception: for OWNS/INVESTS_IN/ASSURES/CONTROLS, use explicit source whenever THIS FRAGMENT is the semantic target.
- For directed ORG->ORG edges above, include qualifiers.source_role and qualifiers.target_role.`
}

func buildResearchDecompositionGuidanceBlock(fragment *models.HorizonFragment, hints []DecompositionHint, originalQuery string) string {
	if fragment == nil {
		return ""
	}
	cp := strings.TrimSpace(fragment.CanonicalPath)
	isAnchor := fragment.Type == models.FragmentTypeMarket ||
		fragment.Type == models.FragmentTypeProgram ||
		strings.HasPrefix(cp, "/market/") ||
		strings.HasPrefix(cp, "/program/")
	if !isAnchor {
		return ""
	}

	for i := range hints {
		if strings.TrimSpace(hints[i].AnchorPath) != cp {
			continue
		}
		hint := hints[i]
		var b strings.Builder
		b.WriteString("MARKET/PROGRAM DECOMPOSITION GUIDANCE:\n")
		b.WriteString("- This fragment is an anchor. Prioritize concrete players and operating assets.\n")
		if len(hint.EntityTypes) > 0 {
			b.WriteString(fmt.Sprintf("- target entity types: %s\n", strings.Join(hint.EntityTypes, ", ")))
		}
		if len(hint.SubMarkets) > 0 {
			b.WriteString(fmt.Sprintf("- sub-markets: %s\n", strings.Join(hint.SubMarkets, ", ")))
		}
		if len(hint.PlayerCategories) > 0 {
			b.WriteString(fmt.Sprintf("- player categories: %s\n", strings.Join(hint.PlayerCategories, ", ")))
		}
		if len(hint.SearchQueries) > 0 {
			b.WriteString("- suggested search queries:\n")
			for _, q := range hint.SearchQueries {
				if strings.TrimSpace(q) == "" {
					continue
				}
				b.WriteString(fmt.Sprintf("  - %s\n", q))
			}
		}
		b.WriteString("- Prefer real-world entities and relations over abstract commentary.\n")
		return b.String()
	}

	originalQuery = strings.TrimSpace(originalQuery)
	if originalQuery == "" {
		return ""
	}
	return fmt.Sprintf(`MARKET/PROGRAM DECOMPOSITION GUIDANCE:
- This fragment is an anchor; prioritize concrete entities and business relations.
- originalQuery: %q
- Discover major players, products/services, regulators, and facilities.
- Emit these as relations with grounded evidence and stable canonical paths.`, originalQuery)
}

func joinPromptBlocks(blocks ...string) string {
	out := make([]string, 0, len(blocks))
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		out = append(out, block)
	}
	return strings.Join(out, "\n\n")
}

func (a *ResearchAgent) buildGeneralEnrichmentPrompt(input ResearchStrategyInput) string {
	frag := input.Fragment
	if frag == nil {
		frag = &models.HorizonFragment{}
	}
	fragmentJSON, _ := json.MarshalIndent(frag, "", "  ")
	questions := strings.Join(input.MissingInfo, "\n- ")
	if strings.TrimSpace(questions) == "" {
		questions = "Resolve critical unknowns for fragment attributes, key dependencies, and supply chain links."
	}

	chainBlock := buildResearchChainContextBlock(input.ChainContext)
	focusBlock := buildResearchFocusContextBlock(input.Focus, input.SpherePaths, input.Keywords, input.OriginalQuery)
	existingEntitiesBlock := buildResearchExistingEntitiesBlock(input.ExistingEntities)
	decompositionBlock := buildResearchDecompositionGuidanceBlock(frag, input.DecompositionHints, input.OriginalQuery)
	domainBlock := buildGeneralResearchDomainExpansionBlock(frag)
	specBlock := buildResearchSpecializationModulesBlock(input.Specialties)
	businessRulesBlock := taxonomy.BusinessTagRulesPrompt()
	profileBlock := entity_semantics.ResearchBlockForPath(frag.CanonicalPath, frag.Type, frag.CommunityType)
	objectTypeTaskBlock := buildObjectTypeTaskSpecializationBlockWithHints(frag.CanonicalPath, frag.Type, frag.CommunityType, frag.Qualifiers, frag.Identifiers, frag.SpherePaths)
	coreBusinessTaxonomyBlock := buildCoreBusinessTaxonomyBlock()
	objectQualifierContractBlock := buildObjectQualifierContractBlockWithHints(frag.CanonicalPath, frag.Type, frag.Qualifiers, frag.Identifiers, frag.Labels)
	nameRulesBlock := naming.PromptEntityNameNormalizationRules()
	canonicalRulesBlock := taxonomy.ExtendedCanonicalPathRulesPrompt() + "\n" + generalResearchCanonicalSupplementBlock()
	relationTaxonomyBlock := buildGeneralResearchRelationTaxonomyBlock(isDomainFragment(frag))

	return joinPromptBlocks(
		`You are a research agent. Enrich the fragment with grounded facts and business-meaningful relations.`,
		`CURRENT FRAGMENT DATA:
`+string(fragmentJSON),
		chainBlock,
		focusBlock,
		existingEntitiesBlock,
		researchPreferExistingEntitiesBlock(),
		decompositionBlock,
		domainBlock,
		specBlock,
		businessRulesBlock,
		profileBlock,
		nameRulesBlock,
		objectTypeTaskBlock,
		coreBusinessTaxonomyBlock,
		objectQualifierContractBlock,
		generalResearchProductCanonicalizationBlock(),
		generalResearchNameHistoryBlock(),
		generalResearchExplicitSourceRulesBlock(),
		relationTaxonomyBlock,
		`CANONICAL PATH RULES:
`+canonicalRulesBlock,
		fmt.Sprintf(`QUESTIONS TO ANSWER (from Critic):
- %s`, questions),
		`Return JSON object:
{
  "fragment": { updated fields only },
  "relations": [ { "targetName", "targetType", "targetCanonicalPath", "relationType", "reason", "confidence", "sourceCanonicalPath?", "sourceType?", "sourceName?", "metrics"?, "qualifiers"? } ],
  "supplyChains": [ { "nodes": [ { "canonicalPath", "name", "type", "role"? } ], "confidence", "reason", "transportVia"?: ["/fac/..."] } ]
}`,
		strategySupplyChainsJSONContractBlock(),
		`OUTPUT BUDGET RULES (CRITICAL):
- Keep fragment.description short (max 500 chars); omit if it risks JSON truncation.
- If budget is tight, prioritize missingInfo answers and valid JSON with closed braces.`,
		strategyMetricValueFormatBlock(),
		`Return ONLY valid JSON. No markdown.`,
	)
}

func isValidMetricValueJSON(raw interface{}) bool {
	b, err := json.Marshal(raw)
	if err != nil {
		return false
	}
	var mv models.MetricValue
	return json.Unmarshal(b, &mv) == nil
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
		if frag != nil && strings.HasPrefix(strings.TrimSpace(frag.CanonicalPath), "/org/") {
			questions = "Find ALL child organizations (subsidiaries, controlled, partners) AND ALL own facilities. One org → many child orgs + many facs. Format: JSON."
		} else if frag != nil && strings.HasPrefix(strings.TrimSpace(frag.CanonicalPath), "/fac/") {
			questions = "Find ALL products/services that THIS facility creates directly. One fac → many products. Do NOT list products of related or parent companies. Format: JSON."
		} else {
			questions = "Build org→fac→product→fac→org trees (1-to-many at each level): (1) From org: ALL child orgs + ALL facilities. (2) From fac: ALL products THIS fac produces. (3) From product: ALL org-recipients. Format: JSON."
		}
	}
	geoContext := geoAndSectorContext(frag)
	chainBlock := buildResearchChainContextBlock(input.ChainContext)
	existingEntitiesBlock := buildResearchExistingEntitiesBlock(input.ExistingEntities)
	objectQualifierContractBlock := buildObjectQualifierContractBlockWithHints(frag.CanonicalPath, frag.Type, frag.Qualifiers, frag.Identifiers, frag.Labels)
	return fmt.Sprintf(`You are a research agent. Enrich the entity for SUPPLY CHAIN context. Build coherent chains.

%s

%s

%s

%s

Use ONLY these relation types (canonical): %s.
Do NOT use RELATED_TO, AFFILIATED_WITH, PARTNERS_WITH, OWNS, COMPETES_WITH, or other non-supply-chain types.
%s

ENTITY TYPES in supply chain: %s. Use correct targetType for each relation.

CANONICAL PATH RULES (targetCanonicalPath MUST follow; supplyChains nodes too):
%s

%s
%s
%s
%s
%s

WHAT TO FIND (org→fac→product→fac→org trees, 1-to-many at each level):
1. From ORG: ALL child organizations (subsidiaries, controlled, partners) AND all its own facilities. One org → many orgs + many facs. Output /org/ and /fac/ paths.
2. From FAC: ALL products/services THIS facility creates directly. One fac → many products. NOT products of related or parent companies. Output /product/ paths.
3. From PRODUCT: ALL org-recipients (orgs that receive this product via their facilities). One product → many recipients. Output /org/ paths.
4. Chains: one chain per leaf path. fac→product→fac. Nodes alternate actor→product→actor. Use /fac/ for all actors. transportVia optional.
5. Extractive: EXTRACTS_FROM (FACILITY→DEPOSIT). If CURRENT FRAGMENT is DEPOSIT: incoming edges with sourceCanonicalPath.

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
  "relations": [ { "targetName", "targetType", "targetCanonicalPath", "relationType", "reason", "confidence", "sourceCanonicalPath?", "sourceType?", "sourceName?", "qualifiers": { "integration_type"?: "vertical|partnership|arm's-length", "transport_via"?: ["/fac/us/port-long-beach", "/fac/ae/jebel-ali-port"], "source_role"?: "supplier|dependent", "target_role"?: "customer|dependency" } } ],
  "supplyChains": [ { "nodes": [ { "canonicalPath", "name", "type", "role"? } ], "confidence", "reason", "transportVia"?: ["/fac/us/port-long-beach", "/fac/ae/jebel-ali-port"] } ]
}
Nodes MUST alternate actor→product. Include FACILITY where transformation happens (product→facility→product).
For SUPPLIES_TO: add qualifiers.transport_via when transport route known. transportVia in chains = facilities product passes through.

%s

%s

%s

Return ONLY valid JSON. No markdown.`, supplyChainMandatoryBlock, supplyChainOperationalModelBlock, supplyChainSpecialScenariosBlock, supplyChainProductGranularityBlock, taxonomy.SupplyChainEdgeTypesString(), GovEntityRelationRulesBlock(), taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeSupplyChain), taxonomy.ExtendedCanonicalPathRulesPrompt(), chainBlock, geoContext, existingEntitiesBlock, researchPreferExistingEntitiesBlock(), objectQualifierContractBlock, string(fragmentJSON), questions, strategySupplyChainMetricsVocabularyBlock(), strategyMetricValueFormatBlock(), strategySupplyChainsJSONContractBlock())
}

// EnrichSupplyChainFull runs anchor-centric full supply chain query (Plan mode).
// Input: SupplyChainQuery (anchor, direction, depth, filters). No fragment.
// Output: SupplyChains with nodes, segmentMetrics, alternatives. Called directly, not via ResearchPipeline.
func (a *ResearchAgent) EnrichSupplyChainFull(ctx context.Context, query SupplyChainQuery) ([]SupplyChain, error) {
	if a == nil || a.executor == nil {
		return nil, fmt.Errorf("research agent executor not configured")
	}
	prompt := a.buildSupplyChainFullQueryPrompt(query)
	out, err := a.runResearchPrompt(ctx, prompt, nil)
	if err != nil || out == nil {
		return nil, err
	}
	filteredChains, _ := FilterDomainChains(ctx, taxonomy.DomainModeSupplyChain, out.SupplyChains)
	return filteredChains, nil
}

// buildSupplyChainFullQueryPrompt builds anchor-centric prompt. Model: trees from org → fac → product → fac → org (recursion).
func (a *ResearchAgent) buildSupplyChainFullQueryPrompt(query SupplyChainQuery) string {
	var anchorDesc string
	if len(query.Anchors) > 0 {
		trimmed := make([]string, 0, len(query.Anchors))
		for _, p := range query.Anchors {
			if p := strings.TrimSpace(p); p != "" {
				trimmed = append(trimmed, p)
			}
		}
		anchorDesc = "anchors=[" + strings.Join(trimmed, ", ") + "]"
	} else {
		anchorDesc = "anchor=" + strings.TrimSpace(query.Anchor)
	}
	depth := query.Depth
	if depth < 3 || depth > 6 {
		depth = 4
	}
	productFilter := strings.Join(query.ProductFilter, ", ")
	geoFilter := strings.Join(query.GeoFilter, ", ")
	filters := ""
	if productFilter != "" {
		filters += ", productFilter=[" + productFilter + "]"
	}
	if geoFilter != "" {
		filters += ", geoFilter=[" + geoFilter + "]"
	}

	taskBlock := `TASK: Build supply chain trees (1-to-many at each level). Depth: ` + fmt.Sprintf("%d", depth) + ` hops. ` + anchorDesc + filters + `

TREE STRUCTURE (each step fans out 1→many):
Step 1 — ORG → [child orgs, facilities]: For each org anchor, find ALL organizations under it (subsidiaries, controlled, partners) AND its own production facilities. One org may have many child orgs and many facilities.
Step 2 — FAC → [products]: For each facility, find ALL products/services THIS facility produces directly. One facility may produce many products. Do NOT include products of related or parent companies.
Step 3 — PRODUCT → [recipient facs → orgs]: For each product, find ALL facilities that receive/consume it, and their parent organizations. One product may go to many recipients.
Step 4 — Recurse: From each recipient org (back to Step 1) until depth reached.

OUTPUT ONE CHAIN PER LEAF PATH. If org A has fac F1 and fac F2, and F1 produces P1 and P2 — output separate chains: A→F1→P1→..., A→F1→P2→..., A→F2→... etc.`

	return fmt.Sprintf(`You are a supply chain research agent.

%s

%s

%s

%s

%s

%s

Use ONLY these relation types: %s.
ENTITY TYPES: %s.

CANONICAL PATH RULES:
%s

%s

%s

OUTPUT: JSON with "supplyChains" array. Each chain:
- nodes: org→fac→product→fac→org chain. Actors: /org/, /fac/, or /deposit/.
- segmentMetrics: volume, share, period per segment when known.
- transportVia: facility paths product passes through (optional).
- confidence, reason.

Return ONLY valid JSON: { "supplyChains": [ { "nodes": [...], "segmentMetrics": [...], "confidence", "reason" } ] }
No markdown.`, supplyChainMandatoryBlock, supplyChainOperationalModelBlock, supplyChainSpecialScenariosBlock, supplyChainProductGranularityBlock, taskBlock, taxonomy.SupplyChainEdgeTypesString(), taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeSupplyChain), taxonomy.ExtendedCanonicalPathRulesPrompt(), researchPreferExistingEntitiesBlock(), strategySupplyChainMetricsVocabularyBlock(), strategySupplyChainsJSONContractBlock())
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

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
	existingEntitiesBlock := buildResearchExistingEntitiesBlock(input.ExistingEntities)
	objectQualifierContractBlock := buildObjectQualifierContractBlockWithHints(frag.CanonicalPath, frag.Type, frag.Qualifiers, frag.Identifiers, frag.Labels)
	return fmt.Sprintf(`You are a research agent. Enrich the entity for GOVERNANCE context. Aim for COMPLETE ownership and control structure:

ENTITY TYPES in governance: %s. Use correct targetType for each relation.

CANONICAL PATH RULES (targetCanonicalPath MUST follow):
%s

%s
%s
%s

%s

Use ONLY these relation types (canonical): %s.
Do NOT use RELATED_TO, AFFILIATED_WITH, KEY_PERSON, HAS_KEY_PERSON, or other non-governance types.

DIRECTION RULES (CRITICAL):
- Use ROLE-FIRST direction for OWNS/SUBSIDIARY_OF/INVESTS_IN/ASSURES/CONTROLS (ORG<->ORG): source is semantic owner/investor/assurer/controller, not automatically CURRENT FRAGMENT.
- For OWNS, direction is ALWAYS owner -> owned entity. Example: "PIF owns Noon" → source=PIF (owner), target=Noon (owned_entity). NEVER the reverse (Noon -> PIF).
- If CURRENT FRAGMENT is owned by X, you MUST keep relationType=OWNS and set explicit sourceCanonicalPath/sourceType/sourceName = X, with targetCanonicalPath = CURRENT FRAGMENT.
- NEVER output reversed OWNS (CURRENT FRAGMENT -> its owner). If scouting Noon and PIF owns it, source=PIF, target=Noon.
- For INVESTS_IN, direction is ALWAYS investor -> investee.
- If CURRENT FRAGMENT is invested in by X, you MUST keep relationType=INVESTS_IN and set explicit sourceCanonicalPath/sourceType/sourceName = X, with targetCanonicalPath = CURRENT FRAGMENT.
- NEVER output reversed INVESTS_IN (CURRENT FRAGMENT -> its investor).
- For ASSURES, direction is ALWAYS assurer -> assuree.
- If CURRENT FRAGMENT is audited/certified/inspected by X, you MUST keep relationType=ASSURES and set explicit sourceCanonicalPath/sourceType/sourceName = X, with targetCanonicalPath = CURRENT FRAGMENT.
- SUBSIDIARY_OF remains child -> parent and is not a replacement for OWNS direction.
- For directed ORG->ORG edges above, include qualifiers.source_role and qualifiers.target_role.

WHAT TO FIND (prioritize completeness):
1. Ownership chain: Trace to ultimate parent or beneficial owner. OWNS (parent→subsidiary): ORG/PERSON/COMMUNITY→ORG. SUBSIDIARY_OF (subsidiary→parent). Include intermediate holding companies. metrics.ownership_percent when known. COMMUNITY (dynasty, tribe) can OWNS ORGANIZATION. UPSTREAM: when this entity is owned/controlled by X, emit OWNS/CONTROLS with explicit sourceCanonicalPath/sourceType/sourceName = X, target = CURRENT FRAGMENT.
2. Control: CONTROLS (controller→controlled): ORG/PERSON/COMMUNITY→ORG/FACILITY. State control, voting rights, contractual control. Who controls at each level? Do NOT model external audit/certification oversight as CONTROLS; use ASSURES with assurance_type.
3. Full board: BOARD_MEMBER_OF (PERSON→ORG). All board members for significant organizations.
4. Management: MANAGES (manager→managed). CEO, director, operator — formal authority over org/subsidiary. Full C-suite: EMPLOYED_BY (PERSON→ORG) for employment. FOUNDED (PERSON→ORG) for founders.
5. Investment: INVESTS_IN (investor→target). Include metrics (investment_amount, round, valuation) when known.
6. Government hierarchy: ADMINISTERS (GOV/ORG with /gov/ path → subordinate GOV or PROGRAM). Full hierarchy to leaf agencies.
7. External assurance in governance: ASSURES for auditors and certifiers linked to this org/facility/program (do not emit AUDITED_BY legacy inverse).
8. Return entities (parents, subsidiaries, board members, investors, gov bodies, auditors/certifiers, programs) that should be explored further (child jobs will recurse).

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
  "relations": [ { "targetName", "targetType", "targetCanonicalPath", "relationType", "reason", "confidence", "sourceCanonicalPath?", "sourceType?", "sourceName?", "metrics": { "ownership_percent", "investment_amount", ... }, "qualifiers": { "integration_type"?: "vertical|partnership|arm's-length", "source_role"?: "owner|investor|assurer|controller|subsidiary", "target_role"?: "owned_entity|investee|assuree|controlled_entity|parent_owner" } } ],
  "supplyChains": []
}

%s

Return ONLY valid JSON. No markdown.`, taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeGovernance), taxonomy.ExtendedCanonicalPathRulesPrompt(), geoContext, existingEntitiesBlock, researchPreferExistingEntitiesBlock(), objectQualifierContractBlock, relationTypesPromptForDomainMode(taxonomy.DomainModeGovernance), string(fragmentJSON), questions, strategyMetricValueFormatBlock())
}

// buildAssuranceEnrichmentPrompt builds a prompt for Third Line: audit, certification, regulatory oversight.
func (a *ResearchAgent) buildAssuranceEnrichmentPrompt(input ResearchStrategyInput) string {
	frag := input.Fragment
	if frag == nil {
		frag = &models.HorizonFragment{}
	}
	fragmentJSON, _ := json.MarshalIndent(frag, "", "  ")
	questions := strings.Join(input.MissingInfo, "\n- ")
	if questions == "" {
		questions = "Identify external auditors, certifiers, regulators; audit firm, certifications, regulatory oversight."
	}
	geoContext := geoAndSectorContext(frag)
	existingEntitiesBlock := buildResearchExistingEntitiesBlock(input.ExistingEntities)
	objectQualifierContractBlock := buildObjectQualifierContractBlockWithHints(frag.CanonicalPath, frag.Type, frag.Qualifiers, frag.Identifiers, frag.Labels)
	return fmt.Sprintf(`You are a research agent. Enrich the entity for ASSURANCE context (Third Line: independent audit, certification, regulatory oversight). Aim for COMPLETE assurance picture:

ENTITY TYPES in assurance: %s. Assurers are audit firms, certifiers, regulators. Assurees are orgs, facilities, programs.

CANONICAL PATH RULES (targetCanonicalPath MUST follow):
%s

%s
%s
%s
%s

Use ONLY these relation types (canonical): %s.
For ASSURES: add qualifiers.assurance_type = "audit" | "certification" | "regulatory" | "inspection".
Direction: assurer (source) → assuree (target). E.g. audit firm ASSURES org, certifier ASSURES org.
If CURRENT FRAGMENT is audited/certified/inspected by X, set explicit sourceCanonicalPath/sourceType/sourceName = X and keep targetCanonicalPath = CURRENT FRAGMENT.
Do NOT emit AUDITED_BY (legacy inverse direction assuree → assurer).
REGULATES: regulator (source) → regulated entity (target). JURISDICTION RULE: use only for domestic regulatory oversight within the regulator's own jurisdiction. For sanctions, trade restrictions, or extra-territorial enforcement against foreign entities use CONTROLS_ACCESS or IMPOSES_CONDITION instead.

WHAT TO FIND (prioritize completeness):
1. External audit: Who audits this org/facility? ASSURES(assurance_type:audit) from auditor to org. Include audit firm legal name.
2. Certifications: ISO, sector-specific (e.g. FDA approval, mining license, banking license). ASSURES(assurance_type:certification). Certifier → org.
3. Regulatory oversight: Which domestic regulators oversee this entity within their jurisdiction? REGULATES (regulator→entity). Or ASSURES(assurance_type:regulatory) when regulator grants license/approval. For foreign sanctions or extra-territorial restrictions use CONTROLS_ACCESS.
4. Inspections: ASSURES(assurance_type:inspection) for inspection bodies.
5. Return assurers (audit firms, certifiers, regulators) that should be explored further (child jobs will recurse).

CURRENT FRAGMENT:
%s

QUESTIONS TO ANSWER (from Critic; prioritize these):
- %s

Return a JSON object:
{
  "fragment": { updated fields if any },
  "relations": [ { "targetName", "targetType", "targetCanonicalPath", "relationType", "reason", "confidence", "sourceCanonicalPath?", "sourceType?", "sourceName?", "qualifiers": { "assurance_type"?: "audit|certification|regulatory|inspection", "source_role"?: "assurer", "target_role"?: "assuree" } } ],
  "supplyChains": []
}

%s

Return ONLY valid JSON. No markdown.`, taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeAssurance), taxonomy.ExtendedCanonicalPathRulesPrompt(), geoContext, existingEntitiesBlock, researchPreferExistingEntitiesBlock(), objectQualifierContractBlock, relationTypesPromptForDomainMode(taxonomy.DomainModeAssurance), string(fragmentJSON), questions, strategyMetricValueFormatBlock())
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
	existingEntitiesBlock := buildResearchExistingEntitiesBlock(input.ExistingEntities)
	objectQualifierContractBlock := buildObjectQualifierContractBlockWithHints(frag.CanonicalPath, frag.Type, frag.Qualifiers, frag.Identifiers, frag.Labels)
	return fmt.Sprintf(`You are a research agent. Enrich the entity for MARKET context. Aim for COMPLETE market picture:

ENTITY TYPES in market: %s. PARTICIPATES_IN source can be any of these. Use correct targetType for each relation.

CANONICAL PATH RULES (targetCanonicalPath, sourceCanonicalPath MUST follow):
%s

%s
%s
%s

Use ONLY these relation types: %s.
Do NOT use DOMINATES, COMPETES_WITH, or other non-market types.
STRICT DIRECTION/TYPING RULES (MANDATORY):
- PARTICIPATES_IN direction is participant -> market.
  - source MUST be ORGANIZATION/FACILITY/COMMUNITY/PROGRAM/PRODUCT/PERSON.
  - target MUST be MARKET and targetCanonicalPath MUST start with /market/.
  - targetCanonicalPath MUST NOT start with /domain/.
  - For ORGANIZATION ↔ MARKET pairs, use PARTICIPATES_IN (ORG -> MARKET). GOV entities (/gov/...) may also use REGULATES (domestic oversight), CONTROLS_ACCESS (sanctions/restrictions), or INFLUENCES.
- HAS_MARKET_SEGMENT direction is parent-market -> child-market (segment).
  - source and target MUST both be MARKET paths (/market/...).
  - Ideal rule: child path = parent path + exactly one segment (e.g. /market/sa/finance -> /market/sa/finance/banking; /market/global/technology -> /market/global/technology/semiconductors).
  - targetCanonicalPath MUST NOT start with /domain/.
- If CURRENT FRAGMENT is MARKET and you need player -> THIS MARKET, you MUST set explicit source:
  - sourceCanonicalPath/sourceType/sourceName = player
  - targetCanonicalPath = THIS FRAGMENT market path
  - relationType = PARTICIPATES_IN
- Never emit non-domain relations with targetCanonicalPath /domain/... in this strategy.

WHAT TO FIND (prioritize completeness):
1. Market participation: PARTICIPATES_IN (entity→market). Source: ORGANIZATION, FACILITY, COMMUNITY, or PROGRAM. metrics.market_share (0..1 or percent). marketRole: participant, leader, dominant.
2. Full market hierarchy: HAS_MARKET_SEGMENT (MARKET→MARKET, parent→segment). Child path = parent path + one segment. Build 3 levels when possible (e.g. market→sub-market→segment).
3. Top 10 players: For MARKET entities, find top 10 player entities (orgs, facilities, communities, programs) by market share and emit PARTICIPATES_IN as player -> market using explicit sourceCanonicalPath.
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
  "relations": [ { "targetName", "targetType", "targetCanonicalPath", "relationType", "reason", "confidence", "metrics": { "market_share": ... }, "sourceCanonicalPath?", "sourceType?", "sourceName?" } ],
  "supplyChains": []
}

%s

Return ONLY valid JSON. No markdown.`, taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeMarket), taxonomy.ExtendedCanonicalPathRulesPrompt(), existingEntitiesBlock, researchPreferExistingEntitiesBlock(), objectQualifierContractBlock, relationTypesPromptForDomainMode(taxonomy.DomainModeMarket), string(fragmentJSON), questions, strategyMetricValueFormatBlock())
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
	existingEntitiesBlock := buildResearchExistingEntitiesBlock(input.ExistingEntities)
	objectQualifierContractBlock := buildObjectQualifierContractBlockWithHints(frag.CanonicalPath, frag.Type, frag.Qualifiers, frag.Identifiers, frag.Labels)
	return fmt.Sprintf(`You are a research agent. Enrich the entity for STRATEGIC context only.

ENTITY TYPES in strategic: %s. Gatekeepers can be org, gov, community, or person. Use correct targetType for each relation.

CANONICAL PATH RULES (targetCanonicalPath, sourceCanonicalPath MUST follow):
%s

%s
%s
%s

Use ONLY these relation types: %s.
Do NOT use generic relation types.

WHAT TO FIND (downstream impact is required):
1. Decision points: DECISION_POINT /decision/{domain}/{slug} — concrete decisions (Grant License, Block Export, Approve). MUST have CONTROLS_ACCESS or IMPOSES_CONDITION to concrete entities (what it affects). GATEKEEPS_DECISION (who decides). Do NOT create decision points without a clear affected target.
2. Mechanisms: MECHANISM /mechanism/{category}/{slug} — instruments (Entity List, Banking License, Export Control). MUST have APPLIES_TO to concrete entities (what/who it affects). USES_MECHANISM (who uses it). Do NOT create mechanisms without a clear affected target.
3. Control/access: CONTROLS_ACCESS — decision/org blocks/enables access to WHAT (target = org/facility/product/market). IMPOSES_CONDITION — who imposes rules/costs on WHOM.
4. Gatekeepers: GATEKEEPS_DECISION — who has authority over the decision.

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
%s

Return ONLY valid JSON. No markdown.`, taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeStrategic), taxonomy.ExtendedCanonicalPathRulesPrompt(), existingEntitiesBlock, researchPreferExistingEntitiesBlock(), objectQualifierContractBlock, relationTypesPromptForDomainMode(taxonomy.DomainModeStrategic), string(fragmentJSON), questions, strategyMetricValueFormatBlock())
}

// buildPolicyImpactEnrichmentPrompt builds a prompt for policy impact channels and exposure graph.
func (a *ResearchAgent) buildPolicyImpactEnrichmentPrompt(input ResearchStrategyInput) string {
	frag := input.Fragment
	if frag == nil {
		frag = &models.HorizonFragment{}
	}
	fragmentJSON, _ := json.MarshalIndent(frag, "", "  ")
	questions := strings.Join(input.MissingInfo, "\n- ")
	if questions == "" {
		questions = "Identify policy parameters, affected segments, impact channels, and regulator/program links."
	}
	existingEntitiesBlock := buildResearchExistingEntitiesBlock(input.ExistingEntities)
	objectQualifierContractBlock := buildObjectQualifierContractBlockWithHints(frag.CanonicalPath, frag.Type, frag.Qualifiers, frag.Identifiers, frag.Labels)
	return fmt.Sprintf(`You are a research agent. Enrich the entity for POLICY IMPACT context.

ENTITY TYPES in policy impact: %s. Use correct targetType for each relation.

CANONICAL PATH RULES (targetCanonicalPath, sourceCanonicalPath MUST follow):
%s

%s
%s
%s

Use ONLY these relation types: %s.
Do NOT use generic relation types outside this list.
STRICT DIRECTION/TYPING RULES (MANDATORY):
- Domain is config, not a graph node: targetCanonicalPath MUST NOT start with /domain/.
- PARTICIPATES_IN direction is participant -> market.
  - source MUST be ORGANIZATION/FACILITY/COMMUNITY/PROGRAM/PRODUCT/PERSON.
  - target MUST be MARKET and targetCanonicalPath MUST start with /market/.
  - For ORGANIZATION ↔ MARKET pairs, use PARTICIPATES_IN (ORG -> MARKET). GOV entities (/gov/...) may also use REGULATES (domestic oversight), CONTROLS_ACCESS (sanctions/restrictions), or INFLUENCES.
  - When CURRENT FRAGMENT is MARKET, use explicit sourceCanonicalPath/sourceType/sourceName for participant -> THIS MARKET.
- REGULATES/ADMINISTERS must originate from regulator/admin source (/gov/...), not from affected market/entity. REGULATES is for same-jurisdiction oversight only; for sanctions use CONTROLS_ACCESS.
- For any relation where semantic source is not CURRENT FRAGMENT, explicit sourceCanonicalPath/sourceType/sourceName is REQUIRED.

WHAT TO FIND:
1. Policy instruments and mechanisms (tax rate changes, exemptions, compliance controls, enforcement mechanisms).
2. Regulator/program authority chains (who sets/administers policy; what is in scope).
3. Exposure links to affected segments (markets, organizations, communities, geographies).
4. Influence channels suitable for propagation modeling (formal and indirect channels).
5. QUANTITATIVE QUALIFIERS (CRITICAL for scenario modeling — provide best estimates even when uncertain):
   - For REGULATES/CONTROLS_ACCESS/IMPOSES_CONDITION edges: qualifiers.rate_from, qualifiers.rate_to, qualifiers.effective_date when the instrument has numeric parameters.
   - For exposure edges (to markets/orgs/communities): qualifiers.pass_through_estimate (0.0-1.0: share of cost passed downstream),
     qualifiers.elasticity_hint ("inelastic"|"unit_elastic"|"elastic"|"unknown"),
     qualifiers.tax_base_coverage ("full"|"partial"|"exempt"),
     qualifiers.sector_sensitivity ("low"|"medium"|"high").
   - For INFLUENCES edges: qualifiers.channel_type ("formal_regulation"|"price_signal"|"behavioral"|"substitution"|"evasion"),
     qualifiers.lag_quarters (estimated delay in quarters before effect materializes),
     qualifiers.observability ("direct"|"proxy"|"modeled").
6. Observability metadata: for each major impact channel, note HOW the effect would be measured
   (e.g., national statistics agency data, customs declarations, tax collection reports).
   Add qualifiers.measurement_lag_months and qualifiers.measurement_confidence (0.0-1.0) when estimable.

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

For relations where SOURCE is not the current fragment, set sourceCanonicalPath, sourceType, sourceName.
%s

Return ONLY valid JSON. No markdown.`, taxonomy.EntityTypesForDomainMode(taxonomy.DomainModePolicyImpact), taxonomy.ExtendedCanonicalPathRulesPrompt(), existingEntitiesBlock, researchPreferExistingEntitiesBlock(), objectQualifierContractBlock, relationTypesPromptForDomainMode(taxonomy.DomainModePolicyImpact), string(fragmentJSON), questions, strategyMetricValueFormatBlock())
}

func (a *ResearchAgent) buildEcosystemEnrichmentPrompt(input ResearchStrategyInput) string {
	frag := input.Fragment
	if frag == nil {
		frag = &models.HorizonFragment{}
	}
	fragmentJSON, _ := json.MarshalIndent(frag, "", "  ")
	questions := strings.Join(input.MissingInfo, "\n- ")
	if questions == "" {
		questions = "Map all actors in this ecosystem, their roles, mutual relationships, power dynamics, and dependencies."
	}
	existingEntitiesBlock := buildResearchExistingEntitiesBlock(input.ExistingEntities)
	objectQualifierContractBlock := buildObjectQualifierContractBlockWithHints(frag.CanonicalPath, frag.Type, frag.Qualifiers, frag.Identifiers, frag.Labels)
	return fmt.Sprintf(`You are a research agent. Enrich the entity for ECOSYSTEM/STAKEHOLDER MAPPING context.

ENTITY TYPES in ecosystem context: %s. Use correct targetType for each relation.

CANONICAL PATH RULES (targetCanonicalPath, sourceCanonicalPath MUST follow):
%s

%s
%s
%s

Use ONLY these relation types: %s.
Do NOT use generic relation types outside this list.
STRICT DIRECTION/TYPING RULES (MANDATORY):
- Domain is config, not a graph node: targetCanonicalPath MUST NOT start with /domain/.
- PARTICIPATES_IN direction is participant -> market. source MUST be ORGANIZATION/FACILITY/COMMUNITY/PROGRAM/PRODUCT/PERSON. target MUST be MARKET.
- OWNS direction is owner -> owned.
- SUBSIDIARY_OF direction is subsidiary -> parent.
- REGULATES direction is regulator -> regulated (same-jurisdiction domestic oversight only; for sanctions/restrictions use CONTROLS_ACCESS or IMPOSES_CONDITION).
- INFLUENCES direction is influencer -> influenced.
- For any relation where semantic source is not CURRENT FRAGMENT, explicit sourceCanonicalPath/sourceType/sourceName is REQUIRED.

WHAT TO FIND:
1. All significant actors in this ecosystem: organizations, government bodies, communities, persons, facilities.
2. Their roles and positions: producers, regulators, investors, competitors, partners, gatekeepers.
3. Mutual relationships: supply links, ownership/control, partnerships, competition, regulatory oversight.
4. Power dynamics: who influences whom, dependencies, bottlenecks, critical nodes.
5. Competitive positions: market shares, dominance, niche roles.
6. Geographic scope: where actors operate and interact.

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

For relations where SOURCE is not the current fragment, set sourceCanonicalPath, sourceType, sourceName.
%s

Return ONLY valid JSON. No markdown.`, taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeEcosystem), taxonomy.ExtendedCanonicalPathRulesPrompt(), existingEntitiesBlock, researchPreferExistingEntitiesBlock(), objectQualifierContractBlock, relationTypesPromptForDomainMode(taxonomy.DomainModeEcosystem), string(fragmentJSON), questions, strategyMetricValueFormatBlock())
}

// EnrichRegulatoryImpactFull performs holistic regulatory impact research in a single LLM call.
// Builds the full impact DAG: decision points, mechanisms, affected segments.
func (a *ResearchAgent) EnrichRegulatoryImpactFull(ctx context.Context, query RegulatoryImpactQuery) ([]DiscoveredRelation, error) {
	if a == nil || a.executor == nil {
		return nil, fmt.Errorf("research agent executor not configured")
	}
	prompt := a.buildRegulatoryImpactFullPrompt(query)
	out, err := a.runResearchPrompt(ctx, prompt, nil)
	if err != nil || out == nil {
		return nil, err
	}
	filteredRelations, _ := FilterDomainRelations(ctx, taxonomy.DomainModePolicyImpact, out.Relations)
	return filteredRelations, nil
}

func (a *ResearchAgent) buildRegulatoryImpactFullPrompt(query RegulatoryImpactQuery) string {
	instrument := strings.TrimSpace(query.Instrument)
	jurisdiction := strings.TrimSpace(query.Jurisdiction)
	if jurisdiction == "" {
		jurisdiction = "global"
	}
	depth := query.Depth
	if depth < 3 || depth > 5 {
		depth = 4
	}
	impactScope := strings.TrimSpace(query.ImpactScope)
	if impactScope == "" {
		impactScope = "economy-wide"
	}
	sectorFilter := strings.Join(query.SectorFilter, ", ")

	var paramBlock string
	if len(query.Parameters) > 0 {
		var parts []string
		for k, v := range query.Parameters {
			parts = append(parts, fmt.Sprintf("%s=%s", k, v))
		}
		paramBlock = "Parameters: " + strings.Join(parts, ", ")
	}

	intent := fmt.Sprintf("instrument=%s, jurisdiction=%s, impactScope=%s, depth=%d", instrument, jurisdiction, impactScope, depth)
	if sectorFilter != "" {
		intent += ", sectorFilter=[" + sectorFilter + "]"
	}
	if paramBlock != "" {
		intent += ", " + paramBlock
	}

	return fmt.Sprintf(`You are a research agent. Build a FULL regulatory impact graph as a HOLISTIC structure.

TASK: Map the full impact graph for: %s
Reason: %s

Use ONLY these relation types: %s.
ENTITY TYPES: %s.

CANONICAL PATH RULES:
%s

%s

BUILD THREE LAYERS:
1. DECISION LAYER: Government entities (GOV) --GATEKEEPS_DECISION/REGULATES(domestic only)/CONTROLS_ACCESS(sanctions)--> DECISION_POINT
2. MECHANISM LAYER: DECISION_POINT --USES_MECHANISM--> MECHANISM; MECHANISM --APPLIES_TO--> MARKET/ORG
3. IMPACT LAYER: MECHANISM --IMPOSES_CONDITION/INFLUENCES--> affected segments/organizations
Note: use REGULATES only for domestic jurisdictional authority. For sanctions, trade bans, or extra-territorial restrictions use CONTROLS_ACCESS or IMPOSES_CONDITION.

QUANTITATIVE QUALIFIERS (CRITICAL):
- For REGULATES/CONTROLS_ACCESS/IMPOSES_CONDITION: qualifiers.rate_from, qualifiers.rate_to, qualifiers.effective_date
- For exposure edges: qualifiers.pass_through_estimate (0.0-1.0), qualifiers.elasticity_hint, qualifiers.sector_sensitivity
- For INFLUENCES: qualifiers.channel_type, qualifiers.lag_quarters, qualifiers.observability
%s

Return a JSON object:
{
  "fragment": null,
  "relations": [ { "targetName", "targetType", "targetCanonicalPath", "relationType", "reason", "confidence", "qualifiers": {}, "sourceCanonicalPath?", "sourceType?", "sourceName?" } ],
  "supplyChains": []
}

For each relation, set sourceCanonicalPath/sourceType/sourceName explicitly.
Chain depth target: %d hops from instrument to furthest affected segment.

Return ONLY valid JSON. No markdown.`, intent, strings.TrimSpace(query.Reason), relationTypesPromptForDomainMode(taxonomy.DomainModePolicyImpact), taxonomy.EntityTypesForDomainMode(taxonomy.DomainModePolicyImpact), taxonomy.ExtendedCanonicalPathRulesPrompt(), researchPreferExistingEntitiesBlock(), strategyMetricValueFormatBlock(), depth)
}

// EnrichGovernanceFull performs holistic governance/ownership research in a single LLM call.
// Builds ownership trees, control layers, and board/people structures.
func (a *ResearchAgent) EnrichGovernanceFull(ctx context.Context, query GovernanceQuery) ([]DiscoveredRelation, error) {
	if a == nil || a.executor == nil {
		return nil, fmt.Errorf("research agent executor not configured")
	}
	prompt := a.buildGovernanceFullPrompt(query)
	out, err := a.runResearchPrompt(ctx, prompt, nil)
	if err != nil || out == nil {
		return nil, err
	}
	filteredRelations, _ := FilterDomainRelations(ctx, taxonomy.DomainModeGovernance, out.Relations)
	return filteredRelations, nil
}

func (a *ResearchAgent) buildGovernanceFullPrompt(query GovernanceQuery) string {
	target := strings.TrimSpace(query.Target)
	targetType := strings.TrimSpace(query.TargetType)
	if targetType == "" {
		targetType = "org"
	}
	direction := strings.TrimSpace(query.Direction)
	if direction == "" {
		direction = "both"
	}
	depth := query.Depth
	if depth < 3 || depth > 6 {
		depth = 4
	}
	geoFilter := strings.Join(query.GeoFilter, ", ")

	intent := fmt.Sprintf("target=%s, targetType=%s, direction=%s, depth=%d, includeBoard=%v", target, targetType, direction, depth, query.IncludeBoard)
	if geoFilter != "" {
		intent += ", geoFilter=[" + geoFilter + "]"
	}

	boardBlock := ""
	if query.IncludeBoard {
		boardBlock = `
BOARD/PEOPLE LAYER:
- PERSON --BOARD_MEMBER_OF--> ORGANIZATION
- PERSON --EMPLOYED_BY--> ORGANIZATION (for C-suite / key executives)
- Include key persons: CEO, CFO, board chairman, major shareholders (individuals).`
	}

	return fmt.Sprintf(`You are a research agent. Build a FULL governance/ownership structure as a HOLISTIC graph.

TASK: Map the ownership and control structure for: %s
Reason: %s

Use ONLY these relation types: %s.
ENTITY TYPES: %s.

CANONICAL PATH RULES:
%s

%s

BUILD THESE LAYERS:
1. OWNERSHIP TREE (%s):
   - UBO (Ultimate Beneficial Owner) --OWNS--> Holding --OWNS--> Subsidiary
   - ORG --SUBSIDIARY_OF--> Parent
2. CONTROL LAYER:
   - ORG/GOV --CONTROLS/MANAGES--> ORG
3. INVESTMENT:
   - ORG --INVESTS_IN--> ORG
%s

QUALIFIERS on OWNS edges: qualifiers.share (ownership percentage 0.0-1.0), qualifiers.share_type (direct|indirect|beneficial).
QUALIFIERS on CONTROLS edges: qualifiers.control_type (operational|strategic|regulatory), qualifiers.mechanism (board_majority|golden_share|veto_right|...).

Return a JSON object:
{
  "fragment": null,
  "relations": [ { "targetName", "targetType", "targetCanonicalPath", "relationType", "reason", "confidence", "qualifiers": {}, "sourceCanonicalPath?", "sourceType?", "sourceName?" } ],
  "supplyChains": []
}

For each relation, set sourceCanonicalPath/sourceType/sourceName explicitly.
Chain depth target: %d levels from target entity.

Return ONLY valid JSON. No markdown.`, intent, strings.TrimSpace(query.Reason), relationTypesPromptForDomainMode(taxonomy.DomainModeGovernance), taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeGovernance), taxonomy.ExtendedCanonicalPathRulesPrompt(), researchPreferExistingEntitiesBlock(), direction, boardBlock, depth)
}

// EnrichEcosystemFull performs holistic ecosystem mapping in a single LLM call.
// Discovers all significant actors and their multi-dimensional relationships.
func (a *ResearchAgent) EnrichEcosystemFull(ctx context.Context, query EcosystemQuery) ([]DiscoveredRelation, error) {
	if a == nil || a.executor == nil {
		return nil, fmt.Errorf("research agent executor not configured")
	}
	prompt := a.buildEcosystemFullPrompt(query)
	out, err := a.runResearchPrompt(ctx, prompt, nil)
	if err != nil || out == nil {
		return nil, err
	}
	filteredRelations, _ := FilterDomainRelations(ctx, taxonomy.DomainModeEcosystem, out.Relations)
	return filteredRelations, nil
}

func (a *ResearchAgent) buildEcosystemFullPrompt(query EcosystemQuery) string {
	focus := strings.TrimSpace(query.Focus)
	focusType := strings.TrimSpace(query.FocusType)
	if focusType == "" {
		focusType = "sector"
	}
	depth := query.Depth
	if depth < 3 || depth > 5 {
		depth = 4
	}
	actorTypes := strings.Join(query.ActorTypes, ", ")
	if actorTypes == "" {
		actorTypes = "ORGANIZATION, GOV, PERSON, FACILITY"
	}
	sectorFilter := strings.Join(query.SectorFilter, ", ")
	geoFilter := strings.Join(query.GeoFilter, ", ")

	intent := fmt.Sprintf("focus=%s, focusType=%s, depth=%d, actorTypes=[%s]", focus, focusType, depth, actorTypes)
	if sectorFilter != "" {
		intent += ", sectorFilter=[" + sectorFilter + "]"
	}
	if geoFilter != "" {
		intent += ", geoFilter=[" + geoFilter + "]"
	}

	return fmt.Sprintf(`You are a research agent. Map a FULL ecosystem as a HOLISTIC multi-actor relationship graph.

TASK: Map all actors and relationships in ecosystem: %s
Reason: %s

Use ONLY these relation types: %s.
ENTITY TYPES: %s.
Target actor types: %s.

CANONICAL PATH RULES:
%s

%s

DISCOVER AND MAP:
1. ALL significant actors: producers, consumers, regulators, investors, competitors, partners, gatekeepers.
2. MULTI-DIMENSIONAL relationships between actors:
   - Supply: SUPPLIES_TO, DEPENDS_ON
   - Ownership/Control: OWNS, SUBSIDIARY_OF, CONTROLS, INVESTS_IN
   - Competition: COMPETES_WITH
   - Cooperation: PARTNERS_WITH
   - Regulation: REGULATES (domestic jurisdiction), CONTROLS_ACCESS (sanctions/restrictions)
   - Influence: INFLUENCES
   - Market: PARTICIPATES_IN
   - Geographic: LOCATED_IN, OPERATES_IN
3. Power dynamics: who are the key influencers, gatekeepers, and bottleneck nodes?
4. Competitive positions: market shares, dominance, niche roles where known.

Return a JSON object:
{
  "fragment": null,
  "relations": [ { "targetName", "targetType", "targetCanonicalPath", "relationType", "reason", "confidence", "qualifiers": {}, "sourceCanonicalPath?", "sourceType?", "sourceName?" } ],
  "supplyChains": []
}

For each relation, set sourceCanonicalPath/sourceType/sourceName explicitly.
Aim for %d levels of relationship depth from the focal entity/area.

Return ONLY valid JSON. No markdown.`, intent, strings.TrimSpace(query.Reason), relationTypesPromptForDomainMode(taxonomy.DomainModeEcosystem), taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeEcosystem), actorTypes, taxonomy.ExtendedCanonicalPathRulesPrompt(), researchPreferExistingEntitiesBlock(), depth)
}

// EnrichAffectedZoneFull performs holistic affected-zone discovery in a single LLM call.
// Discovers zones across condition types (military_conflict, natural_disaster, etc.) for the given geo.
func (a *ResearchAgent) EnrichAffectedZoneFull(ctx context.Context, query AffectedZoneQuery) ([]DiscoveredRelation, error) {
	if a == nil || a.executor == nil {
		return nil, fmt.Errorf("research agent executor not configured")
	}
	prompt := a.buildAffectedZoneFullPrompt(query)
	out, err := a.runResearchPrompt(ctx, prompt, nil)
	if err != nil || out == nil {
		return nil, err
	}
	// AFFECTS is considered generic/strategic. We can use Strategic constraints or leave it unfiltered.
	// We'll leave it unfiltered as it's a specialized edge type not bound to standard domains.
	return out.Relations, nil
}

func (a *ResearchAgent) buildAffectedZoneFullPrompt(query AffectedZoneQuery) string {
	geo := strings.TrimSpace(query.Geo)
	if geo == "" {
		geo = "global"
	}
	reason := strings.TrimSpace(query.Reason)
	if reason == "" {
		reason = "Discover affected zones for risk and exposure analysis"
	}
	title := strings.TrimSpace(query.Title)
	if title != "" {
		reason = title + ". " + reason
	}
	return fmt.Sprintf(`You are a research agent. Discover AFFECTED_ZONE entities and their FULL related graph in a HOLISTIC single query (like supply-chain: complete picture in one pass).

TASK: Find all significant affected zones in geo: %s and for EACH zone discover related entities.
Reason: %s

%s

PHASE 1 — Discover zones (5-15 across condition types):
- military_conflict (conflict zones, disputed territories)
- natural_disaster (earthquake_affected, flood_zone, wildfire, etc.)
- political_tension (unrest, sanctions-affected areas)
- humanitarian_crisis (displacement, famine, disease outbreak zones)

PHASE 2 — For EACH zone, discover related entities in the SAME holistic pass:
- LOCATED_IN: zone → GEO_REGION (parent geo where zone is located). Set sourceCanonicalPath=zone path, sourceType=AFFECTED_ZONE.
- AFFECTS: zone → FACILITY (affected plants, ports, warehouses), ORGANIZATION (affected companies, NGOs, gov bodies), PRODUCT (affected commodities), PROGRAM (humanitarian programs). Set sourceCanonicalPath=zone path.
- TRIGGERED_BY: zone → EVENT (earthquake, flood, conflict event that caused the zone). Set sourceCanonicalPath=zone path.

PHASE 2b — For military_conflict zones ONLY:
- Sides: AFFECTS zone → ORGANIZATION/GOV/COMMUNITY (combatants). qualifiers: conflictRole=combatant, conflictSide=side_a|side_b.
- Force strength: qualifiers.forceStrengthEstimate ("major"|"minor"|"paramilitary"|"state" when known).
- Resources used: AFFECTS zone → PRODUCT (weapons, equipment). qualifiers: conflictRole=resource_used.
- External support: AFFECTS zone → ORGANIZATION/GOV (backers). qualifiers: conflictRole=external_supporter, supportsSide=side_a|side_b.
Discover at least 2 sides, forces, key resources, 1-3 external supporters per side when evidence exists.

PHASE 2c — For natural_disaster zones (earthquake_affected, flood_zone, wildfire, etc.) ONLY:
- Damaged assets: AFFECTS zone → FACILITY (damaged plants, ports, hospitals, power), ORGANIZATION (affected companies). qualifiers: disasterRole=affected, damageScope ("partial"|"severe"|"destroyed" when known).
- Response actors: AFFECTS zone → ORGANIZATION/GOV/PROGRAM (relief agencies, first responders, gov disaster units). qualifiers: disasterRole=responder.
- Critical infrastructure at risk: AFFECTS zone → FACILITY (key sites). qualifiers: disasterRole=critical_infrastructure.
Discover 2-5 damaged/at-risk entities and 1-3 response actors per zone when evidence exists.

PHASE 2d — For political_tension zones (unrest, sanctions-affected) ONLY:
- Parties: AFFECTS zone → ORGANIZATION/GOV/COMMUNITY (sides to tension, protestors, opposition). qualifiers: tensionRole=party, tensionSide=side_a|side_b.
- Sanctions targets: AFFECTS zone → ORGANIZATION (entities under sanctions). qualifiers: tensionRole=sanctions_target.
- Mediators: AFFECTS zone → ORGANIZATION/GOV (mediators, observers). qualifiers: tensionRole=mediator.
- Disputed assets: AFFECTS zone → FACILITY/PRODUCT (disputed resources, assets). qualifiers: tensionRole=disputed.
Discover parties, sanctions targets when applicable, and mediators when evidence exists.

PHASE 2e — For humanitarian_crisis zones (displacement, famine, disease outbreak) ONLY:
- Affected populations: AFFECTS zone → GEO_REGION (displacement destinations, refugee host areas). qualifiers: crisisRole=displacement_flow.
- Relief actors: AFFECTS zone → PROGRAM/ORGANIZATION (NGOs, UN agencies, donors). qualifiers: crisisRole=relief_actor.
- Critical needs: AFFECTS zone → PRODUCT (food, medicine, shelter, water). qualifiers: crisisRole=critical_need.
- Access control: AFFECTS zone → ORGANIZATION/GOV (who controls access, blockades). qualifiers: crisisRole=access_controller.
Discover 1-3 relief actors, critical needs, and access bottlenecks when evidence exists.

RELATION TYPES: FOCUSES_ON (domain→zone), LOCATED_IN, AFFECTS, TRIGGERED_BY.

CANONICAL PATH RULES:
%s

ZONE PATH: /zone/{geo}/{condition_type}/{slug}. slug MUST be descriptive place/region name (northern-rakhine, gaza-strip, donbas). NEVER "na". When creating zone fragments, include radiusKm (approximate zone radius in km) ONLY when you have reliable, evidence-based data. Be conservative: prefer smaller, verifiable estimates; avoid overly broad interpretations. Do NOT extrapolate or guess—omit radiusKm if uncertain. Other types: /org/, /gov/, /fac/, /loc/, /product/, /program/, /event/ per rules.

%s

Return a JSON object:
{
  "fragment": null,
  "relations": [
    { "relationType": "FOCUSES_ON", "targetType": "AFFECTED_ZONE", "targetName", "targetCanonicalPath", "reason", "confidence", "qualifiers": { "radiusKm": 50 } },
    { "relationType": "LOCATED_IN", "sourceCanonicalPath": "/zone/...", "sourceType": "AFFECTED_ZONE", "sourceName", "targetType": "GEO_REGION", "targetName", "targetCanonicalPath", "reason", "confidence", "qualifiers": { "radiusKm": 25 } },
    { "relationType": "AFFECTS", "sourceCanonicalPath": "/zone/...", "sourceType": "AFFECTED_ZONE", "sourceName", "targetType": "FACILITY|ORGANIZATION|PRODUCT|PROGRAM", "targetName", "targetCanonicalPath", "reason", "confidence" },
    { "relationType": "AFFECTS", "sourceCanonicalPath": "/zone/...", "sourceType": "AFFECTED_ZONE", "targetType": "ORGANIZATION", "targetName", "targetCanonicalPath", "reason", "confidence", "qualifiers": { "conflictRole": "combatant", "conflictSide": "side_a", "forceStrengthEstimate": "major" } },
    { "relationType": "AFFECTS", "sourceCanonicalPath": "/zone/...", "sourceType": "AFFECTED_ZONE", "targetType": "ORGANIZATION", "targetName", "targetCanonicalPath", "reason", "confidence", "qualifiers": { "conflictRole": "external_supporter", "supportsSide": "side_b" } },
    { "relationType": "AFFECTS", "sourceCanonicalPath": "/zone/...", "sourceType": "AFFECTED_ZONE", "targetType": "FACILITY", "targetName", "targetCanonicalPath", "reason", "confidence", "qualifiers": { "disasterRole": "affected", "damageScope": "severe" } },
    { "relationType": "AFFECTS", "sourceCanonicalPath": "/zone/...", "sourceType": "AFFECTED_ZONE", "targetType": "PROGRAM", "targetName", "targetCanonicalPath", "reason", "confidence", "qualifiers": { "disasterRole": "responder" } },
    { "relationType": "AFFECTS", "sourceCanonicalPath": "/zone/...", "sourceType": "AFFECTED_ZONE", "targetType": "ORGANIZATION", "targetName", "targetCanonicalPath", "reason", "confidence", "qualifiers": { "tensionRole": "sanctions_target" } },
    { "relationType": "AFFECTS", "sourceCanonicalPath": "/zone/...", "sourceType": "AFFECTED_ZONE", "targetType": "PRODUCT", "targetName", "targetCanonicalPath", "reason", "confidence", "qualifiers": { "crisisRole": "critical_need" } },
    { "relationType": "TRIGGERED_BY", "sourceCanonicalPath": "/zone/...", "sourceType": "AFFECTED_ZONE", "sourceName", "targetType": "EVENT", "targetName", "targetCanonicalPath", "reason", "confidence" }
  ],
  "supplyChains": []
}

For zone relations (source or target is AFFECTED_ZONE): include qualifiers.radiusKm (numeric, km) ONLY when you have reliable, evidence-based data. Be conservative and verifiable—avoid overly broad interpretations. Omit radiusKm if uncertain.

CRITICAL: For zone→X relations, ALWAYS set sourceCanonicalPath, sourceType, sourceName to the zone. For FOCUSES_ON (domain→zone), source can be omitted.
Discover 5-15 zones and 2-8 related entities per zone (facilities, orgs, events, parent geo). Apply condition-type-specific blocks (2b–2e) per zone: military_conflict→sides/forces/support; natural_disaster→damage/responders; political_tension→parties/sanctions/mediators; humanitarian_crisis→relief/needs/access. Politically neutral, factual descriptions.

Return ONLY valid JSON. No markdown.`, geo, reason, taxonomy.ZoneVsEventDistinctionPrompt(), taxonomy.ExtendedCanonicalPathRulesPrompt(), researchPreferExistingEntitiesBlock())
}

// ---------------------------------------------------------------------------
// Holistic entity research methods (Phase 4 of Unified Post-Processing Pipeline)
// ---------------------------------------------------------------------------

// HolisticResearchInput holds context for holistic entity research.
type HolisticResearchInput struct {
	Fragment      *models.HorizonFragment
	OriginalQuery string
	DomainMode    string
	ChainContext  string
}

// EnrichHolisticSupplyChain performs domain-specific holistic research for an entity
// in the supply-chain domain context.
func (a *ResearchAgent) EnrichHolisticSupplyChain(ctx context.Context, input HolisticResearchInput) (*ResearchStrategyOutput, error) {
	if a == nil || a.executor == nil || input.Fragment == nil {
		return nil, fmt.Errorf("holistic supply-chain research input is incomplete")
	}
	frag := input.Fragment
	fragJSON, _ := json.Marshal(compactFragmentForPrompt(frag))
	prompt := fmt.Sprintf(`You are an analytical research agent enriching entity data for a supply chain analysis.

ANALYTICAL CONTEXT: %q

ENTITY: %s (type: %s, canonical path: %s)
%s

In the context of the analytical query above, provide a COMPREHENSIVE description of this entity's role in the supply chain:
- Upstream suppliers and raw material sources
- Downstream customers and distribution channels
- Key products/services in this context
- Logistics and infrastructure dependencies
- Bottleneck risks and single points of failure
- Market position and competitive alternatives

Use ONLY these relation types: %s
ENTITY TYPES: %s

%s
%s

Return JSON object:
{
  "fragment": { partial fields to merge },
  "relations": [ { "targetName", "targetType", "targetCanonicalPath", "relationType", "reason", "confidence", "sourceCanonicalPath?", "sourceType?", "sourceName?" } ],
  "supplyChains": [ { "nodes": [ { "canonicalPath", "name", "type", "role"? } ], "confidence", "reason", "transportVia"?: ["/fac/us/port-long-beach", "/fac/ae/jebel-ali-port"] } ]
}
Nodes MUST alternate actor→product. Include FACILITY where transformation happens (product→facility→product).

%s

Return ONLY valid JSON. No markdown.`,
		input.OriginalQuery,
		nameForPrompt(frag), frag.Type, frag.CanonicalPath,
		string(fragJSON),
		taxonomy.SupplyChainEdgeTypesString(),
		taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeSupplyChain),
		taxonomy.ExtendedCanonicalPathRulesPrompt(),
		researchPreferExistingEntitiesBlock(),
		strategySupplyChainsJSONContractBlock())
	return a.runResearchPrompt(ctx, prompt, input.Fragment)
}

// EnrichHolisticEcosystem performs domain-specific holistic research for ecosystem context.
func (a *ResearchAgent) EnrichHolisticEcosystem(ctx context.Context, input HolisticResearchInput) (*ResearchStrategyOutput, error) {
	if a == nil || a.executor == nil || input.Fragment == nil {
		return nil, fmt.Errorf("holistic ecosystem research input is incomplete")
	}
	frag := input.Fragment
	fragJSON, _ := json.Marshal(compactFragmentForPrompt(frag))
	prompt := fmt.Sprintf(`You are an analytical research agent enriching entity data for an ecosystem analysis.

ANALYTICAL CONTEXT: %q

ENTITY: %s (type: %s, canonical path: %s)
%s

Describe this entity's position in the ecosystem:
- Key partners, collaborators, and alliances
- Competitors and substitutes
- Dependencies on other actors (technology, resources, infrastructure)
- Influence and power dynamics
- Substitution risks and resilience factors

Use ONLY these relation types: %s
ENTITY TYPES: %s

%s
%s

%s

Return ONLY valid JSON. No markdown.`,
		input.OriginalQuery,
		nameForPrompt(frag), frag.Type, frag.CanonicalPath,
		string(fragJSON),
		relationTypesPromptForDomainMode(taxonomy.DomainModeEcosystem),
		taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeEcosystem),
		taxonomy.ExtendedCanonicalPathRulesPrompt(),
		researchPreferExistingEntitiesBlock(),
		holisticOutputSchemaBlock())
	return a.runResearchPrompt(ctx, prompt, input.Fragment)
}

// EnrichHolisticGovernance performs domain-specific holistic research for governance context.
func (a *ResearchAgent) EnrichHolisticGovernance(ctx context.Context, input HolisticResearchInput) (*ResearchStrategyOutput, error) {
	if a == nil || a.executor == nil || input.Fragment == nil {
		return nil, fmt.Errorf("holistic governance research input is incomplete")
	}
	frag := input.Fragment
	fragJSON, _ := json.Marshal(compactFragmentForPrompt(frag))
	prompt := fmt.Sprintf(`You are an analytical research agent enriching entity data for a governance/ownership analysis.

ANALYTICAL CONTEXT: %q

ENTITY: %s (type: %s, canonical path: %s)
%s

Describe this entity's governance structure and ownership:
- Ownership structure (parent companies, UBOs, shareholders)
- Board composition and key personnel
- Control chains (subsidiaries, controlled entities)
- Beneficial owners and investment relationships
- Governance risks (concentration, opacity, cross-jurisdictional)

Use ONLY these relation types: %s
ENTITY TYPES: %s

%s
%s

%s

Return ONLY valid JSON. No markdown.`,
		input.OriginalQuery,
		nameForPrompt(frag), frag.Type, frag.CanonicalPath,
		string(fragJSON),
		relationTypesPromptForDomainMode(taxonomy.DomainModeGovernance),
		taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeGovernance),
		taxonomy.ExtendedCanonicalPathRulesPrompt(),
		researchPreferExistingEntitiesBlock(),
		holisticOutputSchemaBlock())
	return a.runResearchPrompt(ctx, prompt, input.Fragment)
}

// EnrichHolisticRegulatory performs domain-specific holistic research for regulatory impact context.
func (a *ResearchAgent) EnrichHolisticRegulatory(ctx context.Context, input HolisticResearchInput) (*ResearchStrategyOutput, error) {
	if a == nil || a.executor == nil || input.Fragment == nil {
		return nil, fmt.Errorf("holistic regulatory research input is incomplete")
	}
	frag := input.Fragment
	fragJSON, _ := json.Marshal(compactFragmentForPrompt(frag))
	prompt := fmt.Sprintf(`You are an analytical research agent enriching entity data for a regulatory impact analysis.

ANALYTICAL CONTEXT: %q

ENTITY: %s (type: %s, canonical path: %s)
%s

Describe this entity's relationship with the regulatory context:
- Regulatory instruments affecting this entity
- Decision points and mechanisms this entity participates in
- Compliance status and exposure
- Affected stakeholders and downstream impact
- Regulatory risks and opportunities

Use ONLY these relation types: %s
ENTITY TYPES: %s

%s
%s

%s

Return ONLY valid JSON. No markdown.`,
		input.OriginalQuery,
		nameForPrompt(frag), frag.Type, frag.CanonicalPath,
		string(fragJSON),
		relationTypesPromptForDomainMode(taxonomy.DomainModePolicyImpact),
		taxonomy.EntityTypesForDomainMode(taxonomy.DomainModePolicyImpact),
		taxonomy.ExtendedCanonicalPathRulesPrompt(),
		researchPreferExistingEntitiesBlock(),
		holisticOutputSchemaBlock())
	return a.runResearchPrompt(ctx, prompt, input.Fragment)
}

// EnrichHolisticMarket performs domain-specific holistic research for market context.
func (a *ResearchAgent) EnrichHolisticMarket(ctx context.Context, input HolisticResearchInput) (*ResearchStrategyOutput, error) {
	if a == nil || a.executor == nil || input.Fragment == nil {
		return nil, fmt.Errorf("holistic market research input is incomplete")
	}
	frag := input.Fragment
	fragJSON, _ := json.Marshal(compactFragmentForPrompt(frag))
	prompt := fmt.Sprintf(`You are an analytical research agent enriching entity data for a market analysis.

ANALYTICAL CONTEXT: %q

ENTITY: %s (type: %s, canonical path: %s)
%s

Describe this entity's market position:
- Market share and competitive position
- Pricing power and demand/supply drivers
- Key competitors and alternatives
- Growth trajectory and market outlook
- Barriers to entry and regulatory environment

Use ONLY these relation types: %s
ENTITY TYPES: %s

%s
%s

%s

Return ONLY valid JSON. No markdown.`,
		input.OriginalQuery,
		nameForPrompt(frag), frag.Type, frag.CanonicalPath,
		string(fragJSON),
		relationTypesPromptForDomainMode(taxonomy.DomainModeMarket),
		taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeMarket),
		taxonomy.ExtendedCanonicalPathRulesPrompt(),
		researchPreferExistingEntitiesBlock(),
		holisticOutputSchemaBlock())
	return a.runResearchPrompt(ctx, prompt, input.Fragment)
}

// EnrichHolisticStrategic performs domain-specific holistic research for strategic context.
func (a *ResearchAgent) EnrichHolisticStrategic(ctx context.Context, input HolisticResearchInput) (*ResearchStrategyOutput, error) {
	if a == nil || a.executor == nil || input.Fragment == nil {
		return nil, fmt.Errorf("holistic strategic research input is incomplete")
	}
	frag := input.Fragment
	fragJSON, _ := json.Marshal(compactFragmentForPrompt(frag))
	prompt := fmt.Sprintf(`You are an analytical research agent enriching entity data for a strategic/geopolitical analysis.

ANALYTICAL CONTEXT: %q

ENTITY: %s (type: %s, canonical path: %s)
%s

Describe this entity's strategic position:
- Power dynamics and leverage points
- Geopolitical dependencies and alliances
- Strategic assets and vulnerabilities
- Influence networks and key relationships
- Risk exposure and resilience factors

Use ONLY these relation types: %s
ENTITY TYPES: %s

%s
%s

%s

Return ONLY valid JSON. No markdown.`,
		input.OriginalQuery,
		nameForPrompt(frag), frag.Type, frag.CanonicalPath,
		string(fragJSON),
		relationTypesPromptForDomainMode(taxonomy.DomainModeStrategic),
		taxonomy.EntityTypesForDomainMode(taxonomy.DomainModeStrategic),
		taxonomy.ExtendedCanonicalPathRulesPrompt(),
		researchPreferExistingEntitiesBlock(),
		holisticOutputSchemaBlock())
	return a.runResearchPrompt(ctx, prompt, input.Fragment)
}

// EnrichQueryContextual performs query-contextualized research (Level 2).
func (a *ResearchAgent) EnrichQueryContextual(ctx context.Context, frag *models.HorizonFragment, originalQuery string) (*ResearchStrategyOutput, error) {
	if a == nil || a.executor == nil || frag == nil {
		return nil, fmt.Errorf("query-contextual research input is incomplete")
	}
	fragJSON, _ := json.Marshal(compactFragmentForPrompt(frag))
	prompt := fmt.Sprintf(`You are an analytical research agent enriching entity data.

ANALYTICAL CONTEXT: %q

ENTITY: %s (type: %s, canonical path: %s)
%s

In the context of the analytical query above, provide all relevant information about this entity:
- Key facts, relationships, and dependencies relevant to the query
- Role and significance in the described context
- Connected entities and their relationships
- Risk factors and important data points

%s
%s

%s

Return ONLY valid JSON. No markdown.`,
		originalQuery,
		nameForPrompt(frag), frag.Type, frag.CanonicalPath,
		string(fragJSON),
		taxonomy.ExtendedCanonicalPathRulesPrompt(),
		researchPreferExistingEntitiesBlock(),
		holisticOutputSchemaBlock())
	return a.runResearchPrompt(ctx, prompt, frag)
}

// EnrichEntityTypeHolistic performs entity-type-specific comprehensive research (Level 3).
func (a *ResearchAgent) EnrichEntityTypeHolistic(ctx context.Context, frag *models.HorizonFragment) (*ResearchStrategyOutput, error) {
	if a == nil || a.executor == nil || frag == nil {
		return nil, fmt.Errorf("entity-type holistic research input is incomplete")
	}
	fragJSON, _ := json.Marshal(compactFragmentForPrompt(frag))
	focusAreas := entityTypeHolisticFocusAreas(frag.Type)
	prompt := fmt.Sprintf(`You are an analytical research agent building a comprehensive profile of an entity.

ENTITY: %s (type: %s, canonical path: %s)
%s

Provide a comprehensive profile covering:
%s

%s
%s

%s

Return ONLY valid JSON. No markdown.`,
		nameForPrompt(frag), frag.Type, frag.CanonicalPath,
		string(fragJSON),
		focusAreas,
		taxonomy.ExtendedCanonicalPathRulesPrompt(),
		researchPreferExistingEntitiesBlock(),
		holisticOutputSchemaBlock())
	return a.runResearchPrompt(ctx, prompt, frag)
}

func entityTypeHolisticFocusAreas(ft models.FragmentType) string {
	switch ft {
	case models.FragmentTypeOrganization:
		return `- Ownership structure and parent/subsidiary relationships
- Products and services offered
- Key markets and geographic presence
- Financial overview (revenue, employees, market cap if public)
- Key personnel (CEO, board members)
- Principal facilities and operations`
	case models.FragmentTypeFacility:
		return `- Operator and owner
- Capacity and throughput
- Geographic location and coordinates
- Inputs and outputs (what it processes/produces)
- Connected infrastructure (pipelines, ports, grids)
- Operational status and commissioning date`
	case models.FragmentTypeProduct:
		return `- Major producers and suppliers
- Key consumers and end markets
- Alternative products and substitutes
- Price dynamics and benchmarks
- Position in supply chains
- Quality grades and specifications`
	case models.FragmentTypeGeoRegion:
		return `- Key industries and economic base
- Major organizations and employers
- Infrastructure and logistics
- Regulatory environment
- Demographics and population
- Political structure and governance`
	case models.FragmentTypeDeposit:
		return `- Resource type and classification
- Proven and probable reserves
- Current operators and concession holders
- Extraction methods and technology
- Grade and quality characteristics
- Infrastructure connectivity`
	case models.FragmentTypeCommunity:
		return `- Geographic scope and boundaries
- Demographics and population
- Economic base and key industries
- Political structure and governance
- Key institutions and organizations
- Infrastructure and services`
	case models.FragmentTypeMarket:
		return `- Market size and value
- Growth dynamics and trends
- Key players and market shares
- Barriers to entry
- Regulatory environment
- Demand and supply drivers`
	default:
		return `- Key relationships with other entities
- Role and significance in its domain
- Important data points and metrics
- Risk factors and dependencies
- Geographic presence and scope`
	}
}

func nameForPrompt(frag *models.HorizonFragment) string {
	if frag == nil {
		return ""
	}
	if frag.Labels != nil {
		if en := strings.TrimSpace(frag.Labels["en"]); en != "" {
			return en
		}
	}
	if frag.Identifiers != nil {
		if ln := strings.TrimSpace(frag.Identifiers["legalName"]); ln != "" {
			return ln
		}
		if sn := strings.TrimSpace(frag.Identifiers["shortName"]); sn != "" {
			return sn
		}
	}
	parts := strings.Split(strings.Trim(frag.CanonicalPath, "/"), "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return frag.CanonicalPath
}

func compactFragmentForPrompt(frag *models.HorizonFragment) map[string]interface{} {
	if frag == nil {
		return nil
	}
	m := map[string]interface{}{
		"canonicalPath": frag.CanonicalPath,
		"type":          frag.Type,
	}
	if len(frag.Labels) > 0 {
		m["labels"] = frag.Labels
	}
	if len(frag.Identifiers) > 0 {
		m["identifiers"] = frag.Identifiers
	}
	if frag.Description != "" {
		desc := frag.Description
		if len(desc) > 500 {
			desc = desc[:500] + "..."
		}
		m["description"] = desc
	}
	return m
}
