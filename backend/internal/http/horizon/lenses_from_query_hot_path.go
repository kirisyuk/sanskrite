package horizon

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/rtctx"
	horizonscout "github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

const (
	createLensFromQueryModeJustOpen      = "just_open"
	createLensFromQueryModeResearchFirst = "research_first"
	createLensFromQueryModeHotPath       = "hot_path"

	hotPathInquiryTTL               = 6 * time.Hour
	hotPathAssemblyWaitTimeout      = 45 * time.Second
	hotPathSimulationWaitTimeout    = 90 * time.Second
	hotPathPollInterval             = 2 * time.Second
	hotPathInteractionKind          = "horizon.hot_path.inquiry"
	hotPathInteractionSchemaVersion = "v3"
	hotPathQuestionJobType          = "horizon.hot_path.question"
	hotPathPhaseEventType           = "horizon.question.phase"
)

const (
	hotPathPhaseUnderstand     = "question_understood"
	hotPathPhaseInspect        = "substrate_reused"
	hotPathPhaseEnrichGraph    = "scope_completed"
	hotPathPhaseHydrateMetrics = "metrics_hydrated"
	hotPathPhaseSimulateDone   = "simulation_completed"
	hotPathPhaseAnswer         = "answer_materialized"
)

type hotPathInquiryStage struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Detail  string `json:"detail,omitempty"`
	Started string `json:"startedAt,omitempty"`
	Ended   string `json:"endedAt,omitempty"`
}

type hotPathInquiryPhase struct {
	Phase            string         `json:"phase,omitempty"`
	ID               string         `json:"id"`
	Title            string         `json:"title"`
	Status           string         `json:"status"`
	Summary          string         `json:"summary,omitempty"`
	StartedAt        string         `json:"startedAt,omitempty"`
	EndedAt          string         `json:"endedAt,omitempty"`
	DurationMs       int64          `json:"durationMs,omitempty"`
	Details          map[string]any `json:"details,omitempty"`
	Artifacts        map[string]any `json:"artifacts,omitempty"`
	MetricsRefreshed int            `json:"metricsRefreshed,omitempty"`
	SimulationRunID  string         `json:"simulationRunId,omitempty"`
}

type hotPathSimulationRef struct {
	RunID      string `json:"runId,omitempty"`
	Status     string `json:"status,omitempty"`
	Reused     bool   `json:"reused,omitempty"`
	CreatedAt  string `json:"createdAt,omitempty"`
	FinishedAt string `json:"finishedAt,omitempty"`
}

type hotPathPrimaryReadout struct {
	Headline          string           `json:"headline,omitempty"`
	KeyPoints         []string         `json:"keyPoints,omitempty"`
	TopShoulders      []string         `json:"topShoulders,omitempty"`
	TopProducts       []string         `json:"topProducts,omitempty"`
	TopChains         []string         `json:"topChains,omitempty"`
	RunSummary        map[string]any   `json:"runSummary,omitempty"`
	GroundingCoverage float64          `json:"groundingCoverage,omitempty"`
	TopMonitoring     []map[string]any `json:"topMonitoring,omitempty"`
}

type hotPathInquiryResponse struct {
	Mode           string                 `json:"mode"`
	Status         string                 `json:"status"`
	Cached         bool                   `json:"cached,omitempty"`
	LensReused     bool                   `json:"lensReused,omitempty"`
	QuestionRunID  string                 `json:"questionRunId,omitempty"`
	InteractionID  string                 `json:"interactionId,omitempty"`
	ExpiresAt      string                 `json:"expiresAt,omitempty"`
	Stages         []hotPathInquiryStage  `json:"stages,omitempty"`
	Phases         []hotPathInquiryPhase  `json:"phases,omitempty"`
	Simulation     *hotPathSimulationRef  `json:"simulation,omitempty"`
	PrimaryReadout *hotPathPrimaryReadout `json:"primaryReadout,omitempty"`
}

type hotPathInteractionPayload struct {
	SchemaVersion      string                   `json:"schemaVersion"`
	Mode               string                   `json:"mode"`
	Question           string                   `json:"question"`
	NormalizedQuestion string                   `json:"normalizedQuestion"`
	LensID             string                   `json:"lensId"`
	QuestionRunID      string                   `json:"questionRunId,omitempty"`
	Intent             *agents.IntentResolution `json:"intent,omitempty"`
	HotPath            *hotPathInquiryResponse  `json:"hotPath,omitempty"`
	CreatedAt          string                   `json:"createdAt"`
	ExpiresAt          string                   `json:"expiresAt"`
}

type hotPathPhaseDefinition struct {
	ID    string
	Title string
}

type hotPathPhaseRuntime struct {
	tenantID      string
	projectID     string
	lensID        string
	questionRunID string
	correlationID string
	sessionID     string
	defs          []hotPathPhaseDefinition
	phases        []hotPathInquiryPhase
	index         map[string]int
	publisher     *realtime.Publisher
}

type hotPathAssemblySnapshot struct {
	Ready           bool
	Status          string
	TerminalOutcome string
	TotalJobs       int
	CompletedJobs   int
	FailedJobs      int
	PendingJobs     int
	RunningJobs     int
}

func normalizeCreateLensFromQueryMode(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case createLensFromQueryModeJustOpen:
		return createLensFromQueryModeJustOpen
	case createLensFromQueryModeHotPath:
		return createLensFromQueryModeHotPath
	case "", createLensFromQueryModeResearchFirst:
		return createLensFromQueryModeResearchFirst
	default:
		return createLensFromQueryModeResearchFirst
	}
}

func normalizeHotPathQuestion(query string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(query))), " ")
}

func hotPathPhaseDefinitions() []hotPathPhaseDefinition {
	return []hotPathPhaseDefinition{
		{ID: hotPathPhaseUnderstand, Title: "Понимаю запрос"},
		{ID: hotPathPhaseInspect, Title: "Проверяю, что уже есть"},
		{ID: hotPathPhaseEnrichGraph, Title: "Дозаполняю граф"},
		{ID: hotPathPhaseHydrateMetrics, Title: "Обновляю метрики"},
		{ID: hotPathPhaseSimulateDone, Title: "Симулирую сценарии"},
		{ID: hotPathPhaseAnswer, Title: "Собираю ответ"},
	}
}

