package policy

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	grpcCodes "google.golang.org/grpc/codes"
	grpcStatus "google.golang.org/grpc/status"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/ontology"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// =============================================================================
// REPOSITORY INTERFACES FOR ENFORCEMENT EXECUTOR
// =============================================================================

// FragmentsWriter provides read/write access to HorizonFragments.
type FragmentsWriter interface {
	Get(ctx context.Context, id string) (*horizonmodels.HorizonFragment, error)
	// GetByCanonicalPath returns a fragment by exact canonicalPath (global tenant).
	// Used for deterministic collision detection after path rewrites.
	GetByCanonicalPath(ctx context.Context, canonicalPath string) (*horizonmodels.HorizonFragment, error)
	Update(ctx context.Context, frag *horizonmodels.HorizonFragment) error
	Delete(ctx context.Context, id string) error
}

// EdgesWriter provides read/write access to DependencyEdges.
type EdgesWriter interface {
	Get(ctx context.Context, id string) (*horizonmodels.DependencyEdge, error)
	Update(ctx context.Context, edge *horizonmodels.DependencyEdge) error
	Delete(ctx context.Context, id string) error
	ListBySource(ctx context.Context, tenantID, sourceID string, edgeType *horizonmodels.DependencyType, limit int) ([]*horizonmodels.DependencyEdge, error)
	ListByTarget(ctx context.Context, tenantID, targetID string, edgeType *horizonmodels.DependencyType, limit int) ([]*horizonmodels.DependencyEdge, error)
}

// EnforcementJobsWriter provides read/write access to enforcement jobs.
type EnforcementJobsWriter interface {
	UpdateStatus(ctx context.Context, id string, status EnforcementJobStatus, result *EnforcementResult) error
	ListByParentJob(ctx context.Context, parentJobID string, limit int) ([]*PolicyEnforcementJob, error)
	CreateBatch(ctx context.Context, jobs []*PolicyEnforcementJob) error
}

// LensesWriter provides best-effort read/write access to Lens scope.
// Used to propagate canonicalPath rewrites into UI scope after enforcement fixes.
type LensesWriter interface {
	FindByScope(ctx context.Context, tenantID, canonicalPath string, limit int) ([]*horizonmodels.Lens, error)
	Update(ctx context.Context, lens *horizonmodels.Lens) error
}

// RedirectRecorder records canonical path redirects so Resolve() returns the final path.
// Used when enforcement fixes a path (oldPath -> newPath) to avoid future duplicates.
type RedirectRecorder interface {
	RecordRedirect(ctx context.Context, fromPath, toPath string, fragmentType horizonmodels.FragmentType, reason string)
}

// EnforcementExecutor executes policy enforcement tasks.
type EnforcementExecutor struct {
	fragmentsRepo   FragmentsWriter
	edgesRepo       EdgesWriter
	enforcementRepo EnforcementJobsWriter
	geonamesClient  ontology.GeoNamesClientInterface
	lensesRepo      LensesWriter
	aiExecutor      *ai.AITaskExecutor

	// Event publisher for WebSocket updates
	publisher EnforcementEventPublisher

	// Merge fragments: single entry point for merge (data, edges, delete, audit).
	// When nil, merge tasks will fail (merge repo must be wired).
	mergeFragments MergeFragments

	// Optional: records redirects when path is fixed so Resolve() returns the final path.
	redirectRecorder RedirectRecorder
}

// EnforcementEventPublisher publishes enforcement events.
type EnforcementEventPublisher interface {
	PublishEnforcementProgress(ctx context.Context, msg EnforcementProgressMessage) error
	PublishEnforcementComplete(ctx context.Context, msg EnforcementCompleteMessage) error
}

// EnforcementProgressMessage is sent during enforcement execution.
type EnforcementProgressMessage struct {
	Type          string             `json:"type"` // "enforcement_progress"
	JobID         string             `json:"jobId"`
	ParentJobID   string             `json:"parentJobId"`
	Status        string             `json:"status"`
	ViolationType string             `json:"violationType"`
	Action        string             `json:"action"`
	Progress      float64            `json:"progress"`
	Result        *EnforcementResult `json:"result,omitempty"`
}

// EnforcementCompleteMessage is sent when all enforcement jobs complete.
type EnforcementCompleteMessage struct {
	Type        string `json:"type"` // "enforcement_batch_complete"
	ParentJobID string `json:"parentJobId"`
	TotalJobs   int    `json:"totalJobs"`
	Completed   int    `json:"completed"`
	Failed      int    `json:"failed"`
	Skipped     int    `json:"skipped"`
}

// NewEnforcementExecutor creates a new EnforcementExecutor.
func NewEnforcementExecutor(
	fragmentsRepo FragmentsWriter,
	edgesRepo EdgesWriter,
	enforcementRepo EnforcementJobsWriter,
) *EnforcementExecutor {
	return &EnforcementExecutor{
		fragmentsRepo:   fragmentsRepo,
		edgesRepo:       edgesRepo,
		enforcementRepo: enforcementRepo,
	}
}

// WithLensesRepo wires a lenses repository so path fixes can rewrite lens scope (best-effort).
func (e *EnforcementExecutor) WithLensesRepo(repo LensesWriter) *EnforcementExecutor {
	e.lensesRepo = repo
	return e
}

// WithGeoNamesClient sets the GeoNames client.
func (e *EnforcementExecutor) WithGeoNamesClient(client ontology.GeoNamesClientInterface) *EnforcementExecutor {
	e.geonamesClient = client
	return e
}

// WithMergeFragments wires the merge repository (data, edges, delete, audit).
func (e *EnforcementExecutor) WithMergeFragments(m MergeFragments) *EnforcementExecutor {
	e.mergeFragments = m
	return e
}

// WithRedirectRecorder wires a redirect recorder. When set, executeFixPath records
// oldPath -> newPath so canonical path resolution returns the corrected path.
func (e *EnforcementExecutor) WithRedirectRecorder(r RedirectRecorder) *EnforcementExecutor {
	if e == nil {
		return e
	}
	e.redirectRecorder = r
	return e
}

// WithAIExecutor wires an AI executor (required for LLM-based auto-fixes like fix_iso2).
func (e *EnforcementExecutor) WithAIExecutor(executor *ai.AITaskExecutor) *EnforcementExecutor {
	e.aiExecutor = executor
	return e
}

// WithPublisher sets the event publisher.
func (e *EnforcementExecutor) WithPublisher(publisher EnforcementEventPublisher) *EnforcementExecutor {
	e.publisher = publisher
	return e
}

