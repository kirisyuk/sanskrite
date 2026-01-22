package profile

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// Generator generates FragmentProfiles using LLM with Search Grounding.
type Generator struct {
	executor      *ai.AITaskExecutor
	fragmentsRepo *repo.HorizonFragmentsRepository
	edgesRepo     *repo.DependencyEdgesRepository
	profilesRepo  *repo.FragmentProfilesRepository
}

// NewGenerator creates a new profile Generator.
func NewGenerator(
	executor *ai.AITaskExecutor,
	fragmentsRepo *repo.HorizonFragmentsRepository,
	edgesRepo *repo.DependencyEdgesRepository,
	profilesRepo *repo.FragmentProfilesRepository,
) *Generator {
	return &Generator{
		executor:      executor,
		fragmentsRepo: fragmentsRepo,
		edgesRepo:     edgesRepo,
		profilesRepo:  profilesRepo,
	}
}

// GenerateOptions contains options for profile generation.
type GenerateOptions struct {
	MaxNeighbors     int  // Max neighbors to include in context (default: 10)
	ForceRegenerate  bool // Regenerate even if fresh profile exists
	MarkPreviousStale bool // Mark previous profile as stale
}

// DefaultGenerateOptions returns default generation options.
func DefaultGenerateOptions() GenerateOptions {
	return GenerateOptions{
		MaxNeighbors:      10,
		ForceRegenerate:   false,
		MarkPreviousStale: true,
	}
}

// GenerateResult contains the result of profile generation.
type GenerateResult struct {
	Profile      *models.FragmentProfile
	Created      bool
	PromptTokens int
	OutputTokens int
	DurationMs   int
	Error        error
}

// Generate generates a profile for a fragment.
func (g *Generator) Generate(ctx context.Context, fragmentID string, opts GenerateOptions) (*GenerateResult, error) {
	startTime := time.Now()
	result := &GenerateResult{}

	// Load fragment
	fragment, err := g.fragmentsRepo.Get(ctx, fragmentID)
	if err != nil {
		return nil, fmt.Errorf("failed to load fragment: %w", err)
	}

	// Check if fresh profile exists
	if !opts.ForceRegenerate {
		existing, err := g.profilesRepo.GetByFragmentID(ctx, fragmentID)
		if err == nil && existing != nil {
			// Check freshness (default: 24 hours)
			if time.Since(existing.CapturedAt) < 24*time.Hour {
				result.Profile = existing
				result.Created = false
				return result, nil
			}
		}
	}

	// Gather graph context
	graphContext, err := g.gatherGraphContext(ctx, fragment, opts.MaxNeighbors)
	if err != nil {
		log.Printf("[ProfileGenerator] Warning: failed to gather graph context for %s: %v", fragmentID, err)
		graphContext = &models.GraphContextSummary{}
	}

	var profile *models.FragmentProfile
	var genModel string
	var genNotes string

	// For markets, use multi-step generation (one request per section + dashboard synthesis)
	if fragment.Type == models.FragmentTypeMarket {
		p, usedModel, notes, err := g.generateMarketProfileMultiStep(ctx, fragment, graphContext)
		if err != nil {
			return nil, err
		}
		profile = p
		genModel = usedModel
		genNotes = notes
	} else {
		// Default: single-shot generation (legacy v1 behavior)
		prompt := g.buildPrompt(fragment, graphContext)
		task := &ai.AITask{
			TaskType:     ai.TaskTypeKnowledgeSynthesis,
			InputContent: prompt,
			AIRequirements: ai.AIRequirements{
				TextGenerationCapability:  true,
				SearchGroundingCapability: true,
				MinCapabilityScore:        7,
				MinContextWindow:          32000,
				MaxOutputSize:             16000,
			},
			Verifier: ai.NewRepairableVerifier(repairProfileLLMJSON, nil),
		}

		llmResult, usedModel, err := g.executor.ExecuteStructuredTaskWithModel(ctx, task)
		if err != nil {
			return nil, fmt.Errorf("LLM execution failed: %w", err)
		}

		// Parse result
		p, notes, err := g.parseResult(fragment, graphContext, llmResult)
		if err != nil {
			return nil, fmt.Errorf("failed to parse LLM result: %w", err)
		}
		profile = p
		genModel = usedModel
		genNotes = notes
	}

	// Set generation metadata
	profile.Generation.GeneratedAt = time.Now().UTC()
	profile.Generation.GraphContext = *graphContext
	if profile.Generation.SchemaVersion == "" {
		profile.Generation.SchemaVersion = "v1"
	}
	if strings.TrimSpace(genModel) != "" {
		profile.Generation.Model = genModel
	}
	if strings.TrimSpace(genNotes) != "" {
		profile.Generation.GenerationNotes = genNotes
	}

	// Mark previous profile as stale
	if opts.MarkPreviousStale {
		if existing, err := g.profilesRepo.GetByFragmentID(ctx, fragmentID); err == nil && existing != nil {
			_ = g.profilesRepo.MarkStale(ctx, existing.ID)
		}
	}

	// Save profile
	profileID, err := g.profilesRepo.Create(ctx, profile)
	if err != nil {
		return nil, fmt.Errorf("failed to save profile: %w", err)
	}
	profile.ID = profileID

	result.Profile = profile
	result.Created = true
	result.DurationMs = int(time.Since(startTime).Milliseconds())

	log.Printf("[ProfileGenerator] Generated profile for %s (%s) in %dms",
		fragment.CanonicalPath, fragment.Type, result.DurationMs)

	return result, nil
}

