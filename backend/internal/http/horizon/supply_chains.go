package horizon

import (
	"encoding/json"
	"net/http"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
)

// GetSupplyChainsByNode returns materialized supply chain artifacts (SUPPLY_CHAIN_PATH)
// where nodeRefs contains the given canonical path.
// GET /api/horizon/supply-chains/by-node?path=/org/catl
// Query uses FindByNodeRef(tenantId, path, SUPPLY_CHAIN_PATH).
func (h *HorizonHandlers) GetSupplyChainsByNode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	canonicalPath := strings.TrimSpace(r.URL.Query().Get("path"))
	if canonicalPath == "" {
		http.Error(w, "path query parameter is required", http.StatusBadRequest)
		return
	}
	if !strings.HasPrefix(canonicalPath, "/") {
		http.Error(w, "path must be a canonical path (start with /)", http.StatusBadRequest)
		return
	}

	if h.materializedArtifactsRepo == nil {
		http.Error(w, "Supply chains not available", http.StatusServiceUnavailable)
		return
	}

	tenantID := strings.TrimSpace(authInfo.TenantID)
	if tenantID == "" {
		tenantID = "global"
	}

	artifacts, err := h.materializedArtifactsRepo.FindByNodeRef(
		r.Context(),
		tenantID,
		canonicalPath,
		horizonmodels.MaterializedKindSupplyChainPath,
	)
	if err != nil {
		http.Error(w, "Failed to fetch supply chains", http.StatusInternalServerError)
		return
	}

	// Also try "global" tenant if tenant-specific returned nothing
	if len(artifacts) == 0 && tenantID != "global" {
		artifacts, err = h.materializedArtifactsRepo.FindByNodeRef(
			r.Context(),
			"global",
			canonicalPath,
			horizonmodels.MaterializedKindSupplyChainPath,
		)
		if err != nil {
			http.Error(w, "Failed to fetch supply chains", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"path":      canonicalPath,
		"tenantId":  tenantID,
		"chains":    artifacts,
		"total":     len(artifacts),
	})
}

// GetSupplyChainsByLens returns materialized supply chain artifacts (SUPPLY_CHAIN_PATH) for a lens.
// GET /api/horizon/supply-chains/by-lens?lensId=...
func (h *HorizonHandlers) GetSupplyChainsByLens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	lensID := strings.TrimSpace(r.URL.Query().Get("lensId"))
	if lensID == "" {
		http.Error(w, "lensId query parameter is required", http.StatusBadRequest)
		return
	}

	if h.materializedArtifactsRepo == nil {
		http.Error(w, "Supply chains not available", http.StatusServiceUnavailable)
		return
	}

	limit := 100
	artifacts, err := h.materializedArtifactsRepo.ListByLens(r.Context(), lensID, limit)
	if err != nil {
		http.Error(w, "Failed to fetch supply chains", http.StatusInternalServerError)
		return
	}

	// Filter to SUPPLY_CHAIN_PATH only
	chains := make([]*horizonmodels.MaterializedArtifact, 0, len(artifacts))
	for _, a := range artifacts {
		if a.Kind == horizonmodels.MaterializedKindSupplyChainPath {
			chains = append(chains, a)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"lensId": lensID,
		"chains": chains,
		"total":  len(chains),
	})
}
