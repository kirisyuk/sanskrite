package analytical

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"log"
	"sort"
	"strings"
	"time"

	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	redis "github.com/redis/go-redis/v9"
)

const (
	defaultProjectionCacheTTL = 45 * time.Minute
	defaultAggregateCacheTTL  = 10 * time.Minute
	defaultDirtySetTTL        = 10 * time.Minute
	defaultWorkerInterval     = 2 * time.Second
	defaultRefreshTimeout     = 45 * time.Second
)

type ProjectionService struct {
	refScopeRepo            *hrepo.RefScopeProjectionsRepository
	pathFamilyRepo          *hrepo.PathFamilyProjectionsRepository
	compiledWatchRepo       *hrepo.CompiledWatchSubjectsRepository
	watchSubjectsRepo       *hrepo.WatchSubjectsRepository
	aggregatesRepo          *hrepo.AnalyticalAggregatesRepository
	redisClient             *redis.Client
}

func NewProjectionService(
	refScopeRepo *hrepo.RefScopeProjectionsRepository,
	pathFamilyRepo *hrepo.PathFamilyProjectionsRepository,
	compiledWatchRepo *hrepo.CompiledWatchSubjectsRepository,
	watchSubjectsRepo *hrepo.WatchSubjectsRepository,
	aggregatesRepo *hrepo.AnalyticalAggregatesRepository,
) *ProjectionService {
	return &ProjectionService{
		refScopeRepo:      refScopeRepo,
		pathFamilyRepo:    pathFamilyRepo,
		compiledWatchRepo: compiledWatchRepo,
		watchSubjectsRepo: watchSubjectsRepo,
		aggregatesRepo:    aggregatesRepo,
	}
}

func (s *ProjectionService) WithRedis(client *redis.Client) *ProjectionService {
	if s == nil {
		return s
	}
	s.redisClient = client
	return s
}

func (s *ProjectionService) ResolveAffectedScopes(ctx context.Context, tenantID string, refs []string) (*hmodels.AffectedScopeDelta, error) {
	tenantID = strings.TrimSpace(firstNonEmpty(tenantID, "global"))
	refs = compactStrings(refs)
	delta := &hmodels.AffectedScopeDelta{
		TenantID:    tenantID,
		ChangedRefs: refs,
		ComputedAt:  time.Now().UTC(),
	}
	if s == nil || len(refs) == 0 {
		delta.Normalize()
		return delta, nil
	}
	lensSet := map[string]struct{}{}
	watchSet := map[string]struct{}{}
	familySet := map[string]struct{}{}
	scopeSet := map[string]struct{}{}
	fragmentSet := map[string]struct{}{}
	for _, ref := range refs {
		fragmentSet[ref] = struct{}{}
		projection := s.readProjectionCache(ctx, tenantID, ref)
		if projection == nil && s.refScopeRepo != nil {
			item, err := s.refScopeRepo.Get(ctx, tenantID, ref)
			if err == nil && item != nil {
				projection = item
				s.writeProjectionCache(ctx, item)
			}
		}
		if projection != nil {
			for _, lensID := range projection.LensIDs {
				lensSet[strings.TrimSpace(lensID)] = struct{}{}
			}
			for _, subjectID := range projection.WatchSubjectIDs {
				watchSet[strings.TrimSpace(subjectID)] = struct{}{}
			}
			for _, familyID := range projection.PathFamilyIDs {
				familySet[strings.TrimSpace(familyID)] = struct{}{}
			}
			for _, scopeRef := range projection.AggregateScopeRefs {
				scopeSet[strings.TrimSpace(scopeRef)] = struct{}{}
			}
		}
	}
	delta.FragmentRefs = sortedSetKeys(fragmentSet)
	delta.LensIDs = sortedSetKeys(lensSet)
	delta.WatchSubjectIDs = sortedSetKeys(watchSet)
	delta.PathFamilyIDs = sortedSetKeys(familySet)
	delta.AggregateScopeRefs = sortedSetKeys(scopeSet)
	delta.Normalize()
	return delta, nil
}

