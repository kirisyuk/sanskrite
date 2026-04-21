package horizon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/igorkirisyuk/sanskrite/internal/ai"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	horizondomain "github.com/igorkirisyuk/sanskrite/internal/horizon/domain"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/merge"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonnaming "github.com/igorkirisyuk/sanskrite/internal/horizon/naming"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/rtctx"
	horizonscout "github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	horizonagents "github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/trace"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

const (
	graphIntentApplyHardMaxCreateNodes = 200
	graphIntentApplyHardMaxCreateEdges = 400
	graphIntentApplyHardMaxRewriteOps  = 250
)

var errGraphIntentPlanValidation = errors.New("graph intent plan validation failed")

// PlanGraphIntent handles:
// POST /api/horizon/lenses/{lensId}/graph-intent/plan
func (h *HorizonHandlers) PlanGraphIntent(w http.ResponseWriter, r *http.Request) {
	lensId := graphIntentLensIdFromRequest(r)
	corrID := strings.TrimSpace(r.Header.Get("X-Correlation-Id"))

	if r.Method != http.MethodPost {
		logGraphIntentErr("plan", "method", http.StatusMethodNotAllowed, lensId, corrID, nil)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	authInfo, err := requireAuth(r)
	if err != nil || authInfo == nil {
		logGraphIntentErr("plan", "auth", http.StatusUnauthorized, lensId, corrID, err)
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	lens, err := h.resolveGraphIntentLens(r.Context(), r, authInfo.TenantID)
	if err != nil {
		logGraphIntentErr("plan", "resolve_lens", http.StatusBadRequest, lensId, corrID, err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	lensId = lens.ID
	if h.genAIHandler == nil {
		logGraphIntentErr("plan", "genai_unconfigured", http.StatusServiceUnavailable, lensId, corrID, nil)
		http.Error(w, "GenAI handler not configured", http.StatusServiceUnavailable)
		return
	}

	var req horizonmodels.GraphIntentPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logGraphIntentErr("plan", "decode", http.StatusBadRequest, lensId, corrID, err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if err := req.NormalizeAndValidate(); err != nil {
		logGraphIntentErr("plan", "req_validate", http.StatusBadRequest, lensId, corrID, err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Contract: execution stays in English.
	req.ExecutionLanguage = horizonmodels.GraphIntentExecutionLanguageEN

	if corrID == "" {
		corrID = uuid.NewString()
	}
	sessID := strings.TrimSpace(r.Header.Get("X-Session-Id"))
	if sessID == "" {
		sessID = uuid.NewString()
	}
	ctx := rtctx.With(r.Context(), rtctx.Context{
		TenantID:      authInfo.TenantID,
		ProjectID:     lens.ProjectID,
		LensID:        lens.ID,
		CorrelationID: corrID,
		SessionID:     sessID,
	})
	trace.Log(ctx, "graph_intent", "plan.request", map[string]any{
		"lensId":        lens.ID,
		"projectId":     lens.ProjectID,
		"rawQuery":      trace.Trunc(req.RawQuery, 200),
		"snapshotNodes": len(req.Snapshot.Nodes),
		"snapshotEdges": len(req.Snapshot.Edges),
		"maxNewNodes":   req.Options.MaxNewNodes,
		"maxNewEdges":   req.Options.MaxNewEdges,
	})

	plan, err := h.generateGraphIntentPlan(ctx, lens, &req)
	if err != nil {
		status := http.StatusInternalServerError
		errMsg := "Failed to generate graph intent plan"
		if errors.Is(err, errGraphIntentPlanValidation) {
			status = http.StatusBadRequest
			errMsg = "plan validation failed"
		}
		logGraphIntentErr("plan", "generate", status, lens.ID, corrID, err)
		trace.Log(ctx, "graph_intent", "plan.error", map[string]any{
			"lensId": lens.ID, "error": err.Error(),
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":  errMsg,
			"detail": err.Error(),
		})
		return
	}
	if plan == nil {
		logGraphIntentErr("plan", "empty", http.StatusInternalServerError, lens.ID, corrID, fmt.Errorf("planner returned no plan"))
		trace.Log(ctx, "graph_intent", "plan.empty", map[string]any{"lensId": lens.ID})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Empty graph intent plan", "detail": "planner returned no plan"})
		return
	}
	normalizeGraphIntentPlanLoose(plan)
	// Validation is strict in generateGraphIntentPlan; only valid plans reach this point.
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

	trace.Log(ctx, "graph_intent", "plan.ok", map[string]any{
		"lensId":               lens.ID,
		"normalizedQuery":      trace.Trunc(plan.NormalizedEnglishQuery, 120),
		"intentClass":          plan.IntentClass,
		"confidence":           plan.Confidence,
		"requiresConfirmation": plan.RequiresConfirmation,
		"groupingsN":           len(plan.Create.Groupings),
		"createNodesN":         len(plan.Create.Nodes),
		"createEdgesN":         len(plan.Create.Edges),
		"estimatedCreateNodes": createdNodes,
		"estimatedCreateEdges": createdEdges,
		"warningsN":            len(plan.Warnings),
	})

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
	lensId := graphIntentLensIdFromRequest(r)
	corrID := strings.TrimSpace(r.Header.Get("X-Correlation-Id"))

	if r.Method != http.MethodPost {
		logGraphIntentErr("apply", "method", http.StatusMethodNotAllowed, lensId, corrID, nil)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	authInfo, err := requireAuth(r)
	if err != nil || authInfo == nil {
		logGraphIntentErr("apply", "auth", http.StatusUnauthorized, lensId, corrID, err)
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	lens, err := h.resolveGraphIntentLens(r.Context(), r, authInfo.TenantID)
	if err != nil {
		logGraphIntentErr("apply", "resolve_lens", http.StatusBadRequest, lensId, corrID, err)
		writeGraphIntentJSON(w, http.StatusBadRequest, map[string]string{"error": "lens resolution failed", "detail": err.Error()})
		return
	}
	lensId = lens.ID
	if corrID == "" {
		corrID = uuid.NewString()
	}
	sessID := strings.TrimSpace(r.Header.Get("X-Session-Id"))
	if sessID == "" {
		sessID = uuid.NewString()
	}
	applyCtx := rtctx.With(r.Context(), rtctx.Context{
		TenantID:      authInfo.TenantID,
		ProjectID:     lens.ProjectID,
		LensID:        lens.ID,
		CorrelationID: corrID,
		SessionID:     sessID,
	})

	var req horizonmodels.GraphIntentApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logGraphIntentErr("apply", "decode", http.StatusBadRequest, lensId, corrID, err)
		writeGraphIntentJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body", "detail": err.Error()})
		return
	}
	if forceDryRun {
		req.ApplyMode = horizonmodels.GraphIntentApplyModeDry
	}
	if err := req.NormalizeAndValidate(); err != nil {
		logGraphIntentErr("apply", "req_validate", http.StatusBadRequest, lensId, corrID, err)
		writeGraphIntentJSON(w, http.StatusBadRequest, map[string]string{"error": "request validation failed", "detail": err.Error()})
		return
	}
	normalizeGraphIntentPlanLoose(&req.Plan)
	if err := req.Plan.NormalizeAndValidate(); err != nil {
		logGraphIntentErr("apply", "plan_validate", http.StatusBadRequest, lensId, corrID, err)
		writeGraphIntentJSON(w, http.StatusBadRequest, map[string]string{"error": "plan validation failed", "detail": err.Error()})
		return
	}

	createdNodes, createdEdges := req.Plan.EstimatedCreateCounts()
	if createdNodes > graphIntentApplyHardMaxCreateNodes {
		err := fmt.Errorf("create nodes exceed limit: %d > %d", createdNodes, graphIntentApplyHardMaxCreateNodes)
		logGraphIntentErr("apply", "limit_nodes", http.StatusBadRequest, lensId, corrID, err)
		writeGraphIntentJSON(w, http.StatusBadRequest, map[string]string{"error": "apply limit exceeded", "detail": err.Error()})
		return
	}
	if createdEdges > graphIntentApplyHardMaxCreateEdges {
		err := fmt.Errorf("create edges exceed limit: %d > %d", createdEdges, graphIntentApplyHardMaxCreateEdges)
		logGraphIntentErr("apply", "limit_edges", http.StatusBadRequest, lensId, corrID, err)
		writeGraphIntentJSON(w, http.StatusBadRequest, map[string]string{"error": "apply limit exceeded", "detail": err.Error()})
		return
	}

	isDryRun := req.ApplyMode == horizonmodels.GraphIntentApplyModeDry
	if !isDryRun && (req.Plan.RequiresConfirmation || req.Plan.IsPotentiallyDestructive()) && !graphIntentApplyConfirmed(r) {
		logGraphIntentErr("apply", "confirmation_required", http.StatusConflict, lensId, corrID, fmt.Errorf("resend with confirm=true"))
		trace.Log(applyCtx, "graph_intent", "apply.confirmation_required", map[string]any{
			"lensId": lens.ID, "idempotencyKey": trace.Trunc(req.IdempotencyKey, 36),
		})
		resp := horizonmodels.GraphIntentApplyResponse{
			Applied:       false,
			Warnings:      []string{"confirmation required for potentially destructive plan; resend with confirm=true"},
			ActionPreview: req.Plan.MapToActions(),
		}
		writeGraphIntentJSON(w, http.StatusConflict, resp)
		return
	}

	trace.Log(applyCtx, "graph_intent", "apply.start", map[string]any{
		"lensId":         lens.ID,
		"idempotencyKey": trace.Trunc(req.IdempotencyKey, 36),
		"dryRun":         isDryRun,
		"intentClass":    req.Plan.IntentClass,
	})

	exec := &graphIntentApplyExecutor{
		handler:              h,
		ctx:                  r.Context(),
		lens:                 lens,
		dryRun:               isDryRun,
		maxRewriteTouchCount: graphIntentApplyHardMaxRewriteOps,
	}
	if err := exec.init(); err != nil {
		logGraphIntentErr("apply", "init", http.StatusServiceUnavailable, lensId, corrID, err)
		trace.Log(applyCtx, "graph_intent", "apply.init_error", map[string]any{
			"lensId": lens.ID, "error": err.Error(),
		})
		writeGraphIntentJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Graph intent apply is not available", "detail": err.Error()})
		return
	}

	resp := exec.execute(&req.Plan)
	resp.ActionPreview = req.Plan.MapToActions()
	resp.Applied = !isDryRun && len(resp.UnresolvedItems) == 0
	if isDryRun {
		resp.Applied = false
	}

	trace.Log(applyCtx, "graph_intent", "apply.ok", map[string]any{
		"lensId":           lens.ID,
		"applied":          resp.Applied,
		"dryRun":           isDryRun,
		"createdNodeCount": resp.CreatedNodeCount,
		"createdEdgeCount": resp.CreatedEdgeCount,
		"updatedNodeCount": resp.UpdatedNodeCount,
		"updatedEdgeCount": resp.UpdatedEdgeCount,
		"deletedNodeCount": resp.DeletedNodeCount,
		"deletedEdgeCount": resp.DeletedEdgeCount,
		"unresolvedItemsN": len(resp.UnresolvedItems),
		"warningsN":        len(resp.Warnings),
	})

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
	allowedNodeTypes := horizonmodels.GraphIntentAllowedNodeTypesSorted()
	allowedRelations := horizonmodels.GraphIntentAllowedRelationTypesSorted()
	allowedGroupingKinds := horizonmodels.GraphIntentAllowedGroupingKindsSorted()
	nodeTypeDescriptions := taxonomy.GraphIntentNodeTypeDescriptions(allowedNodeTypes)
	relationTypeDescriptions := taxonomy.GraphIntentRelationTypeDescriptions(allowedRelations)
	groupingKindDescriptions := taxonomy.GraphIntentGroupingKindDescriptions(allowedGroupingKinds)
	sharedPromptBlock, activeDomainModes := buildGraphIntentSharedPromptBlocks(snapshot)
	schema := horizonmodels.GraphIntentPlanResponseSchemaDraft07()
	schemaJSON, _ := json.Marshal(schema)
	snapshotJSON, _ := json.Marshal(snapshot)

	systemInstruction := strings.TrimSpace(`
You are a Graph Intent planner for a knowledge graph editor.
Return ONLY a single JSON object. No markdown, no code fences, no extra text.

Required output structure (exact keys):
{
  "version": "graph-intent.v1",
  "normalizedEnglishQuery": "<English translation of user query>",
  "intentClass": "create|update|delete|merge|transform|mixed|unknown",
  "confidence": <0.0-1.0>,
  "create": { "groupings": [], "nodes": [...], "edges": [...] },
  "update": { "nodePatches": [], "edgePatches": [], "relationRewrite": [] },
  "delete": { "nodeIds": [], "edgeIds": [] },
  "merge": { "primaryNodeId": "", "duplicateNodeIds": [], "mergeFragmentData": true },
  "transform": { "deriveChains": [], "restructure": [] },
  "requiresConfirmation": false
}

Every create.nodes and groupings.entities item MUST have: label, type, canonicalPathHint (path starting with /). Edges use fromTempId/toTempId or fromNodeId/toNodeId.

Rules:
- The user query may be in any language.
- normalizedEnglishQuery MUST be in English.
- You must reason over the provided graph snapshot (current visible graph).
- Do NOT run or request scouting.
- Output a structured plan only.
- Use ONLY allowed groupingKind, node types, and relation types from the prompt.
- Keep plan conservative: if uncertain, lower confidence and add warnings.
- If operation can be destructive (broad relation rewrites, deletions), set requiresConfirmation=true.
- Keep output compact and executable.
- Forbidden path forms (never use): /policy/*, /geo/*, /economy/*, /society/*, /org/gov/*.
- CRITICAL: Every node (create.nodes, groupings.entities) MUST have tempId and canonicalPathHint. Edges use fromTempId/toTempId for new nodes, fromNodeId/toNodeId for existing snapshot nodes.
- CRITICAL: DOMAIN canonicalPathHint MUST follow the shared domain contract syntax /domain/v1/{contractType}/{facetKey}/{facetValue}... with even key/value facets only.
- CRITICAL: PRODUCT canonicalPathHint must represent canonical product identity. Do not invent scenario-specific product slugs when the scenario belongs in a DOMAIN or label/title.
- DELETE: Use delete.nodeIds to remove obsolete nodes. Use delete.edgeIds to remove specific edges. Node deletion cascades to its edges.
- MERGE: For "merge duplicates" or "remove duplicates, keep one" use merge block. Set merge.primaryNodeId to the canonical node (from snapshot), merge.duplicateNodeIds to the duplicates. The system will: reassign edges from duplicates to primary (with dedup), optionally merge fragment data (Labels, State, merged_paths), then delete duplicates. Prefer merge over update+delete for duplicate consolidation.
`) + "\n\n" + taxonomy.GraphIntentCanonicalPathRulesPrompt()

	exampleOutput := `{
  "version": "graph-intent.v1",
  "normalizedEnglishQuery": "Add governance structure for X",
  "intentClass": "create",
  "confidence": 0.9,
  "create": {
    "groupings": [],
    "nodes": [
      {"tempId": "n1", "label": "Entity A", "type": "ORGANIZATION", "canonicalPathHint": "/org/us/entity-a-inc"},
      {"tempId": "n2", "label": "Entity B", "type": "PERSON", "canonicalPathHint": "/person/entity-b"}
    ],
    "edges": [
      {"fromTempId": "n1", "toTempId": "n2", "relationType": "EMPLOYED_BY", "direction": "forward"}
    ]
  },
  "update": {},
  "delete": {},
  "transform": {},
  "requiresConfirmation": false
}`

	exampleMergeOutput := `{
  "version": "graph-intent.v1",
  "normalizedEnglishQuery": "Merge duplicate Astana Akimat nodes into a single primary entity",
  "intentClass": "merge",
  "confidence": 0.95,
  "create": {},
  "update": {"nodePatches": [{"nodeId": "cp_1da8c93544d4b313205a", "label": "Astana Akimat", "type": "GOVERNMENT_AGENCY"}]},
  "delete": {},
  "merge": {
    "primaryNodeId": "cp_1da8c93544d4b313205a",
    "duplicateNodeIds": ["cp_33cdb0ab875032572677", "cp_6dea39e6339e15a2e4f9"],
    "mergeFragmentData": true
  },
  "transform": {},
  "explanations": ["Consolidated duplicates into primary. Edges reassigned and deduplicated, fragment data merged, duplicates removed."],
  "requiresConfirmation": true
}`

	prompt := strings.TrimSpace(fmt.Sprintf(`
Now (UTC): %s
Contract version: %s
Execution language: %s

Lens: %s / %s

Request:
- rawQuery: %s
- queryLanguage: %s
- allowSearchGrounding: %t, allowInternalGrounding: %t
- maxNewNodes: %d, maxNewEdges: %d

%s

%s

%s

Shared composable prompt contracts reused from the pipeline:
%s

Graph snapshot JSON:
%s

Example output structure (follow this format exactly):
%s

Example merge output structure:
%s

Full JSON schema (draft-07):
%s

Return exactly one JSON object. No markdown, no code fences. Just the raw JSON.
`, time.Now().UTC().Format(time.RFC3339), horizonmodels.GraphIntentVersionV1, horizonmodels.GraphIntentExecutionLanguageEN, lens.ID, lens.ProjectID, req.RawQuery, req.QueryLanguage, req.Options.AllowSearchGrounding, req.Options.AllowInternalGrounding, req.Options.MaxNewNodes, req.Options.MaxNewEdges, groupingKindDescriptions, nodeTypeDescriptions, relationTypeDescriptions, sharedPromptBlock, string(snapshotJSON), exampleOutput, exampleMergeOutput, string(schemaJSON)))

	llmReq := gen_combined.GenAIRequest{
		Model:             "", // Model Manager picks via MinCapabilityScore=7
		Prompt:            prompt,
		SystemInstruction: systemInstruction,
		WebSearchEnabled:  false,
		MaxOutputTokens:   65536,
		Temperature:       0.1,
		ProjectID:         strings.TrimSpace(lens.ProjectID),
		StrictJSONEnabled: true,
		JSONSchema:        schema,
	}
	trace.Log(ctx, "graph_intent", "plan.llm_request", map[string]any{
		"lensId":                 lens.ID,
		"model":                  llmReq.Model,
		"strictJSONEnabled":      llmReq.StrictJSONEnabled,
		"webSearchEnabled":       llmReq.WebSearchEnabled,
		"requestedWebSearch":     req.Options.AllowSearchGrounding,
		"maxOutputTokensBudget":  llmReq.MaxOutputTokens,
		"promptChars":            len(prompt),
		"systemInstructionChars": len(systemInstruction),
		"schemaChars":            len(schemaJSON),
		"snapshotChars":          len(snapshotJSON),
		"activeDomainModes":      activeDomainModes,
	})
	llmStarted := time.Now()

	genResp, err := h.genAIHandler.GenerateStructuredViaTaskExecutor(ctx, llmReq, 7)
	if err != nil || genResp == nil {
		trace.Log(ctx, "graph_intent", "plan.llm_error", map[string]any{
			"lensId": lens.ID, "error": err.Error(),
		})
		return nil, fmt.Errorf("planner call failed")
	}
	llmElapsedMs := time.Since(llmStarted).Milliseconds()

	// Log LLM response for AI diagnosis (sufficient for debugging)
	contentLen := len(genResp.Content)
	contentPreview := trace.Trunc(genResp.Content, 6000)
	log.Printf("[GraphIntent] plan.llm_response lensId=%s model=%s llmElapsedMs=%d contentLen=%d usage(prompt=%d output=%d reasoning=%d total=%d) contentPreview=%s",
		lens.ID, strings.TrimSpace(genResp.Model), llmElapsedMs, contentLen,
		genResp.Usage.PromptTokens, genResp.Usage.OutputTokens, genResp.Usage.ReasoningTokens, genResp.Usage.TotalTokens,
		contentPreview,
	)
	trace.Log(ctx, "graph_intent", "plan.llm_response", map[string]any{
		"lensId":         lens.ID,
		"model":          strings.TrimSpace(genResp.Model),
		"llmElapsedMs":   llmElapsedMs,
		"contentLen":     contentLen,
		"contentPreview": trace.Trunc(genResp.Content, 2000),
		"usagePrompt":    genResp.Usage.PromptTokens,
		"usageOutput":    genResp.Usage.OutputTokens,
		"usageTotal":     genResp.Usage.TotalTokens,
		"usageReasoning": genResp.Usage.ReasoningTokens,
		"finishReason":   extractGeminiFinishReason(genResp.RawJSON),
	})

	// Parse JSON
	var plan horizonmodels.GraphIntentPlanResponse
	baseVerifier := &ai.BaseVerifier{}
	cleaned, cleanErr := baseVerifier.CleanAndFixJSON(genResp.Content)
	if cleanErr != nil {
		log.Printf("[GraphIntent] plan.parse_failed lensId=%s stage=clean err=%v rawTail=%s", lens.ID, cleanErr, tailForLog(genResp.Content, 1600))
		trace.Log(ctx, "graph_intent", "plan.parse_failed", map[string]any{
			"lensId":         lens.ID,
			"stage":          "clean",
			"error":          cleanErr.Error(),
			"contentLen":     contentLen,
			"contentPreview": contentPreview,
			"rawTail":        tailForLog(genResp.Content, 800),
			"finishReason":   extractGeminiFinishReason(genResp.RawJSON),
			"usagePrompt":    genResp.Usage.PromptTokens,
			"usageOutput":    genResp.Usage.OutputTokens,
			"usageTotal":     genResp.Usage.TotalTokens,
		})
		return nil, fmt.Errorf("planner JSON parse failed: %w", cleanErr)
	}
	if err := json.Unmarshal([]byte(cleaned), &plan); err != nil {
		log.Printf("[GraphIntent] plan.parse_failed lensId=%s stage=unmarshal err=%v cleanedLen=%d cleanedTail=%s", lens.ID, err, len(cleaned), tailForLog(cleaned, 1200))
		trace.Log(ctx, "graph_intent", "plan.parse_failed", map[string]any{
			"lensId":       lens.ID,
			"stage":        "unmarshal",
			"error":        err.Error(),
			"cleanedLen":   len(cleaned),
			"cleanedTail":  tailForLog(cleaned, 600),
			"contentLen":   contentLen,
			"finishReason": extractGeminiFinishReason(genResp.RawJSON),
			"usagePrompt":  genResp.Usage.PromptTokens,
			"usageOutput":  genResp.Usage.OutputTokens,
			"usageTotal":   genResp.Usage.TotalTokens,
		})
		return nil, fmt.Errorf("planner JSON parse failed: %w", err)
	}
	normalizeGraphIntentPlanLoose(&plan)
	h.repairGraphIntentPlanWithResolvers(ctx, lens, snapshot, strings.TrimSpace(req.RawQuery), &plan)
	if err := plan.NormalizeAndValidate(); err != nil {
		log.Printf("[GraphIntent] plan.validation_failed lensId=%s err=%v contentPreview=%s", lens.ID, err, contentPreview)
		trace.Log(ctx, "graph_intent", "plan.validation_failed", map[string]any{
			"lensId": lens.ID, "validationError": err.Error(), "contentLen": contentLen, "contentPreview": contentPreview,
		})
		return nil, fmt.Errorf("%w: %v", errGraphIntentPlanValidation, err)
	}

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

func buildGraphIntentSharedPromptBlocks(snapshot horizonmodels.GraphIntentSnapshot) (string, []string) {
	activeDomainModes := graphIntentActiveDomainModes(snapshot)
	blocks := []string{
		"NAME NORMALIZATION CONTRACT:\n" + strings.TrimSpace(horizonnaming.PromptEntityNameNormalizationRules()),
		"ANCHOR / DISPLAY NAMING CONTRACT:\n" + strings.TrimSpace(horizonnaming.PromptAnchorNameRules()),
		"DOMAIN CONTRACT TYPES (shared with domain resolver):\n" + strings.TrimSpace(taxonomy.DomainContractTypesPromptForLLM()),
		"MARKET / DECOMPOSITION CONTRACT:\n" + strings.TrimSpace(taxonomy.MarketGeographyAndDecompositionPromptBlock()),
		"BUSINESS TAXONOMY CONTRACT:\n" + strings.TrimSpace(horizonagents.PromptCoreBusinessTaxonomyBlock()),
		"GOV VS ORG RELATION CONTRACT:\n" + strings.TrimSpace(horizonagents.PromptGovEntityRelationRulesBlock()),
		"EXTENDED CANONICAL PATH RULES:\n" + strings.TrimSpace(taxonomy.ExtendedCanonicalPathRulesPrompt()),
		"BUSINESS TAG CONTRACT:\n" + strings.TrimSpace(taxonomy.BusinessTagRulesPrompt()),
	}
	if len(activeDomainModes) > 0 {
		modeLines := make([]string, 0, len(activeDomainModes))
		for _, mode := range activeDomainModes {
			mode = strings.TrimSpace(mode)
			if mode == "" {
				continue
			}
			relationTypes := strings.TrimSpace(graphIntentAllowedRelationTypesForMode(mode))
			entityTypes := strings.TrimSpace(taxonomy.EntityTypesForDomainMode(mode))
			modeLines = append(modeLines, fmt.Sprintf("- %s\n  entityTypes: %s\n  relationTypes: %s", mode, entityTypes, relationTypes))
		}
		if len(modeLines) > 0 {
			blocks = append(blocks, "ACTIVE DOMAIN MODES (inferred from visible domain scaffold):\n"+strings.Join(modeLines, "\n"))
		}
	}
	blocks = append(blocks, "RELATION DIRECTION CONTRACT (shared with intent decomposition):\n"+strings.TrimSpace(horizonagents.PromptIntentDecomposerRelationRulesBlock(activeDomainModes)))
	return strings.Join(blocks, "\n\n"), activeDomainModes
}

func graphIntentAllowedRelationTypesForMode(mode string) string {
	shared := strings.Split(strings.TrimSpace(horizonagents.PromptRelationTypesForDomainMode(mode)), ",")
	out := make([]string, 0, len(shared))
	seen := map[string]struct{}{}
	for _, rel := range shared {
		rel = strings.ToUpper(strings.TrimSpace(rel))
		if rel == "" || !horizonmodels.IsGraphIntentRelationTypeAllowed(rel) {
			continue
		}
		if _, ok := seen[rel]; ok {
			continue
		}
		seen[rel] = struct{}{}
		out = append(out, rel)
	}
	if len(out) == 0 {
		return strings.Join(horizonmodels.GraphIntentAllowedRelationTypesSorted(), ", ")
	}
	return strings.Join(out, ", ")
}

func graphIntentActiveDomainModes(snapshot horizonmodels.GraphIntentSnapshot) []string {
	contractTypes := make([]string, 0, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		cp := strings.TrimSpace(node.CanonicalPath)
		if !strings.HasPrefix(cp, "/domain/v1/") {
			continue
		}
		parts := strings.Split(strings.Trim(cp, "/"), "/")
		if len(parts) < 3 {
			continue
		}
		contractTypes = append(contractTypes, strings.TrimSpace(parts[2]))
	}
	modes := horizondomain.DomainModesForContractTypes(contractTypes)
	if len(modes) == 0 {
		return []string{taxonomy.DomainModeGeneral}
	}
	return modes
}

func inferGraphIntentClass(plan *horizonmodels.GraphIntentPlanResponse) string {
	if plan == nil {
		return horizonmodels.GraphIntentClassUnknown
	}
	hasCreate := len(plan.Create.Groupings)+len(plan.Create.Nodes)+len(plan.Create.Edges) > 0
	hasUpdate := len(plan.Update.NodePatches)+len(plan.Update.EdgePatches)+len(plan.Update.RelationRewrite) > 0
	hasDelete := len(plan.Delete.NodeIDs)+len(plan.Delete.EdgeIDs) > 0
	hasMerge := strings.TrimSpace(plan.Merge.PrimaryNodeID) != "" && len(plan.Merge.DuplicateNodeIDs) > 0
	hasTransform := len(plan.Transform.DeriveChains)+len(plan.Transform.Restructure) > 0
	switch {
	case hasCreate && !hasUpdate && !hasDelete && !hasMerge && !hasTransform:
		return horizonmodels.GraphIntentClassCreate
	case !hasCreate && hasUpdate && !hasDelete && !hasMerge && !hasTransform:
		return horizonmodels.GraphIntentClassUpdate
	case !hasCreate && !hasUpdate && hasDelete && !hasMerge && !hasTransform:
		return horizonmodels.GraphIntentClassDelete
	case !hasCreate && !hasUpdate && !hasDelete && hasMerge && !hasTransform:
		return horizonmodels.GraphIntentClassMerge
	case !hasCreate && !hasUpdate && !hasDelete && !hasMerge && hasTransform:
		return horizonmodels.GraphIntentClassTransform
	case hasCreate || hasUpdate || hasDelete || hasMerge || hasTransform:
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
	for i := range plan.Merge.DuplicateNodeIDs {
		plan.Merge.DuplicateNodeIDs[i] = strings.TrimSpace(plan.Merge.DuplicateNodeIDs[i])
	}
	plan.Merge.PrimaryNodeID = strings.TrimSpace(plan.Merge.PrimaryNodeID)
	plan.Warnings = appendUniqueStrings(nil, plan.Warnings...)
	plan.Explanations = appendUniqueStrings(nil, plan.Explanations...)
}

func (h *HorizonHandlers) repairGraphIntentPlanWithResolvers(ctx context.Context, lens *horizonmodels.Lens, snapshot horizonmodels.GraphIntentSnapshot, rawQuery string, plan *horizonmodels.GraphIntentPlanResponse) {
	if h == nil || lens == nil || plan == nil {
		return
	}
	index := newGraphIntentSnapshotIndex(snapshot)
	productPathsByRef := map[string]string{}
	nodeRefsByID := map[string]graphIntentNodeRef{}

	repairNode := func(scope string, spec *horizonmodels.GraphIntentNodeSpec) {
		if spec == nil {
			return
		}
		resolvedPath, warning := h.resolveGraphIntentNodeCanonicalPathHint(ctx, lens, index, rawQuery, spec.Type, spec.Label, spec.CanonicalPathHint)
		if strings.TrimSpace(resolvedPath) != "" && strings.TrimSpace(resolvedPath) != strings.TrimSpace(spec.CanonicalPathHint) {
			plan.Warnings = appendUniqueStrings(plan.Warnings, fmt.Sprintf("%s canonicalPathHint normalized: %s -> %s", scope, strings.TrimSpace(spec.CanonicalPathHint), strings.TrimSpace(resolvedPath)))
			spec.CanonicalPathHint = strings.TrimSpace(resolvedPath)
		}
		if strings.TrimSpace(warning) != "" {
			plan.Warnings = appendUniqueStrings(plan.Warnings, warning)
		}
		refInfo := graphIntentNodeRef{
			CanonicalPath: strings.TrimSpace(spec.CanonicalPathHint),
			Type:          strings.ToUpper(strings.TrimSpace(spec.Type)),
		}
		if tempID := strings.TrimSpace(spec.TempID); tempID != "" {
			nodeRefsByID[tempID] = refInfo
		}
		if strings.EqualFold(strings.TrimSpace(spec.Type), string(ah.FragmentClassProduct)) {
			ref := strings.TrimSpace(firstNonEmptyGraphIntent(spec.TempID, spec.CanonicalPathHint))
			if ref != "" && strings.HasPrefix(strings.TrimSpace(spec.CanonicalPathHint), "/product/") {
				productPathsByRef[ref] = strings.TrimSpace(spec.CanonicalPathHint)
			}
		}
	}

	for gi := range plan.Create.Groupings {
		grouping := &plan.Create.Groupings[gi]
		for ni := range grouping.Entities {
			repairNode(fmt.Sprintf("create.groupings[%d].entities[%d]", gi, ni), &grouping.Entities[ni])
		}
	}
	for ni := range plan.Create.Nodes {
		repairNode(fmt.Sprintf("create.nodes[%d]", ni), &plan.Create.Nodes[ni])
	}

	for _, node := range snapshot.Nodes {
		cp := strings.TrimSpace(node.CanonicalPath)
		refInfo := graphIntentNodeRef{
			CanonicalPath: cp,
			Type:          strings.ToUpper(strings.TrimSpace(node.Type)),
		}
		if nodeID := strings.TrimSpace(node.NodeID); nodeID != "" {
			nodeRefsByID[nodeID] = refInfo
		}
		if fragmentID := strings.TrimSpace(node.FragmentID); fragmentID != "" {
			nodeRefsByID[fragmentID] = refInfo
		}
		if cp == "" || !strings.HasPrefix(cp, "/product/") {
			continue
		}
		if nodeID := strings.TrimSpace(node.NodeID); nodeID != "" {
			productPathsByRef[nodeID] = cp
		}
		if fragmentID := strings.TrimSpace(node.FragmentID); fragmentID != "" {
			productPathsByRef[fragmentID] = cp
		}
	}

	repairEdge := func(scope string, spec *horizonmodels.GraphIntentEdgeSpec) {
		if spec == nil {
			return
		}
		sourceRef := graphIntentEdgeEndpointRef(spec.FromTempID, spec.FromNodeID, nodeRefsByID)
		targetRef := graphIntentEdgeEndpointRef(spec.ToTempID, spec.ToNodeID, nodeRefsByID)
		if normalizedRelation, warning := graphIntentNormalizeUnsupportedRelation(spec.RelationType, sourceRef, targetRef); normalizedRelation != "" && normalizedRelation != strings.TrimSpace(spec.RelationType) {
			plan.Warnings = appendUniqueStrings(plan.Warnings, fmt.Sprintf("%s relationType normalized: %s -> %s", scope, strings.TrimSpace(spec.RelationType), normalizedRelation))
			if warning != "" {
				plan.Warnings = appendUniqueStrings(plan.Warnings, warning)
			}
			spec.RelationType = normalizedRelation
		}
		if h.productDictionary == nil {
			return
		}
		sourcePath, okSrc := graphIntentEdgeEndpointPath(spec.FromTempID, spec.FromNodeID, productPathsByRef)
		targetPath, okDst := graphIntentEdgeEndpointPath(spec.ToTempID, spec.ToNodeID, productPathsByRef)
		if !okSrc || !okDst {
			return
		}
		resolution, err := h.productDictionary.ResolveProductRelationForTenant(ctx, strings.TrimSpace(lens.TenantID), sourcePath, targetPath)
		if err != nil || !resolution.Known {
			return
		}
		normalizedRelation := string(resolution.EdgeType)
		if normalizedRelation != "" && normalizedRelation != strings.TrimSpace(spec.RelationType) {
			plan.Warnings = appendUniqueStrings(plan.Warnings, fmt.Sprintf("%s relationType normalized: %s -> %s", scope, strings.TrimSpace(spec.RelationType), normalizedRelation))
			spec.RelationType = normalizedRelation
		}
		if resolution.Swapped {
			spec.FromTempID, spec.ToTempID = spec.ToTempID, spec.FromTempID
			spec.FromNodeID, spec.ToNodeID = spec.ToNodeID, spec.FromNodeID
			spec.Direction = "forward"
			plan.Warnings = appendUniqueStrings(plan.Warnings, fmt.Sprintf("%s product relation endpoints reordered to canonical upstream/downstream orientation", scope))
		}
	}

	for gi := range plan.Create.Groupings {
		grouping := &plan.Create.Groupings[gi]
		for ei := range grouping.Relations {
			repairEdge(fmt.Sprintf("create.groupings[%d].relations[%d]", gi, ei), &grouping.Relations[ei])
		}
	}
	for ei := range plan.Create.Edges {
		repairEdge(fmt.Sprintf("create.edges[%d]", ei), &plan.Create.Edges[ei])
	}
}

type graphIntentNodeRef struct {
	CanonicalPath string
	Type          string
}

func graphIntentEdgeEndpointRef(tempID string, nodeID string, refs map[string]graphIntentNodeRef) graphIntentNodeRef {
	if ref, ok := refs[strings.TrimSpace(tempID)]; ok {
		return ref
	}
	if ref, ok := refs[strings.TrimSpace(nodeID)]; ok {
		return ref
	}
	return graphIntentNodeRef{}
}

func graphIntentNormalizeUnsupportedRelation(relationType string, sourceRef graphIntentNodeRef, targetRef graphIntentNodeRef) (string, string) {
	relationType = strings.ToUpper(strings.TrimSpace(relationType))
	if relationType == "" {
		return relationType, ""
	}
	// Skip the rewrite only when the wire alias resolves cleanly to an AH
	// edge — Dropped aliases (USED_IN etc.) still need the structural
	// rewrite below since they have no canonical AH successor.
	if decode, ok := horizonmodels.NormalizeGraphIntentRelationType(relationType); ok && !decode.Dropped && strings.TrimSpace(string(decode.Relation)) != "" {
		return relationType, ""
	}
	// Source/target types arrive as either UPPER_SNAKE legacy tokens or AH
	// lower_snake. Normalise both shapes to the AH FragmentClass before
	// matching, so the rule fires regardless of wire dialect.
	sourceClass, _, _ := horizonmodels.MapGraphIntentNodeTypeToClass(sourceRef.Type, "")
	targetClass, _, _ := horizonmodels.MapGraphIntentNodeTypeToClass(targetRef.Type, "")
	switch relationType {
	case "USED_IN":
		if sourceClass == ah.FragmentClassProduct && (targetClass == ah.FragmentClassOrg || targetClass == ah.FragmentClassSite) {
			return "SUPPLIES_TO", "mapped USED_IN to SUPPLIES_TO for product -> actor graph-intent edge"
		}
		if sourceClass == ah.FragmentClassProduct && targetClass == ah.FragmentClassProduct {
			return "assembles_into", "mapped USED_IN to assembles_into for product -> product graph-intent edge"
		}
	}
	return relationType, ""
}

type graphIntentSnapshotIndex struct {
	byNodeID             map[string]horizonmodels.GraphIntentSnapshotNode
	byFragmentID         map[string]horizonmodels.GraphIntentSnapshotNode
	byCanonicalPath      map[string]horizonmodels.GraphIntentSnapshotNode
	canonicalByLookupKey map[string]string
}

func newGraphIntentSnapshotIndex(snapshot horizonmodels.GraphIntentSnapshot) graphIntentSnapshotIndex {
	out := graphIntentSnapshotIndex{
		byNodeID:             map[string]horizonmodels.GraphIntentSnapshotNode{},
		byFragmentID:         map[string]horizonmodels.GraphIntentSnapshotNode{},
		byCanonicalPath:      map[string]horizonmodels.GraphIntentSnapshotNode{},
		canonicalByLookupKey: map[string]string{},
	}
	for _, node := range snapshot.Nodes {
		if nodeID := strings.TrimSpace(node.NodeID); nodeID != "" {
			out.byNodeID[nodeID] = node
		}
		if fragmentID := strings.TrimSpace(node.FragmentID); fragmentID != "" {
			out.byFragmentID[fragmentID] = node
		}
		if canonicalPath := strings.TrimSpace(node.CanonicalPath); canonicalPath != "" {
			out.byCanonicalPath[canonicalPath] = node
		}
		key := graphIntentLookupKey(node.Type, node.Label)
		if key != "" && strings.TrimSpace(node.CanonicalPath) != "" {
			out.canonicalByLookupKey[key] = strings.TrimSpace(node.CanonicalPath)
		}
	}
	return out
}

func (h *HorizonHandlers) resolveGraphIntentNodeCanonicalPathHint(ctx context.Context, lens *horizonmodels.Lens, index graphIntentSnapshotIndex, rawQuery string, nodeType string, label string, hint string) (string, string) {
	nodeType = strings.ToUpper(strings.TrimSpace(nodeType))
	label = strings.TrimSpace(label)
	hint = strings.TrimSpace(hint)
	fragmentClass, fragmentSubtype, _ := horizonmodels.MapGraphIntentNodeTypeToClass(nodeType, hint)

	if strings.EqualFold(nodeType, string(ah.FragmentClassProduct)) || fragmentClass == ah.FragmentClassProduct {
		return h.resolveGraphIntentProductPathHint(ctx, lens, label, hint)
	}
	// Legacy DOMAIN class folded onto canonical_path overlay (see migration_map);
	// detect both the legacy wire token and the path-shape inference made by
	// MapGraphIntentNodeTypeToClass (org/<empty> with `/domain/` hint surfaces
	// here via fragmentSubtype unset; we therefore also fall back to the raw
	// nodeType keyword check for back-compat with pre-AH wire payloads).
	if strings.EqualFold(nodeType, "DOMAIN") || (fragmentClass == ah.FragmentClassOrg && fragmentSubtype == "domain") || strings.HasPrefix(strings.ToLower(strings.TrimSpace(hint)), "/domain/") {
		return h.resolveGraphIntentDomainPathHint(ctx, lens, rawQuery, label, hint)
	}
	if key := graphIntentLookupKey(nodeType, label); key != "" {
		if canonicalPath := strings.TrimSpace(index.canonicalByLookupKey[key]); canonicalPath != "" {
			return canonicalPath, ""
		}
	}
	if hint != "" && taxonomy.ValidateCanonicalPath(hint) == nil {
		return hint, ""
	}
	if h == nil || h.service == nil || h.service.Fragments() == nil || label == "" {
		return hint, ""
	}
	if exact, err := h.service.Fragments().FindByLabelForTenant(ctx, strings.TrimSpace(lens.TenantID), "en", label, fragmentClass); err == nil {
		if canonicalPath := graphIntentPickMatchingFragmentPath(exact, fragmentClass); canonicalPath != "" {
			return canonicalPath, ""
		}
	}
	if prefix, err := h.service.Fragments().SearchPrefixForTenant(ctx, strings.TrimSpace(lens.TenantID), label, 8); err == nil {
		if canonicalPath := graphIntentPickMatchingFragmentPath(prefix, fragmentClass); canonicalPath != "" {
			return canonicalPath, ""
		}
	}
	if semantic, err := h.service.SearchFragments(ctx, label, 8); err == nil {
		if canonicalPath := graphIntentPickMatchingFragmentPath(semantic, fragmentClass); canonicalPath != "" {
			return canonicalPath, ""
		}
	}
	return hint, ""
}

func (h *HorizonHandlers) resolveGraphIntentProductPathHint(ctx context.Context, lens *horizonmodels.Lens, label string, hint string) (string, string) {
	label = strings.TrimSpace(label)
	hint = strings.TrimSpace(hint)
	tenantID := ""
	if lens != nil {
		tenantID = strings.TrimSpace(lens.TenantID)
	}

	if h != nil && h.productDictionary != nil && hint != "" && strings.HasPrefix(hint, "/product/") {
		if canonicalPath, matched, err := h.productDictionary.CanonicalizeProductPathForTenant(ctx, tenantID, hint); err == nil && strings.TrimSpace(canonicalPath) != "" {
			if matched || taxonomy.ValidateCanonicalPath(canonicalPath) == nil {
				return strings.TrimSpace(canonicalPath), ""
			}
		}
	}

	queries := graphIntentProductQueries(label, hint)
	if h != nil && h.productDictionary != nil && h.service != nil && h.service.Fragments() != nil {
		for _, query := range queries {
			suggestions, err := h.findProductPrefetchSuggestions(ctx, tenantID, query, 6)
			if err != nil || len(suggestions) == 0 {
				continue
			}
			for _, suggestion := range suggestions {
				canonicalPath := strings.TrimSpace(suggestion.CanonicalPath)
				if canonicalPath == "" || taxonomy.ValidateCanonicalPath(canonicalPath) != nil {
					continue
				}
				return canonicalPath, ""
			}
		}
	}

	canonicalizer := horizonscout.NewProductCanonicalizer()
	for _, query := range queries {
		if query == "" {
			continue
		}
		result, err := canonicalizer.CanonicalizeProduct(ctx, query)
		if err != nil || result == nil {
			continue
		}
		canonicalPath := strings.TrimSpace(result.CanonicalPath)
		if canonicalPath == "" || taxonomy.ValidateCanonicalPath(canonicalPath) != nil {
			continue
		}
		if h != nil && h.productDictionary != nil {
			if rewritten, matched, err := h.productDictionary.CanonicalizeProductPathForTenant(ctx, tenantID, canonicalPath); err == nil && strings.TrimSpace(rewritten) != "" {
				if matched || taxonomy.ValidateCanonicalPath(rewritten) == nil {
					return strings.TrimSpace(rewritten), ""
				}
			}
		}
		return canonicalPath, fmt.Sprintf("product canonicalPathHint inferred from label %q via deterministic product canonicalizer", query)
	}

	return hint, ""
}

func (h *HorizonHandlers) resolveGraphIntentDomainPathHint(ctx context.Context, lens *horizonmodels.Lens, rawQuery string, label string, hint string) (string, string) {
	label = strings.TrimSpace(label)
	hint = strings.TrimSpace(hint)
	rawQuery = strings.TrimSpace(rawQuery)

	if hint != "" && strings.HasPrefix(strings.ToLower(hint), "/domain/") {
		normalized := strings.TrimSpace(horizonscout.NormalizeRuntimeDomainCanonicalPath(hint))
		if normalized != "" && taxonomy.ValidateCanonicalPath(normalized) == nil {
			if normalized != hint {
				return normalized, fmt.Sprintf("domain canonicalPathHint normalized via runtime domain canonicalizer: %s -> %s", hint, normalized)
			}
			return normalized, ""
		}
	}
	if hint != "" && taxonomy.ValidateCanonicalPath(hint) == nil {
		return hint, ""
	}

	if h == nil || h.service == nil || h.service.AIExecutor() == nil {
		return hint, ""
	}

	existingDomains := append([]string{}, h.collectNearbyDomainPaths(ctx, lens)...)
	existingDomains = append(existingDomains, h.collectExistingDomainPathsForProject(ctx, "", "")...)
	queries := graphIntentDomainQueries(rawQuery, label, hint)
	resolver := horizonagents.NewDomainResolverAgent(h.service.AIExecutor())
	for _, query := range queries {
		if query == "" {
			continue
		}
		result, err := resolver.ResolveWithContext(ctx, query, existingDomains)
		if err != nil || result == nil {
			continue
		}
		for _, domain := range result.Domains {
			canonicalPath := strings.TrimSpace(horizonscout.NormalizeRuntimeDomainCanonicalPath(domain.CanonicalPath))
			if canonicalPath == "" || taxonomy.ValidateCanonicalPath(canonicalPath) != nil {
				continue
			}
			return canonicalPath, fmt.Sprintf("domain canonicalPathHint inferred from query %q via shared domain resolver", query)
		}
	}

	return hint, ""
}

func graphIntentDomainQueries(rawQuery string, label string, hint string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 4)
	appendQuery := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	appendQuery(rawQuery)
	appendQuery(label)
	appendQuery(strings.ReplaceAll(domainLeafSlugFromCanonicalPath(hint), "-", " "))
	appendQuery(humanizeDomainPath(hint))
	return out
}

func domainLeafSlugFromCanonicalPath(path string) string {
	path = strings.TrimSpace(path)
	if !strings.HasPrefix(path, "/domain/") {
		return ""
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 4 {
		return ""
	}
	return strings.TrimSpace(parts[len(parts)-1])
}

func humanizeDomainPath(path string) string {
	path = strings.TrimSpace(path)
	if !strings.HasPrefix(path, "/domain/") {
		return ""
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 4 {
		return ""
	}
	return strings.ReplaceAll(strings.Join(parts[3:], " "), "-", " ")
}

func graphIntentProductQueries(label string, hint string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 6)
	appendQuery := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	appendQuery(label)
	appendQuery(strings.ReplaceAll(productLeafSlugFromCanonicalPath(hint), "-", " "))
	appendQuery(humanizeProductPath(hint))
	appendQuery(strings.TrimPrefix(strings.TrimSpace(hint), "/product/"))
	return out
}

func productLeafSlugFromCanonicalPath(path string) string {
	path = strings.TrimSpace(path)
	if !strings.HasPrefix(path, "/product/") {
		return ""
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[len(parts)-1])
}

func graphIntentPickMatchingFragmentPath(fragments []*horizonmodels.HorizonFragment, fragmentType ah.FragmentClass) string {
	for _, frag := range fragments {
		if frag == nil {
			continue
		}
		if fragmentType != "" && frag.Class != fragmentType {
			continue
		}
		canonicalPath := strings.TrimSpace(frag.CanonicalPath)
		if canonicalPath == "" || taxonomy.ValidateCanonicalPath(canonicalPath) != nil {
			continue
		}
		return canonicalPath
	}
	return ""
}

func graphIntentLookupKey(nodeType string, label string) string {
	nodeType = strings.ToUpper(strings.TrimSpace(nodeType))
	label = strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(label)), " "))
	if nodeType == "" || label == "" {
		return ""
	}
	return nodeType + "::" + label
}

func graphIntentEdgeEndpointPath(tempID string, nodeID string, productPathsByRef map[string]string) (string, bool) {
	if canonicalPath := strings.TrimSpace(productPathsByRef[strings.TrimSpace(tempID)]); canonicalPath != "" {
		return canonicalPath, true
	}
	if canonicalPath := strings.TrimSpace(productPathsByRef[strings.TrimSpace(nodeID)]); canonicalPath != "" {
		return canonicalPath, true
	}
	return "", false
}

type graphIntentApplyExecutor struct {
	handler *HorizonHandlers
	ctx     context.Context
	lens    *horizonmodels.Lens
	dryRun  bool

	fragmentsRepo      *repo.HorizonFragmentsRepository
	edgesRepo          *repo.DependencyEdgesRepository
	lensItemsRepo      *repo.LensItemsRepository
	lensesRepo         *repo.LensesRepository
	mergeFragmentsRepo *repo.FragmentsMergeRepository

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
	deletedNodeCount int
	deletedEdgeCount int

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
	e.mergeFragmentsRepo = e.handler.mergeFragmentsRepo
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

	if strings.TrimSpace(plan.Merge.PrimaryNodeID) != "" && len(plan.Merge.DuplicateNodeIDs) > 0 {
		e.applyMerge(plan.Merge)
	}

	for _, edgeID := range plan.Delete.EdgeIDs {
		e.applyDeleteEdge(edgeID)
	}
	for _, nodeID := range plan.Delete.NodeIDs {
		e.applyDeleteNode(nodeID)
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
		DeletedNodeCount: e.deletedNodeCount,
		DeletedEdgeCount: e.deletedEdgeCount,
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

	fragClass, fragSubtype, _ := horizonmodels.MapGraphIntentNodeTypeToClass(nodeType, canonicalPath)
	state := toMetricState(spec.State)
	now := time.Now().UTC()
	fragment := &horizonmodels.HorizonFragment{
		TenantID:      "global",
		Class:         fragClass,
		Subtype:       fragSubtype,
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
	e.ensureNodeLensItem(id, canonicalPath, label, string(fragClass))
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
	relDecode, ok := horizonmodels.NormalizeGraphIntentRelationType(spec.RelationType)
	if !ok || relDecode.Dropped || strings.TrimSpace(string(relDecode.Relation)) == "" {
		e.unresolved = append(e.unresolved, horizonmodels.GraphIntentUnresolvedItem{
			TempID:          strings.TrimSpace(spec.TempID),
			Reason:          fmt.Sprintf("unsupported relation type: %s", strings.TrimSpace(spec.RelationType)),
			SuggestedAction: "fix_plan",
		})
		return
	}
	depType := relDecode.Relation
	aliasQualifiers := relDecode.ExtraAttributes
	if relDecode.Note != "" {
		e.warnings = appendUniqueStrings(e.warnings, relDecode.Note)
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
		Relation:         depType,
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
		targetClass, targetSubtype, _ := horizonmodels.MapGraphIntentNodeTypeToClass(strings.TrimSpace(*patch.Type), fragment.CanonicalPath)
		if targetClass != "" && (fragment.Class != targetClass || fragment.Subtype != targetSubtype) {
			fragment.Class = targetClass
			fragment.Subtype = targetSubtype
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
		decode, ok := horizonmodels.NormalizeGraphIntentRelationType(*patch.RelationType)
		if !ok || decode.Dropped || strings.TrimSpace(string(decode.Relation)) == "" {
			e.unresolved = append(e.unresolved, horizonmodels.GraphIntentUnresolvedItem{
				TempID:          strings.TrimSpace(patch.PatchID),
				Reason:          fmt.Sprintf("unsupported edge patch relation type: %s", strings.TrimSpace(*patch.RelationType)),
				SuggestedAction: "fix_plan",
			})
			return
		}
		newType := decode.Relation
		if decode.Note != "" {
			e.warnings = appendUniqueStrings(e.warnings, decode.Note)
		}
		if edge.Qualifiers == nil {
			edge.Qualifiers = map[string]interface{}{}
		}
		for k, v := range decode.ExtraAttributes {
			edge.Qualifiers[k] = v
		}
		if edge.Relation != newType {
			edge.Relation = newType
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

func (e *graphIntentApplyExecutor) applyDeleteEdge(edgeID string) {
	edgeID = strings.TrimSpace(edgeID)
	if edgeID == "" {
		return
	}
	if e.dryRun {
		e.deletedEdgeCount++
		return
	}
	if err := e.edgesRepo.Delete(e.ctx, edgeID); err != nil {
		e.unresolved = append(e.unresolved, horizonmodels.GraphIntentUnresolvedItem{
			TempID:          edgeID,
			Reason:          fmt.Sprintf("failed to delete edge: %v", err),
			SuggestedAction: "retry",
		})
		return
	}
	e.deletedEdgeCount++
}

func (e *graphIntentApplyExecutor) applyDeleteNode(nodeID string) {
	ref := strings.TrimSpace(nodeID)
	if ref == "" {
		return
	}
	fragmentID, ok := e.resolveNodeRef(ref)
	if !ok || fragmentID == "" {
		e.unresolved = append(e.unresolved, horizonmodels.GraphIntentUnresolvedItem{
			TempID:          ref,
			Reason:          "delete node target unresolved",
			SuggestedAction: "fix_plan",
		})
		return
	}
	if e.dryRun {
		e.deletedNodeCount++
		return
	}
	if err := e.fragmentsRepo.Delete(e.ctx, fragmentID); err != nil {
		e.unresolved = append(e.unresolved, horizonmodels.GraphIntentUnresolvedItem{
			TempID:          ref,
			Reason:          fmt.Sprintf("failed to delete fragment: %v", err),
			SuggestedAction: "retry",
		})
		return
	}
	e.deletedNodeCount++
}

func (e *graphIntentApplyExecutor) applyMerge(m horizonmodels.GraphIntentMergeBlock) {
	primaryID, ok := e.resolveNodeRef(strings.TrimSpace(m.PrimaryNodeID))
	if !ok || primaryID == "" {
		e.unresolved = append(e.unresolved, horizonmodels.GraphIntentUnresolvedItem{
			TempID:          strings.TrimSpace(m.PrimaryNodeID),
			Reason:          "merge primary node unresolved",
			SuggestedAction: "fix_plan",
		})
		return
	}
	tenantID := strings.TrimSpace(e.lens.TenantID)
	if tenantID == "" {
		tenantID = "global"
	}
	mergeFragmentData := m.MergeFragmentData == nil || *m.MergeFragmentData

	primary, err := e.fragmentsRepo.Get(e.ctx, primaryID)
	if err != nil || primary == nil {
		e.unresolved = append(e.unresolved, horizonmodels.GraphIntentUnresolvedItem{
			TempID:          m.PrimaryNodeID,
			Reason:          "merge primary fragment not found",
			SuggestedAction: "retry",
		})
		return
	}

	duplicateIDs := make([]string, 0, len(m.DuplicateNodeIDs))
	for _, ref := range m.DuplicateNodeIDs {
		dupID, ok := e.resolveNodeRef(strings.TrimSpace(ref))
		if !ok || dupID == "" || dupID == primaryID {
			continue
		}
		duplicateIDs = append(duplicateIDs, dupID)
	}

	if e.dryRun {
		e.updatedNodeCount++
		e.deletedNodeCount += len(duplicateIDs)
		return
	}

	for _, dupID := range duplicateIDs {
		if e.mergeFragmentsRepo != nil {
			onEdge := func(edge *horizonmodels.DependencyEdge) {
				if edge != nil {
					e.syncEdgeLensItem(edge)
				}
			}
			res, err := e.mergeFragmentsRepo.MergeFragmentWithRequest(e.ctx, repo.MergeRequest{
				PrimaryFragmentID:   primaryID,
				DuplicateFragmentID: dupID,
				TenantID:            tenantID,
				Source:              repo.MergeAuditSourceGraphIntent,
				LensID:              strings.TrimSpace(e.lens.ID),
				OnEdgeReassigned:    onEdge,
			})
			if err != nil {
				e.unresolved = append(e.unresolved, horizonmodels.GraphIntentUnresolvedItem{
					TempID:          dupID,
					Reason:          fmt.Sprintf("merge failed: %v", err),
					SuggestedAction: "retry",
				})
				continue
			}
			e.updatedEdgeCount += res.EdgesReassigned
			e.deletedEdgeCount += res.EdgesDeleted
			e.deletedNodeCount++
			continue
		}
		// Fallback when merge repo not wired (legacy path)
		dup, err := e.fragmentsRepo.Get(e.ctx, dupID)
		if err != nil || dup == nil {
			continue
		}
		if mergeFragmentData {
			if merge.MergeFragmentDataFillFirst(primary, dup) {
				primary.UpdatedAt = time.Now().UTC()
				_ = e.fragmentsRepo.Update(e.ctx, primary)
			}
		}
		reassigned, dedupDeleted := e.reassignEdgesWithDedup(tenantID, dupID, primaryID)
		e.updatedEdgeCount += reassigned
		e.deletedEdgeCount += dedupDeleted
		if err := e.fragmentsRepo.Delete(e.ctx, dupID); err != nil {
			e.unresolved = append(e.unresolved, horizonmodels.GraphIntentUnresolvedItem{
				TempID:          dupID,
				Reason:          fmt.Sprintf("failed to delete duplicate after merge: %v", err),
				SuggestedAction: "retry",
			})
			continue
		}
		e.deletedNodeCount++
	}
	if e.mergeFragmentsRepo == nil {
		dedupDeleted := e.bestEffortDedupeEdgesForNode(tenantID, primaryID, 2000)
		e.deletedEdgeCount += dedupDeleted
	}
}

func (e *graphIntentApplyExecutor) reassignEdgesWithDedup(tenantID, fromID, toID string) (reassigned, deleted int) {
	const limit = 5000
	srcEdges, err := e.edgesRepo.ListBySource(e.ctx, tenantID, fromID, nil, limit)
	if err != nil {
		return 0, 0
	}
	tgtEdges, err := e.edgesRepo.ListByTarget(e.ctx, tenantID, fromID, nil, limit)
	if err != nil {
		return 0, 0
	}
	for _, edge := range srcEdges {
		if edge == nil || strings.TrimSpace(edge.ID) == "" || edge.Relation == "" {
			continue
		}
		existing, err := e.edgesRepo.GetBySourceTargetAndType(e.ctx, tenantID, toID, edge.TargetFragmentID, edge.Relation)
		if err == nil && existing != nil {
			if graphIntentMergeEdgeMetrics(existing, edge) {
				_ = e.edgesRepo.Update(e.ctx, existing)
			}
			if err := e.edgesRepo.Delete(e.ctx, edge.ID); err == nil {
				deleted++
			}
			continue
		}
		edge.SourceFragmentID = toID
		if edge.SourceFragmentID == edge.TargetFragmentID {
			if err := e.edgesRepo.Delete(e.ctx, edge.ID); err == nil {
				deleted++
			}
			continue
		}
		edge.Metadata.LastUpdatedAt = time.Now().UTC()
		if err := e.edgesRepo.Update(e.ctx, edge); err == nil {
			reassigned++
			e.syncEdgeLensItem(edge)
		}
	}
	for _, edge := range tgtEdges {
		if edge == nil || strings.TrimSpace(edge.ID) == "" || edge.Relation == "" {
			continue
		}
		existing, err := e.edgesRepo.GetBySourceTargetAndType(e.ctx, tenantID, edge.SourceFragmentID, toID, edge.Relation)
		if err == nil && existing != nil {
			if graphIntentMergeEdgeMetrics(existing, edge) {
				_ = e.edgesRepo.Update(e.ctx, existing)
			}
			if err := e.edgesRepo.Delete(e.ctx, edge.ID); err == nil {
				deleted++
			}
			continue
		}
		edge.TargetFragmentID = toID
		if edge.SourceFragmentID == edge.TargetFragmentID {
			if err := e.edgesRepo.Delete(e.ctx, edge.ID); err == nil {
				deleted++
			}
			continue
		}
		edge.Metadata.LastUpdatedAt = time.Now().UTC()
		if err := e.edgesRepo.Update(e.ctx, edge); err == nil {
			reassigned++
			e.syncEdgeLensItem(edge)
		}
	}
	return reassigned, deleted
}

func graphIntentMergeEdgeMetrics(dst, src *horizonmodels.DependencyEdge) bool {
	if dst == nil || src == nil {
		return false
	}
	changed := false
	if len(src.Metrics) > 0 {
		if dst.Metrics == nil {
			dst.Metrics = map[string]horizonmodels.MetricValue{}
		}
		for k, mv := range src.Metrics {
			key := strings.TrimSpace(k)
			if key == "" {
				continue
			}
			if cur, ok := dst.Metrics[key]; ok {
				if merge.BetterMetricValue(mv, cur) {
					dst.Metrics[key] = mv
					changed = true
				}
			} else {
				dst.Metrics[key] = mv
				changed = true
			}
		}
	}
	if len(src.Qualifiers) > 0 {
		if dst.Qualifiers == nil {
			dst.Qualifiers = map[string]interface{}{}
		}
		for k, v := range src.Qualifiers {
			key := strings.TrimSpace(k)
			if key == "" {
				continue
			}
			if _, ok := dst.Qualifiers[key]; ok {
				continue
			}
			dst.Qualifiers[key] = v
			changed = true
		}
	}
	return changed
}

func (e *graphIntentApplyExecutor) bestEffortDedupeEdgesForNode(tenantID, fragmentID string, limit int) (deleted int) {
	if e == nil || e.edgesRepo == nil {
		return 0
	}
	tenantID = strings.TrimSpace(tenantID)
	fragmentID = strings.TrimSpace(fragmentID)
	if tenantID == "" || fragmentID == "" || limit <= 0 {
		return 0
	}
	srcEdges, err := e.edgesRepo.ListBySource(e.ctx, tenantID, fragmentID, nil, limit)
	if err != nil {
		return 0
	}
	tgtEdges, err := e.edgesRepo.ListByTarget(e.ctx, tenantID, fragmentID, nil, limit)
	if err != nil {
		return 0
	}
	type key struct {
		s  string
		t  string
		ty string
	}
	kept := map[key]*horizonmodels.DependencyEdge{}
	consider := func(ed *horizonmodels.DependencyEdge) {
		if ed == nil {
			return
		}
		s := strings.TrimSpace(ed.SourceFragmentID)
		t := strings.TrimSpace(ed.TargetFragmentID)
		ty := ed.Relation
		ns, nt, nty, _ := horizonscout.NormalizeSymmetricEdge(s, t, ty)
		k := key{s: ns, t: nt, ty: string(nty)}
		if existing, ok := kept[k]; ok && existing != nil {
			keep := existing
			drop := ed
			if ed.Metadata.CreatedAt.Before(existing.Metadata.CreatedAt) ||
				(ed.Metadata.CreatedAt.Equal(existing.Metadata.CreatedAt) && strings.TrimSpace(ed.ID) < strings.TrimSpace(existing.ID)) {
				keep = ed
				drop = existing
				kept[k] = keep
			}
			if drop != nil && strings.TrimSpace(drop.ID) != "" {
				if err := e.edgesRepo.Delete(e.ctx, drop.ID); err == nil {
					deleted++
				}
			}
			return
		}
		kept[k] = ed
	}
	for _, ed := range srcEdges {
		consider(ed)
	}
	for _, ed := range tgtEdges {
		consider(ed)
	}
	return deleted
}

func (e *graphIntentApplyExecutor) applyRelationRewrite(rw horizonmodels.GraphIntentRelationRewrite) {
	matchDecode, matchTypeOK := horizonmodels.NormalizeGraphIntentRelationType(rw.Match.RelationType)
	if strings.TrimSpace(rw.Match.RelationType) == "" || matchDecode.Dropped {
		matchTypeOK = false
	}
	matchType := matchDecode.Relation
	replaceDecode, replaceOK := horizonmodels.NormalizeGraphIntentRelationType(rw.ReplaceWith.RelationType)
	if !replaceOK || replaceDecode.Dropped || strings.TrimSpace(string(replaceDecode.Relation)) == "" {
		e.unresolved = append(e.unresolved, horizonmodels.GraphIntentUnresolvedItem{
			TempID:          strings.TrimSpace(rw.RewriteID),
			Reason:          fmt.Sprintf("unsupported rewrite replacement relation type: %s", strings.TrimSpace(rw.ReplaceWith.RelationType)),
			SuggestedAction: "fix_plan",
		})
		return
	}
	replaceType := replaceDecode.Relation
	replaceAliasQualifiers := replaceDecode.ExtraAttributes
	if replaceDecode.Note != "" {
		e.warnings = appendUniqueStrings(e.warnings, replaceDecode.Note)
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
		if matchTypeOK && edge.Relation != matchType {
			continue
		}
		if len(sourceIDs) > 0 && !containsString(sourceIDs, edge.SourceFragmentID) {
			continue
		}
		if len(targetIDs) > 0 && !containsString(targetIDs, edge.TargetFragmentID) {
			continue
		}

		changed := false
		if edge.Relation != replaceType {
			edge.Relation = replaceType
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
		decode, ok := horizonmodels.NormalizeGraphIntentRelationType(*patch.RelationType)
		if ok && !decode.Dropped && strings.TrimSpace(string(decode.Relation)) != "" {
			edge, err := e.edgesRepo.GetBySourceTargetAndType(e.ctx, strings.TrimSpace(e.lens.TenantID), sourceID, targetID, decode.Relation)
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
	geoHint := graphIntentGeoHint(spec)
	slug := taxonomy.Slugify(label)
	if slug == "" {
		slug = "untitled"
	}
	switch typ {
	case "GOVERNMENT_AGENCY":
		return taxonomy.GovCanonicalPath(firstNonEmptyGraphIntent(geoHint, "na"), label)
	case string(ah.FragmentClassOrg):
		return taxonomy.OrgCanonicalPath(firstNonEmptyGraphIntent(geoHint, "na"), label)
	case string(ah.FragmentClassSite):
		return taxonomy.FacilityCanonicalPath(firstNonEmptyGraphIntent(geoHint, "na"), label)
	case string(ah.FragmentClassGeo):
		return taxonomy.GeoCanonicalPath("global", slug, "")
	case string(ah.FragmentClassProduct):
		return taxonomy.ProductCanonicalPath("", label)
	// Legacy UPPER_SNAKE wire tokens with no first-class AH FragmentClass
	// equivalent (their AH home is a (Class, Subtype) tuple). Match the raw
	// literal so wire payloads keep deterministic canonical_path scaffolding
	// during the migration window.
	case "PROGRAM":
		return taxonomy.ProgramCanonicalPath("", label)
	case string(ah.FragmentClassMarket):
		return taxonomy.MarketCanonicalPath("global", label)
	case string(ah.FragmentClassPerson):
		return taxonomy.PersonCanonicalPath(label)
	case "MEDIA_CHANNEL":
		return taxonomy.JoinPath("media", "channel", slug)
	case "DOMAIN":
		return taxonomy.JoinPath("domain", "v1", "other", slug)
	case "SPHERE":
		return taxonomy.JoinPath("sphere", slug)
	case string(ah.FragmentClassEvidence):
		return taxonomy.JoinPath("event", slug)
	case string(ah.FragmentClassDecision):
		return taxonomy.JoinPath("decision", "general", slug)
	case string(ah.FragmentClassMechanism):
		return taxonomy.JoinPath("mechanism", "general", slug)
	case "DEP" + "OSIT":
		return taxonomy.JoinPath("deposit", "resource", slug)
	case string(ah.FragmentClassMobileAsset):
		return taxonomy.JoinPath("track", "ground", "global", slug)
	case "SUPPLY" + "_LINK":
		return taxonomy.JoinPath("supply-link", "global", "na", slug)
	case string(ah.FragmentClassCommunity), "COMMUNITY_DYNASTY", "COMMUNITY_TRIBE":
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

func graphIntentGeoHint(spec horizonmodels.GraphIntentNodeSpec) string {
	lookup := func(m map[string]interface{}, keys ...string) string {
		for _, key := range keys {
			raw, ok := m[key]
			if !ok || raw == nil {
				continue
			}
			s, ok := raw.(string)
			if !ok {
				continue
			}
			v := strings.ToLower(strings.TrimSpace(s))
			if taxonomy.IsISO2CountryCode(v) || taxonomy.IsRegionalGeoToken(v) || taxonomy.IsCompositeGeoToken(v) {
				return v
			}
		}
		return ""
	}
	for _, m := range []map[string]interface{}{spec.Metadata, spec.State} {
		if geo := lookup(m, "countryIso2", "jurisdictionIso2", "country", "jurisdiction", "iso2", "geo"); geo != "" {
			return geo
		}
	}
	return ""
}

func firstNonEmptyGraphIntent(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
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
	if fragment.Class != "" {
		updates["class"] = string(fragment.Class)
		if fragment.Subtype != "" {
			updates["subtype"] = fragment.Subtype
		}
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
		e.ensureEdgeLensItem(edge.SourceFragmentID, edge.TargetFragmentID, string(edge.Relation), stringValueFromMap(edge.Qualifiers, "direction"), edge.Properties.Criticality, edge.Properties.Amplification, edge.Properties.LatencyHours)
		return
	}
	var selected *horizonmodels.LensItem
	for _, item := range items {
		if item == nil {
			continue
		}
		if strings.TrimSpace(stringPtrValue(item.EdgeType)) == string(edge.Relation) || strings.TrimSpace(stringPtrValue(item.Subkind)) == string(edge.Relation) {
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
		"subkind":       string(edge.Relation),
		"edgeType":      string(edge.Relation),
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

func tailForLog(s string, max int) string {
	if max <= 0 {
		return ""
	}
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[len(s)-max:]
}

func extractGeminiFinishReason(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var root map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		return ""
	}
	cands, ok := root["candidates"].([]interface{})
	if !ok || len(cands) == 0 {
		return ""
	}
	c0, ok := cands[0].(map[string]interface{})
	if !ok {
		return ""
	}
	if fr, ok := c0["finishReason"].(string); ok {
		return strings.TrimSpace(fr)
	}
	return ""
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

// logGraphIntentErr logs every error response so GCP logs suffice for diagnosis.
// Call before every 4xx/5xx return. lensId/corrID may be empty when not yet available.
func logGraphIntentErr(phase, stage string, status int, lensId, corrID string, err error) {
	msg := fmt.Sprintf("[GraphIntent] %s.%s status=%d", phase, stage, status)
	if lensId != "" {
		msg += fmt.Sprintf(" lensId=%s", lensId)
	}
	if corrID != "" {
		msg += fmt.Sprintf(" correlationId=%s", corrID)
	}
	if err != nil {
		msg += fmt.Sprintf(" err=%v", err)
	}
	log.Print(msg)
}

func graphIntentLensIdFromRequest(r *http.Request) string {
	path := strings.TrimPrefix(r.URL.Path, "/api/horizon/lenses/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) > 0 && strings.TrimSpace(parts[0]) != "" {
		return strings.TrimSpace(parts[0])
	}
	return ""
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
