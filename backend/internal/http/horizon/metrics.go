package horizon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	hzninterp "github.com/igorkirisyuk/sanskrite/internal/horizon/interpretation"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
)

// GetFragmentMetrics returns metrics for a fragment.
// GET /api/horizon/fragments/{id}/metrics
func (h *HorizonHandlers) GetFragmentMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if httputil.GetAuthInfo(r.Context()) == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	fragmentID := extractIDFromPath(r, "/api/horizon/fragments/")
	fragmentID = strings.TrimSpace(strings.TrimSuffix(fragmentID, "/metrics"))
	if fragmentID == "" {
		http.Error(w, "Fragment ID required", http.StatusBadRequest)
		return
	}

	fragment, err := h.service.Fragments().Get(r.Context(), fragmentID)
	if err != nil || fragment == nil {
		http.Error(w, "Fragment not found", http.StatusNotFound)
		return
	}

	flat := map[string]float64{}
	for k, mv := range fragment.State {
		flat[k] = mv.Value
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"fragmentId":    fragmentID,
		"canonicalPath": strings.TrimSpace(fragment.CanonicalPath),
		"metrics":       flat,
		"state":         fragment.State, // full MetricValue (includes provenance)
	})
}

// GetFragmentProvenance returns provenance for one metric or all metrics.
// GET /api/horizon/fragments/{id}/provenance?metricPath=/metric/risk/legal/access
func (h *HorizonHandlers) GetFragmentProvenance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if httputil.GetAuthInfo(r.Context()) == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	fragmentID := extractIDFromPath(r, "/api/horizon/fragments/")
	fragmentID = strings.TrimSpace(strings.TrimSuffix(fragmentID, "/provenance"))
	if fragmentID == "" {
		http.Error(w, "Fragment ID required", http.StatusBadRequest)
		return
	}

	fragment, err := h.service.Fragments().Get(r.Context(), fragmentID)
	if err != nil || fragment == nil {
		http.Error(w, "Fragment not found", http.StatusNotFound)
		return
	}

	metricPath := strings.TrimSpace(r.URL.Query().Get("metricPath"))
	if metricPath != "" {
		mv, ok := fragment.State[metricPath]
		if !ok {
			http.Error(w, "Metric not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"fragmentId": fragmentID,
			"metricPath": metricPath,
			"value":      mv.Value,
			"provenance": mv.Provenance,
		})
		return
	}

	all := map[string]any{}
	for k, mv := range fragment.State {
		all[k] = mv.Provenance
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"fragmentId": fragmentID,
		"provenance": all,
	})
}

// GetFragmentInterpretation generates (lazy) interpretation for a fragment.
// GET /api/horizon/fragments/{id}/interpretation?polarity=risk
func (h *HorizonHandlers) GetFragmentInterpretation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if httputil.GetAuthInfo(r.Context()) == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	fragmentID := extractIDFromPath(r, "/api/horizon/fragments/")
	fragmentID = strings.TrimSpace(strings.TrimSuffix(fragmentID, "/interpretation"))
	if fragmentID == "" {
		http.Error(w, "Fragment ID required", http.StatusBadRequest)
		return
	}

	polarity := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("polarity")))
	if polarity == "" {
		polarity = "risk"
	}
	if polarity != "risk" && polarity != "opportunity" {
		http.Error(w, "Invalid polarity (expected 'risk' or 'opportunity')", http.StatusBadRequest)
		return
	}

	frag, err := h.service.Fragments().Get(r.Context(), fragmentID)
	if err != nil || frag == nil {
		http.Error(w, "Fragment not found", http.StatusNotFound)
		return
	}

	aiExec := h.service.AIExecutor()
	if aiExec == nil {
		http.Error(w, "AI executor not available", http.StatusServiceUnavailable)
		return
	}

	req := &hzninterp.InterpretationRequest{
		FragmentID:    frag.ID,
		CanonicalPath: strings.TrimSpace(frag.CanonicalPath),
		NodeLabel:     strings.TrimSpace(frag.Labels["en"]),
		Polarity:      polarity,
		Priority:      hzninterp.TriggerPriorityHigh,
		TriggerType:   hzninterp.TriggerTypeUserQuery,
		TriggerValue:  0,
	}

	interpreter := hzninterp.NewInterpreter(aiExec, h.service.Fragments(), nil)
	if h.factsRepo != nil {
		interpreter = interpreter.WithEventFactsResolver(hzninterp.NewOntologyEventFactsResolver(h.factsRepo))
	}
	out, err := interpreter.Interpret(r.Context(), req)
	if err != nil {
		http.Error(w, fmt.Sprintf("interpretation failed: %v", err), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"fragmentId":     fragmentID,
		"canonicalPath":  strings.TrimSpace(frag.CanonicalPath),
		"polarity":       polarity,
		"interpretation": out,
	})
}

