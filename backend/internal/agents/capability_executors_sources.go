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

	messages, err := e.telegramClient.FetchUpdates(ctx, channelUsername, lastMessageID)
	if err != nil {
		return &CapabilityResult{
			Success:         false,
			Error:           stringPtr(err.Error()),
			ExecutionStatus: models.ExecutionStatusFailed,
			Retryable:       true,
			Reason:          fmt.Sprintf("Failed to fetch messages from @%s: %v", channelUsername, err),
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
			if messages, ok := output["messages"].([]interface{}); ok && len(messages) > 0 {
				telegramReadResults = append(telegramReadResults, map[string]interface{}{
					"channel":  channel,
					"messages": messages,
				})
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

	// Строим промпт для анализа
	prompt := fmt.Sprintf(`Analyze the current state and determine the next action.

PROJECT CONTEXT:
- Project ID: %s
- Goal: %s

CURRENT STATE:
- Web search results found: %d
- Telegram channels already checked: %v
- Sources already created: %d

WEB SEARCH RESULTS (extract Telegram channel usernames from URLs like t.me/username or @username):
%s

TELEGRAM CHANNELS READ (analyze if content is relevant to goal):
%s

TASK:
1. If web_search has results but no channels checked yet: extract ONE channel username and suggest telegram_channel_read
2. If a channel was read: analyze if its content is relevant to the goal, then either:
   - If relevant: suggest source_create for it
   - If not relevant or already have enough sources: suggest checking next channel or mark goal achieved
3. If all channels checked or enough sources created (2-3): set goalAchieved=true

IMPORTANT:
- Extract channel usernames from t.me/USERNAME or @USERNAME patterns
- Only suggest ONE step at a time
- Use exact channelUsername without @ prefix
- For source_create use projectId: "%s"
- NEVER output placeholders like ${step3.channelUsername} or "pending" — return the exact usernames/IDs you observed above.

Return JSON in this EXACT format:
{
  "goalAchieved": false,
  "additionalStepsNeeded": true,
  "extractedChannels": ["channel1", "channel2"],
  "nextChannelToCheck": "channel1",
  "channelRelevant": false,
  "suggestedSteps": [],
  "reason": "explanation"
}`,
		stepCtx.ProjectID,
		goalStr,
		len(webSearchResults),
		checkedChannels,
		len(createdSources),
		formatWebSearchResults(webSearchResults),
		formatTelegramResults(telegramReadResults),
		stepCtx.ProjectID,
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
			"extractedChannels": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "List of Telegram channel usernames extracted from web search results",
			},
			"nextChannelToCheck": map[string]interface{}{
				"type":        "string",
				"description": "The next channel username to check (without @)",
			},
			"channelRelevant": map[string]interface{}{
				"type":        "boolean",
				"description": "Whether the last checked channel is relevant to the goal",
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
							"type":        "object",
							"description": "MUST contain REAL values, NOT placeholders like ${step.field}",
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

	// Убираем markdown code fences если есть
	jsonContent = cleanJSONContent(jsonContent)

	// Попытка извлечь JSON из текста, если Content начинается с текста
	if jsonContent == "" || (len(jsonContent) > 0 && jsonContent[0] != '{') {
		// Ищем JSON объект в тексте
		if idx := strings.Index(response.Content, "{"); idx != -1 {
			// Находим последнюю закрывающую скобку
			lastIdx := strings.LastIndex(response.Content, "}")
			if lastIdx > idx {
				jsonContent = response.Content[idx : lastIdx+1]
				if e.logger != nil {
					e.logger.Printf("[PlanCalibrateExecutor] Extracted JSON from text content")
				}
			}
		}
	}

	// Парсить ответ LLM
	var analysis map[string]interface{}
	if err := json.Unmarshal([]byte(jsonContent), &analysis); err != nil {
		if e.logger != nil {
			e.logger.Printf("[PlanCalibrateExecutor] Failed to parse JSON: %v, content: %s", err, jsonContent)
		}
		// Если не удалось распарсить как JSON, попробовать извлечь JSON из текста
		analysis = parseJSONFromText(jsonContent)
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
				capability, _ := stepMap["capability"].(string)
				if capability == "" {
					if action, ok := stepMap["action"].(string); ok {
						capability = action
					}
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

	// ВАЖНО: Если мы добавляем шаги действий (чтение/создание), нужно обязательно добавить следующий шаг калибровки,
	// чтобы цикл планирования продолжился после выполнения действия.
	if len(suggestedSteps) > 0 && !goalAchieved {
		// Проверяем, нет ли уже шага калибровки в предложенных
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
				Input:      stepCtx.Input, // Передаем тот же input (goal и т.д.)
				Reason:     "Review results of the action and plan next steps",
				Priority:   2,
			})
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

	extractedChannels := make([]string, 0)
	if channels, ok := analysis["extractedChannels"].([]interface{}); ok {
		for _, ch := range channels {
			if chStr, ok := ch.(string); ok {
				extractedChannels = append(extractedChannels, chStr)
			}
		}
	}
	if e.logger != nil {
		e.logger.Printf("[PlanCalibrateExecutor] Extracted channels: %v", extractedChannels)
	}

	nextChannelToCheck := ""
	// Попробовать разные варианты где LLM может вернуть channelUsername
	if next, ok := analysis["nextChannelToCheck"].(string); ok && next != "" {
		nextChannelToCheck = next
	}
	if nextChannelToCheck == "" {
		if next, ok := analysis["channelUsername"].(string); ok && next != "" {
			nextChannelToCheck = next
		}
	}
	if nextChannelToCheck == "" {
		if suggestedAction, ok := analysis["suggestedAction"].(map[string]interface{}); ok {
			if ch, ok := suggestedAction["channelUsername"].(string); ok && ch != "" {
				nextChannelToCheck = ch
			}
		}
	}
	if nextChannelToCheck == "" {
		if params, ok := analysis["parameters"].(map[string]interface{}); ok {
			if ch, ok := params["channelUsername"].(string); ok && ch != "" {
				nextChannelToCheck = ch
			}
		}
	}
	// Если всё ещё пусто, но есть extractedChannels - взять первый
	if nextChannelToCheck == "" && len(extractedChannels) > 0 {
		nextChannelToCheck = extractedChannels[0]
	}
	if e.logger != nil {
		e.logger.Printf("[PlanCalibrateExecutor] Final nextChannelToCheck: '%s' (analysis keys: %v)", nextChannelToCheck, getMapKeys(analysis))
	}

	channelRelevant := false
	if rel, ok := analysis["channelRelevant"].(bool); ok {
		channelRelevant = rel
	}

	// НЕ устанавливаем SuggestsReplan если есть SuggestedSteps -
	// шаги будут добавлены через addSuggestedSteps, полный replan не нужен
	return &CapabilityResult{
		Success: true,
		Output: map[string]interface{}{
			"goalAchieved":          goalAchieved,
			"additionalStepsNeeded": additionalStepsNeeded,
			"extractedChannels":     extractedChannels,
			"nextChannelToCheck":    nextChannelToCheck,
			"channelUsername":       nextChannelToCheck, // Алиас для совместимости с планами LLM
			"channelRelevant":       channelRelevant,
			"checkedChannels":       checkedChannels,
			"createdSources":        createdSources,
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
		messages, _ := r["messages"].([]interface{})
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

func parseJSONFromText(text string) map[string]interface{} {
	// Попытаться найти JSON в тексте
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end > start {
		jsonStr := text[start : end+1]
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(jsonStr), &result); err == nil {
			return result
		}
	}
	// Если не удалось, вернуть пустой объект
	return map[string]interface{}{}
}
