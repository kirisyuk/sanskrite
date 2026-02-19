package horizon

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

const (
	graphIntentPlannerModel = "gemini-3-flash-preview"

	graphIntentApplyHardMaxCreateNodes = 200
	graphIntentApplyHardMaxCreateEdges = 400
	graphIntentApplyHardMaxRewriteOps  = 250
)

// PlanGraphIntent handles:
// POST /api/horizon/lenses/{lensId}/graph-intent/plan
func (h *HorizonHandlers) PlanGraphIntent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	authInfo, err := requireAuth(r)
	if err != nil || authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	lens, err := h.resolveGraphIntentLens(r.Context(), r, authInfo.TenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if h.genAIHandler == nil {
		http.Error(w, "GenAI handler not configured", http.StatusServiceUnavailable)
		return
	}

	var req horizonmodels.GraphIntentPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if err := req.NormalizeAndValidate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Contract: execution stays in English.
	req.ExecutionLanguage = horizonmodels.GraphIntentExecutionLanguageEN

	plan, err := h.generateGraphIntentPlan(r.Context(), lens, &req)
	if err != nil {
		http.Error(w, "Failed to generate graph intent plan", http.StatusInternalServerError)
		return
	}
	if plan == nil {
		http.Error(w, "Empty graph intent plan", http.StatusInternalServerError)
		return
	}
	normalizeGraphIntentPlanLoose(plan)
	if err := plan.NormalizeAndValidate(); err != nil {
		http.Error(w, "Generated plan is invalid", http.StatusInternalServerError)
		return
	}
	if plan.IsPotentiallyDestructive() {
		plan.RequiresConfirmation = true
	}
	createdNodes, createdEdges := plan.EstimatedCreateCounts()
	if createdNodes > req.Options.MaxNewNodes {
		plan.Warnings = appendUniqueStrings(plan.Warnings, fmt.Sprintf("plan exceeds maxNewNodes: %d > %d", createdNodes, req.Options.MaxNewNodes))
		plan.RequiresConfirmation = true
	}
	if createdEdges > req.Options.MaxNewEdges {
		plan.Warnings = appendUniqueStrings(plan.Warnings, fmt.Sprintf("plan exceeds maxNewEdges: %d > %d", createdEdges, req.Options.MaxNewEdges))
		plan.RequiresConfirmation = true
	}

	writeGraphIntentJSON(w, http.StatusOK, plan)
}

// ApplyGraphIntent handles:
// POST /api/horizon/lenses/{lensId}/graph-intent/apply
func (h *HorizonHandlers) ApplyGraphIntent(w http.ResponseWriter, r *http.Request) {
	h.applyGraphIntent(w, r, false)
}

// DryRunGraphIntent handles:
// POST /api/horizon/lenses/{lensId}/graph-intent/dry-run
func (h *HorizonHandlers) DryRunGraphIntent(w http.ResponseWriter, r *http.Request) {
	h.applyGraphIntent(w, r, true)
}

func (h *HorizonHandlers) applyGraphIntent(w http.ResponseWriter, r *http.Request, forceDryRun bool) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	authInfo, err := requireAuth(r)
	if err != nil || authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	lens, err := h.resolveGraphIntentLens(r.Context(), r, authInfo.TenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var req horizonmodels.GraphIntentApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if forceDryRun {
		req.ApplyMode = horizonmodels.GraphIntentApplyModeDry
	}
	if err := req.NormalizeAndValidate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	normalizeGraphIntentPlanLoose(&req.Plan)
	if err := req.Plan.NormalizeAndValidate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	createdNodes, createdEdges := req.Plan.EstimatedCreateCounts()
	if createdNodes > graphIntentApplyHardMaxCreateNodes {
		http.Error(w, fmt.Sprintf("create nodes exceed apply limit: %d > %d", createdNodes, graphIntentApplyHardMaxCreateNodes), http.StatusBadRequest)
		return
	}
	if createdEdges > graphIntentApplyHardMaxCreateEdges {
		http.Error(w, fmt.Sprintf("create edges exceed apply limit: %d > %d", createdEdges, graphIntentApplyHardMaxCreateEdges), http.StatusBadRequest)
		return
	}

	isDryRun := req.ApplyMode == horizonmodels.GraphIntentApplyModeDry
	if !isDryRun && (req.Plan.RequiresConfirmation || req.Plan.IsPotentiallyDestructive()) && !graphIntentApplyConfirmed(r) {
		resp := horizonmodels.GraphIntentApplyResponse{
			Applied:       false,
			Warnings:      []string{"confirmation required for potentially destructive plan; resend with confirm=true"},
			ActionPreview: req.Plan.MapToActions(),
		}
		writeGraphIntentJSON(w, http.StatusConflict, resp)
		return
	}

	exec := &graphIntentApplyExecutor{
		handler:              h,
		ctx:                  r.Context(),
		lens:                 lens,
		dryRun:               isDryRun,
		maxRewriteTouchCount: graphIntentApplyHardMaxRewriteOps,
	}
	if err := exec.init(); err != nil {
		http.Error(w, "Graph intent apply is not available", http.StatusServiceUnavailable)
		return
	}

	resp := exec.execute(&req.Plan)
	resp.ActionPreview = req.Plan.MapToActions()
	resp.Applied = !isDryRun && len(resp.UnresolvedItems) == 0
	if isDryRun {
		resp.Applied = false
	}
	writeGraphIntentJSON(w, http.StatusOK, resp)
}

func (h *HorizonHandlers) resolveGraphIntentLens(ctx context.Context, r *http.Request, tenantID string) (*horizonmodels.Lens, error) {
	lensID := strings.TrimSpace(extractIDFromPath(r, "/api/horizon/lenses/"))
	if lensID == "" {
		return nil, fmt.Errorf("lensId is required in path")
	}
	if h.service == nil || h.service.Lens() == nil {
		return nil, fmt.Errorf("horizon service is not configured")
	}
	lens, err := h.service.Lens().GetLens(ctx, lensID)
	if err != nil || lens == nil {
		return nil, fmt.Errorf("lens not found")
	}
	if strings.TrimSpace(lens.TenantID) == "" || strings.TrimSpace(lens.TenantID) != strings.TrimSpace(tenantID) {
		return nil, fmt.Errorf("access denied")
	}
	return lens, nil
}

