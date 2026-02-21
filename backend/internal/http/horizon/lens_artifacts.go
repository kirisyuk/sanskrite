package horizon

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
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
