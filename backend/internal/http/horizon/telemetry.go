package horizon

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// HandleTelemetryRoutes dispatches /api/horizon/lenses/{lensId}/telemetry/...
func (h *HorizonHandlers) HandleTelemetryRoutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := r.URL.Path
	lensID := extractIDFromPath(r, "/api/horizon/lenses/")
	if strings.TrimSpace(lensID) == "" {
		http.Error(w, "lensId required in path", http.StatusBadRequest)
		return
	}

	sub := strings.TrimPrefix(path, "/api/horizon/lenses/"+lensID+"/telemetry")
	sub = strings.TrimPrefix(sub, "/")

	switch {
	case sub == "" || sub == "summary":
		h.GetTelemetrySummary(w, r, lensID)
	case sub == "jobs":
		h.ListTelemetryJobs(w, r, lensID)
	case strings.HasPrefix(sub, "jobs/"):
		jobID := strings.TrimPrefix(sub, "jobs/")
		jobID = strings.TrimRight(jobID, "/")
		if jobID == "" {
			http.Error(w, "jobId required in path", http.StatusBadRequest)
			return
		}
		h.GetTelemetryJob(w, r, jobID)
	default:
		http.Error(w, "Unknown telemetry subpath", http.StatusNotFound)
	}
}

// ListTelemetryJobs handles GET /api/horizon/lenses/{lensId}/telemetry/jobs
func (h *HorizonHandlers) ListTelemetryJobs(w http.ResponseWriter, r *http.Request, lensID string) {
	authInfo, err := requireAuth(r)
	if err != nil || authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	if h.jobTelemetryRepo == nil {
		http.Error(w, "Telemetry service not configured", http.StatusServiceUnavailable)
		return
	}

	q := r.URL.Query()

	limit := 50
	if v := strings.TrimSpace(q.Get("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}

	opts := horizonrepo.ListOpts{
		JobType: strings.TrimSpace(q.Get("jobType")),
		Limit:   limit,
	}

	if since := strings.TrimSpace(q.Get("since")); since != "" {
		if t, err := time.Parse(time.RFC3339, since); err == nil {
			opts.StartTime = &t
		}
	}

	jobs, err := h.jobTelemetryRepo.ListByLens(r.Context(), lensID, opts)
	if err != nil {
		http.Error(w, "Failed to list telemetry jobs", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"jobs":  jobs,
		"total": len(jobs),
	})
}

// GetTelemetryJob handles GET /api/horizon/lenses/{lensId}/telemetry/jobs/{jobId}
func (h *HorizonHandlers) GetTelemetryJob(w http.ResponseWriter, r *http.Request, jobID string) {
	authInfo, err := requireAuth(r)
	if err != nil || authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	if h.jobTelemetryRepo == nil {
		http.Error(w, "Telemetry service not configured", http.StatusServiceUnavailable)
		return
	}

	job, err := h.jobTelemetryRepo.GetByJobID(r.Context(), jobID)
	if err != nil {
		http.Error(w, "Job telemetry not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(job)
}

// GetTelemetrySummary handles GET /api/horizon/lenses/{lensId}/telemetry/summary
func (h *HorizonHandlers) GetTelemetrySummary(w http.ResponseWriter, r *http.Request, lensID string) {
	authInfo, err := requireAuth(r)
	if err != nil || authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	if h.jobTelemetryRepo == nil {
		http.Error(w, "Telemetry service not configured", http.StatusServiceUnavailable)
		return
	}

	since := time.Now().UTC().AddDate(0, 0, -30)
	if v := strings.TrimSpace(r.URL.Query().Get("since")); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			since = t
		}
	}

	// Fetch all jobs in the window to compute the rich summary.
	jobs, err := h.jobTelemetryRepo.ListByLens(r.Context(), lensID, horizonrepo.ListOpts{
		Limit:     500,
		StartTime: &since,
	})
	if err != nil {
		http.Error(w, "Failed to compute telemetry summary", http.StatusInternalServerError)
		return
	}

	var totalSpendUSD float64
	var totalLLMCalls int
	var totalTokens int
	byJobType := map[string]int{}

	type opAgg struct {
		Name    string  `json:"name"`
		CostUSD float64 `json:"costUsd"`
		Count   int     `json:"count"`
	}
	opMap := map[string]*opAgg{}

	type modelAgg struct {
		Model   string  `json:"model"`
		CostUSD float64 `json:"costUsd"`
		Calls   int     `json:"calls"`
	}
	modelMap := map[string]*modelAgg{}

	for _, j := range jobs {
		if j == nil {
			continue
		}
		totalSpendUSD += j.Rollup.LLMCostUSD
		totalLLMCalls += j.Rollup.LLMCalls
		totalTokens += j.Rollup.LLMInputTokens + j.Rollup.LLMOutputTokens

		jt := strings.TrimSpace(j.JobType)
		if jt != "" {
			byJobType[jt]++
		}

		for _, g := range j.Groups {
			for _, op := range g.Operations {
				name := strings.TrimSpace(op.Name)
				if name == "" {
					name = strings.TrimSpace(op.Kind)
				}
				if name == "" {
					continue
				}
				if _, ok := opMap[name]; !ok {
					opMap[name] = &opAgg{Name: name}
				}
				opMap[name].CostUSD += op.CostUSD
				opMap[name].Count += op.Count

				model := strings.TrimSpace(op.Model)
				if model == "" {
					model = strings.TrimSpace(op.ModelAlias)
				}
				if model != "" {
					if _, ok := modelMap[model]; !ok {
						modelMap[model] = &modelAgg{Model: model}
					}
					modelMap[model].CostUSD += op.CostUSD
					modelMap[model].Calls += op.APICalls
					if op.APICalls == 0 {
						modelMap[model].Calls += op.Count
					}
				}
			}
		}
	}

	topOps := make([]*opAgg, 0, len(opMap))
	for _, v := range opMap {
		topOps = append(topOps, v)
	}
	sort.Slice(topOps, func(i, j int) bool { return topOps[i].CostUSD > topOps[j].CostUSD })
	if len(topOps) > 10 {
		topOps = topOps[:10]
	}

	modelDist := make([]*modelAgg, 0, len(modelMap))
	for _, v := range modelMap {
		modelDist = append(modelDist, v)
	}
	sort.Slice(modelDist, func(i, j int) bool { return modelDist[i].CostUSD > modelDist[j].CostUSD })

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"totalSpendUSD":     totalSpendUSD,
		"totalLLMCalls":     totalLLMCalls,
		"totalTokens":       totalTokens,
		"byJobType":         byJobType,
		"topOperations":     topOps,
		"modelDistribution": modelDist,
	})
}