func (h *HorizonHandlers) generateGraphIntentPlan(ctx context.Context, lens *horizonmodels.Lens, req *horizonmodels.GraphIntentPlanRequest) (*horizonmodels.GraphIntentPlanResponse, error) {
	if h == nil || h.genAIHandler == nil || req == nil || lens == nil {
		return nil, fmt.Errorf("planner is unavailable")
	}
	snapshot := compactGraphIntentSnapshot(req.Snapshot)
	allowedRelations := horizonmodels.GraphIntentAllowedRelationTypesSorted()
	allowedNodeTypes := horizonmodels.GraphIntentAllowedNodeTypesSorted()
	allowedGroupingKinds := horizonmodels.GraphIntentAllowedGroupingKindsSorted()
	schema := horizonmodels.GraphIntentPlanResponseSchemaDraft07()
	schemaJSON, _ := json.Marshal(schema)
	snapshotJSON, _ := json.Marshal(snapshot)

	systemInstruction := strings.TrimSpace(`
You are a Graph Intent planner for a knowledge graph editor.
Return ONLY valid JSON.

Rules:
- The user query may be in any language.
- normalizedEnglishQuery MUST be in English.
- You must reason over the provided graph snapshot (current visible graph).
- Do NOT run or request scouting.
- Output a structured plan only.
- Use ONLY allowed groupingKind, node types, and relation types from the prompt.
- Keep plan conservative: if uncertain, lower confidence and add warnings.
- If operation can be destructive (broad relation rewrites), set requiresConfirmation=true.
- Keep output compact and executable.
`)

	prompt := strings.TrimSpace(fmt.Sprintf(`
Now (UTC): %s
Contract version: %s
Execution language (must be used for planning): %s

Lens context:
- lensId: %s
- projectId: %s

Request:
- rawQuery: %s
- queryLanguage: %s
- options:
  - allowSearchGrounding: %t
  - allowInternalGrounding: %t
  - maxNewNodes: %d
  - maxNewEdges: %d

Allowed grouping kinds:
%s

Allowed node types:
%s

Allowed relation types:
%s

Graph snapshot JSON:
%s

Output JSON schema (draft-07):
%s

Return one JSON object matching the schema.
`, time.Now().UTC().Format(time.RFC3339), horizonmodels.GraphIntentVersionV1, horizonmodels.GraphIntentExecutionLanguageEN, lens.ID, lens.ProjectID, req.RawQuery, req.QueryLanguage, req.Options.AllowSearchGrounding, req.Options.AllowInternalGrounding, req.Options.MaxNewNodes, req.Options.MaxNewEdges, strings.Join(allowedGroupingKinds, ", "), strings.Join(allowedNodeTypes, ", "), strings.Join(allowedRelations, ", "), string(snapshotJSON), string(schemaJSON)))

	llmReq := gen_combined.GenAIRequest{
		Model:             graphIntentPlannerModel,
		Prompt:            prompt,
		SystemInstruction: systemInstruction,
		WebSearchEnabled:  req.Options.AllowSearchGrounding,
		MaxOutputTokens:   4096,
		Temperature:       0.1,
		ProjectID:         strings.TrimSpace(lens.ProjectID),
	}

	genResp, err := h.genAIHandler.GenerateDirect(ctx, llmReq)
	if err != nil || genResp == nil {
		return nil, fmt.Errorf("planner call failed")
	}

	ver := ai.NewGenericVerifier(func(out horizonmodels.GraphIntentPlanResponse) error {
		normalizeGraphIntentPlanLoose(&out)
		if err := out.NormalizeAndValidate(); err != nil {
			return err
		}
		return nil
	})
	parsedAny, err := ver.VerifyAndParse(genResp.Content)
	if err != nil {
		llmReq.SystemInstruction = strings.TrimSpace(systemInstruction + "\n\nCRITICAL: Return ONLY a single JSON object. No markdown. No extra text.")
		genResp2, err2 := h.genAIHandler.GenerateDirect(ctx, llmReq)
		if err2 != nil || genResp2 == nil {
			return nil, fmt.Errorf("planner output validation failed")
		}
		parsedAny, err = ver.VerifyAndParse(genResp2.Content)
		if err != nil {
			return nil, fmt.Errorf("planner output validation failed")
		}
	}
	plan := parsedAny.(horizonmodels.GraphIntentPlanResponse)
	normalizeGraphIntentPlanLoose(&plan)

	if strings.TrimSpace(plan.NormalizedEnglishQuery) == "" {
		plan.NormalizedEnglishQuery = strings.TrimSpace(req.RawQuery)
	}
	if containsCyrillic(plan.NormalizedEnglishQuery) {
		plan.Warnings = appendUniqueStrings(plan.Warnings, "normalizedEnglishQuery may not be fully in English")
	}
	if plan.IntentClass == horizonmodels.GraphIntentClassUnknown {
		plan.IntentClass = inferGraphIntentClass(&plan)
	}
	if plan.Confidence <= 0 {
		plan.Confidence = 0.35
	}
	if plan.IsPotentiallyDestructive() {
		plan.RequiresConfirmation = true
	}
	return &plan, nil
}

func inferGraphIntentClass(plan *horizonmodels.GraphIntentPlanResponse) string {
	if plan == nil {
		return horizonmodels.GraphIntentClassUnknown
	}
	hasCreate := len(plan.Create.Groupings)+len(plan.Create.Nodes)+len(plan.Create.Edges) > 0
	hasUpdate := len(plan.Update.NodePatches)+len(plan.Update.EdgePatches)+len(plan.Update.RelationRewrite) > 0
	hasTransform := len(plan.Transform.DeriveChains)+len(plan.Transform.Restructure) > 0
	switch {
	case hasCreate && !hasUpdate && !hasTransform:
		return horizonmodels.GraphIntentClassCreate
	case !hasCreate && hasUpdate && !hasTransform:
		return horizonmodels.GraphIntentClassUpdate
	case !hasCreate && !hasUpdate && hasTransform:
		return horizonmodels.GraphIntentClassTransform
	case hasCreate || hasUpdate || hasTransform:
		return horizonmodels.GraphIntentClassMixed
	default:
		return horizonmodels.GraphIntentClassUnknown
	}
}

func compactGraphIntentSnapshot(in horizonmodels.GraphIntentSnapshot) horizonmodels.GraphIntentSnapshot {
	const maxNodes = 220
	const maxEdges = 500
	out := in
	if len(in.Nodes) <= maxNodes && len(in.Edges) <= maxEdges {
		return out
	}

	selected := make(map[string]struct{}, len(in.SelectedNodeIDs))
	for _, id := range in.SelectedNodeIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			selected[id] = struct{}{}
		}
	}
	visible := make(map[string]struct{}, len(in.VisibleNodeIDs))
	for _, id := range in.VisibleNodeIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			visible[id] = struct{}{}
		}
	}

	nodeByID := make(map[string]horizonmodels.GraphIntentSnapshotNode, len(in.Nodes))
	nodeIDs := make([]string, 0, len(in.Nodes))
	for _, n := range in.Nodes {
		id := strings.TrimSpace(n.NodeID)
		if id == "" {
			continue
		}
		if _, ok := nodeByID[id]; ok {
			continue
		}
		nodeByID[id] = n
		nodeIDs = append(nodeIDs, id)
	}

	keep := make(map[string]struct{}, maxNodes)
	addID := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if len(keep) >= maxNodes {
			return
		}
		if _, ok := nodeByID[id]; !ok {
			return
		}
		keep[id] = struct{}{}
	}
	for _, id := range in.SelectedNodeIDs {
		addID(id)
	}
	for _, id := range in.VisibleNodeIDs {
		addID(id)
	}
	for _, id := range nodeIDs {
		addID(id)
	}

	out.Nodes = make([]horizonmodels.GraphIntentSnapshotNode, 0, len(keep))
	for _, id := range nodeIDs {
		if _, ok := keep[id]; !ok {
			continue
		}
		out.Nodes = append(out.Nodes, nodeByID[id])
	}

	out.Edges = make([]horizonmodels.GraphIntentSnapshotEdge, 0, minInt(len(in.Edges), maxEdges))
	for _, e := range in.Edges {
		sid := strings.TrimSpace(e.SourceNodeID)
		tid := strings.TrimSpace(e.TargetNodeID)
		if sid == "" || tid == "" {
			continue
		}
		if _, ok := keep[sid]; !ok {
			continue
		}
		if _, ok := keep[tid]; !ok {
			continue
		}
		out.Edges = append(out.Edges, e)
		if len(out.Edges) >= maxEdges {
			break
		}
	}
	return out
}

