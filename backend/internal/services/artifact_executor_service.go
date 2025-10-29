package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
)

// ArtifactExecutorService handles AI generation for artifact sections
type ArtifactExecutorService struct {
	genaiClient *gen_combined.Client
	contextSvc  *ArtifactContextService
	publisher   *realtime.Publisher
}

// NewArtifactExecutorService creates a new executor service
func NewArtifactExecutorService(
	genaiClient *gen_combined.Client,
	contextSvc *ArtifactContextService,
	publisher *realtime.Publisher,
) *ArtifactExecutorService {
	return &ArtifactExecutorService{
		genaiClient: genaiClient,
		contextSvc:  contextSvc,
		publisher:   publisher,
	}
}

// SectionGenerationRequest contains parameters for generating a section
type SectionGenerationRequest struct {
	Section            models.ArtifactSection
	Context            *GatheredContext
	GenerationSettings models.GenerationSettings
	Model              string // Model alias (gemini_quality, gpt_fast, etc.)
	StreamCallback     func(chunk string) error
	SessionID          string // For WebSocket routing
	ArtifactID         string // For WebSocket routing
	TenantID           string // For WebSocket routing
	ProjectID          string // For WebSocket routing
	StreamID           string // For ordering and FE filtering
}

// SectionGenerationResult contains the result of section generation
type SectionGenerationResult struct {
	Content    string
	TokensUsed int
	Duration   time.Duration
	Model      string
	Quality    *SectionQuality
}

// SectionQuality contains quality metrics for a generated section
type SectionQuality struct {
	Overall      float64 // 0-1
	Coherence    float64 // 0-1
	Completeness float64 // 0-1
	Relevance    float64 // 0-1
}

// GenerateSection generates content for a single section
func (s *ArtifactExecutorService) GenerateSection(
	ctx context.Context,
	req SectionGenerationRequest,
) (*SectionGenerationResult, error) {
	startTime := time.Now()

	// Build prompt with context
	prompt := s.buildSectionPrompt(req.Section, req.Context, req.GenerationSettings)

	// Prepare GenAI request
	genReq := gen_combined.GenAIRequest{
		Model:             req.Model,
		Prompt:            prompt,
		SystemInstruction: req.GenerationSettings.SystemPrompt,
		Temperature:       0.7,
		MaxOutputTokens:   4096,
	}

	// Execute generation
	resp, err := s.genaiClient.GenAIGenerate(ctx, genReq)
	if err != nil {
		return nil, fmt.Errorf("failed to generate section: %w", err)
	}

	duration := time.Since(startTime)

	// Calculate quality if context is available
	var quality *SectionQuality
	if req.Context != nil {
		quality, _ = s.evaluateQuality(ctx, resp.Content, req.Section.Prompt, req.Context)
	}

	return &SectionGenerationResult{
		Content:    resp.Content,
		TokensUsed: resp.Usage.TotalTokens,
		Duration:   duration,
		Model:      resp.Model,
		Quality:    quality,
	}, nil
}

