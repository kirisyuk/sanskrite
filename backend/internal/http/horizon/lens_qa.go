package horizon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/analytical"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/qa"
	horizonscout "github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	telemetry "github.com/igorkirisyuk/sanskrite/internal/horizon/telemetrycollector"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

// qaRequest is the body for POST /api/horizon/lenses/{lensId}/qa
type qaRequest struct {
	Question  string `json:"question"`
	AutoScout bool   `json:"autoScout,omitempty"`
}

// writeSSE writes an SSE event.
func writeSSE(w http.ResponseWriter, event string, data interface{}) {
	payload, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

var sentenceSplitRe = regexp.MustCompile(`([.!?])\s+`)

// splitIntoSentences splits text on sentence-ending punctuation followed by
// whitespace or end of string, preserving the punctuation on the sentence.
func splitIntoSentences(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	indices := sentenceSplitRe.FindAllStringIndex(text, -1)
	if len(indices) == 0 {
		return []string{text}
	}
	var sentences []string
	prev := 0
	for _, loc := range indices {
		end := loc[0] + 1 // include the punctuation character
		s := strings.TrimSpace(text[prev:end])
		if s != "" {
			sentences = append(sentences, s)
		}
		prev = loc[1] // skip past the whitespace
	}
	if prev < len(text) {
		s := strings.TrimSpace(text[prev:])
		if s != "" {
			sentences = append(sentences, s)
		}
	}
	return sentences
}

func setSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
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

	// Cache check: same question <1h ago, answered → return cached
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
			log.Printf("[QA][lens=%s][session=%s] cache-hit answeredAt=%s question=%q",
				lensID,
				s.ID,
				s.AnsweredAt.UTC().Format(time.RFC3339),
				truncateForLog(question, 160),
			)
			setSSEHeaders(w)
			writeSSE(w, "status", map[string]interface{}{"status": "answered", "sessionId": s.ID, "cached": true})
			if len(s.Answer.HighlightedNodeIDs) > 0 || len(s.Answer.TracePaths) > 0 {
				writeSSE(w, "highlights", map[string]interface{}{
					"highlightedNodeIds": s.Answer.HighlightedNodeIDs,
					"tracePaths":         s.Answer.TracePaths,
				})
			}
			for _, sentence := range splitIntoSentences(s.Answer.Text) {
				writeSSE(w, "answer_chunk", map[string]interface{}{"text": sentence})
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
	artifactSummaries = append(artifactSummaries, h.loadLensQAAggregateSummaries(r.Context(), lens, question)...)

	// Create QASession
	sessionID := uuid.New().String()
	session := &horizonmodels.QASession{
		ID:          sessionID,
		LensID:      lensID,
		TenantID:    lens.TenantID,
		ProjectID:   lens.ProjectID,
		RawQuestion: question,
		Status:      horizonmodels.QASessionStatusDecomposing,
	}
	createdSessionID, createErr := h.qaSessionsRepo.Create(r.Context(), session)
	if createErr != nil {
		http.Error(w, "Failed to create QA session", http.StatusInternalServerError)
		return
	}
	if strings.TrimSpace(createdSessionID) != "" {
		sessionID = strings.TrimSpace(createdSessionID)
		session.ID = sessionID
	}
	qaLogPrefix := fmt.Sprintf("[QA][lens=%s][session=%s]", lensID, sessionID)
	log.Printf("%s start question=%q autoScout=%t scopePaths=%d fragmentsLoaded=%d scopeFragmentMap=%d artifacts=%d",
		qaLogPrefix,
		truncateForLog(question, 200),
		body.AutoScout,
		len(lens.Scope),
		len(fragments),
		len(scopeFragments),
		len(artifactSummaries),
	)

	// --- SSE response begins ---
	setSSEHeaders(w)
	ctx := r.Context()
	t0 := time.Now()

	// Telemetry: create or retrieve collector for this QA request.
	telCol := telemetry.CollectorFromContext(ctx)
	if telCol == nil {
		telCol = telemetry.New(lens.TenantID, lens.ProjectID, lensID, sessionID, "qa", "user_question")
		ctx = telemetry.WithCollector(ctx, telCol)
	}

	// Build ActiveSignals: top-5 metrics by value from fragment state
	var activeSignals []string
	type sigEntry struct {
		metric string
		value  float64
		path   string
	}
	var sigEntries []sigEntry
	for _, frag := range fragments {
		if frag == nil || frag.State == nil {
			continue
		}
		for metricPath, mv := range frag.State {
			if mv.Value > 0.05 {
				sigEntries = append(sigEntries, sigEntry{metric: metricPath, value: mv.Value, path: frag.CanonicalPath})
			}
		}
	}
	sort.Slice(sigEntries, func(i, j int) bool { return sigEntries[i].value > sigEntries[j].value })
	if len(sigEntries) > 5 {
		sigEntries = sigEntries[:5]
	}
	for _, e := range sigEntries {
		activeSignals = append(activeSignals, fmt.Sprintf("%s: %.2f at %s", e.metric, e.value, e.path))
	}

	// ─── Phase 1: Decompose ───
	writeSSE(w, "status", map[string]interface{}{
		"status":    "decomposing",
		"sessionId": sessionID,
	})

	lensCtx := &qa.DecomposeContext{
		ScopePaths:        lens.Scope[:minInts(30, len(lens.Scope))],
		DomainModes:       lens.DomainModes,
		ActiveSignals:     activeSignals,
		ArtifactSummaries: artifactSummaries,
	}
	telCol.BeginGroup("decompose")
	plan, decompErr := h.qaEngine.Decompose(ctx, question, lensCtx)
	telCol.EndGroup()
	if decompErr != nil {
		log.Printf("%s decompose-failed err=%v", qaLogPrefix, decompErr)
		_ = h.qaSessionsRepo.UpdateStatus(ctx, sessionID, horizonmodels.QASessionStatusFailed)
		writeSSE(w, "status", map[string]interface{}{"status": "failed", "error": decompErr.Error()})
		if h.jobTelemetryRepo != nil {
			_ = telCol.Flush(ctx, "failed", h.jobTelemetryRepo.Save)
		}
		return
	}
	log.Printf("%s decompose-ok answerMode=%s timeHorizon=%s hypotheticalSignals=%d observePatterns=%d observeAxes=%d contextHints=%d",
		qaLogPrefix,
		plan.AnswerMode,
		plan.TimeHorizon,
		len(plan.HypotheticalSignals),
		len(plan.TargetFilter.ObservePathPatterns),
		len(plan.TargetFilter.ObserveAxes),
		len(plan.ContextHints),
	)

	// Out of scope — short-circuit
	if plan.AnswerMode == "out_of_scope" {
		decomposeMs := int(time.Since(t0).Milliseconds())
		answer := &horizonmodels.QAAnswer{
			Text:       "This question doesn't seem related to the current lens scope. Try asking about entities, risks, or supply chain impacts within your lens.",
			Confidence: 0.3,
			AnswerMode: "out_of_scope",
		}
		session.Plan = plan
		session.Answer = answer
		session.DecomposeLatencyMs = decomposeMs
		session.TotalLatencyMs = decomposeMs
		_ = h.qaSessionsRepo.UpdateWithAnswer(ctx, session)
		log.Printf("%s out-of-scope answer emitted decomposeMs=%d", qaLogPrefix, decomposeMs)
		for _, sentence := range splitIntoSentences(answer.Text) {
			writeSSE(w, "answer_chunk", map[string]interface{}{"text": sentence})
		}
		writeSSE(w, "answer_complete", answer)
		return
	}

	// ─── Phase 1.5: Sufficiency check ───
	var suffReport *qa.SufficiencyReport
	if h.qaSufficiencyChecker != nil {
		suffReport = h.qaSufficiencyChecker.Check(plan, scopeFragments, fragments)
		writeSSE(w, "sufficiency_report", suffReport)
		log.Printf("%s sufficiency score=%.3f sufficient=%t gaps=%d",
			qaLogPrefix,
			suffReport.Score,
			suffReport.Sufficient,
			len(suffReport.Gaps),
		)
	}

	// ─── Phase 2: Auto-Scout (if enabled and data is insufficient) ───
	if body.AutoScout && suffReport != nil && !suffReport.Sufficient {
		log.Printf("%s autoscout-triggered gaps=%d", qaLogPrefix, len(suffReport.Gaps))
		h.runAutoScout(ctx, w, sessionID, plan, suffReport, lens, &fragments, scopeFragments)
		log.Printf("%s autoscout-finished fragmentsLoaded=%d scopeFragmentMap=%d",
			qaLogPrefix,
			len(fragments),
			len(scopeFragments),
		)
	} else {
		log.Printf("%s autoscout-skipped enabled=%t sufficiency_available=%t sufficient=%t",
			qaLogPrefix,
			body.AutoScout,
			suffReport != nil,
			suffReport != nil && suffReport.Sufficient,
		)
	}

	// ─── Phase 2.5: Signal Gap Detection + Best-effort Fill ───
	var (
		fillReport *qa.FillReport
		fillErr    error
	)
	if h.qaSignalGapDetector != nil && h.qaGroundingQueryBuilder != nil {
		seedIDs, seedSource := collectSeedIDsForGapFill(plan, scopeFragments, fragments)
		log.Printf("%s gap-detect seeds=%d source=%s", qaLogPrefix, len(seedIDs), seedSource)
		gaps := h.qaSignalGapDetector.Detect(seedIDs, fragments, plan)
		log.Printf("%s gap-detect result gaps=%d", qaLogPrefix, len(gaps))
		if len(gaps) > 0 {
			writeSSE(w, "status", map[string]interface{}{
				"status":    "filling_gaps",
				"sessionId": sessionID,
				"gapCount":  len(gaps),
			})

			queries := h.qaGroundingQueryBuilder.Build(gaps, question)
			log.Printf("%s gap-fill queriesBuilt=%d", qaLogPrefix, len(queries))
			if h.qaBestEffortFiller != nil {
				telCol.BeginGroup("signal_fill")
				fillReport, fillErr = h.qaBestEffortFiller.Fill(ctx, gaps, queries, lensID)
				telCol.EndGroup()
				if fillErr != nil {
					log.Printf("%s gap-fill error=%v", qaLogPrefix, fillErr)
				}
				if fillReport != nil {
					log.Printf("%s gap-fill report filled=%d failed=%d docs=%d facts=%d sources=%d costUsd=%.4f",
						qaLogPrefix,
						len(fillReport.FilledGaps),
						len(fillReport.FailedGaps),
						len(fillReport.CreatedDocIDs),
						len(fillReport.CreatedFactIDs),
						len(fillReport.Sources),
						fillReport.TotalCostUSD,
					)
					writeSSE(w, "signal_fill_report", fillReport)
					if len(fillReport.FilledGaps) > 0 {
						if reloaded, reloadErr := h.service.Fragments().ListByCanonicalPaths(ctx, lens.Scope); reloadErr != nil {
							log.Printf("%s gap-fill reload-fragments failed err=%v", qaLogPrefix, reloadErr)
						} else if len(reloaded) > 0 {
							fragments = reloaded
							for _, f := range reloaded {
								if f != nil && f.CanonicalPath != "" && f.ID != "" {
									scopeFragments[f.CanonicalPath] = f.ID
								}
							}
							log.Printf("%s gap-fill reload-fragments ok fragmentsLoaded=%d scopeFragmentMap=%d",
								qaLogPrefix,
								len(fragments),
								len(scopeFragments),
							)
						}
					}
				}
			} else {
				log.Printf("%s gap-fill skipped reason=best_effort_filler_not_configured", qaLogPrefix)
				writeSSE(w, "signal_fill_report", map[string]interface{}{
					"gapsDetected":  len(gaps),
					"queriesBuilt":  len(queries),
					"fillCompleted": false,
				})
			}
		}
	}
	if len(plan.HypotheticalSignals) == 0 && fillReport != nil && len(fillReport.FilledGaps) > 0 {
		added := appendRecoveryHypotheticalSignals(plan, fillReport)
		log.Printf("%s recovery-signals injected=%d totalHypotheticalSignals=%d",
			qaLogPrefix,
			added,
			len(plan.HypotheticalSignals),
		)
	}

	// ─── Phase 3: Simulate ───
	writeSSE(w, "status", map[string]interface{}{
		"status":    "simulating",
		"sessionId": sessionID,
	})

	simStart := time.Now()
	log.Printf("%s simulate-start hypotheticalSignals=%d", qaLogPrefix, len(plan.HypotheticalSignals))
	telCol.BeginGroup("simulate")
	simResult, simErr := h.qaEngine.Simulate(ctx, plan, scopeFragments, fragments)
	telCol.EndGroup()
	simulateMs := int(time.Since(simStart).Milliseconds())
	if simErr != nil {
		log.Printf("%s simulate-failed err=%v", qaLogPrefix, simErr)
		_ = h.qaSessionsRepo.UpdateStatus(ctx, sessionID, horizonmodels.QASessionStatusFailed)
		writeSSE(w, "status", map[string]interface{}{"status": "failed", "error": simErr.Error()})
		if h.jobTelemetryRepo != nil {
			_ = telCol.Flush(ctx, "failed", h.jobTelemetryRepo.Save)
		}
		return
	}
	log.Printf("%s simulate-ok durationMs=%d nodeImpacts=%d tracePaths=%d seedFailures=%d",
		qaLogPrefix,
		simulateMs,
		len(simResult.NodeImpacts),
		len(simResult.TracePaths),
		len(simResult.SeedFailures),
	)

	telCol.BeginGroup("leaf_evidence")
	affectedIDs := make(map[string]bool)
	for _, ni := range simResult.NodeImpacts {
		affectedIDs[ni.FragmentID] = true
	}
	leafEv := h.qaEngine.CollectLeafEvidence(ctx, fragments, affectedIDs)
	simResult.LeafEvidence = append(simResult.LeafEvidence, leafEv...)
	telCol.EndGroup()
	log.Printf("%s leaf-evidence count=%d", qaLogPrefix, len(simResult.LeafEvidence))

	telCol.BeginGroup("chain_impacts")
	simResult = qa.ApplyTargetFilter(ctx, simResult, plan)
	if h.materializedArtifactsRepo != nil {
		arts, _ := h.materializedArtifactsRepo.ListByLens(ctx, lensID, 50)
		simResult.ChainImpacts = qa.ComputeChainImpacts(arts, simResult.NodeImpacts)
	}
	simResult.HighlightedEdgeIDs = qa.ExtractEdgeIDsFromTracePaths(simResult.TracePaths, scopeFragments)
	telCol.EndGroup()
	log.Printf("%s post-filter nodeImpacts=%d chainImpacts=%d highlightedEdges=%d",
		qaLogPrefix,
		len(simResult.NodeImpacts),
		len(simResult.ChainImpacts),
		len(simResult.HighlightedEdgeIDs),
	)

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
		"highlightedEdgeIds": simResult.HighlightedEdgeIDs,
	})

	// ─── Phase 4: Synthesize ───
	writeSSE(w, "status", map[string]interface{}{
		"status":    "synthesizing",
		"sessionId": sessionID,
	})

	// Domain snapshot (existing analysis)
	var domainSnapshot *horizonmodels.DomainAnswerSnapshot
	if h.service.DomainAnswerSnapshots() != nil {
		snapshots, _ := h.service.DomainAnswerSnapshots().ListByLens(ctx, lensID, 1)
		if len(snapshots) > 0 {
			domainSnapshot = snapshots[0]
		}
	}

	if fillReport != nil {
		h.qaContextBuilder.WithFillReport(fillReport)
	}
	defer func() { h.qaContextBuilder.WithFillReport(nil) }()

	contextPrompt, ctxErr := h.qaContextBuilder.BuildContext(ctx, plan, simResult, domainSnapshot, artifactSummaries)
	if ctxErr != nil {
		log.Printf("%s build-context-failed err=%v", qaLogPrefix, ctxErr)
		_ = h.qaSessionsRepo.UpdateStatus(ctx, sessionID, horizonmodels.QASessionStatusFailed)
		writeSSE(w, "status", map[string]interface{}{"status": "failed", "error": ctxErr.Error()})
		if h.jobTelemetryRepo != nil {
			_ = telCol.Flush(ctx, "failed", h.jobTelemetryRepo.Save)
		}
		return
	}

	// LOGGING FOR DEBUGGING: Log the context prompt
	log.Printf("%s [DEBUG] Context Prompt (truncated): %q", qaLogPrefix, truncateForLog(contextPrompt, 1000))

	synthStart := time.Now()
	telCol.BeginGroup("synthesize")
	answer, synthErr := h.qaSynthesizer.Synthesize(ctx, contextPrompt, plan, simResult, scopeFragments)
	telCol.EndGroup()
	synthesizeMs := int(time.Since(synthStart).Milliseconds())

	if synthErr != nil {
		log.Printf("%s synthesize-failed err=%v", qaLogPrefix, synthErr)
		_ = h.qaSessionsRepo.UpdateStatus(ctx, sessionID, horizonmodels.QASessionStatusFailed)
		writeSSE(w, "status", map[string]interface{}{"status": "failed", "error": synthErr.Error()})
		if h.jobTelemetryRepo != nil {
			_ = telCol.Flush(ctx, "failed", h.jobTelemetryRepo.Save)
		}
		return
	}

	// Stream answer as sentence chunks
	for _, sentence := range splitIntoSentences(answer.Text) {
		writeSSE(w, "answer_chunk", map[string]interface{}{"text": sentence})
	}

	// Persist session
	decomposeSimulateMs := int(time.Since(t0).Milliseconds()) - synthesizeMs
	session.Plan = plan
	session.NormalizedQuestion = plan.NormalizedQuestion
	session.SimulationResult = &horizonmodels.SimulationResultSummary{
		AffectedNodeCount: len(simResult.NodeImpacts),
		TopNodeImpacts:    simResult.NodeImpacts,
		LeafEvidenceCount: len(simResult.LeafEvidence),
		ChainImpacts:      simResult.ChainImpacts,
		SimulatedAxes:     plan.TargetFilter.ObserveAxes,
	}
	session.Answer = answer
	session.DecomposeLatencyMs = decomposeSimulateMs
	session.SimulateLatencyMs = simulateMs
	session.SynthesizeLatencyMs = synthesizeMs
	session.TotalLatencyMs = int(time.Since(t0).Milliseconds())

	if err := h.qaSessionsRepo.UpdateWithAnswer(ctx, session); err != nil {
		log.Printf("%s persist-session-failed err=%v", qaLogPrefix, err)
	} else {
		log.Printf("%s completed totalMs=%d answerChars=%d confidence=%.2f",
			qaLogPrefix,
			session.TotalLatencyMs,
			len(answer.Text),
			answer.Confidence,
		)
		// LOGGING FOR DEBUGGING: Log the answer text to see if it's "meaningless"
		log.Printf("%s [DEBUG] Generated Answer Text: %q", qaLogPrefix, truncateForLog(answer.Text, 500))
	}

	if h.jobTelemetryRepo != nil {
		_ = telCol.Flush(ctx, "completed", h.jobTelemetryRepo.Save)
	}

	writeSSE(w, "answer_complete", answer)
}

