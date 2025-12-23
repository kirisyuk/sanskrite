package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"firebase.google.com/go/v4/auth"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/services"
)

type googleTokenInfo struct {
	Email         string `json:"email"`
	EmailVerified string `json:"email_verified"`
	Sub           string `json:"sub"`
}

func allowGoogleAccessTokenAuth() bool {
	v := strings.TrimSpace(os.Getenv("AUTH_ALLOW_GOOGLE_ACCESS_TOKEN"))
	if v == "" {
		v = strings.TrimSpace(os.Getenv("ALLOW_GOOGLE_ACCESS_TOKEN_AUTH"))
	}
	if v == "" {
		return false
	}
	return !(v == "0" || strings.EqualFold(v, "false"))
}

func tenantOverride() string {
	// Explicit override for dev/testing (to run scenarios under a desired tenant id).
	// Only applied when google access-token fallback is enabled.
	v := strings.TrimSpace(os.Getenv("AUTH_TENANT_OVERRIDE"))
	if v == "" {
		v = strings.TrimSpace(os.Getenv("DEV_TENANT_ID_OVERRIDE"))
	}
	return strings.TrimSpace(v)
}

func verifyGoogleAccessToken(ctx context.Context, accessToken string) (*googleTokenInfo, error) {
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return nil, fmt.Errorf("empty access token")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://oauth2.googleapis.com/tokeninfo?access_token="+accessToken, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("tokeninfo status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var ti googleTokenInfo
	if err := json.Unmarshal(b, &ti); err != nil {
		return nil, err
	}
	ti.Email = strings.TrimSpace(ti.Email)
	ti.Sub = strings.TrimSpace(ti.Sub)
	if ti.Email == "" {
		return nil, fmt.Errorf("tokeninfo missing email")
	}
	return &ti, nil
}

// AuthContextKey is the key for storing auth info in request context
type AuthContextKey struct{}

// AuthInfo contains authenticated user information
type AuthInfo struct {
	UID      string
	Email    string
	TenantID string
	IsSystem bool
}

