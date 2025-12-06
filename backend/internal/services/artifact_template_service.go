package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/artifacts/templates"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// ArtifactTemplateService manages artifact templates
type ArtifactTemplateService struct {
	repo         *repo.ArtifactTemplatesRepository
	coreDocsRepo *repo.CoreDocsRepo
	aiExecutor   *ai.AITaskExecutor
}

// NewArtifactTemplateService creates a new template service
func NewArtifactTemplateService(
	repo *repo.ArtifactTemplatesRepository,
	coreDocsRepo *repo.CoreDocsRepo,
	aiExecutor *ai.AITaskExecutor,
) *ArtifactTemplateService {
	return &ArtifactTemplateService{
		repo:         repo,
		coreDocsRepo: coreDocsRepo,
		aiExecutor:   aiExecutor,
	}
}

// GetTemplate retrieves a template by ID (system or custom)
// For custom templates, tenantID is required to ensure access control
func (s *ArtifactTemplateService) GetTemplate(ctx context.Context, id string, tenantID *string) (*models.ArtifactTemplate, error) {
	// First check system templates
	if template, err := templates.GetSystemTemplate(id); err == nil {
		return template, nil
	}

	// If not found in system templates, check Firestore
	template, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// For custom templates, verify tenant access
	if template.Type == "custom" {
		if template.TenantID == nil {
			// Custom template without tenantID - should not happen, but deny access
			return nil, errors.New("template not found")
		}
		if tenantID == nil || *template.TenantID != *tenantID {
			return nil, errors.New("template not found")
		}
	}

	return template, nil
}

// ListAvailableTemplates returns all templates available to a tenant
func (s *ArtifactTemplateService) ListAvailableTemplates(
	ctx context.Context,
	tenantID string,
	category *string,
) ([]*models.ArtifactTemplate, error) {
	result := make([]*models.ArtifactTemplate, 0)

	// 1. System templates
	var systemTemplates []*models.ArtifactTemplate
	if category != nil {
		systemTemplates = templates.ListSystemTemplatesByCategory(*category)
	} else {
		systemTemplates = templates.ListSystemTemplates()
	}
	result = append(result, systemTemplates...)

	// 2. Public (community) templates
	publicTemplates, err := s.repo.ListPublic(ctx, category, 0)
	if err == nil {
		result = append(result, publicTemplates...)
	}

	// 3. Tenant-specific templates
	tenantTemplates, err := s.repo.ListByTenant(ctx, tenantID)
	if err != nil {
		// Log error but don't fail - return what we have (system + public templates)
		// This allows the API to work even if tenant templates query fails (e.g., index not ready)
		fmt.Printf("[ArtifactTemplateService] Failed to list tenant templates for tenantID=%s: %v\n", tenantID, err)
	} else {
		// Filter by category if specified
		if category != nil {
			filtered := make([]*models.ArtifactTemplate, 0)
			for _, t := range tenantTemplates {
				if t.Category == *category {
					filtered = append(filtered, t)
				}
			}
			result = append(result, filtered...)
		} else {
			result = append(result, tenantTemplates...)
		}
	}

	return result, nil
}

// CreateCustomTemplate creates a new custom template
func (s *ArtifactTemplateService) CreateCustomTemplate(
	ctx context.Context,
	template *models.ArtifactTemplate,
	tenantID, userID string,
) (string, error) {
	template.CreatedBy = userID
	template.TenantID = &tenantID
	template.IsPublic = false
	template.Type = "custom"
	template.UsageCount = 0
	template.Rating = 0
	template.RatingCount = 0

	return s.repo.Create(ctx, template)
}

// PublishToCommunity publishes a template to the community
func (s *ArtifactTemplateService) PublishToCommunity(
	ctx context.Context,
	templateID, tenantID, userID string,
) error {
	template, err := s.repo.Get(ctx, templateID)
	if err != nil {
		return err
	}

	// System templates cannot be republished
	if template.Type == "system" {
		return errors.New("system templates are already public")
	}

	// Check ownership
	if template.CreatedBy != userID {
		return errors.New("unauthorized: only template creator can publish")
	}

	// For custom templates, verify tenant access
	if template.Type == "custom" {
		if template.TenantID == nil || *template.TenantID != tenantID {
			return errors.New("unauthorized")
		}
	}

	template.IsPublic = true
	template.Type = "community"
	// Remove tenantID when publishing to community (becomes global)
	template.TenantID = nil

	return s.repo.Update(ctx, template)
}

