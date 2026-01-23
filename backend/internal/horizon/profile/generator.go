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
	MaxNeighbors      int  // Max neighbors to include in context (default: 10)
	ForceRegenerate   bool // Regenerate even if fresh profile exists
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

	isGov := fragment.Type == models.FragmentTypeOrganization &&
		strings.HasPrefix(strings.ToLower(strings.TrimSpace(fragment.CanonicalPath)), "/gov/")

	// Detect COMMUNITY subtypes: dynasty and tribe
	// Path patterns: /community/{geo}/dynasty/{slug} or /community/{geo}/tribe/{slug}
	// Also check CommunityType field for "dynasty" or "tribe"
	canonicalPathLower := strings.ToLower(strings.TrimSpace(fragment.CanonicalPath))
	isDynasty := false
	isTribe := false
	if fragment.Type == models.FragmentTypeCommunity {
		// Check canonical path for /community/{geo}/dynasty/... or /community/{geo}/tribe/...
		pathParts := strings.Split(strings.TrimPrefix(canonicalPathLower, "/"), "/")
		if len(pathParts) >= 3 && pathParts[0] == "community" {
			communityType := pathParts[2] // community/{geo}/{type}/{slug}
			isDynasty = communityType == "dynasty"
			isTribe = communityType == "tribe"
		}
		// Fallback: check CommunityType field
		if !isDynasty && !isTribe {
			ct := strings.ToLower(strings.TrimSpace(fragment.CommunityType))
			isDynasty = ct == "dynasty"
			isTribe = ct == "tribe"
		}
	}

	// Route to appropriate generator based on fragment type
	if isDynasty {
		// DYNASTY uses multi-step generation with dynasty-specific structure.
		p, usedModel, notes, err := g.generateDynastyProfileMultiStep(ctx, fragment, graphContext)
		if err != nil {
			return nil, err
		}
		profile = p
		genModel = usedModel
		genNotes = notes
	} else if isTribe {
		// TRIBE uses multi-step generation with tribe-specific structure.
		p, usedModel, notes, err := g.generateTribeProfileMultiStep(ctx, fragment, graphContext)
		if err != nil {
			return nil, err
		}
		profile = p
		genModel = usedModel
		genNotes = notes
	} else if isGov {
		// GOV (/gov/*) uses multi-step generation with a gov-specific structure.
		p, usedModel, notes, err := g.generateGovProfileMultiStep(ctx, fragment, graphContext)
		if err != nil {
			return nil, err
		}
		profile = p
		genModel = usedModel
		genNotes = notes
	} else if fragment.Type == models.FragmentTypeMarket {
		// For markets, use multi-step generation (one request per section + dashboard synthesis)
		p, usedModel, notes, err := g.generateMarketProfileMultiStep(ctx, fragment, graphContext)
		if err != nil {
			return nil, err
		}
		profile = p
		genModel = usedModel
		genNotes = notes
	} else if fragment.Type == models.FragmentTypeOrganization && !isGov {
		p, usedModel, notes, err := g.generateOrgProfileMultiStep(ctx, fragment, graphContext)
		if err != nil {
			return nil, err
		}
		profile = p
		genModel = usedModel
		genNotes = notes
	} else if fragment.Type == models.FragmentTypeProgram {
		// PROGRAM uses multi-step generation with program-specific structure.
		p, usedModel, notes, err := g.generateProgramProfileMultiStep(ctx, fragment, graphContext)
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
	Key        string                  `json:"key"`
	Title      string                  `json:"title"`
	Content    string                  `json:"content"`
	Facts      []models.ProfileFact    `json:"facts,omitempty"`
	Structured map[string]any          `json:"structured,omitempty"`
	Datasets   []models.ProfileDataset `json:"datasets,omitempty"`
	Sources    []string                `json:"sources,omitempty"`
	Confidence float64                 `json:"confidence"`
	Order      int                     `json:"order"`
}

type marketDashboardResult struct {
	Thesis     string                    `json:"thesis,omitempty"`
	KPIs       []models.ProfileKPI       `json:"kpis,omitempty"`
	Scenarios  []models.ProfileScenario  `json:"scenarios,omitempty"`
	Watchlist  []models.ProfileWatchItem `json:"watchlist,omitempty"`
	Charts     []models.ProfileDataset   `json:"charts,omitempty"`
	Tables     []models.ProfileDataset   `json:"tables,omitempty"`
	Notes      string                    `json:"notes,omitempty"`
	Confidence float64                   `json:"confidence,omitempty"`
}

type orgSectionResult struct {
	Key        string                  `json:"key"`
	Title      string                  `json:"title"`
	Content    string                  `json:"content"`
	Facts      []models.ProfileFact    `json:"facts,omitempty"`
	Structured map[string]any          `json:"structured,omitempty"`
	Datasets   []models.ProfileDataset `json:"datasets,omitempty"`
	Sources    []string                `json:"sources,omitempty"`
	Confidence float64                 `json:"confidence"`
	Order      int                     `json:"order"`
}

type orgDashboardResult struct {
	Thesis     string                    `json:"thesis,omitempty"`
	KPIs       []models.ProfileKPI       `json:"kpis,omitempty"`
	Scenarios  []models.ProfileScenario  `json:"scenarios,omitempty"`
	Watchlist  []models.ProfileWatchItem `json:"watchlist,omitempty"`
	Charts     []models.ProfileDataset   `json:"charts,omitempty"`
	Tables     []models.ProfileDataset   `json:"tables,omitempty"`
	Notes      string                    `json:"notes,omitempty"`
	Confidence float64                   `json:"confidence,omitempty"`
}

type govSectionResult struct {
	Key        string                  `json:"key"`
	Title      string                  `json:"title"`
	Content    string                  `json:"content"`
	Facts      []models.ProfileFact    `json:"facts,omitempty"`
	Structured map[string]any          `json:"structured,omitempty"`
	Datasets   []models.ProfileDataset `json:"datasets,omitempty"`
	Sources    []string                `json:"sources,omitempty"`
	Confidence float64                 `json:"confidence"`
	Order      int                     `json:"order"`
}

type govDashboardResult struct {
	Thesis     string                    `json:"thesis,omitempty"`
	KPIs       []models.ProfileKPI       `json:"kpis,omitempty"`
	Scenarios  []models.ProfileScenario  `json:"scenarios,omitempty"`
	Watchlist  []models.ProfileWatchItem `json:"watchlist,omitempty"`
	Charts     []models.ProfileDataset   `json:"charts,omitempty"`
	Tables     []models.ProfileDataset   `json:"tables,omitempty"`
	Notes      string                    `json:"notes,omitempty"`
	Confidence float64                   `json:"confidence,omitempty"`
}

// ---------- DYNASTY result structs ----------

type dynastySectionResult struct {
	Key        string                  `json:"key"`
	Title      string                  `json:"title"`
	Content    string                  `json:"content"`
	Facts      []models.ProfileFact    `json:"facts,omitempty"`
	Structured map[string]any          `json:"structured,omitempty"`
	Datasets   []models.ProfileDataset `json:"datasets,omitempty"`
	Sources    []string                `json:"sources,omitempty"`
	Confidence float64                 `json:"confidence"`
	Order      int                     `json:"order"`
}

type dynastyDashboardResult struct {
	Thesis     string                    `json:"thesis,omitempty"`
	KPIs       []models.ProfileKPI       `json:"kpis,omitempty"`
	Scenarios  []models.ProfileScenario  `json:"scenarios,omitempty"`
	Watchlist  []models.ProfileWatchItem `json:"watchlist,omitempty"`
	Charts     []models.ProfileDataset   `json:"charts,omitempty"`
	Tables     []models.ProfileDataset   `json:"tables,omitempty"`
	Notes      string                    `json:"notes,omitempty"`
	Confidence float64                   `json:"confidence,omitempty"`
}

// ---------- TRIBE result structs ----------

type tribeSectionResult struct {
	Key        string                  `json:"key"`
	Title      string                  `json:"title"`
	Content    string                  `json:"content"`
	Facts      []models.ProfileFact    `json:"facts,omitempty"`
	Structured map[string]any          `json:"structured,omitempty"`
	Datasets   []models.ProfileDataset `json:"datasets,omitempty"`
	Sources    []string                `json:"sources,omitempty"`
	Confidence float64                 `json:"confidence"`
	Order      int                     `json:"order"`
}

type tribeDashboardResult struct {
	Thesis     string                    `json:"thesis,omitempty"`
	KPIs       []models.ProfileKPI       `json:"kpis,omitempty"`
	Scenarios  []models.ProfileScenario  `json:"scenarios,omitempty"`
	Watchlist  []models.ProfileWatchItem `json:"watchlist,omitempty"`
	Charts     []models.ProfileDataset   `json:"charts,omitempty"`
	Tables     []models.ProfileDataset   `json:"tables,omitempty"`
	Notes      string                    `json:"notes,omitempty"`
	Confidence float64                   `json:"confidence,omitempty"`
}

