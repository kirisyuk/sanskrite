package http

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

// HorizonTelemetryHandler serves admin telemetry endpoints.
type HorizonTelemetryHandler struct {
	repo      *horizonrepo.HorizonTelemetryRepository
	usersRepo *corerepo.UsersRepository
	logger    *log.Logger
}

func NewHorizonTelemetryHandler(telemetryRepo *horizonrepo.HorizonTelemetryRepository, usersRepo *corerepo.UsersRepository, logger *log.Logger) *HorizonTelemetryHandler {
	return &HorizonTelemetryHandler{
		repo:      telemetryRepo,
		usersRepo: usersRepo,
		logger:    logger,
	}
}

func (h *HorizonTelemetryHandler) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return false
	}
	if h.usersRepo == nil {
		WriteForbidden(w, "Admin access required")
		return false
	}
	user, err := h.usersRepo.GetByIDAndTenant(r.Context(), authInfo.UID, authInfo.TenantID)
	if err != nil || user == nil || user.Role == nil || strings.ToLower(*user.Role) != "admin" {
		WriteForbidden(w, "Admin access required")
		return false
	}
	return true
}

// List GET /api/v1/admin/horizon/telemetry?component=...&metric=...&limit=48
func (h *HorizonTelemetryHandler) List(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}
	if !h.requireAdmin(w, r) {
		return
	}
	if h.repo == nil {
		WriteInternalError(w, "telemetry repo not configured")
		return
	}

	component := strings.TrimSpace(r.URL.Query().Get("component"))
	metric := strings.TrimSpace(r.URL.Query().Get("metric"))
	if component == "" || metric == "" {
		WriteBadRequest(w, "INVALID_QUERY", "component and metric are required")
		return
	}

	limit := 48
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	tenantID := strings.TrimSpace(r.URL.Query().Get("tenantId"))
	if tenantID == "" {
		tenantID = "global"
	}

	items, err := h.repo.List(r.Context(), tenantID, component, metric, limit)
	if err != nil {
		if h.logger != nil {
			h.logger.Printf("telemetry list error: %v", err)
		}
		WriteInternalError(w, "failed to list telemetry")
		return
	}

	summary := map[string]int64{}
	for _, item := range items {
		if item == nil || item.Counters == nil {
			continue
		}
		for k, v := range item.Counters {
			summary[k] += v
		}
	}
	var hitRate float64
	if total := summary["resolve_calls"]; total > 0 {
		hitRate = float64(summary["redirect_hits"]) / float64(total)
	}

	// IMPORTANT: Return JSON keys in lowerCamelCase as expected by the frontend.
	// Do NOT expose internal Go struct field names (WindowStart, Counters, etc).
	type telemetryItemDTO struct {
		Component     string            `json:"component"`
		Metric        string            `json:"metric"`
		WindowStart   string            `json:"windowStart"`
		WindowSizeSec int               `json:"windowSizeSec"`
		Dimensions    map[string]string `json:"dimensions,omitempty"`
		Counters      map[string]int64  `json:"counters"`
		UpdatedAt     string            `json:"updatedAt"`
	}
	out := make([]telemetryItemDTO, 0, len(items))
	for _, it := range items {
		if it == nil {
			continue
		}
		ws := it.WindowStart.UTC().Format(time.RFC3339Nano)
		ua := it.UpdatedAt.UTC().Format(time.RFC3339Nano)
		out = append(out, telemetryItemDTO{
			Component:     it.Component,
			Metric:        it.Metric,
			WindowStart:   ws,
			WindowSizeSec: it.WindowSizeSec,
			Dimensions:    it.Dimensions,
			Counters:      it.Counters,
			UpdatedAt:     ua,
		})
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"items":    out,
		"summary":  summary,
		"hitRate":  hitRate,
		"tenantId": tenantID,
	})
}
