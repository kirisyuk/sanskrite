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
	"github.com/igorkirisyuk/sanskrite/internal/repo"
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
		StateUpdates: map[string]interface{}{
			"web_search_results": results,
			"last_search_query":  query,
		},
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

	// Автоматически добавляем project.dataset prefix если таблица указана без него
	// Известные таблицы в нашем dataset
	knownTables := []string{"core_docs_metadata", "documents_metadata", "documents_object_table", "core_docs_object_table", "core_docs_json_table"}
	projectID := e.bigqueryClient.ProjectID()
	datasetID := e.bigqueryClient.DatasetID()

	for _, table := range knownTables {
		// Заменяем только если таблица указана без полного пути
		// Проверяем разные варианты: FROM table, JOIN table, таблица в начале
		patterns := []string{
			" " + table + " ",
			" " + table + "\n",
			" " + table + ";",
			"FROM " + table,
			"from " + table,
			"JOIN " + table,
			"join " + table,
		}
		fullTable := fmt.Sprintf("`%s.%s.%s`", projectID, datasetID, table)
		for _, pattern := range patterns {
			if strings.Contains(sql, pattern) {
				replacement := strings.Replace(pattern, table, fullTable, 1)
				sql = strings.Replace(sql, pattern, replacement, -1)
			}
		}
	}

	if e.logger != nil {
		e.logger.Printf("[BigQueryQueryExecutor] Executing SQL query (length: %d): %s", len(sql), sql)
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
	// Принимаем либо prompt, либо text
	_, hasPrompt := input["prompt"].(string)
	_, hasText := input["text"].(string)
	if !hasPrompt && !hasText {
		return fmt.Errorf("prompt or text is required and must be a string")
	}
	// schema опциональна - если не указана, используем дефолтную
	return nil
}

