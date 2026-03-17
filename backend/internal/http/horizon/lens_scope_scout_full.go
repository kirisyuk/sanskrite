package horizon

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	horizondomain "github.com/igorkirisyuk/sanskrite/internal/horizon/domain"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/rtctx"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	agentmodels "github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

const (
	scopeScoutFullDefaultMaxAnchors        = 20
	scopeScoutFullDefaultMaxRecursionDepth = 1
	scopeScoutFullFirstFiveForGraph        = 5
)

type scopeScoutFullRequest struct {
	Query             string   `json:"query"`
	MaxAnchors        *int     `json:"maxAnchors,omitempty"`
	MaxRecursionDepth *int     `json:"maxRecursionDepth,omitempty"`
	Mode              string   `json:"mode,omitempty"`
	Specialties       []string `json:"specialties,omitempty"`
}

type scopeScoutFullResponse struct {
	JobIDs            []string                 `json:"jobIds"`
	CanonicalPaths    []string                 `json:"canonicalPaths"`
	FirstFiveForGraph []string                 `json:"firstFiveForGraph"`
	Intent            *agents.IntentResolution `json:"intent,omitempty"`
}

// ScopeScoutLensFull runs the full lens-assembly pipeline inside an existing lens:
// intent resolution, anchors + domains + firstOrderEntities, multiple ScoutingJobs with full context,
// first 5 objects added to scope and graph immediately.
//
// POST /api/horizon/lenses/{lensId}/scope-scout-full
func (h *HorizonHandlers) ScopeScoutLensFull(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo, err := requireAuth(r)
	if err != nil || authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	lensID := extractIDFromPath(r, "/api/horizon/lenses/")
	if strings.TrimSpace(lensID) == "" {
		http.Error(w, "lensId required in path", http.StatusBadRequest)
		return
	}

	var body scopeScoutFullRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	q := strings.TrimSpace(body.Query)
	if q == "" {
		http.Error(w, "query is required", http.StatusBadRequest)
		return
	}

	maxAnchors := scopeScoutFullDefaultMaxAnchors
	if body.MaxAnchors != nil && *body.MaxAnchors > 0 {
		maxAnchors = *body.MaxAnchors
	}
	maxRecursionDepth := scopeScoutFullDefaultMaxRecursionDepth
	if body.MaxRecursionDepth != nil && *body.MaxRecursionDepth >= 0 {
		maxRecursionDepth = *body.MaxRecursionDepth
	}

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

	lens, lerr := h.service.Lens().GetLens(r.Context(), lensID)
	if lerr != nil || lens == nil {
		http.Error(w, "Lens not found", http.StatusNotFound)
		return
	}
	if lens.TenantID != authInfo.TenantID {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	ctx := rtctx.With(r.Context(), rtctx.Context{
		TenantID:      authInfo.TenantID,
		ProjectID:     lens.ProjectID,
		LensID:        lens.ID,
		CorrelationID: correlationID,
		SessionID:     sessionID,
	})

	ready, reason, message := h.scoutExecutionAvailability()
	if !ready {
		h.publishAssemblyFatal(
			ctx,
			authInfo.TenantID,
			lens.ProjectID,
			lens.ID,
			"scouting",
			correlationID,
			sessionID,
			reason,
			message,
		)
		writeFatalPipelineError(w, http.StatusServiceUnavailable, reason, message, "scouting", map[string]interface{}{
			"mode": "scope_scout_full",
		})
		return
	}

	existingDomains := h.collectNearbyDomainPaths(ctx, lens)
	intentAgent := agents.NewIntentResolverAgent(h.service.AIExecutor())
	intent, ierr := intentAgent.ResolveWithContext(ctx, q, existingDomains)
	if ierr != nil || intent == nil {
		http.Error(w, "failed to resolve query", http.StatusBadRequest)
		return
	}
	intent = sanitizeIntentForLensCreation(intent)

	deferredScopePaths := buildDeferredScopePathsFromIntent(intent)
	if len(deferredScopePaths) == 0 {
		http.Error(w, "no anchors or domains resolved", http.StatusBadRequest)
		return
	}

	deferredScopePaths, _ = canonicalizeScopeWithFirestore(ctx, deferredScopePaths, h)
	if len(deferredScopePaths) > maxAnchors {
		deferredScopePaths = deferredScopePaths[:maxAnchors]
	}

	scoutingMode := normalizeScopeScoutMode(body.Mode)
	mergedSpecialties := append([]string(nil), body.Specialties...)
	mergedSpecialties = append(mergedSpecialties, specialtiesForScopeScoutMode(scoutingMode)...)
	specialties := taxonomy.NormalizeSpecialties(mergedSpecialties)
	focus := strings.TrimSpace(intent.Focus)
	spherePaths := intent.SpherePaths
	keywords := intent.Keywords
	domainModes := taxonomy.NormalizeDomainModes(effectiveDomainModesForScopeScoutFull(lens, intent))
	if scoutingMode == taxonomy.DomainModeSupplyChain || scoutingMode == taxonomy.DomainModeGovernance {
		domainModes = taxonomy.NormalizeDomainModes(append([]string{scoutingMode}, domainModes...))
	}
	supplyChainMode := taxonomy.HasDomainMode(domainModes, taxonomy.DomainModeSupplyChain) ||
		taxonomy.HasSpecialty(specialties, taxonomy.SpecialtySupplyChain)

	viewConfig := map[string]interface{}{
		"focus":         focus,
		"originalQuery": q,
		"spherePaths":   spherePaths,
		"keywords":      keywords,
	}
	if len(intent.DecompositionHints) > 0 {
		viewConfig["decompositionHints"] = intent.DecompositionHints
	}
	if len(intent.Domains) > 0 {
		viewConfig["domains"] = domainsToStateMaps(intent.Domains)
	}
	viewConfig["maxRecursionDepth"] = maxRecursionDepth
	executionPlan := scout.BuildExecutionPlanContract(viewConfig, domainModes, specialties)

	jobIDs := make([]string, 0, len(deferredScopePaths))
	activeScoutJobs := map[string]string{}
	if h.service.ScoutingJobs() != nil {
		if jobs, jerr := h.service.ScoutingJobs().ListByProjectAndLens(ctx, lens.TenantID, lens.ProjectID, lensID, 400); jerr == nil {
			for _, j := range jobs {
				if j == nil || (j.Status != agentmodels.AgentJobStatusPending && j.Status != agentmodels.AgentJobStatusRunning) {
					continue
				}
				p := strings.TrimSpace(j.TargetCanonicalPath)
				if p != "" {
					activeScoutJobs[p] = j.ID
				}
			}
		}
	}

	now := time.Now().UTC()
	for _, inputPath := range deferredScopePaths {
		canonicalPath := strings.TrimSpace(inputPath)
		if canonicalPath == "" {
			continue
		}
		if _, ok := activeScoutJobs[canonicalPath]; ok {
			continue
		}

		plannerPriorityTier := inferPlannerPriorityTierForScopeScout(canonicalPath, supplyChainMode)
		state := map[string]interface{}{
			"targetCanonicalPath": canonicalPath,
			"lensId":              lensID,
			"currentState":        string(models.ScoutingJobStatePending),
			"scoutingStrategy":    string(models.ScoutingStrategySnapshot),
			"plannerPriorityTier": plannerPriorityTier,
			"recursionDepth":      0.0,
			"maxRecursionDepth":   float64(maxRecursionDepth),
			"deferredScopeAdd":    true,
			"trigger":             "ui.graph.scope_scout_full",
			"uiScoutingMode":      scoutingMode,
			"correlationId":       correlationID,
			"sessionId":           sessionID,
			"query":               q,
			"originalQuery":       q,
		}
		if inputPath != canonicalPath {
			state["aliasCanonicalPath"] = inputPath
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
		if len(domainModes) > 0 {
			state["domainModes"] = domainModes
		}
		if len(intent.DecompositionHints) > 0 {
			state["decompositionHints"] = intent.DecompositionHints
		}
		if len(intent.Domains) > 0 {
			state["domains"] = domainsToStateMaps(intent.Domains)
		}
		if supplyChainMode {
			state["supplyChainMode"] = true
			state["supplyChainPhase"] = "chain_core"
			state["supplyChainPhases"] = []string{"chain_core", "targeted_enrich"}
			state["supplyChainExpandEnabled"] = true
			state["supplyChainExpandMaxNodes"] = 5.0
		}
		scout.ApplyExecutionPlanToState(state, executionPlan)

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
		jobID, cerr := h.service.ScoutingJobs().Create(ctx, scoutingJob)
		if cerr != nil {
			continue
		}
		if strings.TrimSpace(jobID) != "" {
			jobIDs = append(jobIDs, strings.TrimSpace(jobID))
			activeScoutJobs[canonicalPath] = strings.TrimSpace(jobID)
			if pub := h.service.RealtimePublisher(); pub != nil {
				_ = pub.PublishJSON(ctx, realtime.EventType("horizon.graph.node.upserted"), authInfo.TenantID, map[string]interface{}{
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

	firstFiveForGraph := deferredScopePaths
	if len(firstFiveForGraph) > scopeScoutFullFirstFiveForGraph {
		firstFiveForGraph = firstFiveForGraph[:scopeScoutFullFirstFiveForGraph]
	}

	if len(firstFiveForGraph) > 0 && h.service.Lens() != nil {
		_, _ = h.service.Lens().AddScopePathsAutoLimited(ctx, lensID, firstFiveForGraph, models.MaxAutoScopeAddsPerLens)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Correlation-Id", correlationID)
	_ = json.NewEncoder(w).Encode(scopeScoutFullResponse{
		JobIDs:            jobIDs,
		CanonicalPaths:    deferredScopePaths,
		FirstFiveForGraph: firstFiveForGraph,
		Intent:            intent,
	})
}

func buildDeferredScopePathsFromIntent(intent *agents.IntentResolution) []string {
	if intent == nil {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(intent.Anchors)+len(intent.Domains)*4)
	add := func(cp string) {
		cp = strings.TrimSpace(cp)
		if cp == "" {
			return
		}
		if _, ok := seen[cp]; ok {
			return
		}
		seen[cp] = struct{}{}
		out = append(out, cp)
	}
	for _, a := range intent.Anchors {
		add(a.CanonicalPath)
	}
	for _, d := range intent.Domains {
		add(d.CanonicalPath)
		for _, cp := range d.FirstOrderEntities {
			add(cp)
		}
	}
	return out
}

func effectiveDomainModesForScopeScoutFull(lens *models.Lens, intent *agents.IntentResolution) []string {
	if lens != nil && len(lens.DomainModes) > 0 {
		return lens.DomainModes
	}
	if intent != nil && len(intent.Domains) > 0 {
		types := make([]string, 0, len(intent.Domains))
		for _, d := range intent.Domains {
			if t := strings.TrimSpace(d.ContractType); t != "" {
				types = append(types, t)
			}
		}
		if len(types) > 0 {
			return taxonomy.NormalizeDomainModes(horizondomain.DomainModesForContractTypes(types))
		}
	}
	return nil
}

func domainsToStateMaps(domains []agents.DomainResolution) []map[string]interface{} {
	if len(domains) == 0 {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(domains))
	for _, d := range domains {
		m := map[string]interface{}{
			"canonicalPath": strings.TrimSpace(d.CanonicalPath),
			"contractType":  strings.TrimSpace(d.ContractType),
			"title":         strings.TrimSpace(d.Title),
			"description":   strings.TrimSpace(d.Description),
		}
		if len(d.FirstOrderEntities) > 0 {
			m["firstOrderEntities"] = d.FirstOrderEntities
		}
		out = append(out, m)
	}
	return out
}

func inferPlannerPriorityTierForScopeScout(canonicalPath string, supplyChainMode bool) string {
	cp := strings.TrimSpace(canonicalPath)
	if strings.HasPrefix(cp, "/domain/") {
		return "high"
	}
	if supplyChainMode {
		if strings.HasPrefix(cp, "/org/") || strings.HasPrefix(cp, "/fac/") {
			return "high"
		}
		if strings.HasPrefix(cp, "/market/") {
			return "normal"
		}
	}
	return "low"
}
