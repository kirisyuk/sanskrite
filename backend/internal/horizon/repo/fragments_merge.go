package repo

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
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
// It performs: data merge (merged_paths), edge reassign+dedup, duplicate delete, audit,
// materialized artifacts path rewrite, lens scope rewrite.
// When redirectsRepo is set, records canonical path redirects so Resolve() returns the final path after merges.
type FragmentsMergeRepository struct {
	fragmentsRepo     *HorizonFragmentsRepository
	edgesRepo         *DependencyEdgesRepository
	mergeAuditRepo    *FragmentMergeAuditRepository
	redirectsRepo     *CanonicalPathRedirectsRepository
	materializedRepo  *MaterializedArtifactsRepository
	lensesRepo        *LensesRepository
	watchSubjectsRepo *WatchSubjectsRepository
	ontologyFactsRepo *corerepo.OntologyFactsRepository
	entitiesRepo      *corerepo.EntityRepository
}

// NewFragmentsMergeRepository creates a new merge repository.
func NewFragmentsMergeRepository(
	fragmentsRepo *HorizonFragmentsRepository,
	edgesRepo *DependencyEdgesRepository,
	mergeAuditRepo *FragmentMergeAuditRepository,
) *FragmentsMergeRepository {
	return &FragmentsMergeRepository{
		fragmentsRepo:  fragmentsRepo,
		edgesRepo:      edgesRepo,
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

// WithMaterializedRepo wires the materialized artifacts repository. When set, merge
// rewrites NodeRefs, Payload nodes, and Labels in MaterializedArtifacts (supply chains,
// domain artifacts) so canonical paths stay consistent after fragment merge.
func (r *FragmentsMergeRepository) WithMaterializedRepo(repo *MaterializedArtifactsRepository) *FragmentsMergeRepository {
	if r == nil {
		return r
	}
	r.materializedRepo = repo
	return r
}

// WithLensesRepo wires the lenses repository. When set, merge rewrites lens.Scope,
// ScopeFragments, and ResolvedQueries so paths reference the surviving canonical path.
func (r *FragmentsMergeRepository) WithLensesRepo(repo *LensesRepository) *FragmentsMergeRepository {
	if r == nil {
		return r
	}
	r.lensesRepo = repo
	return r
}

// WithWatchSubjectsRepo wires the watch subjects repository. When set, merge retargets
// anchorCanonicalPath/universePaths and recomputes subject IDs when the anchor path changes.
func (r *FragmentsMergeRepository) WithWatchSubjectsRepo(repo *WatchSubjectsRepository) *FragmentsMergeRepository {
	if r == nil {
		return r
	}
	r.watchSubjectsRepo = repo
	return r
}

// WithOntologyFactsRepo wires ontology facts so canonicalPaths/actorPaths/locationPath are
// rewritten after path merges.
func (r *FragmentsMergeRepository) WithOntologyFactsRepo(repo *corerepo.OntologyFactsRepository) *FragmentsMergeRepository {
	if r == nil {
		return r
	}
	r.ontologyFactsRepo = repo
	return r
}

// WithEntitiesRepo wires ontology entities so canonicalPaths remain aligned after merges.
func (r *FragmentsMergeRepository) WithEntitiesRepo(repo *corerepo.EntityRepository) *FragmentsMergeRepository {
	if r == nil {
		return r
	}
	r.entitiesRepo = repo
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

	// 4.6. Rewrite canonical paths in materialized artifacts (supply chains, domain artifacts).
	r.rewriteMaterializedArtifactsPaths(ctx, tenantID, dup, primary)

	// 4.7. Rewrite lens scope entries (fromPath -> toPath) so UI/graph stay consistent.
	r.rewriteLensScopePaths(ctx, tenantID, dup, primary)

	// 4.8. Rewrite supply-link refs that embed merged node paths into their identity.
	r.rewriteSupplyLinksPaths(ctx, tenantID, dup, primary)

	// 4.9. Rewrite watch/runtime and ontology stores indexed by canonicalPath.
	r.rewriteWatchSubjectsPaths(ctx, tenantID, dup, primary)
	r.rewriteOntologyFactsPaths(ctx, tenantID, dup, primary)
	r.rewriteOntologyEntitiesPaths(ctx, tenantID, dup, primary)

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
	r.recordPathRedirects(ctx, fragType, collectFragmentMergePaths(dup, target), target)
}

func (r *FragmentsMergeRepository) recordPathRedirects(ctx context.Context, fragType string, sources []string, target string) {
	if r == nil || r.redirectsRepo == nil {
		return
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return
	}
	for _, src := range normalizeAndSortStrings(sources) {
		if src == target {
			continue
		}
		_, _, _, _ = r.redirectsRepo.RecordObservation(ctx,
			"global", src, target, "merge", fragType, 1, mergeRedirectStableTTL)
	}
}

// rewriteMaterializedArtifactsPaths replaces old canonical paths with the primary path in all
// MaterializedArtifacts (supply chains, domain artifacts) that reference the duplicate.
func (r *FragmentsMergeRepository) rewriteMaterializedArtifactsPaths(ctx context.Context, tenantID string, dup, primary *models.HorizonFragment) {
	if dup == nil || primary == nil {
		return
	}
	target := strings.TrimSpace(primary.CanonicalPath)
	if target == "" {
		return
	}
	r.rewriteMaterializedArtifactsPathsForPaths(ctx, tenantID, dup.Type, collectFragmentMergePaths(dup, target), target)
}

func (r *FragmentsMergeRepository) rewriteMaterializedArtifactsPathsForPaths(ctx context.Context, tenantID string, fragType models.FragmentType, pathsToReplace []string, target string) {
	if r.materializedRepo == nil {
		return
	}
	target = strings.TrimSpace(target)
	pathsToReplace = normalizeAndSortStrings(pathsToReplace)
	if target == "" || len(pathsToReplace) == 0 {
		return
	}
	kinds := []models.MaterializedKind{
		models.MaterializedKindSupplyChainPath,
		models.MaterializedKindSupplyChainAssemblyGraph,
		models.MaterializedKindRegulatoryImpactMap,
		models.MaterializedKindGovernanceChain,
		models.MaterializedKindEcosystemMap,
	}

	toUpdate := make(map[string]*models.MaterializedArtifact)
	collectArtifacts := func(artifacts []*models.MaterializedArtifact) {
		for _, art := range artifacts {
			if art == nil || strings.TrimSpace(art.ID) == "" {
				continue
			}
			toUpdate[art.ID] = art
		}
	}
	collectIndexed := func(oldPath string, kind models.MaterializedKind) {
		if tenantID == "global" {
			if artifacts, err := r.materializedRepo.FindByNodeRefAnyTenant(ctx, oldPath, kind, 500); err == nil {
				collectArtifacts(artifacts)
			}
			switch fragType {
			case models.FragmentTypeGeoRegion:
				if artifacts, err := r.materializedRepo.FindByGeoRefsAnyTenant(ctx, []string{oldPath}, kind, 500); err == nil {
					collectArtifacts(artifacts)
				}
			case models.FragmentTypeSupplyLink:
				if artifacts, err := r.materializedRepo.FindByFlowRefsAnyTenant(ctx, []string{oldPath}, kind, 500); err == nil {
					collectArtifacts(artifacts)
				}
			case models.FragmentTypeAffectedZone:
				if artifacts, err := r.materializedRepo.FindByZoneRefsAnyTenant(ctx, []string{oldPath}, kind, 500); err == nil {
					collectArtifacts(artifacts)
				}
				if artifacts, err := r.materializedRepo.FindByAffectedZoneRefsAnyTenant(ctx, []string{oldPath}, kind, 500); err == nil {
					collectArtifacts(artifacts)
				}
			}
			return
		}
		if artifacts, err := r.materializedRepo.FindByNodeRef(ctx, tenantID, oldPath, kind); err == nil {
			collectArtifacts(artifacts)
		}
		if artifacts, err := r.materializedRepo.FindByNodeRef(ctx, "global", oldPath, kind); err == nil {
			collectArtifacts(artifacts)
		}
		switch fragType {
		case models.FragmentTypeGeoRegion:
			if artifacts, err := r.materializedRepo.FindByGeoRefsAny(ctx, tenantID, []string{oldPath}, kind, 500); err == nil {
				collectArtifacts(artifacts)
			}
			if artifacts, err := r.materializedRepo.FindByGeoRefsAny(ctx, "global", []string{oldPath}, kind, 500); err == nil {
				collectArtifacts(artifacts)
			}
		case models.FragmentTypeSupplyLink:
			if artifacts, err := r.materializedRepo.FindByFlowRefsAny(ctx, tenantID, []string{oldPath}, kind, 500); err == nil {
				collectArtifacts(artifacts)
			}
			if artifacts, err := r.materializedRepo.FindByFlowRefsAny(ctx, "global", []string{oldPath}, kind, 500); err == nil {
				collectArtifacts(artifacts)
			}
		case models.FragmentTypeAffectedZone:
			if artifacts, err := r.materializedRepo.FindByZoneRefsAny(ctx, tenantID, []string{oldPath}, kind, 500); err == nil {
				collectArtifacts(artifacts)
			}
			if artifacts, err := r.materializedRepo.FindByZoneRefsAny(ctx, "global", []string{oldPath}, kind, 500); err == nil {
				collectArtifacts(artifacts)
			}
			if artifacts, err := r.materializedRepo.FindByAffectedZoneRefsAny(ctx, tenantID, []string{oldPath}, kind, 500); err == nil {
				collectArtifacts(artifacts)
			}
			if artifacts, err := r.materializedRepo.FindByAffectedZoneRefsAny(ctx, "global", []string{oldPath}, kind, 500); err == nil {
				collectArtifacts(artifacts)
			}
		}
	}
	for _, oldPath := range pathsToReplace {
		for _, kind := range kinds {
			collectIndexed(oldPath, kind)
		}
	}
	for _, art := range toUpdate {
		changed := false
		for _, oldPath := range pathsToReplace {
			if models.ReplaceCanonicalPathInArtifact(art, oldPath, target) {
				changed = true
			}
		}
		if changed {
			_ = r.materializedRepo.Update(ctx, art)
		}
	}
}

// rewriteLensScopePaths replaces old canonical paths with the primary path in lens.Scope,
// ScopeFragments, and ResolvedQueries for all lenses that reference the duplicate.
func (r *FragmentsMergeRepository) rewriteLensScopePaths(ctx context.Context, tenantID string, dup, primary *models.HorizonFragment) {
	if dup == nil || primary == nil {
		return
	}
	target := strings.TrimSpace(primary.CanonicalPath)
	if target == "" {
		return
	}
	r.rewriteLensScopePathsForPaths(ctx, tenantID, collectFragmentMergePaths(dup, target), target)
}

func (r *FragmentsMergeRepository) rewriteLensScopePathsForPaths(ctx context.Context, tenantID string, pathsToReplace []string, target string) {
	if r.lensesRepo == nil {
		return
	}
	target = strings.TrimSpace(target)
	pathsToReplace = normalizeAndSortStrings(pathsToReplace)
	if target == "" || len(pathsToReplace) == 0 {
		return
	}
	toUpdate := make(map[string]*models.Lens)
	for _, fromPath := range pathsToReplace {
		var lenses []*models.Lens
		var err error
		if tenantID == "global" {
			lenses, err = r.lensesRepo.FindByScopeAnyTenant(ctx, fromPath, 500)
		} else {
			lenses, err = r.lensesRepo.FindByScope(ctx, tenantID, fromPath, 250)
			if err == nil {
				if globalLenses, globalErr := r.lensesRepo.FindByScope(ctx, "global", fromPath, 250); globalErr == nil {
					lenses = append(lenses, globalLenses...)
				}
			}
		}
		if err != nil {
			continue
		}
		for _, lens := range lenses {
			if lens == nil || strings.TrimSpace(lens.ID) == "" {
				continue
			}
			toUpdate[lens.ID] = lens
		}
	}
	for _, lens := range toUpdate {
		changed := false
		for _, fromPath := range pathsToReplace {
			if replaceCanonicalPathInStringSlice(lens.Scope, fromPath, target) {
				changed = true
			}
			if lens.ScopeFragments != nil {
				if fid, ok := lens.ScopeFragments[fromPath]; ok {
					delete(lens.ScopeFragments, fromPath)
					if _, ok2 := lens.ScopeFragments[target]; !ok2 && strings.TrimSpace(fid) != "" {
						lens.ScopeFragments[target] = fid
					}
					changed = true
				}
			}
			if lens.ResolvedQueries != nil {
				for queryAddr, resolvedPaths := range lens.ResolvedQueries {
					if replaceCanonicalPathInStringSlice(resolvedPaths, fromPath, target) {
						lens.ResolvedQueries[queryAddr] = normalizeAndSortStrings(resolvedPaths)
						changed = true
					}
				}
			}
		}
		if changed {
			lens.Scope = normalizeAndSortStrings(lens.Scope)
			_ = r.lensesRepo.Update(ctx, lens)
		}
	}
}

func (r *FragmentsMergeRepository) rewriteSupplyLinksPaths(ctx context.Context, tenantID string, dup, primary *models.HorizonFragment) {
	if dup == nil || primary == nil || r.fragmentsRepo == nil {
		return
	}
	target := strings.TrimSpace(primary.CanonicalPath)
	if target == "" {
		return
	}
	pathsToReplace := collectFragmentMergePaths(dup, target)
	if len(pathsToReplace) == 0 {
		return
	}
	replacements := make(map[string]string, len(pathsToReplace))
	for _, oldPath := range pathsToReplace {
		oldPath = strings.TrimSpace(oldPath)
		if oldPath == "" || oldPath == target {
			continue
		}
		replacements[oldPath] = target
	}
	if len(replacements) == 0 {
		return
	}
	_, _ = r.RewriteSupplyLinksForPathPairs(ctx, tenantID, replacements)
}

// RewriteSupplyLinksForPathPairs rewrites embedded canonical-path refs inside SUPPLY_LINK fragments.
// It is safe to run as a repair pass after merges, because it rewrites by old->new path pairs and
// then re-materializes the supply-link canonicalPath plus dependent redirects/materialized stores.
func (r *FragmentsMergeRepository) RewriteSupplyLinksForPathPairs(ctx context.Context, tenantID string, replacements map[string]string) (int, error) {
	if r == nil || r.fragmentsRepo == nil {
		return 0, nil
	}
	replacements = normalizePathReplacementMap(replacements)
	if len(replacements) == 0 {
		return 0, nil
	}
	log.Printf("[FragmentsMerge] rewrite supply links: tenant=%s pathPairs=%d", tenantID, len(replacements))

	supplyLinks, err := r.collectSupplyLinksForPathPairs(ctx, tenantID, replacements)
	if err != nil {
		return 0, err
	}
	log.Printf("[FragmentsMerge] rewrite supply links: collected links=%d", len(supplyLinks))
	updated := 0
	orderedOldPaths := make([]string, 0, len(replacements))
	for oldPath := range replacements {
		orderedOldPaths = append(orderedOldPaths, oldPath)
	}
	sort.Strings(orderedOldPaths)

	for idx, link := range supplyLinks {
		if idx > 0 && idx%10 == 0 {
			log.Printf("[FragmentsMerge] rewrite supply links progress: processed=%d updated=%d total=%d", idx, updated, len(supplyLinks))
		}
		changed := false
		for _, oldPath := range orderedOldPaths {
			target := replacements[oldPath]
			if replaceCanonicalPathInOptionalString(link.SupplierRef, oldPath, target) {
				changed = true
			}
			if replaceCanonicalPathInOptionalString(link.CustomerRef, oldPath, target) {
				changed = true
			}
			if replaceCanonicalPathInOptionalString(link.ProductRef, oldPath, target) {
				changed = true
			}
			if replaceCanonicalPathInOptionalString(link.SupplierFacilityRef, oldPath, target) {
				changed = true
			}
			if replaceCanonicalPathInOptionalString(link.CustomerFacilityRef, oldPath, target) {
				changed = true
			}
			if replaceCanonicalPathInStringSlice(link.TransportViaRefs, oldPath, target) {
				changed = true
			}
		}
		if !changed {
			continue
		}
		oldLinkPath := strings.TrimSpace(link.CanonicalPath)
		newLinkPath, err := models.BuildSupplyLinkCanonicalPath(link.TenantID, link.SupplierRef, link.ProductRef, link.CustomerRef)
		if err == nil && strings.TrimSpace(newLinkPath) != "" {
			link.CanonicalPath = strings.TrimSpace(newLinkPath)
			if link.CanonicalPath != oldLinkPath {
				link.AlternativePaths = normalizeAndSortStrings(append(link.AlternativePaths, oldLinkPath))
			}
		}
		if err := r.fragmentsRepo.Update(ctx, link); err != nil {
			return updated, err
		}
		if oldLinkPath != "" && link.CanonicalPath != "" && oldLinkPath != link.CanonicalPath {
			r.recordPathRedirects(ctx, string(models.FragmentTypeSupplyLink), []string{oldLinkPath}, link.CanonicalPath)
			r.rewriteMaterializedArtifactsPathsForPaths(ctx, link.TenantID, models.FragmentTypeSupplyLink, []string{oldLinkPath}, link.CanonicalPath)
			r.rewriteLensScopePathsForPaths(ctx, link.TenantID, []string{oldLinkPath}, link.CanonicalPath)
			r.rewriteWatchSubjectsPathsForPaths(ctx, link.TenantID, []string{oldLinkPath}, link.CanonicalPath, "", "")
			r.rewriteOntologyFactsPathsForPaths(ctx, link.TenantID, []string{oldLinkPath}, link.CanonicalPath)
			r.rewriteOntologyEntitiesPathsForPaths(ctx, link.TenantID, []string{oldLinkPath}, link.CanonicalPath)
		}
		updated++
	}
	return updated, nil
}

// RewriteProductPathReferencesForPathPairs repairs downstream stores for old->new product canonical path pairs.
// It is intended for post-merge backfills where synonym fragments are already gone, but dependent stores
// (materialized artifacts, lenses, watch subjects, ontology, supply links) still reference old product paths.
func (r *FragmentsMergeRepository) RewriteProductPathReferencesForPathPairs(ctx context.Context, tenantID string, replacements map[string]string) (int, error) {
	if r == nil {
		return 0, nil
	}
	replacements = normalizePathReplacementMap(replacements)
	if len(replacements) == 0 {
		return 0, nil
	}
	orderedOldPaths := make([]string, 0, len(replacements))
	for oldPath := range replacements {
		orderedOldPaths = append(orderedOldPaths, oldPath)
	}
	sort.Strings(orderedOldPaths)
	log.Printf("[FragmentsMerge] rewrite product path references: tenant=%s pathPairs=%d", tenantID, len(orderedOldPaths))
	for idx, oldPath := range orderedOldPaths {
		if idx > 0 && idx%10 == 0 {
			log.Printf("[FragmentsMerge] rewrite product path references progress: processed=%d total=%d", idx, len(orderedOldPaths))
		}
		target := replacements[oldPath]
		r.recordPathRedirects(ctx, string(models.FragmentTypeProduct), []string{oldPath}, target)
		r.rewriteMaterializedArtifactsPathsForPaths(ctx, tenantID, models.FragmentTypeProduct, []string{oldPath}, target)
	}
	updatedSupplyLinks, err := r.RewriteSupplyLinksForPathPairs(ctx, tenantID, replacements)
	if err != nil {
		return 0, err
	}
	return updatedSupplyLinks, nil
}

func (r *FragmentsMergeRepository) collectSupplyLinksForPathPairs(ctx context.Context, tenantID string, replacements map[string]string) ([]*models.HorizonFragment, error) {
	supplyLinks := make(map[string]*models.HorizonFragment)
	collectLinks := func(items []*models.HorizonFragment) {
		for _, item := range items {
			if item == nil || item.Type != models.FragmentTypeSupplyLink || strings.TrimSpace(item.ID) == "" {
				continue
			}
			supplyLinks[item.ID] = item
		}
	}
	orderedOldPaths := make([]string, 0, len(replacements))
	for oldPath := range replacements {
		orderedOldPaths = append(orderedOldPaths, oldPath)
	}
	sort.Strings(orderedOldPaths)
	for idx, oldPath := range orderedOldPaths {
		if idx > 0 && idx%10 == 0 {
			log.Printf("[FragmentsMerge] collect supply links progress: scannedPaths=%d collectedLinks=%d totalPaths=%d", idx, len(supplyLinks), len(orderedOldPaths))
		}
		if items, err := r.findSupplyLinksByPath(ctx, tenantID, oldPath); err != nil {
			return nil, err
		} else {
			collectLinks(items)
		}
	}
	out := make([]*models.HorizonFragment, 0, len(supplyLinks))
	for _, link := range supplyLinks {
		out = append(out, link)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.TrimSpace(out[i].ID) < strings.TrimSpace(out[j].ID)
	})
	return out, nil
}

func (r *FragmentsMergeRepository) findSupplyLinksByPath(ctx context.Context, tenantID, path string) ([]*models.HorizonFragment, error) {
	path = strings.TrimSpace(path)
	if path == "" || r == nil || r.fragmentsRepo == nil {
		return nil, nil
	}
	limit := 10000
	collect := func(out map[string]*models.HorizonFragment, items []*models.HorizonFragment) {
		for _, item := range items {
			if item == nil || item.Type != models.FragmentTypeSupplyLink || strings.TrimSpace(item.ID) == "" {
				continue
			}
			out[item.ID] = item
		}
	}
	found := make(map[string]*models.HorizonFragment)
	if tenantID == "global" {
		if items, err := r.fragmentsRepo.FindSupplyLinksBySupplierAnyTenant(ctx, path, limit); err != nil {
			return nil, err
		} else {
			collect(found, items)
		}
		if items, err := r.fragmentsRepo.FindSupplyLinksByCustomerAnyTenant(ctx, path, limit); err != nil {
			return nil, err
		} else {
			collect(found, items)
		}
		if items, err := r.fragmentsRepo.FindSupplyLinksByProductAnyTenant(ctx, path, limit); err != nil {
			return nil, err
		} else {
			collect(found, items)
		}
		if items, err := r.fragmentsRepo.FindSupplyLinksByTransportViaAnyTenant(ctx, path, limit); err != nil {
			return nil, err
		} else {
			collect(found, items)
		}
	} else {
		for _, scopeTenant := range []string{tenantID, "global"} {
			if items, err := r.fragmentsRepo.FindSupplyLinksBySupplier(ctx, path, scopeTenant, limit); err != nil {
				return nil, err
			} else {
				collect(found, items)
			}
			if items, err := r.fragmentsRepo.FindSupplyLinksByCustomer(ctx, path, scopeTenant, limit); err != nil {
				return nil, err
			} else {
				collect(found, items)
			}
			if items, err := r.fragmentsRepo.FindSupplyLinksByProduct(ctx, path, scopeTenant, limit); err != nil {
				return nil, err
			} else {
				collect(found, items)
			}
			if items, err := r.fragmentsRepo.FindSupplyLinksByTransportVia(ctx, path, scopeTenant, limit); err != nil {
				return nil, err
			} else {
				collect(found, items)
			}
		}
	}
	out := make([]*models.HorizonFragment, 0, len(found))
	for _, item := range found {
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.TrimSpace(out[i].ID) < strings.TrimSpace(out[j].ID)
	})
	return out, nil
}

