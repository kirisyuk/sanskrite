package horizon

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
)

func (h *HorizonHandlers) HandleRecommendationsRoot(w http.ResponseWriter, r *http.Request) {
	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	svc := h.service.StrategicRecommendations()
	if svc == nil {
		http.Error(w, "strategic recommendations unavailable", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		limit, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
		filter := horizonrepo.StrategicRecommendationsListFilter{
			Status:    strings.TrimSpace(r.URL.Query().Get("status")),
			ProjectID: strings.TrimSpace(r.URL.Query().Get("projectId")),
			LensID:    strings.TrimSpace(r.URL.Query().Get("lensId")),
			Limit:     limit,
		}
		items, scope, err := h.listRecommendationsForRequest(r, authInfo, filter)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"items": items, "scope": scope})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *HorizonHandlers) GetRecommendationsCount(w http.ResponseWriter, r *http.Request) {
	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	svc := h.service.StrategicRecommendations()
	if svc == nil {
		http.Error(w, "strategic recommendations unavailable", http.StatusServiceUnavailable)
		return
	}
	count, tenantCount, globalCount, scope, err := h.countRecommendationsForRequest(r, authInfo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"count":       count,
		"tenantCount": tenantCount,
		"globalCount": globalCount,
		"scope":       scope,
	})
}

func (h *HorizonHandlers) HandleRecommendationRoutes(w http.ResponseWriter, r *http.Request) {
	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	svc := h.service.StrategicRecommendations()
	if svc == nil {
		http.Error(w, "strategic recommendations unavailable", http.StatusServiceUnavailable)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/horizon/recommendations/")
	path = strings.Trim(path, "/")
	if path == "" {
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(path, "/")
	id := strings.TrimSpace(parts[0])
	if id == "" {
		http.NotFound(w, r)
		return
	}
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		rec, err := svc.Get(r.Context(), id)
		if err != nil {
			status := http.StatusInternalServerError
			if err == horizonrepo.ErrStrategicRecommendationNotFound {
				status = http.StatusNotFound
			}
			http.Error(w, err.Error(), status)
			return
		}
		if !h.canAccessRecommendation(r, authInfo, rec) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rec)
		return
	}
	action := strings.TrimSpace(parts[1])
	switch action {
	case "approve":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		existing, err := svc.Get(r.Context(), id)
		if err != nil {
			status := http.StatusInternalServerError
			if err == horizonrepo.ErrStrategicRecommendationNotFound {
				status = http.StatusNotFound
			}
			http.Error(w, err.Error(), status)
			return
		}
		if !h.canAccessRecommendation(r, authInfo, existing) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		rec, err := svc.Approve(r.Context(), authInfo.TenantID, authInfo.UID, id)
		if err != nil {
			status := http.StatusInternalServerError
			if err == horizonrepo.ErrStrategicRecommendationNotFound {
				status = http.StatusNotFound
			}
			http.Error(w, err.Error(), status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rec)
	case "dismiss":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		existing, err := svc.Get(r.Context(), id)
		if err != nil {
			status := http.StatusInternalServerError
			if err == horizonrepo.ErrStrategicRecommendationNotFound {
				status = http.StatusNotFound
			}
			http.Error(w, err.Error(), status)
			return
		}
		if !h.canAccessRecommendation(r, authInfo, existing) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		rec, err := svc.Dismiss(r.Context(), authInfo.TenantID, id)
		if err != nil {
			status := http.StatusInternalServerError
			if err == horizonrepo.ErrStrategicRecommendationNotFound {
				status = http.StatusNotFound
			}
			http.Error(w, err.Error(), status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rec)
	default:
		http.NotFound(w, r)
	}
}

func (h *HorizonHandlers) canAccessRecommendation(r *http.Request, authInfo *httputil.AuthInfo, rec *horizonmodels.StrategicRecommendation) bool {
	if authInfo == nil || rec == nil {
		return false
	}
	authTenant := strings.TrimSpace(authInfo.TenantID)
	recTenant := strings.TrimSpace(rec.TenantID)
	if recTenant == authTenant {
		return true
	}
	return h.isAdminRequest(r, authInfo) && recTenant == "global"
}

func (h *HorizonHandlers) listRecommendationsForRequest(r *http.Request, authInfo *httputil.AuthInfo, filter horizonrepo.StrategicRecommendationsListFilter) ([]*horizonmodels.StrategicRecommendation, string, error) {
	svc := h.service.StrategicRecommendations()
	scope := h.resolveRecommendationScope(r, authInfo)
	if scope == "all" {
		tenantItems, err := svc.List(r.Context(), authInfo.TenantID, filter)
		if err != nil {
			return nil, scope, err
		}
		if strings.TrimSpace(authInfo.TenantID) == "global" {
			return tenantItems, scope, nil
		}
		globalItems, err := svc.List(r.Context(), "global", filter)
		if err != nil {
			return nil, scope, err
		}
		return mergeRecommendations(tenantItems, globalItems), scope, nil
	}
	tenantID := strings.TrimSpace(authInfo.TenantID)
	if scope == "global" {
		tenantID = "global"
	}
	items, err := svc.List(r.Context(), tenantID, filter)
	return items, scope, err
}

func (h *HorizonHandlers) countRecommendationsForRequest(r *http.Request, authInfo *httputil.AuthInfo) (count, tenantCount, globalCount int, scope string, err error) {
	svc := h.service.StrategicRecommendations()
	scope = h.resolveRecommendationScope(r, authInfo)
	tenantCount, err = svc.CountPending(r.Context(), authInfo.TenantID)
	if err != nil {
		return 0, 0, 0, scope, err
	}
	if strings.TrimSpace(authInfo.TenantID) == "global" {
		return tenantCount, tenantCount, tenantCount, scope, nil
	}
	if scope == "tenant" {
		return tenantCount, tenantCount, 0, scope, nil
	}
	globalCount, err = svc.CountPending(r.Context(), "global")
	if err != nil {
		return 0, 0, 0, scope, err
	}
	if scope == "global" {
		return globalCount, tenantCount, globalCount, scope, nil
	}
	return tenantCount + globalCount, tenantCount, globalCount, scope, nil
}

func (h *HorizonHandlers) resolveRecommendationScope(r *http.Request, authInfo *httputil.AuthInfo) string {
	scope := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("scope")))
	if !h.isAdminRequest(r, authInfo) {
		return "tenant"
	}
	switch scope {
	case "tenant":
		return "tenant"
	case "global", "all":
		return scope
	default:
		return "all"
	}
}

func (h *HorizonHandlers) isAdminRequest(r *http.Request, authInfo *httputil.AuthInfo) bool {
	if h == nil || h.usersRepo == nil || authInfo == nil {
		return false
	}
	user, err := h.usersRepo.GetByUID(r.Context(), authInfo.UID)
	if err != nil || user == nil || user.Role == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(*user.Role), "admin")
}

func mergeRecommendations(groups ...[]*horizonmodels.StrategicRecommendation) []*horizonmodels.StrategicRecommendation {
	byID := make(map[string]*horizonmodels.StrategicRecommendation)
	for _, group := range groups {
		for _, item := range group {
			if item == nil || strings.TrimSpace(item.ID) == "" {
				continue
			}
			existing, ok := byID[item.ID]
			if !ok || item.UpdatedAt.After(existing.UpdatedAt) {
				byID[item.ID] = item
			}
		}
	}
	out := make([]*horizonmodels.StrategicRecommendation, 0, len(byID))
	for _, item := range byID {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}
