package lens

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

// QueryRefreshJob периодически переразрешает QueryAddress в активных Lens
type QueryRefreshJob struct {
	lensesRepo    *repo.LensesRepository
	fragmentsRepo *repo.HorizonFragmentsRepository
	ontologyFactsRepo *corerepo.OntologyFactsRepository
	queryResolver *QueryResolver
	lifecycle     *LifecycleManager
	watchdogHook  func(ctx context.Context, fragmentID string)
	interval      time.Duration
}

// NewQueryRefreshJob создает новый QueryRefreshJob
func NewQueryRefreshJob(
	lensesRepo *repo.LensesRepository,
	fragmentsRepo *repo.HorizonFragmentsRepository,
	ontologyFactsRepo *corerepo.OntologyFactsRepository,
	lifecycle *LifecycleManager,
	interval time.Duration,
) *QueryRefreshJob {
	if interval == 0 {
		interval = 24 * time.Hour // Default: раз в день
	}

	return &QueryRefreshJob{
		lensesRepo:    lensesRepo,
		fragmentsRepo: fragmentsRepo,
		ontologyFactsRepo: ontologyFactsRepo,
		queryResolver: NewQueryResolver(fragmentsRepo, ontologyFactsRepo),
		lifecycle:     lifecycle,
		interval:      interval,
	}
}

// SetWatchdogHook устанавливает hook для синхронизации WatchdogConfig
func (j *QueryRefreshJob) SetWatchdogHook(hook func(ctx context.Context, fragmentID string)) {
	j.watchdogHook = hook
}

// Start запускает фоновый job
func (j *QueryRefreshJob) Start(ctx context.Context) {
	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()

	// Первый запуск сразу
	_ = j.refreshAll(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = j.refreshAll(ctx)
		}
	}
}

