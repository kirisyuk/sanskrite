package http

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// MetaHandler handles meta-analysis endpoints
type MetaHandler struct {
	projects     *repo.ProjectsRepository
	interactions *repo.InteractionsRepository
	eventsRepo   *repo.MetaAnalysesRepository
	aggProj      *repo.MetaAggregateProjectsRepository
	aggUser      *repo.MetaAggregateUsersRepository
	exec         *ai.AITaskExecutor
	logger       *log.Logger
}

func NewMetaHandler(projects *repo.ProjectsRepository, interactions *repo.InteractionsRepository, events *repo.MetaAnalysesRepository, aggProj *repo.MetaAggregateProjectsRepository, aggUser *repo.MetaAggregateUsersRepository, exec *ai.AITaskExecutor, logger *log.Logger) *MetaHandler {
	return &MetaHandler{projects: projects, interactions: interactions, eventsRepo: events, aggProj: aggProj, aggUser: aggUser, exec: exec, logger: logger}
}

// Analyze request/response
type AnalyzeInteractionRef struct {
	RefID  *string        `json:"refId,omitempty"`
	Inline *InlineMessage `json:"inline,omitempty"`
	// Sentinel compatibility: id=="0" means inline
	ID   *string `json:"id,omitempty"`
	Role *string `json:"role,omitempty"`
	Text *string `json:"text,omitempty"`
	Ts   *string `json:"timestamp,omitempty"`
}

type InlineMessage struct {
	Role      string  `json:"role"`
	Text      string  `json:"text"`
	Timestamp *string `json:"timestamp,omitempty"`
}

type AnalyzeRequest struct {
	Analyzers    []string                `json:"analyzers"`
	Interactions []AnalyzeInteractionRef `json:"interactions"`
}

type AnalyzeResult struct {
	Analyzer string         `json:"analyzer"`
	Result   map[string]any `json:"result"`
}

type AnalyzeResponse struct {
	Results         []AnalyzeResult `json:"results"`
	WhoLeads        string          `json:"whoLeads"`
	LeadershipIndex float64         `json:"leadershipIndex"`
	FocusRetention  float64         `json:"focusRetention"`
	StructureScore  float64         `json:"structureScore"`
	Version         string          `json:"version"`
	EventID         string          `json:"eventId"`
}

// POST /api/v1/projects/{id}/meta:analyze
func (h *MetaHandler) Analyze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 5 { // /api/v1/projects/{id}/meta:analyze
		WriteBadRequest(w, "INVALID_PATH", "Project ID is required")
		return
	}
	projectID := parts[3]
	project, err := h.projects.Get(r.Context(), projectID)
	if err != nil || project == nil || project.TenantID != auth.TenantID {
		WriteForbidden(w, "Access denied")
		return
	}

	var req AnalyzeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Failed to parse request body")
		return
	}
	if len(req.Analyzers) == 0 {
		WriteBadRequest(w, "VALIDATION_ERROR", "analyzers is required")
		return
	}
	msgs, err := h.resolveWindow(r.Context(), projectID, req.Interactions)
	if err != nil {
		if h.logger != nil {
			h.logger.Printf("meta: resolveWindow error: %v (project=%s, analyzers=%v)", err, projectID, req.Analyzers)
		}
		WriteBadRequest(w, "INVALID_INTERACTIONS", err.Error())
		return
	}

	// Build plain text window with roles
	window := buildWindowText(msgs)

	// 1) Dialog control classification (common)
	who, leadIdx, focus, structure, err := h.classifyDialogControl(r.Context(), window)
	if err != nil {
		if h.logger != nil {
			h.logger.Printf("meta: classify dialog error: %v (windowChars=%d)", err, len(window))
		}
		WriteInternalError(w, fmt.Sprintf("Dialog control classification failed: %v", err))
		return
	}

	// 2) Run requested analyzers
	results := make([]AnalyzeResult, 0, len(req.Analyzers))
	for _, a := range req.Analyzers {
		resMap, runErr := h.runAnalyzer(r.Context(), a, window)
		if runErr != nil {
			if h.logger != nil {
				// Common place to catch JSON decode/validation mismatches
				h.logger.Printf("meta: analyzer %s failed: %v (windowChars=%d)", a, runErr, len(window))
			}
			WriteInternalError(w, fmt.Sprintf("Analyzer %s failed: %v", a, runErr))
			return
		}
		results = append(results, AnalyzeResult{Analyzer: a, Result: resMap})
	}

	// 3) Persist event
	evt := &models.MetaAnalysisEvent{
		TenantID:   auth.TenantID,
		ProjectID:  projectID,
		UserID:     auth.UID,
		Analyzer:   models.MetaAnalyzer("batch"),
		Version:    "coach.v1",
		Model:      "auto",
		TokensUsed: len(window) / 4,
		Window:     models.MetaWindow{Mode: "ad-hoc", MessageCount: len(msgs)},
		Common:     models.MetaCommon{WhoLeads: who, LeadershipIndex: leadIdx, FocusRetention: focus, StructureScore: structure},
		Result:     map[string]any{"results": results},
		CreatedAt:  time.Now().UTC(),
	}
	id, err := h.eventsRepo.Create(r.Context(), evt)
	if err != nil {
		WriteInternalError(w, "Failed to persist meta analysis event")
		return
	}

	// 4) Update aggregates
	_ = h.updateProjectAggregate(r.Context(), project, evt)
	_ = h.updateUserAggregate(r.Context(), auth.TenantID, auth.UID, evt)

	WriteJSON(w, http.StatusOK, AnalyzeResponse{
		Results:         results,
		WhoLeads:        who,
		LeadershipIndex: leadIdx,
		FocusRetention:  focus,
		StructureScore:  structure,
		Version:         "coach.v1",
		EventID:         id,
	})
}