// ---------- PROGRAM result structs ----------

type programSectionResult struct {
	Key        string                  `json:"key"`
	Title      string                  `json:"title"`
	Content    string                  `json:"content"`
	Facts      []models.ProfileFact    `json:"facts,omitempty"`
	Structured map[string]any          `json:"structured,omitempty"`
	Datasets   []models.ProfileDataset `json:"datasets,omitempty"`
	Sources    []string                `json:"sources,omitempty"`
	Confidence float64                 `json:"confidence"`
	Order      int                     `json:"order"`
}

type programDashboardResult struct {
	Thesis     string                    `json:"thesis,omitempty"`
	KPIs       []models.ProfileKPI       `json:"kpis,omitempty"`
	Scenarios  []models.ProfileScenario  `json:"scenarios,omitempty"`
	Watchlist  []models.ProfileWatchItem `json:"watchlist,omitempty"`
	Charts     []models.ProfileDataset   `json:"charts,omitempty"`
	Tables     []models.ProfileDataset   `json:"tables,omitempty"`
	Notes      string                    `json:"notes,omitempty"`
	Confidence float64                   `json:"confidence,omitempty"`
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
			Facts:      sec.Facts,
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

func orgSectionSpecs() []models.ProfileSectionSpec {
	return []models.ProfileSectionSpec{
		{Key: "identity_operations", Title: "Identity & Operations", Description: "Who they are, where they operate, scale, footprint, how work is organized", Required: true, Order: 1},
		{Key: "business_moat", Title: "Business Model & Moat", Description: "How value is created, revenue streams, pricing power, defensibility", Required: true, Order: 2},
		{Key: "leadership_governance", Title: "Leadership & Governance", Description: "Key leaders, board, governance signals, leadership changes", Required: false, Order: 3},
		{Key: "financial_profile", Title: "Financial Profile", Description: "Revenue, margins, cash flow, capital structure, unit economics", Required: true, Order: 4},
		{Key: "market_position", Title: "Market Position & Competition", Description: "Competitive position, peers, share, differentiation", Required: true, Order: 5},
		{Key: "risks_constraints", Title: "Risks & Constraints", Description: "Regulatory, legal, operational, supply, cyber, concentration risks", Required: false, Order: 6},
		{Key: "trajectory_catalysts", Title: "Trajectory & Catalysts", Description: "Momentum, recent events, upcoming milestones, what changes the outlook", Required: false, Order: 7},
	}
}

// generateOrgProfileMultiStep generates each section as a separate LLM request (org-specific),
// then synthesizes a dashboard object at the end.
func (g *Generator) generateOrgProfileMultiStep(
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

	sections := orgSectionSpecs()
	sectionOrder := make(map[string]int)
	for _, s := range sections {
		sectionOrder[s.Key] = s.Order
	}

	sectionMaxOut := max(8192, envInt("HORIZON_PROFILE_SECTION_MAX_OUTPUT_TOKENS", 12000))
	dashboardMaxOut := max(8192, envInt("HORIZON_PROFILE_DASHBOARD_MAX_OUTPUT_TOKENS", 16000))

	usedModel := ""
	var allNotes []string

	for _, spec := range sections {
		prompt := g.buildOrgSectionPrompt(fragment, graphContext, spec, profile.Sections)
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

		sec, err := g.parseOrgSectionResult(parsed)
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
			Facts:      sec.Facts,
			Structured: sec.Structured,
			Datasets:   sec.Datasets,
			Sources:    sec.Sources,
			Confidence: clamp01(sec.Confidence),
			Order:      sec.Order,
		}
	}

	dashPrompt := g.buildOrgDashboardPrompt(fragment, graphContext, profile.Sections)
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
		log.Printf("[ProfileGenerator] WARNING: org dashboard synthesis failed (canonicalPath=%s): %v", fragment.CanonicalPath, err)
	} else {
		if usedModel == "" {
			usedModel = modelUsed
		}
		dash, err := g.parseOrgDashboardResult(dParsed)
		if err != nil {
			log.Printf("[ProfileGenerator] WARNING: failed to parse org dashboard synthesis (canonicalPath=%s): %v", fragment.CanonicalPath, err)
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

	profile.Generation = models.ProfileGeneration{
		GroundingUsed: true,
		SchemaVersion: "v2",
	}

	return profile, usedModel, strings.TrimSpace(strings.Join(allNotes, "\n")), nil
}

func govSectionSpecs() []models.ProfileSectionSpec {
	return []models.ProfileSectionSpec{
		{Key: "identity_jurisdiction", Title: "Identity & Jurisdiction", Description: "What the institution is, legal status, hierarchy/chain-of-command, geographic/functional jurisdiction, population/territory coverage", Required: true, Order: 1},
		{Key: "mandate_policy_domain", Title: "Mandate & Policy Domain", Description: "Formal mandate, powers, policy remit, key legal instruments, regulatory authority, international obligations if relevant", Required: true, Order: 2},
		{Key: "leadership_political_landscape", Title: "Leadership & Political Landscape", Description: "Leadership, appointments, political context, electoral cycle, coalition stability, key stakeholders, legitimacy signals", Required: false, Order: 3},
		{Key: "budget_resources", Title: "Budget & Resources", Description: "Budget size and trends, staffing/resources, funding sources, spending priorities, procurement and capability constraints", Required: false, Order: 4},
		{Key: "policy_agenda", Title: "Policy Agenda", Description: "Current initiatives, reforms, legislative pipeline, stated priorities, implementation status and timelines", Required: true, Order: 5},
		{Key: "regulatory_impact", Title: "Regulatory Impact", Description: "Regulations/enforcement actions, compliance posture, fines/sanctions/investigations, who is impacted and how", Required: false, Order: 6},
		{Key: "geopolitical_position", Title: "Geopolitical Position", Description: "International posture, alliances/organizations, sanctions regime, trade agreements, major external constraints", Required: false, Order: 7},
	}
}

// ---------- DYNASTY section specs ----------

func dynastySectionSpecs() []models.ProfileSectionSpec {
	return []models.ProfileSectionSpec{
		{Key: "identity_lineage", Title: "Identity & Lineage", Description: "Founding history, lineage, generational depth, succession principles, key branches, historical significance", Required: true, Order: 1},
		{Key: "economic_footprint", Title: "Economic Footprint", Description: "Holdings (companies, investments, real estate), key industries, estimated wealth magnitude (where publicly documented), asset classes", Required: true, Order: 2},
		{Key: "leadership_succession", Title: "Leadership & Succession", Description: "Current patriarch/matriarch or decision-makers, succession pipeline, family governance structures, generational roles", Required: true, Order: 3},
		{Key: "influence_network", Title: "Influence Network", Description: "Political connections, economic influence, board positions, philanthropic networks, advisory roles, international ties", Required: false, Order: 4},
		{Key: "legacy_reputation", Title: "Legacy & Reputation", Description: "Public perception, philanthropic initiatives, cultural contributions, brand and reputation management", Required: false, Order: 5},
		{Key: "trajectory_catalysts", Title: "Trajectory & Catalysts", Description: "Generational transitions, strategic moves, upcoming milestones, potential succession events, emerging opportunities or challenges", Required: false, Order: 6},
	}
}

// ---------- TRIBE section specs ----------

func tribeSectionSpecs() []models.ProfileSectionSpec {
	return []models.ProfileSectionSpec{
		{Key: "identity_heritage", Title: "Identity & Heritage", Description: "Origins, cultural identity, language/traditions, geographic homeland, population estimates, sub-tribes/clans, historical context", Required: true, Order: 1},
		{Key: "traditional_governance", Title: "Traditional Governance", Description: "Leadership structures (councils, chiefs, elders), decision-making processes, succession mechanisms, traditional laws/customs", Required: true, Order: 2},
		{Key: "economic_activities", Title: "Economic Activities", Description: "Traditional livelihoods, modern economic participation, key industries, business interests, economic challenges and opportunities", Required: true, Order: 3},
		{Key: "political_engagement", Title: "Political Engagement", Description: "Political representation, relationship with central government, autonomy status, historical agreements, current political role", Required: false, Order: 4},
		{Key: "alliance_network", Title: "Alliance Network", Description: "Relations with other tribes, confederations, historical alliances, regional partnerships, diaspora communities", Required: false, Order: 5},
		{Key: "contemporary_dynamics", Title: "Contemporary Dynamics", Description: "Current challenges and opportunities, urbanization/migration patterns, cultural preservation efforts, modernization initiatives", Required: false, Order: 6},
	}
}

// ---------- PROGRAM section specs ----------

func programSectionSpecs() []models.ProfileSectionSpec {
	return []models.ProfileSectionSpec{
		{Key: "identity_scope", Title: "Identity & Scope", Description: "What the program is, sponsoring entity, objectives, geographic/sectoral scope, timeline, budget authorization, legal basis", Required: true, Order: 1},
		{Key: "funding_resources", Title: "Funding & Resources", Description: "Budget size, funding sources, disbursement schedule, resource allocation, procurement mechanisms, cost structure", Required: true, Order: 2},
		{Key: "implementation_status", Title: "Implementation Status", Description: "Current phase, milestones achieved, timeline adherence, key deliverables, contractors/implementers, operational structure", Required: true, Order: 3},
		{Key: "stakeholder_ecosystem", Title: "Stakeholder Ecosystem", Description: "Sponsors, implementing agencies, beneficiaries, partners, critics, oversight bodies, political support", Required: false, Order: 4},
		{Key: "impact_outcomes", Title: "Impact & Outcomes", Description: "KPIs, measured results, economic/social impact, success metrics, evaluation findings, beneficiary feedback", Required: false, Order: 5},
		{Key: "risks_constraints", Title: "Risks & Constraints", Description: "Political risks, funding risks, implementation challenges, dependencies, regulatory hurdles, external factors", Required: false, Order: 6},
		{Key: "trajectory_outlook", Title: "Trajectory & Outlook", Description: "Future phases, expansion plans, sunset provisions, successor programs, strategic direction, sustainability", Required: false, Order: 7},
	}
}

// generateGovProfileMultiStep generates each section as a separate LLM request (gov-specific),
// then synthesizes a dashboard object at the end.
func (g *Generator) generateGovProfileMultiStep(
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

	sections := govSectionSpecs()
	sectionOrder := make(map[string]int)
	for _, s := range sections {
		sectionOrder[s.Key] = s.Order
	}

	sectionMaxOut := max(8192, envInt("HORIZON_PROFILE_SECTION_MAX_OUTPUT_TOKENS", 12000))
	dashboardMaxOut := max(8192, envInt("HORIZON_PROFILE_DASHBOARD_MAX_OUTPUT_TOKENS", 16000))

	usedModel := ""
	var allNotes []string

	for _, spec := range sections {
		prompt := g.buildGovSectionPrompt(fragment, graphContext, spec, profile.Sections)
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

		sec, err := g.parseGovSectionResult(parsed)
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
			Facts:      sec.Facts,
			Structured: sec.Structured,
			Datasets:   sec.Datasets,
			Sources:    sec.Sources,
			Confidence: clamp01(sec.Confidence),
			Order:      sec.Order,
		}
	}

	dashPrompt := g.buildGovDashboardPrompt(fragment, graphContext, profile.Sections)
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
		log.Printf("[ProfileGenerator] WARNING: gov dashboard synthesis failed (canonicalPath=%s): %v", fragment.CanonicalPath, err)
	} else {
		if usedModel == "" {
			usedModel = modelUsed
		}
		dash, err := g.parseGovDashboardResult(dParsed)
		if err != nil {
			log.Printf("[ProfileGenerator] WARNING: failed to parse gov dashboard synthesis (canonicalPath=%s): %v", fragment.CanonicalPath, err)
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

	profile.Generation = models.ProfileGeneration{
		GroundingUsed: true,
		SchemaVersion: "v2",
	}

	return profile, usedModel, strings.TrimSpace(strings.Join(allNotes, "\n")), nil
}

// ---------- DYNASTY profile generation ----------

// generateDynastyProfileMultiStep generates each section as a separate LLM request (dynasty-specific),
// then synthesizes a dashboard object at the end.
func (g *Generator) generateDynastyProfileMultiStep(
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

	sections := dynastySectionSpecs()
	sectionOrder := make(map[string]int)
	for _, s := range sections {
		sectionOrder[s.Key] = s.Order
	}

	sectionMaxOut := max(8192, envInt("HORIZON_PROFILE_SECTION_MAX_OUTPUT_TOKENS", 12000))
	dashboardMaxOut := max(8192, envInt("HORIZON_PROFILE_DASHBOARD_MAX_OUTPUT_TOKENS", 16000))

	usedModel := ""
	var allNotes []string

	for _, spec := range sections {
		prompt := g.buildDynastySectionPrompt(fragment, graphContext, spec, profile.Sections)
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

		sec, err := g.parseDynastySectionResult(parsed)
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
			Facts:      sec.Facts,
			Structured: sec.Structured,
			Datasets:   sec.Datasets,
			Sources:    sec.Sources,
			Confidence: clamp01(sec.Confidence),
			Order:      sec.Order,
		}
	}

	dashPrompt := g.buildDynastyDashboardPrompt(fragment, graphContext, profile.Sections)
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
		log.Printf("[ProfileGenerator] WARNING: dynasty dashboard synthesis failed (canonicalPath=%s): %v", fragment.CanonicalPath, err)
	} else {
		if usedModel == "" {
			usedModel = modelUsed
		}
		dash, err := g.parseDynastyDashboardResult(dParsed)
		if err != nil {
			log.Printf("[ProfileGenerator] WARNING: failed to parse dynasty dashboard synthesis (canonicalPath=%s): %v", fragment.CanonicalPath, err)
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

	profile.Generation = models.ProfileGeneration{
		GroundingUsed: true,
		SchemaVersion: "v2",
	}

	return profile, usedModel, strings.TrimSpace(strings.Join(allNotes, "\n")), nil
}

// ---------- TRIBE profile generation ----------

// generateTribeProfileMultiStep generates each section as a separate LLM request (tribe-specific),
// then synthesizes a dashboard object at the end.
func (g *Generator) generateTribeProfileMultiStep(
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

	sections := tribeSectionSpecs()
	sectionOrder := make(map[string]int)
	for _, s := range sections {
		sectionOrder[s.Key] = s.Order
	}

	sectionMaxOut := max(8192, envInt("HORIZON_PROFILE_SECTION_MAX_OUTPUT_TOKENS", 12000))
	dashboardMaxOut := max(8192, envInt("HORIZON_PROFILE_DASHBOARD_MAX_OUTPUT_TOKENS", 16000))

	usedModel := ""
	var allNotes []string

	for _, spec := range sections {
		prompt := g.buildTribeSectionPrompt(fragment, graphContext, spec, profile.Sections)
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

		sec, err := g.parseTribeSectionResult(parsed)
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
			Facts:      sec.Facts,
			Structured: sec.Structured,
			Datasets:   sec.Datasets,
			Sources:    sec.Sources,
			Confidence: clamp01(sec.Confidence),
			Order:      sec.Order,
		}
	}

	dashPrompt := g.buildTribeDashboardPrompt(fragment, graphContext, profile.Sections)
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
		log.Printf("[ProfileGenerator] WARNING: tribe dashboard synthesis failed (canonicalPath=%s): %v", fragment.CanonicalPath, err)
	} else {
		if usedModel == "" {
			usedModel = modelUsed
		}
		dash, err := g.parseTribeDashboardResult(dParsed)
		if err != nil {
			log.Printf("[ProfileGenerator] WARNING: failed to parse tribe dashboard synthesis (canonicalPath=%s): %v", fragment.CanonicalPath, err)
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

	profile.Generation = models.ProfileGeneration{
		GroundingUsed: true,
		SchemaVersion: "v2",
	}

	return profile, usedModel, strings.TrimSpace(strings.Join(allNotes, "\n")), nil
}

// ---------- PROGRAM profile generation ----------

// generateProgramProfileMultiStep generates each section as a separate LLM request (program-specific),
// then synthesizes a dashboard object at the end.
func (g *Generator) generateProgramProfileMultiStep(
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

	sections := programSectionSpecs()
	sectionOrder := make(map[string]int)
	for _, s := range sections {
		sectionOrder[s.Key] = s.Order
	}

	sectionMaxOut := max(8192, envInt("HORIZON_PROFILE_SECTION_MAX_OUTPUT_TOKENS", 12000))
	dashboardMaxOut := max(8192, envInt("HORIZON_PROFILE_DASHBOARD_MAX_OUTPUT_TOKENS", 16000))

	usedModel := ""
	var allNotes []string

	for _, spec := range sections {
		prompt := g.buildProgramSectionPrompt(fragment, graphContext, spec, profile.Sections)
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

		sec, err := g.parseProgramSectionResult(parsed)
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
			Facts:      sec.Facts,
			Structured: sec.Structured,
			Datasets:   sec.Datasets,
			Sources:    sec.Sources,
			Confidence: clamp01(sec.Confidence),
			Order:      sec.Order,
		}
	}

	dashPrompt := g.buildProgramDashboardPrompt(fragment, graphContext, profile.Sections)
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
		log.Printf("[ProfileGenerator] WARNING: program dashboard synthesis failed (canonicalPath=%s): %v", fragment.CanonicalPath, err)
	} else {
		if usedModel == "" {
			usedModel = modelUsed
		}
		dash, err := g.parseProgramDashboardResult(dParsed)
		if err != nil {
			log.Printf("[ProfileGenerator] WARNING: failed to parse program dashboard synthesis (canonicalPath=%s): %v", fragment.CanonicalPath, err)
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
	asOf := time.Now().UTC().Format("2006-01-02")
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
			"key":            s.Key,
			"title":          s.Title,
			"order":          s.Order,
			"confidence":     s.Confidence,
			"structured":     s.Structured,
			"datasets":       s.Datasets,
			"contentExcerpt": content,
		})
	}
	priorJSON, _ := json.MarshalIndent(priorSlim, "", "  ")

	return fmt.Sprintf(`You are generating ONE section of a high-grade market profile for an analyst dashboard.

IMPORTANT:
- As-of date: %s (UTC). Use this as the reference point for "current" data in your grounded research.
- Do NOT include the as-of date in any machine-generated identifiers/keys/titles. The system will display update timestamps separately.
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
  "facts": [
    { "key": "market_size_2023_usd", "label": "Market size", "value": "$693.7B", "unit": "USD", "period": "2023", "sources": ["https://..."] }
  ],
  "structured": { "optional_extra": "avoid raw key/value dumps; prefer facts above" },
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
 - facts[].label MUST be human-readable (no underscores). Use facts for key numbers and short descriptors shown to users.
 - facts[].value MUST be human-formatted (never a bare number). Example: "$44.6B", "77%%", "52 weeks".
 - facts[].unit MUST be set when the value is numeric, using a consistent vocabulary: "USD"|"EUR"|"GBP"|"percent"|"weeks"|"days"|"hours"|"index"|"count"|... If you cannot determine the unit from sources, explicitly say so and do more grounded search until you can.
`, asOf, string(entityJSON), string(priorJSON), spec.Key, spec.Title, spec.Description, spec.Key, spec.Order)
}