// refreshAll переразрешает все QueryAddress во всех активных Lens
func (j *QueryRefreshJob) refreshAll(ctx context.Context) error {
	// Получаем все активные Lens (без фильтра по project)
	// NOTE: ListByProject с пустыми tenantID/projectID вернет все Lens
	activeStatus := string(models.LensStatusActive)
	lenses, err := j.lensesRepo.ListByProject(ctx, "", "", &activeStatus, 1000)
	if err != nil {
		return fmt.Errorf("failed to list lenses: %w", err)
	}

	now := time.Now().UTC()
	refreshedCount := 0

	for _, lens := range lenses {
		if lens == nil || len(lens.Scope) == 0 {
			continue
		}

		// Разделяем Scope на статические пути и QueryAddress
		_, queryAddresses := splitScope(lens.Scope)
		
		if len(queryAddresses) == 0 {
			continue // Нет QueryAddress - пропускаем
		}

		// Переразрешаем все QueryAddress
		if lens.ResolvedQueries == nil {
			lens.ResolvedQueries = make(map[string][]string)
		}

		// Собираем старые resolved paths
		oldResolvedPaths := make(map[string]bool)
		for _, paths := range lens.ResolvedQueries {
			for _, path := range paths {
				oldResolvedPaths[path] = true
			}
		}

		// Резолвим QueryAddress с логированием
		resolvedQueries, resolveErrors := resolveQueryAddresses(ctx, j.queryResolver, lens.TenantID, queryAddresses)
		
		// Обновляем кэш
		for queryAddr, resolvedPaths := range resolvedQueries {
			lens.ResolvedQueries[queryAddr] = resolvedPaths
		}
		
		// Логируем ошибки резолва
		if len(resolveErrors) > 0 {
			log.Printf("[QueryRefreshJob] Lens %s: %d query address resolution errors", lens.ID, len(resolveErrors))
		}

		// Собираем новые resolved paths
		newResolvedPaths := make(map[string]bool)
		for _, paths := range resolvedQueries {
			for _, path := range paths {
				newResolvedPaths[path] = true
			}
		}

		// Управляем подписками: находим новые и удаленные paths
		addedPaths := make([]string, 0)
		removedPaths := make([]string, 0)
		
		for path := range newResolvedPaths {
			if !oldResolvedPaths[path] {
				addedPaths = append(addedPaths, path)
			}
		}
		
		for path := range oldResolvedPaths {
			if !newResolvedPaths[path] {
				removedPaths = append(removedPaths, path)
			}
		}

		// Обновляем timestamp
		lens.LastResolved = &now

		// Сохраняем Lens с обновленным кэшом
		if err := j.lensesRepo.Update(ctx, lens); err != nil {
			log.Printf("[QueryRefreshJob] Failed to update lens %s: %v", lens.ID, err)
			continue
		}

		// Управляем подписками для новых paths
		for _, canonicalPath := range addedPaths {
			fragment, err := j.fragmentsRepo.GetByCanonicalPath(ctx, canonicalPath)
			if err != nil || fragment == nil {
				log.Printf("[QueryRefreshJob] Failed to get fragment for path %s: %v", canonicalPath, err)
				continue
			}

			// Обновляем кэш ScopeFragments
			if lens.ScopeFragments == nil {
				lens.ScopeFragments = make(map[string]string)
			}
			lens.ScopeFragments[canonicalPath] = fragment.ID

			// Increment activeSubscribers
			if err := j.fragmentsRepo.IncrementActiveSubscribers(ctx, fragment.ID); err != nil {
				log.Printf("[QueryRefreshJob] Failed to increment subscribers for fragment %s: %v", fragment.ID, err)
			}

			// Управляем жизненным циклом
			if j.lifecycle != nil {
				if err := j.lifecycle.EnsureActive(ctx, fragment.ID); err != nil {
					log.Printf("[QueryRefreshJob] Failed to ensure active for fragment %s: %v", fragment.ID, err)
				}
			}

			// Синхронизируем WatchdogConfig
			if j.watchdogHook != nil {
				j.watchdogHook(ctx, fragment.ID)
			}
		}

		// Управляем подписками для удаленных paths
		for _, canonicalPath := range removedPaths {
			var fragmentID string
			if lens.ScopeFragments != nil {
				fragmentID = lens.ScopeFragments[canonicalPath]
			}
			
			if fragmentID == "" {
				// Пытаемся найти fragment по canonicalPath
				fragment, err := j.fragmentsRepo.GetByCanonicalPath(ctx, canonicalPath)
				if err == nil && fragment != nil {
					fragmentID = fragment.ID
				}
			}

			if fragmentID != "" {
				// Decrement activeSubscribers
				if err := j.fragmentsRepo.DecrementActiveSubscribers(ctx, fragmentID); err != nil {
					log.Printf("[QueryRefreshJob] Failed to decrement subscribers for fragment %s: %v", fragmentID, err)
				}

				// Проверяем и переводим в DORMANT при необходимости
				if j.lifecycle != nil {
					if err := j.lifecycle.CheckAndTransition(ctx, fragmentID); err != nil {
						log.Printf("[QueryRefreshJob] Failed to check transition for fragment %s: %v", fragmentID, err)
					}
				}

				// Удаляем из кэша
				if lens.ScopeFragments != nil {
					delete(lens.ScopeFragments, canonicalPath)
				}
			}
		}

		// Сохраняем обновленный кэш ScopeFragments
		if len(addedPaths) > 0 || len(removedPaths) > 0 {
			if err := j.lensesRepo.Update(ctx, lens); err != nil {
				log.Printf("[QueryRefreshJob] Failed to update lens %s after subscription changes: %v", lens.ID, err)
			} else {
				log.Printf("[QueryRefreshJob] Lens %s: added %d paths, removed %d paths", lens.ID, len(addedPaths), len(removedPaths))
			}
		}

		refreshedCount++
	}

	log.Printf("[QueryRefreshJob] Refreshed %d lenses", refreshedCount)
	return nil
}

