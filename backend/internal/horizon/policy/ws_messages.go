package policy

import "time"

// =============================================================================
// WEBSOCKET MESSAGE TYPES FOR POLICY ENFORCEMENT
// =============================================================================
// These messages are sent over WebSocket to notify clients about policy
// enforcement progress and completion.

// WSMessageType constants for policy enforcement messages.
// Using horizon.policy.* namespace for consistency with other horizon WS events.
const (
	WSTypePolicyCheckComplete      = "horizon.policy.check_complete"
	WSTypeEnforcementProgress      = "horizon.policy.enforcement_progress"
	WSTypeEnforcementBatchComplete = "horizon.policy.enforcement_batch_complete"
	WSTypeViolationDetected        = "horizon.policy.violation_detected"
)

// WSPolicyCheckComplete is sent when policy check completes for a job.
type WSPolicyCheckComplete struct {
	Type              string   `json:"type"` // "policy_check_complete"
	JobID             string   `json:"jobId"`
	TenantID          string   `json:"tenantId"`
	ViolationsFound   int      `json:"violationsFound"`
	CriticalCount     int      `json:"criticalCount"`
	WarningCount      int      `json:"warningCount"`
	InfoCount         int      `json:"infoCount"`
	EnforcementJobIDs []string `json:"enforcementJobIds,omitempty"`
	AutoFixableCount  int      `json:"autoFixableCount"`
	CheckDurationMs   int64    `json:"checkDurationMs"`
	Timestamp         string   `json:"timestamp"`
}

// NewWSPolicyCheckComplete creates a WebSocket message from a PolicyCheckResult.
func NewWSPolicyCheckComplete(result *PolicyCheckResult, durationMs int64) WSPolicyCheckComplete {
	infoCount := result.TotalViolations - result.CriticalCount - result.WarningCount

	return WSPolicyCheckComplete{
		Type:              WSTypePolicyCheckComplete,
		JobID:             result.JobID,
		TenantID:          result.TenantID,
		ViolationsFound:   result.TotalViolations,
		CriticalCount:     result.CriticalCount,
		WarningCount:      result.WarningCount,
		InfoCount:         infoCount,
		EnforcementJobIDs: result.EnforcementJobIDs,
		AutoFixableCount:  result.AutoFixableCount,
		CheckDurationMs:   durationMs,
		Timestamp:         time.Now().UTC().Format(time.RFC3339),
	}
}

// WSEnforcementProgress is sent during enforcement job execution.
type WSEnforcementProgress struct {
	Type          string  `json:"type"` // "enforcement_progress"
	JobID         string  `json:"jobId"`
	ParentJobID   string  `json:"parentJobId"`
	TenantID      string  `json:"tenantId"`
	Status        string  `json:"status"` // "pending", "in_progress", "completed", "failed"
	ViolationType string  `json:"violationType"`
	TaskType      string  `json:"taskType"`
	TargetID      string  `json:"targetId"`
	Progress      float64 `json:"progress"` // 0.0 - 1.0
	Message       string  `json:"message,omitempty"`
	Timestamp     string  `json:"timestamp"`

	// Result (only when completed)
	Success     *bool    `json:"success,omitempty"`
	AffectedIDs []string `json:"affectedIds,omitempty"`
	Error       string   `json:"error,omitempty"`
}

// NewWSEnforcementProgress creates a WebSocket message from an enforcement job.
func NewWSEnforcementProgress(job *PolicyEnforcementJob) WSEnforcementProgress {
	msg := WSEnforcementProgress{
		Type:        WSTypeEnforcementProgress,
		JobID:       job.ID,
		ParentJobID: job.ParentJobID,
		TenantID:    job.TenantID,
		Status:      string(job.Status),
		TaskType:    string(job.Task.Type),
		TargetID:    job.Task.TargetID,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}

	if job.Violation != nil {
		msg.ViolationType = string(job.Violation.Type)
	}

	// Set progress based on status
	switch job.Status {
	case EnforcementStatusPending:
		msg.Progress = 0.0
	case EnforcementStatusInProgress:
		msg.Progress = 0.5
	case EnforcementStatusCompleted, EnforcementStatusFailed, EnforcementStatusSkipped:
		msg.Progress = 1.0
	}

	// Add result if available
	if job.Result != nil {
		msg.Success = &job.Result.Success
		msg.AffectedIDs = job.Result.AffectedIDs
		msg.Error = job.Result.Error
		if job.Result.Details != "" {
			msg.Message = job.Result.Details
		}
	}

	return msg
}

// WSEnforcementBatchComplete is sent when all enforcement jobs complete.
type WSEnforcementBatchComplete struct {
	Type        string `json:"type"` // "enforcement_batch_complete"
	ParentJobID string `json:"parentJobId"`
	TenantID    string `json:"tenantId"`
	TotalJobs   int    `json:"totalJobs"`
	Completed   int    `json:"completed"`
	Failed      int    `json:"failed"`
	Skipped     int    `json:"skipped"`
	DurationMs  int64  `json:"durationMs"`
	Timestamp   string `json:"timestamp"`

	// Summary of actions taken
	FragmentsMerged    int `json:"fragmentsMerged"`
	PathsFixed         int `json:"pathsFixed"`
	EdgesReversed      int `json:"edgesReversed"`
	EdgesCanonicalized int `json:"edgesCanonicalized"`
	EdgesDeleted       int `json:"edgesDeleted"`
}

