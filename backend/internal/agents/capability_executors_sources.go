package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/telegram"
)

// SourceListExecutor выполняет получение списка источников проекта
type SourceListExecutor struct {
	dataSourcesRepo    *repo.DataSourcesRepository
	sourceProjectsRepo *repo.SourceProjectsRepository
	logger             *log.Logger
}

// NewSourceListExecutor создает новый исполнитель для списка источников
func NewSourceListExecutor(
	dataSourcesRepo *repo.DataSourcesRepository,
	sourceProjectsRepo *repo.SourceProjectsRepository,
	logger *log.Logger,
) *SourceListExecutor {
	return &SourceListExecutor{
		dataSourcesRepo:    dataSourcesRepo,
		sourceProjectsRepo: sourceProjectsRepo,
		logger:             logger,
	}
}

func (e *SourceListExecutor) Capability() Capability {
	return CapabilitySourceList
}

func (e *SourceListExecutor) ValidateInput(input map[string]interface{}) error {
	// projectId опционален - если не указан, вернет все источники tenant
	return nil
}

func (e *SourceListExecutor) Execute(ctx context.Context, stepCtx *AgentStepContext) (*CapabilityResult, error) {
	projectID := stepCtx.ProjectID
	if projectIDFromInput, ok := stepCtx.Input["projectId"].(string); ok && projectIDFromInput != "" {
		projectID = projectIDFromInput
	}

	kindFilter, _ := stepCtx.Input["kind"].(string)
	statusFilter, _ := stepCtx.Input["status"].(string)

	if e.logger != nil {
		e.logger.Printf("[SourceListExecutor] Listing sources for project=%s, kind=%s, status=%s", projectID, kindFilter, statusFilter)
	}

	var sources []*models.DataSource
	var err error

	if projectID != "" {
		// Получить источники через SourceProjects
		if e.sourceProjectsRepo != nil {
			var status *string
			if statusFilter != "" {
				status = &statusFilter
			}
			links, linkErr := e.sourceProjectsRepo.ListByProject(ctx, stepCtx.TenantID, projectID, status)
			if linkErr == nil && len(links) > 0 {
				sourceIDs := make([]string, 0, len(links))
				for _, link := range links {
					sourceIDs = append(sourceIDs, link.SourceID)
				}
				// Получить источники по IDs
				sources = make([]*models.DataSource, 0, len(sourceIDs))
				for _, sourceID := range sourceIDs {
					// Для публичных источников tenantID может быть пустым
					source, err := e.dataSourcesRepo.Get(ctx, sourceID)
					if err == nil && source != nil {
						// Применить фильтр по kind если указан
						if kindFilter == "" || source.Kind == kindFilter {
							sources = append(sources, source)
						}
					}
				}
			} else {
				// Fallback к старому методу
				sources, err = e.dataSourcesRepo.ListByProject(ctx, stepCtx.TenantID, projectID)
			}
		} else {
			// Fallback к старому методу
			sources, err = e.dataSourcesRepo.ListByProject(ctx, stepCtx.TenantID, projectID)
		}
	} else {
		// Получить все источники tenant
		var kinds []string
		if kindFilter != "" {
			kinds = []string{kindFilter}
		}
		var status *string
		if statusFilter != "" {
			status = &statusFilter
		}
		sources, err = e.dataSourcesRepo.ListByTenant(ctx, stepCtx.TenantID, kinds, status)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to list sources: %w", err)
	}

	// Преобразовать в JSON-совместимый формат
	sourcesData := make([]map[string]interface{}, 0, len(sources))
	for _, source := range sources {
		sourceData := map[string]interface{}{
			"id":          source.ID,
			"displayName": source.DisplayName,
			"kind":        source.Kind,
			"status":      source.Status,
			"details":     source.Details,
			"createdAt":   source.CreatedAt,
			"updatedAt":   source.UpdatedAt,
		}
		if source.TenantID != "" {
			sourceData["tenantId"] = source.TenantID
		}
		sourcesData = append(sourcesData, sourceData)
	}

	confidence := 1.0
	return &CapabilityResult{
		Success: true,
		Output: map[string]interface{}{
			"sources": sourcesData,
			"count":   len(sourcesData),
		},
		Message:         fmt.Sprintf("Found %d sources", len(sourcesData)),
		ExecutionStatus: models.ExecutionStatusSuccess,
		Confidence:      &confidence,
		Retryable:       true,
		Reason:          fmt.Sprintf("Successfully listed %d sources", len(sourcesData)),
	}, nil
}

// TelegramChannelReadExecutor выполняет чтение данных из Telegram-канала
type TelegramChannelReadExecutor struct {
	telegramClient *telegram.Client
	logger         *log.Logger
}

// NewTelegramChannelReadExecutor создает новый исполнитель для чтения Telegram
func NewTelegramChannelReadExecutor(telegramClient *telegram.Client, logger *log.Logger) *TelegramChannelReadExecutor {
	return &TelegramChannelReadExecutor{
		telegramClient: telegramClient,
		logger:         logger,
	}
}

func (e *TelegramChannelReadExecutor) Capability() Capability {
	return CapabilityTelegramChannelRead
}

func (e *TelegramChannelReadExecutor) ValidateInput(input map[string]interface{}) error {
	channelUsername, ok := input["channelUsername"].(string)
	if !ok || channelUsername == "" {
		return fmt.Errorf("channelUsername is required and must be a non-empty string")
	}
	return nil
}