func (g *Generator) buildMarketDashboardPrompt(
	fragment *models.HorizonFragment,
	graphContext *models.GraphContextSummary,
	sections map[string]models.ProfileSection,
) string {
	asOf := time.Now().UTC().Format("2006-01-02")
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
			"key":            s.Key,
			"title":          s.Title,
			"order":          s.Order,
			"confidence":     s.Confidence,
			"structured":     s.Structured,
			"datasets":       s.Datasets,
			"contentExcerpt": content,
			"sources":        s.Sources,
		})
	}
	sectionsJSON, _ := json.MarshalIndent(secSlim, "", "  ")

	return fmt.Sprintf(`You are synthesizing a high-grade MARKET DASHBOARD from the already-generated profile sections.

IMPORTANT:
- As-of date: %s (UTC). Use this as the reference point for "current" data in your synthesis.
- Do NOT include the as-of date in any machine-generated identifiers/keys/titles. The system will display update timestamps separately.
- Use Google Search grounding ONLY if needed to validate key numbers; prefer using the provided sections to avoid contradictions.
- Do not introduce new major numbers unless you can cite a source URL.
- Provide decision-support blocks: thesis, KPI strip, scenarios with probabilities+triggers, and a watchlist of leading indicators.
- Output must conform to the JSON shape below. Types matter:
  - charts[].series[].points[].y MUST be a number.
  - charts[].series[].points[].meta MUST be a JSON object.
  - sources MUST be an array of strings.
  - scenarios[].metricItems[] MUST be used for scenario metrics; label MUST be human readable (no underscores).

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
    {
      "id": "base",
      "name": "Base",
      "probability": 0.55,
      "assumptions": ["..."],
      "metricItems": [
        { "key": "2032_market_valuation_usd", "label": "Market valuation", "value": "$1.3T", "unit": "USD", "period": "2032" }
      ],
      "metrics": { "2032_market_valuation_usd": "$1.3T" },
      "triggers": ["..."]
    }
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
`, asOf, string(entityJSON), string(sectionsJSON))
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