func normalizeGraphIntentPlanLoose(plan *horizonmodels.GraphIntentPlanResponse) {
	if plan == nil {
		return
	}
	plan.Version = strings.TrimSpace(plan.Version)
	plan.IntentClass = strings.ToLower(strings.TrimSpace(plan.IntentClass))
	if plan.IntentClass == "" {
		plan.IntentClass = inferGraphIntentClass(plan)
	}
	for gi := range plan.Create.Groupings {
		g := &plan.Create.Groupings[gi]
		g.GroupingKind = strings.ToLower(strings.TrimSpace(g.GroupingKind))
		for ni := range g.Entities {
			n := &g.Entities[ni]
			n.Type = strings.ToUpper(strings.TrimSpace(n.Type))
			n.Label = strings.TrimSpace(n.Label)
			n.CanonicalPathHint = strings.TrimSpace(n.CanonicalPathHint)
		}
		for ei := range g.Relations {
			e := &g.Relations[ei]
			e.RelationType = strings.ToUpper(strings.TrimSpace(e.RelationType))
			e.Direction = strings.ToLower(strings.TrimSpace(e.Direction))
			if e.Direction == "" {
				e.Direction = "forward"
			}
		}
	}
	for ni := range plan.Create.Nodes {
		n := &plan.Create.Nodes[ni]
		n.Type = strings.ToUpper(strings.TrimSpace(n.Type))
		n.Label = strings.TrimSpace(n.Label)
		n.CanonicalPathHint = strings.TrimSpace(n.CanonicalPathHint)
	}
	for ei := range plan.Create.Edges {
		e := &plan.Create.Edges[ei]
		e.RelationType = strings.ToUpper(strings.TrimSpace(e.RelationType))
		e.Direction = strings.ToLower(strings.TrimSpace(e.Direction))
		if e.Direction == "" {
			e.Direction = "forward"
		}
	}
	for i := range plan.Update.NodePatches {
		np := &plan.Update.NodePatches[i]
		if np.Type != nil {
			v := strings.ToUpper(strings.TrimSpace(*np.Type))
			np.Type = &v
		}
	}
	for i := range plan.Update.EdgePatches {
		ep := &plan.Update.EdgePatches[i]
		if ep.RelationType != nil {
			v := strings.ToUpper(strings.TrimSpace(*ep.RelationType))
			ep.RelationType = &v
		}
		if ep.Direction != nil {
			v := strings.ToLower(strings.TrimSpace(*ep.Direction))
			ep.Direction = &v
		}
	}
	for i := range plan.Update.RelationRewrite {
		rw := &plan.Update.RelationRewrite[i]
		rw.Match.RelationType = strings.ToUpper(strings.TrimSpace(rw.Match.RelationType))
		rw.ReplaceWith.RelationType = strings.ToUpper(strings.TrimSpace(rw.ReplaceWith.RelationType))
		rw.ReplaceWith.Direction = strings.ToLower(strings.TrimSpace(rw.ReplaceWith.Direction))
		if rw.ReplaceWith.Direction == "" {
			rw.ReplaceWith.Direction = "forward"
		}
	}
	for i := range plan.Transform.DeriveChains {
		ch := &plan.Transform.DeriveChains[i]
		for j := range ch.Constraints.AllowedRelationTypes {
			ch.Constraints.AllowedRelationTypes[j] = strings.ToUpper(strings.TrimSpace(ch.Constraints.AllowedRelationTypes[j]))
		}
	}
	plan.Warnings = appendUniqueStrings(nil, plan.Warnings...)
	plan.Explanations = appendUniqueStrings(nil, plan.Explanations...)
}

type graphIntentApplyExecutor struct {
	handler *HorizonHandlers
	ctx     context.Context
	lens    *horizonmodels.Lens
	dryRun  bool

	fragmentsRepo *repo.HorizonFragmentsRepository
	edgesRepo     *repo.DependencyEdgesRepository
	lensItemsRepo *repo.LensItemsRepository
	lensesRepo    *repo.LensesRepository

	knownFragmentRefs map[string]string
	tempFragmentRefs  map[string]string

	nodeItemByFragmentID map[string]*horizonmodels.LensItem
	edgeItemsByPair      map[string][]*horizonmodels.LensItem

	createdScopeCanonicalPaths []string
	warnings                   []string
	unresolved                 []horizonmodels.GraphIntentUnresolvedItem

	createdNodeCount int
	createdEdgeCount int
	updatedNodeCount int
	updatedEdgeCount int

	maxRewriteTouchCount int
}

func (e *graphIntentApplyExecutor) init() error {
	if e == nil || e.handler == nil || e.handler.service == nil {
		return fmt.Errorf("handler unavailable")
	}
	if e.handler.service.Fragments() == nil || e.handler.service.Edges() == nil {
		return fmt.Errorf("repositories unavailable")
	}
	e.fragmentsRepo = e.handler.service.Fragments()
	e.edgesRepo = e.handler.service.Edges()
	e.knownFragmentRefs = map[string]string{}
	e.tempFragmentRefs = map[string]string{}
	e.nodeItemByFragmentID = map[string]*horizonmodels.LensItem{}
	e.edgeItemsByPair = map[string][]*horizonmodels.LensItem{}
	if e.handler.lensItemsHandler != nil {
		e.lensItemsRepo = e.handler.lensItemsHandler.lensItemsRepo
		e.lensesRepo = e.handler.lensItemsHandler.lensesRepo
	}
	e.seedKnownFragmentsFromLensScope()
	e.seedKnownFragmentsFromLensItems()
	return nil
}

func (e *graphIntentApplyExecutor) execute(plan *horizonmodels.GraphIntentPlanResponse) horizonmodels.GraphIntentApplyResponse {
	if plan == nil {
		return horizonmodels.GraphIntentApplyResponse{
			Applied:  false,
			Warnings: []string{"plan is empty"},
		}
	}

	for _, g := range plan.Create.Groupings {
		for _, n := range g.Entities {
			e.applyCreateNode(n)
		}
		for _, rel := range g.Relations {
			e.applyCreateEdge(rel)
		}
	}
	for _, n := range plan.Create.Nodes {
		e.applyCreateNode(n)
	}
	for _, rel := range plan.Create.Edges {
		e.applyCreateEdge(rel)
	}

	for _, patch := range plan.Update.NodePatches {
		e.applyNodePatch(patch)
	}
	for _, patch := range plan.Update.EdgePatches {
		e.applyEdgePatch(patch)
	}
	for _, rw := range plan.Update.RelationRewrite {
		e.applyRelationRewrite(rw)
	}

	for _, dc := range plan.Transform.DeriveChains {
		if strings.TrimSpace(dc.FromNodeID) != "" && strings.TrimSpace(dc.ToNodeID) != "" {
			e.warnings = appendUniqueStrings(e.warnings, "transform.deriveChains is non-mutating in apply mode")
			break
		}
	}
	if len(plan.Transform.Restructure) > 0 {
		e.warnings = appendUniqueStrings(e.warnings, "transform.restructure is not executed yet; dry-run only")
	}

	if !e.dryRun && len(e.createdScopeCanonicalPaths) > 0 && e.lensesRepo != nil {
		if err := e.lensesRepo.AddScopeCanonicalPaths(e.ctx, e.lens.ID, e.createdScopeCanonicalPaths); err != nil {
			e.warnings = appendUniqueStrings(e.warnings, "failed to update lens scope with created nodes")
		}
	}

	return horizonmodels.GraphIntentApplyResponse{
		Applied:          !e.dryRun && len(e.unresolved) == 0,
		CreatedNodeCount: e.createdNodeCount,
		CreatedEdgeCount: e.createdEdgeCount,
		UpdatedNodeCount: e.updatedNodeCount,
		UpdatedEdgeCount: e.updatedEdgeCount,
		UnresolvedItems:  e.unresolved,
		Warnings:         e.warnings,
	}
}

func (e *graphIntentApplyExecutor) seedKnownFragmentsFromLensScope() {
	if e == nil || e.fragmentsRepo == nil || e.lens == nil || len(e.lens.Scope) == 0 {
		return
	}
	frags, err := e.fragmentsRepo.ListByCanonicalPaths(e.ctx, e.lens.Scope)
	if err != nil {
		e.warnings = appendUniqueStrings(e.warnings, "failed to resolve lens scope fragments")
		return
	}
	for _, f := range frags {
		if f == nil {
			continue
		}
		e.addKnownFragmentRef(strings.TrimSpace(f.ID), strings.TrimSpace(f.ID))
		e.addKnownFragmentRef(strings.TrimSpace(f.CanonicalPath), strings.TrimSpace(f.ID))
	}
}

