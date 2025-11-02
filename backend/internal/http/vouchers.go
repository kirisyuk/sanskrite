package http

import (
	"crypto/rand"
	"encoding/json"
	"log"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// VouchersHandler handles voucher-related HTTP requests (admin only)
type VouchersHandler struct {
	repo      *repo.VouchersRepository
	usersRepo *repo.UsersRepository
	logger    *log.Logger
}

// NewVouchersHandler creates a new VouchersHandler
func NewVouchersHandler(vouchersRepo *repo.VouchersRepository, usersRepo *repo.UsersRepository, logger *log.Logger) *VouchersHandler {
	return &VouchersHandler{
		repo:      vouchersRepo,
		usersRepo: usersRepo,
		logger:    logger,
	}
}

// requireAdmin checks if the current user is an admin
func (h *VouchersHandler) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return false
	}

	user, err := h.usersRepo.GetByIDAndTenant(r.Context(), authInfo.UID, authInfo.TenantID)
	if err != nil || user == nil || user.Role == nil || strings.ToLower(*user.Role) != "admin" {
		WriteForbidden(w, "Admin access required")
		return false
	}

	return true
}

// CreateVoucherRequest represents the request body for creating a voucher
type CreateVoucherRequest struct {
	Code           string                  `json:"code"`
	Tier           models.SubscriptionTier `json:"tier"` // pro or max
	DurationMonths int                     `json:"durationMonths"`
	ValidFrom      *time.Time              `json:"validFrom,omitempty"`
	ValidTo        *time.Time              `json:"validTo,omitempty"`
	MaxUses        *int                    `json:"maxUses,omitempty"`
	Note           *string                 `json:"note,omitempty"`
}

// CreateVoucher handles POST /api/v1/vouchers
func (h *VouchersHandler) CreateVoucher(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}

	if !h.requireAdmin(w, r) {
		return
	}

	authInfo := GetAuthInfo(r.Context())

	var req CreateVoucherRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Failed to parse request body")
		return
	}

	// Generate code if not provided
	voucherCode := strings.ToUpper(strings.TrimSpace(req.Code))
	if voucherCode == "" {
		voucherCode = generateVoucherCode()
	}
	if req.Tier != models.SubscriptionTierPro && req.Tier != models.SubscriptionTierMax {
		WriteBadRequest(w, "VALIDATION_ERROR", "tier must be 'pro' or 'max'")
		return
	}
	if req.DurationMonths <= 0 {
		WriteBadRequest(w, "VALIDATION_ERROR", "durationMonths must be positive")
		return
	}

	now := time.Now().UTC()
	voucher := &models.Voucher{
		Code:           voucherCode,
		Tier:           req.Tier,
		DurationMonths: req.DurationMonths,
		ValidFrom:      now,
		ValidTo:        req.ValidTo,
		MaxUses:        req.MaxUses,
		UsedCount:      0,
		IsActive:       true,
		CreatedBy:      authInfo.UID,
		Note:           req.Note,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if req.ValidFrom != nil {
		voucher.ValidFrom = *req.ValidFrom
	}

	if err := h.repo.Create(r.Context(), voucher); err != nil {
		h.logger.Printf("Failed to create voucher: %v", err)
		if strings.Contains(err.Error(), "already exists") {
			WriteConflict(w, "VOUCHER_EXISTS", "Voucher code already exists", nil)
			return
		}
		WriteInternalError(w, "Failed to create voucher")
		return
	}

	h.logger.Printf("Admin %s created voucher %s (tier: %s, duration: %d months)", authInfo.UID, voucher.Code, voucher.Tier, voucher.DurationMonths)

	WriteJSON(w, http.StatusCreated, voucher)
}

// ListVouchers handles GET /api/v1/vouchers
func (h *VouchersHandler) ListVouchers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}

	if !h.requireAdmin(w, r) {
		return
	}

	// Parse query parameters
	tierStr := r.URL.Query().Get("tier")

	// Support both "activeOnly" (legacy) and "isActive" (new) parameters
	var activeOnly *bool // nil = show all, true = active only, false = inactive only
	if isActiveStr := r.URL.Query().Get("isActive"); isActiveStr != "" {
		// If isActive is provided, use it (true = active only, false = inactive only)
		val := isActiveStr == "true"
		activeOnly = &val
	} else if activeOnlyStr := r.URL.Query().Get("activeOnly"); activeOnlyStr != "" {
		// Legacy support for activeOnly parameter
		val := activeOnlyStr == "true"
		activeOnly = &val
	}
	// else: activeOnly remains nil = show all

	var tier *models.SubscriptionTier
	if tierStr != "" {
		t := models.SubscriptionTier(strings.ToLower(tierStr))
		if t == models.SubscriptionTierPro || t == models.SubscriptionTierMax {
			tier = &t
		}
	}

	vouchers, err := h.repo.List(r.Context(), tier, activeOnly)
	if err != nil {
		h.logger.Printf("Failed to list vouchers: %v", err)
		WriteInternalError(w, "Failed to list vouchers")
		return
	}

	WriteJSON(w, http.StatusOK, vouchers)
}

