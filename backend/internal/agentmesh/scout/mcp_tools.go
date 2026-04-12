package scout

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/contracts"
)

// SearchKnowledge queries the internal knowledge base and returns a uniform ToolResponse.
func (p *Processor) SearchKnowledge(ctx context.Context, tenantID, query string, limit int) (ToolResponse, error) {
	if p == nil {
		return ToolResponse{Status: "error", Explanation: "scout processor is nil"}, fmt.Errorf("scout processor is nil")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return ToolResponse{Status: "error", Explanation: "query is required"}, fmt.Errorf("query is required")
	}
	if limit <= 0 {
		limit = 5
	}
	if limit > 8 {
		limit = 8
	}
	rawHits := maybeSearchKnowledgeBase(ctx, p.knowledgeBase, tenantID, query, limit)
	hits := make([]map[string]any, 0, min(len(rawHits), limit))
	suspected := false
	for _, raw := range rawHits {
		if len(hits) >= limit {
			break
		}
		title := strings.TrimSpace(stringValue(raw, "title"))
		url := strings.TrimSpace(firstNonEmpty(stringValue(raw, "uri"), stringValue(raw, "url")))
		snippet := truncateForTool(extractKnowledgeSnippet(raw), 320)
		hitSuspected := promptInjectionSuspected(strings.Join(compactStrings([]string{title, snippet}), "\n"))
		suspected = suspected || hitSuspected
		hits = append(hits, map[string]any{
			"id":                       strings.TrimSpace(stringValue(raw, "id")),
			"title":                    title,
			"url":                      url,
			"snippet":                  snippet,
			"score":                    floatValue(raw, "score", 0),
			"provenance":               "vertex_search",
			"promptInjectionSuspected": hitSuspected,
		})
	}
	data := map[string]any{
		"hitCount":                len(hits),
		"hits":                   hits,
		"promptInjectionSuspected": suspected,
	}
	if len(hits) == 0 {
		return ToolResponse{
			Status:      "not_found",
			Explanation: fmt.Sprintf("No documents found in internal KB for %q. The knowledge base has no data on this topic. Use web_search for external sources.", query),
			Data:        data,
		}, nil
	}
	topTitle := stringValue(hits[0], "title")
	topScore := floatValue(hits[0], "score", 0)
	return ToolResponse{
		Status:      "found",
		Explanation: fmt.Sprintf("Found %d documents. Top result: %q (score %.2f). Review the snippets for relevant evidence.", len(hits), topTitle, topScore),
		Data:        data,
	}, nil
}

// ResolveProductCanonical resolves a product label to its canonical path and returns a uniform ToolResponse.
func (p *Processor) ResolveProductCanonical(ctx context.Context, tenantID, productLabel string) (ToolResponse, error) {
	if p == nil {
		return ToolResponse{Status: "error", Explanation: "scout processor is nil"}, fmt.Errorf("scout processor is nil")
	}
	productLabel = strings.TrimSpace(productLabel)
	if productLabel == "" {
		return ToolResponse{Status: "error", Explanation: "productLabel is required"}, fmt.Errorf("productLabel is required")
	}
	data := map[string]any{
		"input":             productLabel,
		"matched":           false,
		"candidateRequired": true,
		"provenance":        "product_dictionary",
	}
	if p.productDictionary == nil {
		return ToolResponse{
			Status:      "not_found",
			Explanation: fmt.Sprintf("Product dictionary unavailable. %q needs candidate registration.", productLabel),
			Data:        data,
		}, nil
	}
	if strings.HasPrefix(productLabel, "/product/") {
		canonical, matched, err := p.productDictionary.CanonicalizeProductPathForTenant(ctx, tenantID, productLabel)
		if err != nil {
			return ToolResponse{Status: "error", Explanation: err.Error()}, err
		}
		known, err := p.productDictionary.IsKnownProductPathForTenant(ctx, tenantID, canonical)
		if err != nil {
			return ToolResponse{Status: "error", Explanation: err.Error()}, err
		}
		data["canonicalPath"] = canonical
		data["matched"] = matched || known
		data["known"] = known
		data["candidateRequired"] = !matched && !known
		if matched || known {
			return ToolResponse{
				Status:      "found",
				Explanation: fmt.Sprintf("Resolved %q to canonical path %q (known=%v). Use this path for graph operations.", productLabel, canonical, known),
				Data:        data,
			}, nil
		}
		return ToolResponse{
			Status:      "not_found",
			Explanation: fmt.Sprintf("Path %q is not recognized. Closest canonical: %q. Register as candidate if proceeding.", productLabel, canonical),
			Data:        data,
		}, nil
	}
	matches, err := p.productDictionary.SearchCanonicalProductPathsForTenant(ctx, tenantID, productLabel, 5)
	if err != nil {
		return ToolResponse{Status: "error", Explanation: err.Error()}, err
	}
	data["alternatives"] = compactStrings(matches)
	if len(matches) > 0 {
		data["canonicalPath"] = matches[0]
		data["matched"] = true
		data["known"] = true
		data["candidateRequired"] = false
		return ToolResponse{
			Status:      "found",
			Explanation: fmt.Sprintf("Matched %q to canonical path %q (%d alternatives). Use this path for graph operations.", productLabel, matches[0], len(matches)),
			Data:        data,
		}, nil
	}
	return ToolResponse{
		Status:      "not_found",
		Explanation: fmt.Sprintf("No canonical match for %q. This product is unknown and must be registered as a candidate.", productLabel),
		Data:        data,
	}, nil
}