func (e *graphIntentApplyExecutor) seedKnownFragmentsFromLensItems() {
	if e == nil || e.lensItemsRepo == nil || e.lens == nil {
		return
	}
	items, err := e.lensItemsRepo.ListByLens(e.ctx, e.lens.ID)
	if err != nil {
		e.warnings = appendUniqueStrings(e.warnings, "failed to load lens items for graph-intent apply")
		return
	}
	for _, it := range items {
		if it == nil {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(it.Kind))
		switch kind {
		case "node":
			fid := strings.TrimSpace(stringPtrValue(it.FragmentID))
			if fid != "" {
				e.addKnownFragmentRef(fid, fid)
				e.addKnownFragmentRef(strings.TrimSpace(it.ID), fid)
				if it.CanonicalPath != nil && strings.TrimSpace(*it.CanonicalPath) != "" {
					e.addKnownFragmentRef(strings.TrimSpace(*it.CanonicalPath), fid)
				}
				e.nodeItemByFragmentID[fid] = it
			}
		case "edge":
			src := strings.TrimSpace(stringPtrValue(it.SourceFragmentID))
			dst := strings.TrimSpace(stringPtrValue(it.TargetFragmentID))
			if src == "" || dst == "" {
				continue
			}
			key := edgePairKey(src, dst)
			e.edgeItemsByPair[key] = append(e.edgeItemsByPair[key], it)
		}
	}
}

func (e *graphIntentApplyExecutor) applyCreateNode(spec horizonmodels.GraphIntentNodeSpec) {
	label := strings.TrimSpace(spec.Label)
	nodeType := strings.ToUpper(strings.TrimSpace(spec.Type))
	if label == "" || nodeType == "" {
		e.unresolved = append(e.unresolved, horizonmodels.GraphIntentUnresolvedItem{
			TempID:          strings.TrimSpace(spec.TempID),
			Reason:          "invalid node spec (label/type required)",
			SuggestedAction: "fix_plan",
		})
		return
	}
	if !horizonmodels.IsGraphIntentNodeTypeAllowed(nodeType) {
		e.unresolved = append(e.unresolved, horizonmodels.GraphIntentUnresolvedItem{
			TempID:          strings.TrimSpace(spec.TempID),
			Reason:          fmt.Sprintf("unsupported node type: %s", nodeType),
			SuggestedAction: "fix_plan",
		})
		return
	}

	canonicalPath := e.normalizeCanonicalPathHint(spec)
	if canonicalPath != "" {
		if existing, err := e.fragmentsRepo.GetByCanonicalPath(e.ctx, canonicalPath); err == nil && existing != nil && strings.TrimSpace(existing.ID) != "" {
			e.bindTempNodeRef(spec, strings.TrimSpace(existing.ID), canonicalPath)
			return
		}
	}

	fragType := horizonmodels.MapGraphIntentNodeTypeToFragmentType(nodeType, canonicalPath)
	state := toMetricState(spec.State)
	now := time.Now().UTC()
	fragment := &horizonmodels.HorizonFragment{
		TenantID:      "global",
		Type:          fragType,
		CanonicalPath: canonicalPath,
		Labels:        map[string]string{"en": label},
		Status:        "active",
		State:         state,
		Provenance: horizonmodels.Provenance{
			Method:    "graph-intent.apply",
			SourceIDs: []string{strings.TrimSpace(e.lens.ID)},
		},
		Metadata: horizonmodels.FragmentMetadata{
			CreatedAt:       now,
			LastUpdatedAt:   now,
			LastVerifiedAt:  now,
			SourceFrameID:   "graph-intent",
			EpistemicStatus: horizonmodels.EpistemicStatusLowConfidence,
		},
		Governance: horizonmodels.Governance{
			Owner:              "SYSTEM",
			Status:             string(horizonmodels.FragmentStatusActive),
			ActiveSubscribers:  0,
			CreatedByProjectID: strPtr(strings.TrimSpace(e.lens.ProjectID)),
		},
	}
	if desc := stringValueFromMap(spec.Metadata, "description"); desc != "" {
		fragment.Description = desc
	}
	if tags := stringSliceFromAny(spec.Metadata["tags"]); len(tags) > 0 {
		fragment.Tags = tags
	}

	if e.dryRun {
		synthID := strings.TrimSpace(spec.TempID)
		if synthID == "" {
			synthID = "dry-node-" + strconv.Itoa(len(e.tempFragmentRefs)+1)
		}
		e.bindTempNodeRef(spec, synthID, canonicalPath)
		e.createdNodeCount++
		return
	}

	id, err := e.fragmentsRepo.Create(e.ctx, fragment)
	if err != nil || strings.TrimSpace(id) == "" {
		e.unresolved = append(e.unresolved, horizonmodels.GraphIntentUnresolvedItem{
			TempID:          strings.TrimSpace(spec.TempID),
			Reason:          "failed to create fragment",
			SuggestedAction: "fix_plan",
		})
		return
	}
	e.bindTempNodeRef(spec, id, canonicalPath)
	e.createdNodeCount++
	if canonicalPath != "" {
		e.createdScopeCanonicalPaths = append(e.createdScopeCanonicalPaths, canonicalPath)
	}
	e.ensureNodeLensItem(id, canonicalPath, label, string(fragType))
}

func (e *graphIntentApplyExecutor) applyCreateEdge(spec horizonmodels.GraphIntentEdgeSpec) {
	fromID, fromOK := e.resolveEdgeEndpoint(spec.FromTempID, spec.FromNodeID)
	toID, toOK := e.resolveEdgeEndpoint(spec.ToTempID, spec.ToNodeID)
	if !fromOK || !toOK {
		e.unresolved = append(e.unresolved, horizonmodels.GraphIntentUnresolvedItem{
			TempID:          strings.TrimSpace(spec.TempID),
			Reason:          "edge endpoint unresolved",
			SuggestedAction: "fix_plan",
		})
		return
	}
	if fromID == toID {
		e.unresolved = append(e.unresolved, horizonmodels.GraphIntentUnresolvedItem{
			TempID:          strings.TrimSpace(spec.TempID),
			Reason:          "edge endpoints must be different",
			SuggestedAction: "fix_plan",
		})
		return
	}
	depType, aliasQualifiers, aliasWarning, ok := horizonmodels.NormalizeGraphIntentRelationType(spec.RelationType)
	if !ok {
		e.unresolved = append(e.unresolved, horizonmodels.GraphIntentUnresolvedItem{
			TempID:          strings.TrimSpace(spec.TempID),
			Reason:          fmt.Sprintf("unsupported relation type: %s", strings.TrimSpace(spec.RelationType)),
			SuggestedAction: "fix_plan",
		})
		return
	}
	if aliasWarning != "" {
		e.warnings = appendUniqueStrings(e.warnings, aliasWarning)
	}

	sourceID := fromID
	targetID := toID
	direction := strings.ToLower(strings.TrimSpace(spec.Direction))
	if direction == "" {
		direction = "forward"
	}
	switch direction {
	case "reverse":
		sourceID, targetID = toID, fromID
	case "bidirectional", "forward":
	default:
		direction = "forward"
	}

	if !e.dryRun {
		if existing, err := e.edgesRepo.GetBySourceTargetAndType(e.ctx, strings.TrimSpace(e.lens.TenantID), sourceID, targetID, depType); err == nil && existing != nil && strings.TrimSpace(existing.ID) != "" {
			// Ensure the visual edge exists in lens items even when dependency edge already exists.
			e.ensureEdgeLensItem(sourceID, targetID, string(depType), direction, existing.Properties.Criticality, existing.Properties.Amplification, existing.Properties.LatencyHours)
			return
		}
	}

	criticality := 0.6
	if spec.Confidence > 0 {
		criticality = clampFloat(spec.Confidence, 0.15, 0.95)
	}
	qualifiers := cloneAnyMap(spec.Qualifiers)
	for k, v := range aliasQualifiers {
		qualifiers[k] = v
	}
	if direction == "bidirectional" {
		qualifiers["direction"] = "bidirectional"
	}
	metrics := toMetricState(spec.Metrics)
	edge := &horizonmodels.DependencyEdge{
		TenantID:         strings.TrimSpace(e.lens.TenantID),
		SourceFragmentID: sourceID,
		TargetFragmentID: targetID,
		Type:             depType,
		Properties: horizonmodels.EdgeProperties{
			Criticality:   criticality,
			Amplification: 1.0,
			LatencyHours:  0,
		},
		Qualifiers: qualifiers,
		Provenance: horizonmodels.Provenance{
			Method:    "graph-intent.apply",
			SourceIDs: []string{strings.TrimSpace(e.lens.ID)},
		},
		Metadata: horizonmodels.EdgeMetadata{
			SourceFrameID: "graph-intent",
		},
		Metrics: metrics,
	}

	if e.dryRun {
		e.createdEdgeCount++
		return
	}
	id, err := e.edgesRepo.Create(e.ctx, edge)
	if err != nil || strings.TrimSpace(id) == "" {
		e.unresolved = append(e.unresolved, horizonmodels.GraphIntentUnresolvedItem{
			TempID:          strings.TrimSpace(spec.TempID),
			Reason:          "failed to create dependency edge",
			SuggestedAction: "fix_plan",
		})
		return
	}
	e.createdEdgeCount++
	e.ensureEdgeLensItem(sourceID, targetID, string(depType), direction, edge.Properties.Criticality, edge.Properties.Amplification, edge.Properties.LatencyHours)
}

