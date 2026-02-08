package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// IntentResolution is a lightweight, reusable representation of user intent.
// It is intentionally taxonomy-first: spherePaths must be canonical (/sphere/...), and anchors must be canonical paths.
type IntentResolution struct {
	Focus       string   `json:"focus"`
	SpherePaths []string `json:"spherePaths"`
	Keywords    []string `json:"keywords"`
	// Domains are executable “knowledge contracts” that must be expanded into first-order entities.
	// Canonical path format: /domain/v1/{contractType}/{facetKey}/{facetValue}...
	Domains []DomainResolution `json:"domains,omitempty"`
	Anchors []struct {
		CanonicalPath string `json:"canonicalPath"`
		Type          string `json:"type"` // ORGANIZATION|PRODUCT|FACILITY|GEO_REGION|PERSON|EVENT|PROGRAM|MARKET|MEDIA_CHANNEL (best-effort)
		// Name is the preferred English display name for this anchor (NOT a slug, NOT a URL).
		// If the original name is non-English and no established English name exists, transliteration is acceptable.
		Name       string  `json:"name,omitempty"`
		Confidence float64 `json:"confidence"`
		Reason     string  `json:"reason"`
	} `json:"anchors"`
	// DecompositionHints describe how abstract anchors (MARKET, PROGRAM) should be decomposed.
	// These are synthesized by the LLM and passed to downstream research to guide exploration.
	DecompositionHints []DecompositionHint `json:"decompositionHints,omitempty"`
}

// DomainResolution describes an executable domain contract extracted from a user query.
// It is a first-class ontology node (DOMAIN fragment) and is expanded into first-order entities by the pipeline.
type DomainResolution struct {
	CanonicalPath      string              `json:"canonicalPath"`
	ContractType       string              `json:"contractType"` // allowlisted, must match /domain/v1/{contractType}/...
	Title              string              `json:"title"`        // human-readable English title (not a slug)
	Description        string              `json:"description"`  // actionable contract (“what to reveal/show”)
	Facets             map[string][]string `json:"facets,omitempty"`
	Projections        []string            `json:"projections,omitempty"`
	RelatedDomains     []string            `json:"relatedDomains,omitempty"`     // canonical /domain/... paths
	AffectedDomains    []string            `json:"affectedDomains,omitempty"`    // canonical /domain/... paths
	FirstOrderEntities []string            `json:"firstOrderEntities,omitempty"` // canonical paths of entities to add to lens scope
	Confidence         float64             `json:"confidence"`
	Reason             string              `json:"reason"`
}

// DecompositionHint describes how to decompose an abstract anchor (MARKET, PROGRAM, etc.)
// into more concrete entities for research and scouting.
type DecompositionHint struct {
	// AnchorPath is the canonicalPath of the anchor this hint applies to.
	AnchorPath string `json:"anchorPath"`
	// EntityTypes suggest what types of entities should be discovered (e.g., ["ORGANIZATION", "PRODUCT"]).
	EntityTypes []string `json:"entityTypes"`
	// SubMarkets suggest market segmentations to explore (e.g., ["commercial banking", "consumer banking"]).
	SubMarkets []string `json:"subMarkets,omitempty"`
	// PlayerCategories suggest categories of players (e.g., ["local banks", "international banks", "fintechs"]).
	PlayerCategories []string `json:"playerCategories,omitempty"`
	// SearchQueries are concrete search queries to use for grounded research.
	SearchQueries []string `json:"searchQueries"`
}

// IntentResolverAgent extracts anchors/spheres/keywords from a natural language query.
// This is a "precheck" step: it should be cheap, deterministic, and produce reusable outputs.
type IntentResolverAgent struct {
	executor *ai.AITaskExecutor
}

func NewIntentResolverAgent(executor *ai.AITaskExecutor) *IntentResolverAgent {
	return &IntentResolverAgent{executor: executor}
}

func (a *IntentResolverAgent) Resolve(ctx context.Context, query string) (*IntentResolution, error) {
	return a.ResolveWithContext(ctx, query, nil)
}

