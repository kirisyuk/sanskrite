package repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

const fragmentPathMigratorRedirectTTL = 7 * 24 * time.Hour

type FragmentPathMigrator struct {
	db                *firestore.Client
	fragmentsRepo     *HorizonFragmentsRepository
	redirectsRepo     *CanonicalPathRedirectsRepository
	mergeRepo         *FragmentsMergeRepository
	materializedRepo  *MaterializedArtifactsRepository
	lensesRepo        *LensesRepository
	lensArtifactsRepo *LensArtifactsRepository
	framesRepo        *FramesRepository
}

type FragmentPathMigrationResult struct {
	FragmentID           string
	OldPath              string
	NewPath              string
	MergedIntoExisting   bool
	MaterializedUpdated  int
	LensesUpdated        int
	LensArtifactsUpdated int
	FramesUpdated        int
	AgentJobsUpdated     int
}

func NewFragmentPathMigrator(
	db *firestore.Client,
	fragmentsRepo *HorizonFragmentsRepository,
	redirectsRepo *CanonicalPathRedirectsRepository,
) *FragmentPathMigrator {
	return &FragmentPathMigrator{
		db:            db,
		fragmentsRepo: fragmentsRepo,
		redirectsRepo: redirectsRepo,
	}
}

func (m *FragmentPathMigrator) WithMergeRepo(repo *FragmentsMergeRepository) *FragmentPathMigrator {
	m.mergeRepo = repo
	return m
}

func (m *FragmentPathMigrator) WithMaterializedRepo(repo *MaterializedArtifactsRepository) *FragmentPathMigrator {
	m.materializedRepo = repo
	return m
}

func (m *FragmentPathMigrator) WithLensesRepo(repo *LensesRepository) *FragmentPathMigrator {
	m.lensesRepo = repo
	return m
}

func (m *FragmentPathMigrator) WithLensArtifactsRepo(repo *LensArtifactsRepository) *FragmentPathMigrator {
	m.lensArtifactsRepo = repo
	return m
}

func (m *FragmentPathMigrator) WithFramesRepo(repo *FramesRepository) *FragmentPathMigrator {
	m.framesRepo = repo
	return m
}

