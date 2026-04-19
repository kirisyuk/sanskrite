package horizon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
)

const (
	lensConsoleModel = "gemini-3.1-flash-lite-preview"
)

type lensConsoleGraphContext struct {
	Snapshot       horizonmodels.GraphIntentSnapshot
	Summary        horizonmodels.LensConsoleGraphSummary
	ScopePaths     []string
	Fragments      []*horizonmodels.HorizonFragment
	FragmentByID   map[string]*horizonmodels.HorizonFragment
	FragmentByPath map[string]*horizonmodels.HorizonFragment
	Items          []*horizonmodels.LensItem
	ItemByID       map[string]*horizonmodels.LensItem
	Warnings       []string
}

func (h *HorizonHandlers) LensConsoleRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	authInfo, err := requireAuth(r)
	if err != nil {
		_ = httputil.WriteUnauthorized(w, err.Error())
		return
	}
	if h.service == nil || h.service.Lens() == nil {
		_ = httputil.WriteInternalError(w, "Horizon service not configured")
		return
	}
	lens, err := h.resolveGraphIntentLens(r.Context(), r, authInfo.TenantID)
	if err != nil {
		_ = httputil.WriteBadRequest(w, "LENS_RESOLUTION_FAILED", err.Error())
		return
	}

	var req horizonmodels.LensConsoleToolRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		_ = httputil.WriteBadRequest(w, "INVALID_JSON", "Failed to parse body")
		return
	}
	req.Tool = strings.TrimSpace(strings.ToLower(req.Tool))
	if !isLensConsoleToolAllowed(req.Tool) {
		_ = httputil.WriteBadRequest(w, "INVALID_TOOL", "unsupported console tool")
		return
	}
	if req.Input == nil {
		req.Input = map[string]any{}
	}

	runID := uuid.NewString()
	startedAt := time.Now().UTC()
	resp := horizonmodels.LensConsoleToolRunResponse{
		RunID:     runID,
		Tool:      req.Tool,
		Status:    horizonmodels.LensConsoleStatusFailed,
		LensID:    strings.TrimSpace(lens.ID),
		ProjectID: strings.TrimSpace(lens.ProjectID),
		StartedAt: startedAt,
	}
	publishEvents := req.PublishEvents == nil || *req.PublishEvents
	h.publishLensConsoleEvent(r.Context(), publishEvents, authInfo.TenantID, lens.ProjectID, lens.ID, runID, req.Tool, "started", map[string]any{
		"inputKeys": sortedMapKeys(req.Input),
	})

	statusCode := http.StatusOK
	result, preview, warnings, requiresConfirmation, execErr := h.executeLensConsoleTool(r.Context(), authInfo.TenantID, lens, &req)
	resp.Result = result
	resp.Preview = preview
	resp.Warnings = uniqueStringsLocal(warnings)
	resp.RequiresConfirmation = requiresConfirmation
	completedAt := time.Now().UTC()
	resp.CompletedAt = &completedAt

	if execErr != nil {
		resp.Status = horizonmodels.LensConsoleStatusFailed
		resp.Warnings = appendUniqueStrings(resp.Warnings, execErr.Error())
		if statusCode < 400 {
			statusCode = http.StatusInternalServerError
		}
		h.publishLensConsoleEvent(r.Context(), publishEvents, authInfo.TenantID, lens.ProjectID, lens.ID, runID, req.Tool, "failed", map[string]any{
			"error":    execErr.Error(),
			"warnings": resp.Warnings,
		})
		_ = httputil.WriteJSON(w, statusCode, resp)
		return
	}
	if requiresConfirmation {
		resp.Status = horizonmodels.LensConsoleStatusRequiresConfirmation
		h.publishLensConsoleEvent(r.Context(), publishEvents, authInfo.TenantID, lens.ProjectID, lens.ID, runID, req.Tool, "requires_confirmation", map[string]any{
			"preview":  preview,
			"warnings": resp.Warnings,
		})
		_ = httputil.WriteJSON(w, http.StatusOK, resp)
		return
	}

	resp.Status = horizonmodels.LensConsoleStatusCompleted
	if preview != nil {
		h.publishLensConsoleEvent(r.Context(), publishEvents, authInfo.TenantID, lens.ProjectID, lens.ID, runID, req.Tool, "preview", map[string]any{
			"preview": preview,
		})
	}
	h.publishLensConsoleEvent(r.Context(), publishEvents, authInfo.TenantID, lens.ProjectID, lens.ID, runID, req.Tool, "completed", map[string]any{
		"result":   result,
		"warnings": resp.Warnings,
	})
	_ = httputil.WriteJSON(w, http.StatusOK, resp)
}

