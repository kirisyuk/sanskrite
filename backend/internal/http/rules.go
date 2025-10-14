package http

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

type RulesHandler struct {
	repo   *repo.RulesRepository
	logger *log.Logger
}

func NewRulesHandler(repo *repo.RulesRepository, logger *log.Logger) *RulesHandler {
	return &RulesHandler{repo: repo, logger: logger}
}

// CreateRule handles POST /api/v1/rules
func (h *RulesHandler) CreateRule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	var body models.Rule
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Failed to parse body")
		return
	}
	body.TenantID = auth.TenantID
	if body.CreatedAt.IsZero() {
		body.CreatedAt = time.Now().UTC()
	}
	body.UpdatedAt = time.Now().UTC()
	id, err := h.repo.Create(r.Context(), &body)
	if err != nil {
		WriteInternalError(w, "Failed to create rule")
		return
	}
	WriteJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// ListRules handles GET /api/v1/rules?level=&id=
func (h *RulesHandler) ListRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	scope := map[string]any{}
	if v := r.URL.Query().Get("level"); v != "" {
		scope["level"] = v
	}
	if v := r.URL.Query().Get("id"); v != "" {
		scope["id"] = v
	}
	items, err := h.repo.ListByScope(r.Context(), auth.TenantID, scope)
	if err != nil {
		WriteInternalError(w, "Failed to list rules")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// UpdateRule handles PATCH /api/v1/rules/{id}
func (h *RulesHandler) UpdateRule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only PATCH is allowed", nil)
		return
	}
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		WriteBadRequest(w, "INVALID_PATH", "Rule ID is required")
		return
	}
	id := parts[3]
	var patch map[string]any
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Failed to parse body")
		return
	}
	if err := h.repo.Update(r.Context(), id, patch); err != nil {
		WriteInternalError(w, "Failed to update rule")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"success": true})
}

// DeleteRule handles DELETE /api/v1/rules/{id}
func (h *RulesHandler) DeleteRule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only DELETE is allowed", nil)
		return
	}
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		WriteBadRequest(w, "INVALID_PATH", "Rule ID is required")
		return
	}
	id := parts[3]
	if err := h.repo.Delete(r.Context(), id); err != nil {
		WriteInternalError(w, "Failed to delete rule")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"success": true})
}