func newHotPathPhaseRuntime(ctx context.Context, tenantID, projectID, lensID, questionRunID string, pub *realtime.Publisher) *hotPathPhaseRuntime {
	rc, _ := rtctx.Get(ctx)
	defs := hotPathPhaseDefinitions()
	phases := make([]hotPathInquiryPhase, 0, len(defs))
	index := make(map[string]int, len(defs))
	for i, def := range defs {
		phases = append(phases, hotPathInquiryPhase{
			Phase:  def.ID,
			ID:     def.ID,
			Title:  def.Title,
			Status: "pending",
		})
		index[def.ID] = i
	}
	return &hotPathPhaseRuntime{
		tenantID:      strings.TrimSpace(tenantID),
		projectID:     strings.TrimSpace(projectID),
		lensID:        strings.TrimSpace(lensID),
		questionRunID: strings.TrimSpace(questionRunID),
		correlationID: strings.TrimSpace(rc.CorrelationID),
		sessionID:     strings.TrimSpace(rc.SessionID),
		defs:          defs,
		phases:        phases,
		index:         index,
		publisher:     pub,
	}
}

func (r *hotPathPhaseRuntime) payload(phase hotPathInquiryPhase, step string) map[string]any {
	payload := map[string]any{
		"tenantId":         r.tenantID,
		"projectId":        r.projectID,
		"lensId":           r.lensID,
		"questionRunId":    r.questionRunID,
		"phase":            phase.ID,
		"title":            phase.Title,
		"step":             strings.TrimSpace(step),
		"status":           phase.Status,
		"startedAt":        phase.StartedAt,
		"endedAt":          phase.EndedAt,
		"durationMs":       phase.DurationMs,
		"details":          phase.Details,
		"artifacts":        phase.Artifacts,
		"metricsRefreshed": phase.MetricsRefreshed,
		"simulationRunId":  phase.SimulationRunID,
		"summary":          phase.Summary,
	}
	if r.correlationID != "" {
		payload["correlationId"] = r.correlationID
	}
	if r.sessionID != "" {
		payload["sessionId"] = r.sessionID
	}
	return payload
}

func (r *hotPathPhaseRuntime) publish(phase hotPathInquiryPhase, step string) {
	if r == nil || r.publisher == nil || r.tenantID == "" {
		return
	}
	payload := r.payload(phase, step)
	human := map[string]any{
		"title":       phase.Title,
		"description": strings.TrimSpace(phase.Summary),
	}
	payload["humanReadable"] = human
	_ = r.publisher.PublishJSON(context.Background(), realtime.EventType(hotPathPhaseEventType), r.tenantID, payload)
}

func (r *hotPathPhaseRuntime) start(id string, summary string, details map[string]any, artifacts map[string]any) {
	if r == nil {
		return
	}
	idx, ok := r.index[id]
	if !ok {
		return
	}
	now := time.Now().UTC()
	phase := r.phases[idx]
	phase.Phase = strings.TrimSpace(id)
	phase.Status = "running"
	phase.StartedAt = now.Format(time.RFC3339)
	phase.EndedAt = ""
	phase.DurationMs = 0
	phase.Summary = strings.TrimSpace(summary)
	phase.Details = cloneHotPathMap(details)
	phase.Artifacts = cloneHotPathMap(artifacts)
	r.phases[idx] = phase
	r.publish(phase, "started")
}

func (r *hotPathPhaseRuntime) finish(id, status, summary string, details map[string]any, artifacts map[string]any, metricsRefreshed int, simulationRunID string) {
	if r == nil {
		return
	}
	idx, ok := r.index[id]
	if !ok {
		return
	}
	now := time.Now().UTC()
	phase := r.phases[idx]
	phase.Phase = strings.TrimSpace(id)
	if phase.StartedAt == "" {
		phase.StartedAt = now.Format(time.RFC3339)
	}
	phase.Status = strings.TrimSpace(status)
	phase.EndedAt = now.Format(time.RFC3339)
	if startedAt, err := time.Parse(time.RFC3339, phase.StartedAt); err == nil {
		phase.DurationMs = now.Sub(startedAt).Milliseconds()
	}
	if strings.TrimSpace(summary) != "" {
		phase.Summary = strings.TrimSpace(summary)
	}
	if len(details) > 0 {
		phase.Details = cloneHotPathMap(details)
	}
	if len(artifacts) > 0 {
		phase.Artifacts = cloneHotPathMap(artifacts)
	}
	phase.MetricsRefreshed = metricsRefreshed
	phase.SimulationRunID = strings.TrimSpace(simulationRunID)
	r.phases[idx] = phase
	r.publish(phase, strings.TrimSpace(status))
}

func (r *hotPathPhaseRuntime) snapshot() []hotPathInquiryPhase {
	if r == nil {
		return nil
	}
	out := make([]hotPathInquiryPhase, len(r.phases))
	copy(out, r.phases)
	return out
}

func (r *hotPathPhaseRuntime) stages() []hotPathInquiryStage {
	phases := r.snapshot()
	out := make([]hotPathInquiryStage, 0, len(phases))
	for _, phase := range phases {
		out = append(out, hotPathInquiryStage{
			ID:      phase.ID,
			Title:   phase.Title,
			Status:  phase.Status,
			Detail:  phase.Summary,
			Started: phase.StartedAt,
			Ended:   phase.EndedAt,
		})
	}
	return out
}

func cloneHotPathMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func isTerminalHotPathStatus(status string) bool {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "answered", "ready", "partial", "failed":
		return true
	default:
		return false
	}
}

