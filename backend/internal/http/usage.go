package http

import (
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// UsageHandler handles usage metrics requests
type UsageHandler struct {
	repo   *repo.UsageMetricsRepository
	logger *log.Logger
}

// NewUsageHandler creates a new UsageHandler
func NewUsageHandler(repo *repo.UsageMetricsRepository, logger *log.Logger) *UsageHandler {
	return &UsageHandler{
		repo:   repo,
		logger: logger,
	}
}

// GetUsage handles GET /api/v1/usage
func (h *UsageHandler) GetUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}

	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	// Parse query parameters
	q := r.URL.Query()
	startDate := q.Get("startDate")
	endDate := q.Get("endDate")
	_ = q.Get("granularity") // "1d", "7d", "30d" - reserved for future use

	// Default to last 7 days
	if startDate == "" || endDate == "" {
		endTime := time.Now().UTC()
		startTime := endTime.AddDate(0, 0, -7)
		startDate = startTime.Format("2006-01-02")
		endDate = endTime.Format("2006-01-02")
	}

	startTime, err := time.Parse("2006-01-02", startDate)
	if err != nil {
		WriteBadRequest(w, "INVALID_DATE", "Invalid startDate format (use YYYY-MM-DD)")
		return
	}
	endTime, err := time.Parse("2006-01-02", endDate)
	if err != nil {
		WriteBadRequest(w, "INVALID_DATE", "Invalid endDate format (use YYYY-MM-DD)")
		return
	}
	endTime = endTime.Add(24 * time.Hour).Add(-1 * time.Second) // End of day

	// Check if events are requested (for backward compatibility)
	includeEvents := q.Get("includeEvents") == "true"

	// Get all aggregates in a single pass (loads metrics once, computes all aggregations)
	aggregates, err := h.repo.GetAllAggregates(r.Context(), auth.TenantID, auth.UID, startTime, endTime)
	if err != nil {
		h.logger.Printf("Failed to get usage aggregates: %v", err)
		WriteInternalError(w, "Failed to retrieve usage metrics")
		return
	}

	h.logger.Printf("[GetUsage] Aggregates: byModel=%d entries, dailyTotals=%d entries, totals.count=%d", len(aggregates.ByModel), len(aggregates.DailyTotals), aggregates.Totals.Count)

	// Format response
	response := map[string]interface{}{
		"byModel":         aggregates.ByModel,
		"dailyTotals":     aggregates.DailyTotals,
		"tokenCategories": aggregates.TokenCategories,
		"totals":          aggregates.Totals,
		"period": map[string]string{
			"startDate": startDate,
			"endDate":   endDate,
		},
	}

	// Only include events if explicitly requested (for backward compatibility)
	if includeEvents {
		metrics, _, err := h.repo.ListByUser(r.Context(), auth.TenantID, auth.UID, startTime, endTime, 0, 0)
		if err != nil {
			h.logger.Printf("Failed to list usage metrics: %v", err)
			// Don't fail the request, just omit events
		} else {
			response["events"] = metrics
		}
	}

	WriteJSON(w, http.StatusOK, response)
}

// GetEvents handles GET /api/v1/usage/events
// Query params: startDate, endDate, page (default 1), limit (default 200, minimum 200)
// Returns paginated events with total count for efficient grouping
func (h *UsageHandler) GetEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}

	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	// Parse query parameters
	q := r.URL.Query()
	startDate := q.Get("startDate")
	endDate := q.Get("endDate")
	pageStr := q.Get("page")
	limitStr := q.Get("limit")

	// Parse dates (required)
	if startDate == "" || endDate == "" {
		WriteBadRequest(w, "MISSING_PARAMS", "startDate and endDate are required")
		return
	}

	startTime, err := time.Parse("2006-01-02", startDate)
	if err != nil {
		WriteBadRequest(w, "INVALID_DATE", "Invalid startDate format (use YYYY-MM-DD)")
		return
	}
	endTime, err := time.Parse("2006-01-02", endDate)
	if err != nil {
		WriteBadRequest(w, "INVALID_DATE", "Invalid endDate format (use YYYY-MM-DD)")
		return
	}
	endTime = endTime.Add(24 * time.Hour).Add(-1 * time.Second) // End of day

	// Parse pagination parameters
	page := 1
	if pageStr != "" {
		page, err = strconv.Atoi(pageStr)
		if err != nil || page < 1 {
			WriteBadRequest(w, "INVALID_PARAM", "page must be a positive integer")
			return
		}
	}

	limit := 200 // Default and minimum limit
	if limitStr != "" {
		limit, err = strconv.Atoi(limitStr)
		if err != nil || limit < 200 {
			WriteBadRequest(w, "INVALID_PARAM", "limit must be at least 200")
			return
		}
	}

	// Get paginated events
	events, total, err := h.repo.ListByUserPaginated(r.Context(), auth.TenantID, auth.UID, startTime, endTime, page, limit)
	if err != nil {
		h.logger.Printf("Failed to get paginated events: %v", err)
		WriteInternalError(w, "Failed to retrieve events")
		return
	}

	h.logger.Printf("[GetEvents] Loaded %d events (page %d, limit %d, total %d) for user %s", len(events), page, limit, total, auth.UID)

	// Calculate pagination metadata
	totalPages := (total + limit - 1) / limit
	if totalPages == 0 {
		totalPages = 1
	}

	response := map[string]interface{}{
		"events": events,
		"pagination": map[string]interface{}{
			"page":       page,
			"limit":      limit,
			"total":      total,
			"totalPages": totalPages,
		},
		"period": map[string]string{
			"startDate": startDate,
			"endDate":   endDate,
		},
	}

	WriteJSON(w, http.StatusOK, response)
}

// GetAverageTiming handles GET /api/v1/usage/avg-timing
// Query params: provider, model
func (h *UsageHandler) GetAverageTiming(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}

	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	// Parse query parameters
	q := r.URL.Query()
	provider := q.Get("provider")
	model := q.Get("model")

	if provider == "" || model == "" {
		WriteBadRequest(w, "MISSING_PARAMS", "provider and model are required")
		return
	}

	avgTiming, err := h.repo.GetAverageTiming(r.Context(), auth.TenantID, auth.UID, provider, model)
	if err != nil {
		h.logger.Printf("Failed to get average timing: %v", err)
		WriteInternalError(w, "Failed to retrieve average timing")
		return
	}

	response := map[string]interface{}{
		"estimatedSeconds": avgTiming,
		"provider":         provider,
		"model":            model,
	}

	WriteJSON(w, http.StatusOK, response)
}
