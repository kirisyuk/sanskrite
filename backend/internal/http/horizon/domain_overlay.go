package horizon

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	horizondomain "github.com/igorkirisyuk/sanskrite/internal/horizon/domain"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/rtctx"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/pipeline"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/trace"
	agentmodels "github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

type domainOverlayPreviewRequest struct {
	Query string `json:"query"`
}

type domainOverlayPreviewResponse struct {
	Intent          *agents.IntentResolution `json:"intent"`
	ExistingDomains []string                 `json:"existingDomains,omitempty"`
}

type domainOverlayExecuteRequest struct {
	Query               string   `json:"query"`
	DomainCanonicalPath string   `json:"domainCanonicalPath,omitempty"`
	Specialties         []string `json:"specialties,omitempty"`
}

type domainOverlayExecuteResponse struct {
	JobID           string   `json:"jobId"`
	DomainPath      string   `json:"domainCanonicalPath"`
	ExistingDomains []string `json:"existingDomains,omitempty"`
}

func domainOverlayTrace(ctx context.Context, event string, data map[string]any) {
	trace.Log(ctx, "domain_contract", "domain_contract.overlay."+strings.TrimSpace(event), data)
}

func (h *HorizonHandlers) resolveDomainOverlay(ctx context.Context, query string, existingDomains []string, preferredDomainPath string) (*horizondomain.ResolveResult, error) {
	domainOverlayTrace(ctx, "resolve.start", map[string]any{
		"queryLength":       len(strings.TrimSpace(query)),
		"existingDomains":   len(existingDomains),
		"preferredDomainId": strings.TrimSpace(preferredDomainPath),
	})
	engine := horizondomain.NewEngine(
		h.service.LensArtifacts(),
		h.service.Fragments(),
		h.service.Edges(),
		h.service.ScoutingJobs(),
		h.service.DomainAnswerSnapshots(),
		h.service.AIExecutor(),
		nil,
	)
	resolved, err := engine.Resolve(ctx, horizondomain.ResolveInput{
		Query:               query,
		ExistingDomains:     existingDomains,
		DomainCanonicalPath: preferredDomainPath,
	})
	if err != nil {
		domainOverlayTrace(ctx, "resolve.error", map[string]any{
			"error": err.Error(),
		})
		return nil, err
	}
	contractCount := 0
	if resolved != nil {
		contractCount = len(resolved.Contracts)
	}
	domainOverlayTrace(ctx, "resolve.completed", map[string]any{
		"contractCount": contractCount,
	})
	return resolved, nil
}

