package http

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/services"
)

// UsersHandler handles user-related HTTP requests
type UsersHandler struct {
	repo               *repo.UsersRepository
	subscriptionService *services.SubscriptionService
	logger             *log.Logger
}

// NewUsersHandler creates a new UsersHandler
func NewUsersHandler(repo *repo.UsersRepository, logger *log.Logger) *UsersHandler {
	return &UsersHandler{
		repo:   repo,
		logger: logger,
	}
}

// WithSubscriptionService sets the subscription service (optional, for auto-creating subscriptions)
func (h *UsersHandler) WithSubscriptionService(service *services.SubscriptionService) *UsersHandler {
	h.subscriptionService = service
	return h
}

// CreateUserRequest represents the request body for creating a user
type CreateUserRequest struct {
	FirebaseUID       string  `json:"firebaseUid"`
	Email             string  `json:"email"`
	AccountType       string  `json:"accountType"` // "personal" or "corporate"
	DisplayName       *string `json:"displayName,omitempty"`
	PreferredLanguage *string `json:"preferredLanguage,omitempty"`
}

// CreateUserResponse represents the response for user creation
type CreateUserResponse struct {
	User    *models.User `json:"user"`
	Success bool         `json:"success"`
	Message string       `json:"message"`
}

// ComputeTenantID calculates tenantId based on email and account type
func ComputeTenantID(email, accountType string) string {
	if accountType == "corporate" {
		parts := strings.Split(email, "@")
		if len(parts) == 2 {
			return parts[1] // domain
		}
	}
	return email // personal account: tenantId = email
}

// CreateUser handles POST /api/v1/users
func (h *UsersHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}

	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Failed to parse request body")
		return
	}

	// Validate required fields
	if req.FirebaseUID == "" || req.Email == "" || req.AccountType == "" {
		WriteBadRequest(w, "VALIDATION_ERROR", "firebaseUid, email, and accountType are required")
		return
	}

	if req.AccountType != "personal" && req.AccountType != "corporate" {
		WriteBadRequest(w, "VALIDATION_ERROR", "accountType must be 'personal' or 'corporate'")
		return
	}

	// Verify that the authenticated user is creating their own account
	if authInfo.UID != req.FirebaseUID && !authInfo.IsSystem {
		WriteForbidden(w, "You can only create your own user account")
		return
	}

	// Compute tenantId
	tenantID := ComputeTenantID(req.Email, req.AccountType)

	// Check if user already exists
	existingUser, err := h.repo.GetByIDAndTenant(r.Context(), req.FirebaseUID, tenantID)
	if err == nil && existingUser != nil {
		h.logger.Printf("User already exists: %s in tenant %s", req.FirebaseUID, tenantID)
		WriteConflict(w, "USER_ALREADY_EXISTS", "User with this Firebase UID already exists in this tenant", map[string]string{
			"firebaseUid": req.FirebaseUID,
		})
		return
	}

	// Enforce waitlist: user must be approved
	// Best-effort: if waitlist repo accessible from context
	if uraw := r.Context().Value(waitlistRepoKey{}); uraw != nil {
		if wl, ok := uraw.(*repo.WaitlistRepository); ok && wl != nil {
			if e, err := wl.Get(r.Context(), strings.ToLower(req.Email)); err == nil && e != nil {
				if e.Status != "approved" {
					WriteForbidden(w, "User not approved for beta")
					return
				}
			} else {
				WriteForbidden(w, "User not found in waitlist")
				return
			}
		}
	}

	// Create user
	now := time.Now().UTC()
	role := "user"
	status := "active"
	schemaVersion := "1.0"

	// Default preferredLanguage to EN if not provided or invalid length
	pl := "EN"
	if req.PreferredLanguage != nil {
		v := strings.ToUpper(strings.TrimSpace(*req.PreferredLanguage))
		if len(v) == 2 {
			pl = v
		}
	}

	user := &models.User{
		ID:                req.FirebaseUID,
		TenantID:          tenantID,
		Email:             req.Email,
		DisplayName:       req.DisplayName,
		AuthType:          &req.AccountType,
		Role:              &role,
		Status:            &status,
		CreatedAt:         now,
		UpdatedAt:         now,
		SchemaVersion:     schemaVersion,
		PreferredLanguage: &pl,
	}

	// Save to Firestore
	if err := h.repo.Upsert(r.Context(), user); err != nil {
		h.logger.Printf("Failed to save user %s: %v", user.ID, err)
		WriteInternalError(w, "Failed to create user")
		return
	}

	h.logger.Printf("Successfully created user %s in tenant %s", user.ID, user.TenantID)

	// Create free subscription for new user (async, best-effort)
	if h.subscriptionService != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if _, err := h.subscriptionService.GetOrCreateSubscription(ctx, user.TenantID, user.ID); err != nil {
				h.logger.Printf("Failed to create subscription for user %s: %v", user.ID, err)
			}
		}()
	}

	response := CreateUserResponse{
		User:    user,
		Success: true,
		Message: "User created successfully",
	}

	WriteJSON(w, http.StatusCreated, response)
}