func (e *LLMAnalyzeExecutor) Execute(ctx context.Context, stepCtx *AgentStepContext) (*CapabilityResult, error) {
	// Поддерживаем оба варианта: prompt или text
	prompt := ""
	if p, ok := stepCtx.Input["prompt"].(string); ok && p != "" {
		prompt = p
	} else if text, ok := stepCtx.Input["text"].(string); ok && text != "" {
		// Если передан text вместо prompt, создаем промпт из text
		analysisType := "general"
		if at, ok := stepCtx.Input["analysisType"].(string); ok && at != "" {
			analysisType = at
		}
		prompt = fmt.Sprintf("Analyze the following text and provide a %s analysis:\n\n%s", analysisType, text)
	}

	if prompt == "" {
		return nil, fmt.Errorf("prompt is empty after processing input")
	}

	// schema опциональна - если не указана, используем дефолтную
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"summary": map[string]interface{}{
				"type":        "string",
				"description": "Summary of the analysis",
			},
			"findings": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "Array of key findings from the analysis",
			},
			"metrics": map[string]interface{}{
				"type":        "object",
				"description": "Quantitative metrics from the analysis",
			},
		},
		"required": []string{"summary", "findings"},
	}
	if s, ok := stepCtx.Input["schema"].(map[string]interface{}); ok {
		schema = s
	}

	// Добавляем детальное описание JSON структуры в промпт
	prompt = prompt + `

CRITICAL: You MUST return ONLY valid JSON matching this EXACT structure:
{
  "summary": "Brief summary of the analysis",
  "findings": ["finding1", "finding2", "finding3"],
  "metrics": {
    "key1": "value1",
    "key2": "value2"
  }
}

Field names must match EXACTLY: "summary", "findings", "metrics". Do not include any explanatory text, markdown, or code blocks. Return ONLY the JSON object.`
	model := "gemini_fast"
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

	// Парсить JSON из Content, ToolCalls, или RawJSON
	jsonContent := resp.Content

	// Если Content пустой или не JSON, проверяем ToolCalls (для Gemini strict JSON mode)
	if jsonContent == "" || (len(jsonContent) > 0 && jsonContent[0] != '{' && jsonContent[0] != '[') {
		for _, tc := range resp.ToolCalls {
			if tc.Name == "return_structured" && tc.Arguments != "" {
				jsonContent = tc.Arguments
				if e.logger != nil {
					e.logger.Printf("[LLMAnalyzeExecutor] Extracted JSON from return_structured tool call")
				}
				break
			}
		}
	}

	// Если все еще пусто, используем RawJSON
	if jsonContent == "" {
		jsonContent = resp.RawJSON
	}

	// Очищаем markdown code fences если есть
	jsonContent = strings.TrimSpace(jsonContent)
	if strings.HasPrefix(jsonContent, "```json") {
		jsonContent = strings.TrimPrefix(jsonContent, "```json")
		jsonContent = strings.TrimSuffix(jsonContent, "```")
		jsonContent = strings.TrimSpace(jsonContent)
	} else if strings.HasPrefix(jsonContent, "```") {
		jsonContent = strings.TrimPrefix(jsonContent, "```")
		jsonContent = strings.TrimSuffix(jsonContent, "```")
		jsonContent = strings.TrimSpace(jsonContent)
	}

	if jsonContent == "" {
		return nil, fmt.Errorf("no JSON content in response")
	}

	var analysisResult map[string]interface{}
	if err := json.Unmarshal([]byte(jsonContent), &analysisResult); err != nil {
		if e.logger != nil {
			e.logger.Printf("[LLMAnalyzeExecutor] Failed to parse JSON, content preview: %s", jsonContent[:min(200, len(jsonContent))])
		}
		return nil, fmt.Errorf("failed to unmarshal analysis result: %w", err)
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

// LLMValidateExecutor выполняет проверку релевантности через LLM и итерационно создает следующие шаги
// Используется для итерационных задач: проверяет элемент и создает следующий шаг (create или skip)
type LLMValidateExecutor struct {
	genaiClient        *gen_combined.Client
	capabilityRegistry *CapabilityRegistry
	logger             *log.Logger
}

// NewLLMValidateExecutor создает новый исполнитель для проверки релевантности
func NewLLMValidateExecutor(
	genaiClient *gen_combined.Client,
	capabilityRegistry *CapabilityRegistry,
	logger *log.Logger,
) *LLMValidateExecutor {
	return &LLMValidateExecutor{
		genaiClient:        genaiClient,
		capabilityRegistry: capabilityRegistry,
		logger:             logger,
	}
}

func (e *LLMValidateExecutor) Capability() Capability {
	return CapabilityLLMValidate
}

func (e *LLMValidateExecutor) ValidateInput(input map[string]interface{}) error {
	// Проверяем наличие контента для валидации
	if _, ok := input["content"].(string); !ok {
		if _, ok := input["text"].(string); !ok {
			if _, ok := input["messages"].([]interface{}); !ok {
				return fmt.Errorf("content, text, or messages is required for validation")
			}
		}
	}
	// goal опционален, но желателен для проверки релевантности
	return nil
}

func (e *LLMValidateExecutor) Execute(ctx context.Context, stepCtx *AgentStepContext) (*CapabilityResult, error) {
	if e.logger != nil {
		e.logger.Printf("[LLMValidateExecutor] Validating relevance for job %s", stepCtx.JobID)
	}

	// Извлекаем метаданные элемента
	itemID := ""
	if ch, ok := stepCtx.Input["channelUsername"].(string); ok {
		itemID = ch
	} else if id, ok := stepCtx.Input["itemId"].(string); ok {
		itemID = id
	}

	// Извлекаем контент для проверки
	// 1. Сначала проверяем явный input
	var contentToValidate string
	if c, ok := stepCtx.Input["content"].(string); ok && len(c) > 20 { // Простая проверка на заглушки
		contentToValidate = c
	} else if t, ok := stepCtx.Input["text"].(string); ok && len(t) > 20 {
		contentToValidate = t
	} else if msgs, ok := stepCtx.Input["messages"].([]interface{}); ok && len(msgs) > 0 {
		contentToValidate = formatMessagesForValidation(msgs)
	}

	// 2. Если контент не передан, ищем в Shared State (Best Practice)
	if contentToValidate == "" && stepCtx.State != nil {
		if activeContent, ok := stepCtx.State["active_content"]; ok {
			if msgs, ok := activeContent.([]interface{}); ok {
				contentToValidate = formatMessagesForValidation(msgs)
				if e.logger != nil { e.logger.Printf("[LLMValidateExecutor] Used active_content from Shared State") }
			} else if str, ok := activeContent.(string); ok {
				contentToValidate = str
				if e.logger != nil { e.logger.Printf("[LLMValidateExecutor] Used active_content from Shared State") }
			}
		}
		// Ищем ID элемента в State
		if itemID == "" {
			if lastID, ok := stepCtx.State["last_read_item_id"].(string); ok {
				itemID = lastID
			}
		}
	}

	// 3. Если контент не передан (или это заглушка), ищем в предыдущих шагах (Auto-Wiring)
	if contentToValidate == "" || strings.Contains(strings.ToLower(contentToValidate), "message") { // Защита от "messages from..."
		if e.logger != nil {
			e.logger.Printf("[LLMValidateExecutor] Content not provided explicitly, searching in previous steps...")
		}
		
		// Ищем в обратном порядке (от последнего к первому)
		// Сначала ищем точное совпадение по itemId, если оно есть
		found := false
		
		// Стратегия 1: Ищем telegram_channel_read с output.channel == itemID
		if itemID != "" {
			for _, depID := range stepCtx.DependsOn { // Сначала смотрим прямые зависимости
				if output, ok := stepCtx.PreviousOutputs[depID]; ok {
					if ch, ok := output["channel"].(string); ok && strings.EqualFold(ch, itemID) {
						if msgs, ok := output["messages"].([]interface{}); ok {
							contentToValidate = formatMessagesForValidation(msgs)
							found = true
							if e.logger != nil { e.logger.Printf("[LLMValidateExecutor] Found content in dependency step %s (matched itemId)", depID) }
							break
						}
					}
				}
			}
		}

		// Стратегия 2: Если не нашли, берем последний шаг, который похож на source read
		if !found {
			// Сортировка ключей для детерминированного перебора (в реальном графе нужно идти по истории)
			// В AgentStepContext PreviousOutputs - это map, порядок не гарантирован.
			// Но обычно DependsOn содержит ID последнего шага.
			for _, depID := range stepCtx.DependsOn {
				if output, ok := stepCtx.PreviousOutputs[depID]; ok {
					// Проверяем наличие сообщений
					if msgs, ok := output["messages"].([]interface{}); ok && len(msgs) > 0 {
						contentToValidate = formatMessagesForValidation(msgs)
						// Если нашли messages и itemID был пуст, пытаемся узнать channel
						if itemID == "" {
							if ch, ok := output["channel"].(string); ok {
								itemID = ch
							}
						}
						found = true
						if e.logger != nil { e.logger.Printf("[LLMValidateExecutor] Found messages in dependency step %s", depID) }
						break
					}
					// Проверяем наличие контента (для документов)
					if content, ok := output["content"].(string); ok && len(content) > 0 {
						contentToValidate = content
						found = true
						break
					}
				}
			}
		}
	}

	if contentToValidate == "" {
		return nil, fmt.Errorf("no content to validate found in input or previous steps")
	}

	// Извлекаем цель для проверки релевантности
	goalStr := ""
	if goal, ok := stepCtx.Input["goal"].(map[string]interface{}); ok {
		if desc, ok := goal["description"].(string); ok {
			goalStr = desc
		}
	} else if goalStrRaw, ok := stepCtx.Input["goal"].(string); ok {
		goalStr = goalStrRaw
	}

	// Универсальный валидатор - только проверка релевантности, без планирования следующих шагов
	// Планирование следующих шагов происходит на основе результата валидации в других шагах
	prompt := fmt.Sprintf(`You are validating whether content is relevant to a goal. This is a universal validation step that can be used in various contexts.

VALIDATION REQUEST:
- Goal: %s
- Content to validate: %s
- Item identifier: %s

TASK:
Analyze if the content above is relevant to the goal. Be strict - only mark as relevant if content truly matches the goal.

CRITICAL: You MUST return ONLY valid JSON matching this EXACT structure. Do not include any explanatory text, markdown formatting, or code blocks. Return pure JSON starting with { and ending with }.

Return JSON in this EXACT format:
{
  "relevant": true,
  "confidence": 0.9,
  "reason": "explanation of why relevant/not relevant"
}

Field names must match EXACTLY: "relevant" (boolean), "confidence" (number 0.0-1.0), "reason" (string). All three fields are REQUIRED.`,
		goalStr,
		contentToValidate,
		itemID,
	)

	// JSON схема для строгого вывода - только результат валидации
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"relevant": map[string]interface{}{
				"type":        "boolean",
				"description": "Whether the content is relevant to the goal",
			},
			"confidence": map[string]interface{}{
				"type":        "number",
				"description": "Confidence in relevance assessment (0.0-1.0)",
			},
			"reason": map[string]interface{}{
				"type":        "string",
				"description": "Explanation of why relevant/not relevant",
			},
		},
		"required": []string{"relevant", "confidence", "reason"},
	}

	// Вызвать LLM для проверки релевантности
	opts := gen_combined.Options{
		Temperature: 0.2, // Низкая температура для более детерминированной проверки
	}
	response, err := e.genaiClient.GenerateJSONStrict(ctx, prompt, nil, schema, opts)
	if err != nil {
		return &CapabilityResult{
			Success:         false,
			Error:           stringPtr(err.Error()),
			ExecutionStatus: models.ExecutionStatusFailed,
			Retryable:       true,
			Reason:          fmt.Sprintf("Failed to validate relevance: %v", err),
		}, nil
	}

	// Извлечь JSON из ответа
	jsonContent := response.Content
	if jsonContent == "" || (len(jsonContent) > 0 && jsonContent[0] != '{') {
		for _, tc := range response.ToolCalls {
			if tc.Name == "return_structured" && tc.Arguments != "" {
				jsonContent = tc.Arguments
				break
			}
		}
	}
	if jsonContent == "" {
		jsonContent = response.RawJSON
	}

	// Очищаем markdown code fences
	jsonContent = strings.TrimSpace(jsonContent)
	if strings.HasPrefix(jsonContent, "```json") {
		jsonContent = strings.TrimPrefix(jsonContent, "```json")
		jsonContent = strings.TrimSuffix(jsonContent, "```")
		jsonContent = strings.TrimSpace(jsonContent)
	} else if strings.HasPrefix(jsonContent, "```") {
		jsonContent = strings.TrimPrefix(jsonContent, "```")
		jsonContent = strings.TrimSuffix(jsonContent, "```")
		jsonContent = strings.TrimSpace(jsonContent)
	}

	// Парсить результат валидации
	var validationResult map[string]interface{}
	if err := json.Unmarshal([]byte(jsonContent), &validationResult); err != nil {
		if e.logger != nil {
			e.logger.Printf("[LLMValidateExecutor] Failed to parse JSON: %v, content: %s", err, jsonContent[:min(200, len(jsonContent))])
		}
		return nil, fmt.Errorf("failed to unmarshal validation result: %w", err)
	}

	relevant := false
	if r, ok := validationResult["relevant"].(bool); ok {
		relevant = r
	}

	confidence := 0.8
	if c, ok := validationResult["confidence"].(float64); ok {
		confidence = c
	}

	reason := ""
	if r, ok := validationResult["reason"].(string); ok {
		reason = r
	}

	if e.logger != nil {
		e.logger.Printf("[LLMValidateExecutor] Validation result: relevant=%v, confidence=%.2f, reason=%s",
			relevant, confidence, reason)
	}

	// Универсальный валидатор только возвращает результат валидации
	// Планирование следующих шагов происходит на основе этого результата в других шагах
	return &CapabilityResult{
		Success: true,
		Output: map[string]interface{}{
			"relevant":   relevant,
			"confidence": confidence,
			"reason":     reason,
			"itemId":     itemID, // Сохраняем идентификатор элемента для использования в следующих шагах
		},
		Message:         reason,
		ExecutionStatus: models.ExecutionStatusSuccess,
		Confidence:      &confidence,
		Retryable:       false,
		Reason:          reason,
		SuggestsReplan:  false,
		// НЕ создаем suggestedSteps здесь - планирование происходит на основе результата валидации
		StateUpdates: map[string]interface{}{
			"last_validation_result": map[string]interface{}{
				"relevant":   relevant,
				"confidence": confidence,
				"reason":     reason,
				"itemId":     itemID,
			},
		},
	}, nil
}