// ResolveWithContext resolves intent with optional existing domain context to avoid duplicates.
// Uses a single unified LLM call (gemini-3-flash-preview) to extract domains, anchors, and decomposition hints simultaneously.
func (a *IntentResolverAgent) ResolveWithContext(ctx context.Context, query string, existingDomains []string) (*IntentResolution, error) {
	if a == nil || a.executor == nil {
		return nil, fmt.Errorf("intent resolver not configured")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}

	prompt := buildUnifiedIntentPrompt(query, existingDomains)

	verifier := ai.NewGenericVerifier(func(result IntentResolution) error {
		if strings.TrimSpace(result.Focus) == "" {
			return fmt.Errorf("focus is required")
		}
		if len(result.Anchors) == 0 && len(result.Domains) == 0 {
			return fmt.Errorf("at least 1 anchor or domain is required")
		}
		return nil
	})

	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentAnalysis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			PreferredModel:               "gemini-3-flash-preview",
			TextGenerationCapability:     true,
			SearchGroundingCapability:    false,
			MinCapabilityScore:           7,
			MinContextWindow:             8000,
			MaxOutputSize:                4096, // domains + anchors + hints in one response
		},
		Verifier: verifier,
	}

	raw, err := a.executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("unified intent resolve failed: %w", err)
	}

	b, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal intent result: %w", err)
	}
	var out IntentResolution
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("decode intent result: %w", err)
	}

	// Normalize + validate (taxonomy-first).
	out.Focus = strings.TrimSpace(out.Focus)

	// Normalize spheres (synonyms + allowlist).
	out.SpherePaths = taxonomy.NormalizeSpherePaths(out.SpherePaths)

	// Normalize keywords (trim, dedup, cap).
	seen := map[string]struct{}{}
	kw := make([]string, 0, len(out.Keywords))
	for _, k := range out.Keywords {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		key := strings.ToLower(k)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		kw = append(kw, k)
		if len(kw) >= 12 {
			break
		}
	}
	out.Keywords = kw

	// Normalize anchors: canonicalize formatting and drop invalid paths.
	nAnchors := make([]struct {
		CanonicalPath string  `json:"canonicalPath"`
		Type          string  `json:"type"`
		Name          string  `json:"name,omitempty"`
		Confidence    float64 `json:"confidence"`
		Reason        string  `json:"reason"`
	}, 0, len(out.Anchors))
	for _, an := range out.Anchors {
		originalCP := strings.TrimSpace(an.CanonicalPath)
		cp := originalCP
		// Fix MSYS conversion if any.
		cp = normalizeCanonicalPathForTools(cp)
		// Best-effort legacy geo normalization: older flows may still emit /region/... paths.
		// We normalize only supported legacy shapes into strict /loc/{country}/{admin1}/{city}.
		if strings.HasPrefix(cp, "/region/") {
			if norm := normalizeLegacyGeoPathForIntent(cp); norm != "" {
				cp = norm
			}
		}
		// Slugify each segment of the path to handle LLM outputs with spaces/uppercase.
		// Example: "/loc/Saudi Arabia" -> "/loc/saudi-arabia"
		cp = slugifyCanonicalPath(cp)
		if err := taxonomy.ValidateCanonicalPath(cp); err != nil {
			// Log rejected anchors for debugging
			fmt.Printf("[IntentResolver] Anchor rejected: original=%q, normalized=%q, error=%v\n", originalCP, cp, err)
			continue
		}
		an.CanonicalPath = cp
		if an.Confidence <= 0 || an.Confidence > 1 {
			an.Confidence = 0.6
		}
		an.Reason = strings.TrimSpace(an.Reason)
		an.Type = strings.TrimSpace(an.Type)
		an.Name = strings.TrimSpace(an.Name)
		nAnchors = append(nAnchors, an)
		if len(nAnchors) >= 5 {
			break
		}
	}
	out.Anchors = nAnchors
	if out.Focus == "" {
		out.Focus = query
	}

	// Normalize domains: canonicalize formatting and drop invalid paths.
	_, countryNameByCC := extractCountryAnchors(out.Anchors)
	existingDomainMap := make(map[string]string, len(existingDomains))
	if len(existingDomains) > 0 {
		for _, p := range existingDomains {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			p = normalizeCanonicalPathForTools(p)
			p = slugifyCanonicalPath(p)
			if !strings.HasPrefix(p, "/domain/") {
				continue
			}
			if err := taxonomy.ValidateCanonicalPath(p); err != nil {
				continue
			}
			if norm, nerr := taxonomy.NormalizeDomainCanonicalPath(p); nerr == nil && strings.TrimSpace(norm) != "" {
				p = norm
			}
			existingDomainMap[p] = p
		}
	}
	if len(out.Domains) > 0 {
		validDomains := make([]DomainResolution, 0, len(out.Domains))
		for _, d := range out.Domains {
			orig := strings.TrimSpace(d.CanonicalPath)
			cp := normalizeCanonicalPathForTools(orig)
			cp = slugifyCanonicalPath(cp)
			if !strings.HasPrefix(cp, "/domain/") {
				fmt.Printf("[IntentResolver] Domain rejected (non-domain path): original=%q, normalized=%q\n", orig, cp)
				continue
			}
			// Normalize using taxonomy validation (now includes /domain/v1).
			if err := taxonomy.ValidateCanonicalPath(cp); err != nil {
				fmt.Printf("[IntentResolver] Domain rejected: original=%q, normalized=%q, error=%v\n", orig, cp, err)
				continue
			}
			if strings.HasPrefix(cp, "/domain/") {
				if norm, nerr := taxonomy.NormalizeDomainCanonicalPath(cp); nerr == nil && strings.TrimSpace(norm) != "" {
					cp = norm
				}
			}
			if existing, ok := existingDomainMap[cp]; ok && strings.TrimSpace(existing) != "" {
				cp = existing
			}
			// Parse contractType + facets from path and treat it as source of truth.
			ct, facets := parseDomainPathForIntent(cp)
			if ct != "" {
				d.ContractType = ct
			} else {
				d.ContractType = strings.TrimSpace(d.ContractType)
			}
			d.CanonicalPath = cp
			d.Title = strings.TrimSpace(d.Title)
			d.Description = strings.TrimSpace(d.Description)
			d.Reason = strings.TrimSpace(d.Reason)
			if d.Confidence <= 0 || d.Confidence > 1 {
				d.Confidence = 0.6
			}
			// Always set facets from the path (ensures determinism and dedupe).
			if len(facets) > 0 {
				d.Facets = facets
			}
			// Ensure a stable human-readable title/description for UX.
			// LLMs sometimes omit these; they should not be empty in the normal pipeline.
			if strings.TrimSpace(d.Title) == "" {
				d.Title = buildDomainTitleForIntent(d.ContractType, d.CanonicalPath, d.Facets, countryNameByCC)
			}
			if strings.TrimSpace(d.Description) == "" {
				d.Description = buildDomainDescriptionForIntent(d.ContractType, d.Title)
			}
			// Normalize related/affected domains (canonical /domain/v1/...).
			d.RelatedDomains = normalizeDomainPathsList(d.RelatedDomains, 8)
			d.AffectedDomains = normalizeDomainPathsList(d.AffectedDomains, 8)
			// Normalize first-order entities canonical paths.
			d.FirstOrderEntities = normalizeCanonicalPathsList(d.FirstOrderEntities, 16)
			// Projections are free-form but we cap/dedupe.
			d.Projections = dedupStrings(d.Projections, 12)
			validDomains = append(validDomains, d)
			if len(validDomains) >= 3 {
				break
			}
		}
		out.Domains = validDomains
	}

	// Meaning-preserving policy for bilateral queries:
	// keep exactly ONE primary DOMAIN (bilateral-relations) so we don't fragment the user's intent.
	// Any extra domains (e.g., investment-thesis) are moved to relatedDomains (best-effort) and
	// do not become separate domain scope nodes.
	{
		text := strings.ToLower(strings.TrimSpace(query + " " + out.Focus))
		if looksLikeBilateralRelationsQuery(text) {
			ccs, _ := extractCountryAnchors(out.Anchors)
			if len(ccs) >= 2 {
				sort.Strings(ccs)
				// composite geo token: ae-sa (stable order)
				geoToken := strings.Join(ccs[:2], "-")
				primaryCP := "/domain/v1/bilateral-relations/geo/" + geoToken
				if err := taxonomy.ValidateCanonicalPath(primaryCP); err == nil {
					if norm, nerr := taxonomy.NormalizeDomainCanonicalPath(primaryCP); nerr == nil && strings.TrimSpace(norm) != "" {
						primaryCP = norm
					}
					var primary *DomainResolution
					others := make([]DomainResolution, 0, len(out.Domains))
					for i := range out.Domains {
						d := out.Domains[i]
						if strings.TrimSpace(d.CanonicalPath) == primaryCP {
							dd := d
							primary = &dd
						} else {
							others = append(others, d)
						}
					}
					if primary == nil {
						// Synthesize a minimal primary bilateral domain; don't lose other domain hints.
						primary = &DomainResolution{
							CanonicalPath: primaryCP,
							ContractType:  "bilateral-relations",
							Title:         "Bilateral Relations",
							Description:   "Map and analyze mutual relationships: diplomacy, security, trade, agreements, conflicts, key actors, and risks.",
							Facets:        map[string][]string{"geo": ccs[:2]},
							Projections:   []string{"GOV", "ORGANIZATION", "EVENT", "MARKET", "GEO_REGION"},
							Confidence:    0.75,
							Reason:        "policy: preserve bilateral meaning as a single primary domain",
						}
					}
					// Move other domains into relatedDomains (best-effort, canonicalized).
					rd := make([]string, 0, len(primary.RelatedDomains)+len(others))
					rd = append(rd, primary.RelatedDomains...)
					for _, od := range others {
						cp := strings.TrimSpace(od.CanonicalPath)
						if cp == "" {
							continue
						}
						rd = append(rd, cp)
					}
					primary.RelatedDomains = normalizeDomainPathsList(rd, 8)
					out.Domains = []DomainResolution{*primary}
				}
			}
		}
	}

	// Deterministic domain inference (taxonomy-first):
	// Some analytical questions (e.g., bilateral relations) are frequently resolved into anchors only.
	// If no domains were produced, synthesize a minimal DOMAIN contract from the query + canonical geo anchors.
	if len(out.Domains) == 0 {
		if inferred := bestEffortInferDomainsFromQuery(query, out.Focus, out.Anchors); len(inferred) > 0 {
			out.Domains = inferred
		}
	}
	if len(existingDomainMap) > 0 && len(out.Domains) > 0 {
		for i := range out.Domains {
			cp := strings.TrimSpace(out.Domains[i].CanonicalPath)
			if existing, ok := existingDomainMap[cp]; ok && strings.TrimSpace(existing) != "" {
				out.Domains[i].CanonicalPath = existing
			}
		}
	}

	// Must have at least one valid anchor or domain after normalization.
	if len(out.Anchors) == 0 && len(out.Domains) == 0 {
		return nil, fmt.Errorf("no valid anchors/domains after canonical validation")
	}

	// Normalize decomposition hints: filter to valid anchors, dedupe, cap.
	if len(out.DecompositionHints) > 0 && len(out.Anchors) > 0 {
		anchorPathSet := make(map[string]struct{}, len(out.Anchors))
		for _, a := range out.Anchors {
			anchorPathSet[a.CanonicalPath] = struct{}{}
		}
		validHints := make([]DecompositionHint, 0, len(out.DecompositionHints))
		for _, h := range out.DecompositionHints {
			h.AnchorPath = slugifyCanonicalPath(strings.TrimSpace(h.AnchorPath))
			if _, ok := anchorPathSet[h.AnchorPath]; !ok {
				continue // skip hints for non-existent anchors
			}
			// Dedupe and cap searchQueries
			seenQ := make(map[string]struct{})
			queries := make([]string, 0, len(h.SearchQueries))
			for _, q := range h.SearchQueries {
				q = strings.TrimSpace(q)
				if q == "" {
					continue
				}
				lq := strings.ToLower(q)
				if _, ok := seenQ[lq]; ok {
					continue
				}
				seenQ[lq] = struct{}{}
				queries = append(queries, q)
				if len(queries) >= 8 {
					break
				}
			}
			h.SearchQueries = queries
			// Dedupe entityTypes
			h.EntityTypes = dedupStrings(h.EntityTypes, 6)
			h.SubMarkets = dedupStrings(h.SubMarkets, 8)
			h.PlayerCategories = dedupStrings(h.PlayerCategories, 8)
			validHints = append(validHints, h)
			if len(validHints) >= 5 {
				break
			}
		}
		out.DecompositionHints = validHints
	}

	return &out, nil
}