func (e *graphIntentApplyExecutor) applyNodePatch(patch horizonmodels.GraphIntentNodePatch) {
	ref := firstNonEmptyTrimmed(patch.FragmentID, patch.NodeID, patch.TempID, patch.CanonicalPath)
	fragmentID, ok := e.resolveNodeRef(ref)
	if !ok {
		e.unresolved = append(e.unresolved, horizonmodels.GraphIntentUnresolvedItem{
			TempID:          strings.TrimSpace(patch.PatchID),
			Reason:          "node patch target unresolved",
			SuggestedAction: "fix_plan",
		})
		return
	}
	fragment, err := e.fragmentsRepo.Get(e.ctx, fragmentID)
	if err != nil || fragment == nil {
		e.unresolved = append(e.unresolved, horizonmodels.GraphIntentUnresolvedItem{
			TempID:          strings.TrimSpace(patch.PatchID),
			Reason:          "node patch target fragment not found",
			SuggestedAction: "fix_plan",
		})
		return
	}

	changed := false
	if patch.Label != nil && strings.TrimSpace(*patch.Label) != "" {
		if fragment.Labels == nil {
			fragment.Labels = map[string]string{}
		}
		if fragment.Labels["en"] != strings.TrimSpace(*patch.Label) {
			fragment.Labels["en"] = strings.TrimSpace(*patch.Label)
			changed = true
		}
	}
	if patch.Type != nil && strings.TrimSpace(*patch.Type) != "" {
		targetType := horizonmodels.MapGraphIntentNodeTypeToFragmentType(strings.TrimSpace(*patch.Type), fragment.CanonicalPath)
		if fragment.Type != targetType && targetType != "" {
			fragment.Type = targetType
			changed = true
		}
	}
	if len(patch.StatePatch) > 0 {
		if fragment.State == nil {
			fragment.State = map[string]horizonmodels.MetricValue{}
		}
		now := time.Now().UTC()
		for k, raw := range patch.StatePatch {
			if strings.TrimSpace(k) == "" {
				continue
			}
			val, ok := toFloat(raw)
			if !ok {
				continue
			}
			metric := horizonmodels.MetricValue{
				Value:      val,
				Confidence: 0.55,
				ObservedAt: now,
				Provenance: horizonmodels.Provenance{Method: "graph-intent.apply"},
			}
			fragment.State[k] = metric
			changed = true
		}
	}
	if len(patch.MetadataPatch) > 0 {
		if desc := stringValueFromMap(patch.MetadataPatch, "description"); desc != "" {
			if fragment.Description != desc {
				fragment.Description = desc
				changed = true
			}
		}
		if tags := stringSliceFromAny(patch.MetadataPatch["tags"]); len(tags) > 0 {
			fragment.Tags = tags
			changed = true
		}
	}
	if !changed {
		return
	}
	if e.dryRun {
		e.updatedNodeCount++
		return
	}
	if err := e.fragmentsRepo.Update(e.ctx, fragment); err != nil {
		e.unresolved = append(e.unresolved, horizonmodels.GraphIntentUnresolvedItem{
			TempID:          strings.TrimSpace(patch.PatchID),
			Reason:          "failed to update fragment",
			SuggestedAction: "retry",
		})
		return
	}
	e.updatedNodeCount++
	e.syncNodeLensItem(fragment)
}

func (e *graphIntentApplyExecutor) applyEdgePatch(patch horizonmodels.GraphIntentEdgePatch) {
	edge, ok := e.resolvePatchEdge(patch)
	if !ok || edge == nil {
		e.unresolved = append(e.unresolved, horizonmodels.GraphIntentUnresolvedItem{
			TempID:          strings.TrimSpace(patch.PatchID),
			Reason:          "edge patch target unresolved",
			SuggestedAction: "fix_plan",
		})
		return
	}

	changed := false
	if patch.RelationType != nil && strings.TrimSpace(*patch.RelationType) != "" {
		newType, aliasQualifiers, aliasWarning, ok := horizonmodels.NormalizeGraphIntentRelationType(*patch.RelationType)
		if !ok {
			e.unresolved = append(e.unresolved, horizonmodels.GraphIntentUnresolvedItem{
				TempID:          strings.TrimSpace(patch.PatchID),
				Reason:          fmt.Sprintf("unsupported edge patch relation type: %s", strings.TrimSpace(*patch.RelationType)),
				SuggestedAction: "fix_plan",
			})
			return
		}
		if aliasWarning != "" {
			e.warnings = appendUniqueStrings(e.warnings, aliasWarning)
		}
		if edge.Qualifiers == nil {
			edge.Qualifiers = map[string]interface{}{}
		}
		for k, v := range aliasQualifiers {
			edge.Qualifiers[k] = v
		}
		if edge.Type != newType {
			edge.Type = newType
			changed = true
		}
	}
	if patch.Direction != nil && strings.TrimSpace(*patch.Direction) != "" {
		dir := strings.ToLower(strings.TrimSpace(*patch.Direction))
		switch dir {
		case "reverse":
			edge.SourceFragmentID, edge.TargetFragmentID = edge.TargetFragmentID, edge.SourceFragmentID
			changed = true
		case "bidirectional":
			if edge.Qualifiers == nil {
				edge.Qualifiers = map[string]interface{}{}
			}
			if qv := strings.TrimSpace(stringValueFromMap(edge.Qualifiers, "direction")); qv != "bidirectional" {
				edge.Qualifiers["direction"] = "bidirectional"
				changed = true
			}
		case "forward":
		}
	}
	if len(patch.Attributes) > 0 {
		if edge.Qualifiers == nil {
			edge.Qualifiers = map[string]interface{}{}
		}
		for k, v := range patch.Attributes {
			if strings.TrimSpace(k) == "" {
				continue
			}
			edge.Qualifiers[k] = v
			changed = true
		}
	}
	if !changed {
		return
	}
	if e.dryRun {
		e.updatedEdgeCount++
		return
	}
	if err := e.edgesRepo.Update(e.ctx, edge); err != nil {
		e.unresolved = append(e.unresolved, horizonmodels.GraphIntentUnresolvedItem{
			TempID:          strings.TrimSpace(patch.PatchID),
			Reason:          "failed to update dependency edge",
			SuggestedAction: "retry",
		})
		return
	}
	e.updatedEdgeCount++
	e.syncEdgeLensItem(edge)
}