func isLensConsoleToolAllowed(tool string) bool {
	switch strings.TrimSpace(tool) {
	case horizonmodels.LensConsoleToolObserveGraph,
		horizonmodels.LensConsoleToolReadObject,
		horizonmodels.LensConsoleToolFindObjects,
		horizonmodels.LensConsoleToolAnswer,
		horizonmodels.LensConsoleToolResearch,
		horizonmodels.LensConsoleToolWebSearch,
		horizonmodels.LensConsoleToolCompose,
		horizonmodels.LensConsoleToolUpdate,
		horizonmodels.LensConsoleToolUpdateMetrics,
		horizonmodels.LensConsoleToolPreviewChanges,
		horizonmodels.LensConsoleToolApplyChanges:
		return true
	default:
		return false
	}
}

func (h *HorizonHandlers) executeLensConsoleTool(ctx context.Context, tenantID string, lens *horizonmodels.Lens, req *horizonmodels.LensConsoleToolRunRequest) (map[string]any, map[string]any, []string, bool, error) {
	switch req.Tool {
	case horizonmodels.LensConsoleToolObserveGraph:
		result, warnings, err := h.lensConsoleObserveGraph(ctx, lens)
		return result, nil, warnings, false, err
	case horizonmodels.LensConsoleToolReadObject:
		result, warnings, err := h.lensConsoleReadObject(ctx, lens, req.Input)
		return result, nil, warnings, false, err
	case horizonmodels.LensConsoleToolFindObjects:
		result, warnings, err := h.lensConsoleFindObjects(ctx, lens, req.Input)
		return result, nil, warnings, false, err
	case horizonmodels.LensConsoleToolAnswer:
		result, warnings, err := h.lensConsoleAnswer(ctx, lens, req.Input)
		return result, nil, warnings, false, err
	case horizonmodels.LensConsoleToolResearch:
		result, warnings, err := h.lensConsoleResearch(ctx, lens, req.Input)
		return result, nil, warnings, false, err
	case horizonmodels.LensConsoleToolWebSearch:
		result, warnings, err := h.lensConsoleWebSearch(ctx, lens, req.Input)
		return result, nil, warnings, false, err
	case horizonmodels.LensConsoleToolCompose, horizonmodels.LensConsoleToolUpdate, horizonmodels.LensConsoleToolUpdateMetrics:
		plan, preview, warnings, err := h.lensConsolePlanChanges(ctx, lens, req.Tool, req.Input)
		if err != nil {
			return nil, preview, warnings, false, err
		}
		requires := false
		if plan != nil {
			requires = plan.RequiresConfirmation || plan.IsPotentiallyDestructive()
		}
		return structToMap(map[string]any{
			"plan":        plan,
			"tool":        req.Tool,
			"previewMode": "plan",
		}), preview, warnings, requires, nil
	case horizonmodels.LensConsoleToolPreviewChanges:
		result, preview, warnings, requires, err := h.lensConsolePreviewOrApply(ctx, lens, req.Tool, req.Input, false, false)
		return result, preview, warnings, requires, err
	case horizonmodels.LensConsoleToolApplyChanges:
		result, preview, warnings, requires, err := h.lensConsolePreviewOrApply(ctx, lens, req.Tool, req.Input, true, req.Confirm)
		return result, preview, warnings, requires, err
	default:
		return nil, nil, nil, false, fmt.Errorf("unsupported console tool: %s", req.Tool)
	}
}

func (h *HorizonHandlers) lensConsoleObserveGraph(ctx context.Context, lens *horizonmodels.Lens) (map[string]any, []string, error) {
	graphCtx, err := h.loadLensConsoleGraphContext(ctx, lens)
	if err != nil {
		return nil, nil, err
	}
	nodes := make([]map[string]any, 0, minIntLocal(len(graphCtx.Snapshot.Nodes), 24))
	for i, node := range graphCtx.Snapshot.Nodes {
		if i >= 24 {
			break
		}
		nodes = append(nodes, map[string]any{
			"nodeId":        node.NodeID,
			"fragmentId":    node.FragmentID,
			"canonicalPath": node.CanonicalPath,
			"label":         node.Label,
			"type":          node.Type,
		})
	}
	edges := make([]map[string]any, 0, minIntLocal(len(graphCtx.Snapshot.Edges), 24))
	for i, edge := range graphCtx.Snapshot.Edges {
		if i >= 24 {
			break
		}
		edges = append(edges, map[string]any{
			"edgeId":           edge.EdgeID,
			"sourceNodeId":     edge.SourceNodeID,
			"targetNodeId":     edge.TargetNodeID,
			"sourceFragmentId": edge.SourceFragmentID,
			"targetFragmentId": edge.TargetFragmentID,
			"relationType":     edge.RelationType,
			"direction":        edge.Direction,
		})
	}
	return map[string]any{
		"summary":     graphCtx.Summary,
		"snapshot":    graphCtx.Snapshot,
		"sampleNodes": nodes,
		"sampleEdges": edges,
		"scopePaths":  graphCtx.ScopePaths,
	}, graphCtx.Warnings, nil
}

