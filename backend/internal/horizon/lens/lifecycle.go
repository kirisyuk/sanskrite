package lens

import (
	"context"
	"fmt"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// LifecycleManager управляет жизненным циклом HorizonFragment
type LifecycleManager struct {
	fragmentsRepo *repo.HorizonFragmentsRepository
	dormantPeriod time.Duration // Период неактивности перед архивацией (по умолчанию 1 год)
}

// NewLifecycleManager создает новый менеджер жизненного цикла
func NewLifecycleManager(fragmentsRepo *repo.HorizonFragmentsRepository) *LifecycleManager {
	return &LifecycleManager{
		fragmentsRepo: fragmentsRepo,
		dormantPeriod: 365 * 24 * time.Hour, // 1 год по умолчанию
	}
}

// EnsureActive переводит HorizonFragment в состояние ACTIVE
func (m *LifecycleManager) EnsureActive(ctx context.Context, fragmentID string) error {
	fragment, err := m.fragmentsRepo.Get(ctx, fragmentID)
	if err != nil {
		return fmt.Errorf("failed to get fragment: %w", err)
	}

	if fragment.Governance.Status == string(models.FragmentStatusActive) {
		return nil // Уже активен
	}

	// Переводим в ACTIVE
	fragment.Governance.Status = string(models.FragmentStatusActive)
	return m.fragmentsRepo.UpdateGovernance(ctx, fragmentID, fragment.Governance)
}

// CheckAndTransition проверяет состояние и переводит в следующее при необходимости
func (m *LifecycleManager) CheckAndTransition(ctx context.Context, fragmentID string) error {
	fragment, err := m.fragmentsRepo.Get(ctx, fragmentID)
	if err != nil {
		return fmt.Errorf("failed to get fragment: %w", err)
	}

	// Если activeSubscribers = 0, переводим в DORMANT
	if fragment.Governance.ActiveSubscribers == 0 {
		if fragment.Governance.Status != string(models.FragmentStatusDormant) {
			fragment.Governance.Status = string(models.FragmentStatusDormant)
			return m.fragmentsRepo.UpdateGovernance(ctx, fragmentID, fragment.Governance)
		}
	}

	// Проверяем, нужно ли архивировать DORMANT фрагменты
	if fragment.Governance.Status == string(models.FragmentStatusDormant) {
		// Проверяем время последнего обновления
		timeSinceUpdate := time.Since(fragment.Metadata.LastUpdatedAt)
		if timeSinceUpdate > m.dormantPeriod {
			fragment.Governance.Status = string(models.FragmentStatusArchived)
			return m.fragmentsRepo.UpdateGovernance(ctx, fragmentID, fragment.Governance)
		}
	}

	return nil
}

// Reactivate реактивирует архивированный HorizonFragment
func (m *LifecycleManager) Reactivate(ctx context.Context, fragmentID string) error {
	fragment, err := m.fragmentsRepo.Get(ctx, fragmentID)
	if err != nil {
		return fmt.Errorf("failed to get fragment: %w", err)
	}

	if fragment.Governance.Status == string(models.FragmentStatusArchived) {
		// При реактивации требуется полная разведка
		// Пока просто переводим в DORMANT, разведка будет запущена отдельно
		fragment.Governance.Status = string(models.FragmentStatusDormant)
		return m.fragmentsRepo.UpdateGovernance(ctx, fragmentID, fragment.Governance)
	}

	return nil
}

// ProcessLifecycleTransitions обрабатывает переходы состояний для всех фрагментов
func (m *LifecycleManager) ProcessLifecycleTransitions(ctx context.Context, limit int) error {
	// Получаем DORMANT фрагменты
	dormantFragments, err := m.fragmentsRepo.ListByStatus(ctx, string(models.FragmentStatusDormant), limit)
	if err != nil {
		return fmt.Errorf("failed to list dormant fragments: %w", err)
	}

	now := time.Now()
	for _, fragment := range dormantFragments {
		timeSinceUpdate := now.Sub(fragment.Metadata.LastUpdatedAt)
		if timeSinceUpdate > m.dormantPeriod {
			fragment.Governance.Status = string(models.FragmentStatusArchived)
			if err := m.fragmentsRepo.UpdateGovernance(ctx, fragment.ID, fragment.Governance); err != nil {
				// Логируем ошибку, но продолжаем обработку
				_ = err
			}
		}
	}

	return nil
}