// ExecuteJob executes a single enforcement job.
func (e *EnforcementExecutor) ExecuteJob(ctx context.Context, job *PolicyEnforcementJob) (*EnforcementResult, error) {
	if job == nil {
		return nil, fmt.Errorf("job is nil")
	}

	// Enforcement is async/background work. Do not inherit short request/WS deadlines.
	// Keep context values (trace/request ids) but detach cancellation/deadlines, then apply a bounded timeout.
	{
		const enforcementJobTimeout = 2 * time.Minute
		jobCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), enforcementJobTimeout)
		defer cancel()
		ctx = jobCtx
	}

	log.Printf("[EnforcementExecutor] Executing job %s: type=%s target=%s",
		job.ID, job.Task.Type, job.Task.TargetID)
	LogPolicyEvent("enforcement_policy", PolicyLogStageInput, map[string]interface{}{
		"trace_id":       "na",
		"tenant_id":      job.TenantID,
		"job_id":         job.ParentJobID,
		"edge_id":        job.Task.TargetID,
		"action":         string(job.Task.Type),
		"policy_status":  "na",
		"policy_reason":  "",
		"policy_penalty": 0,
		"decision_path":  "enforcement_execute",
		"rule_id":        job.ViolationID,
		"confidence":     0,
	})

	// Update status to in_progress
	job.Status = EnforcementStatusInProgress
	job.UpdatedAt = time.Now().UTC()
	if e.enforcementRepo != nil {
		_ = e.enforcementRepo.UpdateStatus(ctx, job.ID, EnforcementStatusInProgress, nil)
	}

	// Publish progress
	if e.publisher != nil {
		_ = e.publisher.PublishEnforcementProgress(ctx, EnforcementProgressMessage{
			Type:          "enforcement_progress",
			JobID:         job.ID,
			ParentJobID:   job.ParentJobID,
			Status:        string(EnforcementStatusInProgress),
			ViolationType: string(job.Violation.Type),
			Action:        string(job.Task.Type),
			Progress:      0.5,
		})
	}

	// Execute based on task type
	var result *EnforcementResult
	var err error

	switch job.Task.Type {
	case TaskMergeFragments:
		result, err = e.executeMergeFragments(ctx, job)
	case TaskFixPath:
		result, err = e.executeFixPath(ctx, job.Task)
	case TaskReverseEdge:
		result, err = e.executeReverseEdge(ctx, job.TenantID, job.Task)
	case TaskCanonicalizeEdge:
		result, err = e.executeCanonicalizeEdge(ctx, job.TenantID, job.Task)
	case TaskDeleteEdge:
		result, err = e.executeDeleteEdge(ctx, job.TenantID, job.Task)
	case TaskDeleteFragment:
		result, err = e.executeDeleteFragment(ctx, job.Task)
	case TaskResolveGeoNames:
		result, err = e.executeResolveGeoNames(ctx, job.Task)
	case TaskNormalizeSymmetric:
		result, err = e.executeNormalizeSymmetric(ctx, job.TenantID, job.Task)
	case TaskFixISO2:
		result, err = e.executeFixISO2(ctx, job.Task)
	case TaskEnrichEdge:
		result, err = e.executeEnrichEdge(ctx, job.Task)
	default:
		err = fmt.Errorf("unknown task type: %s", job.Task.Type)
		result = &EnforcementResult{
			Success: false,
			Action:  string(job.Task.Type),
			Error:   err.Error(),
		}
	}

	if err != nil {
		result = &EnforcementResult{
			Success: false,
			Action:  string(job.Task.Type),
			Error:   err.Error(),
		}
	}

	// Update job status
	status := EnforcementStatusCompleted
	if !result.Success {
		status = EnforcementStatusFailed
	}
	job.Status = status
	job.Result = result
	now := time.Now().UTC()
	job.CompletedAt = &now
	job.UpdatedAt = now

	if e.enforcementRepo != nil {
		_ = e.enforcementRepo.UpdateStatus(ctx, job.ID, status, result)
	}

	// Publish completion
	if e.publisher != nil {
		_ = e.publisher.PublishEnforcementProgress(ctx, EnforcementProgressMessage{
			Type:          "enforcement_progress",
			JobID:         job.ID,
			ParentJobID:   job.ParentJobID,
			Status:        string(status),
			ViolationType: string(job.Violation.Type),
			Action:        string(job.Task.Type),
			Progress:      1.0,
			Result:        result,
		})
	}

	log.Printf("[EnforcementExecutor] Job %s completed: success=%v action=%s",
		job.ID, result.Success, result.Action)
	LogPolicyEvent("enforcement_policy", PolicyLogStageResult, map[string]interface{}{
		"trace_id":       "na",
		"tenant_id":      job.TenantID,
		"job_id":         job.ParentJobID,
		"edge_id":        job.Task.TargetID,
		"action":         result.Action,
		"policy_status":  string(status),
		"policy_reason":  result.Error,
		"policy_penalty": 0,
		"decision_path":  "enforcement_execute",
		"rule_id":        job.ViolationID,
		"confidence":     1,
	})

	return result, nil
}

// executeMergeFragments merges a duplicate fragment into the canonical one via MergeFragments.
func (e *EnforcementExecutor) executeMergeFragments(ctx context.Context, job *PolicyEnforcementJob) (*EnforcementResult, error) {
	task := job.Task
	duplicateID := strings.TrimSpace(task.TargetID)
	canonicalID, ok := task.Params["canonicalId"].(string)
	canonicalID = strings.TrimSpace(canonicalID)
	if !ok || canonicalID == "" {
		return nil, fmt.Errorf("canonicalId not specified")
	}
	if duplicateID == "" {
		return nil, fmt.Errorf("target duplicate id not specified")
	}
	if duplicateID == canonicalID {
		return mergeFragmentsSkippedResult(duplicateID, canonicalID, "duplicate and canonical ids are identical"), nil
	}

	// Load duplicate for tenant + lens scope rewrite (before merge deletes it)
	duplicate, err := e.fragmentsRepo.Get(ctx, duplicateID)
	if err != nil {
		if isNotFoundError(err) {
			return mergeFragmentsSkippedResult(duplicateID, canonicalID, "duplicate fragment already absent"), nil
		}
		return nil, fmt.Errorf("failed to load duplicate fragment: %w", err)
	}
	if duplicate == nil {
		return mergeFragmentsSkippedResult(duplicateID, canonicalID, "duplicate fragment already absent"), nil
	}
	canonical, err := e.fragmentsRepo.Get(ctx, canonicalID)
	if err != nil {
		if isNotFoundError(err) {
			return mergeFragmentsSkippedResult(duplicateID, canonicalID, "canonical fragment already absent"), nil
		}
		return nil, fmt.Errorf("failed to load canonical fragment: %w", err)
	}
	if canonical == nil {
		return mergeFragmentsSkippedResult(duplicateID, canonicalID, "canonical fragment already absent"), nil
	}

	if e.mergeFragments == nil {
		return nil, fmt.Errorf("merge fragments repo not configured")
	}

	tenantID := strings.TrimSpace(duplicate.TenantID)
	if tenantID == "" {
		tenantID = "global"
	}

	res, err := e.mergeFragments.MergeFragment(ctx, canonicalID, duplicateID, tenantID, "enforcement", "", job.ParentJobID, "")
	if err != nil {
		return nil, fmt.Errorf("merge failed: %w", err)
	}

	// Best-effort: rewrite lens scopes from duplicate canonicalPath to canonical canonicalPath
	if e.lensesRepo != nil {
		e.bestEffortRewriteLensScopes(ctx, tenantID, strings.TrimSpace(duplicate.CanonicalPath), strings.TrimSpace(canonical.CanonicalPath))
	}

	return &EnforcementResult{
		Success:     true,
		Action:      "merged",
		AffectedIDs: []string{canonicalID, duplicateID},
		Details:     fmt.Sprintf("Merged %s into %s. Edges: %d reassigned, %d deleted.", duplicateID, canonicalID, res.EdgesReassigned, res.EdgesDeleted),
	}, nil
}

