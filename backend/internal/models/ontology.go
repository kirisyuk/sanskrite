package models

import "time"

// ============================================================================
// Общие базовые поля (используются во всех онтологических типах)
// ============================================================================

// BaseOntologyFields содержит общие поля для всех онтологических объектов
type BaseOntologyFields struct {
	ID          string            `json:"id" firestore:"id"`
	TenantID    string            `json:"tenantId" firestore:"tenantId"` // "global" или "system" для Wikidata
	Version     int               `json:"version" firestore:"version"`
	CreatedAt   time.Time         `json:"createdAt" firestore:"createdAt"`
	UpdatedAt   time.Time         `json:"updatedAt" firestore:"updatedAt"`
	CreatedBy   string            `json:"createdBy" firestore:"createdBy"` // система/пользователь
	UpdatedBy   string            `json:"updatedBy" firestore:"updatedBy"`
	Labels      map[string]string `json:"labels" firestore:"labels"` // lang -> строка
	Description string            `json:"description,omitempty" firestore:"description,omitempty"`
	Tags        []string          `json:"tags,omitempty" firestore:"tags,omitempty"`
	Status      string            `json:"status" firestore:"status"` // active, deprecated, draft, archived
}

// ============================================================================
// 1. Entity (Сущность)
// ============================================================================

type EntityType string

const (
	EntityTypePerson            EntityType = "Person"
	EntityTypeOrganization      EntityType = "Organization"
	EntityTypeProduct           EntityType = "Product"
	EntityTypeLocation          EntityType = "Location"
	EntityTypeEventEntity       EntityType = "EventEntity"
	EntityTypeDocument          EntityType = "Document"
	EntityTypeFacility          EntityType = "Facility"
	EntityTypeSupplyChainNode   EntityType = "SupplyChainNode"
	EntityTypeGovernmentProject EntityType = "GovernmentProject"
	EntityTypeIndexMetric       EntityType = "IndexMetric"
	EntityTypeOther             EntityType = "Other"
)

type DataQuality string

const (
	DataQualityLow    DataQuality = "low"
	DataQualityMedium DataQuality = "medium"
	DataQualityHigh   DataQuality = "high"
)

// KBReference ссылка на внешнюю базу знаний
type KBReference struct {
	KB         string     `json:"kb" firestore:"kb"` // Wikidata, DBpedia, etc.
	ExternalID string     `json:"externalId" firestore:"externalId"`
	Confidence float64    `json:"confidence" firestore:"confidence"` // 0..1
	LastSync   *time.Time `json:"lastSync,omitempty" firestore:"lastSync,omitempty"`
}

// Location представляет местоположение
type Location struct {
	Name    string  `json:"name,omitempty" firestore:"name,omitempty"`
	ISO6709 *string `json:"iso6709,omitempty" firestore:"iso6709,omitempty"` // координаты ISO 6709
}

// Governance информация об управлении данными
type Governance struct {
	OwnerTeam   string      `json:"ownerTeam,omitempty" firestore:"ownerTeam,omitempty"`
	DataQuality DataQuality `json:"dataQuality,omitempty" firestore:"dataQuality,omitempty"`
}

// Entity представляет конкретные объекты (люди, компании, продукты, локации и т.п.)
// This is the canonical ontology entity type
type Entity struct {
	BaseOntologyFields
	CanonicalName string                 `json:"canonicalName" firestore:"canonicalName"`
	Type          EntityType             `json:"type" firestore:"type"`
	Aliases       []string               `json:"aliases,omitempty" firestore:"aliases,omitempty"`
	KBRefs        []KBReference          `json:"kbRefs,omitempty" firestore:"kbRefs,omitempty"`
	Identifiers   map[string]string      `json:"identifiers,omitempty" firestore:"identifiers,omitempty"` // scheme -> value
	Attributes    map[string]interface{} `json:"attributes,omitempty" firestore:"attributes,omitempty"`   // гибкие свойства
	ValidFrom     *time.Time             `json:"validFrom,omitempty" firestore:"validFrom,omitempty"`
	ValidTo       *time.Time             `json:"validTo,omitempty" firestore:"validTo,omitempty"`
	HomeLocation  *Location              `json:"homeLocation,omitempty" firestore:"homeLocation,omitempty"`
	Governance    *Governance            `json:"governance,omitempty" firestore:"governance,omitempty"`
}

// ============================================================================
// 2. Concept (Концепция)
// ============================================================================

type ConceptKind string

const (
	ConceptKindDomain       ConceptKind = "Domain"
	ConceptKindProcess      ConceptKind = "Process"
	ConceptKindStrategy     ConceptKind = "Strategy"
	ConceptKindTrendConcept ConceptKind = "TrendConcept"
	ConceptKindRisk         ConceptKind = "Risk"
	ConceptKindOpportunity  ConceptKind = "Opportunity"
	ConceptKindRelationType ConceptKind = "RelationType"
	ConceptKindPolicy       ConceptKind = "Policy"
	ConceptKindOther        ConceptKind = "Other"
)