func (h *HorizonHandlers) lensConsoleReadObject(ctx context.Context, lens *horizonmodels.Lens, input map[string]any) (map[string]any, []string, error) {
	if h.service == nil || h.service.Fragments() == nil {
		return nil, nil, fmt.Errorf("fragments repository is unavailable")
	}
	graphCtx, err := h.loadLensConsoleGraphContext(ctx, lens)
	if err != nil {
		return nil, nil, err
	}
	itemID := strings.TrimSpace(asStringAny(input["itemId"]))
	fragmentID := strings.TrimSpace(asStringAny(input["fragmentId"]))
	canonicalPath := strings.TrimSpace(asStringAny(input["canonicalPath"]))
	if itemID == "" && fragmentID == "" && canonicalPath == "" {
		return nil, graphCtx.Warnings, fmt.Errorf("read_object requires itemId, fragmentId, or canonicalPath")
	}

	var item *horizonmodels.LensItem
	if itemID != "" {
		item = graphCtx.ItemByID[itemID]
		if item == nil && h.lensItemsHandler != nil && h.lensItemsHandler.lensItemsRepo != nil {
			item, _ = h.lensItemsHandler.lensItemsRepo.Get(ctx, itemID)
		}
		if item != nil {
			if fragmentID == "" {
				fragmentID = strings.TrimSpace(stringPtrValue(item.FragmentID))
			}
			if canonicalPath == "" {
				canonicalPath = strings.TrimSpace(stringPtrValue(item.CanonicalPath))
			}
		}
	}

	var fragment *horizonmodels.HorizonFragment
	if fragmentID != "" {
		fragment = graphCtx.FragmentByID[fragmentID]
		if fragment == nil {
			fragment, _ = h.service.Fragments().Get(ctx, fragmentID)
		}
	}
	if fragment == nil && canonicalPath != "" {
		fragment = graphCtx.FragmentByPath[canonicalPath]
		if fragment == nil {
			fragment, _ = h.service.Fragments().GetByCanonicalPath(ctx, canonicalPath)
		}
	}
	if item == nil && fragment != nil {
		for _, candidate := range graphCtx.Items {
			if candidate == nil {
				continue
			}
			if strings.TrimSpace(stringPtrValue(candidate.FragmentID)) == strings.TrimSpace(fragment.ID) {
				item = candidate
				break
			}
			if strings.TrimSpace(stringPtrValue(candidate.CanonicalPath)) == strings.TrimSpace(fragment.CanonicalPath) {
				item = candidate
				break
			}
		}
	}
	if fragment == nil && item == nil {
		return nil, graphCtx.Warnings, fmt.Errorf("object not found")
	}
	displayLabel := ""
	objectType := ""
	if fragment != nil {
		displayLabel = lensConsoleFragmentLabel(fragment)
		objectType = strings.TrimSpace(string(fragment.Class))
	} else if item != nil {
		displayLabel = strings.TrimSpace(stringPtrValue(item.Label))
		objectType = strings.TrimSpace(stringPtrValue(item.Type))
	}
	if displayLabel == "" {
		displayLabel = humanizeCanonicalPath(firstNonEmptyString(canonicalPath, strings.TrimSpace(stringPtrValue(item.CanonicalPath))))
	}
	return map[string]any{
		"item":         item,
		"fragment":     fragment,
		"displayLabel": displayLabel,
		"objectType":   objectType,
		"inLens":       true,
	}, graphCtx.Warnings, nil
}

func (h *HorizonHandlers) lensConsoleFindObjects(ctx context.Context, lens *horizonmodels.Lens, input map[string]any) (map[string]any, []string, error) {
	query := strings.TrimSpace(firstNonEmptyString(asStringAny(input["query"]), asStringAny(input["name"]), asStringAny(input["text"])))
	if query == "" {
		return nil, nil, fmt.Errorf("find_objects requires query")
	}
	limit := clampIntAny(input["limit"], 8, 1, 24)
	graphCtx, err := h.loadLensConsoleGraphContext(ctx, lens)
	if err != nil {
		return nil, nil, err
	}
	local := h.findObjectsInGraph(query, graphCtx, limit)
	warnings := append([]string(nil), graphCtx.Warnings...)
	if len(local) < limit && h.service != nil {
		globalHits, err := h.service.SearchFragments(ctx, query, limit*4)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("global semantic search failed: %v", err))
		} else {
			for _, frag := range globalHits {
				if frag == nil {
					continue
				}
				if _, ok := graphCtx.FragmentByPath[strings.TrimSpace(frag.CanonicalPath)]; !ok {
					if _, ok := graphCtx.FragmentByID[strings.TrimSpace(frag.ID)]; !ok {
						continue
					}
				}
				local = append(local, lensConsoleFragmentCandidate(frag, 0.85))
			}
			local = dedupeLensConsoleCandidates(local, limit)
		}
	}
	return map[string]any{
		"query":      query,
		"candidates": local,
		"count":      len(local),
	}, uniqueStringsLocal(warnings), nil
}

