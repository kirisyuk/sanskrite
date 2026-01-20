package horizon

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// EnforcementHandlers handles HTTP requests for policy enforcement jobs.
type EnforcementHandlers struct {
	enforcementJobsRepo    *repo.EnforcementJobsRepository
	policyCheckResultsRepo *repo.PolicyCheckResultsRepository
	enforcementExecutor    *policy.EnforcementExecutor
}

// NewEnforcementHandlers creates a new EnforcementHandlers.
func NewEnforcementHandlers(
	enforcementJobsRepo *repo.EnforcementJobsRepository,
	policyCheckResultsRepo *repo.PolicyCheckResultsRepository,
	enforcementExecutor *policy.EnforcementExecutor,
) *EnforcementHandlers {
	return &EnforcementHandlers{
		enforcementJobsRepo:    enforcementJobsRepo,
		policyCheckResultsRepo: policyCheckResultsRepo,
		enforcementExecutor:    enforcementExecutor,
	}
}

// RegisterRoutes registers enforcement job routes.
func (h *EnforcementHandlers) RegisterRoutes(mux *http.ServeMux, authMiddleware func(http.Handler) http.Handler) {
	// Enforcement jobs
	mux.Handle("/api/horizon/enforcement/jobs", authMiddleware(http.HandlerFunc(h.HandleEnforcementJobsRoot)))
	mux.Handle("/api/horizon/enforcement/jobs/", authMiddleware(http.HandlerFunc(h.HandleEnforcementJobRoutes)))

	// Batch execution
	mux.Handle("/api/horizon/enforcement/execute-batch", authMiddleware(http.HandlerFunc(h.ExecuteBatch)))

	// Policy check results
	mux.Handle("/api/horizon/enforcement/check-results", authMiddleware(http.HandlerFunc(h.HandleCheckResultsRoot)))
	mux.Handle("/api/horizon/enforcement/check-results/", authMiddleware(http.HandlerFunc(h.HandleCheckResultRoutes)))
}