func (g *Generator) buildOrgSectionPrompt(
	fragment *models.HorizonFragment,
	graphContext *models.GraphContextSummary,
	spec models.ProfileSectionSpec,
	prior map[string]models.ProfileSection,
) string {
	asOf := time.Now().UTC().Format("2006-01-02")
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
			"key":            s.Key,
			"title":          s.Title,
			"order":          s.Order,
			"confidence":     s.Confidence,
			"structured":     s.Structured,
			"datasets":       s.Datasets,
			"contentExcerpt": content,
		})
	}
	priorJSON, _ := json.MarshalIndent(priorSlim, "", "  ")

	return fmt.Sprintf(`You are generating ONE section of a high-grade ORGANIZATION profile for an analyst dashboard.

IMPORTANT:
- As-of date: %s (UTC). Use this as the reference point for "current" data in your grounded research.
- Do NOT include the as-of date in any machine-generated identifiers/keys/titles. The system will display update timestamps separately.
- Use Google Search grounding to find current, accurate information.
- Do NOT invent facts. If specific numbers are not available, state "unknown" explicitly and explain what you'd need to verify.
- Avoid repeating information that already appears in prior sections.
- Provide sources as direct URLs to the exact pages/reports where the claim/number appears (avoid homepages).
- Focus on the organization itself (execution, economics, governance, constraints), not generic market commentary.
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
  "facts": [
    { "key": "revenue_2025_usd", "label": "Revenue", "value": "$44.6B", "unit": "USD", "period": "2025", "sources": ["https://..."] }
  ],
  "structured": { "optional_extra": "avoid raw key/value dumps; prefer facts above" },
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
 - facts[].label MUST be human-readable (no underscores). Use facts for key numbers and short descriptors shown to users.
 - facts[].value MUST be human-formatted (never a bare number). Example: "$44.6B", "77%%", "52 weeks".
 - facts[].unit MUST be set when the value is numeric, using a consistent vocabulary: "USD"|"EUR"|"GBP"|"percent"|"weeks"|"days"|"hours"|"index"|"count"|... If you cannot determine the unit from sources, explicitly say so and do more grounded search until you can.
`, asOf, string(entityJSON), string(priorJSON), spec.Key, spec.Title, spec.Description, spec.Key, spec.Order)
}

func (g *Generator) buildGovSectionPrompt(
	fragment *models.HorizonFragment,
	graphContext *models.GraphContextSummary,
	spec models.ProfileSectionSpec,
	prior map[string]models.ProfileSection,
) string {
	asOf := time.Now().UTC().Format("2006-01-02")
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
			"key":            s.Key,
			"title":          s.Title,
			"order":          s.Order,
			"confidence":     s.Confidence,
			"structured":     s.Structured,
			"datasets":       s.Datasets,
			"contentExcerpt": content,
		})
	}
	priorJSON, _ := json.MarshalIndent(priorSlim, "", "  ")

	return fmt.Sprintf(`You are generating ONE section of a high-grade GOVERNMENT / PUBLIC INSTITUTION profile for an analyst dashboard.

IMPORTANT:
- As-of date: %s (UTC). Use this as the reference point for "current" data in your grounded research.
- Do NOT include the as-of date in any machine-generated identifiers/keys/titles. The system will display update timestamps separately.
- Use Google Search grounding to find current, accurate information (laws, official sites, budget documents, election calendars, treaty databases, credible datasets).
- Do NOT invent facts. If specific numbers are not available, state "unknown" explicitly and explain what you'd need to verify.
- Avoid repeating information that already appears in prior sections.
- Provide sources as direct URLs to the exact pages/documents where the claim/number appears (avoid homepages).
- Focus on power/mandate/policy/regulatory impact, not corporate financials. Profit/EBITDA are usually not meaningful here.
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
  "content": "Markdown narrative that supports the facts and datasets",
  "facts": [
    { "key": "annual_budget_usd", "label": "Annual budget", "value": "$12.4B", "unit": "USD", "period": "2025", "sources": ["https://..."] },
    { "key": "population_covered", "label": "Population covered", "value": "67.1M", "unit": "count", "period": "2024", "sources": ["https://..."] }
  ],
  "structured": { "optional_extra": "avoid raw key/value dumps; prefer facts above" },
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
 - facts[].label MUST be human-readable (no underscores). Use facts for key numbers and short descriptors shown to users.
 - facts[].value MUST be human-formatted (never a bare number). Example: "$12.4B", "67.1M", "Q4 2026".
 - facts[].unit MUST be set when the value is numeric, using a consistent vocabulary: "USD"|"EUR"|"GBP"|"percent"|"count"|"index"|... If you cannot determine the unit from sources, explicitly say so and do more grounded search until you can.
`, asOf, string(entityJSON), string(priorJSON), spec.Key, spec.Title, spec.Description, spec.Key, spec.Order)
}

