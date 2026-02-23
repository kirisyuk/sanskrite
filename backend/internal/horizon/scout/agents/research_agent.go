package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/entity_semantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/naming"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// ResearchAgent выполняет дообогащение данных через LLM с Search Grounding
type ResearchAgent struct {
	executor *ai.AITaskExecutor
}

// NewResearchAgent создает новый Research Agent
func NewResearchAgent(executor *ai.AITaskExecutor) *ResearchAgent {
	return &ResearchAgent{executor: executor}
}

// Internal struct to parse Research result
type researchResult struct {
	Fragment     map[string]interface{} `json:"fragment,omitempty"`
	Relations    []DiscoveredRelation   `json:"relations,omitempty"`
	SupplyChains []SupplyChain          `json:"supplyChains,omitempty"`
}

// EnrichFragment обогащает HorizonFragment и находит новые связи.
// focus/spherePaths/keywords are optional steering signals for grounded search (they may be non-canonical).
// originalQuery and decompositionHints are additional context for MARKET-aware research.
// existingEntities is a compact list of already-discovered entities in the current session (to avoid duplicates).
// Returns: enriched fragment, discovered relations, discovered supply chains (if supply_chain mode), error
func (a *ResearchAgent) EnrichFragment(
	ctx context.Context,
	fragment *models.HorizonFragment,
	missingInfo []string,
	focus string,
	spherePaths []string,
	keywords []string,
	specialties []string,
	chainContext string,
	originalQuery string,
	decompositionHints []DecompositionHint,
	existingEntities []string,
) (*models.HorizonFragment, []DiscoveredRelation, []SupplyChain, error) {
	enriched, relations, chains, _, err := a.EnrichFragmentWithGrounding(
		ctx,
		fragment,
		missingInfo,
		focus,
		spherePaths,
		keywords,
		specialties,
		chainContext,
		originalQuery,
		decompositionHints,
		existingEntities,
	)
	return enriched, relations, chains, err
}

// EnrichFragmentWithGroundingWithDomainModes runs composable research pipeline by domain modes.
// Empty/GENERAL modes are routed to GeneralResearchStrategy (not monolithic fallback).
func (a *ResearchAgent) EnrichFragmentWithGroundingWithDomainModes(
	ctx context.Context,
	fragment *models.HorizonFragment,
	missingInfo []string,
	focus string,
	spherePaths []string,
	keywords []string,
	specialties []string,
	chainContext string,
	originalQuery string,
	decompositionHints []DecompositionHint,
	existingEntities []string,
	domainModes []string,
	unitPhase models.UnitPipelinePhase,
) (*models.HorizonFragment, []DiscoveredRelation, []SupplyChain, *ai.GroundingEvidence, error) {
	if len(missingInfo) == 0 {
		return fragment, nil, nil, nil, nil
	}

	normalizedModes := taxonomy.NormalizeDomainModes(domainModes)
	if len(normalizedModes) == 0 {
		normalizedModes = []string{taxonomy.DomainModeGeneral}
	}

	if a.executor != nil {
		input := ResearchStrategyInput{
			Fragment:           fragment,
			MissingInfo:        missingInfo,
			Focus:              focus,
			SpherePaths:        spherePaths,
			Keywords:           keywords,
			Specialties:        specialties,
			ChainContext:       chainContext,
			OriginalQuery:      originalQuery,
			DecompositionHints: decompositionHints,
			ExistingEntities:   existingEntities,
			UnitPipelinePhase:  unitPhase,
		}
		enriched, relations, chains, groundingEvidence, err := a.ResearchPipeline().Enrich(ctx, input, normalizedModes)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		if enriched != nil {
			enriched.Labels, enriched.Identifiers = naming.NormalizeFragmentNames(
				enriched.CanonicalPath,
				enriched.Labels,
				enriched.Identifiers,
			)
			return enriched, relations, chains, groundingEvidence, nil
		}
	}
	return a.enrichFragmentWithGroundingMonolith(ctx, fragment, missingInfo, focus, spherePaths, keywords, specialties, chainContext, originalQuery, decompositionHints, existingEntities)
}

// EnrichFragmentWithGrounding returns enriched data plus grounding evidence.
// Primary path uses composable GeneralResearchStrategy; monolithic prompt is retained as fallback.
func (a *ResearchAgent) EnrichFragmentWithGrounding(
	ctx context.Context,
	fragment *models.HorizonFragment,
	missingInfo []string,
	focus string,
	spherePaths []string,
	keywords []string,
	specialties []string,
	chainContext string,
	originalQuery string,
	decompositionHints []DecompositionHint,
	existingEntities []string,
) (*models.HorizonFragment, []DiscoveredRelation, []SupplyChain, *ai.GroundingEvidence, error) {
	if len(missingInfo) == 0 {
		return fragment, nil, nil, nil, nil
	}
	if a.executor != nil {
		input := ResearchStrategyInput{
			Fragment:           fragment,
			MissingInfo:        missingInfo,
			Focus:              focus,
			SpherePaths:        spherePaths,
			Keywords:           keywords,
			Specialties:        specialties,
			ChainContext:       chainContext,
			OriginalQuery:      originalQuery,
			DecompositionHints: decompositionHints,
			ExistingEntities:   existingEntities,
			UnitPipelinePhase:  models.UnitPipelinePhaseP0,
		}
		enriched, relations, chains, groundingEvidence, err := a.ResearchPipeline().Enrich(ctx, input, []string{taxonomy.DomainModeGeneral})
		if err != nil {
			return nil, nil, nil, nil, err
		}
		if enriched != nil {
			enriched.Labels, enriched.Identifiers = naming.NormalizeFragmentNames(
				enriched.CanonicalPath,
				enriched.Labels,
				enriched.Identifiers,
			)
			return enriched, relations, chains, groundingEvidence, nil
		}
	}
	return a.enrichFragmentWithGroundingMonolith(ctx, fragment, missingInfo, focus, spherePaths, keywords, specialties, chainContext, originalQuery, decompositionHints, existingEntities)
}