// ConceptRelation связь концепции с другой концепцией
type ConceptRelation struct {
	Type            string `json:"type" firestore:"type"` // isA, partOf, etc.
	TargetConceptID string `json:"targetConceptId" firestore:"targetConceptId"`
}

// ConceptHierarchy иерархическая позиция концепции
type ConceptHierarchy struct {
	ParentID *string  `json:"parentId,omitempty" firestore:"parentId,omitempty"`
	Path     []string `json:"path,omitempty" firestore:"path,omitempty"` // [id, id, ...]
}

// Concept представляет абстрактные идеи/категории
type Concept struct {
	BaseOntologyFields
	Name                string            `json:"name" firestore:"name"`
	Kind                ConceptKind       `json:"kind" firestore:"kind"`
	Hierarchy           ConceptHierarchy  `json:"hierarchy,omitempty" firestore:"hierarchy,omitempty"`
	RelationsToConcepts []ConceptRelation `json:"relationsToConcepts,omitempty" firestore:"relationsToConcepts,omitempty"`
	KBRefs              []KBReference     `json:"kbRefs,omitempty" firestore:"kbRefs,omitempty"`
}

// ============================================================================
// 3. Relation (Связь)
// ============================================================================

type RelationDirection string

const (
	RelationDirectionSubjectToObject RelationDirection = "subjectToObject"
	RelationDirectionBidirectional   RelationDirection = "bidirectional"
)

// SourceRef ссылка на источник артефакта (документ, чат, веб-страница и т.д.)
type SourceRef struct {
	SourceID   string `json:"sourceId" firestore:"sourceId"`   // ID источника (document ID, chat ID, etc.)
	SourceType string `json:"sourceType" firestore:"sourceType"` // тип источника: document, telegram_channel, rss, web, chat, etc.
}

// Provenance информация о происхождении данных
type Provenance struct {
	SourceIDs   []string    `json:"sourceIds" firestore:"sourceIds"`   // ID источников данных (DataSource IDs) - для обратной совместимости
	SourceRefs  []SourceRef `json:"sourceRefs,omitempty" firestore:"sourceRefs,omitempty"` // детальные ссылки на источники артефактов
	ObservedAt  *time.Time  `json:"observedAt,omitempty" firestore:"observedAt,omitempty"`
	Method      string      `json:"method,omitempty" firestore:"method,omitempty"` // extracted, curated, imported
}

// Relation представляет статические/квазистатические связи между Entity/Concept
type Relation struct {
	BaseOntologyFields
	SubjectID  string                 `json:"subjectId" firestore:"subjectId"`
	Predicate  string                 `json:"predicate" firestore:"predicate"` // из контролируемого словаря
	ObjectID   string                 `json:"objectId" firestore:"objectId"`
	Direction  RelationDirection      `json:"direction" firestore:"direction"`
	Qualifiers map[string]interface{} `json:"qualifiers,omitempty" firestore:"qualifiers,omitempty"` // доля владения, роль и т.п.
	Confidence float64                `json:"confidence" firestore:"confidence"`                     // 0..1
	ValidFrom  *time.Time             `json:"validFrom,omitempty" firestore:"validFrom,omitempty"`
	ValidTo    *time.Time             `json:"validTo,omitempty" firestore:"validTo,omitempty"`
	Provenance Provenance             `json:"provenance" firestore:"provenance"`
}

// ============================================================================
// 4. Claim / Fact (Факт/Утверждение)
// ============================================================================

type ReferenceKind string

const (
	ReferenceKindEntity  ReferenceKind = "Entity"
	ReferenceKindConcept ReferenceKind = "Concept"
	ReferenceKindLiteral ReferenceKind = "Literal"
)

// SubjectRef ссылка на субъект (Entity или Concept)
type SubjectRef struct {
	ID   string        `json:"id" firestore:"id"`
	Kind ReferenceKind `json:"kind" firestore:"kind"`
}

// ObjectRef ссылка на объект (Entity, Concept или Literal)
type ObjectRef struct {
	Kind  ReferenceKind `json:"kind" firestore:"kind"`
	Value interface{}   `json:"value,omitempty" firestore:"value,omitempty"` // для Literal
	ID    *string       `json:"id,omitempty" firestore:"id,omitempty"`       // для Entity/Concept
}

type Stance string

const (
	StanceSupports    Stance = "supports"
	StanceContradicts Stance = "contradicts"
	StanceNeutral     Stance = "neutral"
)

type ExtractionMethod string

const (
	ExtractionMethodNLP     ExtractionMethod = "NLP"
	ExtractionMethodHuman   ExtractionMethod = "human"
	ExtractionMethodAPI     ExtractionMethod = "API"
	ExtractionMethodKBAlign ExtractionMethod = "KBAlign"
)