func (m *FragmentPathMigrator) MoveCanonicalPath(ctx context.Context, fragmentID, targetPath, reason string) (*FragmentPathMigrationResult, error) {
	if m == nil || m.fragmentsRepo == nil {
		return nil, fmt.Errorf("fragment path migrator is not configured")
	}
	fragmentID = strings.TrimSpace(fragmentID)
	targetPath = strings.TrimSpace(targetPath)
	reason = strings.TrimSpace(reason)
	if fragmentID == "" || targetPath == "" {
		return nil, fmt.Errorf("fragmentID and targetPath are required")
	}

	frag, err := m.fragmentsRepo.Get(ctx, fragmentID)
	if err != nil || frag == nil {
		return nil, fmt.Errorf("fragment not found: %w", err)
	}
	oldPath := strings.TrimSpace(frag.CanonicalPath)
	if oldPath == "" {
		return nil, fmt.Errorf("fragment %s has empty canonicalPath", fragmentID)
	}
	if oldPath == targetPath {
		return &FragmentPathMigrationResult{FragmentID: fragmentID, OldPath: oldPath, NewPath: targetPath}, nil
	}

	if existing, err := m.fragmentsRepo.GetByCanonicalPath(ctx, targetPath); err == nil && existing != nil && strings.TrimSpace(existing.ID) != "" && strings.TrimSpace(existing.ID) != fragmentID {
		if m.mergeRepo == nil {
			return nil, fmt.Errorf("target path %s already exists (%s) and mergeRepo is not configured", targetPath, existing.ID)
		}
		_, err := m.mergeRepo.MergeFragmentWithRequest(ctx, MergeRequest{
			PrimaryFragmentID:   existing.ID,
			DuplicateFragmentID: fragmentID,
			TenantID:            "global",
			Source:              FragmentMergeAuditSource("path_migration"),
			ToolName:            "fragment_path_migrator",
		})
		if err != nil {
			return nil, err
		}
		return &FragmentPathMigrationResult{
			FragmentID:         fragmentID,
			OldPath:            oldPath,
			NewPath:            targetPath,
			MergedIntoExisting: true,
		}, nil
	}

	if frag.Identifiers == nil {
		frag.Identifiers = map[string]string{}
	}
	if !csvContainsPath(frag.Identifiers["merged_paths"], oldPath) {
		if strings.TrimSpace(frag.Identifiers["merged_paths"]) == "" {
			frag.Identifiers["merged_paths"] = oldPath
		} else {
			frag.Identifiers["merged_paths"] = strings.TrimSpace(frag.Identifiers["merged_paths"]) + "," + oldPath
		}
	}
	frag.CanonicalPath = targetPath
	if parts, err := taxonomy.ParseCanonicalPathParts(targetPath); err == nil && parts != nil {
		frag.CanonicalParts = parts
	} else {
		frag.CanonicalParts = nil
	}
	if err := m.fragmentsRepo.UpdateDerived(ctx, frag); err != nil {
		return nil, err
	}
	m.fragmentsRepo.invalidateFragmentRedis(ctx, oldPath)

	if m.redirectsRepo != nil {
		_, _, _, _ = m.redirectsRepo.RecordObservation(ctx, "global", oldPath, targetPath, firstNonEmptyReason(reason, "path_migration"), string(frag.Class), 1, fragmentPathMigratorRedirectTTL)
	}

	res := &FragmentPathMigrationResult{
		FragmentID: fragmentID,
		OldPath:    oldPath,
		NewPath:    targetPath,
	}
	res.MaterializedUpdated = m.rewriteMaterializedArtifacts(ctx, oldPath, targetPath)
	res.LensesUpdated = m.rewriteLensScopes(ctx, oldPath, targetPath, fragmentID)
	res.LensArtifactsUpdated = m.rewriteLensArtifacts(ctx, oldPath, targetPath)
	res.FramesUpdated = m.rewriteFrames(ctx, oldPath, targetPath)
	res.AgentJobsUpdated = m.rewriteAgentJobs(ctx, oldPath, targetPath)
	return res, nil
}

func (m *FragmentPathMigrator) rewriteMaterializedArtifacts(ctx context.Context, oldPath, newPath string) int {
	if m == nil || m.materializedRepo == nil {
		return 0
	}
	kinds := []models.MaterializedKind{
		models.MaterializedKindSupplyChainPath,
		models.MaterializedKindRegulatoryImpactMap,
		models.MaterializedKindRegulatoryImpactTransmissionPath,
		models.MaterializedKindRegulatoryTimeline,
		models.MaterializedKindRegulatoryCascadeMap,
		models.MaterializedKindRegulatoryImpactScenario,
		models.MaterializedKindGovernanceChain,
		models.MaterializedKindEcosystemMap,
	}
	updated := 0
	seen := make(map[string]bool)
	for _, kind := range kinds {
		for _, tenantID := range []string{"global"} {
			arts, err := m.materializedRepo.FindByNodeRef(ctx, tenantID, oldPath, kind)
			if err != nil {
				continue
			}
			for _, art := range arts {
				if art == nil || art.ID == "" || seen[art.ID] {
					continue
				}
				seen[art.ID] = true
				if models.ReplaceCanonicalPathInArtifact(art, oldPath, newPath) {
					if err := m.materializedRepo.Update(ctx, art); err == nil {
						updated++
					}
				}
			}
		}
	}
	return updated
}