// enrichFragmentWithGroundingMonolith preserves legacy one-shot enrichment prompt behavior as fallback.
func (a *ResearchAgent) enrichFragmentWithGroundingMonolith(
	ctx context.Context,
	fragment *models.HorizonFragment,
	missingInfo []string,
	focus string,
	spherePaths []string,
	keywords []string,
	specialties []string,
	chainContext string,
	originalQuery string,
	decompositionHints []DecompositionHint,
	existingEntities []string,
) (*models.HorizonFragment, []DiscoveredRelation, []SupplyChain, *ai.GroundingEvidence, error) {
	if a == nil || a.executor == nil {
		return fragment, nil, nil, nil, nil
	}

	prompt := a.buildEnrichmentPrompt(fragment, missingInfo, focus, spherePaths, keywords, specialties, chainContext, originalQuery, decompositionHints, existingEntities)

	verifier := ai.NewSimpleJSONVerifier()

	task := &ai.AITask{
		TaskType:     ai.TaskTypeKnowledgeSynthesis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability:        true,
			SearchGroundingCapability:       true, // Включаем Search Grounding
			InternalDataGroundingCapability: true,
			// Важно: позволяем fallback на gemini-2.5-pro (capabilityScore=8),
			// иначе при сбое gemini-3-pro-preview у нас не остаётся подходящих моделей.
			MinCapabilityScore: 7,
			MinContextWindow:   16000,
			// IMPORTANT: grounded research often spends budget on tool calls + thinking; keep enough headroom
			// to avoid truncated JSON (which triggers expensive multi-model fallbacks).
			MaxOutputSize: 16000,
		},
		Verifier: verifier,
	}

	result, fullResult, err := a.executor.ExecuteStructuredTaskFull(ctx, task)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to enrich fragment with research: %w", err)
	}
	var groundingEvidence *ai.GroundingEvidence
	if fullResult != nil {
		groundingEvidence = fullResult.GroundingEvidence
	}

	// Преобразуем результат
	jsonData, err := json.Marshal(result)
	if err != nil {
		return nil, nil, nil, groundingEvidence, fmt.Errorf("failed to marshal research result: %w", err)
	}
	fmt.Printf("[ResearchAgent] Raw Result: %s\n", jsonData)

	var res researchResult
	if err := json.Unmarshal(jsonData, &res); err != nil {
		// Fallback для старого формата (только фрагмент)
		var partialFrag models.HorizonFragment
		if err2 := json.Unmarshal(jsonData, &partialFrag); err2 == nil {
			// Merge only fragment, no relations
			a.mergeFragment(fragment, &partialFrag)
			return fragment, nil, nil, groundingEvidence, nil
		}
		return nil, nil, nil, groundingEvidence, fmt.Errorf("failed to unmarshal enriched result: %w", err)
	}

	// Merge Fragment
	if res.Fragment != nil {
		fragBytes, _ := json.Marshal(res.Fragment)
		var partialFragment models.HorizonFragment
		if err := json.Unmarshal(fragBytes, &partialFragment); err == nil {
			a.mergeFragment(fragment, &partialFragment)
		}
	}
	fragment.Labels, fragment.Identifiers = naming.NormalizeFragmentNames(
		fragment.CanonicalPath,
		fragment.Labels,
		fragment.Identifiers,
	)
	normalizeDiscoveredRelationsNames(res.Relations)

	return fragment, res.Relations, res.SupplyChains, groundingEvidence, nil
}

// mergeFragment merges partial data into the main fragment
func (a *ResearchAgent) mergeFragment(main *models.HorizonFragment, partial *models.HorizonFragment) {
	if main.State == nil {
		main.State = make(map[string]models.MetricValue)
	}
	if main.Identifiers == nil {
		main.Identifiers = make(map[string]string)
	}
	// Merge CanonicalPath: allow Research to upgrade flat/legacy paths to proper canonical form.
	// Only update if partial provides a non-empty, different path that looks more complete.
	if partialCP := strings.TrimSpace(partial.CanonicalPath); partialCP != "" {
		mainCP := strings.TrimSpace(main.CanonicalPath)
		if partialCP != mainCP && isMoreCanonical(mainCP, partialCP) {
			main.CanonicalPath = partialCP
			// Keep semantic parts in sync for downstream selective canonizers.
			if parts, err := taxonomy.ParseCanonicalPathParts(partialCP); err == nil && parts != nil {
				main.CanonicalParts = parts
			}
		}
	}
	if partial.Type != "" {
		main.Type = partial.Type
	}
	if partial.EntityPath != "" {
		main.EntityPath = partial.EntityPath
	}
	if partial.SectorPath != "" {
		main.SectorPath = partial.SectorPath
	}
	if partial.Description != "" {
		main.Description = partial.Description
	}
	if partial.HomeLocation != nil {
		main.HomeLocation = partial.HomeLocation
	}
	// Merge labels without overwriting existing non-empty values
	if partial.Labels != nil {
		if main.Labels == nil {
			main.Labels = make(map[string]string)
		}
		for k, v := range partial.Labels {
			if strings.TrimSpace(v) == "" {
				continue
			}
			if strings.TrimSpace(main.Labels[k]) == "" {
				main.Labels[k] = v
			}
		}
	}
	if partial.State != nil {
		for k, v := range partial.State {
			main.State[k] = v
		}
	}
	// Merge marketSegments (ORG-level multi-valued field): union + dedup, don't clobber.
	if len(partial.MarketSegments) > 0 {
		if main.MarketSegments == nil {
			main.MarketSegments = make([]string, 0, len(partial.MarketSegments))
		}
		seen := map[string]struct{}{}
		for _, s := range main.MarketSegments {
			s = strings.TrimSpace(s)
			if s != "" {
				seen[s] = struct{}{}
			}
		}
		for _, s := range partial.MarketSegments {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if _, ok := seen[s]; ok {
				continue
			}
			seen[s] = struct{}{}
			main.MarketSegments = append(main.MarketSegments, s)
		}
	}
	// Merge orgSize: fill missing/unknown only.
	if strings.TrimSpace(partial.OrgSize) != "" {
		cur := strings.TrimSpace(strings.ToLower(main.OrgSize))
		in := strings.TrimSpace(partial.OrgSize)
		if cur == "" || cur == "na" {
			main.OrgSize = in
		}
	}
	// Merge identifiers conservatively (fill missing only)
	if partial.Identifiers != nil {
		for k, v := range partial.Identifiers {
			if strings.TrimSpace(v) == "" {
				continue
			}
			if strings.TrimSpace(main.Identifiers[k]) == "" {
				main.Identifiers[k] = v
			}
		}
	}
	if len(partial.Tags) > 0 {
		seen := map[string]struct{}{}
		out := make([]string, 0, len(main.Tags)+len(partial.Tags))
		for _, t := range main.Tags {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			out = append(out, t)
		}
		for _, t := range partial.Tags {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			out = append(out, t)
		}
		main.Tags = out
		if main.Type == models.FragmentTypeMarket {
			main.Tags = taxonomy.NormalizeMarketTags(main.Tags)
		}
	}
	if len(partial.KBRefs) > 0 {
		main.KBRefs = append(main.KBRefs, partial.KBRefs...)
	}
	// Merge NameHistory: add entries from partial that don't duplicate existing entries.
	if len(partial.NameHistory) > 0 {
		seen := make(map[string]struct{}, len(main.NameHistory))
		for _, e := range main.NameHistory {
			key := strings.ToLower(strings.TrimSpace(e.Kind)) + "|" + strings.ToLower(strings.TrimSpace(e.Name))
			seen[key] = struct{}{}
		}
		for _, e := range partial.NameHistory {
			name := strings.TrimSpace(e.Name)
			if name == "" {
				continue
			}
			kind := strings.ToLower(strings.TrimSpace(e.Kind))
			if kind == "" {
				kind = "aka"
			}
			key := kind + "|" + strings.ToLower(name)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			main.NameHistory = append(main.NameHistory, models.NameHistoryEntry{
				Name:      name,
				Kind:      kind,
				ValidFrom: e.ValidFrom,
				ValidTo:   e.ValidTo,
			})
		}
	}
}