// TemporalInfo временная информация
type TemporalInfo struct {
	AsOf  *time.Time `json:"asOf,omitempty" firestore:"asOf,omitempty"`
	Start *time.Time `json:"start,omitempty" firestore:"start,omitempty"`
	End   *time.Time `json:"end,omitempty" firestore:"end,omitempty"`
}

// ClaimProvenance детальная информация о происхождении утверждения
type ClaimProvenance struct {
	SourceIDs       []string         `json:"sourceIds" firestore:"sourceIds"`   // ID источников данных (DataSource IDs) - для обратной совместимости
	SourceRefs      []SourceRef      `json:"sourceRefs,omitempty" firestore:"sourceRefs,omitempty"` // детальные ссылки на источники артефактов
	Method          ExtractionMethod `json:"method" firestore:"method"`
	ExtractedAt     time.Time        `json:"extractedAt" firestore:"extractedAt"`
	EvidenceAnchors []string         `json:"evidenceAnchors,omitempty" firestore:"evidenceAnchors,omitempty"` // строки/offset
	License         *string          `json:"license,omitempty" firestore:"license,omitempty"`
}

// Claim представляет проверяемое утверждение-триплет с доказательной базой
type Claim struct {
	BaseOntologyFields
	SubjectRef SubjectRef             `json:"subjectRef" firestore:"subjectRef"`
	Predicate  string                 `json:"predicate" firestore:"predicate"` // контролируемый словарь
	ObjectRef  ObjectRef              `json:"objectRef" firestore:"objectRef"`
	Temporal   TemporalInfo           `json:"temporal,omitempty" firestore:"temporal,omitempty"`
	Qualifiers map[string]interface{} `json:"qualifiers,omitempty" firestore:"qualifiers,omitempty"`
	Confidence float64                `json:"confidence" firestore:"confidence"` // 0..1
	Stance     Stance                 `json:"stance" firestore:"stance"`
	Provenance ClaimProvenance        `json:"provenance" firestore:"provenance"`
}

// ============================================================================
// 5. Event (Событие)
// ============================================================================

// Participant участник события
type Participant struct {
	EntityID string `json:"entityId" firestore:"entityId"`
	Role     string `json:"role" firestore:"role"` // agent, target, mediator и др.
}

// EventLinks ссылки на связанные объекты
type EventLinks struct {
	Claims        []string `json:"claims,omitempty" firestore:"claims,omitempty"`
	Metrics       []string `json:"metrics,omitempty" firestore:"metrics,omitempty"`
	RelatedEvents []string `json:"relatedEvents,omitempty" firestore:"relatedEvents,omitempty"`
}

// Event представляет дискретное происшествие во времени
// This is the canonical ontology event type
type Event struct {
	BaseOntologyFields
	Type           string        `json:"type" firestore:"type"` // из словаря типов событий
	Title          string        `json:"title" firestore:"title"`
	OccurredAt     *time.Time    `json:"occurredAt,omitempty" firestore:"occurredAt,omitempty"`
	OccurredStart  *time.Time    `json:"occurredStart,omitempty" firestore:"occurredStart,omitempty"`
	OccurredEnd    *time.Time    `json:"occurredEnd,omitempty" firestore:"occurredEnd,omitempty"`
	Location       *Location     `json:"location,omitempty" firestore:"location,omitempty"`
	Participants   []Participant `json:"participants,omitempty" firestore:"participants,omitempty"`
	OutcomeSummary string        `json:"outcomeSummary,omitempty" firestore:"outcomeSummary,omitempty"`
	Links          EventLinks    `json:"links,omitempty" firestore:"links,omitempty"`
	Confidence     float64       `json:"confidence" firestore:"confidence"` // 0..1
	Provenance     Provenance    `json:"provenance" firestore:"provenance"`
}

// ============================================================================
// 6. MetricState (Состояние метрики)
// ============================================================================

type SubjectRefKind string

const (
	SubjectRefKindEntity SubjectRefKind = "Entity"
	SubjectRefKindEvent  SubjectRefKind = "Event"
	SubjectRefKindMarket SubjectRefKind = "Market"
)

// MetricSubjectRef ссылка на субъект метрики
type MetricSubjectRef struct {
	ID   string         `json:"id" firestore:"id"`
	Kind SubjectRefKind `json:"kind" firestore:"kind"`
}

type AggregationType string

const (
	AggregationPoint  AggregationType = "point"
	AggregationMean   AggregationType = "mean"
	AggregationMedian AggregationType = "median"
	AggregationSum    AggregationType = "sum"
	AggregationYoY    AggregationType = "yoy"
	AggregationQoQ    AggregationType = "qoq"
)

// ErrorMargin интервал ошибки
type ErrorMargin struct {
	Low   *float64 `json:"low,omitempty" firestore:"low,omitempty"`
	High  *float64 `json:"high,omitempty" firestore:"high,omitempty"`
	Value *float64 `json:"value,omitempty" firestore:"value,omitempty"` // для симметричного интервала
}

