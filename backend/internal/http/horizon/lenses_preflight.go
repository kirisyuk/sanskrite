package horizon

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/rtctx"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/trace"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
)

type lensPreflightRequest struct {
	ProjectID string `json:"projectId"`
	Query     string `json:"query"`
	// Optional: enables specialized scout/research modules (e.g., ["management","products_and_services"])
	Specialties []string `json:"specialties,omitempty"`
}

type lensPreflightResponse struct {
	Intent *agents.IntentResolution `json:"intent"`
	Recommendation struct {
		Mode   string `json:"mode"`   // just_open|research_first
		Reason string `json:"reason"` // human-readable
		Stats  struct {
			AnchorsTotal  int `json:"anchorsTotal"`
			AnchorsKnown  int `json:"anchorsKnown"`
			AnchorsFresh  int `json:"anchorsFresh"`
			DomainsTotal  int `json:"domainsTotal"`
			DomainsKnown  int `json:"domainsKnown"`
			HasDomains    bool `json:"hasDomains"`
		} `json:"stats"`
	} `json:"recommendation"`
}

// LensPreflight resolves the query into intent and returns a recommendation for:
// - just_open: open a lens without triggering immediate scouting (best-effort)
// - research_first: start research/scouting (domains first, then anchors)
//
// POST /api/horizon/lenses/preflight
func (h *HorizonHandlers) LensPreflight(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	var req lensPreflightRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	req.ProjectID = strings.TrimSpace(req.ProjectID)
	req.Query = strings.TrimSpace(req.Query)
	if req.ProjectID == "" {
		http.Error(w, "projectId is required", http.StatusBadRequest)
		return
	}
	if req.Query == "" {
		http.Error(w, "query is required", http.StatusBadRequest)
		return
	}

	// Correlation/session IDs (optional, but recommended for realtime UX).
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
		ProjectID:     req.ProjectID,
		CorrelationID: correlationID,
		SessionID:     sessionID,
	})

	trace.Log(ctx, "lens_preflight", "lens.preflight.request", map[string]any{
		"tenantId":  authInfo.TenantID,
		"projectId": req.ProjectID,
		"queryLen":  len(req.Query),
		"querySha1": trace.HashText(req.Query),
	})

	intentAgent := agents.NewIntentResolverAgent(h.service.AIExecutor())
	intent, err := intentAgent.Resolve(ctx, req.Query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Best-effort: fill anchor.name from existing fragment English label.
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
			n := ""
			if frag.Labels != nil {
				n = strings.TrimSpace(frag.Labels["en"])
			}
			if n == "" && frag.Identifiers != nil {
				n = strings.TrimSpace(frag.Identifiers["shortName"])
			}
			if n == "" && frag.Identifiers != nil {
				n = strings.TrimSpace(frag.Identifiers["legalName"])
			}
			if n != "" {
				intent.Anchors[i].Name = n
			}
		}
	}

	// Recommendation heuristics (best-effort):
	// - If domains exist: recommend research_first.
	// - Else if any anchors are missing or not fresh: recommend research_first.
	// - Else: just_open.
	type anchorStatus struct {
		exists bool
		fresh  bool
	}
	anchorStatuses := map[string]anchorStatus{}
	anchorsKnown := 0
	anchorsFresh := 0
	if intent != nil && h.service != nil && h.service.Fragments() != nil {
		for _, a := range intent.Anchors {
			cp := strings.TrimSpace(a.CanonicalPath)
			if cp == "" {
				continue
			}
			st := anchorStatus{}
			if frag, ferr := h.service.Fragments().GetByCanonicalPath(ctx, cp); ferr == nil && frag != nil {
				st.exists = true
				st.fresh = isFragmentFreshForJustOpen(frag)
			}
			anchorStatuses[cp] = st
			if st.exists {
				anchorsKnown++
			}
			if st.fresh {
				anchorsFresh++
			}
		}
	}

	domainsKnown := 0
	hasDomains := false
	if intent != nil && h.service != nil {
		for _, d := range intent.Domains {
			cp := strings.TrimSpace(d.CanonicalPath)
			if cp == "" {
				continue
			}
			hasDomains = true
			if existing, _ := h.service.FindDomainByCanonicalPathBestEffort(ctx, cp); existing != nil {
				domainsKnown++
			}
		}
	}

	mode := "just_open"
	reason := "All anchors appear to be known and fresh; opening without immediate research."
	if hasDomains {
		mode = "research_first"
		reason = "Query includes domain contracts that must be expanded before scouting anchors."
	} else {
		// If any anchor missing or not fresh, recommend research_first.
		if len(intent.Anchors) == 0 {
			mode = "research_first"
			reason = "No anchors resolved; research is required."
		} else if anchorsKnown < len(intent.Anchors) {
			mode = "research_first"
			reason = "Some anchors are not present in the graph; research is required."
		} else if anchorsFresh < len(intent.Anchors) {
			mode = "research_first"
			reason = "Some anchors are stale or low-confidence; research is recommended."
		}
	}

	resp := lensPreflightResponse{Intent: intent}
	resp.Recommendation.Mode = mode
	resp.Recommendation.Reason = reason
	resp.Recommendation.Stats.AnchorsTotal = len(intent.Anchors)
	resp.Recommendation.Stats.AnchorsKnown = anchorsKnown
	resp.Recommendation.Stats.AnchorsFresh = anchorsFresh
	resp.Recommendation.Stats.DomainsTotal = len(intent.Domains)
	resp.Recommendation.Stats.DomainsKnown = domainsKnown
	resp.Recommendation.Stats.HasDomains = hasDomains

	// Best-effort: emit a WS event so the UI progress feed can show the preflight in human-readable form.
	if pub := h.service.RealtimePublisher(); pub != nil {
		_ = pub.PublishJSON(ctx, realtime.EventType("horizon.intent.preflight"), authInfo.TenantID, map[string]interface{}{
			"tenantId":      authInfo.TenantID,
			"projectId":     req.ProjectID,
			"correlationId": correlationID,
			"sessionId":     sessionID,
			"humanReadable": map[string]interface{}{
				"title": "How we understood your request",
				"description": func() string {
					parts := []string{}
					if len(intent.Domains) > 0 {
						parts = append(parts, "Domains: "+strconv.Itoa(len(intent.Domains)))
					}
					if len(intent.Anchors) > 0 {
						parts = append(parts, "Anchors: "+strconv.Itoa(len(intent.Anchors)))
					}
					parts = append(parts, "Recommended: "+mode)
					return strings.Join(parts, " • ")
				}(),
			},
			"intent":          intent,
			"recommendation":  resp.Recommendation,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Correlation-Id", correlationID)
	_ = json.NewEncoder(w).Encode(resp)
}

func isFragmentFreshForJustOpen(frag *models.HorizonFragment) bool {
	if frag == nil {
		return false
	}
	// Consider "fresh" when verified and not low confidence, and verified recently-ish.
	if frag.Metadata.EpistemicStatus == models.EpistemicStatusLowConfidence {
		return false
	}
	if frag.Metadata.LastVerifiedAt.IsZero() {
		return false
	}
	// Conservative TTL; can be tuned later.
	const ttl = 45 * 24 * time.Hour
	return time.Since(frag.Metadata.LastVerifiedAt.UTC()) <= ttl
}

