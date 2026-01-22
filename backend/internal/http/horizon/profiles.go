package horizon

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/profile"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// ProfilesHandler handles fragment profile API requests.
type ProfilesHandler struct {
	profilesRepo *repo.FragmentProfilesRepository
	generator    *profile.Generator
	scheduler    *profile.Scheduler
}

// NewProfilesHandler creates a new ProfilesHandler.
func NewProfilesHandler(
	profilesRepo *repo.FragmentProfilesRepository,
	generator *profile.Generator,
	scheduler *profile.Scheduler,
) *ProfilesHandler {
	return &ProfilesHandler{
		profilesRepo: profilesRepo,
		generator:    generator,
		scheduler:    scheduler,
	}
}

// RegisterRoutes registers profile-related routes.
func (h *ProfilesHandler) RegisterRoutes(mux *http.ServeMux, authMiddleware func(http.Handler) http.Handler) {
	// Direct profile access by ID
	mux.Handle("/api/horizon/profiles/", authMiddleware(http.HandlerFunc(h.handleProfileRoutes)))
}

// handleFragmentProfileRoutes handles routes like /api/horizon/fragments/{id}/profile
func (h *ProfilesHandler) handleFragmentProfileRoutes(w http.ResponseWriter, r *http.Request) {
	// Extract fragment ID and sub-path
	path := strings.TrimPrefix(r.URL.Path, "/api/horizon/fragments/")
	parts := strings.SplitN(path, "/", 2)
	
	if len(parts) < 2 || parts[1] != "profile" {
		// Not a profile route, pass through
		http.NotFound(w, r)
		return
	}

	fragmentID := parts[0]
	if fragmentID == "" {
		http.Error(w, "fragment ID is required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getProfileByFragmentID(w, r, fragmentID)
	case http.MethodPost:
		h.refreshProfile(w, r, fragmentID)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleProfileRoutes handles routes like /api/horizon/profiles/{id}
func (h *ProfilesHandler) handleProfileRoutes(w http.ResponseWriter, r *http.Request) {
	profileID := strings.TrimPrefix(r.URL.Path, "/api/horizon/profiles/")
	profileID = strings.TrimSuffix(profileID, "/")
	
	if profileID == "" {
		http.Error(w, "profile ID is required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getProfileByID(w, r, profileID)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// getProfileByFragmentID returns the latest profile for a fragment.
// GET /api/horizon/fragments/{fragmentId}/profile
func (h *ProfilesHandler) getProfileByFragmentID(w http.ResponseWriter, r *http.Request, fragmentID string) {
	ctx := r.Context()

	profile, err := h.profilesRepo.GetByFragmentID(ctx, fragmentID)
	if err != nil {
		if err == repo.ErrFragmentProfileNotFound {
			// Return empty response with 404
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":      "profile not found",
				"fragmentId": fragmentID,
				"hint":       "Use POST to generate a new profile",
			})
			return
		}
		// IMPORTANT: log server-side cause; return JSON for the UI.
		log.Printf("[ProfilesHandler] getProfileByFragmentID failed fragmentId=%s path=%s err=%v", fragmentID, r.URL.Path, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error":      "failed to get profile",
			"fragmentId": fragmentID,
			"details":    fmt.Sprintf("%v", err),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(profile)
}

// deleteProfileByFragmentID deletes all profile versions for a fragment.
// DELETE /api/horizon/fragments/{fragmentId}/profile
func (h *ProfilesHandler) deleteProfileByFragmentID(w http.ResponseWriter, r *http.Request, fragmentID string) {
	ctx := r.Context()

	// List first to report count (best-effort; if listing fails, still attempt delete).
	deletedCount := 0
	if profiles, err := h.profilesRepo.ListByFragmentID(ctx, fragmentID, 1000); err == nil {
		deletedCount = len(profiles)
	}

	if err := h.profilesRepo.DeleteByFragmentID(ctx, fragmentID); err != nil {
		log.Printf("[ProfilesHandler] deleteProfileByFragmentID failed fragmentId=%s path=%s err=%v", fragmentID, r.URL.Path, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error":      "failed to delete profile(s)",
			"fragmentId": fragmentID,
			"details":    fmt.Sprintf("%v", err),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":          true,
		"fragmentId":  fragmentID,
		"deletedCount": deletedCount,
	})
}

// getProfileByID returns a specific profile by ID.
// GET /api/horizon/profiles/{profileId}
func (h *ProfilesHandler) getProfileByID(w http.ResponseWriter, r *http.Request, profileID string) {
	ctx := r.Context()

	profile, err := h.profilesRepo.Get(ctx, profileID)
	if err != nil {
		if err == repo.ErrFragmentProfileNotFound {
			http.Error(w, "profile not found", http.StatusNotFound)
			return
		}
		log.Printf("[ProfilesHandler] getProfileByID failed profileId=%s path=%s err=%v", profileID, r.URL.Path, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error":     "failed to get profile",
			"profileId": profileID,
			"details":   fmt.Sprintf("%v", err),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(profile)
}

// refreshProfile triggers on-demand profile generation.
// POST /api/horizon/fragments/{fragmentId}/profile/refresh
func (h *ProfilesHandler) refreshProfile(w http.ResponseWriter, r *http.Request, fragmentID string) {
	ctx := r.Context()

	if h.scheduler == nil {
		http.Error(w, "profile generation not configured", http.StatusServiceUnavailable)
		return
	}

	// Parse optional options from request body
	var req struct {
		Force bool `json:"force"` // Force regeneration even if fresh profile exists
	}
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&req)
	}

	// Generate profile
	result, err := h.scheduler.RefreshSingleWithOptions(ctx, fragmentID, req.Force)
	if err != nil {
		log.Printf("[ProfilesHandler] refreshProfile failed fragmentId=%s path=%s force=%v err=%v", fragmentID, r.URL.Path, req.Force, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error":      "failed to generate profile",
			"fragmentId": fragmentID,
			"force":      req.Force,
			"details":    fmt.Sprintf("%v", err),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"profileId":   result.Profile.ID,
		"fragmentId":  fragmentID,
		"created":     result.Created,
		"durationMs":  result.DurationMs,
		"capturedAt":  result.Profile.CapturedAt,
		"sectionsCount": len(result.Profile.Sections),
	})
}

// ProfileListResponse represents the response for listing profiles.
type ProfileListResponse struct {
	Profiles []*models.FragmentProfile `json:"profiles"`
	Total    int                       `json:"total"`
}

// listProfileVersions returns all profile versions for a fragment.
// GET /api/horizon/fragments/{fragmentId}/profile/versions
func (h *ProfilesHandler) listProfileVersions(w http.ResponseWriter, r *http.Request, fragmentID string) {
	ctx := r.Context()

	profiles, err := h.profilesRepo.ListByFragmentID(ctx, fragmentID, 10)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to list profiles: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ProfileListResponse{
		Profiles: profiles,
		Total:    len(profiles),
	})
}
