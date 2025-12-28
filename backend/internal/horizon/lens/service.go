package lens

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/critical_path"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/rtctx"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/trace"
	agentmodels "github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

func parseStringSlice(v interface{}) []string {
	switch t := v.(type) {
	case []string:
		out := make([]string, 0, len(t))
		for _, s := range t {
			if strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(t))
		for _, it := range t {
			if s, ok := it.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	case string:
		parts := strings.Split(t, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		return out
	default:
		return nil
	}
}

func parseInt(v interface{}, def int) int {
	switch t := v.(type) {
	case int:
		return t
	case int32:
		return int(t)
	case int64:
		return int(t)
	case float64:
		return int(t)
	case float32:
		return int(t)
	case string:
		t = strings.TrimSpace(t)
		if t == "" {
			return def
		}
		if i, err := strconv.Atoi(t); err == nil {
			return i
		}
		if f, err := strconv.ParseFloat(t, 64); err == nil {
			return int(f)
		}
		return def
	default:
		return def
	}
}

func dedupeCanonicalPaths(in []string) []string {
	if len(in) == 0 {
		return in
	}
	seen := make(map[string]struct{}, len(in))
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

// LensService управляет Lens и подписками на HorizonFragment
type LensService struct {
	lensesRepo        *repo.LensesRepository
	fragmentsRepo     *repo.HorizonFragmentsRepository
	scoutingJobsRepo  *repo.ScoutingJobsRepository
	ontologyFactsRepo *corerepo.OntologyFactsRepository
	lifecycle         *LifecycleManager
	calculator        *MetricsCalculator
	watchdogHook      func(ctx context.Context, fragmentID string)
	queryResolver        *QueryResolver
	realtime             *realtime.Publisher
	criticalPathDetector *critical_path.Detector // Optional: for critical path detection
	edgesRepo            *repo.DependencyEdgesRepository // Required for critical path detection
	canonicalPathResolver *scout.CanonicalPathResolver // Optional: for early canonicalization and deduplication
}

// NewLensService создает новый Lens Service
func NewLensService(
	lensesRepo *repo.LensesRepository,
	fragmentsRepo *repo.HorizonFragmentsRepository,
	scoutingJobsRepo *repo.ScoutingJobsRepository,
	ontologyFactsRepo *corerepo.OntologyFactsRepository,
	lifecycle *LifecycleManager,
	calculator *MetricsCalculator,
	rt *realtime.Publisher,
) *LensService {
	queryResolver := NewQueryResolver(fragmentsRepo, ontologyFactsRepo)
	return &LensService{
		lensesRepo:        lensesRepo,
		fragmentsRepo:     fragmentsRepo,
		scoutingJobsRepo:  scoutingJobsRepo,
		ontologyFactsRepo: ontologyFactsRepo,
		lifecycle:         lifecycle,
		calculator:        calculator,
		queryResolver:     queryResolver,
		realtime:          rt,
	}
}

// WithCriticalPathDetector sets the critical path detector for LensService.
func (s *LensService) WithCriticalPathDetector(detector *critical_path.Detector, edgesRepo *repo.DependencyEdgesRepository) *LensService {
	s.criticalPathDetector = detector
	s.edgesRepo = edgesRepo
	return s
}

// WithCanonicalPathResolver sets the canonical path resolver for early canonicalization and deduplication.
func (s *LensService) WithCanonicalPathResolver(resolver *scout.CanonicalPathResolver) *LensService {
	s.canonicalPathResolver = resolver
	return s
}

func (s *LensService) publish(ctx context.Context, et realtime.EventType, data map[string]interface{}) {
	if s == nil || s.realtime == nil {
		return
	}
	rc, ok := rtctx.Get(ctx)
	tenantID := ""
	if ok {
		tenantID = strings.TrimSpace(rc.TenantID)
		// Ensure mandatory routing keys are present in payload for WS delivery.
		if v := strings.TrimSpace(rc.ProjectID); v != "" {
			data["projectId"] = v
		}
		if v := strings.TrimSpace(rc.LensID); v != "" {
			data["lensId"] = v
		}
		if v := strings.TrimSpace(rc.CorrelationID); v != "" {
			data["correlationId"] = v
		}
		if v := strings.TrimSpace(rc.SessionID); v != "" {
			data["sessionId"] = v
		}
	}
	if tenantID == "" {
		// Fall back to tenant from auth/lens when context is missing.
		if v, ok2 := data["tenantId"].(string); ok2 {
			tenantID = strings.TrimSpace(v)
		}
	}
	if tenantID == "" {
		// As a last resort, do not publish (we must not leak across tenants).
		return
	}
	data["tenantId"] = tenantID
	data["ts"] = time.Now().UTC().Format(time.RFC3339Nano)
	_ = s.realtime.PublishJSON(ctx, et, tenantID, data)
}

// SetWatchdogLifecycleHook устанавливает hook, который синхронизирует WatchdogConfig
// после изменений жизненного цикла фрагментов (ACTIVE/DORMANT/ARCHIVED).
func (s *LensService) SetWatchdogLifecycleHook(hook func(ctx context.Context, fragmentID string)) {
	s.watchdogHook = hook
}

// CreateLens создает новый Lens и управляет подписками
func (s *LensService) CreateLens(ctx context.Context, lens *models.Lens) (string, error) {
	trace.Log(ctx, "lens_create", "lens.service.create.start", map[string]any{
		"tenantId":  strings.TrimSpace(lens.TenantID),
		"projectId": strings.TrimSpace(lens.ProjectID),
		"name":      trace.Trunc(lens.Name, 120),
		"status":    strings.TrimSpace(lens.Status),
		"scopeN":    len(lens.Scope),
		"scopeTop":  trace.StrSlice(lens.Scope, 12),
	})
	if err := lens.Validate(); err != nil {
		trace.Log(ctx, "lens_create", "lens.service.create.validation_error", map[string]any{
			"tenantId":  strings.TrimSpace(lens.TenantID),
			"projectId": strings.TrimSpace(lens.ProjectID),
			"error":     err.Error(),
		})
		// Best-effort: surface assembly failure to the initiating UI (if realtime ctx exists).
		s.publish(ctx, realtime.EventType("horizon.assembly.failed"), map[string]interface{}{
			"tenantId":  lens.TenantID,
			"projectId": lens.ProjectID,
			"phase":     "decomposition",
			"error":     err.Error(),
		})
		return "", fmt.Errorf("validation error: %w", err)
	}

	// Emit assembly start (Hook -> Assemble).
	// Note: lensId is not known yet; we'll re-attach it after creation.
	if rc, ok := rtctx.Get(ctx); ok && s.realtime != nil {
		tmp := map[string]interface{}{
			"tenantId":  lens.TenantID,
			"projectId": lens.ProjectID,
			"phase":     "decomposition",
			"progress":  0.0,
		}
		if strings.TrimSpace(rc.CorrelationID) != "" {
			tmp["correlationId"] = rc.CorrelationID
		}
		if strings.TrimSpace(rc.SessionID) != "" {
			tmp["sessionId"] = rc.SessionID
		}
		s.publish(ctx, realtime.EventType("horizon.assembly.started"), tmp)
		s.publish(ctx, realtime.EventType("horizon.assembly.phase"), map[string]interface{}{
			"tenantId":  lens.TenantID,
			"projectId": lens.ProjectID,
			"phase":     "decomposition",
			"progress":  0.0,
		})
	}

	// Создаем Lens
	lensID, err := s.lensesRepo.Create(ctx, lens)
	if err != nil {
		trace.Log(ctx, "lens_create", "lens.repo.create.error", map[string]any{
			"tenantId":  strings.TrimSpace(lens.TenantID),
			"projectId": strings.TrimSpace(lens.ProjectID),
			"error":     err.Error(),
		})
		s.publish(ctx, realtime.EventType("horizon.assembly.failed"), map[string]interface{}{
			"tenantId":  lens.TenantID,
			"projectId": lens.ProjectID,
			"phase":     "decomposition",
			"error":     err.Error(),
		})
		return "", fmt.Errorf("failed to create lens: %w", err)
	}
	lens.ID = lensID
	trace.Log(ctx, "lens_create", "lens.repo.create.ok", map[string]any{
		"tenantId":  strings.TrimSpace(lens.TenantID),
		"projectId": strings.TrimSpace(lens.ProjectID),
		"lensId":    lensID,
	})

	// Update realtime ctx with lensId (if present).
	if rc, ok := rtctx.Get(ctx); ok {
		rc.LensID = lensID
		ctx = rtctx.With(ctx, rc)
	}

	s.publish(ctx, realtime.EventType("horizon.assembly.phase"), map[string]interface{}{
		"tenantId":  lens.TenantID,
		"projectId": lens.ProjectID,
		"lensId":    lensID,
		"phase":     "cache_hit",
		"progress":  0.05,
	})

	// Optional behavior knobs for Horizon assembly (kept in ViewConfig to avoid breaking schema):
	// - requestedMetrics: []string | []any(strings) | "a,b,c"
	// - maxRecursionDepth: number | string
	// - focus: string (free-form, e.g. "battery logistics procurement")
	// - spherePaths: []string | []any(strings) | "a,b,c" (e.g. "/sphere/logistics")
	// - keywords: []string | []any(strings) | "a,b,c" (free-form tokens; not required to be canonical paths)
	requestedMetrics := []string(nil)
	maxRecursionDepth := 2
	focus := ""
	spherePaths := []string(nil)
	keywords := []string(nil)
	if lens.ViewConfig != nil {
		if v, ok := lens.ViewConfig["requestedMetrics"]; ok {
			requestedMetrics = parseStringSlice(v)
		}
		if v, ok := lens.ViewConfig["maxRecursionDepth"]; ok {
			maxRecursionDepth = parseInt(v, 2)
		}
		if v, ok := lens.ViewConfig["focus"]; ok {
			if s, ok2 := v.(string); ok2 {
				focus = strings.TrimSpace(s)
			}
		}
		// Accept both "spherePaths" (preferred) and legacy "spheres".
		if v, ok := lens.ViewConfig["spherePaths"]; ok {
			spherePaths = parseStringSlice(v)
		} else if v, ok := lens.ViewConfig["spheres"]; ok {
			spherePaths = parseStringSlice(v)
		}
		if v, ok := lens.ViewConfig["keywords"]; ok {
			keywords = parseStringSlice(v)
		}
	}
	if maxRecursionDepth < 0 {
		maxRecursionDepth = 0
	}

	// Инициализируем кэш fragment IDs для оптимизации будущих операций
	if lens.ScopeFragments == nil {
		lens.ScopeFragments = make(map[string]string)
	}
	if lens.ResolvedQueries == nil {
		lens.ResolvedQueries = make(map[string][]string)
	}

	// Разделяем Scope на статические пути и QueryAddress
	staticPaths, queryAddresses := splitScope(lens.Scope)
	trace.Log(ctx, "scope", "scope.split", map[string]any{
		"tenantId":       strings.TrimSpace(lens.TenantID),
		"projectId":      strings.TrimSpace(lens.ProjectID),
		"lensId":         lensID,
		"staticPathsN":   len(staticPaths),
		"queryAddressesN": len(queryAddresses),
		"staticPathsTop": trace.StrSlice(staticPaths, 12),
		"queryAddressesTop": trace.StrSlice(queryAddresses, 12),
	})

	// Резолвим QueryAddress в canonical paths
	now := time.Now().UTC()
	resolvedQueries, resolveErrors := resolveQueryAddresses(ctx, s.queryResolver, lens.TenantID, queryAddresses)
	trace.Log(ctx, "scope", "scope.query_addresses.resolved", map[string]any{
		"tenantId":      strings.TrimSpace(lens.TenantID),
		"projectId":     strings.TrimSpace(lens.ProjectID),
		"lensId":        lensID,
		"resolvedN":     len(resolvedQueries),
		"errorsN":       len(resolveErrors),
	})

	// Сохраняем resolved queries в кэш
	for queryAddr, resolvedPaths := range resolvedQueries {
		lens.ResolvedQueries[queryAddr] = resolvedPaths
		lens.LastResolved = &now
		// Добавляем resolved paths к static paths для последующей обработки
		staticPaths = append(staticPaths, resolvedPaths...)
	}
	// Avoid duplicate work (e.g. repeated anchors / resolved queries).
	staticPaths = dedupeCanonicalPaths(staticPaths)
	trace.Log(ctx, "scope", "scope.deduped", map[string]any{
		"tenantId":     strings.TrimSpace(lens.TenantID),
		"projectId":    strings.TrimSpace(lens.ProjectID),
		"lensId":       lensID,
		"staticPathsN": len(staticPaths),
		"staticPathsTop": trace.StrSlice(staticPaths, 20),
	})

	// Логируем ошибки резолва (если есть)
	if len(resolveErrors) > 0 {
		log.Printf("[LensService] CreateLens: %d query address resolution errors (continuing with partial results)", len(resolveErrors))
	}

	// Detect critical paths before creating lens (if detector is available)
	if s.criticalPathDetector != nil && s.edgesRepo != nil && len(staticPaths) >= 2 {
		criticalEdges, err := s.criticalPathDetector.DetectCriticalPaths(ctx, staticPaths, 0.7)
		if err == nil && len(criticalEdges) > 0 {
			// Store critical path edges in ViewConfig
			if lens.ViewConfig == nil {
				lens.ViewConfig = make(map[string]interface{})
			}
			edgeIDs := make([]string, 0, len(criticalEdges))
			for edgeID := range criticalEdges {
				edgeIDs = append(edgeIDs, edgeID)
			}
			lens.ViewConfig["criticalPathEdges"] = edgeIDs
		}
	}

	// Увеличиваем activeSubscribers для всех HorizonFragment в scope
	cacheHits := 0
	scoutingPlanned := 0

	// Best-effort: prefetch active (pending/running) scout jobs for this lens to avoid duplicates per canonicalPath.
	// This addresses observed repeated processing in logs when multiple jobs are enqueued for the same node.
	activeScoutJobs := map[string]string{} // canonicalPath -> jobId
	if s.scoutingJobsRepo != nil {
		if jobs, jerr := s.scoutingJobsRepo.ListByProjectAndLens(ctx, lens.TenantID, lens.ProjectID, lensID, 400); jerr == nil {
			for _, j := range jobs {
				if j == nil {
					continue
				}
				if j.Status != agentmodels.AgentJobStatusPending && j.Status != agentmodels.AgentJobStatusRunning {
					continue
				}
				p := strings.TrimSpace(j.TargetCanonicalPath)
				// Normalize job path so we dedupe even if older jobs were created under alias paths.
				if s.canonicalPathResolver != nil && p != "" {
					// Best-effort: infer fragment type from path prefix.
					ft := models.FragmentTypeOrganization
					switch {
					case strings.HasPrefix(p, "/product/"):
						ft = models.FragmentTypeProduct
					case strings.HasPrefix(p, "/program/"):
						ft = models.FragmentTypeProgram
					case strings.HasPrefix(p, "/market/"):
						ft = models.FragmentTypeMarket
					case strings.HasPrefix(p, "/fac/"):
						ft = models.FragmentTypeFacility
					case strings.HasPrefix(p, "/loc/") || strings.HasPrefix(p, "/region/"):
						ft = models.FragmentTypeGeoRegion
					case strings.HasPrefix(p, "/event/"):
						ft = models.FragmentTypeEvent
					case strings.HasPrefix(p, "/person/"):
						ft = models.FragmentTypePerson
					case strings.HasPrefix(p, "/org/"):
						ft = models.FragmentTypeOrganization
					}
					if resolved, rerr := s.canonicalPathResolver.Resolve(ctx, p, ft); rerr == nil && resolved != nil && strings.TrimSpace(resolved.CanonicalPath) != "" {
						p = resolved.CanonicalPath
					}
				}
				if p == "" {
					continue
				}
				if _, ok := activeScoutJobs[p]; !ok {
					activeScoutJobs[p] = j.ID
				}
			}
		}
	}
	trace.Log(ctx, "root_jobs", "root_jobs.prefetch_active", map[string]any{
		"tenantId":    strings.TrimSpace(lens.TenantID),
		"projectId":   strings.TrimSpace(lens.ProjectID),
		"lensId":      lensID,
		"activeJobsN": len(activeScoutJobs),
	})

	// Track scope rewrites so we can persist canonical scope back into the lens document.
	// This prevents UI/graph from being stuck on alias paths like /loc/uae.
	scopeRewrites := map[string]string{} // inputPath -> canonicalPath

	for _, inputPath := range staticPaths {
		// EARLY CANONICALIZATION AND DEDUPLICATION: Resolve canonical path before any processing
		// This ensures we operate on canonicalized and deduplicated paths throughout the pipeline
		var canonicalPath string
		var fragment *models.HorizonFragment
		var isCacheHit bool
		
		if s.canonicalPathResolver != nil {
			// Determine fragment type from canonical path prefix (best-effort)
			fragmentType := models.FragmentTypeOrganization // Default
			switch {
			case strings.HasPrefix(inputPath, "/product/"):
				fragmentType = models.FragmentTypeProduct
			case strings.HasPrefix(inputPath, "/program/"):
				fragmentType = models.FragmentTypeProgram
			case strings.HasPrefix(inputPath, "/market/"):
				fragmentType = models.FragmentTypeMarket
			case strings.HasPrefix(inputPath, "/fac/"):
				fragmentType = models.FragmentTypeFacility
			case strings.HasPrefix(inputPath, "/loc/") || strings.HasPrefix(inputPath, "/region/"):
				fragmentType = models.FragmentTypeGeoRegion
			case strings.HasPrefix(inputPath, "/event/"):
				fragmentType = models.FragmentTypeEvent
			case strings.HasPrefix(inputPath, "/org/"):
				fragmentType = models.FragmentTypeOrganization
			case strings.HasPrefix(inputPath, "/person/"):
				fragmentType = models.FragmentTypePerson
			}

			resolved, err := s.canonicalPathResolver.Resolve(ctx, inputPath, fragmentType)
			if err == nil && resolved != nil {
				canonicalPath = resolved.CanonicalPath
				if strings.TrimSpace(inputPath) != "" && strings.TrimSpace(canonicalPath) != "" && strings.TrimSpace(inputPath) != strings.TrimSpace(canonicalPath) {
					scopeRewrites[strings.TrimSpace(inputPath)] = strings.TrimSpace(canonicalPath)
				}
				if resolved.ExistingFragment != nil {
					fragment = resolved.ExistingFragment
					isCacheHit = true
				}
			} else {
				// Fallback to original path if resolution fails
				canonicalPath = strings.TrimSpace(inputPath)
			}
		} else {
			canonicalPath = strings.TrimSpace(inputPath)
		}

		// If fragment not found via resolver, check by canonical path
		if fragment == nil {
			var err error
			fragment, err = s.fragmentsRepo.GetByCanonicalPath(ctx, canonicalPath)
			isCacheHit = err == nil && fragment != nil
		}
		
		isFragmentFreshEnough := false
		if isCacheHit {
			// If we already have verified (or at least non-low-confidence) data, do NOT create a new Scout job.
			// This keeps "known queries" fast and avoids generating Cloud Tasks for cached graphs.
			//
			// Note: Watchdog/explicit refresh paths can still generate jobs when needed.
			if !fragment.Metadata.LastVerifiedAt.IsZero() && fragment.Metadata.EpistemicStatus != models.EpistemicStatusLowConfidence {
				isFragmentFreshEnough = true
			}
		}
		if isCacheHit && fragment != nil {
			cacheHits++
			// Сохраняем в кэш для будущих операций
			lens.ScopeFragments[canonicalPath] = fragment.ID
			// Also store by input path (alias) to keep Lens lifecycle (Update/Delete) consistent
			// even when lens.Scope contains non-canonical aliases.
			if ap := strings.TrimSpace(inputPath); ap != "" && ap != strings.TrimSpace(canonicalPath) {
				lens.ScopeFragments[ap] = fragment.ID
			}

			if err := s.fragmentsRepo.IncrementActiveSubscribers(ctx, fragment.ID); err != nil {
				// Логируем ошибку, но не прерываем создание Lens
				_ = err
			}
			// Управляем жизненным циклом
			if err := s.lifecycle.EnsureActive(ctx, fragment.ID); err != nil {
				_ = err
			}
			// Синхронизируем WatchdogConfig (создать/запустить при ACTIVE)
			if s.watchdogHook != nil {
				s.watchdogHook(ctx, fragment.ID)
			}
			// Graph materialization: cached node is ready to render.
			s.publish(ctx, realtime.EventType("horizon.graph.node.upserted"), map[string]interface{}{
				"tenantId":        lens.TenantID,
				"projectId":       lens.ProjectID,
				"lensId":          lensID,
				"canonicalPath":   canonicalPath,
				"fragmentId":      fragment.ID,
				"status":          "cached",
				"epistemicStatus": string(fragment.Metadata.EpistemicStatus),
				"version":         fragment.Version,
			})
		}

		// Create ScoutingJob only when we DON'T already have sufficiently verified data.
		// This prevents excessive Cloud Tasks for queries that are already in the system (e.g., Astana/LRT).
		if s.scoutingJobsRepo != nil && !isFragmentFreshEnough {
			if existingJobID, ok := activeScoutJobs[strings.TrimSpace(canonicalPath)]; ok && existingJobID != "" {
				trace.Log(ctx, "root_jobs", "root_jobs.dedup_skip_active", map[string]any{
					"tenantId":       strings.TrimSpace(lens.TenantID),
					"projectId":      strings.TrimSpace(lens.ProjectID),
					"lensId":         lensID,
					"canonicalPath":  strings.TrimSpace(canonicalPath),
					"existingJobId":  existingJobID,
					"cacheHit":       isCacheHit,
					"freshEnough":    isFragmentFreshEnough,
				})
				// Best-effort: signal "scouting" state but reuse existing job id.
				s.publish(ctx, realtime.EventType("horizon.graph.node.upserted"), map[string]interface{}{
					"tenantId":      lens.TenantID,
					"projectId":     lens.ProjectID,
					"lensId":        lensID,
					"canonicalPath": strings.TrimSpace(canonicalPath),
					"status":        "scouting",
					"jobId":         existingJobID,
					"hint":          "dedup_existing_scout_job",
				})
				continue
			}
			scoutingPlanned++
			trace.Log(ctx, "root_jobs", "root_jobs.create.start", map[string]any{
				"tenantId":      strings.TrimSpace(lens.TenantID),
				"projectId":     strings.TrimSpace(lens.ProjectID),
				"lensId":        lensID,
				"canonicalPath": strings.TrimSpace(canonicalPath),
				"cacheHit":      isCacheHit,
			})
			now := time.Now().UTC()
			state := map[string]interface{}{
				"targetCanonicalPath": canonicalPath,
				"lensId":              lensID,
				"currentState":        string(models.ScoutingJobStatePending),
				"scoutingStrategy":    string(models.ScoutingStrategySnapshot),
				// Recursion control (used by ScoutEngine.processDiscoveredRelations)
				"recursionDepth":    0.0,
				"maxRecursionDepth": float64(maxRecursionDepth),
			}
			// Preserve alias for traceability (ScoutEngine will also set aliasCanonicalPath on retarget).
			if ap := strings.TrimSpace(inputPath); ap != "" && ap != strings.TrimSpace(canonicalPath) {
				state["aliasCanonicalPath"] = ap
			}
			// Propagate correlation/session IDs into job state for downstream realtime.
			if rc, ok := rtctx.Get(ctx); ok {
				if strings.TrimSpace(rc.CorrelationID) != "" {
					state["correlationId"] = rc.CorrelationID
				}
				if strings.TrimSpace(rc.SessionID) != "" {
					state["sessionId"] = rc.SessionID
				}
			}
			if len(requestedMetrics) > 0 {
				state["requestedMetrics"] = requestedMetrics
			}
			if focus != "" {
				state["focus"] = focus
			}
			if len(spherePaths) > 0 {
				state["spherePaths"] = spherePaths
			}
			if len(keywords) > 0 {
				state["keywords"] = keywords
			}
			scoutingJob := &models.ScoutingJob{
				AgentJob: agentmodels.AgentJob{
					TenantID:  lens.TenantID,
					ProjectID: lens.ProjectID,
					Status:    agentmodels.AgentJobStatusPending,
					AgentID:   "scout",
					CreatedAt: now,
					UpdatedAt: now,
					State:     state,
				},
				LensID:              lensID,
				TargetCanonicalPath: canonicalPath,
				ScoutingStrategy:    string(models.ScoutingStrategySnapshot),
				BillingContext: models.BillingContext{
					ChargeModel: string(models.ChargeModelBounty),
					ProjectID:   lens.ProjectID,
				},
				CurrentState: models.ScoutingJobStatePending,
			}
			jobID, createErr := s.scoutingJobsRepo.Create(ctx, scoutingJob)
			if createErr != nil {
				trace.Log(ctx, "root_jobs", "root_jobs.create.error", map[string]any{
					"tenantId":      strings.TrimSpace(lens.TenantID),
					"projectId":     strings.TrimSpace(lens.ProjectID),
					"lensId":        lensID,
					"canonicalPath": strings.TrimSpace(canonicalPath),
					"error":         createErr.Error(),
				})
				s.publish(ctx, realtime.EventType("horizon.assembly.failed"), map[string]interface{}{
					"tenantId":      lens.TenantID,
					"projectId":     lens.ProjectID,
					"lensId":        lensID,
					"phase":         "scouting",
					"canonicalPath": canonicalPath,
					"error":         createErr.Error(),
				})
				return "", fmt.Errorf("failed to create scouting job for %s: %w", canonicalPath, createErr)
			}
			trace.Log(ctx, "root_jobs", "root_jobs.create.ok", map[string]any{
				"tenantId":      strings.TrimSpace(lens.TenantID),
				"projectId":     strings.TrimSpace(lens.ProjectID),
				"lensId":        lensID,
				"canonicalPath": strings.TrimSpace(canonicalPath),
				"jobId":         strings.TrimSpace(jobID),
			})
			if strings.TrimSpace(jobID) != "" {
				activeScoutJobs[strings.TrimSpace(canonicalPath)] = strings.TrimSpace(jobID)
			}
			// Graph materialization hint: node requires scouting (even if fragment didn't exist at cache-hit time).
			s.publish(ctx, realtime.EventType("horizon.graph.node.upserted"), map[string]interface{}{
				"tenantId":      lens.TenantID,
				"projectId":     lens.ProjectID,
				"lensId":        lensID,
				"canonicalPath": canonicalPath,
				"status":        "scouting",
				"jobId":         strings.TrimSpace(jobID),
			})
		} else if isCacheHit && isFragmentFreshEnough {
			trace.Log(ctx, "root_jobs", "root_jobs.skip_fresh_verified", map[string]any{
				"tenantId":      strings.TrimSpace(lens.TenantID),
				"projectId":     strings.TrimSpace(lens.ProjectID),
				"lensId":        lensID,
				"canonicalPath": strings.TrimSpace(canonicalPath),
				"fragmentId":    strings.TrimSpace(fragment.ID),
				"epistemicStatus": string(fragment.Metadata.EpistemicStatus),
				"lastVerifiedAt": func() string {
					if fragment.Metadata.LastVerifiedAt.IsZero() {
						return ""
					}
					return fragment.Metadata.LastVerifiedAt.UTC().Format(time.RFC3339Nano)
				}(),
			})
			// Best-effort: signal that no scouting is needed for this node.
			s.publish(ctx, realtime.EventType("horizon.scout.job.completed"), map[string]interface{}{
				"tenantId":        lens.TenantID,
				"projectId":       lens.ProjectID,
				"lensId":          lensID,
				"jobId":           "",
				"fragmentId":      fragment.ID,
				"canonicalPath":   strings.TrimSpace(canonicalPath),
				"epistemicStatus": string(fragment.Metadata.EpistemicStatus),
				"confidence":      1.0,
				"hint":            "cached_verified_skip_scout",
			})
		}
	}

	// Persist canonicalized scope back into the lens (best-effort).
	if len(scopeRewrites) > 0 {
		changed := false
		for i, entry := range lens.Scope {
			e := strings.TrimSpace(entry)
			if e == "" {
				continue
			}
			if repl, ok := scopeRewrites[e]; ok && strings.TrimSpace(repl) != "" && repl != e {
				lens.Scope[i] = repl
				changed = true
			}
		}
		if changed {
			// Keep scope stable: de-dup after rewrites.
			seen := map[string]struct{}{}
			out := make([]string, 0, len(lens.Scope))
			for _, entry := range lens.Scope {
				entry = strings.TrimSpace(entry)
				if entry == "" {
					continue
				}
				if _, ok := seen[entry]; ok {
					continue
				}
				seen[entry] = struct{}{}
				out = append(out, entry)
			}
			lens.Scope = out
			_ = s.lensesRepo.Update(ctx, lens)
		}
	}

	// Сохраняем обновленный кэш fragment IDs обратно в Firestore
	if len(lens.ScopeFragments) > 0 {
		_ = s.lensesRepo.Update(ctx, lens)
	}

	// Transition to scouting phase and publish initial progress snapshot.
	progress := 0.1
	if scoutingPlanned > 0 {
		progress = 0.1
	} else {
		progress = 1.0
	}
	s.publish(ctx, realtime.EventType("horizon.assembly.phase"), map[string]interface{}{
		"tenantId":  lens.TenantID,
		"projectId": lens.ProjectID,
		"lensId":    lensID,
		"phase": func() string {
			if scoutingPlanned > 0 {
				return "scouting"
			}
			return "ready"
		}(),
		"progress": progress,
	})
	s.publish(ctx, realtime.EventType("horizon.assembly.progress"), map[string]interface{}{
		"tenantId":  lens.TenantID,
		"projectId": lens.ProjectID,
		"lensId":    lensID,
		"phase": func() string {
			if scoutingPlanned > 0 {
				return "scouting"
			}
			return "ready"
		}(),
		"progress": progress,
		"counters": map[string]interface{}{
			"nodesKnown":  len(staticPaths),
			"nodesCached": cacheHits,
			"jobsTotal":   scoutingPlanned,
			"jobsRunning": 0,
			"jobsDone":    0,
			"jobsFailed":  0,
		},
	})
	if scoutingPlanned == 0 {
		s.publish(ctx, realtime.EventType("horizon.assembly.ready"), map[string]interface{}{
			"tenantId":  lens.TenantID,
			"projectId": lens.ProjectID,
			"lensId":    lensID,
			"phase":     "ready",
			"progress":  1.0,
		})
	}

	return lensID, nil
}

// UpdateLens обновляет Lens и управляет подписками
func (s *LensService) UpdateLens(ctx context.Context, lens *models.Lens) error {
	if err := lens.Validate(); err != nil {
		return fmt.Errorf("validation error: %w", err)
	}

	// Получаем старый Lens для сравнения scope
	oldLens, err := s.lensesRepo.Get(ctx, lens.ID)
	if err != nil {
		return fmt.Errorf("failed to get old lens: %w", err)
	}

	// Управляем подписками: отписываемся от старых, подписываемся на новые
	oldScope := make(map[string]bool)
	for _, path := range oldLens.Scope {
		oldScope[path] = true
	}

	newScope := make(map[string]bool)
	for _, path := range lens.Scope {
		newScope[path] = true
	}

	// Инициализируем кэш если отсутствует
	if lens.ScopeFragments == nil {
		lens.ScopeFragments = make(map[string]string)
	}
	if lens.ResolvedQueries == nil {
		lens.ResolvedQueries = make(map[string][]string)
	}
	// Копируем существующий кэш из oldLens
	if oldLens.ScopeFragments != nil {
		for k, v := range oldLens.ScopeFragments {
			lens.ScopeFragments[k] = v
		}
	}
	if oldLens.ResolvedQueries != nil {
		for k, v := range oldLens.ResolvedQueries {
			lens.ResolvedQueries[k] = v
		}
	}

	// Разделяем новый Scope на статические пути и QueryAddress
	newStaticPaths, newQueryAddresses := splitScope(lens.Scope)

	// Разделяем старый Scope на статические пути и QueryAddress
	oldStaticPaths, oldQueryAddresses := splitScope(oldLens.Scope)

	// Резолвим новые QueryAddress
	now := time.Now().UTC()
	allNewStaticPaths := make([]string, len(newStaticPaths))
	copy(allNewStaticPaths, newStaticPaths)

	resolvedQueries, resolveErrors := resolveQueryAddresses(ctx, s.queryResolver, lens.TenantID, newQueryAddresses)

	// Обновляем кэш и добавляем resolved paths
	for queryAddr, resolvedPaths := range resolvedQueries {
		lens.ResolvedQueries[queryAddr] = resolvedPaths
		lens.LastResolved = &now
		allNewStaticPaths = append(allNewStaticPaths, resolvedPaths...)
	}
	// Avoid duplicate work (e.g. repeated anchors / resolved queries).
	allNewStaticPaths = dedupeCanonicalPaths(allNewStaticPaths)

	// Логируем ошибки резолва (если есть)
	if len(resolveErrors) > 0 {
		log.Printf("[LensService] UpdateLens: %d query address resolution errors (continuing with partial results)", len(resolveErrors))
	}

	// Detect critical paths before updating lens (if detector is available)
	if s.criticalPathDetector != nil && s.edgesRepo != nil && len(allNewStaticPaths) >= 2 {
		criticalEdges, err := s.criticalPathDetector.DetectCriticalPaths(ctx, allNewStaticPaths, 0.7)
		if err == nil && len(criticalEdges) > 0 {
			// Store critical path edges in ViewConfig
			if lens.ViewConfig == nil {
				lens.ViewConfig = make(map[string]interface{})
			}
			edgeIDs := make([]string, 0, len(criticalEdges))
			for edgeID := range criticalEdges {
				edgeIDs = append(edgeIDs, edgeID)
			}
			lens.ViewConfig["criticalPathEdges"] = edgeIDs
		}
	}

	// Обновляем Lens
	if err := s.lensesRepo.Update(ctx, lens); err != nil {
		return fmt.Errorf("failed to update lens: %w", err)
	}

	// Резолвим старые QueryAddress для корректного сравнения
	allOldStaticPaths := make([]string, len(oldStaticPaths))
	copy(allOldStaticPaths, oldStaticPaths)

	for _, queryAddr := range oldQueryAddresses {
		if oldPaths, ok := oldLens.ResolvedQueries[queryAddr]; ok {
			allOldStaticPaths = append(allOldStaticPaths, oldPaths...)
		}
	}
	allOldStaticPaths = dedupeCanonicalPaths(allOldStaticPaths)

	// Удаляем QueryAddress, которые больше не используются
	for _, queryAddr := range oldQueryAddresses {
		stillUsed := false
		for _, newAddr := range newQueryAddresses {
			if newAddr == queryAddr {
				stillUsed = true
				break
			}
		}
		if !stillUsed {
			delete(lens.ResolvedQueries, queryAddr)
		}
	}

	// Создаем map новых paths для быстрого поиска
	newPathsMap := make(map[string]bool)
	for _, path := range allNewStaticPaths {
		newPathsMap[path] = true
	}

	// Отписываемся от удаленных из scope
	for _, path := range allOldStaticPaths {
		if !newPathsMap[path] {
			// ОПТИМИЗАЦИЯ: Используем кэш fragmentID если доступен
			var fragmentID string
			if oldLens.ScopeFragments != nil {
				fragmentID = oldLens.ScopeFragments[path]
			}

			if fragmentID != "" {
				// Быстрый путь: используем cached fragment ID (прямой key lookup)
				_ = s.fragmentsRepo.DecrementActiveSubscribers(ctx, fragmentID)
				_ = s.lifecycle.CheckAndTransition(ctx, fragmentID)
				if s.watchdogHook != nil {
					s.watchdogHook(ctx, fragmentID)
				}
			} else {
				// Fallback: composite index query
				lookupPath := path
				if s.canonicalPathResolver != nil {
					// Best-effort: normalize alias paths so we don't miss unsubscribes.
					// We assume ORGANIZATION by default; resolver will still apply canonization policy for canonical_path.
					if resolved, rerr := s.canonicalPathResolver.Resolve(ctx, path, models.FragmentTypeOrganization); rerr == nil && resolved != nil && strings.TrimSpace(resolved.CanonicalPath) != "" {
						lookupPath = resolved.CanonicalPath
					}
				}
				fragment, err := s.fragmentsRepo.GetByCanonicalPath(ctx, lookupPath)
				if err == nil && fragment != nil {
					_ = s.fragmentsRepo.DecrementActiveSubscribers(ctx, fragment.ID)
					_ = s.lifecycle.CheckAndTransition(ctx, fragment.ID)
					if s.watchdogHook != nil {
						s.watchdogHook(ctx, fragment.ID)
					}
				}
			}

			// Удаляем из кэша
			delete(lens.ScopeFragments, path)
		}
	}

	// Optional behavior knobs for Horizon assembly (kept in ViewConfig to avoid breaking schema):
	// - requestedMetrics: []string | []any(strings) | "a,b,c"
	// - maxRecursionDepth: number | string
	// - focus: string (free-form)
	// - spherePaths: []string | []any(strings) | "a,b,c"
	// - keywords: []string | []any(strings) | "a,b,c"
	requestedMetrics := []string(nil)
	maxRecursionDepth := 2
	focus := ""
	spherePaths := []string(nil)
	keywords := []string(nil)
	if lens.ViewConfig != nil {
		if v, ok := lens.ViewConfig["requestedMetrics"]; ok {
			requestedMetrics = parseStringSlice(v)
		}
		if v, ok := lens.ViewConfig["maxRecursionDepth"]; ok {
			maxRecursionDepth = parseInt(v, 2)
		}
		if v, ok := lens.ViewConfig["focus"]; ok {
			if s, ok2 := v.(string); ok2 {
				focus = strings.TrimSpace(s)
			}
		}
		if v, ok := lens.ViewConfig["spherePaths"]; ok {
			spherePaths = parseStringSlice(v)
		} else if v, ok := lens.ViewConfig["spheres"]; ok {
			spherePaths = parseStringSlice(v)
		}
		if v, ok := lens.ViewConfig["keywords"]; ok {
			keywords = parseStringSlice(v)
		}
	}
	if maxRecursionDepth < 0 {
		maxRecursionDepth = 0
	}

	// Создаем map для быстрой проверки старых canonical paths
	oldPathsMap := make(map[string]bool)
	for _, path := range allOldStaticPaths {
		oldPathsMap[path] = true
	}

	// Подписываемся на новые в scope
	// Best-effort: avoid duplicate Scout jobs for the same canonicalPath within the same lens/project.
	// We prefetch active (pending/running) scout jobs and skip creation if one already exists.
	activeScoutJobs := map[string]string{} // canonicalPath -> jobId
	if s.scoutingJobsRepo != nil {
		if jobs, jerr := s.scoutingJobsRepo.ListByProjectAndLens(ctx, lens.TenantID, lens.ProjectID, lens.ID, 400); jerr == nil {
			for _, j := range jobs {
				if j == nil {
					continue
				}
				if j.Status != agentmodels.AgentJobStatusPending && j.Status != agentmodels.AgentJobStatusRunning {
					continue
				}
				p := strings.TrimSpace(j.TargetCanonicalPath)
				// Normalize job path so we dedupe even if older jobs were created under alias paths.
				if s.canonicalPathResolver != nil && p != "" {
						// Best-effort: infer fragment type from path prefix.
						ft := models.FragmentTypeOrganization
						switch {
						case strings.HasPrefix(p, "/product/"):
							ft = models.FragmentTypeProduct
						case strings.HasPrefix(p, "/program/"):
							ft = models.FragmentTypeProgram
						case strings.HasPrefix(p, "/market/"):
							ft = models.FragmentTypeMarket
						case strings.HasPrefix(p, "/fac/"):
							ft = models.FragmentTypeFacility
						case strings.HasPrefix(p, "/loc/") || strings.HasPrefix(p, "/region/"):
							ft = models.FragmentTypeGeoRegion
						case strings.HasPrefix(p, "/event/"):
							ft = models.FragmentTypeEvent
						case strings.HasPrefix(p, "/person/"):
							ft = models.FragmentTypePerson
						case strings.HasPrefix(p, "/org/"):
							ft = models.FragmentTypeOrganization
						}
						if resolved, rerr := s.canonicalPathResolver.Resolve(ctx, p, ft); rerr == nil && resolved != nil && strings.TrimSpace(resolved.CanonicalPath) != "" {
						p = resolved.CanonicalPath
					}
				}
				if p == "" {
					continue
				}
				if _, ok := activeScoutJobs[p]; !ok {
					activeScoutJobs[p] = j.ID
				}
			}
		}
	}

	for _, inputPath := range allNewStaticPaths {
		if !oldPathsMap[inputPath] {
			// EARLY CANONICALIZATION AND DEDUPLICATION (UpdateLens): resolve before any processing/job creation.
			var canonicalPath string
			var fragment *models.HorizonFragment
			var isCacheHit bool

			if s.canonicalPathResolver != nil {
				fragmentType := models.FragmentTypeOrganization
				switch {
				case strings.HasPrefix(inputPath, "/product/"):
					fragmentType = models.FragmentTypeProduct
				case strings.HasPrefix(inputPath, "/program/"):
					fragmentType = models.FragmentTypeProgram
				case strings.HasPrefix(inputPath, "/market/"):
					fragmentType = models.FragmentTypeMarket
				case strings.HasPrefix(inputPath, "/fac/"):
					fragmentType = models.FragmentTypeFacility
				case strings.HasPrefix(inputPath, "/loc/") || strings.HasPrefix(inputPath, "/region/"):
					fragmentType = models.FragmentTypeGeoRegion
				case strings.HasPrefix(inputPath, "/event/"):
					fragmentType = models.FragmentTypeEvent
				case strings.HasPrefix(inputPath, "/org/"):
					fragmentType = models.FragmentTypeOrganization
				case strings.HasPrefix(inputPath, "/person/"):
					fragmentType = models.FragmentTypePerson
				}

				resolved, rerr := s.canonicalPathResolver.Resolve(ctx, inputPath, fragmentType)
				if rerr == nil && resolved != nil {
					canonicalPath = resolved.CanonicalPath
					if resolved.ExistingFragment != nil {
						fragment = resolved.ExistingFragment
						isCacheHit = true
					}
				} else {
					canonicalPath = strings.TrimSpace(inputPath)
				}
			} else {
				canonicalPath = strings.TrimSpace(inputPath)
			}

			if fragment == nil {
				var err error
				fragment, err = s.fragmentsRepo.GetByCanonicalPath(ctx, canonicalPath)
				isCacheHit = err == nil && fragment != nil
			}
			isFragmentFreshEnough := false
			if isCacheHit {
				if !fragment.Metadata.LastVerifiedAt.IsZero() && fragment.Metadata.EpistemicStatus != models.EpistemicStatusLowConfidence {
					isFragmentFreshEnough = true
				}
			}
			if isCacheHit && fragment != nil {
				// Обновляем кэш для нового узла
				lens.ScopeFragments[canonicalPath] = fragment.ID
				if ap := strings.TrimSpace(inputPath); ap != "" && ap != strings.TrimSpace(canonicalPath) {
					lens.ScopeFragments[ap] = fragment.ID
				}

				_ = s.fragmentsRepo.IncrementActiveSubscribers(ctx, fragment.ID)
				_ = s.lifecycle.EnsureActive(ctx, fragment.ID)
				// Синхронизируем WatchdogConfig (создать/запустить при ACTIVE)
				if s.watchdogHook != nil {
					s.watchdogHook(ctx, fragment.ID)
				}
			}

			// Создаем ScoutingJob для нового узла в scope (аналогично CreateLens)
			// Это гарантирует, что данные для нового узла будут собраны немедленно,
			// а не ждать первого Watchdog scan (который может быть через 1 час)
			if s.scoutingJobsRepo != nil && !isFragmentFreshEnough {
				if existingJobID, ok := activeScoutJobs[strings.TrimSpace(canonicalPath)]; ok && existingJobID != "" {
					// Skip creating duplicate job; best-effort signal for UI.
					s.publish(ctx, realtime.EventType("horizon.graph.node.upserted"), map[string]interface{}{
						"tenantId":      lens.TenantID,
						"projectId":     lens.ProjectID,
						"lensId":        lens.ID,
						"canonicalPath": strings.TrimSpace(canonicalPath),
						"status":        "scouting",
						"jobId":         existingJobID,
						"hint":          "dedup_existing_scout_job",
					})
					continue
				}
				now := time.Now().UTC()
				state := map[string]interface{}{
					"targetCanonicalPath": canonicalPath,
					"lensId":              lens.ID,
					"currentState":        string(models.ScoutingJobStatePending),
					"scoutingStrategy":    string(models.ScoutingStrategySnapshot),
					// Recursion control (used by ScoutEngine.processDiscoveredRelations)
					"recursionDepth":    0.0,
					"maxRecursionDepth": float64(maxRecursionDepth),
				}
				if ap := strings.TrimSpace(inputPath); ap != "" && ap != strings.TrimSpace(canonicalPath) {
					state["aliasCanonicalPath"] = ap
				}
				if len(requestedMetrics) > 0 {
					state["requestedMetrics"] = requestedMetrics
				}
				if focus != "" {
					state["focus"] = focus
				}
				if len(spherePaths) > 0 {
					state["spherePaths"] = spherePaths
				}
				if len(keywords) > 0 {
					state["keywords"] = keywords
				}
				scoutingJob := &models.ScoutingJob{
					AgentJob: agentmodels.AgentJob{
						TenantID:  lens.TenantID,
						ProjectID: lens.ProjectID,
						Status:    agentmodels.AgentJobStatusPending,
						AgentID:   "scout",
						CreatedAt: now,
						UpdatedAt: now,
						State:     state,
					},
					LensID:              lens.ID,
					TargetCanonicalPath: canonicalPath,
					ScoutingStrategy:    string(models.ScoutingStrategySnapshot),
					BillingContext: models.BillingContext{
						ChargeModel: string(models.ChargeModelBounty),
						ProjectID:   lens.ProjectID,
					},
					CurrentState: models.ScoutingJobStatePending,
				}
				// Создаем job, но не прерываем UpdateLens при ошибке
				if id, err := s.scoutingJobsRepo.Create(ctx, scoutingJob); err == nil {
					activeScoutJobs[strings.TrimSpace(canonicalPath)] = id
				}
			} else if isCacheHit && isFragmentFreshEnough {
				// Best-effort: signal skip for UI.
				s.publish(ctx, realtime.EventType("horizon.scout.job.completed"), map[string]interface{}{
					"tenantId":        lens.TenantID,
					"projectId":       lens.ProjectID,
					"lensId":          lens.ID,
					"jobId":           "",
					"fragmentId":      fragment.ID,
					"canonicalPath":   strings.TrimSpace(canonicalPath),
					"epistemicStatus": string(fragment.Metadata.EpistemicStatus),
					"confidence":      1.0,
					"hint":            "cached_verified_skip_scout",
				})
			}
		}
	}

	return nil
}

// DeleteLens удаляет Lens и управляет подписками
func (s *LensService) DeleteLens(ctx context.Context, lensID string) error {
	// Получаем Lens перед удалением
	lens, err := s.lensesRepo.Get(ctx, lensID)
	if err != nil {
		return fmt.Errorf("failed to get lens: %w", err)
	}

	// Удаляем Lens
	if err := s.lensesRepo.Delete(ctx, lensID); err != nil {
		return fmt.Errorf("failed to delete lens: %w", err)
	}

	// Fast path: if we have ScopeFragments cache, decrement by fragment IDs directly.
	// This avoids alias/canonicalPath mismatches when lens.Scope contains non-canonical paths.
	if lens.ScopeFragments != nil && len(lens.ScopeFragments) > 0 {
		seen := map[string]struct{}{}
		for _, fragmentID := range lens.ScopeFragments {
			fragmentID = strings.TrimSpace(fragmentID)
			if fragmentID == "" {
				continue
			}
			if _, ok := seen[fragmentID]; ok {
				continue
			}
			seen[fragmentID] = struct{}{}
			_ = s.fragmentsRepo.DecrementActiveSubscribers(ctx, fragmentID)
			_ = s.lifecycle.CheckAndTransition(ctx, fragmentID)
			if s.watchdogHook != nil {
				s.watchdogHook(ctx, fragmentID)
			}
		}
		return nil
	}

	// Уменьшаем activeSubscribers для всех HorizonFragment в scope
	for _, canonicalPath := range lens.Scope {
		// ОПТИМИЗАЦИЯ: Используем кэш fragmentID если доступен
		var fragmentID string
		if lens.ScopeFragments != nil {
			fragmentID = lens.ScopeFragments[canonicalPath]
		}

		if fragmentID != "" {
			// Быстрый путь: используем cached fragment ID (прямой key lookup)
			_ = s.fragmentsRepo.DecrementActiveSubscribers(ctx, fragmentID)
			_ = s.lifecycle.CheckAndTransition(ctx, fragmentID)
			if s.watchdogHook != nil {
				s.watchdogHook(ctx, fragmentID)
			}
		} else {
			// Fallback: composite index query
			fragment, err := s.fragmentsRepo.GetByCanonicalPath(ctx, canonicalPath)
			if err == nil && fragment != nil {
				_ = s.fragmentsRepo.DecrementActiveSubscribers(ctx, fragment.ID)
				_ = s.lifecycle.CheckAndTransition(ctx, fragment.ID)
				if s.watchdogHook != nil {
					s.watchdogHook(ctx, fragment.ID)
				}
			}
		}
	}

	return nil
}

// GetLens получает Lens по ID
func (s *LensService) GetLens(ctx context.Context, lensID string) (*models.Lens, error) {
	return s.lensesRepo.Get(ctx, lensID)
}

// ListLensesByProject получает список Lens для проекта
func (s *LensService) ListLensesByProject(ctx context.Context, tenantID, projectID string, status *string, limit int) ([]*models.Lens, error) {
	return s.lensesRepo.ListByProject(ctx, tenantID, projectID, status, limit)
}

// DeleteLensesByProject удаляет все Lens для проекта с правильной обработкой подписок
func (s *LensService) DeleteLensesByProject(ctx context.Context, tenantID, projectID string) error {
	// Получаем все линзы проекта (без ограничения по статусу)
	allStatus := ""
	lenses, err := s.lensesRepo.ListByProject(ctx, tenantID, projectID, &allStatus, 1000)
	if err != nil {
		return fmt.Errorf("failed to list lenses for project %s: %w", projectID, err)
	}

	// Удаляем каждую линзу через DeleteLens для правильной обработки подписок
	var errors []string
	for _, lens := range lenses {
		if err := s.DeleteLens(ctx, lens.ID); err != nil {
			errors = append(errors, fmt.Sprintf("failed to delete lens %s: %v", lens.ID, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("errors deleting lenses: %s", strings.Join(errors, "; "))
	}

	return nil
}

// CalculateMetrics пересчитывает метрики для Lens
func (s *LensService) CalculateMetrics(ctx context.Context, lensID string) (map[string]interface{}, error) {
	lens, err := s.lensesRepo.Get(ctx, lensID)
	if err != nil {
		return nil, fmt.Errorf("failed to get lens: %w", err)
	}

	return s.calculator.CalculateLensMetrics(ctx, lens)
}
