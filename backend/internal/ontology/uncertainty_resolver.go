package ontology

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// UncertaintyResolver автоматически разрешает неопределенности через LLM + web search
type UncertaintyResolver struct {
	uncertaintyRepo *repo.UncertaintyModelRepository
	claimsRepo      *repo.ClaimRepository
	eventsRepo      *repo.EventRepository
	genaiClient     *gen_combined.Client
	logger          *log.Logger
}

// NewUncertaintyResolver создает новый резолвер неопределенностей
func NewUncertaintyResolver(
	uncertaintyRepo *repo.UncertaintyModelRepository,
	claimsRepo *repo.ClaimRepository,
	eventsRepo *repo.EventRepository,
	genaiClient *gen_combined.Client,
	logger *log.Logger,
) *UncertaintyResolver {
	return &UncertaintyResolver{
		uncertaintyRepo: uncertaintyRepo,
		claimsRepo:      claimsRepo,
		eventsRepo:      eventsRepo,
		genaiClient:     genaiClient,
		logger:          logger,
	}
}

// ResolveUncertainty разрешает UncertaintyModel через LLM анализ с web search
func (r *UncertaintyResolver) ResolveUncertainty(
	ctx context.Context,
	tenantID string,
	uncertaintyID string,
) error {
	// Получаем UncertaintyModel
	uncertainty, err := r.uncertaintyRepo.Get(ctx, uncertaintyID)
	if err != nil {
		return fmt.Errorf("failed to get uncertainty: %w", err)
	}

	if uncertainty.Status == models.UncertaintyStatusResolved {
		return nil // Уже разрешено
	}

	// Собираем контекст из затронутых объектов
	contextData, err := r.collectContext(ctx, tenantID, uncertainty.AffectedRefs)
	if err != nil {
		return fmt.Errorf("failed to collect context: %w", err)
	}

	// Анализируем через LLM с web search
	resolution, err := r.analyzeWithWebSearch(ctx, uncertainty, contextData)
	if err != nil {
		return fmt.Errorf("failed to analyze: %w", err)
	}

	// Применяем разрешение
	if err := r.applyResolution(ctx, tenantID, uncertainty, resolution); err != nil {
		return fmt.Errorf("failed to apply resolution: %w", err)
	}

	// Обновляем UncertaintyModel
	uncertainty.Status = models.UncertaintyStatusResolved
	uncertainty.UpdatedAt = time.Now().UTC()
	uncertainty.Notes = &resolution.Reasoning

	if err := r.uncertaintyRepo.Upsert(ctx, uncertainty); err != nil {
		return fmt.Errorf("failed to update uncertainty: %w", err)
	}

	if r.logger != nil {
		r.logger.Printf("[UncertaintyResolver] Resolved uncertainty %s: %s", uncertaintyID, resolution.Reasoning)
	}

	return nil
}

// UncertaintyContext контекст для разрешения неопределенности
type UncertaintyContext struct {
	Claims []*models.Claim
	Events []*models.Event
}

// collectContext собирает контекст из затронутых объектов
func (r *UncertaintyResolver) collectContext(
	ctx context.Context,
	tenantID string,
	affectedRefs []models.AffectedRef,
) (*UncertaintyContext, error) {
	context := &UncertaintyContext{
		Claims: []*models.Claim{},
		Events: []*models.Event{},
	}

	for _, ref := range affectedRefs {
		// Пытаемся получить Claim
		claim, err := r.claimsRepo.Get(ctx, ref.ID)
		if err == nil && claim != nil {
			context.Claims = append(context.Claims, claim)
			continue
		}
		// Пытаемся получить Event
		event, err := r.eventsRepo.Get(ctx, ref.ID)
		if err == nil && event != nil {
			context.Events = append(context.Events, event)
		}
	}

	return context, nil
}