func (g *Generator) buildOrgDashboardPrompt(
	fragment *models.HorizonFragment,
	graphContext *models.GraphContextSummary,
	sections map[string]models.ProfileSection,
) string {
	asOf := time.Now().UTC().Format("2006-01-02")
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
			"key":            s.Key,
			"title":          s.Title,
			"order":          s.Order,
			"confidence":     s.Confidence,
			"structured":     s.Structured,
			"datasets":       s.Datasets,
			"contentExcerpt": content,
			"sources":        s.Sources,
		})
	}
	sectionsJSON, _ := json.MarshalIndent(secSlim, "", "  ")

	return fmt.Sprintf(`You are synthesizing a high-grade ORGANIZATION DASHBOARD from the already-generated profile sections.

IMPORTANT:
- As-of date: %s (UTC). Use this as the reference point for "current" data in your synthesis.
- Do NOT include the as-of date in any machine-generated identifiers/keys/titles. The system will display update timestamps separately.
- Use Google Search grounding ONLY if needed to validate key numbers; prefer using the provided sections to avoid contradictions.
- Do not introduce new major numbers unless you can cite a source URL.
- Provide decision-support blocks: thesis, KPI strip, scenarios with probabilities+triggers, and a watchlist of leading indicators.
- Output must conform to the JSON shape below. Types matter:
  - charts[].series[].points[].y MUST be a number.
  - charts[].series[].points[].meta MUST be a JSON object.
  - sources MUST be an array of strings.
  - scenarios[].metricItems[] MUST be used for scenario metrics; label MUST be human readable (no underscores).

ENTITY CONTEXT:
%s

SECTIONS (source-of-truth; avoid duplication and contradictions):
%s

REQUIRED OUTPUT FORMAT (JSON) - output ONLY valid JSON:
{
  "thesis": "5-8 sentence decision-grade summary focused on execution, economics, constraints, and trajectory",
  "kpis": [
    { "id": "revenue", "label": "Revenue", "value": "$12.4B", "subtext": "FY2025", "trendValue": "+8%% YoY", "severity": "neutral", "confidence": 0.0 }
  ],
  "scenarios": [
    {
      "id": "base",
      "name": "Base case",
      "probability": 0.55,
      "assumptions": ["..."],
      "metricItems": [
        { "key": "ebitda_margin_2026", "label": "EBITDA margin", "value": "18%%", "unit": "percent", "period": "2026" }
      ],
      "metrics": { "ebitda_margin_2026": "18%%" },
      "triggers": ["..."]
    }
  ],
  "watchlist": [
    { "id": "leadership_churn", "indicator": "C-level departures", "current": "0 in 12 months", "threshold": ">=2 in 6 months", "signal": "Stable leadership", "frequency": "monthly", "sources": ["https://..."], "confidence": 0.0 }
  ],
  "charts": [
    {
      "id": "revenue_timeseries",
      "title": "Revenue (history + forecast)",
      "type": "timeseries",
      "unit": "billion",
      "currency": "USD",
      "series": [ { "name": "Revenue", "points": [ { "x": "2024", "y": 12.4 } ] } ],
      "sources": ["https://..."],
      "confidence": 0.0
    }
  ],
  "tables": [
    {
      "id": "segment_mix",
      "title": "Segment mix",
      "type": "table",
      "table": {
        "columns": [
          { "id": "segment", "title": "Segment" },
          { "id": "share", "title": "Share", "format": "percent" },
          { "id": "trend", "title": "Trend" }
        ],
        "rows": [
          { "segment": "Enterprise", "share": 0.58, "trend": "growing" }
        ]
      },
      "sources": ["https://..."],
      "confidence": 0.0
    }
  ],
  "notes": "brief caveats and data gaps",
  "confidence": 0.0
}
`, asOf, string(entityJSON), string(sectionsJSON))
}

func (g *Generator) buildGovDashboardPrompt(
	fragment *models.HorizonFragment,
	graphContext *models.GraphContextSummary,
	sections map[string]models.ProfileSection,
) string {
	asOf := time.Now().UTC().Format("2006-01-02")
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
			"key":            s.Key,
			"title":          s.Title,
			"order":          s.Order,
			"confidence":     s.Confidence,
			"structured":     s.Structured,
			"datasets":       s.Datasets,
			"contentExcerpt": content,
			"sources":        s.Sources,
		})
	}
	sectionsJSON, _ := json.MarshalIndent(secSlim, "", "  ")

	return fmt.Sprintf(`You are synthesizing a high-grade GOVERNMENT / PUBLIC INSTITUTION DASHBOARD from the already-generated profile sections.

IMPORTANT:
- As-of date: %s (UTC). Use this as the reference point for "current" data in your synthesis.
- Do NOT include the as-of date in any machine-generated identifiers/keys/titles. The system will display update timestamps separately.
- Use Google Search grounding ONLY if needed to validate key numbers; prefer using the provided sections to avoid contradictions.
- Do not introduce new major numbers unless you can cite a source URL.
- Provide decision-support blocks: thesis, KPI strip, scenarios with probabilities+triggers, and a watchlist of leading indicators.
- Focus on mandate/power/regulatory impact, policy trajectory, budget/resources, political timeline, and geopolitics.
- Output must conform to the JSON shape below. Types matter:
  - charts[].series[].points[].y MUST be a number.
  - charts[].series[].points[].meta MUST be a JSON object.
  - sources MUST be an array of strings.
  - scenarios[].metricItems[] MUST be used for scenario metrics; label MUST be human readable (no underscores).

ENTITY CONTEXT:
%s

SECTIONS (source-of-truth; avoid duplication and contradictions):
%s

REQUIRED OUTPUT FORMAT (JSON) - output ONLY valid JSON:
{
  "thesis": "5-8 sentence decision-grade summary focused on mandate, leverage, policy direction, constraints, and near-term catalysts",
  "kpis": [
    { "id": "budget", "label": "Annual budget", "value": "$12.4B", "subtext": "FY2025", "trendValue": "+6%% YoY", "severity": "neutral", "confidence": 0.0 },
    { "id": "jurisdiction", "label": "Jurisdiction", "value": "National", "subtext": "", "trendValue": "", "severity": "neutral", "confidence": 0.0 }
  ],
  "scenarios": [
    {
      "id": "base",
      "name": "Base case",
      "probability": 0.55,
      "assumptions": ["..."],
      "metricItems": [
        { "key": "policy_direction_12m", "label": "Policy direction (12m)", "value": "status quo", "unit": "index", "period": "12m" }
      ],
      "metrics": { "policy_direction_12m": "status quo" },
      "triggers": ["..."]
    }
  ],
  "watchlist": [
    { "id": "election_calendar", "indicator": "Election / leadership cycle", "current": "Next election: 2026-05", "threshold": "snap election announced", "signal": "Volatility risk", "frequency": "weekly", "sources": ["https://..."], "confidence": 0.0 },
    { "id": "bill_pipeline", "indicator": "Key bill / regulation pipeline", "current": "3 high-impact bills active", "threshold": "bill passed / vetoed", "signal": "Policy shift", "frequency": "weekly", "sources": ["https://..."], "confidence": 0.0 }
  ],
  "charts": [
    {
      "id": "budget_timeseries",
      "title": "Budget (history)",
      "type": "timeseries",
      "unit": "billion",
      "currency": "USD",
      "series": [ { "name": "Budget", "points": [ { "x": "2024", "y": 12.4 } ] } ],
      "sources": ["https://..."],
      "confidence": 0.0
    }
  ],
  "tables": [],
  "notes": "brief caveats and data gaps",
  "confidence": 0.0
}
`, asOf, string(entityJSON), string(sectionsJSON))
}

// ---------- DYNASTY prompt builders ----------

