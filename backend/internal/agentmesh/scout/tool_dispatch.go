package scout

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/contracts"
	agentmeshruntime "github.com/igorkirisyuk/sanskrite/internal/agentmesh/runtime"
	"github.com/igorkirisyuk/sanskrite/internal/ai"
)

const maxToolRounds = 10

func scoutToolSchemas() []ai.ToolSchema {
	return []ai.ToolSchema{
		{
			Name: "search_knowledge",
			Description: "Search the internal knowledge base for documents matching a query. " +
				"Returns structured hits with titles, snippets, and relevance scores. " +
				"Use short, specific queries like 'SQM lithium carbonate Chile'. " +
				"If status is not_found, the KB has no data on this topic — do not retry the same query, use web_search instead.",
			ParametersJSONSchema: map[string]any{
				"type":     "object",
				"required": []string{"query"},
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "Short, specific search query in natural language"},
					"limit": map[string]any{"type": "integer", "description": "Max results (1-8, default 5)"},
				},
			},
		},
		{
			Name: "web_search",
			Description: "Search the web using Google Search for external, current information. " +
				"Use when the internal KB has no data or you need public information to fill a graph gap. " +
				"Use targeted queries — 'SQM lithium export port Antofagasta' not 'trace the supply chain for lithium carbonate'. " +
				"Returns a grounded answer with source URLs.",
			ParametersJSONSchema: map[string]any{
				"type":     "object",
				"required": []string{"query"},
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "Targeted web search query"},
				},
			},
		},
		{
			Name: "resolve_product",
			Description: "Resolve a product label to its canonical path in the knowledge graph. " +
				"Always call this before using a product name in proposed graph operations. " +
				"If status is not_found, the product needs candidate registration — include it in the unresolved list.",
			ParametersJSONSchema: map[string]any{
				"type":     "object",
				"required": []string{"productLabel"},
				"properties": map[string]any{
					"productLabel": map[string]any{"type": "string", "description": "Product name or path to resolve"},
				},
			},
		},
		{
			Name: "lookup_transform",
			Description: "Check if a known transform, assembly, or dependency relation exists between two products. " +
				"Always call this before proposing an edge between products. " +
				"If status is not_found, the relation needs candidate registration or further web_search to identify the industrial process.",
			ParametersJSONSchema: map[string]any{
				"type":     "object",
				"required": []string{"fromProduct", "toProduct"},
				"properties": map[string]any{
					"fromProduct": map[string]any{"type": "string", "description": "Source product name or canonical path"},
					"toProduct":   map[string]any{"type": "string", "description": "Target product name or canonical path"},
				},
			},
		},
	}
}

func (p *Processor) dispatchToolCall(ctx context.Context, rt *agentmeshruntime.Service, tenantID string, call ai.ToolCallRequest) (ToolResponse, error) {
	var args map[string]any
	if err := json.Unmarshal([]byte(call.ArgumentsJSON), &args); err != nil {
		return ToolResponse{Status: "error", Explanation: "Invalid tool arguments: " + err.Error()}, nil
	}

	switch call.ToolName {
	case "search_knowledge":
		query := strings.TrimSpace(stringValue(args, "query"))
		limit := int(floatValue(args, "limit", 5))
		return p.searchKnowledgeTool(ctx, tenantID, query, limit)
	case "web_search":
		query := strings.TrimSpace(stringValue(args, "query"))
		return p.webSearchTool(ctx, rt, tenantID, query)
	case "resolve_product":
		label := strings.TrimSpace(stringValue(args, "productLabel"))
		return p.resolveProductTool(ctx, tenantID, label)
	case "lookup_transform":
		from := strings.TrimSpace(stringValue(args, "fromProduct"))
		to := strings.TrimSpace(stringValue(args, "toProduct"))
		return p.lookupTransformTool(ctx, tenantID, from, to)
	default:
		return ToolResponse{Status: "error", Explanation: fmt.Sprintf("Unknown tool: %s", call.ToolName)}, nil
	}
}

func (p *Processor) searchKnowledgeTool(ctx context.Context, tenantID, query string, limit int) (ToolResponse, error) {
	return p.SearchKnowledge(ctx, tenantID, query, limit)
}