// HandleEnforcementJobsRoot handles /api/horizon/enforcement/jobs
// GET - List pending jobs for a tenant
func (h *EnforcementHandlers) HandleEnforcementJobsRoot(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.ListEnforcementJobs(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleEnforcementJobRoutes handles /api/horizon/enforcement/jobs/{id} and subpaths
func (h *EnforcementHandlers) HandleEnforcementJobRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/horizon/enforcement/jobs/")
	if path == "" {
		http.Error(w, "Job ID required", http.StatusBadRequest)
		return
	}

	pathParts := strings.Split(path, "/")
	jobID := pathParts[0]

	// Check for subpaths
	if len(pathParts) > 1 {
		switch pathParts[1] {
		case "execute":
			if r.Method == http.MethodPost {
				h.ExecuteEnforcementJob(w, r, jobID)
				return
			}
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		case "skip":
			if r.Method == http.MethodPost {
				h.SkipEnforcementJob(w, r, jobID)
				return
			}
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		default:
			http.Error(w, "Unknown subpath", http.StatusNotFound)
			return
		}
	}

	switch r.Method {
	case http.MethodGet:
		h.GetEnforcementJob(w, r, jobID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleCheckResultsRoot handles /api/horizon/enforcement/check-results
// GET - List policy check results for a tenant
func (h *EnforcementHandlers) HandleCheckResultsRoot(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.ListCheckResults(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleCheckResultRoutes handles /api/horizon/enforcement/check-results/{jobId}
func (h *EnforcementHandlers) HandleCheckResultRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/horizon/enforcement/check-results/")
	if path == "" {
		http.Error(w, "Job ID required", http.StatusBadRequest)
		return
	}

	jobID := strings.Split(path, "/")[0]

	switch r.Method {
	case http.MethodGet:
		h.GetCheckResult(w, r, jobID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// =============================================================================
// ENFORCEMENT JOBS HANDLERS
// =============================================================================

// ListEnforcementJobs lists enforcement jobs by status or parent job.
// GET /api/horizon/enforcement/jobs?tenantId={tenantId}&status={status}&parentJobId={parentJobId}&limit={limit}
func (h *EnforcementHandlers) ListEnforcementJobs(w http.ResponseWriter, r *http.Request) {
	tenantID := r.URL.Query().Get("tenantId")
	status := r.URL.Query().Get("status")
	parentJobID := r.URL.Query().Get("parentJobId")
	limitStr := r.URL.Query().Get("limit")

	limit := 100
	if limitStr != "" {
		if _, err := json.Number(limitStr).Int64(); err == nil {
			limit = int(mustParseInt(limitStr, 100))
		}
	}

	ctx := r.Context()
	var jobs []*policy.PolicyEnforcementJob
	var err error

	if parentJobID != "" {
		// List by parent job ID
		jobs, err = h.enforcementJobsRepo.ListByParentJob(ctx, parentJobID, limit)
	} else if status != "" {
		// List by status
		jobStatus := policy.EnforcementJobStatus(status)
		jobs, err = h.enforcementJobsRepo.ListByStatus(ctx, tenantID, jobStatus, limit)
	} else if tenantID != "" {
		// List pending jobs by default
		jobs, err = h.enforcementJobsRepo.ListPending(ctx, tenantID, limit)
	} else {
		http.Error(w, "tenantId or parentJobId is required", http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, "Failed to list enforcement jobs: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"jobs":  jobs,
		"count": len(jobs),
	})
}

// GetEnforcementJob gets a single enforcement job by ID.
// GET /api/horizon/enforcement/jobs/{id}
func (h *EnforcementHandlers) GetEnforcementJob(w http.ResponseWriter, r *http.Request, jobID string) {
	ctx := r.Context()

	job, err := h.enforcementJobsRepo.Get(ctx, jobID)
	if err != nil {
		if err == repo.ErrEnforcementJobNotFound {
			http.Error(w, "Enforcement job not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to get enforcement job: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, job)
}

// ExecuteEnforcementJob executes a single enforcement job.
// POST /api/horizon/enforcement/jobs/{id}/execute
func (h *EnforcementHandlers) ExecuteEnforcementJob(w http.ResponseWriter, r *http.Request, jobID string) {
	ctx := r.Context()

	job, err := h.enforcementJobsRepo.Get(ctx, jobID)
	if err != nil {
		if err == repo.ErrEnforcementJobNotFound {
			http.Error(w, "Enforcement job not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to get enforcement job: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Check if job is already processed
	if job.Status != policy.EnforcementStatusPending {
		http.Error(w, "Job is not pending", http.StatusBadRequest)
		return
	}

	// Execute the job
	result, err := h.enforcementExecutor.ExecuteJob(ctx, job)
	if err != nil {
		http.Error(w, "Failed to execute enforcement job: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Update job status
	var status policy.EnforcementJobStatus
	if result.Success {
		status = policy.EnforcementStatusCompleted
	} else {
		status = policy.EnforcementStatusFailed
	}

	if err := h.enforcementJobsRepo.UpdateStatus(ctx, jobID, status, result); err != nil {
		http.Error(w, "Failed to update job status: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Re-fetch job to get updated status
	job, _ = h.enforcementJobsRepo.Get(ctx, jobID)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"job":    job,
		"result": result,
	})
}

// SkipEnforcementJob skips an enforcement job (marks as skipped without executing).
// POST /api/horizon/enforcement/jobs/{id}/skip
func (h *EnforcementHandlers) SkipEnforcementJob(w http.ResponseWriter, r *http.Request, jobID string) {
	ctx := r.Context()

	job, err := h.enforcementJobsRepo.Get(ctx, jobID)
	if err != nil {
		if err == repo.ErrEnforcementJobNotFound {
			http.Error(w, "Enforcement job not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to get enforcement job: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Check if job is already processed
	if job.Status != policy.EnforcementStatusPending {
		http.Error(w, "Job is not pending", http.StatusBadRequest)
		return
	}

	// Parse optional reason from request body
	var req struct {
		Reason string `json:"reason"`
	}
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&req)
	}
	if req.Reason == "" {
		req.Reason = "Skipped by user"
	}

	// Update job status to skipped
	result := &policy.EnforcementResult{
		Success: false,
		Action:  "skipped",
		Details: req.Reason,
	}

	if err := h.enforcementJobsRepo.UpdateStatus(ctx, jobID, policy.EnforcementStatusSkipped, result); err != nil {
		http.Error(w, "Failed to update job status: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Re-fetch job
	job, _ = h.enforcementJobsRepo.Get(ctx, jobID)

	writeJSON(w, http.StatusOK, job)
}

// ExecuteBatch executes all pending enforcement jobs for a parent job.
// POST /api/horizon/enforcement/execute-batch?parentJobId={parentJobId}
func (h *EnforcementHandlers) ExecuteBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	parentJobID := r.URL.Query().Get("parentJobId")
	if parentJobID == "" {
		// Try to read from request body
		var req struct {
			ParentJobID string `json:"parentJobId"`
		}
		if r.Body != nil {
			json.NewDecoder(r.Body).Decode(&req)
			parentJobID = req.ParentJobID
		}
	}

	if parentJobID == "" {
		http.Error(w, "parentJobId is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	result, err := h.enforcementExecutor.ExecuteBatch(ctx, parentJobID)
	if err != nil {
		http.Error(w, "Failed to execute batch: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// =============================================================================
// POLICY CHECK RESULTS HANDLERS
// =============================================================================

// ListCheckResults lists policy check results for a tenant.
// GET /api/horizon/enforcement/check-results?tenantId={tenantId}&limit={limit}
func (h *EnforcementHandlers) ListCheckResults(w http.ResponseWriter, r *http.Request) {
	tenantID := r.URL.Query().Get("tenantId")
	if tenantID == "" {
		http.Error(w, "tenantId is required", http.StatusBadRequest)
		return
	}

	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if _, err := json.Number(limitStr).Int64(); err == nil {
			limit = int(mustParseInt(limitStr, 50))
		}
	}

	ctx := r.Context()
	results, err := h.policyCheckResultsRepo.ListByTenant(ctx, tenantID, limit)
	if err != nil {
		http.Error(w, "Failed to list check results: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"results": results,
		"count":   len(results),
	})
}

// GetCheckResult gets a policy check result by job ID.
// GET /api/horizon/enforcement/check-results/{jobId}
func (h *EnforcementHandlers) GetCheckResult(w http.ResponseWriter, r *http.Request, jobID string) {
	ctx := r.Context()

	result, err := h.policyCheckResultsRepo.Get(ctx, jobID)
	if err != nil {
		http.Error(w, "Failed to get check result: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if result == nil {
		http.Error(w, "Check result not found", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// =============================================================================
// HELPERS
// =============================================================================

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func mustParseInt(s string, defaultVal int) int {
	var result int
	_, err := json.Number(s).Int64()
	if err != nil {
		return defaultVal
	}
	result = defaultVal
	if n, err := json.Number(s).Int64(); err == nil {
		result = int(n)
	}
	return result
}