// LookupProductTransform looks up a product-to-product relation and returns a uniform ToolResponse.
func (p *Processor) LookupProductTransform(ctx context.Context, tenantID, fromProduct, toProduct string) (ToolResponse, error) {
	if p == nil {
		return ToolResponse{Status: "error", Explanation: "scout processor is nil"}, fmt.Errorf("scout processor is nil")
	}
	fromProduct = strings.TrimSpace(fromProduct)
	toProduct = strings.TrimSpace(toProduct)
	if fromProduct == "" || toProduct == "" {
		return ToolResponse{Status: "error", Explanation: "fromProduct and toProduct are required"}, fmt.Errorf("fromProduct and toProduct are required")
	}
	data := map[string]any{
		"input": map[string]any{
			"fromProduct": fromProduct,
			"toProduct":   toProduct,
		},
		"candidateRequired": true,
		"provenance":        "product_transform_dictionary",
	}
	if p.productDictionary == nil {
		return ToolResponse{
			Status:      "not_found",
			Explanation: fmt.Sprintf("Transform dictionary unavailable. Relation %q → %q needs candidate registration.", fromProduct, toProduct),
			Data:        data,
		}, nil
	}
	resolution, err := p.productDictionary.ResolveProductRelationForTenant(
		ctx,
		tenantID,
		firstCanonicalPath(p.resolveProductRefs(ctx, tenantID, []string{fromProduct}), fromProduct),
		firstCanonicalPath(p.resolveProductRefs(ctx, tenantID, []string{toProduct}), toProduct),
	)
	if err != nil {
		return ToolResponse{Status: "error", Explanation: err.Error()}, err
	}
	data["fromPath"] = resolution.FromPath
	data["toPath"] = resolution.ToPath
	data["known"] = resolution.Known
	data["swapped"] = resolution.Swapped
	data["candidateRequired"] = !resolution.Known
	if resolution.Known {
		data["relationKind"] = resolution.RelationKind
		data["edgeType"] = string(resolution.EdgeType)
		return ToolResponse{
			Status:      "found",
			Explanation: fmt.Sprintf("Known relation: %s → %s is %q (edge: %s). Use this for graph edge creation.", resolution.FromPath, resolution.ToPath, resolution.RelationKind, resolution.EdgeType),
			Data:        data,
		}, nil
	}
	return ToolResponse{
		Status:      "not_found",
		Explanation: fmt.Sprintf("No known transform between %q and %q. Register as candidate relation if evidence supports it.", fromProduct, toProduct),
		Data:        data,
	}, nil
}

// PreviewPath runs deterministic path synthesis without graph writes.
func (p *Processor) PreviewPath(ctx context.Context, meta map[string]any, payload map[string]any) (map[string]any, error) {
	return p.runPathTool(ctx, meta, payload, false)
}

// MaterializePath runs deterministic path synthesis with graph writes enabled.
func (p *Processor) MaterializePath(ctx context.Context, meta map[string]any, payload map[string]any) (map[string]any, error) {
	return p.runPathTool(ctx, meta, payload, true)
}