// TimePeriod временной период
type TimePeriod struct {
	Start *time.Time `json:"start,omitempty" firestore:"start,omitempty"`
	End   *time.Time `json:"end,omitempty" firestore:"end,omitempty"`
}

// MetricState представляет количественный срез для субъектов/событий
type MetricState struct {
	BaseOntologyFields
	MetricName  string           `json:"metricName" firestore:"metricName"` // из контролируемого словаря
	SubjectRef  MetricSubjectRef `json:"subjectRef" firestore:"subjectRef"`
	Value       interface{}      `json:"value" firestore:"value"` // число или строка для категорий
	Unit        string           `json:"unit" firestore:"unit"`   // %, USD, ms, indexPoint
	Timestamp   *time.Time       `json:"timestamp,omitempty" firestore:"timestamp,omitempty"`
	Period      *TimePeriod      `json:"period,omitempty" firestore:"period,omitempty"`
	Aggregation AggregationType  `json:"aggregation" firestore:"aggregation"`
	ErrorMargin *ErrorMargin     `json:"errorMargin,omitempty" firestore:"errorMargin,omitempty"`
	Confidence  float64          `json:"confidence" firestore:"confidence"` // 0..1
	Provenance  Provenance       `json:"provenance" firestore:"provenance"`
}

// ============================================================================
// 7. Trend (Тренд)
// ============================================================================

type TrendDirection string

const (
	TrendDirectionIncreasing TrendDirection = "increasing"
	TrendDirectionDecreasing TrendDirection = "decreasing"
	TrendDirectionStable     TrendDirection = "stable"
	TrendDirectionVolatile   TrendDirection = "volatile"
)

type TrendStage string

const (
	TrendStageIntroduction TrendStage = "Introduction"
	TrendStageRise         TrendStage = "Rise"
	TrendStagePeak         TrendStage = "Peak"
	TrendStageDecline      TrendStage = "Decline"
	TrendStageObsolescence TrendStage = "Obsolescence"
)

// TrendFocus фокус тренда
type TrendFocus struct {
	MetricName   string `json:"metricName" firestore:"metricName"`
	SubjectScope struct {
		IDs    []string `json:"ids,omitempty" firestore:"ids,omitempty"`
		Filter *string  `json:"filter,omitempty" firestore:"filter,omitempty"`
	} `json:"subjectScope,omitempty" firestore:"subjectScope,omitempty"`
}

// TrendEvidence доказательная база тренда
type TrendEvidence struct {
	MetricStates []string `json:"metricStates,omitempty" firestore:"metricStates,omitempty"`
	Events       []string `json:"events,omitempty" firestore:"events,omitempty"`
	Claims       []string `json:"claims,omitempty" firestore:"claims,omitempty"`
}

// TrendTimeHorizon временной горизонт тренда
type TrendTimeHorizon struct {
	Lookback        string  `json:"lookback" firestore:"lookback"` // период
	ForecastHorizon *string `json:"forecastHorizon,omitempty" firestore:"forecastHorizon,omitempty"`
}

// TrendState состояние тренда в момент времени
type TrendState struct {
	AtTime       time.Time  `json:"atTime" firestore:"atTime"`
	Stage        TrendStage `json:"stage" firestore:"stage"`
	Level        *float64   `json:"level,omitempty" firestore:"level,omitempty"`
	Velocity     *float64   `json:"velocity,omitempty" firestore:"velocity,omitempty"`         // dM/dt
	Acceleration *float64   `json:"acceleration,omitempty" firestore:"acceleration,omitempty"` // d²M/dt²
	Volatility   *float64   `json:"volatility,omitempty" firestore:"volatility,omitempty"`
	Confidence   float64    `json:"confidence" firestore:"confidence"` // 0..1
	EvidenceRefs struct {
		MetricStates []string `json:"metricStates,omitempty" firestore:"metricStates,omitempty"`
		Events       []string `json:"events,omitempty" firestore:"events,omitempty"`
	} `json:"evidenceRefs,omitempty" firestore:"evidenceRefs,omitempty"`
}

// Trend представляет агрегированную динамику вокруг метрики/темы
type Trend struct {
	BaseOntologyFields
	Name        string           `json:"name" firestore:"name"`
	Focus       TrendFocus       `json:"focus" firestore:"focus"`
	Direction   TrendDirection   `json:"direction" firestore:"direction"`
	States      []TrendState     `json:"states" firestore:"states"` // упорядочено по времени
	Evidence    TrendEvidence    `json:"evidence,omitempty" firestore:"evidence,omitempty"`
	Confidence  float64          `json:"confidence" firestore:"confidence"` // 0..1
	TimeHorizon TrendTimeHorizon `json:"timeHorizon" firestore:"timeHorizon"`
}

// ============================================================================
// 8. CausalLink (Причинно-следственная связь)
// ============================================================================

type CausalRefKind string