// AuthMiddleware validates Firebase ID tokens and populates context
func AuthMiddleware(authClient *auth.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")

			// Check for system call with function key
			if r.Header.Get("x-functions-key") != "" {
				authInfo := &AuthInfo{
					UID:      "system",
					Email:    "system",
					TenantID: "system",
					IsSystem: true,
				}
				ctx := context.WithValue(r.Context(), AuthContextKey{}, authInfo)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Check for Bearer token
			if authHeader == "" {
				http.Error(w, "missing authorization header", http.StatusUnauthorized)
				return
			}

			const prefix = "Bearer "
			if !strings.HasPrefix(authHeader, prefix) {
				http.Error(w, "invalid authorization header format", http.StatusUnauthorized)
				return
			}

			idToken := strings.TrimPrefix(authHeader, prefix)

			// Verify Firebase token
			token, err := authClient.VerifyIDToken(r.Context(), idToken)
			if err != nil {
				// Optional dev-only fallback: accept Google OAuth access tokens (e.g., gcloud print-access-token)
				// behind an explicit env flag. Intended for local testing only.
				if allowGoogleAccessTokenAuth() {
					if ti, err2 := verifyGoogleAccessToken(r.Context(), idToken); err2 == nil && ti != nil {
						email := strings.TrimSpace(ti.Email)
						uid := "google:" + strings.TrimSpace(ti.Sub)
						if strings.TrimSpace(ti.Sub) == "" {
							uid = "google:" + email
						}

						// Resolve tenantId via UsersRepository if available in request context.
						tenantID := email
						if to := tenantOverride(); to != "" {
							tenantID = to
						}
						if uraw := r.Context().Value(usersRepoKey{}); uraw != nil {
							if usersRepo, ok := uraw.(*repo.UsersRepository); ok && usersRepo != nil {
								if u, err3 := usersRepo.GetByUID(r.Context(), uid); err3 == nil && u != nil {
									if u.TenantID != "" {
										tenantID = u.TenantID
									}
								}
							}
						}

						authInfo := &AuthInfo{
							UID:      uid,
							Email:    email,
							TenantID: tenantID,
							IsSystem: false,
						}

						ctx := context.WithValue(r.Context(), AuthContextKey{}, authInfo)
						next.ServeHTTP(w, r.WithContext(ctx))
						return
					}
				}

				log.Printf("Auth error: %v", err)
				http.Error(w, "invalid or expired token", http.StatusUnauthorized)
				return
			}

			// Extract claims
			uid := token.UID
			email, _ := token.Claims["email"].(string)

			// Resolve tenantId via UsersRepository if available in request context
			tenantID := email
			if uraw := r.Context().Value(usersRepoKey{}); uraw != nil {
				if usersRepo, ok := uraw.(*repo.UsersRepository); ok && usersRepo != nil {
					if u, err := usersRepo.GetByUID(r.Context(), uid); err == nil && u != nil {
						if u.TenantID != "" {
							tenantID = u.TenantID
						}
					}
				}
			}

			authInfo := &AuthInfo{
				UID:      uid,
				Email:    email,
				TenantID: tenantID,
				IsSystem: false,
			}

			ctx := context.WithValue(r.Context(), AuthContextKey{}, authInfo)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetAuthInfo retrieves auth info from request context
func GetAuthInfo(ctx context.Context) *AuthInfo {
	if auth, ok := ctx.Value(AuthContextKey{}).(*AuthInfo); ok {
		return auth
	}
	return nil
}

// RequireAuth wraps a handler and ensures user is authenticated
func RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authInfo := GetAuthInfo(r.Context())
		if authInfo == nil {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// usersRepoKey is a private context key used to pass UsersRepository into middleware
type usersRepoKey struct{}

// WithUsersRepo attaches UsersRepository to request context (to resolve tenant)
func WithUsersRepo(users *repo.UsersRepository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), usersRepoKey{}, users)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// subscriptionServiceKey is a private context key used to pass SubscriptionService into middleware
type subscriptionServiceKey struct{}

// WithSubscriptionService attaches SubscriptionService to request context
func WithSubscriptionService(service *services.SubscriptionService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), subscriptionServiceKey{}, service)

			// Auto-create Max subscription for admins on first request (async, best-effort)
			// Use r.Context() to get authInfo from AuthMiddleware (which runs before this)
			authInfo := GetAuthInfo(r.Context())
			if authInfo != nil && !authInfo.IsSystem {
				go func() {
					defer func() {
						if r := recover(); r != nil {
							log.Printf("[Auth] PANIC in admin subscription check goroutine (uid=%s, tenant=%s): %v", authInfo.UID, authInfo.TenantID, r)
						}
					}() // Don't let panics crash the server

					// Use background context with timeout for async operation
					bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()

					// Get user from database to check if admin
					// Use r.Context() to get usersRepo from WithUsersRepo middleware (which runs before this)
					if uraw := r.Context().Value(usersRepoKey{}); uraw != nil {
						if usersRepo, ok := uraw.(*repo.UsersRepository); ok && usersRepo != nil {
							user, err := usersRepo.GetByIDAndTenant(bgCtx, authInfo.UID, authInfo.TenantID)
							if err == nil && user != nil && user.Role != nil && strings.ToLower(*user.Role) == "admin" {
								// Ensure admin has Max subscription
								_ = service.EnsureAdminMaxSubscription(bgCtx, authInfo.TenantID, authInfo.UID)
								// Errors are logged inside EnsureAdminMaxSubscription, we don't fail the request
							}
						}
					}
				}()
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireSubscription creates middleware that requires a minimum subscription tier
func RequireSubscription(requiredTier models.SubscriptionTier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authInfo := GetAuthInfo(r.Context())
			if authInfo == nil {
				WriteUnauthorized(w, "Authentication required")
				return
			}

			// System calls bypass subscription checks
			if authInfo.IsSystem {
				next.ServeHTTP(w, r)
				return
			}

			// Get subscription service from context
			serviceRaw := r.Context().Value(subscriptionServiceKey{})
			if serviceRaw == nil {
				WriteInternalError(w, "Subscription service not available")
				return
			}

			service, ok := serviceRaw.(*services.SubscriptionService)
			if !ok {
				WriteInternalError(w, "Invalid subscription service")
				return
			}

			// Check subscription
			_, valid, err := service.CheckSubscription(r.Context(), authInfo.TenantID, authInfo.UID, requiredTier)
			if err != nil {
				WriteInternalError(w, "Failed to check subscription")
				return
			}

			if !valid {
				WriteError(w, http.StatusForbidden, "SUBSCRIPTION_REQUIRED",
					fmt.Sprintf("This feature requires %s subscription or higher", requiredTier), nil)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