func (e *TelegramChannelReadExecutor) Execute(ctx context.Context, stepCtx *AgentStepContext) (*CapabilityResult, error) {
	channelUsername, _ := stepCtx.Input["channelUsername"].(string)
	// Нормализовать username (убрать @ если есть)
	channelUsername = strings.TrimPrefix(strings.TrimSpace(channelUsername), "@")

	lastMessageID := 0
	if lastID, ok := stepCtx.Input["lastMessageID"].(float64); ok {
		lastMessageID = int(lastID)
	}

	maxMessages := 50
	if maxMsgs, ok := stepCtx.Input["maxMessages"].(float64); ok {
		maxMessages = int(maxMsgs)
	}

	if e.logger != nil {
		e.logger.Printf("[TelegramChannelReadExecutor] Reading from channel @%s, lastMessageID=%d, maxMessages=%d", channelUsername, lastMessageID, maxMessages)
	}

	// Подготовка списка processed_channels (читаем текущий из State и добавляем этот)
	processedChannels := make([]string, 0)
	if stepCtx.State != nil {
		if pc, ok := stepCtx.State["processed_channels"].([]interface{}); ok {
			for _, c := range pc {
				if s, ok := c.(string); ok {
					processedChannels = append(processedChannels, s)
				}
			}
		} else if pc, ok := stepCtx.State["processed_channels"].([]string); ok {
			processedChannels = append(processedChannels, pc...)
		}
	}
	// Добавляем текущий, если его еще нет
	alreadyProcessed := false
	for _, c := range processedChannels {
		if c == channelUsername {
			alreadyProcessed = true
			break
		}
	}
	if !alreadyProcessed {
		processedChannels = append(processedChannels, channelUsername)
	}

	messages, err := e.telegramClient.FetchUpdates(ctx, channelUsername, lastMessageID)
	if err != nil {
		return &CapabilityResult{
			Success: false,
			Error:   stringPtr(err.Error()),
			Output: map[string]interface{}{
				"channel": channelUsername,
				"error":   err.Error(),
			},
			ExecutionStatus: models.ExecutionStatusFailed,
			Retryable:       true,
			Reason:          fmt.Sprintf("Failed to fetch messages from @%s: %v", channelUsername, err),
			StateUpdates: map[string]interface{}{
				"processed_channels": processedChannels, // Сохраняем обновленный список
			},
		}, nil // Возвращаем nil error, так как это некритичная capability
	}

	// Ограничить количество сообщений если нужно
	if len(messages) > maxMessages {
		messages = messages[:maxMessages]
	}

	// Преобразовать в JSON-совместимый формат
	messagesData := make([]map[string]interface{}, 0, len(messages))
	maxID := 0
	for _, msg := range messages {
		if msg.ID > maxID {
			maxID = msg.ID
		}
		messagesData = append(messagesData, map[string]interface{}{
			"id":   msg.ID,
			"text": msg.Text,
			"date": msg.Date,
			"link": msg.Link,
		})
	}

	confidence := 1.0
	if len(messages) == 0 {
		confidence = 0.5 // Низкая уверенность если нет сообщений
	}

	return &CapabilityResult{
		Success: true,
		Output: map[string]interface{}{
			"messages":      messagesData,
			"count":         len(messages),
			"channel":       channelUsername,
			"lastMessageID": maxID,
		},
		Message:         fmt.Sprintf("Read %d messages from @%s", len(messages), channelUsername),
		ExecutionStatus: models.ExecutionStatusSuccess,
		Confidence:      &confidence,
		Retryable:       true,
		Reason:          fmt.Sprintf("Successfully read %d messages from @%s", len(messages), channelUsername),
		StateUpdates: map[string]interface{}{
			"active_content":     messagesData,
			"last_read_item_id":  channelUsername,
			"current_item_type":  "telegram_channel",
			"processed_channels": processedChannels, // Сохраняем обновленный список
		},
	}, nil
}

// SourceCreateExecutor выполняет создание источника и привязку к проекту
type SourceCreateExecutor struct {
	dataSourcesRepo    *repo.DataSourcesRepository
	sourceProjectsRepo *repo.SourceProjectsRepository
	publisher          *realtime.Publisher
	logger             *log.Logger
}

// NewSourceCreateExecutor создает новый исполнитель для создания источников
func NewSourceCreateExecutor(
	dataSourcesRepo *repo.DataSourcesRepository,
	sourceProjectsRepo *repo.SourceProjectsRepository,
	publisher *realtime.Publisher,
	logger *log.Logger,
) *SourceCreateExecutor {
	return &SourceCreateExecutor{
		dataSourcesRepo:    dataSourcesRepo,
		sourceProjectsRepo: sourceProjectsRepo,
		publisher:          publisher,
		logger:             logger,
	}
}

func (e *SourceCreateExecutor) Capability() Capability {
	return CapabilitySourceCreate
}

func (e *SourceCreateExecutor) ValidateInput(input map[string]interface{}) error {
	kind, ok := input["kind"].(string)
	if !ok || kind == "" {
		return fmt.Errorf("kind is required and must be a non-empty string")
	}
	if kind != "telegram_channel" && kind != "rss" && kind != "web" {
		return fmt.Errorf("kind must be telegram_channel, rss, or web")
	}

	// Проверка обязательных полей в зависимости от kind
	if kind == "telegram_channel" {
		channelUsername, ok := input["channelUsername"].(string)
		if !ok || channelUsername == "" {
			return fmt.Errorf("channelUsername is required for telegram_channel kind")
		}
	} else if kind == "rss" {
		rssURL, ok := input["rssUrl"].(string)
		if !ok || rssURL == "" {
			return fmt.Errorf("rssUrl is required for rss kind")
		}
	} else if kind == "web" {
		uri, ok := input["uri"].(string)
		if !ok || uri == "" {
			return fmt.Errorf("uri is required for web kind")
		}
	}

	return nil
}