// marketSectionResult is the parsed output for a single market section LLM call.
type marketSectionResult struct {
	Key        string                `json:"key"`
	Title      string                `json:"title"`
	Content    string                `json:"content"`
	Structured map[string]any        `json:"structured,omitempty"`
	Datasets   []models.ProfileDataset `json:"datasets,omitempty"`
	Sources    []string              `json:"sources,omitempty"`
	Confidence float64               `json:"confidence"`
	Order      int                   `json:"order"`
}

type marketDashboardResult struct {
	Thesis     string               `json:"thesis,omitempty"`
	KPIs       []models.ProfileKPI    `json:"kpis,omitempty"`
	Scenarios  []models.ProfileScenario `json:"scenarios,omitempty"`
	Watchlist  []models.ProfileWatchItem `json:"watchlist,omitempty"`
	Charts     []models.ProfileDataset `json:"charts,omitempty"`
	Tables     []models.ProfileDataset `json:"tables,omitempty"`
	Notes      string               `json:"notes,omitempty"`
	Confidence float64              `json:"confidence,omitempty"`
}

// generateMarketProfileMultiStep generates each section as a separate LLM request to reduce hallucination risk,
// and then synthesizes a dashboard object at the end. Each subsequent prompt includes prior sections to avoid duplication.
func (g *Generator) generateMarketProfileMultiStep(
	ctx context.Context,
	fragment *models.HorizonFragment,
	graphContext *models.GraphContextSummary,
) (*models.FragmentProfile, string, string, error) {
	now := time.Now().UTC()
	profile := &models.FragmentProfile{
		FragmentID:    fragment.ID,
		FragmentType:  fragment.Type,
		CanonicalPath: fragment.CanonicalPath,
		Version:       2,
		CapturedAt:    now,
		Sections:      make(map[string]models.ProfileSection),
		Status:        models.ProfileStatusActive,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	sections := models.DefaultSectionsForType(fragment.Type)
	sectionOrder := make(map[string]int)
	for _, s := range sections {
		sectionOrder[s.Key] = s.Order
	}

	// Budget configuration: keep >= 8192, allow override.
	sectionMaxOut := max(8192, envInt("HORIZON_PROFILE_SECTION_MAX_OUTPUT_TOKENS", 12000))
	dashboardMaxOut := max(8192, envInt("HORIZON_PROFILE_DASHBOARD_MAX_OUTPUT_TOKENS", 16000))

	usedModel := ""
	var allNotes []string

	// Generate sections sequentially, carrying prior sections.
	for _, spec := range sections {
		prompt := g.buildMarketSectionPrompt(fragment, graphContext, spec, profile.Sections)
		task := &ai.AITask{
			TaskType:     ai.TaskTypeKnowledgeSynthesis,
			InputContent: prompt,
			AIRequirements: ai.AIRequirements{
				TextGenerationCapability:  true,
				SearchGroundingCapability: true,
				MinCapabilityScore:        7,
				MinContextWindow:          32000,
				MaxOutputSize:             sectionMaxOut,
			},
			Verifier: ai.NewRepairableVerifier(repairProfileLLMJSON, nil),
		}

		parsed, modelUsed, err := g.executor.ExecuteStructuredTaskWithModel(ctx, task)
		if err != nil {
			return nil, usedModel, strings.Join(allNotes, "\n"), fmt.Errorf("LLM section generation failed (key=%s): %w", spec.Key, err)
		}
		if usedModel == "" {
			usedModel = modelUsed
		}

		sec, err := g.parseMarketSectionResult(parsed)
		if err != nil {
			return nil, usedModel, strings.Join(allNotes, "\n"), fmt.Errorf("failed to parse section (key=%s): %w", spec.Key, err)
		}

		order := sectionOrder[spec.Key]
		if order == 0 {
			order = spec.Order
		}
		if sec.Order == 0 {
			sec.Order = order
		}
		if strings.TrimSpace(sec.Title) == "" {
			sec.Title = spec.Title
		}
		if strings.TrimSpace(sec.Key) == "" {
			sec.Key = spec.Key
		}

		profile.Sections[sec.Key] = models.ProfileSection{
			Key:        sec.Key,
			Title:      sec.Title,
			Content:    sec.Content,
			Structured: sec.Structured,
			Datasets:   sec.Datasets,
			Sources:    sec.Sources,
			Confidence: clamp01(sec.Confidence),
			Order:      sec.Order,
		}
	}

	// Dashboard synthesis (final step)
	dashPrompt := g.buildMarketDashboardPrompt(fragment, graphContext, profile.Sections)
	dashTask := &ai.AITask{
		TaskType:     ai.TaskTypeKnowledgeSynthesis,
		InputContent: dashPrompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability:  true,
			SearchGroundingCapability: true,
			MinCapabilityScore:        7,
			MinContextWindow:          32000,
			MaxOutputSize:             dashboardMaxOut,
		},
		Verifier: ai.NewRepairableVerifier(repairProfileLLMJSON, nil),
	}
	dParsed, modelUsed, err := g.executor.ExecuteStructuredTaskWithModel(ctx, dashTask)
	if err != nil {
		// Dashboard is optional; don't fail the whole profile.
		log.Printf("[ProfileGenerator] WARNING: dashboard synthesis failed (canonicalPath=%s): %v", fragment.CanonicalPath, err)
	} else {
		if usedModel == "" {
			usedModel = modelUsed
		}
		dash, err := g.parseMarketDashboardResult(dParsed)
		if err != nil {
			log.Printf("[ProfileGenerator] WARNING: failed to parse dashboard synthesis (canonicalPath=%s): %v", fragment.CanonicalPath, err)
		} else {
			profile.Dashboard = &models.ProfileDashboard{
				Thesis:     strings.TrimSpace(dash.Thesis),
				KPIs:       dash.KPIs,
				Scenarios:  dash.Scenarios,
				Watchlist:  dash.Watchlist,
				Charts:     dash.Charts,
				Tables:     dash.Tables,
				Notes:      strings.TrimSpace(dash.Notes),
				Confidence: clamp01(dash.Confidence),
			}
			if strings.TrimSpace(dash.Notes) != "" {
				allNotes = append(allNotes, dash.Notes)
			}
		}
	}

	// Mark schema version
	profile.Generation = models.ProfileGeneration{
		GroundingUsed: true,
		SchemaVersion: "v2",
	}

	return profile, usedModel, strings.TrimSpace(strings.Join(allNotes, "\n")), nil
}

