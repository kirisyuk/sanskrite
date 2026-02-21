package horizon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// QueryGraph performs a one-shot grounded question against the currently visible graph snapshot.
// POST /api/horizon/lenses/{lensId}/graph-query
//
// This is NOT a chat: it stores question+structured answer as an Interaction (project-scoped) with lensId inside payload.
func (h *HorizonHandlers) QueryGraph(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo, err := requireAuth(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	lensID := extractIDFromPath(r, "/api/horizon/lenses/")
	lensID = strings.TrimSpace(lensID)
	if lensID == "" {
		http.Error(w, "lensId is required in path", http.StatusBadRequest)
		return
	}

	if h.service == nil || h.service.Lens() == nil {
		http.Error(w, "Horizon service not available", http.StatusServiceUnavailable)
		return
	}
	lens, err := h.service.Lens().GetLens(r.Context(), lensID)
	if err != nil || lens == nil {
		http.Error(w, "Lens not found", http.StatusNotFound)
		return
	}
	if strings.TrimSpace(lens.TenantID) == "" || lens.TenantID != authInfo.TenantID {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}
	projectID := strings.TrimSpace(lens.ProjectID)
	if projectID == "" {
		http.Error(w, "Lens is missing projectId", http.StatusInternalServerError)
		return
	}

	if h.genAIHandler == nil {
		http.Error(w, "GenAI handler not configured", http.StatusServiceUnavailable)
		return
	}
	if h.interactions == nil {
		http.Error(w, "Interactions repository not configured", http.StatusServiceUnavailable)
		return
	}

	var req graphQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	req.Question = strings.TrimSpace(req.Question)
	if req.Question == "" {
		http.Error(w, "question is required", http.StatusBadRequest)
		return
	}
	// Best-effort: if caller provided projectId, ensure it matches lens.projectId.
	if strings.TrimSpace(req.ProjectID) != "" && strings.TrimSpace(req.ProjectID) != projectID {
		http.Error(w, "projectId does not match lens.projectId", http.StatusBadRequest)
		return
	}
	if len(req.Graph.Nodes) == 0 {
		http.Error(w, "graph.nodes is required", http.StatusBadRequest)
		return
	}

	// Create stable simple id mapping: original -> n1..nN
	idMap := make(map[string]string, len(req.Graph.Nodes))
	revIDMap := make(map[string]string, len(req.Graph.Nodes))
	mappedNodes := make([]mappedNode, 0, len(req.Graph.Nodes))

	seq := 0
	for _, n := range req.Graph.Nodes {
		orig := strings.TrimSpace(n.ID)
		if orig == "" {
			continue
		}
		if _, ok := idMap[orig]; ok {
			continue
		}
		seq++
		sid := fmt.Sprintf("n%d", seq)
		idMap[orig] = sid
		revIDMap[sid] = orig

		label := strings.TrimSpace(n.Label)
		if label == "" {
			label = strings.TrimSpace(n.CanonicalPath)
		}
		if label == "" {
			label = sid
		}
		mappedNodes = append(mappedNodes, mappedNode{
			ID:            sid,
			Label:         label,
			Type:          strings.TrimSpace(n.Type),
			CanonicalPath: strings.TrimSpace(n.CanonicalPath),
		})
	}
	if len(mappedNodes) == 0 {
		http.Error(w, "graph.nodes must contain at least one node with non-empty id", http.StatusBadRequest)
		return
	}

	mappedEdges := make([]mappedEdge, 0, len(req.Graph.Edges))
	for _, e := range req.Graph.Edges {
		fromOrig := strings.TrimSpace(e.From)
		toOrig := strings.TrimSpace(e.To)
		if fromOrig == "" || toOrig == "" {
			continue
		}
		from := idMap[fromOrig]
		to := idMap[toOrig]
		// Only include edges fully inside the provided nodes snapshot.
		if from == "" || to == "" {
			continue
		}
		mappedEdges = append(mappedEdges, mappedEdge{
			From:  from,
			To:    to,
			Type:  strings.TrimSpace(e.Type),
			Label: strings.TrimSpace(e.Label),
		})
	}

	now := time.Now().UTC()
	prompt := buildGraphQueryPrompt(req.Question, now, mappedNodes, mappedEdges)

	ctx, cancel := context.WithTimeout(r.Context(), 180*time.Second)
	defer cancel()

	// IMPORTANT: For Gemini, StrictJSONEnabled + WebSearchEnabled triggers a two-step flow in combined client
	// where the strict JSON content is generated WITHOUT grounding (grounded pass only fetches sources).
	// For this feature we need grounding to affect the answer, so we rely on server-side JSON verification instead.
	genReq := gen_combined.GenAIRequest{
		Model:             "gemini-3-flash-preview",
		Prompt:            prompt,
		SystemInstruction: graphQuerySystemInstruction(req.Question),
		WebSearchEnabled:  true,
		MaxOutputTokens:   4096,
		Temperature:       0.2,
		ProjectID:         projectID,
	}

	genResp, err := h.genAIHandler.GenerateDirect(ctx, genReq)
	if err != nil || genResp == nil {
		http.Error(w, "Failed to generate answer", http.StatusInternalServerError)
		return
	}

	// Verify and parse structured output.
	ver := ai.NewGenericVerifier(func(out graphQueryLLMResponse) error {
		if strings.TrimSpace(out.Answer) == "" {
			return fmt.Errorf("answer is required")
		}
		// Validate node ids are within the provided set.
		allowed := make(map[string]struct{}, len(revIDMap))
		for sid := range revIDMap {
			allowed[sid] = struct{}{}
		}
		if len(allowed) == 0 {
			if len(out.RelevantNodeIDs) > 0 {
				return fmt.Errorf("relevantNodeIds must be empty when graph snapshot has no nodes")
			}
			if len(out.Findings) > 0 {
				return fmt.Errorf("findings must be empty when graph snapshot has no nodes")
			}
			return nil
		}
		for _, nid := range out.RelevantNodeIDs {
			nid = strings.TrimSpace(nid)
			if nid == "" {
				return fmt.Errorf("relevantNodeIds contains empty id")
			}
			if _, ok := allowed[nid]; !ok {
				return fmt.Errorf("relevantNodeIds contains unknown id: %s", nid)
			}
		}
		// Ensure uniqueness.
		seen := map[string]struct{}{}
		for _, nid := range out.RelevantNodeIDs {
			if _, ok := seen[nid]; ok {
				return fmt.Errorf("relevantNodeIds contains duplicate id: %s", nid)
			}
			seen[nid] = struct{}{}
		}
		// Validate findings (best-effort strictness).
		relevantSet := make(map[string]struct{}, len(out.RelevantNodeIDs))
		for _, nid := range out.RelevantNodeIDs {
			relevantSet[strings.TrimSpace(nid)] = struct{}{}
		}
		for i, f := range out.Findings {
			if strings.TrimSpace(f.NodeID) == "" {
				return fmt.Errorf("findings[%d].nodeId is required", i)
			}
			if _, ok := allowed[strings.TrimSpace(f.NodeID)]; !ok {
				return fmt.Errorf("findings[%d].nodeId is unknown: %s", i, f.NodeID)
			}
			// Enforce that findings reference only relevant nodes (keeps UI smaller and semantics crisp).
			if _, ok := relevantSet[strings.TrimSpace(f.NodeID)]; !ok {
				return fmt.Errorf("findings[%d].nodeId must be in relevantNodeIds: %s", i, f.NodeID)
			}
			if strings.TrimSpace(f.Title) == "" {
				return fmt.Errorf("findings[%d].title is required", i)
			}
			if strings.TrimSpace(f.Source) == "" {
				return fmt.Errorf("findings[%d].source is required", i)
			}
			u := strings.TrimSpace(f.URL)
			if u == "" {
				return fmt.Errorf("findings[%d].url is required", i)
			}
			if !(strings.HasPrefix(strings.ToLower(u), "http://") || strings.HasPrefix(strings.ToLower(u), "https://")) {
				return fmt.Errorf("findings[%d].url must start with http(s)://", i)
			}
			d := strings.TrimSpace(f.EventDate)
			if d == "" {
				return fmt.Errorf("findings[%d].eventDate is required", i)
			}
			// Accept YYYY-MM-DD or RFC3339.
			if _, err := time.Parse("2006-01-02", d); err != nil {
				if _, err2 := time.Parse(time.RFC3339, d); err2 != nil {
					return fmt.Errorf("findings[%d].eventDate must be YYYY-MM-DD or RFC3339", i)
				}
			}
		}
		return nil
	})
	parsedAny, err := ver.VerifyAndParse(genResp.Content)
	if err != nil {
		// One retry with a stricter instruction (still grounded) to handle occasional formatting drift.
		genReq.SystemInstruction = strings.TrimSpace(genReq.SystemInstruction + "\n\nCRITICAL: Return ONLY a single JSON object. No markdown. No extra text.")
		genResp2, err2 := h.genAIHandler.GenerateDirect(ctx, genReq)
		if err2 != nil || genResp2 == nil {
			http.Error(w, "LLM response validation failed", http.StatusInternalServerError)
			return
		}
		genResp = genResp2
		parsedAny, err = ver.VerifyAndParse(genResp.Content)
		if err != nil {
			http.Error(w, "LLM response validation failed", http.StatusInternalServerError)
			return
		}
	}
	parsed := parsedAny.(graphQueryLLMResponse)

	// Map relevant node ids back to original ids.
	resolvedNodeIDs := make([]string, 0, len(parsed.RelevantNodeIDs))
	for _, sid := range parsed.RelevantNodeIDs {
		if orig, ok := revIDMap[strings.TrimSpace(sid)]; ok {
			resolvedNodeIDs = append(resolvedNodeIDs, orig)
		}
	}

	// Map findings node ids back to original ids.
	resolvedFindings := make([]graphQueryFinding, 0, len(parsed.Findings))
	for _, f := range parsed.Findings {
		sid := strings.TrimSpace(f.NodeID)
		orig, ok := revIDMap[sid]
		if !ok || strings.TrimSpace(orig) == "" {
			// Should not happen (validated), but be conservative.
			continue
		}
		resolvedFindings = append(resolvedFindings, graphQueryFinding{
			NodeID:    orig,
			Title:     strings.TrimSpace(f.Title),
			EventDate: strings.TrimSpace(f.EventDate),
			Source:    strings.TrimSpace(f.Source),
			URL:       strings.TrimSpace(f.URL),
		})
	}

	// Persist as Interaction (project-scoped) with lensId embedded in payload.
	atts := make([]graphQueryAttribution, 0, len(genResp.Attributions))
	for _, a := range genResp.Attributions {
		if strings.TrimSpace(a.Title) == "" && strings.TrimSpace(a.URL) == "" {
			continue
		}
		atts = append(atts, graphQueryAttribution{Title: strings.TrimSpace(a.Title), URL: strings.TrimSpace(a.URL)})
	}
	payload := graphQueryInteractionPayload{
		SchemaVersion:       "v1",
		LensID:              lensID,
		ProjectID:           projectID,
		AskedAt:             now.Format(time.RFC3339),
		Question:            req.Question,
		Answer:              strings.TrimSpace(parsed.Answer),
		RelevantNodeIDs:     resolvedNodeIDs,
		Findings:            resolvedFindings,
		LLM: graphQueryLLMMeta{
			Model:          strings.TrimSpace(genResp.Model),
			Grounding:      true,
			SearchQueries:  dedupeStrings(genResp.SearchQueries),
			Sources:        dedupeStrings(genResp.Sources),
			Attributions:   atts,
			PromptTokens:   genResp.Usage.PromptTokens,
			OutputTokens:   genResp.Usage.OutputTokens,
			TotalTokens:    genResp.Usage.TotalTokens,
			RawJSONPreview: safePreview(genResp.RawJSON, 5000),
		},
		LLMResponseRaw: safePreview(genResp.Content, 20000),
		LLMNodeIDMap:   revIDMap, // simple -> original
	}
	// Store minimal snapshot for audit/replay (optional but useful).
	payload.Graph.Nodes = req.Graph.Nodes
	payload.Graph.Edges = req.Graph.Edges
	contentBytes, _ := json.Marshal(payload)
	contentStr := string(contentBytes)

	subkind := "v1"
	it := &repo.Interaction{
		TenantID:  authInfo.TenantID,
		ProjectID: projectID,
		Kind:      "horizon.graph.query",
		Subkind:   &subkind,
		Content:   contentStr,
		Model:     strPtr(strings.TrimSpace(genResp.Model)),
		Usage: &repo.UsageMeta{
			PromptTokens:        genResp.Usage.PromptTokens,
			OutputTokens:        genResp.Usage.OutputTokens,
			TotalTokens:         genResp.Usage.TotalTokens,
			ReasoningTokens:     genResp.Usage.ReasoningTokens,
			ToolUsePromptTokens: genResp.Usage.ToolUsePromptTokens,
		},
		SearchQueries: dedupeStrings(genResp.SearchQueries),
		Sources:       dedupeStrings(genResp.Sources),
		GenerationParams: &struct {
			Temperature      *float64 `json:"temperature,omitempty" firestore:"temperature,omitempty"`
			MaxOutputTokens  *int     `json:"maxOutputTokens,omitempty" firestore:"maxOutputTokens,omitempty"`
			GroundingEnabled *bool    `json:"groundingEnabled,omitempty" firestore:"groundingEnabled,omitempty"`
		}{
			Temperature:      floatPtr(0.2),
			MaxOutputTokens:  intPtr(4096),
			GroundingEnabled: boolPtr(true),
		},
	}

	interactionID, err := h.interactions.Create(r.Context(), it)
	if err != nil {
		http.Error(w, "Failed to persist interaction", http.StatusInternalServerError)
		return
	}

	resp := graphQueryResponse{
		InteractionID:   interactionID,
		ProjectID:       projectID,
		LensID:          lensID,
		Question:        req.Question,
		Answer:          strings.TrimSpace(parsed.Answer),
		RelevantNodeIDs: resolvedNodeIDs,
		Findings:        resolvedFindings,
		Model:           strings.TrimSpace(genResp.Model),
		SearchQueries:   dedupeStrings(genResp.SearchQueries),
		Sources:         dedupeStrings(genResp.Sources),
		Timestamp:       now.Format(time.RFC3339),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

type graphQueryRequest struct {
	ProjectID string `json:"projectId,omitempty"`
	Question  string `json:"question"`
	Graph     struct {
		Nodes []graphSnapshotNode `json:"nodes"`
		Edges []graphSnapshotEdge `json:"edges,omitempty"`
	} `json:"graph"`
}

type graphSnapshotNode struct {
	ID            string `json:"id"`
	Label         string `json:"label,omitempty"`
	Type          string `json:"type,omitempty"`
	CanonicalPath string `json:"canonicalPath,omitempty"`
}

type graphSnapshotEdge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Type  string `json:"type,omitempty"`
	Label string `json:"label,omitempty"`
}

type mappedNode struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	Type          string `json:"type,omitempty"`
	CanonicalPath string `json:"canonicalPath,omitempty"`
}

type mappedEdge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Type  string `json:"type,omitempty"`
	Label string `json:"label,omitempty"`
}

