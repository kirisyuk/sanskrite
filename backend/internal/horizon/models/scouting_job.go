package models

import (
	"github.com/igorkirisyuk/sanskrite/internal/models"
)

// ScoutingJobState состояние ScoutingJob
type ScoutingJobState string

const (
	ScoutingJobStatePending    ScoutingJobState = "PENDING"
	ScoutingJobStatePlanning   ScoutingJobState = "PLANNING"
	ScoutingJobStateExecution  ScoutingJobState = "EXECUTION"
	ScoutingJobStateExtraction ScoutingJobState = "EXTRACTION"
	ScoutingJobStateValidation ScoutingJobState = "VALIDATION"
	ScoutingJobStateResolution ScoutingJobState = "RESOLUTION"
	ScoutingJobStateVerified   ScoutingJobState = "VERIFIED"
	ScoutingJobStateRejected   ScoutingJobState = "REJECTED"
	ScoutingJobStateFailed     ScoutingJobState = "FAILED"
)

// ScoutingJob расширяет AgentJob для работы Scout Engine
// Использует существующую систему AgentJob для отслеживания прогресса
type ScoutingJob struct {
	models.AgentJob // Встраивание существующей структуры

	// Контекст проекта
	LensID string `json:"lensId" firestore:"lensId"`

	// Целевой узел
	TargetCanonicalPath string `json:"targetCanonicalPath" firestore:"targetCanonicalPath"`

	// Источники данных
	SourceIDs []string `json:"sourceIds,omitempty" firestore:"sourceIds,omitempty"` // DataSource IDs из проекта

	// Стратегия и биллинг
	ScoutingStrategy string         `json:"scoutingStrategy" firestore:"scoutingStrategy"` // Snapshot, DeepDive
	BillingContext   BillingContext `json:"billingContext" firestore:"billingContext"`

	// Состояние
	CurrentState ScoutingJobState `json:"currentState" firestore:"currentState"`
}

// BillingContext контекст биллинга для ScoutingJob
type BillingContext struct {
	ChargeModel string  `json:"chargeModel" firestore:"chargeModel"` // "Bounty" или "Access"
	MaxCost     float64 `json:"maxCost" firestore:"maxCost"`
	ProjectID   string  `json:"projectId" firestore:"projectId"`
}

// ScoutingStrategy стратегия разведки
type ScoutingStrategy string

const (
	ScoutingStrategySnapshot ScoutingStrategy = "Snapshot" // Быстрый снимок (≤ 1 минуты)
	ScoutingStrategyDeepDive ScoutingStrategy = "DeepDive" // Глубокое исследование (≤ 30 минут)
)

// ChargeModel модель оплаты
type ChargeModel string

const (
	ChargeModelBounty ChargeModel = "Bounty" // Первый проект оплачивает создание
	ChargeModelAccess ChargeModel = "Access" // Последующие проекты получают доступ
)

// DirectedExplorationConfig configures a user-driven directed supply-chain expansion run.
type DirectedExplorationConfig struct {
	DomainMode          string   `json:"domainMode,omitempty" firestore:"domainMode,omitempty"`
	Direction           string   `json:"direction,omitempty" firestore:"direction,omitempty"`
	HotPathOnly         bool     `json:"hotPathOnly,omitempty" firestore:"hotPathOnly,omitempty"`
	HotPathNodeIDs      []string `json:"hotPathNodeIds,omitempty" firestore:"hotPathNodeIds,omitempty"`
	IncludeCapabilities bool     `json:"includeCapabilities,omitempty" firestore:"includeCapabilities,omitempty"`
	IncludeEdgeMetrics  bool     `json:"includeEdgeMetrics,omitempty" firestore:"includeEdgeMetrics,omitempty"`
	CustomQuery         string   `json:"customQuery,omitempty" firestore:"customQuery,omitempty"`
	RenderTarget        string   `json:"renderTarget,omitempty" firestore:"renderTarget,omitempty"`
	IgnoreFreshness     bool     `json:"ignoreFreshness,omitempty" firestore:"ignoreFreshness,omitempty"`
}

// ScoutingJobParams carries the parameters needed to create a new ScoutingJob.
// Used by QAScoutRequestBuilder to bridge QA sufficiency gaps into scouting work.
type ScoutingJobParams struct {
	LensID              string   `json:"lensId"`
	TenantID            string   `json:"tenantId"`
	ProjectID           string   `json:"projectId"`
	TargetCanonicalPath string   `json:"targetCanonicalPath"`
	SeedHints           []string `json:"seedHints,omitempty"`
	RecommendedFacets   []string `json:"recommendedFacets,omitempty"`
	Strategy            string   `json:"strategy"`
	// WorkScopeRefs optional explicit focus (simulation baseline ref shape). Merged with lens, target path, and seed hints by workscope.FromScoutingJobParams.
	WorkScopeRefs SimulationBaselineRefs `json:"workScopeRefs,omitempty" firestore:"workScopeRefs,omitempty"`
}