func (g *Generator) buildMarketSectionPrompt(
	fragment *models.HorizonFragment,
	graphContext *models.GraphContextSummary,
	spec models.ProfileSectionSpec,
	prior map[string]models.ProfileSection,
) string {
	// Build entity context
	entityContext := map[string]any{
		"canonicalPath": fragment.CanonicalPath,
		"type":          string(fragment.Type),
		"labels":        fragment.Labels,
		"description":   fragment.Description,
	}
	if fragment.Identifiers != nil {
		entityContext["identifiers"] = fragment.Identifiers
	}
	if fragment.HomeLocation != nil {
		entityContext["location"] = fragment.HomeLocation
	}
	if graphContext != nil && len(graphContext.TopNeighbors) > 0 {
		entityContext["relatedEntities"] = graphContext.TopNeighbors
	}
	if graphContext != nil && len(graphContext.EdgeTypes) > 0 {
		entityContext["relationshipTypes"] = graphContext.EdgeTypes
	}
	entityJSON, _ := json.MarshalIndent(entityContext, "", "  ")

	// Prior sections (trimmed) to reduce duplication risk without exploding context.
	priorSlim := make([]map[string]any, 0, len(prior))
	keys := make([]string, 0, len(prior))
	for k := range prior {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		s := prior[k]
		content := strings.TrimSpace(s.Content)
		if len(content) > 2400 {
			content = content[:2400] + "…"
		}
		priorSlim = append(priorSlim, map[string]any{
			"key":        s.Key,
			"title":      s.Title,
			"order":      s.Order,
			"confidence": s.Confidence,
			"structured": s.Structured,
			"datasets":   s.Datasets,
			"contentExcerpt": content,
		})
	}
	priorJSON, _ := json.MarshalIndent(priorSlim, "", "  ")

	return fmt.Sprintf(`You are generating ONE section of a high-grade market profile for an analyst dashboard.

IMPORTANT:
- Use Google Search grounding to find current, accurate information.
- Do NOT invent facts. If specific numbers are not available, state "unknown" explicitly and explain what you'd need to verify.
- Avoid repeating information that already appears in prior sections.
- Provide sources as direct URLs to the exact pages/reports where the claim/number appears (avoid homepages).
- Prefer quantification: ranges, CAGR, shares, HHI/CR, capacity/utilization proxies, lead times, pricing, etc.
- Output must conform to the JSON shape below. Types matter:
  - datasets[].series[].points[].y MUST be a number (not a string).
  - datasets[].series[].points[].meta MUST be a JSON object (use { "flag": true }, not "flag").
  - sources MUST be an array of strings (not a single string).

ENTITY CONTEXT:
%s

PRIOR SECTIONS (do not duplicate; use as context):
%s

SECTION TO GENERATE:
{
  "key": %q,
  "title": %q,
  "description": %q
}

REQUIRED OUTPUT FORMAT (JSON) - output ONLY valid JSON:
{
  "key": %q,
  "title": "Human title",
  "order": %d,
  "confidence": 0.0,
  "content": "Markdown narrative that supports the numbers and charts",
  "structured": { "key_metrics": "..." },
  "datasets": [
    {
      "id": "string",
      "title": "string",
      "type": "timeseries|bar|pie|table",
      "description": "optional",
      "unit": "optional",
      "currency": "optional",
      "series": [
        { "name": "optional", "points": [ { "x": "label", "y": 0.0, "meta": {} } ] }
      ],
      "table": {
        "columns": [ { "id": "colKey", "title": "Column", "format": "optional" } ],
        "rows": [ { "colKey": "value" } ]
      },
      "sources": ["https://..."],
      "confidence": 0.0
    }
  ],
  "sources": ["https://..."],
  "notes": "optional brief caveats for this section"
}

Rules:
- For non-table chart types, omit "table" or set it to null.
- For table type, fill "table" and you may omit "series".
- Keep datasets minimal but decision-useful (2-6 per section max).
`, string(entityJSON), string(priorJSON), spec.Key, spec.Title, spec.Description, spec.Key, spec.Order)
}