func mergeFragmentsSkippedResult(duplicateID, canonicalID, reason string) *EnforcementResult {
	ids := make([]string, 0, 2)
	if canonicalID != "" {
		ids = append(ids, canonicalID)
	}
	if duplicateID != "" {
		ids = append(ids, duplicateID)
	}
	return &EnforcementResult{
		Success:     true,
		Action:      "merge_fragments_skipped",
		AffectedIDs: ids,
		Details:     fmt.Sprintf("Skipped merge %s -> %s: %s.", duplicateID, canonicalID, strings.TrimSpace(reason)),
	}
}

// executeFixPath fixes a fragment's canonical path.
func (e *EnforcementExecutor) executeFixPath(ctx context.Context, task EnforcementTask) (*EnforcementResult, error) {
	fragID := task.TargetID
	correctPath, ok := task.Params["correctPath"].(string)
	if !ok || correctPath == "" {
		return nil, fmt.Errorf("correctPath not specified")
	}

	frag, err := e.fragmentsRepo.Get(ctx, fragID)
	if err != nil {
		return nil, fmt.Errorf("failed to load fragment: %w", err)
	}

	oldPath := frag.CanonicalPath
	frag.UpdatedAt = time.Now().UTC()

	// Prefer writing canonicalParts over canonicalPath so independent fixers can safely touch different parts.
	// For org/gov, apply selectively (geo vs legalForm) rather than overwriting the whole struct.
	trimmedCorrect := strings.TrimSpace(correctPath)
	parsed, perr := taxonomy.ParseCanonicalPathParts(trimmedCorrect)
	if perr == nil && parsed != nil {
		switch strings.TrimSpace(parsed.Namespace) {
		case "org":
			_, _, aerr := (&OrgSelectiveCanonizer{}).ApplyFromCanonicalPath(frag, trimmedCorrect)
			if aerr != nil {
				frag.CanonicalParts = parsed // fallback
			}
		case "gov":
			_, _, aerr := (&GovSelectiveCanonizer{}).ApplyFromCanonicalPath(frag, trimmedCorrect)
			if aerr != nil {
				frag.CanonicalParts = parsed // fallback
			}
		default:
			// For other namespaces the "fix" is typically a full rewrite; keep it simple for now.
			frag.CanonicalParts = parsed
		}
	} else {
		// Fail-open: keep old canonicalParts; fall back to legacy behavior (string path update).
		frag.CanonicalPath = correctPath
	}

	// Store old path in identifiers
	if frag.Identifiers == nil {
		frag.Identifiers = make(map[string]string)
	}
	if existing := frag.Identifiers["merged_paths"]; existing != "" {
		frag.Identifiers["merged_paths"] = existing + "," + oldPath
	} else {
		frag.Identifiers["merged_paths"] = oldPath
	}

	if err := e.fragmentsRepo.Update(ctx, frag); err != nil {
		return nil, fmt.Errorf("failed to update fragment: %w", err)
	}

	// Best-effort: rewrite lens scopes referencing the old path to the corrected path.
	newPath := strings.TrimSpace(correctPath)
	if frag != nil && strings.TrimSpace(frag.CanonicalPath) != "" {
		// After Update(), repository may have reassembled canonicalPath from canonicalParts; prefer the persisted value.
		newPath = strings.TrimSpace(frag.CanonicalPath)
	}
	e.bestEffortRewriteLensScopes(ctx, strings.TrimSpace(frag.TenantID), strings.TrimSpace(oldPath), newPath)

	// Record redirect so Resolve() returns the corrected path on future LLM outputs.
	if e.redirectRecorder != nil && strings.TrimSpace(oldPath) != "" && newPath != "" && strings.TrimSpace(oldPath) != newPath {
		e.redirectRecorder.RecordRedirect(ctx, strings.TrimSpace(oldPath), newPath, frag.Type, "enforcement_fix_path")
	}

	// Deterministic: if this fix caused a canonicalPath collision (two fragment IDs now share the same canonicalPath),
	// immediately merge them so edges (which are keyed by fragment IDs) converge to a single node.
	merged, mergeCanonicalID, mergeDuplicateID := e.bestEffortMergeCanonicalPathCollision(ctx, frag.ID, newPath)

	return &EnforcementResult{
		Success: true,
		Action:  "path_fixed",
		AffectedIDs: func() []string {
			if merged && strings.TrimSpace(mergeCanonicalID) != "" && strings.TrimSpace(mergeDuplicateID) != "" {
				return []string{mergeCanonicalID, mergeDuplicateID}
			}
			return []string{fragID}
		}(),
		Details: func() string {
			base := fmt.Sprintf("Changed path from %s to %s", oldPath, newPath)
			if merged && strings.TrimSpace(mergeCanonicalID) != "" && strings.TrimSpace(mergeDuplicateID) != "" {
				base += fmt.Sprintf(". CanonicalPath collision detected; merged %s into %s.", mergeDuplicateID, mergeCanonicalID)
			}
			return base
		}(),
	}, nil
}