func CompileWatchSubject(subject *hmodels.WatchSubject, pathFamilyIDs []string, aggregateScopeRefs []string) *hmodels.CompiledWatchSubject {
	if subject == nil {
		return nil
	}
	compiledRefs := compactStrings(append([]string{strings.TrimSpace(subject.AnchorCanonicalPath)}, subject.UniversePaths...))
	compiled := &hmodels.CompiledWatchSubject{
		WatchSubjectID:             strings.TrimSpace(subject.ID),
		TenantID:                   strings.TrimSpace(firstNonEmpty(subject.TenantID, "global")),
		AnchorRef:                  strings.TrimSpace(subject.AnchorCanonicalPath),
		LensIDs:                    compactStrings(append([]string{strings.TrimSpace(subject.LensID)}, subject.LensBindings...)),
		CompiledRefSet:             compiledRefs,
		CompiledPathFamilyIDs:      compactStrings(pathFamilyIDs),
		CompiledAggregateScopeRefs: compactStrings(aggregateScopeRefs),
		RuntimeMode:                subject.RuntimeMode,
		Priority:                   subject.Priority,
		BasisDigest:                runtimeDigest(append(append([]string{}, compiledRefs...), pathFamilyIDs...)...),
		UpdatedAt:                  time.Now().UTC(),
	}
	compiled.Normalize()
	return compiled
}

func PersistCompiledWatchSubject(
	ctx context.Context,
	compiledRepo *hrepo.CompiledWatchSubjectsRepository,
	refScopeRepo *hrepo.RefScopeProjectionsRepository,
	subject *hmodels.WatchSubject,
	pathFamilyIDs []string,
	aggregateScopeRefs []string,
) (*hmodels.CompiledWatchSubject, error) {
	if compiledRepo == nil || subject == nil {
		return nil, nil
	}
	compiled := CompileWatchSubject(subject, pathFamilyIDs, aggregateScopeRefs)
	if compiled == nil {
		return nil, nil
	}
	if err := compiledRepo.Upsert(ctx, compiled); err != nil {
		return nil, err
	}
	if refScopeRepo != nil {
		for _, ref := range compiled.CompiledRefSet {
			if err := UpsertRefScopeProjection(ctx, refScopeRepo, RefScopeProjectionInput{
				TenantID:           compiled.TenantID,
				Ref:                ref,
				WatchSubjectIDs:    []string{compiled.WatchSubjectID},
				LensIDs:            compiled.LensIDs,
				PathFamilyIDs:      compiled.CompiledPathFamilyIDs,
				AggregateScopeRefs: compiled.CompiledAggregateScopeRefs,
			}); err != nil {
				return compiled, err
			}
		}
	}
	return compiled, nil
}

type RefScopeProjectionInput struct {
	TenantID               string
	Ref                    string
	LensIDs                []string
	WatchSubjectIDs        []string
	PathFamilyIDs          []string
	AggregateScopeRefs     []string
	MaterializedArtifactKeys []string
}

func UpsertRefScopeProjection(ctx context.Context, repo *hrepo.RefScopeProjectionsRepository, in RefScopeProjectionInput) error {
	if repo == nil {
		return nil
	}
	tenantID := strings.TrimSpace(firstNonEmpty(in.TenantID, "global"))
	ref := strings.TrimSpace(in.Ref)
	if ref == "" {
		return nil
	}
	existing, _ := repo.Get(ctx, tenantID, ref)
	projection := &hmodels.RefScopeProjection{
		TenantID:                 tenantID,
		Ref:                      ref,
		LensIDs:                  compactStrings(in.LensIDs),
		WatchSubjectIDs:          compactStrings(in.WatchSubjectIDs),
		PathFamilyIDs:            compactStrings(in.PathFamilyIDs),
		AggregateScopeRefs:       compactStrings(in.AggregateScopeRefs),
		MaterializedArtifactKeys: compactStrings(in.MaterializedArtifactKeys),
		UpdatedAt:                time.Now().UTC(),
	}
	if existing != nil {
		projection.LensIDs = compactStrings(append(existing.LensIDs, projection.LensIDs...))
		projection.WatchSubjectIDs = compactStrings(append(existing.WatchSubjectIDs, projection.WatchSubjectIDs...))
		projection.PathFamilyIDs = compactStrings(append(existing.PathFamilyIDs, projection.PathFamilyIDs...))
		projection.AggregateScopeRefs = compactStrings(append(existing.AggregateScopeRefs, projection.AggregateScopeRefs...))
		projection.MaterializedArtifactKeys = compactStrings(append(existing.MaterializedArtifactKeys, projection.MaterializedArtifactKeys...))
	}
	projection.BasisDigest = runtimeDigest(
		append(
			append(
				append([]string{projection.Ref}, projection.LensIDs...),
				projection.WatchSubjectIDs...),
			projection.PathFamilyIDs...,
		)...,
	)
	return repo.Upsert(ctx, projection)
}

