package repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
)

const mergeRedirectStableTTL = 7 * 24 * time.Hour

// Ensure FragmentsMergeRepository implements policy.MergeFragments.
var _ policy.MergeFragments = (*FragmentsMergeRepository)(nil)

// MergeRequest describes a single fragment merge operation.
type MergeRequest struct {
	PrimaryFragmentID   string
	DuplicateFragmentID string
	TenantID            string
	Source              FragmentMergeAuditSource
	ToolName            string
	JobID               string
	LensID              string

	// OnEdgeReassigned is called when an edge is reassigned (e.g. for lens item sync).
	// Optional; may be nil.
	OnEdgeReassigned func(edge *models.DependencyEdge)
}

// MergeResult holds the outcome of a merge.
type MergeResult struct {
	DataMerged       bool
	EdgesReassigned  int
	EdgesDeleted     int
	DuplicateDeleted bool
}

// FragmentsMergeRepository is the single entry point for fragment merge operations.
// It performs: data merge (merged_paths), edge reassign+dedup, duplicate delete, audit.
// When redirectsRepo is set, records canonical path redirects so Resolve() returns the final path after merges.
type FragmentsMergeRepository struct {
	fragmentsRepo   *HorizonFragmentsRepository
	edgesRepo       *DependencyEdgesRepository
	mergeAuditRepo  *FragmentMergeAuditRepository
	redirectsRepo   *CanonicalPathRedirectsRepository
}

// NewFragmentsMergeRepository creates a new merge repository.
func NewFragmentsMergeRepository(
	fragmentsRepo *HorizonFragmentsRepository,
	edgesRepo *DependencyEdgesRepository,
	mergeAuditRepo *FragmentMergeAuditRepository,
) *FragmentsMergeRepository {
	return &FragmentsMergeRepository{
		fragmentsRepo:  fragmentsRepo,
		edgesRepo:     edgesRepo,
		mergeAuditRepo: mergeAuditRepo,
	}
}

// WithRedirectsRepo wires the redirects repository. When set, merge records redirects
// (duplicate path -> primary path, and all merged_paths -> primary) so canonical path
// resolution returns the final surviving path.
func (r *FragmentsMergeRepository) WithRedirectsRepo(repo *CanonicalPathRedirectsRepository) *FragmentsMergeRepository {
	if r == nil {
		return r
	}
	r.redirectsRepo = repo
	return r
}

// MergeFragment implements policy.MergeFragments (string params for cross-package use).
func (r *FragmentsMergeRepository) MergeFragment(ctx context.Context, primaryID, duplicateID, tenantID, source, toolName, jobID, lensID string) (*policy.MergeResult, error) {
	return r.MergeFragmentWithRequest(ctx, MergeRequest{
		PrimaryFragmentID:   primaryID,
		DuplicateFragmentID: duplicateID,
		TenantID:            tenantID,
		Source:              FragmentMergeAuditSource(source),
		ToolName:            toolName,
		JobID:               jobID,
		LensID:              lensID,
	})
}

// MergeFragmentWithRequest merges with full request (e.g. OnEdgeReassigned for lens sync).
func (r *FragmentsMergeRepository) MergeFragmentWithRequest(ctx context.Context, req MergeRequest) (*policy.MergeResult, error) {
	res, err := r.mergeFragment(ctx, req)
	if err != nil {
		return nil, err
	}
	return &policy.MergeResult{
		DataMerged:       res.DataMerged,
		EdgesReassigned:  res.EdgesReassigned,
		EdgesDeleted:     res.EdgesDeleted,
		DuplicateDeleted: res.DuplicateDeleted,
	}, nil
}