func (h *HorizonHandlers) lensConsoleAnswer(ctx context.Context, lens *horizonmodels.Lens, input map[string]any) (map[string]any, []string, error) {
	query := strings.TrimSpace(firstNonEmptyString(asStringAny(input["query"]), asStringAny(input["message"]), asStringAny(input["question"])))
	if query == "" {
		return nil, nil, fmt.Errorf("answer requires query")
	}
	history := []lensChatTurn{}
	warnings := []string{}
	if h.interactions != nil {
		threadID := lensChatThreadID(lens.ID)
		var historyWarnings []string
		history, historyWarnings = h.loadLensChatHistory(ctx, strings.TrimSpace(lens.ProjectID), threadID, "", defaultLensChatHistoryTurns)
		warnings = append(warnings, historyWarnings...)
	}
	answer, hits, highlightedPaths, modelUsed, answerWarnings := h.answerLensChat(ctx, lens, query, history)
	warnings = append(warnings, answerWarnings...)
	if strings.TrimSpace(modelUsed) == "" {
		modelUsed = lensChatModel
	}
	return map[string]any{
		"answer":           answer,
		"hits":             summarizeLensChatHits(hits, defaultLensChatHitLimit),
		"highlightedPaths": highlightedPaths,
		"model":            modelUsed,
	}, uniqueStringsLocal(warnings), nil
}

func (h *HorizonHandlers) lensConsoleResearch(ctx context.Context, lens *horizonmodels.Lens, input map[string]any) (map[string]any, []string, error) {
	query := strings.TrimSpace(firstNonEmptyString(asStringAny(input["query"]), asStringAny(input["message"]), asStringAny(input["question"])))
	if query == "" {
		return nil, nil, fmt.Errorf("research requires query")
	}
	limit := clampIntAny(input["limit"], 8, 1, 24)
	history := []lensChatTurn{}
	if h.interactions != nil {
		history, _ = h.loadLensChatHistory(ctx, strings.TrimSpace(lens.ProjectID), lensChatThreadID(lens.ID), "", defaultLensChatHistoryTurns)
	}
	warmState := lensChatWarmState(lens)
	warmedReady := lensChatTemperatureReady(warmState, lensChatRequiredTemperature(query, classifyLensChatAnalyticalIntent(query)))
	corpusID := strings.TrimSpace(asStringAny(warmState["corpusId"]))
	storeID := strings.TrimSpace(asStringAny(warmState["answerStore"]))
	scopePaths := lensChatScopePaths(lens)
	warnings := []string{}

	var internalHits []lensChatInternalHit
	if client := h.service.VertexSearchClient(); client != nil {
		if storeID != "" {
			client = client.WithDataStore(storeID)
		}
		var searchWarnings []string
		internalHits, searchWarnings = searchLensChatInternalKnowledge(ctx, client, lensChatSearchSpec{
			Question:          query,
			CorpusID:          lensChatFilteredCorpusID(warmedReady, corpusID),
			Limit:             limit,
			ScopeRefs:         scopePaths,
			PreferredCorpusID: corpusID,
			History:           history,
		})
		warnings = append(warnings, searchWarnings...)
	}

	graphHits := []map[string]any{}
	if h.service != nil {
		fragments, err := h.service.SearchFragments(ctx, query, limit)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("graph search failed: %v", err))
		} else {
			graphHits = summarizeLensConsoleFragments(fragments, limit)
		}
	}

	result := map[string]any{
		"query":             query,
		"internalKnowledge": summarizeLensChatHits(internalHits, limit),
		"graphCandidates":   graphHits,
		"highlightedPaths":  collectLensChatPaths(internalHits, 32),
	}
	if asBoolAny(input["allowWebSearch"]) {
		webResult, webWarnings, err := h.lensConsoleWebSearch(ctx, lens, map[string]any{
			"query":   query,
			"purpose": firstNonEmptyString(asStringAny(input["purpose"]), "research"),
		})
		if err != nil {
			warnings = append(warnings, err.Error())
		} else {
			warnings = append(warnings, webWarnings...)
			result["web"] = webResult
		}
	}
	return result, uniqueStringsLocal(warnings), nil
}

func (h *HorizonHandlers) lensConsoleWebSearch(ctx context.Context, lens *horizonmodels.Lens, input map[string]any) (map[string]any, []string, error) {
	if h.genAIHandler == nil {
		return nil, nil, fmt.Errorf("GenAI handler is not configured")
	}
	query := strings.TrimSpace(firstNonEmptyString(asStringAny(input["query"]), asStringAny(input["message"]), asStringAny(input["question"])))
	if query == "" {
		return nil, nil, fmt.Errorf("web_search requires query")
	}
	purpose := strings.TrimSpace(asStringAny(input["purpose"]))
	if purpose == "" {
		purpose = "research"
	}
	prompt := fmt.Sprintf("Use live web search grounding. Purpose: %s.\nQuestion: %s\nReturn a concise grounded answer in markdown, followed by a short 'Sources' section.", purpose, query)
	resp, err := h.genAIHandler.GenerateDirect(ctx, gen_combined.GenAIRequest{
		Model:            lensConsoleModel,
		Prompt:           prompt,
		MaxOutputTokens:  900,
		Temperature:      0.1,
		WebSearchEnabled: true,
		ProjectID:        strings.TrimSpace(lens.ProjectID),
	})
	if err != nil {
		return nil, nil, err
	}
	return map[string]any{
		"answer":        strings.TrimSpace(resp.Content),
		"model":         firstNonEmptyString(resp.Model, lensConsoleModel),
		"sources":       resp.Sources,
		"searchQueries": resp.SearchQueries,
		"attributions":  resp.Attributions,
	}, nil, nil
}