// formatMessagesForValidation форматирует сообщения для валидации
func formatMessagesForValidation(msgs []interface{}) string {
	var sb strings.Builder
	for i, msg := range msgs {
		if i >= 15 { // Ограничиваем для промпта
			sb.WriteString(fmt.Sprintf("\n... and %d more messages", len(msgs)-15))
			break
		}
		if msgMap, ok := msg.(map[string]interface{}); ok {
			if text, ok := msgMap["text"].(string); ok {
				// Очищаем текст от лишних пробелов
				text = strings.TrimSpace(text)
				if text != "" {
					sb.WriteString(fmt.Sprintf("- %s\n", text))
				}
			}
		}
	}
	return sb.String()
}

// DataSearchExecutor выполняет поиск данных в документах проекта через Vertex AI Search
type DataSearchExecutor struct {
	vertexSearchClient *ai.VertexAISearchClient
	documentsRepo      *repo.DocumentsRepository // Для валидации существования документов
	logger             *log.Logger
}

// NewDataSearchExecutor создает новый исполнитель для поиска данных
func NewDataSearchExecutor(
	vertexSearchClient *ai.VertexAISearchClient,
	documentsRepo *repo.DocumentsRepository,
	logger *log.Logger,
) *DataSearchExecutor {
	return &DataSearchExecutor{
		vertexSearchClient: vertexSearchClient,
		documentsRepo:      documentsRepo,
		logger:             logger,
	}
}