// resolveWindow merges refId and inline messages
func (h *MetaHandler) resolveWindow(ctx context.Context, projectID string, items []AnalyzeInteractionRef) ([]InlineMessage, error) {
	var out []InlineMessage
	for _, it := range items {
		if it.RefID != nil && *it.RefID != "" {
			inter, err := h.interactions.Get(ctx, *it.RefID)
			if err != nil || inter == nil || inter.ProjectID != projectID {
				return nil, fmt.Errorf("invalid refId: %s", *it.RefID)
			}
			role := "assistant"
			if inter.Author != nil {
				switch strings.ToLower(inter.Author.Kind) {
				case "human":
					role = "user"
				case "system":
					role = "system"
				case "tool":
					role = "tool"
				}
			}
			out = append(out, InlineMessage{Role: role, Text: inter.Content})
			continue
		}
		if it.Inline != nil {
			out = append(out, *it.Inline)
			continue
		}
		// Sentinel: id=="0" treated as inline
		if it.ID != nil && *it.ID == "0" && it.Role != nil && it.Text != nil {
			out = append(out, InlineMessage{Role: *it.Role, Text: *it.Text, Timestamp: it.Ts})
			continue
		}
		return nil, fmt.Errorf("invalid interaction element: must provide refId or inline")
	}
	return out, nil
}

func buildWindowText(msgs []InlineMessage) string {
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(strings.ToUpper(m.Role))
		b.WriteString(": ")
		b.WriteString(m.Text)
		b.WriteString("\n\n")
	}
	return b.String()
}

// Dialog control classification using structured LLM
type dialogControlJSON struct {
	WhoLeads        string  `json:"whoLeads"`
	LeadershipIndex float64 `json:"leadershipIndex"`
	FocusRetention  float64 `json:"focusRetention"`
	StructureScore  float64 `json:"structureScore"`
}

func (h *MetaHandler) classifyDialogControl(ctx context.Context, window string) (string, float64, float64, float64, error) {
	schema := `{"type":"object","additionalProperties":false,"properties":{"whoLeads":{"type":"string","enum":["User","AI","Mixed"]},"leadershipIndex":{"type":"number","minimum":0,"maximum":1},"focusRetention":{"type":"number","minimum":0,"maximum":1},"structureScore":{"type":"number","minimum":0,"maximum":1}},"required":["whoLeads","leadershipIndex","focusRetention","structureScore"]}`
	prompt := fmt.Sprintf("Return ONLY JSON strictly matching the schema. Classify dialog control over the conversation window.\n\nSchema:%s\n\nWINDOW:\n%s", schema, window)
	task := &ai.AITask{
		TaskType:       ai.TaskTypeTextProcessing,
		InputContent:   prompt,
		AIRequirements: ai.AIRequirements{MinContextWindow: intMax(4096, len(prompt)/4), TextGenerationCapability: true, MinCapabilityScore: 6},
		Verifier:       ai.NewGenericVerifier(func(dc dialogControlJSON) error { return nil }),
	}
	res, err := h.exec.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return "", 0, 0, 0, err
	}
	dc := res.(dialogControlJSON)
	return dc.WhoLeads, dc.LeadershipIndex, dc.FocusRetention, dc.StructureScore, nil
}

