package horizon

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonpolicy "github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
	horizonscout "github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/rtctx"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
	"github.com/google/uuid"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// HumanInTheLoopEdge handles admin-only curation actions for a dependency edge.
//
// Endpoint: POST /api/horizon/edges/{id}/hitl
// Body: { "type": "OWNS", "swap": true }
//
// Notes:
// - Deleting is done via the existing DELETE /api/horizon/edges/{id}.
// - This handler is intentionally small: it supports only "change type" and/or "swap direction".
func (h *HorizonHandlers) HumanInTheLoopEdge(w http.ResponseWriter, r *http.Request, edgeID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	edgeID = strings.TrimSpace(edgeID)
	if edgeID == "" {
		http.Error(w, "Edge ID required in path", http.StatusBadRequest)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// HumanInTheLoop Policy: admin-only
	if !authInfo.IsSystem {
		p := &horizonpolicy.HumanInTheLoopPolicy{UsersRepo: h.usersRepo}
		if !p.IsAdmin(r.Context(), authInfo.TenantID, authInfo.UID) {
			http.Error(w, "Admin access required", http.StatusForbidden)
			return
		}
	}

	var req struct {
		Type string `json:"type,omitempty"`
		Swap bool   `json:"swap,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	edge, err := h.service.Edges().Get(r.Context(), edgeID)
	if err != nil || edge == nil {
		http.Error(w, "Edge not found", http.StatusNotFound)
		return
	}
	// Tenant access:
	// - Normal case: tenant-scoped edges must match the caller tenant.
	// - Exception: allow tenant-admins to curate "global" edges (shared corpus).
	if edge.TenantID != authInfo.TenantID {
		if strings.EqualFold(strings.TrimSpace(edge.TenantID), "global") {
			// Admin check already performed above (for non-system calls).
			// This keeps non-admin users blocked while allowing admin curation of global data.
		} else {
			http.Error(w, "Access denied", http.StatusForbidden)
			return
		}
	}

	// Apply edits (type + optional swap). Wire input may be legacy
	// UPPER_SNAKE or AH lower_snake — normalise to AH.
	newType := edge.Relation
	if raw := strings.TrimSpace(req.Type); raw != "" {
		if rel, ok := models.NormalizeWireRelationStrict(raw); ok {
			newType = rel
		} else {
			newType = ah.RelationName(strings.ToLower(raw))
		}
	}
	newSource := strings.TrimSpace(edge.SourceFragmentID)
	newTarget := strings.TrimSpace(edge.TargetFragmentID)
	if req.Swap {
		newSource, newTarget = newTarget, newSource
	}

	// Normalize symmetric edges (direction doesn't matter).
	if horizonscout.SymmetricEdgeTypes[newType] {
		ns, nt, _, _ := horizonscout.NormalizeSymmetricEdge(newSource, newTarget, newType)
		newSource, newTarget = ns, nt
	}

	// Validate direction using fragment types (best-effort).
	// If fragments are missing, we allow (avoid blocking corrections on partial data),
	// but for known types with strict rules we try to validate.
	sourceFrag, _ := h.service.Fragments().Get(r.Context(), newSource)
	targetFrag, _ := h.service.Fragments().Get(r.Context(), newTarget)
	if sourceFrag != nil && targetFrag != nil {
		isValid, shouldReverse, rule := horizonscout.ValidateEdgeDirection(
			newType,
			sourceFrag.Class,
			targetFrag.Class,
		)
		if !isValid {
			// If the reverse direction would be valid, guide the client instead of silently changing.
			if shouldReverse && rule != nil {
				http.Error(w, "Invalid direction for edge type; try swapping From/To", http.StatusBadRequest)
				return
			}
			http.Error(w, "Invalid edge direction", http.StatusBadRequest)
			return
		}
	}

	edge.Relation = newType
	edge.SourceFragmentID = newSource
	edge.TargetFragmentID = newTarget

	if err := h.service.Edges().Update(r.Context(), edge); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Best-effort realtime edge upsert (needs projectId in headers for routing).
	projectID := strings.TrimSpace(r.Header.Get("X-Project-Id"))
	if projectID == "" {
		projectID = strings.TrimSpace(r.Header.Get("X-Project-ID"))
	}
	lensID := strings.TrimSpace(r.Header.Get("X-Lens-Id"))
	if lensID == "" {
		lensID = strings.TrimSpace(r.Header.Get("X-Lens-ID"))
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
		ProjectID:     projectID,
		LensID:        lensID,
		CorrelationID: correlationID,
		SessionID:     sessionID,
	})
	h.publishEdgeUpserted(ctx, authInfo.TenantID, projectID, lensID, correlationID, sessionID, edge)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(edge)
}