type graphQueryLLMResponse struct {
	Answer          string              `json:"answer"`
	RelevantNodeIDs []string            `json:"relevantNodeIds"`
	Findings        []graphQueryFinding `json:"findings"`
}

type graphQueryResponse struct {
	InteractionID   string              `json:"interactionId"`
	ProjectID       string              `json:"projectId"`
	LensID          string              `json:"lensId"`
	Question        string              `json:"question"`
	Answer          string              `json:"answer"`
	RelevantNodeIDs []string            `json:"relevantNodeIds"`
	Findings        []graphQueryFinding `json:"findings,omitempty"`
	Model           string              `json:"model,omitempty"`
	SearchQueries   []string            `json:"searchQueries,omitempty"`
	Sources         []string            `json:"sources,omitempty"`
	Timestamp       string              `json:"timestamp"`
}

// graphQueryFinding represents a single fresh grounded finding linked to a graph node.
// eventDate should be ISO-8601 date (YYYY-MM-DD) or RFC3339 datetime.
type graphQueryFinding struct {
	NodeID    string `json:"nodeId"`
	Title     string `json:"title"`
	EventDate string `json:"eventDate"`
	Source    string `json:"source"`
	URL       string `json:"url"`
}

type graphQueryLLMMeta struct {
	Model          string                  `json:"model"`
	Grounding      bool                    `json:"grounding"`
	SearchQueries  []string                `json:"searchQueries,omitempty"`
	Sources        []string                `json:"sources,omitempty"`
	Attributions   []graphQueryAttribution `json:"attributions,omitempty"`
	PromptTokens   int                     `json:"promptTokens,omitempty"`
	OutputTokens   int                     `json:"outputTokens,omitempty"`
	TotalTokens    int                     `json:"totalTokens,omitempty"`
	RawJSONPreview string                  `json:"rawJsonPreview,omitempty"`
}

