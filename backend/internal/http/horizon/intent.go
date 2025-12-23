package horizon

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
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
	intent, err := agent.Resolve(r.Context(), req.Query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resolveIntentResponse{Intent: intent})
}


