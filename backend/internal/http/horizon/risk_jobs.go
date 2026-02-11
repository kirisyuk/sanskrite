package horizon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
)

func (h *HorizonHandlers) HandleRiskJobsRoot(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.CreateRiskJob(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *HorizonHandlers) HandleRiskJobRoutes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.GetRiskJob(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

type createRiskJobRequest struct {
	ProjectID           string `json:"projectId"`
	LensID              string `json:"lensId,omitempty"`
	TargetCanonicalPath string `json:"targetCanonicalPath"`
	Polarity            string `json:"polarity,omitempty"`
	TriggerRef          string `json:"triggerRef,omitempty"`
	TriggerType         string `json:"triggerType,omitempty"`
	DedupKey            string `json:"dedupKey,omitempty"`
}

func (h *HorizonHandlers) CreateRiskJob(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.riskJobsRepo == nil {
		http.Error(w, "risk runtime not configured", http.StatusServiceUnavailable)
		return
	}
	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	var req createRiskJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}

	tenantID := strings.TrimSpace(authInfo.TenantID)
	projectID := strings.TrimSpace(req.ProjectID)
	targetPath := strings.TrimSpace(req.TargetCanonicalPath)
	if tenantID == "" {
		http.Error(w, "tenantId is required", http.StatusBadRequest)
		return
	}
	if projectID == "" {
		http.Error(w, "projectId is required", http.StatusBadRequest)
		return
	}
	if targetPath == "" {
		http.Error(w, "targetCanonicalPath is required", http.StatusBadRequest)
		return
	}

	polarity := strings.ToLower(strings.TrimSpace(req.Polarity))
	if polarity == "" {
		polarity = "risk"
	}
	if polarity != "risk" && polarity != "opportunity" {
		http.Error(w, "polarity must be risk|opportunity", http.StatusBadRequest)
		return
	}

	triggerType := strings.ToLower(strings.TrimSpace(req.TriggerType))
	if triggerType == "" {
		triggerType = "manual"
	}
	triggerRef := strings.TrimSpace(req.TriggerRef)
	if triggerRef == "" {
		triggerRef = fmt.Sprintf("manual:%d", time.Now().UTC().UnixNano())
	}
	dedupKey := strings.TrimSpace(req.DedupKey)
	if dedupKey == "" {
		dedupKey = strings.Join([]string{
			"risk_job_dedup",
			tenantID,
			projectID,
			strings.TrimSpace(req.LensID),
			targetPath,
			triggerRef,
			polarity,
		}, "|")
	}

	job := &horizonmodels.RiskJob{
		TenantID:            tenantID,
		ProjectID:           projectID,
		LensID:              strings.TrimSpace(req.LensID),
		TriggerType:         triggerType,
		TriggerRef:          triggerRef,
		DedupKey:            dedupKey,
		TargetCanonicalPath: targetPath,
		Polarity:            polarity,
		CurrentStage:        horizonmodels.RiskStageIdentify,
		CurrentState:        horizonmodels.RiskJobStatePending,
		State: map[string]any{
			"triggerOrigin": "api_manual",
		},
	}

	id, created, err := h.riskJobsRepo.Create(r.Context(), job)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	res := map[string]any{
		"id":         id,
		"created":    created,
		"state":      string(job.CurrentState),
		"stage":      string(job.CurrentStage),
		"polarity":   polarity,
		"targetPath": targetPath,
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(res); err != nil {
		http.Error(w, fmt.Sprintf("failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}

func (h *HorizonHandlers) GetRiskJob(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.riskJobsRepo == nil {
		http.Error(w, "risk runtime not configured", http.StatusServiceUnavailable)
		return
	}
	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	jobID := extractIDFromPath(r, "/api/horizon/risk/jobs/")
	if jobID == "" {
		http.Error(w, "job id required", http.StatusBadRequest)
		return
	}
	job, err := h.riskJobsRepo.Get(r.Context(), jobID)
	if err != nil || job == nil {
		http.Error(w, "risk job not found", http.StatusNotFound)
		return
	}
	if strings.TrimSpace(job.TenantID) != strings.TrimSpace(authInfo.TenantID) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(job); err != nil {
		http.Error(w, fmt.Sprintf("failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}
