package horizon

import (
	"encoding/json"
	"net/http"

	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// CreateLens создает новый Lens
func (h *HorizonHandlers) CreateLens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var lens models.Lens
	if err := json.NewDecoder(r.Body).Decode(&lens); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	lens.TenantID = authInfo.TenantID

	lensID, err := h.service.Lens().CreateLens(r.Context(), &lens)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	lens.ID = lensID
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(lens)
}

// GetLens получает Lens по ID
func (h *HorizonHandlers) GetLens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	lensID := extractIDFromPath(r, "/api/horizon/lenses/")
	if lensID == "" {
		http.Error(w, "lensId required in path", http.StatusBadRequest)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	lens, err := h.service.Lens().GetLens(r.Context(), lensID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Проверяем доступ
	if lens.TenantID != authInfo.TenantID {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(lens)
}

// UpdateLens обновляет Lens
func (h *HorizonHandlers) UpdateLens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	lensID := extractIDFromPath(r, "/api/horizon/lenses/")
	if lensID == "" {
		http.Error(w, "lensId required in path", http.StatusBadRequest)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	var lens models.Lens
	if err := json.NewDecoder(r.Body).Decode(&lens); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	lens.ID = lensID
	if err := h.service.Lens().UpdateLens(r.Context(), &lens); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(lens)
}

// DeleteLens удаляет Lens
func (h *HorizonHandlers) DeleteLens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	lensID := extractIDFromPath(r, "/api/horizon/lenses/")
	if lensID == "" {
		http.Error(w, "lensId required in path", http.StatusBadRequest)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	if err := h.service.Lens().DeleteLens(r.Context(), lensID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