// UpdateTemplate updates an existing template (owner-only, tenant-scoped)
func (s *ArtifactTemplateService) UpdateTemplate(ctx context.Context, t *models.ArtifactTemplate, tenantID, userID string) error {
	if t == nil || t.ID == "" {
		return errors.New("invalid template")
	}
	existing, err := s.repo.Get(ctx, t.ID)
	if err != nil {
		return err
	}
	// System templates cannot be updated
	if existing.Type == "system" {
		return errors.New("system templates cannot be updated")
	}
	// Check ownership
	if existing.CreatedBy != userID {
		return errors.New("unauthorized")
	}
	// For custom templates, verify tenant access
	if existing.Type == "custom" {
		if existing.TenantID == nil || *existing.TenantID != tenantID {
			return errors.New("unauthorized")
		}
		// Ensure tenantID is preserved
		t.TenantID = existing.TenantID
	}
	// Preserve immutable fields
	t.CreatedBy = existing.CreatedBy
	t.CreatedAt = existing.CreatedAt
	t.UsageCount = existing.UsageCount
	t.Rating = existing.Rating
	t.RatingCount = existing.RatingCount
	t.Type = existing.Type // Preserve type
	return s.repo.Update(ctx, t)
}

// DeleteTemplate deletes a template (owner-only, tenant-scoped)
func (s *ArtifactTemplateService) DeleteTemplate(ctx context.Context, id, tenantID, userID string) error {
	existing, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	// System templates cannot be deleted
	if existing.Type == "system" {
		return errors.New("system templates cannot be deleted")
	}
	// Check ownership
	if existing.CreatedBy != userID {
		return errors.New("unauthorized")
	}
	// For custom templates, verify tenant access
	if existing.Type == "custom" {
		if existing.TenantID == nil || *existing.TenantID != tenantID {
			return errors.New("unauthorized")
		}
	}
	return s.repo.Delete(ctx, id)
}

// CreateFromArtifactRequest contains options for creating a template from an artifact
type CreateFromArtifactRequest struct {
	ArtifactID            string
	TemplateName          string
	Description           string
	Category              string
	IncludeExamples       bool
	IncludePromptHistory  bool
	IncludeSettings       bool
	Visibility            string // "private" | "team" | "public"
	SectionCustomizations map[string]SectionCustomization
}

// SectionCustomization allows customizing sections when creating template
type SectionCustomization struct {
	Include      bool
	NewTitle     *string
	NewPrompt    *string
	MakeRequired *bool
}

// CreateFromArtifact creates a template from an existing artifact
func (s *ArtifactTemplateService) CreateFromArtifact(
	ctx context.Context,
	req CreateFromArtifactRequest,
	tenantID, userID string,
) (string, error) {
	// 1. Load artifact
	artifact, err := s.coreDocsRepo.Get(ctx, req.ArtifactID)
	if err != nil {
		return "", fmt.Errorf("artifact not found: %w", err)
	}

	if artifact.ArtifactConfig == nil {
		return "", errors.New("not an artifact")
	}

	// 2. Check access
	if artifact.TenantID != tenantID {
		return "", errors.New("unauthorized")
	}

	// 3. Convert structure to template
	sectionTemplates := make([]models.SectionTemplate, 0)

	for _, section := range artifact.ArtifactConfig.Structure {
		// Skip sections not included
		if customization, ok := req.SectionCustomizations[section.ID]; ok && !customization.Include {
			continue
		}

		// Determine best prompt
		prompt := section.Prompt
		if req.IncludePromptHistory && len(section.PromptHistory) > 0 {
			// Find prompt with highest quality
			bestQuality := 0.0
			for _, ph := range section.PromptHistory {
				if ph.ResultQuality != nil && *ph.ResultQuality > bestQuality {
					bestQuality = *ph.ResultQuality
					prompt = ph.Prompt
				}
			}
		}

		// Apply customizations
		title := section.Title
		required := section.Required

		if customization, ok := req.SectionCustomizations[section.ID]; ok {
			if customization.NewTitle != nil {
				title = *customization.NewTitle
			}
			if customization.NewPrompt != nil {
				prompt = *customization.NewPrompt
			}
			if customization.MakeRequired != nil {
				required = *customization.MakeRequired
			}
		}

		// Add example content if requested
		var exampleContent *string
		if req.IncludeExamples && section.Content != nil {
			example := *section.Content
			if len(example) > 500 {
				example = example[:500] + "..."
			}
			exampleContent = &example
		}

		sectionTemplate := models.SectionTemplate{
			ID:             section.ID,
			Title:          title,
			Order:          section.Order,
			Description:    getStringOrDefault(section.Description, ""),
			PromptTemplate: prompt,
			Required:       required,
			Editable:       section.Editable,
			DependsOn:      section.DependsOn,
			ExampleContent: exampleContent,
		}

		sectionTemplates = append(sectionTemplates, sectionTemplate)
	}

	// 4. Create system prompts
	systemPrompts := make(map[string]string)
	if req.IncludeSettings {
		lang := artifact.ArtifactConfig.GenerationSettings.PreferredLanguage
		systemPrompts[lang] = artifact.ArtifactConfig.GenerationSettings.SystemPrompt
	}

	// 5. Determine visibility
	isPublic := false
	templateType := "custom"
	var templateTenantID *string

	switch req.Visibility {
	case "private":
		templateTenantID = &tenantID
		isPublic = false
	case "team":
		templateTenantID = &tenantID
		isPublic = false
	case "public":
		templateTenantID = nil
		isPublic = true
		templateType = "community"
	default:
		templateTenantID = &tenantID
		isPublic = false
	}

	// 6. Create template
	template := &models.ArtifactTemplate{
		Name:                         req.TemplateName,
		Description:                  req.Description,
		Category:                     req.Category,
		Type:                         templateType,
		RecommendedFormat:            artifact.ArtifactConfig.Format,
		RecommendedFocus:             artifact.ArtifactConfig.Focus,
		RecommendedOrchestratorModel: artifact.ArtifactConfig.OrchestratorModel,
		RecommendedSectionModel:      artifact.ArtifactConfig.DefaultModel,
		DefaultStructure:             sectionTemplates,
		SystemPrompts:                systemPrompts,
		CreatedBy:                    userID,
		TenantID:                     templateTenantID,
		IsPublic:                     isPublic,
		IsFeatured:                   false,
		UsageCount:                   0,
		Rating:                       0,
		RatingCount:                  0,
		SourceArtifactID:             &req.ArtifactID,
		Tags:                         extractTags(req.TemplateName, req.Description),
	}

	return s.repo.Create(ctx, template)
}

