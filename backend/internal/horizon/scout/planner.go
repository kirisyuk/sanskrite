package scout

import (
	"context"
	"fmt"
	"strings"
	"time"

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
	factsRepo          *repo.OntologyFactsRepository
	vertexSearchClient *ai.VertexAISearchClient
}

// NewScoutPlanner создает новый планировщик источников
func NewScoutPlanner(dataSourcesRepo *repo.DataSourcesRepository, factsRepo *repo.OntologyFactsRepository, vertexSearchClient *ai.VertexAISearchClient) *ScoutPlanner {
	return &ScoutPlanner{
		dataSourcesRepo:    dataSourcesRepo,
		factsRepo:          factsRepo,
		vertexSearchClient: vertexSearchClient,
	}
}

// PlanSources планирует источники для ScoutingJob
func (p *ScoutPlanner) PlanSources(ctx context.Context, tenantID, projectID string, targetCanonicalPath string, strategy horizonmodels.ScoutingStrategy, since *time.Time) (*SourcePlan, error) {
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

	// 3. Фильтруем релевантные Sources для целевого узла.
	// Приоритет: фактовый срез (Vertex AI Search по ontology_facts) -> Firestore facts -> legacy doc-based -> heuristic.
	relevantSources := p.filterRelevantSourcesByFacts(ctx, tenantID, projectSources, targetCanonicalPath, strategy, since)
	if relevantSources == nil {
		// legacy behavior: use doc search heuristic
		relevantSources = p.filterRelevantSources(ctx, projectSources, targetCanonicalPath, strategy)
	}

	return &SourcePlan{
		DataSourceIDs: extractDataSourceIDs(relevantSources),
		Strategy:      string(strategy),
		Sources:       relevantSources,
	}, nil
}

// filterRelevantSourcesByFacts selects sources referenced by recent ontology facts for the target canonicalPath.
// Returns:
// - []*models.DataSource{} (empty slice) if no facts found (signals ScoutEngine to go Research)
// - nil if it cannot run fact mode (so caller may fall back to legacy doc-based filtering)
func (p *ScoutPlanner) filterRelevantSourcesByFacts(
	ctx context.Context,
	tenantID string,
	allSources []*models.DataSource,
	targetCanonicalPath string,
	strategy horizonmodels.ScoutingStrategy,
	since *time.Time,
) []*models.DataSource {
	if strings.TrimSpace(targetCanonicalPath) == "" {
		return nil
	}

	// Prefer Vertex AI Search over ontology_facts (native) when configured.
	if p.vertexSearchClient != nil && p.vertexSearchClient.IsConfigured() {
		filterParts := []string{`structData.type: "ontology_fact"`}
		if strings.TrimSpace(tenantID) != "" {
			filterParts = append(filterParts, fmt.Sprintf(`structData.tenantId: "%s"`, strings.TrimSpace(tenantID)))
		}
		if since != nil {
			filterParts = append(filterParts, fmt.Sprintf("structData.updatedAtUnixMs >= %d", since.UTC().UnixMilli()))
		}
		req := &ai.SearchRequest{
			// Query uses vectorText/content (semantic), while canonicalPaths filter is not guaranteed in all DS schemas.
			Query:    strings.TrimSpace(targetCanonicalPath),
			PageSize: 30,
			Filter:   strings.Join(filterParts, " AND "),
		}
		resp, err := p.vertexSearchClient.Search(ctx, req)
		if err == nil && resp != nil {
			sourceIDs := make(map[string]struct{})
			for _, r := range resp.Results {
				sd := r.Document.StructData
				if sd == nil {
					continue
				}
				// Primary: sourceIds array
				if arr, ok := sd["sourceIds"].([]interface{}); ok {
					for _, it := range arr {
						if s, ok := it.(string); ok {
							s = strings.TrimSpace(s)
							if s != "" {
								sourceIDs[s] = struct{}{}
							}
						}
					}
				}
				// Secondary: single sourceId
				if s, ok := sd["sourceId"].(string); ok && strings.TrimSpace(s) != "" {
					sourceIDs[strings.TrimSpace(s)] = struct{}{}
				}
			}

			// If Vertex returned zero facts, treat as "no internal facts" -> trigger Research.
			if len(resp.Results) == 0 || len(sourceIDs) == 0 {
				return []*models.DataSource{}
			}

			return p.materializeSources(ctx, allSources, sourceIDs, strategy)
		}
		// If Vertex query fails, fall through to Firestore facts (if available).
	}

	// Firestore fallback: deterministic delta list over facts.
	if p.factsRepo != nil && since != nil {
		facts, err := p.factsRepo.ListUpdatedSinceByCanonicalPath(ctx, tenantID, strings.TrimSpace(targetCanonicalPath), since.UTC(), 200)
		if err == nil {
			sourceIDs := make(map[string]struct{})
			for _, f := range facts {
				if f == nil {
					continue
				}
				for _, sid := range f.SourceIDs {
					sid = strings.TrimSpace(sid)
					if sid != "" {
						sourceIDs[sid] = struct{}{}
					}
				}
			}
			if len(facts) == 0 || len(sourceIDs) == 0 {
				return []*models.DataSource{}
			}
			return p.materializeSources(ctx, allSources, sourceIDs, strategy)
		}
	}

	return nil
}

func (p *ScoutPlanner) materializeSources(ctx context.Context, allSources []*models.DataSource, ids map[string]struct{}, strategy horizonmodels.ScoutingStrategy) []*models.DataSource {
	// Filter already-loaded sources first
	out := make([]*models.DataSource, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, s := range allSources {
		if s == nil || s.ID == "" {
			continue
		}
		if _, ok := ids[s.ID]; ok {
			out = append(out, s)
			seen[s.ID] = struct{}{}
		}
	}
	// Fetch missing by ID (best effort)
	for id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		if p.dataSourcesRepo == nil {
			continue
		}
		if ds, err := p.dataSourcesRepo.Get(ctx, id); err == nil && ds != nil {
			out = append(out, ds)
		}
	}

	// Apply strategy cap (Snapshot keeps it small)
	if strategy == horizonmodels.ScoutingStrategySnapshot && len(out) > 5 {
		return out[:5]
	}
	return out
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

