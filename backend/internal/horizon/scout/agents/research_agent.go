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

// EnrichFragment обогащает HorizonFragment и находит новые связи
func (a *ResearchAgent) EnrichFragment(ctx context.Context, fragment *models.HorizonFragment, missingInfo []string) (*models.HorizonFragment, []DiscoveredRelation, error) {
	if len(missingInfo) == 0 {
		return fragment, nil, nil
	}

	prompt := a.buildEnrichmentPrompt(fragment, missingInfo)

	verifier := ai.NewSimpleJSONVerifier()

	task := &ai.AITask{
		TaskType:     ai.TaskTypeKnowledgeSynthesis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability:  true,
			SearchGroundingCapability: true, // Включаем Search Grounding
			// Важно: позволяем fallback на gemini-2.5-pro (capabilityScore=8),
			// иначе при сбое gemini-3-pro-preview у нас не остаётся подходящих моделей.
			MinCapabilityScore: 8,
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
func (a *ResearchAgent) buildEnrichmentPrompt(fragment *models.HorizonFragment, missingInfo []string) string {
	fragmentJSON, _ := json.MarshalIndent(fragment, "", "  ")
	questions := strings.Join(missingInfo, "\n- ")

	return fmt.Sprintf(`You are a research agent. Your task is to find missing information about the provided Horizon Fragment using your search capabilities.

CURRENT HORIZON FRAGMENT DATA:
%s

CRITICAL MISSING INFORMATION (QUESTIONS TO ANSWER):
- %s

TASK: Use your search tools to find answers to the missing information questions.
Return a JSON object with two fields:
1. "fragment": A JSON object containing ONLY the new or updated fields for the HorizonFragment that you found (e.g., "state", "homeLocation").
2. "relations": An array of DiscoveredRelation objects if you found new dependencies (suppliers, competitors, etc.).

Name Normalization Rules:
- For organizations and similar entities, ALWAYS provide:
  - fragment.identifiers.legalName (and/or fragment.labels.en): legal/official name including ownership suffix if relevant (Inc., Ltd., LLC, GmbH, AG, S.A., PLC...)
  - fragment.identifiers.shortName: base name without ownership suffix
- For relations, ALWAYS provide targetLegalName and targetShortName in addition to targetName.

DiscoveredRelation Schema:
{
  "targetName": "string",
  "targetLegalName": "string",
  "targetShortName": "string",
  "targetType": "ORGANIZATION" | "FACILITY" | "GEO_REGION" | "PRODUCT" | "PROGRAM" | "MARKET" | "EVENT" | "PERSON",
  "relationType": "SUPPLIES_TO" | "LOCATED_IN" | "OWNS" | "PRODUCES" | "OPERATES_IN" | "REGULATES" | "COMPETES_WITH" | "INFLUENCES" | "DEPENDS_ON" | "SUBSIDIARY_OF",
  "reason": "string",
  "confidence": 0.0,
  "targetCanonicalPath": "string, REQUIRED"
}

CANONICAL PATH RULES:
- NEVER use "/discovered/*" (not allowed).
- Prefer stable namespaces:
  - Organizations: /org/{lowercase-slug}
  - People: /person/{lowercase-slug}
  - Products:
    - Prefer /product/{orgSlug}/{productSlug} when org is known
    - Otherwise /product/{productSlug}
  - Programs:
    - Prefer /program/{jurisdictionSlug}/{programSlug} when jurisdiction is known
    - Otherwise /program/{programSlug}
  - Markets:
    - Prefer /market/{domainSlug}/{marketSlug} when domain is known
    - Otherwise /market/{marketSlug}
  - Facilities:
    - Prefer /fac/{orgSlug}/{facilitySlug} when org can be inferred
    - Otherwise /fac/{facilitySlug}
  - Geo regions / locations: prefer /region/... or /loc/... when you can be specific; otherwise /region/{lowercase-slug}
- canonicalPath slug SHOULD be derived from targetShortName (without ownership suffix) to avoid duplicates when legal form changes.
- ONLY include ownership suffix in canonicalPath if needed to disambiguate between different entities with same shortName.
- If you are unsure, still output your best stable canonicalPath (do not leave it empty).

Ensure the "fragment" JSON adheres to the HorizonFragment schema for the fields you are updating.
Return ONLY valid JSON. Do not include markdown code fences.`, string(fragmentJSON), questions)
}