func (g *Generator) buildMarketDashboardPrompt(
	fragment *models.HorizonFragment,
	graphContext *models.GraphContextSummary,
	sections map[string]models.ProfileSection,
) string {
	entityContext := map[string]any{
		"canonicalPath": fragment.CanonicalPath,
		"type":          string(fragment.Type),
		"labels":        fragment.Labels,
		"description":   fragment.Description,
	}
	if graphContext != nil && len(graphContext.TopNeighbors) > 0 {
		entityContext["relatedEntities"] = graphContext.TopNeighbors
	}
	entityJSON, _ := json.MarshalIndent(entityContext, "", "  ")

	// Provide full sections but with trimmed content to keep context reasonable.
	secSlim := make([]map[string]any, 0, len(sections))
	keys := make([]string, 0, len(sections))
	for k := range sections {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		s := sections[k]
		content := strings.TrimSpace(s.Content)
		if len(content) > 3200 {
			content = content[:3200] + "…"
		}
		secSlim = append(secSlim, map[string]any{
			"key":        s.Key,
			"title":      s.Title,
			"order":      s.Order,
			"confidence": s.Confidence,
			"structured": s.Structured,
			"datasets":   s.Datasets,
			"contentExcerpt": content,
			"sources":    s.Sources,
		})
	}
	sectionsJSON, _ := json.MarshalIndent(secSlim, "", "  ")

	return fmt.Sprintf(`You are synthesizing a high-grade MARKET DASHBOARD from the already-generated profile sections.

IMPORTANT:
- Use Google Search grounding ONLY if needed to validate key numbers; prefer using the provided sections to avoid contradictions.
- Do not introduce new major numbers unless you can cite a source URL.
- Provide decision-support blocks: thesis, KPI strip, scenarios with probabilities+triggers, and a watchlist of leading indicators.
- Output must conform to the JSON shape below. Types matter:
  - charts[].series[].points[].y MUST be a number.
  - charts[].series[].points[].meta MUST be a JSON object.
  - sources MUST be an array of strings.

ENTITY CONTEXT:
%s

SECTIONS (source-of-truth; avoid duplication and contradictions):
%s

REQUIRED OUTPUT FORMAT (JSON) - output ONLY valid JSON:
{
  "thesis": "1 sentence",
  "kpis": [
    { "id": "tam", "label": "TAM", "value": "$109B", "subtext": "(2024)", "trendValue": "+9%% YoY", "severity": "neutral", "confidence": 0.0 }
  ],
  "scenarios": [
    { "id": "base", "name": "Base", "probability": 0.55, "assumptions": ["..."], "metrics": {"2026_revenue":"$138B"}, "triggers": ["..."] }
  ],
  "watchlist": [
    { "id": "cowos_lead_time", "indicator": "TSMC CoWoS lead time", "current": "52 weeks", "threshold": "<40 weeks", "signal": "Easing bottleneck", "frequency": "monthly", "sources": ["https://..."], "confidence": 0.0 }
  ],
  "charts": [
    {
      "id": "tam_timeseries",
      "title": "TAM (history + forecast)",
      "type": "timeseries",
      "unit": "billion",
      "currency": "USD",
      "series": [ { "name": "TAM", "points": [ { "x": "2024", "y": 109.0 } ] } ],
      "sources": ["https://..."],
      "confidence": 0.0
    }
  ],
  "tables": [
    {
      "id": "concentration_by_segment",
      "title": "Concentration metrics by subsegment",
      "type": "table",
      "table": {
        "columns": [
          { "id": "segment", "title": "Segment" },
          { "id": "hhi", "title": "HHI", "format": "number" },
          { "id": "cr3", "title": "CR3", "format": "percent" },
          { "id": "risk", "title": "Risk" }
        ],
        "rows": [
          { "segment": "ABF film", "hhi": 9000, "cr3": 0.95, "risk": "critical" }
        ]
      },
      "sources": ["https://..."],
      "confidence": 0.0
    }
  ],
  "notes": "brief caveats and data gaps",
  "confidence": 0.0
}
`, string(entityJSON), string(sectionsJSON))
}

