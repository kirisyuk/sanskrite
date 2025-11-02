package http

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/services"
)

// SubscriptionsHandler handles subscription-related HTTP requests
type SubscriptionsHandler struct {
	service *services.SubscriptionService
	logger  *log.Logger
}

// NewSubscriptionsHandler creates a new SubscriptionsHandler
func NewSubscriptionsHandler(service *services.SubscriptionService, logger *log.Logger) *SubscriptionsHandler {
	return &SubscriptionsHandler{
		service: service,
		logger:  logger,
	}
}

// GetSubscription handles GET /api/v1/subscriptions/me
func (h *SubscriptionsHandler) GetSubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}

	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	sub, err := h.service.GetOrCreateSubscription(r.Context(), authInfo.TenantID, authInfo.UID)
	if err != nil {
		h.logger.Printf("Failed to get subscription: %v", err)
		WriteInternalError(w, "Failed to get subscription")
		return
	}

	WriteJSON(w, http.StatusOK, sub)
}

// ApplyVoucherRequest represents the request body for applying a voucher
type ApplyVoucherRequest struct {
	Code string `json:"code"`
}

// ApplyVoucher handles POST /api/v1/subscriptions/voucher
func (h *SubscriptionsHandler) ApplyVoucher(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}

	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	var req ApplyVoucherRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Failed to parse request body")
		return
	}

	if req.Code == "" {
		WriteBadRequest(w, "VALIDATION_ERROR", "voucher code is required")
		return
	}

	sub, err := h.service.ApplyVoucher(r.Context(), authInfo.TenantID, authInfo.UID, strings.ToUpper(strings.TrimSpace(req.Code)))
	if err != nil {
		h.logger.Printf("Failed to apply voucher: %v", err)
		if strings.Contains(err.Error(), "invalid voucher") {
			WriteBadRequest(w, "INVALID_VOUCHER", err.Error())
			return
		}
		WriteInternalError(w, "Failed to apply voucher")
		return
	}

	WriteJSON(w, http.StatusOK, sub)
}