type ArtifactProjectionInput struct {
	Artifact       *hmodels.MaterializedArtifact
	AggregateTypes []string
}

func PersistArtifactProjection(
	ctx context.Context,
	refScopeRepo *hrepo.RefScopeProjectionsRepository,
	pathFamilyRepo *hrepo.PathFamilyProjectionsRepository,
	in ArtifactProjectionInput,
) error {
	artifact := in.Artifact
	if artifact == nil {
		return nil
	}
	refs := compactStrings(append(append(append([]string{}, artifact.NodeRefs...), artifact.ZoneRefs...), artifact.AffectedZoneRefs...))
	aggregateScopeRefs := compactStrings(append([]string{strings.TrimSpace(artifact.CanonicalKey)}, in.AggregateTypes...))
	for _, ref := range refs {
		if err := UpsertRefScopeProjection(ctx, refScopeRepo, RefScopeProjectionInput{
			TenantID:                 strings.TrimSpace(firstNonEmpty(artifact.TenantID, "global")),
			Ref:                      ref,
			LensIDs:                  compactStrings([]string{artifact.LensID}),
			PathFamilyIDs:            compactStrings([]string{artifact.FamilyID}),
			AggregateScopeRefs:       aggregateScopeRefs,
			MaterializedArtifactKeys: compactStrings([]string{artifact.CanonicalKey}),
		}); err != nil {
			return err
		}
	}
	if pathFamilyRepo != nil && strings.TrimSpace(artifact.FamilyID) != "" {
		existing, _ := pathFamilyRepo.Get(ctx, strings.TrimSpace(firstNonEmpty(artifact.TenantID, "global")), strings.TrimSpace(artifact.FamilyID))
		projection := &hmodels.PathFamilyProjection{
			TenantID:           strings.TrimSpace(firstNonEmpty(artifact.TenantID, "global")),
			FamilyID:           strings.TrimSpace(artifact.FamilyID),
			LensIDs:            compactStrings([]string{artifact.LensID}),
			ArtifactKeys:       compactStrings([]string{artifact.CanonicalKey}),
			NodeRefs:           compactStrings(artifact.NodeRefs),
			RouteRefs:          compactStrings(artifact.FlowRefs),
			ZoneRefs:           compactStrings(append(append([]string{}, artifact.ZoneRefs...), artifact.AffectedZoneRefs...)),
			AggregateScopeRefs: aggregateScopeRefs,
			UpdatedAt:          time.Now().UTC(),
		}
		if existing != nil {
			projection.LensIDs = compactStrings(append(existing.LensIDs, projection.LensIDs...))
			projection.ArtifactKeys = compactStrings(append(existing.ArtifactKeys, projection.ArtifactKeys...))
			projection.NodeRefs = compactStrings(append(existing.NodeRefs, projection.NodeRefs...))
			projection.RouteRefs = compactStrings(append(existing.RouteRefs, projection.RouteRefs...))
			projection.ZoneRefs = compactStrings(append(existing.ZoneRefs, projection.ZoneRefs...))
			projection.AggregateScopeRefs = compactStrings(append(existing.AggregateScopeRefs, projection.AggregateScopeRefs...))
		}
		projection.BasisDigest = runtimeDigest(append([]string{projection.FamilyID}, projection.ArtifactKeys...)...)
		if err := pathFamilyRepo.Upsert(ctx, projection); err != nil {
			return err
		}
	}
	return nil
}