const (
	CausalRefKindEvent       CausalRefKind = "Event"
	CausalRefKindTrend       CausalRefKind = "Trend"
	CausalRefKindTrendState  CausalRefKind = "TrendState"
	CausalRefKindMetricState CausalRefKind = "MetricState"
)

// CausalRef ссылка на причину или следствие
type CausalRef struct {
	ID   string        `json:"id" firestore:"id"`
	Kind CausalRefKind `json:"kind" firestore:"kind"`
}

type CausalSign string

const (
	CausalSignPositive CausalSign = "positive"
	CausalSignNegative CausalSign = "negative"
)

// CausalCondition условие применимости причинно-следственной связи
type CausalCondition struct {
	Left  string      `json:"left" firestore:"left"`   // выражение
	Op    string      `json:"op" firestore:"op"`       // оператор
	Right interface{} `json:"right" firestore:"right"` // значение
}

// CausalLag задержка причинно-следственной связи
type CausalLag struct {
	Value        *string `json:"value,omitempty" firestore:"value,omitempty"` // длительность
	Distribution *string `json:"distribution,omitempty" firestore:"distribution,omitempty"`
	Unit         *string `json:"unit,omitempty" firestore:"unit,omitempty"`
}

// CausalEvidence доказательная база причинно-следственной связи
type CausalEvidence struct {
	Claims  []string `json:"claims,omitempty" firestore:"claims,omitempty"`
	Events  []string `json:"events,omitempty" firestore:"events,omitempty"`
	Metrics []string `json:"metrics,omitempty" firestore:"metrics,omitempty"`
	Method  *string  `json:"method,omitempty" firestore:"method,omitempty"`
}

// CausalValidity период валидности связи
type CausalValidity struct {
	ValidFrom *time.Time `json:"validFrom,omitempty" firestore:"validFrom,omitempty"`
	ValidTo   *time.Time `json:"validTo,omitempty" firestore:"validTo,omitempty"`
}

// CausalLink представляет гипотезу причинности для моделирования
type CausalLink struct {
	BaseOntologyFields
	CauseRef    CausalRef         `json:"causeRef" firestore:"causeRef"`
	EffectRef   CausalRef         `json:"effectRef" firestore:"effectRef"`
	Sign        CausalSign        `json:"sign" firestore:"sign"`
	Strength    float64           `json:"strength" firestore:"strength"`       // 0..1
	Probability float64           `json:"probability" firestore:"probability"` // 0..1
	Lag         *CausalLag        `json:"lag,omitempty" firestore:"lag,omitempty"`
	Conditions  []CausalCondition `json:"conditions,omitempty" firestore:"conditions,omitempty"`
	Assumptions []string          `json:"assumptions,omitempty" firestore:"assumptions,omitempty"`
	Evidence    CausalEvidence    `json:"evidence,omitempty" firestore:"evidence,omitempty"`
	Validity    CausalValidity    `json:"validity,omitempty" firestore:"validity,omitempty"`
}

// ============================================================================
// 9. Scenario (Сценарий)
// ============================================================================

type ConditionRefKind string

const (
	ConditionRefKindEvent       ConditionRefKind = "Event"
	ConditionRefKindTrendState  ConditionRefKind = "TrendState"
	ConditionRefKindMetricState ConditionRefKind = "MetricState"
)

// ScenarioCondition условие сценария
type ScenarioCondition struct {
	Ref  string           `json:"ref" firestore:"ref"` // id
	Kind ConditionRefKind `json:"kind" firestore:"kind"`
	Expr *string          `json:"expr,omitempty" firestore:"expr,omitempty"` // выражение
}

// ScenarioPathway шаг пути развития сценария
type ScenarioPathway struct {
	Step    int     `json:"step" firestore:"step"`
	LinkRef *string `json:"linkRef,omitempty" firestore:"linkRef,omitempty"` // CausalLink ID
	Note    *string `json:"note,omitempty" firestore:"note,omitempty"`
}

type ImpactDirection string

const (
	ImpactDirectionIncrease ImpactDirection = "increase"
	ImpactDirectionDecrease ImpactDirection = "decrease"
	ImpactDirectionStable   ImpactDirection = "stable"
)

// ImpactTargetRef целевой объект воздействия
type ImpactTargetRef struct {
	ID   string        `json:"id" firestore:"id"`
	Kind ReferenceKind `json:"kind" firestore:"kind"`
}

// ExpectedChange ожидаемое изменение
type ExpectedChange struct {
	Delta     *float64         `json:"delta,omitempty" firestore:"delta,omitempty"`
	Percent   *float64         `json:"percent,omitempty" firestore:"percent,omitempty"`
	Direction *ImpactDirection `json:"direction,omitempty" firestore:"direction,omitempty"`
}