// GetVoucher handles GET /api/v1/vouchers/{voucherId}
func (h *VouchersHandler) GetVoucher(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}

	if !h.requireAdmin(w, r) {
		return
	}

	// Extract voucher ID from path
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 4 {
		WriteBadRequest(w, "INVALID_PATH", "Voucher ID is required")
		return
	}
	voucherID := pathParts[3]

	voucher, err := h.repo.Get(r.Context(), voucherID)
	if err != nil {
		if err == repo.ErrVoucherNotFound {
			WriteNotFound(w, "Voucher not found")
			return
		}
		h.logger.Printf("Failed to get voucher: %v", err)
		WriteInternalError(w, "Failed to get voucher")
		return
	}

	WriteJSON(w, http.StatusOK, voucher)
}

// UpdateVoucherRequest represents the request body for updating a voucher
type UpdateVoucherRequest struct {
	IsActive *bool      `json:"isActive,omitempty"`
	ValidTo  *time.Time `json:"validTo,omitempty"`
	MaxUses  *int       `json:"maxUses,omitempty"`
	Note     *string    `json:"note,omitempty"`
}

// UpdateVoucher handles PATCH /api/v1/vouchers/{voucherId}
func (h *VouchersHandler) UpdateVoucher(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch && r.Method != http.MethodPut {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only PATCH or PUT is allowed", nil)
		return
	}

	if !h.requireAdmin(w, r) {
		return
	}

	// Extract voucher ID from path
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 4 {
		WriteBadRequest(w, "INVALID_PATH", "Voucher ID is required")
		return
	}
	voucherID := pathParts[3]

	voucher, err := h.repo.Get(r.Context(), voucherID)
	if err != nil {
		if err == repo.ErrVoucherNotFound {
			WriteNotFound(w, "Voucher not found")
			return
		}
		h.logger.Printf("Failed to get voucher: %v", err)
		WriteInternalError(w, "Failed to get voucher")
		return
	}

	var req UpdateVoucherRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Failed to parse request body")
		return
	}

	// Update fields
	if req.IsActive != nil {
		voucher.IsActive = *req.IsActive
	}
	if req.ValidTo != nil {
		voucher.ValidTo = req.ValidTo
	}
	if req.MaxUses != nil {
		voucher.MaxUses = req.MaxUses
	}
	if req.Note != nil {
		voucher.Note = req.Note
	}

	voucher.UpdatedAt = time.Now().UTC()

	if err := h.repo.Update(r.Context(), voucher); err != nil {
		h.logger.Printf("Failed to update voucher: %v", err)
		WriteInternalError(w, "Failed to update voucher")
		return
	}

	WriteJSON(w, http.StatusOK, voucher)
}

// DeleteVoucher handles DELETE /api/v1/vouchers/{voucherId}
func (h *VouchersHandler) DeleteVoucher(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only DELETE is allowed", nil)
		return
	}

	if !h.requireAdmin(w, r) {
		return
	}

	// Extract voucher ID from path
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 4 {
		WriteBadRequest(w, "INVALID_PATH", "Voucher ID is required")
		return
	}
	voucherID := pathParts[3]

	if err := h.repo.Delete(r.Context(), voucherID); err != nil {
		if err == repo.ErrVoucherNotFound {
			WriteNotFound(w, "Voucher not found")
			return
		}
		h.logger.Printf("Failed to delete voucher: %v", err)
		WriteInternalError(w, "Failed to delete voucher")
		return
	}

	WriteJSON(w, http.StatusOK, map[string]string{"message": "Voucher deleted successfully"})
}

// generateVoucherCode generates a random voucher code using uppercase Latin letters
// Format: XXXX-XXXX-XXXX (12 characters total, 3 groups of 4)
func generateVoucherCode() string {
	const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	const codeLength = 12
	const groupSize = 4

	b := make([]byte, codeLength)
	for i := range b {
		num, _ := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		b[i] = letters[num.Int64()]
	}

	// Format as XXXX-XXXX-XXXX
	result := make([]byte, codeLength+2) // +2 for dashes
	idx := 0
	for i := 0; i < codeLength; i++ {
		if i > 0 && i%groupSize == 0 {
			result[idx] = '-'
			idx++
		}
		result[idx] = b[i]
		idx++
	}

	return string(result)
}
