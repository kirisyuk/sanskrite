package horizon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	horizondomain "github.com/igorkirisyuk/sanskrite/internal/horizon/domain"
	hlens "github.com/igorkirisyuk/sanskrite/internal/horizon/lens"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/rtctx"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/trace"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

func sanitizeIntentForLensCreation(in *agents.IntentResolution) *agents.IntentResolution {
	if in == nil {
		return nil
	}
	out := *in
	out.Title = strings.TrimSpace(out.Title)
	out.Focus = strings.TrimSpace(out.Focus)
	out.SpherePaths = dedupStrings(trimStrings(out.SpherePaths))
	out.Keywords = dedupStrings(trimStrings(out.Keywords))

	// Domains: keep only valid /domain/v1/... paths (taxonomy validated).
	if len(out.Domains) > 0 {
		bestByPath := map[string]agents.DomainResolution{}
		order := make([]string, 0, len(out.Domains))
		domainScore := func(d agents.DomainResolution) int {
			score := 0
			if strings.TrimSpace(d.Title) != "" {
				score += 8
			}
			if strings.TrimSpace(d.Description) != "" {
				score += 4
			}
			score += len(d.FirstOrderEntities)
			score += len(d.Projections)
			score += len(d.RelatedDomains)
			score += len(d.AffectedDomains)
			score += len(d.Facets)
			return score
		}
		for _, d := range out.Domains {
			cp := strings.TrimSpace(d.CanonicalPath)
			if cp == "" || !strings.HasPrefix(cp, "/domain/") {
				continue
			}
			if err := taxonomy.ValidateCanonicalPath(cp); err != nil {
				continue
			}
			if norm, nerr := taxonomy.NormalizeDomainCanonicalPath(cp); nerr == nil && strings.TrimSpace(norm) != "" {
				cp = norm
			}
			d.CanonicalPath = cp
			d.ContractType = strings.TrimSpace(d.ContractType)
			d.Title = strings.TrimSpace(d.Title)
			d.Description = strings.TrimSpace(d.Description)
			// Best-effort trimming/dedup for lists
			d.Projections = dedupStrings(trimStrings(d.Projections))
			d.RelatedDomains = dedupStrings(trimStrings(d.RelatedDomains))
			d.AffectedDomains = dedupStrings(trimStrings(d.AffectedDomains))
			d.FirstOrderEntities = dedupStrings(trimStrings(d.FirstOrderEntities))
			if prev, ok := bestByPath[cp]; ok {
				if domainScore(d) > domainScore(prev) {
					bestByPath[cp] = d
				}
				continue
			}
			order = append(order, cp)
			bestByPath[cp] = d
		}
		domains := make([]agents.DomainResolution, 0, len(order))
		for _, cp := range order {
			if d, ok := bestByPath[cp]; ok {
				domains = append(domains, d)
			}
		}
		out.Domains = domains
	}

	// Anchors: keep only taxonomy-valid canonical paths (any namespace).
	if len(out.Anchors) > 0 {
		seen := map[string]struct{}{}
		anchors := out.Anchors[:0]
		for _, a := range out.Anchors {
			cp := strings.TrimSpace(a.CanonicalPath)
			if cp == "" {
				continue
			}
			if err := taxonomy.ValidateCanonicalPath(cp); err != nil {
				continue
			}
			if _, ok := seen[cp]; ok {
				continue
			}
			seen[cp] = struct{}{}
			a.CanonicalPath = cp
			a.Type = strings.TrimSpace(a.Type)
			a.Name = strings.TrimSpace(a.Name)
			a.Reason = strings.TrimSpace(a.Reason)
			anchors = append(anchors, a)
		}
		out.Anchors = anchors
	}

	// Decomposition hints: keep only those with a valid anchorPath; trim lists.
	if len(out.DecompositionHints) > 0 {
		seen := map[string]struct{}{}
		hints := out.DecompositionHints[:0]
		for _, h := range out.DecompositionHints {
			ap := strings.TrimSpace(h.AnchorPath)
			if ap == "" {
				continue
			}
			if err := taxonomy.ValidateCanonicalPath(ap); err != nil {
				continue
			}
			// Best-effort dedupe by anchorPath + joined entityTypes
			h.AnchorPath = ap
			h.EntityTypes = dedupStrings(trimStrings(h.EntityTypes))
			h.SubMarkets = dedupStrings(trimStrings(h.SubMarkets))
			h.PlayerCategories = dedupStrings(trimStrings(h.PlayerCategories))
			h.SearchQueries = dedupStrings(trimStrings(h.SearchQueries))
			key := ap + "|" + strings.Join(h.EntityTypes, ",")
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			hints = append(hints, h)
		}
		out.DecompositionHints = hints
	}
	return &out
}

func trimStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func dedupStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
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

func inferFragmentTypeFromPath(cp string) ah.FragmentClass {
	switch {
	case strings.HasPrefix(cp, "/product/"):
		return ah.FragmentClassProduct
	case strings.HasPrefix(cp, "/project/"):
		return ah.FragmentClassProject
	case strings.HasPrefix(cp, "/market/"):
		return ah.FragmentClassMarket
	case strings.HasPrefix(cp, "/fac/"):
		return ah.FragmentClassSite
	case strings.HasPrefix(cp, "/loc/"), strings.HasPrefix(cp, "/region/"):
		return ah.FragmentClassGeo
	case strings.HasPrefix(cp, "/evidence/"):
		return ah.FragmentClassEvidence
	case strings.HasPrefix(cp, "/person/"):
		return ah.FragmentClassPerson
	case strings.HasPrefix(cp, "/media/"):
		return ah.FragmentClassOrg
	case strings.HasPrefix(cp, "/org/"), strings.HasPrefix(cp, "/gov/"):
		return ah.FragmentClassOrg
	case strings.HasPrefix(cp, "/domain/"):
		return ah.FragmentClassGeo
	default:
		return ah.FragmentClassOrg
	}
}

func canonicalizeScopeWithFirestore(
	ctx context.Context,
	scope []string,
	h *HorizonHandlers,
) ([]string, map[string]string) {
	if h == nil || h.service == nil || len(scope) == 0 {
		return scope, nil
	}

	var resolver interface {
		Resolve(ctx context.Context, canonicalPath string, fragmentType scout.FragmentType) (*scout.ResolveResult, error)
	}
	if eng := h.service.Scout(); eng != nil && eng.CanonicalPathResolver() != nil {
		resolver = eng.CanonicalPathResolver()
	}
	fragments := h.service.Fragments()

	// Phase 1: Pre-filter and classify scope entries (no DB calls, fast).
	type scopeEntry struct {
		orig     string
		cp       string // after local normalization
		isQuery  bool
		isDomain bool
	}
	entries := make([]scopeEntry, 0, len(scope))
	for _, raw := range scope {
		orig := strings.TrimSpace(raw)
		if orig == "" {
			continue
		}
		cp := orig
		isQuery := strings.HasPrefix(cp, "/query/")
		isDomain := strings.HasPrefix(cp, "/domain/")
		if isDomain {
			if norm, nerr := taxonomy.NormalizeDomainCanonicalPath(cp); nerr == nil && strings.TrimSpace(norm) != "" {
				cp = norm
			}
		}
		if !isQuery {
			if err := taxonomy.ValidateCanonicalPath(cp); err != nil {
				continue
			}
		}
		entries = append(entries, scopeEntry{orig: orig, cp: cp, isQuery: isQuery, isDomain: isDomain})
	}

	// Phase 2: Resolve non-domain, non-query entries in parallel.
	type resolveResult struct {
		idx        int
		resolvedCP string
	}
	results := make([]resolveResult, len(entries))
	var wg sync.WaitGroup
	for i := range entries {
		e := &entries[i]
		results[i] = resolveResult{idx: i, resolvedCP: e.cp}
		if e.isQuery || e.isDomain {
			continue // no DB resolution needed
		}
		wg.Add(1)
		go func(idx int, cp string) {
			defer wg.Done()
			resolved := cp
			if resolver != nil {
				ft := inferFragmentTypeFromPath(cp)
				if rr, rerr := resolver.Resolve(ctx, cp, ft); rerr == nil && rr != nil && strings.TrimSpace(rr.CanonicalPath) != "" {
					candidate := strings.TrimSpace(rr.CanonicalPath)
					if rr.ExistingFragment != nil {
						resolved = candidate
					} else if fragments != nil {
						if f, ferr := fragments.GetByCanonicalPath(ctx, candidate); ferr == nil && f != nil {
							resolved = candidate
						}
					}
				}
			}
			results[idx].resolvedCP = resolved
		}(i, e.cp)
	}
	wg.Wait()

	// Phase 3: Collect results preserving order, dedup.
	rewrites := map[string]string{}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(entries))
	for i, e := range entries {
		cp := results[i].resolvedCP
		if cp != e.orig {
			rewrites[e.orig] = cp
		}
		if _, ok := seen[cp]; ok {
			continue
		}
		seen[cp] = struct{}{}
		out = append(out, cp)
	}
	return out, rewrites
}

