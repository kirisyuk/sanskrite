package lens

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	agentmodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

func parseStringSlice(v interface{}) []string {
	switch t := v.(type) {
	case []string:
		out := make([]string, 0, len(t))
		for _, s := range t {
			if strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(t))
		for _, it := range t {
			if s, ok := it.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	case string:
		parts := strings.Split(t, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		return out
	default:
		return nil
	}
}

func parseInt(v interface{}, def int) int {
	switch t := v.(type) {
	case int:
		return t
	case int32:
		return int(t)
	case int64:
		return int(t)
	case float64:
		return int(t)
	case float32:
		return int(t)
	case string:
		t = strings.TrimSpace(t)
		if t == "" {
			return def
		}
		if i, err := strconv.Atoi(t); err == nil {
			return i
		}
		if f, err := strconv.ParseFloat(t, 64); err == nil {
			return int(f)
		}
		return def
	default:
		return def
	}
}

// LensService управляет Lens и подписками на HorizonFragment
type LensService struct {
	lensesRepo       *repo.LensesRepository
	fragmentsRepo    *repo.HorizonFragmentsRepository
	scoutingJobsRepo *repo.ScoutingJobsRepository
	lifecycle        *LifecycleManager
	calculator       *MetricsCalculator
	watchdogHook     func(ctx context.Context, fragmentID string)
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

// SetWatchdogLifecycleHook устанавливает hook, который синхронизирует WatchdogConfig
// после изменений жизненного цикла фрагментов (ACTIVE/DORMANT/ARCHIVED).
func (s *LensService) SetWatchdogLifecycleHook(hook func(ctx context.Context, fragmentID string)) {
	s.watchdogHook = hook
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

	// Optional behavior knobs for Horizon assembly (kept in ViewConfig to avoid breaking schema):
	// - requestedMetrics: []string | []any(strings) | "a,b,c"
	// - maxRecursionDepth: number | string
	requestedMetrics := []string(nil)
	maxRecursionDepth := 2
	if lens.ViewConfig != nil {
		if v, ok := lens.ViewConfig["requestedMetrics"]; ok {
			requestedMetrics = parseStringSlice(v)
		}
		if v, ok := lens.ViewConfig["maxRecursionDepth"]; ok {
			maxRecursionDepth = parseInt(v, 2)
		}
	}
	if maxRecursionDepth < 0 {
		maxRecursionDepth = 0
	}

	// Увеличиваем activeSubscribers для всех HorizonFragment в scope
	for _, canonicalPath := range lens.Scope {
		fragment, err := s.fragmentsRepo.GetByCanonicalPath(ctx, canonicalPath)
		if err == nil && fragment != nil {
			if err := s.fragmentsRepo.IncrementActiveSubscribers(ctx, fragment.ID); err != nil {
				// Логируем ошибку, но не прерываем создание Lens
				_ = err
			}
			// Управляем жизненным циклом
			if err := s.lifecycle.EnsureActive(ctx, fragment.ID); err != nil {
				_ = err
			}
			// Синхронизируем WatchdogConfig (создать/запустить при ACTIVE)
			if s.watchdogHook != nil {
				s.watchdogHook(ctx, fragment.ID)
			}
		}

		// Создаем ScoutingJob для сборки/освежения данных (ScoutEngine выполнит freshness-check и быстро завершит,
		// если данные уже достаточно свежие). Это убирает расхождения между тестовым сценарием и ядром.
		if s.scoutingJobsRepo != nil {
			now := time.Now().UTC()
			state := map[string]interface{}{
				"targetCanonicalPath": canonicalPath,
				"lensId":              lensID,
				"currentState":        string(models.ScoutingJobStatePending),
				"scoutingStrategy":    string(models.ScoutingStrategySnapshot),
				// Recursion control (used by ScoutEngine.processDiscoveredRelations)
				"recursionDepth":     0.0,
				"maxRecursionDepth":  float64(maxRecursionDepth),
			}
			if len(requestedMetrics) > 0 {
				state["requestedMetrics"] = requestedMetrics
			}
			scoutingJob := &models.ScoutingJob{
				AgentJob: agentmodels.AgentJob{
					TenantID:  lens.TenantID,
					ProjectID: lens.ProjectID,
					Status:    agentmodels.AgentJobStatusPending,
					AgentID:   "scout",
					CreatedAt: now,
					UpdatedAt: now,
					State:     state,
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
			if _, createErr := s.scoutingJobsRepo.Create(ctx, scoutingJob); createErr != nil {
				return "", fmt.Errorf("failed to create scouting job for %s: %w", canonicalPath, createErr)
			}
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
				// Синхронизируем WatchdogConfig (паузить при DORMANT)
				if s.watchdogHook != nil {
					s.watchdogHook(ctx, fragment.ID)
				}
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
				// Синхронизируем WatchdogConfig (создать/запустить при ACTIVE)
				if s.watchdogHook != nil {
					s.watchdogHook(ctx, fragment.ID)
				}
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
			// Синхронизируем WatchdogConfig (паузить при DORMANT)
			if s.watchdogHook != nil {
				s.watchdogHook(ctx, fragment.ID)
			}
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