func hotPathArtifactScopeCount(lens *horizonmodels.Lens) int {
	if lens == nil {
		return 0
	}
	count := 0
	for _, entry := range lens.Scope {
		normalized := strings.TrimSpace(strings.ToLower(entry))
		if strings.HasPrefix(normalized, "/artifact/supply-chain/") || strings.HasPrefix(normalized, "/artifact/service-chain/") {
			count++
		}
	}
	for _, ref := range lens.ScopeArtifacts {
		if strings.EqualFold(strings.TrimSpace(ref.Kind), string(horizonmodels.MaterializedKindSupplyChainPath)) ||
			strings.EqualFold(strings.TrimSpace(ref.Kind), string(horizonmodels.MaterializedKindServiceChainPath)) {
			count++
		}
	}
	if count > 0 {
		return count
	}
	if lens.ViewConfig == nil {
		return 0
	}
	switch raw := lens.ViewConfig["artifactScopeAddCount"].(type) {
	case int:
		if raw > 0 {
			return raw
		}
	case int32:
		if raw > 0 {
			return int(raw)
		}
	case int64:
		if raw > 0 {
			return int(raw)
		}
	case float64:
		if raw > 0 {
			return int(raw)
		}
	case float32:
		if raw > 0 {
			return int(raw)
		}
	}
	return 0
}

func intentSupportsHotPath(intent *agents.IntentResolution) bool {
	if intent == nil {
		return false
	}
	for _, domain := range intent.Domains {
		cp := strings.TrimSpace(strings.ToLower(domain.CanonicalPath))
		ct := strings.TrimSpace(strings.ToLower(domain.ContractType))
		if strings.Contains(cp, "/supply-chain/") || strings.Contains(cp, "/service-chain/") || ct == "supply-chain" || ct == "value-chain" || ct == "service-chain" {
			return true
		}
		if domain.SupplyChainExecutionPlan != nil && len(domain.SupplyChainExecutionPlan.Queries) > 0 {
			return true
		}
		if domain.ServiceChainExecutionPlan != nil && len(domain.ServiceChainExecutionPlan.Queries) > 0 {
			return true
		}
	}
	return false
}

func forceIntentHotPathOnly(in *agents.IntentResolution) *agents.IntentResolution {
	if in == nil {
		return nil
	}
	out := *in
	if len(in.Domains) == 0 {
		return &out
	}
	out.Domains = make([]agents.DomainResolution, len(in.Domains))
	for i, domain := range in.Domains {
		copyDomain := domain
		if domain.SupplyChainExecutionPlan != nil {
			planCopy := *domain.SupplyChainExecutionPlan
			planCopy.GraphBuildMode = agents.SupplyChainGraphBuildModeHotPathOnly
			copyDomain.SupplyChainExecutionPlan = &planCopy
		} else if domain.ServiceChainExecutionPlan != nil {
			planCopy := *domain.ServiceChainExecutionPlan
			planCopy.GraphBuildMode = agents.SupplyChainGraphBuildModeHotPathOnly
			copyDomain.ServiceChainExecutionPlan = &planCopy
		} else {
			cp := strings.TrimSpace(strings.ToLower(domain.CanonicalPath))
			ct := strings.TrimSpace(strings.ToLower(domain.ContractType))
			if strings.Contains(cp, "/supply-chain/") || ct == "supply-chain" || ct == "value-chain" {
				copyDomain.SupplyChainExecutionPlan = &agents.SupplyChainExecutionPlan{
					GraphBuildMode: agents.SupplyChainGraphBuildModeHotPathOnly,
				}
			}
			if strings.Contains(cp, "/service-chain/") || ct == "service-chain" {
				copyDomain.ServiceChainExecutionPlan = &agents.SupplyChainExecutionPlan{
					GraphBuildMode: agents.SupplyChainGraphBuildModeHotPathOnly,
				}
			}
		}
		out.Domains[i] = copyDomain
	}
	return &out
}

func buildHotPathExecutionPlanContract(domainModes, specialties []string) horizonscout.ExecutionPlanContract {
	plan := horizonscout.BuildExecutionPlanContract(nil, domainModes, specialties)
	if plan.Mode != "supply_chain" {
		return plan
	}
	if plan.MaxAnchors > 8 {
		plan.MaxAnchors = 8
	}
	if plan.MaxRootJobs > 6 {
		plan.MaxRootJobs = 6
	}
	if plan.MaxRecursionDepth > 2 {
		plan.MaxRecursionDepth = 2
	}
	if plan.MaxChildrenPerJob > 8 {
		plan.MaxChildrenPerJob = 8
	}
	if plan.MaxCascadeJobs > 48 {
		plan.MaxCascadeJobs = 48
	}
	if plan.MaxSupplyChainQueries > 3 {
		plan.MaxSupplyChainQueries = 3
	}
	plan.PriorityTiers = []string{"high", "normal"}
	plan.StopConditions = []string{"budget_exhausted", "max_root_jobs_reached", "max_cascade_jobs_reached"}
	plan.BudgetCeilingOutputTokens = 16000
	plan.StopOnBudgetExhaustion = true
	return plan
}

func isHotPathRuntimeLens(lens *horizonmodels.Lens) bool {
	if lens == nil {
		return false
	}
	for _, dq := range lens.DomainQueries {
		if dq.SupplyChainExecutionPlan != nil && strings.TrimSpace(strings.ToLower(dq.SupplyChainExecutionPlan.GraphBuildMode)) == agents.SupplyChainGraphBuildModeHotPathOnly {
			return true
		}
	}
	if lens.ViewConfig == nil {
		return false
	}
	runtimeCfg, ok := lens.ViewConfig["questionRuntime"].(map[string]any)
	if !ok || runtimeCfg == nil {
		return false
	}
	mode, _ := runtimeCfg["mode"].(string)
	return strings.TrimSpace(strings.ToLower(mode)) == createLensFromQueryModeHotPath
}

func markLensHotPathRuntime(lens *horizonmodels.Lens, executionPlan horizonscout.ExecutionPlanContract) {
	if lens == nil {
		return
	}
	if lens.ViewConfig == nil {
		lens.ViewConfig = map[string]any{}
	}
	lens.ViewConfig["questionRuntime"] = map[string]any{
		"mode":      createLensFromQueryModeHotPath,
		"ttlHours":  int(hotPathInquiryTTL / time.Hour),
		"createdAt": time.Now().UTC().Format(time.RFC3339),
	}
	lens.ViewConfig[horizonscout.ExecutionPlanStateKey] = executionPlan.ToStateMap()
}

