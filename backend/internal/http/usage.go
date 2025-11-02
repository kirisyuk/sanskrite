package http

import (
	"log"
	"net/http"
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

	// Get all events
	metrics, err := h.repo.ListByUser(r.Context(), auth.TenantID, auth.UID, startTime, endTime)
	if err != nil {
		h.logger.Printf("Failed to list usage metrics: %v", err)
		WriteInternalError(w, "Failed to retrieve usage metrics")
		return
	}

	// Get aggregates
	byModel, err := h.repo.AggregateByModel(r.Context(), auth.TenantID, auth.UID, startTime, endTime)
	if err != nil {
		h.logger.Printf("Failed to aggregate by model: %v", err)
		byModel = make(map[string]repo.ModelAggregate)
	}

	dailyTotals, err := h.repo.GetDailyTotals(r.Context(), auth.TenantID, auth.UID, startTime, endTime)
	if err != nil {
		h.logger.Printf("Failed to get daily totals: %v", err)
		dailyTotals = make(map[string]repo.DailyTotal)
	}

	// Format response
	response := map[string]interface{}{
		"events":      metrics,
		"byModel":     byModel,
		"dailyTotals": dailyTotals,
		"period": map[string]string{
			"startDate": startDate,
			"endDate":   endDate,
		},
	}

	WriteJSON(w, http.StatusOK, response)
}
