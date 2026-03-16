package models

import (
	"fmt"
	"strings"
	"time"
)

type WatchSubjectKind string

const (
	WatchSubjectKindFragment       WatchSubjectKind = "fragment"
	WatchSubjectKindLensUniverse   WatchSubjectKind = "lens_universe"
	WatchSubjectKindExplicitTarget WatchSubjectKind = "explicit_target"
	WatchSubjectKindAffectedZone   WatchSubjectKind = "affected_zone"
	WatchSubjectKindPathOverlay    WatchSubjectKind = "path_overlay"
	WatchSubjectKindMarket         WatchSubjectKind = "market"
	WatchSubjectKindGeography      WatchSubjectKind = "geography"
)

type WatchSubjectStatus string

const (
	WatchSubjectStatusActive  WatchSubjectStatus = "ACTIVE"
	WatchSubjectStatusPaused  WatchSubjectStatus = "PAUSED"
	WatchSubjectStatusDeleted WatchSubjectStatus = "DELETED"
)

type WatchSubjectSourceMode string

const (
	WatchSubjectSourceFacts          WatchSubjectSourceMode = "facts"
	WatchSubjectSourcePinnedSources  WatchSubjectSourceMode = "pinned_sources"
	WatchSubjectSourceProjectSources WatchSubjectSourceMode = "project_sources"
	WatchSubjectSourceHybrid         WatchSubjectSourceMode = "hybrid"
)

type WatchSubjectRuntimeMode string

const (
	WatchSubjectRuntimeCustomFragment WatchSubjectRuntimeMode = "custom_fragment"
	WatchSubjectRuntimeGeneralized    WatchSubjectRuntimeMode = "generalized_watch"
	WatchSubjectRuntimeRiskOnly       WatchSubjectRuntimeMode = "risk_only"
	WatchSubjectRuntimeScoutOnly      WatchSubjectRuntimeMode = "scout_only"
	WatchSubjectRuntimeCombined       WatchSubjectRuntimeMode = "combined"
)

type WatchSubject struct {
	ID                  string                  `json:"id" firestore:"id"`
	TenantID            string                  `json:"tenantId" firestore:"tenantId"`
	ProjectID           string                  `json:"projectId,omitempty" firestore:"projectId,omitempty"`
	LensID              string                  `json:"lensId,omitempty" firestore:"lensId,omitempty"`
	PackID              string                  `json:"packId,omitempty" firestore:"packId,omitempty"`
	UserID              string                  `json:"userId,omitempty" firestore:"userId,omitempty"`
	SubjectKind         WatchSubjectKind        `json:"subjectKind" firestore:"subjectKind"`
	AnchorCanonicalPath string                  `json:"anchorCanonicalPath" firestore:"anchorCanonicalPath"`
	AnchorFragmentID    string                  `json:"anchorFragmentId,omitempty" firestore:"anchorFragmentId,omitempty"`
	DisplayLabel        string                  `json:"displayLabel" firestore:"displayLabel"`
	Status              WatchSubjectStatus      `json:"status" firestore:"status"`
	SourceMode          WatchSubjectSourceMode  `json:"sourceMode" firestore:"sourceMode"`
	RuntimeMode         WatchSubjectRuntimeMode `json:"runtimeMode" firestore:"runtimeMode"`
	DomainModes         []string                `json:"domainModes,omitempty" firestore:"domainModes,omitempty"`
	Roles               []string                `json:"roles,omitempty" firestore:"roles,omitempty"`
	Priority            int                     `json:"priority" firestore:"priority"`
	LensBindings        []string                `json:"lensBindings,omitempty" firestore:"lensBindings,omitempty"`
	UniversePaths       []string                `json:"universePaths,omitempty" firestore:"universePaths,omitempty"`
	Metadata            map[string]any          `json:"metadata,omitempty" firestore:"metadata,omitempty"`
	CreatedAt           time.Time               `json:"createdAt" firestore:"createdAt"`
	UpdatedAt           time.Time               `json:"updatedAt" firestore:"updatedAt"`
}

func (s *WatchSubject) Validate() error {
	if s == nil {
		return fmt.Errorf("watch subject is required")
	}
	if strings.TrimSpace(s.TenantID) == "" {
		return fmt.Errorf("tenantId is required")
	}
	if strings.TrimSpace(s.AnchorCanonicalPath) == "" {
		return fmt.Errorf("anchorCanonicalPath is required")
	}
	if strings.TrimSpace(s.DisplayLabel) == "" {
		return fmt.Errorf("displayLabel is required")
	}
	switch s.SubjectKind {
	case WatchSubjectKindFragment, WatchSubjectKindLensUniverse, WatchSubjectKindExplicitTarget,
		WatchSubjectKindAffectedZone, WatchSubjectKindPathOverlay, WatchSubjectKindMarket, WatchSubjectKindGeography:
	default:
		return fmt.Errorf("invalid subjectKind")
	}
	switch s.Status {
	case WatchSubjectStatusActive, WatchSubjectStatusPaused, WatchSubjectStatusDeleted:
	default:
		return fmt.Errorf("invalid status")
	}
	switch s.SourceMode {
	case WatchSubjectSourceFacts, WatchSubjectSourcePinnedSources, WatchSubjectSourceProjectSources, WatchSubjectSourceHybrid:
	default:
		return fmt.Errorf("invalid sourceMode")
	}
	switch s.RuntimeMode {
	case WatchSubjectRuntimeCustomFragment, WatchSubjectRuntimeGeneralized, WatchSubjectRuntimeRiskOnly, WatchSubjectRuntimeScoutOnly, WatchSubjectRuntimeCombined:
	default:
		return fmt.Errorf("invalid runtimeMode")
	}
	if s.Priority == 0 {
		s.Priority = 50
	}
	return nil
}