func (h *HorizonHandlers) findReusableHotPathLens(ctx context.Context, tenantID, projectID, query string) (*horizonmodels.Lens, error) {
	if h == nil || h.service == nil || h.service.Lens() == nil {
		return nil, nil
	}
	activeStatus := string(horizonmodels.LensStatusActive)
	lenses, err := h.service.Lens().ListLensesByProject(ctx, tenantID, projectID, &activeStatus, 200)
	if err != nil {
		return nil, err
	}
	normalized := normalizeHotPathQuestion(query)
	sort.SliceStable(lenses, func(i, j int) bool {
		return lenses[i].UpdatedAt.After(lenses[j].UpdatedAt)
	})
	now := time.Now().UTC()
	for _, lens := range lenses {
		if lens == nil || !isHotPathRuntimeLens(lens) {
			continue
		}
		if normalizeHotPathQuestion(lens.OriginalQuery) != normalized {
			continue
		}
		ts := lens.UpdatedAt
		if ts.IsZero() {
			ts = lens.CreatedAt
		}
		if !ts.IsZero() && now.Sub(ts) > hotPathInquiryTTL {
			continue
		}
		return lens, nil
	}
	return nil, nil
}

func (h *HorizonHandlers) findCachedHotPathInteraction(ctx context.Context, tenantID, projectID, query string) (*hotPathInteractionPayload, *horizonmodels.Lens, string, error) {
	if h == nil || h.interactions == nil {
		return nil, nil, "", nil
	}
	items, err := h.interactions.ListByProject(ctx, projectID)
	if err != nil {
		return nil, nil, "", err
	}
	normalized := normalizeHotPathQuestion(query)
	now := time.Now().UTC()
	for i := len(items) - 1; i >= 0; i-- {
		it := items[i]
		if it == nil || strings.TrimSpace(it.TenantID) != tenantID || strings.TrimSpace(it.Kind) != hotPathInteractionKind {
			continue
		}
		var payload hotPathInteractionPayload
		if err := json.Unmarshal([]byte(it.Content), &payload); err != nil {
			continue
		}
		if strings.TrimSpace(payload.SchemaVersion) != hotPathInteractionSchemaVersion {
			continue
		}
		if payload.NormalizedQuestion != normalized || payload.HotPath == nil {
			continue
		}
		if payload.ExpiresAt != "" {
			if exp, err := time.Parse(time.RFC3339, payload.ExpiresAt); err == nil && now.After(exp) {
				continue
			}
		}
		if h.service == nil || h.service.Lens() == nil || strings.TrimSpace(payload.LensID) == "" {
			continue
		}
		lens, err := h.service.Lens().GetLens(ctx, strings.TrimSpace(payload.LensID))
		if err != nil || lens == nil || strings.TrimSpace(lens.TenantID) != tenantID {
			continue
		}
		if h.hotPathInteractionIsStale(ctx, &payload, lens) {
			continue
		}
		payload.HotPath.Cached = true
		payload.HotPath.InteractionID = strings.TrimSpace(it.ID)
		return &payload, lens, strings.TrimSpace(it.ID), nil
	}
	return nil, nil, "", nil
}

func (h *HorizonHandlers) hotPathInteractionIsStale(ctx context.Context, payload *hotPathInteractionPayload, lens *horizonmodels.Lens) bool {
	if payload == nil || lens == nil {
		return true
	}
	createdAt, err := time.Parse(time.RFC3339, strings.TrimSpace(payload.CreatedAt))
	if err != nil {
		return true
	}
	if !lens.UpdatedAt.IsZero() && lens.UpdatedAt.After(createdAt) {
		return true
	}
	if h == nil || h.service == nil || h.service.Simulation() == nil {
		return false
	}
	latest, err := h.service.Simulation().GetLatestRunByLens(ctx, strings.TrimSpace(lens.TenantID), strings.TrimSpace(lens.ID))
	if err != nil || latest == nil {
		return false
	}
	if !isTerminalSimulationStatus(latest.Status) {
		return true
	}
	if !latest.UpdatedAt.IsZero() && latest.UpdatedAt.After(createdAt) {
		return true
	}
	return false
}

func (h *HorizonHandlers) snapshotHotPathAssembly(ctx context.Context, lens *horizonmodels.Lens) hotPathAssemblySnapshot {
	snapshot := hotPathAssemblySnapshot{Ready: true, Status: "ready", TerminalOutcome: horizonscout.TerminalOutcomeCompleted}
	if h == nil || h.service == nil || h.service.ScoutingJobs() == nil || lens == nil {
		return snapshot
	}
	jobs, err := h.service.ScoutingJobs().ListByProjectAndLens(ctx, strings.TrimSpace(lens.TenantID), strings.TrimSpace(lens.ProjectID), strings.TrimSpace(lens.ID), 1000)
	if err != nil {
		snapshot.Status = "error"
		snapshot.Ready = false
		snapshot.TerminalOutcome = horizonscout.TerminalOutcomeFailed
		return snapshot
	}
	for _, job := range jobs {
		if job == nil {
			continue
		}
		snapshot.TotalJobs++
		switch strings.ToLower(strings.TrimSpace(string(job.Status))) {
		case "completed":
			snapshot.CompletedJobs++
		case "failed":
			snapshot.FailedJobs++
		case "pending":
			snapshot.PendingJobs++
		case "running":
			snapshot.RunningJobs++
		default:
			switch strings.ToLower(strings.TrimSpace(string(job.CurrentState))) {
			case "verified":
				snapshot.CompletedJobs++
			case "failed", "rejected":
				snapshot.FailedJobs++
			case "pending":
				snapshot.PendingJobs++
			default:
				snapshot.RunningJobs++
			}
		}
	}
	artifactScopeCount := hotPathArtifactScopeCount(lens)
	if artifactScopeCount > 0 && snapshot.CompletedJobs > 0 {
		snapshot.Ready = true
		snapshot.Status = "ready"
		if snapshot.PendingJobs > 0 || snapshot.RunningJobs > 0 || snapshot.FailedJobs > 0 {
			snapshot.TerminalOutcome = horizonscout.TerminalOutcomePartialCompleted
		}
		return snapshot
	}
	if snapshot.TotalJobs == 0 || (snapshot.PendingJobs == 0 && snapshot.RunningJobs == 0) {
		snapshot.Ready = true
		if snapshot.FailedJobs > 0 {
			snapshot.TerminalOutcome = horizonscout.TerminalOutcomePartialCompleted
		}
		return snapshot
	}
	snapshot.Ready = false
	snapshot.Status = "scouting"
	snapshot.TerminalOutcome = ""
	return snapshot
}

