package scout

import (
	"context"
	"fmt"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
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
	dataSourcesRepo    *repo.DataSourcesRepository
	vertexSearchClient *ai.VertexAISearchClient
}

// NewScoutPlanner создает новый планировщик источников
func NewScoutPlanner(dataSourcesRepo *repo.DataSourcesRepository, vertexSearchClient *ai.VertexAISearchClient) *ScoutPlanner {
	return &ScoutPlanner{
		dataSourcesRepo:    dataSourcesRepo,
		vertexSearchClient: vertexSearchClient,
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
	// Используем нативный Vertex AI Search для поиска релевантных источников
	relevantSources := p.filterRelevantSources(ctx, projectSources, targetCanonicalPath, strategy)

	return &SourcePlan{
		DataSourceIDs: extractDataSourceIDs(relevantSources),
		Strategy:      string(strategy),
		Sources:       relevantSources,
	}, nil
}

// filterRelevantSources фильтрует релевантные источники с помощью Vertex AI Search
func (p *ScoutPlanner) filterRelevantSources(ctx context.Context, sources []*models.DataSource, targetCanonicalPath string, strategy horizonmodels.ScoutingStrategy) []*models.DataSource {
	// Fallback: если клиент не настроен или нет источников, используем эвристику
	if p.vertexSearchClient == nil || !p.vertexSearchClient.IsConfigured() || len(sources) == 0 {
		return p.fallbackFiltering(sources, strategy)
	}

	// Ищем релевантные документы в Vertex AI Search
	// Это найдет источники, в которых упоминается тема (targetCanonicalPath)
	req := &ai.SearchRequest{
		Query:    targetCanonicalPath,
		PageSize: 20, // Проверяем топ-20 документов
		// Можно добавить фильтр по типу документов, если нужно
	}

	resp, err := p.vertexSearchClient.Search(ctx, req)
	if err != nil {
		// При ошибке используем fallback
		return p.fallbackFiltering(sources, strategy)
	}

	// Если поиск ничего не нашел, возвращаем пустой список.
	// Это сигнал для ScoutEngine, что внутренних данных нет и нужно запускать Research.
	if len(resp.Results) == 0 {
		return []*models.DataSource{}
	}

	// Собираем ID источников, которые выдали релевантные документы
	relevantSourceIDs := make(map[string]bool)
	for _, res := range resp.Results {
		if res.Document.StructData != nil {
			if sourceID, ok := res.Document.StructData["sourceId"].(string); ok && sourceID != "" {
				relevantSourceIDs[sourceID] = true
			}
		}
	}

	// Фильтруем входной список источников
	var filtered []*models.DataSource
	for _, s := range sources {
		if relevantSourceIDs[s.ID] {
			filtered = append(filtered, s)
		}
	}

	return filtered
}

// fallbackFiltering простая эвристика фильтрации (взять первые N)
func (p *ScoutPlanner) fallbackFiltering(sources []*models.DataSource, strategy horizonmodels.ScoutingStrategy) []*models.DataSource {
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