func normalizePathReplacementMap(replacements map[string]string) map[string]string {
	if len(replacements) == 0 {
		return nil
	}
	out := make(map[string]string, len(replacements))
	for oldPath, newPath := range replacements {
		oldPath = strings.TrimSpace(oldPath)
		newPath = strings.TrimSpace(newPath)
		if oldPath == "" || newPath == "" || oldPath == newPath {
			continue
		}
		out[oldPath] = newPath
	}
	return out
}

func (r *FragmentsMergeRepository) rewriteWatchSubjectsPaths(ctx context.Context, tenantID string, dup, primary *models.HorizonFragment) {
	if dup == nil || primary == nil {
		return
	}
	target := strings.TrimSpace(primary.CanonicalPath)
	if target == "" {
		return
	}
	r.rewriteWatchSubjectsPathsForPaths(ctx, tenantID, collectFragmentMergePaths(dup, target), target, strings.TrimSpace(dup.ID), strings.TrimSpace(primary.ID))
}

func (r *FragmentsMergeRepository) rewriteWatchSubjectsPathsForPaths(ctx context.Context, tenantID string, pathsToReplace []string, target, oldFragmentID, newFragmentID string) {
	if r.watchSubjectsRepo == nil {
		return
	}
	target = strings.TrimSpace(target)
	pathsToReplace = normalizeAndSortStrings(pathsToReplace)
	oldFragmentID = strings.TrimSpace(oldFragmentID)
	newFragmentID = strings.TrimSpace(newFragmentID)
	if target == "" || len(pathsToReplace) == 0 {
		return
	}
	toUpdate := make(map[string]*models.WatchSubject)
	collectSubjects := func(items []*models.WatchSubject) {
		for _, item := range items {
			if item == nil || strings.TrimSpace(item.ID) == "" {
				continue
			}
			toUpdate[item.ID] = item
		}
	}
	for _, oldPath := range pathsToReplace {
		if tenantID == "global" {
			if items, err := r.watchSubjectsRepo.ListByAnchorCanonicalPathAnyTenant(ctx, oldPath, 250); err == nil {
				collectSubjects(items)
			}
			if items, err := r.watchSubjectsRepo.ListByUniversePathAnyTenant(ctx, oldPath, 500); err == nil {
				collectSubjects(items)
			}
			continue
		}
		if items, err := r.watchSubjectsRepo.ListByAnchorCanonicalPath(ctx, tenantID, oldPath, 200); err == nil {
			collectSubjects(items)
		}
		if items, err := r.watchSubjectsRepo.ListByUniversePath(ctx, tenantID, oldPath, 400); err == nil {
			collectSubjects(items)
		}
	}
	for _, subject := range toUpdate {
		oldID := strings.TrimSpace(subject.ID)
		changed := false
		for _, oldPath := range pathsToReplace {
			if strings.TrimSpace(subject.AnchorCanonicalPath) == oldPath {
				subject.AnchorCanonicalPath = target
				changed = true
			}
			if replaceCanonicalPathInStringSlice(subject.UniversePaths, oldPath, target) {
				changed = true
			}
			if replaceCanonicalPathInMetadata(subject.Metadata, oldPath, target) {
				changed = true
			}
		}
		if oldFragmentID != "" && strings.TrimSpace(subject.AnchorFragmentID) == oldFragmentID && newFragmentID != "" {
			subject.AnchorFragmentID = newFragmentID
			changed = true
		}
		if !changed {
			continue
		}
		subject.UniversePaths = normalizeAndSortStrings(subject.UniversePaths)
		subject.ID = BuildWatchSubjectID(subject.TenantID, subject.ProjectID, subject.LensID, subject.PackID, subject.UserID, subject.SubjectKind, subject.AnchorCanonicalPath)
		if existing, err := r.watchSubjectsRepo.Get(ctx, subject.ID); err == nil && existing != nil && existing.ID != oldID {
			mergeWatchSubject(existing, subject)
			subject = existing
		}
		if err := r.watchSubjectsRepo.Upsert(ctx, subject); err != nil {
			continue
		}
		if oldID != "" && oldID != subject.ID {
			_ = r.watchSubjectsRepo.Delete(ctx, oldID)
		}
	}
}