func (h *HorizonHandlers) waitForHotPathAssembly(ctx context.Context, lens *horizonmodels.Lens, timeout time.Duration) hotPathAssemblySnapshot {
	deadline := time.Now().UTC().Add(timeout)
	for {
		snapshot := h.snapshotHotPathAssembly(ctx, lens)
		if snapshot.Ready || time.Now().UTC().After(deadline) {
			return snapshot
		}
		select {
		case <-ctx.Done():
			return snapshot
		case <-time.After(hotPathPollInterval):
		}
	}
}

func isTerminalSimulationStatus(status string) bool {
	switch strings.TrimSpace(strings.ToUpper(status)) {
	case string(horizonmodels.SimulationRunStatusSucceeded),
		string(horizonmodels.SimulationRunStatusFailed),
		string(horizonmodels.SimulationRunStatusDegraded),
		string(horizonmodels.SimulationRunStatusCancelled),
		string(horizonmodels.SimulationRunStatusExpired):
		return true
	default:
		return false
	}
}

func (h *HorizonHandlers) waitForSimulationTerminal(ctx context.Context, runID string, timeout time.Duration) (*horizonmodels.SimulationRun, error) {
	if h == nil || h.service == nil || h.service.Simulation() == nil {
		return nil, nil
	}
	deadline := time.Now().UTC().Add(timeout)
	for {
		run, err := h.service.Simulation().GetRun(ctx, runID)
		if err != nil {
			return nil, err
		}
		if run != nil && isTerminalSimulationStatus(run.Status) {
			return run, nil
		}
		if time.Now().UTC().After(deadline) {
			return run, nil
		}
		select {
		case <-ctx.Done():
			return run, nil
		case <-time.After(hotPathPollInterval):
		}
	}
}

func topOverlayTitles(items []map[string]any, limit int) []string {
	if len(items) == 0 || limit <= 0 {
		return nil
	}
	out := make([]string, 0, limit)
	seen := make(map[string]struct{}, limit)
	for _, item := range items {
		if title, ok := item["title"].(string); ok && strings.TrimSpace(title) != "" {
			title = strings.TrimSpace(title)
			if _, exists := seen[title]; exists {
				continue
			}
			seen[title] = struct{}{}
			out = append(out, title)
		}
		if len(out) >= limit {
			break
		}
	}
	return out
}