func (h *MetaHandler) runAnalyzer(ctx context.Context, analyzer string, window string) (map[string]any, error) {
	switch analyzer {
	case string(models.MetaAnalyzerThinking):
		return h.runThinking(ctx, window)
	case string(models.MetaAnalyzerBiases):
		return h.runBiases(ctx, window)
	case string(models.MetaAnalyzerDecisions):
		return h.runDecisions(ctx, window)
	case string(models.MetaAnalyzerComm):
		return h.runComm(ctx, window)
	case string(models.MetaAnalyzerLearning):
		return h.runLearning(ctx, window)
	default:
		return nil, fmt.Errorf("unknown analyzer: %s", analyzer)
	}
}

func (h *MetaHandler) runThinking(ctx context.Context, window string) (map[string]any, error) {
	// Enum dictionaries
	depth := []string{"Surface", "Analytical", "Systemic"}
	uncertainty := []string{"Avoids", "Acknowledges", "WorksWithIt"}
	horizon := []string{"Reactive", "Tactical", "Strategic"}
	ttype := []string{"Convergent", "Divergent", "Integrative"}
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"depth":        map[string]any{"type": "object", "properties": map[string]any{"label": map[string]any{"type": "string", "enum": depth}, "intensity": map[string]any{"type": "number", "minimum": 0, "maximum": 1}, "confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1}}, "required": []string{"label", "intensity"}},
			"timeHorizon":  map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{"label": map[string]any{"type": "string", "enum": horizon}, "intensity": map[string]any{"type": "number", "minimum": 0, "maximum": 1}}, "required": []string{"label", "intensity"}}},
			"uncertainty":  map[string]any{"type": "object", "properties": map[string]any{"label": map[string]any{"type": "string", "enum": uncertainty}, "intensity": map[string]any{"type": "number", "minimum": 0, "maximum": 1}}, "required": []string{"label", "intensity"}},
			"thinkingType": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{"label": map[string]any{"type": "string", "enum": ttype}, "intensity": map[string]any{"type": "number", "minimum": 0, "maximum": 1}}, "required": []string{"label", "intensity"}}},
		},
		"required": []string{"depth", "timeHorizon", "uncertainty", "thinkingType"},
	}
	schemaJSON, _ := json.Marshal(schema)
	prompt := fmt.Sprintf("Return ONLY JSON strictly matching the schema below. Classify thinking patterns over the conversation window.\n\nSchema:%s\n\nWINDOW:\n%s", string(schemaJSON), window)
	verifier := ai.NewGenericVerifier(func(v models.ThinkingPatternsResult) error { return nil })
	task := &ai.AITask{TaskType: ai.TaskTypeTextProcessing, InputContent: prompt, AIRequirements: ai.AIRequirements{MinContextWindow: len(prompt) / 4, TextGenerationCapability: true, MinCapabilityScore: 6}, Verifier: verifier}
	res, err := h.exec.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, err
	}
	return toMap(res)
}

func (h *MetaHandler) runBiases(ctx context.Context, window string) (map[string]any, error) {
	// Keep open list but require label + intensity
	schema := map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"biases":  map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{"label": map[string]any{"type": "string"}, "intensity": map[string]any{"type": "number", "minimum": 0, "maximum": 1}, "confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1}, "rationale": map[string]any{"type": "string"}, "evidence": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}, "required": []string{"label", "intensity"}}},
			"metrics": map[string]any{"type": "object"},
		},
		"required": []string{"biases"},
	}
	schemaJSON, _ := json.Marshal(schema)
	prompt := fmt.Sprintf("Return ONLY JSON strictly matching the schema below. Detect cognitive biases in the conversation window.\n\nSchema:%s\n\nWINDOW:\n%s", string(schemaJSON), window)
	verifier := ai.NewGenericVerifier(func(v models.CognitiveBiasesResult) error { return nil })
	task := &ai.AITask{TaskType: ai.TaskTypeTextProcessing, InputContent: prompt, AIRequirements: ai.AIRequirements{MinContextWindow: len(prompt) / 4, TextGenerationCapability: true, MinCapabilityScore: 6}, Verifier: verifier}
	res, err := h.exec.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, err
	}
	return toMap(res)
}

