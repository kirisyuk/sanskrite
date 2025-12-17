package lens

import (
	"context"
	"fmt"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// SubscriptionManager управляет подписками Lens на HorizonFragment
type SubscriptionManager struct {
	fragmentsRepo *repo.HorizonFragmentsRepository
}

// NewSubscriptionManager создает новый менеджер подписок
func NewSubscriptionManager(fragmentsRepo *repo.HorizonFragmentsRepository) *SubscriptionManager {
	return &SubscriptionManager{
		fragmentsRepo: fragmentsRepo,
	}
}

// Subscribe увеличивает счетчик подписчиков для HorizonFragment
func (m *SubscriptionManager) Subscribe(ctx context.Context, fragmentID string) error {
	return m.fragmentsRepo.IncrementActiveSubscribers(ctx, fragmentID)
}

// Unsubscribe уменьшает счетчик подписчиков для HorizonFragment
func (m *SubscriptionManager) Unsubscribe(ctx context.Context, fragmentID string) error {
	return m.fragmentsRepo.DecrementActiveSubscribers(ctx, fragmentID)
}

// GetSubscriberCount получает количество активных подписчиков
func (m *SubscriptionManager) GetSubscriberCount(ctx context.Context, fragmentID string) (int, error) {
	fragment, err := m.fragmentsRepo.Get(ctx, fragmentID)
	if err != nil {
		return 0, fmt.Errorf("failed to get fragment: %w", err)
	}
	return fragment.Governance.ActiveSubscribers, nil
}

