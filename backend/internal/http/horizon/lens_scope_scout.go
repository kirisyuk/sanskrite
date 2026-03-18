package horizon

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	hlens "github.com/igorkirisyuk/sanskrite/internal/horizon/lens"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/rtctx"
	agentmodels "github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

type scopeScoutLensNodeRequest struct {
	FragmentID        string `json:"fragmentId"`
	CanonicalPath     string `json:"canonicalPath"`
	MaxRecursionDepth *int   `json:"maxRecursionDepth"`
	// Optional UI scouting mode: general | supply_chain | governance.
	Mode string `json:"mode,omitempty"`
	// Optional: enables specialized scout/research modules (e.g., ["management","products_and_services"])
	Specialties []string                              `json:"specialties,omitempty"`
	Exploration *scopeScoutDirectedExplorationRequest `json:"exploration,omitempty"`
}

type scopeScoutLensNodeResponse struct {
	JobID      string       `json:"jobId"`
	ScopeAdded bool         `json:"scopeAdded"`
	Target     string       `json:"targetCanonicalPath"`
	Lens       *models.Lens `json:"lens,omitempty"`
}

func normalizeScopeScoutMode(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case taxonomy.DomainModeSupplyChain:
		return taxonomy.DomainModeSupplyChain
	case taxonomy.DomainModeGovernance:
		return taxonomy.DomainModeGovernance
	default:
		return taxonomy.DomainModeGeneral
	}
}

func specialtiesForScopeScoutMode(mode string) []string {
	switch normalizeScopeScoutMode(mode) {
	case taxonomy.DomainModeSupplyChain:
		return []string{taxonomy.SpecialtySupplyChain, taxonomy.SpecialtyProductsAndServices}
	case taxonomy.DomainModeGovernance:
		return []string{taxonomy.SpecialtyManagement, taxonomy.SpecialtyEquity}
	default:
		return nil
	}
}