func (g *Generator) buildDynastySectionPrompt(
	fragment *models.HorizonFragment,
	graphContext *models.GraphContextSummary,
	spec models.ProfileSectionSpec,
	prior map[string]models.ProfileSection,
) string {
	asOf := time.Now().UTC().Format("2006-01-02")
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
			"key":            s.Key,
			"title":          s.Title,
			"order":          s.Order,
			"confidence":     s.Confidence,
			"structured":     s.Structured,
			"datasets":       s.Datasets,
			"contentExcerpt": content,
		})
	}
	priorJSON, _ := json.MarshalIndent(priorSlim, "", "  ")

	return fmt.Sprintf(`You are generating ONE section of a high-grade DYNASTY/FAMILY profile for an analyst dashboard.

CRITICAL GUIDELINES:
- MAINTAIN A RESPECTFUL, NEUTRAL, AND PROFESSIONAL TONE throughout. This profile covers influential families and dynasties.
- Do NOT make political judgments, one-sided interpretations, or speculative claims about individuals or the family.
- Present information factually, citing credible sources. Avoid gossip, tabloid content, or unverified claims.
- When discussing risks or challenges, frame them neutrally without editorializing (e.g., "succession planning is a common consideration" rather than negative framing).
- Focus on publicly documented information: holdings, philanthropy, public roles, historical significance.

IMPORTANT:
- As-of date: %s (UTC). Use this as the reference point for "current" data in your grounded research.
- Do NOT include the as-of date in any machine-generated identifiers/keys/titles. The system will display update timestamps separately.
- Use Google Search grounding to find current, accurate information (company registries, official family office sites, credible business publications, philanthropy databases).
- Do NOT invent facts. If specific numbers are not available, state "unknown" or "not publicly disclosed" explicitly.
- Avoid repeating information that already appears in prior sections.
- Provide sources as direct URLs to the exact pages/documents where the claim/number appears (avoid homepages).
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
  "content": "Markdown narrative that supports the facts and datasets, written in a respectful and professional tone",
  "facts": [
    { "key": "founding_year", "label": "Founded", "value": "1870", "unit": "year", "period": "", "sources": ["https://..."] },
    { "key": "generations_active", "label": "Generations active", "value": "6", "unit": "count", "period": "", "sources": ["https://..."] }
  ],
  "structured": { "optional_extra": "avoid raw key/value dumps; prefer facts above" },
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
- facts[].label MUST be human-readable (no underscores). Use facts for key numbers and short descriptors shown to users.
- facts[].value MUST be human-formatted (never a bare number). Example: "$12.4B", "6 generations", "1870".
- facts[].unit MUST be set when the value is numeric, using a consistent vocabulary: "USD"|"EUR"|"GBP"|"percent"|"count"|"year"|... If you cannot determine the unit from sources, explicitly say so and do more grounded search until you can.
`, asOf, string(entityJSON), string(priorJSON), spec.Key, spec.Title, spec.Description, spec.Key, spec.Order)
}

func (g *Generator) buildDynastyDashboardPrompt(
	fragment *models.HorizonFragment,
	graphContext *models.GraphContextSummary,
	sections map[string]models.ProfileSection,
) string {
	asOf := time.Now().UTC().Format("2006-01-02")
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
			"key":            s.Key,
			"title":          s.Title,
			"order":          s.Order,
			"confidence":     s.Confidence,
			"structured":     s.Structured,
			"datasets":       s.Datasets,
			"contentExcerpt": content,
			"sources":        s.Sources,
		})
	}
	sectionsJSON, _ := json.MarshalIndent(secSlim, "", "  ")

	return fmt.Sprintf(`You are synthesizing a high-grade DYNASTY/FAMILY DASHBOARD from the already-generated profile sections.

CRITICAL GUIDELINES:
- MAINTAIN A RESPECTFUL, NEUTRAL, AND PROFESSIONAL TONE throughout. This profile covers influential families and dynasties.
- Do NOT make political judgments, one-sided interpretations, or speculative claims about individuals or the family.
- Present information factually. Avoid gossip, tabloid content, or unverified claims.
- When discussing risks or scenarios, frame them neutrally (e.g., "succession transitions are a standard consideration for family enterprises").
- Focus on publicly documented information: holdings, philanthropy, public roles, historical significance.

IMPORTANT:
- As-of date: %s (UTC). Use this as the reference point for "current" data in your synthesis.
- Do NOT include the as-of date in any machine-generated identifiers/keys/titles. The system will display update timestamps separately.
- Use Google Search grounding ONLY if needed to validate key numbers; prefer using the provided sections to avoid contradictions.
- Do not introduce new major numbers unless you can cite a source URL.
- Provide decision-support blocks: thesis, KPI strip, scenarios with probabilities+triggers, and a watchlist of leading indicators.
- Focus on economic footprint, succession dynamics, influence network, and trajectory.
- Output must conform to the JSON shape below. Types matter:
  - charts[].series[].points[].y MUST be a number.
  - charts[].series[].points[].meta MUST be a JSON object.
  - sources MUST be an array of strings.
  - scenarios[].metricItems[] MUST be used for scenario metrics; label MUST be human readable (no underscores).

ENTITY CONTEXT:
%s

SECTIONS (source-of-truth; avoid duplication and contradictions):
%s

REQUIRED OUTPUT FORMAT (JSON) - output ONLY valid JSON:
{
  "thesis": "5-8 sentence decision-grade summary focused on economic footprint, influence, succession dynamics, and near-term trajectory — written in a respectful and professional tone",
  "kpis": [
    { "id": "estimated_wealth", "label": "Estimated wealth", "value": "$50B+", "subtext": "Forbes estimate", "trendValue": "", "severity": "neutral", "confidence": 0.0 },
    { "id": "generations", "label": "Generations active", "value": "5", "subtext": "", "trendValue": "", "severity": "neutral", "confidence": 0.0 }
  ],
  "scenarios": [
    {
      "id": "continuity",
      "name": "Continuity",
      "probability": 0.60,
      "assumptions": ["..."],
      "metricItems": [
        { "key": "succession_stability", "label": "Succession stability", "value": "stable", "unit": "index", "period": "3-5y" }
      ],
      "metrics": { "succession_stability": "stable" },
      "triggers": ["..."]
    }
  ],
  "watchlist": [
    { "id": "succession_events", "indicator": "Leadership transitions", "current": "No imminent changes", "threshold": "New generation taking control", "signal": "Strategic shift", "frequency": "quarterly", "sources": ["https://..."], "confidence": 0.0 },
    { "id": "major_transactions", "indicator": "Major asset transactions", "current": "Steady holdings", "threshold": "Large acquisition or divestiture", "signal": "Portfolio rebalancing", "frequency": "monthly", "sources": ["https://..."], "confidence": 0.0 }
  ],
  "charts": [
    {
      "id": "wealth_estimate_history",
      "title": "Estimated wealth (where publicly reported)",
      "type": "timeseries",
      "unit": "billion",
      "currency": "USD",
      "series": [ { "name": "Wealth", "points": [ { "x": "2024", "y": 50.0 } ] } ],
      "sources": ["https://..."],
      "confidence": 0.0
    }
  ],
  "tables": [],
  "notes": "brief caveats and data gaps — note that wealth estimates are approximations from public sources",
  "confidence": 0.0
}
`, asOf, string(entityJSON), string(sectionsJSON))
}

// ---------- TRIBE prompt builders ----------

func (g *Generator) buildTribeSectionPrompt(
	fragment *models.HorizonFragment,
	graphContext *models.GraphContextSummary,
	spec models.ProfileSectionSpec,
	prior map[string]models.ProfileSection,
) string {
	asOf := time.Now().UTC().Format("2006-01-02")
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
			"key":            s.Key,
			"title":          s.Title,
			"order":          s.Order,
			"confidence":     s.Confidence,
			"structured":     s.Structured,
			"datasets":       s.Datasets,
			"contentExcerpt": content,
		})
	}
	priorJSON, _ := json.MarshalIndent(priorSlim, "", "  ")

	return fmt.Sprintf(`You are generating ONE section of a high-grade TRIBE/CLAN profile for an analyst dashboard.

CRITICAL GUIDELINES:
- MAINTAIN A RESPECTFUL, NEUTRAL, AND CULTURALLY SENSITIVE TONE throughout. This profile covers tribal and clan communities with rich cultural heritage.
- Do NOT make political judgments, one-sided interpretations, or speculative claims about the tribe or its members.
- Present information factually, citing credible sources (academic research, government statistics, reputable journalism).
- Respect cultural traditions and governance structures. Frame traditional practices as legitimate forms of social organization.
- When discussing challenges, use neutral language without editorializing (e.g., "urbanization presents both opportunities and challenges for cultural preservation").
- Avoid stereotypes or generalizations. Focus on documented facts about specific communities.

IMPORTANT:
- As-of date: %s (UTC). Use this as the reference point for "current" data in your grounded research.
- Do NOT include the as-of date in any machine-generated identifiers/keys/titles. The system will display update timestamps separately.
- Use Google Search grounding to find current, accurate information (academic sources, government census data, reputable ethnographic research, official tribal/community websites).
- Do NOT invent facts. If specific numbers are not available, state "unknown" or "estimates vary" explicitly.
- Avoid repeating information that already appears in prior sections.
- Provide sources as direct URLs to the exact pages/documents where the claim/number appears (avoid homepages).
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
  "content": "Markdown narrative that supports the facts and datasets, written in a respectful and culturally sensitive tone",
  "facts": [
    { "key": "population_estimate", "label": "Estimated population", "value": "~500,000", "unit": "count", "period": "2024", "sources": ["https://..."] },
    { "key": "homeland_region", "label": "Traditional homeland", "value": "Eastern Province", "unit": "", "period": "", "sources": ["https://..."] }
  ],
  "structured": { "optional_extra": "avoid raw key/value dumps; prefer facts above" },
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
- facts[].label MUST be human-readable (no underscores). Use facts for key numbers and short descriptors shown to users.
- facts[].value MUST be human-formatted (never a bare number). Example: "~500,000", "Eastern Province", "12 sub-clans".
- facts[].unit MUST be set when the value is numeric, using a consistent vocabulary: "count"|"percent"|"year"|... If you cannot determine the unit from sources, explicitly say so.
`, asOf, string(entityJSON), string(priorJSON), spec.Key, spec.Title, spec.Description, spec.Key, spec.Order)
}