func (e *SourceCreateExecutor) Execute(ctx context.Context, stepCtx *AgentStepContext) (*CapabilityResult, error) {
	kind, _ := stepCtx.Input["kind"].(string)
	displayName, _ := stepCtx.Input["displayName"].(string)
	status, _ := stepCtx.Input["status"].(string)
	if status == "" {
		status = "paused" // По умолчанию создаем в paused состоянии
	}

	// Построить SourceDetails
	details := models.SourceDetails{}
	if kind == "telegram_channel" {
		channelUsername, _ := stepCtx.Input["channelUsername"].(string)
		channelUsername = strings.TrimPrefix(strings.TrimSpace(channelUsername), "@")
		details.ChannelUsername = &channelUsername
	} else if kind == "rss" {
		rssURL, _ := stepCtx.Input["rssUrl"].(string)
		details.RSSURL = &rssURL
	} else if kind == "web" {
		uri, _ := stepCtx.Input["uri"].(string)
		details.URI = &uri
	}

	// Генерировать displayName если не указан
	if displayName == "" {
		if kind == "telegram_channel" && details.ChannelUsername != nil {
			displayName = "@" + *details.ChannelUsername
		} else if kind == "rss" && details.RSSURL != nil {
			displayName = *details.RSSURL
		} else if kind == "web" && details.URI != nil {
			displayName = *details.URI
		} else {
			displayName = kind + " source"
		}
	}

	now := time.Now().UTC()
	ds := &models.DataSource{
		TenantID:    stepCtx.TenantID, // Будет очищен для публичных источников в Create()
		DisplayName: displayName,
		Kind:        kind,
		Status:      status,
		Details:     details,
		OwnerUserID: stepCtx.UserID,
		CreatedAt:   now,
		UpdatedAt:   now,
		FirstSeen:   now,
		LastSeen:    now,
		ErrorCount:  0,
		TrustLevel:  0.5,
	}

	sourceID, err := e.dataSourcesRepo.Create(ctx, ds)
	if err != nil {
		return &CapabilityResult{
			Success:         false,
			Error:           stringPtr(err.Error()),
			ExecutionStatus: models.ExecutionStatusFailed,
			Retryable:       true,
			Reason:          fmt.Sprintf("Failed to create source: %v", err),
		}, nil
	}

	ds.ID = sourceID

	// Привязать источник к проекту
	if stepCtx.ProjectID != "" {
		sp := &models.SourceProject{
			TenantID:  stepCtx.TenantID,
			SourceID:  sourceID,
			ProjectID: stepCtx.ProjectID,
			Status:    status, // Используем тот же статус
			CreatedAt: now,
			UpdatedAt: now,
		}

		linkID, linkErr := e.sourceProjectsRepo.Create(ctx, sp)
		if linkErr != nil {
			if e.logger != nil {
				e.logger.Printf("[SourceCreateExecutor] Failed to attach source to project: %v", linkErr)
			}
			// Не считаем это критичной ошибкой - источник создан
		} else {
			sp.ID = linkID
			if e.logger != nil {
				e.logger.Printf("[SourceCreateExecutor] Attached source %s to project %s (linkID: %s)", sourceID, stepCtx.ProjectID, linkID)
			}
		}

		// Отправить WebSocket событие о создании источника
		if e.publisher != nil {
			eventPayload := map[string]interface{}{
				"sourceId":    sourceID,
				"projectId":   stepCtx.ProjectID,
				"tenantId":    stepCtx.TenantID,
				"kind":        kind,
				"displayName": displayName,
				"status":      status,
				"createdAt":   now,
			}
			if err := e.publisher.PublishJSON(ctx, realtime.EventSourceCreated, stepCtx.TenantID, eventPayload); err != nil {
				if e.logger != nil {
					e.logger.Printf("[SourceCreateExecutor] Failed to publish source.created event: %v", err)
				}
			}
		}
	}

	confidence := 1.0
	return &CapabilityResult{
		Success: true,
		Output: map[string]interface{}{
			"sourceId":    sourceID,
			"displayName": displayName,
			"kind":        kind,
			"status":      status,
			"projectId":   stepCtx.ProjectID,
		},
		Message:         fmt.Sprintf("Created %s source '%s' and attached to project", kind, displayName),
		ExecutionStatus: models.ExecutionStatusSuccess,
		Confidence:      &confidence,
		Retryable:       false, // Создание источника не должно повторяться
		Reason:          fmt.Sprintf("Successfully created source %s", sourceID),
	}, nil
}

// PlanCalibrateExecutor выполняет промежуточный шаг калибровки - просмотр результатов и допланирование
type PlanCalibrateExecutor struct {
	genaiClient        *gen_combined.Client
	capabilityRegistry *CapabilityRegistry
	logger             *log.Logger
}

// NewPlanCalibrateExecutor создает новый исполнитель для калибровки плана
func NewPlanCalibrateExecutor(
	genaiClient *gen_combined.Client,
	capabilityRegistry *CapabilityRegistry,
	logger *log.Logger,
) *PlanCalibrateExecutor {
	return &PlanCalibrateExecutor{
		genaiClient:        genaiClient,
		capabilityRegistry: capabilityRegistry,
		logger:             logger,
	}
}

func (e *PlanCalibrateExecutor) Capability() Capability {
	return CapabilityPlanCalibrate
}

func (e *PlanCalibrateExecutor) ValidateInput(input map[string]interface{}) error {
	// Входные данные опциональны - executor будет анализировать результаты предыдущих шагов
	return nil
}