// bestEffortRewriteLensScopes rewrites lens.Scope entries from fromPath -> toPath for the given tenant.
// This mirrors ScoutEngine.bestEffortRewriteLensScope, but works without lensId (enforcement runs async).
func (e *EnforcementExecutor) bestEffortRewriteLensScopes(ctx context.Context, tenantID, fromPath, toPath string) {
	if e == nil || e.lensesRepo == nil {
		return
	}
	tenantID = strings.TrimSpace(tenantID)
	fromPath = strings.TrimSpace(fromPath)
	toPath = strings.TrimSpace(toPath)
	if tenantID == "" || fromPath == "" || toPath == "" || fromPath == toPath {
		return
	}
	// Best-effort: update a bounded number of lenses to avoid heavy fanout.
	lenses, err := e.lensesRepo.FindByScope(ctx, tenantID, fromPath, 200)
	if err != nil || len(lenses) == 0 {
		return
	}
	for _, lens := range lenses {
		if lens == nil {
			continue
		}
		changed := false
		for i, s := range lens.Scope {
			if strings.TrimSpace(s) == fromPath {
				lens.Scope[i] = toPath
				changed = true
			}
		}
		if !changed {
			continue
		}
		// Best-effort: keep scopeFragments consistent.
		if lens.ScopeFragments != nil {
			if fid, ok := lens.ScopeFragments[fromPath]; ok {
				delete(lens.ScopeFragments, fromPath)
				if _, ok2 := lens.ScopeFragments[toPath]; !ok2 && strings.TrimSpace(fid) != "" {
					lens.ScopeFragments[toPath] = fid
				}
			}
		}
		// Best-effort: keep ResolvedQueries cache consistent.
		// This cache stores results of QueryAddress resolution (e.g., /query/sector:/Tech/** -> [paths]).
		if lens.ResolvedQueries != nil {
			for queryAddr, resolvedPaths := range lens.ResolvedQueries {
				updated := false
				for i, p := range resolvedPaths {
					if strings.TrimSpace(p) == fromPath {
						resolvedPaths[i] = toPath
						updated = true
					}
				}
				if updated {
					// De-dupe after rewrite.
					seen := map[string]struct{}{}
					out := make([]string, 0, len(resolvedPaths))
					for _, p := range resolvedPaths {
						p = strings.TrimSpace(p)
						if p == "" {
							continue
						}
						if _, ok := seen[p]; ok {
							continue
						}
						seen[p] = struct{}{}
						out = append(out, p)
					}
					lens.ResolvedQueries[queryAddr] = out
				}
			}
		}
		// De-dupe scope after rewrite.
		seen := map[string]struct{}{}
		out := make([]string, 0, len(lens.Scope))
		for _, s := range lens.Scope {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if _, ok := seen[s]; ok {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
		lens.Scope = out
		_ = e.lensesRepo.Update(ctx, lens)
	}
}

// executeReverseEdge reverses an edge's direction.
func (e *EnforcementExecutor) executeReverseEdge(ctx context.Context, _ string, task EnforcementTask) (*EnforcementResult, error) {
	edgeID := task.TargetID

	edge, err := e.edgesRepo.Get(ctx, edgeID)
	if err != nil {
		return nil, fmt.Errorf("failed to load edge: %w", err)
	}

	// Swap source and target
	oldSource := edge.SourceFragmentID
	edge.SourceFragmentID = edge.TargetFragmentID
	edge.TargetFragmentID = oldSource
	edge.Metadata.LastUpdatedAt = time.Now().UTC()

	// Add qualifier to track the fix
	if edge.Qualifiers == nil {
		edge.Qualifiers = make(map[string]interface{})
	}
	edge.Qualifiers["directionFixed"] = true
	edge.Qualifiers["directionFixedAt"] = time.Now().UTC().Format(time.RFC3339)

	if err := e.edgesRepo.Update(ctx, edge); err != nil {
		return nil, fmt.Errorf("failed to update edge: %w", err)
	}

	return &EnforcementResult{
		Success:     true,
		Action:      "edge_reversed",
		AffectedIDs: []string{edgeID},
		Details:     fmt.Sprintf("Reversed edge direction: %s <-> %s", edge.SourceFragmentID, edge.TargetFragmentID),
	}, nil
}

// executeCanonicalizeEdge canonicalizes an edge type.
func (e *EnforcementExecutor) executeCanonicalizeEdge(ctx context.Context, _ string, task EnforcementTask) (*EnforcementResult, error) {
	edgeID := task.TargetID
	newTypeStr, ok := task.Params["newType"].(string)
	if !ok || newTypeStr == "" {
		return nil, fmt.Errorf("newType not specified")
	}
	shouldSwap, _ := task.Params["shouldSwap"].(bool)

	edge, err := e.edgesRepo.Get(ctx, edgeID)
	if err != nil {
		return nil, fmt.Errorf("failed to load edge: %w", err)
	}

	oldType := edge.Type
	edge.Type = horizonmodels.DependencyType(newTypeStr)

	if shouldSwap {
		oldSource := edge.SourceFragmentID
		edge.SourceFragmentID = edge.TargetFragmentID
		edge.TargetFragmentID = oldSource
	}

	// Apply symmetric normalization if needed
	edge.SourceFragmentID, edge.TargetFragmentID, edge.Type, _ = horizonmodels.NormalizeSymmetricEdge(
		edge.SourceFragmentID, edge.TargetFragmentID, edge.Type)

	edge.Metadata.LastUpdatedAt = time.Now().UTC()

	// Add qualifiers
	if edge.Qualifiers == nil {
		edge.Qualifiers = make(map[string]interface{})
	}
	edge.Qualifiers["canonicalized"] = true
	edge.Qualifiers["canonicalizedFrom"] = string(oldType)
	edge.Qualifiers["canonicalizedAt"] = time.Now().UTC().Format(time.RFC3339)

	if err := e.edgesRepo.Update(ctx, edge); err != nil {
		return nil, fmt.Errorf("failed to update edge: %w", err)
	}

	return &EnforcementResult{
		Success:     true,
		Action:      "edge_canonicalized",
		AffectedIDs: []string{edgeID},
		Details:     fmt.Sprintf("Changed edge type from %s to %s", oldType, edge.Type),
	}, nil
}

// executeDeleteEdge deletes an edge.
func (e *EnforcementExecutor) executeDeleteEdge(ctx context.Context, _ string, task EnforcementTask) (*EnforcementResult, error) {
	edgeID := task.TargetID
	reason, _ := task.Params["reason"].(string)

	if err := e.edgesRepo.Delete(ctx, edgeID); err != nil {
		return nil, fmt.Errorf("failed to delete edge: %w", err)
	}

	return &EnforcementResult{
		Success:     true,
		Action:      "edge_deleted",
		AffectedIDs: []string{edgeID},
		Details:     fmt.Sprintf("Deleted edge (reason: %s)", reason),
	}, nil
}

// executeDeleteFragment deletes a fragment.
func (e *EnforcementExecutor) executeDeleteFragment(ctx context.Context, task EnforcementTask) (*EnforcementResult, error) {
	fragID := task.TargetID

	if err := e.fragmentsRepo.Delete(ctx, fragID); err != nil {
		return nil, fmt.Errorf("failed to delete fragment: %w", err)
	}

	return &EnforcementResult{
		Success:     true,
		Action:      "fragment_deleted",
		AffectedIDs: []string{fragID},
	}, nil
}

// executeResolveGeoNames resolves a GeoNames-based path.
func (e *EnforcementExecutor) executeResolveGeoNames(ctx context.Context, task EnforcementTask) (*EnforcementResult, error) {
	fragID := task.TargetID
	correctPath, ok := task.Params["correctPath"].(string)
	if !ok || correctPath == "" {
		// Try to resolve via GeoNames
		if e.geonamesClient == nil {
			return nil, fmt.Errorf("GeoNames client not configured and correctPath not provided")
		}

		currentPath, _ := task.Params["currentPath"].(string)
		if currentPath == "" {
			return nil, fmt.Errorf("currentPath not specified")
		}

		// Parse current path and resolve (city-level only).
		cc, level1, level2, ok := taxonomy.ParseLocCanonicalPath(currentPath)
		if !ok {
			return nil, fmt.Errorf("invalid geo path format: %s", currentPath)
		}
		city := level2
		if city == "" {
			city = level1
		}
		if city == "" {
			return nil, fmt.Errorf("geo path %s has no resolvable city token", currentPath)
		}

		// Search GeoNames
		resp, err := e.geonamesClient.Search(ctx, city, 5, ontology.GeoNamesSearchOptions{
			Country:      cc,
			FeatureClass: "P",
		})
		if err != nil || resp == nil || len(resp.Results) == 0 {
			return nil, fmt.Errorf("GeoNames resolution failed for %s", city)
		}

		// Find best match
		for _, place := range resp.Results {
			if strings.EqualFold(place.CountryCode, cc) {
				adminCode := strings.ToLower(strings.TrimSpace(place.AdminCode1))
				citySlug := slugifyName(place.Name)
				correctPath = taxonomy.GeoCanonicalPath(strings.ToLower(cc), adminCode, citySlug)
				break
			}
		}

		if correctPath == "" {
			return nil, fmt.Errorf("could not resolve GeoNames path for %s", currentPath)
		}
	}

	// Now fix the path
	return e.executeFixPath(ctx, EnforcementTask{
		Type:     TaskFixPath,
		TargetID: fragID,
		Params: map[string]interface{}{
			"correctPath": correctPath,
		},
	})
}

// executeNormalizeSymmetric normalizes a symmetric edge.
func (e *EnforcementExecutor) executeNormalizeSymmetric(ctx context.Context, _ string, task EnforcementTask) (*EnforcementResult, error) {
	edgeID := task.TargetID

	edge, err := e.edgesRepo.Get(ctx, edgeID)
	if err != nil {
		return nil, fmt.Errorf("failed to load edge: %w", err)
	}

	normalizedSource, normalizedTarget, _, swapped := horizonmodels.NormalizeSymmetricEdge(
		edge.SourceFragmentID, edge.TargetFragmentID, edge.Type)

	if !swapped {
		return &EnforcementResult{
			Success:     true,
			Action:      "no_change_needed",
			AffectedIDs: []string{edgeID},
			Details:     "Edge already normalized",
		}, nil
	}

	edge.SourceFragmentID = normalizedSource
	edge.TargetFragmentID = normalizedTarget
	edge.Metadata.LastUpdatedAt = time.Now().UTC()

	if edge.Qualifiers == nil {
		edge.Qualifiers = make(map[string]interface{})
	}
	edge.Qualifiers["symmetricNormalized"] = true

	if err := e.edgesRepo.Update(ctx, edge); err != nil {
		return nil, fmt.Errorf("failed to update edge: %w", err)
	}

	return &EnforcementResult{
		Success:     true,
		Action:      "symmetric_normalized",
		AffectedIDs: []string{edgeID},
		Details:     fmt.Sprintf("Normalized symmetric edge: %s -> %s", normalizedSource, normalizedTarget),
	}, nil
}

// executeFixISO2 fixes an invalid ISO2 code in org path.
func (e *EnforcementExecutor) executeFixISO2(ctx context.Context, task EnforcementTask) (*EnforcementResult, error) {
	const minISO2Confidence = 0.7
	if e.aiExecutor == nil {
		return &EnforcementResult{
			Success: false,
			Action:  "fix_iso2",
			Error:   "AI executor not configured - cannot infer ISO2 automatically",
		}, nil
	}

	fragID := task.TargetID
	frag, err := e.fragmentsRepo.Get(ctx, fragID)
	if err != nil {
		return nil, fmt.Errorf("failed to load fragment: %w", err)
	}
	if frag == nil {
		return nil, fmt.Errorf("fragment not found: %s", fragID)
	}

	oldPath := strings.TrimSpace(frag.CanonicalPath)
	tenantID := strings.TrimSpace(frag.TenantID)

	// Determine namespace (org/gov) and current geo token.
	namespace := ""
	if ns, ok := task.Params["namespace"].(string); ok {
		namespace = strings.ToLower(strings.TrimSpace(ns))
	}
	parts, perr := taxonomy.ParseCanonicalPathParts(oldPath)
	if perr != nil || parts == nil {
		// Fail-open: try using CanonicalParts if it exists.
		parts = frag.CanonicalParts
	}
	if parts == nil || strings.TrimSpace(parts.Namespace) == "" {
		return &EnforcementResult{Success: false, Action: "fix_iso2", Error: "cannot parse canonical path for ISO2 fix"}, nil
	}
	if namespace == "" {
		namespace = strings.ToLower(strings.TrimSpace(parts.Namespace))
	}
	if namespace != "org" && namespace != "gov" {
		return &EnforcementResult{Success: false, Action: "fix_iso2", Error: fmt.Sprintf("unsupported namespace for ISO2 fix: %s", namespace)}, nil
	}

	currentGeo := ""
	switch namespace {
	case "org":
		if parts.Org == nil {
			return &EnforcementResult{Success: false, Action: "fix_iso2", Error: "org canonical parts are missing"}, nil
		}
		currentGeo = strings.ToLower(strings.TrimSpace(parts.Org.Geo))
	case "gov":
		if parts.Gov != nil {
			currentGeo = strings.ToLower(strings.TrimSpace(parts.Gov.Geo))
		}
	}
	if currentGeo != "na" {
		return &EnforcementResult{
			Success: true,
			Action:  "no_change_needed",
			Details: fmt.Sprintf("Geo token is not 'na' (current=%s); skipping ISO2 inference", currentGeo),
		}, nil
	}

	// Build context from fragment fields.
	labelEN := ""
	if frag.Labels != nil {
		labelEN = strings.TrimSpace(frag.Labels["en"])
	}
	legalName := ""
	shortName := ""
	if frag.Identifiers != nil {
		legalName = strings.TrimSpace(frag.Identifiers["legalName"])
		shortName = strings.TrimSpace(frag.Identifiers["shortName"])
	}
	desc := strings.TrimSpace(frag.Description)
	if len(desc) > 1200 {
		desc = desc[:1200] + "…"
	}

	homeISO2 := ""
	if frag.HomeLocation != nil && frag.HomeLocation.ISO3166_1 != nil {
		homeISO2 = strings.ToLower(strings.TrimSpace(*frag.HomeLocation.ISO3166_1))
	}

	// Gather nearby GEO evidence via edges (best-effort).
	// We intentionally keep it bounded to avoid huge prompts and to keep this fix cheap.
	type geoHint struct {
		EdgeType  string `json:"edgeType"`
		OtherID   string `json:"otherId"`
		OtherPath string `json:"otherPath"`
	}
	geoHints := make([]geoHint, 0, 12)
	if e.edgesRepo != nil {
		srcEdges, _ := e.edgesRepo.ListBySource(ctx, tenantID, frag.ID, nil, 200)
		tgtEdges, _ := e.edgesRepo.ListByTarget(ctx, tenantID, frag.ID, nil, 200)
		edges := make([]*horizonmodels.DependencyEdge, 0, len(srcEdges)+len(tgtEdges))
		edges = append(edges, srcEdges...)
		edges = append(edges, tgtEdges...)

		// Keep only geo-relevant edge types.
		for _, ed := range edges {
			if ed == nil {
				continue
			}
			et := strings.ToUpper(strings.TrimSpace(string(ed.Type)))
			if et != string(horizonmodels.DependencyTypeLocatedIn) &&
				et != string(horizonmodels.DependencyTypeOperatesIn) &&
				et != string(horizonmodels.DependencyTypeAdministers) {
				continue
			}
			otherID := ""
			if ed.SourceFragmentID == frag.ID {
				otherID = ed.TargetFragmentID
			} else if ed.TargetFragmentID == frag.ID {
				otherID = ed.SourceFragmentID
			}
			otherID = strings.TrimSpace(otherID)
			if otherID == "" || otherID == frag.ID {
				continue
			}
			otherFrag, _ := e.fragmentsRepo.Get(ctx, otherID)
			otherPath := ""
			if otherFrag != nil {
				otherPath = strings.TrimSpace(otherFrag.CanonicalPath)
			}
			if otherPath == "" {
				continue
			}
			geoHints = append(geoHints, geoHint{EdgeType: et, OtherID: otherID, OtherPath: otherPath})
			if len(geoHints) >= 10 {
				break
			}
		}
	}

	// LLM task: infer ISO2/composite geo token.
	type iso2FixResult struct {
		ISO2       string   `json:"iso2"`
		Confidence float64  `json:"confidence"`
		Reason     string   `json:"reason"`
		Evidence   []string `json:"evidence,omitempty"`
	}
	isISO2OrComposite := func(s string) bool {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" || s == "na" {
			return false
		}
		// alpha2
		if len(s) == 2 {
			return true
		}
		// composite like "ae-sa-om"
		if strings.Contains(s, "-") {
			segs := strings.Split(s, "-")
			if len(segs) < 2 {
				return false
			}
			for _, seg := range segs {
				seg = strings.TrimSpace(seg)
				if len(seg) != 2 {
					return false
				}
			}
			return true
		}
		return false
	}

	verifier := ai.NewGenericVerifier(func(r iso2FixResult) error {
		if r.Confidence < 0 || r.Confidence > 1 {
			return fmt.Errorf("confidence must be 0..1")
		}
		r.ISO2 = strings.ToLower(strings.TrimSpace(r.ISO2))
		if !isISO2OrComposite(r.ISO2) {
			return fmt.Errorf("iso2 must be ISO3166-1 alpha2 or %s (got %q)", taxonomy.CompositeGeoDescriptionForPrompts(), r.ISO2)
		}
		return nil
	})

	// Optional context from scout job (original query, domain path) — helps LLM infer geo from user intent.
	originalQuery := ""
	targetCanonicalPath := ""
	domainPath := ""
	if task.Params != nil {
		if s, ok := task.Params["originalQuery"].(string); ok {
			originalQuery = strings.TrimSpace(s)
		}
		if s, ok := task.Params["targetCanonicalPath"].(string); ok {
			targetCanonicalPath = strings.TrimSpace(s)
		}
		if s, ok := task.Params["domainPath"].(string); ok {
			domainPath = strings.TrimSpace(s)
		}
	}

	contextBlock := ""
	if originalQuery != "" || targetCanonicalPath != "" || domainPath != "" {
		contextBlock = "\nScout job context (use to infer jurisdiction when fragment lacks geo hints):\n"
		if originalQuery != "" {
			contextBlock += fmt.Sprintf("- originalQuery: %s\n", originalQuery)
		}
		if targetCanonicalPath != "" {
			contextBlock += fmt.Sprintf("- targetCanonicalPath: %s\n", targetCanonicalPath)
		}
		if domainPath != "" {
			contextBlock += fmt.Sprintf("- domainPath: %s\n", domainPath)
		}
		contextBlock += "E.g. query \"soft drinks South Africa\" or domain /domain/.../geo/za/... strongly suggests iso2=za.\n"
	}

	prompt := strings.TrimSpace(fmt.Sprintf(`
You are fixing a Horizon canonical path geo token.

Context:
- Canonical path format:
  - org: /org/{iso2}/{slug}
  - gov: /gov/{iso2_or_composite}/{slug}
- The geo token "na" means "unknown jurisdiction". It is allowed, but we want to infer a better ISO token when possible.

Task:
- Infer the correct geo token for this fragment.
- Return STRICT JSON only:
  {"iso2": "<lowercase iso3166-1 alpha2 like 'kr' OR " + taxonomy.CompositeGeoDescriptionForPrompts() + ">", "confidence": 0..1, "reason": "...", "evidence": ["..."] }
- If you are not confident, still output a best guess but lower confidence (<0.7). It will NOT be applied automatically.
- NEVER output "na".
%s
Fragment:
- namespace: %s
- currentCanonicalPath: %s
- label_en: %s
- legalName: %s
- shortName: %s
- description: %s
- homeLocation.iso3166_1: %s

Nearby edges that may hint location/jurisdiction (best-effort):
%v
`, contextBlock, namespace, oldPath, labelEN, legalName, shortName, desc, homeISO2, geoHints))

	llmTask := &ai.AITask{
		TaskType:     ai.TaskTypeContentAnalysis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       6,
			MinContextWindow:         8000,
			MaxOutputSize:            4096,
		},
		Verifier: verifier,
	}

	// Detach from any short parent deadline (this task is async) and enforce our own timeout.
	llmCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 90*time.Second)
	defer cancel()

	raw, err := e.aiExecutor.ExecuteStructuredTask(llmCtx, llmTask)
	if err != nil {
		return &EnforcementResult{
			Success: false,
			Action:  "fix_iso2",
			Error:   fmt.Sprintf("LLM iso2 inference failed: %v", err),
		}, nil
	}
	res, ok := raw.(iso2FixResult)
	if !ok {
		// Best-effort: convert from map when executor returns an untyped payload.
		if m, ok2 := raw.(map[string]interface{}); ok2 {
			res = iso2FixResult{
				ISO2:       getString(m, "iso2"),
				Confidence: getFloat(m, "confidence"),
				Reason:     getString(m, "reason"),
			}
		} else {
			return &EnforcementResult{
				Success: false,
				Action:  "fix_iso2",
				Error:   fmt.Sprintf("LLM iso2 inference returned unexpected type %T", raw),
			}, nil
		}
	}
	// Re-validate post-conversion (keeps behavior consistent with typed path).
	if err := verifier.ValidateFunc(res); err != nil {
		return &EnforcementResult{
			Success: false,
			Action:  "fix_iso2",
			Error:   fmt.Sprintf("LLM iso2 inference parse/validate failed: %v", err),
		}, nil
	}
	if res.Confidence < minISO2Confidence {
		return &EnforcementResult{
			Success: false,
			Action:  "fix_iso2",
			Error:   fmt.Sprintf("low_confidence_iso2_inference (conf=%.2f < %.2f): %s", res.Confidence, minISO2Confidence, strings.TrimSpace(res.Reason)),
		}, nil
	}

	newGeo := strings.ToLower(strings.TrimSpace(res.ISO2))
	if norm := normalizeISO2Alias(newGeo); norm != "" {
		newGeo = norm
	}
	if newGeo == "na" || newGeo == "" {
		return &EnforcementResult{Success: false, Action: "fix_iso2", Error: "LLM returned empty/na iso2"}, nil
	}

	// Apply selectively to CanonicalParts (only the geo segment).
	if frag.CanonicalParts == nil {
		frag.CanonicalParts = parts
	}
	switch namespace {
	case "org":
		if frag.CanonicalParts.Org == nil {
			frag.CanonicalParts.Org = &taxonomy.OrgPathParts{}
		}
		frag.CanonicalParts.Org.Geo = newGeo
	case "gov":
		if frag.CanonicalParts.Gov == nil {
			frag.CanonicalParts.Gov = &taxonomy.GovPathParts{}
		}
		frag.CanonicalParts.Gov.Geo = newGeo
	}

	// Optional: stash inferred geo in identifiers for traceability (non-blocking).
	if frag.Identifiers == nil {
		frag.Identifiers = map[string]string{}
	}
	if strings.TrimSpace(frag.Identifiers["jurisdictionIso2"]) == "" {
		frag.Identifiers["jurisdictionIso2"] = newGeo
	}
	frag.Identifiers["jurisdictionIso2InferredBy"] = "policy_fix_iso2_llm"

	frag.UpdatedAt = time.Now().UTC()
	if err := e.fragmentsRepo.Update(ctx, frag); err != nil {
		return nil, fmt.Errorf("failed to update fragment: %w", err)
	}

	// Best-effort: rewrite lens scopes referencing the old path to the new path (post-Update canonicalPath).
	newPath := strings.TrimSpace(frag.CanonicalPath)
	if newPath != "" && oldPath != "" && newPath != oldPath {
		e.bestEffortRewriteLensScopes(ctx, tenantID, oldPath, newPath)
	}

	// Deterministic: merge on canonicalPath collision to avoid duplicate nodes by different IDs.
	merged, mergeCanonicalID, mergeDuplicateID := e.bestEffortMergeCanonicalPathCollision(ctx, frag.ID, newPath)

	return &EnforcementResult{
		Success:     true,
		Action:      "fix_iso2",
		AffectedIDs: []string{fragID},
		Details: func() string {
			base := fmt.Sprintf("Inferred geo token %q -> %q (conf=%.2f). %s", currentGeo, newGeo, res.Confidence, strings.TrimSpace(res.Reason))
			if merged && strings.TrimSpace(mergeCanonicalID) != "" && strings.TrimSpace(mergeDuplicateID) != "" {
				base += fmt.Sprintf(" CanonicalPath collision detected; merged %s into %s.", mergeDuplicateID, mergeCanonicalID)
			}
			return base
		}(),
	}, nil
}

