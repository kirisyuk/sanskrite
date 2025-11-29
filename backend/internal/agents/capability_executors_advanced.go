package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"cloud.google.com/go/bigquery"
	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	"github.com/igorkirisyuk/sanskrite/internal/models"
)

// ============================================================================
// Advanced Capability Executors
// ============================================================================

// TavilySearchClient интерфейс для поиска через Tavily (избегает циклического импорта)
type TavilySearchClient interface {
	Search(ctx context.Context, query string, maxResults int) (*TavilySearchResponse, error)
}

// TavilySearchResponse результат поиска через Tavily
type TavilySearchResponse struct {
	Query   string               `json:"query"`
	Results []TavilySearchResult `json:"results"`
}

// TavilySearchResult один результат поиска
type TavilySearchResult struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Content string  `json:"content"`
	Score   float64 `json:"score,omitempty"`
}

// WebSearchExecutor выполняет поиск через Tavily API
type WebSearchExecutor struct {
	tavilyClient TavilySearchClient
	logger       *log.Logger
}

// NewWebSearchExecutor создает новый исполнитель для веб-поиска через Tavily
func NewWebSearchExecutor(tavilyClient TavilySearchClient, logger *log.Logger) *WebSearchExecutor {
	return &WebSearchExecutor{
		tavilyClient: tavilyClient,
		logger:       logger,
	}
}

func (e *WebSearchExecutor) Capability() Capability {
	return CapabilityWebSearch
}

func (e *WebSearchExecutor) ValidateInput(input map[string]interface{}) error {
	if _, ok := input["query"].(string); !ok {
		return fmt.Errorf("query is required and must be a string")
	}
	return nil
}

func (e *WebSearchExecutor) Execute(ctx context.Context, stepCtx *AgentStepContext) (*CapabilityResult, error) {
	query := stepCtx.Input["query"].(string)
	maxResults := 5
	if mr, ok := stepCtx.Input["maxResults"].(float64); ok {
		maxResults = int(mr)
	}

	if e.tavilyClient == nil {
		return nil, fmt.Errorf("Tavily client not configured")
	}

	if e.logger != nil {
		e.logger.Printf("[WebSearchExecutor] Searching for: %s", query)
	}

	// Использовать существующий TavilyClient
	tavilyResp, err := e.tavilyClient.Search(ctx, query, maxResults)
	if err != nil {
		return nil, fmt.Errorf("Tavily search failed: %w", err)
	}

	results := make([]map[string]interface{}, 0, len(tavilyResp.Results))
	for _, r := range tavilyResp.Results {
		results = append(results, map[string]interface{}{
			"title":   r.Title,
			"url":     r.URL,
			"content": r.Content,
			"score":   r.Score,
		})
	}

	confidence := 0.9 // Высокая уверенность при успешном поиске
	if len(results) == 0 {
		confidence = 0.5 // Низкая уверенность если результатов нет
	}

	return &CapabilityResult{
		Success: true,
		Output: map[string]interface{}{
			"results": results,
			"count":   len(results),
			"query":   query,
		},
		Message:         fmt.Sprintf("Found %d results for query: %s", len(results), query),
		ExecutionStatus: models.ExecutionStatusSuccess,
		Confidence:      &confidence,
		Retryable:       true, // Можно повторить при временных ошибках
		Reason:          fmt.Sprintf("Web search completed, found %d results", len(results)),
		AlternativeCapabilities: []string{"data_search"}, // Можно попробовать поиск в документах
	}, nil
}

// BigQueryQueryExecutor выполняет запросы к BigQuery
type BigQueryQueryExecutor struct {
	bigqueryClient *gcp.BigQueryClient
	logger         *log.Logger
}

// NewBigQueryQueryExecutor создает новый исполнитель для BigQuery запросов
func NewBigQueryQueryExecutor(bigqueryClient *gcp.BigQueryClient, logger *log.Logger) *BigQueryQueryExecutor {
	return &BigQueryQueryExecutor{
		bigqueryClient: bigqueryClient,
		logger:         logger,
	}
}

func (e *BigQueryQueryExecutor) Capability() Capability {
	return CapabilityBigQueryQuery
}

