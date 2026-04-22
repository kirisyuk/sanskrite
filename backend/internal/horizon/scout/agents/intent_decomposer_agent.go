package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/edgesemantics"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// IntentDecomposerAgent proposes a reusable, taxonomy-aligned set of candidate relations
// for a given (entity + intent focus + spheres) even when the local document corpus is empty.
//
// Output is intentionally hypothesis-like: low confidence, designed to seed exploration and ontology facts.
type IntentDecomposerAgent struct {
	executor *ai.AITaskExecutor
}

func NewIntentDecomposerAgent(executor *ai.AITaskExecutor) *IntentDecomposerAgent {
	return &IntentDecomposerAgent{executor: executor}
}

type intentDecompositionResult struct {
	Relations []DiscoveredRelation `json:"relations"`
}

// DecomposeOptions contains additional options for intent decomposition.
type DecomposeOptions struct {
	// IsAbstractAnchor indicates the target is a MARKET/PROJECT/abstract entity
	// that should be decomposed into concrete entities with higher confidence.
	IsAbstractAnchor bool
	// DecompositionHints provide structured guidance for decomposition.
	DecompositionHints []DecompositionHint
	// OriginalQuery is the raw user query for context.
	OriginalQuery string
	// DomainModes restrict relation types when set (e.g. supply_chain → supply-chain-only enum).
	DomainModes []string
}

func (a *IntentDecomposerAgent) Decompose(
	ctx context.Context,
	targetCanonicalPath string,
	focus string,
	spherePaths []string,
	keywords []string,
) ([]DiscoveredRelation, error) {
	return a.DecomposeWithOptions(ctx, targetCanonicalPath, focus, spherePaths, keywords, DecomposeOptions{})
}

// DecomposeWithOptions is like Decompose but accepts additional options.
func (a *IntentDecomposerAgent) DecomposeWithOptions(
	ctx context.Context,
	targetCanonicalPath string,
	focus string,
	spherePaths []string,
	keywords []string,
	opts DecomposeOptions,
) ([]DiscoveredRelation, error) {
	rels, _, err := a.DecomposeWithGrounding(ctx, targetCanonicalPath, focus, spherePaths, keywords, opts)
	return rels, err
}

// DecomposeWithGrounding is like DecomposeWithOptions but also returns grounding evidence
// from the full AI task result.
func (a *IntentDecomposerAgent) DecomposeWithGrounding(
	ctx context.Context,
	targetCanonicalPath string,
	focus string,
	spherePaths []string,
	keywords []string,
	opts DecomposeOptions,
) ([]DiscoveredRelation, *ai.GroundingEvidence, error) {
	targetCanonicalPath = strings.TrimSpace(targetCanonicalPath)
	if targetCanonicalPath == "" {
		return nil, nil, fmt.Errorf("targetCanonicalPath is required")
	}

	// Auto-detect abstract anchor from path prefix
	isAbstract := opts.IsAbstractAnchor ||
		strings.HasPrefix(targetCanonicalPath, "/market/") ||
		strings.HasPrefix(targetCanonicalPath, "/project/")

	prompt := a.buildPromptWithOptions(targetCanonicalPath, focus, spherePaths, keywords, opts)
	verifier := ai.NewSimpleJSONVerifier()

	task := &ai.AITask{
		TaskType:     ai.TaskTypeKnowledgeSynthesis,
		InputContent: normalizeLivePromptTaxonomy(prompt),
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability:        true,
			SearchGroundingCapability:       true, // ok to use grounded search when available
			InternalDataGroundingCapability: true,
			MinCapabilityScore:              7,
			MinContextWindow:                16000,
			MaxOutputSize:                   8192,
		},
		Verifier: verifier,
	}

	result, fullResult, err := a.executor.ExecuteStructuredTaskFull(ctx, task)
	if err != nil {
		return nil, nil, fmt.Errorf("intent decomposition failed: %w", err)
	}
	var groundingEvidence *ai.GroundingEvidence
	if fullResult != nil {
		groundingEvidence = fullResult.GroundingEvidence
	}

	b, err := json.Marshal(result)
	if err != nil {
		return nil, groundingEvidence, err
	}
	var out intentDecompositionResult
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, groundingEvidence, fmt.Errorf("failed to parse intent decomposition result: %w", err)
	}
	// Best-effort cleanup: drop empty rows, clamp confidence to appropriate envelope.
	clean := make([]DiscoveredRelation, 0, len(out.Relations))
	for _, r := range out.Relations {
		r.TargetName = strings.TrimSpace(r.TargetName)
		r.TargetCanonicalPath = strings.TrimSpace(r.TargetCanonicalPath)
		r.TargetType = strings.TrimSpace(r.TargetType)
		r.RelationType = strings.TrimSpace(r.RelationType)
		r.Reason = strings.TrimSpace(r.Reason)
		if r.TargetName == "" || r.TargetCanonicalPath == "" || r.RelationType == "" || r.TargetType == "" {
			continue
		}
		if isAbstract {
			// For MARKET/PROJECT decomposition, use higher confidence (0.60..0.75)
			// to ensure discovered relations trigger recursive scouting (threshold >= 0.55).
			if r.Confidence <= 0 || r.Confidence > 0.85 {
				r.Confidence = 0.65
			} else if r.Confidence < 0.55 {
				// Boost low-confidence relations to meet scouting threshold
				r.Confidence = 0.60
			}
		} else {
			if r.Confidence <= 0 || r.Confidence > 0.65 {
				// Force hypothesis envelope: we are seeding, not asserting.
				r.Confidence = 0.35
			}
		}
		clean = append(clean, r)
		if len(clean) >= 12 {
			break
		}
	}
	return clean, groundingEvidence, nil
}