func (h *MetaHandler) runDecisions(ctx context.Context, window string) (map[string]any, error) {
	proc := []string{"Intuitive", "Analytical", "Consensus"}
	risk := []string{"RiskAverse", "RiskNeutral", "RiskSeeking"}
	crit := []string{"Mono", "Multi", "Adaptive"}
	speed := []string{"Impulsive", "Reflective", "Paralyzing"}
	schema := map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"process":       enumScoreSchema(proc),
			"riskAttitude":  enumScoreSchema(risk),
			"criteriaStyle": enumScoreSchema(crit),
			"speed":         enumScoreSchema(speed),
		},
		"required": []string{"process", "riskAttitude", "criteriaStyle", "speed"},
	}
	schemaJSON, _ := json.Marshal(schema)
	prompt := fmt.Sprintf("Return ONLY JSON strictly matching the schema below. Classify decision-making style.\n\nSchema:%s\n\nWINDOW:\n%s", string(schemaJSON), window)
	verifier := ai.NewGenericVerifier(func(v models.DecisionMakingResult) error { return nil })
	task := &ai.AITask{TaskType: ai.TaskTypeTextProcessing, InputContent: prompt, AIRequirements: ai.AIRequirements{MinContextWindow: len(prompt) / 4, TextGenerationCapability: true, MinCapabilityScore: 6}, Verifier: verifier}
	res, err := h.exec.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, err
	}
	return toMap(res)
}

func (h *MetaHandler) runComm(ctx context.Context, window string) (map[string]any, error) {
	clarity := []string{"Clear", "Vague", "Contradictory"}
	abstraction := []string{"Concrete", "General", "Conceptual"}
	ctxc := []string{"Provided", "Assumed", "Missing"}
	loop := []string{"Reacts", "Ignores", "Corrects"}
	schema := map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"clarity":             enumScoreSchema(clarity),
			"abstraction":         enumScoreSchema(abstraction),
			"contextCompleteness": enumScoreSchema(ctxc),
			"feedbackLoop":        enumScoreSchema(loop),
		},
		"required": []string{"clarity", "abstraction", "contextCompleteness", "feedbackLoop"},
	}
	schemaJSON, _ := json.Marshal(schema)
	prompt := fmt.Sprintf("Return ONLY JSON strictly matching the schema below. Classify communication patterns.\n\nSchema:%s\n\nWINDOW:\n%s", string(schemaJSON), window)
	verifier := ai.NewGenericVerifier(func(v models.CommunicationPatternsResult) error { return nil })
	task := &ai.AITask{TaskType: ai.TaskTypeTextProcessing, InputContent: prompt, AIRequirements: ai.AIRequirements{MinContextWindow: len(prompt) / 4, TextGenerationCapability: true, MinCapabilityScore: 6}, Verifier: verifier}
	res, err := h.exec.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, err
	}
	return toMap(res)
}

func (h *MetaHandler) runLearning(ctx context.Context, window string) (map[string]any, error) {
	dyn := []string{"Linear", "Deepening", "Jumping"}
	integ := []string{"Isolated", "Connected", "Synthetic"}
	meta := []string{"None", "Partial", "Full"}
	schema := map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"dynamics":      enumScoreSchema(dyn),
			"integration":   enumScoreSchema(integ),
			"metaAwareness": enumScoreSchema(meta),
		},
		"required": []string{"dynamics", "integration", "metaAwareness"},
	}
	schemaJSON, _ := json.Marshal(schema)
	prompt := fmt.Sprintf("Return ONLY JSON strictly matching the schema below. Classify learning trajectory.\n\nSchema:%s\n\nWINDOW:\n%s", string(schemaJSON), window)
	verifier := ai.NewGenericVerifier(func(v models.LearningTrajectoryResult) error { return nil })
	task := &ai.AITask{TaskType: ai.TaskTypeTextProcessing, InputContent: prompt, AIRequirements: ai.AIRequirements{MinContextWindow: len(prompt) / 4, TextGenerationCapability: true, MinCapabilityScore: 6}, Verifier: verifier}
	res, err := h.exec.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, err
	}
	return toMap(res)
}