type RuntimeRefreshDeps struct {
	FragmentsRepo       *hrepo.HorizonFragmentsRepository
	LensesRepo          *hrepo.LensesRepository
	MaterializedRepo    *hrepo.MaterializedArtifactsRepository
	WatchSubjectsRepo   *hrepo.WatchSubjectsRepository
	PrimitivesRepo      *hrepo.AnalyticalPrimitivesRepository
	AggregatesRepo      *hrepo.AnalyticalAggregatesRepository
	ProjectionService   *ProjectionService
	RedisClient         *redis.Client
}

type RuntimeWorker struct {
	projectionService *ProjectionService
	deps              RuntimeRefreshDeps
	interval          time.Duration
	logger            *log.Logger
}

func NewRuntimeWorker(projectionService *ProjectionService, deps RuntimeRefreshDeps) *RuntimeWorker {
	return &RuntimeWorker{
		projectionService: projectionService,
		deps:              deps,
		interval:          defaultWorkerInterval,
	}
}

func (w *RuntimeWorker) WithInterval(interval time.Duration) *RuntimeWorker {
	if w == nil {
		return w
	}
	if interval > 0 {
		w.interval = interval
	}
	return w
}

func (w *RuntimeWorker) WithLogger(logger *log.Logger) *RuntimeWorker {
	if w == nil {
		return w
	}
	w.logger = logger
	return w
}

func (w *RuntimeWorker) Start(ctx context.Context) {
	if w == nil || w.deps.RedisClient == nil || w.projectionService == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.runOnce(ctx)
			}
		}
	}()
}

func (w *RuntimeWorker) runOnce(ctx context.Context) {
	tenants, err := queuedTenants(ctx, w.deps.RedisClient)
	if err != nil || len(tenants) == 0 {
		return
	}
	for _, tenantID := range tenants {
		refs, drainErr := DrainChangedRefs(ctx, w.deps.RedisClient, tenantID)
		if drainErr != nil || len(refs) == 0 {
			_ = removeQueuedTenant(ctx, w.deps.RedisClient, tenantID)
			continue
		}
		runCtx, cancel := context.WithTimeout(ctx, defaultRefreshTimeout)
		delta, resolveErr := w.projectionService.ResolveAffectedScopes(runCtx, tenantID, refs)
		if resolveErr != nil {
			cancel()
			w.logf("projection resolve failed tenant=%s refs=%d err=%v", tenantID, len(refs), resolveErr)
			continue
		}
		refreshResult, refreshErr := RefreshForDelta(runCtx, w.deps, delta, time.Now().UTC())
		cancel()
		if refreshErr != nil {
			w.logf("runtime refresh failed tenant=%s refs=%d err=%v", tenantID, len(refs), refreshErr)
			continue
		}
		w.logf(
			"runtime refresh tenant=%s refs=%d fragments=%d pathFamilies=%d lenses=%d watchSubjects=%d primitives=%d",
			tenantID,
			len(refs),
			len(refreshResult.UpdatedFragmentScopes),
			len(refreshResult.UpdatedPathFamilyIDs),
			len(refreshResult.UpdatedLensIDs),
			len(refreshResult.UpdatedWatchSubjectIDs),
			refreshResult.UpdatedPrimitiveCount,
		)
		_ = removeQueuedTenant(ctx, w.deps.RedisClient, tenantID)
	}
}

func (w *RuntimeWorker) logf(format string, args ...interface{}) {
	if w == nil {
		return
	}
	if w.logger != nil {
		w.logger.Printf(format, args...)
		return
	}
	log.Printf("[AnalyticalRuntime] "+format, args...)
}

type RuntimeRefreshResult struct {
	Delta                  *hmodels.AffectedScopeDelta
	UpdatedPrimitiveCount  int
	UpdatedFragmentScopes  []string
	UpdatedPathFamilyIDs   []string
	UpdatedLensIDs         []string
	UpdatedWatchSubjectIDs []string
}