func (m *FragmentPathMigrator) rewriteLensScopes(ctx context.Context, oldPath, newPath, fragmentID string) int {
	if m == nil || m.lensesRepo == nil {
		return 0
	}
	updated := 0
	lenses, err := m.lensesRepo.FindByScope(ctx, "global", oldPath, 500)
	if err != nil {
		return 0
	}
	for _, lens := range lenses {
		if lens == nil {
			continue
		}
		changed := false
		for i, p := range lens.Scope {
			if strings.TrimSpace(p) == oldPath {
				lens.Scope[i] = newPath
				changed = true
			}
		}
		if lens.ScopeFragments != nil {
			if fid, ok := lens.ScopeFragments[oldPath]; ok {
				delete(lens.ScopeFragments, oldPath)
				if strings.TrimSpace(fid) == "" {
					fid = fragmentID
				}
				if _, exists := lens.ScopeFragments[newPath]; !exists {
					lens.ScopeFragments[newPath] = fid
				}
				changed = true
			}
		}
		if lens.ResolvedQueries != nil {
			for key, paths := range lens.ResolvedQueries {
				localChanged := false
				for i, p := range paths {
					if strings.TrimSpace(p) == oldPath {
						paths[i] = newPath
						localChanged = true
					}
				}
				if localChanged {
					lens.ResolvedQueries[key] = dedupeStrings(paths)
					changed = true
				}
			}
		}
		if changed {
			lens.Scope = dedupeStrings(lens.Scope)
			if err := m.lensesRepo.Update(ctx, lens); err == nil {
				updated++
			}
		}
	}
	return updated
}

func (m *FragmentPathMigrator) rewriteLensArtifacts(ctx context.Context, oldPath, newPath string) int {
	if m == nil || m.lensArtifactsRepo == nil || m.db == nil {
		return 0
	}
	docs, err := m.lensArtifactsRepo.col.Where("canonicalPath", "==", oldPath).Documents(ctx).GetAll()
	if err != nil {
		return 0
	}
	updated := 0
	for _, doc := range docs {
		if doc == nil || !doc.Exists() {
			continue
		}
		_, err := m.lensArtifactsRepo.col.Doc(doc.Ref.ID).Update(ctx, []firestore.Update{
			{Path: "canonicalPath", Value: newPath},
			{Path: "updatedAt", Value: time.Now().UTC()},
		})
		if err == nil {
			updated++
		}
	}
	return updated
}

func (m *FragmentPathMigrator) rewriteFrames(ctx context.Context, oldPath, newPath string) int {
	if m == nil || m.framesRepo == nil || m.db == nil {
		return 0
	}
	frames, err := m.framesRepo.ListByTargetCanonicalPath(ctx, "global", oldPath, 2000)
	if err != nil {
		return 0
	}
	updated := 0
	for _, f := range frames {
		if f == nil || strings.TrimSpace(f.ID) == "" {
			continue
		}
		_, err := m.db.Collection("horizon_frames").Doc(f.ID).Update(ctx, []firestore.Update{
			{Path: "targetCanonicalPath", Value: newPath},
		})
		if err == nil {
			updated++
		}
	}
	return updated
}

func (m *FragmentPathMigrator) rewriteAgentJobs(ctx context.Context, oldPath, newPath string) int {
	if m == nil || m.db == nil {
		return 0
	}
	docs, err := m.db.Collection("agent_jobs").
		Where("tenantId", "==", "global").
		Where("state.targetCanonicalPath", "==", oldPath).
		Limit(2000).
		Documents(ctx).GetAll()
	if err != nil {
		return 0
	}
	updated := 0
	for _, doc := range docs {
		if doc == nil || !doc.Exists() {
			continue
		}
		updates := []firestore.Update{
			{Path: "state.targetCanonicalPath", Value: newPath},
			{Path: "updatedAt", Value: time.Now().UTC()},
		}
		var job coremodels.AgentJob
		if err := doc.DataTo(&job); err == nil {
			if _, ok := job.State["aliasCanonicalPath"]; !ok {
				updates = append(updates, firestore.Update{Path: "state.aliasCanonicalPath", Value: oldPath})
			}
		}
		if _, err := m.db.Collection("agent_jobs").Doc(doc.Ref.ID).Update(ctx, updates); err == nil {
			updated++
		}
	}
	return updated
}

func csvContainsPath(csv, path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	for _, part := range strings.Split(strings.TrimSpace(csv), ",") {
		if strings.TrimSpace(part) == path {
			return true
		}
	}
	return false
}

func dedupeStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
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
	return out
}

func firstNonEmptyReason(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return "path_migration"
}
