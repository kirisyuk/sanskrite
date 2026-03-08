package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

type AnchorResolutionPhaseResult struct {
	Anchors []struct {
		CanonicalPath string  `json:"canonicalPath"`
		Type          string  `json:"type"`
		Name          string  `json:"name,omitempty"`
		Confidence    float64 `json:"confidence"`
		Reason        string  `json:"reason"`
	} `json:"anchors"`
	DecompositionHints []DecompositionHint `json:"decompositionHints,omitempty"`
}

// AnchorResolverAgent extracts anchors + decomposition hints (phase 2).
type AnchorResolverAgent struct {
	executor *ai.AITaskExecutor
}

func NewAnchorResolverAgent(executor *ai.AITaskExecutor) *AnchorResolverAgent {
	return &AnchorResolverAgent{executor: executor}
}

func (a *AnchorResolverAgent) ResolveWithContext(ctx context.Context, query string, domains []DomainResolution) (*AnchorResolutionPhaseResult, error) {
	if a == nil || a.executor == nil {
		return nil, fmt.Errorf("anchor resolver not configured")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}

	prompt := buildAnchorResolverPrompt(query, domains)
	verifier := ai.NewGenericVerifier(func(result AnchorResolutionPhaseResult) error {
		if len(result.Anchors) == 0 {
			return fmt.Errorf("at least 1 anchor is required")
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
		return nil, fmt.Errorf("anchor resolve failed: %w", err)
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal anchor result: %w", err)
	}
	var out AnchorResolutionPhaseResult
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("decode anchor result: %w", err)
	}
	return &out, nil
}

func buildAnchorResolverPrompt(query string, domains []DomainResolution) string {
	domainContext := ""
	if len(domains) > 0 {
		domainContext = "DOMAIN CONTEXT:\n"
		for i := range domains {
			d := domains[i]
			if strings.TrimSpace(d.CanonicalPath) == "" {
				continue
			}
			domainContext += fmt.Sprintf("- %s (%s)\n", d.CanonicalPath, strings.TrimSpace(d.ContractType))
		}
		domainContext += "\n"
	}
	quoted := extractQuotedPhrases(query)
	quotedBlock := ""
	if len(quoted) > 0 {
		quotedBlock = "QUOTED PHRASES (treat each as a likely standalone entity name):\n"
		for i, q := range quoted {
			if i >= 5 {
				break
			}
			quotedBlock += fmt.Sprintf("- %s\n", q)
		}
		quotedBlock += "\n"
	}
	multiHint := ""
	if looksLikeMultiEntityQuery(query) {
		multiHint = `MULTI-ENTITY HINT:
- This query likely contains multiple distinct real-world entities. Prefer returning multiple separate anchors (2-3) rather than collapsing them into one.
- Typical patterns that indicate multiple entities:
  - "X and Y", "X vs Y", "X & Y"
  - Person name + institution keyword (bank, ministry, company, authority, university, foundation)
  - Organization + product/brand/facility name
- Examples:
  - "Ibrahim Al Rashid Social Development Bank" → anchors:
    - PERSON: /person/ibrahim-al-rashid
    - ORGANIZATION: /org/{iso2|na}/social-development-bank
  - "Apple and Microsoft cloud partnership" → anchors:
    - ORGANIZATION: /org/us/apple-inc
    - ORGANIZATION: /org/us/microsoft-corp

`
	}
	return fmt.Sprintf(`You are an ANCHOR resolver. Output ONLY JSON.

USER QUERY:
%s

%s%s%sOUTPUT JSON SCHEMA:
{
  "anchors": [
    {
      "canonicalPath": "string (canonicalPath; must match taxonomy rules below)",
      "type": "ORGANIZATION|PRODUCT|FACILITY|GEO_REGION|PERSON|EVENT|PROGRAM|MARKET|MEDIA_CHANNEL",
      "name": "string (preferred English display name; NOT a slug; translate to English when possible)",
      "confidence": 0.0,
      "reason": "short reason"
    }
  ],
  "decompositionHints": [
    {
      "anchorPath": "string (must match one of the anchors' canonicalPath)",
      "entityTypes": ["ORGANIZATION", "PRODUCT", ...],
      "subMarkets": ["segment1", "segment2", ...],
      "playerCategories": ["category1", "category2", ...],
      "searchQueries": ["concrete search query 1", "concrete search query 2", ...]
    }
  ]
}

TAXONOMY RULES:

- Do NOT create domains. Domains are provided in context.
- canonicalPath MUST be a valid canonical path with allowed namespaces:
  /gov, /org, /person, /product, /program, /market, /fac, /loc, /sphere, /event, /media
- ALL path segments MUST be lowercase slugs (use hyphens instead of spaces):
  - CORRECT: /org/sa/saudi-aramco, /fac/sa/ras-tanura-refinery, /loc/ae
  - WRONG: /loc/UAE, /org/Tesla Inc, /product/Lithium Carbonate

- The anchor type MUST match the canonicalPath namespace:
  - ORGANIZATION -> /org/... OR /gov/... (for government bodies)
  - FACILITY     -> /fac/...
  - GEO_REGION   -> /loc/...
  - PERSON       -> /person/...
  - PRODUCT      -> /product/...
  - MEDIA_CHANNEL -> /media/{platform}/{slug}
  - PROGRAM      -> /program/...
  - MARKET       -> /market/...
  - EVENT        -> /event/...

GEO_REGION (CRITICAL):
- canonicalPath MUST be strict: /loc/{countryAlpha2}[/{level1}[/{level2}]]
  - countryAlpha2: `+taxonomy.ISO2GeoDescriptionForPrompts()+`
  - Regional tokens ALWAYS go in the countryAlpha2 slot (2nd segment), never at the end.
    - Example: GCC → /loc/gcc (NOT /loc/global/gcc)
  - omit unknown trailing levels (no mandatory "na")
  - country-only locations MUST be /loc/{cc}
    - Saudi Arabia -> /loc/sa
    - United Arab Emirates -> /loc/ae
    - Russia -> /loc/ru

PRODUCT (CRITICAL):
- canonicalPath MUST be strict hierarchical taxonomy:
  - /product/{category}/{type}/{stage}/{slug}
  - category: raw|component|finished|service
  - type: mineral|agricultural|chemical|electronic|mechanical|energy (best-effort)
  - stage: extraction|refining|processing|manufacturing|assembly (best-effort)
  - If classification is uncertain, still keep this 5-segment format with your best taxonomy estimate
  - Example: /product/raw/chemical/refining/lithium-carbonate

FACILITY:
- use /fac/{geo}/{facilitySlug}; use /fac/na/{facilitySlug} ONLY as a last resort after checking facility/site/place clues in the name and source
- Example: /fac/sa/ras-tanura-refinery, /fac/us/gigafactory-texas

ORGANIZATION:
- Organizations: /org/{iso2|na}/{slug}
  - iso2: country of origin / primary jurisdiction (`+taxonomy.ISO2GeoDescriptionForPrompts()+`). /org/na is last-resort only after checking official name, HQ/site/place clue, and source text for jurisdiction.
  - Regional tokens go in the geo slot (2nd segment): /org/gcc/{slug} (NOT /org/na/gcc/{slug}).
  - slug: lowercase slug derived from the base name (avoid legal suffixes like Inc/LLC in the path if possible)
- Government bodies: /gov/{iso2|na}/{slug}
  - Use for ministries, regulators, agencies, state bodies (prevents collisions like "Ministry of Defence" across countries).
  - /gov/na/{slug} is last-resort only after checking the source for jurisdiction/country clues.
  - Regional tokens go in the geo slot (2nd segment): /gov/gcc/{slug} (NOT /gov/na/gcc/{slug}).

GOV HIERARCHY (best-effort):
- Government bodies form administrative hierarchies: government → ministry → department → agency/authority
- GovType values: government, ministry, department, agency, authority, regulator, committee, service, bureau, office, council, commission
- When anchoring government bodies, prefer specific agencies over generic ministry references when the query is specific.
- Example: "Saudi FDA" → /gov/sa/sfda (type: authority/regulator), parent would be /gov/sa/ministry-of-health

PROGRAM/PERSON/EVENT:
- /program/{jurisdictionSlug}/{programSlug}, /event/{slug}, /person/{slug}
- PROGRAM:
  - jurisdictionSlug can be ISO2, regional token, or "global"/"int"
  - If jurisdiction is unknown, use "global"
  - Regional tokens ALWAYS go in the jurisdiction slot (2nd segment): /program/gcc/{slug}
- PERSON: /person/{slug} (e.g., /person/ibrahim-al-rashid)

MARKET (CRITICAL):
- HIERARCHY LEVELS:
  - L1 (domain): /market/{geo}/{domain} — entire sector
  - L2 (vertical): /market/{geo}/{domain}/{vertical} — sub-segment of a domain
  - L3+ (sub-vertical): /market/{geo}/{domain}/{vertical}/{sub} — narrower niche under a vertical
  - Rule: child path = parent path + exactly one segment
  - NEVER repeat a segment already in the path (e.g. /market/iq/agriculture/agriculture is INVALID)
  - Examples:
    - "Iraqi agriculture" → /market/iq/agriculture (L1)
    - "Iraqi crop production" → /market/iq/agriculture/crop-production (L2)
    - "Iraqi rice production" → /market/iq/agriculture/crop-production/rice-production (L3)
- domain/vertical SHOULD align with the /segment/{domain}/{vertical}/{geo} taxonomy used for organizations.
- Use GICS terminology (no numeric codes). If not in GICS, use the closest GICS parent + a descriptive term.
- geo: `+taxonomy.ISO2GeoDescriptionForPrompts()+` (default "global" if unknown)
  - Regional tokens ALWAYS go in the geo slot (2nd segment): /market/gcc/{domain}/{vertical}

%s

MEDIA_CHANNEL:
- If the query refers to a media brand/channel (e.g., a Telegram channel), do NOT collapse it to a GEO_REGION just because it contains a city name.
  - Example: Telegram channel "В казани поймут" should be anchored as /media/telegram/{channel-slug}, not as /loc/... for Kazan.
  - Prefer using a stable handle/username if present; otherwise use the quoted title as the slug source.

%s

- Prefer STABLE anchors:
  - choose country canonical paths for country scope, not adjective/alias forms
  - choose products/materials at the right level (raw vs component vs finished) when possible
- Provide 1-3 anchors (anchor = what the Lens should attach to).

- Return ONLY JSON. No markdown. No commentary.`, query, domainContext, quotedBlock, multiHint, taxonomy.MarketGeographyAndHierarchyPromptBlock(), taxonomy.DecompositionHintsPromptBlock())
}