// ScenarioImpact воздействие сценария
type ScenarioImpact struct {
	TargetRef            ImpactTargetRef `json:"targetRef" firestore:"targetRef"`
	MetricName           *string         `json:"metricName,omitempty" firestore:"metricName,omitempty"`
	ExpectedChange       ExpectedChange  `json:"expectedChange" firestore:"expectedChange"`
	AffectedCausalLinkID *string         `json:"affectedCausalLinkId,omitempty" firestore:"affectedCausalLinkId,omitempty"`
}

// ScenarioHorizon временной горизонт сценария
type ScenarioHorizon struct {
	Start    *time.Time `json:"start,omitempty" firestore:"start,omitempty"`
	End      *time.Time `json:"end,omitempty" firestore:"end,omitempty"`
	Duration *string    `json:"duration,omitempty" firestore:"duration,omitempty"` // длительность
}

// MonitoringIndicator индикатор мониторинга сценария
type MonitoringIndicator struct {
	Ref       interface{}      `json:"ref" firestore:"ref"` // id или metricName
	Threshold *float64         `json:"threshold,omitempty" firestore:"threshold,omitempty"`
	Direction *ImpactDirection `json:"direction,omitempty" firestore:"direction,omitempty"`
}

// Scenario представляет возможное развитие с вероятностью и условиями
type Scenario struct {
	BaseOntologyFields
	Title                     string                `json:"title" firestore:"title"`
	Description               string                `json:"description,omitempty" firestore:"description,omitempty"`
	Horizon                   ScenarioHorizon       `json:"horizon" firestore:"horizon"`
	Probability               float64               `json:"probability" firestore:"probability"` // 0..1
	Conditions                []ScenarioCondition   `json:"conditions,omitempty" firestore:"conditions,omitempty"`
	Pathways                  []ScenarioPathway     `json:"pathways,omitempty" firestore:"pathways,omitempty"`
	Impacts                   []ScenarioImpact      `json:"impacts,omitempty" firestore:"impacts,omitempty"`
	ActionableRecommendations []string              `json:"actionableRecommendations,omitempty" firestore:"actionableRecommendations,omitempty"` // ActionableItem IDs
	Assumptions               []string              `json:"assumptions,omitempty" firestore:"assumptions,omitempty"`
	Confidence                float64               `json:"confidence" firestore:"confidence"` // 0..1
	MonitoringIndicators      []MonitoringIndicator `json:"monitoringIndicators,omitempty" firestore:"monitoringIndicators,omitempty"`
}

// ============================================================================
// 10. ActionableItem (Действенный элемент)
// ============================================================================

type Priority string

const (
	PriorityLow      Priority = "low"
	PriorityMedium   Priority = "medium"
	PriorityHigh     Priority = "high"
	PriorityCritical Priority = "critical"
)

type ActionableStatus string

const (
	ActionableStatusProposed   ActionableStatus = "proposed"
	ActionableStatusApproved   ActionableStatus = "approved"
	ActionableStatusInProgress ActionableStatus = "inProgress"
	ActionableStatusBlocked    ActionableStatus = "blocked"
	ActionableStatusDone       ActionableStatus = "done"
	ActionableStatusDiscarded  ActionableStatus = "discarded"
)

type ReviewCadence string

const (
	ReviewCadenceWeekly   ReviewCadence = "weekly"
	ReviewCadenceBiweekly ReviewCadence = "biweekly"
	ReviewCadenceMonthly  ReviewCadence = "monthly"
)

// Objective цель действия
type Objective struct {
	LinksTo  interface{} `json:"linksTo" firestore:"linksTo"` // id или {scenarioId, trendId}
	GoalText *string     `json:"goalText,omitempty" firestore:"goalText,omitempty"`
}

// Owner владелец действия
type Owner struct {
	Role          *string `json:"role,omitempty" firestore:"role,omitempty"`
	ResponsibleID *string `json:"responsibleId,omitempty" firestore:"responsibleId,omitempty"`
}

// DueDate срок выполнения
type DueDate struct {
	Date         *time.Time  `json:"date,omitempty" firestore:"date,omitempty"`
	Period       *TimePeriod `json:"period,omitempty" firestore:"period,omitempty"`
	TimeboxWeeks *int        `json:"timeboxWeeks,omitempty" firestore:"timeboxWeeks,omitempty"`
}

// SuccessMetric метрика успеха
type SuccessMetric struct {
	Name   string      `json:"name" firestore:"name"`
	Target interface{} `json:"target" firestore:"target"` // число или строка
	By     time.Time   `json:"by" firestore:"by"`
}

// ActionableRisk риск действия
type ActionableRisk struct {
	RiskRef     *string `json:"riskRef,omitempty" firestore:"riskRef,omitempty"`
	Description *string `json:"description,omitempty" firestore:"description,omitempty"`
	Mitigation  *string `json:"mitigation,omitempty" firestore:"mitigation,omitempty"`
}

