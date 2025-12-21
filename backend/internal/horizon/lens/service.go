package lens

import (
	"context"
	"fmt"
	"log"
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
	queryResolver    *QueryResolver
}

// NewLensService создает новый Lens Service
func NewLensService(
	lensesRepo *repo.LensesRepository,
	fragmentsRepo *repo.HorizonFragmentsRepository,
	scoutingJobsRepo *repo.ScoutingJobsRepository,
	lifecycle *LifecycleManager,
	calculator *MetricsCalculator,
) *LensService {
	queryResolver := NewQueryResolver(fragmentsRepo)
	return &LensService{
		lensesRepo:       lensesRepo,
		fragmentsRepo:    fragmentsRepo,
		scoutingJobsRepo: scoutingJobsRepo,
		lifecycle:        lifecycle,
		calculator:       calculator,
		queryResolver:    queryResolver,
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

	// Инициализируем кэш fragment IDs для оптимизации будущих операций
	if lens.ScopeFragments == nil {
		lens.ScopeFragments = make(map[string]string)
	}
	if lens.ResolvedQueries == nil {
		lens.ResolvedQueries = make(map[string][]string)
	}

	// Разделяем Scope на статические пути и QueryAddress
	staticPaths, queryAddresses := splitScope(lens.Scope)

	// Резолвим QueryAddress в canonical paths
	now := time.Now().UTC()
	resolvedQueries, resolveErrors := resolveQueryAddresses(ctx, s.queryResolver, lens.TenantID, queryAddresses)
	
	// Сохраняем resolved queries в кэш
	for queryAddr, resolvedPaths := range resolvedQueries {
		lens.ResolvedQueries[queryAddr] = resolvedPaths
		lens.LastResolved = &now
		// Добавляем resolved paths к static paths для последующей обработки
		staticPaths = append(staticPaths, resolvedPaths...)
	}
	
	// Логируем ошибки резолва (если есть)
	if len(resolveErrors) > 0 {
		log.Printf("[LensService] CreateLens: %d query address resolution errors (continuing with partial results)", len(resolveErrors))
	}

	// Увеличиваем activeSubscribers для всех HorizonFragment в scope
	for _, canonicalPath := range staticPaths {
		fragment, err := s.fragmentsRepo.GetByCanonicalPath(ctx, canonicalPath)
		if err == nil && fragment != nil {
			// Сохраняем в кэш для будущих операций
			lens.ScopeFragments[canonicalPath] = fragment.ID

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

	// Сохраняем обновленный кэш fragment IDs обратно в Firestore
	if len(lens.ScopeFragments) > 0 {
		_ = s.lensesRepo.Update(ctx, lens)
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

	// Инициализируем кэш если отсутствует
	if lens.ScopeFragments == nil {
		lens.ScopeFragments = make(map[string]string)
	}
	if lens.ResolvedQueries == nil {
		lens.ResolvedQueries = make(map[string][]string)
	}
	// Копируем существующий кэш из oldLens
	if oldLens.ScopeFragments != nil {
		for k, v := range oldLens.ScopeFragments {
			lens.ScopeFragments[k] = v
		}
	}
	if oldLens.ResolvedQueries != nil {
		for k, v := range oldLens.ResolvedQueries {
			lens.ResolvedQueries[k] = v
		}
	}

	// Разделяем новый Scope на статические пути и QueryAddress
	newStaticPaths, newQueryAddresses := splitScope(lens.Scope)

	// Разделяем старый Scope на статические пути и QueryAddress
	oldStaticPaths, oldQueryAddresses := splitScope(oldLens.Scope)

	// Резолвим новые QueryAddress
	now := time.Now().UTC()
	allNewStaticPaths := make([]string, len(newStaticPaths))
	copy(allNewStaticPaths, newStaticPaths)

	resolvedQueries, resolveErrors := resolveQueryAddresses(ctx, s.queryResolver, lens.TenantID, newQueryAddresses)
	
	// Обновляем кэш и добавляем resolved paths
	for queryAddr, resolvedPaths := range resolvedQueries {
		lens.ResolvedQueries[queryAddr] = resolvedPaths
		lens.LastResolved = &now
		allNewStaticPaths = append(allNewStaticPaths, resolvedPaths...)
	}
	
	// Логируем ошибки резолва (если есть)
	if len(resolveErrors) > 0 {
		log.Printf("[LensService] UpdateLens: %d query address resolution errors (continuing with partial results)", len(resolveErrors))
	}

	// Резолвим старые QueryAddress для корректного сравнения
	allOldStaticPaths := make([]string, len(oldStaticPaths))
	copy(allOldStaticPaths, oldStaticPaths)

	for _, queryAddr := range oldQueryAddresses {
		if oldPaths, ok := oldLens.ResolvedQueries[queryAddr]; ok {
			allOldStaticPaths = append(allOldStaticPaths, oldPaths...)
		}
	}

	// Удаляем QueryAddress, которые больше не используются
	for _, queryAddr := range oldQueryAddresses {
		stillUsed := false
		for _, newAddr := range newQueryAddresses {
			if newAddr == queryAddr {
				stillUsed = true
				break
			}
		}
		if !stillUsed {
			delete(lens.ResolvedQueries, queryAddr)
		}
	}

	// Создаем map новых paths для быстрого поиска
	newPathsMap := make(map[string]bool)
	for _, path := range allNewStaticPaths {
		newPathsMap[path] = true
	}

	// Отписываемся от удаленных из scope
	for _, path := range allOldStaticPaths {
		if !newPathsMap[path] {
			// ОПТИМИЗАЦИЯ: Используем кэш fragmentID если доступен
			var fragmentID string
			if oldLens.ScopeFragments != nil {
				fragmentID = oldLens.ScopeFragments[path]
			}

			if fragmentID != "" {
				// Быстрый путь: используем cached fragment ID (прямой key lookup)
				_ = s.fragmentsRepo.DecrementActiveSubscribers(ctx, fragmentID)
				_ = s.lifecycle.CheckAndTransition(ctx, fragmentID)
				if s.watchdogHook != nil {
					s.watchdogHook(ctx, fragmentID)
				}
			} else {
				// Fallback: composite index query
				fragment, err := s.fragmentsRepo.GetByCanonicalPath(ctx, path)
				if err == nil && fragment != nil {
					_ = s.fragmentsRepo.DecrementActiveSubscribers(ctx, fragment.ID)
					_ = s.lifecycle.CheckAndTransition(ctx, fragment.ID)
					if s.watchdogHook != nil {
						s.watchdogHook(ctx, fragment.ID)
					}
				}
			}

			// Удаляем из кэша
			delete(lens.ScopeFragments, path)
		}
	}

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

	// Создаем map для быстрой проверки старых canonical paths
	oldPathsMap := make(map[string]bool)
	for _, path := range allOldStaticPaths {
		oldPathsMap[path] = true
	}

	// Подписываемся на новые в scope
	for _, path := range allNewStaticPaths {
		if !oldPathsMap[path] {
			fragment, err := s.fragmentsRepo.GetByCanonicalPath(ctx, path)
			if err == nil && fragment != nil {
				// Обновляем кэш для нового узла
				lens.ScopeFragments[path] = fragment.ID

				_ = s.fragmentsRepo.IncrementActiveSubscribers(ctx, fragment.ID)
				_ = s.lifecycle.EnsureActive(ctx, fragment.ID)
				// Синхронизируем WatchdogConfig (создать/запустить при ACTIVE)
				if s.watchdogHook != nil {
					s.watchdogHook(ctx, fragment.ID)
				}
			}

			// Создаем ScoutingJob для нового узла в scope (аналогично CreateLens)
			// Это гарантирует, что данные для нового узла будут собраны немедленно,
			// а не ждать первого Watchdog scan (который может быть через 1 час)
			if s.scoutingJobsRepo != nil {
				now := time.Now().UTC()
				state := map[string]interface{}{
					"targetCanonicalPath": path,
					"lensId":              lens.ID,
					"currentState":        string(models.ScoutingJobStatePending),
					"scoutingStrategy":    string(models.ScoutingStrategySnapshot),
					// Recursion control (used by ScoutEngine.processDiscoveredRelations)
					"recursionDepth":    0.0,
					"maxRecursionDepth": float64(maxRecursionDepth),
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
					LensID:              lens.ID,
					TargetCanonicalPath: path,
					ScoutingStrategy:    string(models.ScoutingStrategySnapshot),
					BillingContext: models.BillingContext{
						ChargeModel: string(models.ChargeModelBounty),
						ProjectID:   lens.ProjectID,
					},
					CurrentState: models.ScoutingJobStatePending,
				}
				// Создаем job, но не прерываем UpdateLens при ошибке
				_, _ = s.scoutingJobsRepo.Create(ctx, scoutingJob)
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
		// ОПТИМИЗАЦИЯ: Используем кэш fragmentID если доступен
		var fragmentID string
		if lens.ScopeFragments != nil {
			fragmentID = lens.ScopeFragments[canonicalPath]
		}

		if fragmentID != "" {
			// Быстрый путь: используем cached fragment ID (прямой key lookup)
			_ = s.fragmentsRepo.DecrementActiveSubscribers(ctx, fragmentID)
			_ = s.lifecycle.CheckAndTransition(ctx, fragmentID)
			if s.watchdogHook != nil {
				s.watchdogHook(ctx, fragmentID)
			}
		} else {
			// Fallback: composite index query
			fragment, err := s.fragmentsRepo.GetByCanonicalPath(ctx, canonicalPath)
			if err == nil && fragment != nil {
				_ = s.fragmentsRepo.DecrementActiveSubscribers(ctx, fragment.ID)
				_ = s.lifecycle.CheckAndTransition(ctx, fragment.ID)
				if s.watchdogHook != nil {
					s.watchdogHook(ctx, fragment.ID)
				}
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

// DeleteLensesByProject удаляет все Lens для проекта с правильной обработкой подписок
func (s *LensService) DeleteLensesByProject(ctx context.Context, tenantID, projectID string) error {
	// Получаем все линзы проекта (без ограничения по статусу)
	allStatus := ""
	lenses, err := s.lensesRepo.ListByProject(ctx, tenantID, projectID, &allStatus, 1000)
	if err != nil {
		return fmt.Errorf("failed to list lenses for project %s: %w", projectID, err)
	}

	// Удаляем каждую линзу через DeleteLens для правильной обработки подписок
	var errors []string
	for _, lens := range lenses {
		if err := s.DeleteLens(ctx, lens.ID); err != nil {
			errors = append(errors, fmt.Sprintf("failed to delete lens %s: %v", lens.ID, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("errors deleting lenses: %s", strings.Join(errors, "; "))
	}

	return nil
}

// CalculateMetrics пересчитывает метрики для Lens
func (s *LensService) CalculateMetrics(ctx context.Context, lensID string) (map[string]interface{}, error) {
	lens, err := s.lensesRepo.Get(ctx, lensID)
	if err != nil {
		return nil, fmt.Errorf("failed to get lens: %w", err)
	}

	return s.calculator.CalculateLensMetrics(ctx, lens)
}