func (g *Generator) parseMarketSectionResult(parsed any) (*marketSectionResult, error) {
	b, err := json.Marshal(parsed)
	if err != nil {
		return nil, fmt.Errorf("marshal section parsed: %w", err)
	}
	// Allow an optional "notes" field without failing strict parsing.
	var raw map[string]any
	_ = json.Unmarshal(b, &raw)

	var out marketSectionResult
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("unmarshal section: %w", err)
	}
	return &out, nil
}

func (g *Generator) parseMarketDashboardResult(parsed any) (*marketDashboardResult, error) {
	b, err := json.Marshal(parsed)
	if err != nil {
		return nil, fmt.Errorf("marshal dashboard parsed: %w", err)
	}
	var out marketDashboardResult
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("unmarshal dashboard: %w", err)
	}
	return &out, nil
}

func envInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// gatherGraphContext collects neighboring nodes for identification context.
func (g *Generator) gatherGraphContext(ctx context.Context, fragment *models.HorizonFragment, maxNeighbors int) (*models.GraphContextSummary, error) {
	if maxNeighbors <= 0 {
		maxNeighbors = 10
	}

	summary := &models.GraphContextSummary{
		FragmentLabels: make([]string, 0),
	}

	// Add fragment labels
	if fragment.Labels != nil {
		for _, label := range fragment.Labels {
			if strings.TrimSpace(label) != "" {
				summary.FragmentLabels = append(summary.FragmentLabels, label)
			}
		}
	}

	// Get outgoing edges
	outEdges, err := g.edgesRepo.ListBySource(ctx, "global", fragment.ID, nil, maxNeighbors)
	if err != nil {
		return summary, err
	}

	// Get incoming edges
	inEdges, err := g.edgesRepo.ListByTarget(ctx, "global", fragment.ID, nil, maxNeighbors)
	if err != nil {
		return summary, err
	}

	// Collect unique neighbor IDs and edge types
	neighborIDs := make(map[string]struct{})
	edgeTypes := make(map[string]struct{})

	for _, edge := range outEdges {
		neighborIDs[edge.TargetFragmentID] = struct{}{}
		edgeTypes[string(edge.Type)] = struct{}{}
	}
	for _, edge := range inEdges {
		neighborIDs[edge.SourceFragmentID] = struct{}{}
		edgeTypes[string(edge.Type)] = struct{}{}
	}

	summary.EdgeCount = len(outEdges) + len(inEdges)
	summary.NeighborCount = len(neighborIDs)

	// Convert edge types to slice
	for et := range edgeTypes {
		summary.EdgeTypes = append(summary.EdgeTypes, et)
	}
	sort.Strings(summary.EdgeTypes)

	// Load top neighbors (by canonical path for readability)
	topNeighborIDs := make([]string, 0, maxNeighbors)
	for id := range neighborIDs {
		topNeighborIDs = append(topNeighborIDs, id)
		if len(topNeighborIDs) >= maxNeighbors {
			break
		}
	}

	if len(topNeighborIDs) > 0 {
		neighbors, err := g.fragmentsRepo.GetMany(ctx, topNeighborIDs)
		if err == nil {
			for _, n := range neighbors {
				if n != nil && strings.TrimSpace(n.CanonicalPath) != "" {
					summary.TopNeighbors = append(summary.TopNeighbors, n.CanonicalPath)
				}
			}
			sort.Strings(summary.TopNeighbors)
		}
	}

	return summary, nil
}

