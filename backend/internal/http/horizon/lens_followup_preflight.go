package horizon

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/rtctx"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
)

type lensFollowUpPreflightRequest struct {
	Query string `json:"query"`
}

type lensFollowUpPreflightResponse struct {
	Intent *agents.IntentResolution `json:"intent"`
}

// LensFollowUpPreflight resolves a follow-up query for an existing lens without mutating it.
// Unlike global intent resolve, it injects the current lens context so the confirmation modal
// reflects what the runtime will actually use during follow-up enrichment.
//
// POST /api/horizon/lenses/{lensId}/follow-up-preflight
func (h *HorizonHandlers) LensFollowUpPreflight(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo, err := requireAuth(r)
	if err != nil || authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	lensID := extractIDFromPath(r, "/api/horizon/lenses/")
	if strings.TrimSpace(lensID) == "" {
		http.Error(w, "lensId required in path", http.StatusBadRequest)
		return
	}

	var body lensFollowUpPreflightRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	query := strings.TrimSpace(body.Query)
	if query == "" {
		http.Error(w, "query is required", http.StatusBadRequest)
		return
	}

	lens, lerr := h.service.Lens().GetLens(r.Context(), lensID)
	if lerr != nil || lens == nil {
		http.Error(w, "Lens not found", http.StatusNotFound)
		return
	}
	if lens.TenantID != authInfo.TenantID {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	correlationID := strings.TrimSpace(r.Header.Get("X-Correlation-Id"))
	if correlationID == "" {
		correlationID = strings.TrimSpace(r.Header.Get("X-Correlation-ID"))
	}
	if correlationID == "" {
		correlationID = uuid.NewString()
	}
	sessionID := strings.TrimSpace(r.Header.Get("X-Session-Id"))
	if sessionID == "" {
		sessionID = strings.TrimSpace(r.Header.Get("X-Session-ID"))
	}

	ctx := rtctx.With(r.Context(), rtctx.Context{
		TenantID:      authInfo.TenantID,
		ProjectID:     lens.ProjectID,
		LensID:        lens.ID,
		CorrelationID: correlationID,
		SessionID:     sessionID,
	})

	existingDomains := h.collectNearbyDomainPaths(ctx, lens)
	intentAgent := agents.NewIntentResolverAgent(h.service.AIExecutor())
	intent, err := intentAgent.ResolveWithContext(ctx, query, existingDomains)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	intent = sanitizeIntentForLensCreation(intent)

	if intent != nil && h.service != nil && h.service.Fragments() != nil && len(intent.Anchors) > 0 {
		for i := range intent.Anchors {
			cp := strings.TrimSpace(intent.Anchors[i].CanonicalPath)
			if cp == "" {
				continue
			}
			frag, ferr := h.service.Fragments().GetByCanonicalPath(ctx, cp)
			if ferr != nil || frag == nil {
				continue
			}
			name := ""
			if frag.Labels != nil {
				name = strings.TrimSpace(frag.Labels["en"])
			}
			if name == "" && frag.Identifiers != nil {
				name = strings.TrimSpace(frag.Identifiers["shortName"])
			}
			if name == "" && frag.Identifiers != nil {
				name = strings.TrimSpace(frag.Identifiers["legalName"])
			}
			if name != "" {
				intent.Anchors[i].Name = name
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Correlation-Id", correlationID)
	_ = json.NewEncoder(w).Encode(lensFollowUpPreflightResponse{Intent: intent})
}
