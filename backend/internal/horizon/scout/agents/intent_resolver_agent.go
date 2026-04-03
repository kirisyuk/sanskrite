package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/naming"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// IntentResolution is a lightweight, reusable representation of user intent.
// It is intentionally taxonomy-first: spherePaths must be canonical (/sphere/...), and anchors must be canonical paths.
type IntentResolution struct {
	Title       string   `json:"title"`
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
// Domain is config (contract), not a graph node; used for domainModes and contract rules.
type DomainResolution struct {
	CanonicalPath      string              `json:"canonicalPath" firestore:"canonicalPath"`
	ContractType       string              `json:"contractType" firestore:"contractType"` // allowlisted, must match /domain/v1/{contractType}/...
	Title              string              `json:"title" firestore:"title"`               // human-readable English title (not a slug)
	Description        string              `json:"description" firestore:"description"`   // actionable contract (“what to reveal/show”)
	Facets             map[string][]string `json:"facets,omitempty" firestore:"facets,omitempty"`
	Projections        []string            `json:"projections,omitempty" firestore:"projections,omitempty"`
	RelatedDomains     []string            `json:"relatedDomains,omitempty" firestore:"relatedDomains,omitempty"`         // canonical /domain/... paths
	AffectedDomains    []string            `json:"affectedDomains,omitempty" firestore:"affectedDomains,omitempty"`       // canonical /domain/... paths
	FirstOrderEntities []string            `json:"firstOrderEntities,omitempty" firestore:"firstOrderEntities,omitempty"` // canonical paths of entities to add to lens scope
	Confidence         float64             `json:"confidence" firestore:"confidence"`
	Reason             string              `json:"reason" firestore:"reason"`
	// SupplyChainExecutionPlan is populated when contractType is in supply-chain group.
	SupplyChainExecutionPlan *SupplyChainExecutionPlan `json:"supplyChainExecutionPlan,omitempty" firestore:"supplyChainExecutionPlan,omitempty"`
	// ServiceChainExecutionPlan is populated when contractType is in service-chain group.
	ServiceChainExecutionPlan *SupplyChainExecutionPlan `json:"serviceChainExecutionPlan,omitempty" firestore:"serviceChainExecutionPlan,omitempty"`
	// RegulatoryImpactExecutionPlan is populated when contractType is policy-impact or regulatory-impact.
	RegulatoryImpactExecutionPlan *RegulatoryImpactExecutionPlan `json:"regulatoryImpactExecutionPlan,omitempty" firestore:"regulatoryImpactExecutionPlan,omitempty"`
	// GovernanceExecutionPlan is populated when contractType is ownership-structure or control-map.
	GovernanceExecutionPlan *GovernanceExecutionPlan `json:"governanceExecutionPlan,omitempty" firestore:"governanceExecutionPlan,omitempty"`
	// EcosystemExecutionPlan is populated when contractType is ecosystem-mapping or influence-network.
	EcosystemExecutionPlan *EcosystemExecutionPlan `json:"ecosystemExecutionPlan,omitempty" firestore:"ecosystemExecutionPlan,omitempty"`
}

// SupplyChainExecutionPlan is a plan of executable supply chain queries.
// Generated when contractType is in supply-chain group (supply-chain, value-chain, logistics-flow, etc.).
type SupplyChainExecutionPlan struct {
	GraphBuildMode string                    `json:"graphBuildMode,omitempty" firestore:"graphBuildMode,omitempty"` // "hot_path_only" | "hot_path_plus_graph"
	Queries        []SupplyChainQuery        `json:"queries" firestore:"queries"`
	OverlayQueries []SupplyChainOverlayQuery `json:"overlayQueries,omitempty" firestore:"overlayQueries,omitempty"` // Stage 2, after physical chains
}

const (
	SupplyChainGraphBuildModeHotPathOnly      = "hot_path_only"
	SupplyChainGraphBuildModeHotPathPlusGraph = "hot_path_plus_graph"
)

// SupplyChainQuery defines one holistic supply chain request (anchor + direction + filters).
// Optional Level: 1 = top ORGs only (depth 1), 2 = ORG+FACs+products expansion (depth 2-3).
// When Anchors is non-empty, it takes precedence over Anchor.
// Supported anchor types are org, fac, deposit, and product.
type SupplyChainQuery struct {
	Anchor        string   `json:"anchor" firestore:"anchor"`                                   // canonical path (single). Used when Anchors is empty.
	Anchors       []string `json:"anchors,omitempty" firestore:"anchors,omitempty"`             // list of org paths for "from organizations" queries
	AnchorType    string   `json:"anchorType" firestore:"anchorType"`                           // "org" | "fac" | "product" | "deposit"
	Direction     string   `json:"direction" firestore:"direction"`                             // "down" (org→fac→product→fac→org tree). Legacy: "upstream"/"downstream"/"both" mapped to "down".
	Depth         int      `json:"depth" firestore:"depth"`                                     // 3-6
	Level         int      `json:"level,omitempty" firestore:"level,omitempty"`                 // optional: 1 = top ORGs, 2 = expansion (staged queries)
	ProductFilter []string `json:"productFilter,omitempty" firestore:"productFilter,omitempty"` // e.g. ["ev-batteries", "lithium-carbonate"]
	GeoFilter     []string `json:"geoFilter,omitempty" firestore:"geoFilter,omitempty"`         // e.g. ["de", "cn"]
	Reason        string   `json:"reason" firestore:"reason"`
}

// SupplyChainOverlayQuery is a Stage 2 query over parsed chains (regulatory, market, decision points).
type SupplyChainOverlayQuery struct {
	Type    string   `json:"type" firestore:"type"`       // "regulatory" | "market" | "decision_points"
	Anchors []string `json:"anchors" firestore:"anchors"` // from Stage 1 (org, market canonical paths)
	Scope   string   `json:"scope" firestore:"scope"`     // "markets" | "orgs" | "both"
}

// RegulatoryImpactExecutionPlan is a plan of regulatory/policy impact queries.
type RegulatoryImpactExecutionPlan struct {
	Queries []RegulatoryImpactQuery `json:"queries" firestore:"queries"`
}

type RegulatoryImpactQuery struct {
	Instrument   string            `json:"instrument" firestore:"instrument"`
	Jurisdiction string            `json:"jurisdiction" firestore:"jurisdiction"`
	Parameters   map[string]string `json:"parameters,omitempty" firestore:"parameters,omitempty"`
	ImpactScope  string            `json:"impactScope" firestore:"impactScope"`
	Depth        int               `json:"depth" firestore:"depth"`
	SectorFilter []string          `json:"sectorFilter,omitempty" firestore:"sectorFilter,omitempty"`
	Reason       string            `json:"reason" firestore:"reason"`
}

// GovernanceExecutionPlan is a plan for tracing ownership/control structures.
type GovernanceExecutionPlan struct {
	Queries []GovernanceQuery `json:"queries" firestore:"queries"`
}

type GovernanceQuery struct {
	Target       string   `json:"target" firestore:"target"`
	TargetType   string   `json:"targetType" firestore:"targetType"`
	Direction    string   `json:"direction" firestore:"direction"`
	Depth        int      `json:"depth" firestore:"depth"`
	IncludeBoard bool     `json:"includeBoard" firestore:"includeBoard"`
	GeoFilter    []string `json:"geoFilter,omitempty" firestore:"geoFilter,omitempty"`
	Reason       string   `json:"reason" firestore:"reason"`
}

// EcosystemExecutionPlan is a plan for mapping multi-actor ecosystems.
type EcosystemExecutionPlan struct {
	Queries []EcosystemQuery `json:"queries" firestore:"queries"`
}

type EcosystemQuery struct {
	Focus        string   `json:"focus" firestore:"focus"`
	FocusType    string   `json:"focusType" firestore:"focusType"`
	Depth        int      `json:"depth" firestore:"depth"`
	ActorTypes   []string `json:"actorTypes" firestore:"actorTypes"`
	SectorFilter []string `json:"sectorFilter,omitempty" firestore:"sectorFilter,omitempty"`
	GeoFilter    []string `json:"geoFilter,omitempty" firestore:"geoFilter,omitempty"`
	Reason       string   `json:"reason" firestore:"reason"`
}

// AffectedZoneQuery holds parameters for holistic affected-zone discovery.
// Geo: see taxonomy.ISO2GeoDescriptionForPrompts(). No execution plan needed—derived from contract facets.
type AffectedZoneQuery struct {
	Geo    string `json:"geo" firestore:"geo"`
	Reason string `json:"reason" firestore:"reason"`
	Title  string `json:"title,omitempty" firestore:"title,omitempty"`
}

// DecompositionHint describes how to decompose an abstract anchor (MARKET, PROGRAM, etc.)
// into more concrete entities for research and scouting.
type DecompositionHint struct {
	// AnchorPath is the canonicalPath of the anchor this hint applies to.
	AnchorPath string `json:"anchorPath" firestore:"anchorPath"`
	// EntityTypes suggest what types of entities should be discovered (e.g., ["ORGANIZATION", "PRODUCT"]).
	EntityTypes []string `json:"entityTypes" firestore:"entityTypes"`
	// SubMarkets suggest market segmentations to explore (e.g., ["upstream", "midstream", "downstream"] for energy; ["commercial", "retail"] for finance).
	SubMarkets []string `json:"subMarkets,omitempty" firestore:"subMarkets,omitempty"`
	// PlayerCategories suggest categories of players (e.g., ["local banks", "international banks", "fintechs"]).
	PlayerCategories []string `json:"playerCategories,omitempty" firestore:"playerCategories,omitempty"`
	// SearchQueries are concrete search queries to use for grounded research.
	SearchQueries []string `json:"searchQueries" firestore:"searchQueries"`
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
	initialProfile := inferIntentScenarioProfile(query, nil)

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
			PreferredModel:            "gemini-3-flash-preview",
			TextGenerationCapability:  true,
			SearchGroundingCapability: false,
			MinCapabilityScore:        7,
			MinContextWindow:          8000,
			MaxOutputSize:             4096, // domains + anchors + hints in one response
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
	out.Title = strings.TrimSpace(out.Title)
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
		// We normalize only supported legacy shapes into strict /loc/{country}[/{level1}[/{level2}]].
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
		an.Name = naming.StripParentheticalAliasAnnotations(strings.TrimSpace(an.Name))
		if an.Name == "" {
			an.Name = naming.BestEffortLabelFromCanonicalPath(cp)
		}
		if an.Name != "" && !naming.IsLatinScript(an.Name) {
			if fallback := naming.BestEffortLabelFromCanonicalPath(cp); fallback != "" {
				an.Name = fallback
			}
		}
		nAnchors = append(nAnchors, an)
		if len(nAnchors) >= initialProfile.anchorCap() {
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
			cp = repairSupplyChainDomainNamespacePath(cp)
			// Lenient: if LLM returns trailing facet key without value (odd segments), drop it.
			cp = trimDomainPathTrailingIncompleteFacet(cp)
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
			d.ContractType = normalizeContractTypeAlias(d.ContractType)
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
			// If the model omits title or returns a generic fallback ("Supply chain — BR"),
			// prefer the query-level focus text over contractType-based auto-titles.
			title := strings.TrimSpace(d.Title)
			focusTitle := buildDomainTitleFromFocusForIntent(out.Focus)
			if title == "" {
				if focusTitle != "" {
					d.Title = focusTitle
				} else {
					d.Title = buildDomainTitleForIntent(d.ContractType, d.CanonicalPath, d.Facets, countryNameByCC)
				}
			} else if isGenericDomainTitleForIntent(title, d.ContractType, d.CanonicalPath, d.Facets, countryNameByCC) {
				if focusTitle != "" {
					d.Title = focusTitle
				}
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
			if len(validDomains) >= initialProfile.domainCap() {
				break
			}
		}
		out.Domains = validDomains
		// Apply supply chain plan quality rule: cap queries, prioritize by firstOrderEntities.
		applySupplyChainPlanQualityRule(out.Domains, out.Anchors)
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
	applyIntentResolutionQualityRules(query, &out)

	// Must have at least one valid anchor or domain after normalization.
	if len(out.Anchors) == 0 && len(out.Domains) == 0 {
		return nil, fmt.Errorf("no valid anchors/domains after canonical validation")
	}
	out.Title = buildLensTitleForIntent(out.Title, out.Focus, query, out.Domains)

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
	anchorNamingBlock := naming.PromptAnchorNameRules()
	return fmt.Sprintf(`You are an INTENT RESOLVER for a risk/intelligence analysis system. Output ONLY JSON.

USER QUERY:
%s
%s
%s%sOUTPUT JSON SCHEMA:
{
  "title": "string (concise English lens title; this will be used directly as the lens name)",
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
      "reason": "short reason",
      "supplyChainExecutionPlan": {
        "graphBuildMode": "hot_path_plus_graph",
        "queries": [
          { "anchor": "/org/de/bmw", "anchorType": "org", "direction": "upstream", "depth": 4, "productFilter": ["ev-batteries"], "geoFilter": ["de", "cn"], "reason": "..." },
          { "anchors": ["/org/th/cp-group", "/org/th/riceland"], "anchorType": "org", "direction": "both", "depth": 4, "productFilter": ["rice"], "geoFilter": ["th"], "reason": "Thai rice producers" }
        ],
        "overlayQueries": [{ "type": "regulatory", "anchors": [], "scope": "orgs" }]
      },
      "serviceChainExecutionPlan": {
        "graphBuildMode": "hot_path_plus_graph",
        "queries": [
          { "anchor": "/org/us/google-cloud", "anchorType": "org", "direction": "both", "depth": 4, "productFilter": ["cloud-region-service"], "geoFilter": ["us"], "reason": "provider anchor" },
          { "anchor": "/fac/us/google-cloud-us-central1", "anchorType": "fac", "direction": "both", "depth": 4, "productFilter": ["compute-service"], "geoFilter": ["us"], "reason": "delivery facility anchor" }
        ],
        "overlayQueries": [{ "type": "market", "anchors": [], "scope": "both" }]
      },
      "regulatoryImpactExecutionPlan": {
        "queries": [
          { "instrument": "/mechanism/tax/vat-rate", "jurisdiction": "tr", "parameters": {"rate_from": "18%%", "rate_to": "20%%"}, "impactScope": "sector-specific", "depth": 4, "sectorFilter": ["energy"], "reason": "..." }
        ]
      },
      "governanceExecutionPlan": {
        "queries": [
          { "target": "/org/tr/botas", "targetType": "org", "direction": "upward", "depth": 5, "includeBoard": true, "geoFilter": ["tr"], "reason": "..." }
        ]
      },
      "ecosystemExecutionPlan": {
        "queries": [
          { "focus": "/market/semiconductor", "focusType": "sector", "depth": 4, "actorTypes": ["ORGANIZATION","GOV","FACILITY"], "sectorFilter": ["semiconductors"], "geoFilter": [], "reason": "..." }
        ]
      }
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
- title MUST be a concise English lens title derived from the user query. It will be used directly as the lens name.
- A DOMAIN is an executable knowledge contract: "what the user wants to know" and how to explore it.
- Use domains when the query is analytical or a research focus that must be expanded into multiple entities.
- If a matching DOMAIN already exists, reuse the SAME canonicalPath.
- DOMAIN canonicalPath: /domain/v1/{contractType}/{facetKey}/{facetValue}...
  Examples: /domain/v1/ecosystem-mapping/geo/my/industry/energy, /domain/v1/bilateral-relations/geo/sa-ye
- facetKey MUST be one of: geo, topic, subject, project, asset, material, product, commodity, crop, goods, item, application, sector, industry, market, regulation, framework, time
- contractType MUST be one of (prefer canonical types; aliases accepted but auto-normalized):
  OPERATIONAL CHAINS: supply-chain, service-chain, value-chain (aliases, avoid if possible: logistics-flow, trade-flow, sourcing-map, procurement-map; service aliases: service-delivery-network, capability-chain)
  RISK/GEOPOLITICS: geopolitical-risk, bilateral-relations, sanctions-exposure, affected-zone (aliases: risk-assessment, conflict-dynamics)
  MARKETS: market-opportunity, competitive-landscape, geographic-concentration (aliases: market-entry, market-sizing → market-opportunity; demand-forecast, pricing-dynamics → competitive-landscape)
  INVESTMENT: investment-thesis, portfolio-exposure (aliases: funding-landscape, exit-strategy, valuation-drivers → investment-thesis)
  OWNERSHIP/STRUCTURE: ownership-structure, control-map
  TECHNOLOGY: technology-landscape, innovation-map, patent-landscape, rd-landscape, adoption-curve, disruption-analysis
  ECOSYSTEM: ecosystem-mapping, influence-network (aliases: stakeholder-mapping, partnership-landscape, power-dynamics → ecosystem-mapping)
  RELATIONSHIP/COMMONALITY: commonality-analysis
  POLICY: policy-impact, regulatory-impact (aliases: policy-and-regulation, compliance-landscape, security-and-compliance, licensing-requirements, esg-landscape → policy-impact)
  SCENARIOS: scenario-analysis, future-outlook (aliases: macro-trends, megatrends → scenario-analysis)
  INDUSTRY: industry-analysis
  FALLBACK: other (only if none above fit): /domain/v1/other/{custom-slug}/...
- SUPPLY CHAIN PLAN: When contractType is supply-chain or value-chain, you MUST include supplyChainExecutionPlan with 1-10 queries. CRITICAL: The FIRST queries must go FROM ORGANIZATIONS (as producers/suppliers for downstream, or as consumers for upstream). Supply chains are actor-centric: org→product→org. direction: use "upstream" when the query asks about suppliers, raw materials, inputs, "who supplies X", "where does X get its materials", sourcing; use "downstream" when asking about customers, buyers, "who buys X", distribution; use "both" for full chain mapping. For upstream: anchors are the CONSUMER orgs (e.g. Coca-Cola for "who supplies Coca-Cola"). For downstream: anchors are PRODUCER orgs. Organization-focused supply-chain intents are valid: keep the organization in anchors / supplyChainExecutionPlan, and if canonicalPath must encode it, use facet form like /domain/v1/supply-chain/geo/us/subject/apple-inc. Do NOT emit /domain/v1/supply-chain/org/us/apple-inc/... . For product+geo queries (e.g. "Thai rice supply chain"): contractType MUST be supply-chain, facets MUST include geo and product or material. anchorType MUST be org. Use anchors (or anchors array) with canonical /org/... paths. Each query: anchor (single) or anchors (array), anchorType (org|product|deposit), direction (upstream|downstream|both), depth (3-6), productFilter/geoFilter from facets, reason. overlayQueries (Stage 2) optional. graphBuildMode MUST be "hot_path_only" for fast, hot-path-first answers when only the primary chain is needed, and "hot_path_plus_graph" when the system should continue with broader graph completion; default to "hot_path_plus_graph" when unsure.
- SERVICE CHAIN PLAN: When contractType is service-chain, you MUST include serviceChainExecutionPlan with 1-10 queries. Treat service-chain as a distinct operational contract for service delivery lifecycle, not as value analysis. The FIRST queries should prefer provider organizations or concrete delivery facilities. Focus on provider→capability→facility/control-gate→recipient paths, not profit pools or margin capture. Use direction "upstream" for dependencies, control gates, required providers, support vendors, or access prerequisites; "downstream" for customers, tenants, recipients, and exposed services; "both" for full delivery mapping. Use canonical /org/... or /fac/... anchors when possible. Each query: anchor (single) or anchors (array), anchorType (org|fac|product), direction (upstream|downstream|both), depth (3-6), productFilter/geoFilter from facets, reason. overlayQueries optional. graphBuildMode MUST be "hot_path_only" or "hot_path_plus_graph"; default to "hot_path_plus_graph" when unsure.
- ROUTE-SENSITIVE SUPPLY CHAIN: If the query mentions chokepoints, shipping routes, sea lanes, ports, terminals, pipelines, carriers, rerouting, "through/via", LNG/LPG shipping, or "from production to end-users", structure the intent for path execution. Include 3-8 anchors when evidence exists across these roles: producer org, upstream deposit/field, export facility, chokepoint/route geo, import terminal or market hub, downstream industrial consumer.
- SERVICE VS VALUE CHAIN: Use service-chain for service delivery topology, dependencies, access/control gates, service facilities, coverage, and downstream recipients. Use value-chain for economic value creation/capture, profit pools, bargaining power, and margin structure. Do not substitute one for the other.
- REGULATORY IMPACT PLAN: When contractType is policy-impact or regulatory-impact, you MUST include regulatoryImpactExecutionPlan with 1-5 queries. Each query: instrument (canonical path /mechanism/... or /program/...), jurisdiction (ISO2), parameters (rate_from, rate_to, effective_date when applicable), impactScope (economy-wide|sector-specific|entity-specific), depth (3-5), sectorFilter from facets, reason.
- POLICY / REGULATORY TRANSMISSION: If the query asks what happens when a tax, tariff, VAT, CBAM-like cost, export control, sanctions measure, subsidy, capital rule, customs rule, licensing requirement, or compliance burden changes, include both the policy side and the exposed operational side. Do not return only the regulator/mechanism anchor.
	- GOVERNANCE PLAN: When contractType is ownership-structure or control-map, you MUST include governanceExecutionPlan with 1-5 queries. Each query: target (canonical path /org/... or /gov/... or /community/... for dynasty/tribe/family ownership), targetType (org|gov|person|community), direction (upward|downward|both), depth (3-6), includeBoard (true/false), geoFilter, reason.
	- ECOSYSTEM PLAN: When contractType is ecosystem-mapping or influence-network, you MUST include ecosystemExecutionPlan with 1-5 queries. Each query: focus (canonical path or description), focusType (geo|sector|market|org|community), depth (3-5), actorTypes (subset of [ORGANIZATION, GOV, PERSON, FACILITY, COMMUNITY]), sectorFilter, geoFilter, reason.
- AFFECTED-ZONE / EXPOSURE: For conflict, disaster, earthquake, flood, tension, humanitarian, and security-zone queries, include anchors for zone geography plus exposed operators, facilities, markets, routes, or chokepoints when inferable. Do not reduce the intent to only /loc/... if the query implies operational exposure.
- FINANCE / MARKET INFRASTRUCTURE: For banking, payments, insurance, exchanges, clearing, custody, portfolio exposure, or trade-finance questions, include anchors across the transmission chain when inferable: institutions, market infrastructure, regulators, sectors, and exposed corporate endpoints.
- INDUSTRY ANALYSIS: For industry-analysis and competitive-landscape queries, include anchors across the chain when inferable: producers, facilities, markets, logistics hubs, major downstream consumers, and relevant regulators.
- BILATERAL: If query is about relations between two countries → contractType bilateral-relations, geo = {cc1}-{cc2} (alphabetically sorted lowercase ISO2).
- AFFECTED ZONE: If query asks about conflict zones, military conflicts, disaster zones, earthquake-affected areas, tension zones, humanitarian crisis areas, flood zones → contractType affected-zone, facet geo. geo: " + taxonomy.ISO2GeoDescriptionForPrompts() + ". Examples: /domain/v1/affected-zone/geo/tr-sy, /domain/v1/affected-zone/geo/sy, /domain/v1/affected-zone/geo/africa, /domain/v1/affected-zone/geo/ssa.
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
%s

ORGANIZATION: /org/{geo}/{slug} (e.g., /org/sa/saudi-aramco). GOV bodies: /gov/{geo}/{slug}. Use concrete evidence-backed geo whenever possible. /org/na/{slug} or /gov/na/{slug} is allowed ONLY as an exceptional last-resort when neither context nor the entity name provides a defensible jurisdiction clue.
FACILITY: /fac/{geo}/{facilitySlug}. geo like org/gov (ISO2, na, or regional). /fac/na is LAST-RESORT ONLY after checking facility name, site/city mention, operator context, and source text for geography. If the name contains a clear place clue, infer the ISO2 instead of using na.
GEO_REGION: /loc/{countryAlpha2}[/{level1}[/{level2}]] — omit unknown trailing levels. Country-only: /loc/{cc}. Regional: /loc/gcc.
PRODUCT: /product/{category}/{type}/{stage}/{slug} — category: raw|component|finished|service. For services, use category=service and map service domain/subdomain into {type}/{stage}.
MARKET: /market/{geo}/{domain}/{vertical} — geo: " + taxonomy.ISO2GeoDescriptionForPrompts() + ". Inherit country ISO2 from query when geographic context is present.
PROGRAM: /program/{jurisdictionSlug}/{programSlug}. If jurisdiction is unknown, use /program/global/{programSlug}
PERSON: /person/{slug}
EVENT: /event/{slug}
MEDIA_CHANNEL: /media/{platform}/{slug}

MARKET GEOGRAPHY: When query mentions a specific country AND a market → MARKET anchor MUST use that country's ISO2 as geo (NOT "global").

DECOMPOSITION HINTS (REQUIRED for MARKET/PROGRAM anchors):
- entityTypes: what to discover. subMarkets: segments. playerCategories: player types.
- searchQueries: 3-5 concrete English web search queries designed to yield real-world entity names (NOT abstract "market size" queries).

GENERAL:
- Provide 1-3 anchors for narrow entity questions. Provide 3-8 anchors for route-sensitive, transmission-heavy, affected-zone, finance/market-infrastructure, or sector-structure queries when evidence exists.
- Prefer stable, specific anchors.
- Return ONLY JSON. No markdown. No commentary.`, query, existingDomainsBlock, quotedBlock, multiHint, anchorNamingBlock)
}

// contractTypeAliases maps alias contract types to their canonical forms.
// All aliases are still accepted by the LLM prompt and validation, but
// post-processing normalizes them so downstream pipeline logic only sees canonical types.
var contractTypeAliases = map[string]string{
	// Supply chain aliases -> supply-chain
	"logistics-flow":  "supply-chain",
	"trade-flow":      "supply-chain",
	"sourcing-map":    "supply-chain",
	"procurement-map": "supply-chain",
	// Service chain aliases -> service-chain
	"service-delivery-network": "service-chain",
	"capability-chain":         "service-chain",
	// Risk aliases -> geopolitical-risk
	"risk-assessment":   "geopolitical-risk",
	"conflict-dynamics": "geopolitical-risk",
	// Policy aliases -> policy-impact
	"policy-and-regulation":   "policy-impact",
	"compliance-landscape":    "policy-impact",
	"security-and-compliance": "policy-impact",
	"licensing-requirements":  "policy-impact",
	"esg-landscape":           "policy-impact",
	// Market aliases
	"market-entry":     "market-opportunity",
	"market-sizing":    "market-opportunity",
	"demand-forecast":  "competitive-landscape",
	"pricing-dynamics": "competitive-landscape",
	// Investment aliases -> investment-thesis
	"funding-landscape": "investment-thesis",
	"exit-strategy":     "investment-thesis",
	"valuation-drivers": "investment-thesis",
	// Ecosystem aliases -> ecosystem-mapping
	"stakeholder-mapping":   "ecosystem-mapping",
	"partnership-landscape": "ecosystem-mapping",
	"power-dynamics":        "ecosystem-mapping",
	// Scenario aliases -> scenario-analysis
	"macro-trends": "scenario-analysis",
	"megatrends":   "scenario-analysis",
}

// normalizeContractTypeAlias maps alias contract types to their canonical form.
// If the contract type is already canonical, it is returned unchanged.
func normalizeContractTypeAlias(ct string) string {
	ct = strings.ToLower(strings.TrimSpace(ct))
	if canonical, ok := contractTypeAliases[ct]; ok {
		return canonical
	}
	return ct
}

// IsSupplyChainContractType returns true if contractType is in the supply-chain group.
func IsSupplyChainContractType(contractType string) bool {
	ct := normalizeContractTypeAlias(contractType)
	switch ct {
	case "supply-chain", "value-chain":
		return true
	}
	return false
}

func IsServiceChainContractType(contractType string) bool {
	return normalizeContractTypeAlias(contractType) == "service-chain"
}

func IsOperationalChainContractType(contractType string) bool {
	return IsSupplyChainContractType(contractType) || IsServiceChainContractType(contractType)
}

// IsPolicyImpactContractType returns true if contractType is in the policy/regulatory impact group.
func IsPolicyImpactContractType(contractType string) bool {
	ct := normalizeContractTypeAlias(contractType)
	switch ct {
	case "policy-impact", "regulatory-impact":
		return true
	}
	return false
}

// IsGovernanceContractType returns true if contractType is in the governance/ownership group.
func IsGovernanceContractType(contractType string) bool {
	ct := normalizeContractTypeAlias(contractType)
	switch ct {
	case "ownership-structure", "control-map":
		return true
	}
	return false
}

// IsEcosystemContractType returns true if contractType is in the ecosystem group.
func IsEcosystemContractType(contractType string) bool {
	ct := normalizeContractTypeAlias(contractType)
	switch ct {
	case "ecosystem-mapping", "influence-network":
		return true
	}
	return false
}

// IsAffectedZoneContractType returns true if contractType is affected-zone.
func IsAffectedZoneContractType(contractType string) bool {
	return normalizeContractTypeAlias(contractType) == "affected-zone"
}

const maxSupplyChainQueriesPerPlan = 10

type intentScenarioProfile struct {
	supply         bool
	service        bool
	value          bool
	routeSensitive bool
	affectedZone   bool
	policy         bool
	governance     bool
	ecosystem      bool
	investment     bool
	marketInfra    bool
	sector         bool
}

func inferIntentScenarioProfile(query string, domains []DomainResolution) intentScenarioProfile {
	text := strings.ToLower(strings.TrimSpace(query))
	p := intentScenarioProfile{}
	for _, d := range domains {
		switch normalizeContractTypeAlias(d.ContractType) {
		case "supply-chain":
			p.supply = true
		case "value-chain":
			p.value = true
		case "service-chain":
			p.service = true
		case "affected-zone", "geopolitical-risk", "bilateral-relations", "sanctions-exposure":
			p.affectedZone = true
		case "policy-impact", "regulatory-impact":
			p.policy = true
		case "ownership-structure", "control-map":
			p.governance = true
		case "ecosystem-mapping", "influence-network", "commonality-analysis":
			p.ecosystem = true
		case "investment-thesis", "portfolio-exposure":
			p.investment = true
		case "industry-analysis", "competitive-landscape", "market-opportunity":
			p.sector = true
		}
	}
	if containsAnyIntentToken(text, "supply chain", "downstream", "upstream", "industrial end-users", "industrial consumers", "producers", "receiving terminal", "export terminal", "raw materials", "buyers", "who buys", "who supplies", "distribution") {
		p.supply = true
	}
	if containsAnyIntentToken(text, "value chain", "margin pool", "profit pool", "value capture", "value creation", "rent capture", "bargaining power", "commoditized layer", "margin leakage") {
		p.value = true
	}
	if containsAnyIntentToken(text, "service chain", "service delivery", "service-delivery", "capability chain", "delivery lifecycle", "managed service", "service dependency", "support contract", "support response", "authorization gate", "access control gate", "control plane", "service outage", "service exposure", "cloud region", "data center service", "field service", "aftermarket service", "delivery path") {
		p.service = true
	}
	if containsAnyIntentToken(text, "route", "shipping", "sea lane", "pipeline", "port", "terminal", "carrier", "strait", "canal", "reroute", "transit", "through ", "via ", "crossing", "lng", "lpg", "container") {
		p.routeSensitive = true
	}
	if containsAnyIntentToken(text, "conflict", "tension", "earthquake", "flood", "humanitarian", "security incidents", "security incident", "war", "affected zone", "conflict zone", "disaster", "military escalation", "escalation") {
		p.affectedZone = true
	}
	if containsAnyIntentToken(text, "vat", "tax", "tariff", "cbam", "carbon border", "export control", "sanction", "licensing", "license", "regulation", "regulatory", "subsid", "customs", "capital rule", "compliance") {
		p.policy = true
	}
	if containsAnyIntentToken(text, "ownership", "owned by", "who owns", "control map", "shareholder", "board", "beneficial owner", "ultimate owner", "controlled by") {
		p.governance = true
	}
	if containsAnyIntentToken(text, "ecosystem", "stakeholder", "influence network", "partnership", "power dynamics", "commonality", "what connects") {
		p.ecosystem = true
	}
	if containsAnyIntentToken(text, "portfolio", "valuation", "funding", "bank", "payments", "insurance", "exchange", "clearing", "custody", "trade finance", "lender", "credit") {
		p.investment = true
	}
	if containsAnyIntentToken(text, "payments", "clearing", "custody", "exchange", "trading venue", "market infrastructure", "payment rail", "correspondent banking") {
		p.marketInfra = true
	}
	if containsAnyIntentToken(text, "sector", "industry", "competitive landscape", "market landscape", "industry structure", "vertical") {
		p.sector = true
	}
	return p
}

func (p intentScenarioProfile) anchorCap() int {
	switch {
	case p.routeSensitive || p.affectedZone || p.policy || p.marketInfra || p.investment:
		return 8
	case p.supply || p.service || p.value || p.sector || p.ecosystem || p.governance:
		return 6
	default:
		return 5
	}
}

func (p intentScenarioProfile) domainCap() int {
	switch {
	case p.routeSensitive || p.policy || p.affectedZone || p.investment || p.marketInfra:
		return 5
	case p.supply || p.service || p.value || p.sector || p.ecosystem || p.governance:
		return 4
	default:
		return 3
	}
}

func containsAnyIntentToken(text string, tokens ...string) bool {
	for _, token := range tokens {
		if strings.Contains(text, strings.ToLower(strings.TrimSpace(token))) {
			return true
		}
	}
	return false
}

// applySupplyChainPlanQualityRule caps queries at budget and prioritizes by firstOrderEntities.
func applySupplyChainPlanQualityRule(domains []DomainResolution, anchors []struct {
	CanonicalPath string  `json:"canonicalPath"`
	Type          string  `json:"type"`
	Name          string  `json:"name,omitempty"`
	Confidence    float64 `json:"confidence"`
	Reason        string  `json:"reason"`
}) {
	firstOrderSet := make(map[string]struct{})
	for _, a := range anchors {
		cp := strings.TrimSpace(a.CanonicalPath)
		if cp != "" {
			firstOrderSet[cp] = struct{}{}
		}
	}
	for _, d := range domains {
		for _, e := range d.FirstOrderEntities {
			cp := strings.TrimSpace(e)
			if cp != "" {
				firstOrderSet[cp] = struct{}{}
			}
		}
	}
	for i := range domains {
		d := &domains[i]
		plans := []*SupplyChainExecutionPlan{d.SupplyChainExecutionPlan, d.ServiceChainExecutionPlan}
		for _, plan := range plans {
			if plan == nil || len(plan.Queries) == 0 {
				continue
			}
			queries := plan.Queries
			if len(queries) <= maxSupplyChainQueriesPerPlan {
				continue
			}
			// Prioritize: queries whose anchor(s) are in firstOrderEntities first, then by order.
			anchorInSet := func(q *SupplyChainQuery) bool {
				if cp := strings.TrimSpace(q.Anchor); cp != "" {
					if _, ok := firstOrderSet[cp]; ok {
						return true
					}
				}
				for _, cp := range q.Anchors {
					if _, ok := firstOrderSet[strings.TrimSpace(cp)]; ok {
						return true
					}
				}
				return false
			}
			sort.Slice(queries, func(a, b int) bool {
				inA, inB := anchorInSet(&queries[a]), anchorInSet(&queries[b])
				if inA != inB {
					return inA
				}
				return a < b
			})
			plan.Queries = queries[:maxSupplyChainQueriesPerPlan]
		}
	}
}

func applyIntentResolutionQualityRules(query string, out *IntentResolution) {
	if out == nil {
		return
	}
	profile := inferIntentScenarioProfile(query, out.Domains)
	if capN := profile.anchorCap(); len(out.Anchors) > capN {
		out.Anchors = out.Anchors[:capN]
	}
	if capN := profile.domainCap(); len(out.Domains) > capN {
		out.Domains = prioritizeIntentDomains(out.Domains, profile)[:capN]
	}
	for i := range out.Domains {
		d := &out.Domains[i]
		d.ContractType = normalizeContractTypeAlias(d.ContractType)
		switch {
		case IsSupplyChainContractType(d.ContractType):
			applySupplyChainDomainQuality(query, d, out.Anchors, profile)
		case IsServiceChainContractType(d.ContractType):
			applyServiceChainDomainQuality(query, d, out.Anchors, profile)
		case IsPolicyImpactContractType(d.ContractType):
			applyPolicyImpactDomainQuality(query, d, out.Anchors, profile)
		case IsGovernanceContractType(d.ContractType):
			applyGovernanceDomainQuality(query, d, out.Anchors)
		case IsEcosystemContractType(d.ContractType):
			applyEcosystemDomainQuality(query, d, out.Anchors, profile)
		case IsAffectedZoneContractType(d.ContractType):
			applyAffectedZoneDomainQuality(d, out.Anchors, profile)
		}
	}
}

func prioritizeIntentDomains(domains []DomainResolution, profile intentScenarioProfile) []DomainResolution {
	out := append([]DomainResolution(nil), domains...)
	score := func(d DomainResolution) int {
		ct := normalizeContractTypeAlias(d.ContractType)
		switch {
		case profile.service && ct == "service-chain":
			return 100
		case profile.value && ct == "value-chain":
			return 99
		case profile.supply && ct == "supply-chain":
			return 98
		case (profile.service || profile.value || profile.supply) && IsOperationalChainContractType(ct):
			return 97
		case profile.policy && (ct == "policy-impact" || ct == "regulatory-impact"):
			return 95
		case profile.governance && (ct == "ownership-structure" || ct == "control-map"):
			return 92
		case profile.affectedZone && ct == "affected-zone":
			return 90
		case profile.ecosystem && (ct == "ecosystem-mapping" || ct == "influence-network"):
			return 88
		case profile.investment && (ct == "investment-thesis" || ct == "portfolio-exposure"):
			return 85
		case profile.sector && (ct == "industry-analysis" || ct == "competitive-landscape"):
			return 80
		default:
			return 50
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return score(out[i]) > score(out[j])
	})
	return out
}

func applySupplyChainDomainQuality(
	query string,
	d *DomainResolution,
	anchors []struct {
		CanonicalPath string  `json:"canonicalPath"`
		Type          string  `json:"type"`
		Name          string  `json:"name,omitempty"`
		Confidence    float64 `json:"confidence"`
		Reason        string  `json:"reason"`
	},
	profile intentScenarioProfile,
) {
	if d == nil {
		return
	}
	d.Projections = dedupStrings(append(d.Projections,
		"ORGANIZATION", "FACILITY", "PRODUCT", "MARKET", "GEO_REGION", "DEPOSIT"), 12)
	d.FirstOrderEntities = normalizeCanonicalPathsList(append(d.FirstOrderEntities,
		collectIntentAnchorPathsByPrefix(anchors, "/org/", "/fac/", "/deposit/", "/market/", "/loc/")...), 16)
	if d.SupplyChainExecutionPlan == nil || len(d.SupplyChainExecutionPlan.Queries) == 0 {
		if plan := synthesizeSupplyChainExecutionPlan(query, d, anchors, profile); plan != nil {
			d.SupplyChainExecutionPlan = plan
		}
	} else {
		d.SupplyChainExecutionPlan.GraphBuildMode = normalizeSupplyChainGraphBuildMode(d.SupplyChainExecutionPlan.GraphBuildMode)
		for i := range d.SupplyChainExecutionPlan.Queries {
			if d.SupplyChainExecutionPlan.Queries[i].Depth <= 0 {
				d.SupplyChainExecutionPlan.Queries[i].Depth = 4
			}
			if strings.TrimSpace(d.SupplyChainExecutionPlan.Queries[i].Direction) == "" {
				d.SupplyChainExecutionPlan.Queries[i].Direction = inferOperationalChainDirection(query, false)
			}
		}
		if profile.routeSensitive && len(d.SupplyChainExecutionPlan.OverlayQueries) == 0 {
			overlayAnchors := collectIntentAnchorPathsByPrefix(anchors, "/org/", "/fac/", "/market/")
			if len(overlayAnchors) > 0 {
				d.SupplyChainExecutionPlan.OverlayQueries = []SupplyChainOverlayQuery{{
					Type:    "market",
					Anchors: overlayAnchors[:minInt(len(overlayAnchors), 6)],
					Scope:   "both",
				}}
			}
		}
	}
	if d.SupplyChainExecutionPlan != nil {
		d.SupplyChainExecutionPlan.GraphBuildMode = normalizeSupplyChainGraphBuildMode(d.SupplyChainExecutionPlan.GraphBuildMode)
	}
}

func applyServiceChainDomainQuality(
	query string,
	d *DomainResolution,
	anchors []struct {
		CanonicalPath string  `json:"canonicalPath"`
		Type          string  `json:"type"`
		Name          string  `json:"name,omitempty"`
		Confidence    float64 `json:"confidence"`
		Reason        string  `json:"reason"`
	},
	profile intentScenarioProfile,
) {
	if d == nil {
		return
	}
	d.Projections = dedupStrings(append(d.Projections,
		"ORGANIZATION", "FACILITY", "PRODUCT", "MARKET", "GEO_REGION", "PROGRAM", "GOV"), 12)
	d.FirstOrderEntities = normalizeCanonicalPathsList(append(d.FirstOrderEntities,
		collectIntentAnchorPathsByPrefix(anchors, "/org/", "/fac/", "/product/", "/market/", "/program/", "/gov/", "/loc/")...), 16)
	if d.ServiceChainExecutionPlan == nil || len(d.ServiceChainExecutionPlan.Queries) == 0 {
		if plan := synthesizeServiceChainExecutionPlan(query, d, anchors, profile); plan != nil {
			d.ServiceChainExecutionPlan = plan
		}
	} else {
		d.ServiceChainExecutionPlan.GraphBuildMode = normalizeSupplyChainGraphBuildMode(d.ServiceChainExecutionPlan.GraphBuildMode)
		for i := range d.ServiceChainExecutionPlan.Queries {
			if d.ServiceChainExecutionPlan.Queries[i].Depth <= 0 {
				d.ServiceChainExecutionPlan.Queries[i].Depth = 4
			}
			if strings.TrimSpace(d.ServiceChainExecutionPlan.Queries[i].Direction) == "" {
				d.ServiceChainExecutionPlan.Queries[i].Direction = inferOperationalChainDirection(query, true)
			}
		}
		if (profile.routeSensitive || profile.service) && len(d.ServiceChainExecutionPlan.OverlayQueries) == 0 {
			overlayAnchors := collectIntentAnchorPathsByPrefix(anchors, "/org/", "/fac/", "/market/")
			if len(overlayAnchors) > 0 {
				d.ServiceChainExecutionPlan.OverlayQueries = []SupplyChainOverlayQuery{{
					Type:    "market",
					Anchors: overlayAnchors[:minInt(len(overlayAnchors), 6)],
					Scope:   "both",
				}}
			}
		}
	}
	if d.ServiceChainExecutionPlan != nil {
		d.ServiceChainExecutionPlan.GraphBuildMode = normalizeSupplyChainGraphBuildMode(d.ServiceChainExecutionPlan.GraphBuildMode)
	}
}

func applyPolicyImpactDomainQuality(
	query string,
	d *DomainResolution,
	anchors []struct {
		CanonicalPath string  `json:"canonicalPath"`
		Type          string  `json:"type"`
		Name          string  `json:"name,omitempty"`
		Confidence    float64 `json:"confidence"`
		Reason        string  `json:"reason"`
	},
	profile intentScenarioProfile,
) {
	if d == nil {
		return
	}
	d.Projections = dedupStrings(append(d.Projections,
		"GOV", "ORGANIZATION", "MARKET", "FACILITY", "PRODUCT", "PROGRAM"), 12)
	d.FirstOrderEntities = normalizeCanonicalPathsList(append(d.FirstOrderEntities,
		collectIntentAnchorPathsByPrefix(anchors, "/gov/", "/org/", "/market/", "/product/", "/program/", "/loc/")...), 16)
	if d.RegulatoryImpactExecutionPlan == nil || len(d.RegulatoryImpactExecutionPlan.Queries) == 0 {
		if plan := synthesizeRegulatoryImpactExecutionPlan(query, d, anchors, profile); plan != nil {
			d.RegulatoryImpactExecutionPlan = plan
		}
	}
}

func applyGovernanceDomainQuality(
	query string,
	d *DomainResolution,
	anchors []struct {
		CanonicalPath string  `json:"canonicalPath"`
		Type          string  `json:"type"`
		Name          string  `json:"name,omitempty"`
		Confidence    float64 `json:"confidence"`
		Reason        string  `json:"reason"`
	},
) {
	if d == nil {
		return
	}
	d.Projections = dedupStrings(append(d.Projections,
		"ORGANIZATION", "GOV", "PERSON", "FACILITY", "PROGRAM", "COMMUNITY"), 12)
	d.FirstOrderEntities = normalizeCanonicalPathsList(append(d.FirstOrderEntities,
		collectIntentAnchorPathsByPrefix(anchors, "/org/", "/gov/", "/person/", "/fac/", "/community/")...), 16)
	if d.GovernanceExecutionPlan == nil || len(d.GovernanceExecutionPlan.Queries) == 0 {
		if plan := synthesizeGovernanceExecutionPlan(query, d, anchors); plan != nil {
			d.GovernanceExecutionPlan = plan
		}
	}
}

func applyEcosystemDomainQuality(
	query string,
	d *DomainResolution,
	anchors []struct {
		CanonicalPath string  `json:"canonicalPath"`
		Type          string  `json:"type"`
		Name          string  `json:"name,omitempty"`
		Confidence    float64 `json:"confidence"`
		Reason        string  `json:"reason"`
	},
	profile intentScenarioProfile,
) {
	if d == nil {
		return
	}
	d.Projections = dedupStrings(append(d.Projections,
		"ORGANIZATION", "GOV", "FACILITY", "MARKET", "PRODUCT", "GEO_REGION", "COMMUNITY"), 12)
	d.FirstOrderEntities = normalizeCanonicalPathsList(append(d.FirstOrderEntities,
		collectIntentAnchorPathsByPrefix(anchors, "/org/", "/gov/", "/fac/", "/market/", "/product/", "/loc/", "/community/")...), 16)
	if d.EcosystemExecutionPlan == nil || len(d.EcosystemExecutionPlan.Queries) == 0 {
		if plan := synthesizeEcosystemExecutionPlan(query, d, anchors, profile); plan != nil {
			d.EcosystemExecutionPlan = plan
		}
	}
}

func applyAffectedZoneDomainQuality(
	d *DomainResolution,
	anchors []struct {
		CanonicalPath string  `json:"canonicalPath"`
		Type          string  `json:"type"`
		Name          string  `json:"name,omitempty"`
		Confidence    float64 `json:"confidence"`
		Reason        string  `json:"reason"`
	},
	profile intentScenarioProfile,
) {
	if d == nil {
		return
	}
	d.Projections = dedupStrings(append(d.Projections,
		"GEO_REGION", "FACILITY", "ORGANIZATION", "MARKET", "TRACK", "EVENT"), 12)
	prefixes := []string{"/loc/", "/zone/", "/fac/", "/org/", "/market/", "/track/"}
	if profile.routeSensitive {
		prefixes = append(prefixes, "/deposit/")
	}
	d.FirstOrderEntities = normalizeCanonicalPathsList(append(d.FirstOrderEntities,
		collectIntentAnchorPathsByPrefix(anchors, prefixes...)...), 16)
}

func collectIntentAnchorPathsByPrefix(
	anchors []struct {
		CanonicalPath string  `json:"canonicalPath"`
		Type          string  `json:"type"`
		Name          string  `json:"name,omitempty"`
		Confidence    float64 `json:"confidence"`
		Reason        string  `json:"reason"`
	},
	prefixes ...string,
) []string {
	if len(prefixes) == 0 {
		return nil
	}
	out := make([]string, 0, len(anchors))
	for _, a := range anchors {
		cp := strings.TrimSpace(a.CanonicalPath)
		for _, prefix := range prefixes {
			if strings.HasPrefix(cp, prefix) {
				out = append(out, cp)
				break
			}
		}
	}
	return dedupStrings(out, len(out))
}

func inferOperationalChainDirection(query string, serviceMode bool) string {
	text := strings.ToLower(strings.TrimSpace(query))
	if serviceMode {
		switch {
		case containsAnyIntentToken(text, "depends on", "dependency", "dependencies", "upstream provider", "control gate", "access gate", "authorization", "service contract", "support vendor", "identity provider", "network provider"):
			return "upstream"
		case containsAnyIntentToken(text, "customer", "customers", "tenant", "tenants", "recipient", "recipients", "served by", "service exposure", "end-user", "user-facing", "delivery to"):
			return "downstream"
		}
	}
	switch {
	case containsAnyIntentToken(text, "supplier", "suppliers", "inputs", "source from", "sourcing", "where does", "upstream"):
		return "upstream"
	case containsAnyIntentToken(text, "buyers", "customers", "distribution", "who buys", "end-users", "downstream"):
		return "downstream"
	default:
		return "both"
	}
}

func inferSupplyChainDirection(query string) string {
	return inferOperationalChainDirection(query, false)
}

func synthesizeSupplyChainExecutionPlan(
	query string,
	d *DomainResolution,
	anchors []struct {
		CanonicalPath string  `json:"canonicalPath"`
		Type          string  `json:"type"`
		Name          string  `json:"name,omitempty"`
		Confidence    float64 `json:"confidence"`
		Reason        string  `json:"reason"`
	},
	profile intentScenarioProfile,
) *SupplyChainExecutionPlan {
	anchorPaths := collectIntentAnchorPathsByPrefix(anchors, "/org/", "/fac/", "/deposit/", "/product/")
	if len(anchorPaths) == 0 {
		return nil
	}
	productFilter := domainFacetValues(d, "product", "material", "commodity", "goods", "item")
	geoFilter := domainFacetValues(d, "geo")
	queries := make([]SupplyChainQuery, 0, minInt(len(anchorPaths), 4))
	for _, cp := range anchorPaths {
		q := SupplyChainQuery{
			Anchor:        cp,
			Direction:     inferOperationalChainDirection(query, false),
			Depth:         4,
			ProductFilter: productFilter,
			GeoFilter:     geoFilter,
			Reason:        "deterministic fallback from intent anchors and domain facets",
		}
		switch {
		case strings.HasPrefix(cp, "/org/"):
			q.AnchorType = "org"
		case strings.HasPrefix(cp, "/fac/"):
			q.AnchorType = "fac"
		case strings.HasPrefix(cp, "/deposit/"):
			q.AnchorType = "deposit"
		case strings.HasPrefix(cp, "/product/"):
			q.AnchorType = "product"
		default:
			continue
		}
		if profile.routeSensitive {
			q.Depth = 5
		}
		queries = append(queries, q)
		if len(queries) >= 4 {
			break
		}
	}
	if len(queries) == 0 {
		return nil
	}
	plan := &SupplyChainExecutionPlan{
		GraphBuildMode: SupplyChainGraphBuildModeHotPathPlusGraph,
		Queries:        queries,
	}
	if profile.routeSensitive {
		overlayAnchors := collectIntentAnchorPathsByPrefix(anchors, "/org/", "/fac/", "/market/")
		if len(overlayAnchors) > 0 {
			plan.OverlayQueries = []SupplyChainOverlayQuery{{
				Type:    "market",
				Anchors: overlayAnchors[:minInt(len(overlayAnchors), 6)],
				Scope:   "both",
			}}
		}
	}
	return plan
}

func synthesizeServiceChainExecutionPlan(
	query string,
	d *DomainResolution,
	anchors []struct {
		CanonicalPath string  `json:"canonicalPath"`
		Type          string  `json:"type"`
		Name          string  `json:"name,omitempty"`
		Confidence    float64 `json:"confidence"`
		Reason        string  `json:"reason"`
	},
	profile intentScenarioProfile,
) *SupplyChainExecutionPlan {
	anchorPaths := collectIntentAnchorPathsByPrefix(anchors, "/org/", "/fac/", "/product/")
	if len(anchorPaths) == 0 {
		return nil
	}
	productFilter := domainFacetValues(d, "product", "application", "market", "sector", "industry")
	geoFilter := domainFacetValues(d, "geo")
	queries := make([]SupplyChainQuery, 0, minInt(len(anchorPaths), 4))
	for _, cp := range anchorPaths {
		q := SupplyChainQuery{
			Anchor:        cp,
			Direction:     inferOperationalChainDirection(query, true),
			Depth:         4,
			ProductFilter: productFilter,
			GeoFilter:     geoFilter,
			Reason:        "deterministic fallback from intent anchors and service-delivery facets",
		}
		switch {
		case strings.HasPrefix(cp, "/org/"):
			q.AnchorType = "org"
		case strings.HasPrefix(cp, "/fac/"):
			q.AnchorType = "fac"
		case strings.HasPrefix(cp, "/product/"):
			q.AnchorType = "product"
		default:
			continue
		}
		if profile.routeSensitive {
			q.Depth = 5
		}
		queries = append(queries, q)
		if len(queries) >= 4 {
			break
		}
	}
	if len(queries) == 0 {
		return nil
	}
	plan := &SupplyChainExecutionPlan{
		GraphBuildMode: SupplyChainGraphBuildModeHotPathPlusGraph,
		Queries:        queries,
	}
	if profile.routeSensitive || profile.service {
		overlayAnchors := collectIntentAnchorPathsByPrefix(anchors, "/org/", "/fac/", "/market/")
		if len(overlayAnchors) > 0 {
			plan.OverlayQueries = []SupplyChainOverlayQuery{{
				Type:    "market",
				Anchors: overlayAnchors[:minInt(len(overlayAnchors), 6)],
				Scope:   "both",
			}}
		}
	}
	return plan
}

func normalizeSupplyChainGraphBuildMode(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case SupplyChainGraphBuildModeHotPathOnly:
		return SupplyChainGraphBuildModeHotPathOnly
	case "", SupplyChainGraphBuildModeHotPathPlusGraph:
		return SupplyChainGraphBuildModeHotPathPlusGraph
	default:
		return SupplyChainGraphBuildModeHotPathPlusGraph
	}
}

func synthesizeRegulatoryImpactExecutionPlan(
	query string,
	d *DomainResolution,
	anchors []struct {
		CanonicalPath string  `json:"canonicalPath"`
		Type          string  `json:"type"`
		Name          string  `json:"name,omitempty"`
		Confidence    float64 `json:"confidence"`
		Reason        string  `json:"reason"`
	},
	profile intentScenarioProfile,
) *RegulatoryImpactExecutionPlan {
	instrument := ""
	for _, cp := range collectIntentAnchorPathsByPrefix(anchors, "/mechanism/", "/program/") {
		instrument = cp
		break
	}
	if instrument == "" {
		instrument = inferMechanismFromQuery(query)
	}
	jurisdiction := firstGeoFacetOrAnchor(d, anchors)
	if instrument == "" || jurisdiction == "" || jurisdiction == "global" {
		return nil
	}
	impactScope := "economy-wide"
	sectorFilter := domainFacetValues(d, "sector", "industry", "market")
	if len(sectorFilter) > 0 || profile.marketInfra || profile.sector {
		impactScope = "sector-specific"
	}
	return &RegulatoryImpactExecutionPlan{
		Queries: []RegulatoryImpactQuery{{
			Instrument:   instrument,
			Jurisdiction: jurisdiction,
			ImpactScope:  impactScope,
			Depth:        4,
			SectorFilter: sectorFilter,
			Reason:       "deterministic fallback from policy query and intent facets",
		}},
	}
}

func synthesizeGovernanceExecutionPlan(
	query string,
	d *DomainResolution,
	anchors []struct {
		CanonicalPath string  `json:"canonicalPath"`
		Type          string  `json:"type"`
		Name          string  `json:"name,omitempty"`
		Confidence    float64 `json:"confidence"`
		Reason        string  `json:"reason"`
	},
) *GovernanceExecutionPlan {
	cps := collectIntentAnchorPathsByPrefix(anchors, "/community/", "/org/", "/gov/", "/person/")
	if len(cps) == 0 {
		return nil
	}
	target := cps[0]
	var targetType string
	switch {
	case strings.HasPrefix(target, "/community/"):
		targetType = "community"
	case strings.HasPrefix(target, "/org/"):
		targetType = "org"
	case strings.HasPrefix(target, "/gov/"):
		targetType = "gov"
	case strings.HasPrefix(target, "/person/"):
		targetType = "person"
	}
	if targetType == "" {
		return nil
	}
	contractType := normalizeContractTypeAlias(d.ContractType)
	direction := "both"
	text := strings.ToLower(strings.TrimSpace(query))
	if contractType == "control-map" {
		direction = "both"
	} else if containsAnyIntentToken(text, "who owns", "owned by", "shareholder", "beneficial owner", "ultimate owner") {
		direction = "upward"
	}
	if strings.HasPrefix(target, "/community/") || containsAnyIntentToken(text, "family", "families", "dynasty", "dynasties", "tribe", "tribes", "house of", "royal family", "clan") {
		direction = "downward"
	}
	includeBoard := containsAnyIntentToken(text, "board", "director", "executive")
	if contractType == "control-map" && (strings.HasPrefix(target, "/org/") || strings.HasPrefix(target, "/gov/")) &&
		!containsAnyIntentToken(text, "ownership only", "shareholding only", "shareholder only", "owner only") {
		includeBoard = true
	}
	return &GovernanceExecutionPlan{
		Queries: []GovernanceQuery{{
			Target:       target,
			TargetType:   targetType,
			Direction:    direction,
			Depth:        4,
			IncludeBoard: includeBoard,
			GeoFilter:    domainFacetValues(d, "geo"),
			Reason:       "deterministic fallback from governance query and anchors",
		}},
	}
}

func synthesizeEcosystemExecutionPlan(
	_ string,
	d *DomainResolution,
	anchors []struct {
		CanonicalPath string  `json:"canonicalPath"`
		Type          string  `json:"type"`
		Name          string  `json:"name,omitempty"`
		Confidence    float64 `json:"confidence"`
		Reason        string  `json:"reason"`
	},
	profile intentScenarioProfile,
) *EcosystemExecutionPlan {
	focus := ""
	focusType := "sector"
	if cps := collectIntentAnchorPathsByPrefix(anchors, "/community/", "/org/", "/market/", "/loc/", "/fac/"); len(cps) > 0 {
		focus = cps[0]
		switch {
		case strings.HasPrefix(focus, "/community/"):
			focusType = "community"
		case strings.HasPrefix(focus, "/org/"):
			focusType = "org"
		case strings.HasPrefix(focus, "/market/"):
			focusType = "market"
		case strings.HasPrefix(focus, "/loc/"):
			focusType = "geo"
		}
	}
	if focus == "" {
		if sector := firstNonEmpty(domainFacetValues(d, "sector", "industry", "market")); sector != "" {
			focus = sector
		} else {
			focus = strings.TrimSpace(d.Title)
		}
	}
	if focus == "" {
		return nil
	}
	actorTypes := []string{"ORGANIZATION", "GOV", "FACILITY", "COMMUNITY"}
	if profile.ecosystem || profile.investment {
		actorTypes = append(actorTypes, "PERSON")
	}
	return &EcosystemExecutionPlan{
		Queries: []EcosystemQuery{{
			Focus:        focus,
			FocusType:    focusType,
			Depth:        4,
			ActorTypes:   actorTypes,
			SectorFilter: domainFacetValues(d, "sector", "industry", "market"),
			GeoFilter:    domainFacetValues(d, "geo"),
			Reason:       "deterministic fallback from ecosystem query and anchors",
		}},
	}
}

func domainFacetValues(d *DomainResolution, keys ...string) []string {
	if d == nil || d.Facets == nil {
		return nil
	}
	out := make([]string, 0, len(keys))
	for _, want := range keys {
		for k, vals := range d.Facets {
			if !strings.EqualFold(strings.TrimSpace(k), want) {
				continue
			}
			for _, v := range vals {
				v = strings.TrimSpace(v)
				if v != "" {
					out = append(out, v)
				}
			}
		}
	}
	return dedupStrings(out, len(out))
}

func firstGeoFacetOrAnchor(
	d *DomainResolution,
	anchors []struct {
		CanonicalPath string  `json:"canonicalPath"`
		Type          string  `json:"type"`
		Name          string  `json:"name,omitempty"`
		Confidence    float64 `json:"confidence"`
		Reason        string  `json:"reason"`
	},
) string {
	if vals := domainFacetValues(d, "geo"); len(vals) > 0 {
		return strings.TrimSpace(strings.ToLower(vals[0]))
	}
	ccs, _ := extractCountryAnchors(anchors)
	if len(ccs) > 0 {
		return strings.TrimSpace(strings.ToLower(ccs[0]))
	}
	return "global"
}

func inferMechanismFromQuery(query string) string {
	text := strings.ToLower(strings.TrimSpace(query))
	switch {
	case containsAnyIntentToken(text, "vat", "sales tax"):
		return "/mechanism/regulatory/vat-rate"
	case containsAnyIntentToken(text, "tariff", "customs duty", "import duty"):
		return "/mechanism/regulatory/tariff"
	case containsAnyIntentToken(text, "cbam", "carbon border"):
		return "/mechanism/regulatory/carbon-border-adjustment"
	case containsAnyIntentToken(text, "export control", "entity list", "license requirement"):
		return "/mechanism/regulatory/export-control"
	case containsAnyIntentToken(text, "sanction", "embargo"):
		return "/mechanism/political/sanctions"
	case containsAnyIntentToken(text, "subsid", "incentive"):
		return "/mechanism/economic/subsidy"
	case containsAnyIntentToken(text, "capital rule", "capital adequacy"):
		return "/mechanism/regulatory/capital-adequacy"
	default:
		return ""
	}
}

func firstNonEmpty(values []string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
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
	case "service-chain":
		return "Service chain"
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
	case "geographic-concentration":
		return "Geographic concentration"
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
	case "policy-impact":
		return "Policy impact"
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
	case "industry-analysis":
		return "Industry analysis"

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

func isGenericDomainTitleForIntent(title, contractType, canonicalPath string, facets map[string][]string, countryNameByCC map[string]string) bool {
	t := strings.TrimSpace(title)
	if t == "" {
		return true
	}
	base := strings.TrimSpace(baseDomainTitle(contractType))
	if base != "" && strings.EqualFold(t, base) {
		return true
	}
	auto := strings.TrimSpace(buildDomainTitleForIntent(contractType, canonicalPath, facets, countryNameByCC))
	if auto != "" && strings.EqualFold(t, auto) {
		return true
	}
	return false
}

func buildDomainTitleFromFocusForIntent(focus string) string {
	s := strings.TrimSpace(focus)
	if s == "" {
		return ""
	}
	s = strings.Trim(s, " \t\r\n.,;:!?-")
	if s == "" {
		return ""
	}
	parts := strings.Fields(s)
	if len(parts) > 10 {
		parts = parts[:10]
	}
	s = strings.Join(parts, " ")
	if s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) > 0 {
		r[0] = unicode.ToUpper(r[0])
	}
	return strings.TrimSpace(string(r))
}

func buildLensTitleForIntent(existingTitle, focus, query string, domains []DomainResolution) string {
	if title := normalizeLensTitleCandidate(existingTitle); title != "" {
		return title
	}
	for _, d := range domains {
		if title := normalizeLensTitleCandidate(d.Title); title != "" {
			return title
		}
	}
	if title := normalizeLensTitleCandidate(buildDomainTitleFromFocusForIntent(focus)); title != "" {
		return title
	}
	return normalizeLensTitleCandidate(buildDomainTitleFromFocusForIntent(query))
}

func normalizeLensTitleCandidate(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	title = strings.Trim(title, " \t\r\n.,;:!?-")
	if title == "" {
		return ""
	}
	parts := strings.Fields(title)
	if len(parts) > 10 {
		parts = parts[:10]
	}
	title = strings.Join(parts, " ")
	if title == "" {
		return ""
	}
	r := []rune(title)
	if len(r) > 0 {
		r[0] = unicode.ToUpper(r[0])
	}
	return strings.TrimSpace(string(r))
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
	case "service-chain":
		return fmt.Sprintf("Expand %s into first-order entities and relationships across the service delivery lifecycle: providers, delivery facilities, service capabilities, dependencies, control gates, and downstream recipients.", ttl)
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
	case "geographic-concentration":
		return fmt.Sprintf("Expand %s into first-order entities: geographies, facilities, suppliers, capacity distribution, and concentration signals.", ttl)
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
	case "policy-impact", "regulatory-impact", "policy-and-regulation":
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
	case "industry-analysis":
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
		cc, _, _, ok := taxonomy.ParseLocCanonicalPath(cp)
		if !ok || len(cc) != 2 {
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
      "reason": "short reason",
      "supplyChainExecutionPlan": {
        "graphBuildMode": "hot_path_plus_graph",
        "queries": [
          { "anchor": "/org/de/bmw", "anchorType": "org", "direction": "upstream", "depth": 4, "productFilter": ["ev-batteries"], "geoFilter": ["de", "cn"], "reason": "..." }
        ],
        "overlayQueries": [{ "type": "regulatory", "anchors": [], "scope": "orgs" }]
      },
      "serviceChainExecutionPlan": {
        "graphBuildMode": "hot_path_plus_graph",
        "queries": [
          { "anchor": "/org/us/google-cloud", "anchorType": "org", "direction": "both", "depth": 4, "productFilter": ["cloud-service"], "geoFilter": ["us"], "reason": "..." }
        ],
        "overlayQueries": [{ "type": "market", "anchors": [], "scope": "both" }]
      }
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
- For supply-chain or value-chain domains, include supplyChainExecutionPlan. graphBuildMode MUST be "hot_path_only" for hot-path-only execution and "hot_path_plus_graph" for broader graph completion. Default to "hot_path_plus_graph" when unsure.
- For service-chain domains, include serviceChainExecutionPlan. Use it for service delivery topology, dependencies, access/control gates, and downstream recipients. Do not use value-chain when the user is asking about service delivery reality.

- DOMAINS (CRITICAL):
  - A DOMAIN is an executable knowledge contract: “what the user wants to know about the world” and how to structure exploration.
  - Use domains when the query is an analytical question or a research focus that must be expanded into multiple entities.
  - If a matching DOMAIN already exists in the context list, reuse the SAME canonicalPath (do NOT create duplicates).
  - If the query is about relations/interaction between two countries or actors, you MUST create a DOMAIN of contractType bilateral-relations (in addition to anchors).
    Example: "Saudi Arabia and Yemen bilateral relations" -> domain:
      /domain/v1/bilateral-relations/geo/sa/geo/ye
  - If the query asks about conflict zones, military conflicts, disaster zones, earthquake-affected areas, tension zones, humanitarian crisis areas, flood zones, you MUST create a DOMAIN of contractType affected-zone with facet geo. geo: " + taxonomy.ISO2GeoDescriptionForPrompts() + ".
    Examples: "Northern Syria conflict zone" -> /domain/v1/affected-zone/geo/tr-sy; "Turkey earthquake" -> /domain/v1/affected-zone/geo/tr; "Africa military conflicts" -> /domain/v1/affected-zone/geo/africa
  - If the query asks who owns/controls an entity, you MUST create a DOMAIN of contractType ownership-structure.
  - If the query asks what connects/unifies two entities, you MUST create a DOMAIN of contractType commonality-analysis (in addition to anchors).
  - DOMAIN canonicalPath MUST start with /domain/ and follow the DOMAIN taxonomy. /market/... is NEVER a DOMAIN path.
  - /market/... CAN be used as an ANCHOR when the user asks about a market as a concrete entity.
  - Domain canonicalPath MUST be: /domain/v1/{contractType}/{facetKey}/{facetValue}...
    Good DOMAIN examples:
      /domain/v1/ecosystem-mapping/geo/my/industry/energy
      /domain/v1/industry-analysis/geo/kz/industry/agro-food
      /domain/v1/market-opportunity/geo/sg/industry/finance
      /domain/v1/affected-zone/geo/tr-sy
    Bad DOMAIN examples (these are ANCHORS, not DOMAINS):
      /market/global/finance
      /market/sg/finance/banking
      /market/global/technology/semiconductors
      /market/kz/industrial/agriculture
  - contractType MUST be one of (choose the BEST fit):
    OPERATIONAL CHAINS:
      supply-chain, service-chain, value-chain, logistics-flow, trade-flow, sourcing-map, procurement-map, service-delivery-network, capability-chain
    RISK / GEOPOLITICS:
      risk-assessment, geopolitical-risk, bilateral-relations, sanctions-exposure, conflict-dynamics, affected-zone
    MARKETS / OPPORTUNITY:
      market-opportunity, competitive-landscape, geographic-concentration, market-entry, market-sizing, demand-forecast, pricing-dynamics
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
      policy-impact, regulatory-impact, policy-and-regulation, compliance-landscape, security-and-compliance, licensing-requirements, esg-landscape
    SCENARIOS / TRENDS:
      scenario-analysis, macro-trends, megatrends, future-outlook
    SECTOR / INDUSTRY SPECIFIC (universal; no domain fallback):
      industry-analysis
      - industry-analysis: top-level analytical branch for strict /industry scope
    FALLBACK (use ONLY if none of the above fit):
      other — Format: /domain/v1/other/{custom-slug}/... where {custom-slug} is a lowercase kebab-case descriptor (e.g., energy-ecosystem-mapping, startup-landscape, talent-pool-analysis).
      Example: /domain/v1/other/energy-ecosystem-mapping/geo/my
  - facetKey MUST be one of:
    geo, topic, subject, project, asset, material, product, commodity, crop, goods, item, application, sector, industry, market, regulation, framework, time
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
      - ORGANIZATION: /org/{iso2}/social-development-bank
      - If jurisdiction is still genuinely unknowable after checking context and the name itself, /org/na/social-development-bank is the exceptional last-resort fallback
    - "Apple and Microsoft cloud partnership" → anchors:
      - ORGANIZATION: /org/us/apple-inc
      - ORGANIZATION: /org/us/microsoft-corp
      - (optional) MARKET: /market/global/technology/cloud-computing
    - "Tesla Gigafactory Texas Austin" → anchors:
      - ORGANIZATION: /org/us/tesla-inc
      - FACILITY: /fac/us/gigafactory-texas
      - GEO_REGION: /loc/us/tx/austin
- canonicalPath MUST be a valid canonical path with allowed namespaces:
  /gov, /org, /person, /product, /program, /market, /fac, /loc, /sphere, /event, /media, /domain, /zone
- If the query refers to a media brand/channel (e.g. a Telegram channel), do NOT collapse it to a GEO_REGION just because it contains a city name.
  - Example: Telegram channel "В казани поймут" should be anchored as a MEDIA_CHANNEL: /media/telegram/{channel-slug}, not as /loc/... for Kazan.
  - Prefer using a stable handle/username if present; otherwise use the quoted title as the slug source.
  - Add keyword "telegram" and consider /sphere/social/information.
- ALL path segments MUST be lowercase slugs (use hyphens instead of spaces):
  - CORRECT: /org/sa/saudi-aramco, /fac/sa/ras-tanura-refinery, /loc/ae, /product/raw/chemical/refining/lithium-carbonate
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
  - AFFECTED_ZONE -> /zone/{geo}/{condition_type}/{slug}
    - slug: descriptive place/region name (northern-rakhine, gaza-strip, donbas). NEVER "na".

- GEO_REGION canonicalPath MUST be strict: /loc/{countryAlpha2}[/{level1}[/{level2}]]
  - countryAlpha2: %s
  - Regional tokens ALWAYS go in the countryAlpha2 slot (2nd segment), never at the end.
    - Example: GCC → /loc/gcc (NOT /loc/global/gcc)
  - omit unknown trailing levels (no mandatory "na")
  - country-only locations MUST be /loc/{cc}
    - Saudi Arabia -> /loc/sa
    - United Arab Emirates -> /loc/ae

- PRODUCT canonicalPath MUST be strict hierarchical taxonomy:
  - /product/{category}/{type}/{stage}/{slug}
  - category: raw|component|finished|service
  - type: mineral|agricultural|chemical|electronic|mechanical|energy (best-effort)
  - stage: extraction|refining|processing|manufacturing|assembly (best-effort)
  - If classification is uncertain, still keep this 5-segment format with your best taxonomy estimate

- FACILITY canonicalPath:
  - use /fac/{geo}/{facilitySlug} — geo: %s
  - infer the best concrete geo from facility name, site/city mention, operator context, and source text instead of falling back to /fac/na

- ORGANIZATION canonicalPath:
  - Organizations: /org/{iso2|regional}/{slug}
    - iso2: country of origin / primary jurisdiction (%s). Use your best evidence-backed jurisdiction instead of falling back to /org/na.
    - Regional tokens go in the geo slot (2nd segment): /org/gcc/{slug}.
    - slug: lowercase slug derived from the base name (avoid legal suffixes like Inc/LLC in the path if possible)
  - Government bodies: /gov/{iso2|regional}/{slug}
    - Use for ministries, regulators, agencies, state bodies (prevents collisions like "Ministry of Defence" across countries).
    - Resolve the best jurisdiction from the source, official name, and place clues instead of falling back to /gov/na.
    - Regional tokens go in the geo slot (2nd segment): /gov/gcc/{slug}.

- PROGRAM/MARKET/EVENT/PERSON canonicalPath:
  - /program/{jurisdictionSlug}/{programSlug}, /event/{slug}, /person/{slug}
  - PROGRAM:
    - jurisdictionSlug: %s
    - If jurisdiction is unknown, use "global"
    - Regional tokens ALWAYS go in the jurisdiction slot (2nd segment): /program/gcc/{slug} (NOT /program/na/gcc/{slug})
  - MARKET (segment-first):
    - HIERARCHY LEVELS:
      - L1 (domain): /market/{geo}/{domain} — entire sector
      - L2 (vertical): /market/{geo}/{domain}/{vertical} — sub-segment of a domain
      - L3+ (sub-vertical): /market/{geo}/{domain}/{vertical}/{sub} — narrower niche under a vertical
      - Rule: child path = parent path + exactly one segment
      - NEVER repeat a segment already in the path (e.g. /market/iq/agriculture/agriculture is INVALID)
      - Examples: /market/iq/agriculture (L1) → /market/iq/agriculture/crop-production (L2) → /market/iq/agriculture/crop-production/rice-production (L3)
    - domain/vertical SHOULD align with the /segment/{domain}/{vertical}/{geo} taxonomy used for organizations.
    - Use GICS terminology (no numeric codes). If not in GICS, use the closest GICS parent + a descriptive term.
    - This enables consistent market hierarchy + aggregation.
  - MARKET geo: %s (default "global" if unknown)
    - Regional tokens ALWAYS go in the geo slot (2nd segment): /market/gcc/{domain}/{vertical} (NOT /market/na/{domain}/gcc)

%s

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

%s
- Return ONLY JSON. No markdown. No commentary.`,
		query, quotedBlock, multiHint, existingDomainsBlock,
		taxonomy.ISO2GeoDescriptionForPrompts(), taxonomy.ISO2GeoDescriptionForPrompts(), taxonomy.ISO2GeoDescriptionForPrompts(),
		taxonomy.ISO2GeoDescriptionForPrompts(), taxonomy.ISO2GeoDescriptionForPrompts(),
		taxonomy.MarketGeographyAndHierarchyPromptBlock(), taxonomy.DecompositionHintsPromptBlock())
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

// trimDomainPathTrailingIncompleteFacet fixes domain paths where LLM returned a trailing facet key without value.
// Example: /domain/v1/supply-chain/geo/th/product -> /domain/v1/supply-chain/geo/th
func trimDomainPathTrailingIncompleteFacet(path string) string {
	p := strings.TrimSpace(path)
	if p == "" || !strings.HasPrefix(p, "/domain/") {
		return p
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	// domain, v1, contractType, [k,v]* — need at least 5 segments
	if len(parts) < 5 {
		return p
	}
	rest := parts[3:]
	if len(rest)%2 == 0 {
		return p
	}
	// Odd: drop last (incomplete key)
	return "/" + strings.Join(parts[:len(parts)-1], "/")
}

// repairSupplyChainDomainNamespacePath converts malformed namespace-style org paths inside
// supply-chain domains into valid facet pairs understood by the domain taxonomy.
// Example:
// /domain/v1/supply-chain/org/us/apple/product/macbook
// -> /domain/v1/supply-chain/geo/us/subject/apple/product/macbook
func repairSupplyChainDomainNamespacePath(path string) string {
	p := strings.TrimSpace(path)
	if p == "" || !strings.HasPrefix(p, "/domain/v1/") {
		return p
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) < 6 {
		return p
	}
	contractType := strings.ToLower(strings.TrimSpace(parts[2]))
	if contractType != "supply-chain" && contractType != "value-chain" && contractType != "service-chain" {
		return p
	}
	rest := parts[3:]
	if len(rest) < 3 || strings.ToLower(strings.TrimSpace(rest[0])) != "org" {
		return p
	}
	geo := taxonomy.Slugify(rest[1])
	subject := taxonomy.Slugify(rest[2])
	if geo == "" || subject == "" {
		return p
	}
	rebuilt := []string{"domain", "v1", contractType, "geo", geo, "subject", subject}
	rebuilt = append(rebuilt, rest[3:]...)
	return "/" + strings.Join(rebuilt, "/")
}

// normalizeCanonicalPathForTools fixes MSYS/Git-Bash path conversion for canonical paths.
// Example: "C:/Program Files/Git/org/tesla" -> "/org/tesla" (we strip prefix to first namespace).
// Must NOT strip when path already starts with / — domain paths like /domain/v1/.../product/fruits
// contain "/product/" as a facet key and would be incorrectly truncated.
func normalizeCanonicalPathForTools(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return s
	}
	if strings.HasPrefix(s, "/") {
		return s
	}
	// Path does not start with / — likely MSYS corruption. Find first canonical namespace.
	// Order matters: /domain/ first — domain paths contain other ns as facets (e.g. .../product/fruits, .../org/partners).
	candidates := []string{"/domain/", "/zone/", "/gov/", "/org/", "/person/", "/product/", "/fac/", "/loc/", "/event/", "/sphere/", "/program/", "/market/", "/segment/", "/media/"}
	for _, c := range candidates {
		if idx := strings.Index(s, c); idx >= 0 {
			s = s[idx:]
			break
		}
	}
	if !strings.HasPrefix(s, "/") {
		s = "/" + s
	}
	return s
}

// normalizeLegacyGeoPathForIntent maps older geo namespaces to variable-depth /loc model.
// Supported legacy patterns:
// - /region/na/{country}/{admin1}/{city} -> /loc/{country}[/{level1}[/{level2}]]
// - /region/{country}/{admin1}/{city}    -> /loc/{country}[/{level1}[/{level2}]]
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
	extractCountry := func(idx int) string {
		if idx < 0 || idx >= len(parts) {
			return ""
		}
		c := strings.ToLower(strings.TrimSpace(parts[idx]))
		if c == "" || c == "na" {
			return ""
		}
		return c
	}
	// parts[0] == "region"
	if len(parts) == 5 && parts[1] == "na" {
		if c := extractCountry(2); c != "" {
			return taxonomy.GeoCanonicalPath(c, parts[3], parts[4])
		}
		return ""
	}
	if len(parts) == 4 {
		if c := extractCountry(1); c != "" {
			return taxonomy.GeoCanonicalPath(c, parts[2], parts[3])
		}
		return ""
	}
	if len(parts) == 3 && parts[1] == "na" {
		if c := extractCountry(2); c != "" {
			return taxonomy.GeoCanonicalPath(c, "", "")
		}
		return ""
	}
	if len(parts) == 3 {
		if c := extractCountry(1); c != "" {
			return taxonomy.GeoCanonicalPath(c, parts[2], "")
		}
		return ""
	}
	if len(parts) == 2 {
		if c := extractCountry(1); c != "" {
			return taxonomy.GeoCanonicalPath(c, "", "")
		}
		return ""
	}
	return ""
}