// mergeFragment merges duplicate into primary: data merge, edge reassign+dedup, delete, audit.
func (r *FragmentsMergeRepository) mergeFragment(ctx context.Context, req MergeRequest) (*MergeResult, error) {
	primaryID := strings.TrimSpace(req.PrimaryFragmentID)
	dupID := strings.TrimSpace(req.DuplicateFragmentID)
	tenantID := strings.TrimSpace(req.TenantID)
	if tenantID == "" {
		tenantID = "global"
	}
	if primaryID == "" || dupID == "" || primaryID == dupID {
		return nil, fmt.Errorf("invalid merge: primary=%q duplicate=%q", primaryID, dupID)
	}

	result := &MergeResult{}

	primary, err := r.fragmentsRepo.Get(ctx, primaryID)
	if err != nil || primary == nil {
		return nil, fmt.Errorf("primary fragment not found: %w", err)
	}
	dup, err := r.fragmentsRepo.Get(ctx, dupID)
	if err != nil || dup == nil {
		return nil, fmt.Errorf("duplicate fragment not found: %w", err)
	}

	// 1. Merge data (Labels, Identifiers, State, merged_paths)
	if mergeFragmentDataFillFirst(primary, dup) {
		primary.UpdatedAt = time.Now().UTC()
		if err := r.fragmentsRepo.Update(ctx, primary); err != nil {
			return nil, fmt.Errorf("failed to update primary: %w", err)
		}
		result.DataMerged = true
	}

	// 2. Reassign edges from duplicate to primary (with dedup)
	reassigned, deleted := r.reassignEdgesWithDedup(ctx, tenantID, dupID, primaryID, req.OnEdgeReassigned)
	result.EdgesReassigned = reassigned
	result.EdgesDeleted = deleted

	// 3. Best-effort dedupe edges for primary node
	dedupDeleted := r.bestEffortDedupeEdgesForNode(ctx, tenantID, primaryID, 2000)
	result.EdgesDeleted += dedupDeleted

	// 4. Record audit (best-effort)
	if r.mergeAuditRepo != nil {
		_ = r.mergeAuditRepo.RecordMerge(ctx,
			dupID, strings.TrimSpace(dup.CanonicalPath), strings.TrimSpace(dup.TenantID),
			primaryID, strings.TrimSpace(primary.CanonicalPath), strings.TrimSpace(primary.TenantID),
			req.Source, req.ToolName, req.JobID, req.LensID,
		)
	}

	// 4.5. Record canonical path redirects so Resolve() returns the final path after merges.
	r.recordMergeRedirects(ctx, dup, primary)

	// 5. Delete duplicate
	if err := r.fragmentsRepo.Delete(ctx, dupID); err != nil {
		return nil, fmt.Errorf("failed to delete duplicate: %w", err)
	}
	result.DuplicateDeleted = true

	return result, nil
}

// recordMergeRedirects records redirects so canonical path resolution returns the final path.
// Records: duplicate path -> primary path, and each path in merged_paths -> primary path.
// Uses warmupThreshold=1 so redirects become stable immediately.
func (r *FragmentsMergeRepository) recordMergeRedirects(ctx context.Context, dup, primary *models.HorizonFragment) {
	if r == nil || r.redirectsRepo == nil || dup == nil || primary == nil {
		return
	}
	target := strings.TrimSpace(primary.CanonicalPath)
	if target == "" {
		return
	}
	fragType := string(primary.Type)
	if fragType == "" {
		fragType = string(dup.Type)
	}

	sources := make(map[string]struct{})
	if p := strings.TrimSpace(dup.CanonicalPath); p != "" && p != target {
		sources[p] = struct{}{}
	}
	if merged := strings.TrimSpace(dup.Identifiers["merged_paths"]); merged != "" {
		for _, p := range strings.Split(merged, ",") {
			if p = strings.TrimSpace(p); p != "" && p != target {
				sources[p] = struct{}{}
			}
		}
	}

	for src := range sources {
		_, _, _, _ = r.redirectsRepo.RecordObservation(ctx,
			"global", src, target, "merge", fragType, 1, mergeRedirectStableTTL)
	}
}