// GenerateSectionStreaming generates content with streaming callback
func (s *ArtifactExecutorService) GenerateSectionStreaming(
	ctx context.Context,
	req SectionGenerationRequest,
) (*SectionGenerationResult, error) {
	startTime := time.Now()

	// Publish section generation started event
	if s.publisher != nil && req.SessionID != "" {
		_ = s.publisher.PublishJSON(ctx, realtime.EventArtifactSectionGenerating, req.TenantID, map[string]interface{}{
			"sessionId":    req.SessionID,
			"artifactId":   req.ArtifactID,
			"sectionId":    req.Section.ID,
			"sectionTitle": req.Section.Title,
			"projectId":    req.ProjectID,
			"streamId":     req.StreamID,
		})
	}

	// Build prompt
	prompt := s.buildSectionPrompt(req.Section, req.Context, req.GenerationSettings)

	// Prepare options for streaming
	opts := gen_combined.Options{
		ModelOverride:     req.Model,
		SystemInstruction: req.GenerationSettings.SystemPrompt,
		Temperature:       0.7,
		MaxOutputTokens:   4096,
	}

	// Accumulate content
	var fullContent strings.Builder
	var totalTokens int

	// Stream generation
	err := s.genaiClient.Stream(ctx, prompt, nil, opts, func(evt gen_combined.StreamEvent) error {
		if evt.Type == "chunk" && evt.Text != "" {
			fullContent.WriteString(evt.Text)

			// Publish chunk event
			if s.publisher != nil && req.SessionID != "" {
				_ = s.publisher.PublishJSON(ctx, realtime.EventArtifactSectionChunk, req.TenantID, map[string]interface{}{
					"sessionId":  req.SessionID,
					"artifactId": req.ArtifactID,
					"sectionId":  req.Section.ID,
					"chunk":      evt.Text,
					"projectId":  req.ProjectID,
					"streamId":   req.StreamID,
				})
			}

			if req.StreamCallback != nil {
				if err := req.StreamCallback(evt.Text); err != nil {
					return err
				}
			}
		}
		if evt.Type == "done" {
			totalTokens = evt.Usage.TotalTokens
		}
		return nil
	})

	if err != nil {
		// Publish error event
		if s.publisher != nil && req.SessionID != "" {
			_ = s.publisher.PublishJSON(ctx, realtime.EventArtifactGenerationError, req.TenantID, map[string]interface{}{
				"sessionId":  req.SessionID,
				"artifactId": req.ArtifactID,
				"sectionId":  req.Section.ID,
				"error":      err.Error(),
				"projectId":  req.ProjectID,
				"streamId":   req.StreamID,
			})
		}
		return nil, fmt.Errorf("streaming generation failed: %w", err)
	}

	duration := time.Since(startTime)
	content := fullContent.String()

	// Calculate quality
	var quality *SectionQuality
	if req.Context != nil {
		quality, _ = s.evaluateQuality(ctx, content, req.Section.Prompt, req.Context)
	}

	result := &SectionGenerationResult{
		Content:    content,
		TokensUsed: totalTokens,
		Duration:   duration,
		Model:      req.Model,
		Quality:    quality,
	}

	// Publish section completed event
	if s.publisher != nil && req.SessionID != "" {
		_ = s.publisher.PublishJSON(ctx, realtime.EventArtifactSectionCompleted, req.TenantID, map[string]interface{}{
			"sessionId":  req.SessionID,
			"artifactId": req.ArtifactID,
			"sectionId":  req.Section.ID,
			"tokensUsed": totalTokens,
			"durationMs": duration.Milliseconds(),
			"quality":    quality,
			"projectId":  req.ProjectID,
			"streamId":   req.StreamID,
		})
	}

	return result, nil
}

// buildSectionPrompt constructs the full prompt for section generation
func (s *ArtifactExecutorService) buildSectionPrompt(
	section models.ArtifactSection,
	context *GatheredContext,
	settings models.GenerationSettings,
) string {
	var builder strings.Builder

	// Section instructions
	builder.WriteString("Generate content for the following section:\n\n")
	builder.WriteString(fmt.Sprintf("**Section Title:** %s\n\n", section.Title))

	if section.Description != nil && *section.Description != "" {
		builder.WriteString(fmt.Sprintf("**Description:** %s\n\n", *section.Description))
	}

	builder.WriteString(fmt.Sprintf("**Instructions:** %s\n\n", section.Prompt))

	// Add generation settings context
	if settings.Tone != nil && *settings.Tone != "" {
		builder.WriteString(fmt.Sprintf("**Tone:** %s\n", *settings.Tone))
	}
	if settings.Audience != nil && *settings.Audience != "" {
		builder.WriteString(fmt.Sprintf("**Target Audience:** %s\n", *settings.Audience))
	}
	if settings.DetailLevel != nil && *settings.DetailLevel != "" {
		builder.WriteString(fmt.Sprintf("**Detail Level:** %s\n", *settings.DetailLevel))
	}
	builder.WriteString(fmt.Sprintf("**Language:** %s\n\n", settings.PreferredLanguage))

	// Add context if available
	if context != nil {
		builder.WriteString("---\n\n**Context for Generation:**\n\n")

		if context.ConversationText != "" {
			builder.WriteString("**Conversation History:**\n")
			builder.WriteString(context.ConversationText)
			builder.WriteString("\n\n")
		}

		if len(context.DocumentTexts) > 0 {
			builder.WriteString("**Reference Documents:**\n\n")
			for _, doc := range context.DocumentTexts {
				builder.WriteString(fmt.Sprintf("### %s\n\n", doc.Title))
				// Truncate if too long
				text := doc.Text
				if len(text) > 5000 {
					text = text[:5000] + "...\n[Document truncated]"
				}
				builder.WriteString(text)
				builder.WriteString("\n\n")
			}
		}

		if len(context.PinnedMessages) > 0 {
			builder.WriteString("**Key Points (Pinned Messages):**\n\n")
			for i, msg := range context.PinnedMessages {
				builder.WriteString(fmt.Sprintf("%d. %s\n", i+1, msg))
			}
			builder.WriteString("\n")
		}

		if len(context.SearchResults) > 0 {
			builder.WriteString("**Search Results:**\n\n")
			for _, result := range context.SearchResults {
				builder.WriteString(fmt.Sprintf("- %s (Source: %s)\n", result.Text, result.Source))
			}
			builder.WriteString("\n")
		}
	}

	builder.WriteString("---\n\n")
	builder.WriteString("Generate the section content now. Write in a clear, professional manner appropriate for the specified audience and tone.")

	return builder.String()
}

