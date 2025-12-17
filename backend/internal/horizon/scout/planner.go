package scout

import (
	"context"
	"fmt"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// SourcePlan план источников для ScoutingJob
type SourcePlan struct {
	DataSourceIDs []string              `json:"dataSourceIds"` // DataSource IDs из проекта
	Strategy      string                `json:"strategy"`       // "Snapshot" или "DeepDive"
	Sources       []*models.DataSource  `json:"sources"`       // Полные объекты DataSource
}

// ScoutPlanner планирует источники данных для ScoutingJob
type ScoutPlanner struct {
	dataSourcesRepo *repo.DataSourcesRepository
}

// NewScoutPlanner создает новый планировщик источников
func NewScoutPlanner(dataSourcesRepo *repo.DataSourcesRepository) *ScoutPlanner {
	return &ScoutPlanner{
		dataSourcesRepo: dataSourcesRepo,
	}
}

// PlanSources планирует источники для ScoutingJob
func (p *ScoutPlanner) PlanSources(ctx context.Context, tenantID, projectID string, targetCanonicalPath string, strategy horizonmodels.ScoutingStrategy) (*SourcePlan, error) {
	// 1. Получаем Sources проекта
	projectSources, err := p.dataSourcesRepo.ListByProject(ctx, tenantID, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to list project sources: %w", err)
	}

	// 2. Получаем глобальные Sources (web, rss, telegram_channel)
	globalSources, err := p.dataSourcesRepo.ListByKinds(ctx, []string{"web", "rss", "telegram_channel"})
	if err == nil && len(globalSources) > 0 {
		projectSources = append(projectSources, globalSources...)
	}

	// 3. Фильтруем релевантные Sources для целевого узла
	// В реальной реализации здесь будет семантический поиск или фильтрация по домену
	relevantSources := p.filterRelevantSources(projectSources, targetCanonicalPath, strategy)

	return &SourcePlan{
		DataSourceIDs: extractDataSourceIDs(relevantSources),
		Strategy:      string(strategy),
		Sources:       relevantSources,
	}, nil
}

// filterRelevantSources фильтрует релевантные источники
// В MVP используем простую фильтрацию, в будущем добавим семантический поиск
func (p *ScoutPlanner) filterRelevantSources(sources []*models.DataSource, targetCanonicalPath string, strategy horizonmodels.ScoutingStrategy) []*models.DataSource {
	// Простая фильтрация: для Snapshot берем первые 3 источника, для DeepDive - все
	// В реальной реализации здесь будет семантический поиск по vectorText источников
	if strategy == horizonmodels.ScoutingStrategySnapshot {
		if len(sources) > 3 {
			return sources[:3]
		}
		return sources
	}
	// DeepDive: используем все источники
	return sources
}

// extractDataSourceIDs извлекает ID из источников
func extractDataSourceIDs(sources []*models.DataSource) []string {
	ids := make([]string, 0, len(sources))
	for _, s := range sources {
		if s != nil && s.ID != "" {
			ids = append(ids, s.ID)
		}
	}
	return ids
}

