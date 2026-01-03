package horizon

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/trace"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
)

type resolveIntentRequest struct {
	Query string `json:"query"`
}

type resolveIntentResponse struct {
	Intent *agents.IntentResolution `json:"intent"`
}

// ResolveIntent implements the "precheck" step: natural language query -> taxonomy-aligned intent.
// POST /api/horizon/intent/resolve
// Body: { "query": "..." }
func (h *HorizonHandlers) ResolveIntent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	var req resolveIntentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	req.Query = strings.TrimSpace(req.Query)
	if req.Query == "" {
		http.Error(w, "query is required", http.StatusBadRequest)
		return
	}

	agent := agents.NewIntentResolverAgent(h.service.AIExecutor())
	trace.Log(r.Context(), "intent", "intent.resolve.http.start", map[string]any{
		"tenantId": authInfo.TenantID,
		"queryLen": len(req.Query),
		"querySha1": trace.HashText(req.Query),
	})
	intent, err := agent.Resolve(r.Context(), req.Query)
	if err != nil {
		trace.Log(r.Context(), "intent", "intent.resolve.http.error", map[string]any{
			"tenantId": authInfo.TenantID,
			"error":    err.Error(),
		})
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Best-effort: if anchors already exist in our graph, fill anchor.name from the stored English label.
	// This improves linking and makes UI/lens naming more stable without requiring extra LLM grounding.
	if intent != nil && h.service != nil && h.service.Fragments() != nil && len(intent.Anchors) > 0 {
		for i := range intent.Anchors {
			cp := strings.TrimSpace(intent.Anchors[i].CanonicalPath)
			if cp == "" {
				continue
			}
			frag, ferr := h.service.Fragments().GetByCanonicalPath(r.Context(), cp)
			if ferr != nil || frag == nil {
				continue
			}
			n := strings.TrimSpace(frag.Labels["en"])
			if n == "" {
				n = strings.TrimSpace(frag.Identifiers["shortName"])
			}
			if n == "" {
				n = strings.TrimSpace(frag.Identifiers["legalName"])
			}
			if n != "" {
				intent.Anchors[i].Name = n
			}
		}
	}
	trace.Log(r.Context(), "intent", "intent.resolve.http.ok", map[string]any{
		"tenantId":    authInfo.TenantID,
		"focus":       trace.Trunc(intent.Focus, 140),
		"anchorsN":    len(intent.Anchors),
		"spherePathsN": len(intent.SpherePaths),
		"keywordsN":   len(intent.Keywords),
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resolveIntentResponse{Intent: intent})
}