// evaluateQuality evaluates the quality of generated content
func (s *ArtifactExecutorService) evaluateQuality(
	ctx context.Context,
	content string,
	originalPrompt string,
	context *GatheredContext,
) (*SectionQuality, error) {
	// Build evaluation prompt
	evalPrompt := fmt.Sprintf(`Evaluate the quality of the following generated content on three dimensions:

		1. **Coherence** (0-1): How well-structured and logically organized is the text?
		2. **Completeness** (0-1): How thoroughly does it address the requirements?
		3. **Relevance** (0-1): How relevant is it to the provided context and instructions?

		**Original Instructions:**
		%s

		**Generated Content:**
		%s

		Return a JSON object with scores:
		{
		"coherence": 0.0-1.0,
		"completeness": 0.0-1.0,
		"relevance": 0.0-1.0
}`, originalPrompt, content)

	// Use gemini_fast for quick evaluation
	genReq := gen_combined.GenAIRequest{
		Model:             "gemini_fast",
		Prompt:            evalPrompt,
		MaxOutputTokens:   256,
		StrictJSONEnabled: true,
		JSONSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"coherence":    map[string]interface{}{"type": "number", "minimum": 0, "maximum": 1},
				"completeness": map[string]interface{}{"type": "number", "minimum": 0, "maximum": 1},
				"relevance":    map[string]interface{}{"type": "number", "minimum": 0, "maximum": 1},
			},
			"required": []string{"coherence", "completeness", "relevance"},
		},
	}

	resp, err := s.genaiClient.GenAIGenerate(ctx, genReq)
	if err != nil {
		// Quality evaluation is optional, don't fail the whole generation
		return nil, err
	}

	// Parse quality scores
	var scores struct {
		Coherence    float64 `json:"coherence"`
		Completeness float64 `json:"completeness"`
		Relevance    float64 `json:"relevance"`
	}

	// Try to parse JSON from content
	content = strings.TrimSpace(resp.Content)
	if err := parseJSON(content, &scores); err != nil {
		return nil, err
	}

	overall := (scores.Coherence + scores.Completeness + scores.Relevance) / 3.0

	return &SectionQuality{
		Overall:      overall,
		Coherence:    scores.Coherence,
		Completeness: scores.Completeness,
		Relevance:    scores.Relevance,
	}, nil
}

// AssembleDocument uses orchestrator model to create final assembled document
func (s *ArtifactExecutorService) AssembleDocument(
	ctx context.Context,
	sections []models.ArtifactSection,
	config models.ArtifactConfig,
) (string, error) {
	// Build assembly prompt
	var builder strings.Builder

	builder.WriteString("Assemble the following sections into a cohesive, well-formatted document.\n\n")
	builder.WriteString(fmt.Sprintf("**Document Format:** %s\n", config.Format))
	builder.WriteString(fmt.Sprintf("**Document Focus:** %s\n\n", config.Focus))

	if config.GenerationSettings.OrchestratorSystemPrompt != nil {
		builder.WriteString(fmt.Sprintf("**Additional Instructions:** %s\n\n", *config.GenerationSettings.OrchestratorSystemPrompt))
	}

	builder.WriteString("**Sections to Assemble:**\n\n")

	for _, section := range sections {
		builder.WriteString(fmt.Sprintf("### %s\n\n", section.Title))
		if section.Content != nil {
			builder.WriteString(*section.Content)
		} else {
			builder.WriteString("[Section not generated]")
		}
		builder.WriteString("\n\n")
	}

	builder.WriteString("---\n\n")
	builder.WriteString("Create a final, polished document that:\n")
	builder.WriteString("1. Maintains consistency in tone and style throughout\n")
	builder.WriteString("2. Ensures smooth transitions between sections\n")
	builder.WriteString("3. Removes any redundancy or contradictions\n")
	builder.WriteString("4. Applies appropriate formatting for the document type\n")
	builder.WriteString("5. Adds a brief introduction and conclusion if appropriate\n\n")
	builder.WriteString("Return the complete assembled document.")

	// Use orchestrator model
	genReq := gen_combined.GenAIRequest{
		Model:             config.OrchestratorModel,
		Prompt:            builder.String(),
		SystemInstruction: config.GenerationSettings.SystemPrompt,
		Temperature:       0.5,
		MaxOutputTokens:   8192,
	}

	resp, err := s.genaiClient.GenAIGenerate(ctx, genReq)
	if err != nil {
		return "", fmt.Errorf("failed to assemble document: %w", err)
	}

	return resp.Content, nil
}

// Helper function to parse JSON
func parseJSON(content string, target interface{}) error {
	// Remove markdown code fences if present
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	return json.Unmarshal([]byte(content), target)
}