func (e *DataSearchExecutor) Capability() Capability {
	return CapabilityDataSearch
}

func (e *DataSearchExecutor) ValidateInput(input map[string]interface{}) error {
	// Поддерживаем оба варианта: "query" и "searchQuery"
	if _, ok := input["query"].(string); !ok {
		if _, ok := input["searchQuery"].(string); !ok {
			return fmt.Errorf("query or searchQuery is required and must be a string")
		}
	}
	return nil
}

func (e *DataSearchExecutor) Execute(ctx context.Context, stepCtx *AgentStepContext) (*CapabilityResult, error) {
	// Поддерживаем оба варианта: "query" и "searchQuery"
	var searchQuery string
	if q, ok := stepCtx.Input["query"].(string); ok {
		searchQuery = q
	} else if sq, ok := stepCtx.Input["searchQuery"].(string); ok {
		searchQuery = sq
	} else {
		return nil, fmt.Errorf("query or searchQuery is required and must be a string")
	}
	limit := 10
	if l, ok := stepCtx.Input["limit"].(float64); ok {
		limit = int(l)
	}
	// Минимальный limit 5, чтобы иметь запас после фильтрации устаревших документов из GCS
	if limit < 5 {
		limit = 5
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
						sourceID := parts[2]   // sourceId (например, project:test-project)
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
				sourceID := parts[2]   // sourceId (например, project:test-project)
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

		// Извлекаем coreDocId из разных источников
		coreDocID := ""

		// 1. Пробуем из structData (самый надежный источник)
		// GCS metadata при загрузке содержит: tenantId, sourceId, documentId, title
		// documentId - это Document.ID в Firestore!
		if result.Document.StructData != nil {
			match["structData"] = result.Document.StructData
			if e.logger != nil && i == 0 {
				structKeys := make([]string, 0, len(result.Document.StructData))
				for k := range result.Document.StructData {
					structKeys = append(structKeys, k)
				}
				e.logger.Printf("[DataSearchExecutor] structData keys: %v", structKeys)
				// Логируем все значения для отладки
				for k, v := range result.Document.StructData {
					e.logger.Printf("[DataSearchExecutor] structData[%s] = %v", k, v)
				}
			}
			// Проверяем documentId из GCS metadata - это Document.ID в Firestore!
			if docID, ok := result.Document.StructData["documentId"].(string); ok && docID != "" {
				// Это Document.ID из Firestore!
				match["firestoreDocumentId"] = docID
				if e.logger != nil && i == 0 {
					e.logger.Printf("[DataSearchExecutor] Found documentId in structData (GCS metadata): %s - this is Document.ID in Firestore!", docID)
				}
			}
			if cid, ok := result.Document.StructData["coreDocId"].(string); ok && cid != "" {
				coreDocID = cid
				if e.logger != nil && i == 0 {
					e.logger.Printf("[DataSearchExecutor] Found coreDocId in structData: %s", coreDocID)
				}
			}
		}

		// 2. Если не нашли, пробуем из derivedStructData (может содержать coreDocId)
		if coreDocID == "" && result.Document.DerivedStructData != nil {
			match["derivedStructData"] = result.Document.DerivedStructData
			if e.logger != nil && i == 0 {
				derivedKeys := make([]string, 0, len(result.Document.DerivedStructData))
				for k := range result.Document.DerivedStructData {
					derivedKeys = append(derivedKeys, k)
				}
				e.logger.Printf("[DataSearchExecutor] derivedStructData keys: %v", derivedKeys)
			}
			if cid, ok := result.Document.DerivedStructData["coreDocId"].(string); ok && cid != "" {
				coreDocID = cid
				if e.logger != nil && i == 0 {
					e.logger.Printf("[DataSearchExecutor] Found coreDocId in derivedStructData: %s", coreDocID)
				}
			}
		}

		// 3. Если не нашли, пробуем из title (title может содержать Document ID или CoreDoc ID)
		if coreDocID == "" && result.Document.Title != "" {
			// Title может быть Document ID или CoreDoc ID
			// Попробуем использовать его как Document ID (будем искать Document и извлекать coreDocId из Attrs)
			if e.logger != nil && i == 0 {
				e.logger.Printf("[DataSearchExecutor] Title found: %s (will try as Document ID)", result.Document.Title)
			}
		}

		// 4. Если не нашли, пробуем из derivedStructData.link (извлекаем docID из пути)
		// Это может быть Document ID, а не CoreDoc ID!
		extractedDocID := ""
		if result.Document.DerivedStructData != nil {
			if link, ok := result.Document.DerivedStructData["link"].(string); ok && link != "" {
				// Формат: gs://bucket/tenantId/sourceId/docID.txt
				// docID - это последняя часть пути без расширения
				if strings.HasPrefix(link, "gs://") {
					uriWithoutPrefix := strings.TrimPrefix(link, "gs://")
					parts := strings.Split(uriWithoutPrefix, "/")
					if len(parts) >= 4 {
						docIDWithExt := parts[len(parts)-1] // последняя часть (docID.txt)
						// Убираем расширение
						if idx := strings.LastIndex(docIDWithExt, "."); idx > 0 {
							extractedDocID = docIDWithExt[:idx]
						} else {
							extractedDocID = docIDWithExt
						}
						if e.logger != nil && i == 0 {
							e.logger.Printf("[DataSearchExecutor] Extracted ID from link: %s (link: %s) - this may be Document ID, not CoreDoc ID", extractedDocID, link)
						}
					}
				}
			}
		}

		// Приоритет для documentIds:
		// 1. firestoreDocumentId из structData (GCS metadata) - это Document.ID в Firestore!
		// 2. coreDocId из structData - это CoreDoc ID (если используется)
		// 3. extractedDocID из link пути GCS
		// 4. result.ID из Vertex AI Search (внутренний ID, НЕ рекомендуется)

		// Получаем firestoreDocumentId из match (установлен выше из structData)
		firestoreDocID := ""
		if fid, ok := match["firestoreDocumentId"].(string); ok && fid != "" {
			firestoreDocID = fid
		}

		if firestoreDocID != "" {
			// firestoreDocumentId - это Document.ID в Firestore (из GCS metadata)
			match["documentId"] = firestoreDocID
			if e.logger != nil && i == 0 {
				e.logger.Printf("[DataSearchExecutor] Using firestoreDocumentId from GCS metadata: %s (Document.ID in Firestore)", firestoreDocID)
			}
		} else if coreDocID != "" {
			// coreDocID - это CoreDoc ID
			match["coreDocId"] = coreDocID
			match["documentId"] = coreDocID
			if e.logger != nil && i == 0 {
				e.logger.Printf("[DataSearchExecutor] Using coreDocId: %s (CoreDoc ID)", coreDocID)
			}
		} else if extractedDocID != "" {
			// extractedDocID из link - это Document ID в Firestore (из пути GCS)
			match["documentId"] = extractedDocID
			match["vertexSearchDocumentId"] = result.ID
			if e.logger != nil && i == 0 {
				e.logger.Printf("[DataSearchExecutor] Using extractedDocID from GCS path: %s", extractedDocID)
			}
		} else {
			// Fallback: используем result.ID
			match["documentId"] = result.ID
			match["vertexSearchDocumentId"] = result.ID
			if e.logger != nil && i == 0 {
				e.logger.Printf("[DataSearchExecutor] WARNING: Using result.ID as fallback: %s", result.ID)
			}
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

	// Извлекаем массив ID для document_read
	// Валидируем существование Document в Firestore (GCS может содержать устаревшие файлы)
	documentIds := make([]string, 0, len(matches))
	for i, match := range matches {
		var docID string
		// Сначала пробуем firestoreDocumentId из GCS metadata (structData)
		if firestoreDocID, ok := match["firestoreDocumentId"].(string); ok && firestoreDocID != "" {
			docID = firestoreDocID
			if e.logger != nil && i == 0 {
				e.logger.Printf("[DataSearchExecutor] Using firestoreDocumentId from GCS metadata: %s", docID)
			}
		} else if coreDocID, ok := match["coreDocId"].(string); ok && coreDocID != "" {
			// coreDocId - это CoreDoc ID
			docID = coreDocID
			if e.logger != nil && i == 0 {
				e.logger.Printf("[DataSearchExecutor] Using coreDocId from match: %s", docID)
			}
		} else {
			// Используем documentId из match (это extractedDocID из link)
			if docIDFromMatch, ok := match["documentId"].(string); ok && docIDFromMatch != "" {
				docID = docIDFromMatch
				if e.logger != nil && i == 0 {
					e.logger.Printf("[DataSearchExecutor] Using documentId from match: %s", docID)
				}
			} else if id, ok := match["id"].(string); ok && id != "" {
				// Fallback: result.ID (внутренний ID Vertex AI Search, обычно не работает)
				docID = id
				if e.logger != nil && i == 0 {
					e.logger.Printf("[DataSearchExecutor] Using result.ID as fallback: %s (may not work)", docID)
				}
			}
		}

		// Валидируем существование Document в Firestore
		if docID != "" && e.documentsRepo != nil {
			doc, err := e.documentsRepo.GetByID(ctx, docID)
			if err != nil || doc == nil {
				// Document не существует в Firestore - это устаревший файл в GCS
				if e.logger != nil {
					e.logger.Printf("[DataSearchExecutor] WARNING: Document %s not found in Firestore (stale GCS file), skipping", docID)
				}
				continue // Пропускаем этот результат
			}
			if e.logger != nil && i == 0 {
				e.logger.Printf("[DataSearchExecutor] Validated Document %s exists in Firestore", docID)
			}
		}

		if docID != "" {
			documentIds = append(documentIds, docID)
		}
	}

	confidence := 0.9 // Высокая уверенность при успешном поиске
	if len(matches) == 0 {
		confidence = 0.6 // Ниже если результатов нет
	}

	return &CapabilityResult{
		Success: true,
		Output: map[string]interface{}{
			"matches":       matches,
			"documentIds":   documentIds, // Массив CoreDoc ID для использования в document_read
			"count":         len(matches),
			"query":         searchQuery,
			"nextPageToken": searchResp.NextPageToken,
		},
		Message:                 fmt.Sprintf("Found %d matching documents", len(matches)),
		ExecutionStatus:         models.ExecutionStatusSuccess,
		Confidence:              &confidence,
		Retryable:               true, // Можно повторить при временных ошибках
		Reason:                  fmt.Sprintf("Data search completed, found %d matching documents", len(matches)),
		AlternativeCapabilities: []string{"web_search"}, // Можно попробовать веб-поиск
	}, nil
}

// getMapKeys is defined in capability_executors.go
