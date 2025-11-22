package ontology

import (
	"context"
	"fmt"
	"log"
	"math"

	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// TrustIntegrator интегрирует Source Trustworthiness в онтологические объекты
type TrustIntegrator struct {
	sourcesRepo *repo.SourceRepository
	claimsRepo  *repo.ClaimRepository
	eventsRepo  *repo.EventRepository
	trustRepo   *repo.TrustRepository
	logger      *log.Logger
}

// NewTrustIntegrator создает новый интегратор доверия
func NewTrustIntegrator(
	sourcesRepo *repo.SourceRepository,
	claimsRepo *repo.ClaimRepository,
	eventsRepo *repo.EventRepository,
	trustRepo *repo.TrustRepository,
	logger *log.Logger,
) *TrustIntegrator {
	return &TrustIntegrator{
		sourcesRepo: sourcesRepo,
		claimsRepo:  claimsRepo,
		eventsRepo:  eventsRepo,
		trustRepo:   trustRepo,
		logger:      logger,
	}
}

// AdjustClaimConfidence корректирует Confidence Claim на основе Source Trust
func (t *TrustIntegrator) AdjustClaimConfidence(
	ctx context.Context,
	tenantID string,
	claim *models.Claim,
	sourceID string,
	baseConfidence float64,
) float64 {
	if sourceID == "" {
		return baseConfidence
	}

	// Получаем Source
	source, err := t.sourcesRepo.Get(ctx, sourceID)
	if err != nil || source == nil {
		if t.logger != nil {
			t.logger.Printf("[TrustIntegrator] Source not found: %s, using base confidence", sourceID)
		}
		return baseConfidence
	}

	// Получаем Trust (если есть)
	var trustScore float64 = source.ReliabilityPrior
	trust, err := t.trustRepo.GetBySourceID(ctx, tenantID, sourceID)
	if err == nil && trust != nil {
		// Используем текущий Trust Score, если он есть
		trustScore = trust.CurrentScore
	}

	// Формула: adjustedConfidence = baseConfidence * trustScore
	// Но не опускаем ниже 0.1 и не поднимаем выше 1.0
	adjustedConfidence := baseConfidence * trustScore
	adjustedConfidence = math.Max(0.1, math.Min(1.0, adjustedConfidence))

	if t.logger != nil {
		t.logger.Printf("[TrustIntegrator] Adjusted claim confidence: base=%.2f, trust=%.2f, adjusted=%.2f",
			baseConfidence, trustScore, adjustedConfidence)
	}

	return adjustedConfidence
}

// AdjustEventConfidence корректирует Confidence Event на основе Source Trust
func (t *TrustIntegrator) AdjustEventConfidence(
	ctx context.Context,
	tenantID string,
	event *models.Event,
	sourceID string,
	baseConfidence float64,
) float64 {
	if sourceID == "" {
		return baseConfidence
	}

	// Получаем Source
	source, err := t.sourcesRepo.Get(ctx, sourceID)
	if err != nil || source == nil {
		return baseConfidence
	}

	// Получаем Trust
	var trustScore float64 = source.ReliabilityPrior
	trust, err := t.trustRepo.GetBySourceID(ctx, tenantID, sourceID)
	if err == nil && trust != nil {
		trustScore = trust.CurrentScore
	}

	adjustedConfidence := baseConfidence * trustScore
	adjustedConfidence = math.Max(0.1, math.Min(1.0, adjustedConfidence))

	return adjustedConfidence
}

// RecalculateConfidencesForSource пересчитывает confidence всех Claims/Events для источника
// Вызывается при обновлении Trust.CurrentScore
func (t *TrustIntegrator) RecalculateConfidencesForSource(
	ctx context.Context,
	tenantID, sourceID string,
) error {
	// Получаем Source и Trust
	source, err := t.sourcesRepo.Get(ctx, sourceID)
	if err != nil || source == nil {
		return fmt.Errorf("source not found: %w", err)
	}

	trust, err := t.trustRepo.GetBySourceID(ctx, tenantID, sourceID)
	if err != nil {
		// Trust может не существовать, используем ReliabilityPrior
		if t.logger != nil {
			t.logger.Printf("[TrustIntegrator] Trust not found for source %s, using ReliabilityPrior", sourceID)
		}
	}

	trustScore := source.ReliabilityPrior
	if trust != nil {
		trustScore = trust.CurrentScore
	}

	// Находим все Claims с этим SourceID в Provenance
	// Получаем все Claims для tenant и фильтруем по SourceID
	allClaims, err := t.claimsRepo.ListByTenant(ctx, tenantID, 10000)
	if err != nil {
		return fmt.Errorf("failed to list claims: %w", err)
	}

	updatedCount := 0
	for _, claim := range allClaims {
		// Проверяем, содержит ли Provenance.SourceIDs наш sourceID
		hasSource := false
		for _, sid := range claim.Provenance.SourceIDs {
			if sid == sourceID {
				hasSource = true
				break
			}
		}

		if hasSource {
			// Пересчитываем confidence на основе нового trust score
			// Находим базовый confidence (обратная операция от AdjustClaimConfidence)
			// Но проще пересчитать заново, используя исходный confidence из атрибутов или пересчитать
			// Для простоты используем текущий confidence и корректируем пропорционально
			oldTrustScore := source.ReliabilityPrior
			if trust != nil {
				// Если Trust существовал, используем старое значение (но мы его уже обновили)
				// Поэтому используем текущее значение как новое
				oldTrustScore = source.ReliabilityPrior
			}

			// Корректируем confidence пропорционально изменению trust score
			if oldTrustScore > 0 {
				newConfidence := claim.Confidence * (trustScore / oldTrustScore)
				newConfidence = math.Max(0.1, math.Min(1.0, newConfidence))
				claim.Confidence = newConfidence
				if err := t.claimsRepo.Upsert(ctx, claim); err != nil {
					if t.logger != nil {
						t.logger.Printf("[TrustIntegrator] Failed to update claim %s: %v", claim.ID, err)
					}
					continue
				}
				updatedCount++
			}
		}
	}

	// Аналогично для Events
	allEvents, err := t.eventsRepo.ListByTenant(ctx, tenantID, 10000)
	if err == nil {
		for _, event := range allEvents {
			hasSource := false
			for _, sid := range event.Provenance.SourceIDs {
				if sid == sourceID {
					hasSource = true
					break
				}
			}

			if hasSource {
				oldTrustScore := source.ReliabilityPrior
				if oldTrustScore > 0 {
					newConfidence := event.Confidence * (trustScore / oldTrustScore)
					newConfidence = math.Max(0.1, math.Min(1.0, newConfidence))
					event.Confidence = newConfidence
					if err := t.eventsRepo.Upsert(ctx, event); err == nil {
						updatedCount++
					}
				}
			}
		}
	}

	if t.logger != nil {
		t.logger.Printf("[TrustIntegrator] Recalculated confidences for source %s with trust score %.2f: updated %d objects",
			sourceID, trustScore, updatedCount)
	}

	return nil
}

// GetSourceTrustScore возвращает актуальный Trust Score для источника
func (t *TrustIntegrator) GetSourceTrustScore(
	ctx context.Context,
	tenantID, sourceID string,
) (float64, error) {
	if sourceID == "" {
		return 0.5, nil // Default trust для неизвестных источников
	}

	source, err := t.sourcesRepo.Get(ctx, sourceID)
	if err != nil || source == nil {
		return 0.5, nil
	}

	trustScore := source.ReliabilityPrior

	// Проверяем Trust
	trust, err := t.trustRepo.GetBySourceID(ctx, tenantID, sourceID)
	if err == nil && trust != nil {
		trustScore = trust.CurrentScore
	}

	return trustScore, nil
}