// reassignEdgesWithDedup reassigns edges from fromID to toID, deduplicating when (toID, other, type) already exists.
func (r *FragmentsMergeRepository) reassignEdgesWithDedup(
	ctx context.Context,
	tenantID, fromID, toID string,
	onReassigned func(*models.DependencyEdge),
) (reassigned, deleted int) {
	const limit = 5000
	srcEdges, err := r.edgesRepo.ListBySource(ctx, tenantID, fromID, nil, limit)
	if err != nil {
		return 0, 0
	}
	tgtEdges, err := r.edgesRepo.ListByTarget(ctx, tenantID, fromID, nil, limit)
	if err != nil {
		return 0, 0
	}
	for _, edge := range srcEdges {
		if edge == nil || strings.TrimSpace(edge.ID) == "" || edge.Type == "" {
			continue
		}
		existing, err := r.edgesRepo.GetBySourceTargetAndType(ctx, tenantID, toID, edge.TargetFragmentID, edge.Type)
		if err == nil && existing != nil {
			if mergeEdgeMetrics(existing, edge) {
				_ = r.edgesRepo.Update(ctx, existing)
			}
			if err := r.edgesRepo.Delete(ctx, edge.ID); err == nil {
				deleted++
			}
			continue
		}
		edge.SourceFragmentID = toID
		if edge.SourceFragmentID == edge.TargetFragmentID {
			if err := r.edgesRepo.Delete(ctx, edge.ID); err == nil {
				deleted++
			}
			continue
		}
		edge.Metadata.LastUpdatedAt = time.Now().UTC()
		if err := r.edgesRepo.Update(ctx, edge); err == nil {
			reassigned++
			if onReassigned != nil {
				onReassigned(edge)
			}
		}
	}
	for _, edge := range tgtEdges {
		if edge == nil || strings.TrimSpace(edge.ID) == "" || edge.Type == "" {
			continue
		}
		existing, err := r.edgesRepo.GetBySourceTargetAndType(ctx, tenantID, edge.SourceFragmentID, toID, edge.Type)
		if err == nil && existing != nil {
			if mergeEdgeMetrics(existing, edge) {
				_ = r.edgesRepo.Update(ctx, existing)
			}
			if err := r.edgesRepo.Delete(ctx, edge.ID); err == nil {
				deleted++
			}
			continue
		}
		edge.TargetFragmentID = toID
		if edge.SourceFragmentID == edge.TargetFragmentID {
			if err := r.edgesRepo.Delete(ctx, edge.ID); err == nil {
				deleted++
			}
			continue
		}
		edge.Metadata.LastUpdatedAt = time.Now().UTC()
		if err := r.edgesRepo.Update(ctx, edge); err == nil {
			reassigned++
			if onReassigned != nil {
				onReassigned(edge)
			}
		}
	}
	return reassigned, deleted
}

func mergeEdgeMetrics(dst, src *models.DependencyEdge) bool {
	if dst == nil || src == nil {
		return false
	}
	changed := false
	if len(src.Metrics) > 0 {
		if dst.Metrics == nil {
			dst.Metrics = map[string]models.MetricValue{}
		}
		for k, mv := range src.Metrics {
			key := strings.TrimSpace(k)
			if key == "" {
				continue
			}
			if cur, ok := dst.Metrics[key]; ok {
				if betterMetricValue(mv, cur) {
					dst.Metrics[key] = mv
					changed = true
				}
			} else {
				dst.Metrics[key] = mv
				changed = true
			}
		}
	}
	if len(src.Qualifiers) > 0 {
		if dst.Qualifiers == nil {
			dst.Qualifiers = map[string]interface{}{}
		}
		for k, v := range src.Qualifiers {
			key := strings.TrimSpace(k)
			if key == "" {
				continue
			}
			if _, ok := dst.Qualifiers[key]; ok {
				continue
			}
			dst.Qualifiers[key] = v
			changed = true
		}
	}
	return changed
}

func betterMetricValue(a, b models.MetricValue) bool {
	if a.Confidence > b.Confidence+1e-9 {
		return true
	}
	if b.Confidence > a.Confidence+1e-9 {
		return false
	}
	if !b.ObservedAt.IsZero() && !a.ObservedAt.IsZero() && a.ObservedAt.After(b.ObservedAt) {
		return true
	}
	if b.ObservedAt.IsZero() && !a.ObservedAt.IsZero() {
		return true
	}
	return false
}