type graphQueryAttribution struct {
	Title string `json:"title,omitempty"`
	URL   string `json:"url,omitempty"`
}

type graphQueryInteractionPayload struct {
	SchemaVersion       string                         `json:"schemaVersion"`
	ProjectID           string                         `json:"projectId"`
	LensID              string                         `json:"lensId"`
	AskedAt             string                         `json:"askedAt"`
	Question            string                         `json:"question"`
	Answer              string                         `json:"answer"`
	RelevantNodeIDs     []string                       `json:"relevantNodeIds"`
	Findings []graphQueryFinding `json:"findings,omitempty"`
	Graph               struct {
		Nodes []graphSnapshotNode `json:"nodes"`
		Edges []graphSnapshotEdge `json:"edges,omitempty"`
	} `json:"graph"`
	LLM            graphQueryLLMMeta `json:"llm"`
	LLMResponseRaw string            `json:"llmResponseRaw,omitempty"`
	// Mapping from LLM-visible simple ids (n1..) back to original ids.
	LLMNodeIDMap map[string]string `json:"llmNodeIdMap"`
}

func graphQuerySystemInstruction(question string) string {
	langHint := "Respond in the same language as the user's question."
	if containsCyrillic(question) {
		langHint = "Отвечай на языке вопроса пользователя (русский)."
	}
	return strings.TrimSpace(`
You answer questions about a provided graph snapshot.
You MAY use Google Search grounding for fresh information when needed.

Rules:
- You MUST return ONLY valid JSON (no markdown, no prose).
- You MUST NOT invent node ids. You can ONLY reference node ids that exist in the provided graph snapshot.
- You MUST return a conservative set of relevantNodeIds: only nodes directly impacted by the question and your grounded findings.
- If nothing in the graph is relevant, return an empty relevantNodeIds array and answer explaining why.
- Your answer MUST be based on the most relevant and up-to-date information available as of the provided current date/time.
- Prefer recent events; when citing events, include their date and a credible source with URL.

` + langHint)
}