type WatchTriggerType string

const (
	WatchTriggerFactDelta              WatchTriggerType = "fact_delta"
	WatchTriggerSourceDelta            WatchTriggerType = "source_delta"
	WatchTriggerSignalDetected         WatchTriggerType = "signal_detected"
	WatchTriggerAffectedZoneChanged    WatchTriggerType = "affected_zone_changed"
	WatchTriggerPropagationGap         WatchTriggerType = "propagation_gap"
	WatchTriggerRecommendationApproved WatchTriggerType = "recommendation_approved"
	WatchTriggerManual                 WatchTriggerType = "manual"
)

type WatchTriggerDecision string

const (
	WatchTriggerDecisionNoop                          WatchTriggerDecision = "noop"
	WatchTriggerDecisionWarmOnly                      WatchTriggerDecision = "warm_only"
	WatchTriggerDecisionWakeScopeAgent                WatchTriggerDecision = "wake_scope_agent"
	WatchTriggerDecisionEnqueueRisk                   WatchTriggerDecision = "enqueue_risk"
	WatchTriggerDecisionEnqueueScout                  WatchTriggerDecision = "enqueue_scout"
	WatchTriggerDecisionCreateOrRefreshRecommendation WatchTriggerDecision = "create_or_refresh_recommendation"
)

type WatchTriggerMatchMode string

const (
	WatchTriggerMatchAnchor   WatchTriggerMatchMode = "anchor"
	WatchTriggerMatchUniverse WatchTriggerMatchMode = "universe"
	WatchTriggerMatchDerived  WatchTriggerMatchMode = "derived"
)

type WatchTrigger struct {
	ID                string                `json:"id" firestore:"id"`
	SubjectID         string                `json:"subjectId" firestore:"subjectId"`
	TriggerType       WatchTriggerType      `json:"triggerType" firestore:"triggerType"`
	TriggerReason     string                `json:"triggerReason,omitempty" firestore:"triggerReason,omitempty"`
	CanonicalPaths    []string              `json:"canonicalPaths,omitempty" firestore:"canonicalPaths,omitempty"`
	SourceIDs         []string              `json:"sourceIds,omitempty" firestore:"sourceIds,omitempty"`
	Polarity          string                `json:"polarity,omitempty" firestore:"polarity,omitempty"`
	SignalStrength    float64               `json:"signalStrength" firestore:"signalStrength"`
	Confidence        float64               `json:"confidence" firestore:"confidence"`
	DedupeKey         string                `json:"dedupeKey" firestore:"dedupeKey"`
	SemanticWatermark string                `json:"semanticWatermark,omitempty" firestore:"semanticWatermark,omitempty"`
	MatchMode         WatchTriggerMatchMode `json:"matchMode,omitempty" firestore:"matchMode,omitempty"`
	Metadata          map[string]any        `json:"metadata,omitempty" firestore:"metadata,omitempty"`
	ProcessedAt       *time.Time            `json:"processedAt,omitempty" firestore:"processedAt,omitempty"`
	Decision          WatchTriggerDecision  `json:"decision,omitempty" firestore:"decision,omitempty"`
	DecisionReason    string                `json:"decisionReason,omitempty" firestore:"decisionReason,omitempty"`
	RunRefs           []string              `json:"runRefs,omitempty" firestore:"runRefs,omitempty"`
	JobRefs           []string              `json:"jobRefs,omitempty" firestore:"jobRefs,omitempty"`
	ProcessingError   string                `json:"processingError,omitempty" firestore:"processingError,omitempty"`
	CreatedAt         time.Time             `json:"createdAt" firestore:"createdAt"`
	UpdatedAt         time.Time             `json:"updatedAt" firestore:"updatedAt"`
}

func (t *WatchTrigger) Validate() error {
	if t == nil {
		return fmt.Errorf("watch trigger is required")
	}
	if strings.TrimSpace(t.SubjectID) == "" {
		return fmt.Errorf("subjectId is required")
	}
	if strings.TrimSpace(t.DedupeKey) == "" {
		return fmt.Errorf("dedupeKey is required")
	}
	switch t.TriggerType {
	case WatchTriggerFactDelta, WatchTriggerSourceDelta, WatchTriggerSignalDetected,
		WatchTriggerAffectedZoneChanged, WatchTriggerPropagationGap, WatchTriggerRecommendationApproved, WatchTriggerManual:
	default:
		return fmt.Errorf("invalid triggerType")
	}
	switch t.MatchMode {
	case "", WatchTriggerMatchAnchor, WatchTriggerMatchUniverse, WatchTriggerMatchDerived:
	default:
		return fmt.Errorf("invalid matchMode")
	}
	switch t.Decision {
	case "",
		WatchTriggerDecisionNoop,
		WatchTriggerDecisionWarmOnly,
		WatchTriggerDecisionWakeScopeAgent,
		WatchTriggerDecisionEnqueueRisk,
		WatchTriggerDecisionEnqueueScout,
		WatchTriggerDecisionCreateOrRefreshRecommendation:
	default:
		return fmt.Errorf("invalid decision")
	}
	if t.Confidence < 0 || t.Confidence > 1 {
		return fmt.Errorf("confidence must be between 0 and 1")
	}
	if t.SignalStrength < 0 {
		return fmt.Errorf("signalStrength must be non-negative")
	}
	return nil
}
