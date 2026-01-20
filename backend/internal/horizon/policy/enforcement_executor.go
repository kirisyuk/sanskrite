package policy

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

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

	log.Printf("[EnforcementExecutor] Executing job %s: type=%s target=%s",
		job.ID, job.Task.Type, job.Task.TargetID)

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
		result, err = e.executeMergeFragments(ctx, job.Task)
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
	case TaskFixLegalForm:
		result, err = e.executeFixLegalForm(ctx, job.Task)
	case TaskFixISO2:
		result, err = e.executeFixISO2(ctx, job.Task)
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

	return result, nil
}

// executeMergeFragments merges a duplicate fragment into the canonical one.
func (e *EnforcementExecutor) executeMergeFragments(ctx context.Context, task EnforcementTask) (*EnforcementResult, error) {
	duplicateID := task.TargetID
	canonicalID, ok := task.Params["canonicalId"].(string)
	if !ok || canonicalID == "" {
		return nil, fmt.Errorf("canonicalId not specified")
	}

	// Load both fragments
	duplicate, err := e.fragmentsRepo.Get(ctx, duplicateID)
	if err != nil {
		return nil, fmt.Errorf("failed to load duplicate fragment: %w", err)
	}
	canonical, err := e.fragmentsRepo.Get(ctx, canonicalID)
	if err != nil {
		return nil, fmt.Errorf("failed to load canonical fragment: %w", err)
	}

	// Merge data from duplicate into canonical
	merged := mergeFragmentData(canonical, duplicate)

	// Update canonical
	if merged {
		canonical.UpdatedAt = time.Now().UTC()
		if err := e.fragmentsRepo.Update(ctx, canonical); err != nil {
			return nil, fmt.Errorf("failed to update canonical: %w", err)
		}
	}

	// Reassign edges from duplicate to canonical
	reassigned, deleted, err := e.reassignEdges(ctx, duplicate.TenantID, duplicateID, canonicalID)
	if err != nil {
		log.Printf("[EnforcementExecutor] WARNING: edge reassignment partial failure: %v", err)
	}

	// Delete duplicate
	if err := e.fragmentsRepo.Delete(ctx, duplicateID); err != nil {
		return nil, fmt.Errorf("failed to delete duplicate: %w", err)
	}

	// Best-effort: rewrite lens scopes from duplicate canonicalPath to canonical canonicalPath,
	// otherwise UI may keep referencing deleted aliases.
	if duplicate != nil && canonical != nil {
		e.bestEffortRewriteLensScopes(ctx, strings.TrimSpace(duplicate.TenantID), strings.TrimSpace(duplicate.CanonicalPath), strings.TrimSpace(canonical.CanonicalPath))
	}

	return &EnforcementResult{
		Success:     true,
		Action:      "merged",
		AffectedIDs: []string{canonicalID, duplicateID},
		Details:     fmt.Sprintf("Merged %s into %s. Edges: %d reassigned, %d deleted.", duplicateID, canonicalID, reassigned, deleted),
	}, nil
}

// reassignEdges reassigns edges from one fragment to another.
func (e *EnforcementExecutor) reassignEdges(ctx context.Context, tenantID, fromID, toID string) (reassigned, deleted int, err error) {
	// Get edges where fromID is source
	sourceEdges, err := e.edgesRepo.ListBySource(ctx, tenantID, fromID, nil, 1000)
	if err != nil {
		return 0, 0, err
	}

	// Get edges where fromID is target
	targetEdges, err := e.edgesRepo.ListByTarget(ctx, tenantID, fromID, nil, 1000)
	if err != nil {
		return 0, 0, err
	}

	// Reassign source edges
	for _, edge := range sourceEdges {
		// Check for self-loop after reassignment
		if edge.TargetFragmentID == toID {
			// Would become self-loop, delete instead
			if err := e.edgesRepo.Delete(ctx, edge.ID); err == nil {
				deleted++
			}
			continue
		}

		edge.SourceFragmentID = toID
		edge.Metadata.LastUpdatedAt = time.Now().UTC()
		if err := e.edgesRepo.Update(ctx, edge); err == nil {
			reassigned++
		}
	}

	// Reassign target edges
	for _, edge := range targetEdges {
		// Check for self-loop after reassignment
		if edge.SourceFragmentID == toID {
			// Would become self-loop, delete instead
			if err := e.edgesRepo.Delete(ctx, edge.ID); err == nil {
				deleted++
			}
			continue
		}

		edge.TargetFragmentID = toID
		edge.Metadata.LastUpdatedAt = time.Now().UTC()
		if err := e.edgesRepo.Update(ctx, edge); err == nil {
			reassigned++
		}
	}

	// Best-effort: de-duplicate edges that now may overlap with existing canonical edges.
	// This prevents parallel identical edges after merge (duplicate->canonical reassignment).
	dedupDeleted, _ := e.bestEffortDedupeEdgesForNode(ctx, tenantID, toID, 2000)
	deleted += dedupDeleted

	return reassigned, deleted, nil
}