// bestEffortMergeCanonicalPathCollision checks if another fragment already has the same canonicalPath.
// If so, it merges the "duplicate" into the "canonical" immediately so edges converge by fragment ID.
//
// This is intentionally conservative and fail-open:
// - If we cannot look up by canonicalPath, or merge fails, we just skip.
// - We choose canonical deterministically (earlier CreatedAt; tie-break by lexicographic ID).
func (e *EnforcementExecutor) bestEffortMergeCanonicalPathCollision(ctx context.Context, updatedFragmentID, canonicalPath string) (merged bool, canonicalID string, duplicateID string) {
	if e == nil || e.fragmentsRepo == nil {
		return false, "", ""
	}
	updatedFragmentID = strings.TrimSpace(updatedFragmentID)
	canonicalPath = strings.TrimSpace(canonicalPath)
	if updatedFragmentID == "" || canonicalPath == "" {
		return false, "", ""
	}
	other, err := e.fragmentsRepo.GetByCanonicalPath(ctx, canonicalPath)
	if err != nil || other == nil {
		return false, "", ""
	}
	if strings.TrimSpace(other.ID) == "" || strings.TrimSpace(other.ID) == updatedFragmentID {
		return false, "", ""
	}

	// Load the updated fragment so we can deterministically select canonical vs duplicate.
	thisFrag, err := e.fragmentsRepo.Get(ctx, updatedFragmentID)
	if err != nil || thisFrag == nil {
		// If we can't load, keep existing "other" as canonical and attempt to merge updated into it.
		canonicalID = strings.TrimSpace(other.ID)
		duplicateID = updatedFragmentID
	} else {
		// Deterministic selection: keep earlier CreatedAt; tie-break by lexicographic ID.
		a := thisFrag
		b := other
		keepA := false
		if a.CreatedAt.Before(b.CreatedAt) {
			keepA = true
		} else if a.CreatedAt.Equal(b.CreatedAt) {
			keepA = strings.TrimSpace(a.ID) < strings.TrimSpace(b.ID)
		}
		if keepA {
			canonicalID = strings.TrimSpace(a.ID)
			duplicateID = strings.TrimSpace(b.ID)
		} else {
			canonicalID = strings.TrimSpace(b.ID)
			duplicateID = strings.TrimSpace(a.ID)
		}
	}
	if canonicalID == "" || duplicateID == "" || canonicalID == duplicateID {
		return false, "", ""
	}

	// Execute merge immediately (best-effort). This will reassign edges and delete the duplicate fragment.
	if _, err := e.executeMergeFragments(ctx, &PolicyEnforcementJob{
		Task: EnforcementTask{
			Type:     TaskMergeFragments,
			TargetID: duplicateID,
			Params: map[string]interface{}{
				"canonicalId": canonicalID,
			},
		},
	}); err != nil {
		log.Printf("[EnforcementExecutor] WARNING: canonicalPath collision merge failed: path=%q canonical=%q duplicate=%q err=%v",
			canonicalPath, canonicalID, duplicateID, err)
		return false, "", ""
	}

	return true, canonicalID, duplicateID
}