func (h *HorizonHandlers) lensConsolePlanChanges(ctx context.Context, lens *horizonmodels.Lens, tool string, input map[string]any) (*horizonmodels.GraphIntentPlanResponse, map[string]any, []string, error) {
	query := strings.TrimSpace(firstNonEmptyString(asStringAny(input["query"]), asStringAny(input["message"]), asStringAny(input["instruction"])))
	if query == "" {
		return nil, nil, nil, fmt.Errorf("%s requires query", tool)
	}
	graphCtx, err := h.loadLensConsoleGraphContext(ctx, lens)
	if err != nil {
		return nil, nil, nil, err
	}
	req := &horizonmodels.GraphIntentPlanRequest{
		Version:       horizonmodels.GraphIntentVersionV1,
		RawQuery:      query,
		QueryLanguage: strings.TrimSpace(firstNonEmptyString(asStringAny(input["queryLanguage"]), "auto")),
		Snapshot:      graphCtx.Snapshot,
		Options: horizonmodels.GraphIntentPlanOptions{
			AllowInternalGrounding: !asBoolAny(input["disableInternalGrounding"]),
			AllowSearchGrounding:   asBoolAny(input["allowWebSearch"]),
			MaxNewNodes:            clampIntAny(input["maxNewNodes"], horizonmodels.DefaultGraphIntentPlanOptions().MaxNewNodes, 1, 500),
			MaxNewEdges:            clampIntAny(input["maxNewEdges"], horizonmodels.DefaultGraphIntentPlanOptions().MaxNewEdges, 1, 1000),
		},
	}
	if err := req.NormalizeAndValidate(); err != nil {
		return nil, nil, graphCtx.Warnings, err
	}
	plan, err := h.generateGraphIntentPlan(ctx, lens, req)
	if err != nil {
		return nil, nil, graphCtx.Warnings, err
	}
	normalizeGraphIntentPlanLoose(plan)
	if err := plan.NormalizeAndValidate(); err != nil {
		return nil, nil, graphCtx.Warnings, err
	}
	if plan.IsPotentiallyDestructive() {
		plan.RequiresConfirmation = true
	}
	preview := map[string]any{
		"intentClass":          plan.IntentClass,
		"confidence":           plan.Confidence,
		"requiresConfirmation": plan.RequiresConfirmation,
		"actionPreview":        plan.MapToActions(),
		"summary":              graphCtx.Summary,
	}
	return plan, preview, append([]string(nil), graphCtx.Warnings...), nil
}

func (h *HorizonHandlers) lensConsolePreviewOrApply(ctx context.Context, lens *horizonmodels.Lens, tool string, input map[string]any, apply bool, confirmed bool) (map[string]any, map[string]any, []string, bool, error) {
	plan, preview, warnings, err := h.resolveLensConsolePlan(ctx, lens, tool, input)
	if err != nil {
		return nil, preview, warnings, false, err
	}
	if plan == nil {
		return nil, preview, warnings, false, fmt.Errorf("plan is required")
	}
	if apply && (plan.RequiresConfirmation || plan.IsPotentiallyDestructive()) && !confirmed {
		return map[string]any{
			"plan": plan,
		}, preview, warnings, true, nil
	}

	exec := &graphIntentApplyExecutor{
		handler:              h,
		ctx:                  ctx,
		lens:                 lens,
		dryRun:               !apply,
		maxRewriteTouchCount: graphIntentApplyHardMaxRewriteOps,
	}
	if err := exec.init(); err != nil {
		return nil, preview, warnings, false, err
	}
	applyResp := exec.execute(plan)
	applyResp.ActionPreview = plan.MapToActions()
	applyResp.Applied = apply && len(applyResp.UnresolvedItems) == 0
	preview = mergeMapsLocal(preview, map[string]any{
		"actionPreview":    applyResp.ActionPreview,
		"unresolvedItems":  applyResp.UnresolvedItems,
		"warnings":         applyResp.Warnings,
		"applied":          applyResp.Applied,
		"dryRun":           !apply,
		"createdNodeCount": applyResp.CreatedNodeCount,
		"createdEdgeCount": applyResp.CreatedEdgeCount,
		"updatedNodeCount": applyResp.UpdatedNodeCount,
		"updatedEdgeCount": applyResp.UpdatedEdgeCount,
	})
	warnings = append(warnings, applyResp.Warnings...)
	result := map[string]any{
		"plan":        plan,
		"applyResult": applyResp,
	}
	return result, preview, uniqueStringsLocal(warnings), false, nil
}