func (h *HorizonHandlers) loadLensQAAggregateSummaries(ctx context.Context, lens *horizonmodels.Lens, question string) []string {
	if h == nil || lens == nil || h.analyticalPrimitivesRepo == nil || h.analyticalAggregatesRepo == nil {
		return nil
	}
	scopePaths := lensChatScopePaths(lens)
	out := make([]string, 0, 4)
	if aggregate, err := analytical.EnsureLensAggregate(ctx, h.analyticalPrimitivesRepo, h.analyticalAggregatesRepo, h.materializedArtifactsRepo, lens, scopePaths, time.Now().UTC()); err == nil && aggregate != nil && strings.TrimSpace(aggregate.Explanation) != "" {
		out = append(out, "Analytical weak points: "+strings.TrimSpace(aggregate.Explanation))
	}
	if classifyLensChatAnalyticalIntent(question) == "scenario" && h.service != nil && h.service.Simulation() != nil {
		if run, err := h.service.Simulation().GetLatestRunByLens(ctx, strings.TrimSpace(lens.TenantID), strings.TrimSpace(lens.ID)); err == nil && run != nil {
			if aggregate, err := h.service.Simulation().EnsureScenarioAggregate(ctx, strings.TrimSpace(lens.TenantID), strings.TrimSpace(run.ID)); err == nil && aggregate != nil && strings.TrimSpace(aggregate.Explanation) != "" {
				out = append(out, "Scenario disruption impact: "+strings.TrimSpace(aggregate.Explanation))
			}
		}
	}
	return uniqueStringsLocal(out)
}

