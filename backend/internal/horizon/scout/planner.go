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
	DataSourceIDs []string             `json:"dataSourceIds"` // DataSource IDs из проекта
	Strategy      string               `json:"strategy"`      // "Snapshot" или "DeepDive"
	Sources       []*models.DataSource `json:"sources"`       // Полные объекты DataSource
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
		vertexSearchClient: horizonVertexClient(vertexSearchClient),
	}
}

// PlanSources планирует источники для ScoutingJob
// searchQuery is an optional free-form focus query (e.g. "battery logistics procurement") that is used
// for semantic retrieval (Vertex/doc search) but MUST NOT replace targetCanonicalPath as the key for
// facts/fragments.
func (p *ScoutPlanner) PlanSources(ctx context.Context, tenantID, projectID string, targetCanonicalPath string, strategy horizonmodels.ScoutingStrategy, since *time.Time, searchQuery string) (*SourcePlan, error) {
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
	// Приоритет: Firestore facts (immediate availability) -> Vertex AI Search (fallback for semantic search) -> legacy doc-based -> heuristic.
	relevantSources := p.filterRelevantSourcesByFacts(ctx, tenantID, projectSources, targetCanonicalPath, strategy, since, searchQuery)
	if relevantSources == nil {
		// legacy behavior: use doc search heuristic
		relevantSources = p.filterRelevantSources(ctx, projectSources, targetCanonicalPath, strategy, searchQuery)
	}

	return &SourcePlan{
		DataSourceIDs: extractDataSourceIDs(relevantSources),
		Strategy:      string(strategy),
		Sources:       relevantSources,
	}, nil
}

// filterRelevantSourcesByFacts selects sources referenced by ontology facts for the target canonicalPath.
//
// IMPORTANT:
//   - If we cannot confidently select sources from facts (no facts, no sourceIds, query failure),
//     we return nil so caller may fall back to doc-based planning or heuristic source selection.
//   - We intentionally avoid returning an empty slice to force Research, because internal documents
//     may exist even when facts are not yet present/indexed.
func (p *ScoutPlanner) filterRelevantSourcesByFacts(
	ctx context.Context,
	tenantID string,
	allSources []*models.DataSource,
	targetCanonicalPath string,
	strategy horizonmodels.ScoutingStrategy,
	since *time.Time,
	searchQuery string,
) []*models.DataSource {
	if strings.TrimSpace(targetCanonicalPath) == "" {
		return nil
	}
	q := strings.TrimSpace(searchQuery)
	if q == "" {
		q = strings.TrimSpace(targetCanonicalPath)
	}

	// PRIORITY 1: Firestore facts first (immediate availability, no indexing delay)
	// This ensures newly created facts are immediately available for planning.
	if p.factsRepo != nil {
		// If since is provided, use it; otherwise check all facts (use zero time to get all facts)
		var facts []*models.OntologyFact
		var err error
		if since != nil {
			facts, err = p.factsRepo.ListUpdatedSinceByCanonicalPath(ctx, tenantID, strings.TrimSpace(targetCanonicalPath), since.UTC(), 200)
		} else {
			// No since timestamp - check all facts for this canonicalPath (use zero time = beginning of epoch)
			facts, err = p.factsRepo.ListUpdatedSinceByCanonicalPath(ctx, tenantID, strings.TrimSpace(targetCanonicalPath), time.Time{}, 200)
		}
		if err == nil && len(facts) > 0 {
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
			if len(sourceIDs) > 0 {
				return p.materializeSources(ctx, allSources, sourceIDs, strategy)
			}
			// Facts exist but do not reference sources (or data is incomplete) -> fall back.
			return nil
		}
		// No facts -> fall back (do NOT force Research).
		if err == nil && len(facts) == 0 {
			return nil
		}
	}

	// PRIORITY 2: Vertex AI Search fallback (for semantic search when Firestore doesn't have facts)
	// Use Vertex AI Search only if Firestore didn't return facts, as it may have indexing delay.
	vertex := horizonVertexClient(p.vertexSearchClient)
	if vertex != nil && vertex.IsConfigured() {
		filterParts := []string{`structData.type: "ontology_fact"`}
		if strings.TrimSpace(tenantID) != "" {
			filterParts = append(filterParts, fmt.Sprintf(`structData.tenantId: "%s"`, strings.TrimSpace(tenantID)))
		}
		if since != nil {
			filterParts = append(filterParts, fmt.Sprintf("structData.updatedAtUnixMs >= %d", since.UTC().UnixMilli()))
		}
		req := &ai.SearchRequest{
			// Query uses vectorText/content (semantic), while canonicalPaths filter is not guaranteed in all DS schemas.
			Query:    q,
			PageSize: 30,
			Filter:   strings.Join(filterParts, " AND "),
		}
		resp, err := vertex.Search(ctx, req)
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

			// If Vertex returned zero facts, fall back (do NOT force Research).
			if len(resp.Results) == 0 || len(sourceIDs) == 0 {
				return nil
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
func (p *ScoutPlanner) filterRelevantSources(ctx context.Context, sources []*models.DataSource, targetCanonicalPath string, strategy horizonmodels.ScoutingStrategy, searchQuery string) []*models.DataSource {
	// Fallback: если клиент не настроен или нет источников, используем эвристику
	vertex := horizonVertexClient(p.vertexSearchClient)
	if vertex == nil || !vertex.IsConfigured() || len(sources) == 0 {
		return p.fallbackFiltering(sources, strategy)
	}

	q := strings.TrimSpace(searchQuery)
	if q == "" {
		q = strings.TrimSpace(targetCanonicalPath)
	}

	// Ищем релевантные документы в Vertex AI Search
	// Это найдет источники, в которых упоминается тема (targetCanonicalPath)
	req := &ai.SearchRequest{
		Query:    q,
		PageSize: 20, // Проверяем топ-20 документов
		// Можно добавить фильтр по типу документов, если нужно
	}

	resp, err := vertex.Search(ctx, req)
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
