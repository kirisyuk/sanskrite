package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/cache"
	horizonanalytical "github.com/igorkirisyuk/sanskrite/internal/horizon/analytical"
	horizoncriticalpath "github.com/igorkirisyuk/sanskrite/internal/horizon/critical_path"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonpolicy "github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
	horizonprop "github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
)

func main() {
	var (
		projectID         = flag.String("project", "nv-horizon", "GCP project id")
		tenantID          = flag.String("tenant", "global", "Tenant id")
		canonicalPath     = flag.String("path", "/org/byd", "canonical path to propagate from (e.g., /org/byd)")
		metricName        = flag.String("metric", "/metric/risk/supply_chain_criticality", "metric name to change")
		delta             = flag.Float64("delta", 0.1, "delta to apply to the source metric before propagation")
		enableRealtime    = flag.Bool("realtime", true, "Publish propagation events to realtime Pub/Sub")
		refreshAnalytical = flag.Bool("refresh-analytical", true, "Persist fragment-backed analytical primitives and refresh impacted aggregates")
		hotOnly           = flag.Bool("hot-only", true, "Use projection-backed hot-path analytical refresh")
		skipCorpus        = flag.Bool("skip-corpus", true, "Do not trigger corpus rebuild side effects")
		skipSimulation    = flag.Bool("skip-simulation", true, "Do not trigger scenario simulation side effects")
		projectionSource  = flag.String("projection-source", "firestore", "Projection source: redis|firestore")
		coalescingWindow  = flag.Duration("coalescing-window", 2*time.Second, "Dirty ref coalescing window")
		analyticalTimeout = flag.Duration("analytical-timeout", 45*time.Second, "Timeout for analytical refresh after propagation")
	)
	flag.Parse()

	ctx := context.Background()

	fs, err := firestore.NewClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer fs.Close()

	var pub *realtime.Publisher
	if *enableRealtime {
		pub, err = realtime.NewPublisher(ctx, strings.TrimSpace(*projectID))
		if err != nil {
			log.Fatalf("realtime publisher init failed: %v", err)
		}
	}

	frRepo := horizonrepo.NewHorizonFragmentsRepository(fs)
	versionsRepo := horizonrepo.NewHorizonFragmentVersionsRepository(fs)
	edgesRepo := horizonrepo.NewDependencyEdgesRepository(fs)
	lensesRepo := horizonrepo.NewLensesRepository(fs)
	watchSubjectsRepo := horizonrepo.NewWatchSubjectsRepository(fs)
	propagationRulesRepo := horizonrepo.NewPropagationPoliciesRepository(fs)
	analyticalPrimitivesRepo := horizonrepo.NewAnalyticalPrimitivesRepository(fs)
	analyticalAggregatesRepo := horizonrepo.NewAnalyticalAggregatesRepository(fs)
	materializedArtifactsRepo := horizonrepo.NewMaterializedArtifactsRepository(fs)
	refScopeProjectionsRepo := horizonrepo.NewRefScopeProjectionsRepository(fs)
	pathFamilyProjectionsRepo := horizonrepo.NewPathFamilyProjectionsRepository(fs)
	compiledWatchSubjectsRepo := horizonrepo.NewCompiledWatchSubjectsRepository(fs)

	// Policy infrastructure
	telemetryWriter := horizonpolicy.NewFirestoreTelemetryWriter(fs)
	propagationPolicy := horizonpolicy.NewPropagationPolicy(propagationRulesRepo)
	policyExecutor := horizonpolicy.NewPolicyExecutor(propagationPolicy, telemetryWriter, nil)

	// Critical path detector
	criticalPathDetector := horizoncriticalpath.NewDetector(frRepo, edgesRepo)

	// Resolve fragment
	cp := strings.TrimSpace(*canonicalPath)
	if cp == "" {
		log.Fatal("missing -path")
	}
	log.Printf("Looking up fragment: canonicalPath=%q", cp)
	frag, err := frRepo.GetByCanonicalPath(ctx, cp)
	if err != nil {
		log.Fatalf("GetByCanonicalPath failed: %v", err)
	}
	log.Printf("Found fragment: id=%s canonicalPath=%s", frag.ID, frag.CanonicalPath)

	mn := strings.TrimSpace(*metricName)
	if mn == "" {
		log.Fatal("missing -metric")
	}

	if frag.State == nil {
		frag.State = map[string]models.MetricValue{}
	}

	old := frag.State[mn] // zero value ok
	newVal := old
	newVal.Value = old.Value + *delta
	if math.Abs(newVal.Value) > 1e9 {
		log.Fatalf("refusing to set extreme metric value: %v", newVal.Value)
	}
	// keep existing confidence/unit; provenance will show update as manual tool
	newVal.Provenance.Method = "tool:horizon_propagate_metric"
	newVal.ObservedAt = time.Now().UTC()

	// Update source fragment first (so state is consistent in Firestore)
	updated := *frag
	updated.State = make(map[string]models.MetricValue, len(frag.State)+1)
	for k, v := range frag.State {
		updated.State[k] = v
	}
	updated.State[mn] = newVal

	log.Printf("Updating source fragment %s (%s): %s %.4f -> %.4f (delta=%.4f)", frag.ID, frag.CanonicalPath, mn, old.Value, newVal.Value, *delta)
	if err := frRepo.Update(ctx, &updated); err != nil {
		log.Fatalf("failed to update source fragment: %v", err)
	}

	// Run propagation (axis-based when ProfileLookup is set and metric is canonical)
	profilesRepo := horizonrepo.NewPropagationProfilesRepository(fs)
	profileLookup := horizonprop.NewProfileLookup(profilesRepo)
	_ = profileLookup.Initialize(ctx)
	engine := horizonprop.NewPropagationEngine(frRepo, versionsRepo, edgesRepo, lensesRepo, pub).
		WithPolicyExecutor(policyExecutor).
		WithCriticalPathDetector(criticalPathDetector).
		WithProfileLookup(profileLookup)
	res, err := engine.PropagateChange(ctx, frag.ID, mn, old, newVal)
	if err != nil {
		log.Fatalf("PropagateChange failed: %v", err)
	}

	log.Printf("Propagation result: affectedFragments=%d affectedLenses=%d paths=%d time=%s metrics=%v",
		len(res.AffectedFragments), len(res.AffectedLenses), len(res.PropagationPaths), res.PropagationTime.String(), res.Metrics)
	if len(res.PropagationPaths) > 0 {
		limit := 5
		if len(res.PropagationPaths) < limit {
			limit = len(res.PropagationPaths)
		}
		for i := 0; i < limit; i++ {
			log.Printf("path[%d]=%v", i, res.PropagationPaths[i])
		}
	}

	if *refreshAnalytical {
		changedRefs := make([]string, 0, len(res.AffectedFragments)+1)
		changedRefs = append(changedRefs, strings.TrimSpace(frag.CanonicalPath))
		for _, affectedFragmentID := range res.AffectedFragments {
			affectedFragmentID = strings.TrimSpace(affectedFragmentID)
			if affectedFragmentID == "" {
				continue
			}
			affected, err := frRepo.Get(ctx, affectedFragmentID)
			if err != nil || affected == nil || strings.TrimSpace(affected.CanonicalPath) == "" {
				continue
			}
			changedRefs = append(changedRefs, strings.TrimSpace(affected.CanonicalPath))
		}
		projectionService := horizonanalytical.NewProjectionService(
			refScopeProjectionsRepo,
			pathFamilyProjectionsRepo,
			compiledWatchSubjectsRepo,
			watchSubjectsRepo,
			analyticalAggregatesRepo,
		)
		redisEnabled := false
		if strings.EqualFold(strings.TrimSpace(*projectionSource), "redis") {
			redisClient := cache.NewRedisClient()
			if redisClient != nil {
				pingCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
				pingErr := redisClient.Ping(pingCtx).Err()
				cancel()
				if pingErr != nil {
					log.Printf("Redis unavailable at %s: %v; falling back to firestore projection path", redisClient.Options().Addr, pingErr)
				} else {
					redisEnabled = true
					projectionService.WithRedis(redisClient)
					_ = horizonanalytical.EnqueueChangedRefs(ctx, redisClient, strings.TrimSpace(firstNonEmpty(*tenantID, frag.TenantID, "global")), changedRefs, *coalescingWindow+2*time.Minute)
					if *coalescingWindow > 0 {
						time.Sleep(*coalescingWindow)
					}
					if drained, drainErr := horizonanalytical.DrainChangedRefs(ctx, redisClient, strings.TrimSpace(firstNonEmpty(*tenantID, frag.TenantID, "global"))); drainErr == nil && len(drained) > 0 {
						changedRefs = drained
					}
				}
			}
		}
		refreshCtx, cancel := context.WithTimeout(ctx, *analyticalTimeout)
		defer cancel()
		deltaState, err := projectionService.ResolveAffectedScopes(refreshCtx, strings.TrimSpace(firstNonEmpty(*tenantID, frag.TenantID, "global")), changedRefs)
		if err != nil {
			log.Fatalf("projection resolve failed: %v", err)
		}
		var refreshResult *horizonanalytical.RuntimeRefreshResult
		if *hotOnly {
			refreshResult, err = horizonanalytical.RefreshForDelta(refreshCtx, horizonanalytical.RuntimeRefreshDeps{
				FragmentsRepo:     frRepo,
				LensesRepo:        lensesRepo,
				MaterializedRepo:  materializedArtifactsRepo,
				WatchSubjectsRepo: watchSubjectsRepo,
				PrimitivesRepo:    analyticalPrimitivesRepo,
				AggregatesRepo:    analyticalAggregatesRepo,
				ProjectionService: projectionService,
			}, deltaState, time.Now().UTC())
		} else {
			legacyResult, legacyErr := horizonanalytical.RefreshKnowledgeForRefs(
				refreshCtx,
				frRepo,
				lensesRepo,
				materializedArtifactsRepo,
				watchSubjectsRepo,
				analyticalPrimitivesRepo,
				analyticalAggregatesRepo,
				strings.TrimSpace(firstNonEmpty(*tenantID, frag.TenantID, "global")),
				changedRefs,
				time.Now().UTC(),
			)
			err = legacyErr
			if legacyResult != nil {
				refreshResult = &horizonanalytical.RuntimeRefreshResult{
					Delta:                  deltaState,
					UpdatedPrimitiveCount:  legacyResult.UpdatedPrimitiveCount,
					UpdatedFragmentScopes:  legacyResult.UpdatedFragmentScopes,
					UpdatedLensIDs:         legacyResult.UpdatedLensIDs,
					UpdatedWatchSubjectIDs: legacyResult.UpdatedWatchSubjectIDs,
				}
			}
		}
		if err != nil {
			log.Fatalf("analytical refresh failed: %v", err)
		}
		if refreshResult != nil {
			log.Printf(
				"Analytical refresh: refs=%d fragmentScopes=%d lenses=%d watchSubjects=%d primitivesUpserted=%d hotOnly=%t redisEnabled=%t skipCorpus=%t skipSimulation=%t",
				len(refreshResult.Delta.ChangedRefs),
				len(refreshResult.UpdatedFragmentScopes),
				len(refreshResult.UpdatedLensIDs),
				len(refreshResult.UpdatedWatchSubjectIDs),
				refreshResult.UpdatedPrimitiveCount,
				*hotOnly,
				redisEnabled,
				*skipCorpus,
				*skipSimulation,
			)
			if len(refreshResult.UpdatedLensIDs) > 0 {
				log.Printf("Analytical lens refresh ids=%v", refreshResult.UpdatedLensIDs)
			}
			if len(refreshResult.UpdatedFragmentScopes) > 0 {
				log.Printf("Analytical fragment refresh refs=%v", refreshResult.UpdatedFragmentScopes)
			}
		}
	}

	fmt.Println("OK")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