func (a *IntentDecomposerAgent) buildPromptWithOptions(targetCanonicalPath string, focus string, spherePaths []string, keywords []string, opts DecomposeOptions) string {
	focus = strings.TrimSpace(focus)
	originalQuery := strings.TrimSpace(opts.OriginalQuery)
	spheresLine := strings.Join(dedupNonEmptyStrings(spherePaths), ", ")
	keywordsLine := strings.Join(dedupNonEmptyStrings(keywords), ", ")

	ctxBlock := ""
	if focus != "" || spheresLine != "" || keywordsLine != "" || originalQuery != "" {
		ctxBlock = "INTENT CONTEXT (steer towards reusable supply-chain/logistics structure; do NOT invent citations):\n"
		if originalQuery != "" {
			ctxBlock += fmt.Sprintf("- originalQuery: \"%s\"\n", originalQuery)
		}
		if focus != "" {
			ctxBlock += fmt.Sprintf("- focus: %s\n", focus)
		}
		if spheresLine != "" {
			ctxBlock += fmt.Sprintf("- spherePaths: %s\n", spheresLine)
		}
		if keywordsLine != "" {
			ctxBlock += fmt.Sprintf("- keywords: %s\n", keywordsLine)
		}
		ctxBlock += "\n"
	}

	// Add MARKET/PROJECT decomposition guidance if applicable
	decompositionBlock := ""
	isAbstract := opts.IsAbstractAnchor ||
		strings.HasPrefix(targetCanonicalPath, "/market/") ||
		strings.HasPrefix(targetCanonicalPath, "/project/")
	if isAbstract {
		decompositionBlock = "MARKET/PROJECT DECOMPOSITION MODE:\n"
		decompositionBlock += "This is an ABSTRACT anchor. Your PRIMARY goal is to discover CONCRETE entities.\n"
		decompositionBlock += "Output relations to:\n"
		decompositionBlock += "- Major players (organizations, banks, companies, service providers)\n"
		decompositionBlock += "- Key products or services\n"
		decompositionBlock += "- Regulatory bodies\n"
		decompositionBlock += "- Key facilities or infrastructure\n"
		decompositionBlock += "Use confidence >= 0.60 for well-known players.\n\n"

		// Add hints if available
		for _, h := range opts.DecompositionHints {
			if h.AnchorPath == targetCanonicalPath {
				if len(h.EntityTypes) > 0 {
					decompositionBlock += fmt.Sprintf("Target entity types: %s\n", strings.Join(h.EntityTypes, ", "))
				}
				if len(h.SubMarkets) > 0 {
					decompositionBlock += fmt.Sprintf("Sub-markets: %s\n", strings.Join(h.SubMarkets, ", "))
				}
				if len(h.PlayerCategories) > 0 {
					decompositionBlock += fmt.Sprintf("Player categories: %s\n", strings.Join(h.PlayerCategories, ", "))
				}
				if len(h.SearchQueries) > 0 {
					decompositionBlock += "Search guidance:\n"
					for _, q := range h.SearchQueries {
						decompositionBlock += fmt.Sprintf("  - %s\n", q)
					}
				}
				decompositionBlock += "\n"
				break
			}
		}
	} else if strings.HasPrefix(targetCanonicalPath, "/product/") {
		decompositionBlock = "PRODUCT DECOMPOSITION MODE:\n"
		decompositionBlock += "TARGET is a product. Output relations to ORG and SITE entities that produce this product, and org-recipients that receive it.\n"
		decompositionBlock += "Focus on: producing orgs, their sites, org-recipients. Use targetType ORG or SITE. targetCanonicalPath: /org/... or /fac/...\n"
		decompositionBlock += "Use produces (producer->product) or supplies (supplier->customer flow). Output concrete /org/ and /fac/ paths only. Do NOT use depends_on.\n\n"
	}

	isSupplyChainMode := taxonomy.HasDomainMode(opts.DomainModes, taxonomy.DomainModeSupplyChain) ||
		taxonomy.HasDomainMode(opts.DomainModes, taxonomy.DomainModeServiceChain)
	relationEnum := AllEdgeTypesForPrompt()
	relationRulesBlock := generatedIntentDecomposerRelationRulesBlock(opts.DomainModes)
	validationSuffix := validationSuffixGeneral
	dependsOnBlock := `DEPENDS_ON FACETS (qualifiers for more granular dependency analysis):
When outputting DEPENDS_ON relations, you MAY add a "facet" qualifier to specify the nature of dependency:
- communication: dependency on communication channels/platforms
- infrastructure: dependency on physical or digital infrastructure
- payment: dependency on payment/financial mechanisms
- distribution: dependency on distribution networks/channels
- logistics: dependency on logistics/transportation
- supply: dependency on supply of materials/goods
- labor: dependency on labor/workforce
- legitimization: dependency on legitimization mechanisms (for illicit activities)
- enforcement: dependency on enforcement/protection mechanisms
- regulatory: dependency on regulatory environment/approvals
Example: { "relationType": "DEPENDS_ON", "qualifiers": { "facet": "communication" } }
`
	if isSupplyChainMode {
		relationEnum = taxonomy.SupplyChainEdgeTypesForPrompt()
		validationSuffix = validationSuffixSupplyChain
		dependsOnBlock = `SUPPLY CHAIN: Do NOT use depends_on. Use produces, supplies, transforms_into, assembles_into, used_in, extracts_from, participates_in for supply chain links. Use listed_on only for explicit market-listing context, not for operational flow.
`
	}

	return fmt.Sprintf(`You are an intent decomposition agent for a risk analysis system.

WORKSPACE:
You operate inside the Horizon graph runtime.
This is not a file workspace and not a document summarizer.
Your job is to expand a TARGET graph entity into likely neighboring entities and typed relations that can seed investigation.
The graph is made of canonical entities, typed edges, metadata-bearing fragments, and later materialized artifacts.
Treat canonical paths as stable graph identities.
Reason in terms of graph structure, not prose sections or documents.

GOAL:
Given a TARGET entity and intent context, propose a SMALL, reusable set of candidate relations (hypotheses) that describe the relevant structure.
This is used to seed exploration when the local corpus is empty. Be conservative: output only high-likelihood nodes in the structure.
Your output should help the runtime decide what entities and relations to explore next, not write a report.

TARGET:
- canonicalPath: %s

%s
%s
%s

%s

OUTPUT:
Return ONLY JSON with this schema:
{
  "relations": [
    {
      "targetName": "string",
      "targetLegalName": "string",
      "targetShortName": "string",
      "targetType": "ORG" | "SITE" | "GEO" | "PRODUCT" | "PROJECT" | "MARKET" | "EVIDENCE" | "PERSON" | "MEDIA_CHANNEL" | "COMMUNITY" | "GOV" | "ZONE",
      "relationType": %s,
      "reason": "string",
      "confidence": 0.0,
      "targetCanonicalPath": "string (REQUIRED, stable, NOT /discovered/*)"
    }
  ]
}

RULES:
- Output 5..12 relations max.
- Prefer supply-chain/logistics structure when the intent suggests it (suppliers, battery materials/components, logistics providers, ports/regions, manufacturing facilities, key dependencies).
- confidence MUST be <= 0.65 (hypothesis), use ~0.25..0.55 typically.
- Use stable canonical paths:
  - Organizations: /org/{iso2|regional}/{slug}
    - iso2: country of origin / primary jurisdiction (%s). Use your best evidence-backed jurisdiction instead of falling back to /org/na.
  - Government bodies: /gov/{iso2|regional}/{slug}
    - Use for ministries, regulators, agencies, state bodies. Resolve the best jurisdiction from the source, official name, and place clues instead of falling back to /gov/na.
  - Media channels (last-mile endpoints): /media/{platform}/{slug}
    - Example: Telegram channel "В казани поймут" -> /media/telegram/v-kazani-poymut
  - Communities (informal social groups): /community/{geo}/{type}/{slug}
    - geo: %s
    - Regional tokens ALWAYS go in the geo slot (2nd segment): /community/gcc/{type}/{slug} (NOT /community/na/{type}/gcc)
    - type: trade | activist | professional | criminal | religious | ethnic | interest | dynasty | tribe | representatives | industry-association | key-community
    - Examples:
      - "Hawala network in Dubai" -> /community/ae/trade/hawala-dubai-network
      - "Kazan drug dealers" -> /community/ru/criminal/kazan-synthetic-dealers
      - "OSINT community" -> /community/global/professional/osint-researchers
      - "House of Saud" -> /community/sa/dynasty/house-of-saud
      - "Pashtun tribes" -> /community/af/tribe/pashtun
      - "Senior Zhuz (Kazakhstan)" -> /community/kz/tribe/senior-zhuz
    - CRITICAL: COMMUNITY is for INFORMAL groups WITHOUT legal structure.
      - If the entity has legal registration → use ORG (/org/)
      - If the entity is a media endpoint (channel/group) → use MEDIA_CHANNEL (/media/)
    - COMMUNITY HIERARCHY: Use HAS_SUBGROUP to link parent community → narrower sub-community
      - dynasties: broader family → branch (house-of-saud → sudairi-seven)
      - tribes: parent tribe → sub-tribe/clan (pashtun → durrani → popalzai)
      - Direction: broader → narrower
  - Facilities: /fac/{geo}/{slug} — geo: %s. Infer the best concrete geo from facility/site/place clues in the name and source instead of falling back to /fac/na.
  - People: /person/{slug}
  - Products: MUST use hierarchical structure /product/{category}/{type}/{stage}/{slug}
    - category: "raw" | "component" | "finished" | "service"
    - type: "mineral" | "agricultural" | "chemical" | "electronic" | "mechanical" | "energy"
    - stage: "extraction" | "refining" | "processing" | "manufacturing" | "assembly"
    - slug: lowercase-slug derived from product name
    - If category/type/stage is uncertain, still output the same 5-segment format with your best estimate
  - GEO regions/locations: MUST be /loc/{country}[/{level1}[/{level2}]]
    - country: %s
    - Regional tokens ALWAYS go in the country slot (2nd segment), never at the end:
      - GCC → /loc/gcc (NOT /loc/global/gcc)
    - level1/level2: optional subdivision/city slugs (omit unknown trailing levels)
    - country-only locations: /loc/{country}
    - CRITICAL: country code MUST be valid. Invalid codes will be REJECTED.
  - MARKET (segment-first preferred):
    - HIERARCHY LEVELS:
      - L1 (domain): /market/{geo}/{domain}; L2 (vertical): /market/{geo}/{domain}/{vertical}; L3+ (sub-vertical): parent path + one segment
      - NEVER repeat a segment already in the path (e.g. /market/iq/agriculture/agriculture is INVALID)
      - Example chains: /market/sa/finance → /market/sa/finance/banking → .../islamic-banking; /market/tw/technology → /market/tw/technology/semiconductors → .../foundry
    - domain/vertical SHOULD align with /segment/{domain}/{vertical}/{geo} taxonomy used for organizations
    - geo: %s
    - Regional tokens ALWAYS go in the geo slot (2nd segment): /market/gcc/{domain}/{vertical} (NOT /market/na/{domain}/gcc)
    - GEOGRAPHY INHERITANCE: If TARGET is a regional market (e.g., /market/sa/...), new MARKET relations MUST inherit that geo
      - Example: decomposing /market/sa/event-marketing → sub-markets should be /market/sa/... (NOT /market/global/...)
    - Use "global" ONLY when the market is truly global or no geographic context exists
    - MARKET HIERARCHY: Use HAS_MARKET_SEGMENT to link parent market → narrower segment (child path = parent path + one segment)
      - Example: /market/sa/marketing HAS_MARKET_SEGMENT /market/sa/marketing/event-marketing
      - Direction: broader → narrower
  - PROJECT: /project/{jurisdictionSlug}/{projectSlug}
    - jurisdictionSlug: %s
    - Regional tokens ALWAYS go in the jurisdiction slot (2nd segment): /project/gcc/{slug} (NOT /project/global/gcc/{slug})
    - If TARGET is a regional project, new related entities should reflect that jurisdiction context
  - GOV: /gov/{iso2}/{slug}
    - GOV HIERARCHY: Use ADMINISTERS to link superior gov body → subordinate body
      - Example: /gov/sa/ministry-of-health ADMINISTERS /gov/sa/sfda
      - Direction: superior → subordinate
    - GovTypes: government, ministry, department, agency, authority, regulator, committee, service, bureau, office
- VALIDATION POLICY RULES (MUST comply or output will be REJECTED):
  - canonicalPath MUST NOT start with "/discovered/" (will be rejected)
  - canonicalPath MUST match expected prefix for targetType
  - GEO paths MUST match /loc/{country}[/{level1}[/{level2}]] with valid country code
%s
%s
- NEVER use "/discovered/*".
- Return ONLY valid JSON.`, targetCanonicalPath, ctxBlock, decompositionBlock, relationRulesBlock, dependsOnBlock, relationEnum,
		taxonomy.ISO2GeoDescriptionForPrompts(), taxonomy.ISO2GeoDescriptionForPrompts(), taxonomy.ISO2GeoDescriptionForPrompts(),
		taxonomy.ISO2GeoDescriptionForPrompts(), taxonomy.ISO2GeoDescriptionForPrompts(), taxonomy.ISO2GeoDescriptionForPrompts(),
		edgesemantics.EdgeDirectionValidationRulesForPrompt(), validationSuffix)
}

const (
	validationSuffixGeneral = `  - ORG ↔ MARKET: participates_in (org -> market). GOV entities (/gov/...) may also use regulates (domestic oversight), controls_access (sanctions/restrictions), or influences.
  - For PRODUCT -> PRODUCT steps, use transforms_into for processing/refining, assembles_into for direct assembly, and used_in for strong operational input semantics (not produces).
`

	validationSuffixSupplyChain = `  - For PRODUCT -> PRODUCT steps, use transforms_into for processing/refining, assembles_into for direct assembly, and used_in for strong operational input semantics (not produces).
  - In supply_chain mode, MARKET source may emit only participates_in or has_market_segment. PRODUCT -> MARKET uses listed_on only when explicit market-listing context exists.
`
)
