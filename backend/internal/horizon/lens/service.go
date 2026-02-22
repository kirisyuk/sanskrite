package lens

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/critical_path"
	horizondomain "github.com/igorkirisyuk/sanskrite/internal/horizon/domain"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/rtctx"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/trace"
	agentmodels "github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// effectiveDomainModesForLens returns domain modes for job state.
// Prefers lens.DomainModes; falls back to computing from ViewConfig["domains"] for legacy lenses.
func effectiveDomainModesForLens(lens *models.Lens) []string {
	if lens == nil {
		return nil
	}
	if len(lens.DomainModes) > 0 {
		return lens.DomainModes
	}
	if lens.ViewConfig == nil {
		return nil
	}
	raw, ok := lens.ViewConfig["domains"]
	if !ok || raw == nil {
		return nil
	}
	contractTypes := contractTypesFromDomainsRaw(raw)
	if len(contractTypes) == 0 {
		return nil
	}
	return horizondomain.DomainModesForContractTypes(contractTypes)
}

func contractTypesFromDomainsRaw(raw interface{}) []string {
	if raw == nil {
		return nil
	}
	out := make([]string, 0, 8)
	add := func(v string) {
		v = strings.TrimSpace(strings.ToLower(v))
		if v != "" {
			out = append(out, v)
		}
	}
	switch t := raw.(type) {
	case []agents.DomainResolution:
		for _, d := range t {
			add(d.ContractType)
		}
	case []map[string]interface{}:
		for _, m := range t {
			add(getStringFieldFromMap(m, "contractType", "ContractType"))
		}
	case []interface{}:
		for _, it := range t {
			m, ok := it.(map[string]interface{})
			if !ok {
				continue
			}
			add(getStringFieldFromMap(m, "contractType", "ContractType"))
		}
	}
	return out
}

func getStringFieldFromMap(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok {
				s = strings.TrimSpace(s)
				if s != "" {
					return s
				}
			}
		}
	}
	return ""
}

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

// addDeferredScopePathIfUnderLimit adds canonicalPath to lens scope if under maxCount (for deferred/cache-hit paths).
func addDeferredScopePathIfUnderLimit(ctx context.Context, lensesRepo *repo.LensesRepository, lens *models.Lens, lensID, canonicalPath string, maxCount int) {
	if lensesRepo == nil || lens == nil || lensID == "" || strings.TrimSpace(canonicalPath) == "" || maxCount <= 0 {
		return
	}
	canonicalPath = strings.TrimSpace(canonicalPath)
	res, err := lensesRepo.AddScopeCanonicalPathAutoLimited(ctx, lensID, canonicalPath, maxCount)
	if err != nil {
		return
	}
	if lens.ViewConfig == nil {
		lens.ViewConfig = map[string]interface{}{}
	}
	lens.ViewConfig["deferredScopeAddCount"] = res.DeferredScopeAddCount
	if (res.Added || res.AlreadyInScope) && !scopeContainsPath(lens.Scope, canonicalPath) {
		lens.Scope = append(lens.Scope, canonicalPath)
	}
}

func scopeContainsPath(scope []string, canonicalPath string) bool {
	for _, s := range scope {
		if strings.TrimSpace(s) == canonicalPath {
			return true
		}
	}
	return false
}

func preserveDeferredScopeAddCount(lens, oldLens *models.Lens) {
	if lens == nil || oldLens == nil {
		return
	}
	oldCount, oldOK := deferredScopeAddCountFromViewConfig(oldLens.ViewConfig)
	if !oldOK {
		return
	}
	newCount, newOK := deferredScopeAddCountFromViewConfig(lens.ViewConfig)
	if newOK && newCount >= oldCount {
		return
	}
	if lens.ViewConfig == nil {
		lens.ViewConfig = map[string]interface{}{}
	}
	lens.ViewConfig["deferredScopeAddCount"] = oldCount
}