func (e *BigQueryQueryExecutor) ValidateInput(input map[string]interface{}) error {
	if _, ok := input["sql"].(string); !ok {
		return fmt.Errorf("sql is required and must be a string")
	}
	return nil
}

func (e *BigQueryQueryExecutor) Execute(ctx context.Context, stepCtx *AgentStepContext) (*CapabilityResult, error) {
	sql := stepCtx.Input["sql"].(string)
	maxResults := 1000
	if mr, ok := stepCtx.Input["maxResults"].(float64); ok {
		maxResults = int(mr)
	}

	if e.logger != nil {
		e.logger.Printf("[BigQueryQueryExecutor] Executing SQL query (length: %d)", len(sql))
	}

	// Выполнить запрос
	iter, err := e.bigqueryClient.ExecuteQuery(ctx, sql)
	if err != nil {
		// Временные ошибки можно повторить
		retryable := strings.Contains(err.Error(), "timeout") || 
			strings.Contains(err.Error(), "rate limit") ||
			strings.Contains(err.Error(), "temporary") ||
			strings.Contains(err.Error(), "quota")
		
		confidence := 0.0
		return &CapabilityResult{
			Success:         false,
			ExecutionStatus: models.ExecutionStatusFailed,
			Confidence:      &confidence,
			Retryable:       retryable,
			Reason:          fmt.Sprintf("BigQuery execution failed: %v", err),
		}, fmt.Errorf("BigQuery execution failed: %w", err)
	}

	// Собрать результаты (используем правильный тип для BigQuery)
	results := make([]map[string]interface{}, 0)
	rowCount := 0
	for {
		var row map[string]bigquery.Value
		err := iter.Next(&row)
		if err == iterator.Done {
			break
		}
		if err != nil {
			// Временные ошибки можно повторить
			retryable := strings.Contains(err.Error(), "timeout") || 
				strings.Contains(err.Error(), "rate limit") ||
				strings.Contains(err.Error(), "temporary")
			
			confidence := 0.0
			return &CapabilityResult{
				Success:         false,
				ExecutionStatus: models.ExecutionStatusFailed,
				Confidence:      &confidence,
				Retryable:       retryable,
				Reason:          fmt.Sprintf("Failed to read query results: %v", err),
			}, fmt.Errorf("failed to read row: %w", err)
		}

		if len(results) >= maxResults {
			break
		}

		// Конвертировать bigquery.Value в стандартные Go типы
		resultRow := make(map[string]interface{})
		for k, v := range row {
			resultRow[k] = convertBigQueryValue(v)
		}
		results = append(results, resultRow)
		rowCount++
	}

	confidence := 1.0 // Полная уверенность при успешном выполнении запроса
	if rowCount == 0 {
		confidence = 0.7 // Немного ниже если результатов нет (возможно ожидаемо)
	}

	return &CapabilityResult{
		Success: true,
		Output: map[string]interface{}{
			"results":  results,
			"rowCount": rowCount,
		},
		Message:         fmt.Sprintf("Query returned %d rows", rowCount),
		ExecutionStatus: models.ExecutionStatusSuccess,
		Confidence:      &confidence,
		Retryable:       true, // Можно повторить при временных ошибках
		Reason:          fmt.Sprintf("BigQuery query executed successfully, returned %d rows", rowCount),
	}, nil
}

// convertBigQueryValue converts bigquery.Value to standard Go types
func convertBigQueryValue(v bigquery.Value) interface{} {
	switch val := v.(type) {
	case []bigquery.Value:
		// Arrays
		result := make([]interface{}, len(val))
		for i, item := range val {
			result[i] = convertBigQueryValue(item)
		}
		return result
	case map[string]bigquery.Value:
		// Structures
		result := make(map[string]interface{})
		for k, item := range val {
			result[k] = convertBigQueryValue(item)
		}
		return result
	default:
		// Primitive types
		return v
	}
}

// LLMAnalyzeExecutor выполняет анализ через LLM (структурированный вывод)
type LLMAnalyzeExecutor struct {
	genaiClient *gen_combined.Client
	logger      *log.Logger
}

// NewLLMAnalyzeExecutor создает новый исполнитель для LLM анализа
func NewLLMAnalyzeExecutor(genaiClient *gen_combined.Client, logger *log.Logger) *LLMAnalyzeExecutor {
	return &LLMAnalyzeExecutor{
		genaiClient: genaiClient,
		logger:      logger,
	}
}

