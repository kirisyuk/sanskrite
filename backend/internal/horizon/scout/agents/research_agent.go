package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
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
	Fragment  map[string]interface{} `json:"fragment,omitempty"`
	Relations []DiscoveredRelation   `json:"relations,omitempty"`
}

// EnrichFragment обогащает HorizonFragment и находит новые связи.
// focus/spherePaths/keywords are optional steering signals for grounded search (they may be non-canonical).
func (a *ResearchAgent) EnrichFragment(
	ctx context.Context,
	fragment *models.HorizonFragment,
	missingInfo []string,
	focus string,
	spherePaths []string,
	keywords []string,
) (*models.HorizonFragment, []DiscoveredRelation, error) {
	if len(missingInfo) == 0 {
		return fragment, nil, nil
	}

	prompt := a.buildEnrichmentPrompt(fragment, missingInfo, focus, spherePaths, keywords)

	verifier := ai.NewSimpleJSONVerifier()

	task := &ai.AITask{
		TaskType:     ai.TaskTypeKnowledgeSynthesis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability:  true,
			SearchGroundingCapability: true, // Включаем Search Grounding
			// Важно: позволяем fallback на gemini-2.5-pro (capabilityScore=8),
			// иначе при сбое gemini-3-pro-preview у нас не остаётся подходящих моделей.
			MinCapabilityScore: 7,
			MinContextWindow:   16000,
			MaxOutputSize:      4096,
		},
		Verifier: verifier,
	}

	result, err := a.executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to enrich fragment with research: %w", err)
	}

	// Преобразуем результат
	jsonData, err := json.Marshal(result)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal research result: %w", err)
	}
	fmt.Printf("[ResearchAgent] Raw Result: %s\n", jsonData)

	var res researchResult
	if err := json.Unmarshal(jsonData, &res); err != nil {
		// Fallback для старого формата (только фрагмент)
		var partialFrag models.HorizonFragment
		if err2 := json.Unmarshal(jsonData, &partialFrag); err2 == nil {
			// Merge only fragment, no relations
			a.mergeFragment(fragment, &partialFrag)
			return fragment, nil, nil
		}
		return nil, nil, fmt.Errorf("failed to unmarshal enriched result: %w", err)
	}

	// Merge Fragment
	if res.Fragment != nil {
		fragBytes, _ := json.Marshal(res.Fragment)
		var partialFragment models.HorizonFragment
		if err := json.Unmarshal(fragBytes, &partialFragment); err == nil {
			a.mergeFragment(fragment, &partialFragment)
		}
	}

	return fragment, res.Relations, nil
}

// mergeFragment merges partial data into the main fragment
func (a *ResearchAgent) mergeFragment(main *models.HorizonFragment, partial *models.HorizonFragment) {
	if main.State == nil {
		main.State = make(map[string]models.MetricValue)
	}
	if main.Identifiers == nil {
		main.Identifiers = make(map[string]string)
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
		main.Tags = append(main.Tags, partial.Tags...)
	}
	if len(partial.KBRefs) > 0 {
		main.KBRefs = append(main.KBRefs, partial.KBRefs...)
	}
}

