package lens

import (
	"context"
	"fmt"
	"strings"

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
	// Support both legacy keys ("operational_health") and canonical metric paths ("/metric/risk/operational_health").
	if avgHealth, ok := metrics["operational_health"].(float64); ok {
		metrics["totalRiskScore"] = 1.0 - avgHealth // Инвертируем: низкий operational_health = высокий риск
	} else {
		// Look for canonical path key
		if v, ok2 := metrics["/metric/risk/operational_health"].(float64); ok2 {
			metrics["totalRiskScore"] = 1.0 - v
		} else {
			// As a fallback, find any metric whose base name matches operational_health.
			for k, vv := range metrics {
				parts := strings.Split(strings.Trim(k, "/"), "/")
				if len(parts) > 0 && parts[len(parts)-1] == "operational_health" {
					if f, ok4 := vv.(float64); ok4 {
						metrics["totalRiskScore"] = 1.0 - f
						break
					}
				}
			}
		}
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

