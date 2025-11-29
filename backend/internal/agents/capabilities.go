package agents

import (
	"context"
	"fmt"

	"github.com/igorkirisyuk/sanskrite/internal/models"
)

// Capability определяет тип шага, который может выполнить агент
type Capability string

const (
	// Критичные capabilities (сбой = остановка job)
	CapabilityDocumentRead  Capability = "document_read"  // Чтение документов
	CapabilityDocumentWrite Capability = "document_write" // Запись документов

	// Некритичные capabilities (сбой = пропуск шага, продолжение)
	CapabilityDataSearch          Capability = "data_search"           // Поиск данных
	CapabilityDataEnhancement     Capability = "data_enhancement"      // Улучшение данных
	CapabilityWebSearch           Capability = "web_search"           // Поиск через Tavily
	CapabilityEntityExtract         Capability = "entity_extract"           // Извлечение сущностей из текста
	CapabilityEntitySearch          Capability = "entity_search"            // Поиск сущностей с дедупликацией
	CapabilityEntityDeduplicate     Capability = "entity_deduplicate"       // Дедупликация сущностей
	CapabilityEntityRelationBuild   Capability = "entity_relation_build"    // Построение связей между сущностями
	CapabilityEntityEnrich          Capability = "entity_enrich"            // Обогащение сущности данными из Wikidata
	CapabilityEntityHierarchyBuild  Capability = "entity_hierarchy_build"   // Построение иерархии сущностей (поиск подчиненных)
	CapabilityEntityValidate        Capability = "entity_validate"          // Валидация сущностей (проверка полноты данных)
	CapabilityEntityMergeAttributes Capability = "entity_merge_attributes"   // Объединение атрибутов сущностей
	CapabilityConflictDetect        Capability = "conflict_detect"          // Обнаружение конфликтов между claims/сущностями
	CapabilityCausalLinkDetect      Capability = "causal_link_detect"       // Обнаружение причинно-следственных связей
	CapabilityRelationFind          Capability = "relation_find"             // Поиск существующих связей (не построение)
	CapabilityEventCreate         Capability = "event_create"          // Создание событий
	CapabilityArtifactCreate      Capability = "artifact_create"      // Создание артефактов
	CapabilityBigQueryQuery       Capability = "bigquery_query"        // Запросы к BigQuery
	CapabilityLLMGenerate         Capability = "llm_generate"          // Генерация через LLM
	CapabilityLLMAnalyze          Capability = "llm_analyze"           // Анализ через LLM

	// Планирование и репланирование
	CapabilityPlanGenerate Capability = "plan_generate" // Генерация плана из естественного языка
	CapabilityReplan       Capability = "replan"        // Корректировка плана на основе результатов
	CapabilityReflect      Capability = "reflect"       // Оценка прогресса относительно цели
	CapabilityPlanCalibrate Capability = "plan_calibrate" // Промежуточный шаг калибровки - просмотр результатов и допланирование

	// Управление источниками
	CapabilitySourceList              Capability = "source_list"               // Список источников проекта
	CapabilityTelegramChannelRead     Capability = "telegram_channel_read"     // Чтение данных из Telegram-канала
	CapabilitySourceCreate            Capability = "source_create"             // Создание источника и привязка к проекту
	CapabilityTelegramSourceDiscovery Capability = "telegram_source_discovery" // Автоматический поиск и добавление Telegram каналов
)

// IsCritical определяет критичность capability
func (c Capability) IsCritical() bool {
	return c == CapabilityDocumentRead || c == CapabilityDocumentWrite
}

// CapabilityExecutor интерфейс для выполнения capability
type CapabilityExecutor interface {
	// Capability возвращает тип capability
	Capability() Capability

	// Execute выполняет шаг
	Execute(ctx context.Context, step *AgentStepContext) (*CapabilityResult, error)

	// ValidateInput валидирует входные данные шага
	ValidateInput(input map[string]interface{}) error
}

// AgentStepContext контекст для выполнения шага
type AgentStepContext struct {
	JobID      string
	StepID     string
	TenantID   string
	ProjectID  string
	UserID     string
	Input      map[string]interface{}
	DependsOn  []string // ID шагов, от которых зависит этот шаг
	PreviousOutputs map[string]map[string]interface{} // Output предыдущих шагов (stepID -> output)
}

// SuggestedStep предложенный следующий шаг
type SuggestedStep struct {
	Capability string                 `json:"capability"`
	Input      map[string]interface{} `json:"input"`
	Reason     string                 `json:"reason"`   // Почему этот шаг предложен
	Priority   int                    `json:"priority"` // Приоритет (меньше = выше)
}

// CapabilityResult результат выполнения capability
type CapabilityResult struct {
	Success   bool                   `json:"success"`
	Output    map[string]interface{} `json:"output,omitempty"`
	Error     *string                `json:"error,omitempty"`
	Message   string                 `json:"message,omitempty"`
	NextSteps []string               `json:"nextSteps,omitempty"` // Опциональные следующие шаги (определяются динамически)

	// Детализированный статус выполнения
	ExecutionStatus models.ExecutionStatus `json:"executionStatus,omitempty"` // "success" | "partial" | "failed" | "retry_needed"
	Confidence      *float64               `json:"confidence,omitempty"`      // Уверенность в результате (0.0-1.0)
	Retryable       bool                   `json:"retryable"`                  // Можно ли повторить
	Reason          string                 `json:"reason,omitempty"`          // Причина успеха/неудачи

	// Для репланирования
	SuggestsReplan  bool            `json:"suggestsReplan,omitempty"`  // Рекомендует ли репланирование
	ReplanReason    string          `json:"replanReason,omitempty"`    // Причина репланирования
	AlternativeCapabilities []string `json:"alternativeCapabilities,omitempty"` // Альтернативные подходы

	// Динамические шаги
	SuggestedSteps []SuggestedStep `json:"suggestedSteps,omitempty"` // Предложенные следующие шаги
}

// CapabilityRegistry реестр исполнителей capabilities
type CapabilityRegistry struct {
	executors map[Capability]CapabilityExecutor
}

// NewCapabilityRegistry создает новый реестр
func NewCapabilityRegistry() *CapabilityRegistry {
	return &CapabilityRegistry{
		executors: make(map[Capability]CapabilityExecutor),
	}
}

// Register регистрирует исполнителя capability
func (r *CapabilityRegistry) Register(executor CapabilityExecutor) {
	r.executors[executor.Capability()] = executor
}

// Get получает исполнителя по типу capability
func (r *CapabilityRegistry) Get(capability Capability) (CapabilityExecutor, error) {
	executor, ok := r.executors[capability]
	if !ok {
		return nil, fmt.Errorf("capability executor not found: %s", capability)
	}
	return executor, nil
}

// List возвращает список всех зарегистрированных capabilities
func (r *CapabilityRegistry) List() []Capability {
	capabilities := make([]Capability, 0, len(r.executors))
	for cap := range r.executors {
		capabilities = append(capabilities, cap)
	}
	return capabilities
}

