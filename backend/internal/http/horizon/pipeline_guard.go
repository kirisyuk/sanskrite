package horizon

import (
	"context"
	"net/http"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/realtime"
)

func scoutExecutionMessageForReason(reason string) string {
	switch strings.TrimSpace(reason) {
	case "scout_cloud_tasks_enabled", "scout_firestore_poller_enabled":
		return ""
	case "scout_cloud_tasks_not_configured":
		return "Scout pipeline is unavailable: CLOUD_TASKS_TARGET_BASE_URL and/or CLOUD_TASKS_OIDC_SERVICE_ACCOUNT is missing."
	case "scout_cloud_tasks_init_failed":
		return "Scout pipeline is unavailable: Cloud Tasks dispatcher initialization failed."
	case "scout_cloud_tasks_disabled":
		return "Scout pipeline is unavailable: Cloud Tasks dispatch is disabled."
	case "horizon_service_unavailable":
		return "Scout pipeline is unavailable: Horizon service is not initialized."
	default:
		return "Scout pipeline is unavailable: background execution is not configured."
	}
}

func (h *HorizonHandlers) scoutExecutionAvailability() (bool, string, string) {
	if h == nil || h.service == nil {
		return false, "horizon_service_unavailable", scoutExecutionMessageForReason("horizon_service_unavailable")
	}
	ready, reason := h.service.ScoutExecutionRuntime()
	reason = strings.TrimSpace(reason)
	if ready {
		if reason == "" {
			reason = "scout_execution_ready"
		}
		return true, reason, ""
	}
	if reason == "" {
		reason = "scout_dispatch_unavailable"
	}
	return false, reason, scoutExecutionMessageForReason(reason)
}

func (h *HorizonHandlers) publishAssemblyFatal(
	ctx context.Context,
	tenantID, projectID, lensID, phase, correlationID, sessionID, code, message string,
) {
	if h == nil || h.service == nil {
		return
	}
	pub := h.service.RealtimePublisher()
	if pub == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(projectID) == "" {
		return
	}
	payload := map[string]interface{}{
		"tenantId":  strings.TrimSpace(tenantID),
		"projectId": strings.TrimSpace(projectID),
		"phase":     strings.TrimSpace(phase),
		"stage":     "failed",
		"status":    "failed",
		"fatal":     true,
		"code":      strings.TrimSpace(code),
		"error":     strings.TrimSpace(message),
		"humanReadable": map[string]string{
			"title":       "Fatal pipeline error",
			"description": strings.TrimSpace(message),
		},
	}
	if strings.TrimSpace(lensID) != "" {
		payload["lensId"] = strings.TrimSpace(lensID)
	}
	if strings.TrimSpace(correlationID) != "" {
		payload["correlationId"] = strings.TrimSpace(correlationID)
	}
	if strings.TrimSpace(sessionID) != "" {
		payload["sessionId"] = strings.TrimSpace(sessionID)
	}
	_ = pub.PublishJSON(ctx, realtime.EventType("horizon.assembly.failed"), strings.TrimSpace(tenantID), payload)
}

func writeFatalPipelineError(
	w http.ResponseWriter,
	status int,
	code, message, phase string,
	details map[string]interface{},
) {
	if status <= 0 {
		status = http.StatusServiceUnavailable
	}
	payload := map[string]interface{}{
		"error":   strings.TrimSpace(message),
		"message": strings.TrimSpace(message),
		"code":    strings.TrimSpace(code),
		"fatal":   true,
	}
	if strings.TrimSpace(phase) != "" {
		payload["phase"] = strings.TrimSpace(phase)
	}
	if len(details) > 0 {
		payload["details"] = details
	}
	writeJSON(w, status, payload)
}
