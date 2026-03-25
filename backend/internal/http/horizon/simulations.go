package horizon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
)

func (h *HorizonHandlers) HandleSimulationsRoot(w http.ResponseWriter, r *http.Request) {
	authInfo, err := requireAuth(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if h.service == nil || h.service.Simulation() == nil {
		http.Error(w, "simulation service is not configured", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodPost:
		h.createSimulation(w, r, authInfo)
	case http.MethodGet:
		runs, err := h.service.Simulation().ListRecentByTenant(r.Context(), strings.TrimSpace(authInfo.TenantID), 25)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *HorizonHandlers) HandleSimulationRoutes(w http.ResponseWriter, r *http.Request) {
	authInfo, err := requireAuth(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if h.service == nil || h.service.Simulation() == nil {
		http.Error(w, "simulation service is not configured", http.StatusServiceUnavailable)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/horizon/simulations/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		http.NotFound(w, r)
		return
	}
	runID := strings.TrimSpace(parts[0])
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		run, err := h.service.Simulation().GetRun(r.Context(), runID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if !h.simulationAccessibleToTenant(r.Context(), run, authInfo.TenantID) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		writeJSON(w, http.StatusOK, run)
		return
	}
	switch parts[1] {
	case "bundle":
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		run, err := h.service.Simulation().GetRun(r.Context(), runID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if !h.simulationAccessibleToTenant(r.Context(), run, authInfo.TenantID) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		artifact, err := h.service.Simulation().GetBundle(r.Context(), strings.TrimSpace(run.TenantID), runID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, artifact)
	case "bundle-view":
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		run, err := h.service.Simulation().GetRun(r.Context(), runID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if !h.simulationAccessibleToTenant(r.Context(), run, authInfo.TenantID) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		bundle, artifact, err := h.service.Simulation().GetBundleView(r.Context(), strings.TrimSpace(run.TenantID), runID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"runId":      runID,
			"artifactId": strings.TrimSpace(artifact.ID),
			"kind":       "bundle_view",
			"bundle":     bundle,
		})
	case "overlays":
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		run, err := h.service.Simulation().GetRun(r.Context(), runID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if !h.simulationAccessibleToTenant(r.Context(), run, authInfo.TenantID) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		bundle, artifact, err := h.service.Simulation().GetBundleView(r.Context(), strings.TrimSpace(run.TenantID), runID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		filters := parseSimulationOverlayFilters(r)
		overlays := filterSimulationResultOverlays(bundle.ResultOverlays, filters)
		writeJSON(w, http.StatusOK, map[string]any{
			"runId":      runID,
			"artifactId": strings.TrimSpace(artifact.ID),
			"kind":       "simulation_result_overlays",
			"filters":    filters.toMap(),
			"overlays":   overlays,
		})
	case "explain":
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		run, err := h.service.Simulation().GetRun(r.Context(), runID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if !h.simulationAccessibleToTenant(r.Context(), run, authInfo.TenantID) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if h.genAIHandler == nil {
			http.Error(w, "genai handler is not configured", http.StatusServiceUnavailable)
			return
		}
		var req simulationExplanationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		req.OverlayID = strings.TrimSpace(req.OverlayID)
		req.Kind = strings.TrimSpace(req.Kind)
		req.MatchedArtifactIDs = splitQueryCSV(strings.Join(req.MatchedArtifactIDs, ","))
		if req.OverlayID == "" {
			http.Error(w, "overlayId is required", http.StatusBadRequest)
			return
		}
		bundle, artifact, err := h.service.Simulation().GetBundleView(r.Context(), strings.TrimSpace(run.TenantID), runID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		overlayContext, err := buildSimulationExplanationContext(bundle, req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		prompt := buildSimulationExplanationPrompt(run, bundle, overlayContext, req)
		schema := simulationExplanationSchema()
		resp, err := h.genAIHandler.GenerateDirect(r.Context(), gen_combined.GenAIRequest{
			Model:             "gemini-3-flash-preview",
			SystemInstruction: simulationExplanationSystemInstruction(),
			Prompt:            prompt,
			Temperature:       0.15,
			MaxOutputTokens:   1600,
			StrictJSONEnabled: true,
			JSONSchema:        schema,
		})
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to generate explanation: %v", err), http.StatusInternalServerError)
			return
		}
		var explanation simulationExplanationPayload
		if err := json.Unmarshal([]byte(resp.Content), &explanation); err != nil {
			http.Error(w, fmt.Sprintf("failed to parse explanation: %v", err), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"runId":       runID,
			"artifactId":  strings.TrimSpace(artifact.ID),
			"kind":        "simulation_explanation",
			"overlayId":   overlayContext.ItemID,
			"overlayKind": overlayContext.Kind,
			"explanation": explanation,
		})
	case "worlds":
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		run, err := h.service.Simulation().GetRun(r.Context(), runID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if !h.simulationAccessibleToTenant(r.Context(), run, authInfo.TenantID) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		worlds, err := h.service.Simulation().ListWorlds(r.Context(), strings.TrimSpace(run.TenantID), runID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"worlds": worlds})
	case "trace":
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		run, err := h.service.Simulation().GetRun(r.Context(), runID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if !h.simulationAccessibleToTenant(r.Context(), run, authInfo.TenantID) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		trace, err := h.service.Simulation().GetTrace(r.Context(), strings.TrimSpace(run.TenantID), runID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, trace)
	default:
		http.NotFound(w, r)
	}
}

func (h *HorizonHandlers) createSimulation(w http.ResponseWriter, r *http.Request, authInfo *httputil.AuthInfo) {
	var req horizonmodels.SimulationRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	run, err := h.service.Simulation().CreateRun(r.Context(), strings.TrimSpace(authInfo.TenantID), strings.TrimSpace(req.ProjectID), &req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	agentmeshTaskID := ""
	if len(run.AgentMeshTaskIDs) > 0 {
		agentmeshTaskID = run.AgentMeshTaskIDs[0]
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"runId":           run.ID,
		"status":          run.Status,
		"agentmeshTaskId": agentmeshTaskID,
		"createdAt":       run.CreatedAt,
	})
}

func (h *HorizonHandlers) simulationAccessibleToTenant(ctx context.Context, run *horizonmodels.SimulationRun, tenantID string) bool {
	if run == nil {
		return false
	}
	runTenantID := strings.TrimSpace(run.TenantID)
	tenantID = strings.TrimSpace(tenantID)
	if runTenantID == tenantID {
		return true
	}
	if runTenantID != "system" && runTenantID != "global" {
		return false
	}
	lensID := strings.TrimSpace(run.Request.LensID)
	if lensID == "" {
		lensID = strings.TrimSpace(run.BaselineRefs.LensID)
	}
	if lensID == "" || h.service == nil || h.service.Lens() == nil {
		return false
	}
	lens, err := h.service.Lens().GetLens(ctx, lensID)
	if err != nil || lens == nil {
		return false
	}
	return strings.TrimSpace(lens.TenantID) == tenantID
}

type simulationOverlayFilters struct {
	Kinds       []string
	Functions   []string
	Disciplines []string
	Roles       []string
	Targets     []string
	IDs         []string
	Categories  []string
	Query       string
	MinScore    float64
}

func parseSimulationOverlayFilters(r *http.Request) simulationOverlayFilters {
	query := r.URL.Query()
	minScore := 0.0
	if raw := strings.TrimSpace(query.Get("minScore")); raw != "" {
		if parsed, err := strconv.ParseFloat(raw, 64); err == nil {
			minScore = parsed
		}
	}
	return simulationOverlayFilters{
		Kinds:       splitQueryCSV(query.Get("kinds")),
		Functions:   splitQueryCSV(query.Get("functions")),
		Disciplines: splitQueryCSV(query.Get("disciplines")),
		Roles:       splitQueryCSV(query.Get("roles")),
		Targets:     splitQueryCSV(query.Get("targets")),
		IDs:         splitQueryCSV(query.Get("ids")),
		Categories:  splitQueryCSV(query.Get("categories")),
		Query:       strings.ToLower(strings.TrimSpace(query.Get("q"))),
		MinScore:    minScore,
	}
}

func (f simulationOverlayFilters) toMap() map[string]any {
	return map[string]any{
		"kinds":       f.Kinds,
		"functions":   f.Functions,
		"disciplines": f.Disciplines,
		"roles":       f.Roles,
		"targets":     f.Targets,
		"ids":         f.IDs,
		"categories":  f.Categories,
		"q":           f.Query,
		"minScore":    f.MinScore,
	}
}

func filterSimulationResultOverlays(resultOverlays map[string]any, filters simulationOverlayFilters) map[string]any {
	if len(resultOverlays) == 0 {
		return map[string]any{
			"shoulders":  []map[string]any{},
			"products":   []map[string]any{},
			"pathChains": []map[string]any{},
			"cascades":   []map[string]any{},
			"roles":      []map[string]any{},
		}
	}
	return map[string]any{
		"shoulders":  filterOverlayItems(toMapSlice(resultOverlays["shoulders"]), "shoulders", filters),
		"products":   filterOverlayItems(toMapSlice(resultOverlays["products"]), "products", filters),
		"pathChains": filterOverlayItems(toMapSlice(resultOverlays["pathChains"]), "pathChains", filters),
		"cascades":   filterCascadeOverlayItems(toMapSlice(resultOverlays["cascades"]), filters),
		"roles":      filterRoleOverlayItems(toMapSlice(resultOverlays["roles"]), filters),
	}
}

func filterOverlayItems(items []map[string]any, kind string, filters simulationOverlayFilters) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if !overlayKindAllowed(kind, filters.Kinds) {
			continue
		}
		if !overlayPassesCommonFilters(item, filters) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func filterCascadeOverlayItems(items []map[string]any, filters simulationOverlayFilters) []map[string]any {
	if !overlayKindAllowed("cascades", filters.Kinds) {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if !overlayPassesIDQueryCategoryFilters(item, filters) {
			continue
		}
		if filters.MinScore > 0 && simNumberField(item, "score") < filters.MinScore {
			continue
		}
		if len(filters.Roles) > 0 && !anyFoldIntersect(simStringSliceField(item, "affectedRoles"), filters.Roles) {
			continue
		}
		if len(filters.Targets) > 0 && !overlayCascadeTargetsMatch(item, filters.Targets) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func filterRoleOverlayItems(items []map[string]any, filters simulationOverlayFilters) []map[string]any {
	if !overlayKindAllowed("roles", filters.Kinds) {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if !overlayPassesIDQueryCategoryFilters(item, filters) {
			continue
		}
		if len(filters.Roles) > 0 && !containsFold(filters.Roles, simStringField(item, "role")) {
			continue
		}
		if len(filters.Functions) > 0 && !anyFoldIntersect(simStringSliceField(item, "priorityFunctions"), filters.Functions) {
			continue
		}
		if len(filters.Disciplines) > 0 && !anyFoldIntersect(simStringSliceField(item, "priorityDisciplines"), filters.Disciplines) {
			continue
		}
		if len(filters.Targets) > 0 && !roleTargetsMatch(item, filters.Targets) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func overlayKindAllowed(kind string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	return containsFold(allowed, kind)
}

func overlayPassesCommonFilters(item map[string]any, filters simulationOverlayFilters) bool {
	if !overlayPassesIDQueryCategoryFilters(item, filters) {
		return false
	}
	if filters.MinScore > 0 && simNumberField(item, "score") < filters.MinScore {
		return false
	}
	if len(filters.Functions) > 0 && !anyFoldIntersect(simStringSliceField(item, "functions"), filters.Functions) {
		return false
	}
	if len(filters.Disciplines) > 0 && !anyFoldIntersect(simStringSliceField(item, "disciplines"), filters.Disciplines) {
		return false
	}
	if len(filters.Targets) > 0 && !anyFoldIntersect(simStringSliceField(item, "targetRefs"), filters.Targets) && !containsTargetCountry(item, filters.Targets) {
		return false
	}
	return true
}

func overlayPassesIDQueryCategoryFilters(item map[string]any, filters simulationOverlayFilters) bool {
	if len(filters.IDs) > 0 && !containsFold(filters.IDs, simStringField(item, "id")) {
		return false
	}
	if len(filters.Categories) > 0 && !containsFold(filters.Categories, simStringField(item, "riskCategory")) {
		return false
	}
	if filters.Query != "" && !overlayMatchesQuery(item, filters.Query) {
		return false
	}
	return true
}

func overlayMatchesQuery(item map[string]any, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	fields := []string{
		simStringField(item, "id"),
		simStringField(item, "title"),
		simStringField(item, "riskCategory"),
		simStringField(item, "explanation"),
		simStringField(item, "sourceTitle"),
		simStringField(item, "targetTitle"),
		simStringField(item, "role"),
	}
	for _, value := range fields {
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	for _, value := range append(append(simStringSliceField(item, "functions"), simStringSliceField(item, "disciplines")...), simStringSliceField(item, "targetRefs")...) {
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}

func overlayCascadeTargetsMatch(item map[string]any, targets []string) bool {
	for _, candidate := range []string{
		simStringField(item, "sourceId"),
		simStringField(item, "targetId"),
		simStringField(item, "sourceTitle"),
		simStringField(item, "targetTitle"),
	} {
		if containsFold(targets, candidate) {
			return true
		}
		for _, target := range targets {
			if strings.Contains(strings.ToLower(candidate), strings.ToLower(strings.TrimSpace(target))) {
				return true
			}
		}
	}
	return false
}

func roleTargetsMatch(item map[string]any, targets []string) bool {
	targetMap := simMapField(item, "targets")
	for _, section := range []string{"shoulders", "products", "pathChains", "cascades"} {
		if anyFoldIntersect(simStringSliceField(targetMap, section), targets) {
			return true
		}
	}
	return false
}

func containsTargetCountry(item map[string]any, targets []string) bool {
	candidate := simStringField(item, "targetCountryCode")
	if candidate == "" {
		return false
	}
	return containsFold(targets, candidate)
}

func splitQueryCSV(raw string) []string {
	parts := strings.Split(strings.TrimSpace(raw), ",")
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[strings.ToLower(part)]; ok {
			continue
		}
		seen[strings.ToLower(part)] = struct{}{}
		out = append(out, part)
	}
	return out
}

func toMapSlice(raw any) []map[string]any {
	switch typed := raw.(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			m, ok := item.(map[string]any)
			if ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func simStringField(item map[string]any, key string) string {
	if item == nil {
		return ""
	}
	value, _ := item[key].(string)
	return strings.TrimSpace(value)
}

func simMapField(item map[string]any, key string) map[string]any {
	if item == nil {
		return nil
	}
	value, _ := item[key].(map[string]any)
	return value
}

func simStringSliceField(item map[string]any, key string) []string {
	if item == nil {
		return nil
	}
	switch typed := item[key].(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, value := range typed {
			if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	default:
		return nil
	}
}

func simNumberField(item map[string]any, key string) float64 {
	if item == nil {
		return 0
	}
	switch typed := item[key].(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		value, _ := typed.Float64()
		return value
	default:
		return 0
	}
}

func anyFoldIntersect(left, right []string) bool {
	for _, item := range left {
		if containsFold(right, item) {
			return true
		}
	}
	return false
}

func containsFold(values []string, needle string) bool {
	needle = strings.TrimSpace(needle)
	if needle == "" {
		return false
	}
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), needle) {
			return true
		}
	}
	return false
}

type simulationExplanationRequest struct {
	OverlayID          string   `json:"overlayId"`
	Kind               string   `json:"kind,omitempty"`
	MatchedArtifactIDs []string `json:"matchedArtifactIds,omitempty"`
}

type simulationExplanationPayload struct {
	Title              string                            `json:"title"`
	Summary            string                            `json:"summary"`
	StressNarrative    []string                          `json:"stressNarrative"`
	AffectedObjects    []string                          `json:"affectedObjects"`
	AffectedLinks      []string                          `json:"affectedLinks"`
	CascadeRisks       []string                          `json:"cascadeRisks"`
	RoleImplications   []simulationExplanationRoleImpact `json:"roleImplications"`
	RecommendedActions []string                          `json:"recommendedActions"`
	KeyMetrics         []simulationExplanationMetric     `json:"keyMetrics"`
}

type simulationExplanationRoleImpact struct {
	Role        string `json:"role"`
	Implication string `json:"implication"`
}

type simulationExplanationMetric struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type simulationExplanationContext struct {
	Kind         string
	ItemID       string
	Item         map[string]any
	PathChains   []map[string]any
	Cascades     []map[string]any
	RoleReadouts []map[string]any
}

func buildSimulationExplanationContext(bundle *horizonmodels.SimulationBundle, req simulationExplanationRequest) (*simulationExplanationContext, error) {
	if bundle == nil || len(bundle.ResultOverlays) == 0 {
		return nil, fmt.Errorf("simulation result overlays are not available")
	}
	kinds := []string{"shoulders", "products", "pathChains", "cascades"}
	if req.Kind != "" {
		kinds = []string{req.Kind}
	}
	var selected map[string]any
	selectedKind := ""
	for _, kind := range kinds {
		for _, item := range toMapSlice(bundle.ResultOverlays[kind]) {
			if strings.EqualFold(simStringField(item, "id"), req.OverlayID) {
				selected = item
				selectedKind = kind
				break
			}
		}
		if selected != nil {
			break
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("overlay %q not found", req.OverlayID)
	}
	context := &simulationExplanationContext{
		Kind:   selectedKind,
		ItemID: simStringField(selected, "id"),
		Item:   selected,
	}
	targetRefs := simStringSliceField(selected, "targetRefs")
	productPath := simStringField(selected, "productPath")
	targetCountryCode := simStringField(selected, "targetCountryCode")
	for _, item := range toMapSlice(bundle.ResultOverlays["pathChains"]) {
		if overlayRelationMatches(item, req.OverlayID, targetRefs, productPath, targetCountryCode) {
			context.PathChains = append(context.PathChains, item)
		}
	}
	for _, item := range toMapSlice(bundle.ResultOverlays["cascades"]) {
		if overlayRelationMatches(item, req.OverlayID, targetRefs, productPath, targetCountryCode) {
			context.Cascades = append(context.Cascades, item)
		}
	}
	for _, item := range toMapSlice(bundle.ResultOverlays["roles"]) {
		if roleTargetsMatch(item, append([]string{req.OverlayID}, targetRefs...)) {
			context.RoleReadouts = append(context.RoleReadouts, item)
		}
	}
	return context, nil
}

func overlayRelationMatches(item map[string]any, overlayID string, targetRefs []string, productPath string, targetCountryCode string) bool {
	if item == nil {
		return false
	}
	if strings.EqualFold(simStringField(item, "id"), overlayID) {
		return true
	}
	if strings.EqualFold(simStringField(item, "sourceId"), overlayID) || strings.EqualFold(simStringField(item, "targetId"), overlayID) {
		return true
	}
	if anyFoldIntersect(simStringSliceField(item, "targetRefs"), targetRefs) {
		return true
	}
	if productPath != "" && strings.EqualFold(simStringField(item, "productPath"), productPath) {
		return true
	}
	if targetCountryCode != "" && strings.EqualFold(simStringField(item, "targetCountryCode"), targetCountryCode) {
		return true
	}
	for _, signalRef := range simStringSliceField(item, "signalRefs") {
		if strings.EqualFold(signalRef, overlayID) {
			return true
		}
	}
	return false
}

func simulationExplanationSystemInstruction() string {
	return strings.TrimSpace(`
You explain one selected simulation stress-factor.
You must use only the provided runtime facts. Do not invent new actors, routes, products, or metrics.
Your job is to convert trace-backed simulation facts into a concise business explanation in the same discipline-first style as the runtime:
- stress driver
- affected objects and links
- cascade risks
- role implications
- recommended actions
Return STRICT JSON only matching the schema.
`)
}

func buildSimulationExplanationPrompt(run *horizonmodels.SimulationRun, bundle *horizonmodels.SimulationBundle, ctx *simulationExplanationContext, req simulationExplanationRequest) string {
	payload := map[string]any{
		"run": map[string]any{
			"id":          strings.TrimSpace(run.ID),
			"question":    strings.TrimSpace(run.Request.Question),
			"lensId":      strings.TrimSpace(run.Request.LensID),
			"status":      strings.TrimSpace(run.Status),
			"domainModes": run.Request.DomainModes,
		},
		"selectedOverlay":    ctx.Item,
		"matchedArtifactIds": req.MatchedArtifactIDs,
		"relatedPathChains":  ctx.PathChains,
		"relatedCascades":    ctx.Cascades,
		"relatedRoles":       ctx.RoleReadouts,
		"runSummary":         bundle.RunSummary,
		"sensitivity":        bundle.Sensitivity,
		"disciplinedSignals": bundle.DisciplinedSignals,
		"functionExposures":  bundle.FunctionExposures,
		"decisionDemands":    bundle.DecisionDemands,
		"monitoringSignals":  bundle.MonitoringSignals,
		"actorReactions":     bundle.ActorReactions,
		"mechanismChains":    bundle.MechanismChains,
		"protocolSummary":    bundle.ProtocolSummary,
		"transactionSummary": bundle.TransactionSummary,
	}
	raw, _ := json.MarshalIndent(payload, "", "  ")
	return strings.TrimSpace(`
Explain the selected simulation stress-factor for a business operator.

Rules:
1. Explain only the selected factor and directly related cascades and roles.
2. Use the same logic as the simulation runtime:
   - route/shoulder/product/path-chain stress
   - dominant mechanisms and transaction/protocol pressure
   - function and role implications
3. Do not generalize beyond the supplied facts.
4. Keep the explanation concrete and executive-readable.
5. "Affected links" must describe visible stressed connections, not abstract policy.
6. "Key metrics" must come from the supplied facts and be rendered as short label/value strings.

FACTS:
` + string(raw))
}

func simulationExplanationSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"title":   map[string]any{"type": "string"},
			"summary": map[string]any{"type": "string"},
			"stressNarrative": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
			"affectedObjects": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
			"affectedLinks": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
			"cascadeRisks": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
			"roleImplications": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"role":        map[string]any{"type": "string"},
						"implication": map[string]any{"type": "string"},
					},
					"required": []string{"role", "implication"},
				},
			},
			"recommendedActions": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
			"keyMetrics": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"label": map[string]any{"type": "string"},
						"value": map[string]any{"type": "string"},
					},
					"required": []string{"label", "value"},
				},
			},
		},
		"required": []string{
			"title",
			"summary",
			"stressNarrative",
			"affectedObjects",
			"affectedLinks",
			"cascadeRisks",
			"roleImplications",
			"recommendedActions",
			"keyMetrics",
		},
	}
}
