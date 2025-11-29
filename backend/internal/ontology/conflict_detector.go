package ontology

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// ConflictDetector обнаруживает конфликты между Claims и создает UncertaintyModel
type ConflictDetector struct {
	claimsRepo        *repo.ClaimRepository
	uncertaintyRepo   *repo.UncertaintyModelRepository
	trustIntegrator   *TrustIntegrator
	logger            *log.Logger
}

// NewConflictDetector создает новый детектор конфликтов
func NewConflictDetector(
	claimsRepo *repo.ClaimRepository,
	uncertaintyRepo *repo.UncertaintyModelRepository,
	trustIntegrator *TrustIntegrator,
	logger *log.Logger,
) *ConflictDetector {
	return &ConflictDetector{
		claimsRepo:      claimsRepo,
		uncertaintyRepo: uncertaintyRepo,
		trustIntegrator: trustIntegrator,
		logger:          logger,
	}
}

// DetectConflictsForClaim проверяет новый Claim на конфликты с существующими
func (d *ConflictDetector) DetectConflictsForClaim(
	ctx context.Context,
	tenantID string,
	newClaim *models.Claim,
) error {
	// Получаем все Claims для того же SubjectRef
	allClaims, err := d.claimsRepo.ListByTenant(ctx, tenantID, 1000)
	if err != nil {
		return fmt.Errorf("failed to list claims: %w", err)
	}

	var conflictingClaims []*models.Claim

	// Ищем конфликты
	for _, existingClaim := range allClaims {
		if existingClaim.ID == newClaim.ID {
			continue // Пропускаем сам новый Claim
		}

		// Проверяем, относится ли к тому же субъекту и предикату
		if existingClaim.SubjectRef.ID != newClaim.SubjectRef.ID ||
			existingClaim.SubjectRef.Kind != newClaim.SubjectRef.Kind {
			continue
		}

		if existingClaim.Predicate != newClaim.Predicate {
			continue
		}

		// Проверяем конфликт значений ObjectRef
		if d.isConflicting(newClaim.ObjectRef, existingClaim.ObjectRef) {
			conflictingClaims = append(conflictingClaims, existingClaim)
		}
	}

	// Если найдены конфликты, создаем UncertaintyModel
	if len(conflictingClaims) > 0 {
		if err := d.createConflictUncertainty(ctx, tenantID, newClaim, conflictingClaims); err != nil {
			if d.logger != nil {
				d.logger.Printf("[ConflictDetector] Failed to create conflict uncertainty: %v", err)
			}
			return err
		}

		if d.logger != nil {
			d.logger.Printf("[ConflictDetector] Detected %d conflicts for claim %s", len(conflictingClaims), newClaim.ID)
		}
	}

	return nil
}

// isConflicting проверяет, конфликтуют ли два ObjectRef
func (d *ConflictDetector) isConflicting(obj1, obj2 models.ObjectRef) bool {
	// Если оба Literal, сравниваем значения
	if obj1.Kind == models.ReferenceKindLiteral && obj2.Kind == models.ReferenceKindLiteral {
		return obj1.Value != obj2.Value
	}

	// Если оба Reference, сравниваем ID
	if obj1.Kind != models.ReferenceKindLiteral && obj2.Kind != models.ReferenceKindLiteral {
		if obj1.ID != nil && obj2.ID != nil {
			return *obj1.ID != *obj2.ID
		}
	}

	// Разные типы - не конфликт (может быть нормализация)
	return false
}