// ExecuteBatch executes all pending enforcement jobs for a parent job.
func (e *EnforcementExecutor) ExecuteBatch(ctx context.Context, parentJobID string) (*EnforcementCompleteMessage, error) {
	if e.enforcementRepo == nil {
		return nil, fmt.Errorf("enforcement repo not configured")
	}

	// Get all jobs for this parent
	jobs, err := e.enforcementRepo.ListByParentJob(ctx, parentJobID, 1000)
	if err != nil {
		return nil, fmt.Errorf("failed to list jobs: %w", err)
	}

	result := &EnforcementCompleteMessage{
		Type:        "enforcement_batch_complete",
		ParentJobID: parentJobID,
		TotalJobs:   len(jobs),
	}

	for _, job := range jobs {
		if job.Status != EnforcementStatusPending {
			// Count non-pending jobs
			switch job.Status {
			case EnforcementStatusCompleted:
				result.Completed++
			case EnforcementStatusFailed:
				result.Failed++
			case EnforcementStatusSkipped:
				result.Skipped++
			}
			continue
		}

		// Execute the job
		execResult, err := e.ExecuteJob(ctx, job)
		if err != nil {
			log.Printf("[EnforcementExecutor] Job %s failed: %v", job.ID, err)
			result.Failed++
		} else if execResult.Success {
			result.Completed++
		} else {
			result.Failed++
		}
	}

	// Publish batch completion
	if e.publisher != nil {
		_ = e.publisher.PublishEnforcementComplete(ctx, *result)
	}

	return result, nil
}