func RefreshForDelta(ctx context.Context, deps RuntimeRefreshDeps, delta *hmodels.AffectedScopeDelta, computedAt time.Time) (*RuntimeRefreshResult, error) {
	if delta == nil {
		return &RuntimeRefreshResult{}, nil
	}
	delta.Normalize()
	if len(delta.ChangedRefs) == 0 {
		return &RuntimeRefreshResult{Delta: delta}, nil
	}
	now := computedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}

	refresh := &RuntimeRefreshResult{Delta: delta}
	if err := refreshDeltaPrimitives(ctx, deps, delta, now, refresh); err != nil {
		return nil, err
	}
	if err := refreshFragmentAggregates(ctx, deps, delta, now, refresh); err != nil {
		return nil, err
	}
	if err := refreshPathFamilyAggregates(ctx, deps, delta, now, refresh); err != nil {
		return nil, err
	}
	if err := refreshWatchSubjectAggregatesForDelta(ctx, deps, delta, now, refresh); err != nil {
		return nil, err
	}
	if err := refreshLensAggregatesForDelta(ctx, deps, delta, now, refresh); err != nil {
		return nil, err
	}
	result := &RuntimeRefreshResult{
		Delta:                  delta,
		UpdatedPrimitiveCount:  refresh.UpdatedPrimitiveCount,
		UpdatedFragmentScopes:  compactStrings(refresh.UpdatedFragmentScopes),
		UpdatedPathFamilyIDs:   compactStrings(refresh.UpdatedPathFamilyIDs),
		UpdatedLensIDs:         compactStrings(refresh.UpdatedLensIDs),
		UpdatedWatchSubjectIDs: compactStrings(refresh.UpdatedWatchSubjectIDs),
	}
	if deps.RedisClient != nil {
		writeAggregateScopeCache(ctx, deps.RedisClient, delta.TenantID, result.UpdatedFragmentScopes, defaultAggregateCacheTTL)
		writeAggregateScopeCache(ctx, deps.RedisClient, delta.TenantID, result.UpdatedPathFamilyIDs, defaultAggregateCacheTTL)
		writeAggregateScopeCache(ctx, deps.RedisClient, delta.TenantID, result.UpdatedLensIDs, defaultAggregateCacheTTL)
		writeAggregateScopeCache(ctx, deps.RedisClient, delta.TenantID, result.UpdatedWatchSubjectIDs, defaultAggregateCacheTTL)
	}
	return result, nil
}

func refreshDeltaPrimitives(ctx context.Context, deps RuntimeRefreshDeps, delta *hmodels.AffectedScopeDelta, now time.Time, refresh *RuntimeRefreshResult) error {
	if deps.PrimitivesRepo == nil || deps.FragmentsRepo == nil || refresh == nil {
		return nil
	}
	fragments, err := deps.FragmentsRepo.BatchGetByCanonicalPathsForTenant(ctx, delta.TenantID, delta.ChangedRefs)
	if err != nil {
		return err
	}
	for _, ref := range delta.ChangedRefs {
		fragment := fragments[strings.TrimSpace(ref)]
		if fragment == nil {
			continue
		}
		primitives := BuildFragmentStatePrimitives(fragment, now)
		for _, primitive := range primitives {
			if primitive == nil {
				continue
			}
			if err := deps.PrimitivesRepo.Upsert(ctx, primitive); err != nil {
				return err
			}
			refresh.UpdatedPrimitiveCount++
		}
	}
	return nil
}

