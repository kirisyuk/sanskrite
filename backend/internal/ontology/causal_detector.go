package ontology

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// CausalDetector автоматически обнаруживает причинно-следственные связи между Events
type CausalDetector struct {
	eventsRepo      *repo.EventRepository
	causalLinksRepo *repo.CausalLinkRepository
	genaiClient     *gen_combined.Client
	executor        *ai.AITaskExecutor
	logger          *log.Logger
}

// NewCausalDetector создает новый детектор причинно-следственных связей
func NewCausalDetector(
	eventsRepo *repo.EventRepository,
	causalLinksRepo *repo.CausalLinkRepository,
	genaiClient *gen_combined.Client,
	executor *ai.AITaskExecutor,
	logger *log.Logger,
) *CausalDetector {
	return &CausalDetector{
		eventsRepo:      eventsRepo,
		causalLinksRepo: causalLinksRepo,
		genaiClient:     genaiClient,
		executor:        executor,
		logger:          logger,
	}
}

// DetectCausalLinksForEvent анализирует новый Event на возможные причинно-следственные связи
func (d *CausalDetector) DetectCausalLinksForEvent(
	ctx context.Context,
	tenantID string,
	newEvent *models.Event,
) error {
	// Получаем недавние Events (за последние 30 дней)
	recentEvents, err := d.getRecentEvents(ctx, tenantID, 30*24*time.Hour)
	if err != nil {
		return fmt.Errorf("failed to get recent events: %w", err)
	}

	// Фильтруем события, которые произошли до нового события
	var candidateEvents []*models.Event
	for _, event := range recentEvents {
		if event.ID == newEvent.ID {
			continue
		}
		if event.OccurredAt != nil && newEvent.OccurredAt != nil {
			if event.OccurredAt.Before(*newEvent.OccurredAt) {
				candidateEvents = append(candidateEvents, event)
			}
		}
	}

	if len(candidateEvents) == 0 {
		return nil // Нет кандидатов для анализа
	}

	// Используем LLM для анализа причинно-следственных связей
	causalLinks, err := d.analyzeCausalLinks(ctx, newEvent, candidateEvents)
	if err != nil {
		return fmt.Errorf("failed to analyze causal links: %w", err)
	}

	// Создаем CausalLink объекты
	for _, link := range causalLinks {
		if err := d.createCausalLink(ctx, tenantID, link); err != nil {
			if d.logger != nil {
				d.logger.Printf("[CausalDetector] Failed to create causal link: %v", err)
			}
			continue
		}
	}

	if d.logger != nil {
		d.logger.Printf("[CausalDetector] Detected %d causal links for event %s", len(causalLinks), newEvent.ID)
	}

	return nil
}

// getRecentEvents получает недавние события
func (d *CausalDetector) getRecentEvents(ctx context.Context, tenantID string, duration time.Duration) ([]*models.Event, error) {
	// Получаем все события за период
	allEvents, err := d.eventsRepo.ListByTenant(ctx, tenantID, 1000)
	if err != nil {
		return nil, err
	}

	cutoff := time.Now().UTC().Add(-duration)
	var recentEvents []*models.Event

	for _, event := range allEvents {
		if event.OccurredAt != nil && event.OccurredAt.After(cutoff) {
			recentEvents = append(recentEvents, event)
		} else if event.CreatedAt.After(cutoff) {
			// Если OccurredAt не указан, используем CreatedAt
			recentEvents = append(recentEvents, event)
		}
	}

	return recentEvents, nil
}

// CausalLinkCandidate кандидат на причинно-следственную связь
type CausalLinkCandidate struct {
	CauseEventID  string
	EffectEventID string
	Strength      float64
	Probability   float64
	Reasoning     string
	Sign          models.CausalSign
}

