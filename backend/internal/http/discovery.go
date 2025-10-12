package http

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/discovery"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// DiscoveryHandler handles discovery-related HTTP requests
type DiscoveryHandler struct {
	projectsRepo    *repo.ProjectsRepository
	dataSourcesRepo *repo.DataSourcesRepository
	logger          *log.Logger
	orchestrator    *discovery.Orchestrator
}

// NewDiscoveryHandler creates a new DiscoveryHandler
func NewDiscoveryHandler(
	projectsRepo *repo.ProjectsRepository,
	dataSourcesRepo *repo.DataSourcesRepository,
	logger *log.Logger,
) *DiscoveryHandler {
	return &DiscoveryHandler{
		projectsRepo:    projectsRepo,
		dataSourcesRepo: dataSourcesRepo,
		logger:          logger,
	}
}

// WithOrchestrator wires orchestrator instance for execution
func (h *DiscoveryHandler) WithOrchestrator(o *discovery.Orchestrator) *DiscoveryHandler {
	h.orchestrator = o
	return h
}

// ExecuteDiscoveryRequest represents the request body for discovery execution
type ExecuteDiscoveryRequest struct {
	ProjectID string  `json:"projectId"`
	QueryText string  `json:"queryText"`
	Language  *string `json:"language,omitempty"`
	Sources   struct {
		Web      []string `json:"web"`
		Telegram []string `json:"telegram"`
	} `json:"sources"`
	CorrelationID *string `json:"correlationId,omitempty"`
}

// ExecuteDiscovery handles POST /api/v1/sources-discovery/execute
func (h *DiscoveryHandler) ExecuteDiscovery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}

	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	var req ExecuteDiscoveryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Failed to parse request body")
		return
	}

	// If projectID is provided, verify it exists and belongs to user's tenant
	if req.ProjectID != "" {
		project, err := h.projectsRepo.Get(r.Context(), req.ProjectID)
		if err != nil {
			h.logger.Printf("Project not found: %s", req.ProjectID)
			WriteNotFound(w, "Project not found")
			return
		}

		if project.TenantID != authInfo.TenantID {
			WriteForbidden(w, "Access denied")
			return
		}

		// Update project's lastInteractionAt
		if err := h.projectsRepo.UpdateLastInteraction(r.Context(), req.ProjectID); err != nil {
			h.logger.Printf("Failed to update project interaction time: %v", err)
			// Non-fatal, continue
		}
	}

	// Generate correlation ID if not provided
	correlationID := ""
	if req.CorrelationID != nil {
		correlationID = *req.CorrelationID
	} else {
		// Generate a simple ID (in production, use UUID)
		correlationID = generateCorrelationID()
	}

	// Parse and canonicalize sources
	var webSources []string
	var telegramSources []string

	for _, url := range req.Sources.Web {
		// URL canonicalization would happen here
		canonical := strings.TrimSpace(url)
		if canonical != "" {
			webSources = append(webSources, canonical)
		}
	}

	for _, handle := range req.Sources.Telegram {
		// Normalize telegram handles (remove @ if present)
		canonical := strings.TrimSpace(strings.TrimPrefix(handle, "@"))
		if canonical != "" {
			telegramSources = append(telegramSources, canonical)
		}
	}

	if len(webSources) == 0 && len(telegramSources) == 0 {
		WriteBadRequest(w, "VALIDATION_ERROR", "At least one source (web or telegram) is required")
		return
	}

	// Execute orchestrator (fire-and-forget go routine) using independent timeout-bound context
	if h.orchestrator != nil {
		go func() {
			lang := "en"
			if req.Language != nil && *req.Language != "" {
				lang = *req.Language
			}
			// Important: decouple from r.Context() so work continues after HTTP returns
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			_ = h.orchestrator.Execute(ctx, discovery.ExecuteInput{
				TenantID:    authInfo.TenantID,
				ProjectID:   req.ProjectID,
				QueryText:   req.QueryText,
				Language:    lang,
				WebSources:  webSources,
				TgHandles:   telegramSources,
				Correlation: correlationID,
				OwnerUID:    authInfo.UID,
			})
		}()
	}

	response := map[string]interface{}{
		"success":       true,
		"correlationId": correlationID,
		"message":       "Discovery task queued",
		"sourcesCount": map[string]int{
			"web":      len(webSources),
			"telegram": len(telegramSources),
		},
	}

	WriteJSON(w, http.StatusAccepted, response)
}

// ListProjectSources handles GET /api/v1/projects/{id}/datasources
func (h *DiscoveryHandler) ListProjectSources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}

	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	// Extract projectId from path
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 5 {
		WriteBadRequest(w, "INVALID_PATH", "Project ID is required")
		return
	}
	projectID := pathParts[3]

	// Verify project exists and belongs to user's tenant
	project, err := h.projectsRepo.Get(r.Context(), projectID)
	if err != nil {
		h.logger.Printf("Project not found: %s", projectID)
		WriteNotFound(w, "Project not found")
		return
	}

	if project.TenantID != authInfo.TenantID {
		WriteForbidden(w, "Access denied")
		return
	}

	// Fetch sources for project
	sources, err := h.dataSourcesRepo.ListByProject(r.Context(), authInfo.TenantID, projectID)
	if err != nil {
		h.logger.Printf("Failed to list sources for project %s: %v", projectID, err)
		WriteInternalError(w, "Failed to list sources")
		return
	}

	WriteJSON(w, http.StatusOK, sources)
}

// generateCorrelationID generates a simple correlation ID
// In production, use UUID
func generateCorrelationID() string {
	// Simple implementation for now
	return fmt.Sprintf("disc_%d", time.Now().UnixNano())
}