func refreshFragmentAggregates(ctx context.Context, deps RuntimeRefreshDeps, delta *hmodels.AffectedScopeDelta, now time.Time, refresh *RuntimeRefreshResult) error {
	if deps.PrimitivesRepo == nil || deps.AggregatesRepo == nil || refresh == nil {
		return nil
	}
	for _, ref := range delta.ChangedRefs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		primitives, err := deps.PrimitivesRepo.ListByImpactedRefs(ctx, delta.TenantID, []string{ref}, 160)
		if err != nil {
			return err
		}
		if len(primitives) == 0 {
			continue
		}
		if _, err := PersistScopeAggregates(ctx, deps.AggregatesRepo, AggregateScopeInput{
			TenantID:     delta.TenantID,
			ScopeType:    "fragment",
			ScopeRef:     ref,
			Layer:        "baseline",
			SourceClass:  "delta_fragment_refresh",
			SubjectRef:   ref,
			RefreshClass: "metric_driven",
			Primitives:   primitives,
			ComputedAt:   now,
		}); err != nil {
			return err
		}
		refresh.UpdatedFragmentScopes = append(refresh.UpdatedFragmentScopes, ref)
	}
	return nil
}

func refreshPathFamilyAggregates(ctx context.Context, deps RuntimeRefreshDeps, delta *hmodels.AffectedScopeDelta, now time.Time, refresh *RuntimeRefreshResult) error {
	if deps.PrimitivesRepo == nil || deps.AggregatesRepo == nil || deps.ProjectionService == nil || deps.ProjectionService.pathFamilyRepo == nil || refresh == nil {
		return nil
	}
	for _, familyID := range delta.PathFamilyIDs {
		familyID = strings.TrimSpace(familyID)
		if familyID == "" {
			continue
		}
		projection, err := deps.ProjectionService.pathFamilyRepo.Get(ctx, delta.TenantID, familyID)
		if err != nil || projection == nil {
			continue
		}
		impactedRefs := compactStrings(append(append([]string{}, projection.NodeRefs...), projection.ZoneRefs...))
		if len(impactedRefs) == 0 {
			continue
		}
		primitives, err := deps.PrimitivesRepo.ListByImpactedRefs(ctx, delta.TenantID, impactedRefs, 320)
		if err != nil {
			return err
		}
		if len(primitives) == 0 {
			continue
		}
		if _, err := PersistScopeAggregates(ctx, deps.AggregatesRepo, AggregateScopeInput{
			TenantID:     delta.TenantID,
			ScopeType:    "path_family",
			ScopeRef:     familyID,
			Layer:        "baseline",
			SourceClass:  "delta_path_family_refresh",
			SubjectRef:   familyID,
			LensRefs:     projection.LensIDs,
			RefreshClass: "metric_driven",
			Primitives:   primitives,
			ComputedAt:   now,
		}); err != nil {
			return err
		}
		refresh.UpdatedPathFamilyIDs = append(refresh.UpdatedPathFamilyIDs, familyID)
	}
	return nil
}

func refreshWatchSubjectAggregatesForDelta(ctx context.Context, deps RuntimeRefreshDeps, delta *hmodels.AffectedScopeDelta, now time.Time, refresh *RuntimeRefreshResult) error {
	if deps.PrimitivesRepo == nil || deps.AggregatesRepo == nil || deps.ProjectionService == nil || deps.ProjectionService.compiledWatchRepo == nil || refresh == nil {
		return nil
	}
	for _, subjectID := range delta.WatchSubjectIDs {
		subjectID = strings.TrimSpace(subjectID)
		if subjectID == "" {
			continue
		}
		compiled, err := deps.ProjectionService.compiledWatchRepo.Get(ctx, subjectID)
		if err != nil || compiled == nil {
			continue
		}
		impactedRefs := compiledRefsForHotRefresh(compiled, deps.ProjectionService.pathFamilyRepo, ctx)
		if len(impactedRefs) == 0 {
			continue
		}
		primitives, err := deps.PrimitivesRepo.ListByImpactedRefs(ctx, delta.TenantID, impactedRefs, 320)
		if err != nil {
			return err
		}
		if len(primitives) == 0 {
			continue
		}
		if _, err := PersistScopeAggregates(ctx, deps.AggregatesRepo, AggregateScopeInput{
			TenantID:        delta.TenantID,
			ScopeType:       "watch_subject",
			ScopeRef:        subjectID,
			Layer:           "baseline",
			SourceClass:     "delta_watch_refresh",
			SubjectRef:      strings.TrimSpace(firstNonEmpty(compiled.AnchorRef, subjectID)),
			LensRefs:        compiled.LensIDs,
			WatchSubjectIDs: []string{subjectID},
			RefreshClass:    "metric_driven",
			Primitives:      primitives,
			ComputedAt:      now,
		}); err != nil {
			return err
		}
		refresh.UpdatedWatchSubjectIDs = append(refresh.UpdatedWatchSubjectIDs, subjectID)
	}
	return nil
}

