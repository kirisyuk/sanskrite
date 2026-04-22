package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/contractsemantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/edgesemantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/entity_semantics"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/naming"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
	"strings"
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
		InputContent: normalizeLivePromptTaxonomy(prompt),
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
	if partial.Class != "" {
		main.Class = partial.Class
	}
	if partial.Subtype != "" {
		main.Subtype = partial.Subtype
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
		if main.Class == ah.FragmentClassMarket {
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
	isDomain := fragment != nil && fragment.IsDomainScopeFragment()
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
	if !isDomain && fragment != nil && (fragment.Class == ah.FragmentClassMarket || fragment.Class == ah.FragmentClassProject || strings.HasPrefix(fragment.CanonicalPath, "/market/") || strings.HasPrefix(fragment.CanonicalPath, "/project/")) {
		// Find matching decomposition hint
		var hint *DecompositionHint
		for i := range decompositionHints {
			if decompositionHints[i].AnchorPath == fragment.CanonicalPath {
				hint = &decompositionHints[i]
				break
			}
		}
		if hint != nil {
			decompositionBlock = "MARKET/PROJECT DECOMPOSITION GUIDANCE:\n"
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
			decompositionBlock += "\nYour PRIMARY goal is to DISCOVER relations to concrete entities (orgs, products, sites) that constitute this market/project.\n"
			decompositionBlock += "Use the suggested search queries to find real players. Output them as relations with high confidence (0.7+).\n"
			decompositionBlock += "IMPORTANT: Prefer extracting REAL-WORLD ENTITIES (players) first. If you also find market metrics (TAM/CAGR/HHI/CR4) or market shares, include them in fragment.state / relation.metrics as best-effort.\n\n"
		} else if originalQuery != "" {
			// Fallback: generate decomposition guidance from originalQuery
			decompositionBlock = "MARKET/PROJECT DECOMPOSITION GUIDANCE:\n"
			decompositionBlock += fmt.Sprintf("This is an abstract anchor. The user's original query was: \"%s\"\n", originalQuery)
			decompositionBlock += "Your PRIMARY goal is to DISCOVER relations to concrete entities (orgs, products, sites) that constitute this market/project.\n"
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
		domainBlock += "  org->/org or /gov, person->/person, product->/product, project->/project, market->/market, site->/fac, geo->/loc, evidence->/evidence, media_channel->/media, community->/community, evidence/domain->/domain, zone->/zone\n"
		domainBlock += "If the user asks about a market, use /market/... as a TARGET (anchor), NOT as a DOMAIN.\n"
		domainBlock += "Do NOT output placeholder names like NA/N-A/Unknown. Use fewer entities instead.\n"
		domainBlock += "For zone: slug MUST be descriptive place/region name (northern-rakhine, gaza-strip, donbas). NEVER \"na\".\n"
		domainBlock += taxonomy.ZoneVsEventDistinctionPrompt() + "\n\n"
	}
	specBlock := buildResearchSpecializationModulesBlock(specialties)
	businessRulesBlock := taxonomy.BusinessTagRulesPrompt()
	// Object detail: from entity_semantics (unified per type).
	profileBlock := entity_semantics.ResearchBlockForPath(fragment.CanonicalPath, fragment.Class, fragment.Subtype, fragment.CommunityType)
	objectTypeTaskBlock := buildObjectTypeTaskSpecializationBlockWithHints(fragment.CanonicalPath, fragment.Class, fragment.CommunityType, fragment.Qualifiers, fragment.Identifiers, fragment.SpherePaths)
	coreBusinessTaxonomyBlock := buildCoreBusinessTaxonomyBlock()
	objectQualifierContractBlock := buildObjectQualifierContractBlockWithHints(fragment.CanonicalPath, fragment.Class, fragment.Qualifiers, fragment.Identifiers, fragment.Labels)
	targetTypeEnum := entity_semantics.PromptRelationTargetTypesPipeList()
	nameRulesBlock := naming.PromptEntityNameNormalizationRules()
	canonicalRulesBlock := taxonomy.ExtendedCanonicalPathRulesPrompt() + `
- /segment/{domain}/{vertical}/{geo} for org marketSegments; /media/{platform}/{slug} for media channels
- NEVER use "/discovered/*" (not allowed)
- canonicalPath MUST match expected prefix for targetType; if unsure, still output your best stable canonicalPath
`
	productCanonicalizationBlock := generalResearchProductCanonicalizationBlock()
	nameHistoryBlock := generalResearchNameHistoryBlock()
	relationDirectionBlock := generatedResearchRelationDirectionRulesBlock()
	relationSchemaBlock := generatedDiscoveredRelationSchemaBlock(targetTypeEnum)
	explicitSourceBlock := generatedExplicitSourceRulesBlock()
	roleQualifierBlock := generatedRoleQualifierRulesBlock()

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

%s

%s

%s

%s

OUTPUT BUDGET RULES (CRITICAL):
- Keep fragment.description SHORT (max 500 chars). Prefer omitting description entirely if it risks truncating JSON.
- If you cannot fit everything, prioritize answering the missingInfo questions and producing VALID JSON with closed braces.

%s

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

%s

%s

%s

%s

METRIC VALUE FORMAT (IMPORTANT):
- value MUST be a JSON number (expand \"10M\" to 10000000; do NOT include commas)
- unit MUST be lowercase string (or omit)
- confidence MUST be 0..1 (if you only know \"80\", output 0.8)
- observedAt MUST be ISO8601 (\"YYYY-MM-DD\" or RFC3339)

Ensure the "fragment" JSON adheres to the fragment schema for the fields you are updating.
Return ONLY valid JSON. Do not include markdown code fences.`,
		string(fragmentJSON),
		chainBlock,
		focusBlock,
		existingEntitiesBlock,
		researchPreferExistingEntitiesBlock(),
		decompositionBlock,
		domainBlock,
		specBlock,
		businessRulesBlock,
		profileBlock,
		questions,
		canonicalRulesBlock,
		productCanonicalizationBlock,
		nameHistoryBlock,
		joinPromptBlocks(nameRulesBlock, objectTypeTaskBlock),
		joinPromptBlocks(coreBusinessTaxonomyBlock, objectQualifierContractBlock),
		relationDirectionBlock,
		relationSchemaBlock,
		explicitSourceBlock,
		roleQualifierBlock,
		edgesemantics.EdgeDirectionValidationRulesForPrompt(),
	)
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