func (e *graphIntentApplyExecutor) applyRelationRewrite(rw horizonmodels.GraphIntentRelationRewrite) {
	matchType, _, _, matchTypeOK := horizonmodels.NormalizeGraphIntentRelationType(rw.Match.RelationType)
	if strings.TrimSpace(rw.Match.RelationType) == "" {
		matchTypeOK = false
	}
	replaceType, replaceAliasQualifiers, replaceAliasWarning, replaceOK := horizonmodels.NormalizeGraphIntentRelationType(rw.ReplaceWith.RelationType)
	if !replaceOK {
		e.unresolved = append(e.unresolved, horizonmodels.GraphIntentUnresolvedItem{
			TempID:          strings.TrimSpace(rw.RewriteID),
			Reason:          fmt.Sprintf("unsupported rewrite replacement relation type: %s", strings.TrimSpace(rw.ReplaceWith.RelationType)),
			SuggestedAction: "fix_plan",
		})
		return
	}
	if replaceAliasWarning != "" {
		e.warnings = appendUniqueStrings(e.warnings, replaceAliasWarning)
	}

	sourceIDs := e.resolveManyNodeRefs(rw.Match.SourceNodeIDs)
	targetIDs := e.resolveManyNodeRefs(rw.Match.TargetNodeIDs)
	if len(sourceIDs) == 0 && len(targetIDs) == 0 {
		e.unresolved = append(e.unresolved, horizonmodels.GraphIntentUnresolvedItem{
			TempID:          strings.TrimSpace(rw.RewriteID),
			Reason:          "global relationRewrite without source/target constraints is blocked",
			SuggestedAction: "confirm_and_scope",
		})
		return
	}

	candidates := e.collectRewriteCandidates(sourceIDs, targetIDs)
	if len(candidates) == 0 {
		return
	}
	direction := strings.ToLower(strings.TrimSpace(rw.ReplaceWith.Direction))
	if direction == "" {
		direction = "forward"
	}

	touched := 0
	for _, edge := range candidates {
		if edge == nil {
			continue
		}
		if matchTypeOK && edge.Type != matchType {
			continue
		}
		if len(sourceIDs) > 0 && !containsString(sourceIDs, edge.SourceFragmentID) {
			continue
		}
		if len(targetIDs) > 0 && !containsString(targetIDs, edge.TargetFragmentID) {
			continue
		}

		changed := false
		if edge.Type != replaceType {
			edge.Type = replaceType
			changed = true
		}
		if edge.Qualifiers == nil {
			edge.Qualifiers = map[string]interface{}{}
		}
		for k, v := range replaceAliasQualifiers {
			edge.Qualifiers[k] = v
		}
		switch direction {
		case "reverse":
			edge.SourceFragmentID, edge.TargetFragmentID = edge.TargetFragmentID, edge.SourceFragmentID
			changed = true
		case "bidirectional":
			edge.Qualifiers["direction"] = "bidirectional"
			changed = true
		}
		if !changed {
			continue
		}
		if e.dryRun {
			e.updatedEdgeCount++
			touched++
		} else if err := e.edgesRepo.Update(e.ctx, edge); err == nil {
			e.updatedEdgeCount++
			touched++
			e.syncEdgeLensItem(edge)
		}
		if touched >= e.maxRewriteTouchCount {
			e.warnings = appendUniqueStrings(e.warnings, fmt.Sprintf("rewrite %s truncated at %d edges", strings.TrimSpace(rw.RewriteID), e.maxRewriteTouchCount))
			break
		}
	}
}

func (e *graphIntentApplyExecutor) collectRewriteCandidates(sourceIDs, targetIDs []string) []*horizonmodels.DependencyEdge {
	seen := map[string]struct{}{}
	out := make([]*horizonmodels.DependencyEdge, 0, 128)
	appendEdge := func(edge *horizonmodels.DependencyEdge) {
		if edge == nil || strings.TrimSpace(edge.ID) == "" {
			return
		}
		if _, ok := seen[edge.ID]; ok {
			return
		}
		seen[edge.ID] = struct{}{}
		out = append(out, edge)
	}
	tenantID := strings.TrimSpace(e.lens.TenantID)
	if tenantID == "" {
		return out
	}
	if len(sourceIDs) > 0 {
		for _, sid := range sourceIDs {
			edges, err := e.edgesRepo.ListBySource(e.ctx, tenantID, sid, nil, 300)
			if err != nil {
				continue
			}
			for _, edge := range edges {
				appendEdge(edge)
			}
		}
		return out
	}
	for _, tid := range targetIDs {
		edges, err := e.edgesRepo.ListByTarget(e.ctx, tenantID, tid, nil, 300)
		if err != nil {
			continue
		}
		for _, edge := range edges {
			appendEdge(edge)
		}
	}
	return out
}

func (e *graphIntentApplyExecutor) resolvePatchEdge(patch horizonmodels.GraphIntentEdgePatch) (*horizonmodels.DependencyEdge, bool) {
	if strings.TrimSpace(patch.EdgeID) != "" {
		edge, err := e.edgesRepo.Get(e.ctx, strings.TrimSpace(patch.EdgeID))
		if err == nil && edge != nil {
			return edge, true
		}
	}
	sourceID := strings.TrimSpace(patch.SourceFragmentID)
	targetID := strings.TrimSpace(patch.TargetFragmentID)
	if sourceID == "" && strings.TrimSpace(patch.SourceNodeID) != "" {
		sourceID, _ = e.resolveNodeRef(strings.TrimSpace(patch.SourceNodeID))
	}
	if targetID == "" && strings.TrimSpace(patch.TargetNodeID) != "" {
		targetID, _ = e.resolveNodeRef(strings.TrimSpace(patch.TargetNodeID))
	}
	if sourceID == "" || targetID == "" {
		return nil, false
	}
	if patch.RelationType != nil && strings.TrimSpace(*patch.RelationType) != "" {
		typ, _, _, ok := horizonmodels.NormalizeGraphIntentRelationType(*patch.RelationType)
		if ok {
			edge, err := e.edgesRepo.GetBySourceTargetAndType(e.ctx, strings.TrimSpace(e.lens.TenantID), sourceID, targetID, typ)
			if err == nil && edge != nil {
				return edge, true
			}
		}
	}
	edge, err := e.edgesRepo.GetBySourceAndTarget(e.ctx, strings.TrimSpace(e.lens.TenantID), sourceID, targetID)
	if err != nil || edge == nil {
		return nil, false
	}
	return edge, true
}

