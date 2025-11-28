package http

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/services"
)

// BigQueryHandler handles BigQuery requests via natural language
type BigQueryHandler struct {
	service *services.BigQueryAIService
	logger  *log.Logger
}

// NewBigQueryHandler creates a new BigQuery handler
func NewBigQueryHandler(service *services.BigQueryAIService, logger *log.Logger) *BigQueryHandler {
	return &BigQueryHandler{
		service: service,
		logger:  logger,
	}
}

// BigQueryRequest represents a natural language query request
type BigQueryRequest struct {
	Query             string `json:"query"`
	ProjectID         string `json:"projectID,omitempty"`         // GCP project ID (BigQuery project)
	DatasetID         string `json:"datasetID,omitempty"`         // BigQuery dataset ID
	MaxResults        int    `json:"maxResults,omitempty"`         // Max results to return
	TenantID          string `json:"tenantID,omitempty"`          // Tenant ID for filtering documents
	DocumentProjectID string `json:"documentProjectID,omitempty"` // Document project ID for filtering (CoreDoc projectId)
}

// Query handles POST /api/v1/bigquery/query
func (h *BigQueryHandler) Query(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}

	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	var req BigQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Failed to parse request body")
		return
	}

	if strings.TrimSpace(req.Query) == "" {
		WriteBadRequest(w, "MISSING_QUERY", "Query is required")
		return
	}

	if h.logger != nil {
		h.logger.Printf("[BigQueryHandler] Query request: query=%q, tenantID=%s, documentProjectID=%s, projectID=%s, datasetID=%s", 
			req.Query, auth.TenantID, req.DocumentProjectID, req.ProjectID, req.DatasetID)
	}

	// Use tenantID from auth if not provided in request
	tenantID := req.TenantID
	if tenantID == "" {
		tenantID = auth.TenantID
	}

	// Convert to service format
	serviceReq := &services.QueryRequest{
		NaturalLanguageQuery: req.Query,
		ProjectID:            req.ProjectID,
		DatasetID:            req.DatasetID,
		MaxResults:           req.MaxResults,
		TenantID:             tenantID,
		DocumentProjectID:    req.DocumentProjectID,
	}

	if h.logger != nil {
		h.logger.Printf("[BigQueryHandler] Service request prepared: tenantID=%s, documentProjectID=%s", 
			serviceReq.TenantID, serviceReq.DocumentProjectID)
	}

	// Execute query
	response, err := h.service.ExecuteQuery(r.Context(), serviceReq)
	if err != nil {
		if h.logger != nil {
			h.logger.Printf("[BigQueryHandler] Error: %v", err)
		}
		WriteInternalError(w, fmt.Sprintf("Failed to execute query: %v", err))
		return
	}

	// Return result
	WriteJSON(w, http.StatusOK, response)
}