// isMoreCanonical returns true if newPath is a more complete/canonical version of oldPath.
// This helps decide whether to upgrade a path during merge.
func isMoreCanonical(oldPath, newPath string) bool {
	oldPath = strings.TrimSpace(oldPath)
	newPath = strings.TrimSpace(newPath)
	if newPath == "" || oldPath == newPath {
		return false
	}
	// Empty old path -> any new path is better
	if oldPath == "" {
		return true
	}
	oldParts := strings.Split(strings.Trim(oldPath, "/"), "/")
	newParts := strings.Split(strings.Trim(newPath, "/"), "/")
	// More segments = more hierarchical = better
	if len(newParts) > len(oldParts) {
		return true
	}
	// Product: flat /product/{slug} -> hierarchical /product/{cat}/{type}/{stage}/{slug}
	if strings.HasPrefix(oldPath, "/product/") && strings.HasPrefix(newPath, "/product/") {
		if len(oldParts) == 2 && len(newParts) >= 4 {
			return true
		}
	}
	// Org: /org/na/... -> /org/{iso2}/... (not "na")
	if strings.HasPrefix(oldPath, "/org/na/") && strings.HasPrefix(newPath, "/org/") {
		if len(newParts) >= 3 && newParts[1] != "na" && len(newParts[1]) == 2 {
			return true
		}
	}
	// Legacy /org/{slug} -> /org/{iso2}/{slug}
	if strings.HasPrefix(oldPath, "/org/") && len(oldParts) == 2 && len(newParts) >= 3 {
		return true
	}
	return false
}

