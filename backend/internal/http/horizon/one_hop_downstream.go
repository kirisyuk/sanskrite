package horizon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonsupplychain "github.com/igorkirisyuk/sanskrite/internal/horizon/supplychain"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
	agentmodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

type oneHopDownstreamRequest struct {
	AnchorRef             string `json:"anchorRef,omitempty"`
	AnchorType            string `json:"anchorType,omitempty"`
	MaxBranchesPerProduct int    `json:"maxBranchesPerProduct,omitempty"`
	IncludeInputsContext  *bool  `json:"includeInputsContext,omitempty"`
	AllowOrgFallback      *bool  `json:"allowOrgFallback,omitempty"`
	AllowLLMFallback      *bool  `json:"allowLLMFallback,omitempty"`
}

type oneHopDownstreamResponse struct {
	JobID      string `json:"jobId"`
	Status     string `json:"status"`
	Scope      string `json:"scope"`
	AnchorRef  string `json:"anchorRef"`
	AnchorType string `json:"anchorType"`
}

func (h *HorizonHandlers) StartLensOneHopDownstream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.oneHopDownstreamService == nil || h.scoutingJobsRepo == nil {
		http.Error(w, "One-hop downstream service unavailable", http.StatusServiceUnavailable)
		return
	}
	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	lensID := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/horizon/lenses/"))
	if parts := strings.Split(lensID, "/"); len(parts) > 0 {
		lensID = strings.TrimSpace(parts[0])
	}
	if lensID == "" {
		http.Error(w, "lens id is required", http.StatusBadRequest)
		return
	}
	lens, err := h.service.Lens().GetLens(r.Context(), lensID)
	if err != nil || lens == nil {
		http.Error(w, "lens not found", http.StatusNotFound)
		return
	}
	if strings.TrimSpace(lens.TenantID) != strings.TrimSpace(authInfo.TenantID) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	var req oneHopDownstreamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}
	opts := requestToOptions(req)
	if strings.TrimSpace(opts.AnchorRef) == "" {
		http.Error(w, "anchorRef is required", http.StatusBadRequest)
		return
	}
	jobID, err := h.createOneHopJob(r.Context(), authInfo.UID, authInfo.TenantID, lens.ProjectID, lensID, opts)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create one-hop downstream job: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(oneHopDownstreamResponse{
		JobID:      jobID,
		Status:     "pending",
		Scope:      "one_hop_downstream",
		AnchorRef:  strings.TrimSpace(opts.AnchorRef),
		AnchorType: strings.TrimSpace(opts.AnchorType),
	})
}

func (h *HorizonHandlers) StartFragmentOneHopDownstream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.oneHopDownstreamService == nil || h.scoutingJobsRepo == nil {
		http.Error(w, "One-hop downstream service unavailable", http.StatusServiceUnavailable)
		return
	}
	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	fragmentID := extractIDFromPath(r, "/api/horizon/fragments/")
	fragmentID = strings.TrimSpace(strings.TrimSuffix(fragmentID, "/expand-one-hop-downstream"))
	if fragmentID == "" {
		http.Error(w, "Fragment ID required", http.StatusBadRequest)
		return
	}
	fragment, err := h.service.Fragments().Get(r.Context(), fragmentID)
	if err != nil || fragment == nil {
		http.Error(w, "Fragment not found", http.StatusNotFound)
		return
	}
	var req oneHopDownstreamRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	opts := requestToOptions(req)
	opts.AnchorRef = strings.TrimSpace(fragment.CanonicalPath)
	opts.AnchorType = string(fragment.Type)
	jobID, err := h.createOneHopJob(r.Context(), authInfo.UID, authInfo.TenantID, "", "", opts)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create one-hop downstream job: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(oneHopDownstreamResponse{
		JobID:      jobID,
		Status:     "pending",
		Scope:      "one_hop_downstream",
		AnchorRef:  strings.TrimSpace(opts.AnchorRef),
		AnchorType: strings.TrimSpace(opts.AnchorType),
	})
}

func (h *HorizonHandlers) createOneHopJob(
	ctx context.Context,
	userID string,
	tenantID string,
	projectID string,
	lensID string,
	opts horizonsupplychain.OneHopDownstreamOptions,
) (string, error) {
	opts = horizonsupplychain.NormalizeOptions(opts)
	job := &horizonmodels.ScoutingJob{
		AgentJob: agentmodels.AgentJob{
			TenantID:  strings.TrimSpace(tenantID),
			ProjectID: strings.TrimSpace(projectID),
			UserID:    strings.TrimSpace(userID),
			Status:    agentmodels.AgentJobStatusPending,
			AgentID:   "scout",
			Title:     "One-Hop Downstream Expansion",
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
			AutoMode:  true,
			PlanMode:  agentmodels.PlanModeAuto,
			Task: map[string]interface{}{
				"useCase":   horizonsupplychain.JobUseCaseOneHopDownstream,
				"anchorRef": opts.AnchorRef,
			},
			State: horizonsupplychain.DefaultJobState(opts, lensID, projectID),
		},
		LensID:              strings.TrimSpace(lensID),
		TargetCanonicalPath: strings.TrimSpace(opts.AnchorRef),
		ScoutingStrategy:    string(horizonmodels.ScoutingStrategySnapshot),
		CurrentState:        horizonmodels.ScoutingJobStatePending,
	}
	jobID, err := h.scoutingJobsRepo.Create(ctx, job)
	if err != nil {
		return "", err
	}
	ready, _ := h.service.ScoutExecutionRuntime()
	if !ready {
		go func(jobCopy *horizonmodels.ScoutingJob, id string) {
			bg := context.Background()
			jobCopy.ID = id
			_ = h.oneHopDownstreamService.ExecuteJob(bg, jobCopy)
		}(job, jobID)
	}
	return jobID, nil
}

func requestToOptions(req oneHopDownstreamRequest) horizonsupplychain.OneHopDownstreamOptions {
	opts := horizonsupplychain.OneHopDownstreamOptions{
		AnchorRef:             strings.TrimSpace(req.AnchorRef),
		AnchorType:            strings.TrimSpace(req.AnchorType),
		MaxBranchesPerProduct: req.MaxBranchesPerProduct,
		IncludeInputsContext:  true,
		AllowOrgFallback:      true,
		AllowLLMFallback:      true,
	}
	if req.IncludeInputsContext != nil {
		opts.IncludeInputsContext = *req.IncludeInputsContext
	}
	if req.AllowOrgFallback != nil {
		opts.AllowOrgFallback = *req.AllowOrgFallback
	}
	if req.AllowLLMFallback != nil {
		opts.AllowLLMFallback = *req.AllowLLMFallback
	}
	return horizonsupplychain.NormalizeOptions(opts)
}