func (h *HorizonHandlers) resolveLensConsolePlan(ctx context.Context, lens *horizonmodels.Lens, tool string, input map[string]any) (*horizonmodels.GraphIntentPlanResponse, map[string]any, []string, error) {
	if rawPlan, ok := input["plan"]; ok && rawPlan != nil {
		plan, err := decodeLensConsolePlan(rawPlan)
		if err != nil {
			return nil, nil, nil, err
		}
		normalizeGraphIntentPlanLoose(plan)
		if err := plan.NormalizeAndValidate(); err != nil {
			return nil, nil, nil, err
		}
		preview := map[string]any{
			"intentClass":          plan.IntentClass,
			"confidence":           plan.Confidence,
			"requiresConfirmation": plan.RequiresConfirmation,
			"actionPreview":        plan.MapToActions(),
		}
		return plan, preview, nil, nil
	}
	return h.lensConsolePlanChanges(ctx, lens, tool, input)
}

func decodeLensConsolePlan(raw any) (*horizonmodels.GraphIntentPlanResponse, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal plan: %w", err)
	}
	var plan horizonmodels.GraphIntentPlanResponse
	if err := json.Unmarshal(b, &plan); err != nil {
		return nil, fmt.Errorf("decode plan: %w", err)
	}
	return &plan, nil
}

func (h *HorizonHandlers) loadLensConsoleGraphContext(ctx context.Context, lens *horizonmodels.Lens) (*lensConsoleGraphContext, error) {
	if lens == nil {
		return nil, fmt.Errorf("lens is required")
	}
	out := &lensConsoleGraphContext{
		ScopePaths:     lensChatScopePaths(lens),
		FragmentByID:   map[string]*horizonmodels.HorizonFragment{},
		FragmentByPath: map[string]*horizonmodels.HorizonFragment{},
		ItemByID:       map[string]*horizonmodels.LensItem{},
	}
	if h.service != nil && h.service.Fragments() != nil && len(out.ScopePaths) > 0 {
		fragments, err := h.service.Fragments().ListByCanonicalPaths(ctx, out.ScopePaths)
		if err != nil {
			out.Warnings = append(out.Warnings, fmt.Sprintf("failed to load scope fragments: %v", err))
		} else {
			out.Fragments = fragments
			for _, frag := range fragments {
				if frag == nil {
					continue
				}
				out.FragmentByID[strings.TrimSpace(frag.ID)] = frag
				out.FragmentByPath[strings.TrimSpace(frag.CanonicalPath)] = frag
			}
		}
	}
	if h.lensItemsHandler != nil && h.lensItemsHandler.lensItemsRepo != nil {
		items, err := h.lensItemsHandler.lensItemsRepo.ListByLens(ctx, strings.TrimSpace(lens.ID))
		if err != nil {
			out.Warnings = append(out.Warnings, fmt.Sprintf("failed to load lens items: %v", err))
		} else {
			out.Items = items
			for _, item := range items {
				if item == nil {
					continue
				}
				out.ItemByID[strings.TrimSpace(item.ID)] = item
			}
		}
	}
	out.Snapshot, out.Summary = buildLensConsoleSnapshot(out.Fragments, out.Items)
	out.Warnings = uniqueStringsLocal(out.Warnings)
	return out, nil
}