func (r *FragmentsMergeRepository) rewriteOntologyFactsPaths(ctx context.Context, tenantID string, dup, primary *models.HorizonFragment) {
	if dup == nil || primary == nil {
		return
	}
	target := strings.TrimSpace(primary.CanonicalPath)
	if target == "" {
		return
	}
	r.rewriteOntologyFactsPathsForPaths(ctx, tenantID, collectFragmentMergePaths(dup, target), target)
}

func (r *FragmentsMergeRepository) rewriteOntologyFactsPathsForPaths(ctx context.Context, tenantID string, pathsToReplace []string, target string) {
	if r.ontologyFactsRepo == nil {
		return
	}
	target = strings.TrimSpace(target)
	pathsToReplace = normalizeAndSortStrings(pathsToReplace)
	if target == "" || len(pathsToReplace) == 0 {
		return
	}
	for _, oldPath := range pathsToReplace {
		if tenantID == "global" {
			_, _ = r.ontologyFactsRepo.ReplaceCanonicalPathReferences(ctx, "", oldPath, target, 2000)
			continue
		}
		_, _ = r.ontologyFactsRepo.ReplaceCanonicalPathReferences(ctx, tenantID, oldPath, target, 1500)
		_, _ = r.ontologyFactsRepo.ReplaceCanonicalPathReferences(ctx, "global", oldPath, target, 1500)
	}
}