func (e *PlanCalibrateExecutor) Execute(ctx context.Context, stepCtx *AgentStepContext) (*CapabilityResult, error) {
	if e.logger != nil {
		e.logger.Printf("[PlanCalibrateExecutor] Calibrating plan for job %s", stepCtx.JobID)
	}

	// Анализируем предыдущие шаги и определяем контекст
	var webSearchResults []map[string]interface{}
	var telegramReadResults []map[string]interface{}
	var checkedChannels []string
	var createdSources []string
	var validationResults []map[string]interface{} // Результаты валидации из llm_validate

	// 1. Сначала проверяем Shared State (наиболее актуальный результат)
	if stepCtx.State != nil {
		if lastVal, ok := stepCtx.State["last_validation_result"].(map[string]interface{}); ok {
			validationResults = append(validationResults, lastVal)
			if e.logger != nil {
				e.logger.Printf("[PlanCalibrateExecutor] Found validation result in Shared State: relevant=%v", lastVal["relevant"])
			}
		}
		// Читаем результаты веб-поиска из State
		if webResults, ok := stepCtx.State["web_search_results"].([]interface{}); ok {
			// Преобразуем []interface{} (map[string]interface{}) в []map[string]interface{}
			// для использования в webSearchResults
			// Очищаем текущий список, если он был заполнен из PreviousOutputs (State приоритетнее)
			if len(webSearchResults) > 0 {
				if e.logger != nil {
					e.logger.Printf("[PlanCalibrateExecutor] Overwriting web search results from PreviousOutputs with Shared State data")
				}
				webSearchResults = make([]map[string]interface{}, 0)
			}

			for _, r := range webResults {
				if rm, ok := r.(map[string]interface{}); ok {
					webSearchResults = append(webSearchResults, rm)
				}
			}
			if e.logger != nil {
				e.logger.Printf("[PlanCalibrateExecutor] Found %d web search results in Shared State", len(webSearchResults))
			}
		} else if webResultsMap, ok := stepCtx.State["web_search_results"].([]map[string]interface{}); ok {
			// Если вдруг тип уже правильный (внутри одного процесса)
			if len(webSearchResults) > 0 {
				webSearchResults = make([]map[string]interface{}, 0)
			}
			webSearchResults = append(webSearchResults, webResultsMap...)
			if e.logger != nil {
				e.logger.Printf("[PlanCalibrateExecutor] Found %d web search results in Shared State (direct map)", len(webSearchResults))
			}
		}

		// Читаем processed_channels из State (список всех обработанных каналов, даже неудачных)
		if processed, ok := stepCtx.State["processed_channels"].([]interface{}); ok {
			for _, c := range processed {
				if s, ok := c.(string); ok {
					// Добавляем в checkedChannels если еще нет
					isNew := true
					for _, existing := range checkedChannels {
						if existing == s {
							isNew = false
							break
						}
					}
					if isNew {
						checkedChannels = append(checkedChannels, s)
					}
				}
			}
			if e.logger != nil {
				e.logger.Printf("[PlanCalibrateExecutor] Found %d processed channels in Shared State", len(processed))
			}
		} else if processed, ok := stepCtx.State["processed_channels"].([]string); ok {
			for _, s := range processed {
				// Добавляем в checkedChannels если еще нет
				isNew := true
				for _, existing := range checkedChannels {
					if existing == s {
						isNew = false
						break
					}
				}
				if isNew {
					checkedChannels = append(checkedChannels, s)
				}
			}
			if e.logger != nil {
				e.logger.Printf("[PlanCalibrateExecutor] Found %d processed channels in Shared State (string slice)", len(processed))
			}
		}
	}

	for stepID, output := range stepCtx.PreviousOutputs {
		if e.logger != nil {
			e.logger.Printf("[PlanCalibrateExecutor] Previous step %s output keys: %v", stepID, getMapKeys(output))
		}

		// Найти результаты web_search
		if results, ok := output["results"].([]interface{}); ok {
			for _, r := range results {
				if rm, ok := r.(map[string]interface{}); ok {
					webSearchResults = append(webSearchResults, rm)
				}
			}
		}

		// Найти результаты telegram_channel_read
		if channel, ok := output["channel"].(string); ok {
			checkedChannels = append(checkedChannels, channel)

			entry := map[string]interface{}{
				"channel": channel,
			}

			if messages, ok := output["messages"].([]interface{}); ok && len(messages) > 0 {
				entry["messages"] = messages
			} else if errStr, ok := output["error"].(string); ok {
				entry["error"] = errStr
			} else {
				entry["status"] = "no_content"
			}

			telegramReadResults = append(telegramReadResults, entry)
		}

		// Найти результаты валидации (из llm_validate) - добавляем из истории, если их еще нет
		if relevant, ok := output["relevant"].(bool); ok {
			// Проверяем дубликаты (по itemId, если есть)
			isDuplicate := false
			itemID, _ := output["itemId"].(string)
			for _, existing := range validationResults {
				if existingID, ok := existing["itemId"].(string); ok && existingID == itemID && itemID != "" {
					isDuplicate = true
					break
				}
			}

			if !isDuplicate {
				validationResult := map[string]interface{}{
					"relevant":   relevant,
					"itemId":     output["itemId"],
					"confidence": output["confidence"],
					"reason":     output["reason"],
				}
				validationResults = append(validationResults, validationResult)
			}
		}

		// Найти созданные источники
		if sourceId, ok := output["sourceId"].(string); ok {
			createdSources = append(createdSources, sourceId)
		}
	}

	// Получить цель
	goalStr := ""
	if goal, ok := stepCtx.Input["goal"].(map[string]interface{}); ok {
		if desc, ok := goal["description"].(string); ok {
			goalStr = desc
		}
	} else if goalStrRaw, ok := stepCtx.Input["goal"].(string); ok {
		goalStr = goalStrRaw
	}

	// Определяем последний обработанный элемент
	lastProcessedItem := ""
	if len(checkedChannels) > 0 {
		lastProcessedItem = checkedChannels[len(checkedChannels)-1]
	} else if len(telegramReadResults) > 0 {
		lastResult := telegramReadResults[len(telegramReadResults)-1]
		if ch, ok := lastResult["channel"].(string); ok {
			lastProcessedItem = ch
		}
	}

	// Получить список доступных возможностей
	availableCaps := formatAvailableCapabilities(e.capabilityRegistry)

	// Строим универсальный промпт для итератора
	prompt := fmt.Sprintf(`You are an AI Agent acting as an ITERATIVE PLANNER. Your task is to process a list of items one by one to achieve a Goal.

GOAL: %s
PROJECT ID: %s

AVAILABLE CAPABILITIES:
%s

CONTEXT & STATE:
- Input Data (e.g. Search Results): %d items found
- Items Already Processed (IDs/Names): %v
- Created Resources: %d
- Last Processed Item ID: "%s"

INPUT DATA CONTENT:
%s

LAST ACTION RESULTS (e.g. Read/Fetch):
%s

VALIDATION RESULTS (from llm_validate):
%s

ITERATION STRATEGY (The Loop):
1. ANALYZE Input Data to extract potential Items (IDs, Names, URLs) relevant to the Goal.
   - IMPORTANT: Extract items that can be processed with AVAILABLE CAPABILITIES.
   - Example: If only 'telegram_channel_read' is available, extract Telegram channel usernames/links, NOT generic URLs.
2. FILTER out Items that are listed in "Items Already Processed".
3. SELECT the NEXT single Item to process.
   - If an item was JUST processed (Last Processed Item), look at its Validation Result.
   - If Validated & Relevant -> Plan creation step (e.g. source_create).
   - If Validated & Not Relevant -> Skip.
   - If Not Validated yet -> Plan validation step (llm_validate).
4. IF a new Item is selected -> Plan the action step to fetch it using an AVAILABLE CAPABILITY.
5. ALWAYS plan 'plan_calibrate' as the last step to continue the loop.

DECISION LOGIC:

A. IF (No Item is currently being processed) OR (Last Item was fully handled):
   - Look at Input Data.
   - Pick the FIRST item that is NOT in "Items Already Processed".
   - Suggest: Action Step (MUST be from AVAILABLE CAPABILITIES) for this item.
   - Suggest: plan_calibrate.

B. IF (Last Item was just fetched/read) AND (No Validation Result):
   - Suggest: llm_validate to check relevance.
   - Input: content from Last Action Results, goal, and itemId.
   - Suggest: plan_calibrate.

C. IF (Last Item was Validated) AND (Relevant == true):
   - Suggest: Resource Creation Step (MUST be from AVAILABLE CAPABILITIES, e.g. source_create).
   - Suggest: plan_calibrate.

D. IF (Last Item was Validated) AND (Relevant == false):
   - Log reason for skipping.
   - Proceed to Logic A (Pick next item).

E. IF (All Items Processed) OR (Enough Resources Created):
   - Set goalAchieved = true.
   - additionalStepsNeeded = false.

CRITICAL RULES:
- Return ONLY valid JSON.
- Use exact field names.
- "extractedItems": List of all potential item IDs found in Input Data.
- "nextItemToCheck": The specific ID of the item you decided to process next.
- USE ONLY CAPABILITIES LISTED ABOVE. Do not hallucinate 'fetch_url' or others.

JSON SCHEMA:
{
  "goalAchieved": boolean,
  "additionalStepsNeeded": boolean,
  "extractedItems": ["id1", "id2"],
  "nextItemToCheck": "id1" (or null),
  "lastItemRelevant": boolean,
  "suggestedSteps": [
    { "capability": "...", "input": {...}, "reason": "...", "priority": 1 }
  ],
  "reason": "Detailed explanation of decision"
}
`,
		goalStr,
		stepCtx.ProjectID,
		availableCaps,
		len(webSearchResults),
		checkedChannels, // In universal context, these are "Processed Items"
		len(createdSources),
		lastProcessedItem,
		formatWebSearchResults(webSearchResults), // Pass all results, let LLM extract
		formatTelegramResults(telegramReadResults), // TODO: Make this generic formatLastActionResults
		formatValidationResults(validationResults),
	)

	// Определить JSON схему для строгого вывода
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"goalAchieved": map[string]interface{}{
				"type": "boolean",
			},
			"additionalStepsNeeded": map[string]interface{}{
				"type": "boolean",
			},
			"extractedItems": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "List of all potential item IDs (e.g. usernames, URLs) extracted from input",
			},
			"nextItemToCheck": map[string]interface{}{
				"type":        "string",
				"description": "The ID of the next item to process",
			},
			"lastItemRelevant": map[string]interface{}{
				"type":        "boolean",
				"description": "Whether the last processed item was relevant",
			},
			"suggestedSteps": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"capability": map[string]interface{}{
							"type": "string",
						},
						"input": map[string]interface{}{
							"type": "object",
						},
						"reason": map[string]interface{}{
							"type": "string",
						},
						"priority": map[string]interface{}{
							"type": "integer",
						},
					},
					"required": []string{"capability", "input", "reason"},
				},
			},
			"reason": map[string]interface{}{
				"type": "string",
			},
		},
		"required": []string{"goalAchieved", "additionalStepsNeeded", "suggestedSteps", "reason"},
	}

	// Вызвать LLM для анализа с строгой JSON типизацией
	response, err := e.genaiClient.GenerateJSONStrict(ctx, prompt, nil, schema, gen_combined.Options{})
	if err != nil {
		return &CapabilityResult{
			Success:         false,
			Error:           stringPtr(err.Error()),
			ExecutionStatus: models.ExecutionStatusFailed,
			Retryable:       true,
			Reason:          fmt.Sprintf("Failed to analyze results: %v", err),
		}, nil
	}

	// Извлечь JSON из ответа (аналогично PlanGenerateExecutor)
	jsonContent := response.Content

	// Если Content пустой или не начинается с JSON, проверяем ToolCalls (для Gemini strict JSON mode)
	if (jsonContent == "" || (len(jsonContent) > 0 && jsonContent[0] != '{')) && len(response.ToolCalls) > 0 {
		for _, tc := range response.ToolCalls {
			if tc.Name == "return_structured" && tc.Arguments != "" {
				jsonContent = tc.Arguments
				if e.logger != nil {
					e.logger.Printf("[PlanCalibrateExecutor] Extracted JSON from return_structured tool call")
				}
				break
			}
		}
	}

	// Если все еще пусто или не JSON, используем RawJSON
	if jsonContent == "" || (len(jsonContent) > 0 && jsonContent[0] != '{') {
		if response.RawJSON != "" {
			jsonContent = response.RawJSON
		}
	}

	// Убираем markdown code fences если есть (только для совместимости, GenerateJSONStrict не должен их возвращать)
	jsonContent = cleanJSONContent(jsonContent)

	// Строгая типизация: GenerateJSONStrict должен возвращать валидный JSON
	// Если JSON невалиден - это ошибка, не пытаемся извлекать из текста
	if jsonContent == "" {
		return &CapabilityResult{
			Success:         false,
			Error:           stringPtr("GenerateJSONStrict returned empty JSON content"),
			ExecutionStatus: models.ExecutionStatusFailed,
			Retryable:       true,
			Reason:          "LLM did not return valid JSON - this should not happen with GenerateJSONStrict",
		}, nil
	}

	// Проверяем, что это валидный JSON (начинается с {)
	if len(jsonContent) > 0 && jsonContent[0] != '{' {
		return &CapabilityResult{
			Success:         false,
			Error:           stringPtr(fmt.Sprintf("GenerateJSONStrict returned non-JSON content (starts with '%c')", jsonContent[0])),
			ExecutionStatus: models.ExecutionStatusFailed,
			Retryable:       true,
			Reason:          "LLM returned text instead of JSON - this violates strict JSON mode",
		}, nil
	}

	// Парсить ответ LLM с строгой типизацией
	var analysis map[string]interface{}
	if err := json.Unmarshal([]byte(jsonContent), &analysis); err != nil {
		if e.logger != nil {
			e.logger.Printf("[PlanCalibrateExecutor] Failed to parse JSON: %v, content preview: %s", err, jsonContent[:min(500, len(jsonContent))])
		}
		return &CapabilityResult{
			Success:         false,
			Error:           stringPtr(fmt.Sprintf("Failed to parse JSON: %v", err)),
			ExecutionStatus: models.ExecutionStatusFailed,
			Retryable:       true,
			Reason:          fmt.Sprintf("Invalid JSON returned by LLM: %v", err),
		}, nil
	}

	goalAchieved := false
	if ga, ok := analysis["goalAchieved"].(bool); ok {
		goalAchieved = ga
	}

	additionalStepsNeeded := false
	if asn, ok := analysis["additionalStepsNeeded"].(bool); ok {
		additionalStepsNeeded = asn
	}

	suggestedSteps := make([]SuggestedStep, 0)
	if stepsRaw, ok := analysis["suggestedSteps"].([]interface{}); ok {
		for _, stepRaw := range stepsRaw {
			if stepMap, ok := stepRaw.(map[string]interface{}); ok {
				// Строгая типизация: используем ТОЛЬКО поле "capability" как объявлено в схеме
				capability, _ := stepMap["capability"].(string)
				if capability == "" {
					// Пропускаем шаг без capability - это нарушение схемы
					if e.logger != nil {
						e.logger.Printf("[PlanCalibrateExecutor] Dropping step without 'capability' field (required by schema)")
					}
					continue
				}
				input := extractSuggestedInput(stepMap)
				reason, _ := stepMap["reason"].(string)
				priority := 0
				if p, ok := stepMap["priority"].(float64); ok {
					priority = int(p)
				}
				suggestedSteps = append(suggestedSteps, SuggestedStep{
					Capability: capability,
					Input:      input,
					Reason:     reason,
					Priority:   priority,
				})
			}
		}
	}

	// Отфильтровать шаги без конкретных параметров (например, с плейсхолдерами)
	suggestedSteps = e.filterSuggestedSteps(suggestedSteps)

	// УЛУЧШЕННАЯ ЛОГИКА: Создаем явные пары шагов для итерационных задач
	// Паттерн: action_step → plan_calibrate (для проверки результата и продолжения)
	if len(suggestedSteps) > 0 && !goalAchieved {
		// Проверяем, есть ли action-шаги (read, create) без последующего plan_calibrate
		hasActionStep := false
		hasCalibrateAfterAction := false

		for i, s := range suggestedSteps {
			// Определяем action-шаги
			if s.Capability == string(CapabilityTelegramChannelRead) ||
				s.Capability == string(CapabilitySourceCreate) {
				hasActionStep = true
				// Проверяем, есть ли plan_calibrate после этого action-шага
				if i+1 < len(suggestedSteps) {
					if suggestedSteps[i+1].Capability == string(CapabilityPlanCalibrate) {
						hasCalibrateAfterAction = true
					}
				}
			}
		}

		// Если есть action-шаги, но нет plan_calibrate после них - добавляем
		if hasActionStep && !hasCalibrateAfterAction {
			// Находим последний action-шаг
			lastActionIdx := -1
			for i := len(suggestedSteps) - 1; i >= 0; i-- {
				if suggestedSteps[i].Capability == string(CapabilityTelegramChannelRead) ||
					suggestedSteps[i].Capability == string(CapabilitySourceCreate) {
					lastActionIdx = i
					break
				}
			}

			// Добавляем plan_calibrate после последнего action-шага
			if lastActionIdx >= 0 {
				calibrateStep := SuggestedStep{
					Capability: string(CapabilityPlanCalibrate),
					Input:      stepCtx.Input, // Передаем тот же input (goal и т.д.)
					Reason:     "Review results of the action, check relevance, and plan next steps",
					Priority:   2,
				}
				// Вставляем после последнего action-шага
				newSteps := make([]SuggestedStep, 0, len(suggestedSteps)+1)
				newSteps = append(newSteps, suggestedSteps[:lastActionIdx+1]...)
				newSteps = append(newSteps, calibrateStep)
				if lastActionIdx+1 < len(suggestedSteps) {
					newSteps = append(newSteps, suggestedSteps[lastActionIdx+1:]...)
				}
				suggestedSteps = newSteps

				if e.logger != nil {
					e.logger.Printf("[PlanCalibrateExecutor] Added plan_calibrate after action step at index %d", lastActionIdx)
				}
			}
		}

		// Если нет action-шагов, но есть plan_calibrate - это нормально (может быть финальная проверка)
		// Если нет ни action, ни calibrate - добавляем calibrate для продолжения
		if !hasActionStep {
			hasCalibrate := false
			for _, s := range suggestedSteps {
				if s.Capability == string(CapabilityPlanCalibrate) {
					hasCalibrate = true
					break
				}
			}
			if !hasCalibrate {
				suggestedSteps = append(suggestedSteps, SuggestedStep{
					Capability: string(CapabilityPlanCalibrate),
					Input:      stepCtx.Input,
					Reason:     "Review current state and plan next steps",
					Priority:   2,
				})
			}
		}
	}

	if e.logger != nil {
		e.logger.Printf("[PlanCalibrateExecutor] Created %d suggested steps with concrete values", len(suggestedSteps))
		for i, s := range suggestedSteps {
			e.logger.Printf("[PlanCalibrateExecutor]   Step %d: %s, input: %v", i+1, s.Capability, s.Input)
		}
	}

	reason := ""
	if r, ok := analysis["reason"].(string); ok {
		reason = r
	}

	confidence := 0.8
	if goalAchieved {
		confidence = 1.0
	}

	executionStatus := models.ExecutionStatusSuccess
	if additionalStepsNeeded && len(suggestedSteps) > 0 {
		executionStatus = models.ExecutionStatusPartial
	}

	// Извлечь дополнительные данные из анализа
	if e.logger != nil {
		e.logger.Printf("[PlanCalibrateExecutor] LLM analysis result: %+v", analysis)
	}

	extractedItems := make([]string, 0)
	if items, ok := analysis["extractedItems"].([]interface{}); ok {
		for _, item := range items {
			if itemStr, ok := item.(string); ok {
				extractedItems = append(extractedItems, itemStr)
			}
		}
	}
	if e.logger != nil {
		e.logger.Printf("[PlanCalibrateExecutor] Extracted items: %v", extractedItems)
	}

	// Строгая типизация: используем ТОЛЬКО поле "nextItemToCheck" как объявлено в схеме
	nextItemToCheck := ""
	if next, ok := analysis["nextItemToCheck"].(string); ok && next != "" {
		nextItemToCheck = next
	}
	// Fallback на extractedItems только если nextItemToCheck не указан
	if nextItemToCheck == "" && len(extractedItems) > 0 {
		nextItemToCheck = extractedItems[0]
	}
	if e.logger != nil {
		e.logger.Printf("[PlanCalibrateExecutor] Final nextItemToCheck: '%s'", nextItemToCheck)
	}

	lastItemRelevant := false
	if rel, ok := analysis["lastItemRelevant"].(bool); ok {
		lastItemRelevant = rel
	}

	// НЕ устанавливаем SuggestsReplan если есть SuggestedSteps -
	// шаги будут добавлены через addSuggestedSteps, полный replan не нужен
	return &CapabilityResult{
		Success: true,
		Output: map[string]interface{}{
			"goalAchieved":          goalAchieved,
			"additionalStepsNeeded": additionalStepsNeeded,
			"extractedItems":        extractedItems,
			"nextItemToCheck":       nextItemToCheck,
			"lastItemRelevant":      lastItemRelevant,
			"processedItems":        checkedChannels, // Use generic name in output
			"createdSources":        createdSources,
			// Legacy aliases for compatibility if needed by other components
			"extractedChannels":  extractedItems,
			"nextChannelToCheck": nextItemToCheck,
			"channelRelevant":    lastItemRelevant,
			"checkedChannels":    checkedChannels,
		},
		Message:         reason,
		ExecutionStatus: executionStatus,
		Confidence:      &confidence,
		Retryable:       false,
		Reason:          reason,
		SuggestsReplan:  false, // Не триггерим replan - шаги добавляются через SuggestedSteps
		ReplanReason:    "",
		SuggestedSteps:  suggestedSteps,
	}, nil
}

