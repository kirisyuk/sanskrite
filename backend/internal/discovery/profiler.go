package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/prompts"
)

// SourceProfiler handles AI-based source profiling
type SourceProfiler struct {
	executor *ai.AITaskExecutor
	logger   *log.Logger
}

// NewSourceProfiler creates a new source profiler
func NewSourceProfiler(executor *ai.AITaskExecutor, logger *log.Logger) *SourceProfiler {
	return &SourceProfiler{
		executor: executor,
		logger:   logger,
	}
}

// ProfileRequest contains the data needed for profiling
type ProfileRequest struct {
	SourceID      string
	ExtractedText string
	Language      string // "en", "ru", etc.
}

// ProfileSource performs AI-based analysis of a source and returns structured profile data
func (p *SourceProfiler) ProfileSource(ctx context.Context, req ProfileRequest) (*models.SourceProfileData, error) {
	if req.Language == "" {
		req.Language = "en"
	}

	// Format the profiling prompt
	promptText := prompts.Format(prompts.SourceProfilingPrompt, map[string]string{
		"sourceId":      req.SourceID,
		"extractedText": req.ExtractedText,
		"language":      req.Language,
	})

	// Create AI task
	task := &ai.AITask{
		TaskType:       "source_profiling",
		InputContent:   promptText,
		AIRequirements: ai.AIRequirements{TextGenerationCapability: true},
		Verifier:       &SourceProfileVerifier{},
	}

	// Execute structured task
	result, err := p.executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("AI profiling failed: %w", err)
	}

	// Type assertion
	profile, ok := result.(*models.SourceProfileData)
	if !ok {
		return nil, fmt.Errorf("unexpected result type: %T", result)
	}

	return profile, nil
}

// SourceProfileVerifier verifies and parses source profile JSON
type SourceProfileVerifier struct{}

// VerifyAndParse implements AITaskResultVerifier
func (v *SourceProfileVerifier) VerifyAndParse(rawJSON string) (interface{}, error) {
	var profile models.SourceProfileData

	// Use strict JSON decoder
	decoder := json.NewDecoder(bytes.NewReader([]byte(rawJSON)))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&profile); err != nil {
		return nil, fmt.Errorf("JSON decode failed: %w", err)
	}

	// Validate ranges
	if err := validateProfile(&profile); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	return &profile, nil
}