// PreviewDomainOverlay resolves a query into a domain contract without creating a job.
// POST /api/horizon/lenses/{lensId}/domain-overlay-preview
func (h *HorizonHandlers) PreviewDomainOverlay(w http.ResponseWriter, r *http.Request) {
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
	var body domainOverlayPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	q := strings.TrimSpace(body.Query)
	if q == "" {
		http.Error(w, "query is required", http.StatusBadRequest)
		return
	}
	domainOverlayTrace(r.Context(), "preview.start", map[string]any{
		"lensId":      strings.TrimSpace(lensID),
		"queryLength": len(q),
	})

	lens, lerr := h.service.Lens().GetLens(r.Context(), lensID)
	if lerr != nil || lens == nil {
		http.Error(w, "Lens not found", http.StatusNotFound)
		return
	}
	if lens.TenantID != authInfo.TenantID {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	existingDomains := h.collectNearbyDomainPaths(r.Context(), lens)
	resolved, rerr := h.resolveDomainOverlay(r.Context(), q, existingDomains, "")
	if rerr != nil || resolved == nil || resolved.Intent == nil {
		errMsg := ""
		if rerr != nil {
			errMsg = rerr.Error()
		}
		domainOverlayTrace(r.Context(), "preview.resolve.error", map[string]any{
			"lensId": strings.TrimSpace(lensID),
			"error":  errMsg,
		})
		http.Error(w, "failed to resolve query", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(domainOverlayPreviewResponse{
		Intent:          resolved.Intent,
		ExistingDomains: existingDomains,
	}); err != nil {
		domainOverlayTrace(r.Context(), "preview.encode.error", map[string]any{
			"lensId": strings.TrimSpace(lensID),
			"error":  err.Error(),
		})
	}
}

// ExecuteDomainOverlay creates a domain contract execution job for an existing lens graph.
// POST /api/horizon/lenses/{lensId}/domain-overlay-execute
func (h *HorizonHandlers) ExecuteDomainOverlay(w http.ResponseWriter, r *http.Request) {
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
	var body domainOverlayExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	q := strings.TrimSpace(body.Query)
	if q == "" {
		http.Error(w, "query is required", http.StatusBadRequest)
		return
	}
	domainOverlayTrace(r.Context(), "execute.start", map[string]any{
		"lensId":      strings.TrimSpace(lensID),
		"queryLength": len(q),
	})

	lens, lerr := h.service.Lens().GetLens(r.Context(), lensID)
	if lerr != nil || lens == nil {
		http.Error(w, "Lens not found", http.StatusNotFound)
		return
	}
	if lens.TenantID != authInfo.TenantID {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	existingDomains := h.collectNearbyDomainPaths(r.Context(), lens)
	preferredPath := strings.TrimSpace(body.DomainCanonicalPath)
	resolved, rerr := h.resolveDomainOverlay(r.Context(), q, existingDomains, preferredPath)
	if rerr != nil || resolved == nil || resolved.Intent == nil || len(resolved.Contracts) == 0 {
		errMsg := ""
		if rerr != nil {
			errMsg = rerr.Error()
		}
		domainOverlayTrace(r.Context(), "execute.resolve.error", map[string]any{
			"lensId":        strings.TrimSpace(lensID),
			"preferredPath": preferredPath,
			"error":         errMsg,
		})
		http.Error(w, "failed to resolve query into a domain", http.StatusBadRequest)
		return
	}

	domainPath := preferredPath
	if domainPath == "" {
		domainPath = strings.TrimSpace(resolved.Contracts[0].CanonicalPath)
	}
	if domainPath == "" || taxonomy.ValidateCanonicalPath(domainPath) != nil || !strings.HasPrefix(domainPath, "/domain/") {
		domainOverlayTrace(r.Context(), "execute.domain_path.invalid", map[string]any{
			"lensId":     strings.TrimSpace(lensID),
			"domainPath": domainPath,
		})
		http.Error(w, "invalid domainCanonicalPath", http.StatusBadRequest)
		return
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

	ctx := rtctx.With(r.Context(), rtctx.Context{
		TenantID:      authInfo.TenantID,
		ProjectID:     lens.ProjectID,
		LensID:        lens.ID,
		CorrelationID: correlationID,
		SessionID:     sessionID,
	})

	now := time.Now().UTC()
	specialties := taxonomy.NormalizeSpecialties(body.Specialties)
	state := map[string]interface{}{
		"query":                       q,
		"originalQuery":               q,
		"targetCanonicalPath":         domainPath,
		"lensId":                      lens.ID,
		"focus":                       resolved.Intent.Focus,
		"spherePaths":                 resolved.Intent.SpherePaths,
		"keywords":                    resolved.Intent.Keywords,
		"domains":                     resolved.Intent.Domains,
		"currentState":                string(models.ScoutingJobStatePending),
		"scoutingStrategy":            string(models.ScoutingStrategySnapshot),
		"recursionDepth":              0.0,
		"maxRecursionDepth":           0.0,
		pipeline.StateKeyPipelinePass: string(pipeline.PassBaseEnrichment),
		"trigger":                     "ui.domain_contract_execute",
		"correlationId":               correlationID,
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
		TargetCanonicalPath: domainPath,
		ScoutingStrategy:    string(models.ScoutingStrategySnapshot),
		BillingContext: models.BillingContext{
			ChargeModel: string(models.ChargeModelBounty),
			ProjectID:   lens.ProjectID,
		},
		CurrentState: models.ScoutingJobStatePending,
	}
	jobID := ""
	if id, cerr := h.service.ScoutingJobs().Create(ctx, scoutingJob); cerr == nil && strings.TrimSpace(id) != "" {
		jobID = strings.TrimSpace(id)
		domainOverlayTrace(ctx, "execute.job.created", map[string]any{
			"lensId":     strings.TrimSpace(lensID),
			"domainPath": domainPath,
			"jobId":      jobID,
		})
	} else if cerr != nil {
		domainOverlayTrace(ctx, "execute.job.create.error", map[string]any{
			"lensId":     strings.TrimSpace(lensID),
			"domainPath": domainPath,
			"error":      cerr.Error(),
		})
	} else {
		domainOverlayTrace(ctx, "execute.job.create.empty_id", map[string]any{
			"lensId":     strings.TrimSpace(lensID),
			"domainPath": domainPath,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Correlation-Id", correlationID)
	if err := json.NewEncoder(w).Encode(domainOverlayExecuteResponse{
		JobID:           jobID,
		DomainPath:      domainPath,
		ExistingDomains: existingDomains,
	}); err != nil {
		domainOverlayTrace(ctx, "execute.encode.error", map[string]any{
			"lensId": strings.TrimSpace(lensID),
			"jobId":  jobID,
			"error":  err.Error(),
		})
	}
}

func (h *HorizonHandlers) collectNearbyDomainPaths(ctx context.Context, lens *models.Lens) []string {
	if lens == nil {
		return nil
	}
	frRepo := h.service.Fragments()
	edgeRepo := h.service.Edges()
	if frRepo == nil || edgeRepo == nil {
		domainOverlayTrace(ctx, "collect_domains.fallback", map[string]any{
			"lensId": strings.TrimSpace(lens.ID),
			"reason": "repos_unavailable",
		})
		return collectNearbyDomainPathsFromGraph(lens.Scope, nil)
	}
	scopePaths := make([]string, 0, len(lens.Scope))
	for _, p := range lens.Scope {
		p = strings.TrimSpace(p)
		if p == "" || strings.HasPrefix(p, "/domain/") {
			continue
		}
		scopePaths = append(scopePaths, p)
		if len(scopePaths) >= 20 {
			break
		}
	}
	frags, err := frRepo.ListByCanonicalPaths(ctx, scopePaths)
	if err != nil {
		domainOverlayTrace(ctx, "collect_domains.fragments.error", map[string]any{
			"lensId":       strings.TrimSpace(lens.ID),
			"scopePathCnt": len(scopePaths),
			"error":        err.Error(),
		})
		return collectNearbyDomainPathsFromGraph(lens.Scope, nil)
	}
	if len(frags) == 0 {
		domainOverlayTrace(ctx, "collect_domains.fragments.empty", map[string]any{
			"lensId":       strings.TrimSpace(lens.ID),
			"scopePathCnt": len(scopePaths),
		})
		return collectNearbyDomainPathsFromGraph(lens.Scope, nil)
	}
	edgePaths := make([]overlayEdgePath, 0, 64)
	edgeListErrors := 0
	neighborReadErrors := 0
	for _, f := range frags {
		if f == nil || strings.TrimSpace(f.ID) == "" {
			continue
		}
		outEdges, outErr := edgeRepo.ListBySource(ctx, "global", f.ID, nil, 20)
		if outErr != nil {
			edgeListErrors++
		}
		inEdges, inErr := edgeRepo.ListByTarget(ctx, "global", f.ID, nil, 20)
		if inErr != nil {
			edgeListErrors++
		}
		for _, ed := range append(outEdges, inEdges...) {
			if ed == nil {
				continue
			}
			otherID := ed.SourceFragmentID
			if otherID == f.ID {
				otherID = ed.TargetFragmentID
			}
			if strings.TrimSpace(otherID) == "" {
				continue
			}
			other, err := frRepo.Get(ctx, otherID)
			if err != nil || other == nil {
				if err != nil {
					neighborReadErrors++
				}
				continue
			}
			cp := strings.TrimSpace(other.CanonicalPath)
			if cp == "" || !strings.HasPrefix(cp, "/domain/") {
				continue
			}
			edgePaths = append(edgePaths, overlayEdgePath{Source: strings.TrimSpace(f.CanonicalPath), Target: cp})
		}
	}
	if edgeListErrors > 0 {
		domainOverlayTrace(ctx, "collect_domains.edges.error", map[string]any{
			"lensId":         strings.TrimSpace(lens.ID),
			"edgeListErrors": edgeListErrors,
		})
	}
	if neighborReadErrors > 0 {
		domainOverlayTrace(ctx, "collect_domains.neighbor.error", map[string]any{
			"lensId":             strings.TrimSpace(lens.ID),
			"neighborReadErrors": neighborReadErrors,
		})
	}
	return collectNearbyDomainPathsFromGraph(lens.Scope, edgePaths)
}

type overlayEdgePath struct {
	Source string
	Target string
}

func collectNearbyDomainPathsFromGraph(scope []string, edges []overlayEdgePath) []string {
	out := make([]string, 0, 12)
	seen := map[string]struct{}{}
	for _, p := range scope {
		p = strings.TrimSpace(p)
		if p == "" || !strings.HasPrefix(p, "/domain/") {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
		if len(out) >= 12 {
			return out
		}
	}
	for _, e := range edges {
		if strings.HasPrefix(e.Source, "/domain/") {
			if _, ok := seen[e.Source]; !ok {
				seen[e.Source] = struct{}{}
				out = append(out, e.Source)
			}
		}
		if strings.HasPrefix(e.Target, "/domain/") {
			if _, ok := seen[e.Target]; !ok {
				seen[e.Target] = struct{}{}
				out = append(out, e.Target)
			}
		}
		if len(out) >= 12 {
			return out
		}
	}
	return out
}