// CreateEnforcementJobs creates enforcement jobs from policy check violations.
func (e *EnforcementExecutor) CreateEnforcementJobs(
	ctx context.Context,
	tenantID string,
	parentJobID string,
	violations []PolicyViolation,
) ([]*PolicyEnforcementJob, error) {
	jobs := make([]*PolicyEnforcementJob, 0)
	tenantID = strings.TrimSpace(tenantID)
	parentJobID = strings.TrimSpace(parentJobID)

	// Best-effort: avoid creating duplicate jobs within the same parent job.
	// Also avoid re-fixing an already-fixed fragment (idempotency).
	type existingStatus struct {
		status  EnforcementJobStatus
		success bool
	}
	existing := map[string]existingStatus{}
	type existingMergePairState struct {
		hasCompletedSuccess bool
		hasPendingWork      bool
	}
	existingMergePairs := map[string]existingMergePairState{}
	if e.enforcementRepo != nil && parentJobID != "" {
		if prev, err := e.enforcementRepo.ListByParentJob(ctx, parentJobID, 2000); err == nil {
			for _, pj := range prev {
				if pj == nil {
					continue
				}
				k := enforcementTaskKey(pj.Task)
				if k == "" {
					continue
				}
				st := existingStatus{status: pj.Status}
				if pj.Result != nil {
					st.success = pj.Result.Success
				}
				existing[k] = st
				if mpk := mergeTaskPairKey(pj.Task); mpk != "" {
					pst := existingMergePairs[mpk]
					if pj.Status == EnforcementStatusPending || pj.Status == EnforcementStatusInProgress {
						pst.hasPendingWork = true
					}
					if pj.Status == EnforcementStatusCompleted && pj.Result != nil && pj.Result.Success {
						pst.hasCompletedSuccess = true
					}
					existingMergePairs[mpk] = pst
				}
			}
		}
	}
	seenInBatch := map[string]struct{}{}
	seenMergePairs := map[string]struct{}{}

	for _, v := range violations {
		task, ok := e.resolveEnforcementTaskForViolation(v)
		if !ok {
			continue
		}
		mergePairKey := mergeTaskPairKey(task)
		if mergePairKey != "" {
			if _, ok := seenMergePairs[mergePairKey]; ok {
				continue
			}
			if pst, ok := existingMergePairs[mergePairKey]; ok {
				if pst.hasCompletedSuccess || pst.hasPendingWork {
					continue
				}
			}
		}

		key := enforcementTaskKey(task)
		if key != "" {
			if _, ok := seenInBatch[key]; ok {
				continue
			}
			// If a successful identical task already ran for this parent job, skip.
			if st, ok := existing[key]; ok {
				if st.success && st.status == EnforcementStatusCompleted {
					continue
				}
				// Also skip if already pending/in_progress to avoid duplicate concurrent work.
				if st.status == EnforcementStatusPending || st.status == EnforcementStatusInProgress {
					continue
				}
			}
			seenInBatch[key] = struct{}{}
		}
		if mergePairKey != "" {
			seenMergePairs[mergePairKey] = struct{}{}
		}

		job := &PolicyEnforcementJob{
			ID:          uuid.New().String(),
			TenantID:    tenantID,
			ParentJobID: parentJobID,
			ViolationID: v.ID,
			Violation:   &v,
			Task:        task,
			Status:      EnforcementStatusPending,
			CreatedAt:   time.Now().UTC(),
			UpdatedAt:   time.Now().UTC(),
		}
		jobs = append(jobs, job)
	}

	if e.enforcementRepo != nil && len(jobs) > 0 {
		if err := e.enforcementRepo.CreateBatch(ctx, jobs); err != nil {
			return nil, fmt.Errorf("failed to create enforcement jobs: %w", err)
		}
	}

	return jobs, nil
}

