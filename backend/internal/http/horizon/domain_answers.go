package horizon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	horizondomain "github.com/igorkirisyuk/sanskrite/internal/horizon/domain"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/rtctx"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/pipeline"
	agentmodels "github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

type domainAnswersListResponse struct {
	DomainAnswers []*models.DomainAnswerSnapshot `json:"domainAnswers"`
	TotalCount    int                            `json:"totalCount"`
}

// ListDomainAnswers returns domain answer snapshots for a lens.
// GET /api/horizon/lenses/{lensId}/domain-answers
func (h *HorizonHandlers) ListDomainAnswers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	authInfo, err := requireAuth(r)
	if err != nil || authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	lensID := extractIDFromPath(r, "/api/horizon/lenses/")
	lensID = strings.TrimSuffix(strings.TrimSpace(lensID), "/domain-answers")
	if lensID == "" {
		http.Error(w, "lensId required in path", http.StatusBadRequest)
		return
	}

	lens, lerr := h.service.Lens().GetLens(r.Context(), lensID)
	if lerr != nil || lens == nil {
		http.Error(w, "Lens not found", http.StatusNotFound)
		return
	}
	if strings.TrimSpace(lens.TenantID) != strings.TrimSpace(authInfo.TenantID) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	repo := h.service.DomainAnswerSnapshots()
	if repo == nil {
		http.Error(w, "Domain answers not available", http.StatusServiceUnavailable)
		return
	}

	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > 200 {
		limit = 200
	}

	items, err := repo.ListByLens(r.Context(), lensID, limit)
	if err != nil {
		http.Error(w, "Failed to load domain answers", http.StatusInternalServerError)
		return
	}

	out := make([]*models.DomainAnswerSnapshot, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, domainAnswersListResponse{
		DomainAnswers: out,
		TotalCount:    len(out),
	})
}

// GetDomainAnswer returns one domain answer snapshot by domainId (or snapshot id).
// GET /api/horizon/lenses/{lensId}/domain-answers/{domainId}
func (h *HorizonHandlers) GetDomainAnswer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	authInfo, err := requireAuth(r)
	if err != nil || authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/horizon/lenses/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 3 || strings.TrimSpace(parts[0]) == "" {
		http.Error(w, "lensId required in path", http.StatusBadRequest)
		return
	}
	lensID := strings.TrimSpace(parts[0])
	domainToken, _ := url.PathUnescape(strings.TrimSpace(parts[2]))
	domainToken = strings.TrimSpace(domainToken)
	if domainToken == "" {
		http.Error(w, "domainId required in path", http.StatusBadRequest)
		return
	}

	lens, lerr := h.service.Lens().GetLens(r.Context(), lensID)
	if lerr != nil || lens == nil {
		http.Error(w, "Lens not found", http.StatusNotFound)
		return
	}
	if strings.TrimSpace(lens.TenantID) != strings.TrimSpace(authInfo.TenantID) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	repo := h.service.DomainAnswerSnapshots()
	if repo == nil {
		http.Error(w, "Domain answers not available", http.StatusServiceUnavailable)
		return
	}

	var snapshot *models.DomainAnswerSnapshot
	if strings.HasPrefix(domainToken, "das_") {
		item, err := repo.Get(r.Context(), domainToken)
		if err != nil {
			if errors.Is(err, horizonrepo.ErrDomainAnswerSnapshotNotFound) {
				http.Error(w, "Domain answer not found", http.StatusNotFound)
				return
			}
			http.Error(w, "Failed to load domain answer", http.StatusInternalServerError)
			return
		}
		if item == nil || strings.TrimSpace(item.LensID) != lensID {
			http.Error(w, "Domain answer not found", http.StatusNotFound)
			return
		}
		snapshot = item
	} else {
		item, err := repo.GetByLensAndDomainID(r.Context(), lensID, domainToken)
		if err != nil {
			if errors.Is(err, horizonrepo.ErrDomainAnswerSnapshotNotFound) {
				http.Error(w, "Domain answer not found", http.StatusNotFound)
				return
			}
			http.Error(w, "Failed to load domain answer", http.StatusInternalServerError)
			return
		}
		snapshot = item
	}

	writeJSON(w, http.StatusOK, snapshot)
}