func refreshLensAggregatesForDelta(ctx context.Context, deps RuntimeRefreshDeps, delta *hmodels.AffectedScopeDelta, now time.Time, refresh *RuntimeRefreshResult) error {
	if deps.PrimitivesRepo == nil || deps.AggregatesRepo == nil || deps.LensesRepo == nil || refresh == nil {
		return nil
	}
	for _, lensID := range delta.LensIDs {
		lensID = strings.TrimSpace(lensID)
		if lensID == "" {
			continue
		}
		lens, err := deps.LensesRepo.Get(ctx, lensID)
		if err != nil || lens == nil {
			continue
		}
		impactedRefs := hotLensRefs(ctx, deps, delta, lensID, lens)
		if len(impactedRefs) == 0 {
			continue
		}
		primitives, err := deps.PrimitivesRepo.ListByImpactedRefs(ctx, strings.TrimSpace(firstNonEmpty(lens.TenantID, delta.TenantID, "global")), impactedRefs, 480)
		if err != nil {
			return err
		}
		if len(primitives) == 0 {
			continue
		}
		if _, err := PersistScopeAggregates(ctx, deps.AggregatesRepo, AggregateScopeInput{
			TenantID:     strings.TrimSpace(firstNonEmpty(lens.TenantID, delta.TenantID, "global")),
			ScopeType:    "lens",
			ScopeRef:     lensID,
			Layer:        "baseline",
			SourceClass:  "delta_lens_refresh",
			SubjectRef:   strings.TrimSpace(firstNonEmpty(lens.Name, lens.ID)),
			LensRefs:     []string{lensID},
			RefreshClass: "metric_driven",
			Primitives:   primitives,
			ComputedAt:   now,
		}); err != nil {
			return err
		}
		refresh.UpdatedLensIDs = append(refresh.UpdatedLensIDs, lensID)
	}
	return nil
}

func compiledRefsForHotRefresh(compiled *hmodels.CompiledWatchSubject, pathFamilyRepo *hrepo.PathFamilyProjectionsRepository, ctx context.Context) []string {
	if compiled == nil {
		return nil
	}
	refs := append([]string{}, compiled.CompiledRefSet...)
	if pathFamilyRepo != nil {
		for _, familyID := range compiled.CompiledPathFamilyIDs {
			projection, err := pathFamilyRepo.Get(ctx, compiled.TenantID, familyID)
			if err != nil || projection == nil {
				continue
			}
			refs = append(refs, projection.NodeRefs...)
			refs = append(refs, projection.ZoneRefs...)
		}
	}
	return compactStrings(refs)
}

func hotLensRefs(ctx context.Context, deps RuntimeRefreshDeps, delta *hmodels.AffectedScopeDelta, lensID string, lens *hmodels.Lens) []string {
	refs := lensAggregateRefs(lens.Scope)
	if deps.ProjectionService != nil && deps.ProjectionService.pathFamilyRepo != nil {
		for _, familyID := range delta.PathFamilyIDs {
			projection, err := deps.ProjectionService.pathFamilyRepo.Get(ctx, strings.TrimSpace(firstNonEmpty(lens.TenantID, delta.TenantID, "global")), familyID)
			if err != nil || projection == nil || !containsString(projection.LensIDs, lensID) {
				continue
			}
			refs = append(refs, projection.NodeRefs...)
			refs = append(refs, projection.ZoneRefs...)
		}
	}
	if deps.MaterializedRepo != nil {
		if artifacts, err := deps.MaterializedRepo.ListByLens(ctx, lensID, 240); err == nil {
			refs = append(refs, lensAggregateArtifactRefs(artifacts)...)
		}
	}
	return compactStrings(refs)
}