type createLensFromQueryRequest struct {
	Name      string `json:"name"`
	ProjectID string `json:"projectId"`
	Query     string `json:"query"`
	// Mode controls whether to immediately start scouting/researching after lens creation.
	// - just_open: create lens + scope, but suppress initial scouting job creation (best-effort)
	// - research_first: create lens and start jobs (domains first, then anchors)
	// - hot_path: adapt the existing supply-chain hot-path runtime, reuse/create a hot-path lens,
	//   wait briefly for readiness, and run a primary simulation-backed answer when possible
	Mode              string                   `json:"mode,omitempty"` // just_open|research_first|hot_path
	ScoutingMode      string                   `json:"scoutingMode,omitempty"`
	IntentOverride    *agents.IntentResolution `json:"intent,omitempty"` // Optional: edited intent from preflight UI
	MaxRecursionDepth *int                     `json:"maxRecursionDepth,omitempty"`
	RequestedMetrics  []string                 `json:"requestedMetrics,omitempty"`
	// Optional: canonical paths selected from prefetch/autocomplete UI.
	// These are merged into the final lens scope (deduped).
	PrefetchScope []string `json:"prefetchScope,omitempty"`
	// Optional: enables specialized scout/research modules (e.g., ["management","products_and_services"])
	Specialties []string `json:"specialties,omitempty"`
	Status      string   `json:"status,omitempty"` // ACTIVE|DORMANT
}

type createLensFromQueryResponse struct {
	Intent  *agents.IntentResolution `json:"intent"`
	Lens    *models.Lens             `json:"lens"`
	HotPath *hotPathInquiryResponse  `json:"hotPath,omitempty"`
}

