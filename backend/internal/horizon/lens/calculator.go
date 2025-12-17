package lens

import (
	"context"
	"fmt"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// MetricsCalculator пересчитывает метрики для Lens
type MetricsCalculator struct {
	fragmentsRepo *repo.HorizonFragmentsRepository
}

// NewMetricsCalculator создает новый калькулятор метрик
func NewMetricsCalculator(fragmentsRepo *repo.HorizonFragmentsRepository) *MetricsCalculator {
	return &MetricsCalculator{
		fragmentsRepo: fragmentsRepo,
	}
}

// CalculateLensMetrics пересчитывает метрики для Lens на основе HorizonFragment в scope
func (c *MetricsCalculator) CalculateLensMetrics(ctx context.Context, lens *models.Lens) (map[string]interface{}, error) {
	// Получаем все HorizonFragment из scope
	fragments, err := c.fragmentsRepo.ListByCanonicalPaths(ctx, lens.Scope)
	if err != nil {
		return nil, fmt.Errorf("failed to list fragments: %w", err)
	}

	// Агрегируем метрики
	metrics := make(map[string]interface{})
	metricCounts := make(map[string]int)
	metricSums := make(map[string]float64)

	for _, fragment := range fragments {
		for metricName, metricValue := range fragment.State {
			metricCounts[metricName]++
			metricSums[metricName] += metricValue.Value
		}
	}

	// Вычисляем средние значения
	for metricName, count := range metricCounts {
		if count > 0 {
			metrics[metricName] = metricSums[metricName] / float64(count)
		}
	}

	// Добавляем общую информацию
	metrics["totalFragments"] = len(fragments)
	metrics["activeFragments"] = c.countActiveFragments(fragments)

	// Вычисляем общий риск-скор (если есть метрика operational_health)
	if avgHealth, ok := metrics["operational_health"].(float64); ok {
		metrics["totalRiskScore"] = 1.0 - avgHealth // Инвертируем: низкий operational_health = высокий риск
	}

	return metrics, nil
}

// countActiveFragments подсчитывает активные фрагменты
func (c *MetricsCalculator) countActiveFragments(fragments []*models.HorizonFragment) int {
	count := 0
	for _, fragment := range fragments {
		if fragment.Governance.Status == string(models.FragmentStatusActive) {
			count++
		}
	}
	return count
}

// FilterFragmentsByScope фильтрует HorizonFragment по scope Lens
func (c *MetricsCalculator) FilterFragmentsByScope(ctx context.Context, lens *models.Lens) ([]*models.HorizonFragment, error) {
	return c.fragmentsRepo.ListByCanonicalPaths(ctx, lens.Scope)
}

