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
	coreDocs  *repo.CoreDocsRepo
	documents *repo.DocumentsRepository // Опционально, для поиска CoreDoc ID через Document
	logger    *log.Logger
}

// NewDocumentReadExecutor создает новый исполнитель для чтения документов
func NewDocumentReadExecutor(coreDocs *repo.CoreDocsRepo, logger *log.Logger) *DocumentReadExecutor {
	return &DocumentReadExecutor{
		coreDocs: coreDocs,
		logger:   logger,
	}
}

// WithDocumentsRepository настраивает доступ к DocumentsRepository для поиска CoreDoc ID через Document
func (e *DocumentReadExecutor) WithDocumentsRepository(documents *repo.DocumentsRepository) *DocumentReadExecutor {
	e.documents = documents
	return e
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
		// docID из data_search - это Document ID из пути GCS: gs://bucket/{tenantId}/{sourceId}/{docID}.txt
		// Где docID - это Document.ID (Firestore ID), НЕ CoreDoc ID!
		// Сначала ищем Document по ID (это Document ID в Firestore)
		var docFromRAG *models.Document
		var docErr error
		if e.documents != nil {
			docFromRAG, docErr = e.documents.GetByID(ctx, docID)
			if docErr != nil || docFromRAG == nil {
				// Если не нашли Document по ID, пробуем как CoreDoc ID (fallback)
				if e.logger != nil {
					if docErr != nil {
						e.logger.Printf("[DocumentReadExecutor] Document %s not found by ID (Document ID from GCS path), trying as CoreDoc ID, error: %v", docID, docErr)
					} else {
						e.logger.Printf("[DocumentReadExecutor] Document %s not found by ID, trying as CoreDoc ID", docID)
					}
				}
				// Пробуем найти Document по coreDocId в Attrs (docID может быть CoreDoc ID)
				docFromRAG, docErr = e.documents.GetByCoreDocID(ctx, stepCtx.TenantID, docID)
				if docErr != nil || docFromRAG == nil {
					// Если не нашли Document, пробуем найти CoreDoc напрямую
					doc, coreDocErr := e.coreDocs.Get(ctx, docID)
					if coreDocErr == nil && doc != nil {
						if e.logger != nil {
							e.logger.Printf("[DocumentReadExecutor] Found CoreDoc %s directly (docID was CoreDoc ID)", docID)
						}
						// Проверить доступ
						if doc.TenantID != stepCtx.TenantID || doc.ProjectID != stepCtx.ProjectID {
							return nil, fmt.Errorf("access denied: CoreDoc %s belongs to different tenant/project", docID)
						}
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
						continue
					}
					// Не нашли ни Document, ни CoreDoc
					return nil, fmt.Errorf("failed to read document %s: Document not found by ID and CoreDoc not found: %w", docID, docErr)
				} else if e.logger != nil {
					e.logger.Printf("[DocumentReadExecutor] Found Document %s by coreDocId (docID was CoreDoc ID)", docFromRAG.ID)
				}
			} else if e.logger != nil {
				e.logger.Printf("[DocumentReadExecutor] Found Document %s by ID (Document ID from GCS path)", docID)
			}
		} else {
			// Если documents repo не доступен, пробуем найти CoreDoc напрямую
			doc, err := e.coreDocs.Get(ctx, docID)
			if err != nil {
				return nil, fmt.Errorf("failed to read document %s: %w", docID, err)
			}
			// Проверить доступ
			if doc.TenantID != stepCtx.TenantID || doc.ProjectID != stepCtx.ProjectID {
				return nil, fmt.Errorf("access denied: CoreDoc %s belongs to different tenant/project", docID)
			}
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
			continue
		}

		// Document найден (либо по ID, либо по coreDocId)
		if docFromRAG != nil {
			// Проверяем, есть ли coreDocId в Attrs
			if coreDocID, ok := docFromRAG.Attrs["coreDocId"].(string); ok && coreDocID != "" {
				if e.logger != nil {
					e.logger.Printf("[DocumentReadExecutor] Found Document %s, coreDocId: %s", docFromRAG.ID, coreDocID)
				}
				// Пробуем получить CoreDoc по правильному ID
				doc, err := e.coreDocs.Get(ctx, coreDocID)
				if err != nil {
					if e.logger != nil {
						e.logger.Printf("[DocumentReadExecutor] CoreDoc %s (from Document) also not found, using Document content", coreDocID)
					}
					// Если и CoreDoc не найден, используем контент из Document
					content := docFromRAG.FullText
					documents = append(documents, map[string]interface{}{
						"id":          coreDocID, // Возвращаем CoreDoc ID, если был найден
						"filename":    docFromRAG.Title,
						"contentType": docFromRAG.ContentType,
						"content":     content,
						"sizeBytes":   nil,
					})
					continue
				}
				// CoreDoc найден, используем его
				// Проверить доступ (tenantID и projectID должны совпадать)
				if doc.TenantID != stepCtx.TenantID || doc.ProjectID != stepCtx.ProjectID {
					return nil, fmt.Errorf("access denied: CoreDoc %s belongs to different tenant/project", coreDocID)
				}
				// Получить контент из CoreDoc или Document
				content := docFromRAG.FullText
				if doc.ExtractedText != nil && *doc.ExtractedText != "" {
					content = *doc.ExtractedText
				}
				documents = append(documents, map[string]interface{}{
					"id":          doc.ID,
					"filename":    doc.Filename,
					"contentType": doc.ContentType,
					"content":     content,
					"sizeBytes":   doc.SizeBytes,
				})
				continue
			} else {
				// Document найден, но coreDocId нет - используем контент из Document
				if e.logger != nil {
					e.logger.Printf("[DocumentReadExecutor] Document %s found but no coreDocId, using Document content", docFromRAG.ID)
				}
				content := docFromRAG.FullText
				documents = append(documents, map[string]interface{}{
					"id":          docFromRAG.ID,
					"filename":    docFromRAG.Title,
					"contentType": docFromRAG.ContentType,
					"content":     content,
					"sizeBytes":   nil,
				})
				continue
			}
		}
		// Если мы дошли сюда, значит Document не был найден и CoreDoc тоже не был найден
		// Это не должно произойти, так как мы уже обработали все случаи выше
		return nil, fmt.Errorf("failed to read document %s: unexpected state - Document and CoreDoc not found", docID)
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
		Temperature:     0.7,
	}

	resp, err := e.genaiClient.GenAIGenerate(ctx, req)
	if err != nil {
		// Временные ошибки можно повторить
		retryable := strings.Contains(err.Error(), "timeout") ||
			strings.Contains(err.Error(), "rate limit") ||
			strings.Contains(err.Error(), "temporary")

		confidence := 0.0
		return &CapabilityResult{
			Success:                 false,
			ExecutionStatus:         models.ExecutionStatusFailed,
			Confidence:              &confidence,
			Retryable:               retryable,
			Reason:                  fmt.Sprintf("LLM generation failed: %v", err),
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

// DocumentWriteExecutor выполняет запись документов
type DocumentWriteExecutor struct {
	coreDocs *repo.CoreDocsRepo
	logger   *log.Logger
}

// NewDocumentWriteExecutor создает новый исполнитель для записи документов
func NewDocumentWriteExecutor(coreDocs *repo.CoreDocsRepo, logger *log.Logger) *DocumentWriteExecutor {
	return &DocumentWriteExecutor{
		coreDocs: coreDocs,
		logger:   logger,
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

	// Получить документ или создать новый, если не существует
	doc, err := e.coreDocs.Get(ctx, docID)
	if err != nil {
		// Документ не существует - создаем новый
		now := time.Now().UTC()
		sizeBytes := int64(len(content))
		filename := docID
		if fn, ok := stepCtx.Input["filename"].(string); ok && fn != "" {
			filename = fn
		}

		doc = &models.CoreDoc{
			ID:            docID,
			TenantID:      stepCtx.TenantID,
			ProjectID:     stepCtx.ProjectID,
			Filename:      filename,
			ContentType:   "text/plain",
			SizeBytes:     &sizeBytes,
			ExtractedText: &content,
			CreatedAt:     now,
			UpdatedAt:     now,
		}

		if e.logger != nil {
			e.logger.Printf("[DocumentWriteExecutor] Creating new document %s", docID)
		}
	} else {
		// Проверить доступ для существующего документа
		if doc.TenantID != stepCtx.TenantID || doc.ProjectID != stepCtx.ProjectID {
			return nil, fmt.Errorf("access denied")
		}

		// Обновить содержимое существующего документа
		if e.logger != nil {
			e.logger.Printf("[DocumentWriteExecutor] Updating existing document %s", docID)
		}
	}

	// Обновить/создать содержимое
	doc.ExtractedText = &content
	doc.UpdatedAt = time.Now().UTC()
	if doc.SizeBytes != nil {
		sizeBytes := int64(len(content))
		doc.SizeBytes = &sizeBytes
	}

	if err := e.coreDocs.Upsert(ctx, doc); err != nil {
		return nil, fmt.Errorf("failed to upsert document: %w", err)
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