func (g *Generator) buildTribeDashboardPrompt(
	fragment *models.HorizonFragment,
	graphContext *models.GraphContextSummary,
	sections map[string]models.ProfileSection,
) string {
	asOf := time.Now().UTC().Format("2006-01-02")
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
			"key":            s.Key,
			"title":          s.Title,
			"order":          s.Order,
			"confidence":     s.Confidence,
			"structured":     s.Structured,
			"datasets":       s.Datasets,
			"contentExcerpt": content,
			"sources":        s.Sources,
		})
	}
	sectionsJSON, _ := json.MarshalIndent(secSlim, "", "  ")

	return fmt.Sprintf(`You are synthesizing a high-grade TRIBE/CLAN DASHBOARD from the already-generated profile sections.

CRITICAL GUIDELINES:
- MAINTAIN A RESPECTFUL, NEUTRAL, AND CULTURALLY SENSITIVE TONE throughout. This profile covers tribal and clan communities with rich cultural heritage.
- Do NOT make political judgments, one-sided interpretations, or speculative claims about the tribe or its members.
- Present information factually. Respect cultural traditions and governance structures.
- When discussing scenarios or challenges, frame them neutrally and respectfully.
- Avoid stereotypes or generalizations. Focus on documented facts about specific communities.

IMPORTANT:
- As-of date: %s (UTC). Use this as the reference point for "current" data in your synthesis.
- Do NOT include the as-of date in any machine-generated identifiers/keys/titles. The system will display update timestamps separately.
- Use Google Search grounding ONLY if needed to validate key numbers; prefer using the provided sections to avoid contradictions.
- Do not introduce new major numbers unless you can cite a source URL.
- Provide decision-support blocks: thesis, KPI strip, scenarios with probabilities+triggers, and a watchlist of leading indicators.
- Focus on cultural heritage, governance, economic activities, political engagement, and alliance networks.
- Output must conform to the JSON shape below. Types matter:
  - charts[].series[].points[].y MUST be a number.
  - charts[].series[].points[].meta MUST be a JSON object.
  - sources MUST be an array of strings.
  - scenarios[].metricItems[] MUST be used for scenario metrics; label MUST be human readable (no underscores).

ENTITY CONTEXT:
%s

SECTIONS (source-of-truth; avoid duplication and contradictions):
%s

REQUIRED OUTPUT FORMAT (JSON) - output ONLY valid JSON:
{
  "thesis": "5-8 sentence decision-grade summary focused on heritage, governance structures, economic role, political engagement, and contemporary dynamics — written in a respectful and culturally sensitive tone",
  "kpis": [
    { "id": "population", "label": "Estimated population", "value": "~500,000", "subtext": "2024 estimate", "trendValue": "", "severity": "neutral", "confidence": 0.0 },
    { "id": "sub_clans", "label": "Major sub-clans", "value": "12", "subtext": "", "trendValue": "", "severity": "neutral", "confidence": 0.0 }
  ],
  "scenarios": [
    {
      "id": "continuity",
      "name": "Cultural continuity",
      "probability": 0.55,
      "assumptions": ["..."],
      "metricItems": [
        { "key": "cultural_preservation", "label": "Cultural preservation", "value": "strong", "unit": "index", "period": "5y" }
      ],
      "metrics": { "cultural_preservation": "strong" },
      "triggers": ["..."]
    }
  ],
  "watchlist": [
    { "id": "leadership_changes", "indicator": "Leadership transitions", "current": "Stable traditional leadership", "threshold": "New paramount chief / council changes", "signal": "Governance shift", "frequency": "quarterly", "sources": ["https://..."], "confidence": 0.0 },
    { "id": "economic_projects", "indicator": "Major economic initiatives", "current": "Ongoing development projects", "threshold": "New major project announced", "signal": "Economic opportunity", "frequency": "monthly", "sources": ["https://..."], "confidence": 0.0 }
  ],
  "charts": [],
  "tables": [],
  "notes": "brief caveats and data gaps — note that population and demographic data may be approximate",
  "confidence": 0.0
}
`, asOf, string(entityJSON), string(sectionsJSON))
}

// ---------- PROGRAM prompt builders ----------

func (g *Generator) buildProgramSectionPrompt(
	fragment *models.HorizonFragment,
	graphContext *models.GraphContextSummary,
	spec models.ProfileSectionSpec,
	prior map[string]models.ProfileSection,
) string {
	asOf := time.Now().UTC().Format("2006-01-02")
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
			"key":            s.Key,
			"title":          s.Title,
			"order":          s.Order,
			"confidence":     s.Confidence,
			"structured":     s.Structured,
			"datasets":       s.Datasets,
			"contentExcerpt": content,
		})
	}
	priorJSON, _ := json.MarshalIndent(priorSlim, "", "  ")

	return fmt.Sprintf(`You are generating ONE section of a high-grade PROGRAM profile for an analyst dashboard.

A PROGRAM is a structured initiative with defined objectives, timeline, budget, and deliverables. Examples include:
- Government programs (infrastructure, social welfare, defense modernization)
- Corporate programs (digital transformation, R&D initiatives, expansion projects)
- International programs (development aid, trade agreements, multilateral initiatives)
- Research programs (scientific, academic, grant-funded projects)

IMPORTANT:
- As-of date: %s (UTC). Use this as the reference point for "current" data in your grounded research.
- Do NOT include the as-of date in any machine-generated identifiers/keys/titles. The system will display update timestamps separately.
- Use Google Search grounding to find current, accurate information (official program sites, government portals, budget documents, progress reports, evaluation studies).
- Do NOT invent facts. If specific numbers are not available, state "unknown" or "not publicly disclosed" explicitly.
- Avoid repeating information that already appears in prior sections.
- Provide sources as direct URLs to the exact pages/documents where the claim/number appears (avoid homepages).
- Focus on budget, timeline, implementation status, stakeholders, and measurable outcomes.
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
  "content": "Markdown narrative that supports the facts and datasets",
  "facts": [
    { "key": "total_budget_usd", "label": "Total budget", "value": "$12.4B", "unit": "USD", "period": "2024-2030", "sources": ["https://..."] },
    { "key": "completion_pct", "label": "Completion", "value": "45%%", "unit": "percent", "period": "2025", "sources": ["https://..."] }
  ],
  "structured": { "optional_extra": "avoid raw key/value dumps; prefer facts above" },
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
- facts[].label MUST be human-readable (no underscores). Use facts for key numbers and short descriptors shown to users.
- facts[].value MUST be human-formatted (never a bare number). Example: "$12.4B", "45%%", "Phase 2 of 4".
- facts[].unit MUST be set when the value is numeric, using a consistent vocabulary: "USD"|"EUR"|"GBP"|"percent"|"count"|"year"|... If you cannot determine the unit from sources, explicitly say so.
`, asOf, string(entityJSON), string(priorJSON), spec.Key, spec.Title, spec.Description, spec.Key, spec.Order)
}