// ResolutionResult результат разрешения неопределенности
type ResolutionResult struct {
	ResolvedValue   interface{} `json:"resolvedValue"`
	Confidence      float64     `json:"confidence"`
	Reasoning       string      `json:"reasoning"`
	SelectedClaimID *string     `json:"selectedClaimId,omitempty"`
	Action          string      `json:"action"` // "accept", "reject", "merge", "defer"
}

// analyzeWithWebSearch анализирует неопределенность через LLM с web search
func (r *UncertaintyResolver) analyzeWithWebSearch(
	ctx context.Context,
	uncertainty *models.UncertaintyModel,
	context *UncertaintyContext,
) (*ResolutionResult, error) {
	// Формируем промпт
	claimsText := ""
	for i, claim := range context.Claims {
		claimsText += fmt.Sprintf("%d. Claim ID: %s\n   Subject: %s\n   Predicate: %s\n   Object: %v\n   Confidence: %.2f\n   Source: %v\n\n",
			i+1, claim.ID, claim.SubjectRef.ID, claim.Predicate, claim.ObjectRef.Value, claim.Confidence, claim.Provenance.SourceIDs)
	}

	prompt := fmt.Sprintf(`You are an expert uncertainty resolver. Analyze the following uncertainty and resolve it using web search if needed.

UNCERTAINTY TYPE: %s
MATERIALITY: %s
STATUS: %s

AFFECTED CLAIMS:
%s

TASK:
1. Use web search to gather additional information about the conflicting claims
2. Analyze the reliability of sources
3. Determine the most likely resolution
4. Provide reasoning for your decision

Return JSON with resolution:
{
  "resolvedValue": "the resolved value or null",
  "confidence": 0.85,
  "reasoning": "Detailed explanation of the resolution",
  "selectedClaimId": "claim_id or null",
  "action": "accept|reject|merge|defer"
}`,
		uncertainty.Type,
		uncertainty.Materiality,
		uncertainty.Status,
		claimsText,
	)

	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"resolvedValue": map[string]interface{}{
				"type": []interface{}{"string", "number", "null"},
			},
			"confidence": map[string]interface{}{
				"type":    "number",
				"minimum": 0,
				"maximum": 1,
			},
			"reasoning": map[string]interface{}{
				"type": "string",
			},
			"selectedClaimId": map[string]interface{}{
				"type": []interface{}{"string", "null"},
			},
			"action": map[string]interface{}{
				"type": "string",
				"enum": []string{"accept", "reject", "merge", "defer"},
			},
		},
		"required": []string{"resolvedValue", "confidence", "reasoning", "action"},
	}

	opts := gen_combined.Options{
		ModelOverride:     "gemini_quality",
		GroundingEnabled:  true, // Включаем web search
		SystemInstruction: "You are an expert uncertainty resolver. Use web search to gather additional information and resolve conflicts with high confidence.",
		Temperature:       0.2,
		MaxOutputTokens:   2048,
	}

	resp, err := r.genaiClient.GenerateJSONStrict(ctx, prompt, nil, schema, opts)
	if err != nil {
		return nil, fmt.Errorf("LLM analysis failed: %w", err)
	}

	// Парсим результат
	var result ResolutionResult
	content := resp.Content
	// Убираем markdown code fences если есть
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)
	
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response: %w", err)
	}

	return &result, nil
}