// runAutoScout creates scouting jobs for unresolved seed hints, waits for
// completion, and reloads fragments into the caller's slice.
func (h *HorizonHandlers) runAutoScout(
	ctx context.Context,
	w http.ResponseWriter,
	sessionID string,
	plan *horizonmodels.QuestionPlan,
	suffReport *qa.SufficiencyReport,
	lens *horizonmodels.Lens,
	fragments *[]*horizonmodels.HorizonFragment,
	scopeFragments map[string]string,
) {
	scoutJobsRepo := h.scoutingJobsRepo
	if scoutJobsRepo == nil {
		scoutJobsRepo = h.service.ScoutingJobs()
	}
	if scoutJobsRepo == nil {
		return
	}

	builder := qa.NewQAScoutRequestBuilder()
	jobParams := builder.Build(plan, suffReport, lens.ID, lens.TenantID, lens.ProjectID)
	if len(jobParams) == 0 {
		return
	}

	const scoutPollInterval = 3 * time.Second
	const scoutTimeout = 2 * time.Minute

	for _, params := range jobParams {
		scoutJob := &horizonmodels.ScoutingJob{
			AgentJob: coremodels.AgentJob{
				TenantID:  params.TenantID,
				ProjectID: params.ProjectID,
				AgentID:   "scout",
				Status:    coremodels.AgentJobStatusPending,
				CreatedAt: time.Now().UTC(),
				UpdatedAt: time.Now().UTC(),
				State: map[string]interface{}{
					"lensId":              params.LensID,
					"targetCanonicalPath": params.TargetCanonicalPath,
					"seedHints":           params.SeedHints,
					"recommendedFacets":   params.RecommendedFacets,
					"scoutingStrategy":    params.Strategy,
					"qaTriggered":         true,
				},
			},
			LensID:              params.LensID,
			TargetCanonicalPath: params.TargetCanonicalPath,
			ScoutingStrategy:    params.Strategy,
		}

		jobID, createErr := scoutJobsRepo.Create(ctx, scoutJob)
		if createErr != nil {
			continue
		}

		writeSSE(w, "scout_started", map[string]interface{}{
			"jobId":               jobID,
			"targetCanonicalPath": params.TargetCanonicalPath,
			"sessionId":           sessionID,
		})

		// Poll with incremental progress heartbeats
		deadline := time.Now().Add(scoutTimeout)
		var finishedJob *horizonmodels.ScoutingJob
		var waitErr error
		tick := 0
		for {
			if ctx.Err() != nil {
				waitErr = ctx.Err()
				break
			}
			if time.Now().After(deadline) {
				waitErr = fmt.Errorf("scout timeout after %v", scoutTimeout)
				break
			}
			time.Sleep(scoutPollInterval)
			tick++
			polled, pollErr := scoutJobsRepo.Get(ctx, jobID)
			if pollErr != nil {
				continue
			}
			if horizonscout.IsTerminal(string(polled.CurrentState)) || horizonscout.IsTerminal(string(polled.Status)) {
				finishedJob = polled
				break
			}
			writeSSE(w, "scout_progress", map[string]interface{}{
				"jobId": jobID,
				"state": string(polled.CurrentState),
				"tick":  tick,
			})
		}
		if waitErr != nil {
			writeSSE(w, "scout_progress", map[string]interface{}{
				"jobId":    jobID,
				"state":    "timeout",
				"progress": 0.0,
				"error":    waitErr.Error(),
			})
			continue
		}
		if finishedJob == nil {
			continue
		}

		newFragCount := 0
		if finishedJob.Result != nil {
			if v, ok := finishedJob.Result["fragmentsCreated"]; ok {
				switch n := v.(type) {
				case float64:
					newFragCount = int(n)
				case int:
					newFragCount = n
				}
			}
		}

		writeSSE(w, "scout_complete", map[string]interface{}{
			"jobId":        jobID,
			"status":       string(finishedJob.Status),
			"newFragments": newFragCount,
		})
	}

	// Reload fragments after all scout jobs finish
	reloaded, reloadErr := h.service.Fragments().ListByCanonicalPaths(ctx, lens.Scope)
	if reloadErr == nil && len(reloaded) > 0 {
		*fragments = reloaded
		for _, f := range reloaded {
			if f != nil && f.CanonicalPath != "" && f.ID != "" {
				scopeFragments[f.CanonicalPath] = f.ID
			}
		}
	}
}