func buildHotPathPrimaryReadout(run *horizonmodels.SimulationRun, bundle *horizonmodels.SimulationBundle) *hotPathPrimaryReadout {
	if run == nil || bundle == nil {
		return nil
	}
	overlays := filterSimulationResultOverlays(bundle.ResultOverlays, simulationOverlayFilters{MinScore: 0.35})
	shoulders := toMapSlice(overlays["shoulders"])
	products := toMapSlice(overlays["products"])
	chains := toMapSlice(overlays["pathChains"])
	headline := "Primary hot-path answer is ready."
	keyPoints := make([]string, 0, 4)
	if len(shoulders) > 0 {
		keyPoints = append(keyPoints, "Top external shoulders: "+strings.Join(topOverlayTitles(shoulders, 3), ", "))
	}
	if len(products) > 0 {
		keyPoints = append(keyPoints, "Top product risks: "+strings.Join(topOverlayTitles(products, 3), ", "))
	}
	if len(chains) > 0 {
		keyPoints = append(keyPoints, "Top pressured chains: "+strings.Join(topOverlayTitles(chains, 2), ", "))
	}
	if len(keyPoints) == 0 && len(bundle.ExecutiveNarrative) > 0 {
		keyPoints = append(keyPoints, strings.TrimSpace(bundle.ExecutiveNarrative[0]))
	}
	topMonitoring := make([]map[string]any, 0, 4)
	for _, item := range bundle.MonitoringSignals {
		if len(topMonitoring) >= 4 {
			break
		}
		topMonitoring = append(topMonitoring, item)
	}
	runSummary := make(map[string]any, len(bundle.RunSummary)+4)
	for k, v := range bundle.RunSummary {
		runSummary[k] = v
	}
	runSummary["runId"] = strings.TrimSpace(run.ID)
	runSummary["status"] = strings.TrimSpace(run.Status)
	if run.UpdatedAt.IsZero() {
		runSummary["updatedAt"] = run.CreatedAt.UTC().Format(time.RFC3339)
	} else {
		runSummary["updatedAt"] = run.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return &hotPathPrimaryReadout{
		Headline:          headline,
		KeyPoints:         keyPoints,
		TopShoulders:      topOverlayTitles(shoulders, 4),
		TopProducts:       topOverlayTitles(products, 4),
		TopChains:         topOverlayTitles(chains, 3),
		RunSummary:        runSummary,
		GroundingCoverage: bundle.GroundingCoverage,
		TopMonitoring:     topMonitoring,
	}
}

func (h *HorizonHandlers) persistHotPathInteraction(ctx context.Context, tenantID, projectID, question string, lens *horizonmodels.Lens, intent *agents.IntentResolution, hotPath *hotPathInquiryResponse) (string, error) {
	if h == nil || h.interactions == nil || lens == nil || hotPath == nil {
		return "", nil
	}
	now := time.Now().UTC()
	payload := hotPathInteractionPayload{
		SchemaVersion:      hotPathInteractionSchemaVersion,
		Mode:               createLensFromQueryModeHotPath,
		Question:           question,
		NormalizedQuestion: normalizeHotPathQuestion(question),
		LensID:             strings.TrimSpace(lens.ID),
		QuestionRunID:      strings.TrimSpace(hotPath.QuestionRunID),
		Intent:             intent,
		HotPath:            hotPath,
		CreatedAt:          now.Format(time.RFC3339),
		ExpiresAt:          now.Add(hotPathInquiryTTL).Format(time.RFC3339),
	}
	content, _ := json.Marshal(payload)
	subkind := "v1"
	summary := "Hot-path inquiry for " + strings.TrimSpace(lens.Name)
	it := &corerepo.Interaction{
		TenantID:  tenantID,
		ProjectID: projectID,
		Kind:      hotPathInteractionKind,
		Subkind:   &subkind,
		Content:   string(content),
		Summary:   &summary,
	}
	return h.interactions.Create(ctx, it)
}

func (h *HorizonHandlers) saveHotPathTelemetry(ctx context.Context, tenantID, projectID string, lens *horizonmodels.Lens, resp *hotPathInquiryResponse) {
	if h == nil || h.jobTelemetryRepo == nil || lens == nil || resp == nil {
		return
	}
	startedAt := time.Now().UTC()
	if len(resp.Phases) > 0 {
		if ts, err := time.Parse(time.RFC3339, strings.TrimSpace(resp.Phases[0].StartedAt)); err == nil {
			startedAt = ts
		}
	}
	completedAt := time.Now().UTC()
	phaseEvents := make([]horizonmodels.TelemetryPhaseEvent, 0, len(resp.Phases))
	groups := make([]horizonmodels.TelemetryGroup, 0, len(resp.Phases))
	for _, phase := range resp.Phases {
		event := horizonmodels.TelemetryPhaseEvent{
			Phase:            strings.TrimSpace(phase.ID),
			Title:            strings.TrimSpace(phase.Title),
			Step:             firstNonEmpty(strings.TrimSpace(phase.Status), "completed"),
			Status:           strings.TrimSpace(phase.Status),
			StartedAt:        startedAt,
			DurationMs:       phase.DurationMs,
			Details:          cloneHotPathMap(phase.Details),
			Artifacts:        cloneHotPathMap(phase.Artifacts),
			MetricsRefreshed: phase.MetricsRefreshed,
			SimulationRunID:  strings.TrimSpace(phase.SimulationRunID),
		}
		if ts, err := time.Parse(time.RFC3339, strings.TrimSpace(phase.StartedAt)); err == nil {
			event.StartedAt = ts
		}
		if endedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(phase.EndedAt)); err == nil {
			event.EndedAt = &endedAt
			completedAt = endedAt
		}
		phaseEvents = append(phaseEvents, event)
		groups = append(groups, horizonmodels.TelemetryGroup{
			Name:       strings.TrimSpace(phase.ID),
			StartedAt:  event.StartedAt,
			DurationMs: phase.DurationMs,
		})
	}
	status := strings.TrimSpace(resp.Status)
	tel := &horizonmodels.JobTelemetry{
		TenantID:    strings.TrimSpace(tenantID),
		ProjectID:   strings.TrimSpace(projectID),
		LensID:      strings.TrimSpace(lens.ID),
		JobID:       strings.TrimSpace(resp.QuestionRunID),
		JobType:     hotPathQuestionJobType,
		Trigger:     createLensFromQueryModeHotPath,
		StartedAt:   startedAt,
		CompletedAt: &completedAt,
		DurationMs:  completedAt.Sub(startedAt).Milliseconds(),
		Status:      firstNonEmpty(status, "completed"),
		Groups:      groups,
		PhaseEvents: phaseEvents,
		Rollup: horizonmodels.TelemetryRollup{
			TotalDurationMs: completedAt.Sub(startedAt).Milliseconds(),
		},
	}
	_ = h.jobTelemetryRepo.Save(ctx, tel)
}