func enforcementTaskKey(t EnforcementTask) string {
	base := strings.TrimSpace(string(t.Type)) + ":" + strings.TrimSpace(t.TargetID)
	if strings.TrimSpace(string(t.Type)) == "" || strings.TrimSpace(t.TargetID) == "" {
		return ""
	}
	// Include key params to avoid collapsing distinct fixes of different kinds.
	switch t.Type {
	case TaskMergeFragments:
		canonicalID, _ := t.Params["canonicalId"].(string)
		return base + ":" + strings.TrimSpace(canonicalID)
	case TaskFixPath:
		cp, _ := t.Params["correctPath"].(string)
		reason, _ := t.Params["reason"].(string)
		return base + ":" + strings.TrimSpace(cp) + ":" + strings.TrimSpace(reason)
	case TaskFixISO2:
		ns, _ := t.Params["namespace"].(string)
		return base + ":" + strings.TrimSpace(ns)
	case TaskCanonicalizeEdge:
		nt, _ := t.Params["newType"].(string)
		return base + ":" + strings.TrimSpace(nt)
	case TaskEnrichEdge:
		rule, _ := t.Params["rule"].(string)
		return base + ":" + strings.TrimSpace(rule)
	default:
		return base
	}
}

func mergeTaskPairKey(t EnforcementTask) string {
	if t.Type != TaskMergeFragments {
		return ""
	}
	duplicateID := strings.TrimSpace(t.TargetID)
	canonicalID, _ := t.Params["canonicalId"].(string)
	canonicalID = strings.TrimSpace(canonicalID)
	if duplicateID == "" || canonicalID == "" {
		return ""
	}
	if duplicateID <= canonicalID {
		return duplicateID + "|" + canonicalID
	}
	return canonicalID + "|" + duplicateID
}

func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	if grpcStatus.Code(err) == grpcCodes.NotFound {
		return true
	}
	low := strings.ToLower(err.Error())
	return strings.Contains(low, "not found") || strings.Contains(low, "notfound")
}

func (e *EnforcementExecutor) resolveEnforcementTaskForViolation(v PolicyViolation) (EnforcementTask, bool) {
	if v.AutoFixable && v.SuggestedFix != nil {
		return *v.SuggestedFix, true
	}
	if strings.TrimSpace(v.EdgeID) != "" && isEdgeCompletenessRule(v.Rule) {
		return EnforcementTask{
			Type:     TaskEnrichEdge,
			TargetID: strings.TrimSpace(v.EdgeID),
			Params: map[string]interface{}{
				"rule":          v.Rule,
				"description":   v.Description,
				"expectedValue": v.ExpectedValue,
			},
		}, true
	}
	return EnforcementTask{}, false
}

func isEdgeCompletenessRule(rule string) bool {
	r := strings.TrimSpace(strings.ToLower(rule))
	return strings.HasPrefix(r, "edge_completeness_") || strings.HasPrefix(r, "edge_temporal_")
}

func (e *EnforcementExecutor) executeEnrichEdge(ctx context.Context, task EnforcementTask) (*EnforcementResult, error) {
	if e.edgesRepo == nil {
		return nil, fmt.Errorf("edges repo is not configured")
	}
	edgeID := strings.TrimSpace(task.TargetID)
	if edgeID == "" {
		return nil, fmt.Errorf("target edge id is required")
	}
	edge, err := e.edgesRepo.Get(ctx, edgeID)
	if err != nil {
		return nil, fmt.Errorf("failed to load edge: %w", err)
	}
	if edge == nil {
		return nil, fmt.Errorf("edge not found: %s", edgeID)
	}
	if edge.Qualifiers == nil {
		edge.Qualifiers = map[string]interface{}{}
	}

	hints := []string{}
	if desc, _ := task.Params["description"].(string); strings.TrimSpace(desc) != "" {
		hints = append(hints, strings.TrimSpace(desc))
	}
	if exp, _ := task.Params["expectedValue"].(string); strings.TrimSpace(exp) != "" {
		hints = append(hints, "Missing: "+strings.TrimSpace(exp))
	}
	if rule, _ := task.Params["rule"].(string); strings.TrimSpace(rule) != "" {
		edge.Qualifiers["policyEnrichmentRule"] = strings.TrimSpace(rule)
	}
	for _, h := range hints {
		appendUniqueStringQualifier(edge.Qualifiers, "enrichmentHints", h)
	}
	appendUniqueStringQualifier(edge.Qualifiers, "enrichmentHints", "Best-effort enrichment requested by policy enforcement.")
	edge.Qualifiers["needsEnrichment"] = true

	if err := e.edgesRepo.Update(ctx, edge); err != nil {
		return nil, fmt.Errorf("failed to update edge with enrichment hints: %w", err)
	}

	return &EnforcementResult{
		Success:     true,
		Action:      "edge_enrichment_marked",
		AffectedIDs: []string{edge.ID},
		Details:     "Marked edge for best-effort enrichment and attached policy hints",
	}, nil
}

func appendUniqueStringQualifier(qualifiers map[string]interface{}, key, val string) {
	val = strings.TrimSpace(val)
	if qualifiers == nil || key == "" || val == "" {
		return
	}
	raw, ok := qualifiers[key]
	if !ok {
		qualifiers[key] = []string{val}
		return
	}
	switch v := raw.(type) {
	case []string:
		for _, s := range v {
			if strings.TrimSpace(s) == val {
				return
			}
		}
		qualifiers[key] = append(v, val)
	case []interface{}:
		out := make([]string, 0, len(v)+1)
		seen := map[string]struct{}{}
		for _, item := range v {
			s := strings.TrimSpace(fmt.Sprint(item))
			if s == "" {
				continue
			}
			if _, exists := seen[s]; exists {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
		if _, exists := seen[val]; !exists {
			out = append(out, val)
		}
		qualifiers[key] = out
	default:
		s := strings.TrimSpace(fmt.Sprint(v))
		if s == "" || s == val {
			qualifiers[key] = []string{val}
		} else {
			qualifiers[key] = []string{s, val}
		}
	}
}

// Helper functions

func slugifyName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return s
}