// buildPrompt builds the LLM prompt for profile generation.
func (g *Generator) buildPrompt(fragment *models.HorizonFragment, graphContext *models.GraphContextSummary) string {
	sections := models.DefaultSectionsForType(fragment.Type)
	sectionSpecs := make([]map[string]any, 0, len(sections))
	for _, s := range sections {
		sectionSpecs = append(sectionSpecs, map[string]any{
			"key":         s.Key,
			"title":       s.Title,
			"description": s.Description,
			"required":    s.Required,
		})
	}
	sectionsJSON, _ := json.MarshalIndent(sectionSpecs, "", "  ")

	// Build entity context
	entityContext := map[string]any{
		"canonicalPath": fragment.CanonicalPath,
		"type":          string(fragment.Type),
		"labels":        fragment.Labels,
		"description":   fragment.Description,
	}
	if fragment.Identifiers != nil {
		entityContext["identifiers"] = fragment.Identifiers
	}
	if fragment.HomeLocation != nil {
		entityContext["location"] = fragment.HomeLocation
	}
	if graphContext != nil && len(graphContext.TopNeighbors) > 0 {
		entityContext["relatedEntities"] = graphContext.TopNeighbors
	}
	if graphContext != nil && len(graphContext.EdgeTypes) > 0 {
		entityContext["relationshipTypes"] = graphContext.EdgeTypes
	}
	entityJSON, _ := json.MarshalIndent(entityContext, "", "  ")

	return fmt.Sprintf(`Generate a comprehensive profile/dossier for the following entity.

IMPORTANT:
- Use Google Search grounding to find current, accurate information.
- Do NOT invent facts. If information is not available, say so explicitly.
- Include source URLs where possible.
- Provide confidence scores (0.0-1.0) for each section based on source quality.

ENTITY CONTEXT:
%s

REQUIRED OUTPUT FORMAT (JSON):
{
  "sections": {
    "<section_key>": {
      "title": "Section Title",
      "content": "Markdown content with facts, figures, analysis...",
      "structured": {
        // Optional: key metrics, lists, structured data
        // Examples: "revenue_usd": 1000000000, "employees": 5000, "founded_year": 2010
      },
      "sources": ["https://source1.com", "https://source2.com"],
      "confidence": 0.85
    }
  },
  "overallConfidence": 0.8,
  "generationNotes": "Brief notes about data quality, gaps, caveats"
}

SECTIONS TO GENERATE:
%s

OUTPUT ONLY VALID JSON. No markdown code fences, no explanations outside JSON.
`, string(entityJSON), string(sectionsJSON))
}