// CreateLensFromQuery resolves a natural language query into intent and creates a Lens with:
// - scope = anchors.canonicalPath
// - viewConfig = focus + spherePaths + keywords (+ optional knobs)
//
// POST /api/horizon/lenses/from-query
func (h *HorizonHandlers) CreateLensFromQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	var req createLensFromQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	req.Query = strings.TrimSpace(req.Query)
	req.Name = strings.TrimSpace(req.Name)
	req.Status = strings.TrimSpace(req.Status)
	// Normalize prefetch scope paths.
	if len(req.PrefetchScope) > 0 {
		out := make([]string, 0, len(req.PrefetchScope))
		seen := make(map[string]struct{}, len(req.PrefetchScope))
		for _, raw := range req.PrefetchScope {
			cp := strings.TrimSpace(raw)
			if cp == "" {
				continue
			}
			if _, ok := seen[cp]; ok {
				continue
			}
			seen[cp] = struct{}{}
			out = append(out, cp)
		}
		req.PrefetchScope = out
	}

	if req.ProjectID == "" {
		http.Error(w, "projectId is required", http.StatusBadRequest)
		return
	}
	if req.Query == "" && req.IntentOverride == nil {
		http.Error(w, "query or intent is required", http.StatusBadRequest)
		return
	}
	requestedMode := normalizeCreateLensFromQueryMode(req.Mode)

	// Correlation/session IDs (optional, but recommended for realtime UX).
	correlationID := strings.TrimSpace(r.Header.Get("X-Correlation-Id"))
	if correlationID == "" {
		correlationID = strings.TrimSpace(r.Header.Get("X-Correlation-ID"))
	}
	if correlationID == "" {
		correlationID = uuid.NewString()
	}
	sessionID := strings.TrimSpace(r.Header.Get("X-Session-Id"))
	if sessionID == "" {
		sessionID = strings.TrimSpace(r.Header.Get("X-Session-ID"))
	}
	ctx := rtctx.With(r.Context(), rtctx.Context{
		TenantID:      authInfo.TenantID,
		ProjectID:     req.ProjectID,
		CorrelationID: correlationID,
		SessionID:     sessionID,
	})

	trace.Log(ctx, "lens_create", "lens.from_query.request", map[string]any{
		"tenantId":          authInfo.TenantID,
		"projectId":         req.ProjectID,
		"queryLen":          len(req.Query),
		"querySha1":         trace.HashText(req.Query),
		"mode":              strings.ToLower(strings.TrimSpace(req.Mode)),
		"intentProvided":    req.IntentOverride != nil,
		"nameProvided":      req.Name != "",
		"statusProvided":    req.Status != "",
		"maxRecursionDepth": req.MaxRecursionDepth,
		"requestedMetricsN": len(req.RequestedMetrics),
		"prefetchScopeN":    len(req.PrefetchScope),
	})

	if requestedMode == createLensFromQueryModeHotPath {
		if cached, cachedLens, interactionID, err := h.findCachedHotPathInteraction(ctx, authInfo.TenantID, req.ProjectID, req.Query); err == nil && cached != nil && cachedLens != nil {
			if cached.HotPath != nil && isTerminalHotPathStatus(cached.HotPath.Status) {
				cached.HotPath.Cached = true
				cached.HotPath.InteractionID = interactionID
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Correlation-Id", correlationID)
				_ = json.NewEncoder(w).Encode(createLensFromQueryResponse{
					Intent:  cached.Intent,
					Lens:    cachedLens,
					HotPath: cached.HotPath,
				})
				return
			}
		}
	}

	if requestedMode != "just_open" {
		ready, reason, message := h.scoutExecutionAvailability()
		if !ready {
			trace.Log(ctx, "lens_create", "lens.from_query.blocked", map[string]any{
				"tenantId":          authInfo.TenantID,
				"projectId":         req.ProjectID,
				"mode":              requestedMode,
				"scoutReady":        false,
				"scoutReadyReason":  reason,
				"maxRecursionDepth": req.MaxRecursionDepth,
			})
			h.publishAssemblyFatal(
				ctx,
				authInfo.TenantID,
				req.ProjectID,
				"",
				"scouting",
				correlationID,
				sessionID,
				reason,
				message,
			)
			writeFatalPipelineError(w, http.StatusServiceUnavailable, reason, message, "scouting", map[string]interface{}{
				"mode": requestedMode,
			})
			return
		}
	}

	// 1) Resolve intent (precheck). Pass existing domains so LLM can avoid duplicates.
	intent := (*agents.IntentResolution)(nil)
	var err error
	if req.IntentOverride != nil {
		intent = sanitizeIntentForLensCreation(req.IntentOverride)
	} else {
		existingDomains := h.collectExistingDomainPathsForProject(ctx, authInfo.TenantID, req.ProjectID)
		for _, p := range req.PrefetchScope {
			p = strings.TrimSpace(p)
			if p != "" && strings.HasPrefix(p, "/domain/") {
				existingDomains = append(existingDomains, p)
			}
		}
		existingDomains = dedupStrings(existingDomains)
		if len(existingDomains) > 20 {
			existingDomains = existingDomains[:20]
		}
		intentAgent := agents.NewIntentResolverAgent(h.service.AIExecutor())
		trace.Log(ctx, "intent", "intent.resolve.start", map[string]any{
			"projectId":       req.ProjectID,
			"queryLen":        len(req.Query),
			"existingDomains": len(existingDomains),
		})
		intent, err = intentAgent.ResolveWithContext(ctx, req.Query, existingDomains)
	}
	if err != nil {
		trace.Log(ctx, "intent", "intent.resolve.error", map[string]any{
			"projectId": req.ProjectID,
			"error":     err.Error(),
		})
		// Best-effort: publish assembly failed for UI visibility.
		if pub := h.service.RealtimePublisher(); pub != nil {
			_ = pub.PublishJSON(ctx, realtime.EventType("horizon.assembly.failed"), authInfo.TenantID, map[string]interface{}{
				"tenantId":      authInfo.TenantID,
				"projectId":     req.ProjectID,
				"phase":         "intent",
				"error":         err.Error(),
				"correlationId": correlationID,
				"sessionId":     sessionID,
			})
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if requestedMode == createLensFromQueryModeHotPath {
		if !intentSupportsHotPath(intent) {
			http.Error(w, "hot_path mode currently requires a supply-chain execution plan", http.StatusBadRequest)
			return
		}
		intent = forceIntentHotPathOnly(intent)
	}

	// Best-effort: if anchors already exist in our graph, fill anchor.name from the stored English label.
	// This helps prefer known English names (often Wikidata-backed) over freshly generated/transliterated ones.
	// Parallelized: all GetByCanonicalPath calls run concurrently to reduce total latency.
	if intent != nil && h.service != nil && h.service.Fragments() != nil && len(intent.Anchors) > 0 {
		type anchorNameResult struct {
			idx  int
			name string
		}
		results := make(chan anchorNameResult, len(intent.Anchors))
		var wg sync.WaitGroup
		for i := range intent.Anchors {
			cp := strings.TrimSpace(intent.Anchors[i].CanonicalPath)
			if cp == "" {
				continue
			}
			wg.Add(1)
			go func(idx int, canonicalPath string) {
				defer wg.Done()
				frag, ferr := h.service.Fragments().GetByCanonicalPath(ctx, canonicalPath)
				if ferr != nil || frag == nil {
					return
				}
				n := strings.TrimSpace(frag.Labels["en"])
				if n == "" {
					n = strings.TrimSpace(frag.Identifiers["shortName"])
				}
				if n == "" {
					n = strings.TrimSpace(frag.Identifiers["legalName"])
				}
				if n != "" {
					results <- anchorNameResult{idx: idx, name: n}
				}
			}(i, cp)
		}
		wg.Wait()
		close(results)
		for r := range results {
			intent.Anchors[r.idx].Name = r.name
		}
	}
	anchorPaths := make([]string, 0, len(intent.Anchors))
	for _, a := range intent.Anchors {
		anchorPaths = append(anchorPaths, strings.TrimSpace(a.CanonicalPath))
	}
	domainPaths := make([]string, 0, len(intent.Domains))
	for _, d := range intent.Domains {
		domainPaths = append(domainPaths, strings.TrimSpace(d.CanonicalPath))
	}
	trace.Log(ctx, "intent", "intent.resolve.ok", map[string]any{
		"projectId":      req.ProjectID,
		"focus":          trace.Trunc(intent.Focus, 140),
		"spherePathsN":   len(intent.SpherePaths),
		"keywordsN":      len(intent.Keywords),
		"domainsN":       len(intent.Domains),
		"anchorsN":       len(intent.Anchors),
		"domainPaths":    trace.StrSlice(domainPaths, 10),
		"anchorPaths":    trace.StrSlice(anchorPaths, 10),
		"spherePathsTop": trace.StrSlice(intent.SpherePaths, 10),
	})

	// 2) Build Lens.
	// Scope = prefetch only (user-selected).
	// Deferred scope candidates come from:
	// - intent anchors
	// - domain firstOrderEntities (fallback when anchors are sparse/empty)
	// and are added to scope only when Scout creates fragment (max 5).
	scopeCap := len(req.PrefetchScope)
	scope := make([]string, 0, scopeCap)
	seenScope := make(map[string]struct{}, scopeCap)
	deferredScopePaths := make([]string, 0, len(intent.Anchors)+len(intent.Domains)*4)
	addDeferredScopeCandidate := func(path string) {
		cp := strings.TrimSpace(path)
		if cp == "" {
			return
		}
		if _, ok := seenScope[cp]; ok {
			return
		}
		seenScope[cp] = struct{}{}
		deferredScopePaths = append(deferredScopePaths, cp)
	}
	for _, a := range intent.Anchors {
		addDeferredScopeCandidate(a.CanonicalPath)
	}
	for _, d := range intent.Domains {
		// Include domain paths so jobs are created for /domain/v1/... (enables pipeline/Domain Engine execution).
		addDeferredScopeCandidate(d.CanonicalPath)
		for _, cp := range d.FirstOrderEntities {
			addDeferredScopeCandidate(cp)
		}
	}
	// Merge prefetch scope (UI-selected entities) into the final scope.
	for _, raw := range req.PrefetchScope {
		cp := strings.TrimSpace(raw)
		if cp == "" {
			continue
		}
		if _, ok := seenScope[cp]; ok {
			continue
		}
		seenScope[cp] = struct{}{}
		scope = append(scope, cp)
	}
	// Canonicalize scope entries against Firestore (best-effort).
	if len(scope) > 0 {
		var rewrites map[string]string
		scope, rewrites = canonicalizeScopeWithFirestore(ctx, scope, h)
		if len(rewrites) > 0 {
			for i := range intent.Domains {
				cp := strings.TrimSpace(intent.Domains[i].CanonicalPath)
				if next, ok := rewrites[cp]; ok && strings.TrimSpace(next) != "" {
					intent.Domains[i].CanonicalPath = next
				}
			}
			for i := range intent.Anchors {
				cp := strings.TrimSpace(intent.Anchors[i].CanonicalPath)
				if next, ok := rewrites[cp]; ok && strings.TrimSpace(next) != "" {
					intent.Anchors[i].CanonicalPath = next
				}
			}
		}
	}
	// Canonicalize deferred scope paths (anchors) for job creation.
	if len(deferredScopePaths) > 0 {
		canon, rewrites := canonicalizeScopeWithFirestore(ctx, deferredScopePaths, h)
		deferredScopePaths = canon
		if len(rewrites) > 0 {
			for i := range intent.Anchors {
				cp := strings.TrimSpace(intent.Anchors[i].CanonicalPath)
				if next, ok := rewrites[cp]; ok && strings.TrimSpace(next) != "" {
					intent.Anchors[i].CanonicalPath = next
				}
			}
		}
	}
	if len(scope) == 0 && len(deferredScopePaths) == 0 && len(intent.Domains) == 0 {
		trace.Log(ctx, "scope", "scope.empty", map[string]any{
			"projectId": req.ProjectID,
			"domainsN":  len(intent.Domains),
			"anchorsN":  len(intent.Anchors),
		})
		http.Error(w, "no anchors/domains resolved", http.StatusBadRequest)
		return
	}

	name := ""
	if intent != nil {
		name = strings.TrimSpace(intent.Title)
	}
	if name == "" {
		http.Error(w, "intent title is required", http.StatusBadRequest)
		return
	}
	status := strings.ToUpper(req.Status)
	if status == "" {
		status = "ACTIVE"
	}

	viewConfig := map[string]interface{}{
		"title":       intent.Title,
		"focus":       intent.Focus,
		"spherePaths": intent.SpherePaths,
		"keywords":    intent.Keywords,
	}
	domainQueries := models.ParseLensDomainQueries(intent.Domains)
	// Preserve original query for downstream research (helps with abstract/market queries).
	if req.Query != "" {
		viewConfig["originalQuery"] = req.Query
	}
	// Decomposition hints help research understand how to break down abstract anchors (markets, programs).
	if len(intent.DecompositionHints) > 0 {
		viewConfig["decompositionHints"] = intent.DecompositionHints
	}
	// Deferred scope paths: anchors added to scope only when Scout creates fragment (max 5 per lens).
	if len(deferredScopePaths) > 0 {
		viewConfig["deferredScopePaths"] = deferredScopePaths
	}
	// Persist domains (contracts) for downstream scout/research.
	// When domain-as-config: store domainModes and intents; keep domains in ViewConfig for backward compat.
	if len(domainQueries) > 0 {
		viewConfig["domains"] = models.LensDomainQueriesToMaps(domainQueries)
	}
	if req.MaxRecursionDepth != nil {
		viewConfig["maxRecursionDepth"] = *req.MaxRecursionDepth
	}
	if len(req.RequestedMetrics) > 0 {
		viewConfig["requestedMetrics"] = req.RequestedMetrics
	}
	mergedSpecialties := append([]string(nil), req.Specialties...)
	mergedSpecialties = append(mergedSpecialties, specialtiesForScopeScoutMode(req.ScoutingMode)...)
	if specs := taxonomy.NormalizeSpecialties(mergedSpecialties); len(specs) > 0 {
		viewConfig["specialties"] = specs
	}
	if scoutingMode := normalizeScopeScoutMode(req.ScoutingMode); scoutingMode != "" {
		viewConfig["uiScoutingMode"] = scoutingMode
	}

	lens := &models.Lens{
		TenantID:      authInfo.TenantID,
		ProjectID:     req.ProjectID,
		Name:          name,
		Scope:         scope,
		OriginalQuery: strings.TrimSpace(req.Query),
		DomainQueries: domainQueries,
		ViewConfig:    viewConfig,
		Status:        status,
	}
	// DomainModes: execution contracts from intent domains (contract types).
	if len(intent.Domains) > 0 {
		contractTypes := make([]string, 0, len(intent.Domains))
		for _, d := range intent.Domains {
			ct := strings.TrimSpace(d.ContractType)
			if ct != "" {
				contractTypes = append(contractTypes, ct)
			}
		}
		if len(contractTypes) > 0 {
			lens.DomainModes = horizondomain.DomainModesForContractTypes(contractTypes)
		}
	}
	// Intents: living questions from domains (title/description) and focus.
	if len(intent.Domains) > 0 || strings.TrimSpace(intent.Focus) != "" {
		now := time.Now().UTC()
		intents := make([]models.LensIntent, 0, len(intent.Domains)+1)
		for i, d := range intent.Domains {
			q := strings.TrimSpace(d.Description)
			if q == "" {
				q = strings.TrimSpace(d.Title)
			}
			if q != "" {
				intents = append(intents, models.LensIntent{
					ID:        fmt.Sprintf("intent-%d", i),
					Question:  q,
					CreatedAt: now,
					Status:    "pending",
				})
			}
		}
		if strings.TrimSpace(intent.Focus) != "" {
			hasFocus := false
			for _, i := range intents {
				if i.Question == strings.TrimSpace(intent.Focus) {
					hasFocus = true
					break
				}
			}
			if !hasFocus {
				intents = append(intents, models.LensIntent{
					ID:        "intent-focus",
					Question:  strings.TrimSpace(intent.Focus),
					CreatedAt: now,
					Status:    "pending",
				})
			}
		}
		lens.Intents = intents
	}

	if requestedMode == createLensFromQueryModeHotPath {
		executionPlan := buildHotPathExecutionPlanContract(lens.DomainModes, taxonomy.NormalizeSpecialties(mergedSpecialties))
		markLensHotPathRuntime(lens, executionPlan)
	}

	trace.Log(ctx, "lens_create", "lens.built", map[string]any{
		"tenantId":  lens.TenantID,
		"projectId": lens.ProjectID,
		"name":      trace.Trunc(lens.Name, 120),
		"status":    lens.Status,
		"scopeN":    len(lens.Scope),
		"scopeTop":  trace.StrSlice(lens.Scope, 12),
		"viewConfigKeys": func() []string {
			keys := make([]string, 0, len(lens.ViewConfig))
			for k := range lens.ViewConfig {
				keys = append(keys, k)
			}
			return trace.StrSlice(keys, 20)
		}(),
	})

	// Mode semantics:
	// - just_open: create lens without creating initial scouting jobs (best-effort)
	// - research_first: normal behavior (jobs created for scope + deferred anchors/first-order entities)
	// - hot_path: force existing hot-path-only supply-chain execution and run a primary answer flow
	mode := requestedMode
	createCtx := ctx
	if mode == createLensFromQueryModeJustOpen {
		createCtx = hlens.WithSkipScouting(createCtx)
	}

	lensReused := false
	if mode == createLensFromQueryModeHotPath {
		if reusableLens, rerr := h.findReusableHotPathLens(ctx, authInfo.TenantID, req.ProjectID, req.Query); rerr == nil && reusableLens != nil {
			lens = reusableLens
			lensReused = true
		}
	}
	if !lensReused {
		lensID, err := h.service.Lens().CreateLens(createCtx, lens)
		if err != nil {
			trace.Log(ctx, "lens_create", "lens.create.error", map[string]any{
				"tenantId":  lens.TenantID,
				"projectId": lens.ProjectID,
				"error":     err.Error(),
			})
			if pub := h.service.RealtimePublisher(); pub != nil {
				_ = pub.PublishJSON(ctx, realtime.EventType("horizon.assembly.failed"), authInfo.TenantID, map[string]interface{}{
					"tenantId":      authInfo.TenantID,
					"projectId":     req.ProjectID,
					"phase":         "decomposition",
					"error":         err.Error(),
					"correlationId": correlationID,
					"sessionId":     sessionID,
				})
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		lens.ID = lensID
		trace.Log(ctx, "lens_create", "lens.create.ok", map[string]any{
			"tenantId":  lens.TenantID,
			"projectId": lens.ProjectID,
			"lensId":    lensID,
			"scopeN":    len(lens.Scope),
		})
	}

	var hotPath *hotPathInquiryResponse
	if mode == createLensFromQueryModeHotPath {
		hotPath, _ = h.runHotPathInquiry(ctx, authInfo.TenantID, req.ProjectID, req.Query, lens, intent, lensReused)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Correlation-Id", correlationID)
	_ = json.NewEncoder(w).Encode(createLensFromQueryResponse{
		Intent:  intent,
		Lens:    lens,
		HotPath: hotPath,
	})
}
