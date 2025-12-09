package http

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// SignalHistoryHandler manages signal history endpoints
type SignalHistoryHandler struct {
	historyRepo *repo.SignalHistoryRepository
	linkRepo    *repo.DocumentArtifactSignalsRepository
}

// NewSignalHistoryHandler creates a new handler
func NewSignalHistoryHandler(historyRepo *repo.SignalHistoryRepository, linkRepo *repo.DocumentArtifactSignalsRepository) *SignalHistoryHandler {
	return &SignalHistoryHandler{
		historyRepo: historyRepo,
		linkRepo:    linkRepo,
	}
}

// ListBySignal lists history entries for a specific signal
// GET /api/v1/listening-signals/:signalId/history
func (h *SignalHistoryHandler) ListBySignal(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	path := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(path) < 4 {
		WriteBadRequest(w, "INVALID_PATH", "Signal ID required")
		return
	}
	signalID := path[2]

	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 && parsed <= 1000 {
			limit = parsed
		}
	}

	history, err := h.historyRepo.ListBySignal(r.Context(), signalID, limit)
	if err != nil {
		WriteInternalError(w, "Failed to list history")
		return
	}

	WriteJSON(w, http.StatusOK, history)
}

// ListByArtifact lists history entries for signals linked to an artifact
// GET /api/v1/artifacts/:artifactId/signal-history
func (h *SignalHistoryHandler) ListByArtifact(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	path := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(path) < 4 {
		WriteBadRequest(w, "INVALID_PATH", "Artifact ID required")
		return
	}
	artifactID := path[2]

	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 && parsed <= 1000 {
			limit = parsed
		}
	}

	history, err := h.historyRepo.ListByArtifact(r.Context(), artifactID, limit, h.linkRepo)
	if err != nil {
		WriteInternalError(w, "Failed to list history")
		return
	}

	WriteJSON(w, http.StatusOK, history)
}

// ListByTenant lists history entries for a tenant (optionally filtered by project)
// GET /api/v1/projects/:projectId/signal-history
// GET /api/v1/signal-history
func (h *SignalHistoryHandler) ListByTenant(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	path := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	var projectID *string

	// Check if projectId is in path
	if len(path) >= 4 && path[2] == "projects" {
		projectIDStr := path[3]
		projectID = &projectIDStr
	}

	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 && parsed <= 1000 {
			limit = parsed
		}
	}

	history, err := h.historyRepo.ListByTenant(r.Context(), auth.TenantID, projectID, limit)
	if err != nil {
		WriteInternalError(w, "Failed to list history")
		return
	}

	WriteJSON(w, http.StatusOK, history)
}