// llmProfileResponse represents the expected LLM output structure.
type llmProfileResponse struct {
	Sections          map[string]llmSectionResponse `json:"sections"`
	OverallConfidence float64                       `json:"overallConfidence"`
	GenerationNotes   string                        `json:"generationNotes"`
}

type llmSectionResponse struct {
	Title      string         `json:"title"`
	Content    string         `json:"content"`
	Structured map[string]any `json:"structured,omitempty"`
	Datasets   []models.ProfileDataset `json:"datasets,omitempty"`
	Sources    []string       `json:"sources,omitempty"`
	Confidence float64        `json:"confidence"`
}

// parseResult parses the LLM result into a FragmentProfile.
func (g *Generator) parseResult(
	fragment *models.HorizonFragment,
	graphContext *models.GraphContextSummary,
	llmResult any,
) (*models.FragmentProfile, string, error) {
	// Marshal and unmarshal to get consistent structure
	jsonData, err := json.Marshal(llmResult)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal LLM result: %w", err)
	}

	var response llmProfileResponse
	if err := json.Unmarshal(jsonData, &response); err != nil {
		return nil, "", fmt.Errorf("failed to unmarshal LLM response: %w", err)
	}

	// Build profile
	now := time.Now().UTC()
	profile := &models.FragmentProfile{
		FragmentID:    fragment.ID,
		FragmentType:  fragment.Type,
		CanonicalPath: fragment.CanonicalPath,
		Version:       1,
		CapturedAt:    now,
		Sections:      make(map[string]models.ProfileSection),
		Status:        models.ProfileStatusActive,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	// Convert sections
	defaultSections := models.DefaultSectionsForType(fragment.Type)
	sectionOrder := make(map[string]int)
	for _, s := range defaultSections {
		sectionOrder[s.Key] = s.Order
	}

	for key, section := range response.Sections {
		order := sectionOrder[key]
		if order == 0 {
			order = 100 // Unknown sections go last
		}

		profile.Sections[key] = models.ProfileSection{
			Key:        key,
			Title:      section.Title,
			Content:    section.Content,
			Structured: section.Structured,
			Datasets:   section.Datasets,
			Sources:    section.Sources,
			Confidence: section.Confidence,
			Order:      order,
		}
	}

	// Set generation metadata (partially; caller fills in the rest)
	profile.Generation = models.ProfileGeneration{
		GroundingUsed: true,
		SchemaVersion: "v1",
	}

	return profile, strings.TrimSpace(response.GenerationNotes), nil
}

// GenerateBatch generates profiles for multiple fragments.
func (g *Generator) GenerateBatch(ctx context.Context, fragmentIDs []string, opts GenerateOptions) ([]*GenerateResult, error) {
	results := make([]*GenerateResult, 0, len(fragmentIDs))

	for _, id := range fragmentIDs {
		result, err := g.Generate(ctx, id, opts)
		if err != nil {
			results = append(results, &GenerateResult{
				Error: err,
			})
			continue
		}
		results = append(results, result)
	}

	return results, nil
}