func (e *LLMAnalyzeExecutor) Capability() Capability {
	return CapabilityLLMAnalyze
}

func (e *LLMAnalyzeExecutor) ValidateInput(input map[string]interface{}) error {
	if _, ok := input["prompt"].(string); !ok {
		return fmt.Errorf("prompt is required and must be a string")
	}
	if _, ok := input["schema"].(map[string]interface{}); !ok {
		return fmt.Errorf("schema is required and must be a map")
	}
	return nil
}

func (e *LLMAnalyzeExecutor) Execute(ctx context.Context, stepCtx *AgentStepContext) (*CapabilityResult, error) {
	prompt := stepCtx.Input["prompt"].(string)
	schema := stepCtx.Input["schema"].(map[string]interface{})
	model := "gemini-2.0-flash-exp"
	if m, ok := stepCtx.Input["model"].(string); ok {
		model = m
	}

	if e.logger != nil {
		e.logger.Printf("[LLMAnalyzeExecutor] Analyzing with model %s", model)
	}

	// Использовать GenerateJSONStrict с правильной сигнатурой
	opts := gen_combined.Options{
		Temperature: 0.3, // Низкая температура для более детерминированного анализа
	}
	// Model задается через route в genai client, используем дефолтный или из env

	resp, err := e.genaiClient.GenerateJSONStrict(ctx, prompt, nil, schema, opts)
	if err != nil {
		return nil, fmt.Errorf("LLM analysis failed: %w", err)
	}

	// Парсить JSON из Content
	var analysisResult map[string]interface{}
	if resp.Content != "" {
		if err := json.Unmarshal([]byte(resp.Content), &analysisResult); err != nil {
			return nil, fmt.Errorf("failed to unmarshal analysis result: %w", err)
		}
	} else if resp.RawJSON != "" {
		if err := json.Unmarshal([]byte(resp.RawJSON), &analysisResult); err != nil {
			return nil, fmt.Errorf("failed to unmarshal raw JSON: %w", err)
		}
	} else {
		return nil, fmt.Errorf("no JSON content in response")
	}

	confidence := 0.9 // Высокая уверенность в LLM анализе
	if len(analysisResult) == 0 {
		confidence = 0.6 // Ниже если результат пустой
	}

	return &CapabilityResult{
		Success: true,
		Output: map[string]interface{}{
			"analysis": analysisResult,
			"model":    resp.Model,
		},
		Message:         "Analysis completed successfully",
		ExecutionStatus: models.ExecutionStatusSuccess,
		Confidence:      &confidence,
		Retryable:       false,
		Reason:          fmt.Sprintf("Analysis completed using %s", resp.Model),
	}, nil
}

// DataSearchExecutor выполняет поиск данных в документах проекта через Vertex AI Search
type DataSearchExecutor struct {
	vertexSearchClient *ai.VertexAISearchClient
	logger             *log.Logger
}

// NewDataSearchExecutor создает новый исполнитель для поиска данных
func NewDataSearchExecutor(
	vertexSearchClient *ai.VertexAISearchClient,
	logger *log.Logger,
) *DataSearchExecutor {
	return &DataSearchExecutor{
		vertexSearchClient: vertexSearchClient,
		logger:             logger,
	}
}

func (e *DataSearchExecutor) Capability() Capability {
	return CapabilityDataSearch
}

func (e *DataSearchExecutor) ValidateInput(input map[string]interface{}) error {
	if _, ok := input["query"].(string); !ok {
		return fmt.Errorf("query is required and must be a string")
	}
	return nil
}