func (g *Generator) buildProgramDashboardPrompt(
	fragment *models.HorizonFragment,
	graphContext *models.GraphContextSummary,
	sections map[string]models.ProfileSection,
) string {
	asOf := time.Now().UTC().Format("2006-01-02")
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
			"key":            s.Key,
			"title":          s.Title,
			"order":          s.Order,
			"confidence":     s.Confidence,
			"structured":     s.Structured,
			"datasets":       s.Datasets,
			"contentExcerpt": content,
			"sources":        s.Sources,
		})
	}
	sectionsJSON, _ := json.MarshalIndent(secSlim, "", "  ")

	return fmt.Sprintf(`You are synthesizing a high-grade PROGRAM DASHBOARD from the already-generated profile sections.

A PROGRAM is a structured initiative with defined objectives, timeline, budget, and deliverables.

IMPORTANT:
- As-of date: %s (UTC). Use this as the reference point for "current" data in your synthesis.
- Do NOT include the as-of date in any machine-generated identifiers/keys/titles. The system will display update timestamps separately.
- Use Google Search grounding ONLY if needed to validate key numbers; prefer using the provided sections to avoid contradictions.
- Do not introduce new major numbers unless you can cite a source URL.
- Provide decision-support blocks: thesis, KPI strip, scenarios with probabilities+triggers, and a watchlist of leading indicators.
- Focus on budget execution, timeline adherence, milestone completion, stakeholder dynamics, and outcomes.
- Output must conform to the JSON shape below. Types matter:
  - charts[].series[].points[].y MUST be a number.
  - charts[].series[].points[].meta MUST be a JSON object.
  - sources MUST be an array of strings.
  - scenarios[].metricItems[] MUST be used for scenario metrics; label MUST be human readable (no underscores).

ENTITY CONTEXT:
%s

SECTIONS (source-of-truth; avoid duplication and contradictions):
%s

REQUIRED OUTPUT FORMAT (JSON) - output ONLY valid JSON:
{
  "thesis": "5-8 sentence decision-grade summary focused on program objectives, progress, budget status, key risks, and near-term milestones",
  "kpis": [
    { "id": "budget", "label": "Total budget", "value": "$12.4B", "subtext": "2024-2030", "trendValue": "65%% committed", "severity": "neutral", "confidence": 0.0 },
    { "id": "completion", "label": "Completion", "value": "45%%", "subtext": "Phase 2 of 4", "trendValue": "On track", "severity": "positive", "confidence": 0.0 }
  ],
  "scenarios": [
    {
      "id": "on_track",
      "name": "On-track completion",
      "probability": 0.55,
      "assumptions": ["..."],
      "metricItems": [
        { "key": "completion_date", "label": "Completion date", "value": "Q4 2028", "unit": "date", "period": "" }
      ],
      "metrics": { "completion_date": "Q4 2028" },
      "triggers": ["..."]
    },
    {
      "id": "delayed",
      "name": "Significant delays",
      "probability": 0.30,
      "assumptions": ["..."],
      "metricItems": [
        { "key": "delay_months", "label": "Delay", "value": "12-18 months", "unit": "months", "period": "" }
      ],
      "metrics": { "delay_months": "12-18 months" },
      "triggers": ["..."]
    }
  ],
  "watchlist": [
    { "id": "budget_approval", "indicator": "Budget approval cycle", "current": "FY2026 approved", "threshold": "FY2027 pending review", "signal": "Funding continuity", "frequency": "annual", "sources": ["https://..."], "confidence": 0.0 },
    { "id": "milestone_deadline", "indicator": "Next major milestone", "current": "Phase 2 completion: Q2 2026", "threshold": "Milestone missed", "signal": "Timeline risk", "frequency": "quarterly", "sources": ["https://..."], "confidence": 0.0 }
  ],
  "charts": [
    {
      "id": "budget_execution",
      "title": "Budget execution (cumulative)",
      "type": "timeseries",
      "unit": "billion",
      "currency": "USD",
      "series": [ { "name": "Spent", "points": [ { "x": "2024", "y": 2.1 }, { "x": "2025", "y": 4.8 } ] } ],
      "sources": ["https://..."],
      "confidence": 0.0
    }
  ],
  "tables": [],
  "notes": "brief caveats and data gaps",
  "confidence": 0.0
}
`, asOf, string(entityJSON), string(sectionsJSON))
}

func (g *Generator) parseOrgSectionResult(parsed any) (*orgSectionResult, error) {
	b, err := json.Marshal(parsed)
	if err != nil {
		return nil, fmt.Errorf("marshal section parsed: %w", err)
	}
	var out orgSectionResult
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("unmarshal section: %w", err)
	}
	return &out, nil
}

func (g *Generator) parseOrgDashboardResult(parsed any) (*orgDashboardResult, error) {
	b, err := json.Marshal(parsed)
	if err != nil {
		return nil, fmt.Errorf("marshal dashboard parsed: %w", err)
	}
	var out orgDashboardResult
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("unmarshal dashboard: %w", err)
	}
	return &out, nil
}

func (g *Generator) parseGovSectionResult(parsed any) (*govSectionResult, error) {
	b, err := json.Marshal(parsed)
	if err != nil {
		return nil, fmt.Errorf("marshal section parsed: %w", err)
	}
	var out govSectionResult
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("unmarshal section: %w", err)
	}
	return &out, nil
}

func (g *Generator) parseGovDashboardResult(parsed any) (*govDashboardResult, error) {
	b, err := json.Marshal(parsed)
	if err != nil {
		return nil, fmt.Errorf("marshal dashboard parsed: %w", err)
	}
	var out govDashboardResult
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("unmarshal dashboard: %w", err)
	}
	return &out, nil
}

// ---------- DYNASTY parsing ----------

func (g *Generator) parseDynastySectionResult(parsed any) (*dynastySectionResult, error) {
	b, err := json.Marshal(parsed)
	if err != nil {
		return nil, fmt.Errorf("marshal section parsed: %w", err)
	}
	var out dynastySectionResult
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("unmarshal section: %w", err)
	}
	return &out, nil
}

func (g *Generator) parseDynastyDashboardResult(parsed any) (*dynastyDashboardResult, error) {
	b, err := json.Marshal(parsed)
	if err != nil {
		return nil, fmt.Errorf("marshal dashboard parsed: %w", err)
	}
	var out dynastyDashboardResult
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("unmarshal dashboard: %w", err)
	}
	return &out, nil
}

// ---------- TRIBE parsing ----------

func (g *Generator) parseTribeSectionResult(parsed any) (*tribeSectionResult, error) {
	b, err := json.Marshal(parsed)
	if err != nil {
		return nil, fmt.Errorf("marshal section parsed: %w", err)
	}
	var out tribeSectionResult
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("unmarshal section: %w", err)
	}
	return &out, nil
}

func (g *Generator) parseTribeDashboardResult(parsed any) (*tribeDashboardResult, error) {
	b, err := json.Marshal(parsed)
	if err != nil {
		return nil, fmt.Errorf("marshal dashboard parsed: %w", err)
	}
	var out tribeDashboardResult
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("unmarshal dashboard: %w", err)
	}
	return &out, nil
}

// ---------- PROGRAM parsing ----------

func (g *Generator) parseProgramSectionResult(parsed any) (*programSectionResult, error) {
	b, err := json.Marshal(parsed)
	if err != nil {
		return nil, fmt.Errorf("marshal section parsed: %w", err)
	}
	var out programSectionResult
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("unmarshal section: %w", err)
	}
	return &out, nil
}

func (g *Generator) parseProgramDashboardResult(parsed any) (*programDashboardResult, error) {
	b, err := json.Marshal(parsed)
	if err != nil {
		return nil, fmt.Errorf("marshal dashboard parsed: %w", err)
	}
	var out programDashboardResult
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
	asOf := time.Now().UTC().Format("2006-01-02")
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
- As-of date: %s (UTC). Use this as the reference point for "current" data in your grounded research.
- Do NOT include the as-of date in any machine-generated identifiers/keys/titles. The system will display update timestamps separately.
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
`, asOf, string(entityJSON), string(sectionsJSON))
}

// llmProfileResponse represents the expected LLM output structure.
type llmProfileResponse struct {
	Sections          map[string]llmSectionResponse `json:"sections"`
	OverallConfidence float64                       `json:"overallConfidence"`
	GenerationNotes   string                        `json:"generationNotes"`
}

type llmSectionResponse struct {
	Title      string                  `json:"title"`
	Content    string                  `json:"content"`
	Structured map[string]any          `json:"structured,omitempty"`
	Datasets   []models.ProfileDataset `json:"datasets,omitempty"`
	Sources    []string                `json:"sources,omitempty"`
	Confidence float64                 `json:"confidence"`
}

// parseResult parses the LLM result into a FragmentProfile.
func (g *Generator) parseResult(
	fragment *models.HorizonFragment,
	graphContext *models.GraphContextSummary,
	llmResult any,
) (*models.FragmentProfile, string, error) {
	// graphContext may be persisted in future profile versions; keep signature stable.
	_ = graphContext
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