func (r *FragmentsMergeRepository) rewriteOntologyEntitiesPaths(ctx context.Context, tenantID string, dup, primary *models.HorizonFragment) {
	if dup == nil || primary == nil {
		return
	}
	target := strings.TrimSpace(primary.CanonicalPath)
	if target == "" {
		return
	}
	r.rewriteOntologyEntitiesPathsForPaths(ctx, tenantID, collectFragmentMergePaths(dup, target), target)
}

func (r *FragmentsMergeRepository) rewriteOntologyEntitiesPathsForPaths(ctx context.Context, tenantID string, pathsToReplace []string, target string) {
	if r.entitiesRepo == nil {
		return
	}
	target = strings.TrimSpace(target)
	pathsToReplace = normalizeAndSortStrings(pathsToReplace)
	if target == "" || len(pathsToReplace) == 0 {
		return
	}
	for _, oldPath := range pathsToReplace {
		if tenantID == "global" {
			_, _ = r.entitiesRepo.ReplaceCanonicalPathReferences(ctx, "", oldPath, target, 2000)
			continue
		}
		_, _ = r.entitiesRepo.ReplaceCanonicalPathReferences(ctx, tenantID, oldPath, target, 1500)
		_, _ = r.entitiesRepo.ReplaceCanonicalPathReferences(ctx, "global", oldPath, target, 1500)
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

func collectFragmentMergePaths(fragment *models.HorizonFragment, exclude string) []string {
	if fragment == nil {
		return nil
	}
	paths := make([]string, 0, 1+len(fragment.AlternativePaths))
	if p := strings.TrimSpace(fragment.CanonicalPath); p != "" && p != exclude {
		paths = append(paths, p)
	}
	if merged := strings.TrimSpace(fragment.Identifiers["merged_paths"]); merged != "" {
		paths = append(paths, splitCommaTrimmed(merged)...)
	}
	paths = append(paths, fragment.AlternativePaths...)
	return filterExcludedPaths(paths, exclude)
}

func collectFragmentAlternativeNames(fragment *models.HorizonFragment) []string {
	if fragment == nil {
		return nil
	}
	out := make([]string, 0, len(fragment.AlternativeNames)+3)
	out = append(out, fragment.AlternativeNames...)
	if v := strings.TrimSpace(fragment.Identifiers["legalName"]); v != "" {
		out = append(out, v)
	}
	if v := strings.TrimSpace(fragment.Identifiers["shortName"]); v != "" {
		out = append(out, v)
	}
	if v := strings.TrimSpace(fragment.Labels["en"]); v != "" {
		out = append(out, v)
	}
	return normalizeAndSortStrings(out)
}

func normalizeAndSortStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func splitCommaTrimmed(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	out := make([]string, 0, 4)
	for _, item := range strings.Split(raw, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func filterExcludedPaths(values []string, exclude string) []string {
	if len(values) == 0 {
		return nil
	}
	exclude = strings.TrimSpace(exclude)
	out := make([]string, 0, len(values))
	for _, value := range normalizeAndSortStrings(values) {
		if value == exclude {
			continue
		}
		out = append(out, value)
	}
	return out
}

func replaceCanonicalPathInStringSlice(values []string, oldPath, newPath string) bool {
	oldPath = strings.TrimSpace(oldPath)
	newPath = strings.TrimSpace(newPath)
	if oldPath == "" || newPath == "" || oldPath == newPath {
		return false
	}
	changed := false
	for i, raw := range values {
		if strings.TrimSpace(raw) != oldPath {
			continue
		}
		values[i] = newPath
		changed = true
	}
	return changed
}

func replaceCanonicalPathInOptionalString(value *string, oldPath, newPath string) bool {
	if value == nil {
		return false
	}
	oldPath = strings.TrimSpace(oldPath)
	newPath = strings.TrimSpace(newPath)
	if oldPath == "" || newPath == "" || oldPath == newPath {
		return false
	}
	if strings.TrimSpace(*value) != oldPath {
		return false
	}
	*value = newPath
	return true
}

func replaceCanonicalPathInMetadata(metadata map[string]any, oldPath, newPath string) bool {
	if len(metadata) == 0 {
		return false
	}
	changed := false
	for key, value := range metadata {
		replaced, ok := replaceCanonicalPathInAny(value, oldPath, newPath)
		if !ok {
			continue
		}
		metadata[key] = replaced
		changed = true
	}
	return changed
}

func replaceCanonicalPathInAny(value any, oldPath, newPath string) (any, bool) {
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == strings.TrimSpace(oldPath) {
			return newPath, true
		}
		return value, false
	case []string:
		if !replaceCanonicalPathInStringSlice(typed, oldPath, newPath) {
			return value, false
		}
		return normalizeAndSortStrings(typed), true
	case []interface{}:
		changed := false
		for i := range typed {
			replaced, ok := replaceCanonicalPathInAny(typed[i], oldPath, newPath)
			if !ok {
				continue
			}
			typed[i] = replaced
			changed = true
		}
		return typed, changed
	case map[string]any:
		changed := false
		for key, nested := range typed {
			replaced, ok := replaceCanonicalPathInAny(nested, oldPath, newPath)
			if !ok {
				continue
			}
			typed[key] = replaced
			changed = true
		}
		return typed, changed
	default:
		return value, false
	}
}

func mergeWatchSubject(dst, src *models.WatchSubject) {
	if dst == nil || src == nil {
		return
	}
	if strings.TrimSpace(dst.DisplayLabel) == "" {
		dst.DisplayLabel = src.DisplayLabel
	}
	if strings.TrimSpace(dst.AnchorFragmentID) == "" {
		dst.AnchorFragmentID = src.AnchorFragmentID
	}
	if strings.TrimSpace(dst.ProjectID) == "" {
		dst.ProjectID = src.ProjectID
	}
	if strings.TrimSpace(dst.LensID) == "" {
		dst.LensID = src.LensID
	}
	if strings.TrimSpace(dst.PackID) == "" {
		dst.PackID = src.PackID
	}
	if strings.TrimSpace(dst.UserID) == "" {
		dst.UserID = src.UserID
	}
	if dst.SourceMode == "" {
		dst.SourceMode = src.SourceMode
	}
	if dst.RuntimeMode == "" {
		dst.RuntimeMode = src.RuntimeMode
	}
	if dst.Status == models.WatchSubjectStatusDeleted && src.Status != "" && src.Status != models.WatchSubjectStatusDeleted {
		dst.Status = src.Status
	}
	if src.Priority > 0 && (dst.Priority == 0 || src.Priority < dst.Priority) {
		dst.Priority = src.Priority
	}
	dst.DomainModes = normalizeAndSortStrings(append(dst.DomainModes, src.DomainModes...))
	dst.Roles = normalizeAndSortStrings(append(dst.Roles, src.Roles...))
	dst.LensBindings = normalizeAndSortStrings(append(dst.LensBindings, src.LensBindings...))
	dst.UniversePaths = normalizeAndSortStrings(append(dst.UniversePaths, src.UniversePaths...))
	if dst.Metadata == nil && src.Metadata != nil {
		dst.Metadata = map[string]any{}
	}
	for key, value := range src.Metadata {
		if _, exists := dst.Metadata[key]; exists {
			continue
		}
		dst.Metadata[key] = value
	}
	if dst.CreatedAt.IsZero() || (!src.CreatedAt.IsZero() && src.CreatedAt.Before(dst.CreatedAt)) {
		dst.CreatedAt = src.CreatedAt
	}
	if src.UpdatedAt.After(dst.UpdatedAt) {
		dst.UpdatedAt = src.UpdatedAt
	}
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
	mergedPaths := normalizeAndSortStrings(append(splitCommaTrimmed(target.Identifiers["merged_paths"]), collectFragmentMergePaths(source, strings.TrimSpace(target.CanonicalPath))...))
	mergedPathsJoined := strings.Join(mergedPaths, ",")
	if strings.TrimSpace(target.Identifiers["merged_paths"]) != mergedPathsJoined {
		target.Identifiers["merged_paths"] = mergedPathsJoined
		changed = true
	}
	targetAltPaths := normalizeAndSortStrings(append(target.AlternativePaths, collectFragmentMergePaths(source, strings.TrimSpace(target.CanonicalPath))...))
	if strings.Join(target.AlternativePaths, "\n") != strings.Join(targetAltPaths, "\n") {
		target.AlternativePaths = targetAltPaths
		changed = true
	}
	targetAltNames := normalizeAndSortStrings(append(target.AlternativeNames, collectFragmentAlternativeNames(source)...))
	if strings.Join(target.AlternativeNames, "\n") != strings.Join(targetAltNames, "\n") {
		target.AlternativeNames = targetAltNames
		changed = true
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
