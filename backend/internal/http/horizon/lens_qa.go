package horizon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	horizoninterp "github.com/igorkirisyuk/sanskrite/internal/horizon/interpretation"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/qa"
)

// qaRequest is the body for POST /api/horizon/lenses/{lensId}/qa
type qaRequest struct {
	Question string `json:"question"`
}

// writeSSE writes an SSE event.
func writeSSE(w http.ResponseWriter, event string, data interface{}) {
	payload, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// LensQA handles POST /api/horizon/lenses/{lensId}/qa — SSE stream.
func (h *HorizonHandlers) LensQA(w http.ResponseWriter, r *http.Request) {
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

	var body qaRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	question := strings.TrimSpace(body.Question)
	if question == "" {
		http.Error(w, "question is required", http.StatusBadRequest)
		return
	}

	// Dependencies
	if h.qaEngine == nil || h.qaContextBuilder == nil || h.qaSynthesizer == nil || h.qaSessionsRepo == nil {
		http.Error(w, "QA service not configured", http.StatusServiceUnavailable)
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

	// Load scope fragments
	fragments, err := h.service.Fragments().ListByCanonicalPaths(r.Context(), lens.Scope)
	if err != nil {
		http.Error(w, "Failed to load lens scope", http.StatusInternalServerError)
		return
	}

	// Build scopeFragments map (canonicalPath -> fragmentID)
	scopeFragments := lens.ScopeFragments
	if scopeFragments == nil {
		scopeFragments = make(map[string]string)
	}
	for _, f := range fragments {
		if f != nil && f.CanonicalPath != "" && f.ID != "" {
			scopeFragments[f.CanonicalPath] = f.ID
		}
	}
	lens.ScopeFragments = scopeFragments

	// Cache check: same question <1h ago, answered → return cached (plan §4.3)
	recent, _ := h.qaSessionsRepo.ListByLens(r.Context(), lensID, 30)
	now := time.Now().UTC()
	for _, s := range recent {
		if s.Status != horizonmodels.QASessionStatusAnswered || s.Answer == nil {
			continue
		}
		if s.AnsweredAt == nil || now.Sub(*s.AnsweredAt) > time.Hour {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(s.RawQuestion), question) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			w.Header().Set("X-Accel-Buffering", "no")
			w.WriteHeader(http.StatusOK)
			writeSSE(w, "status", map[string]interface{}{"status": "answered", "sessionId": s.ID, "cached": true})
			if len(s.Answer.HighlightedNodeIDs) > 0 || len(s.Answer.TracePaths) > 0 {
				writeSSE(w, "highlights", map[string]interface{}{
					"highlightedNodeIds": s.Answer.HighlightedNodeIDs,
					"tracePaths":         s.Answer.TracePaths,
				})
			}
			if s.Answer.Text != "" {
				writeSSE(w, "answer_chunk", map[string]interface{}{"text": s.Answer.Text})
			}
			writeSSE(w, "answer_complete", s.Answer)
			return
		}
	}

	// Artifact summaries (best-effort)
	var artifactSummaries []string
	if h.materializedArtifactsRepo != nil {
		arts, _ := h.materializedArtifactsRepo.ListByLens(r.Context(), lensID, 20)
		for _, a := range arts {
			if a != nil && a.DisplayName != "" {
				artifactSummaries = append(artifactSummaries, a.DisplayName+": "+a.CanonicalKey)
			}
		}
	}

	// Create QASession
	sessionID := uuid.New().String()
	session := &horizonmodels.QASession{
		ID:             sessionID,
		LensID:         lensID,
		TenantID:       lens.TenantID,
		ProjectID:      lens.ProjectID,
		RawQuestion:    question,
		Status:         horizonmodels.QASessionStatusDecomposing,
	}
	if _, err := h.qaSessionsRepo.Create(r.Context(), session); err != nil {
		http.Error(w, "Failed to create QA session", http.StatusInternalServerError)
		return
	}

	// SSE response
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	writeSSE(w, "status", map[string]interface{}{
		"status":    "decomposing",
		"sessionId": sessionID,
	})

	ctx := r.Context()
	t0 := time.Now()

	// Phase 1+2: Decompose + Simulate
	req := &qa.QARequest{
		Question:          question,
		Lens:              lens,
		Fragments:         fragments,
		ArtifactSummaries: artifactSummaries,
	}
	simResult, plan, runErr := h.qaEngine.Run(ctx, req)
	decomposeSimulateMs := int(time.Since(t0).Milliseconds())

	if runErr != nil {
		_ = h.qaSessionsRepo.UpdateStatus(ctx, sessionID, horizonmodels.QASessionStatusFailed)
		writeSSE(w, "status", map[string]interface{}{
			"status": "failed",
			"error":  runErr.Error(),
		})
		return
	}

	writeSSE(w, "status", map[string]interface{}{
		"status":    "simulating",
		"sessionId": sessionID,
	})

	// Out of scope
	if plan.AnswerMode == "out_of_scope" {
		answer := &horizonmodels.QAAnswer{
			Text:       "This question doesn't seem related to the current lens scope. Try asking about entities, risks, or supply chain impacts within your lens.",
			Confidence: 0.3,
			AnswerMode: "out_of_scope",
		}
		session.Plan = plan
		session.SimulationResult = nil
		session.Answer = answer
		session.DecomposeLatencyMs = decomposeSimulateMs
		session.TotalLatencyMs = decomposeSimulateMs
		_ = h.qaSessionsRepo.UpdateWithAnswer(ctx, session)
		writeSSE(w, "answer_chunk", map[string]interface{}{"text": answer.Text})
		writeSSE(w, "answer_complete", answer)
		return
	}

	writeSSE(w, "status", map[string]interface{}{
		"status":    "synthesizing",
		"sessionId": sessionID,
	})

	// Highlights (for graph)
	highlightedNodeIDs := make([]string, 0, len(simResult.NodeImpacts))
	for _, ni := range simResult.NodeImpacts {
		if ni.FragmentID != "" {
			highlightedNodeIDs = append(highlightedNodeIDs, ni.FragmentID)
		}
	}
	writeSSE(w, "highlights", map[string]interface{}{
		"highlightedNodeIds": highlightedNodeIDs,
		"tracePaths":         simResult.TracePaths,
	})

	// Domain snapshot (existing analysis)
	var domainSnapshot *horizonmodels.DomainAnswerSnapshot
	if h.service.DomainAnswerSnapshots() != nil {
		snapshots, _ := h.service.DomainAnswerSnapshots().ListByLens(ctx, lensID, 1)
		if len(snapshots) > 0 {
			domainSnapshot = snapshots[0]
		}
	}

	// Humanization at finish: run Interpreter on top affected nodes (12-axis → human-readable)
	var nodeInterpretations []*horizoninterp.RiskInterpretation
	if h.qaInterpreter != nil && len(simResult.NodeImpacts) > 0 {
		polarity := "risk"
		if len(plan.HypotheticalSignals) > 0 && plan.HypotheticalSignals[0].Polarity != "" {
			polarity = plan.HypotheticalSignals[0].Polarity
		}
		nodeInterpretations = qa.RunFinishInterpretation(ctx, h.qaInterpreter, simResult, fragments, polarity, 5)
	}

	// Phase 3: Synthesize
	contextPrompt, ctxErr := h.qaContextBuilder.BuildContext(ctx, plan, simResult, domainSnapshot, artifactSummaries, nodeInterpretations, simResult.VertexAlternatives)
	if ctxErr != nil {
		_ = h.qaSessionsRepo.UpdateStatus(ctx, sessionID, horizonmodels.QASessionStatusFailed)
		writeSSE(w, "status", map[string]interface{}{"status": "failed", "error": ctxErr.Error()})
		return
	}

	synthStart := time.Now()
	answer, synthErr := h.qaSynthesizer.Synthesize(ctx, contextPrompt, plan, simResult)
	synthesizeMs := int(time.Since(synthStart).Milliseconds())

	if synthErr != nil {
		_ = h.qaSessionsRepo.UpdateStatus(ctx, sessionID, horizonmodels.QASessionStatusFailed)
		writeSSE(w, "status", map[string]interface{}{"status": "failed", "error": synthErr.Error()})
		return
	}

	// Evaluate business relevance (how well answer addresses the question)
	if h.qaRelevanceEvaluator != nil && answer.Text != "" {
		if score, err := h.qaRelevanceEvaluator.Evaluate(ctx, question, answer); err == nil {
			answer.BusinessRelevanceScore = score
		}
	}

	// Stream answer text as chunk
	if answer.Text != "" {
		writeSSE(w, "answer_chunk", map[string]interface{}{"text": answer.Text})
	}

	// Persist session
	session.Plan = plan
	session.NormalizedQuestion = plan.NormalizedQuestion
	session.SimulationResult = &horizonmodels.SimulationResultSummary{
		AffectedNodeCount:  len(simResult.NodeImpacts),
		TopNodeImpacts:     simResult.NodeImpacts,
		LeafEvidenceCount:  len(simResult.LeafEvidence),
		ChainImpacts:       simResult.ChainImpacts,
		SimulatedAxes:      plan.TargetFilter.ObserveAxes,
	}
	session.Answer = answer
	session.DecomposeLatencyMs = decomposeSimulateMs
	session.SynthesizeLatencyMs = synthesizeMs
	session.TotalLatencyMs = int(time.Since(t0).Milliseconds())

	if err := h.qaSessionsRepo.UpdateWithAnswer(ctx, session); err != nil {
		// Log but don't fail — answer was already streamed
		_ = err
	}

	writeSSE(w, "answer_complete", answer)
}
