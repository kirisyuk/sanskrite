package policy

import (
	"context"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// MergeFragments is the single entry point for fragment merge operations.
// Implementations perform: data merge, edge reassign+dedup, delete, audit.
type MergeFragments interface {
	MergeFragment(ctx context.Context, primaryID, duplicateID, tenantID, source, toolName, jobID, lensID string) (*MergeResult, error)
}

// MergeResult holds the outcome of a merge.
type MergeResult struct {
	DataMerged       bool
	EdgesReassigned  int
	EdgesDeleted     int
	DuplicateDeleted bool
}

// =============================================================================
// POLICY VIOLATION TYPES
// =============================================================================

// PolicyViolationType represents the type of policy violation.
type PolicyViolationType string

const (
	ViolationDuplicate     PolicyViolationType = "duplicate"
	ViolationEdgeDirection PolicyViolationType = "edge_direction"
	ViolationPathFormat    PolicyViolationType = "path_format"
	ViolationGeoNamesAdmin PolicyViolationType = "geonames_admin_code"
	// ViolationGeoUnknown indicates that geo/jurisdiction segment is unknown (e.g., /org/na/... or /gov/na/...).
	// "na" is allowed by taxonomy, but it reduces usefulness for search/grouping and should be fixed when possible.
	ViolationGeoUnknown       PolicyViolationType = "geo_unknown"
	ViolationSelfLoop         PolicyViolationType = "self_loop"
	ViolationTypeMismatch     PolicyViolationType = "type_mismatch"
	ViolationSymmetricEdge PolicyViolationType = "symmetric_edge"
	ViolationInvalidISO2   PolicyViolationType = "invalid_iso2"
	ViolationOrphanedEdge     PolicyViolationType = "orphaned_edge"
	// ViolationMissingField indicates a required field is missing (e.g., title for DecisionPoint).
	ViolationMissingField PolicyViolationType = "missing_field"
)

// PolicyViolationSeverity represents the severity of a violation.
type PolicyViolationSeverity string

const (
	SeverityCritical PolicyViolationSeverity = "critical"
	SeverityWarning  PolicyViolationSeverity = "warning"
	SeverityInfo     PolicyViolationSeverity = "info"
)

// PolicyViolation represents a single policy violation found during check.
type PolicyViolation struct {
	ID       string                  `json:"id" firestore:"id"`
	Type     PolicyViolationType     `json:"type" firestore:"type"`
	Severity PolicyViolationSeverity `json:"severity" firestore:"severity"`

	// What's wrong
	FragmentID    string `json:"fragmentId,omitempty" firestore:"fragmentId,omitempty"`
	EdgeID        string `json:"edgeId,omitempty" firestore:"edgeId,omitempty"`
	ActualValue   string `json:"actualValue" firestore:"actualValue"`
	ExpectedValue string `json:"expectedValue,omitempty" firestore:"expectedValue,omitempty"`

	// Context
	Rule        string  `json:"rule" firestore:"rule"`
	Description string  `json:"description" firestore:"description"`
	Confidence  float64 `json:"confidence" firestore:"confidence"`

	// For duplicate detection
	DuplicateOfID   string `json:"duplicateOfId,omitempty" firestore:"duplicateOfId,omitempty"`
	DuplicateOfPath string `json:"duplicateOfPath,omitempty" firestore:"duplicateOfPath,omitempty"`

	// Resolution
	AutoFixable  bool             `json:"autoFixable" firestore:"autoFixable"`
	SuggestedFix *EnforcementTask `json:"suggestedFix,omitempty" firestore:"suggestedFix,omitempty"`

	// Metadata
	DetectedAt time.Time `json:"detectedAt" firestore:"detectedAt"`
	DetectedBy string    `json:"detectedBy" firestore:"detectedBy"` // "llm" or "deterministic"
}

// =============================================================================
// ENFORCEMENT TASK TYPES
// =============================================================================

// EnforcementTaskType represents the type of enforcement action.
type EnforcementTaskType string

const (
	TaskMergeFragments     EnforcementTaskType = "merge_fragments"
	TaskFixPath            EnforcementTaskType = "fix_path"
	TaskReverseEdge        EnforcementTaskType = "reverse_edge"
	TaskCanonicalizeEdge   EnforcementTaskType = "canonicalize_edge"
	TaskDeleteEdge         EnforcementTaskType = "delete_edge"
	TaskDeleteFragment     EnforcementTaskType = "delete_fragment"
	TaskResolveGeoNames    EnforcementTaskType = "resolve_geonames"
	TaskNormalizeSymmetric EnforcementTaskType = "normalize_symmetric"
	TaskFixISO2            EnforcementTaskType = "fix_iso2"
	TaskEnrichEdge         EnforcementTaskType = "enrich_edge"
)

// EnforcementTask represents a single enforcement action to fix a violation.
type EnforcementTask struct {
	Type     EnforcementTaskType    `json:"type" firestore:"type"`
	TargetID string                 `json:"targetId" firestore:"targetId"`
	Params   map[string]interface{} `json:"params,omitempty" firestore:"params,omitempty"`
}

// =============================================================================
// ENFORCEMENT JOB
// =============================================================================

// EnforcementJobStatus represents the status of an enforcement job.
type EnforcementJobStatus string

const (
	EnforcementStatusPending    EnforcementJobStatus = "pending"
	EnforcementStatusInProgress EnforcementJobStatus = "in_progress"
	EnforcementStatusCompleted  EnforcementJobStatus = "completed"
	EnforcementStatusFailed     EnforcementJobStatus = "failed"
	EnforcementStatusSkipped    EnforcementJobStatus = "skipped"
)

// PolicyEnforcementJob represents a job to fix a policy violation.
type PolicyEnforcementJob struct {
	ID          string               `json:"id" firestore:"id"`
	TenantID    string               `json:"tenantId" firestore:"tenantId"`
	ParentJobID string               `json:"parentJobId" firestore:"parentJobId"`
	ViolationID string               `json:"violationId" firestore:"violationId"`
	Violation   *PolicyViolation     `json:"violation,omitempty" firestore:"violation,omitempty"`
	Task        EnforcementTask      `json:"task" firestore:"task"`
	Status      EnforcementJobStatus `json:"status" firestore:"status"`
	Result      *EnforcementResult   `json:"result,omitempty" firestore:"result,omitempty"`
	CreatedAt   time.Time            `json:"createdAt" firestore:"createdAt"`
	UpdatedAt   time.Time            `json:"updatedAt" firestore:"updatedAt"`
	CompletedAt *time.Time           `json:"completedAt,omitempty" firestore:"completedAt,omitempty"`
}

// EnforcementResult represents the result of an enforcement action.
type EnforcementResult struct {
	Success     bool     `json:"success" firestore:"success"`
	Action      string   `json:"action" firestore:"action"`
	AffectedIDs []string `json:"affectedIds,omitempty" firestore:"affectedIds,omitempty"`
	Error       string   `json:"error,omitempty" firestore:"error,omitempty"`
	Details     string   `json:"details,omitempty" firestore:"details,omitempty"`
}

// =============================================================================
// POLICY CHECK RESULT
// =============================================================================

// PolicyCheckResult represents the result of a policy check on a job.
type PolicyCheckResult struct {
	JobID             string            `json:"jobId" firestore:"jobId"`
	TenantID          string            `json:"tenantId" firestore:"tenantId"`
	Violations        []PolicyViolation `json:"violations" firestore:"violations"`
	EnforcementJobIDs []string          `json:"enforcementJobIds,omitempty" firestore:"enforcementJobIds,omitempty"`
	CheckedAt         time.Time         `json:"checkedAt" firestore:"checkedAt"`

	// Summary
	TotalViolations  int `json:"totalViolations" firestore:"totalViolations"`
	CriticalCount    int `json:"criticalCount" firestore:"criticalCount"`
	WarningCount     int `json:"warningCount" firestore:"warningCount"`
	AutoFixableCount int `json:"autoFixableCount" firestore:"autoFixableCount"`

	// Fragments and edges checked
	FragmentsChecked int `json:"fragmentsChecked" firestore:"fragmentsChecked"`
	EdgesChecked     int `json:"edgesChecked" firestore:"edgesChecked"`
}

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

// NewMergeTask creates an enforcement task to merge two fragments.
func NewMergeTask(duplicateID, canonicalID string) EnforcementTask {
	return EnforcementTask{
		Type:     TaskMergeFragments,
		TargetID: duplicateID,
		Params: map[string]interface{}{
			"canonicalId": canonicalID,
		},
	}
}

// NewFixPathTask creates an enforcement task to fix a canonical path.
func NewFixPathTask(fragmentID, currentPath, correctPath string) EnforcementTask {
	return EnforcementTask{
		Type:     TaskFixPath,
		TargetID: fragmentID,
		Params: map[string]interface{}{
			"currentPath": currentPath,
			"correctPath": correctPath,
		},
	}
}

// NewReverseEdgeTask creates an enforcement task to reverse an edge direction.
func NewReverseEdgeTask(edgeID string) EnforcementTask {
	return EnforcementTask{
		Type:     TaskReverseEdge,
		TargetID: edgeID,
	}
}

// NewCanonicalizeEdgeTask creates an enforcement task to canonicalize an edge type.
func NewCanonicalizeEdgeTask(edgeID string, newType ah.RelationName, shouldSwap bool) EnforcementTask {
	return EnforcementTask{
		Type:     TaskCanonicalizeEdge,
		TargetID: edgeID,
		Params: map[string]interface{}{
			"newType":    string(newType),
			"shouldSwap": shouldSwap,
		},
	}
}

// NewDeleteEdgeTask creates an enforcement task to delete an edge.
func NewDeleteEdgeTask(edgeID, reason string) EnforcementTask {
	return EnforcementTask{
		Type:     TaskDeleteEdge,
		TargetID: edgeID,
		Params: map[string]interface{}{
			"reason": reason,
		},
	}
}

// NewResolveGeoNamesTask creates an enforcement task to resolve GeoNames admin code.
func NewResolveGeoNamesTask(fragmentID, currentPath string) EnforcementTask {
	return EnforcementTask{
		Type:     TaskResolveGeoNames,
		TargetID: fragmentID,
		Params: map[string]interface{}{
			"currentPath": currentPath,
		},
	}
}

// NewNormalizeSymmetricTask creates an enforcement task to normalize a symmetric edge.
func NewNormalizeSymmetricTask(edgeID string) EnforcementTask {
	return EnforcementTask{
		Type:     TaskNormalizeSymmetric,
		TargetID: edgeID,
	}
}
