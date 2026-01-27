package horizon

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	agentmodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

// LensArtifactsListResponse is the response for listing lens artifacts.
type LensArtifactsListResponse struct {
	Artifacts        []*models.LensArtifact `json:"artifacts"`
	TotalCount       int                    `json:"totalCount"`
	FragmentCount    int                    `json:"fragmentCount"`
	EdgeCount        int                    `json:"edgeCount"`
	UnprocessedCount int                    `json:"unprocessedCount"`
}

// LensArtifactStatsResponse is the response for artifact statistics.
type LensArtifactStatsResponse struct {
	TotalArtifacts       int        `json:"totalArtifacts"`
	FragmentArtifacts    int        `json:"fragmentArtifacts"`
	EdgeArtifacts        int        `json:"edgeArtifacts"`
	ProcessedForPhase2   int        `json:"processedForPhase2"`
	UnprocessedForPhase2 int        `json:"unprocessedForPhase2"`
	LastUpdated          *time.Time `json:"lastUpdated,omitempty"`
}

// Phase2TriggerRequest is the request to trigger Phase 2 Strategic Overlay.
type Phase2TriggerRequest struct {
	DomainPaths []string `json:"domainPaths,omitempty"` // Optional: specific domains to process
	Force       bool     `json:"force,omitempty"`       // Force reprocessing even if already processed
}

// Phase2TriggerResponse is the response after triggering Phase 2.
type Phase2TriggerResponse struct {
	JobIDs  []string `json:"jobIds"`
	Message string   `json:"message"`
	Queued  int      `json:"queued"`
}