// Helper functions

func formatStepsForPrompt(steps []map[string]interface{}) string {
	if len(steps) == 0 {
		return "No completed steps yet."
	}
	b, _ := json.MarshalIndent(steps, "", "  ")
	return string(b)
}

func formatAvailableCapabilities(registry *CapabilityRegistry) string {
	capabilities := registry.List()
	capNames := make([]string, 0, len(capabilities))
	for _, cap := range capabilities {
		capNames = append(capNames, string(cap))
	}
	return strings.Join(capNames, ", ")
}

func formatWebSearchResults(results []map[string]interface{}) string {
	if len(results) == 0 {
		return "No web search results."
	}
	var sb strings.Builder
	for i, r := range results {
		title, _ := r["title"].(string)
		url, _ := r["url"].(string)
		content, _ := r["content"].(string)
		sb.WriteString(fmt.Sprintf("%d. Title: %s\n   URL: %s\n   Content: %s\n\n", i+1, title, url, truncateString(content, 300)))
	}
	return sb.String()
}

func formatTelegramResults(results []map[string]interface{}) string {
	if len(results) == 0 {
		return "No Telegram channels read yet."
	}
	var sb strings.Builder
	for _, r := range results {
		channel, _ := r["channel"].(string)

		if errStr, ok := r["error"].(string); ok {
			sb.WriteString(fmt.Sprintf("Channel @%s: FAILED - %s\n\n", channel, errStr))
			continue
		}

		messages, _ := r["messages"].([]interface{})
		if len(messages) == 0 {
			sb.WriteString(fmt.Sprintf("Channel @%s: NO CONTENT (read successfully but found 0 messages)\n\n", channel))
			continue
		}

		sb.WriteString(fmt.Sprintf("Channel @%s (%d messages):\n", channel, len(messages)))
		// Show first 3 messages as sample
		for i, msg := range messages {
			if i >= 3 {
				sb.WriteString(fmt.Sprintf("  ... and %d more messages\n", len(messages)-3))
				break
			}
			if msgMap, ok := msg.(map[string]interface{}); ok {
				text, _ := msgMap["text"].(string)
				sb.WriteString(fmt.Sprintf("  - %s\n", truncateString(text, 150)))
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func formatValidationResults(results []map[string]interface{}) string {
	if len(results) == 0 {
		return "No validation results yet."
	}
	var sb strings.Builder
	for _, r := range results {
		relevant, _ := r["relevant"].(bool)
		itemId, _ := r["itemId"].(string)
		confidence, _ := r["confidence"].(float64)
		reason, _ := r["reason"].(string)
		sb.WriteString(fmt.Sprintf("Item %s: relevant=%v, confidence=%.2f, reason=%s\n", itemId, relevant, confidence, reason))
	}
	return sb.String()
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func (e *PlanCalibrateExecutor) filterSuggestedSteps(steps []SuggestedStep) []SuggestedStep {
	cleaned := make([]SuggestedStep, 0, len(steps))
	for _, step := range steps {
		if step.Capability == "" {
			if e.logger != nil {
				e.logger.Printf("[PlanCalibrateExecutor] Dropping suggested step with empty capability: %+v", step)
			}
			continue
		}

		if hasPlaceholderInput(step.Input) {
			if e.logger != nil {
				e.logger.Printf("[PlanCalibrateExecutor] Dropping step %s due to placeholder values: %+v", step.Capability, step.Input)
			}
			continue
		}

		switch step.Capability {
		case string(CapabilityTelegramChannelRead):
			username, _ := step.Input["channelUsername"].(string)
			if username == "" || isPlaceholderString(username) {
				if e.logger != nil {
					e.logger.Printf("[PlanCalibrateExecutor] Dropping telegram_channel_read without concrete username: %+v", step.Input)
				}
				continue
			}
		case string(CapabilitySourceCreate):
			username, _ := step.Input["channelUsername"].(string)
			displayName, _ := step.Input["displayName"].(string)
			projectID, _ := step.Input["projectId"].(string)
			if username == "" || isPlaceholderString(username) || displayName == "" || isPlaceholderString(displayName) || projectID == "" {
				if e.logger != nil {
					e.logger.Printf("[PlanCalibrateExecutor] Dropping source_create without concrete fields: %+v", step.Input)
				}
				continue
			}
		}

		cleaned = append(cleaned, step)
	}
	return cleaned
}

func hasPlaceholderInput(input map[string]interface{}) bool {
	for _, v := range input {
		if hasPlaceholderValue(v) {
			return true
		}
	}
	return false
}

func hasPlaceholderValue(value interface{}) bool {
	switch v := value.(type) {
	case string:
		return isPlaceholderString(v)
	case []interface{}:
		for _, item := range v {
			if hasPlaceholderValue(item) {
				return true
			}
		}
	case map[string]interface{}:
		for _, item := range v {
			if hasPlaceholderValue(item) {
				return true
			}
		}
	}
	return false
}

func isPlaceholderString(s string) bool {
	if s == "" {
		return true
	}
	normalized := strings.ToLower(s)
	return strings.Contains(s, "${") ||
		strings.Contains(normalized, "placeholder") ||
		strings.Contains(normalized, "tbd") ||
		strings.Contains(normalized, "<pending>") ||
		strings.Contains(normalized, "doc-id")
}

func extractSuggestedInput(stepMap map[string]interface{}) map[string]interface{} {
	if in, ok := stepMap["input"].(map[string]interface{}); ok && len(in) > 0 {
		return in
	}

	fallbackKeys := []string{
		"channelUsername", "projectId", "displayName", "kind",
		"query", "maxResults", "reason",
	}
	fallback := make(map[string]interface{})
	for _, key := range fallbackKeys {
		if val, ok := stepMap[key]; ok {
			fallback[key] = val
		}
	}

	return fallback
}
