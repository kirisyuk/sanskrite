package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// DomainAgentAdapter адаптирует DomainAgent к универсальному интерфейсу Agent
// Реализует Agent через встраивание BaseAgent методов DomainAgent
type DomainAgentAdapter struct {
	DomainAgent // Встраивание для доступа к BaseAgent методам
	logger      *log.Logger
}

// NewDomainAgentAdapter создает новый адаптер для DomainAgent
func NewDomainAgentAdapter(domainAgent DomainAgent, logger *log.Logger) *DomainAgentAdapter {
	return &DomainAgentAdapter{
		DomainAgent: domainAgent,
		logger:      logger,
	}
}

// ID возвращает ID агента (использует Domain)
func (a *DomainAgentAdapter) ID() string {
	return a.Domain()
}

// DisplayName возвращает отображаемое имя (использует Name из BaseAgent)
func (a *DomainAgentAdapter) DisplayName() string {
	return a.Name()
}

// Description возвращает описание (из Capabilities)
func (a *DomainAgentAdapter) Description() string {
	caps := a.Capabilities()
	if len(caps) > 0 {
		return fmt.Sprintf("Domain agent for %s: %v", a.Domain(), caps)
	}
	return fmt.Sprintf("Domain agent for %s", a.Domain())
}

// Skills возвращает навыки (из Capabilities для совместимости с BaseAgent)
func (a *DomainAgentAdapter) Skills() []string {
	return a.Capabilities()
}

// TaskSchema возвращает JSON Schema для задачи DomainAgent
func (a *DomainAgentAdapter) TaskSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"context": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"tenantId":  map[string]interface{}{"type": "string"},
					"projectId": map[string]interface{}{"type": "string"},
					"userId":    map[string]interface{}{"type": "string"},
				},
				"required": []string{"tenantId", "projectId", "userId"},
			},
			"query": map[string]interface{}{
				"type": "string",
			},
			"intent": map[string]interface{}{
				"type": "string",
				"enum": []string{"analyze", "extract", "generate"},
				"default": "analyze",
			},
			"options": map[string]interface{}{
				"type": "object",
			},
		},
		"required": []string{"context", "query"},
	}
}

// ResultSchema возвращает JSON Schema для результата DomainAgent
func (a *DomainAgentAdapter) ResultSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"data": map[string]interface{}{
				"type": "object",
			},
			"confidence": map[string]interface{}{
				"type":    "number",
				"minimum": 0,
				"maximum": 1,
			},
			"processingTime": map[string]interface{}{
				"type": "string",
			},
			"sources": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
				},
			},
			"processingNotes": map[string]interface{}{
				"type": "string",
			},
		},
		"required": []string{"data", "confidence"},
	}
}

// ExecuteAsTask реализует универсальный Agent интерфейс
// Преобразует taskJSON в AgentQueryRequest и вызывает HandleQuery
func (a *DomainAgentAdapter) ExecuteAsTask(
	ctx context.Context,
	taskJSON []byte,
	onUpdate func(Update),
) ([]byte, error) {
	startTime := time.Now()
	taskID := generateTaskID()

	// Парсим taskJSON как структуру с context и query
	var task struct {
		Context AgentContext          `json:"context"`
		Query   string                `json:"query"`
		Intent  string                `json:"intent,omitempty"`
		Options map[string]interface{} `json:"options,omitempty"`
	}

	if err := json.Unmarshal(taskJSON, &task); err != nil {
		if a.logger != nil {
			a.logger.Printf("[DomainAgentAdapter] Task unmarshal failed: %v", err)
		}
		return nil, fmt.Errorf("invalid task schema: %w", err)
	}

	// Валидация projectId
	if task.Context.ProjectID == "" {
		if a.logger != nil {
			a.logger.Printf("[DomainAgentAdapter] Task validation failed: projectId is required")
		}
		return nil, fmt.Errorf("projectId is required")
	}

	// Валидация query
	if task.Query == "" {
		if a.logger != nil {
			a.logger.Printf("[DomainAgentAdapter] Task validation failed: query is required")
		}
		return nil, fmt.Errorf("query is required")
	}

	if task.Intent == "" {
		task.Intent = "analyze"
	}

	if a.logger != nil {
		a.logger.Printf("[DomainAgentAdapter] Task %s started: domain=%s, query=%q, intent=%s, projectId=%s",
			taskID, a.Domain(), task.Query, task.Intent, task.Context.ProjectID)
	}

	// Публикуем начало выполнения
	onUpdate(Update{
		TaskID:    taskID,
		AgentID:   a.ID(),
		AgentName: a.DisplayName(),
		Stage:     "analyzing",
		Progress:  0.1,
		Message:   fmt.Sprintf("Processing query: %s", task.Query),
		Timestamp: time.Now(),
	})

	// Создаем AgentQueryRequest
	req := &AgentQueryRequest{
		Query:     task.Query,
		ProjectID: task.Context.ProjectID,
		Intent:    task.Intent,
		Options:   task.Options,
	}

	// Вызываем DomainAgent.HandleQuery
	resp, err := a.HandleQuery(ctx, req)
	if err != nil {
		if a.logger != nil {
			a.logger.Printf("[DomainAgentAdapter] Task %s HandleQuery failed: %v", taskID, err)
		}
		onUpdate(Update{
			TaskID:    taskID,
			AgentID:   a.ID(),
			AgentName: a.DisplayName(),
			Stage:     "error",
			Progress:  0.0,
			Message:   fmt.Sprintf("Error: %v", err),
			Timestamp: time.Now(),
			Error:     stringPtr(err.Error()),
		})
		return nil, fmt.Errorf("domain agent query failed: %w", err)
	}

	// Формируем результат в формате Agent интерфейса
	result := map[string]interface{}{
		"data":            resp.Data,
		"confidence":      resp.Confidence,
		"processingTime":  resp.ProcessingTime.String(),
		"sources":         resp.Sources,
		"processingNotes": resp.ProcessingNotes,
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		if a.logger != nil {
			a.logger.Printf("[DomainAgentAdapter] Task %s result marshal failed: %v", taskID, err)
		}
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	if a.logger != nil {
		a.logger.Printf("[DomainAgentAdapter] Task %s completed successfully: confidence=%.2f, duration=%v",
			taskID, resp.Confidence, time.Since(startTime))
	}

	// Публикуем завершение
	onUpdate(Update{
		TaskID:    taskID,
		AgentID:   a.ID(),
		AgentName: a.DisplayName(),
		Stage:     "completed",
		Progress:  1.0,
		Message:   fmt.Sprintf("Query processed successfully. Confidence: %.2f", resp.Confidence),
		Timestamp: time.Now(),
	})

	return resultJSON, nil
}

// stringPtr создает указатель на строку
func stringPtr(s string) *string {
	return &s
}

