package agents

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// ============================================================================
// Capability Executors
// ============================================================================

// DocumentReadExecutor выполняет чтение документов
type DocumentReadExecutor struct {
	coreDocs *repo.CoreDocsRepo
	logger   *log.Logger
}

// NewDocumentReadExecutor создает новый исполнитель для чтения документов
func NewDocumentReadExecutor(coreDocs *repo.CoreDocsRepo, logger *log.Logger) *DocumentReadExecutor {
	return &DocumentReadExecutor{
		coreDocs: coreDocs,
		logger:    logger,
	}
}

func (e *DocumentReadExecutor) Capability() Capability {
	return CapabilityDocumentRead
}

func (e *DocumentReadExecutor) ValidateInput(input map[string]interface{}) error {
	docIDs, ok := input["documentIds"].([]interface{})
	if !ok || len(docIDs) == 0 {
		return fmt.Errorf("documentIds is required and must be a non-empty array")
	}
	return nil
}

func (e *DocumentReadExecutor) Execute(ctx context.Context, stepCtx *AgentStepContext) (*CapabilityResult, error) {
	docIDsRaw := stepCtx.Input["documentIds"].([]interface{})
	docIDs := make([]string, 0, len(docIDsRaw))
	for _, id := range docIDsRaw {
		if idStr, ok := id.(string); ok {
			docIDs = append(docIDs, idStr)
		}
	}

	if e.logger != nil {
		e.logger.Printf("[DocumentReadExecutor] Reading %d documents", len(docIDs))
	}

	documents := make([]map[string]interface{}, 0, len(docIDs))
	for _, docID := range docIDs {
		doc, err := e.coreDocs.Get(ctx, docID)
		if err != nil {
			return nil, fmt.Errorf("failed to read document %s: %w", docID, err)
		}

		// Проверить доступ (tenantID и projectID должны совпадать)
		if doc.TenantID != stepCtx.TenantID || doc.ProjectID != stepCtx.ProjectID {
			return nil, fmt.Errorf("access denied for document %s", docID)
		}

		// Получить содержимое документа
		content := ""
		if doc.ExtractedText != nil {
			content = *doc.ExtractedText
		}

		documents = append(documents, map[string]interface{}{
			"id":          doc.ID,
			"filename":    doc.Filename,
			"contentType": doc.ContentType,
			"content":     content,
			"sizeBytes":   doc.SizeBytes,
		})
	}

	confidence := 1.0 // Полная уверенность при успешном чтении
	emptyCount := 0
	for _, doc := range documents {
		if content, ok := doc["content"].(string); !ok || content == "" {
			emptyCount++
		}
	}
	
	// Если некоторые документы пустые, снижаем уверенность
	if emptyCount > 0 {
		confidence = 0.8
	}

	return &CapabilityResult{
		Success: true,
		Output: map[string]interface{}{
			"documents": documents,
			"count":     len(documents),
		},
		Message:         fmt.Sprintf("Read %d documents", len(documents)),
		ExecutionStatus: models.ExecutionStatusSuccess,
		Confidence:      &confidence,
		Retryable:       true, // Можно повторить при временных ошибках
		Reason:          fmt.Sprintf("Successfully read %d documents", len(documents)),
	}, nil
}

// LLMGenerateExecutor выполняет генерацию через LLM
type LLMGenerateExecutor struct {
	genaiClient *gen_combined.Client
	logger      *log.Logger
}

// NewLLMGenerateExecutor создает новый исполнитель для LLM генерации
func NewLLMGenerateExecutor(genaiClient *gen_combined.Client, logger *log.Logger) *LLMGenerateExecutor {
	return &LLMGenerateExecutor{
		genaiClient: genaiClient,
		logger:      logger,
	}
}

func (e *LLMGenerateExecutor) Capability() Capability {
	return CapabilityLLMGenerate
}

func (e *LLMGenerateExecutor) ValidateInput(input map[string]interface{}) error {
	if _, ok := input["prompt"].(string); !ok {
		return fmt.Errorf("prompt is required and must be a string")
	}
	return nil
}