// RefreshLens переразрешает QueryAddress для конкретного Lens
// Используется для ручного refresh или при явных изменениях в онтологии
func (j *QueryRefreshJob) RefreshLens(ctx context.Context, lensID string) error {
	lens, err := j.lensesRepo.Get(ctx, lensID)
	if err != nil {
		return fmt.Errorf("failed to get lens: %w", err)
	}

	_, queryAddresses := splitScope(lens.Scope)
	if len(queryAddresses) == 0 {
		return nil // Нет QueryAddress для резолва
	}

	if lens.ResolvedQueries == nil {
		lens.ResolvedQueries = make(map[string][]string)
	}

	// Собираем старые resolved paths
	oldResolvedPaths := make(map[string]bool)
	for _, paths := range lens.ResolvedQueries {
		for _, path := range paths {
			oldResolvedPaths[path] = true
		}
	}

	now := time.Now().UTC()

	// Резолвим QueryAddress с логированием
	resolvedQueries, resolveErrors := resolveQueryAddresses(ctx, j.queryResolver, lens.TenantID, queryAddresses)
	
	// Обновляем кэш
	for queryAddr, resolvedPaths := range resolvedQueries {
		lens.ResolvedQueries[queryAddr] = resolvedPaths
	}
	
	// Логируем ошибки резолва
	if len(resolveErrors) > 0 {
		log.Printf("[QueryRefreshJob] RefreshLens %s: %d query address resolution errors", lensID, len(resolveErrors))
	}

	// Собираем новые resolved paths
	newResolvedPaths := make(map[string]bool)
	for _, paths := range resolvedQueries {
		for _, path := range paths {
			newResolvedPaths[path] = true
		}
	}

	// Управляем подписками (аналогично refreshAll)
	addedPaths := make([]string, 0)
	removedPaths := make([]string, 0)
	
	for path := range newResolvedPaths {
		if !oldResolvedPaths[path] {
			addedPaths = append(addedPaths, path)
		}
	}
	
	for path := range oldResolvedPaths {
		if !newResolvedPaths[path] {
			removedPaths = append(removedPaths, path)
		}
	}

	lens.LastResolved = &now

	// Сохраняем обновленный кэш
	if err := j.lensesRepo.Update(ctx, lens); err != nil {
		return fmt.Errorf("failed to update lens: %w", err)
	}

	// Управляем подписками для новых paths
	for _, canonicalPath := range addedPaths {
		fragment, err := j.fragmentsRepo.GetByCanonicalPath(ctx, canonicalPath)
		if err != nil || fragment == nil {
			log.Printf("[QueryRefreshJob] Failed to get fragment for path %s: %v", canonicalPath, err)
			continue
		}

		if lens.ScopeFragments == nil {
			lens.ScopeFragments = make(map[string]string)
		}
		lens.ScopeFragments[canonicalPath] = fragment.ID

		if err := j.fragmentsRepo.IncrementActiveSubscribers(ctx, fragment.ID); err != nil {
			log.Printf("[QueryRefreshJob] Failed to increment subscribers for fragment %s: %v", fragment.ID, err)
		}

		if j.lifecycle != nil {
			if err := j.lifecycle.EnsureActive(ctx, fragment.ID); err != nil {
				log.Printf("[QueryRefreshJob] Failed to ensure active for fragment %s: %v", fragment.ID, err)
			}
		}

		if j.watchdogHook != nil {
			j.watchdogHook(ctx, fragment.ID)
		}
	}

	// Управляем подписками для удаленных paths
	for _, canonicalPath := range removedPaths {
		var fragmentID string
		if lens.ScopeFragments != nil {
			fragmentID = lens.ScopeFragments[canonicalPath]
		}
		
		if fragmentID == "" {
			fragment, err := j.fragmentsRepo.GetByCanonicalPath(ctx, canonicalPath)
			if err == nil && fragment != nil {
				fragmentID = fragment.ID
			}
		}

		if fragmentID != "" {
			if err := j.fragmentsRepo.DecrementActiveSubscribers(ctx, fragmentID); err != nil {
				log.Printf("[QueryRefreshJob] Failed to decrement subscribers for fragment %s: %v", fragmentID, err)
			}

			if j.lifecycle != nil {
				if err := j.lifecycle.CheckAndTransition(ctx, fragmentID); err != nil {
					log.Printf("[QueryRefreshJob] Failed to check transition for fragment %s: %v", fragmentID, err)
				}
			}

			if lens.ScopeFragments != nil {
				delete(lens.ScopeFragments, canonicalPath)
			}
		}
	}

	// Сохраняем обновленный кэш ScopeFragments
	if len(addedPaths) > 0 || len(removedPaths) > 0 {
		if err := j.lensesRepo.Update(ctx, lens); err != nil {
			return fmt.Errorf("failed to update lens after subscription changes: %w", err)
		}
		log.Printf("[QueryRefreshJob] RefreshLens %s: added %d paths, removed %d paths", lensID, len(addedPaths), len(removedPaths))
	}

	return nil
}
