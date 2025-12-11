package http

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// waitlistRepoKey is a context key for waitlist repository
type waitlistRepoKey struct{}

// WithWaitlistRepo attaches WaitlistRepository into request context
func WithWaitlistRepo(wl *repo.WaitlistRepository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), waitlistRepoKey{}, wl)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// WaitlistHandler manages waitlist operations
type WaitlistHandler struct {
	wl     *repo.WaitlistRepository
	users  *repo.UsersRepository
	logger *log.Logger
}

func NewWaitlistHandler(wl *repo.WaitlistRepository, users *repo.UsersRepository, logger *log.Logger) *WaitlistHandler {
	return &WaitlistHandler{wl: wl, users: users, logger: logger}
}

type waitlistJoinReq struct {
	Email string `json:"email"`
	Note  string `json:"note,omitempty"`
}

// Join POST /api/v1/waitlist
func (h *WaitlistHandler) Join(w http.ResponseWriter, r *http.Request) {
	var req waitlistJoinReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" {
		WriteBadRequest(w, "INVALID_JSON", "email is required")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	note := strings.TrimSpace(req.Note)
	var notePtr *string
	if note != "" {
		notePtr = &note
	}
	// If already exists and approved, do not overwrite; return ok silently
	if existing, err := h.wl.Get(r.Context(), email); err == nil && existing != nil {
		if strings.ToLower(existing.Status) == "approved" {
			WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
			return
		}
		// keep existing status; update note if provided
		if notePtr != nil {
			existing.Note = notePtr
		}
		if err := h.wl.Upsert(r.Context(), existing); err != nil {
			WriteInternalError(w, "failed to save waitlist entry")
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	// Not exists: create pending
	entry := &models.WaitlistEntry{Email: email, Note: notePtr, Status: "pending"}
	if err := h.wl.Upsert(r.Context(), entry); err != nil {
		WriteInternalError(w, "failed to save waitlist entry")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// Check GET /api/v1/waitlist/check?email=
func (h *WaitlistHandler) Check(w http.ResponseWriter, r *http.Request) {
	email := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("email")))
	if email == "" {
		WriteBadRequest(w, "MISSING_EMAIL", "email required")
		return
	}

	// Hardcoded admin check
	if strings.EqualFold(email, "kirisyuk@gmail.com") {
		WriteJSON(w, http.StatusOK, map[string]any{
			"found": true, 
			"status": "approved",
		})
		return
	}

	e, err := h.wl.Get(r.Context(), email)
	if err != nil || e == nil {
		WriteJSON(w, http.StatusOK, map[string]any{"found": false})
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"found": true, "status": e.Status})
}

// List GET /api/v1/waitlist (admin only)
func (h *WaitlistHandler) List(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	status := r.URL.Query().Get("status")
	var st *string
	if status != "" {
		st = &status
	}
	items, err := h.wl.List(r.Context(), st)
	if err != nil {
		if h.logger != nil {
			h.logger.Printf("waitlist list error: %v", err)
		}
		WriteInternalError(w, "failed to list waitlist")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// Approve POST /api/v1/waitlist/approve { email }
func (h *WaitlistHandler) Approve(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	var req waitlistJoinReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" {
		WriteBadRequest(w, "INVALID_JSON", "email is required")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	e := &models.WaitlistEntry{Email: email, Status: "approved"}
	if err := h.wl.Upsert(r.Context(), e); err != nil {
		WriteInternalError(w, "failed to approve")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *WaitlistHandler) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "auth required")
		return false
	}
	if h.users == nil {
		WriteForbidden(w, "no users repo")
		return false
	}
	u, err := h.users.GetByIDAndTenant(r.Context(), auth.UID, auth.TenantID)
	if err != nil || u == nil || u.Role == nil || strings.ToLower(*u.Role) != "admin" {
		WriteForbidden(w, "admin required")
		return false
	}
	return true
}