// buildUnifiedIntentPrompt creates a single combined prompt that extracts domains, anchors,
// and decomposition hints in one LLM call. This eliminates the sequential dependency between
// the old DomainResolver and AnchorResolver phases.
func buildUnifiedIntentPrompt(query string, existingDomains []string) string {
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
- Typical patterns: "X and Y", "X vs Y", person name + institution keyword, organization + product.

`
	}
	return fmt.Sprintf(`You are an INTENT RESOLVER for a risk/intelligence analysis system. Output ONLY JSON.

USER QUERY:
%s
%s
%s%sOUTPUT JSON SCHEMA:
{
  "focus": "string (short, reusable intent statement, English)",
  "spherePaths": ["string (canonical /sphere/... only)"],
  "keywords": ["string (non-canonical tokens for search)"],
  "domains": [
    {
      "canonicalPath": "string (/domain/v1/{contractType}/{facetKey}/{facetValue}...)",
      "contractType": "string (allowlisted — see DOMAIN RULES)",
      "title": "string (English human-readable Title Case; NOT a slug; 6-10 words max)",
      "description": "string (actionable: Map X, identify Y, highlight Z...)",
      "facets": { "geo": ["us"], "material": ["lithium"], "application": ["ev"] },
      "projections": ["ORGANIZATION", "MARKET", "FACILITY", "GEO_REGION", "PRODUCT"],
      "relatedDomains": ["string (/domain/v1/... optional)"],
      "affectedDomains": ["string (/domain/v1/... optional)"],
      "firstOrderEntities": ["string (canonical paths to add to Lens scope)"],
      "confidence": 0.0,
      "reason": "short reason"
    }
  ],
  "anchors": [
    {
      "canonicalPath": "string (must match ANCHOR TAXONOMY below)",
      "type": "ORGANIZATION|PRODUCT|FACILITY|GEO_REGION|PERSON|EVENT|PROGRAM|MARKET|MEDIA_CHANNEL",
      "name": "string (preferred English display name; NOT a slug; translate when possible)",
      "confidence": 0.0,
      "reason": "short reason"
    }
  ],
  "decompositionHints": [
    {
      "anchorPath": "string (must match one anchor's canonicalPath)",
      "entityTypes": ["ORGANIZATION", "PRODUCT", ...],
      "subMarkets": ["segment1", ...],
      "playerCategories": ["category1", ...],
      "searchQueries": ["concrete English search query 1", ...]
    }
  ]
}

═══════════════════════════════════════════
DOMAIN RULES
═══════════════════════════════════════════
- A DOMAIN is an executable knowledge contract: "what the user wants to know" and how to explore it.
- Use domains when the query is analytical or a research focus that must be expanded into multiple entities.
- If a matching DOMAIN already exists, reuse the SAME canonicalPath.
- DOMAIN canonicalPath: /domain/v1/{contractType}/{facetKey}/{facetValue}...
  Examples: /domain/v1/ecosystem-mapping/geo/my/sector/energy, /domain/v1/bilateral-relations/geo/sa-ye
- facetKey MUST be one of: geo, topic, subject, project, asset, material, application, sector, industry, market, regulation, framework, time
- contractType MUST be one of:
  SUPPLY CHAIN: supply-chain, value-chain, logistics-flow, trade-flow, sourcing-map, procurement-map
  RISK/GEOPOLITICS: risk-assessment, geopolitical-risk, bilateral-relations, sanctions-exposure, conflict-dynamics
  MARKETS: market-opportunity, competitive-landscape, market-entry, market-sizing, demand-forecast, pricing-dynamics
  INVESTMENT: investment-thesis, funding-landscape, exit-strategy, valuation-drivers, portfolio-exposure
  OWNERSHIP/STRUCTURE: ownership-structure, control-map
  TECHNOLOGY: technology-landscape, innovation-map, patent-landscape, rd-landscape, adoption-curve, disruption-analysis
  ECOSYSTEM: ecosystem-mapping, stakeholder-mapping, partnership-landscape, influence-network, power-dynamics
  RELATIONSHIP/COMMONALITY: commonality-analysis
  POLICY: regulatory-impact, policy-and-regulation, compliance-landscape, security-and-compliance, licensing-requirements, esg-landscape
  SCENARIOS: scenario-analysis, macro-trends, megatrends, future-outlook
  SECTOR: sector-overview, industry-structure, vertical-deep-dive
  FALLBACK: other (only if none above fit): /domain/v1/other/{custom-slug}/...
- BILATERAL: If query is about relations between two countries → contractType bilateral-relations, geo = {cc1}-{cc2} (alphabetically sorted lowercase ISO2).
- OWNERSHIP: If query asks who owns/controls an entity → contractType ownership-structure, facet subject.
- COMMONALITY: If query asks what connects/unifies X and Y → contractType commonality-analysis, facets subject.
- title: Title Case, specific to query (BAD: "Market Landscape", GOOD: "EV Lithium Batteries Market Landscape"), Latin-script only, 6-10 words.
- description: actionable ("Map X, identify Y, highlight Z...").
- projections: ORGANIZATION, MARKET, FACILITY, GEO_REGION, PRODUCT, PERSON, EVENT, PROGRAM, GOV.

═══════════════════════════════════════════
ANCHOR TAXONOMY
═══════════════════════════════════════════
- Anchors are concrete real-world entities. Do NOT create domains as anchors.
- canonicalPath namespaces: /gov, /org, /person, /product, /program, /market, /fac, /loc, /sphere, /event, /media
- ALL path segments MUST be lowercase slugs (hyphens, no spaces).

ORGANIZATION: /org/{iso2|na}/{slug} (e.g., /org/sa/saudi-aramco). GOV bodies: /gov/{iso2|na}/{slug}.
FACILITY: /fac/{orgSlug}/{facilitySlug} or /fac/{facilitySlug}
GEO_REGION: /loc/{countryAlpha2}/{admin1}/{city} — use "na" for unknown segments. Country-only: /loc/{cc}/na/na. Regional: /loc/gcc/na/na.
PRODUCT: /product/{category}/{type}/{stage}/{slug} — category: raw|component|finished|service. Fallback: /product/{slug}.
MARKET: /market/{geo}/{domain}/{vertical} — geo: ISO2, composite, regional (gcc, eu), or "global". Inherit country ISO2 from query when geographic context is present.
PROGRAM: /program/{jurisdictionSlug}/{programSlug} or /program/{slug}
PERSON: /person/{slug}
EVENT: /event/{slug}
MEDIA_CHANNEL: /media/{platform}/{slug}

MARKET GEOGRAPHY: When query mentions a specific country AND a market → MARKET anchor MUST use that country's ISO2 as geo (NOT "global").

DECOMPOSITION HINTS (REQUIRED for MARKET/PROGRAM anchors):
- entityTypes: what to discover. subMarkets: segments. playerCategories: player types.
- searchQueries: 3-5 concrete English web search queries designed to yield real-world entity names (NOT abstract "market size" queries).

GENERAL:
- Provide 1-3 anchors (what the Lens should attach to).
- Prefer stable, specific anchors.
- Return ONLY JSON. No markdown. No commentary.`, query, existingDomainsBlock, quotedBlock, multiHint)
}

func parseDomainPathForIntent(canonicalPath string) (contractType string, facets map[string][]string) {
	parts := strings.Split(strings.Trim(strings.TrimSpace(canonicalPath), "/"), "/")
	// domain/v1/{contractType}/(k/v)*
	if len(parts) < 3 {
		return "", nil
	}
	if parts[0] != "domain" || parts[1] != "v1" {
		return "", nil
	}
	contractType = strings.TrimSpace(parts[2])
	if contractType == "" {
		return "", nil
	}
	rest := parts[3:]
	if len(rest)%2 != 0 {
		return contractType, nil
	}
	facets = map[string][]string{}
	for i := 0; i < len(rest); i += 2 {
		k := strings.TrimSpace(rest[i])
		v := strings.TrimSpace(rest[i+1])
		if k == "" || v == "" {
			continue
		}
		facets[k] = append(facets[k], v)
	}
	return contractType, facets
}

func titleCaseWords(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	parts := strings.Fields(s)
	for i, p := range parts {
		r := []rune(p)
		if len(r) == 0 {
			continue
		}
		r[0] = unicode.ToUpper(r[0])
		parts[i] = string(r)
	}
	return strings.Join(parts, " ")
}

func baseDomainTitle(contractType string) string {
	ct := strings.ToLower(strings.TrimSpace(contractType))
	switch ct {
	// Supply chain / industry
	case "supply-chain":
		return "Supply chain"
	case "value-chain":
		return "Value chain"
	case "logistics-flow":
		return "Logistics flow"
	case "trade-flow":
		return "Trade flow"
	case "sourcing-map":
		return "Sourcing map"
	case "procurement-map":
		return "Procurement map"

	// Risk / geopolitics
	case "risk-assessment":
		return "Risk assessment"
	case "geopolitical-risk":
		return "Geopolitical risk"
	case "bilateral-relations":
		return "Bilateral relations"
	case "sanctions-exposure":
		return "Sanctions exposure"
	case "conflict-dynamics":
		return "Conflict dynamics"

	// Markets / opportunity
	case "market-opportunity":
		return "Market opportunity"
	case "competitive-landscape":
		return "Competitive landscape"
	case "market-entry":
		return "Market entry"
	case "market-sizing":
		return "Market sizing"
	case "demand-forecast":
		return "Demand forecast"
	case "pricing-dynamics":
		return "Pricing dynamics"

	// Investment / finance
	case "investment-thesis":
		return "Investment thesis"
	case "funding-landscape":
		return "Funding landscape"
	case "exit-strategy":
		return "Exit strategy"
	case "valuation-drivers":
		return "Valuation drivers"
	case "portfolio-exposure":
		return "Portfolio exposure"

	// Technology / innovation
	case "technology-landscape":
		return "Technology landscape"
	case "innovation-map":
		return "Innovation map"
	case "patent-landscape":
		return "Patent landscape"
	case "rd-landscape":
		return "R&D landscape"
	case "adoption-curve":
		return "Adoption curve"
	case "disruption-analysis":
		return "Disruption analysis"

	// Ecosystem / stakeholder
	case "ecosystem-mapping":
		return "Ecosystem mapping"
	case "stakeholder-mapping":
		return "Stakeholder mapping"
	case "partnership-landscape":
		return "Partnership landscape"
	case "influence-network":
		return "Influence network"
	case "power-dynamics":
		return "Power dynamics"

	// Policy / regulation
	case "regulatory-impact":
		return "Regulatory impact"
	case "policy-and-regulation":
		return "Policy and regulation"
	case "compliance-landscape":
		return "Compliance landscape"
	case "security-and-compliance":
		return "Security and compliance"
	case "licensing-requirements":
		return "Licensing requirements"
	case "esg-landscape":
		return "ESG landscape"

	// Scenarios / trends
	case "scenario-analysis":
		return "Scenario analysis"
	case "macro-trends":
		return "Macro trends"
	case "megatrends":
		return "Megatrends"
	case "future-outlook":
		return "Future outlook"

	// Sector / industry
	case "sector-overview":
		return "Sector overview"
	case "industry-structure":
		return "Industry structure"
	case "vertical-deep-dive":
		return "Vertical deep-dive"

	// Other - handled specially (returns just "Other", custom slug added elsewhere)
	case "other":
		return "Other"

	default:
		// best-effort, deterministic humanization
		return titleCaseWords(strings.ReplaceAll(ct, "-", " "))
	}
}

// extractOtherSlugFromPath extracts custom slug from /domain/v1/other/{slug}/... paths.
func extractOtherSlugFromPath(canonicalPath string) string {
	cp := strings.TrimSpace(canonicalPath)
	if !strings.HasPrefix(cp, "/domain/v1/other/") {
		return ""
	}
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	// ["domain", "v1", "other", slug, ...]
	if len(parts) < 4 {
		return ""
	}
	return strings.TrimSpace(parts[3])
}

func buildDomainTitleForIntent(contractType string, canonicalPath string, facets map[string][]string, countryNameByCC map[string]string) string {
	ct := strings.ToLower(strings.TrimSpace(contractType))
	var base string

	// For "other" contract type, use the custom slug as the base title.
	if ct == "other" {
		slug := extractOtherSlugFromPath(canonicalPath)
		if slug != "" {
			base = titleCaseWords(strings.ReplaceAll(slug, "-", " "))
		} else {
			base = "Other"
		}
	} else {
		base = baseDomainTitle(contractType)
	}

	if strings.TrimSpace(base) == "" {
		base = "Domain"
	}
	gs := []string(nil)
	if facets != nil {
		if v, ok := facets["geo"]; ok && len(v) > 0 {
			gs = append([]string(nil), v...)
		}
	}
	if len(gs) == 0 {
		return base
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(gs))
	for _, g := range gs {
		cc := strings.TrimSpace(strings.ToLower(g))
		if len(cc) != 2 {
			continue
		}
		if _, ok := seen[cc]; ok {
			continue
		}
		seen[cc] = struct{}{}
		if n := strings.TrimSpace(countryNameByCC[cc]); n != "" {
			out = append(out, n)
		} else {
			out = append(out, strings.ToUpper(cc))
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		return base
	}
	return fmt.Sprintf("%s — %s", base, strings.Join(out, " · "))
}

func buildDomainDescriptionForIntent(contractType string, title string) string {
	ct := strings.ToLower(strings.TrimSpace(contractType))
	ttl := strings.TrimSpace(title)
	if ttl == "" {
		ttl = "this domain"
	}
	switch ct {
	// Supply chain / industry
	case "supply-chain", "value-chain":
		return fmt.Sprintf("Expand %s into first-order entities and relationships across the supply chain: actors, facilities, products/materials, markets, regulators, and key dependencies.", ttl)
	case "logistics-flow", "trade-flow":
		return fmt.Sprintf("Expand %s into first-order entities: logistics corridors, ports, warehouses, transport links, trade routes, and bottlenecks.", ttl)
	case "sourcing-map", "procurement-map":
		return fmt.Sprintf("Expand %s into first-order entities: suppliers, sourcing regions, raw materials, procurement channels, and alternative sources.", ttl)

	// Risk / geopolitics
	case "risk-assessment", "geopolitical-risk":
		return fmt.Sprintf("Expand %s into first-order entities and relationships: risk factors, affected actors, geographies, mitigation options, and interdependencies.", ttl)
	case "bilateral-relations":
		return fmt.Sprintf("Expand %s into first-order entities and relationships: diplomacy, security, trade, conflicts, agreements, key actors, and risks.", ttl)
	case "sanctions-exposure":
		return fmt.Sprintf("Expand %s into first-order entities: sanctioned actors, jurisdictions, compliance risks, exposure pathways, and mitigation strategies.", ttl)
	case "conflict-dynamics":
		return fmt.Sprintf("Expand %s into first-order entities: conflict actors, territorial scope, alliances, drivers, and potential outcomes.", ttl)

	// Markets / opportunity
	case "market-opportunity", "market-entry":
		return fmt.Sprintf("Expand %s into first-order entities and relationships: market segments, key players, demand drivers, supply constraints, and regulatory factors.", ttl)
	case "competitive-landscape":
		return fmt.Sprintf("Expand %s into first-order entities: competitors, market shares, product offerings, strategic positioning, and differentiation factors.", ttl)
	case "market-sizing", "demand-forecast":
		return fmt.Sprintf("Expand %s into first-order entities: market segments, size drivers, growth factors, demand indicators, and key forecasts.", ttl)
	case "pricing-dynamics":
		return fmt.Sprintf("Expand %s into first-order entities: pricing factors, cost drivers, market benchmarks, elasticity indicators, and trend signals.", ttl)

	// Investment / finance
	case "investment-thesis":
		return fmt.Sprintf("Expand %s into first-order entities and relationships: thesis drivers, markets, competitors, supply chain, regulators, and major risks.", ttl)
	case "funding-landscape":
		return fmt.Sprintf("Expand %s into first-order entities: funding sources, investor profiles, deal structures, valuation benchmarks, and capital availability.", ttl)
	case "exit-strategy", "valuation-drivers":
		return fmt.Sprintf("Expand %s into first-order entities: exit pathways, comparable transactions, valuation metrics, strategic buyers, and timing factors.", ttl)
	case "portfolio-exposure":
		return fmt.Sprintf("Expand %s into first-order entities: portfolio assets, sector allocations, risk concentrations, performance drivers, and rebalancing options.", ttl)

	// Technology / innovation
	case "technology-landscape", "innovation-map":
		return fmt.Sprintf("Expand %s into first-order entities: technology players, innovation clusters, IP leaders, adoption trends, and disruption signals.", ttl)
	case "patent-landscape", "rd-landscape":
		return fmt.Sprintf("Expand %s into first-order entities: patent holders, research centers, technology clusters, IP portfolios, and innovation trends.", ttl)
	case "adoption-curve", "disruption-analysis":
		return fmt.Sprintf("Expand %s into first-order entities: adoption phases, early movers, barriers, accelerators, and disruption pathways.", ttl)

	// Ecosystem / stakeholder
	case "ecosystem-mapping":
		return fmt.Sprintf("Expand %s into first-order entities: ecosystem participants, value flows, partnerships, dependencies, and growth dynamics.", ttl)
	case "stakeholder-mapping":
		return fmt.Sprintf("Expand %s into first-order entities: key stakeholders, influence levels, interests, alliances, and decision-making dynamics.", ttl)
	case "partnership-landscape":
		return fmt.Sprintf("Expand %s into first-order entities: partnership types, strategic alliances, joint ventures, collaboration models, and synergies.", ttl)
	case "influence-network", "power-dynamics":
		return fmt.Sprintf("Expand %s into first-order entities: influential actors, power centers, decision networks, gatekeepers, and leverage points.", ttl)

	// Policy / regulation
	case "regulatory-impact", "policy-and-regulation":
		return fmt.Sprintf("Expand %s into first-order entities: regulatory bodies, policy frameworks, compliance requirements, enforcement trends, and impact areas.", ttl)
	case "compliance-landscape", "security-and-compliance":
		return fmt.Sprintf("Expand %s into first-order entities: compliance standards, certifications, audit requirements, risk areas, and best practices.", ttl)
	case "licensing-requirements":
		return fmt.Sprintf("Expand %s into first-order entities: licensing authorities, permit types, application processes, requirements, and timelines.", ttl)
	case "esg-landscape":
		return fmt.Sprintf("Expand %s into first-order entities: ESG frameworks, rating agencies, disclosure standards, sustainability initiatives, and reporting requirements.", ttl)

	// Scenarios / trends
	case "scenario-analysis", "future-outlook":
		return fmt.Sprintf("Expand %s into first-order entities: scenario drivers, key uncertainties, trend signals, wildcards, and strategic implications.", ttl)
	case "macro-trends", "megatrends":
		return fmt.Sprintf("Expand %s into first-order entities: trend drivers, affected sectors, timeline indicators, impact areas, and strategic responses.", ttl)

	// Sector / industry
	case "sector-overview", "industry-structure", "vertical-deep-dive":
		return fmt.Sprintf("Expand %s into first-order entities: industry players, market structure, value chain, competitive dynamics, and growth drivers.", ttl)

	// Other - use title-based description
	case "other":
		return fmt.Sprintf("Expand %s into first-order entities and relationships relevant to this analytical focus.", ttl)

	default:
		return fmt.Sprintf("Expand %s into first-order entities and relationships for comprehensive exploration.", ttl)
	}
}

func bestEffortInferDomainsFromQuery(
	originalQuery string,
	focus string,
	anchors []struct {
		CanonicalPath string  `json:"canonicalPath"`
		Type          string  `json:"type"`
		Name          string  `json:"name,omitempty"`
		Confidence    float64 `json:"confidence"`
		Reason        string  `json:"reason"`
	},
) []DomainResolution {
	q := strings.ToLower(strings.TrimSpace(originalQuery))
	f := strings.ToLower(strings.TrimSpace(focus))
	text := strings.TrimSpace(q + " " + f)
	if text == "" {
		return nil
	}

	// Heuristic: bilateral relations queries with 2+ country-level geo anchors.
	if !looksLikeBilateralRelationsQuery(text) {
		return nil
	}

	ccs, nameByCC := extractCountryAnchors(anchors)
	if len(ccs) < 2 {
		return nil
	}
	// Use only first 2 distinct countries for the domain path (stable ordering).
	sort.Strings(ccs)
	cc1, cc2 := ccs[0], ccs[1]

	cp := fmt.Sprintf("/domain/v1/bilateral-relations/geo/%s-%s", cc1, cc2)
	if err := taxonomy.ValidateCanonicalPath(cp); err != nil {
		return nil
	}
	if norm, nerr := taxonomy.NormalizeDomainCanonicalPath(cp); nerr == nil && strings.TrimSpace(norm) != "" {
		cp = norm
	}

	n1 := nameByCC[cc1]
	if strings.TrimSpace(n1) == "" {
		n1 = strings.ToUpper(cc1)
	}
	n2 := nameByCC[cc2]
	if strings.TrimSpace(n2) == "" {
		n2 = strings.ToUpper(cc2)
	}

	title := fmt.Sprintf("%s–%s Bilateral Relations", n1, n2)
	desc := fmt.Sprintf(
		"Map and analyze bilateral relations between %s and %s: diplomacy, security, trade, conflicts, agreements, key actors, and risks.",
		n1, n2,
	)
	firstOrder := []string(nil)
	for _, a := range anchors {
		p := strings.TrimSpace(a.CanonicalPath)
		if strings.HasPrefix(p, "/loc/") && (strings.HasPrefix(p, "/loc/"+cc1+"/") || strings.HasPrefix(p, "/loc/"+cc2+"/")) {
			firstOrder = append(firstOrder, p)
		}
	}
	firstOrder = normalizeCanonicalPathsList(firstOrder, 10)

	return []DomainResolution{
		{
			CanonicalPath:      cp,
			ContractType:       "bilateral-relations",
			Title:              title,
			Description:        desc,
			Facets:             map[string][]string{"geo": {cc1, cc2}},
			Projections:        []string{"GOV", "ORGANIZATION", "EVENT", "MARKET", "GEO_REGION"},
			FirstOrderEntities: firstOrder,
			Confidence:         0.85,
			Reason:             "deterministic: bilateral relations query with geo anchors",
		},
	}
}

func looksLikeBilateralRelationsQuery(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" {
		return false
	}
	// English + Russian core patterns (keep conservative).
	patterns := []string{
		"bilateral relations",
		"relations between",
		"relationship between",
		"ties between",
		"diplomatic relations",
		"отношения между",
		"двусторонн",
	}
	for _, p := range patterns {
		if strings.Contains(t, p) {
			return true
		}
	}
	return false
}

func extractCountryAnchors(
	anchors []struct {
		CanonicalPath string  `json:"canonicalPath"`
		Type          string  `json:"type"`
		Name          string  `json:"name,omitempty"`
		Confidence    float64 `json:"confidence"`
		Reason        string  `json:"reason"`
	},
) (countries []string, nameByCC map[string]string) {
	seen := map[string]struct{}{}
	nameByCC = map[string]string{}
	for _, a := range anchors {
		cp := strings.TrimSpace(a.CanonicalPath)
		if !strings.HasPrefix(cp, "/loc/") {
			continue
		}
		parts := strings.Split(strings.Trim(cp, "/"), "/")
		// loc/{cc}/{admin1}/{city}
		if len(parts) < 4 {
			continue
		}
		cc := strings.TrimSpace(parts[1])
		if len(cc) != 2 {
			continue
		}
		if _, ok := seen[cc]; ok {
			continue
		}
		seen[cc] = struct{}{}
		countries = append(countries, cc)
		if strings.TrimSpace(a.Name) != "" {
			nameByCC[cc] = strings.TrimSpace(a.Name)
		}
	}
	return countries, nameByCC
}

func normalizeDomainPathsList(in []string, maxN int) []string {
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, p := range in {
		p = normalizeCanonicalPathForTools(strings.TrimSpace(p))
		p = slugifyCanonicalPath(p)
		if p == "" {
			continue
		}
		if !strings.HasPrefix(p, "/domain/") {
			continue
		}
		if err := taxonomy.ValidateCanonicalPath(p); err != nil {
			continue
		}
		if norm, nerr := taxonomy.NormalizeDomainCanonicalPath(p); nerr == nil && strings.TrimSpace(norm) != "" {
			p = norm
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
		if len(out) >= maxN {
			break
		}
	}
	return out
}

func normalizeCanonicalPathsList(in []string, maxN int) []string {
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, p := range in {
		p = normalizeCanonicalPathForTools(strings.TrimSpace(p))
		p = slugifyCanonicalPath(p)
		if p == "" {
			continue
		}
		if err := taxonomy.ValidateCanonicalPath(p); err != nil {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
		if len(out) >= maxN {
			break
		}
	}
	return out
}

// dedupStrings deduplicates and caps a string slice.
func dedupStrings(in []string, maxN int) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		key := strings.ToLower(s)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, s)
		if len(out) >= maxN {
			break
		}
	}
	return out
}

// Legacy: buildPrompt is the pre-refactor monolithic intent prompt builder.
// ResolveWithContext no longer uses it (split into DomainResolverAgent + AnchorResolverAgent),
// but we keep it for reference while the migration settles. The blank identifier assignment
// prevents editor "unusedfunc" warnings from gopls.
var _ = (*IntentResolverAgent).buildPrompt

func (a *IntentResolverAgent) buildPrompt(query string, existingDomains []string) string {
	quoted := extractQuotedPhrases(query)
	quotedBlock := ""
	if len(quoted) > 0 {
		quotedBlock = "\n\nQUOTED PHRASES (treat each as a likely standalone entity name):\n"
		for i, q := range quoted {
			if i >= 5 {
				break
			}
			quotedBlock += fmt.Sprintf("- %s\n", q)
		}
	}
	multiHint := ""
	if looksLikeMultiEntityQuery(query) {
		multiHint = "\n\nMULTI-ENTITY HINT:\n- This query likely contains multiple distinct real-world entities. Prefer returning multiple separate anchors (2-3) rather than collapsing them into one."
	}
	existingDomainsBlock := ""
	if len(existingDomains) > 0 {
		existing := normalizeDomainPathsList(existingDomains, 10)
		if len(existing) > 0 {
			existingDomainsBlock = "\n\nEXISTING DOMAINS (reuse if matches; do not create duplicates):\n"
			for _, d := range existing {
				existingDomainsBlock += fmt.Sprintf("- %s\n", d)
			}
		}
	}
	return fmt.Sprintf(`You are an intent resolver for a risk analysis system. You MUST convert a user query into a reusable, taxonomy-aligned intent.

USER QUERY:
%s
%s
%s
%s

OUTPUT JSON SCHEMA:
{
  "focus": "string (short, reusable intent statement)",
  "spherePaths": ["string (canonical /sphere/... only)"],
  "keywords": ["string (non-canonical tokens)"],
  "domains": [
    {
      "canonicalPath": "string (canonical /domain/v1/... contract path; MUST follow DOMAIN rules below)",
      "contractType": "string (allowlisted; MUST match the {contractType} in canonicalPath)",
      "title": "string (English human-readable title; NOT a slug)",
      "description": "string (actionable contract describing what the system should reveal/show)",
      "facets": { "geo": ["us"], "material": ["lithium"], "application": ["ev"] },
      "projections": ["ORGANIZATION", "MARKET", "FACILITY", "GEO_REGION", "PRODUCT"],
      "relatedDomains": ["string (/domain/v1/... optional)"],
      "affectedDomains": ["string (/domain/v1/... optional)"],
      "firstOrderEntities": ["string (canonical paths to add to the Lens scope)"],
      "confidence": 0.0,
      "reason": "short reason"
    }
  ],
  "anchors": [
    {
      "canonicalPath": "string (canonicalPath, must match the taxonomy rules below)",
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

RULES:
- focus MUST be in English (translate from the user query when needed). Keep it short and reusable.
- keywords SHOULD be in English when possible (translate), but can include raw tokens like handles.
- spherePaths MUST be canonical under /sphere/... (if unsure, use /sphere/other).

- DOMAINS (CRITICAL):
  - A DOMAIN is an executable knowledge contract: “what the user wants to know about the world” and how to structure exploration.
  - Use domains when the query is an analytical question or a research focus that must be expanded into multiple entities.
  - If a matching DOMAIN already exists in the context list, reuse the SAME canonicalPath (do NOT create duplicates).
  - If the query is about relations/interaction between two countries or actors, you MUST create a DOMAIN of contractType bilateral-relations (in addition to anchors).
    Example: "Saudi Arabia and Yemen bilateral relations" -> domain:
      /domain/v1/bilateral-relations/geo/sa/geo/ye
  - If the query asks who owns/controls an entity, you MUST create a DOMAIN of contractType ownership-structure.
  - If the query asks what connects/unifies two entities, you MUST create a DOMAIN of contractType commonality-analysis (in addition to anchors).
  - DOMAIN canonicalPath MUST start with /domain/ and follow the DOMAIN taxonomy. /market/... is NEVER a DOMAIN path.
  - /market/... CAN be used as an ANCHOR when the user asks about a market as a concrete entity.
  - Domain canonicalPath MUST be: /domain/v1/{contractType}/{facetKey}/{facetValue}...
    Good DOMAIN examples:
      /domain/v1/ecosystem-mapping/geo/my/sector/energy
      /domain/v1/market-opportunity/geo/sg/industry/finance
    Bad DOMAIN examples (these are ANCHORS, not DOMAINS):
      /market/global/finance
      /market/sg/finance/banking
  - contractType MUST be one of (choose the BEST fit):
    SUPPLY CHAIN / INDUSTRY:
      supply-chain, value-chain, logistics-flow, trade-flow, sourcing-map, procurement-map
    RISK / GEOPOLITICS:
      risk-assessment, geopolitical-risk, bilateral-relations, sanctions-exposure, conflict-dynamics
    MARKETS / OPPORTUNITY:
      market-opportunity, competitive-landscape, market-entry, market-sizing, demand-forecast, pricing-dynamics
    INVESTMENT / FINANCE:
      investment-thesis, funding-landscape, exit-strategy, valuation-drivers, portfolio-exposure
    OWNERSHIP / STRUCTURE:
      ownership-structure, control-map
    TECHNOLOGY / INNOVATION:
      technology-landscape, innovation-map, patent-landscape, rd-landscape, adoption-curve, disruption-analysis
    ECOSYSTEM / STAKEHOLDER:
      ecosystem-mapping, stakeholder-mapping, partnership-landscape, influence-network, power-dynamics
    RELATIONSHIP / COMMONALITY:
      commonality-analysis
    POLICY / REGULATION:
      regulatory-impact, policy-and-regulation, compliance-landscape, security-and-compliance, licensing-requirements, esg-landscape
    SCENARIOS / TRENDS:
      scenario-analysis, macro-trends, megatrends, future-outlook
    SECTOR / INDUSTRY SPECIFIC:
      sector-overview, industry-structure, vertical-deep-dive
    FALLBACK (use ONLY if none of the above fit):
      other — Format: /domain/v1/other/{custom-slug}/... where {custom-slug} is a lowercase kebab-case descriptor (e.g., energy-ecosystem-mapping, startup-landscape, talent-pool-analysis).
      Example: /domain/v1/other/energy-ecosystem-mapping/geo/my
  - facetKey MUST be one of:
    geo, topic, subject, project, asset, material, application, sector, industry, market, regulation, framework, time
  - facets MUST match the canonicalPath (best-effort). If you provide facets, keep them consistent with the path.
  - title MUST follow Title Case and be human-readable.
  - title MUST be specific and grounded in the user's query (avoid generic titles).
    - BAD: "Market Landscape"
    - GOOD: "EV Lithium Batteries Market Landscape"
  - title MUST be Latin-script only (broad Latin is OK: diacritics, Scandinavian, Turkish, etc.). No non-Latin scripts in title.
  - title MUST be concise: 6–10 words (max 10).
  - description MUST be actionable (“Map X, identify Y, highlight Z…”).

- MULTI-ENTITY ANCHORS (CRITICAL):
  - If the query clearly mentions multiple different entities (people + organizations, org + product, org + facility, competitors, partners, etc.),
    you MUST return multiple separate anchors (2-3), one per entity, instead of collapsing them into a single canonicalPath.
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
      - (optional) MARKET: /market/global/technology/cloud-computing
    - "Tesla Gigafactory Texas Austin" → anchors:
      - ORGANIZATION: /org/us/tesla-inc
      - FACILITY: /fac/tesla/gigafactory-texas
      - GEO_REGION: /loc/us/tx/austin
- canonicalPath MUST be a valid canonical path with allowed namespaces:
  /gov, /org, /person, /product, /program, /market, /fac, /loc, /sphere, /event, /media, /domain
- If the query refers to a media brand/channel (e.g. a Telegram channel), do NOT collapse it to a GEO_REGION just because it contains a city name.
  - Example: Telegram channel "В казани поймут" should be anchored as a MEDIA_CHANNEL: /media/telegram/{channel-slug}, not as /loc/... for Kazan.
  - Prefer using a stable handle/username if present; otherwise use the quoted title as the slug source.
  - Add keyword "telegram" and consider /sphere/social/information.
- ALL path segments MUST be lowercase slugs (use hyphens instead of spaces):
  - CORRECT: /org/saudi-aramco, /fac/aramco/ras-tanura-refinery, /loc/ae/na/na, /product/raw/chemical/refining/lithium-carbonate
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

- GEO_REGION canonicalPath MUST be strict: /loc/{countryAlpha2}/{admin1}/{city}
  - countryAlpha2: ISO3166-1 alpha2 lowercase OR regional token (e.g., gcc, eu, asean, opec)
  - Regional tokens ALWAYS go in the countryAlpha2 slot (2nd segment), never at the end.
    - Example: GCC → /loc/gcc/na/na (NOT /loc/na/na/gcc)
  - use "na" for unknown admin1/city
  - country-only locations MUST be /loc/{cc}/na/na
    - Saudi Arabia -> /loc/sa/na/na
    - United Arab Emirates -> /loc/ae/na/na

- PRODUCT canonicalPath MUST be strict hierarchical taxonomy:
  - /product/{category}/{type}/{stage}/{slug}
  - category: raw|component|finished|service
  - type: mineral|agricultural|chemical|electronic|mechanical|energy (best-effort)
  - stage: extraction|refining|processing|manufacturing|assembly (best-effort)
  - If you truly cannot classify, use fallback: /product/{slug}

- FACILITY canonicalPath:
  - prefer /fac/{orgSlug}/{facilitySlug} when the owning org is known and stable
  - otherwise use /fac/{facilitySlug}

- ORGANIZATION canonicalPath:
  - Organizations: /org/{iso2|na}/{slug}
    - iso2: country of origin / primary jurisdiction (ISO3166-1 alpha2 lowercase, 2 letters). If unknown, use "na".
    - Regional tokens go in the geo slot (2nd segment): /org/gcc/{slug} (NOT /org/na/gcc/{slug}).
    - slug: lowercase slug derived from the base name (avoid legal suffixes like Inc/LLC in the path if possible)
    - Legacy /org/{slug} may exist in the graph; prefer /org/{iso2}/{slug} for NEW anchors to reduce collisions.
  - Government bodies: /gov/{iso2|na}/{slug}
    - Use for ministries, regulators, agencies, state bodies (prevents collisions like "Ministry of Defence" across countries).
    - If jurisdiction is unknown, use /gov/na/{slug}.
    - Regional tokens go in the geo slot (2nd segment): /gov/gcc/{slug} (NOT /gov/na/gcc/{slug}).

- PROGRAM/MARKET/EVENT/PERSON canonicalPath:
  - /program/{slug}, /event/{slug}, /person/{slug}
  - PROGRAM:
    - If jurisdiction is known, prefer /program/{jurisdictionSlug}/{programSlug}
      - jurisdictionSlug can be ISO2, regional token, or "global"/"int"
      - Regional tokens ALWAYS go in the jurisdiction slot (2nd segment): /program/gcc/{slug} (NOT /program/na/gcc/{slug})
  - MARKET (segment-first):
    - Preferred: /market/{geo}/{domain}/{vertical}
      - domain/vertical SHOULD align with the /segment/{domain}/{vertical}/{geo} taxonomy used for organizations.
      - Use GICS terminology (no numeric codes). If not in GICS, use the closest GICS parent + a descriptive term.
      - This enables consistent market hierarchy + aggregation.
    - Legacy /market/{geo}/{slug} may exist in older data; do NOT emit legacy for new markets.
      - Always provide domain/vertical for NEW outputs.
  - MARKET geo MUST be ISO2 lowercase, composite ISO2, regional token (e.g., eu, gcc, asean, opec), or "global"/"int" (default "global" if unknown)
    - Regional tokens ALWAYS go in the geo slot (2nd segment): /market/gcc/{domain}/{vertical} (NOT /market/na/{domain}/gcc)

- MARKET GEOGRAPHY INHERITANCE (CRITICAL):
  - When the query mentions a specific country/region AND a market/industry together:
    - The MARKET anchor MUST inherit that country's ISO2 code as geo
    - Do NOT default to "global" when geographic context is present in the query
  - Examples:
    - "Saudi Arabia Event marketing market" → /market/sa/event-marketing (NOT /market/global/event-marketing)
    - "German automotive industry" → /market/de/automotive (NOT /market/global/automotive)
    - "UAE banking sector" → /market/ae/banking (NOT /market/global/banking)
  - Use "global" as geo ONLY when NO geographic context is provided in the query
  - You CAN also include a separate GEO_REGION anchor for the country - that is fine and often useful

- MARKET HIERARCHY (best-effort):
  - Markets form hierarchies: broader → narrower (e.g., marketing → digital-marketing → event-marketing)
  - When creating a MARKET anchor, consider if there's an obvious parent market
  - In decompositionHints.subMarkets, list narrower segments that belong to this market
  - Example hierarchy (segment-first): /market/sa/{domain}/marketing → /market/sa/{domain}/digital-marketing → /market/sa/{domain}/event-marketing

- GOV HIERARCHY (best-effort):
  - Government bodies form administrative hierarchies: government → ministry → department → agency/authority
  - GovType values: government, ministry, department, agency, authority, regulator, committee, service, bureau, office, council, commission
  - When anchoring government bodies, prefer specific agencies over generic ministry references when the query is specific
  - Example: "Saudi FDA" → /gov/sa/sfda (type: authority/regulator), parent would be /gov/sa/ministry-of-health

- Prefer STABLE anchors:
  - choose country canonical paths for country scope, not adjective/alias forms
  - choose products/materials at the right level (raw vs component vs finished) when possible
- Provide 1-3 anchors (anchor = what the Lens should attach to).
- Provide 1-3 spherePaths.

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
- Return ONLY JSON. No markdown. No commentary.`,
		query, quotedBlock, multiHint, existingDomainsBlock)
}

// looksLikeMultiEntityQuery is a cheap heuristic to hint the LLM to avoid collapsing multiple entities into one anchor.
// It is intentionally conservative (best-effort) and is only used for prompt conditioning.
func looksLikeMultiEntityQuery(query string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return false
	}
	// Strong separators often mean multiple entities.
	seps := []string{" and ", " vs ", " versus ", " & ", " + ", ",", " / ", " и ", " против "}
	for _, s := range seps {
		if strings.Contains(q, s) {
			return true
		}
	}
	// Person + institution keyword pattern.
	institutionWords := []string{
		" bank", " ministry", " authority", " regulator", " agency", " committee",
		" company", " inc", " llc", " ltd", " pbc", " corp", " plc",
		" university", " foundation", " fund",
	}
	for _, w := range institutionWords {
		if strings.Contains(q, w) && len(strings.Fields(q)) >= 3 {
			return true
		}
	}
	return false
}

// extractQuotedPhrases returns a best-effort list of quoted substrings, preserving original casing.
// Supports: "..." and «...».
func extractQuotedPhrases(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	out := []string{}
	seen := map[string]struct{}{}

	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}

	// Parse "...".
	for {
		i := strings.Index(s, `"`)
		if i < 0 {
			break
		}
		rest := s[i+1:]
		j := strings.Index(rest, `"`)
		if j < 0 {
			break
		}
		add(rest[:j])
		s = rest[j+1:]
		if len(out) >= 10 {
			return out
		}
	}

	// Parse «...».
	s2 := strings.TrimSpace(s)
	for {
		i := strings.Index(s2, "«")
		if i < 0 {
			break
		}
		rest := s2[i+len("«"):]
		j := strings.Index(rest, "»")
		if j < 0 {
			break
		}
		add(rest[:j])
		s2 = rest[j+len("»"):]
		if len(out) >= 10 {
			return out
		}
	}

	return out
}

// slugifyCanonicalPath normalizes each segment of a canonical path to handle LLM outputs
// with spaces, uppercase, or special characters.
// Example: "/loc/Saudi Arabia" -> "/loc/saudi-arabia"
// Example: "/org/Tesla, Inc." -> "/org/tesla-inc"
func slugifyCanonicalPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/") {
		return path
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 {
		return path
	}
	// Keep namespace as-is (it should already be lowercase), slugify rest
	out := make([]string, 0, len(parts))
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if i == 0 {
			// Namespace: just lowercase, no other changes
			out = append(out, strings.ToLower(p))
		} else {
			// Slugify: lowercase, replace non-alnum with hyphens
			out = append(out, taxonomy.Slugify(p))
		}
	}
	// Filter out empty segments
	filtered := make([]string, 0, len(out))
	for _, p := range out {
		if p != "" {
			filtered = append(filtered, p)
		}
	}
	if len(filtered) == 0 {
		return path
	}
	return "/" + strings.Join(filtered, "/")
}