// createConflictUncertainty создает UncertaintyModel для конфликта
func (d *ConflictDetector) createConflictUncertainty(
	ctx context.Context,
	tenantID string,
	newClaim *models.Claim,
	conflictingClaims []*models.Claim,
) error {
	now := time.Now().UTC()

	// Собираем все затронутые Claims
	affectedRefs := []models.AffectedRef{
		{
			ID:   newClaim.ID,
			Kind: models.ReferenceKindLiteral, // Используем Literal для Claims
		},
	}

	for _, confClaim := range conflictingClaims {
		affectedRefs = append(affectedRefs, models.AffectedRef{
			ID:   confClaim.ID,
			Kind: models.ReferenceKindLiteral,
		})
	}

	// Определяем materiality на основе количества конфликтов и confidence
	materiality := models.MaterialityLow
	if len(conflictingClaims) >= 3 {
		materiality = models.MaterialityHigh
	} else if len(conflictingClaims) >= 2 {
		materiality = models.MaterialityMedium
	}

	// Вычисляем средний confidence конфликтующих Claims
	avgConfidence := newClaim.Confidence
	for _, confClaim := range conflictingClaims {
		avgConfidence += confClaim.Confidence
	}
	avgConfidence /= float64(len(conflictingClaims) + 1)

	if avgConfidence > 0.7 {
		materiality = models.MaterialityHigh
	}

	// Создаем UncertaintyModel
	uncertainty := &models.UncertaintyModel{
		BaseOntologyFields: models.BaseOntologyFields{
			TenantID:  tenantID,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
			CreatedBy: "system",
			UpdatedBy: "system",
			Labels: map[string]string{
				"en": fmt.Sprintf("Conflict between %d claims", len(conflictingClaims)+1),
			},
			Status: "active",
		},
		Type:         models.UncertaintyTypeConflict,
		AffectedRefs: affectedRefs,
		Representation: models.UncertaintyRepresentation{
			Confidence: &avgConfidence,
		},
		Materiality: materiality,
		ImpactOnAnswers: models.ImpactOnAnswers{
			Modes: []models.AnswerMode{models.AnswerModeCaveat},
		},
		Status: models.UncertaintyStatusOpen,
		Notes:  stringPtr(fmt.Sprintf("Conflicting claims detected: %s vs %d other claims", newClaim.ID, len(conflictingClaims))),
	}

	_, err := d.uncertaintyRepo.Create(ctx, uncertainty)
	if err != nil {
		return fmt.Errorf("failed to create uncertainty model: %w", err)
	}

	return nil
}

// ResolveConflict разрешает конфликт через LLM анализ (будет реализовано в UncertaintyResolutionService)
func (d *ConflictDetector) ResolveConflict(
	ctx context.Context,
	tenantID string,
	uncertaintyID string,
) error {
	// Получаем UncertaintyModel
	uncertainty, err := d.uncertaintyRepo.Get(ctx, uncertaintyID)
	if err != nil {
		return fmt.Errorf("failed to get uncertainty: %w", err)
	}

	if uncertainty.Type != models.UncertaintyTypeConflict {
		return fmt.Errorf("uncertainty is not a conflict")
	}

	// Получаем все конфликтующие Claims
	var claims []*models.Claim
	for _, ref := range uncertainty.AffectedRefs {
		// Проверяем ID (для Claims используем ID напрямую)
		claim, err := d.claimsRepo.Get(ctx, ref.ID)
		if err == nil && claim != nil {
			claims = append(claims, claim)
		}
	}

	if len(claims) < 2 {
		return fmt.Errorf("not enough claims for conflict resolution")
	}

	// Анализ через Trust Scores
	// Более надежный источник имеет приоритет
	bestClaim := claims[0]
	bestTrustScore := 0.0

	for _, claim := range claims {
		// Получаем Trust Score для каждого Source
		if len(claim.Provenance.SourceIDs) > 0 {
			for _, sourceID := range claim.Provenance.SourceIDs {
				trustScore, err := d.trustIntegrator.GetSourceTrustScore(ctx, tenantID, sourceID)
				if err == nil && trustScore > bestTrustScore {
					bestTrustScore = trustScore
					bestClaim = claim
				}
			}
		}
	}

	// Обновляем UncertaintyModel
	uncertainty.Status = models.UncertaintyStatusResolved
	uncertainty.Notes = stringPtr(fmt.Sprintf("Resolved: Claim %s selected based on source trust (score: %.2f)", bestClaim.ID, bestTrustScore))
	uncertainty.UpdatedAt = time.Now().UTC()

	if err := d.uncertaintyRepo.Upsert(ctx, uncertainty); err != nil {
		return fmt.Errorf("failed to update uncertainty: %w", err)
	}

	// Помечаем другие Claims как deprecated или снижаем confidence
	for _, claim := range claims {
		if claim.ID != bestClaim.ID {
			claim.Status = "deprecated"
			claim.Confidence = claim.Confidence * 0.5 // Снижаем confidence
			_ = d.claimsRepo.Upsert(ctx, claim)
		}
	}

	return nil
}

// GetUncertaintiesForClaim получает все UncertaintyModel для указанного claim
func (d *ConflictDetector) GetUncertaintiesForClaim(ctx context.Context, tenantID, claimID string) ([]*models.UncertaintyModel, error) {
	allUncertainties, err := d.uncertaintyRepo.ListByTenant(ctx, tenantID, 1000)
	if err != nil {
		return nil, err
	}

	var result []*models.UncertaintyModel
	for _, unc := range allUncertainties {
		// Проверяем, относится ли uncertainty к этому claim
		for _, ref := range unc.AffectedRefs {
			if ref.ID == claimID {
				result = append(result, unc)
				break
			}
		}
	}

	return result, nil
}

