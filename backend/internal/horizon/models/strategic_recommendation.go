package models

import (
	"fmt"
	"strings"
	"time"
)

type StrategicRecommendationSubjectType string

const (
	StrategicRecommendationSubjectAffectedZone StrategicRecommendationSubjectType = "affected_zone"
	StrategicRecommendationSubjectFragment     StrategicRecommendationSubjectType = "fragment"
	StrategicRecommendationSubjectDomain       StrategicRecommendationSubjectType = "domain"
	StrategicRecommendationSubjectSignalBundle StrategicRecommendationSubjectType = "signal_bundle"
)

type StrategicRecommendationType string

const (
	StrategicRecommendationTypeStrategicGap StrategicRecommendationType = "strategic_gap"
)

type StrategicRecommendationStatus string

const (
	StrategicRecommendationStatusPending   StrategicRecommendationStatus = "pending"
	StrategicRecommendationStatusApproved  StrategicRecommendationStatus = "approved"
	StrategicRecommendationStatusDismissed StrategicRecommendationStatus = "dismissed"
	StrategicRecommendationStatusExecuting StrategicRecommendationStatus = "executing"
	StrategicRecommendationStatusCompleted StrategicRecommendationStatus = "completed"
	StrategicRecommendationStatusFailed    StrategicRecommendationStatus = "failed"
	StrategicRecommendationStatusStale     StrategicRecommendationStatus = "stale"
)

type StrategicRecommendationPriority string

const (
	StrategicRecommendationPriorityCritical StrategicRecommendationPriority = "critical"
	StrategicRecommendationPriorityHigh     StrategicRecommendationPriority = "high"
	StrategicRecommendationPriorityMedium   StrategicRecommendationPriority = "medium"
)

type StrategicRecommendationProposedAction string

const (
	StrategicRecommendationActionStagedEnrich StrategicRecommendationProposedAction = "staged_enrich"
)

type StrategicCoverageFamilyStat struct {
	Family      string   `json:"family" firestore:"family"`
	Matched     bool     `json:"matched" firestore:"matched"`
	MatchedRefs []string `json:"matchedRefs,omitempty" firestore:"matchedRefs,omitempty"`
}

type StrategicCoverageStats struct {
	ProfileID            string                        `json:"profileId,omitempty" firestore:"profileId,omitempty"`
	ProfileVersion       string                        `json:"profileVersion,omitempty" firestore:"profileVersion,omitempty"`
	DirectTargets        int                           `json:"directTargets" firestore:"directTargets"`
	NeighborhoodNodes    int                           `json:"neighborhoodNodes" firestore:"neighborhoodNodes"`
	NeighborhoodEdges    int                           `json:"neighborhoodEdges" firestore:"neighborhoodEdges"`
	MaxHops              int                           `json:"maxHops" firestore:"maxHops"`
	RemoteMatches        int                           `json:"remoteMatches" firestore:"remoteMatches"`
	BusinessEndpoints    int                           `json:"businessEndpoints" firestore:"businessEndpoints"`
	PathFamilies         int                           `json:"pathFamilies" firestore:"pathFamilies"`
	ExpectedFamilies     int                           `json:"expectedFamilies" firestore:"expectedFamilies"`
	MatchedFamilies      int                           `json:"matchedFamilies" firestore:"matchedFamilies"`
	MissingFamilyDetails []StrategicCoverageFamilyStat `json:"missingFamilyDetails,omitempty" firestore:"missingFamilyDetails,omitempty"`
}

type StrategicRecommendationExecution struct {
	JobID             string     `json:"jobId,omitempty" firestore:"jobId,omitempty"`
	JobKind           string     `json:"jobKind,omitempty" firestore:"jobKind,omitempty"`
	JobStage          string     `json:"jobStage,omitempty" firestore:"jobStage,omitempty"`
	LastEvaluatedAt   *time.Time `json:"lastEvaluatedAt,omitempty" firestore:"lastEvaluatedAt,omitempty"`
	LastCompletedAt   *time.Time `json:"lastCompletedAt,omitempty" firestore:"lastCompletedAt,omitempty"`
	ResultSummary     string     `json:"resultSummary,omitempty" firestore:"resultSummary,omitempty"`
	GapStillOpen      bool       `json:"gapStillOpen" firestore:"gapStillOpen"`
	EscalatedDeepDive bool       `json:"escalatedDeepDive" firestore:"escalatedDeepDive"`
}