func mergeFragmentDataFillFirst(target, source *models.HorizonFragment) bool {
	if target == nil || source == nil {
		return false
	}
	changed := false
	if target.Labels == nil {
		target.Labels = map[string]string{}
	}
	for k, v := range source.Labels {
		if strings.TrimSpace(v) != "" && strings.TrimSpace(target.Labels[k]) == "" {
			target.Labels[k] = v
			changed = true
		}
	}
	if target.Identifiers == nil {
		target.Identifiers = map[string]string{}
	}
	for k, v := range source.Identifiers {
		if strings.TrimSpace(v) != "" && strings.TrimSpace(target.Identifiers[k]) == "" {
			target.Identifiers[k] = v
			changed = true
		}
	}
	if strings.TrimSpace(target.Description) == "" && strings.TrimSpace(source.Description) != "" {
		target.Description = source.Description
		changed = true
	}
	tagSet := map[string]bool{}
	for _, t := range target.Tags {
		tagSet[t] = true
	}
	for _, t := range source.Tags {
		if !tagSet[t] {
			target.Tags = append(target.Tags, t)
			tagSet[t] = true
			changed = true
		}
	}
	if mergeStatePreferMetrics(target, source.State) {
		changed = true
	}
	if strings.TrimSpace(source.CanonicalPath) != "" {
		if target.Identifiers["merged_paths"] == "" {
			target.Identifiers["merged_paths"] = source.CanonicalPath
			changed = true
		} else if !strings.Contains(target.Identifiers["merged_paths"], source.CanonicalPath) {
			target.Identifiers["merged_paths"] += "," + source.CanonicalPath
			changed = true
		}
	}
	return changed
}

func mergeStatePreferMetrics(target *models.HorizonFragment, src map[string]models.MetricValue) bool {
	if target == nil || src == nil || len(src) == 0 {
		return false
	}
	if target.State == nil {
		target.State = map[string]models.MetricValue{}
	}
	changed := false
	for k, mv := range src {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		cur, ok := target.State[key]
		if !ok {
			target.State[key] = mv
			changed = true
			continue
		}
		if betterMetricValue(mv, cur) {
			target.State[key] = mv
			changed = true
		}
	}
	return changed
}

func (r *FragmentsMergeRepository) bestEffortDedupeEdgesForNode(ctx context.Context, tenantID, fragmentID string, limit int) (deleted int) {
	if r == nil || r.edgesRepo == nil {
		return 0
	}
	tenantID = strings.TrimSpace(tenantID)
	fragmentID = strings.TrimSpace(fragmentID)
	if tenantID == "" || fragmentID == "" || limit <= 0 {
		return 0
	}
	srcEdges, err := r.edgesRepo.ListBySource(ctx, tenantID, fragmentID, nil, limit)
	if err != nil {
		return 0
	}
	tgtEdges, err := r.edgesRepo.ListByTarget(ctx, tenantID, fragmentID, nil, limit)
	if err != nil {
		return 0
	}
	type key struct {
		s  string
		t  string
		ty string
	}
	kept := map[key]*models.DependencyEdge{}
	consider := func(ed *models.DependencyEdge) {
		if ed == nil {
			return
		}
		s := strings.TrimSpace(ed.SourceFragmentID)
		t := strings.TrimSpace(ed.TargetFragmentID)
		ty := ed.Type
		ns, nt, nty, _ := models.NormalizeSymmetricEdge(s, t, ty)
		k := key{s: ns, t: nt, ty: string(nty)}
		if existing, ok := kept[k]; ok && existing != nil {
			keep := existing
			drop := ed
			if ed.Metadata.CreatedAt.Before(existing.Metadata.CreatedAt) ||
				(ed.Metadata.CreatedAt.Equal(existing.Metadata.CreatedAt) && strings.TrimSpace(ed.ID) < strings.TrimSpace(existing.ID)) {
				keep = ed
				drop = existing
				kept[k] = keep
			}
			if drop != nil && strings.TrimSpace(drop.ID) != "" {
				if r.edgesRepo.Delete(ctx, drop.ID) == nil {
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
	return deleted
}
