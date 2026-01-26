package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
)

type AnchorResolutionPhaseResult struct {
	Anchors            []struct {
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
  - CORRECT: /org/sa/saudi-aramco, /fac/aramco/ras-tanura-refinery, /loc/ae/na/na
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
- canonicalPath MUST be strict: /loc/{countryAlpha2}/{admin1}/{city}
  - countryAlpha2: ISO3166-1 alpha2 lowercase OR regional token (e.g., gcc, eu, asean, opec)
  - Regional tokens ALWAYS go in the countryAlpha2 slot (2nd segment), never at the end.
    - Example: GCC → /loc/gcc/na/na (NOT /loc/na/na/gcc)
  - use "na" for unknown admin1/city
  - country-only locations MUST be /loc/{cc}/na/na
    - Saudi Arabia -> /loc/sa/na/na
    - United Arab Emirates -> /loc/ae/na/na
    - Russia -> /loc/ru/na/na

PRODUCT (CRITICAL):
- canonicalPath MUST be strict hierarchical taxonomy:
  - /product/{category}/{type}/{stage}/{slug}
  - category: raw|component|finished|service
  - type: mineral|agricultural|chemical|electronic|mechanical|energy (best-effort)
  - stage: extraction|refining|processing|manufacturing|assembly (best-effort)
  - If you truly cannot classify, use fallback: /product/{slug}
  - Example: /product/raw/chemical/refining/lithium-carbonate

FACILITY:
- prefer /fac/{orgSlug}/{facilitySlug} when the owning org is known and stable
- otherwise use /fac/{facilitySlug}
- Example: /fac/aramco/ras-tanura-refinery, /fac/tesla/gigafactory-texas

ORGANIZATION:
- Organizations: /org/{iso2|na}/{slug}
  - iso2: country of origin / primary jurisdiction (ISO3166-1 alpha2 lowercase, 2 letters). If unknown, use "na".
  - Regional tokens go in the geo slot (2nd segment): /org/gcc/{slug} (NOT /org/na/gcc/{slug}).
  - slug: lowercase slug derived from the base name (avoid legal suffixes like Inc/LLC in the path if possible)
  - Legacy /org/{slug} may exist in the graph; prefer /org/{iso2}/{slug} for NEW anchors to reduce collisions.
- Government bodies: /gov/{iso2|na}/{slug}
  - Use for ministries, regulators, agencies, state bodies (prevents collisions like "Ministry of Defence" across countries).
  - If jurisdiction is unknown, use /gov/na/{slug}.
  - Regional tokens go in the geo slot (2nd segment): /gov/gcc/{slug} (NOT /gov/na/gcc/{slug}).

GOV HIERARCHY (best-effort):
- Government bodies form administrative hierarchies: government → ministry → department → agency/authority
- GovType values: government, ministry, department, agency, authority, regulator, committee, service, bureau, office, council, commission
- When anchoring government bodies, prefer specific agencies over generic ministry references when the query is specific.
- Example: "Saudi FDA" → /gov/sa/sfda (type: authority/regulator), parent would be /gov/sa/ministry-of-health

PROGRAM/PERSON/EVENT:
- /program/{slug}, /event/{slug}, /person/{slug}
- PROGRAM:
  - If jurisdiction is known, prefer /program/{jurisdictionSlug}/{programSlug}
    - jurisdictionSlug can be ISO2, regional token, or "global"/"int"
    - Regional tokens ALWAYS go in the jurisdiction slot (2nd segment): /program/gcc/{slug}
- PERSON: /person/{slug} (e.g., /person/ibrahim-al-rashid)

MARKET (CRITICAL):
- Preferred: /market/{geo}/{domain}/{vertical}
  - domain/vertical SHOULD align with the /segment/{domain}/{vertical}/{geo} taxonomy used for organizations.
  - Use GICS terminology (no numeric codes). If not in GICS, use the closest GICS parent + a descriptive term.
- geo MUST be ISO2 lowercase, composite ISO2, regional token (e.g., eu, gcc, asean, opec), or "global"/"int" (default "global" if unknown)
  - Regional tokens ALWAYS go in the geo slot (2nd segment): /market/gcc/{domain}/{vertical}

MARKET GEOGRAPHY INHERITANCE (CRITICAL):
- When the query mentions a specific country/region AND a market/industry together:
  - The MARKET anchor MUST inherit that country's ISO2 code as geo
  - Do NOT default to "global" when geographic context is present in the query
- Examples:
  - "Saudi Arabia Event marketing market" → /market/sa/marketing/event-marketing (NOT /market/global/...)
  - "German automotive industry" → /market/de/automotive/automotive (NOT /market/global/...)
  - "UAE banking sector" → /market/ae/financials/banking (NOT /market/global/...)
- Use "global" as geo ONLY when NO geographic context is provided in the query.

MARKET HIERARCHY (best-effort):
- Markets form hierarchies: broader → narrower (e.g., marketing → digital-marketing → event-marketing)
- When creating a MARKET anchor, consider if there's an obvious parent market.
- In decompositionHints.subMarkets, list narrower segments that belong to this market.

MEDIA_CHANNEL:
- If the query refers to a media brand/channel (e.g., a Telegram channel), do NOT collapse it to a GEO_REGION just because it contains a city name.
  - Example: Telegram channel "В казани поймут" should be anchored as /media/telegram/{channel-slug}, not as /loc/... for Kazan.
  - Prefer using a stable handle/username if present; otherwise use the quoted title as the slug source.

DECOMPOSITION HINTS (REQUIRED for MARKET/PROGRAM anchors):
- If any anchor has type MARKET or PROGRAM, you MUST provide decompositionHints for that anchor.
- decompositionHints help downstream research know HOW to explore abstract anchors.
- For MARKET anchors:
  - entityTypes: what entities to discover (typically ["ORGANIZATION", "PRODUCT", "FACILITY"])
  - subMarkets: market segments to explore (e.g., ["commercial banking", "retail banking", "islamic banking"])
  - playerCategories: types of players (e.g., ["local banks", "international banks", "fintechs", "regulators"])
  - searchQueries: 3-5 concrete search queries in English for grounded research
    - searchQueries MUST be designed to yield REAL-WORLD entities (company/bank/regulator names) that we can anchor as /org/... etc.
    - Avoid "market size" / "market share" queries that only return abstract numbers.
      - If you include a market-share query, phrase it as "top X by market share" so the intent is to extract PLAYERS.
    - Example for "Saudi B2B SME banking":
      ["top SME banks in Saudi Arabia 2024", "Saudi Arabia SME banking providers list", "Saudi B2B banking platforms for SMEs", "Saudi Arabian Monetary Authority SME banking regulations"]
- For PROGRAM anchors:
  - entityTypes: what entities participate (e.g., ["ORGANIZATION", "FACILITY", "PRODUCT"])
  - playerCategories: types of participants (e.g., ["implementing agencies", "contractors", "beneficiaries"])
  - searchQueries: 3-5 concrete search queries about the program
- searchQueries MUST be in English and actionable for web search.

- Prefer STABLE anchors:
  - choose country canonical paths for country scope, not adjective/alias forms
  - choose products/materials at the right level (raw vs component vs finished) when possible
- Provide 1-3 anchors (anchor = what the Lens should attach to).

- Return ONLY JSON. No markdown. No commentary.`, query, domainContext, quotedBlock, multiHint)
}