func (e *graphIntentApplyExecutor) normalizeCanonicalPathHint(spec horizonmodels.GraphIntentNodeSpec) string {
	hint := strings.TrimSpace(spec.CanonicalPathHint)
	if hint != "" {
		if !strings.HasPrefix(hint, "/") {
			hint = "/" + hint
		}
		hint = strings.TrimSpace(hint)
		if err := taxonomy.ValidateCanonicalPath(hint); err == nil {
			return hint
		}
	}
	typ := strings.ToUpper(strings.TrimSpace(spec.Type))
	label := strings.TrimSpace(spec.Label)
	slug := taxonomy.Slugify(label)
	if slug == "" {
		slug = "untitled"
	}
	switch typ {
	case "GOVERNMENT_AGENCY":
		return taxonomy.GovCanonicalPath("na", label)
	case string(horizonmodels.FragmentTypeOrganization):
		return taxonomy.OrgCanonicalPath("na", label)
	case string(horizonmodels.FragmentTypeFacility):
		return taxonomy.FacilityCanonicalPath("", label)
	case string(horizonmodels.FragmentTypeGeoRegion):
		return taxonomy.GeoCanonicalPath("na", "na", slug)
	case string(horizonmodels.FragmentTypeProduct):
		return taxonomy.ProductCanonicalPath("", label)
	case string(horizonmodels.FragmentTypeProgram):
		return taxonomy.ProgramCanonicalPath("", label)
	case string(horizonmodels.FragmentTypeMarket):
		return taxonomy.MarketCanonicalPath("global", label)
	case string(horizonmodels.FragmentTypePerson):
		return taxonomy.PersonCanonicalPath(label)
	case string(horizonmodels.FragmentTypeMediaChannel):
		return taxonomy.JoinPath("media", "channel", slug)
	case string(horizonmodels.FragmentTypeDomain):
		return taxonomy.JoinPath("domain", "v1", "other", slug)
	case string(horizonmodels.FragmentTypeSphere):
		return taxonomy.JoinPath("sphere", slug)
	case string(horizonmodels.FragmentTypeEvent):
		return taxonomy.JoinPath("event", slug)
	case string(horizonmodels.FragmentTypeDecisionPoint):
		return taxonomy.JoinPath("decision", "general", slug)
	case string(horizonmodels.FragmentTypeMechanism):
		return taxonomy.JoinPath("mechanism", "general", slug)
	case string(horizonmodels.FragmentTypeDeposit):
		return taxonomy.JoinPath("deposit", "resource", slug)
	case string(horizonmodels.FragmentTypeTrack):
		return taxonomy.JoinPath("track", "ground", "global", slug)
	case string(horizonmodels.FragmentTypeSupplyLink):
		return taxonomy.JoinPath("supply-link", "global", "na", slug)
	case string(horizonmodels.FragmentTypeCommunity), "COMMUNITY_DYNASTY", "COMMUNITY_TRIBE":
		communityType := "interest"
		if typ == "COMMUNITY_DYNASTY" {
			communityType = "dynasty"
		}
		if typ == "COMMUNITY_TRIBE" {
			communityType = "tribe"
		}
		return taxonomy.JoinPath("community", "global", communityType, slug)
	default:
		return taxonomy.OrgCanonicalPath("na", label)
	}
}

func (e *graphIntentApplyExecutor) resolveEdgeEndpoint(tempID, nodeID string) (string, bool) {
	if strings.TrimSpace(tempID) != "" {
		if fid, ok := e.tempFragmentRefs[strings.TrimSpace(tempID)]; ok && strings.TrimSpace(fid) != "" {
			return strings.TrimSpace(fid), true
		}
	}
	if strings.TrimSpace(nodeID) != "" {
		return e.resolveNodeRef(nodeID)
	}
	return "", false
}

func (e *graphIntentApplyExecutor) resolveNodeRef(ref string) (string, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", false
	}
	if fid, ok := e.tempFragmentRefs[ref]; ok && strings.TrimSpace(fid) != "" {
		return strings.TrimSpace(fid), true
	}
	if fid, ok := e.knownFragmentRefs[ref]; ok && strings.TrimSpace(fid) != "" {
		return strings.TrimSpace(fid), true
	}
	if strings.HasPrefix(ref, "/") {
		if frag, err := e.fragmentsRepo.GetByCanonicalPath(e.ctx, ref); err == nil && frag != nil && strings.TrimSpace(frag.ID) != "" {
			e.addKnownFragmentRef(ref, strings.TrimSpace(frag.ID))
			e.addKnownFragmentRef(strings.TrimSpace(frag.ID), strings.TrimSpace(frag.ID))
			return strings.TrimSpace(frag.ID), true
		}
	}
	if frag, err := e.fragmentsRepo.Get(e.ctx, ref); err == nil && frag != nil && strings.TrimSpace(frag.ID) != "" {
		e.addKnownFragmentRef(strings.TrimSpace(frag.ID), strings.TrimSpace(frag.ID))
		if strings.TrimSpace(frag.CanonicalPath) != "" {
			e.addKnownFragmentRef(strings.TrimSpace(frag.CanonicalPath), strings.TrimSpace(frag.ID))
		}
		return strings.TrimSpace(frag.ID), true
	}
	return "", false
}