// CostEstimate оценка стоимости
type CostEstimate struct {
	Amount       *float64 `json:"amount,omitempty" firestore:"amount,omitempty"`
	Currency     *string  `json:"currency,omitempty" firestore:"currency,omitempty"`
	EffortPoints *int     `json:"effortPoints,omitempty" firestore:"effortPoints,omitempty"`
}

// FeedbackLoop цикл обратной связи
type FeedbackLoop struct {
	ReviewCadence ReviewCadence `json:"reviewCadence" firestore:"reviewCadence"`
	LastReview    *time.Time    `json:"lastReview,omitempty" firestore:"lastReview,omitempty"`
	OutcomeNotes  *string       `json:"outcomeNotes,omitempty" firestore:"outcomeNotes,omitempty"`
}

// ActionableProvenance происхождение действия
type ActionableProvenance struct {
	InitiatedFrom interface{} `json:"initiatedFrom" firestore:"initiatedFrom"` // Scenario/Trend/Claim ID
	CreatedBy     string      `json:"createdBy" firestore:"createdBy"`
	CreatedAt     time.Time   `json:"createdAt" firestore:"createdAt"`
}

// ActionableItem представляет конкретное действие с измеримым результатом
type ActionableItem struct {
	BaseOntologyFields
	Description    string               `json:"description" firestore:"description"`
	Objective      Objective            `json:"objective" firestore:"objective"`
	Owner          Owner                `json:"owner,omitempty" firestore:"owner,omitempty"`
	Priority       Priority             `json:"priority" firestore:"priority"`
	Due            DueDate              `json:"due,omitempty" firestore:"due,omitempty"`
	ExpectedImpact []ScenarioImpact     `json:"expectedImpact,omitempty" firestore:"expectedImpact,omitempty"`
	SuccessMetrics []SuccessMetric      `json:"successMetrics,omitempty" firestore:"successMetrics,omitempty"`
	Prerequisites  []string             `json:"prerequisites,omitempty" firestore:"prerequisites,omitempty"`
	Risks          []ActionableRisk     `json:"risks,omitempty" firestore:"risks,omitempty"`
	CostEstimate   *CostEstimate        `json:"costEstimate,omitempty" firestore:"costEstimate,omitempty"`
	Status         ActionableStatus     `json:"status" firestore:"status"`
	FeedbackLoop   FeedbackLoop         `json:"feedbackLoop,omitempty" firestore:"feedbackLoop,omitempty"`
	Provenance     ActionableProvenance `json:"provenance" firestore:"provenance"`
}

// ============================================================================
// 11. Source / Trust (Источник и доверие)
// ============================================================================

type SourceType string

const (
	SourceTypeNews        SourceType = "news"
	SourceTypeReport      SourceType = "report"
	SourceTypeKB          SourceType = "KB"
	SourceTypeAPI         SourceType = "api"
	SourceTypeDataset     SourceType = "dataset"
	SourceTypeHumanExpert SourceType = "humanExpert"
	SourceTypeChatMessage SourceType = "chatMessage"
)

type UpdateFrequency string

const (
	UpdateFrequencyRealtime UpdateFrequency = "realtime"
	UpdateFrequencyDaily    UpdateFrequency = "daily"
	UpdateFrequencyWeekly   UpdateFrequency = "weekly"
	UpdateFrequencyMonthly  UpdateFrequency = "monthly"
	UpdateFrequencyAdHoc    UpdateFrequency = "adHoc"
)

type BiasDimension string

const (
	BiasDimensionPolitical  BiasDimension = "political"
	BiasDimensionRegional   BiasDimension = "regional"
	BiasDimensionCommercial BiasDimension = "commercial"
)

// SourceBias информация о смещении источника
type SourceBias struct {
	Dimension BiasDimension `json:"dimension" firestore:"dimension"`
	Polarity  float64       `json:"polarity" firestore:"polarity"` // -1..1
	Notes     *string       `json:"notes,omitempty" firestore:"notes,omitempty"`
}

// Source представляет источник данных
type Source struct {
	BaseOntologyFields
	Name             string           `json:"name" firestore:"name"`
	Type             SourceType       `json:"type" firestore:"type"`
	URL              *string          `json:"url,omitempty" firestore:"url,omitempty"`
	License          *string          `json:"license,omitempty" firestore:"license,omitempty"`
	Bias             *SourceBias      `json:"bias,omitempty" firestore:"bias,omitempty"`
	ReliabilityPrior float64          `json:"reliabilityPrior" firestore:"reliabilityPrior"` // 0..1
	UpdateFrequency  *UpdateFrequency `json:"updateFrequency,omitempty" firestore:"updateFrequency,omitempty"`
	LastChecked      *time.Time       `json:"lastChecked,omitempty" firestore:"lastChecked,omitempty"`
}

// TrustEvidenceHistory история доказательств доверия
type TrustEvidenceHistory struct {
	ClaimID     *string   `json:"claimId,omitempty" firestore:"claimId,omitempty"`
	WasAccurate *bool     `json:"wasAccurate,omitempty" firestore:"wasAccurate,omitempty"`
	Error       *float64  `json:"error,omitempty" firestore:"error,omitempty"`
	CheckedAt   time.Time `json:"checkedAt" firestore:"checkedAt"`
}

