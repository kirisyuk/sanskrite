package horizon

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"github.com/google/uuid"
	hlens "github.com/igorkirisyuk/sanskrite/internal/horizon/lens"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/rtctx"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/trace"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func sanitizeIntentForLensCreation(in *agents.IntentResolution) *agents.IntentResolution {
	if in == nil {
		return nil
	}
	out := *in
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

func inferFragmentTypeFromPath(cp string) models.FragmentType {
	switch {
	case strings.HasPrefix(cp, "/product/"):
		return models.FragmentTypeProduct
	case strings.HasPrefix(cp, "/program/"):
		return models.FragmentTypeProgram
	case strings.HasPrefix(cp, "/market/"):
		return models.FragmentTypeMarket
	case strings.HasPrefix(cp, "/fac/"):
		return models.FragmentTypeFacility
	case strings.HasPrefix(cp, "/loc/"), strings.HasPrefix(cp, "/region/"):
		return models.FragmentTypeGeoRegion
	case strings.HasPrefix(cp, "/event/"):
		return models.FragmentTypeEvent
	case strings.HasPrefix(cp, "/person/"):
		return models.FragmentTypePerson
	case strings.HasPrefix(cp, "/media/"):
		return models.FragmentTypeMediaChannel
	case strings.HasPrefix(cp, "/org/"), strings.HasPrefix(cp, "/gov/"):
		return models.FragmentTypeOrganization
	case strings.HasPrefix(cp, "/domain/"):
		return models.FragmentTypeDomain
	default:
		return models.FragmentTypeOrganization
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
		Resolve(ctx context.Context, canonicalPath string, fragmentType models.FragmentType) (*scout.ResolveResult, error)
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
	Mode              string                   `json:"mode,omitempty"`   // just_open|research_first
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
	Intent *agents.IntentResolution `json:"intent"`
	Lens   *models.Lens             `json:"lens"`
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

	// 1) Resolve intent (precheck).
	intent := (*agents.IntentResolution)(nil)
	var err error
	if req.IntentOverride != nil {
		intent = sanitizeIntentForLensCreation(req.IntentOverride)
	} else {
		intentAgent := agents.NewIntentResolverAgent(h.service.AIExecutor())
		trace.Log(ctx, "intent", "intent.resolve.start", map[string]any{
			"projectId": req.ProjectID,
			"queryLen":  len(req.Query),
		})
		intent, err = intentAgent.Resolve(ctx, req.Query)
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
	scope := make([]string, 0, len(intent.Domains)+len(intent.Anchors)+len(req.PrefetchScope))
	seenScope := make(map[string]struct{}, len(intent.Domains)+len(intent.Anchors)+len(req.PrefetchScope))
	// Domains go first so their scouting jobs are created first.
	for _, d := range intent.Domains {
		cp := strings.TrimSpace(d.CanonicalPath)
		if cp == "" {
			continue
		}
		if _, ok := seenScope[cp]; ok {
			continue
		}
		seenScope[cp] = struct{}{}
		scope = append(scope, cp)
	}
	// Then anchors (unchanged behavior).
	for _, a := range intent.Anchors {
		cp := strings.TrimSpace(a.CanonicalPath)
		if cp == "" {
			continue
		}
		if _, ok := seenScope[cp]; ok {
			continue
		}
		seenScope[cp] = struct{}{}
		scope = append(scope, cp)
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
			// Keep intent domains/anchors in sync with canonicalized scope.
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
	if len(scope) == 0 {
		trace.Log(ctx, "scope", "scope.empty", map[string]any{
			"projectId": req.ProjectID,
			"domainsN":  len(intent.Domains),
			"anchorsN":  len(intent.Anchors),
		})
		http.Error(w, "no anchors/domains resolved", http.StatusBadRequest)
		return
	}

	name := req.Name
	if name == "" {
		// Prefer an explicit English anchor name when available (more stable than free-form focus).
		// Fallback to focus otherwise.
		if intent != nil && len(intent.Anchors) > 0 {
			if n := strings.TrimSpace(intent.Anchors[0].Name); n != "" {
				name = n
			} else {
				name = intent.Focus
			}
		} else if intent != nil && len(intent.Domains) > 0 {
			if t := strings.TrimSpace(intent.Domains[0].Title); t != "" {
				name = t
			} else {
				name = intent.Focus
			}
		} else {
			name = intent.Focus
		}
	}
	status := strings.ToUpper(req.Status)
	if status == "" {
		status = "ACTIVE"
	}

	viewConfig := map[string]interface{}{
		"focus":       intent.Focus,
		"spherePaths": intent.SpherePaths,
		"keywords":    intent.Keywords,
	}
	// Preserve original query for downstream research (helps with abstract/market queries).
	if req.Query != "" {
		viewConfig["originalQuery"] = req.Query
	}
	// Decomposition hints help research understand how to break down abstract anchors (markets, programs).
	if len(intent.DecompositionHints) > 0 {
		viewConfig["decompositionHints"] = intent.DecompositionHints
	}
	// Persist domains (contracts) for downstream scout/research.
	if len(intent.Domains) > 0 {
		viewConfig["domains"] = intent.Domains
	}
	if req.MaxRecursionDepth != nil {
		viewConfig["maxRecursionDepth"] = *req.MaxRecursionDepth
	}
	if len(req.RequestedMetrics) > 0 {
		viewConfig["requestedMetrics"] = req.RequestedMetrics
	}
	if specs := taxonomy.NormalizeSpecialties(req.Specialties); len(specs) > 0 {
		viewConfig["specialties"] = specs
	}

	lens := &models.Lens{
		TenantID:   authInfo.TenantID,
		ProjectID:  req.ProjectID,
		Name:       name,
		Scope:      scope,
		ViewConfig: viewConfig,
		Status:     status,
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
	// - research_first: normal behavior (jobs created, domains first due to scope ordering)
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		// Default: if domains exist, research first; else keep legacy behavior (research first).
		mode = "research_first"
	}
	createCtx := ctx
	if mode == "just_open" {
		createCtx = hlens.WithSkipScouting(createCtx)
	}

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

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Correlation-Id", correlationID)
	_ = json.NewEncoder(w).Encode(createLensFromQueryResponse{
		Intent: intent,
		Lens:   lens,
	})
}