// normalizeCanonicalPathForTools fixes MSYS/Git-Bash path conversion for canonical paths.
// Example: "/org/tesla" -> "C:/Program Files/Git/org/tesla" (we strip prefix).
func normalizeCanonicalPathForTools(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return s
	}
	// IMPORTANT: keep this list in sync with allowed canonical namespaces (including /gov).
	candidates := []string{"/gov/", "/org/", "/person/", "/product/", "/fac/", "/loc/", "/event/", "/sphere/", "/program/", "/market/", "/segment/", "/media/", "/domain/"}
	for _, c := range candidates {
		if idx := strings.Index(s, c); idx > 0 {
			s = s[idx:]
			break
		}
	}
	if !strings.HasPrefix(s, "/") {
		s = "/" + s
	}
	return s
}

// normalizeLegacyGeoPathForIntent maps older geo namespaces to the strict /loc/{country}/{admin1}/{city} model.
// Supported legacy patterns:
// - /region/na/{country}/{admin1}/{city} -> /loc/{country}/{admin1}/{city}
// - /region/{country}/{admin1}/{city}    -> /loc/{country}/{admin1}/{city}
func normalizeLegacyGeoPathForIntent(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "/loc/") {
		return p
	}
	if !strings.HasPrefix(p, "/region/") {
		return ""
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	// parts[0] == "region"
	if len(parts) == 5 && parts[1] == "na" {
		return fmt.Sprintf("/loc/%s/%s/%s", parts[2], parts[3], parts[4])
	}
	if len(parts) == 4 {
		return fmt.Sprintf("/loc/%s/%s/%s", parts[1], parts[2], parts[3])
	}
	return ""
}