// StructureRequirements defines requirements for AI structure generation
type StructureRequirements struct {
	DocumentType string // "proposal", "brief", "report", etc.
	Topic        string
	Audience     string // "technical", "executive", "general"
	DetailLevel  string // "brief", "moderate", "detailed"
	Language     string
}

// GenerateStructure uses AI to generate a document structure
func (s *ArtifactTemplateService) GenerateStructure(
	ctx context.Context,
	requirements StructureRequirements,
) ([]models.SectionTemplate, error) {
	// Build prompt for AI
	prompt := fmt.Sprintf(`Generate a document structure for the following requirements:

Document Type: %s
Topic: %s
Target Audience: %s
Detail Level: %s
Language: %s

Return a JSON array of sections. Each section should have:
- id: unique identifier (lowercase, underscores)
- title: section title
- order: numeric order (starting from 1)
- description: what this section should contain
- promptTemplate: detailed prompt for generating this section's content
- required: boolean indicating if section is required
- editable: boolean indicating if user can edit/remove

The structure should be logical, comprehensive, and appropriate for the document type and audience.
Include 4-8 sections depending on the detail level.

Example format:
[
  {
    "id": "executive_summary",
    "title": "Executive Summary",
    "order": 1,
    "description": "High-level overview for decision makers",
    "promptTemplate": "Provide a concise executive summary...",
    "required": true,
    "editable": false
  }
]`,
		requirements.DocumentType,
		requirements.Topic,
		requirements.Audience,
		requirements.DetailLevel,
		requirements.Language,
	)

	// Create verifier for structure generation
	verifier := &structureVerifier{}

	// Execute AI task
	task := &ai.AITask{
		TaskType:     ai.TaskTypeTextProcessing,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			MinContextWindow:   8000,
			MinCapabilityScore: 80,
		},
		Verifier:        verifier,
		EnableTelemetry: true,
	}

	result, err := s.aiExecutor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("failed to generate structure: %w", err)
	}

	// Type assert result
	sections, ok := result.([]models.SectionTemplate)
	if !ok {
		return nil, fmt.Errorf("unexpected result type from structure generation")
	}

	return sections, nil
}

// structureVerifier verifies and parses structure generation results
type structureVerifier struct{}

func (v *structureVerifier) VerifyAndParse(jsonStr string) (interface{}, error) {
	var sections []models.SectionTemplate
	if err := json.Unmarshal([]byte(jsonStr), &sections); err != nil {
		return nil, fmt.Errorf("failed to parse sections: %w", err)
	}

	// Validate sections
	if len(sections) == 0 {
		return nil, errors.New("no sections generated")
	}

	for i, section := range sections {
		if section.ID == "" {
			return nil, fmt.Errorf("section %d missing ID", i)
		}
		if section.Title == "" {
			return nil, fmt.Errorf("section %s missing title", section.ID)
		}
		if section.PromptTemplate == "" {
			return nil, fmt.Errorf("section %s missing prompt template", section.ID)
		}
	}

	return sections, nil
}

// Helper functions

func getStringOrDefault(ptr *string, defaultValue string) string {
	if ptr != nil {
		return *ptr
	}
	return defaultValue
}

func extractTags(name, description string) []string {
	tags := make([]string, 0)

	text := strings.ToLower(name + " " + description)

	keywords := []string{
		"proposal", "brief", "report", "summary", "contract",
		"technical", "business", "legal", "research",
		"meeting", "api", "design", "architecture",
		"executive", "analysis", "documentation",
	}

	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			tags = append(tags, keyword)
		}
	}

	return tags
}