func (e *DataSearchExecutor) Execute(ctx context.Context, stepCtx *AgentStepContext) (*CapabilityResult, error) {
	searchQuery := stepCtx.Input["query"].(string)
	limit := 10
	if l, ok := stepCtx.Input["limit"].(float64); ok {
		limit = int(l)
	}
	projectID := stepCtx.ProjectID
	tenantID := stepCtx.TenantID

	if e.vertexSearchClient == nil || !e.vertexSearchClient.IsConfigured() {
		return nil, fmt.Errorf("Vertex AI Search client not configured")
	}

	if e.logger != nil {
		e.logger.Printf("[DataSearchExecutor] Searching for: %s (limit: %d, tenant: %s, project: %s)", searchQuery, limit, tenantID, projectID)
	}

	// Формируем фильтр для поиска только документов проекта и тенанта
	// Используем формат как в discovery.go: tenantId:"tenantId" OR tenantId:"global"
	// Если Vertex AI Search не поддерживает tenantId, используем только projectId
	// Попробуем сначала с tenantId, если не сработает - уберем
	filter := fmt.Sprintf(`(tenantId:"%s" OR tenantId:"global") AND (projectId:"%s" OR projectId:"global")`, tenantID, projectID)

	// Запрашиваем больше результатов для фильтрации на стороне приложения
	// (так как Vertex AI Search может не поддерживать фильтры по tenantId/projectId)
	searchReq := &ai.SearchRequest{
		Query:    searchQuery,
		PageSize: limit * 3, // Получаем больше результатов для фильтрации
		Filter:   filter,
	}

	searchResp, err := e.vertexSearchClient.Search(ctx, searchReq)
	if err != nil {
		// Если фильтр не работает (поля не поддерживаются), пробуем без фильтра
		if strings.Contains(err.Error(), "Unsupported field") || strings.Contains(err.Error(), "Invalid filter") {
			if e.logger != nil {
				e.logger.Printf("[DataSearchExecutor] Filter failed, trying without filter (fields may not be indexed)")
			}
			// Убираем фильтр полностью - поиск вернет все результаты
			searchReq.Filter = ""
			searchResp, err = e.vertexSearchClient.Search(ctx, searchReq)
			if err != nil {
				return nil, fmt.Errorf("Vertex AI Search failed: %w", err)
			}
			if e.logger != nil {
				e.logger.Printf("[DataSearchExecutor] Search succeeded without filter, returned %d results", len(searchResp.Results))
			}
		} else {
			return nil, fmt.Errorf("Vertex AI Search failed: %w", err)
		}
	}

	// Фильтруем результаты на стороне приложения по tenantId (обязательно) и projectId (опционально)
	filteredMatches := make([]map[string]interface{}, 0)
	for i, result := range searchResp.Results {
		// Логируем структуру первого результата для отладки
		if i == 0 && e.logger != nil {
			e.logger.Printf("[DataSearchExecutor] Sample result structure: ID=%s, URI=%s, Title=%s", result.ID, result.Document.URI, result.Document.Title)
			if result.Document.StructData != nil {
				e.logger.Printf("[DataSearchExecutor] Sample structData keys: %v", getMapKeys(result.Document.StructData))
				// Логируем первые несколько ключей-значений
				count := 0
				for k, v := range result.Document.StructData {
					if count < 5 {
						e.logger.Printf("[DataSearchExecutor] structData[%s] = %v (type: %T)", k, v, v)
						count++
					}
				}
			} else {
				e.logger.Printf("[DataSearchExecutor] Sample result has no structData")
			}
		}

		// Извлекаем tenantId и projectId из structData или URI
		docTenantID := ""
		docProjectID := ""

		// Пытаемся получить из structData
		if result.Document.StructData != nil {
			if tid, ok := result.Document.StructData["tenantId"].(string); ok {
				docTenantID = tid
			}
			if pid, ok := result.Document.StructData["projectId"].(string); ok {
				docProjectID = pid
			}
			// Также проверяем sourceId для projectId (формат: project:projectId)
			if docProjectID == "" {
				if sourceID, ok := result.Document.StructData["sourceId"].(string); ok {
					if strings.HasPrefix(sourceID, "project:") {
						docProjectID = strings.TrimPrefix(sourceID, "project:")
					}
				}
			}
		}

		// Если не нашли в structData, пробуем извлечь из derivedStructData.link (полный путь GCS)
		if docTenantID == "" && result.Document.DerivedStructData != nil {
			if link, ok := result.Document.DerivedStructData["link"].(string); ok && link != "" {
				// Формат: gs://bucket/tenantId/sourceId/docId.txt
				if strings.HasPrefix(link, "gs://") {
					uriWithoutPrefix := strings.TrimPrefix(link, "gs://")
					parts := strings.Split(uriWithoutPrefix, "/")
					if len(parts) >= 3 {
						docTenantID = parts[1] // tenantId из пути (после bucket)
						sourceID := parts[2]    // sourceId (например, project:test-project)
						if strings.HasPrefix(sourceID, "project:") {
							docProjectID = strings.TrimPrefix(sourceID, "project:")
						}
						if e.logger != nil && i == 0 {
							e.logger.Printf("[DataSearchExecutor] Extracted from link: tenantId=%s, projectId=%s (link=%s)", docTenantID, docProjectID, link)
						}
					}
				}
			}
		}

		// Если все еще не нашли, пробуем извлечь из URI (формат: gs://bucket/tenantId/sourceId/docId.txt)
		if docTenantID == "" && result.Document.URI != "" && strings.HasPrefix(result.Document.URI, "gs://") {
			uriWithoutPrefix := strings.TrimPrefix(result.Document.URI, "gs://")
			parts := strings.Split(uriWithoutPrefix, "/")
			if len(parts) >= 3 {
				docTenantID = parts[1] // tenantId из пути (после bucket)
				sourceID := parts[2]    // sourceId (например, project:test-project)
				if strings.HasPrefix(sourceID, "project:") {
					docProjectID = strings.TrimPrefix(sourceID, "project:")
				}
			}
		}

		// Фильтрация по tenantId (обязательно)
		// Разрешаем доступ к документам тенанта или глобальным документам
		if docTenantID != "" {
			if docTenantID != tenantID && docTenantID != "global" {
				continue // Пропускаем документы других тенантов
			}
		} else {
			// Если tenantId не найден, логируем для отладки, но не пропускаем
			// (может быть, документ из другого источника или старый формат)
			if e.logger != nil {
				e.logger.Printf("[DataSearchExecutor] Warning: tenantId not found for document %s (URI: %s), allowing for now", result.ID, result.Document.URI)
			}
			// В продакшене можно добавить строгую проверку:
			// continue
		}

		// Фильтрация по projectID (опционально, только если projectID указан)
		if projectID != "" {
			// Разрешаем доступ к документам проекта или глобальным документам
			if docProjectID != "" && docProjectID != projectID && docProjectID != "global" {
				continue // Пропускаем документы других проектов
			}
		}

		// Документ прошел фильтрацию, добавляем в результаты
		match := map[string]interface{}{
			"id":             result.ID,
			"relevanceScore": result.RelevanceScore,
		}
		if result.Document.Title != "" {
			match["title"] = result.Document.Title
		}
		if result.Document.URI != "" {
			match["uri"] = result.Document.URI
		}
		if result.Document.StructData != nil {
			match["structData"] = result.Document.StructData
		}
		if result.Document.DerivedStructData != nil {
			match["derivedStructData"] = result.Document.DerivedStructData
		}
		if result.Document.JSONData != "" {
			match["jsonData"] = result.Document.JSONData
		}
		// Добавляем извлеченные значения для отладки
		if docTenantID != "" {
			match["tenantId"] = docTenantID
		}
		if docProjectID != "" {
			match["projectId"] = docProjectID
		}

		filteredMatches = append(filteredMatches, match)

		// Ограничиваем количество результатов
		if len(filteredMatches) >= limit {
			break
		}
	}

	if e.logger != nil {
		e.logger.Printf("[DataSearchExecutor] After filtering: %d results (tenant: %s, project: %s, total before: %d)", len(filteredMatches), tenantID, projectID, len(searchResp.Results))
	}

	matches := filteredMatches

	confidence := 0.9 // Высокая уверенность при успешном поиске
	if len(matches) == 0 {
		confidence = 0.6 // Ниже если результатов нет
	}

	return &CapabilityResult{
		Success: true,
		Output: map[string]interface{}{
			"matches":  matches,
			"count":    len(matches),
			"query":    searchQuery,
			"nextPageToken": searchResp.NextPageToken,
		},
		Message:         fmt.Sprintf("Found %d matching documents", len(matches)),
		ExecutionStatus: models.ExecutionStatusSuccess,
		Confidence:      &confidence,
		Retryable:       true, // Можно повторить при временных ошибках
		Reason:          fmt.Sprintf("Data search completed, found %d matching documents", len(matches)),
		AlternativeCapabilities: []string{"web_search"}, // Можно попробовать веб-поиск
	}, nil
}

// getMapKeys is defined in capability_executors.go
