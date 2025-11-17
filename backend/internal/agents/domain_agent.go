package agents

import (
	"context"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/models"
)

// DomainAgent интерфейс для доменных агентов Knowledge Graph системы
// Расширяет BaseAgent, добавляя специализированные методы для Knowledge Graph
type DomainAgent interface {
	BaseAgent

	// Domain возвращает домен агента (financial, political, social, technical)
	Domain() string

	// Version возвращает версию агента
	Version() string

	// Capabilities возвращает список возможностей агента (эквивалент Skills для BaseAgent)
	Capabilities() []string

	// HandleQuery обрабатывает запрос к агенту
	HandleQuery(ctx context.Context, req *AgentQueryRequest) (*AgentQueryResponse, error)

	// ExtractKnowledge извлекает знания из документа
	ExtractKnowledge(ctx context.Context, doc *models.Document) (*ExtractedKnowledge, error)

	// GenerateScenario генерирует сценарий на основе знаний
	GenerateScenario(ctx context.Context, knowledge []DomainKnowledge) (*DomainScenario, error)
}

// AgentQueryRequest запрос к доменному агенту
type AgentQueryRequest struct {
	Query     string                 `json:"query"`
	ProjectID string                 `json:"projectId"`
	Intent    string                 `json:"intent"` // "analyze", "extract", "generate"
	Options   map[string]interface{} `json:"options,omitempty"`
}

// AgentQueryResponse ответ от доменного агента
type AgentQueryResponse struct {
	Data            interface{} `json:"data"`
	Confidence      float64     `json:"confidence"`
	ProcessingTime  time.Duration `json:"processingTime"`
	Sources         []SourceRef `json:"sources,omitempty"`
	ProcessingNotes string      `json:"processingNotes,omitempty"`
}

// SourceRef ссылка на источник данных
type SourceRef struct {
	ID       string  `json:"id"`
	Type     string  `json:"type"` // "document", "web", "api"
	URL      *string `json:"url,omitempty"`
	Title    *string `json:"title,omitempty"`
	TrustScore float64 `json:"trustScore,omitempty"`
}

// ExtractedKnowledge извлеченные знания от доменного агента
type ExtractedKnowledge struct {
	Entities []KnowledgeEntity `json:"entities"`
	Facts    []KnowledgeFact   `json:"facts"`
	Events   []KnowledgeEvent  `json:"events,omitempty"`
	Metrics  []KnowledgeMetric `json:"metrics,omitempty"`
	Confidence float64          `json:"confidence"`
}

// KnowledgeEntity сущность из Knowledge Graph
type KnowledgeEntity struct {
	ID            string                 `json:"id"`
	CanonicalName string                 `json:"canonicalName"`
	Type          string                 `json:"type"` // "Person", "Organization", "Location", "Product", "Document", "Facility", "Other" (ontology EntityType)
	Aliases       []string               `json:"aliases,omitempty"`
	Attributes    map[string]interface{} `json:"attributes,omitempty"`
	Confidence    float64                `json:"confidence"`
	Sources       []SourceRef            `json:"sources"`
}

// KnowledgeFact факт из Knowledge Graph
type KnowledgeFact struct {
	ID            string                 `json:"id"`
	Summary       string                 `json:"summary"`
	Domain        string                 `json:"domain"`
	SubjectEntity *string                `json:"subjectEntity,omitempty"`
	Predicate     string                 `json:"predicate"`
	ObjectEntity  *string                `json:"objectEntity,omitempty"`
	ObjectValue   *string                `json:"objectValue,omitempty"`
	Confidence    float64                `json:"confidence"`
	Sources       []SourceRef            `json:"sources"`
	Perspectives  []PerspectiveData      `json:"perspectives,omitempty"` // Для политических/социальных фактов
}

// PerspectiveData данные о перспективе (для multi-perspective extraction)
type PerspectiveData struct {
	Label      string   `json:"label"`
	Statement  string   `json:"statement"`
	Source     SourceRef `json:"source"`
	Confidence float64  `json:"confidence"`
}

// KnowledgeEvent событие из Knowledge Graph
type KnowledgeEvent struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	Timestamp   time.Time   `json:"timestamp"`
	Domain      string      `json:"domain"`
	Confidence  float64     `json:"confidence"`
	Sources     []SourceRef `json:"sources"`
}

// KnowledgeMetric метрика из Knowledge Graph
type KnowledgeMetric struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Value       interface{} `json:"value"`
	Unit        string      `json:"unit,omitempty"`
	Timestamp   time.Time   `json:"timestamp"`
	Domain      string      `json:"domain"`
	Confidence  float64     `json:"confidence"`
	Sources     []SourceRef `json:"sources"`
}

// DomainKnowledge знание конкретного домена (обобщенный тип)
type DomainKnowledge struct {
	Domain     string                 `json:"domain"`
	Type       string                 `json:"type"` // "entity", "fact", "event", "metric"
	Data       map[string]interface{} `json:"data"`
	Confidence float64                `json:"confidence"`
}

// DomainScenario сценарий для конкретного домена
type DomainScenario struct {
	ID          string                 `json:"id"`
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	Domain      string                 `json:"domain"`
	Likelihood  float64               `json:"likelihood"`
	Confidence  float64               `json:"confidence"`
	Data        map[string]interface{} `json:"data"`
	Sources     []SourceRef            `json:"sources"`
}


