package services

import (
	"context"
	"fmt"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// ArtifactStorageSaver interface for saving artifacts to storage
type ArtifactStorageSaver interface {
	SaveArtifactToStorage(ctx context.Context, tenantID, projectID string, artifact *models.CoreDoc) error
}

// ArtifactOrchestratorService coordinates artifact generation process
type ArtifactOrchestratorService struct {
	sessionsRepo *repo.GenerationSessionsRepository
	coreDocsRepo *repo.CoreDocsRepo
	executor     *ArtifactExecutorService
	contextSvc   *ArtifactContextService
	storageSaver ArtifactStorageSaver // optional storage saver
}

// NewArtifactOrchestratorService creates a new orchestrator service
func NewArtifactOrchestratorService(
	sessionsRepo *repo.GenerationSessionsRepository,
	coreDocsRepo *repo.CoreDocsRepo,
	executor *ArtifactExecutorService,
	contextSvc *ArtifactContextService,
) *ArtifactOrchestratorService {
	return &ArtifactOrchestratorService{
		sessionsRepo: sessionsRepo,
		coreDocsRepo: coreDocsRepo,
		executor:     executor,
		contextSvc:   contextSvc,
	}
}

// WithStorageSaver sets the storage saver for artifact updates
func (s *ArtifactOrchestratorService) WithStorageSaver(saver ArtifactStorageSaver) *ArtifactOrchestratorService {
	s.storageSaver = saver
	return s
}

// StartGenerationRequest contains parameters for starting generation
type StartGenerationRequest struct {
	ArtifactID string
	Mode       string // "auto" | "interactive"
	TenantID   string
	ProjectID  string
}

// StartGeneration creates a new generation session
func (s *ArtifactOrchestratorService) StartGeneration(
	ctx context.Context,
	req StartGenerationRequest,
) (*models.GenerationSession, error) {
	// Load artifact
	artifact, err := s.coreDocsRepo.Get(ctx, req.ArtifactID)
	if err != nil {
		return nil, fmt.Errorf("failed to load artifact: %w", err)
	}

	if artifact.ArtifactConfig == nil {
		return nil, fmt.Errorf("not an artifact")
	}

	// Check for active session
	activeSession, err := s.sessionsRepo.GetActive(ctx, req.ArtifactID)
	if err != nil {
		return nil, fmt.Errorf("failed to check active session: %w", err)
	}
	if activeSession != nil {
		return nil, fmt.Errorf("generation already in progress (session: %s)", activeSession.ID)
	}

	// Create new session
	session := &models.GenerationSession{
		ArtifactID:        req.ArtifactID,
		Mode:              req.Mode,
		Status:            "pending",
		PreferredLanguage: artifact.ArtifactConfig.GenerationSettings.PreferredLanguage,
		SystemPrompt:      artifact.ArtifactConfig.GenerationSettings.SystemPrompt,
		CompletedSections: []string{},
		Progress:          0.0,
		SectionResults:    make(map[string]models.SectionResult),
		Errors:            []models.GenerationError{},
	}

	sessionID, err := s.sessionsRepo.Create(ctx, session)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	session.ID = sessionID

	// If auto mode, start generation immediately
	if req.Mode == "auto" {
		go s.runAutoGeneration(context.Background(), sessionID, req.TenantID, req.ProjectID)
	}

	return session, nil
}

// GenerateSectionRequest contains parameters for generating a section
type GenerateSectionRequest struct {
	SessionID      string
	SectionID      string
	TenantID       string
	ProjectID      string
	UserID         string // For usage tracking
	PromptOverride *string
	ModelOverride  *string
	StreamCallback func(chunk string) error
	StreamID       string
}

// GenerateSection generates a specific section
func (s *ArtifactOrchestratorService) GenerateSection(
	ctx context.Context,
	req GenerateSectionRequest,
) (*models.SectionResult, error) {
	// Load session
	session, err := s.sessionsRepo.Get(ctx, req.SessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to load session: %w", err)
	}

	// Load artifact
	artifact, err := s.coreDocsRepo.Get(ctx, session.ArtifactID)
	if err != nil {
		return nil, fmt.Errorf("failed to load artifact: %w", err)
	}

	// Find section
	var section *models.ArtifactSection
	for i := range artifact.ArtifactConfig.Structure {
		if artifact.ArtifactConfig.Structure[i].ID == req.SectionID {
			section = &artifact.ArtifactConfig.Structure[i]
			break
		}
	}
	if section == nil {
		return nil, fmt.Errorf("section not found: %s", req.SectionID)
	}

	// Apply overrides
	if req.PromptOverride != nil {
		section.Prompt = *req.PromptOverride
	}

	model := artifact.ArtifactConfig.DefaultModel
	if section.Model != nil {
		model = *section.Model
	}
	if req.ModelOverride != nil {
		model = *req.ModelOverride
	}

	// Gather context
	gatheredContext, err := s.contextSvc.GatherContext(ctx, artifact.TenantID, req.ProjectID, artifact.ArtifactConfig.ContextSources)
	if err != nil {
		// Log but don't fail - generation can proceed without context
		fmt.Printf("Warning: failed to gather context: %v\n", err)
	}

	// Update session status
	_ = s.sessionsRepo.UpdateFields(ctx, req.SessionID, map[string]interface{}{
		"status":         "running",
		"currentSection": req.SectionID,
	})

	// Generate section
	genReq := SectionGenerationRequest{
		Section:            *section,
		Context:            gatheredContext,
		GenerationSettings: artifact.ArtifactConfig.GenerationSettings,
		Model:              model,
		StreamCallback:     req.StreamCallback,
		SessionID:          req.SessionID,
		ArtifactID:         session.ArtifactID,
		TenantID:           req.TenantID,
		ProjectID:          req.ProjectID,
		UserID:             req.UserID,
		StreamID:           req.StreamID,
		FullStructure:      artifact.ArtifactConfig.Structure,
		UserRequest:        artifact.ArtifactConfig.UserRequest,
	}

	var genResult *SectionGenerationResult
	if req.StreamCallback != nil {
		genResult, err = s.executor.GenerateSectionStreaming(ctx, genReq)
	} else {
		genResult, err = s.executor.GenerateSection(ctx, genReq)
	}

	if err != nil {
		// Record error
		genErr := models.GenerationError{
			SectionID: &req.SectionID,
			Message:   err.Error(),
			Timestamp: time.Now().UTC(),
		}
		_ = s.sessionsRepo.AddError(ctx, req.SessionID, genErr)
		return nil, fmt.Errorf("section generation failed: %w", err)
	}

	// Create section result
	sectionResult := models.SectionResult{
		SectionID:  req.SectionID,
		Content:    genResult.Content,
		Model:      genResult.Model,
		TokensUsed: genResult.TokensUsed,
		Duration:   genResult.Duration,
		Status:     "completed",
	}

	if genResult.Quality != nil {
		sectionResult.Quality = &genResult.Quality.Overall
	}

	// Save result to session
	_ = s.sessionsRepo.AddSectionResult(ctx, req.SessionID, req.SectionID, sectionResult)

	// Update artifact section content
	for i := range artifact.ArtifactConfig.Structure {
		if artifact.ArtifactConfig.Structure[i].ID == req.SectionID {
			now := time.Now().UTC()
			artifact.ArtifactConfig.Structure[i].Content = &genResult.Content
			artifact.ArtifactConfig.Structure[i].GeneratedAt = &now
			artifact.ArtifactConfig.Structure[i].TokensUsed = &genResult.TokensUsed
			break
		}
	}
	_ = s.coreDocsRepo.Upsert(ctx, artifact)

	// Save assembled document to storage if saver is configured
	if s.storageSaver != nil {
		_ = s.storageSaver.SaveArtifactToStorage(ctx, req.TenantID, req.ProjectID, artifact)
	}

	// Update progress
	completed := len(session.SectionResults) + 1
	total := len(artifact.ArtifactConfig.Structure)
	progress := float64(completed) / float64(total)
	_ = s.sessionsRepo.UpdateProgress(ctx, req.SessionID, progress, nil)

	return &sectionResult, nil
}

// AssembleDocument assembles the final document from generated sections
func (s *ArtifactOrchestratorService) AssembleDocument(
	ctx context.Context,
	sessionID string,
) (string, error) {
	// Load session
	session, err := s.sessionsRepo.Get(ctx, sessionID)
	if err != nil {
		return "", fmt.Errorf("failed to load session: %w", err)
	}

	// Load artifact
	artifact, err := s.coreDocsRepo.Get(ctx, session.ArtifactID)
	if err != nil {
		return "", fmt.Errorf("failed to load artifact: %w", err)
	}

	// Assemble using executor
	assembled, err := s.executor.AssembleDocument(ctx, artifact.ArtifactConfig.Structure, *artifact.ArtifactConfig)
	if err != nil {
		return "", fmt.Errorf("assembly failed: %w", err)
	}

	return assembled, nil
}

// GetSession returns generation session by ID
func (s *ArtifactOrchestratorService) GetSession(ctx context.Context, sessionID string) (*models.GenerationSession, error) {
	return s.sessionsRepo.Get(ctx, sessionID)
}

// RestoreVersion sets artifact content to a specified version snapshot and saves new version with trigger "restore"
func (s *ArtifactOrchestratorService) RestoreVersion(ctx context.Context, artifactID string, versionID string) error {
	artifact, err := s.coreDocsRepo.Get(ctx, artifactID)
	if err != nil {
		return fmt.Errorf("failed to load artifact: %w", err)
	}
	if artifact.ArtifactConfig == nil {
		return fmt.Errorf("not an artifact")
	}
	var target *models.ArtifactVersion
	for i := range artifact.ArtifactConfig.Versions {
		if artifact.ArtifactConfig.Versions[i].ID == versionID {
			target = &artifact.ArtifactConfig.Versions[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("version not found: %s", versionID)
	}
	// Restore section contents
	for i := range artifact.ArtifactConfig.Structure {
		sec := &artifact.ArtifactConfig.Structure[i]
		if snap, ok := target.Sections[sec.ID]; ok {
			sec.Content = &snap.Content
			t := snap.GeneratedAt
			sec.GeneratedAt = &t
			sec.TokensUsed = &snap.TokensUsed
		}
	}
	if err := s.coreDocsRepo.Upsert(ctx, artifact); err != nil {
		return err
	}
	// Append a new version noting the restore action
	restored := models.ArtifactVersion{
		ID:                fmt.Sprintf("v%d", len(artifact.ArtifactConfig.Versions)+1),
		Timestamp:         time.Now().UTC(),
		Trigger:           "restore",
		FullDocument:      target.FullDocument,
		Sections:          target.Sections,
		ContextSnapshot:   target.ContextSnapshot,
		GenerationMeta:    target.GenerationMeta,
		GenerationMode:    target.GenerationMode,
		GenerationParams:  target.GenerationParams,
		StructureSnapshot: target.StructureSnapshot,
	}
	artifact.ArtifactConfig.Versions = append(artifact.ArtifactConfig.Versions, restored)
	return s.coreDocsRepo.Upsert(ctx, artifact)
}

// SaveVersion saves the current state as a new version
func (s *ArtifactOrchestratorService) SaveVersion(
	ctx context.Context,
	sessionID string,
	trigger string,
) (*models.ArtifactVersion, error) {
	// Load session
	session, err := s.sessionsRepo.Get(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to load session: %w", err)
	}

	// Load artifact
	artifact, err := s.coreDocsRepo.Get(ctx, session.ArtifactID)
	if err != nil {
		return nil, fmt.Errorf("failed to load artifact: %w", err)
	}

	// Assemble document
	fullDocument, err := s.AssembleDocument(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to assemble document: %w", err)
	}

	// Create version
	version := models.ArtifactVersion{
		ID:           fmt.Sprintf("v%d", len(artifact.ArtifactConfig.Versions)+1),
		Timestamp:    time.Now().UTC(),
		Trigger:      trigger,
		FullDocument: fullDocument,
		Sections:     make(map[string]models.ArtifactSectionSnapshot),
		ContextSnapshot: s.contextSvc.createSnapshot(
			artifact.ArtifactConfig.ContextSources,
			&GatheredContext{}, // Simplified - would need actual context
		),
		GenerationMeta: models.ArtifactGenerationMeta{
			TotalTokensUsed:   0, // Would calculate from session results
			DurationMs:        0,
			SectionsGenerated: len(session.SectionResults),
			Errors:            []string{},
		},
		GenerationMode: session.Mode,
		GenerationParams: models.GenerationParams{
			PreferredLanguage: session.PreferredLanguage,
			SystemPrompt:      session.SystemPrompt,
			OrchestratorModel: artifact.ArtifactConfig.OrchestratorModel,
			SectionModels:     make(map[string]string),
		},
		StructureSnapshot: artifact.ArtifactConfig.Structure,
	}

	// Add section snapshots
	for _, section := range artifact.ArtifactConfig.Structure {
		if section.Content != nil {
			snapshot := models.ArtifactSectionSnapshot{
				Content:     *section.Content,
				Prompt:      section.Prompt,
				Model:       artifact.ArtifactConfig.DefaultModel,
				TokensUsed:  0,
				GeneratedAt: time.Now().UTC(),
			}
			if section.Model != nil {
				snapshot.Model = *section.Model
			}
			if section.TokensUsed != nil {
				snapshot.TokensUsed = *section.TokensUsed
			}
			version.Sections[section.ID] = snapshot
		}
	}

	// Add version to artifact
	artifact.ArtifactConfig.Versions = append([]models.ArtifactVersion{version}, artifact.ArtifactConfig.Versions...)
	artifact.ArtifactConfig.CurrentVersionID = version.ID

	// Trim old versions if needed
	maxVersions := artifact.ArtifactConfig.VersionsRetained
	if maxVersions == 0 {
		maxVersions = 10
	}
	if len(artifact.ArtifactConfig.Versions) > maxVersions {
		artifact.ArtifactConfig.Versions = artifact.ArtifactConfig.Versions[:maxVersions]
	}

	// Save artifact
	if err := s.coreDocsRepo.Upsert(ctx, artifact); err != nil {
		return nil, fmt.Errorf("failed to save version: %w", err)
	}

	// Save assembled document to storage if saver is configured
	if s.storageSaver != nil {
		_ = s.storageSaver.SaveArtifactToStorage(ctx, artifact.TenantID, artifact.ProjectID, artifact)
	}

	// Mark session as completed
	_ = s.sessionsRepo.MarkCompleted(ctx, sessionID)

	return &version, nil
}

// CancelGeneration cancels an in-progress generation
func (s *ArtifactOrchestratorService) CancelGeneration(ctx context.Context, sessionID string) error {
	return s.sessionsRepo.MarkCancelled(ctx, sessionID)
}

// runAutoGeneration runs automatic generation for all sections
func (s *ArtifactOrchestratorService) runAutoGeneration(ctx context.Context, sessionID, tenantID, projectID string) {
	// Load session
	session, err := s.sessionsRepo.Get(ctx, sessionID)
	if err != nil {
		_ = s.sessionsRepo.MarkFailed(ctx, sessionID, fmt.Sprintf("Failed to load session: %v", err))
		return
	}

	// Load artifact
	artifact, err := s.coreDocsRepo.Get(ctx, session.ArtifactID)
	if err != nil {
		_ = s.sessionsRepo.MarkFailed(ctx, sessionID, fmt.Sprintf("Failed to load artifact: %v", err))
		return
	}

	// Generate each section
	for _, section := range artifact.ArtifactConfig.Structure {
		// Check if session was cancelled
		currentSession, _ := s.sessionsRepo.Get(ctx, sessionID)
		if currentSession != nil && currentSession.Status == "cancelled" {
			return
		}

		// Generate section
		_, err := s.GenerateSection(ctx, GenerateSectionRequest{
			SessionID: sessionID,
			SectionID: section.ID,
			TenantID:  tenantID,
			ProjectID: projectID,
			UserID:    "", // Auto-generation doesn't have user context
		})

		if err != nil {
			// Log error but continue with other sections
			fmt.Printf("Auto-generation error for section %s: %v\n", section.ID, err)
		}
	}

	// Save as version
	_, err = s.SaveVersion(ctx, sessionID, "auto")
	if err != nil {
		_ = s.sessionsRepo.MarkFailed(ctx, sessionID, fmt.Sprintf("Failed to save version: %v", err))
		return
	}
}