func buildLensConsoleSnapshot(fragments []*horizonmodels.HorizonFragment, items []*horizonmodels.LensItem) (horizonmodels.GraphIntentSnapshot, horizonmodels.LensConsoleGraphSummary) {
	snapshot := horizonmodels.GraphIntentSnapshot{
		Nodes: make([]horizonmodels.GraphIntentSnapshotNode, 0, len(fragments)+len(items)),
		Edges: make([]horizonmodels.GraphIntentSnapshotEdge, 0, len(items)),
		ViewState: horizonmodels.GraphIntentViewState{
			Zoom: 1.0,
		},
	}
	summary := horizonmodels.LensConsoleGraphSummary{
		ObjectCountsByType: map[string]int{},
	}

	nodeIDsByFragmentID := map[string]string{}
	nodeIDsByCanonicalPath := map[string]string{}
	seenNodeIDs := map[string]struct{}{}
	for _, item := range items {
		if item == nil || strings.ToLower(strings.TrimSpace(item.Kind)) != "node" {
			continue
		}
		nodeID := strings.TrimSpace(item.ID)
		if nodeID == "" {
			nodeID = strings.TrimSpace(stringPtrValue(item.FragmentID))
		}
		if nodeID == "" {
			nodeID = strings.TrimSpace(stringPtrValue(item.CanonicalPath))
		}
		if nodeID == "" {
			continue
		}
		if _, ok := seenNodeIDs[nodeID]; ok {
			continue
		}
		node := horizonmodels.GraphIntentSnapshotNode{
			NodeID:        nodeID,
			FragmentID:    strings.TrimSpace(stringPtrValue(item.FragmentID)),
			CanonicalPath: strings.TrimSpace(stringPtrValue(item.CanonicalPath)),
			Label:         strings.TrimSpace(stringPtrValue(item.Label)),
			Type:          strings.TrimSpace(stringPtrValue(item.Type)),
		}
		if item.Position != nil {
			node.Position = &horizonmodels.XYPoint{X: item.Position.X, Y: item.Position.Y}
		}
		if node.Label == "" {
			node.Label = humanizeCanonicalPath(node.CanonicalPath)
		}
		snapshot.Nodes = append(snapshot.Nodes, node)
		seenNodeIDs[nodeID] = struct{}{}
		if node.FragmentID != "" {
			nodeIDsByFragmentID[node.FragmentID] = nodeID
		}
		if node.CanonicalPath != "" {
			nodeIDsByCanonicalPath[node.CanonicalPath] = nodeID
		}
		summary.ObjectCountsByType[node.Type]++
	}

	for _, frag := range fragments {
		if frag == nil {
			continue
		}
		nodeID := strings.TrimSpace(frag.ID)
		if nodeID == "" {
			nodeID = strings.TrimSpace(frag.CanonicalPath)
		}
		if nodeID == "" {
			continue
		}
		if _, ok := seenNodeIDs[nodeID]; ok {
			continue
		}
		node := horizonmodels.GraphIntentSnapshotNode{
			NodeID:        nodeID,
			FragmentID:    strings.TrimSpace(frag.ID),
			CanonicalPath: strings.TrimSpace(frag.CanonicalPath),
			Label:         lensConsoleFragmentLabel(frag),
			Type:          strings.TrimSpace(string(frag.Class)),
		}
		snapshot.Nodes = append(snapshot.Nodes, node)
		seenNodeIDs[nodeID] = struct{}{}
		if node.FragmentID != "" {
			nodeIDsByFragmentID[node.FragmentID] = nodeID
		}
		if node.CanonicalPath != "" {
			nodeIDsByCanonicalPath[node.CanonicalPath] = nodeID
		}
		summary.ObjectCountsByType[node.Type]++
	}

	for _, item := range items {
		if item == nil || strings.ToLower(strings.TrimSpace(item.Kind)) != "edge" {
			continue
		}
		srcFragID := strings.TrimSpace(stringPtrValue(item.SourceFragmentID))
		dstFragID := strings.TrimSpace(stringPtrValue(item.TargetFragmentID))
		srcNodeID := nodeIDsByFragmentID[srcFragID]
		dstNodeID := nodeIDsByFragmentID[dstFragID]
		if srcNodeID == "" {
			srcNodeID = srcFragID
		}
		if dstNodeID == "" {
			dstNodeID = dstFragID
		}
		edge := horizonmodels.GraphIntentSnapshotEdge{
			EdgeID:           strings.TrimSpace(item.ID),
			SourceNodeID:     srcNodeID,
			TargetNodeID:     dstNodeID,
			SourceFragmentID: srcFragID,
			TargetFragmentID: dstFragID,
			RelationType:     firstNonEmptyString(strings.TrimSpace(stringPtrValue(item.EdgeType)), strings.TrimSpace(stringPtrValue(item.Subkind))),
			Direction:        strings.TrimSpace(stringPtrValue(item.Direction)),
		}
		if edge.RelationType == "" {
			edge.RelationType = "RELATED_TO"
		}
		snapshot.Edges = append(snapshot.Edges, edge)
	}

	summary.NodeCount = len(snapshot.Nodes)
	summary.EdgeCount = len(snapshot.Edges)
	summary.SelectedNodeIDs = append([]string(nil), snapshot.SelectedNodeIDs...)
	summary.VisibleNodeIDs = append([]string(nil), snapshot.VisibleNodeIDs...)
	return compactGraphIntentSnapshot(snapshot), summary
}

func (h *HorizonHandlers) findObjectsInGraph(query string, graphCtx *lensConsoleGraphContext, limit int) []map[string]any {
	if graphCtx == nil {
		return nil
	}
	q := normalizeLensConsoleSearchText(query)
	if q == "" {
		return nil
	}
	candidates := make([]lensConsoleRankedCandidate, 0, len(graphCtx.Fragments))
	for _, frag := range graphCtx.Fragments {
		if frag == nil {
			continue
		}
		text := normalizeLensConsoleSearchText(strings.Join([]string{
			lensConsoleFragmentLabel(frag),
			frag.CanonicalPath,
			frag.Description,
			strings.Join(frag.AlternativeNames, " "),
		}, " "))
		score := scoreLensConsoleQuery(q, text)
		if score <= 0 {
			continue
		}
		candidates = append(candidates, lensConsoleRankedCandidate{
			Score: score,
			Data:  lensConsoleFragmentCandidate(frag, score),
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			return asStringAny(candidates[i].Data["label"]) < asStringAny(candidates[j].Data["label"])
		}
		return candidates[i].Score > candidates[j].Score
	})
	out := make([]map[string]any, 0, minIntLocal(limit, len(candidates)))
	for _, candidate := range candidates {
		out = append(out, candidate.Data)
		if len(out) >= limit {
			break
		}
	}
	return out
}