func (p *Processor) webSearchTool(ctx context.Context, rt *agentmeshruntime.Service, tenantID, query string) (ToolResponse, error) {
	if query == "" {
		return ToolResponse{Status: "error", Explanation: "query is required"}, nil
	}
	if rt == nil || rt.Vertex() == nil {
		return ToolResponse{Status: "error", Explanation: "web search is not available (vertex service not configured)"}, nil
	}

	aiTask := &ai.AITask{
		TaskType:     ai.TaskTypeContentGeneration,
		InputContent: fmt.Sprintf("Answer this question using web search results. Be factual and cite specific entities, locations, companies, and numbers.\n\nQuestion: %s", query),
		AIRequirements: ai.AIRequirements{
			PreferredModel:            rt.Vertex().ModelNameForCapability("scout.research", false),
			TextGenerationCapability:  true,
			SearchGroundingCapability: true,
			MaxOutputSize:             4096,
			MinCapabilityScore:        7,
		},
		TenantID:  tenantID,
		SessionID: "",
		TraceID:   "web-search-tool",
		UseCase:   "agentmesh",
	}
	model := aiTask.AIRequirements.PreferredModel
	result, err := rt.Vertex().AIService().(*ai.VertexAIService).ExecuteTask(ctx, aiTask, model)
	if err != nil {
		return ToolResponse{
			Status:      "error",
			Explanation: fmt.Sprintf("Web search failed: %s. Try a different query or proceed with available data.", err.Error()),
			Data:        map[string]any{},
		}, nil
	}

	answer := strings.TrimSpace(result.Content)
	sources := make([]map[string]any, 0)

	if ge := result.GroundingEvidence; ge != nil {
		for _, ws := range ge.WebSources {
			sources = append(sources, map[string]any{
				"title": strings.TrimSpace(ws.Title),
				"url":   strings.TrimSpace(ws.URL),
			})
		}
	}

	if answer == "" {
		return ToolResponse{
			Status:      "not_found",
			Explanation: fmt.Sprintf("Web search for %q returned no useful results. Try rephrasing or a more specific query.", query),
			Data:        map[string]any{"answer": "", "sources": []any{}},
		}, nil
	}

	explanation := fmt.Sprintf("Web search returned results for %q.", query)
	if len(sources) > 0 {
		explanation += fmt.Sprintf(" %d source(s) found.", len(sources))
	}
	return ToolResponse{
		Status:      "found",
		Explanation: explanation,
		Data: map[string]any{
			"answer":  truncateForTool(answer, 2000),
			"sources": sources,
		},
	}, nil
}

func (p *Processor) resolveProductTool(ctx context.Context, tenantID, productLabel string) (ToolResponse, error) {
	return p.ResolveProductCanonical(ctx, tenantID, productLabel)
}

func (p *Processor) lookupTransformTool(ctx context.Context, tenantID, fromProduct, toProduct string) (ToolResponse, error) {
	return p.LookupProductTransform(ctx, tenantID, fromProduct, toProduct)
}