// GetUser handles GET /api/v1/users/{userId}
func (h *UsersHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}

	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	// Extract userId from path
	// Expecting /api/v1/users/{userId}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 4 {
		WriteBadRequest(w, "INVALID_PATH", "User ID is required")
		return
	}
	userID := pathParts[3]

	// Check access: user can only get their own data, or admin can get any
	// For now, simplified: user can only get their own data
	if authInfo.UID != userID && !authInfo.IsSystem {
		WriteForbidden(w, "You do not have permission to access this resource")
		return
	}

	// Fetch user
	user, err := h.repo.GetByIDAndTenant(r.Context(), userID, authInfo.TenantID)
	if err != nil {
		h.logger.Printf("Failed to fetch user %s: %v", userID, err)
		WriteNotFound(w, "User not found")
		return
	}

	WriteJSON(w, http.StatusOK, user)
}

// UpdateUser handles PUT /api/v1/users/{userId}
func (h *UsersHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only PUT is allowed", nil)
		return
	}

	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	// Extract userId from path
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 4 {
		WriteBadRequest(w, "INVALID_PATH", "User ID is required")
		return
	}
	userID := pathParts[3]

	// Check access
	if authInfo.UID != userID && !authInfo.IsSystem {
		WriteForbidden(w, "You do not have permission to update this resource")
		return
	}

	// Fetch existing user
	userToUpdate, err := h.repo.GetByIDAndTenant(r.Context(), userID, authInfo.TenantID)
	if err != nil {
		h.logger.Printf("User not found: %s", userID)
		WriteNotFound(w, "User not found")
		return
	}

	// Parse update request
	var updates models.User
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Failed to parse request body")
		return
	}

	// Update allowed fields
	if updates.DisplayName != nil {
		userToUpdate.DisplayName = updates.DisplayName
	}
	if updates.CompanyRef != nil {
		userToUpdate.CompanyRef = updates.CompanyRef
	}
	if updates.JobTitle != nil {
		userToUpdate.JobTitle = updates.JobTitle
	}
	if updates.Department != nil {
		userToUpdate.Department = updates.Department
	}
	if updates.DefaultProjectID != nil {
		userToUpdate.DefaultProjectID = updates.DefaultProjectID
	}
	if updates.ProjectRoles != nil {
		userToUpdate.ProjectRoles = updates.ProjectRoles
	}
	if updates.AssignedGoalIDs != nil {
		userToUpdate.AssignedGoalIDs = updates.AssignedGoalIDs
	}
	if updates.AssignedInitiativeIDs != nil {
		userToUpdate.AssignedInitiativeIDs = updates.AssignedInitiativeIDs
	}
	if updates.Metadata != nil {
		userToUpdate.Metadata = updates.Metadata
	}
	if updates.PreferredLanguage != nil {
		v := strings.ToUpper(strings.TrimSpace(*updates.PreferredLanguage))
		if len(v) == 2 {
			userToUpdate.PreferredLanguage = &v
		}
	}
	if updates.CountryOfResidence != nil {
		v := strings.ToUpper(strings.TrimSpace(*updates.CountryOfResidence))
		if len(v) == 2 {
			userToUpdate.CountryOfResidence = &v
		}
	}
	// Parent-child link and demographics
	if updates.ParentUserID != nil {
		userToUpdate.ParentUserID = updates.ParentUserID
	}
	if updates.Gender != nil {
		userToUpdate.Gender = updates.Gender
	}
	if updates.Age != nil {
		userToUpdate.Age = updates.Age
	}
	if updates.RecommendationSettings != nil {
		userToUpdate.RecommendationSettings = updates.RecommendationSettings
	}

	// Admin can update role and status (for now, simplified - no admin check)
	// if authInfo.IsSystem {
	// 	if updates.Role != nil {
	// 		userToUpdate.Role = updates.Role
	// 	}
	// 	if updates.Status != nil {
	// 		userToUpdate.Status = updates.Status
	// 	}
	// }

	userToUpdate.UpdatedAt = time.Now().UTC()

	// Save
	if err := h.repo.Upsert(r.Context(), userToUpdate); err != nil {
		h.logger.Printf("Failed to update user %s: %v", userID, err)
		WriteInternalError(w, "Failed to update user")
		return
	}

	h.logger.Printf("Successfully updated user %s", userID)
	WriteJSON(w, http.StatusOK, userToUpdate)
}