// analyzeCausalLinks анализирует причинно-следственные связи через LLM
func (d *CausalDetector) analyzeCausalLinks(
	ctx context.Context,
	effectEvent *models.Event,
	candidateCauses []*models.Event,
) ([]*CausalLinkCandidate, error) {
	if len(candidateCauses) == 0 {
		return nil, nil
	}

	// Формируем промпт для LLM
	causesText := ""
	for i, cause := range candidateCauses {
		causesText += fmt.Sprintf("%d. %s (at: %s)\n", i+1, cause.Title, formatTime(cause.OccurredAt))
	}

	prompt := fmt.Sprintf(`Analyze potential causal relationships between events.

EFFECT EVENT (new event):
Title: %s
Occurred At: %s
Description: %s

CANDIDATE CAUSE EVENTS (previous events):
%s

Task: Identify which candidate events might have caused the effect event. Return JSON with causal links array.

Return JSON strictly matching this schema:
{
  "causalLinks": [
    {
      "causeEventIndex": 0,
      "strength": 0.8,
      "probability": 0.7,
      "sign": "positive|negative|neutral",
      "reasoning": "Brief explanation of the causal relationship"
    }
  ]
}

All fields are required.`,
		effectEvent.Title,
		formatTime(effectEvent.OccurredAt),
		effectEvent.Description,
		causesText,
	)

	// Используем AITaskExecutor с верификатором для проверки JSON десериализации
	validateFunc := func(data map[string]interface{}) error {
		return nil
	}
	verifier := ai.NewGenericVerifier(validateFunc)

	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentGeneration,
		InputContent: prompt + "\n\nReturn JSON strictly matching the schema. Do not include markdown code fences.",
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       6,
			MaxOutputSize:            2048,
			MinContextWindow:         4000,
		},
		Verifier: verifier,
	}

	parsed, err := d.executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("LLM analysis failed: %w", err)
	}

	// Преобразуем результат
	resultMap, ok := parsed.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected result type: %T", parsed)
	}

	// Преобразуем map в структуру
	var result struct {
		CausalLinks []struct {
			CauseEventIndex int     `json:"causeEventIndex"`
			Strength        float64 `json:"strength"`
			Probability     float64 `json:"probability"`
			Sign            string  `json:"sign"`
			Reasoning       string  `json:"reasoning"`
		} `json:"causalLinks"`
	}

	if links, ok := resultMap["causalLinks"].([]interface{}); ok {
		result.CausalLinks = make([]struct {
			CauseEventIndex int     `json:"causeEventIndex"`
			Strength        float64 `json:"strength"`
			Probability     float64 `json:"probability"`
			Sign            string  `json:"sign"`
			Reasoning       string  `json:"reasoning"`
		}, len(links))
		for i, link := range links {
			if linkMap, ok := link.(map[string]interface{}); ok {
				if idx, ok := linkMap["causeEventIndex"].(float64); ok {
					result.CausalLinks[i].CauseEventIndex = int(idx)
				}
				if strength, ok := linkMap["strength"].(float64); ok {
					result.CausalLinks[i].Strength = strength
				}
				if prob, ok := linkMap["probability"].(float64); ok {
					result.CausalLinks[i].Probability = prob
				}
				if sign, ok := linkMap["sign"].(string); ok {
					result.CausalLinks[i].Sign = sign
				}
				if reason, ok := linkMap["reasoning"].(string); ok {
					result.CausalLinks[i].Reasoning = reason
				}
			}
		}
	}

	candidates := d.processCausalLinksResult(&result, candidateCauses, effectEvent)
	return candidates, nil
}

func (d *CausalDetector) processCausalLinksResult(result *struct {
	CausalLinks []struct {
		CauseEventIndex int     `json:"causeEventIndex"`
		Strength        float64 `json:"strength"`
		Probability     float64 `json:"probability"`
		Sign            string  `json:"sign"`
		Reasoning       string  `json:"reasoning"`
	} `json:"causalLinks"`
}, candidateCauses []*models.Event, effectEvent *models.Event) []*CausalLinkCandidate {

	// Преобразуем в CausalLinkCandidate
	var candidates []*CausalLinkCandidate
	for _, link := range result.CausalLinks {
		if link.CauseEventIndex < 0 || link.CauseEventIndex >= len(candidateCauses) {
			continue
		}

		causeEvent := candidateCauses[link.CauseEventIndex]

		// Определяем CausalSign
		var sign models.CausalSign
		switch link.Sign {
		case "positive":
			sign = models.CausalSignPositive
		case "negative":
			sign = models.CausalSignNegative
		default:
			sign = models.CausalSignPositive // По умолчанию positive
		}

		candidates = append(candidates, &CausalLinkCandidate{
			CauseEventID:  causeEvent.ID,
			EffectEventID: effectEvent.ID,
			Strength:      link.Strength,
			Probability:   link.Probability,
			Reasoning:     link.Reasoning,
			Sign:          sign,
		})
	}

	return candidates
}

// createCausalLink создает CausalLink объект
func (d *CausalDetector) createCausalLink(
	ctx context.Context,
	tenantID string,
	candidate *CausalLinkCandidate,
) error {
	now := time.Now().UTC()

	causalLink := &models.CausalLink{
		BaseOntologyFields: models.BaseOntologyFields{
			TenantID:  tenantID,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
			CreatedBy: "system",
			UpdatedBy: "system",
			Labels:    map[string]string{"en": "Causal relationship"},
			Status:    "active",
		},
		CauseRef: models.CausalRef{
			ID:   candidate.CauseEventID,
			Kind: models.CausalRefKindEvent,
		},
		EffectRef: models.CausalRef{
			ID:   candidate.EffectEventID,
			Kind: models.CausalRefKindEvent,
		},
		Sign:        candidate.Sign,
		Strength:    candidate.Strength,
		Probability: candidate.Probability,
		Evidence: models.CausalEvidence{
			Method: stringPtr("LLM_Analysis"),
		},
		Assumptions: []string{candidate.Reasoning},
	}

	_, err := d.causalLinksRepo.Create(ctx, causalLink)
	if err != nil {
		return fmt.Errorf("failed to create causal link: %w", err)
	}

	return nil
}

func formatTime(t *time.Time) string {
	if t == nil {
		return "Unknown"
	}
	return t.Format(time.RFC3339)
}

func stringPtr(s string) *string {
	return &s
}

// GetCausalLinksForEvent получает все CausalLink для указанного event
func (d *CausalDetector) GetCausalLinksForEvent(ctx context.Context, tenantID, eventID string) ([]*models.CausalLink, error) {
	allLinks, err := d.causalLinksRepo.ListByTenant(ctx, tenantID, 1000)
	if err != nil {
		return nil, err
	}

	var result []*models.CausalLink
	for _, link := range allLinks {
		// Проверяем, относится ли link к этому event (как cause или effect)
		if link.CauseRef.ID == eventID || link.EffectRef.ID == eventID {
			result = append(result, link)
		}
	}

	return result, nil
}