func (e *graphIntentApplyExecutor) resolveManyNodeRefs(refs []string) []string {
	out := make([]string, 0, len(refs))
	seen := map[string]struct{}{}
	for _, ref := range refs {
		id, ok := e.resolveNodeRef(ref)
		if !ok || id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func (e *graphIntentApplyExecutor) bindTempNodeRef(spec horizonmodels.GraphIntentNodeSpec, fragmentID, canonicalPath string) {
	fragmentID = strings.TrimSpace(fragmentID)
	if fragmentID == "" {
		return
	}
	if strings.TrimSpace(spec.TempID) != "" {
		e.tempFragmentRefs[strings.TrimSpace(spec.TempID)] = fragmentID
	}
	if strings.TrimSpace(canonicalPath) != "" {
		e.addKnownFragmentRef(strings.TrimSpace(canonicalPath), fragmentID)
	}
	e.addKnownFragmentRef(fragmentID, fragmentID)
}

func (e *graphIntentApplyExecutor) addKnownFragmentRef(ref, fragmentID string) {
	ref = strings.TrimSpace(ref)
	fragmentID = strings.TrimSpace(fragmentID)
	if ref == "" || fragmentID == "" {
		return
	}
	e.knownFragmentRefs[ref] = fragmentID
}

func (e *graphIntentApplyExecutor) ensureNodeLensItem(fragmentID, canonicalPath, label, fragType string) {
	fragmentID = strings.TrimSpace(fragmentID)
	if fragmentID == "" || e.dryRun || e.lensItemsRepo == nil {
		return
	}
	if _, exists := e.nodeItemByFragmentID[fragmentID]; exists {
		return
	}
	subkind := "fragment"
	item := &horizonmodels.LensItem{
		TenantID:      strings.TrimSpace(e.lens.TenantID),
		ProjectID:     strings.TrimSpace(e.lens.ProjectID),
		LensID:        strings.TrimSpace(e.lens.ID),
		Kind:          "node",
		Subkind:       &subkind,
		FragmentID:    &fragmentID,
		CanonicalPath: strPtr(strings.TrimSpace(canonicalPath)),
		Label:         strPtr(strings.TrimSpace(label)),
		Type:          strPtr(strings.TrimSpace(fragType)),
		Status:        strPtr("active"),
	}
	if _, err := e.lensItemsRepo.Create(e.ctx, item); err != nil {
		e.warnings = appendUniqueStrings(e.warnings, fmt.Sprintf("failed to create lens node item for fragment %s", fragmentID))
		return
	}
	e.nodeItemByFragmentID[fragmentID] = item
}

func (e *graphIntentApplyExecutor) syncNodeLensItem(fragment *horizonmodels.HorizonFragment) {
	if e == nil || e.dryRun || e.lensItemsRepo == nil || fragment == nil {
		return
	}
	fid := strings.TrimSpace(fragment.ID)
	if fid == "" {
		return
	}
	item, ok := e.nodeItemByFragmentID[fid]
	if !ok || item == nil || strings.TrimSpace(item.ID) == "" {
		return
	}
	updates := map[string]interface{}{}
	if label := strings.TrimSpace(fragment.Labels["en"]); label != "" {
		updates["label"] = label
	}
	if fragment.Type != "" {
		updates["type"] = string(fragment.Type)
	}
	if cp := strings.TrimSpace(fragment.CanonicalPath); cp != "" {
		updates["canonicalPath"] = cp
	}
	if len(updates) == 0 {
		return
	}
	if err := e.lensItemsRepo.Update(e.ctx, item.ID, updates); err != nil {
		e.warnings = appendUniqueStrings(e.warnings, fmt.Sprintf("failed to sync lens node item for fragment %s", fid))
	}
}

func (e *graphIntentApplyExecutor) ensureEdgeLensItem(sourceID, targetID, edgeType, direction string, criticality, amplification float64, latencyHours int) {
	if e == nil || e.dryRun || e.lensItemsRepo == nil {
		return
	}
	sourceID = strings.TrimSpace(sourceID)
	targetID = strings.TrimSpace(targetID)
	edgeType = strings.TrimSpace(edgeType)
	if sourceID == "" || targetID == "" || edgeType == "" {
		return
	}
	pairKey := edgePairKey(sourceID, targetID)
	for _, item := range e.edgeItemsByPair[pairKey] {
		if item == nil {
			continue
		}
		if strings.TrimSpace(stringPtrValue(item.EdgeType)) == edgeType || strings.TrimSpace(stringPtrValue(item.Subkind)) == edgeType {
			return
		}
	}
	subkind := edgeType
	source := sourceID
	target := targetID
	item := &horizonmodels.LensItem{
		TenantID:         strings.TrimSpace(e.lens.TenantID),
		ProjectID:        strings.TrimSpace(e.lens.ProjectID),
		LensID:           strings.TrimSpace(e.lens.ID),
		Kind:             "edge",
		Subkind:          &subkind,
		SourceFragmentID: &source,
		TargetFragmentID: &target,
		EdgeType:         &subkind,
		Criticality:      &criticality,
		Amplification:    &amplification,
		Direction:        strPtr(direction),
	}
	if latencyHours != 0 {
		item.LatencyHours = &latencyHours
	}
	if _, err := e.lensItemsRepo.Create(e.ctx, item); err != nil {
		e.warnings = appendUniqueStrings(e.warnings, fmt.Sprintf("failed to create lens edge item (%s -> %s, %s)", sourceID, targetID, edgeType))
		return
	}
	e.edgeItemsByPair[pairKey] = append(e.edgeItemsByPair[pairKey], item)
}

func (e *graphIntentApplyExecutor) syncEdgeLensItem(edge *horizonmodels.DependencyEdge) {
	if e == nil || e.dryRun || e.lensItemsRepo == nil || edge == nil {
		return
	}
	pairKey := edgePairKey(edge.SourceFragmentID, edge.TargetFragmentID)
	items := e.edgeItemsByPair[pairKey]
	if len(items) == 0 {
		e.ensureEdgeLensItem(edge.SourceFragmentID, edge.TargetFragmentID, string(edge.Type), stringValueFromMap(edge.Qualifiers, "direction"), edge.Properties.Criticality, edge.Properties.Amplification, edge.Properties.LatencyHours)
		return
	}
	var selected *horizonmodels.LensItem
	for _, item := range items {
		if item == nil {
			continue
		}
		if strings.TrimSpace(stringPtrValue(item.EdgeType)) == string(edge.Type) || strings.TrimSpace(stringPtrValue(item.Subkind)) == string(edge.Type) {
			selected = item
			break
		}
		if selected == nil {
			selected = item
		}
	}
	if selected == nil || strings.TrimSpace(selected.ID) == "" {
		return
	}
	updates := map[string]interface{}{
		"subkind":       string(edge.Type),
		"edgeType":      string(edge.Type),
		"criticality":   edge.Properties.Criticality,
		"amplification": edge.Properties.Amplification,
		"latencyHours":  edge.Properties.LatencyHours,
	}
	if dir := strings.TrimSpace(stringValueFromMap(edge.Qualifiers, "direction")); dir != "" {
		updates["direction"] = dir
	} else {
		updates["direction"] = "forward"
	}
	if err := e.lensItemsRepo.Update(e.ctx, selected.ID, updates); err != nil {
		e.warnings = appendUniqueStrings(e.warnings, fmt.Sprintf("failed to sync lens edge item %s", strings.TrimSpace(selected.ID)))
	}
}

func appendUniqueStrings(dst []string, values ...string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(dst)+len(values))
	for _, s := range dst {
		ss := strings.TrimSpace(s)
		if ss == "" {
			continue
		}
		if _, ok := seen[ss]; ok {
			continue
		}
		seen[ss] = struct{}{}
		out = append(out, ss)
	}
	for _, s := range values {
		ss := strings.TrimSpace(s)
		if ss == "" {
			continue
		}
		if _, ok := seen[ss]; ok {
			continue
		}
		seen[ss] = struct{}{}
		out = append(out, ss)
	}
	sort.Strings(out)
	return out
}

func graphIntentApplyConfirmed(r *http.Request) bool {
	if r == nil {
		return false
	}
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("confirm")))
	if q == "1" || q == "true" || q == "yes" {
		return true
	}
	h := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Graph-Intent-Confirm")))
	return h == "1" || h == "true" || h == "yes"
}

func writeGraphIntentJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func cloneAnyMap(in map[string]interface{}) map[string]interface{} {
	if len(in) == 0 {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func toMetricState(raw map[string]interface{}) map[string]horizonmodels.MetricValue {
	if len(raw) == 0 {
		return map[string]horizonmodels.MetricValue{}
	}
	now := time.Now().UTC()
	out := make(map[string]horizonmodels.MetricValue, len(raw))
	for k, val := range raw {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		switch vv := val.(type) {
		case map[string]interface{}:
			num, ok := toFloat(vv["value"])
			if !ok {
				continue
			}
			conf := 0.5
			if cv, ok := toFloat(vv["confidence"]); ok {
				conf = clampFloat(cv, 0.0, 1.0)
			}
			out[key] = horizonmodels.MetricValue{
				Value:      num,
				Confidence: conf,
				ObservedAt: now,
				Provenance: horizonmodels.Provenance{Method: "graph-intent.apply"},
			}
		default:
			num, ok := toFloat(vv)
			if !ok {
				continue
			}
			out[key] = horizonmodels.MetricValue{
				Value:      num,
				Confidence: 0.5,
				ObservedAt: now,
				Provenance: horizonmodels.Provenance{Method: "graph-intent.apply"},
			}
		}
	}
	return out
}

func toFloat(v interface{}) (float64, bool) {
	switch x := v.(type) {
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return 0, false
		}
		return x, true
	case float32:
		f := float64(x)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, false
		}
		return f, true
	case int:
		return float64(x), true
	case int8:
		return float64(x), true
	case int16:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint:
		return float64(x), true
	case uint8:
		return float64(x), true
	case uint16:
		return float64(x), true
	case uint32:
		return float64(x), true
	case uint64:
		return float64(x), true
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return 0, false
		}
		n, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}

func minInt(a, b int) int {
	if a <= b {
		return a
	}
	return b
}

func edgePairKey(sourceID, targetID string) string {
	return strings.TrimSpace(sourceID) + "->" + strings.TrimSpace(targetID)
}

func containsString(values []string, needle string) bool {
	needle = strings.TrimSpace(needle)
	for _, v := range values {
		if strings.TrimSpace(v) == needle {
			return true
		}
	}
	return false
}

func stringPtrValue(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

func stringValueFromMap(m map[string]interface{}, key string) string {
	if len(m) == 0 {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return strings.TrimSpace(s)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", s))
	}
}

func stringSliceFromAny(v interface{}) []string {
	switch arr := v.(type) {
	case []string:
		out := make([]string, 0, len(arr))
		for _, s := range arr {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(arr))
		for _, raw := range arr {
			s := strings.TrimSpace(fmt.Sprintf("%v", raw))
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