func enumScoreSchema(options []string) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"label":      map[string]any{"type": "string", "enum": options},
			"intensity":  map[string]any{"type": "number", "minimum": 0, "maximum": 1},
			"confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1},
			"rationale":  map[string]any{"type": "string"},
			"evidence":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
		"required": []string{"label", "intensity"},
	}
}

func toMap(v interface{}) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func intMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (h *MetaHandler) updateProjectAggregate(ctx context.Context, p *models.Project, evt *models.MetaAnalysisEvent) error {
	a, _ := h.aggProj.Get(ctx, p.ID)
	if a == nil {
		a = &models.MetaAggregateProject{ID: p.ID, TenantID: p.TenantID, ProjectID: p.ID, OwnerUserID: p.OwnerUserID, Version: "coach.v1", Samples: 0}
	}
	// Incremental average
	n := float64(a.Samples)
	a.Samples++
	a.LeadershipIndex = (a.LeadershipIndex*n + evt.Common.LeadershipIndex) / float64(a.Samples)
	a.FocusRetention = (a.FocusRetention*n + evt.Common.FocusRetention) / float64(a.Samples)
	a.StructureScore = (a.StructureScore*n + evt.Common.StructureScore) / float64(a.Samples)
	a.WhoLeads = evt.Common.WhoLeads
	a.Effectiveness = 0.5*a.LeadershipIndex + 0.25*a.FocusRetention + 0.25*a.StructureScore
	a.LastResults = evt.Result
	return h.aggProj.Upsert(ctx, a)
}

func (h *MetaHandler) updateUserAggregate(ctx context.Context, tenantID, userID string, evt *models.MetaAnalysisEvent) error {
	u, _ := h.aggUser.Get(ctx, tenantID, userID)
	if u == nil {
		u = &models.MetaAggregateUser{TenantID: tenantID, UserID: userID, Version: "coach.v1", Samples: 0}
	}
	n := float64(u.Samples)
	u.Samples++
	u.LeadershipIndex = (u.LeadershipIndex*n + evt.Common.LeadershipIndex) / float64(u.Samples)
	u.FocusRetention = (u.FocusRetention*n + evt.Common.FocusRetention) / float64(u.Samples)
	u.StructureScore = (u.StructureScore*n + evt.Common.StructureScore) / float64(u.Samples)
	u.Effectiveness = 0.5*u.LeadershipIndex + 0.25*u.FocusRetention + 0.25*u.StructureScore
	return h.aggUser.Upsert(ctx, u)
}

// List events: GET /api/v1/projects/{id}/meta/events
func (h *MetaHandler) ListEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 6 { // /api/v1/projects/{id}/meta/events
		WriteBadRequest(w, "INVALID_PATH", "Project ID is required")
		return
	}
	projectID := parts[3]
	project, err := h.projects.Get(r.Context(), projectID)
	if err != nil || project == nil || project.TenantID != auth.TenantID {
		WriteForbidden(w, "Access denied")
		return
	}
	events, err := h.eventsRepo.ListByProject(r.Context(), projectID, 100)
	if err != nil {
		WriteInternalError(w, "Failed to list meta events")
		return
	}
	WriteJSON(w, http.StatusOK, events)
}

// Get aggregate: GET /api/v1/projects/{id}/meta/aggregate
func (h *MetaHandler) GetAggregate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 6 { // /api/v1/projects/{id}/meta/aggregate
		WriteBadRequest(w, "INVALID_PATH", "Project ID is required")
		return
	}
	projectID := parts[3]
	project, err := h.projects.Get(r.Context(), projectID)
	if err != nil || project == nil || project.TenantID != auth.TenantID {
		WriteForbidden(w, "Access denied")
		return
	}
	agg, err := h.aggProj.Get(r.Context(), projectID)
	if err != nil || agg == nil {
		WriteNotFound(w, "Aggregate not found")
		return
	}
	WriteJSON(w, http.StatusOK, agg)
}