// NewWSEnforcementBatchComplete creates a WebSocket message for batch completion.
func NewWSEnforcementBatchComplete(parentJobID, tenantID string, jobs []*PolicyEnforcementJob, durationMs int64) WSEnforcementBatchComplete {
	msg := WSEnforcementBatchComplete{
		Type:        WSTypeEnforcementBatchComplete,
		ParentJobID: parentJobID,
		TenantID:    tenantID,
		TotalJobs:   len(jobs),
		DurationMs:  durationMs,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}

	for _, job := range jobs {
		switch job.Status {
		case EnforcementStatusCompleted:
			msg.Completed++
			// Count actions
			if job.Result != nil && job.Result.Success {
				switch job.Task.Type {
				case TaskMergeFragments:
					msg.FragmentsMerged++
				case TaskFixPath, TaskResolveGeoNames, TaskFixLegalForm, TaskFixISO2:
					msg.PathsFixed++
				case TaskReverseEdge:
					msg.EdgesReversed++
				case TaskCanonicalizeEdge:
					msg.EdgesCanonicalized++
				case TaskDeleteEdge:
					msg.EdgesDeleted++
				}
			}
		case EnforcementStatusFailed:
			msg.Failed++
		case EnforcementStatusSkipped:
			msg.Skipped++
		}
	}

	return msg
}

// WSViolationDetected is sent when a violation is detected (for real-time monitoring).
type WSViolationDetected struct {
	Type          string                  `json:"type"` // "violation_detected"
	JobID         string                  `json:"jobId"`
	TenantID      string                  `json:"tenantId"`
	ViolationID   string                  `json:"violationId"`
	ViolationType PolicyViolationType     `json:"violationType"`
	Severity      PolicyViolationSeverity `json:"severity"`
	FragmentID    string                  `json:"fragmentId,omitempty"`
	EdgeID        string                  `json:"edgeId,omitempty"`
	Description   string                  `json:"description"`
	AutoFixable   bool                    `json:"autoFixable"`
	Timestamp     string                  `json:"timestamp"`
}

// NewWSViolationDetected creates a WebSocket message for a detected violation.
func NewWSViolationDetected(jobID, tenantID string, violation *PolicyViolation) WSViolationDetected {
	return WSViolationDetected{
		Type:          WSTypeViolationDetected,
		JobID:         jobID,
		TenantID:      tenantID,
		ViolationID:   violation.ID,
		ViolationType: violation.Type,
		Severity:      violation.Severity,
		FragmentID:    violation.FragmentID,
		EdgeID:        violation.EdgeID,
		Description:   violation.Description,
		AutoFixable:   violation.AutoFixable,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
	}
}

// =============================================================================
// PUBLISHER INTERFACE
// =============================================================================

// WSPublisher is the interface for publishing WebSocket messages.
type WSPublisher interface {
	// Publish sends a message to subscribers for a specific topic.
	Publish(ctx interface{}, topic string, message interface{}) error
}

// PolicyWSPublisher wraps a generic WebSocket publisher for policy messages.
type PolicyWSPublisher struct {
	publisher WSPublisher
}

// NewPolicyWSPublisher creates a new PolicyWSPublisher.
func NewPolicyWSPublisher(publisher WSPublisher) *PolicyWSPublisher {
	return &PolicyWSPublisher{publisher: publisher}
}

// PublishPolicyCheckComplete publishes a policy check complete message.
func (p *PolicyWSPublisher) PublishPolicyCheckComplete(ctx interface{}, result *PolicyCheckResult, durationMs int64) error {
	if p.publisher == nil {
		return nil
	}
	msg := NewWSPolicyCheckComplete(result, durationMs)
	topic := "job:" + result.JobID
	return p.publisher.Publish(ctx, topic, msg)
}

// PublishEnforcementProgress publishes an enforcement progress message.
func (p *PolicyWSPublisher) PublishEnforcementProgress(ctx interface{}, job *PolicyEnforcementJob) error {
	if p.publisher == nil {
		return nil
	}
	msg := NewWSEnforcementProgress(job)
	topic := "job:" + job.ParentJobID
	return p.publisher.Publish(ctx, topic, msg)
}

// PublishEnforcementBatchComplete publishes a batch completion message.
func (p *PolicyWSPublisher) PublishEnforcementBatchComplete(ctx interface{}, parentJobID, tenantID string, jobs []*PolicyEnforcementJob, durationMs int64) error {
	if p.publisher == nil {
		return nil
	}
	msg := NewWSEnforcementBatchComplete(parentJobID, tenantID, jobs, durationMs)
	topic := "job:" + parentJobID
	return p.publisher.Publish(ctx, topic, msg)
}

// PublishViolationDetected publishes a violation detected message.
func (p *PolicyWSPublisher) PublishViolationDetected(ctx interface{}, jobID, tenantID string, violation *PolicyViolation) error {
	if p.publisher == nil {
		return nil
	}
	msg := NewWSViolationDetected(jobID, tenantID, violation)
	topic := "job:" + jobID
	return p.publisher.Publish(ctx, topic, msg)
}
