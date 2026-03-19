package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// DomainResolutionPhaseResult captures the first-phase output for intent resolution.
type DomainResolutionPhaseResult struct {
	Focus       string             `json:"focus"`
	SpherePaths []string           `json:"spherePaths"`
	Keywords    []string           `json:"keywords"`
	Domains     []DomainResolution `json:"domains,omitempty"`
}

// DomainResolverAgent extracts domains + high-level focus signals.
type DomainResolverAgent struct {
	executor *ai.AITaskExecutor
}

func NewDomainResolverAgent(executor *ai.AITaskExecutor) *DomainResolverAgent {
	return &DomainResolverAgent{executor: executor}
}

func (a *DomainResolverAgent) ResolveWithContext(ctx context.Context, query string, existingDomains []string) (*DomainResolutionPhaseResult, error) {
	if a == nil || a.executor == nil {
		return nil, fmt.Errorf("domain resolver not configured")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}

	prompt := buildDomainResolverPrompt(query, existingDomains)
	verifier := ai.NewGenericVerifier(func(result DomainResolutionPhaseResult) error {
		if strings.TrimSpace(result.Focus) == "" {
			return fmt.Errorf("focus is required")
		}
		if len(result.Domains) == 0 {
			return fmt.Errorf("at least 1 domain is required")
		}
		return nil
	})

	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentAnalysis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability:  true,
			SearchGroundingCapability: false,
			MinCapabilityScore:        7,
			MinContextWindow:          4000,
			MaxOutputSize:             1024,
		},
		Verifier: verifier,
	}
	raw, err := a.executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("domain resolve failed: %w", err)
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal domain result: %w", err)
	}
	var out DomainResolutionPhaseResult
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("decode domain result: %w", err)
	}
	return &out, nil
}

func buildDomainResolverPrompt(query string, existingDomains []string) string {
	existingDomainsBlock := ""
	if len(existingDomains) > 0 {
		existingDomainsBlock = "EXISTING DOMAINS (reuse if matches; do not duplicate):\n"
		for _, d := range normalizeDomainPathsList(existingDomains, 10) {
			existingDomainsBlock += fmt.Sprintf("- %s\n", d)
		}
		if existingDomainsBlock != "" {
			existingDomainsBlock = "\n" + existingDomainsBlock
		}
	}
	return fmt.Sprintf(`You are a DOMAIN resolver for a risk analysis system. Output ONLY JSON.

USER QUERY:
%s
%s

OUTPUT JSON SCHEMA:
{
  "focus": "string (short, reusable intent statement, English)",
  "spherePaths": ["string (canonical /sphere/... only)"],
  "keywords": ["string (non-canonical tokens)"],
  "domains": [
    {
      "canonicalPath": "string (/domain/v1/{contractType}/{facetKey}/{facetValue}...)",
      "contractType": "string (allowlisted)",
      "title": "string (English human-readable title; NOT a slug)",
      "description": "string (actionable contract)",
      "facets": { "geo": ["us"], "material": ["lithium"], "application": ["ev"] },
      "projections": ["ORGANIZATION", "MARKET", "FACILITY", "GEO_REGION", "PRODUCT"],
      "relatedDomains": ["string (/domain/v1/... optional)"],
      "affectedDomains": ["string (/domain/v1/... optional)"],
      "firstOrderEntities": ["string (canonical paths to add to the Lens scope)"],
      "confidence": 0.0,
      "reason": "short reason"
    }
  ]
}

DOMAIN RULES:
- A DOMAIN is an executable knowledge contract: "what the user wants to know about the world" and how to structure exploration.
- Use domains when the query is an analytical question or a research focus that must be expanded into multiple entities.
- If a matching DOMAIN already exists in the context list, reuse the SAME canonicalPath (do NOT create duplicates).

- DOMAIN canonicalPath MUST be: /domain/v1/{contractType}/{facetKey}/{facetValue}...
  Good examples:
    /domain/v1/ecosystem-mapping/geo/my/industry/energy
    /domain/v1/industry-analysis/geo/kz/industry/agro-food
    /domain/v1/market-opportunity/geo/sg/industry/finance
    /domain/v1/bilateral-relations/geo/sa-ye (`+taxonomy.CompositeGeoDescriptionForPrompts()+` for two countries)
  Bad examples (these are ANCHORS, not DOMAINS):
    /market/global/finance
    /market/sg/finance/banking
    /market/tw/technology/semiconductors

- facetKey MUST be one of: geo, topic, subject, project, asset, material, application, sector, industry, market, regulation, framework, time
- facets MUST match the canonicalPath (best-effort). If you provide facets, keep them consistent with the path.

%s

- BILATERAL RELATIONS (CRITICAL):
  - If the query is about relations/interaction between two countries or actors, you MUST create a DOMAIN of contractType bilateral-relations.
  - Use `+taxonomy.CompositeGeoDescriptionForPrompts()+` for two countries: /domain/v1/bilateral-relations/geo/{cc1}-{cc2} (alphabetically sorted, lowercase ISO2).
  - Examples:
    "Saudi Arabia and Yemen relations" -> /domain/v1/bilateral-relations/geo/sa-ye
    "UAE-Iran tensions" -> /domain/v1/bilateral-relations/geo/ae-ir

- title MUST follow Title Case and be human-readable.
- title MUST be specific and grounded in the user's query (avoid generic titles).
  - BAD: "Market Landscape"
  - GOOD: "EV Lithium Batteries Market Landscape"
- title MUST be Latin-script only (broad Latin is OK: diacritics, Scandinavian, Turkish, etc.). No non-Latin scripts.
- title MUST be concise: 6-10 words (max 10).

- description MUST be actionable ("Map X, identify Y, highlight Z...").

- projections suggest what entity types to discover: ORGANIZATION, MARKET, FACILITY, GEO_REGION, PRODUCT, PERSON, EVENT, PROGRAM, GOV.

- Return ONLY JSON. No markdown. No commentary.`, query, existingDomainsBlock, taxonomy.DomainContractTypesPromptForLLM())
}