func (e *LLMGenerateExecutor) Execute(ctx context.Context, stepCtx *AgentStepContext) (*CapabilityResult, error) {
	prompt := stepCtx.Input["prompt"].(string)
	model := "gemini_fast" // Default model alias (will be converted to gemini-2.5-flash-lite via normalizeOptionsForGemini)
	if m, ok := stepCtx.Input["model"].(string); ok {
		model = m
	}
	maxTokens := 4096
	if mt, ok := stepCtx.Input["maxTokens"].(float64); ok {
		maxTokens = int(mt)
	}

	// Автоматически добавляем контекст из предыдущих шагов
	enhancedPrompt := e.enhancePromptWithContext(prompt, stepCtx.PreviousOutputs)

	if e.logger != nil {
		e.logger.Printf("[LLMGenerateExecutor] Generating with model %s, prompt length: %d (enhanced: %d)", model, len(prompt), len(enhancedPrompt))
	}

	req := gen_combined.GenAIRequest{
		Model:           model,
		Prompt:          enhancedPrompt,
		MaxOutputTokens: maxTokens,
		Temperature:    0.7,
	}

	resp, err := e.genaiClient.GenAIGenerate(ctx, req)
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
			Reason:          fmt.Sprintf("LLM generation failed: %v", err),
			AlternativeCapabilities: []string{"llm_analyze"}, // Можно попробовать другой подход
		}, fmt.Errorf("LLM generation failed: %w", err)
	}

	confidence := 0.9 // Высокая уверенность в LLM генерации
	if len(resp.Content) == 0 {
		confidence = 0.5 // Низкая уверенность если контент пустой
	}

	return &CapabilityResult{
		Success: true,
		Output: map[string]interface{}{
			"content": resp.Content,
			"model":   resp.Model,
		},
		Message:         "Generated content successfully",
		ExecutionStatus: models.ExecutionStatusSuccess,
		Confidence:      &confidence,
		Retryable:       false,
		Reason:          fmt.Sprintf("Generated %d characters using %s", len(resp.Content), resp.Model),
	}, nil
}