// ScopeScoutLensNode:
// - adds a node to lens.Scope (if missing)
// - creates (or reuses active) scouting job for that node with maxRecursionDepth=1 by default
//
// POST /api/horizon/lenses/{lensId}/scope-scout
func (h *HorizonHandlers) ScopeScoutLensNode(w http.ResponseWriter, r *http.Request) {
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

	var body scopeScoutLensNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	maxDepth := 1
	if body.MaxRecursionDepth != nil && *body.MaxRecursionDepth >= 0 && *body.MaxRecursionDepth <= 3 {
		maxDepth = *body.MaxRecursionDepth
	}

	// Correlation/session IDs (optional, but recommended for realtime UX)
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

	// Load lens (for auth + project context)
	lens, lerr := h.service.Lens().GetLens(r.Context(), lensID)
	if lerr != nil || lens == nil {
		http.Error(w, "Lens not found", http.StatusNotFound)
		return
	}
	if lens.TenantID != authInfo.TenantID {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Resolve canonical path:
	// Prefer fragmentId → fragment.canonicalPath (avoids alias issues).
	targetCanonicalPath := strings.TrimSpace(body.CanonicalPath)
	if fid := strings.TrimSpace(body.FragmentID); fid != "" && h.service.Fragments() != nil {
		if frag, ferr := h.service.Fragments().Get(r.Context(), fid); ferr == nil && frag != nil {
			if cp := strings.TrimSpace(frag.CanonicalPath); cp != "" {
				targetCanonicalPath = cp
			}
		}
	}
	if targetCanonicalPath == "" {
		http.Error(w, "canonicalPath or fragmentId required", http.StatusBadRequest)
		return
	}
	directedExploration, derr := normalizeDirectedExploration(body.Exploration)
	if derr != nil {
		http.Error(w, derr.Error(), http.StatusBadRequest)
		return
	}
	if directedExploration != nil && !isAllowedSupplyChainAnchorPath(targetCanonicalPath) {
		http.Error(w, "directed exploration anchor must be org/gov/fac/product/deposit", http.StatusBadRequest)
		return
	}

	// Attach realtime ctx (best-effort)
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
			"mode": "scope_scout",
		})
		return
	}

	// Ensure scope contains this canonical path
	scopeAdded := false
	{
		seen := map[string]struct{}{}
		for _, p := range lens.Scope {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			seen[p] = struct{}{}
		}
		if _, ok := seen[targetCanonicalPath]; !ok {
			lens.Scope = append(lens.Scope, targetCanonicalPath)
			scopeAdded = true
		}
	}

	// Create scouting job (depth=1 by default) — best-effort and idempotent on active jobs.
	jobID := ""
	if h.service.ScoutingJobs() != nil {
		now := time.Now().UTC()
		scoutingMode := normalizeScopeScoutMode(body.Mode)
		mergedSpecialties := append([]string(nil), body.Specialties...)
		mergedSpecialties = append(mergedSpecialties, specialtiesForScopeScoutMode(scoutingMode)...)
		specialties := taxonomy.NormalizeSpecialties(mergedSpecialties)
		trigger := "ui.graph.scope_scout"
		hotPathCanonicalPaths := []string(nil)
		if directedExploration != nil {
			hotPathCanonicalPaths = make([]string, 0, len(directedExploration.HotPathNodeIDs))
			for _, fragmentID := range directedExploration.HotPathNodeIDs {
				if h.service.Fragments() == nil {
					break
				}
				frag, ferr := h.service.Fragments().Get(r.Context(), fragmentID)
				if ferr != nil || frag == nil {
					continue
				}
				cp := strings.TrimSpace(frag.CanonicalPath)
				if cp == "" {
					continue
				}
				hotPathCanonicalPaths = append(hotPathCanonicalPaths, cp)
			}
			if directedExploration.RenderTarget == "map" {
				trigger = "ui.map.directed_explore"
			} else {
				trigger = "ui.graph.directed_explore"
			}
		}
		state := map[string]interface{}{
			"targetCanonicalPath":          targetCanonicalPath,
			"lensId":                       lens.ID,
			"currentState":                 string(models.ScoutingJobStatePending),
			"scoutingStrategy":             string(models.ScoutingStrategySnapshot),
			"recursionDepth":               0.0,
			"maxRecursionDepth":            float64(maxDepth),
			"trigger":                      trigger,
			"uiScoutingMode":               scoutingMode,
			"correlationId":                correlationID,
			"relationChildSchedulingOptIn": true, // User explicitly requested hop depth — allow relation children regardless of domain expansion
		}
		if scoutingMode == taxonomy.DomainModeSupplyChain || scoutingMode == taxonomy.DomainModeGovernance {
			state["domainModes"] = []string{scoutingMode}
		}
		if directedExploration != nil {
			state["directedExploration"] = directedExploration
			if len(hotPathCanonicalPaths) > 0 {
				state["hotPathCanonicalPaths"] = hotPathCanonicalPaths
			}
			state["ignoreFreshnessForUserRequest"] = directedExploration.IgnoreFreshness
		}
		if len(specialties) > 0 {
			state["specialties"] = specialties
		}
		if sessionID != "" {
			state["sessionId"] = sessionID
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
			TargetCanonicalPath: targetCanonicalPath,
			ScoutingStrategy:    string(models.ScoutingStrategySnapshot),
			BillingContext: models.BillingContext{
				ChargeModel: string(models.ChargeModelBounty),
				ProjectID:   lens.ProjectID,
			},
			CurrentState: models.ScoutingJobStatePending,
		}
		// NOTE: Do NOT emit horizon.scout.job.created here — the WithOnCreate hook
		// in main.go already publishes that event when Create() succeeds.
		// Emitting here would cause duplicate "Scout Job Created" messages in the UI.
		if id, cerr := h.service.ScoutingJobs().Create(ctx, scoutingJob); cerr == nil && strings.TrimSpace(id) != "" {
			jobID = strings.TrimSpace(id)
		} else if cerr != nil {
			msg := "Failed to enqueue scouting job: " + strings.TrimSpace(cerr.Error())
			h.publishAssemblyFatal(
				ctx,
				authInfo.TenantID,
				lens.ProjectID,
				lens.ID,
				"scouting",
				correlationID,
				sessionID,
				"scout_job_enqueue_failed",
				msg,
			)
			writeFatalPipelineError(w, http.StatusServiceUnavailable, "scout_job_enqueue_failed", msg, "scouting", map[string]interface{}{
				"targetCanonicalPath": targetCanonicalPath,
			})
			return
		}
	}

	// Persist updated scope (and manage subscriptions) best-effort.
	// If scope didn't change, we still return success (idempotent).
	if scopeAdded {
		// ScopeScout already enqueues the exact target job above; suppress auto-scout side effects
		// from generic scope update logic to avoid scheduling unrelated scope nodes.
		_ = h.service.Lens().UpdateLens(hlens.WithSkipScouting(ctx), lens)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Correlation-Id", correlationID)
	json.NewEncoder(w).Encode(scopeScoutLensNodeResponse{
		JobID:      jobID,
		ScopeAdded: scopeAdded,
		Target:     targetCanonicalPath,
		Lens:       lens,
	})
}
