package horizon

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	signalsys "github.com/igorkirisyuk/sanskrite/internal/horizon/signal"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
)

// ExtractEventSignals runs 12-axis signal extraction + propagation for ontology event facts
// that have no signals yet. Manual invocation only — no auto-trigger on Create.
//
// POST /api/horizon/events:extract-signals
//
// Body:
//
//	{
//	  "eventIds": ["id1", "id2"] ,  // optional: specific event IDs
//	  "force": false                 // optional: re-run even if signals exist
//	}
//
// If eventIds is empty, returns 400. Each event is processed only if it has
// kind=event and (no 12-axis signals or force=true).
func (h *HorizonHandlers) ExtractEventSignals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	if h.factsRepo == nil {
		http.Error(w, "OntologyFactsRepository not configured", http.StatusServiceUnavailable)
		return
	}
	if h.signalPipeline == nil {
		http.Error(w, "SignalPipeline not configured", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		EventIDs []string `json:"eventIds"`
		Force   bool     `json:"force,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	eventIDs := req.EventIDs
	if len(eventIDs) == 0 {
		http.Error(w, "eventIds is required and must not be empty", http.StatusBadRequest)
		return
	}

	tenantID := strings.TrimSpace(authInfo.TenantID)
	if tenantID == "" {
		tenantID = "global"
	}

	type result struct {
		EventID   string `json:"eventId"`
		Status    string `json:"status"`
		Signals   int    `json:"signals,omitempty"`
		Error     string `json:"error,omitempty"`
		Skipped   string `json:"skipped,omitempty"`
	}

	results := make([]result, 0, len(eventIDs))

	for _, eventID := range eventIDs {
		eventID = strings.TrimSpace(eventID)
		if eventID == "" {
			continue
		}

		// Load event fact
		event, err := h.factsRepo.Get(r.Context(), eventID)
		if err != nil || event == nil {
			results = append(results, result{EventID: eventID, Status: "error", Error: "event not found"})
			continue
		}

		if event.Kind != "event" {
			results = append(results, result{EventID: eventID, Status: "skipped", Skipped: "not an event fact"})
			continue
		}
		if strings.TrimSpace(event.TenantID) != "" && strings.TrimSpace(event.TenantID) != tenantID {
			results = append(results, result{EventID: eventID, Status: "skipped", Skipped: "tenant mismatch"})
			continue
		}

		// Check 12-axis linkage: skip if already has signals (unless force)
		if !req.Force {
			signals, err := h.factsRepo.ListByEventIDAndKind(r.Context(), tenantID, eventID, "signal", 10)
			if err == nil && len(signals) > 0 {
				results = append(results, result{EventID: eventID, Status: "skipped", Skipped: "already has 12-axis signals", Signals: len(signals)})
				continue
			}
		}

		// Run pipeline
		if err := h.signalPipeline.ProcessEvent(r.Context(), event); err != nil {
			log.Printf("%s extract_signals failed eventId=%s err=%v", signalsys.LogPipeline, eventID, err)
			results = append(results, result{EventID: eventID, Status: "error", Error: err.Error()})
			continue
		}

		// Count new signals
		signals, _ := h.factsRepo.ListByEventIDAndKind(r.Context(), tenantID, eventID, "signal", 500)
		results = append(results, result{EventID: eventID, Status: "ok", Signals: len(signals)})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"results": results,
	})
}