// ─── Create / Run ────────────────────────────────────────────────────────────

type createDomainAnswerRequest struct {
	Query string `json:"query"`
}

type createDomainAnswerResponse struct {
	JobID      string `json:"jobId"`
	DomainPath string `json:"domainCanonicalPath"`
}

type runDomainAnswerResponse struct {
	JobID      string `json:"jobId"`
	DomainPath string `json:"domainCanonicalPath"`
}

// CreateDomainAnswer resolves a user query into a domain contract and creates a scouting job.
// POST /api/horizon/lenses/{lensId}/domain-answers
func (h *HorizonHandlers) CreateDomainAnswer(w http.ResponseWriter, r *http.Request) {
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
	lensID = strings.TrimSuffix(strings.TrimSpace(lensID), "/domain-answers")
	if lensID == "" {
		http.Error(w, "lensId required in path", http.StatusBadRequest)
		return
	}

	var body createDomainAnswerRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	q := strings.TrimSpace(body.Query)
	if q == "" {
		http.Error(w, "query is required", http.StatusBadRequest)
		return
	}

	lens, lerr := h.service.Lens().GetLens(r.Context(), lensID)
	if lerr != nil || lens == nil {
		http.Error(w, "Lens not found", http.StatusNotFound)
		return
	}
	if strings.TrimSpace(lens.TenantID) != strings.TrimSpace(authInfo.TenantID) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Resolve query → domain contract
	existingDomains := h.collectNearbyDomainPaths(r.Context(), lens)
	resolved, rerr := h.resolveDomainOverlay(r.Context(), q, existingDomains, "")
	if rerr != nil || resolved == nil || resolved.Intent == nil || len(resolved.Contracts) == 0 {
		http.Error(w, "Failed to resolve query into a domain", http.StatusBadRequest)
		return
	}
	domainPath := strings.TrimSpace(resolved.Contracts[0].CanonicalPath)
	if domainPath == "" || taxonomy.ValidateCanonicalPath(domainPath) != nil || !strings.HasPrefix(domainPath, "/domain/") {
		http.Error(w, "invalid domain path resolved", http.StatusBadRequest)
		return
	}

	// Create scouting job (same pattern as ExecuteDomainOverlay)
	correlationID, sessionID := extractCorrelationSession(r)
	ctx := rtctx.With(r.Context(), rtctx.Context{
		TenantID:      authInfo.TenantID,
		ProjectID:     lens.ProjectID,
		LensID:        lens.ID,
		CorrelationID: correlationID,
		SessionID:     sessionID,
	})

	jobID := h.createDomainScoutingJob(ctx, lens, domainPath, q, resolved, correlationID, sessionID, "ui.create_domain_answer")
	if jobID == "" {
		http.Error(w, "Failed to create domain job", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Correlation-Id", correlationID)
	_ = json.NewEncoder(w).Encode(createDomainAnswerResponse{
		JobID:      jobID,
		DomainPath: domainPath,
	})
}

// RunDomainAnswer triggers DomainContractEngine for an existing domain fragment that has no snapshot yet.
// POST /api/horizon/lenses/{lensId}/domain-answers/{domainId}/run
func (h *HorizonHandlers) RunDomainAnswer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	authInfo, err := requireAuth(r)
	if err != nil || authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/horizon/lenses/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 4 || strings.TrimSpace(parts[0]) == "" {
		http.Error(w, "lensId required in path", http.StatusBadRequest)
		return
	}
	lensID := strings.TrimSpace(parts[0])
	domainToken, _ := url.PathUnescape(strings.TrimSpace(parts[2]))
	domainToken = strings.TrimSpace(domainToken)
	if domainToken == "" {
		http.Error(w, "domainId required in path", http.StatusBadRequest)
		return
	}

	lens, lerr := h.service.Lens().GetLens(r.Context(), lensID)
	if lerr != nil || lens == nil {
		http.Error(w, "Lens not found", http.StatusNotFound)
		return
	}
	if strings.TrimSpace(lens.TenantID) != strings.TrimSpace(authInfo.TenantID) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Resolve domainToken: it can be a canonical path or a fragment ID.
	domainPath := ""
	if strings.HasPrefix(domainToken, "/domain/") {
		domainPath = domainToken
	} else {
		// Try to find the fragment and get its canonical path.
		frRepo := h.service.Fragments()
		if frRepo != nil {
			frag, ferr := frRepo.Get(r.Context(), domainToken)
			if ferr == nil && frag != nil && strings.HasPrefix(frag.CanonicalPath, "/domain/") {
				domainPath = frag.CanonicalPath
			}
		}
	}
	if domainPath == "" || !strings.HasPrefix(domainPath, "/domain/") {
		http.Error(w, "Domain not found or not a domain fragment", http.StatusNotFound)
		return
	}

	// Try to reconstruct query from existing scouting jobs for this domain.
	query := h.reconstructQueryForDomain(r.Context(), authInfo.TenantID, lensID, domainPath)

	// Create scouting job to run domain contract engine.
	correlationID, sessionID := extractCorrelationSession(r)
	ctx := rtctx.With(r.Context(), rtctx.Context{
		TenantID:      authInfo.TenantID,
		ProjectID:     lens.ProjectID,
		LensID:        lens.ID,
		CorrelationID: correlationID,
		SessionID:     sessionID,
	})

	// Resolve if we have a query; otherwise build a minimal contract from the path.
	var resolved *horizondomain.ResolveResult
	if query != "" {
		existingDomains := h.collectNearbyDomainPaths(r.Context(), lens)
		resolved, _ = h.resolveDomainOverlay(ctx, query, existingDomains, domainPath)
	}

	jobID := h.createDomainScoutingJob(ctx, lens, domainPath, query, resolved, correlationID, sessionID, "ui.run_domain_answer")
	if jobID == "" {
		http.Error(w, "Failed to create domain job", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Correlation-Id", correlationID)
	_ = json.NewEncoder(w).Encode(runDomainAnswerResponse{
		JobID:      jobID,
		DomainPath: domainPath,
	})
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func extractCorrelationSession(r *http.Request) (string, string) {
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
	return correlationID, sessionID
}

func (h *HorizonHandlers) createDomainScoutingJob(
	ctx context.Context,
	lens *models.Lens,
	domainPath, query string,
	resolved *horizondomain.ResolveResult,
	correlationID, sessionID, trigger string,
) string {
	now := time.Now().UTC()
	state := map[string]interface{}{
		"targetCanonicalPath":         domainPath,
		"lensId":                      lens.ID,
		"currentState":                string(models.ScoutingJobStatePending),
		"scoutingStrategy":            string(models.ScoutingStrategySnapshot),
		"recursionDepth":              0.0,
		"maxRecursionDepth":           0.0,
		pipeline.StateKeyPipelinePass: string(pipeline.PassBaseEnrichment),
		"trigger":                     trigger,
		"correlationId":               correlationID,
	}
	if query != "" {
		state["query"] = query
		state["originalQuery"] = query
	}
	if resolved != nil && resolved.Intent != nil {
		state["focus"] = resolved.Intent.Focus
		state["spherePaths"] = resolved.Intent.SpherePaths
		state["keywords"] = resolved.Intent.Keywords
		state["domains"] = resolved.Intent.Domains
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
			UserQuery: query,
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

	jobID, cerr := h.service.ScoutingJobs().Create(ctx, scoutingJob)
	if cerr != nil {
		return ""
	}
	return strings.TrimSpace(jobID)
}

func (h *HorizonHandlers) reconstructQueryForDomain(ctx context.Context, tenantID, lensID, domainPath string) string {
	jobsRepo := h.service.ScoutingJobs()
	if jobsRepo == nil {
		return ""
	}
	// Find the most recent scouting job that targeted this domain path in this lens.
	jobs, err := jobsRepo.ListByLens(ctx, tenantID, lensID, 100)
	if err != nil || len(jobs) == 0 {
		return ""
	}
	for _, j := range jobs {
		if j == nil {
			continue
		}
		if strings.TrimSpace(j.TargetCanonicalPath) == domainPath {
			if q := strings.TrimSpace(j.UserQuery); q != "" {
				return q
			}
			if j.State != nil {
				if v, ok := j.State["originalQuery"].(string); ok && strings.TrimSpace(v) != "" {
					return strings.TrimSpace(v)
				}
			}
		}
	}
	return ""
}