// applyResolution применяет результат разрешения
func (r *UncertaintyResolver) applyResolution(
	ctx context.Context,
	tenantID string,
	uncertainty *models.UncertaintyModel,
	resolution *ResolutionResult,
) error {
	switch resolution.Action {
	case "accept":
		// Принимаем выбранный Claim, отклоняем остальные
		if resolution.SelectedClaimID != nil {
			for _, ref := range uncertainty.AffectedRefs {
				claim, err := r.claimsRepo.Get(ctx, ref.ID)
				if err == nil && claim != nil {
					if claim.ID == *resolution.SelectedClaimID {
						// Повышаем confidence выбранного Claim
						claim.Confidence = math.Min(1.0, claim.Confidence*1.2)
					} else {
						// Снижаем confidence отклоненных Claims
						claim.Confidence = claim.Confidence * 0.7
						claim.Status = "deprecated"
					}
					_ = r.claimsRepo.Upsert(ctx, claim)
				}
			}
		}

	case "reject":
		// Отклоняем все Claims
		for _, ref := range uncertainty.AffectedRefs {
			claim, err := r.claimsRepo.Get(ctx, ref.ID)
			if err == nil && claim != nil {
				claim.Status = "deprecated"
				claim.Confidence = claim.Confidence * 0.5
				_ = r.claimsRepo.Upsert(ctx, claim)
			}
		}

	case "merge":
		// Объединяем Claims (создаем новый объединенный Claim)
		if len(uncertainty.AffectedRefs) < 2 {
			break
		}

		// Собираем все Claims для объединения
		var claimsToMerge []*models.Claim
		for _, ref := range uncertainty.AffectedRefs {
			claim, err := r.claimsRepo.Get(ctx, ref.ID)
			if err == nil && claim != nil {
				claimsToMerge = append(claimsToMerge, claim)
			}
		}

		if len(claimsToMerge) < 2 {
			break
		}

		// Создаем объединенный Claim
		// Используем данные из первого Claim как основу
		mergedClaim := claimsToMerge[0]
		
		// Объединяем ObjectRef значения (если они разные)
		var mergedValue string
		var allSourceIDs []string
		var allEvidenceURLs []string
		maxConfidence := mergedClaim.Confidence

		for _, claim := range claimsToMerge {
			if claim.ObjectRef.Value != nil {
				if strVal, ok := claim.ObjectRef.Value.(string); ok && strVal != "" {
					if mergedValue == "" {
						mergedValue = strVal
					} else if mergedValue != strVal {
						mergedValue = mergedValue + "; " + strVal
					}
				}
			}
			allSourceIDs = append(allSourceIDs, claim.Provenance.SourceIDs...)
			allEvidenceURLs = append(allEvidenceURLs, claim.Provenance.EvidenceAnchors...)
			if claim.Confidence > maxConfidence {
				maxConfidence = claim.Confidence
			}
		}

		// Обновляем объединенный Claim
		mergedClaim.ObjectRef.Value = mergedValue
		mergedClaim.Confidence = math.Min(1.0, maxConfidence*1.1) // Немного повышаем confidence
		
		// Объединяем SourceIDs (убираем дубликаты)
		sourceIDMap := make(map[string]bool)
		for _, id := range allSourceIDs {
			sourceIDMap[id] = true
		}
		mergedClaim.Provenance.SourceIDs = make([]string, 0, len(sourceIDMap))
		for id := range sourceIDMap {
			mergedClaim.Provenance.SourceIDs = append(mergedClaim.Provenance.SourceIDs, id)
		}

		// Объединяем EvidenceAnchors (убираем дубликаты)
		evidenceMap := make(map[string]bool)
		for _, url := range allEvidenceURLs {
			evidenceMap[url] = true
		}
		mergedClaim.Provenance.EvidenceAnchors = make([]string, 0, len(evidenceMap))
		for url := range evidenceMap {
			mergedClaim.Provenance.EvidenceAnchors = append(mergedClaim.Provenance.EvidenceAnchors, url)
		}

		// Обновляем объединенный Claim
		if err := r.claimsRepo.Upsert(ctx, mergedClaim); err != nil {
			if r.logger != nil {
				r.logger.Printf("[UncertaintyResolver] Failed to update merged claim: %v", err)
			}
		}

		// Помечаем остальные Claims как merged
		for _, claim := range claimsToMerge {
			if claim.ID != mergedClaim.ID {
				claim.Status = "merged"
				claim.Confidence = claim.Confidence * 0.8
				_ = r.claimsRepo.Upsert(ctx, claim)
			}
		}

	case "defer":
		// Откладываем разрешение
		uncertainty.Status = models.UncertaintyStatusDeferred
	}

	return nil
}