// validateProfile validates numeric ranges and constraints
func validateProfile(p *models.SourceProfileData) error {
	// Validate confidence scores [0..1]
	if p.EditorialStance.Confidence < 0 || p.EditorialStance.Confidence > 1 {
		return fmt.Errorf("editorialStance.confidence must be in [0, 1]")
	}
	if p.TrustScore < 0 || p.TrustScore > 1 {
		return fmt.Errorf("trustScore must be in [0, 1]")
	}
	if p.BehaviorPatterns.EventResponsiveness < 0 || p.BehaviorPatterns.EventResponsiveness > 1 {
		return fmt.Errorf("behaviorPatterns.eventResponsiveness must be in [0, 1]")
	}
	if p.BehaviorPatterns.ContentConsistency < 0 || p.BehaviorPatterns.ContentConsistency > 1 {
		return fmt.Errorf("behaviorPatterns.contentConsistency must be in [0, 1]")
	}
	if p.BehaviorPatterns.OriginalityScore < 0 || p.BehaviorPatterns.OriginalityScore > 1 {
		return fmt.Errorf("behaviorPatterns.originalityScore must be in [0, 1]")
	}

	// Validate topics shares sum <= 1
	if p.Topics != nil {
		totalShare := 0.0
		for _, topic := range p.Topics {
			if topic.Share < 0 || topic.Share > 1 {
				return fmt.Errorf("topic '%s' share must be in [0, 1]", topic.Name)
			}
			totalShare += topic.Share
		}
		if totalShare > 1.01 { // Allow small floating point error
			return fmt.Errorf("total topics share exceeds 1.0: %.2f", totalShare)
		}
	}

	// Validate editorial methods shares
	if p.EditorialMethods != nil {
		totalShare := 0.0
		for _, method := range p.EditorialMethods {
			if method.Share < 0 || method.Share > 1 {
				return fmt.Errorf("editorial method '%s' share must be in [0, 1]", method.Method)
			}
			totalShare += method.Share
		}
		if totalShare > 1.01 {
			return fmt.Errorf("total editorial methods share exceeds 1.0: %.2f", totalShare)
		}
	}

	// Validate goals confidence
	if p.Goals != nil {
		for _, goal := range p.Goals {
			if goal.Confidence < 0 || goal.Confidence > 1 {
				return fmt.Errorf("goal '%s' confidence must be in [0, 1]", goal.Goal)
			}
		}
	}

	// Validate goalAlignmentScore
	if p.GoalAlignmentScore != nil {
		if *p.GoalAlignmentScore < 0 || *p.GoalAlignmentScore > 1 {
			return fmt.Errorf("goalAlignmentScore must be in [0, 1]")
		}
	}

	// Validate emotional tone distribution
	if p.ContentPatterns != nil && p.ContentPatterns.EmotionalToneDistribution != nil {
		etd := p.ContentPatterns.EmotionalToneDistribution
		total := 0.0
		if etd.Neutral != nil {
			if *etd.Neutral < 0 || *etd.Neutral > 1 {
				return fmt.Errorf("emotionalTone.neutral must be in [0, 1]")
			}
			total += *etd.Neutral
		}
		if etd.Outrage != nil {
			if *etd.Outrage < 0 || *etd.Outrage > 1 {
				return fmt.Errorf("emotionalTone.outrage must be in [0, 1]")
			}
			total += *etd.Outrage
		}
		if etd.Fear != nil {
			if *etd.Fear < 0 || *etd.Fear > 1 {
				return fmt.Errorf("emotionalTone.fear must be in [0, 1]")
			}
			total += *etd.Fear
		}
		if etd.Hope != nil {
			if *etd.Hope < 0 || *etd.Hope > 1 {
				return fmt.Errorf("emotionalTone.hope must be in [0, 1]")
			}
			total += *etd.Hope
		}
		if total > 1.01 {
			return fmt.Errorf("total emotional tone distribution exceeds 1.0: %.2f", total)
		}
	}

	// Validate red flags scores
	if p.RedFlags != nil {
		for _, flag := range p.RedFlags {
			if flag.Score < 0 || flag.Score > 1 {
				return fmt.Errorf("redFlag '%s' score must be in [0, 1]", flag.Code)
			}
		}
	}

	// Validate coordinated sources scores
	if p.CoordinatedSources != nil {
		for _, cs := range p.CoordinatedSources {
			if cs.CoordinationScore < 0 || cs.CoordinationScore > 1 {
				return fmt.Errorf("coordinatedSource '%s' score must be in [0, 1]", cs.SourceID)
			}
		}
	}

	// Validate metrics
	if p.Metrics != nil {
		if p.Metrics.ClaimDensity != nil && (*p.Metrics.ClaimDensity < 0 || *p.Metrics.ClaimDensity > 1) {
			return fmt.Errorf("metrics.claimDensity must be in [0, 1]")
		}
		if p.Metrics.ExternalRefShare != nil && (*p.Metrics.ExternalRefShare < 0 || *p.Metrics.ExternalRefShare > 1) {
			return fmt.Errorf("metrics.externalRefShare must be in [0, 1]")
		}
		if p.Metrics.Polarization != nil && (*p.Metrics.Polarization < 0 || *p.Metrics.Polarization > 1) {
			return fmt.Errorf("metrics.polarization must be in [0, 1]")
		}
		if p.Metrics.CTAShare != nil && (*p.Metrics.CTAShare < 0 || *p.Metrics.CTAShare > 1) {
			return fmt.Errorf("metrics.ctaShare must be in [0, 1]")
		}
		if p.Metrics.PromoLinkShare != nil && (*p.Metrics.PromoLinkShare < 0 || *p.Metrics.PromoLinkShare > 1) {
			return fmt.Errorf("metrics.promoLinkShare must be in [0, 1]")
		}
		if p.Metrics.EvidenceQuality != nil && (*p.Metrics.EvidenceQuality < 0 || *p.Metrics.EvidenceQuality > 1) {
			return fmt.Errorf("metrics.evidenceQuality must be in [0, 1]")
		}
	}

	return nil
}