func (h *HorizonHandlers) runHotPathInquiry(ctx context.Context, tenantID, projectID, question string, lens *horizonmodels.Lens, intent *agents.IntentResolution, lensReused bool) (*hotPathInquiryResponse, error) {
	questionRunID := uuid.NewString()
	intentFocus := ""
	intentDomains := 0
	intentAnchors := 0
	intentHotPath := false
	if intent != nil {
		intentFocus = strings.TrimSpace(intent.Focus)
		intentDomains = len(intent.Domains)
		intentAnchors = len(intent.Anchors)
		intentHotPath = intentSupportsHotPath(intent)
	}
	if lens != nil {
		if rc, ok := rtctx.Get(ctx); ok {
			ctx = rtctx.With(ctx, rtctx.Context{
				TenantID:      firstNonEmpty(strings.TrimSpace(rc.TenantID), strings.TrimSpace(tenantID)),
				ProjectID:     firstNonEmpty(strings.TrimSpace(rc.ProjectID), strings.TrimSpace(projectID)),
				LensID:        strings.TrimSpace(lens.ID),
				CorrelationID: strings.TrimSpace(rc.CorrelationID),
				SessionID:     strings.TrimSpace(rc.SessionID),
			})
		}
	}
	phaseRuntime := newHotPathPhaseRuntime(ctx, tenantID, projectID, strings.TrimSpace(lens.ID), questionRunID, h.service.RealtimePublisher())
	resp := &hotPathInquiryResponse{
		Mode:          createLensFromQueryModeHotPath,
		Status:        "assembling",
		LensReused:    lensReused,
		QuestionRunID: questionRunID,
		ExpiresAt:     time.Now().UTC().Add(hotPathInquiryTTL).Format(time.RFC3339),
	}
	phaseRuntime.start(hotPathPhaseUnderstand, "Определяю доменную логику вопроса и нужный hot path.", map[string]any{
		"focus":           intentFocus,
		"domains":         intentDomains,
		"anchors":         intentAnchors,
		"question":        strings.TrimSpace(question),
		"requiresHotPath": intentHotPath,
	}, map[string]any{
		"domainModes": append([]string(nil), lens.DomainModes...),
	})
	phaseRuntime.finish(hotPathPhaseUnderstand, "completed", "Запрос распознан как hot-path сценарий поверх существующего supply-chain пайплайна.", nil, nil, 0, "")

	phaseRuntime.start(hotPathPhaseInspect, map[bool]string{
		true:  "Переиспользую существующую линзу и её substrate.",
		false: "Использую только что собранную линзу как substrate.",
	}[lensReused], map[string]any{
		"lensReused":  lensReused,
		"lensId":      strings.TrimSpace(lens.ID),
		"lensName":    strings.TrimSpace(lens.Name),
		"scopePaths":  len(lens.Scope),
		"domainModes": append([]string(nil), lens.DomainModes...),
	}, map[string]any{
		"lensId":   strings.TrimSpace(lens.ID),
		"lensName": strings.TrimSpace(lens.Name),
	})
	phaseRuntime.finish(hotPathPhaseInspect, "completed", map[bool]string{
		true:  "База уже была в системе и переиспользована.",
		false: "Создана новая линза для ответа на вопрос.",
	}[lensReused], nil, nil, 0, "")

	phaseRuntime.start(hotPathPhaseEnrichGraph, "Проверяю, что hot-path substrate уже достаточно наполнен для первичного ответа.", map[string]any{
		"artifactScopeCount": hotPathArtifactScopeCount(lens),
	}, map[string]any{
		"lensId": strings.TrimSpace(lens.ID),
	})
	assembly := h.waitForHotPathAssembly(ctx, lens, hotPathAssemblyWaitTimeout)
	if assembly.Ready {
		phaseRuntime.finish(hotPathPhaseEnrichGraph, "completed", "Граф достаточно наполнен для первичного ответа.", map[string]any{
			"status":        assembly.Status,
			"totalJobs":     assembly.TotalJobs,
			"completedJobs": assembly.CompletedJobs,
			"failedJobs":    assembly.FailedJobs,
			"runningJobs":   assembly.RunningJobs,
			"pendingJobs":   assembly.PendingJobs,
		}, map[string]any{
			"artifactScopeCount": hotPathArtifactScopeCount(lens),
		}, 0, "")
	} else {
		phaseRuntime.finish(hotPathPhaseEnrichGraph, "pending", "Граф ещё достраивается; первичный ответ пока рано собирать.", map[string]any{
			"status":        assembly.Status,
			"totalJobs":     assembly.TotalJobs,
			"completedJobs": assembly.CompletedJobs,
			"failedJobs":    assembly.FailedJobs,
			"runningJobs":   assembly.RunningJobs,
			"pendingJobs":   assembly.PendingJobs,
		}, map[string]any{
			"artifactScopeCount": hotPathArtifactScopeCount(lens),
		}, 0, "")
		resp.Status = "assembling"
		resp.Phases = phaseRuntime.snapshot()
		resp.Stages = phaseRuntime.stages()
		interactionID, _ := h.persistHotPathInteraction(ctx, tenantID, projectID, question, lens, intent, resp)
		resp.InteractionID = interactionID
		h.saveHotPathTelemetry(ctx, tenantID, projectID, lens, resp)
		return resp, nil
	}

	if h.service == nil || h.service.Simulation() == nil {
		resp.Status = "ready"
		resp.Phases = phaseRuntime.snapshot()
		resp.Stages = phaseRuntime.stages()
		interactionID, _ := h.persistHotPathInteraction(ctx, tenantID, projectID, question, lens, intent, resp)
		resp.InteractionID = interactionID
		h.saveHotPathTelemetry(ctx, tenantID, projectID, lens, resp)
		return resp, nil
	}

	var run *horizonmodels.SimulationRun
	var reused bool
	latest, _ := h.service.Simulation().GetLatestRunByLens(ctx, tenantID, strings.TrimSpace(lens.ID))
	if latest != nil && isTerminalSimulationStatus(latest.Status) && time.Since(latest.CreatedAt.UTC()) <= hotPathInquiryTTL {
		run = latest
		reused = true
	} else {
		req := &horizonmodels.SimulationRunRequest{
			Question:    question,
			LensID:      strings.TrimSpace(lens.ID),
			ProjectID:   strings.TrimSpace(projectID),
			Audience:    "executive",
			Mode:        createLensFromQueryModeHotPath,
			WorldCount:  8,
			TickCount:   6,
			TimeHorizon: "30d",
			DomainModes: append([]string(nil), lens.DomainModes...),
		}
		var err error
		phaseRuntime.start(hotPathPhaseHydrateMetrics, "Перед симуляцией дотягиваю свежие operational metrics из общего metric-core.", map[string]any{
			"mode":        createLensFromQueryModeHotPath,
			"timeHorizon": "30d",
		}, map[string]any{
			"lensId": strings.TrimSpace(lens.ID),
		})
		run, err = h.service.Simulation().CreateRun(ctx, tenantID, projectID, req)
		if err != nil {
			phaseRuntime.finish(hotPathPhaseHydrateMetrics, "failed", "Не удалось подготовить мир к симуляции.", map[string]any{
				"error": err.Error(),
			}, nil, 0, "")
			resp.Status = "partial"
			resp.Phases = phaseRuntime.snapshot()
			resp.Stages = phaseRuntime.stages()
			interactionID, _ := h.persistHotPathInteraction(ctx, tenantID, projectID, question, lens, intent, resp)
			resp.InteractionID = interactionID
			h.saveHotPathTelemetry(ctx, tenantID, projectID, lens, resp)
			return resp, nil
		}
		hydration := map[string]any{}
		if raw, ok := run.BaselinePreview["operationalHydration"].(map[string]any); ok && raw != nil {
			hydration = cloneHotPathMap(raw)
		}
		metricsRefreshed := 0
		switch raw := run.BaselinePreview["operational_targets_refreshed"].(type) {
		case int:
			metricsRefreshed = raw
		case int64:
			metricsRefreshed = int(raw)
		case float64:
			metricsRefreshed = int(raw)
		}
		phaseRuntime.finish(hotPathPhaseHydrateMetrics, "completed", "Базовые operational metrics перед симуляцией обновлены.", hydration, map[string]any{
			"missingMetricCount": run.BaselinePreview["operational_missing_metric_count"],
			"targetCount":        run.BaselinePreview["operational_target_count"],
			"bottleneckRefs":     run.BaselinePreview["operational_bottleneck_refs"],
		}, metricsRefreshed, strings.TrimSpace(run.ID))
	}
	if reused {
		hydration := map[string]any{
			"reusedRun": true,
			"status":    strings.TrimSpace(run.Status),
		}
		phaseRuntime.start(hotPathPhaseHydrateMetrics, "Переиспользую уже подготовленный simulation baseline.", hydration, map[string]any{
			"runId": strings.TrimSpace(run.ID),
		})
		phaseRuntime.finish(hotPathPhaseHydrateMetrics, "completed", "Использован уже существующий свежий simulation baseline.", hydration, nil, 0, strings.TrimSpace(run.ID))
	}

	phaseRuntime.start(hotPathPhaseSimulateDone, "Запускаю или переиспользую горячую симуляцию сценариев.", map[string]any{
		"reused": reused,
		"status": strings.TrimSpace(run.Status),
	}, map[string]any{
		"runId": strings.TrimSpace(run.ID),
	})
	resp.Simulation = &hotPathSimulationRef{
		RunID:     strings.TrimSpace(run.ID),
		Status:    strings.TrimSpace(run.Status),
		Reused:    reused,
		CreatedAt: run.CreatedAt.UTC().Format(time.RFC3339),
	}
	run, _ = h.waitForSimulationTerminal(ctx, strings.TrimSpace(run.ID), hotPathSimulationWaitTimeout)
	if run != nil {
		resp.Simulation.Status = strings.TrimSpace(run.Status)
		resp.Simulation.FinishedAt = run.UpdatedAt.UTC().Format(time.RFC3339)
	}
	if run == nil || !isTerminalSimulationStatus(run.Status) {
		phaseRuntime.finish(hotPathPhaseSimulateDone, "pending", "Симуляция ещё выполняется; ответ вернётся с текущим состоянием.", map[string]any{
			"status": firstNonEmpty(resp.Simulation.Status, "queued"),
		}, map[string]any{
			"runId": strings.TrimSpace(resp.Simulation.RunID),
		}, 0, strings.TrimSpace(resp.Simulation.RunID))
		resp.Status = "simulating"
		resp.Phases = phaseRuntime.snapshot()
		resp.Stages = phaseRuntime.stages()
		interactionID, _ := h.persistHotPathInteraction(ctx, tenantID, projectID, question, lens, intent, resp)
		resp.InteractionID = interactionID
		h.saveHotPathTelemetry(ctx, tenantID, projectID, lens, resp)
		return resp, nil
	}

	if strings.EqualFold(run.Status, string(horizonmodels.SimulationRunStatusFailed)) {
		phaseRuntime.finish(hotPathPhaseSimulateDone, "failed", "Симуляция завершилась ошибкой.", map[string]any{
			"status": strings.TrimSpace(run.Status),
		}, map[string]any{
			"runId": strings.TrimSpace(run.ID),
		}, 0, strings.TrimSpace(run.ID))
		resp.Status = "partial"
		resp.Phases = phaseRuntime.snapshot()
		resp.Stages = phaseRuntime.stages()
		interactionID, _ := h.persistHotPathInteraction(ctx, tenantID, projectID, question, lens, intent, resp)
		resp.InteractionID = interactionID
		h.saveHotPathTelemetry(ctx, tenantID, projectID, lens, resp)
		return resp, nil
	}

	bundle, _, err := h.service.Simulation().GetBundleView(ctx, strings.TrimSpace(run.TenantID), strings.TrimSpace(run.ID))
	if err == nil && bundle != nil {
		resp.PrimaryReadout = buildHotPathPrimaryReadout(run, bundle)
	}
	phaseRuntime.finish(hotPathPhaseSimulateDone, "completed", "Симуляция завершена и дала пригодный первичный readout.", map[string]any{
		"status": strings.TrimSpace(run.Status),
	}, map[string]any{
		"runId": strings.TrimSpace(run.ID),
	}, 0, strings.TrimSpace(run.ID))
	phaseRuntime.start(hotPathPhaseAnswer, "Собираю управленческий ответ поверх графа, метрик и симуляции.", nil, map[string]any{
		"runId": strings.TrimSpace(run.ID),
	})
	answerDetails := map[string]any{}
	answerArtifacts := map[string]any{
		"runId": strings.TrimSpace(run.ID),
	}
	if resp.PrimaryReadout != nil {
		answerDetails["headline"] = strings.TrimSpace(resp.PrimaryReadout.Headline)
		answerDetails["keyPoints"] = append([]string(nil), resp.PrimaryReadout.KeyPoints...)
		answerArtifacts["topShoulders"] = append([]string(nil), resp.PrimaryReadout.TopShoulders...)
		answerArtifacts["topProducts"] = append([]string(nil), resp.PrimaryReadout.TopProducts...)
		answerArtifacts["topChains"] = append([]string(nil), resp.PrimaryReadout.TopChains...)
	}
	phaseRuntime.finish(hotPathPhaseAnswer, "completed", "Первичный ответ собран и сохранён в кэшируемом question run.", answerDetails, answerArtifacts, 0, strings.TrimSpace(run.ID))
	resp.Status = "answered"
	resp.Phases = phaseRuntime.snapshot()
	resp.Stages = phaseRuntime.stages()
	interactionID, _ := h.persistHotPathInteraction(ctx, tenantID, projectID, question, lens, intent, resp)
	resp.InteractionID = interactionID
	h.saveHotPathTelemetry(ctx, tenantID, projectID, lens, resp)
	return resp, nil
}