func (p *Processor) runPathTool(ctx context.Context, meta map[string]any, payload map[string]any, allowWrites bool) (map[string]any, error) {
	if p == nil {
		return nil, fmt.Errorf("scout processor is nil")
	}
	if len(mapValue(payload["sessionState"])) == 0 {
		return nil, fmt.Errorf("sessionState is required")
	}
	req := normalizeRequest(decodeRequest(payload["request"]))
	if req.TenantID == "" {
		req.TenantID = firstNonEmpty(stringValue(meta, "tenantId"), "global")
	}
	req.AllowWrites = allowWrites
	plan := decodePlan(payload["plan"])
	input := map[string]any{
		"request":              req,
		"goal":                 payload["goal"],
		"environment":          payload["environment"],
		"role":                 payload["role"],
		"completeness":         payload["completeness"],
		"plan":                 plan,
		"sessionState":         payload["sessionState"],
		"research":             payload["research"],
		"researchDeltas":       payload["researchDeltas"],
		"critic":               payload["critic"],
		"activeFrontier":       payload["activeFrontier"],
		"pathTargetBranchIds":  payload["pathTargetBranchIds"],
		"pathContextBranchIds": payload["pathContextBranchIds"],
		"pathScopeProducts":    payload["pathScopeProducts"],
	}
	task := &contracts.MeshTask{
		ID:         firstNonEmpty(stringValue(meta, "taskRef"), "mcp-scout-path"),
		TenantID:   firstNonEmpty(req.TenantID, "global"),
		Capability: "scout.path",
		Input:      input,
		SessionRef: strings.TrimSpace(stringValue(meta, "sessionRef")),
	}
	result, err := p.processPath(ctx, nil, task)
	if err != nil {
		return nil, err
	}
	if result == nil || result.Output == nil {
		return nil, fmt.Errorf("path tool returned empty output")
	}
	path := decodePathResult(result.Output)
	return map[string]any{
		"summary":            summarizePathToolResult(path),
		"queriedProducts":    limitStrings(path.QueriedProducts, 8),
		"traceDigests":       limitTraceDigests(path.TraceDigests, 4),
		"artifactIds":        limitStrings(extractArtifactIDs(result.Output), 8),
		"openGaps":           limitStrings(path.OpenGaps, 8),
		"continuityStatus":   strings.TrimSpace(path.ContinuityStatus),
		"pathHypotheses":     limitPathHypotheses(path.PathHypotheses, 6),
		"sourceBranchIds":    limitStrings(path.SourceBranchIDs, 6),
		"stateDelta":         result.Output["stateDelta"],
		"allowWritesApplied": allowWrites,
	}, nil
}

func summarizePathToolResult(path PathResult) string {
	artifacts := len(compactStrings(append(append([]string{}, path.MaterializedArtifactIDs...), path.AssemblyArtifactID)))
	return fmt.Sprintf(
		"continuity=%s products=%d traces=%d artifacts=%d gaps=%d",
		firstNonEmpty(path.ContinuityStatus, "unknown"),
		len(compactStrings(path.QueriedProducts)),
		len(path.TraceDigests),
		artifacts,
		len(compactStrings(path.OpenGaps)),
	)
}

func limitPathHypotheses(items []PathHypothesis, n int) []map[string]any {
	if n <= 0 || len(items) == 0 {
		return nil
	}
	if len(items) > n {
		items = items[:n]
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"productRef":    strings.TrimSpace(item.ProductRef),
			"bindingStatus": strings.TrimSpace(item.BindingStatus),
			"gapSummary":    strings.TrimSpace(item.GapSummary),
		})
	}
	return out
}

func extractKnowledgeSnippet(hit map[string]any) string {
	for _, value := range []any{hit["derived"], hit["data"]} {
		if text := extractSnippetText(value); text != "" {
			return text
		}
	}
	return firstNonEmpty(stringValue(hit, "snippet"), stringValue(hit, "title"))
}

func extractSnippetText(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []string:
		return strings.Join(limitStrings(typed, 2), " ")
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := extractSnippetText(item); text != "" {
				items = append(items, text)
			}
			if len(items) >= 2 {
				break
			}
		}
		return strings.Join(items, " ")
	case map[string]any:
		for _, key := range []string{"snippet", "snippets", "summary", "extractiveAnswers", "extractive_answers", "text", "content"} {
			if text := extractSnippetText(typed[key]); text != "" {
				return text
			}
		}
		raw, _ := json.Marshal(typed)
		return string(raw)
	default:
		return ""
	}
}

func truncateForTool(text string, maxLen int) string {
	text = strings.TrimSpace(text)
	if maxLen <= 0 || len(text) <= maxLen {
		return text
	}
	return strings.TrimSpace(text[:maxLen]) + "..."
}

func promptInjectionSuspected(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return false
	}
	patterns := []string{
		"ignore previous instructions",
		"ignore your instructions",
		"disregard prior instructions",
		"system prompt",
		"developer message",
		"reveal the prompt",
		"do not follow the rules",
		"override the instruction",
	}
	for _, pattern := range patterns {
		if strings.Contains(text, pattern) {
			return true
		}
	}
	return false
}

func firstCanonicalPath(values []string, fallback string) string {
	values = compactStrings(values)
	if len(values) == 0 {
		return strings.TrimSpace(fallback)
	}
	return values[0]
}