// ListLensArtifacts returns the artifacts accumulated for a lens.
// GET /api/horizon/lenses/{lensId}/artifacts
func (h *HorizonHandlers) ListLensArtifacts(w http.ResponseWriter, r *http.Request) {
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
	lensID = strings.TrimSuffix(lensID, "/artifacts")
	if strings.TrimSpace(lensID) == "" {
		http.Error(w, "lensId required in path", http.StatusBadRequest)
		return
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

	// Parse query params
	limit := 200
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 1000 {
			limit = v
		}
	}
	typeFilter := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("type")))

	artifactsRepo := h.service.LensArtifacts()
	if artifactsRepo == nil {
		http.Error(w, "Lens artifacts not available", http.StatusServiceUnavailable)
		return
	}

	var artifacts []*models.LensArtifact
	var artErr error

	switch typeFilter {
	case "FRAGMENT":
		artifacts, artErr = artifactsRepo.ListFragmentsByLens(r.Context(), lensID, limit)
	case "EDGE":
		artifacts, artErr = artifactsRepo.ListEdgesByLens(r.Context(), lensID, limit)
	default:
		artifacts, artErr = artifactsRepo.ListByLens(r.Context(), lensID, limit)
	}

	if artErr != nil {
		http.Error(w, "Failed to fetch artifacts", http.StatusInternalServerError)
		return
	}

	// Count by type
	fragmentCount := 0
	edgeCount := 0
	unprocessedCount := 0
	for _, a := range artifacts {
		if a.IsFragment() {
			fragmentCount++
		} else if a.IsEdge() {
			edgeCount++
		}
		if a.NeedsStrategicProcessing() {
			unprocessedCount++
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(LensArtifactsListResponse{
		Artifacts:        artifacts,
		TotalCount:       len(artifacts),
		FragmentCount:    fragmentCount,
		EdgeCount:        edgeCount,
		UnprocessedCount: unprocessedCount,
	})
}

// GetLensArtifactStats returns statistics about lens artifacts.
// GET /api/horizon/lenses/{lensId}/artifacts/stats
func (h *HorizonHandlers) GetLensArtifactStats(w http.ResponseWriter, r *http.Request) {
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
	lensID = strings.TrimSuffix(lensID, "/artifacts/stats")
	if strings.TrimSpace(lensID) == "" {
		http.Error(w, "lensId required in path", http.StatusBadRequest)
		return
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

	artifactsRepo := h.service.LensArtifacts()
	if artifactsRepo == nil {
		http.Error(w, "Lens artifacts not available", http.StatusServiceUnavailable)
		return
	}

	// Fetch all artifacts for stats (limited to reasonable amount)
	artifacts, err := artifactsRepo.ListByLens(r.Context(), lensID, 2000)
	if err != nil {
		http.Error(w, "Failed to fetch artifacts", http.StatusInternalServerError)
		return
	}

	fragmentCount := 0
	edgeCount := 0
	processedCount := 0
	unprocessedCount := 0
	var lastUpdated *time.Time

	for _, a := range artifacts {
		if a.IsFragment() {
			fragmentCount++
		} else if a.IsEdge() {
			edgeCount++
		}
		if a.NeedsStrategicProcessing() {
			unprocessedCount++
		} else {
			processedCount++
		}
		if lastUpdated == nil || a.UpdatedAt.After(*lastUpdated) {
			t := a.UpdatedAt
			lastUpdated = &t
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(LensArtifactStatsResponse{
		TotalArtifacts:       len(artifacts),
		FragmentArtifacts:    fragmentCount,
		EdgeArtifacts:        edgeCount,
		ProcessedForPhase2:   processedCount,
		UnprocessedForPhase2: unprocessedCount,
		LastUpdated:          lastUpdated,
	})
}

// TriggerPhase2StrategicOverlay manually triggers Phase 2 Strategic Overlay for a lens.
// POST /api/horizon/lenses/{lensId}/trigger-phase2
func (h *HorizonHandlers) TriggerPhase2StrategicOverlay(w http.ResponseWriter, r *http.Request) {
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
	lensID = strings.TrimSuffix(lensID, "/trigger-phase2")
	if strings.TrimSpace(lensID) == "" {
		http.Error(w, "lensId required in path", http.StatusBadRequest)
		return
	}

	var body Phase2TriggerRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body) // Optional body
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

	// Correlation/session IDs (optional, but improves realtime UX and filtering).
	correlationID := strings.TrimSpace(r.Header.Get("X-Correlation-Id"))
	if correlationID == "" {
		correlationID = strings.TrimSpace(r.Header.Get("X-Correlation-ID"))
	}
	sessionID := strings.TrimSpace(r.Header.Get("X-Session-Id"))
	if sessionID == "" {
		sessionID = strings.TrimSpace(r.Header.Get("X-Session-ID"))
	}

	// Find domains in lens scope to trigger Phase 2
	domainPaths := body.DomainPaths
	if len(domainPaths) == 0 {
		// Auto-detect domains from lens scope
		for _, p := range lens.Scope {
			p = strings.TrimSpace(p)
			if strings.HasPrefix(p, "/domain/") {
				domainPaths = append(domainPaths, p)
			}
		}
	}

	if len(domainPaths) == 0 {
		http.Error(w, "No domains found in lens scope", http.StatusBadRequest)
		return
	}

	// Ensure domains are present in lens scope (idempotent; improves UI visibility).
	{
		seen := map[string]struct{}{}
		for _, p := range lens.Scope {
			p = strings.TrimSpace(p)
			if p != "" {
				seen[p] = struct{}{}
			}
		}
		changed := false
		for _, p := range domainPaths {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			lens.Scope = append(lens.Scope, p)
			changed = true
		}
		if changed {
			_ = h.service.Lens().UpdateLens(r.Context(), lens) // best-effort
		}
	}

	// Create Phase 2 jobs for each domain
	jobIDs := make([]string, 0, len(domainPaths))
	scoutJobsRepo := h.service.ScoutingJobs()
	if scoutJobsRepo == nil {
		http.Error(w, "Scouting jobs not available", http.StatusServiceUnavailable)
		return
	}

	for _, domainPath := range domainPaths {
		domainPath = strings.TrimSpace(domainPath)
		if domainPath == "" || !strings.HasPrefix(domainPath, "/domain/") {
			continue
		}

		now := time.Now().UTC()
		state := map[string]interface{}{
			"targetCanonicalPath": domainPath,
			"lensId":              lens.ID,
			"currentState":        string(models.ScoutingJobStatePending),
			"scoutingStrategy":    string(models.ScoutingStrategySnapshot),
			"recursionDepth":      0.0,
			"maxRecursionDepth":   0.0,
			"domainOverlayMode":   true,
			"phase2Overlay":       true,
			"trigger":             "api.trigger_phase2",
			"force":               body.Force,
		}
		if correlationID != "" {
			state["correlationId"] = correlationID
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

		if id, cerr := scoutJobsRepo.Create(r.Context(), scoutingJob); cerr == nil && strings.TrimSpace(id) != "" {
			jobIDs = append(jobIDs, strings.TrimSpace(id))
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(Phase2TriggerResponse{
		JobIDs:  jobIDs,
		Message: "Phase 2 Strategic Overlay jobs queued",
		Queued:  len(jobIDs),
	})
}

// DeleteLensArtifacts deletes all artifacts for a lens (cleanup).
// DELETE /api/horizon/lenses/{lensId}/artifacts
func (h *HorizonHandlers) DeleteLensArtifacts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	authInfo, err := requireAuth(r)
	if err != nil || authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	lensID := extractIDFromPath(r, "/api/horizon/lenses/")
	lensID = strings.TrimSuffix(lensID, "/artifacts")
	if strings.TrimSpace(lensID) == "" {
		http.Error(w, "lensId required in path", http.StatusBadRequest)
		return
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

	artifactsRepo := h.service.LensArtifacts()
	if artifactsRepo == nil {
		http.Error(w, "Lens artifacts not available", http.StatusServiceUnavailable)
		return
	}

	if err := artifactsRepo.DeleteByLens(r.Context(), lensID); err != nil {
		http.Error(w, "Failed to delete artifacts", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Lens artifacts deleted",
		"lensId":  lensID,
	})
}

// GetLensArtifactContext returns the simplified context for LLM processing.
// GET /api/horizon/lenses/{lensId}/artifacts/context
func (h *HorizonHandlers) GetLensArtifactContext(w http.ResponseWriter, r *http.Request) {
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
	lensID = strings.TrimSuffix(lensID, "/artifacts/context")
	if strings.TrimSpace(lensID) == "" {
		http.Error(w, "lensId required in path", http.StatusBadRequest)
		return
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

	artifactsRepo := h.service.LensArtifacts()
	if artifactsRepo == nil {
		http.Error(w, "Lens artifacts not available", http.StatusServiceUnavailable)
		return
	}

	// Parse pagination params
	pageSize := 50
	if ps := r.URL.Query().Get("pageSize"); ps != "" {
		if v, err := strconv.Atoi(ps); err == nil && v > 0 && v <= 200 {
			pageSize = v
		}
	}
	pageNumber := 0
	if pn := r.URL.Query().Get("page"); pn != "" {
		if v, err := strconv.Atoi(pn); err == nil && v >= 0 {
			pageNumber = v
		}
	}

	page, err := artifactsRepo.ListByLensConnectivityPaginated(r.Context(), lensID, pageSize, pageNumber)
	if err != nil {
		http.Error(w, "Failed to fetch artifacts", http.StatusInternalServerError)
		return
	}

	// Build simplified context for LLM
	ctx := models.NewLensArtifactContext(page.Artifacts)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"fragments":      ctx.Fragments,
		"edges":          ctx.Edges,
		"pageNumber":     page.PageNumber,
		"totalPages":     page.TotalPages,
		"totalArtifacts": page.TotalArtifacts,
		"nextCursor":     page.NextCursor,
	})
}