// buildEnrichmentPrompt строит промпт для дообогащения фрагмента
func (a *ResearchAgent) buildEnrichmentPrompt(fragment *models.HorizonFragment, missingInfo []string, focus string, spherePaths []string, keywords []string) string {
	fragmentJSON, _ := json.MarshalIndent(fragment, "", "  ")
	questions := strings.Join(missingInfo, "\n- ")
	focus = strings.TrimSpace(focus)
	spheresLine := strings.Join(dedupNonEmptyStrings(spherePaths), ", ")
	keywordsLine := strings.Join(dedupNonEmptyStrings(keywords), ", ")
	focusBlock := ""
	if focus != "" || spheresLine != "" || keywordsLine != "" {
		focusBlock = "FOCUS CONTEXT (use this to steer your grounded search; it may include non-canonical keywords):\n"
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

	return fmt.Sprintf(`You are a research agent. Your task is to find missing information about the provided fragment using your search capabilities.

CURRENT FRAGMENT DATA:
%s

%s
CRITICAL MISSING INFORMATION (QUESTIONS TO ANSWER):
- %s

TASK: Use your search tools to find answers to the missing information questions.
Return a JSON object with two fields:
1. "fragment": A JSON object containing ONLY the new or updated fields for the fragment that you found (e.g., "state", "homeLocation").
2. "relations": An array of DiscoveredRelation objects if you found new dependencies (suppliers, competitors, etc.).

Name Normalization Rules:
- For organizations and similar entities, ALWAYS provide:
  - fragment.identifiers.legalName (and/or fragment.labels.en): legal/official name including ownership suffix if relevant (Inc., Ltd., LLC, GmbH, AG, S.A., PLC...)
  - fragment.identifiers.shortName: base name without ownership suffix
- For relations, ALWAYS provide targetLegalName and targetShortName in addition to targetName.

RELATION DIRECTION RULES (STRICT):
Each relation object describes a directed edge FROM THIS FRAGMENT (source) TO targetCanonicalPath (target).
Choose relationType to match this direction:
- DEPENDS_ON: source depends on target (use for suppliers, contractors, infrastructure dependencies).
  Example: If CRRC supplies trains for the Astana LRT project, Astana/CTS may DEPEND_ON CRRC.
- SUPPLIES_TO: source supplies goods/services to target (use for customers/recipients).
- LOCATED_IN: source is located in target (target MUST be GEO_REGION).
- OPERATES_IN: source operates in target (source MUST be ORGANIZATION, target MUST be GEO_REGION or FACILITY).
- REGULATES: source regulates target (source MUST be ORGANIZATION - only orgs can regulate).
- OWNS: source owns target (source MUST be ORGANIZATION or PERSON - only orgs/persons can own).
  CRITICAL: NEVER output OWNS where targetType=PERSON.
  If THIS FRAGMENT is owned/controlled by a person or parent org, use SUBSIDIARY_OF with the owner as target (it will be normalized to OWNS).
  Use OWNS ONLY for equity ownership/control relationships (shareholder, stake, holding company, ultimate beneficial owner).
  Do NOT use OWNS for roles/employment/management like CEO, director, founder, chairman; use INFLUENCES if relevant, or omit the relation.
- SUBSIDIARY_OF: source is owned/controlled by target (target can be ORGANIZATION or PERSON; will be normalized to OWNS in graph).
- CONTROLS: source controls target (non-equity/indirect control: voting control, state control, contractual control). Use when control is explicit but ownership is unclear.
- MANAGES: source manages target (formal authority: CEO/executive/director/operator). Do NOT use for informal influence.
- EMPLOYED_BY: source (PERSON) is employed by target (ORGANIZATION). Do NOT use for board-only or founder-only relationships.
- BOARD_MEMBER_OF: source (PERSON) is a board member of target (ORGANIZATION).
- FOUNDED: source (PERSON or ORGANIZATION) founded target (ORGANIZATION or PROGRAM). Not ownership.
- INVESTS_IN: source (ORGANIZATION or PERSON) invests in target (ORGANIZATION/PROGRAM/PRODUCT). Not supply-chain.
- PARTNERS_WITH: partnership/collaboration (symmetric; direction is not important but still output as source->target).
- PRODUCES: source produces target (source MUST be ORGANIZATION or FACILITY, target MUST be PRODUCT).
- COMPETES_WITH / INFLUENCES: as labeled.

EDGE DIRECTION VALIDATION RULES (CRITICAL - edges violating these will be REJECTED or REVERSED):
- REGULATES: source MUST be ORGANIZATION, target can be ORGANIZATION/FACILITY/PRODUCT/GEO_REGION
- OWNS: source MUST be ORGANIZATION or PERSON, target MUST be ORGANIZATION/FACILITY/PRODUCT
- CONTROLS: source MUST be ORGANIZATION or PERSON, target MUST be ORGANIZATION/FACILITY
- MANAGES: source MUST be ORGANIZATION or PERSON, target MUST be ORGANIZATION/FACILITY/PROGRAM
- EMPLOYED_BY: source MUST be PERSON, target MUST be ORGANIZATION
- BOARD_MEMBER_OF: source MUST be PERSON, target MUST be ORGANIZATION
- FOUNDED: source MUST be PERSON or ORGANIZATION, target MUST be ORGANIZATION/PROGRAM
- INVESTS_IN: source MUST be ORGANIZATION or PERSON, target MUST be ORGANIZATION/PROGRAM/PRODUCT
- PARTNERS_WITH: source/target MUST be ORGANIZATION or FACILITY (symmetric)
- PRODUCES: source MUST be ORGANIZATION or FACILITY, target MUST be PRODUCT
- LOCATED_IN: target MUST be GEO_REGION
- OPERATES_IN: source MUST be ORGANIZATION, target MUST be GEO_REGION or FACILITY
- SUPPLIES_TO: source and target can be ORGANIZATION or FACILITY
- DEPENDS_ON: source can be ORGANIZATION/FACILITY/PRODUCT, target can be ORGANIZATION/FACILITY/PRODUCT/GEO_REGION

DiscoveredRelation Schema:
{
  "targetName": "string",
  "targetLegalName": "string",
  "targetShortName": "string",
  "targetType": "ORGANIZATION" | "FACILITY" | "GEO_REGION" | "PRODUCT" | "PROGRAM" | "MARKET" | "EVENT" | "PERSON",
  "relationType": "SUPPLIES_TO" | "DEPENDS_ON" | "LOCATED_IN" | "OPERATES_IN" | "REGULATES" | "PRODUCES" | "CONSUMES" | "TRANSFORMS_INTO" | "OWNS" | "SUBSIDIARY_OF" | "CONTROLS" | "MANAGES" | "EMPLOYED_BY" | "BOARD_MEMBER_OF" | "FOUNDED" | "INVESTS_IN" | "PARTNERS_WITH" | "COMPETES_WITH" | "INFLUENCES",
  "reason": "string",
  "confidence": 0.0,
  "targetCanonicalPath": "string, REQUIRED"
}

CANONICAL PATH RULES:
- NEVER use "/discovered/*" (not allowed).
- Prefer stable namespaces:
  - Organizations: /org/{lowercase-slug}
  - People: /person/{lowercase-slug}
  - Products: MUST use hierarchical structure /product/{category}/{type}/{stage}/{slug}
    - category: "raw" | "component" | "finished" | "service"
    - type: "mineral" | "agricultural" | "chemical" | "electronic" | "mechanical" | "energy"
    - stage: "extraction" | "refining" | "processing" | "manufacturing" | "assembly"
    - slug: lowercase-slug derived from product name
    - Examples:
      - "Lithium Ore" -> /product/raw/mineral/extraction/lithium-ore
      - "Lithium Carbonate" -> /product/raw/chemical/refining/lithium-carbonate
      - "Battery Cell" -> /product/component/electronic/manufacturing/battery-cell
      - "Electric Vehicle" -> /product/finished/mechanical/assembly/electric-vehicle
    - If category/type/stage cannot be determined, use flat /product/{slug} as fallback
  - Programs:
    - Prefer /program/{jurisdictionSlug}/{programSlug} when jurisdiction is known
    - Otherwise /program/{programSlug}
  - Markets:
    - Prefer /market/{domainSlug}/{marketSlug} when domain is known
    - Otherwise /market/{marketSlug}
  - Facilities:
    - Prefer /fac/{orgSlug}/{facilitySlug} when org can be inferred
    - Otherwise /fac/{facilitySlug}
  - GEO regions / locations: MUST be /loc/{country}/{admin1}/{city}
    - country: ISO3166-1 alpha2 lowercase, EXACTLY 2 characters (e.g., us, de, ru, kz, jp)
    - admin1: subdivision code/slug lowercase; use "na" if not applicable/unknown
    - city: lowercase slug; use "na" if not applicable/unknown
    - country-only locations: /loc/{country}/na/na
    - CRITICAL: country code MUST be valid ISO3166-1 alpha2 (exactly 2 lowercase letters). Invalid codes will be REJECTED.
- VALIDATION POLICY RULES (MUST comply or output will be REJECTED):
  - canonicalPath MUST NOT start with "/discovered/" (will be rejected)
  - canonicalPath MUST match expected prefix for targetType (see namespaces above)
  - GEO_REGION paths MUST have exactly 4 parts: /loc/{country}/{admin1}/{city} with valid country code
  - Facilities: prefer /fac/{orgSlug}/{facilitySlug} over flat /fac/{facilitySlug} (flat paths get penalty)
- canonicalPath slug SHOULD be derived from targetShortName (without ownership suffix) to avoid duplicates when legal form changes.
- ONLY include ownership suffix in canonicalPath if needed to disambiguate between different entities with same shortName.
- If you are unsure, still output your best stable canonicalPath (do not leave it empty).

Ensure the "fragment" JSON adheres to the fragment schema for the fields you are updating.
Return ONLY valid JSON. Do not include markdown code fences.`, string(fragmentJSON), focusBlock, questions)
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