type StrategicRecommendation struct {
	ID                     string                                `json:"id" firestore:"id"`
	TenantID               string                                `json:"tenantId" firestore:"tenantId"`
	ProjectID              string                                `json:"projectId,omitempty" firestore:"projectId,omitempty"`
	LensID                 string                                `json:"lensId,omitempty" firestore:"lensId,omitempty"`
	SubjectType            StrategicRecommendationSubjectType    `json:"subjectType" firestore:"subjectType"`
	SubjectCanonicalPath   string                                `json:"subjectCanonicalPath" firestore:"subjectCanonicalPath"`
	RecommendationType     StrategicRecommendationType           `json:"recommendationType" firestore:"recommendationType"`
	Status                 StrategicRecommendationStatus         `json:"status" firestore:"status"`
	Priority               StrategicRecommendationPriority       `json:"priority" firestore:"priority"`
	Confidence             float64                               `json:"confidence" firestore:"confidence"`
	Headline               string                                `json:"headline" firestore:"headline"`
	Reason                 string                                `json:"reason" firestore:"reason"`
	Evidence               []string                              `json:"evidence,omitempty" firestore:"evidence,omitempty"`
	ExpectedMissingTargets []string                              `json:"expectedMissingTargets,omitempty" firestore:"expectedMissingTargets,omitempty"`
	CurrentCoverageStats   StrategicCoverageStats                `json:"currentCoverageStats" firestore:"currentCoverageStats"`
	ProposedAction         StrategicRecommendationProposedAction `json:"proposedAction" firestore:"proposedAction"`
	DomainContractHint     string                                `json:"domainContractHint,omitempty" firestore:"domainContractHint,omitempty"`
	DedupeKey              string                                `json:"dedupeKey" firestore:"dedupeKey"`
	SupersedesID           string                                `json:"supersedesId,omitempty" firestore:"supersedesId,omitempty"`
	Execution              StrategicRecommendationExecution      `json:"execution,omitempty" firestore:"execution,omitempty"`
	CreatedAt              time.Time                             `json:"createdAt" firestore:"createdAt"`
	UpdatedAt              time.Time                             `json:"updatedAt" firestore:"updatedAt"`
}

func (r *StrategicRecommendation) Validate() error {
	if r == nil {
		return fmt.Errorf("recommendation is required")
	}
	if strings.TrimSpace(r.TenantID) == "" {
		return fmt.Errorf("tenantId is required")
	}
	if strings.TrimSpace(r.SubjectCanonicalPath) == "" {
		return fmt.Errorf("subjectCanonicalPath is required")
	}
	if strings.TrimSpace(r.Headline) == "" {
		return fmt.Errorf("headline is required")
	}
	if strings.TrimSpace(r.Reason) == "" {
		return fmt.Errorf("reason is required")
	}
	if strings.TrimSpace(r.DedupeKey) == "" {
		return fmt.Errorf("dedupeKey is required")
	}
	if r.Confidence < 0 || r.Confidence > 1 {
		return fmt.Errorf("confidence must be between 0 and 1")
	}
	switch r.SubjectType {
	case StrategicRecommendationSubjectAffectedZone,
		StrategicRecommendationSubjectFragment,
		StrategicRecommendationSubjectDomain,
		StrategicRecommendationSubjectSignalBundle:
	default:
		return fmt.Errorf("invalid subjectType")
	}
	switch r.RecommendationType {
	case StrategicRecommendationTypeStrategicGap:
	default:
		return fmt.Errorf("invalid recommendationType")
	}
	switch r.Status {
	case StrategicRecommendationStatusPending,
		StrategicRecommendationStatusApproved,
		StrategicRecommendationStatusDismissed,
		StrategicRecommendationStatusExecuting,
		StrategicRecommendationStatusCompleted,
		StrategicRecommendationStatusFailed,
		StrategicRecommendationStatusStale:
	default:
		return fmt.Errorf("invalid status")
	}
	switch r.Priority {
	case StrategicRecommendationPriorityCritical,
		StrategicRecommendationPriorityHigh,
		StrategicRecommendationPriorityMedium:
	default:
		return fmt.Errorf("invalid priority")
	}
	switch r.ProposedAction {
	case StrategicRecommendationActionStagedEnrich:
	default:
		return fmt.Errorf("invalid proposedAction")
	}
	return nil
}