// enhancePromptWithContext добавляет контекст из предыдущих шагов в промпт
func (e *LLMGenerateExecutor) enhancePromptWithContext(prompt string, previousOutputs map[string]map[string]interface{}) string {
	if len(previousOutputs) == 0 {
		if e.logger != nil {
			e.logger.Printf("[LLMGenerateExecutor] No previous outputs to enhance prompt")
		}
		return prompt
	}

	if e.logger != nil {
		e.logger.Printf("[LLMGenerateExecutor] Enhancing prompt with %d previous outputs", len(previousOutputs))
	}

	var contextParts []string

	// Ищем результаты document_read
	for stepID, output := range previousOutputs {
		if e.logger != nil {
			e.logger.Printf("[LLMGenerateExecutor] Checking step %s output keys: %v", stepID, getMapKeys(output))
		}

		// Проверяем наличие documents
		if documentsRaw, hasDocuments := output["documents"]; hasDocuments {
			if e.logger != nil {
				e.logger.Printf("[LLMGenerateExecutor] Found 'documents' key in step %s, type: %T", stepID, documentsRaw)
			}

			// Пробуем разные типы
			var documents []interface{}
			switch v := documentsRaw.(type) {
			case []interface{}:
				documents = v
			case []map[string]interface{}:
				// Конвертируем []map[string]interface{} в []interface{}
				documents = make([]interface{}, len(v))
				for i, m := range v {
					documents[i] = m
				}
			default:
				if e.logger != nil {
					e.logger.Printf("[LLMGenerateExecutor] Unexpected documents type: %T", v)
				}
				continue
			}

			if len(documents) > 0 {
				if e.logger != nil {
					e.logger.Printf("[LLMGenerateExecutor] Found %d documents in step %s", len(documents), stepID)
				}
				contextParts = append(contextParts, fmt.Sprintf("\n=== Контекст из шага %s (document_read) ===", stepID))
				for i, docRaw := range documents {
					if doc, ok := docRaw.(map[string]interface{}); ok {
						filename := ""
						content := ""
						if fn, ok := doc["filename"].(string); ok {
							filename = fn
						}
						if cnt, ok := doc["content"].(string); ok {
							content = cnt
						}
						if content != "" {
							contextParts = append(contextParts, fmt.Sprintf("\nДокумент %d: %s\n%s", i+1, filename, content))
						}
					}
				}
			}
		}

		// Ищем результаты data_search
		if results, ok := output["results"].([]interface{}); ok && len(results) > 0 {
			contextParts = append(contextParts, fmt.Sprintf("\n=== Контекст из шага %s (data_search) ===", stepID))
			for i, resultRaw := range results {
				if i >= 5 { // Ограничиваем до 5 результатов
					break
				}
				if result, ok := resultRaw.(map[string]interface{}); ok {
					title := ""
					content := ""
					if t, ok := result["title"].(string); ok {
						title = t
					}
					if c, ok := result["content"].(string); ok {
						content = c
					}
					if content != "" {
						contextParts = append(contextParts, fmt.Sprintf("\nРезультат поиска %d: %s\n%s", i+1, title, content[:min(500, len(content))]))
					}
				}
			}
		}

		// Ищем результаты web_search
		if results, ok := output["results"].([]interface{}); ok && len(results) > 0 {
			// Проверяем, есть ли URL (признак web_search)
			if len(results) > 0 {
				if firstResult, ok := results[0].(map[string]interface{}); ok {
					if _, hasURL := firstResult["url"]; hasURL {
						contextParts = append(contextParts, fmt.Sprintf("\n=== Контекст из шага %s (web_search) ===", stepID))
						for i, resultRaw := range results {
							if i >= 5 {
								break
							}
							if result, ok := resultRaw.(map[string]interface{}); ok {
								title := ""
								content := ""
								if t, ok := result["title"].(string); ok {
									title = t
								}
								if c, ok := result["content"].(string); ok {
									content = c
								}
								if content != "" {
									contextParts = append(contextParts, fmt.Sprintf("\nВеб-результат %d: %s\n%s", i+1, title, content[:min(500, len(content))]))
								}
							}
						}
					}
				}
			}
		}
	}

	if len(contextParts) > 0 {
		contextText := strings.Join(contextParts, "\n")
		return fmt.Sprintf("%s\n\n%s", prompt, contextText)
	}

	return prompt
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// getMapKeys возвращает ключи map для логирования
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// newSuccessResult создает CapabilityResult с успешным статусом
func newSuccessResult(output map[string]interface{}, message string, confidence float64) *CapabilityResult {
	return &CapabilityResult{
		Success:         true,
		Output:          output,
		Message:         message,
		ExecutionStatus: models.ExecutionStatusSuccess,
		Confidence:      &confidence,
		Retryable:       false,
		Reason:          message,
	}
}

// newFailedResult создает CapabilityResult с ошибкой
func newFailedResult(err error, retryable bool) (*CapabilityResult, error) {
	confidence := 0.0
	return &CapabilityResult{
		Success:         false,
		ExecutionStatus: models.ExecutionStatusFailed,
		Confidence:      &confidence,
		Retryable:       retryable,
		Reason:          err.Error(),
	}, err
}

// DocumentWriteExecutor выполняет запись документов
type DocumentWriteExecutor struct {
	coreDocs *repo.CoreDocsRepo
	logger   *log.Logger
}

// NewDocumentWriteExecutor создает новый исполнитель для записи документов
func NewDocumentWriteExecutor(coreDocs *repo.CoreDocsRepo, logger *log.Logger) *DocumentWriteExecutor {
	return &DocumentWriteExecutor{
		coreDocs: coreDocs,
		logger:    logger,
	}
}

func (e *DocumentWriteExecutor) Capability() Capability {
	return CapabilityDocumentWrite
}

func (e *DocumentWriteExecutor) ValidateInput(input map[string]interface{}) error {
	if _, ok := input["documentId"].(string); !ok {
		return fmt.Errorf("documentId is required")
	}
	if _, ok := input["content"].(string); !ok {
		return fmt.Errorf("content is required")
	}
	return nil
}

func (e *DocumentWriteExecutor) Execute(ctx context.Context, stepCtx *AgentStepContext) (*CapabilityResult, error) {
	docID := stepCtx.Input["documentId"].(string)
	content := stepCtx.Input["content"].(string)

	// Получить документ
	doc, err := e.coreDocs.Get(ctx, docID)
	if err != nil {
		return nil, fmt.Errorf("failed to get document: %w", err)
	}

	// Проверить доступ
	if doc.TenantID != stepCtx.TenantID || doc.ProjectID != stepCtx.ProjectID {
		return nil, fmt.Errorf("access denied")
	}

	// Обновить содержимое
	doc.ExtractedText = &content
	doc.UpdatedAt = time.Now().UTC()

	if err := e.coreDocs.Upsert(ctx, doc); err != nil {
		return nil, fmt.Errorf("failed to update document: %w", err)
	}

	if e.logger != nil {
		e.logger.Printf("[DocumentWriteExecutor] Updated document %s, content length: %d", docID, len(content))
	}

	confidence := 1.0 // Полная уверенность при успешной записи

	return &CapabilityResult{
		Success: true,
		Output: map[string]interface{}{
			"documentId": docID,
			"updatedAt":  doc.UpdatedAt,
		},
		Message:         fmt.Sprintf("Document %s updated successfully", docID),
		ExecutionStatus: models.ExecutionStatusSuccess,
		Confidence:      &confidence,
		Retryable:       true, // Можно повторить при временных ошибках
		Reason:          fmt.Sprintf("Document %s updated with %d characters", docID, len(content)),
	}, nil
}