func deferredScopeAddCountFromViewConfig(viewConfig map[string]interface{}) (int, bool) {
	if viewConfig == nil {
		return 0, false
	}
	raw, ok := viewConfig["deferredScopeAddCount"]
	if !ok {
		return 0, false
	}
	switch n := raw.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func inferFragmentTypeFromPath(path string) models.FragmentType {
	switch {
	case strings.HasPrefix(path, "/product/"):
		return models.FragmentTypeProduct
	case strings.HasPrefix(path, "/program/"):
		return models.FragmentTypeProgram
	case strings.HasPrefix(path, "/market/"):
		return models.FragmentTypeMarket
	case strings.HasPrefix(path, "/fac/"):
		return models.FragmentTypeFacility
	case strings.HasPrefix(path, "/loc/"), strings.HasPrefix(path, "/region/"):
		return models.FragmentTypeGeoRegion
	case strings.HasPrefix(path, "/event/"):
		return models.FragmentTypeEvent
	case strings.HasPrefix(path, "/person/"):
		return models.FragmentTypePerson
	case strings.HasPrefix(path, "/media/"):
		return models.FragmentTypeMediaChannel
	case strings.HasPrefix(path, "/org/"), strings.HasPrefix(path, "/gov/"):
		return models.FragmentTypeOrganization
	case strings.HasPrefix(path, "/domain/"):
		return models.FragmentTypeDomain
	default:
		return models.FragmentTypeOrganization
	}
}

func (s *LensService) canonicalizeScopeEntries(ctx context.Context, lens *models.Lens) bool {
	if s == nil || lens == nil || len(lens.Scope) == 0 {
		return false
	}
	changed := false
	seen := map[string]struct{}{}
	out := make([]string, 0, len(lens.Scope))
	for _, raw := range lens.Scope {
		orig := strings.TrimSpace(raw)
		if orig == "" {
			changed = true
			continue
		}
		cp := orig

		// Query scopes are kept as-is; they resolve later.
		if strings.HasPrefix(cp, "/query/") {
			if _, ok := seen[cp]; ok {
				changed = true
				continue
			}
			seen[cp] = struct{}{}
			out = append(out, cp)
			continue
		}

		// Domains are contract paths; normalize but do not require fragments to exist.
		if strings.HasPrefix(cp, "/domain/") {
			if norm, nerr := taxonomy.NormalizeDomainCanonicalPath(cp); nerr == nil && strings.TrimSpace(norm) != "" {
				cp = strings.TrimSpace(norm)
			}
		}

		// Validate canonical path syntax; invalid paths are dropped.
		if err := taxonomy.ValidateCanonicalPath(cp); err != nil {
			changed = true
			continue
		}

		// Canonicalize only when we can verify an existing fragment.
		if !strings.HasPrefix(cp, "/domain/") && s.canonicalPathResolver != nil {
			ft := inferFragmentTypeFromPath(cp)
			if resolved, rerr := s.canonicalPathResolver.Resolve(ctx, cp, ft); rerr == nil && resolved != nil && strings.TrimSpace(resolved.CanonicalPath) != "" {
				candidate := strings.TrimSpace(resolved.CanonicalPath)
				if resolved.ExistingFragment != nil {
					cp = candidate
				} else if s.fragmentsRepo != nil {
					if f, ferr := s.fragmentsRepo.GetByCanonicalPath(ctx, candidate); ferr == nil && f != nil {
						cp = candidate
					}
				}
			}
		}

		if cp != orig {
			changed = true
			if lens.ScopeFragments != nil {
				if fid, ok := lens.ScopeFragments[orig]; ok {
					delete(lens.ScopeFragments, orig)
					if _, ok2 := lens.ScopeFragments[cp]; !ok2 && strings.TrimSpace(fid) != "" {
						lens.ScopeFragments[cp] = fid
					}
				}
			}
		}

		// If scope fragment mapping points to a missing fragment, drop it (safe cleanup).
		if lens.ScopeFragments != nil && s.fragmentsRepo != nil && !strings.HasPrefix(cp, "/domain/") {
			if fid := strings.TrimSpace(lens.ScopeFragments[cp]); fid != "" {
				if f, ferr := s.fragmentsRepo.Get(ctx, fid); ferr != nil || f == nil {
					delete(lens.ScopeFragments, cp)
					changed = true
					continue
				}
			}
		}

		if _, ok := seen[cp]; ok {
			changed = true
			continue
		}
		seen[cp] = struct{}{}
		out = append(out, cp)
	}
	if changed {
		lens.Scope = out
	}
	return changed
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

func normalizeDomainContractMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	out := map[string]interface{}{}
	get := func(keys ...string) (interface{}, bool) {
		for _, k := range keys {
			if v, ok := m[k]; ok {
				return v, true
			}
		}
		return nil, false
	}
	setString := func(key string, v interface{}) {
		if s, ok := v.(string); ok {
			if trimmed := strings.TrimSpace(s); trimmed != "" {
				out[key] = trimmed
			}
		}
	}
	if v, ok := get("canonicalPath", "CanonicalPath"); ok {
		setString("canonicalPath", v)
	}
	if v, ok := get("contractType", "ContractType"); ok {
		setString("contractType", v)
	}
	if v, ok := get("title", "Title"); ok {
		setString("title", v)
	}
	if v, ok := get("description", "Description"); ok {
		setString("description", v)
	}
	if v, ok := get("facets", "Facets"); ok && v != nil {
		out["facets"] = v
	}
	if v, ok := get("projections", "Projections"); ok && v != nil {
		out["projections"] = v
	}
	if v, ok := get("relatedDomains", "RelatedDomains"); ok && v != nil {
		out["relatedDomains"] = v
	}
	if v, ok := get("affectedDomains", "AffectedDomains"); ok && v != nil {
		out["affectedDomains"] = v
	}
	if v, ok := get("firstOrderEntities", "FirstOrderEntities"); ok && v != nil {
		out["firstOrderEntities"] = v
	}
	if v, ok := get("confidence", "Confidence"); ok && v != nil {
		out["confidence"] = v
	}
	if v, ok := get("reason", "Reason"); ok && v != nil {
		out["reason"] = v
	}
	known := map[string]struct{}{
		"canonicalpath": {}, "contracttype": {}, "title": {}, "description": {}, "facets": {}, "projections": {},
		"relateddomains": {}, "affecteddomains": {}, "firstorderentities": {}, "confidence": {}, "reason": {},
	}
	for k, v := range m {
		lk := strings.ToLower(strings.TrimSpace(k))
		if _, ok := known[lk]; ok {
			continue
		}
		out[k] = v
	}
	return out
}

func normalizeDomainContractsForState(raw interface{}) []map[string]interface{} {
	switch t := raw.(type) {
	case []agents.DomainResolution:
		out := make([]map[string]interface{}, 0, len(t))
		for _, d := range t {
			m := map[string]interface{}{
				"canonicalPath": strings.TrimSpace(d.CanonicalPath),
				"contractType":  strings.TrimSpace(d.ContractType),
				"title":         strings.TrimSpace(d.Title),
				"description":   strings.TrimSpace(d.Description),
				"confidence":    d.Confidence,
				"reason":        strings.TrimSpace(d.Reason),
			}
			if len(d.Facets) > 0 {
				m["facets"] = d.Facets
			}
			if len(d.Projections) > 0 {
				m["projections"] = d.Projections
			}
			if len(d.RelatedDomains) > 0 {
				m["relatedDomains"] = d.RelatedDomains
			}
			if len(d.AffectedDomains) > 0 {
				m["affectedDomains"] = d.AffectedDomains
			}
			if len(d.FirstOrderEntities) > 0 {
				m["firstOrderEntities"] = d.FirstOrderEntities
			}
			out = append(out, m)
		}
		return out
	case []map[string]interface{}:
		out := make([]map[string]interface{}, 0, len(t))
		for _, m := range t {
			if norm := normalizeDomainContractMap(m); norm != nil {
				out = append(out, norm)
			}
		}
		return out
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(t))
		for _, item := range t {
			if m, ok := item.(map[string]interface{}); ok {
				if norm := normalizeDomainContractMap(m); norm != nil {
					out = append(out, norm)
				}
			}
		}
		return out
	default:
		return nil
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
	lensesRepo            *repo.LensesRepository
	fragmentsRepo         *repo.HorizonFragmentsRepository
	scoutingJobsRepo      *repo.ScoutingJobsRepository
	ontologyFactsRepo     *corerepo.OntologyFactsRepository
	lifecycle             *LifecycleManager
	calculator            *MetricsCalculator
	watchdogHook          func(ctx context.Context, fragmentID string)
	queryResolver         *QueryResolver
	realtime              *realtime.Publisher
	criticalPathDetector  *critical_path.Detector         // Optional: for critical path detection
	edgesRepo             *repo.DependencyEdgesRepository // Required for critical path detection
	canonicalPathResolver *scout.CanonicalPathResolver    // Optional: for early canonicalization and deduplication

	// Optional cleanup repos for cascade delete (best-effort, non-critical)
	lensItemsRepo     *repo.LensItemsRepository
	exposurePathsRepo *repo.ExposurePathsRepository
	materializedRepo  *repo.MaterializedArtifactsRepository
	lensArtifactsRepo *repo.LensArtifactsRepository
	spendLedger       *repo.HorizonSpendLedger
	agentJobsRepo     AgentJobsDeleter // interface for deleting agent jobs by ID
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

// WithLensArtifactsRepo wires LensArtifactsRepository for cascade deletion.
func (s *LensService) WithLensArtifactsRepo(repo *repo.LensArtifactsRepository) *LensService {
	s.lensArtifactsRepo = repo
	return s
}

// WithCanonicalPathResolver sets the canonical path resolver for early canonicalization and deduplication.
func (s *LensService) WithCanonicalPathResolver(resolver *scout.CanonicalPathResolver) *LensService {
	s.canonicalPathResolver = resolver
	return s
}

// AgentJobsDeleter abstracts agent_jobs deletion for cascade cleanup.
// This avoids importing the full AgentJobsRepository into the horizon/lens package.
type AgentJobsDeleter interface {
	Delete(ctx context.Context, id string) error
}

// WithCleanupRepos wires optional repositories for cascade cleanup on lens deletion.
// All are best-effort: errors are logged but do not fail DeleteLens.
func (s *LensService) WithCleanupRepos(
	lensItemsRepo *repo.LensItemsRepository,
	exposurePathsRepo *repo.ExposurePathsRepository,
	materializedRepo *repo.MaterializedArtifactsRepository,
	spendLedger *repo.HorizonSpendLedger,
	agentJobsDeleter AgentJobsDeleter,
) *LensService {
	s.lensItemsRepo = lensItemsRepo
	s.exposurePathsRepo = exposurePathsRepo
	s.materializedRepo = materializedRepo
	s.spendLedger = spendLedger
	s.agentJobsRepo = agentJobsDeleter
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
	// - specialties: []string | []any(strings) | "a,b,c" (optional task focus modules: "management", "products_and_services")
	requestedMetrics := []string(nil)
	maxRecursionDepth := 2
	focus := ""
	spherePaths := []string(nil)
	keywords := []string(nil)
	specialties := []string(nil)
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
		if v, ok := lens.ViewConfig["specialties"]; ok {
			specialties = taxonomy.NormalizeSpecialties(parseStringSlice(v))
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
		"tenantId":          strings.TrimSpace(lens.TenantID),
		"projectId":         strings.TrimSpace(lens.ProjectID),
		"lensId":            lensID,
		"staticPathsN":      len(staticPaths),
		"queryAddressesN":   len(queryAddresses),
		"staticPathsTop":    trace.StrSlice(staticPaths, 12),
		"queryAddressesTop": trace.StrSlice(queryAddresses, 12),
	})

	// Резолвим QueryAddress в canonical paths
	now := time.Now().UTC()
	resolvedQueries, resolveErrors := resolveQueryAddresses(ctx, s.queryResolver, lens.TenantID, queryAddresses)
	trace.Log(ctx, "scope", "scope.query_addresses.resolved", map[string]any{
		"tenantId":  strings.TrimSpace(lens.TenantID),
		"projectId": strings.TrimSpace(lens.ProjectID),
		"lensId":    lensID,
		"resolvedN": len(resolvedQueries),
		"errorsN":   len(resolveErrors),
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

	// Deferred scope paths: anchors from intent, added to scope only when Scout creates fragment (max 5 per lens).
	deferredScopePaths := []string(nil)
	if lens.ViewConfig != nil {
		if v, ok := lens.ViewConfig["deferredScopePaths"]; ok {
			deferredScopePaths = parseStringSlice(v)
			deferredScopePaths = dedupeCanonicalPaths(deferredScopePaths)
		}
	}
	trace.Log(ctx, "scope", "scope.deduped", map[string]any{
		"tenantId":            strings.TrimSpace(lens.TenantID),
		"projectId":           strings.TrimSpace(lens.ProjectID),
		"lensId":              lensID,
		"staticPathsN":        len(staticPaths),
		"staticPathsTop":      trace.StrSlice(staticPaths, 20),
		"deferredScopePathsN": len(deferredScopePaths),
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
					ft := scout.InferFragmentTypeFromCanonicalPath(p)
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
		// Domain is config, not a graph node — skip /domain/ paths.
		if strings.HasPrefix(strings.TrimSpace(inputPath), "/domain/") {
			continue
		}
		// EARLY CANONICALIZATION AND DEDUPLICATION: Resolve canonical path before any processing
		// This ensures we operate on canonicalized and deduplicated paths throughout the pipeline
		var canonicalPath string
		var fragment *models.HorizonFragment
		var isCacheHit bool

		if s.canonicalPathResolver != nil {
			fragmentType := scout.InferFragmentTypeFromCanonicalPath(inputPath)

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

		// Scope-only lens creation should not trigger scouting jobs.
		// We still publish a placeholder node for unknown entities so the UI can render the intent.
		if SkipScouting(ctx) {
			if !isCacheHit {
				s.publish(ctx, realtime.EventType("horizon.graph.node.upserted"), map[string]interface{}{
					"tenantId":      lens.TenantID,
					"projectId":     lens.ProjectID,
					"lensId":        lensID,
					"canonicalPath": strings.TrimSpace(canonicalPath),
					"status":        "pending",
					"hint":          "just_open_skip_scouting",
				})
			}
			continue
		}

		// Create ScoutingJob only when we DON'T already have sufficiently verified data.
		// This prevents excessive Cloud Tasks for queries that are already in the system (e.g., Astana/LRT).
		if s.scoutingJobsRepo != nil && !isFragmentFreshEnough {
			if existingJobID, ok := activeScoutJobs[strings.TrimSpace(canonicalPath)]; ok && existingJobID != "" {
				trace.Log(ctx, "root_jobs", "root_jobs.dedup_skip_active", map[string]any{
					"tenantId":      strings.TrimSpace(lens.TenantID),
					"projectId":     strings.TrimSpace(lens.ProjectID),
					"lensId":        lensID,
					"canonicalPath": strings.TrimSpace(canonicalPath),
					"existingJobId": existingJobID,
					"cacheHit":      isCacheHit,
					"freshEnough":   isFragmentFreshEnough,
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
			if len(specialties) > 0 {
				state["specialties"] = specialties
			}
			// Supply chain mode needs higher breadth to build branching chains.
			// Keep general behavior unchanged by only setting this when supply_chain specialty is enabled.
			if taxonomy.HasSpecialty(specialties, taxonomy.SpecialtySupplyChain) {
				state["maxChildrenPerJob"] = 80.0
			}
			// Pass through originalQuery and decompositionHints for MARKET-aware research.
			if oq, ok := lens.ViewConfig["originalQuery"].(string); ok && strings.TrimSpace(oq) != "" {
				state["originalQuery"] = strings.TrimSpace(oq)
			}
			if dh, ok := lens.ViewConfig["decompositionHints"]; ok && dh != nil {
				state["decompositionHints"] = dh
			}
			// Pass through resolved domains (knowledge contracts) so domain jobs can expand deterministically.
			if ds, ok := lens.ViewConfig["domains"]; ok && ds != nil {
				state["domains"] = normalizeDomainContractsForState(ds)
			}
			// DomainModes from lens (execution contracts) — preferred when set.
			// Fallback: compute from ViewConfig["domains"] for legacy lenses.
			if domainModes := effectiveDomainModesForLens(lens); len(domainModes) > 0 {
				state["domainModes"] = domainModes
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
				"tenantId":        strings.TrimSpace(lens.TenantID),
				"projectId":       strings.TrimSpace(lens.ProjectID),
				"lensId":          lensID,
				"canonicalPath":   strings.TrimSpace(canonicalPath),
				"fragmentId":      strings.TrimSpace(fragment.ID),
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

	// Process deferred scope paths (anchors): create scout jobs. Scope added when Scout creates fragment (max 5 per lens).
	for _, inputPath := range deferredScopePaths {
		if strings.HasPrefix(strings.TrimSpace(inputPath), "/domain/") {
			continue
		}
		var canonicalPath string
		var fragment *models.HorizonFragment
		var isCacheHit bool
		if s.canonicalPathResolver != nil {
			resolved, err := s.canonicalPathResolver.Resolve(ctx, inputPath, scout.InferFragmentTypeFromCanonicalPath(inputPath))
			if err == nil && resolved != nil {
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
		if isCacheHit && fragment != nil && !fragment.Metadata.LastVerifiedAt.IsZero() && fragment.Metadata.EpistemicStatus != models.EpistemicStatusLowConfidence {
			isFragmentFreshEnough = true
		}
		// Cache hit for deferred path: add to scope so node shows (max 5 per lens).
		if isCacheHit && fragment != nil {
			addDeferredScopePathIfUnderLimit(ctx, s.lensesRepo, lens, lensID, strings.TrimSpace(canonicalPath), models.MaxAutoScopeAddsPerLens)
			cacheHits++
			lens.ScopeFragments[canonicalPath] = fragment.ID
			if ap := strings.TrimSpace(inputPath); ap != "" && ap != strings.TrimSpace(canonicalPath) {
				lens.ScopeFragments[ap] = fragment.ID
			}
			_ = s.fragmentsRepo.IncrementActiveSubscribers(ctx, fragment.ID)
			_ = s.lifecycle.EnsureActive(ctx, fragment.ID)
			if s.watchdogHook != nil {
				s.watchdogHook(ctx, fragment.ID)
			}
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
			continue
		}
		if SkipScouting(ctx) {
			if !isCacheHit {
				s.publish(ctx, realtime.EventType("horizon.graph.node.upserted"), map[string]interface{}{
					"tenantId":      lens.TenantID,
					"projectId":     lens.ProjectID,
					"lensId":        lensID,
					"canonicalPath": strings.TrimSpace(canonicalPath),
					"status":        "pending",
					"hint":          "just_open_skip_scouting",
				})
			}
			continue
		}
		if s.scoutingJobsRepo != nil && !isFragmentFreshEnough {
			if existingJobID, ok := activeScoutJobs[strings.TrimSpace(canonicalPath)]; ok && existingJobID != "" {
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
			now := time.Now().UTC()
			state := map[string]interface{}{
				"targetCanonicalPath":           canonicalPath,
				"lensId":                        lensID,
				"currentState":                  string(models.ScoutingJobStatePending),
				"scoutingStrategy":              string(models.ScoutingStrategySnapshot),
				"recursionDepth":                0.0,
				"maxRecursionDepth":             float64(maxRecursionDepth),
				models.StateKeyDeferredScopeAdd: true,
			}
			if ap := strings.TrimSpace(inputPath); ap != "" && ap != strings.TrimSpace(canonicalPath) {
				state["aliasCanonicalPath"] = ap
			}
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
			if len(specialties) > 0 {
				state["specialties"] = specialties
			}
			if taxonomy.HasSpecialty(specialties, taxonomy.SpecialtySupplyChain) {
				state["maxChildrenPerJob"] = 80.0
			}
			if oq, ok := lens.ViewConfig["originalQuery"].(string); ok && strings.TrimSpace(oq) != "" {
				state["originalQuery"] = strings.TrimSpace(oq)
			}
			if dh, ok := lens.ViewConfig["decompositionHints"]; ok && dh != nil {
				state["decompositionHints"] = dh
			}
			if ds, ok := lens.ViewConfig["domains"]; ok && ds != nil {
				state["domains"] = normalizeDomainContractsForState(ds)
			}
			if domainModes := effectiveDomainModesForLens(lens); len(domainModes) > 0 {
				state["domainModes"] = domainModes
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
					"deferred":      true,
					"error":         createErr.Error(),
				})
			}
			if strings.TrimSpace(jobID) != "" {
				activeScoutJobs[strings.TrimSpace(canonicalPath)] = strings.TrimSpace(jobID)
				s.publish(ctx, realtime.EventType("horizon.graph.node.upserted"), map[string]interface{}{
					"tenantId":      lens.TenantID,
					"projectId":     lens.ProjectID,
					"lensId":        lensID,
					"canonicalPath": canonicalPath,
					"status":        "scouting",
					"jobId":         strings.TrimSpace(jobID),
				})
			}
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
	preserveDeferredScopeAddCount(lens, oldLens)

	// Инициализируем кэш если отсутствует (нужно для канонизации).
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

	// Canonicalize/cleanup scope entries (best-effort) before diffing.
	_ = s.canonicalizeScopeEntries(ctx, lens)

	// Управляем подписками: отписываемся от старых, подписываемся на новые
	oldScope := make(map[string]bool)
	for _, path := range oldLens.Scope {
		oldScope[path] = true
	}

	newScope := make(map[string]bool)
	for _, path := range lens.Scope {
		newScope[path] = true
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
	// - specialties: []string | []any(strings) | "a,b,c"
	requestedMetrics := []string(nil)
	maxRecursionDepth := 2
	focus := ""
	spherePaths := []string(nil)
	keywords := []string(nil)
	specialties := []string(nil)
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
		if v, ok := lens.ViewConfig["specialties"]; ok {
			specialties = taxonomy.NormalizeSpecialties(parseStringSlice(v))
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
					case strings.HasPrefix(p, "/media/"):
						ft = models.FragmentTypeMediaChannel
					case strings.HasPrefix(p, "/org/"):
						ft = models.FragmentTypeOrganization
					case strings.HasPrefix(p, "/gov/"):
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
				case strings.HasPrefix(inputPath, "/gov/"):
					fragmentType = models.FragmentTypeOrganization
				case strings.HasPrefix(inputPath, "/person/"):
					fragmentType = models.FragmentTypePerson
				case strings.HasPrefix(inputPath, "/media/"):
					fragmentType = models.FragmentTypeMediaChannel
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

			// Pin/unpin (scope-only) updates should not trigger scouting.
			if SkipScouting(ctx) {
				continue
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
				if len(specialties) > 0 {
					state["specialties"] = specialties
				}
				// Pass through originalQuery + decompositionHints + domains for consistency with CreateLens jobs.
				if lens.ViewConfig != nil {
					if oq, ok := lens.ViewConfig["originalQuery"].(string); ok && strings.TrimSpace(oq) != "" {
						state["originalQuery"] = strings.TrimSpace(oq)
					}
					if dh, ok := lens.ViewConfig["decompositionHints"]; ok && dh != nil {
						state["decompositionHints"] = dh
					}
					if ds, ok := lens.ViewConfig["domains"]; ok && ds != nil {
						state["domains"] = normalizeDomainContractsForState(ds)
					}
					if domainModes := effectiveDomainModesForLens(lens); len(domainModes) > 0 {
						state["domainModes"] = domainModes
					}
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

	// Удаляем Lens document
	if err := s.lensesRepo.Delete(ctx, lensID); err != nil {
		return fmt.Errorf("failed to delete lens: %w", err)
	}

	// ── Cascade cleanup (best-effort, non-critical) ──
	// Delete orphaned lens-specific data that would otherwise remain in Firestore.
	s.cascadeDeleteLensData(ctx, lens.TenantID, lens.ProjectID, lensID)

	// ── Fragment subscription cleanup ──
	// Fast path: if we have ScopeFragments cache, decrement by fragment IDs directly.
	// This avoids alias/canonicalPath mismatches when lens.Scope contains non-canonical paths.
	if len(lens.ScopeFragments) > 0 {
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

// cascadeDeleteLensData performs best-effort cleanup of lens-specific orphan data.
// Errors are logged but do not propagate (lens deletion succeeds regardless).
func (s *LensService) cascadeDeleteLensData(ctx context.Context, tenantID, projectID, lensID string) {
	tenantID = strings.TrimSpace(tenantID)
	projectID = strings.TrimSpace(projectID)
	lensID = strings.TrimSpace(lensID)
	if lensID == "" {
		return
	}

	// 1. LensItems (horizon_lens_items)
	if s.lensItemsRepo != nil {
		if tenantID != "" {
			if _, err := s.lensItemsRepo.DeleteByTenantAndLens(ctx, tenantID, lensID); err != nil {
				log.Printf("[LensService] cascadeDeleteLensData: failed to delete lens items for %s: %v", lensID, err)
			}
		} else {
			if err := s.lensItemsRepo.DeleteByLens(ctx, lensID); err != nil {
				log.Printf("[LensService] cascadeDeleteLensData: failed to delete lens items for %s: %v", lensID, err)
			}
		}
	}

	// 2. MaterializedArtifacts (horizon_materialized_artifacts)
	if s.materializedRepo != nil {
		if tenantID != "" {
			if _, err := s.materializedRepo.DeleteByTenantAndLens(ctx, tenantID, lensID); err != nil {
				log.Printf("[LensService] cascadeDeleteLensData: failed to delete materialized artifacts for %s: %v", lensID, err)
			}
		} else {
			if _, err := s.materializedRepo.DeleteByLens(ctx, lensID); err != nil {
				log.Printf("[LensService] cascadeDeleteLensData: failed to delete materialized artifacts for %s: %v", lensID, err)
			}
		}
	}

	// 2.5. LensArtifacts (horizon_lens_artifacts)
	if s.lensArtifactsRepo != nil {
		if err := s.lensArtifactsRepo.DeleteByLens(ctx, lensID); err != nil {
			log.Printf("[LensService] cascadeDeleteLensData: failed to delete lens artifacts for %s: %v", lensID, err)
		}
	}

	// 3. ExposurePaths (horizon_exposure_paths)
	if s.exposurePathsRepo != nil && tenantID != "" {
		if _, err := s.exposurePathsRepo.DeleteByLens(ctx, tenantID, lensID); err != nil {
			log.Printf("[LensService] cascadeDeleteLensData: failed to delete exposure paths for %s: %v", lensID, err)
		}
	}

	// 4. SpendLedger (horizon_lens_spend)
	if s.spendLedger != nil && tenantID != "" {
		if _, err := s.spendLedger.DeleteByLens(ctx, tenantID, lensID); err != nil {
			log.Printf("[LensService] cascadeDeleteLensData: failed to delete lens spend for %s: %v", lensID, err)
		}
		if _, err := s.spendLedger.DeleteCascadeSpendByLens(ctx, tenantID, lensID); err != nil {
			log.Printf("[LensService] cascadeDeleteLensData: failed to delete cascade spend for %s: %v", lensID, err)
		}
	}

	// 5. ScoutingJobs (agent_jobs with state.lensId = lensID)
	// Best-effort: delete pending/running scout jobs for this lens to avoid orphan work.
	if s.scoutingJobsRepo != nil && s.agentJobsRepo != nil && tenantID != "" {
		jobs := ([]*models.ScoutingJob)(nil)
		var err error
		// Prefer project-scoped listing for higher recall and better indexes.
		if projectID != "" {
			jobs, err = s.scoutingJobsRepo.ListByProjectAndLens(ctx, tenantID, projectID, lensID, 2000)
		} else {
			jobs, err = s.scoutingJobsRepo.ListByLens(ctx, tenantID, lensID, 2000)
		}
		if err == nil {
			for _, j := range jobs {
				if j == nil || j.ID == "" {
					continue
				}
				// Only delete pending/running jobs; completed/failed are historical artifacts
				if j.Status != agentmodels.AgentJobStatusPending && j.Status != agentmodels.AgentJobStatusRunning {
					continue
				}
				if err := s.agentJobsRepo.Delete(ctx, j.ID); err != nil {
					log.Printf("[LensService] cascadeDeleteLensData: failed to delete scout job %s for lens %s: %v", j.ID, lensID, err)
				}
			}
		}
	}
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

// TransferLens moves a lens from one project to another within the same tenant.
// This updates the lens's projectId and preserves all associated data (artifacts, scope, etc.).
// Scout jobs remain in the original project as historical records.
func (s *LensService) TransferLens(ctx context.Context, lensID, targetProjectID string) (*models.Lens, error) {
	lensID = strings.TrimSpace(lensID)
	targetProjectID = strings.TrimSpace(targetProjectID)

	if lensID == "" {
		return nil, fmt.Errorf("lensId is required")
	}
	if targetProjectID == "" {
		return nil, fmt.Errorf("targetProjectId is required")
	}

	// Get the lens
	lens, err := s.lensesRepo.Get(ctx, lensID)
	if err != nil {
		return nil, fmt.Errorf("failed to get lens: %w", err)
	}
	if lens == nil {
		return nil, fmt.Errorf("lens not found")
	}

	// Check if already in target project
	if lens.ProjectID == targetProjectID {
		return lens, nil // Already in target project, no-op
	}

	sourceProjectID := lens.ProjectID

	// Update the project ID
	lens.ProjectID = targetProjectID
	lens.UpdatedAt = time.Now().UTC()

	// Save the updated lens
	if err := s.lensesRepo.Update(ctx, lens); err != nil {
		return nil, fmt.Errorf("failed to update lens: %w", err)
	}

	// Log the transfer
	trace.Log(ctx, "lens_transfer", "lens.transfer.ok", map[string]any{
		"tenantId":        lens.TenantID,
		"lensId":          lensID,
		"sourceProjectId": sourceProjectID,
		"targetProjectId": targetProjectID,
		"scopeN":          len(lens.Scope),
	})

	// Best-effort: publish realtime event for UI updates
	if s.realtime != nil {
		_ = s.realtime.PublishJSON(ctx, realtime.EventType("horizon.lens.transferred"), lens.TenantID, map[string]interface{}{
			"tenantId":        lens.TenantID,
			"lensId":          lensID,
			"lensName":        lens.Name,
			"sourceProjectId": sourceProjectID,
			"targetProjectId": targetProjectID,
		})
	}

	return lens, nil
}