type lensConsoleRankedCandidate struct {
	Score float64
	Data  map[string]any
}

func lensConsoleFragmentCandidate(frag *horizonmodels.HorizonFragment, score float64) map[string]any {
	if frag == nil {
		return nil
	}
	return map[string]any{
		"fragmentId":    strings.TrimSpace(frag.ID),
		"canonicalPath": strings.TrimSpace(frag.CanonicalPath),
		"label":         lensConsoleFragmentLabel(frag),
		"type":          strings.TrimSpace(string(frag.Class)),
		"description":   strings.TrimSpace(frag.Description),
		"score":         score,
	}
}

func summarizeLensConsoleFragments(fragments []*horizonmodels.HorizonFragment, limit int) []map[string]any {
	out := make([]map[string]any, 0, minIntLocal(len(fragments), limit))
	for _, frag := range fragments {
		if frag == nil {
			continue
		}
		out = append(out, lensConsoleFragmentCandidate(frag, 0))
		if len(out) >= limit {
			break
		}
	}
	return out
}

func dedupeLensConsoleCandidates(items []map[string]any, limit int) []map[string]any {
	out := make([]map[string]any, 0, minIntLocal(len(items), limit))
	seen := map[string]struct{}{}
	for _, item := range items {
		key := strings.TrimSpace(asStringAny(item["canonicalPath"]))
		if key == "" {
			key = strings.TrimSpace(asStringAny(item["fragmentId"]))
		}
		if key == "" {
			key = strings.TrimSpace(asStringAny(item["label"]))
		}
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func lensConsoleFragmentLabel(frag *horizonmodels.HorizonFragment) string {
	if frag == nil {
		return ""
	}
	if frag.Labels != nil {
		for _, key := range []string{"en", "title", "name", "display"} {
			if v := strings.TrimSpace(frag.Labels[key]); v != "" {
				return v
			}
		}
	}
	if frag.Identifiers != nil {
		for _, key := range []string{"shortName", "legalName", "name"} {
			if v := strings.TrimSpace(frag.Identifiers[key]); v != "" {
				return v
			}
		}
	}
	return humanizeCanonicalPath(strings.TrimSpace(frag.CanonicalPath))
}

func normalizeLensConsoleSearchText(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func scoreLensConsoleQuery(query, haystack string) float64 {
	if query == "" || haystack == "" {
		return 0
	}
	score := 0.0
	if strings.Contains(haystack, query) {
		score += 1.0
	}
	for _, token := range strings.Fields(query) {
		if len(token) < 2 {
			continue
		}
		if strings.Contains(haystack, token) {
			score += 0.35
		}
	}
	return score
}

func clampIntAny(v any, fallback, minValue, maxValue int) int {
	out := fallback
	switch typed := v.(type) {
	case int:
		out = typed
	case int32:
		out = int(typed)
	case int64:
		out = int(typed)
	case float64:
		out = int(typed)
	case json.Number:
		if i, err := typed.Int64(); err == nil {
			out = int(i)
		}
	}
	if out < minValue {
		return minValue
	}
	if out > maxValue {
		return maxValue
	}
	return out
}

func asBoolAny(v any) bool {
	switch typed := v.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "1", "true", "yes", "y":
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func structToMap(v any) map[string]any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	b, err := json.Marshal(v)
	if err != nil {
		return map[string]any{"value": fmt.Sprintf("%v", v)}
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return map[string]any{"value": string(b)}
	}
	return out
}

func mergeMapsLocal(base, extra map[string]any) map[string]any {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func sortedMapKeys(m map[string]any) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (h *HorizonHandlers) publishLensConsoleEvent(ctx context.Context, enabled bool, tenantID, projectID, lensID, runID, tool, phase string, payload map[string]any) {
	if !enabled || h == nil || h.service == nil {
		return
	}
	pub := h.service.RealtimePublisher()
	if pub == nil {
		return
	}
	body := map[string]any{
		"tenantId":  strings.TrimSpace(tenantID),
		"projectId": strings.TrimSpace(projectID),
		"lensId":    strings.TrimSpace(lensID),
		"runId":     strings.TrimSpace(runID),
		"tool":      strings.TrimSpace(tool),
		"phase":     strings.TrimSpace(phase),
		"ts":        time.Now().UTC().Format(time.RFC3339Nano),
	}
	for k, v := range payload {
		body[k] = v
	}
	_ = pub.PublishJSON(ctx, realtime.EventType("horizon.console.tool."+strings.TrimSpace(phase)), strings.TrimSpace(tenantID), body)
}