// collectAllSeedIDs gathers all seed fragment IDs from the question plan.
func collectAllSeedIDs(plan *horizonmodels.QuestionPlan, scopeFragments map[string]string) []string {
	if plan == nil {
		return nil
	}
	resolver := qa.NewSeedResolver()
	seen := make(map[string]bool)
	var out []string
	for _, sig := range plan.HypotheticalSignals {
		for _, id := range resolver.Resolve(sig.SeedHints, scopeFragments) {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

// collectSeedIDsForGapFill returns seed IDs used by gap detection.
// Priority:
// 1) Hypothetical signal seeds (if present).
// 2) Target filter path patterns matched against scope + loaded fragments.
func collectSeedIDsForGapFill(
	plan *horizonmodels.QuestionPlan,
	scopeFragments map[string]string,
	fragments []*horizonmodels.HorizonFragment,
) ([]string, string) {
	seedIDs := collectAllSeedIDs(plan, scopeFragments)
	if len(seedIDs) > 0 {
		return seedIDs, "hypothetical"
	}
	if plan == nil || len(plan.TargetFilter.ObservePathPatterns) == 0 {
		return nil, "none"
	}

	seen := make(map[string]bool)
	var out []string

	addID := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}

	// Keep deterministic preference by observePathPatterns order.
	for _, pattern := range plan.TargetFilter.ObservePathPatterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		for canonicalPath, fragmentID := range scopeFragments {
			if matchesPathPattern(canonicalPath, pattern) {
				addID(fragmentID)
			}
		}
		for _, frag := range fragments {
			if frag == nil {
				continue
			}
			if matchesPathPattern(strings.TrimSpace(frag.CanonicalPath), pattern) {
				addID(frag.ID)
			}
		}
	}

	if len(out) == 0 {
		return nil, "none"
	}
	return out, "target_filter"
}

func matchesPathPattern(path string, pattern string) bool {
	path = strings.TrimSpace(path)
	pattern = strings.TrimSpace(pattern)
	if path == "" || pattern == "" {
		return false
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(path, strings.TrimSuffix(pattern, "*"))
	}
	return path == pattern
}

// appendRecoveryHypotheticalSignals injects synthetic hypothetical signals from successful
// fill results so simulation can run in propagation mode even for blocker-style questions.
func appendRecoveryHypotheticalSignals(plan *horizonmodels.QuestionPlan, fillReport *qa.FillReport) int {
	if plan == nil || fillReport == nil || len(fillReport.FilledGaps) == 0 {
		return 0
	}

	existing := make(map[string]bool)
	for _, sig := range plan.HypotheticalSignals {
		for _, hint := range sig.SeedHints {
			key := strings.TrimSpace(sig.Axis) + "|" + strings.TrimSpace(hint)
			if key != "|" {
				existing[key] = true
			}
		}
	}

	added := 0
	for _, fg := range fillReport.FilledGaps {
		axis := strings.TrimSpace(fg.Gap.Axis)
		seedPath := strings.TrimSpace(fg.Gap.CanonicalPath)
		if axis == "" || seedPath == "" {
			continue
		}
		key := axis + "|" + seedPath
		if existing[key] {
			continue
		}
		existing[key] = true

		amp := fg.Amplitude
		if amp <= 0 {
			amp = 0.5
		}
		if amp > 1 {
			amp = 1
		}

		plan.HypotheticalSignals = append(plan.HypotheticalSignals, horizonmodels.HypotheticalSignal{
			Axis:        axis,
			Mechanism:   "grounded_signal_fill",
			Amplitude:   amp,
			Polarity:    "risk",
			SeedHints:   []string{seedPath},
			TimeHorizon: plan.TimeHorizon,
			Modality:    "actual",
		})
		added++
	}
	if added == 0 {
		return 0
	}

	// Keep observeAxes aligned with injected recovery signals.
	axesSeen := make(map[string]bool)
	for _, axis := range plan.TargetFilter.ObserveAxes {
		axis = strings.TrimSpace(axis)
		if axis != "" {
			axesSeen[axis] = true
		}
	}
	for _, sig := range plan.HypotheticalSignals {
		axis := strings.TrimSpace(sig.Axis)
		if axis == "" || axesSeen[axis] {
			continue
		}
		axesSeen[axis] = true
		plan.TargetFilter.ObserveAxes = append(plan.TargetFilter.ObserveAxes, axis)
	}
	return added
}

func truncateForLog(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

// minInts returns the smaller of a and b.
func minInts(a, b int) int {
	if a < b {
		return a
	}
	return b
}
