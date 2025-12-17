package horizon

import (
	"encoding/json"
	"net/http"
	"strings"

	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// ListFragments получает список HorizonFragment через Lens scope
func (h *HorizonHandlers) ListFragments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Получаем lensID из query параметра
	lensID := r.URL.Query().Get("lensId")
	if lensID == "" {
		http.Error(w, "lensId query parameter is required", http.StatusBadRequest)
		return
	}

	// Получаем Lens для определения scope
	lens, err := h.service.Lens().GetLens(r.Context(), lensID)
	if err != nil {
		http.Error(w, "Lens not found", http.StatusNotFound)
		return
	}

	// Проверяем доступ к Lens
	if lens.TenantID != authInfo.TenantID {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Получаем все HorizonFragment из scope
	fragments, err := h.service.Fragments().ListByCanonicalPaths(r.Context(), lens.Scope)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"fragments": fragments,
		"total":     len(fragments),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetFragment получает HorizonFragment по ID или canonicalPath
func (h *HorizonHandlers) GetFragment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Извлекаем ID из пути: /api/horizon/fragments/{id}
	path := strings.TrimPrefix(r.URL.Path, "/api/horizon/fragments/")
	if path == "" {
		http.Error(w, "Fragment ID or path required", http.StatusBadRequest)
		return
	}

	// Проверяем, это ID или canonicalPath
	var fragment *models.HorizonFragment
	var err error

	// Если путь содержит "/", это canonicalPath
	if strings.Contains(path, "/") {
		fragment, err = h.service.Fragments().GetByCanonicalPath(r.Context(), path)
	} else {
		fragment, err = h.service.Fragments().Get(r.Context(), path)
	}

	if err != nil {
		http.Error(w, "Fragment not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(fragment)
}
