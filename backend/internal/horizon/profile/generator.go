package profile

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
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

	// Build prompt
	prompt := g.buildPrompt(fragment, graphContext)

	// Execute LLM task
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
		Verifier: ai.NewSimpleJSONVerifier(),
	}

	llmResult, err := g.executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("LLM execution failed: %w", err)
	}

	// Parse result
	profile, err := g.parseResult(fragment, graphContext, llmResult)
	if err != nil {
		return nil, fmt.Errorf("failed to parse LLM result: %w", err)
	}

	// Set generation metadata
	profile.Generation.GeneratedAt = time.Now().UTC()
	profile.Generation.GraphContext = *graphContext
	profile.Generation.SchemaVersion = "v1"

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
	Sources    []string       `json:"sources,omitempty"`
	Confidence float64        `json:"confidence"`
}

// parseResult parses the LLM result into a FragmentProfile.
func (g *Generator) parseResult(
	fragment *models.HorizonFragment,
	graphContext *models.GraphContextSummary,
	llmResult any,
) (*models.FragmentProfile, error) {
	// Marshal and unmarshal to get consistent structure
	jsonData, err := json.Marshal(llmResult)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal LLM result: %w", err)
	}

	var response llmProfileResponse
	if err := json.Unmarshal(jsonData, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal LLM response: %w", err)
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
			Sources:    section.Sources,
			Confidence: section.Confidence,
			Order:      order,
		}
	}

	// Set generation metadata (partially; caller fills in the rest)
	profile.Generation = models.ProfileGeneration{
		GroundingUsed: true,
	}

	return profile, nil
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
