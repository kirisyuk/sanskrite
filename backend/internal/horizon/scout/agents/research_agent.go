package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/contractsemantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/edgesemantics"
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
	enriched, relations, chains, _, _, err := a.EnrichFragmentWithGrounding(
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
// The returned rawContent is the concatenated raw LLM response text from all strategies;
// callers should append it to aggregatedContent so downstream Critic/Resolver have source material.
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
) (*models.HorizonFragment, []DiscoveredRelation, []SupplyChain, *ai.GroundingEvidence, string, error) {
	if len(missingInfo) == 0 {
		return fragment, nil, nil, nil, "", nil
	}

	normalizedModes := contractsemantics.SortDomainModesForExecution(domainModes)
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
		}
		enriched, relations, chains, groundingEvidence, rawContent, err := a.ResearchPipeline().Enrich(ctx, input, normalizedModes)
		if err != nil {
			return nil, nil, nil, nil, "", err
		}
		if enriched != nil {
			enriched.Labels, enriched.Identifiers = naming.NormalizeFragmentNames(
				enriched.CanonicalPath,
				enriched.Labels,
				enriched.Identifiers,
			)
			applyInferredHomeLocationHint(enriched)
			return enriched, relations, chains, groundingEvidence, rawContent, nil
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
) (*models.HorizonFragment, []DiscoveredRelation, []SupplyChain, *ai.GroundingEvidence, string, error) {
	if len(missingInfo) == 0 {
		return fragment, nil, nil, nil, "", nil
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
		}
		enriched, relations, chains, groundingEvidence, rawContent, err := a.ResearchPipeline().Enrich(ctx, input, []string{taxonomy.DomainModeGeneral})
		if err != nil {
			return nil, nil, nil, nil, "", err
		}
		if enriched != nil {
			enriched.Labels, enriched.Identifiers = naming.NormalizeFragmentNames(
				enriched.CanonicalPath,
				enriched.Labels,
				enriched.Identifiers,
			)
			applyInferredHomeLocationHint(enriched)
			return enriched, relations, chains, groundingEvidence, rawContent, nil
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
) (*models.HorizonFragment, []DiscoveredRelation, []SupplyChain, *ai.GroundingEvidence, string, error) {
	if a == nil || a.executor == nil {
		return fragment, nil, nil, nil, "", nil
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
		return nil, nil, nil, nil, "", fmt.Errorf("failed to enrich fragment with research: %w", err)
	}
	var groundingEvidence *ai.GroundingEvidence
	var rawContent string
	if fullResult != nil {
		groundingEvidence = fullResult.GroundingEvidence
		rawContent = fullResult.Content
	}

	jsonData, err := json.Marshal(result)
	if err != nil {
		return nil, nil, nil, groundingEvidence, rawContent, fmt.Errorf("failed to marshal research result: %w", err)
	}
	fmt.Printf("[ResearchAgent] Raw Result: %s\n", jsonData)

	var res researchResult
	if err := json.Unmarshal(jsonData, &res); err != nil {
		var partialFrag models.HorizonFragment
		if err2 := json.Unmarshal(jsonData, &partialFrag); err2 == nil {
			a.mergeFragment(fragment, &partialFrag)
			return fragment, nil, nil, groundingEvidence, rawContent, nil
		}
		return nil, nil, nil, groundingEvidence, rawContent, fmt.Errorf("failed to unmarshal enriched result: %w", err)
	}

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
	applyInferredHomeLocationHint(fragment)
	normalizeDiscoveredRelationsNames(res.Relations)

	return fragment, res.Relations, res.SupplyChains, groundingEvidence, rawContent, nil
}

// MergeFragmentPartial merges a partial fragment update into the current fragment
// and returns the resulting working fragment.
func (a *ResearchAgent) MergeFragmentPartial(main *models.HorizonFragment, partial *models.HorizonFragment) *models.HorizonFragment {
	if main == nil {
		return partial
	}
	if partial == nil || main == partial {
		return main
	}
	a.mergeFragment(main, partial)
	return main
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
	if partial.Description != "" {
		main.Description = partial.Description
	}
	if partial.HomeLocation != nil {
		if main.HomeLocation == nil {
			main.HomeLocation = &models.Location{}
		}
		mergeResearchHomeLocation(main.HomeLocation, partial.HomeLocation)
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

func mergeResearchHomeLocation(dst, src *models.Location) {
	if dst == nil || src == nil {
		return
	}
	if strings.TrimSpace(dst.Name) == "" && strings.TrimSpace(src.Name) != "" {
		dst.Name = strings.TrimSpace(src.Name)
	}
	if (dst.ISO3166_1 == nil || strings.TrimSpace(*dst.ISO3166_1) == "") &&
		src.ISO3166_1 != nil && strings.TrimSpace(*src.ISO3166_1) != "" {
		v := strings.TrimSpace(*src.ISO3166_1)
		dst.ISO3166_1 = &v
	}
	if (dst.ISO3166_2 == nil || strings.TrimSpace(*dst.ISO3166_2) == "") &&
		src.ISO3166_2 != nil && strings.TrimSpace(*src.ISO3166_2) != "" {
		v := strings.TrimSpace(*src.ISO3166_2)
		dst.ISO3166_2 = &v
	}
	if (dst.ISO6709 == nil || strings.TrimSpace(*dst.ISO6709) == "") &&
		src.ISO6709 != nil && strings.TrimSpace(*src.ISO6709) != "" {
		v := strings.TrimSpace(*src.ISO6709)
		dst.ISO6709 = &v
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
		// Chokepoint dependency discovery for energy/logistics/trade spheres
		if hasChokepointRelevantSphere(spherePaths) {
			focusBlock += "- CRITICAL: For entities in energy/logistics/trade spheres, identify transport chokepoints (Strait of Hormuz, Suez Canal, Strait of Malacca, Bab el-Mandeb, etc.) that this entity depends on. Create DEPENDS_ON edges to these infrastructure nodes with qualifiers: throughputShare (0-1), substitutability (0-1), reroutePenalty (0-1). These qualifiers feed signal propagation physics.\n"
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
		domainBlock += fmt.Sprintf("Use ONLY domain relation types: %s.\n", domainScaffoldRelationTypesPromptString())
		domainBlock += "Target canonicalPath MUST match targetType:\n"
		domainBlock += "  ORGANIZATION->/org or /gov, PERSON->/person, PRODUCT->/product, PROGRAM->/program, MARKET->/market, FACILITY->/fac, GEO_REGION->/loc, EVENT->/event, MEDIA_CHANNEL->/media, COMMUNITY->/community, DOMAIN->/domain, AFFECTED_ZONE->/zone\n"
		domainBlock += "If the user asks about a market, use /market/... as a TARGET (anchor), NOT as a DOMAIN.\n"
		domainBlock += "Do NOT output placeholder names like NA/N-A/Unknown. Use fewer entities instead.\n"
		domainBlock += "For AFFECTED_ZONE: slug MUST be descriptive place/region name (northern-rakhine, gaza-strip, donbas). NEVER \"na\".\n"
		domainBlock += taxonomy.ZoneVsEventDistinctionPrompt() + "\n\n"
	}
	specBlock := buildResearchSpecializationModulesBlock(specialties)
	businessRulesBlock := taxonomy.BusinessTagRulesPrompt()
	// Object detail: from entity_semantics (unified per type).
	profileBlock := entity_semantics.ResearchBlockForPath(fragment.CanonicalPath, fragment.Type, fragment.CommunityType)
	objectTypeTaskBlock := buildObjectTypeTaskSpecializationBlockWithHints(fragment.CanonicalPath, fragment.Type, fragment.CommunityType, fragment.Qualifiers, fragment.Identifiers, fragment.SpherePaths)
	coreBusinessTaxonomyBlock := buildCoreBusinessTaxonomyBlock()
	objectQualifierContractBlock := buildObjectQualifierContractBlockWithHints(fragment.CanonicalPath, fragment.Type, fragment.Qualifiers, fragment.Identifiers, fragment.Labels)
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
   - Geo contract: if you update "homeLocation", use { "name": "city/place", "iso3166_1": "US|AE|..." }.
   - For ORG/FACILITY/GOV: if the official name, slug, source text, or site label contains a clear place clue (e.g. Wuxi, Huizhou, Eilat, Binh Duong, Riyadh), set homeLocation.name to that city/site instead of leaving it empty.
   - Set iso3166_1 ONLY when the country is explicit or strongly corroborated by the source and the place clue.
   - If the place clue and iso3166_1 conflict, correct the ISO2 or omit it; never preserve a contradictory guess in a partial update.
   - If jurisdiction is inferable from official name/place clue/source, resolve it explicitly instead of preserving or introducing /org/na or /gov/na.
2. "relations": An array of DiscoveredRelation objects if you found new dependencies (suppliers, competitors, etc.).
3. "supplyChains": Array of supply chain objects, each with:
   - "nodes": ordered array of {canonicalPath, name, type, role?}
   - "confidence": 0.0-1.0
   - "reason": evidence for this chain
   - "transportVia": include facility/chokepoint path legs when the route is business-relevant and reasonably inferable; do not leave it empty for obvious maritime/pipeline/chokepoint flows unless evidence is genuinely absent

%s

Product Canonicalization:
- If you can resolve a more specific product name/path, update fragment.canonicalPath.
- Prefer /product/{category}/{type}/{stage}/{slug} consistently.
- For services, use category=service and map service domain/subdomain into {type}/{stage}.
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
Use ROLE-FIRST semantics for direction:
- For directed ORG<->ORG relations (OWNS, SUBSIDIARY_OF, INVESTS_IN, ASSURES, CONTROLS), determine source by semantic role first (owner/investor/assurer/controller), not by CURRENT FRAGMENT.
- If CURRENT FRAGMENT is the semantic target, sourceCanonicalPath/sourceType/sourceName is REQUIRED.
- For STRATEGIC edge types, use EXPLICIT SOURCE (sourceCanonicalPath) when THIS FRAGMENT would be wrong as source.
Choose relationType to match the CORRECT semantic direction:

=== PHASE 1 RELATIONS ===
- DEPENDS_ON: source depends on target (use for suppliers, contractors, infrastructure dependencies).
  Example: If CRRC supplies trains for the Astana LRT project, Astana/CTS may DEPEND_ON CRRC.
-  Clarification: use when the source requires the target to operate or deliver outcomes.
-  Do NOT use when the source is the supplier and the target is the customer (use SUPPLIES_TO).
-  For actor↔actor dependencies (ORG/FACILITY/PROGRAM/COMMUNITY/PERSON), include qualifiers.source_role=dependent and qualifiers.target_role=dependency (or supplier/provider).
- SUPPLIES_TO: source supplies goods/services to target (use for customers/recipients).
-  Clarification: commercial flow from supplier -> customer/recipient; not ownership or control.
-  Source MUST be ORGANIZATION or FACILITY or PRODUCT.
-  Target MUST be ORGANIZATION or FACILITY.
-  MARKET MUST NOT be source or target for SUPPLIES_TO.
-  For ORG/FACILITY -> ORG/FACILITY supply links, include qualifiers.source_role=supplier and qualifiers.target_role=customer.
- LOCATED_IN: source is located in target (target MUST be GEO_REGION).
-  Clarification: physical/geographic location only (HQ, plant site, office presence).
- OPERATES_IN: source operates in target (source MUST be ORGANIZATION or PERSON, target MUST be GEO_REGION or FACILITY or DEPOSIT).
-  Clarification: operational activity or facility operation; not just a registered address.
-  MARKET MUST NOT be source for OPERATES_IN.
- REGULATES: source regulates target (source MUST be ORGANIZATION — only orgs can regulate).
   JURISDICTION RULE: REGULATES means ongoing domestic regulatory oversight within the regulator's own jurisdiction (e.g., SEC regulates US-listed companies, BaFin regulates German banks).
   Do NOT use REGULATES for sanctions, trade restrictions, or extra-territorial enforcement against foreign entities. For those, use CONTROLS_ACCESS or IMPOSES_CONDITION instead.
   Example: US Treasury OFAC → Iranian company = CONTROLS_ACCESS (sanctions), NOT REGULATES.
- OWNS: source owns target (source MUST be ORGANIZATION or PERSON - only orgs/persons can own).
  Clarification: ownership flows from the higher/parent entity to the lower/subordinate entity in the corporate hierarchy.
  Use OWNS only when the source is the true owner/controller of the target (parent → subsidiary, owner → asset).
  CRITICAL: NEVER output OWNS where targetType=PERSON.
  CRITICAL: NEVER output OWNS where sourceType=FACILITY/PRODUCT/DEPOSIT.
    Facilities/products/deposits do not own organizations.
  CRITICAL: If evidence says THIS FRAGMENT is owned by X, DO NOT invert semantics and DO NOT output THIS FRAGMENT as owner.
    Keep relationType=OWNS and set explicit sourceCanonicalPath/sourceType/sourceName = X, with targetCanonicalPath = THIS FRAGMENT.
  CRITICAL: NEVER output OWNS from ORGANIZATION to COMMUNITY if target canonicalPath is /community/{geo}/dynasty/{slug} or /community/{geo}/tribe/{slug}.
    Dynasties and tribes own organizations, not the reverse. For "THIS FRAGMENT is owned by dynasty/tribe", set explicit sourceCanonicalPath to the dynasty/tribe and targetCanonicalPath to THIS FRAGMENT.
  Use OWNS ONLY for equity ownership/control relationships (shareholder, stake, holding company, ultimate beneficial owner).
  Do NOT use OWNS for roles/employment/management like CEO, director, founder, chairman; use INFLUENCES if relevant, or omit the relation.
- SUBSIDIARY_OF: source is owned/controlled by target (target can be ORGANIZATION or PERSON; will be normalized to OWNS in graph).
-  Clarification: inverse of OWNS (child -> parent). Use only when evidence explicitly describes the child->parent relation.
- CONTROLS: source controls target (non-equity/indirect control: voting control, state control, contractual control). Use when control is explicit but ownership is unclear.
-  Clarification: control flows from controller to controlled entity (controller → controlled). Authority without clear equity ownership (e.g., state control, voting rights, contractual control).
-  Do NOT use CONTROLS for external audit/certification/inspection oversight; use ASSURES with qualifiers.assurance_type.
- MANAGES: source manages target (formal authority: CEO/executive/director/operator). Do NOT use for informal influence.
-  Clarification: formal management authority or operational responsibility, not ownership.
- EMPLOYED_BY: source (PERSON) is employed by target (ORGANIZATION). Do NOT use for board-only or founder-only relationships.
- BOARD_MEMBER_OF: source (PERSON) is a board member of target (ORGANIZATION).
- FOUNDED: source (PERSON or ORGANIZATION) founded target (ORGANIZATION, PROGRAM, or COMMUNITY). Not ownership.
-  Clarification: historical origin event; does NOT imply ongoing ownership or control.
- INVESTS_IN: source (ORGANIZATION or PERSON) invests in target (ORGANIZATION/PROGRAM/PRODUCT). Not supply-chain.
-  Clarification: funding/minority stake; do not use unless investment is explicit.
-  Direction is strict: investor -> investee.
-  If THIS FRAGMENT is invested in by X, set explicit sourceCanonicalPath/sourceType/sourceName = X and keep targetCanonicalPath = THIS FRAGMENT.
- PARTNERS_WITH: partnership/collaboration (symmetric; direction is not important but still output as source->target).
-  Clarification: peer collaboration (joint venture, MoU, alliance). Not ownership or supply-chain.
- PRODUCES: source produces target (source MUST be ORGANIZATION or FACILITY or DEPOSIT, target MUST be PRODUCT).
  For PRODUCT -> PRODUCT value-chain steps, use TRANSFORMS_INTO (NOT PRODUCES).
-  Clarification: manufacturer/producer -> product output.
- TRANSFORMS_INTO: upstream raw/earlier-stage PRODUCT -> downstream refined/later-stage PRODUCT.
  Direction is strict and MUST follow the value chain: extraction → refining → smelting → processing → manufacturing → assembly.
  Example: copper-ore TRANSFORMS_INTO copper-concentrate TRANSFORMS_INTO copper-anode (NEVER reversed).
  Source = raw input material. Target = processed output material.
- EXTRACTS_FROM: source (FACILITY) extracts from target (DEPOSIT). Use for mines/oil fields/ore bodies.
-  Clarification: extraction relationship (facility/mine -> resource deposit).
- ASSURES: source (ORGANIZATION) provides independent assurance to target (ORG/FACILITY/PROGRAM). Add qualifiers.assurance_type: audit|certification|regulatory|inspection.
-  If THIS FRAGMENT is audited/certified/inspected by X, keep relationType=ASSURES and set explicit sourceCanonicalPath/sourceType/sourceName = X, with targetCanonicalPath = THIS FRAGMENT.
- AUDITED_BY is legacy inverse form. Do NOT emit it; express the same fact via ASSURES with direction assurer -> assuree.
- COMPETES_WITH: market rivalry/competition (symmetric); not collaboration or supply.
- INFLUENCES: soft power, pressure, or indirect impact; do NOT use for formal authority (REGULATES) or ownership/control (OWNS/CONTROLS).
  Source MAY be PERSON. Target MAY be MARKET or SPHERE (SPHERE links are treated as weaker evidence).
- RENAMED_TO: organization legal rename (source MUST be ORGANIZATION, target MUST be ORGANIZATION).
- REBRANDED_TO: organization brand rename/repositioning (source MUST be ORGANIZATION, target MUST be ORGANIZATION).
- LEGAL_SUCCESSOR_OF: legal succession between organizations (source MUST be ORGANIZATION, target MUST be ORGANIZATION).
- MERGED_INTO: source organization merged into target organization.
- SPUN_OFF_FROM: source organization spun off from target organization.
- ACQUIRED_BY: source organization acquired by target organization.
- PARTICIPATES_IN: entity participates in a market (canonical direction: ORG/FACILITY/COMMUNITY/PROGRAM/PERSON -> MARKET).
  Clarification: entity is an active market participant (player).
  - If THIS FRAGMENT is MARKET, you may still emit PARTICIPATES_IN relations to player entities; backend will reverse the edge direction.
  - Put market share in relation.metrics["market_share_percent"] (unit="percent") when known.
- TRADED_IN: product is traded or price-discovered in a market (canonical direction: PRODUCT -> MARKET).
  Use this only for product↔market trading or pricing context. Do NOT use PARTICIPATES_IN for PRODUCT -> MARKET.
- For ORGANIZATION ↔ MARKET pairs, use PARTICIPATES_IN (ORG -> MARKET).
  Exception: GOV entities (/gov/...) may use REGULATES (domestic regulatory oversight only), CONTROLS_ACCESS (sanctions, restrictions), or INFLUENCES toward MARKET targets.
  Never use OPERATES_IN, DEPENDS_ON, or other non-market types between regular ORGANIZATION and MARKET.
  If leadership is known, keep relationType=PARTICIPATES_IN and set qualifiers.marketRole = "leader" | "dominant" (plus market_share metric when available).

=== PHASE 2 STRATEGIC RELATIONS (may require EXPLICIT SOURCE) ===
These edge types have strict direction rules. Use sourceCanonicalPath when THIS FRAGMENT is NOT the correct semantic source.

- GATEKEEPS_DECISION: source MUST be ORG/GOV/COMMUNITY/PERSON, target MUST be DECISION_POINT
  *** CRITICAL: If THIS FRAGMENT is DECISION_POINT ***:
    When answering "Who gatekeeps this decision?", you MUST use explicit source:
    - sourceCanonicalPath = gatekeeper path (e.g., /gov/sa/sama)
    - sourceType = ORGANIZATION (or GOV/COMMUNITY)
    - sourceName = gatekeeper name
    - targetCanonicalPath = THIS FRAGMENT's path (the decision point being gatekept)
    - targetType = DECISION_POINT
    DO NOT output DECISION_POINT as source for GATEKEEPS_DECISION - this is semantically wrong!

- USES_MECHANISM: source MUST be DECISION_POINT/ORG/GOV/COMMUNITY/PROGRAM, target MUST be MECHANISM (/mechanism/...)
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

%s
- ORGANIZATION ↔ MARKET: PARTICIPATES_IN (ORG -> MARKET). GOV entities (/gov/...) may also use REGULATES (domestic oversight only), CONTROLS_ACCESS (sanctions/restrictions), or INFLUENCES.
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
  "relationType": %s,
  "reason": "string",
  "confidence": 0.0,
  "targetCanonicalPath": "string, REQUIRED",
  "validFrom": "YYYY-MM-DD (optional)",
  "validTo": "YYYY-MM-DD (optional)",
  "qualifiers": { "edgeModality": "actual|projected|historical|potential", "eventId": "/event/{slug}" },
  "metrics": { "ownership_percent": { "value": 45.0, "unit": "percent", "confidence": 0.9, "observedAt": "YYYY-MM-DD", "provenance": {"sourceIds":["..."], "method":"extracted"} } }
}

EXPLICIT SOURCE RULES (CRITICAL for strict direction semantics):
Do not assume edges originate FROM THIS FRAGMENT.
For direction-sensitive relations, infer semantic source from roles first.
When semantic source is not THIS FRAGMENT, you MUST use explicit sourceCanonicalPath.

WHEN TO USE sourceCanonicalPath (MANDATORY):

1. OWNS: source MUST be the owner, target MUST be the owned entity.
   - If THIS FRAGMENT is owned by X (e.g., "Who owns this org?"):
     - Set sourceCanonicalPath = X's path (the owner)
     - Set sourceType/sourceName for X
     - Set targetCanonicalPath = THIS FRAGMENT's canonicalPath
     - Set relationType = OWNS
   - Never output reversed OWNS like THIS FRAGMENT -> owner.

1b. INVESTS_IN: source MUST be investor, target MUST be investee.
   - If THIS FRAGMENT is invested in by X:
     - Set sourceCanonicalPath = X's path (the investor)
     - Set sourceType/sourceName for X
     - Set targetCanonicalPath = THIS FRAGMENT's canonicalPath
     - Set relationType = INVESTS_IN
   - Never output reversed INVESTS_IN like THIS FRAGMENT -> investor.

2. GATEKEEPS_DECISION: source MUST be ORG/GOV/COMMUNITY, target MUST be DECISION_POINT
   - If THIS FRAGMENT is DECISION_POINT and you are answering "Who gatekeeps this decision?":
     - Set sourceCanonicalPath = gatekeeper's path (e.g., /gov/sa/sama)
     - Set sourceType = ORGANIZATION or GOV
     - Set sourceName = gatekeeper's name
     - Set targetCanonicalPath = THIS FRAGMENT's canonicalPath (the decision point)
     - Set targetType = DECISION_POINT
   - Examples: { "sourceCanonicalPath": "/gov/sa/sama", "targetCanonicalPath": "/decision/islamic-finance/grant-banking-license", "relationType": "GATEKEEPS_DECISION" }; { "sourceCanonicalPath": "/gov/us/bis", "targetCanonicalPath": "/decision/export-control/approve-euv-export", "relationType": "GATEKEEPS_DECISION" }

3. USES_MECHANISM: source MUST be DECISION_POINT/ORG/GOV/COMMUNITY, target MUST be MECHANISM
   - If THIS FRAGMENT is MECHANISM and you are answering "Who uses this mechanism?":
     - Set sourceCanonicalPath = user's path
     - Set targetCanonicalPath = THIS FRAGMENT's canonicalPath (the mechanism)
   - If THIS FRAGMENT is DECISION_POINT and you are answering "What mechanism does this decision use?":
     - Do NOT set sourceCanonicalPath (default = THIS FRAGMENT is correct)
     - Set targetCanonicalPath = mechanism path

4. APPLIES_TO: source MUST be MECHANISM, target MUST be entity
   - If THIS FRAGMENT is MECHANISM and you are answering "What does this mechanism apply to?":
     - Do NOT set sourceCanonicalPath (default = THIS FRAGMENT is correct)
     - Set targetCanonicalPath = affected entity path
   - If THIS FRAGMENT is an ORG/FACILITY and you are answering "What mechanism applies to this entity?":
     - Set sourceCanonicalPath = mechanism's path
     - Set targetCanonicalPath = THIS FRAGMENT's canonicalPath

5. CONTROLS_ACCESS / IMPOSES_CONDITION:
   - If THIS FRAGMENT is the controlled/affected entity:
     - Set sourceCanonicalPath = controller's path
     - Set targetCanonicalPath = THIS FRAGMENT's canonicalPath

6. CONTROLS: source MUST be ORG/PERSON/COMMUNITY. PRODUCT cannot be source.
   - If THIS FRAGMENT is PRODUCT and you find "controlled by X" or "X controls this product":
     - Set sourceCanonicalPath = X's path (the gov/org)
     - Set targetCanonicalPath = THIS FRAGMENT's canonicalPath (the product)

7. ADMINISTERS: source MUST be ORGANIZATION or GEO_REGION (superior), target MUST be ORG/PROGRAM/GEO_REGION/FACILITY/DEPOSIT. FACILITY/DEPOSIT cannot be source. /org/ cannot administer /gov/.
   - FACILITY cannot be source. If THIS FRAGMENT is FACILITY and you find "administered by X":
     - Set sourceCanonicalPath = X's path (the ministry/gov)
     - Set targetCanonicalPath = THIS FRAGMENT's canonicalPath (the facility)
   - DEPOSIT cannot be source. If THIS FRAGMENT is DEPOSIT and you find "administered by X":
     - Set sourceCanonicalPath = X's path (the ministry/gov)
     - Set targetCanonicalPath = THIS FRAGMENT's canonicalPath (the deposit)
   - /org/ (company) cannot administer /gov/ (government). Gov bodies administer companies, not vice versa.
     - If you find "ministry X administers company Y": source=X (/gov/...), target=Y (/org/...)
     - Never output /org/... as source when target is /gov/...
   - If THIS FRAGMENT is /gov/... or /org/... and you find "administers subordinate Y":
     - Do NOT set sourceCanonicalPath (THIS FRAGMENT is correct source)
     - Set targetCanonicalPath = Y's path (subordinate gov/org/program/facility/deposit)

8. DEPOSIT in supply-chain edges (critical direction handling):
   - DEPOSIT is usually a resource/location node, not a supplier actor for SUPPLIES_TO.
   - If THIS FRAGMENT is DEPOSIT and evidence says "facility X extracts from this deposit":
     - Set sourceCanonicalPath = X facility path
     - Set sourceType = FACILITY
     - Set targetCanonicalPath = THIS FRAGMENT's canonicalPath
     - Set relationType = EXTRACTS_FROM
   - If THIS FRAGMENT is DEPOSIT and evidence says "organization/facility X relies on this deposit":
     - Set sourceCanonicalPath = X path
     - Set targetCanonicalPath = THIS FRAGMENT's canonicalPath
     - Set relationType = DEPENDS_ON
   - Rare exception: if the evidence identifies an upstream resource source but the extracting/processing facility is unknown, you MAY emit DEPOSIT -> ORGANIZATION|FACILITY with relationType SUPPLIES_TO as a temporary shortcut.
     Use this rarely, keep confidence conservative, and prefer explicit FACILITY -> PRODUCT -> ORG/FAC chains whenever available.

WHEN NOT TO USE sourceCanonicalPath:
- For most Phase 1 relations (DEPENDS_ON, SUPPLIES_TO, LOCATED_IN, etc.) the default "FROM THIS FRAGMENT" is correct.
- Exception: use explicit sourceCanonicalPath for OWNS/INVESTS_IN/ASSURES/CONTROLS whenever THIS FRAGMENT is the semantic target.
- When the semantic direction naturally flows FROM THIS FRAGMENT TO target.

ROLE QUALIFIERS (REQUIRED for directed ORG<->ORG):
- For OWNS / SUBSIDIARY_OF / INVESTS_IN / ASSURES / CONTROLS between ORGANIZATION nodes, include:
  qualifiers.source_role and qualifiers.target_role.
- For directional actor↔actor DEPENDS_ON / SUPPLIES_TO, include source_role/target_role as well.
- Allowed examples:
  - OWNS: owner -> owned_entity
  - SUBSIDIARY_OF: subsidiary -> parent_owner
  - INVESTS_IN: investor -> investee
  - ASSURES: assurer -> assuree
  - CONTROLS: controller -> controlled_entity
  - DEPENDS_ON: dependent -> dependency
  - SUPPLIES_TO: supplier -> customer

CRITICAL: Always verify that the resulting edge direction matches EDGE DIRECTION VALIDATION RULES below.

%s

METRIC VALUE FORMAT (IMPORTANT):
- value MUST be a JSON number (expand \"10M\" to 10000000; do NOT include commas)
- unit MUST be lowercase string (or omit)
- confidence MUST be 0..1 (if you only know \"80\", output 0.8)
- observedAt MUST be ISO8601 (\"YYYY-MM-DD\" or RFC3339)

Ensure the "fragment" JSON adheres to the fragment schema for the fields you are updating.
Return ONLY valid JSON. Do not include markdown code fences.`, string(fragmentJSON), chainBlock, focusBlock, existingEntitiesBlock, researchPreferExistingEntitiesBlock(), decompositionBlock, domainBlock, specBlock, businessRulesBlock, profileBlock, questions, nameRulesBlock, objectTypeTaskBlock, coreBusinessTaxonomyBlock, objectQualifierContractBlock, edgesemantics.EdgeDirectionValidationRulesForPrompt(), targetTypeEnum, edgesemantics.AllEdgeTypesForPrompt(), canonicalRulesBlock)
}

func hasChokepointRelevantSphere(spherePaths []string) bool {
	for _, p := range spherePaths {
		p = strings.ToLower(strings.TrimSpace(p))
		if strings.Contains(p, "/sphere/energy") || strings.Contains(p, "/sphere/logistics") || strings.Contains(p, "/sphere/trade") {
			return true
		}
	}
	return false
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
