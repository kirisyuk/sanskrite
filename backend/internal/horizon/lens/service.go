package lens

import (
	"context"
	"fmt"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	agentmodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

// LensService управляет Lens и подписками на HorizonFragment
type LensService struct {
	lensesRepo       *repo.LensesRepository
	fragmentsRepo    *repo.HorizonFragmentsRepository
	scoutingJobsRepo *repo.ScoutingJobsRepository
	lifecycle        *LifecycleManager
	calculator       *MetricsCalculator
}

// NewLensService создает новый Lens Service
func NewLensService(
	lensesRepo *repo.LensesRepository,
	fragmentsRepo *repo.HorizonFragmentsRepository,
	scoutingJobsRepo *repo.ScoutingJobsRepository,
	lifecycle *LifecycleManager,
	calculator *MetricsCalculator,
) *LensService {
	return &LensService{
		lensesRepo:       lensesRepo,
		fragmentsRepo:    fragmentsRepo,
		scoutingJobsRepo: scoutingJobsRepo,
		lifecycle:        lifecycle,
		calculator:       calculator,
	}
}

// CreateLens создает новый Lens и управляет подписками
func (s *LensService) CreateLens(ctx context.Context, lens *models.Lens) (string, error) {
	if err := lens.Validate(); err != nil {
		return "", fmt.Errorf("validation error: %w", err)
	}

	// Создаем Lens
	lensID, err := s.lensesRepo.Create(ctx, lens)
	if err != nil {
		return "", fmt.Errorf("failed to create lens: %w", err)
	}
	lens.ID = lensID

	// Увеличиваем activeSubscribers для всех HorizonFragment в scope
	for _, canonicalPath := range lens.Scope {
		fragment, err := s.fragmentsRepo.GetByCanonicalPath(ctx, canonicalPath)
		if err != nil {
			// Если фрагмент не существует, создаем ScoutingJob (Pioneer модель)
			// Согласно документации, первый проект оплачивает создание (Bounty модель)
			if s.scoutingJobsRepo != nil {
				now := time.Now().UTC()
				scoutingJob := &models.ScoutingJob{
					AgentJob: agentmodels.AgentJob{
						TenantID:  lens.TenantID,
						ProjectID: lens.ProjectID,
						Status:    "pending",
						AgentID:   "scout",
						CreatedAt: now,
						UpdatedAt: now,
						State: map[string]interface{}{
							"targetCanonicalPath": canonicalPath,
							"lensId":              lensID,
							"currentState":        string(models.ScoutingJobStatePending),
						},
					},
					LensID:             lensID,
					TargetCanonicalPath: canonicalPath,
					ScoutingStrategy:    string(models.ScoutingStrategySnapshot),
					BillingContext: models.BillingContext{
						ChargeModel: string(models.ChargeModelBounty),
						ProjectID:   lens.ProjectID,
					},
					CurrentState: models.ScoutingJobStatePending,
				}
				// Создаем ScoutingJob (будет обработан Scout Engine позже)
				_, _ = s.scoutingJobsRepo.Create(ctx, scoutingJob)
			}
			continue
		}
		if err := s.fragmentsRepo.IncrementActiveSubscribers(ctx, fragment.ID); err != nil {
			// Логируем ошибку, но не прерываем создание Lens
			// TODO: добавить логирование
			_ = err
		}
		// Управляем жизненным циклом
		if err := s.lifecycle.EnsureActive(ctx, fragment.ID); err != nil {
			_ = err
		}
	}

	return lensID, nil
}

// UpdateLens обновляет Lens и управляет подписками
func (s *LensService) UpdateLens(ctx context.Context, lens *models.Lens) error {
	if err := lens.Validate(); err != nil {
		return fmt.Errorf("validation error: %w", err)
	}

	// Получаем старый Lens для сравнения scope
	oldLens, err := s.lensesRepo.Get(ctx, lens.ID)
	if err != nil {
		return fmt.Errorf("failed to get old lens: %w", err)
	}

	// Обновляем Lens
	if err := s.lensesRepo.Update(ctx, lens); err != nil {
		return fmt.Errorf("failed to update lens: %w", err)
	}

	// Управляем подписками: отписываемся от старых, подписываемся на новые
	oldScope := make(map[string]bool)
	for _, path := range oldLens.Scope {
		oldScope[path] = true
	}

	newScope := make(map[string]bool)
	for _, path := range lens.Scope {
		newScope[path] = true
	}

	// Отписываемся от удаленных из scope
	for _, path := range oldLens.Scope {
		if !newScope[path] {
			fragment, err := s.fragmentsRepo.GetByCanonicalPath(ctx, path)
			if err == nil {
				_ = s.fragmentsRepo.DecrementActiveSubscribers(ctx, fragment.ID)
				_ = s.lifecycle.CheckAndTransition(ctx, fragment.ID)
			}
		}
	}

	// Подписываемся на новые в scope
	for _, path := range lens.Scope {
		if !oldScope[path] {
			fragment, err := s.fragmentsRepo.GetByCanonicalPath(ctx, path)
			if err == nil {
				_ = s.fragmentsRepo.IncrementActiveSubscribers(ctx, fragment.ID)
				_ = s.lifecycle.EnsureActive(ctx, fragment.ID)
			}
		}
	}

	return nil
}

// DeleteLens удаляет Lens и управляет подписками
func (s *LensService) DeleteLens(ctx context.Context, lensID string) error {
	// Получаем Lens перед удалением
	lens, err := s.lensesRepo.Get(ctx, lensID)
	if err != nil {
		return fmt.Errorf("failed to get lens: %w", err)
	}

	// Удаляем Lens
	if err := s.lensesRepo.Delete(ctx, lensID); err != nil {
		return fmt.Errorf("failed to delete lens: %w", err)
	}

	// Уменьшаем activeSubscribers для всех HorizonFragment в scope
	for _, canonicalPath := range lens.Scope {
		fragment, err := s.fragmentsRepo.GetByCanonicalPath(ctx, canonicalPath)
		if err == nil {
			_ = s.fragmentsRepo.DecrementActiveSubscribers(ctx, fragment.ID)
			_ = s.lifecycle.CheckAndTransition(ctx, fragment.ID)
		}
	}

	return nil
}

// GetLens получает Lens по ID
func (s *LensService) GetLens(ctx context.Context, lensID string) (*models.Lens, error) {
	return s.lensesRepo.Get(ctx, lensID)
}

// ListLensesByProject получает список Lens для проекта
func (s *LensService) ListLensesByProject(ctx context.Context, tenantID, projectID string, status *string, limit int) ([]*models.Lens, error) {
	return s.lensesRepo.ListByProject(ctx, tenantID, projectID, status, limit)
}

// CalculateMetrics пересчитывает метрики для Lens
func (s *LensService) CalculateMetrics(ctx context.Context, lensID string) (map[string]interface{}, error) {
	lens, err := s.lensesRepo.Get(ctx, lensID)
	if err != nil {
		return nil, fmt.Errorf("failed to get lens: %w", err)
	}

	return s.calculator.CalculateLensMetrics(ctx, lens)
}