func (p *Processor) runModelWithTools(
	ctx context.Context,
	rt *agentmeshruntime.Service,
	task *contracts.MeshTask,
	input map[string]any,
	tenantID string,
) (map[string]any, []ToolCallLog) {
	if p != nil && p.runModelObjectFn != nil {
		return p.runModelObjectFn(ctx, rt, task, input), nil
	}
	if rt == nil || rt.Vertex() == nil || task == nil {
		return nil, nil
	}

	tools := scoutToolSchemas()
	pseudoTask := &contracts.MeshTask{
		ID:         task.ID + ":" + strings.TrimSpace(task.Capability) + ":tools",
		TenantID:   task.TenantID,
		Capability: task.Capability,
		Input:      input,
		SessionRef: strings.TrimSpace(task.SessionRef),
	}

	branchID, branchQ := extractBranchLogFields(input["branch"])
	scoutReasoningLogf("research_tool_session_start task=%s capability=%s branch=%s question=%s",
		task.ID, task.Capability, branchID, scoutReasoningSingleLine(branchQ, 400))

	step, err := rt.Vertex().BeginChatWithTools(ctx, pseudoTask, tools)
	if err != nil {
		if p.logger != nil {
			p.logger.Printf("[agentmesh.scout] BeginChatWithTools failed capability=%s err=%v, falling back to runModelObject", task.Capability, err)
		}
		return p.runModelObject(ctx, rt, task, input), nil
	}

	var logs []ToolCallLog
	for round := 0; round < maxToolRounds; round++ {
		if step.IsFinal || len(step.ToolCalls) == 0 {
			if round == 0 && step != nil && strings.TrimSpace(step.Content) != "" {
				scoutReasoningLogf("research_model_message task=%s round=%d final=%v text=%s",
					task.ID, round, step.IsFinal, scoutReasoningSingleLine(step.Content, 700))
			}
			break
		}
		if txt := strings.TrimSpace(step.Content); txt != "" {
			scoutReasoningLogf("research_model_message task=%s round=%d text=%s",
				task.ID, round, scoutReasoningSingleLine(txt, 700))
		}
		scoutReasoningLogf("research_tool_round task=%s round=%d tool_calls=%d", task.ID, round, len(step.ToolCalls))

		results := make([]ai.ToolExecutionResult, 0, len(step.ToolCalls))
		for _, call := range step.ToolCalls {
			scoutReasoningLogf("research_tool_call task=%s round=%d name=%s args=%s",
				task.ID, round, call.ToolName, scoutReasoningSingleLine(call.ArgumentsJSON, 500))
			start := time.Now()
			resp, dispatchErr := p.dispatchToolCall(ctx, rt, tenantID, call)
			elapsed := time.Since(start)

			if dispatchErr != nil {
				resp = ToolResponse{Status: "error", Explanation: dispatchErr.Error()}
			}

			hitCount := 0
			if d, ok := resp.Data.(map[string]any); ok {
				hitCount = int(floatValue(d, "hitCount", 0))
			}
			logs = append(logs, ToolCallLog{
				ToolName:   call.ToolName,
				Args:       truncateForTool(call.ArgumentsJSON, 200),
				Status:     resp.Status,
				DurationMs: elapsed.Milliseconds(),
				HitCount:   hitCount,
			})
			scoutReasoningLogf("research_tool_result task=%s round=%d name=%s status=%s ms=%d hits=%d explain=%s",
				task.ID, round, call.ToolName, resp.Status, elapsed.Milliseconds(), hitCount,
				scoutReasoningSingleLine(resp.Explanation, 360))

			respJSON, _ := json.Marshal(resp)
			results = append(results, ai.ToolExecutionResult{
				CallID:     call.CallID,
				ToolName:   call.ToolName,
				ResultJSON: string(respJSON),
			})
		}

		step, err = rt.Vertex().ContinueWithToolResults(ctx, step.SessionID, pseudoTask, results)
		if err != nil {
			if p.logger != nil {
				p.logger.Printf("[agentmesh.scout] ContinueWithToolResults failed round=%d err=%v", round, err)
			}
			break
		}
		if step != nil && strings.TrimSpace(step.Content) != "" {
			scoutReasoningLogf("research_model_after_tools task=%s round=%d text=%s",
				task.ID, round, scoutReasoningSingleLine(step.Content, 700))
		}
	}

	if step == nil {
		return nil, logs
	}
	if scoutReasoningLogEnabled() {
		scoutReasoningLogf("research_tool_session_done task=%s final_json_preview=%s",
			task.ID, scoutReasoningSingleLine(step.Content, 900))
	}
	parsed := parseContentObject(map[string]any{"content": step.Content})
	if len(parsed) == 0 {
		parsed = tryExtractJSONFromText(step.Content)
	}
	return parsed, logs
}

func extractBranchLogFields(raw any) (id, question string) {
	if raw == nil {
		return "", ""
	}
	if m, ok := raw.(map[string]any); ok {
		return strings.TrimSpace(stringValue(m, "id")), strings.TrimSpace(stringValue(m, "question"))
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return "", ""
	}
	var br InvestigationBranch
	if err := json.Unmarshal(b, &br); err != nil {
		return "", ""
	}
	return strings.TrimSpace(br.ID), strings.TrimSpace(br.Question)
}

func tryExtractJSONFromText(text string) map[string]any {
	text = strings.TrimSpace(text)
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text[start:end+1]), &out); err != nil {
		return nil
	}
	return out
}

func toolCallStats(logs []ToolCallLog) (totalCalls int, totalHits int) {
	for _, l := range logs {
		totalCalls++
		totalHits += l.HitCount
	}
	return
}