// buildEnrichmentPrompt строит промпт для дообогащения фрагмента
func (a *ResearchAgent) buildEnrichmentPrompt(fragment *models.HorizonFragment, missingInfo []string, focus string, spherePaths []string, keywords []string, specialties []string, chainContext string, originalQuery string, decompositionHints []DecompositionHint, existingEntities []string) string {
	fragmentJSON, _ := json.MarshalIndent(fragment, "", "  ")
	questions := strings.Join(missingInfo, "\n- ")
	focus = strings.TrimSpace(focus)
	originalQuery = strings.TrimSpace(originalQuery)
	spheresLine := strings.Join(dedupNonEmptyStrings(spherePaths), ", ")
	keywordsLine := strings.Join(dedupNonEmptyStrings(keywords), ", ")
	specialties = taxonomy.NormalizeSpecialties(specialties)
	isDomain := fragment != nil && (fragment.Type == models.FragmentTypeDomain || strings.HasPrefix(strings.TrimSpace(fragment.CanonicalPath), "/domain/"))
	chainBlock := ""
	if strings.TrimSpace(chainContext) != "" {
		chainBlock = strings.TrimSpace(chainContext) + "\n"
	}
	focusBlock := ""
	if focus != "" || spheresLine != "" || keywordsLine != "" || originalQuery != "" {
		focusBlock = "FOCUS CONTEXT (use this to steer your grounded search; it may include non-canonical keywords):\n"
		if originalQuery != "" {
			focusBlock += fmt.Sprintf("- originalQuery: \"%s\"\n", originalQuery)
		}
		if focus != "" {
			focusBlock += fmt.Sprintf("- focus: %s\n", focus)
		}
		if spheresLine != "" {
			focusBlock += fmt.Sprintf("- spherePaths: %s\n", spheresLine)
		}
		if keywordsLine != "" {
			focusBlock += fmt.Sprintf("- keywords: %s\n", keywordsLine)
		}
		focusBlock += "\n"
	}

	// EXISTING ENTITIES: Compact list of already-discovered entities in this session.
	// This helps avoid duplicates and enables referencing existing entities by their canonical paths.
	existingEntitiesBlock := ""
	if len(existingEntities) > 0 {
		// Limit to 50 entities to avoid prompt bloat
		ents := existingEntities
		if len(ents) > 50 {
			ents = ents[:50]
		}
		existingEntitiesBlock = "EXISTING ENTITIES (already discovered in this session - reference these instead of creating duplicates):\n"
		for _, e := range ents {
			existingEntitiesBlock += fmt.Sprintf("  - %s\n", e)
		}
		existingEntitiesBlock += "\nWhen outputting relations, prefer using these existing paths as targetCanonicalPath if they match your discoveries.\n"
		existingEntitiesBlock += "If an entity with the same name exists, use the EXISTING path to avoid duplicates.\n\n"
	}

	// Add MARKET decomposition block if applicable
	decompositionBlock := ""
	if !isDomain && fragment != nil && (fragment.Type == models.FragmentTypeMarket || fragment.Type == models.FragmentTypeProgram || strings.HasPrefix(fragment.CanonicalPath, "/market/") || strings.HasPrefix(fragment.CanonicalPath, "/program/")) {
		// Find matching decomposition hint
		var hint *DecompositionHint
		for i := range decompositionHints {
			if decompositionHints[i].AnchorPath == fragment.CanonicalPath {
				hint = &decompositionHints[i]
				break
			}
		}
		if hint != nil {
			decompositionBlock = "MARKET/PROGRAM DECOMPOSITION GUIDANCE:\n"
			decompositionBlock += "This is an abstract anchor that should be DECOMPOSED into concrete entities.\n"
			if len(hint.EntityTypes) > 0 {
				decompositionBlock += fmt.Sprintf("- Target entity types: %s\n", strings.Join(hint.EntityTypes, ", "))
			}
			if len(hint.SubMarkets) > 0 {
				decompositionBlock += fmt.Sprintf("- Sub-markets to explore: %s\n", strings.Join(hint.SubMarkets, ", "))
			}
			if len(hint.PlayerCategories) > 0 {
				decompositionBlock += fmt.Sprintf("- Player categories: %s\n", strings.Join(hint.PlayerCategories, ", "))
			}
			if len(hint.SearchQueries) > 0 {
				decompositionBlock += "- Suggested search queries:\n"
				for _, q := range hint.SearchQueries {
					decompositionBlock += fmt.Sprintf("  * %s\n", q)
				}
			}
			decompositionBlock += "\nYour PRIMARY goal is to DISCOVER relations to concrete entities (organizations, products, facilities) that constitute this market/program.\n"
			decompositionBlock += "Use the suggested search queries to find real players. Output them as relations with high confidence (0.7+).\n"
			decompositionBlock += "IMPORTANT: Prefer extracting REAL-WORLD ENTITIES (players) first. If you also find market metrics (TAM/CAGR/HHI/CR4) or market shares, include them in fragment.state / relation.metrics as best-effort.\n\n"
		} else if originalQuery != "" {
			// Fallback: generate decomposition guidance from originalQuery
			decompositionBlock = "MARKET/PROGRAM DECOMPOSITION GUIDANCE:\n"
			decompositionBlock += fmt.Sprintf("This is an abstract anchor. The user's original query was: \"%s\"\n", originalQuery)
			decompositionBlock += "Your PRIMARY goal is to DISCOVER relations to concrete entities (organizations, products, facilities) that constitute this market/program.\n"
			decompositionBlock += "Search for:\n"
			decompositionBlock += "- Major players (companies, banks, service providers)\n"
			decompositionBlock += "- Key products or services in this market\n"
			decompositionBlock += "- Regulatory bodies or governing organizations\n"
			decompositionBlock += "- Notable facilities or infrastructure\n"
			decompositionBlock += "Output discovered entities as relations with confidence >= 0.7.\n"
			decompositionBlock += "IMPORTANT: Prefer extracting REAL-WORLD ENTITIES (players) first. If you also find market metrics (TAM/CAGR/HHI/CR4) or market shares, include them in fragment.state / relation.metrics as best-effort.\n\n"
		}
	}
	domainBlock := ""
	if isDomain {
		domainBlock = "DOMAIN EXPANSION GUIDANCE (CRITICAL OVERRIDE):\n"
		domainBlock += "This fragment is a DOMAIN contract. Your primary goal is to expand it into first-order entities.\n"
		domainBlock += "Use ONLY domain relation types: FOCUSES_ON, INVOLVES_ACTOR, IN_GEO_SCOPE, ABOUT, AFFECTS, DEPENDS_ON.\n"
		domainBlock += "Target canonicalPath MUST match targetType:\n"
		domainBlock += "  ORGANIZATION->/org or /gov, PERSON->/person, PRODUCT->/product, PROGRAM->/program, MARKET->/market, FACILITY->/fac, GEO_REGION->/loc, EVENT->/event, MEDIA_CHANNEL->/media, COMMUNITY->/community, DOMAIN->/domain\n"
		domainBlock += "If the user asks about a market, use /market/... as a TARGET (anchor), NOT as a DOMAIN.\n"
		domainBlock += "Do NOT output placeholder names like NA/N-A/Unknown. Use fewer entities instead.\n\n"
	}
	specBlock := buildResearchSpecializationModulesBlock(specialties)
	businessRulesBlock := taxonomy.BusinessTagRulesPrompt()
	// Object detail: from entity_semantics (unified per type).
	profileBlock := entity_semantics.ResearchBlockForPath(fragment.CanonicalPath, fragment.Type, fragment.CommunityType)
	objectTypeTaskBlock := buildObjectTypeTaskSpecializationBlockWithHints(fragment.CanonicalPath, fragment.Type, fragment.CommunityType, fragment.Qualifiers, fragment.Identifiers)
	coreBusinessTaxonomyBlock := buildCoreBusinessTaxonomyBlock()
	objectQualifierUnitTemplateContractBlock := buildObjectQualifierUnitTemplateContractBlockWithHints(fragment.CanonicalPath, fragment.Type, fragment.Qualifiers, fragment.Identifiers, fragment.Labels)
	targetTypeEnum := entity_semantics.PromptRelationTargetTypesPipeList()
	nameRulesBlock := naming.PromptEntityNameNormalizationRules()
	canonicalRulesBlock := taxonomy.ExtendedCanonicalPathRulesPrompt() + `
- /segment/{domain}/{vertical}/{geo} for org marketSegments; /media/{platform}/{slug} for media channels
- NEVER use "/discovered/*" (not allowed)
- canonicalPath MUST match expected prefix for targetType; if unsure, still output your best stable canonicalPath
`

	return fmt.Sprintf(`You are a research agent. Your task is to find missing information about the provided fragment using your search capabilities.

CURRENT FRAGMENT DATA:
%s

%s
%s
%s
%s
%s
%s
%s
%s
%s
CRITICAL MISSING INFORMATION (QUESTIONS TO ANSWER):
- %s

TASK: Use your search tools to find answers to the missing information questions.
Return a JSON object with fields:
1. "fragment": A JSON object containing ONLY the new or updated fields for the fragment that you found (e.g., "state", "homeLocation").
2. "relations": An array of DiscoveredRelation objects if you found new dependencies (suppliers, competitors, etc.).
3. "supplyChains": Array of supply chain objects, each with:
   - "nodes": ordered array of {canonicalPath, name, type, role?, unitRef?, unitCode?, unitLabel?}
   - "confidence": 0.0-1.0
   - "reason": evidence for this chain

%s

Product Canonicalization:
- If you can resolve a more specific product name/path, update fragment.canonicalPath.
- Prefer /product/{category}/{type}/{stage}/{slug} or /product/service/{domain}/{subdomain}/{slug}.
- If the current path is flat or acronym-like, expand to the full product name and ensure relations use the updated canonical path.
- Examples: rice -> /product/raw/agricultural/extraction/rice; salt -> /product/raw/mineral/extraction/salt; swad (Indian savory snack) -> /product/finished/agricultural/processing/swad; processed/finished goods -> /product/finished/mechanical/assembly/{slug}.
- Product/material naming rules (CRITICAL):
  - Use SINGULAR nouns by default (e.g., "silicon wafer", "battery cell", "lithium carbonate"), not plurals.
  - Use plural ONLY when plural is the natural/standard product term in industry terminology (avoid invented plurals).
  - Do NOT include a company/brand name in product names/paths unless it is clearly the official and commonly used product name.
    - Example: "IBM watsonx" should only be used if sources consistently use that full official name; otherwise use "watsonx".

Name History (ORG/PERSON - for rebrands, renames, formerly-known-as):
If you discover that the entity has former names, previous brand names, or was renamed/rebranded:
- Output fragment.nameHistory as an array of objects: { "name": "Former Name", "kind": "short|legal|brand|aka", "validFrom": "YYYY-MM-DD", "validTo": "YYYY-MM-DD" }
- Kind values: "short" (former short/trade name), "legal" (former legal/official name), "brand" (former brand), "aka" (alias/nickname)
- validFrom/validTo are OPTIONAL (use when you know dates; omit if unknown)
- Example: Tinkoff Bank rebranded to T-Bank → { "name": "Tinkoff Bank", "kind": "short", "validTo": "2024-06-01" }
- Search for: "X formerly known as", "X renamed", "X rebrand", "X previous name"
- Do NOT include the current name in nameHistory.

%s

%s

%s

OUTPUT BUDGET RULES (CRITICAL):
- Keep fragment.description SHORT (max 500 chars). Prefer omitting description entirely if it risks truncating JSON.
- If you cannot fit everything, prioritize answering the missingInfo questions and producing VALID JSON with closed braces.

RELATION DIRECTION RULES (STRICT):
By default, each relation object describes a directed edge FROM THIS FRAGMENT (source) TO targetCanonicalPath (target).
For STRATEGIC edge types, use EXPLICIT SOURCE (sourceCanonicalPath) when THIS FRAGMENT would be wrong as source.
Choose relationType to match the CORRECT semantic direction:

=== PHASE 1 RELATIONS (default: FROM THIS FRAGMENT → target) ===
- DEPENDS_ON: source depends on target (use for suppliers, contractors, infrastructure dependencies).
  Example: If CRRC supplies trains for the Astana LRT project, Astana/CTS may DEPEND_ON CRRC.
-  Clarification: use when the source requires the target to operate or deliver outcomes.
-  Do NOT use when the source is the supplier and the target is the customer (use SUPPLIES_TO).
- SUPPLIES_TO: source supplies goods/services to target (use for customers/recipients).
-  Clarification: commercial flow from supplier -> customer/recipient; not ownership or control.
- LOCATED_IN: source is located in target (target MUST be GEO_REGION).
-  Clarification: physical/geographic location only (HQ, plant site, office presence).
- OPERATES_IN: source operates in target (source MUST be ORGANIZATION, target MUST be GEO_REGION or FACILITY).
-  Clarification: operational activity or facility operation; not just a registered address.
- REGULATES: source regulates target (source MUST be ORGANIZATION - only orgs can regulate).
-  Clarification: formal authority imposing rules/compliance (government or regulator), not soft influence.
- OWNS: source owns target (source MUST be ORGANIZATION or PERSON - only orgs/persons can own).
  Clarification: ownership flows from the higher/parent entity to the lower/subordinate entity in the corporate hierarchy.
  Use OWNS only when the source is the true owner/controller of the target (parent → subsidiary, owner → asset).
  CRITICAL: NEVER output OWNS where targetType=PERSON.
  CRITICAL: NEVER output OWNS where sourceType=FACILITY/PRODUCT/DEPOSIT.
    Facilities/products/deposits do not own organizations. If a facility/product/deposit is owned by an org/person,
    use SUBSIDIARY_OF with the owner as target (will be normalized to OWNS in graph).
  CRITICAL: NEVER output OWNS from ORGANIZATION to COMMUNITY if target canonicalPath is /community/{geo}/dynasty/{slug} or /community/{geo}/tribe/{slug}.
    Dynasties and tribes own organizations, not the reverse. If THIS FRAGMENT (org) is owned by a dynasty/tribe, use SUBSIDIARY_OF from org to community (will be normalized to OWNS with reversed direction).
  If THIS FRAGMENT is owned/controlled by a person or parent org, use SUBSIDIARY_OF with the owner as target (it will be normalized to OWNS).
  Use OWNS ONLY for equity ownership/control relationships (shareholder, stake, holding company, ultimate beneficial owner).
  Do NOT use OWNS for roles/employment/management like CEO, director, founder, chairman; use INFLUENCES if relevant, or omit the relation.
- SUBSIDIARY_OF: source is owned/controlled by target (target can be ORGANIZATION or PERSON; will be normalized to OWNS in graph).
-  Clarification: inverse of OWNS (child -> parent). Use when describing who owns THIS FRAGMENT.
- CONTROLS: source controls target (non-equity/indirect control: voting control, state control, contractual control). Use when control is explicit but ownership is unclear.
-  Clarification: control flows from controller to controlled entity (controller → controlled). Authority without clear equity ownership (e.g., state control, voting rights, contractual control).
- MANAGES: source manages target (formal authority: CEO/executive/director/operator). Do NOT use for informal influence.
-  Clarification: formal management authority or operational responsibility, not ownership.
- EMPLOYED_BY: source (PERSON) is employed by target (ORGANIZATION). Do NOT use for board-only or founder-only relationships.
- BOARD_MEMBER_OF: source (PERSON) is a board member of target (ORGANIZATION).
- FOUNDED: source (PERSON or ORGANIZATION) founded target (ORGANIZATION or PROGRAM). Not ownership.
-  Clarification: historical origin event; does NOT imply ongoing ownership or control.
- INVESTS_IN: source (ORGANIZATION or PERSON) invests in target (ORGANIZATION/PROGRAM/PRODUCT). Not supply-chain.
-  Clarification: funding/minority stake; do not use unless investment is explicit.
- PARTNERS_WITH: partnership/collaboration (symmetric; direction is not important but still output as source->target).
-  Clarification: peer collaboration (joint venture, MoU, alliance). Not ownership or supply-chain.
- PRODUCES: source produces target (source MUST be ORGANIZATION or FACILITY or DEPOSIT, target MUST be PRODUCT).
  For PRODUCT -> PRODUCT value-chain steps, use TRANSFORMS_INTO (NOT PRODUCES).
-  Clarification: manufacturer/producer -> product output.
- EXTRACTS_FROM: source (FACILITY) extracts from target (DEPOSIT). Use for mines/oil fields/ore bodies.
-  Clarification: extraction relationship (facility/mine -> resource deposit).
- ASSURES: source (ORGANIZATION) provides independent assurance to target (ORG/FACILITY/PROGRAM). Add qualifiers.assurance_type: audit|certification|regulatory|inspection.
- AUDITED_BY: source (ORGANIZATION/FACILITY/PROGRAM) is audited by target (ORGANIZATION). Legacy; prefer ASSURES for new data.
- COMPETES_WITH: market rivalry/competition (symmetric); not collaboration or supply.
- INFLUENCES: soft power, pressure, or indirect impact; do NOT use for formal authority (REGULATES) or ownership/control (OWNS/CONTROLS).
  Source MAY be PERSON. Target MAY be MARKET or SPHERE (SPHERE links are treated as weaker evidence).
- RENAMED_TO: organization legal rename (source MUST be ORGANIZATION, target MUST be ORGANIZATION).
- REBRANDED_TO: organization brand rename/repositioning (source MUST be ORGANIZATION, target MUST be ORGANIZATION).
- LEGAL_SUCCESSOR_OF: legal succession between organizations (source MUST be ORGANIZATION, target MUST be ORGANIZATION).
- MERGED_INTO: source organization merged into target organization.
- SPUN_OFF_FROM: source organization spun off from target organization.
- ACQUIRED_BY: source organization acquired by target organization.
- PARTICIPATES_IN: entity participates in a market (canonical direction: ORG/FACILITY/COMMUNITY/PROGRAM/PRODUCT/PERSON -> MARKET).
  Clarification: entity is an active market participant (player).
  - If THIS FRAGMENT is MARKET, you may still emit PARTICIPATES_IN relations to player entities; backend will reverse the edge direction.
  - Put market share in relation.metrics["market_share_percent"] (unit="percent") when known.
- For ORGANIZATION ↔ MARKET pairs, use ONLY PARTICIPATES_IN.
  Never use OPERATES_IN, DEPENDS_ON, CONSUMES, REGULATES, CONTROLS_ACCESS, INFLUENCES, or other relation types between ORGANIZATION and MARKET.
  If leadership is known, keep relationType=PARTICIPATES_IN and set qualifiers.marketRole = "leader" | "dominant" (plus market_share metric when available).

=== PHASE 2 STRATEGIC RELATIONS (may require EXPLICIT SOURCE) ===
These edge types have strict direction rules. Use sourceCanonicalPath when THIS FRAGMENT is NOT the correct semantic source.

- GATEKEEPS_DECISION: source MUST be ORG/GOV/COMMUNITY, target MUST be DECISION_POINT
  *** CRITICAL: If THIS FRAGMENT is DECISION_POINT ***:
    When answering "Who gatekeeps this decision?", you MUST use explicit source:
    - sourceCanonicalPath = gatekeeper path (e.g., /gov/sa/sama)
    - sourceType = ORGANIZATION (or GOV/COMMUNITY)
    - sourceName = gatekeeper name
    - targetCanonicalPath = THIS FRAGMENT's path (the decision point being gatekept)
    - targetType = DECISION_POINT
    DO NOT output DECISION_POINT as source for GATEKEEPS_DECISION - this is semantically wrong!

- USES_MECHANISM: source MUST be DECISION_POINT/ORG/GOV/COMMUNITY, target MUST be MECHANISM (/mechanism/...)
  *** CRITICAL: If THIS FRAGMENT is MECHANISM ***:
    When answering "Who uses this mechanism?", you MUST use explicit source:
    - sourceCanonicalPath = user path (e.g., /gov/sa/sama or /decision/...)
    - targetCanonicalPath = THIS FRAGMENT's path (the mechanism being used)
  If THIS FRAGMENT is DECISION_POINT:
    When answering "What mechanism does this decision use?":
    - No sourceCanonicalPath needed (THIS FRAGMENT is correct source)
    - targetCanonicalPath = mechanism path

- APPLIES_TO: source MUST be MECHANISM, target MUST be entity (ORG/FACILITY/PRODUCT/MARKET)
  *** CRITICAL: If THIS FRAGMENT is ORG/FACILITY/PRODUCT/MARKET ***:
    When answering "What mechanism applies to this entity?", you MUST use explicit source:
    - sourceCanonicalPath = mechanism path
    - targetCanonicalPath = THIS FRAGMENT's path
  If THIS FRAGMENT is MECHANISM:
    - No sourceCanonicalPath needed
    - targetCanonicalPath = affected entity path

- CONTROLS_ACCESS: source MUST be DECISION_POINT/ORG/GOV/COMMUNITY, target MUST be entity
  If THIS FRAGMENT is the controlled entity:
    - Use explicit sourceCanonicalPath = controller path
    - targetCanonicalPath = THIS FRAGMENT's path

- IMPOSES_CONDITION: source MUST be ORG/GOV/COMMUNITY/DECISION_POINT, target MUST be entity
  If THIS FRAGMENT is the affected entity:
    - Use explicit sourceCanonicalPath = imposer path
    - targetCanonicalPath = THIS FRAGMENT's path

EDGE DIRECTION VALIDATION RULES (CRITICAL - edges violating these will be REJECTED or REVERSED):
- REGULATES: source MUST be ORGANIZATION, target can be ORGANIZATION/FACILITY/PRODUCT/GEO_REGION
- OWNS: source MUST be ORGANIZATION or PERSON, target MUST be ORGANIZATION/FACILITY/PRODUCT/DEPOSIT/MEDIA_CHANNEL/COMMUNITY
- CONTROLS: source MUST be ORGANIZATION or PERSON or COMMUNITY, target MUST be ORGANIZATION/FACILITY
- MANAGES: source MUST be ORGANIZATION or PERSON, target MUST be ORGANIZATION/FACILITY/PROGRAM
- EMPLOYED_BY: source MUST be PERSON, target MUST be ORGANIZATION
- BOARD_MEMBER_OF: source MUST be PERSON, target MUST be ORGANIZATION
- FOUNDED: source MUST be PERSON or ORGANIZATION, target MUST be ORGANIZATION/PROGRAM
- INVESTS_IN: source MUST be ORGANIZATION or PERSON, target MUST be ORGANIZATION/PROGRAM/PRODUCT
- PARTNERS_WITH: source/target MUST be ORGANIZATION or FACILITY (symmetric)
- PRODUCES: source MUST be ORGANIZATION or FACILITY or DEPOSIT, target MUST be PRODUCT
- LOCATED_IN: target MUST be GEO_REGION
- OPERATES_IN: source MUST be ORGANIZATION, target MUST be GEO_REGION or FACILITY or DEPOSIT
- SUPPLIES_TO: source and target can be ORGANIZATION or FACILITY
- DEPENDS_ON: source can be DOMAIN/ORGANIZATION/PERSON/MARKET/PROGRAM/FACILITY/PRODUCT/COMMUNITY/MEDIA_CHANNEL/DEPOSIT, target can be DOMAIN/ORGANIZATION/PERSON/MARKET/PROGRAM/FACILITY/PRODUCT/GEO_REGION/MEDIA_CHANNEL/COMMUNITY/DEPOSIT
- ASSURES: source MUST be ORGANIZATION, target MUST be ORGANIZATION or FACILITY or PROGRAM
- AUDITED_BY: source MUST be ORGANIZATION or FACILITY or PROGRAM, target MUST be ORGANIZATION
- RELATIVE_OF: source MUST be PERSON, target MUST be PERSON (symmetric)
- RELATED_TO: source MUST be PERSON, target MUST be PERSON (symmetric)
- RENAMED_TO: source MUST be ORGANIZATION, target MUST be ORGANIZATION
- REBRANDED_TO: source MUST be ORGANIZATION, target MUST be ORGANIZATION
- LEGAL_SUCCESSOR_OF: source MUST be ORGANIZATION, target MUST be ORGANIZATION
- MERGED_INTO: source MUST be ORGANIZATION, target MUST be ORGANIZATION
- SPUN_OFF_FROM: source MUST be ORGANIZATION, target MUST be ORGANIZATION
- ACQUIRED_BY: source MUST be ORGANIZATION, target MUST be ORGANIZATION
- EXTRACTS_FROM: source MUST be FACILITY, target MUST be DEPOSIT
- PARTICIPATES_IN: source MUST be ORGANIZATION or FACILITY or COMMUNITY, target MUST be MARKET
- PARTICIPATES_IN: source MAY also be PRODUCT or PERSON, target MUST be MARKET
- ORGANIZATION ↔ MARKET: relationType MUST be PARTICIPATES_IN only (ORG -> MARKET)
- HAS_MARKET_SEGMENT: source MUST be MARKET, target MUST be MARKET (parent → segment; child path = parent path + one segment)
- ADMINISTERS: source MUST be ORGANIZATION (/gov/...), target MUST be ORGANIZATION (/gov/...) (superior → subordinate)
- OPERATES_VIA: source MUST be COMMUNITY or ORGANIZATION, target MUST be MEDIA_CHANNEL
- MEMBER_OF: source MUST be PERSON, target MUST be COMMUNITY
- AFFILIATED_WITH: source MUST be COMMUNITY or PERSON, target MUST be ORGANIZATION
- HAS_SUBGROUP: source MUST be COMMUNITY, target MUST be COMMUNITY (parent group → narrower sub-group/branch/clan)
- GATEKEEPS_DECISION: source MUST be ORGANIZATION or GOV or COMMUNITY or PERSON, target MUST be DECISION_POINT
- USES_MECHANISM: source MUST be DECISION_POINT or ORGANIZATION or GOV or COMMUNITY or PROGRAM, target MUST be MECHANISM (path starts with /mechanism/)
- CONTROLS_ACCESS: source MUST be DECISION_POINT or ORGANIZATION or GOV or COMMUNITY, target MUST be ORGANIZATION/FACILITY/PRODUCT/MARKET/PROGRAM
- IMPOSES_CONDITION: source MUST be ORGANIZATION or GOV or COMMUNITY or DECISION_POINT, target MUST be ORGANIZATION/FACILITY/PRODUCT
- APPLIES_TO: source MUST be MECHANISM, target MUST be ORGANIZATION/FACILITY/PRODUCT/MARKET
- Domain is config, not a graph node: targetCanonicalPath MUST NOT start with /domain/.
  DOMAIN targets are allowed only for DOMAIN scaffold relations (FOCUSES_ON, INVOLVES_ACTOR, IN_GEO_SCOPE, ABOUT, RELATED_DOMAIN, BROADENS_TO, NARROWS_TO, AFFECTS, DEPENDS_ON with DOMAIN source).

DEPENDS_ON FACETS (qualifiers for granular dependency analysis):
When outputting DEPENDS_ON relations, you SHOULD add a "facet" qualifier to specify the nature of dependency:
- communication: dependency on communication channels/platforms (Telegram, WhatsApp, Signal)
- infrastructure: dependency on physical or digital infrastructure (servers, logistics hubs)
- payment: dependency on payment/financial mechanisms (crypto, hawala, cash corridors)
- distribution: dependency on distribution networks/channels (dealers, couriers)
- logistics: dependency on logistics/transportation (shipping, smuggling routes)
- supply: dependency on supply of materials/goods
- labor: dependency on labor/workforce
- legitimization: dependency on legitimization mechanisms (shell companies, front businesses)
- enforcement: dependency on enforcement/protection mechanisms (corruption, private security)
- regulatory: dependency on regulatory environment/approvals
Format: { "qualifiers": { "facet": "communication", "substitutability": 0.3 } }
- substitutability: 0..1 (0 = irreplaceable, 1 = easily substituted)
- transmission qualifiers for systemic-risk edges (required when available for logistics/supply/infrastructure/distribution):
  - throughput_share: 0..1 (share of total flow routed through this dependency)
  - capacity_constraint: 0..1 (how constrained the route/channel is)
  - reroute_penalty: 0..1 (difficulty of substitution/rerouting)
  - reroute_latency_delta_hours: extra delay in hours under reroute
  - reroute_cost_delta_pct: extra cost percent under reroute
  - if unknown, do not remove relation; keep DEPENDS_ON with facet and explicit uncertainty in reason.

PLATFORM → CHANNEL RESOLUTION (IMPORTANT):
When you see a dependency on a platform (e.g., "uses Telegram", "operates via WhatsApp"):
- DO NOT create a generic dependency on the platform organization (e.g., /org/ae/telegram)
- INSTEAD, try to identify SPECIFIC channels/groups and create:
  1. MEDIA_CHANNEL targets: /media/{platform}/{channel-slug}
  2. OPERATES_VIA edges from source to the specific channels
  3. If specific channels unknown, create a DEPENDS_ON with facet="communication" and note in reason that specific channels need resolution

DiscoveredRelation Schema:
{
  "sourceCanonicalPath": "string (OPTIONAL - see EXPLICIT SOURCE RULES below)",
  "sourceType": "string (OPTIONAL - required when sourceCanonicalPath is set)",
  "sourceName": "string (OPTIONAL - required when sourceCanonicalPath is set)",
  "targetName": "string",
  "targetLegalName": "string",
  "targetShortName": "string",
  "targetType": %s,
  "relationType": "SUPPLIES_TO" | "DEPENDS_ON" | "LOCATED_IN" | "OPERATES_IN" | "REGULATES" | "PRODUCES" | "CONSUMES" | "TRANSFORMS_INTO" | "OWNS" | "SUBSIDIARY_OF" | "CONTROLS" | "CONTROLS_ACCESS" | "IMPOSES_CONDITION" | "GATEKEEPS_DECISION" | "USES_MECHANISM" | "APPLIES_TO" | "MANAGES" | "EMPLOYED_BY" | "BOARD_MEMBER_OF" | "FOUNDED" | "INVESTS_IN" | "PARTNERS_WITH" | "ASSURES" | "AUDITED_BY" | "RELATIVE_OF" | "RELATED_TO" | "RENAMED_TO" | "REBRANDED_TO" | "LEGAL_SUCCESSOR_OF" | "MERGED_INTO" | "SPUN_OFF_FROM" | "ACQUIRED_BY" | "COMPETES_WITH" | "INFLUENCES" | "EXTRACTS_FROM" | "PARTICIPATES_IN" | "HAS_MARKET_SEGMENT" | "HAS_SUBGROUP" | "ADMINISTERS" | "OPERATES_VIA" | "MEMBER_OF" | "AFFILIATED_WITH",
  "reason": "string",
  "confidence": 0.0,
  "targetCanonicalPath": "string, REQUIRED",
  "validFrom": "YYYY-MM-DD (optional)",
  "validTo": "YYYY-MM-DD (optional)",
  "qualifiers": { "edgeModality": "actual|projected|historical|potential", "eventId": "/event/{slug}" },
  "metrics": { "ownership_percent": { "value": 45.0, "unit": "percent", "confidence": 0.9, "observedAt": "YYYY-MM-DD", "provenance": {"sourceIds":["..."], "method":"extracted"} } }
}

EXPLICIT SOURCE RULES (CRITICAL for Phase 2 strategic relations):
By default, edges originate FROM THIS FRAGMENT (the fragment you are enriching).
However, for STRATEGIC EDGE TYPES where the semantic direction REQUIRES a different source, you MUST use explicit sourceCanonicalPath.

WHEN TO USE sourceCanonicalPath (MANDATORY):

1. GATEKEEPS_DECISION: source MUST be ORG/GOV/COMMUNITY, target MUST be DECISION_POINT
   - If THIS FRAGMENT is DECISION_POINT and you are answering "Who gatekeeps this decision?":
     - Set sourceCanonicalPath = gatekeeper's path (e.g., /gov/sa/sama)
     - Set sourceType = ORGANIZATION or GOV
     - Set sourceName = gatekeeper's name
     - Set targetCanonicalPath = THIS FRAGMENT's canonicalPath (the decision point)
     - Set targetType = DECISION_POINT
   - Example: { "sourceCanonicalPath": "/gov/sa/sama", "sourceType": "ORGANIZATION", "sourceName": "SAMA",
               "targetCanonicalPath": "/decision/islamic-finance/grant-banking-license", "targetType": "DECISION_POINT",
               "relationType": "GATEKEEPS_DECISION" }

2. USES_MECHANISM: source MUST be DECISION_POINT/ORG/GOV/COMMUNITY, target MUST be MECHANISM
   - If THIS FRAGMENT is MECHANISM and you are answering "Who uses this mechanism?":
     - Set sourceCanonicalPath = user's path
     - Set targetCanonicalPath = THIS FRAGMENT's canonicalPath (the mechanism)
   - If THIS FRAGMENT is DECISION_POINT and you are answering "What mechanism does this decision use?":
     - Do NOT set sourceCanonicalPath (default = THIS FRAGMENT is correct)
     - Set targetCanonicalPath = mechanism path

3. APPLIES_TO: source MUST be MECHANISM, target MUST be entity
   - If THIS FRAGMENT is MECHANISM and you are answering "What does this mechanism apply to?":
     - Do NOT set sourceCanonicalPath (default = THIS FRAGMENT is correct)
     - Set targetCanonicalPath = affected entity path
   - If THIS FRAGMENT is an ORG/FACILITY and you are answering "What mechanism applies to this entity?":
     - Set sourceCanonicalPath = mechanism's path
     - Set targetCanonicalPath = THIS FRAGMENT's canonicalPath

4. CONTROLS_ACCESS / IMPOSES_CONDITION:
   - If THIS FRAGMENT is the controlled/affected entity:
     - Set sourceCanonicalPath = controller's path
     - Set targetCanonicalPath = THIS FRAGMENT's canonicalPath

WHEN NOT TO USE sourceCanonicalPath:
- For most Phase 1 relations (DEPENDS_ON, SUPPLIES_TO, LOCATED_IN, OWNS, etc.) the default "FROM THIS FRAGMENT" is correct.
- When the semantic direction naturally flows FROM THIS FRAGMENT TO target.

CRITICAL: Always verify that the resulting edge direction matches EDGE DIRECTION VALIDATION RULES below.

%s

METRIC VALUE FORMAT (IMPORTANT):
- value MUST be a JSON number (expand \"10M\" to 10000000; do NOT include commas)
- unit MUST be lowercase string (or omit)
- confidence MUST be 0..1 (if you only know \"80\", output 0.8)
- observedAt MUST be ISO8601 (\"YYYY-MM-DD\" or RFC3339)

Ensure the "fragment" JSON adheres to the fragment schema for the fields you are updating.
Return ONLY valid JSON. Do not include markdown code fences.`, string(fragmentJSON), chainBlock, focusBlock, existingEntitiesBlock, researchPreferExistingEntitiesBlock(), decompositionBlock, domainBlock, specBlock, businessRulesBlock, profileBlock, questions, nameRulesBlock, objectTypeTaskBlock, coreBusinessTaxonomyBlock, objectQualifierUnitTemplateContractBlock, targetTypeEnum, canonicalRulesBlock)
}

func dedupNonEmptyStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