// bestEffortDedupeEdgesForNode deletes duplicate edges for a fragment based on (source,target,type),
// keeping one "best" edge per key. This is intentionally conservative and bounded.
func (e *EnforcementExecutor) bestEffortDedupeEdgesForNode(ctx context.Context, tenantID, fragmentID string, limit int) (deleted int, _ error) {
	if e == nil || e.edgesRepo == nil {
		return 0, nil
	}
	tenantID = strings.TrimSpace(tenantID)
	fragmentID = strings.TrimSpace(fragmentID)
	if tenantID == "" || fragmentID == "" || limit <= 0 {
		return 0, nil
	}

	// Pull both directions (bounded).
	srcEdges, err := e.edgesRepo.ListBySource(ctx, tenantID, fragmentID, nil, limit)
	if err != nil {
		return 0, err
	}
	tgtEdges, err := e.edgesRepo.ListByTarget(ctx, tenantID, fragmentID, nil, limit)
	if err != nil {
		return 0, err
	}

	// Key on normalized symmetric representation to avoid duplicates across swapped endpoints for symmetric edges.
	type key struct {
		s  string
		t  string
		ty string
	}
	kept := map[key]*horizonmodels.DependencyEdge{}

	consider := func(ed *horizonmodels.DependencyEdge) {
		if ed == nil {
			return
		}
		s := strings.TrimSpace(ed.SourceFragmentID)
		t := strings.TrimSpace(ed.TargetFragmentID)
		ty := ed.Type
		// Normalize symmetric edges for stable keys.
		ns, nt, nty, _ := horizonmodels.NormalizeSymmetricEdge(s, t, ty)
		k := key{s: ns, t: nt, ty: string(nty)}

		if existing, ok := kept[k]; ok && existing != nil {
			// Choose which to keep: prefer earlier CreatedAt; if equal, prefer lexicographically smaller ID.
			keep := existing
			drop := ed
			if ed.Metadata.CreatedAt.Before(existing.Metadata.CreatedAt) ||
				(ed.Metadata.CreatedAt.Equal(existing.Metadata.CreatedAt) && strings.TrimSpace(ed.ID) < strings.TrimSpace(existing.ID)) {
				keep = ed
				drop = existing
				kept[k] = keep
			}
			// Delete the dropped edge (best-effort).
			if drop != nil && strings.TrimSpace(drop.ID) != "" {
				if derr := e.edgesRepo.Delete(ctx, drop.ID); derr == nil {
					deleted++
				}
			}
			return
		}
		kept[k] = ed
	}

	for _, ed := range srcEdges {
		consider(ed)
	}
	for _, ed := range tgtEdges {
		consider(ed)
	}

	return deleted, nil
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

	return &EnforcementResult{
		Success:     true,
		Action:      "path_fixed",
		AffectedIDs: []string{fragID},
		Details:     fmt.Sprintf("Changed path from %s to %s", oldPath, newPath),
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

		// Parse current path and resolve
		parts := strings.Split(strings.Trim(currentPath, "/"), "/")
		if len(parts) != 4 || parts[0] != "loc" {
			return nil, fmt.Errorf("invalid geo path format: %s", currentPath)
		}

		cc := parts[1]
		city := parts[3]

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
				if adminCode == "" {
					adminCode = "na"
				}
				citySlug := slugifyName(place.Name)
				correctPath = fmt.Sprintf("/loc/%s/%s/%s", strings.ToLower(cc), adminCode, citySlug)
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

// executeFixLegalForm fixes a missing legal form in org path.
func (e *EnforcementExecutor) executeFixLegalForm(ctx context.Context, task EnforcementTask) (*EnforcementResult, error) {
	fragID := task.TargetID
	correctPath, ok := task.Params["correctPath"].(string)
	if !ok || correctPath == "" {
		return nil, fmt.Errorf("correctPath not specified")
	}

	return e.executeFixPath(ctx, EnforcementTask{
		Type:     TaskFixPath,
		TargetID: fragID,
		Params: map[string]interface{}{
			"correctPath": correctPath,
		},
	})
}

// executeFixISO2 fixes an invalid ISO2 code in org path.
func (e *EnforcementExecutor) executeFixISO2(ctx context.Context, task EnforcementTask) (*EnforcementResult, error) {
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
		if parts.Org != nil {
			currentGeo = strings.ToLower(strings.TrimSpace(parts.Org.Geo))
		}
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
			return fmt.Errorf("iso2 must be ISO3166-1 alpha2 or composite (got %q)", r.ISO2)
		}
		return nil
	})

	prompt := strings.TrimSpace(fmt.Sprintf(`
You are fixing a Horizon canonical path geo token.

Context:
- Canonical path format:
  - org: /org/{iso2}/{slug}-{legal_form}
  - gov: /gov/{iso2_or_composite}/{slug}
- The geo token "na" means "unknown jurisdiction". It is allowed, but we want to infer a better ISO token when possible.

Task:
- Infer the correct geo token for this fragment.
- Return STRICT JSON only:
  {"iso2": "<lowercase iso3166-1 alpha2 like 'kr' OR a composite like 'ae-sa-om'>", "confidence": 0..1, "reason": "...", "evidence": ["..."] }
- If you are not confident, still output a best guess but lower confidence (<0.7).
- NEVER output "na".

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
`, namespace, oldPath, labelEN, legalName, shortName, desc, homeISO2, geoHints))

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

	llmCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
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

	return &EnforcementResult{
		Success:     true,
		Action:      "fix_iso2",
		AffectedIDs: []string{fragID},
		Details:     fmt.Sprintf("Inferred geo token %q -> %q (conf=%.2f). %s", currentGeo, newGeo, res.Confidence, strings.TrimSpace(res.Reason)),
	}, nil
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

	for _, v := range violations {
		if !v.AutoFixable || v.SuggestedFix == nil {
			continue
		}

		job := &PolicyEnforcementJob{
			ID:          uuid.New().String(),
			TenantID:    tenantID,
			ParentJobID: parentJobID,
			ViolationID: v.ID,
			Violation:   &v,
			Task:        *v.SuggestedFix,
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

// Helper functions

func mergeFragmentData(canonical, duplicate *horizonmodels.HorizonFragment) bool {
	changed := false

	// Merge labels
	if canonical.Labels == nil {
		canonical.Labels = make(map[string]string)
	}
	for k, v := range duplicate.Labels {
		if canonical.Labels[k] == "" && v != "" {
			canonical.Labels[k] = v
			changed = true
		}
	}

	// Merge identifiers
	if canonical.Identifiers == nil {
		canonical.Identifiers = make(map[string]string)
	}
	for k, v := range duplicate.Identifiers {
		if canonical.Identifiers[k] == "" && v != "" {
			canonical.Identifiers[k] = v
			changed = true
		}
	}

	// Merge description
	if canonical.Description == "" && duplicate.Description != "" {
		canonical.Description = duplicate.Description
		changed = true
	}

	// Merge KB refs
	existingRefs := make(map[string]bool)
	for _, ref := range canonical.KBRefs {
		existingRefs[ref.KB+":"+ref.ExternalID] = true
	}
	for _, ref := range duplicate.KBRefs {
		key := ref.KB + ":" + ref.ExternalID
		if !existingRefs[key] {
			canonical.KBRefs = append(canonical.KBRefs, ref)
			changed = true
		}
	}

	// Store merged paths
	if canonical.Identifiers["merged_paths"] == "" {
		canonical.Identifiers["merged_paths"] = duplicate.CanonicalPath
	} else if !strings.Contains(canonical.Identifiers["merged_paths"], duplicate.CanonicalPath) {
		canonical.Identifiers["merged_paths"] += "," + duplicate.CanonicalPath
	}
	changed = true

	return changed
}

func slugifyName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return s
}