func containsString(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}
	return false
}

func EnqueueChangedRefs(ctx context.Context, client *redis.Client, tenantID string, refs []string, ttl time.Duration) error {
	if client == nil {
		return nil
	}
	tenantID = strings.TrimSpace(firstNonEmpty(tenantID, "global"))
	refs = compactStrings(refs)
	if len(refs) == 0 {
		return nil
	}
	if ttl <= 0 {
		ttl = defaultDirtySetTTL
	}
	key := "hzn:delta:dirty_refs:" + tenantID
	values := make([]interface{}, 0, len(refs))
	for _, ref := range refs {
		values = append(values, ref)
	}
	if err := client.SAdd(ctx, key, values...).Err(); err != nil {
		return err
	}
	if err := client.Expire(ctx, key, ttl).Err(); err != nil {
		return err
	}
	if err := client.SAdd(ctx, "hzn:delta:queue", tenantID).Err(); err != nil {
		return err
	}
	return client.Expire(ctx, "hzn:delta:queue", ttl).Err()
}

func DrainChangedRefs(ctx context.Context, client *redis.Client, tenantID string) ([]string, error) {
	if client == nil {
		return nil, nil
	}
	key := "hzn:delta:dirty_refs:" + strings.TrimSpace(firstNonEmpty(tenantID, "global"))
	refs, err := client.SMembers(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		return nil, nil
	}
	_ = client.Del(ctx, key).Err()
	return compactStrings(refs), nil
}

func queuedTenants(ctx context.Context, client *redis.Client) ([]string, error) {
	if client == nil {
		return nil, nil
	}
	items, err := client.SMembers(ctx, "hzn:delta:queue").Result()
	if err != nil {
		return nil, err
	}
	return compactStrings(items), nil
}

func removeQueuedTenant(ctx context.Context, client *redis.Client, tenantID string) error {
	if client == nil || strings.TrimSpace(tenantID) == "" {
		return nil
	}
	return client.SRem(ctx, "hzn:delta:queue", strings.TrimSpace(tenantID)).Err()
}

func (s *ProjectionService) readProjectionCache(ctx context.Context, tenantID, ref string) *hmodels.RefScopeProjection {
	if s == nil || s.redisClient == nil {
		return nil
	}
	payload, err := s.redisClient.Get(ctx, "hzn:projection:ref:"+tenantID+":"+cacheDigest(ref)).Bytes()
	if err != nil || len(payload) == 0 {
		return nil
	}
	var projection hmodels.RefScopeProjection
	if err := json.Unmarshal(payload, &projection); err != nil {
		return nil
	}
	return &projection
}

func (s *ProjectionService) writeProjectionCache(ctx context.Context, projection *hmodels.RefScopeProjection) {
	if s == nil || s.redisClient == nil || projection == nil {
		return
	}
	payload, err := json.Marshal(projection)
	if err != nil {
		return
	}
	_ = s.redisClient.Set(ctx, "hzn:projection:ref:"+projection.TenantID+":"+cacheDigest(projection.Ref), payload, defaultProjectionCacheTTL).Err()
}

func writeAggregateScopeCache(ctx context.Context, client *redis.Client, tenantID string, scopeRefs []string, ttl time.Duration) {
	if client == nil {
		return
	}
	if ttl <= 0 {
		ttl = defaultAggregateCacheTTL
	}
	for _, scopeRef := range compactStrings(scopeRefs) {
		_ = client.Set(ctx, "hzn:aggregate:scope:"+tenantID+":"+cacheDigest(scopeRef), time.Now().UTC().Format(time.RFC3339), ttl).Err()
	}
}

func runtimeDigest(parts ...string) string {
	sum := sha1.Sum([]byte(strings.Join(compactStrings(parts), "\n")))
	return hex.EncodeToString(sum[:10])
}

func cacheDigest(value string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:10])
}

func sortedSetKeys(in map[string]struct{}) []string {
	out := make([]string, 0, len(in))
	for key := range in {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