func buildGraphQueryPrompt(question string, now time.Time, nodes []mappedNode, edges []mappedEdge) string {
	nodesJSON, _ := json.Marshal(nodes)
	edgesJSON, _ := json.Marshal(edges)
	schemaJSON, _ := json.Marshal(graphQueryJSONSchema())
	graphIDsNote := "Node IDs are available. Use only those IDs in relevantNodeIds and findings[].nodeId."
	return strings.TrimSpace(fmt.Sprintf(`
Now (UTC): %s
Interpret "now" as the reference date/time. Your answer MUST be based on the most relevant information available as of this moment (prefer recent data).

Graph snapshot:
- nodes: %s
- edges: %s

Constraints:
- %s

User question:
%s

Required JSON schema:
%s
`, now.Format(time.RFC3339), string(nodesJSON), string(edgesJSON), graphIDsNote, question, string(schemaJSON)))
}

func graphQueryJSONSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"answer": map[string]interface{}{
				"type": "string",
			},
			"relevantNodeIds": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "string",
				},
			},
			"findings": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"nodeId":    map[string]interface{}{"type": "string"},
						"title":     map[string]interface{}{"type": "string"},
						"eventDate": map[string]interface{}{"type": "string"},
						"source":    map[string]interface{}{"type": "string"},
						"url":       map[string]interface{}{"type": "string"},
					},
					"required": []string{"nodeId", "title", "eventDate", "source", "url"},
				},
			},
		},
		"required": []string{"answer", "relevantNodeIds", "findings"},
	}
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
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

func containsCyrillic(s string) bool {
	for _, r := range s {
		if (r >= 'А' && r <= 'я') || r == 'ё' || r == 'Ё' {
			return true
		}
	}
	return false
}

func safePreview(s string, max int) string {
	ss := strings.TrimSpace(s)
	if max <= 0 || len(ss) <= max {
		return ss
	}
	return ss[:max] + "...(truncated)"
}

func strPtr(s string) *string {
	ss := strings.TrimSpace(s)
	if ss == "" {
		return nil
	}
	return &ss
}
func intPtr(v int) *int           { return &v }
func boolPtr(v bool) *bool        { return &v }
func floatPtr(v float64) *float64 { return &v }