// PolicyFlag флаг политики/комплаенса
type PolicyFlag struct {
	Name  string `json:"name" firestore:"name"`
	Value bool   `json:"value" firestore:"value"`
}

// Trust представляет модель доверия к источнику/каналу
type Trust struct {
	BaseOntologyFields
	SourceID        string                 `json:"sourceId" firestore:"sourceId"`
	CurrentScore    float64                `json:"currentScore" firestore:"currentScore"` // 0..1
	DecayRate       *float64               `json:"decayRate,omitempty" firestore:"decayRate,omitempty"`
	EvidenceHistory []TrustEvidenceHistory `json:"evidenceHistory,omitempty" firestore:"evidenceHistory,omitempty"`
	PolicyFlags     []PolicyFlag           `json:"policyFlags,omitempty" firestore:"policyFlags,omitempty"`
	Notes           *string                `json:"notes,omitempty" firestore:"notes,omitempty"`
}

// ============================================================================
// 12. UncertaintyModel (Неопределённость)
// ============================================================================

type UncertaintyType string

const (
	UncertaintyTypeConflict       UncertaintyType = "conflict"
	UncertaintyTypeIncompleteness UncertaintyType = "incompleteness"
	UncertaintyTypeTemporalLag    UncertaintyType = "temporalLag"
	UncertaintyTypeModelRisk      UncertaintyType = "modelRisk"
	UncertaintyTypeAmbiguity      UncertaintyType = "ambiguity"
)

type Materiality string

const (
	MaterialityLow      Materiality = "low"
	MaterialityMedium   Materiality = "medium"
	MaterialityHigh     Materiality = "high"
	MaterialityCritical Materiality = "critical"
)

type UncertaintyStatus string

const (
	UncertaintyStatusOpen          UncertaintyStatus = "open"
	UncertaintyStatusInvestigating UncertaintyStatus = "investigating"
	UncertaintyStatusMitigated     UncertaintyStatus = "mitigated"
	UncertaintyStatusResolved      UncertaintyStatus = "resolved"
	UncertaintyStatusDeferred      UncertaintyStatus = "deferred"
)

type AnswerMode string

const (
	AnswerModeRange    AnswerMode = "range"
	AnswerModeCaveat   AnswerMode = "caveat"
	AnswerModeWithhold AnswerMode = "withhold"
)

// AffectedRef ссылка на затронутый объект
type AffectedRef struct {
	ID   string        `json:"id" firestore:"id"`
	Kind ReferenceKind `json:"kind" firestore:"kind"` // Claim, Event, MetricState, Trend, CausalLink, Scenario
}

// UncertaintyRepresentation представление неопределённости
type UncertaintyRepresentation struct {
	Interval     *ErrorMargin `json:"interval,omitempty" firestore:"interval,omitempty"`
	Distribution *string      `json:"distribution,omitempty" firestore:"distribution,omitempty"`
	Confidence   *float64     `json:"confidence,omitempty" firestore:"confidence,omitempty"`
}

// ImpactOnAnswers влияние на ответы
type ImpactOnAnswers struct {
	Modes             []AnswerMode `json:"modes" firestore:"modes"`
	AffectedScenarios []string     `json:"affectedScenarios,omitempty" firestore:"affectedScenarios,omitempty"`
}

// ResolutionPlan план разрешения неопределённости
type ResolutionPlan struct {
	ActionableItemID *string    `json:"actionableItemId,omitempty" firestore:"actionableItemId,omitempty"`
	Due              *time.Time `json:"due,omitempty" firestore:"due,omitempty"`
}

// UncertaintyModel представляет явный учёт и управление неопределённостями
type UncertaintyModel struct {
	BaseOntologyFields
	Type            UncertaintyType           `json:"type" firestore:"type"`
	AffectedRefs    []AffectedRef             `json:"affectedRefs" firestore:"affectedRefs"`
	Representation  UncertaintyRepresentation `json:"representation" firestore:"representation"`
	Materiality     Materiality               `json:"materiality" firestore:"materiality"`
	ImpactOnAnswers ImpactOnAnswers           `json:"impactOnAnswers" firestore:"impactOnAnswers"`
	Status          UncertaintyStatus         `json:"status" firestore:"status"`
	Owner           *string                   `json:"owner,omitempty" firestore:"owner,omitempty"`
	ResolutionPlan  *ResolutionPlan           `json:"resolutionPlan,omitempty" firestore:"resolutionPlan,omitempty"`
	LastReviewedAt  *time.Time                `json:"lastReviewedAt,omitempty" firestore:"lastReviewedAt,omitempty"`
	Notes           *string                   `json:"notes,omitempty" firestore:"notes,omitempty"`
}
