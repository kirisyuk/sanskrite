package horizon

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/rtctx"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	agentmodels "github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

type scopeScoutLensQueryRequest struct {
	Query string `json:"query"`
	// Optional: enables specialized scout/research modules (e.g., ["management","products_and_services"])
	Specialties []string `json:"specialties,omitempty"`
}

type scopeScoutLensQueryResponse struct {
	JobID      string       `json:"jobId"`
	ScopeAdded bool         `json:"scopeAdded"`
	Target     string       `json:"targetCanonicalPath"`
	Lens       *models.Lens `json:"lens,omitempty"`
}

// ScopeScoutLensQuery:
// - resolves a free-form query into a single anchor canonicalPath (intent resolver)
// - adds that canonicalPath to lens.Scope (if missing)
// - creates (or reuses active) scouting job for that node with maxRecursionDepth=0 by default
//
// POST /api/horizon/lenses/{lensId}/scope-scout-query
func (h *HorizonHandlers) ScopeScoutLensQuery(w http.ResponseWriter, r *http.Request) {
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

	var body scopeScoutLensQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	q := strings.TrimSpace(body.Query)
	if q == "" {
		http.Error(w, "query is required", http.StatusBadRequest)
		return
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
			"mode": "scope_scout_query",
		})
		return
	}

	// Resolve intent -> pick the first anchor canonical path as the target.
	existingDomains := h.collectNearbyDomainPaths(ctx, lens)
	intentAgent := agents.NewIntentResolverAgent(h.service.AIExecutor())
	intent, ierr := intentAgent.ResolveWithContext(ctx, q, existingDomains)
	if ierr != nil || intent == nil || len(intent.Anchors) == 0 {
		http.Error(w, "failed to resolve query", http.StatusBadRequest)
		return
	}
	targetCanonicalPath := strings.TrimSpace(intent.Anchors[0].CanonicalPath)
	if targetCanonicalPath == "" {
		http.Error(w, "no canonical path resolved", http.StatusBadRequest)
		return
	}

	// Deferred scope: do NOT add to scope here. Scope is added only when Scout creates the fragment (max 5 per lens).
	scopeAdded := false

	// Create scouting job (maxRecursionDepth=0: add as a single node, no automatic relationship expansion).
	// Job has deferredScopeAdd=true so scope is added on success.
	jobID := ""
	if h.service.ScoutingJobs() != nil {
		now := time.Now().UTC()
		specialties := taxonomy.NormalizeSpecialties(body.Specialties)
		state := map[string]interface{}{
			"query":               q,
			"targetCanonicalPath": targetCanonicalPath,
			"lensId":              lens.ID,
			"currentState":        string(models.ScoutingJobStatePending),
			"scoutingStrategy":    string(models.ScoutingStrategySnapshot),
			"recursionDepth":      0.0,
			"maxRecursionDepth":   0.0,
			"trigger":             "ui.graph.scope_scout_query",
			"correlationId":       correlationID,
			"deferredScopeAdd":    true,
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

	// No scope update: deferred until fragment is created by Scout.

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Correlation-Id", correlationID)
	_ = json.NewEncoder(w).Encode(scopeScoutLensQueryResponse{
		JobID:      jobID,
		ScopeAdded: scopeAdded,
		Target:     targetCanonicalPath,
		Lens:       lens,
	})
}
