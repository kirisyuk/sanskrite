package horizon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/analytical"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/workscope"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

const (
	lensChatModel                   = "gemini-3.1-flash-lite-preview"
	lensChatThreadPrefix            = "lens-chat:"
	defaultLensChatHitLimit         = 10
	defaultLensChatEvidenceDocLimit = 8
	defaultLensChatGlobalHitLimit   = 8
	defaultLensChatHistoryTurns     = 6
)

var canonicalPathPattern = regexp.MustCompile(`/[a-z0-9][a-z0-9/_-]*`)

type lensChatRequest struct {
	Message string `json:"message"`
}

type lensChatResponse struct {
	ThreadID         string                `json:"threadId"`
	Answer           string                `json:"answer"`
	Hits             []map[string]any      `json:"hits,omitempty"`
	HighlightedPaths []string              `json:"highlightedPaths,omitempty"`
	Warnings         []string              `json:"warnings,omitempty"`
	UserMessage      *corerepo.Interaction `json:"userMessage,omitempty"`
	AssistantMessage *corerepo.Interaction `json:"assistantMessage,omitempty"`
}

type lensChatInternalHit struct {
	ID         string
	Title      string
	Kind       string
	URI        string
	Score      float64
	StructData map[string]any
}

type lensChatPreparedEvidenceDoc struct {
	ID             string
	Kind           string
	Title          string
	Content        string
	URI            string
	CanonicalPaths []string
}

type lensChatTurn struct {
	Kind    string
	Content string
}

func (h *HorizonHandlers) LensChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo, err := requireAuth(r)
	if err != nil {
		_ = httputil.WriteUnauthorized(w, err.Error())
		return
	}
	if h.service == nil || h.service.Lens() == nil {
		_ = httputil.WriteInternalError(w, "Horizon service not configured")
		return
	}
	if h.genAIHandler == nil {
		_ = httputil.WriteInternalError(w, "GenAI handler not configured")
		return
	}
	if h.interactions == nil {
		_ = httputil.WriteInternalError(w, "Interactions repository not configured")
		return
	}

	lensID := strings.TrimSpace(extractIDFromPath(r, "/api/horizon/lenses/"))
	if lensID == "" {
		_ = httputil.WriteBadRequest(w, "INVALID_PATH", "lensId is required")
		return
	}

	lens, err := h.service.Lens().GetLens(r.Context(), lensID)
	if err != nil || lens == nil {
		_ = httputil.WriteNotFound(w, "Lens not found")
		return
	}
	if strings.TrimSpace(lens.TenantID) != strings.TrimSpace(authInfo.TenantID) {
		_ = httputil.WriteForbidden(w, "Access denied")
		return
	}
	projectID := strings.TrimSpace(lens.ProjectID)
	if projectID == "" {
		_ = httputil.WriteInternalError(w, "Lens is missing projectId")
		return
	}

	var req lensChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		_ = httputil.WriteBadRequest(w, "INVALID_JSON", "Failed to parse body")
		return
	}
	message := strings.TrimSpace(req.Message)
	if message == "" {
		_ = httputil.WriteBadRequest(w, "INVALID_MESSAGE", "message is required")
		return
	}

	threadID := lensChatThreadID(lensID)
	userMessage := &corerepo.Interaction{
		TenantID:  authInfo.TenantID,
		ProjectID: projectID,
		Kind:      "user",
		Subkind:   lensChatStringPtr("lens.chat"),
		Content:   message,
		SectionID: &threadID,
		Author: &corerepo.ParticipantRef{
			ParticipantID: strings.TrimSpace(authInfo.UID),
			Kind:          "human",
			DisplayName:   lensChatNonEmptyPtr(strings.TrimSpace(authInfo.Email)),
		},
		SearchQueries: []string{message},
	}
	userID, err := h.interactions.Create(r.Context(), userMessage)
	if err != nil {
		_ = httputil.WriteInternalError(w, "Failed to persist user message")
		return
	}
	userMessage.ID = userID

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	history, historyWarnings := h.loadLensChatHistory(ctx, projectID, threadID, userID, defaultLensChatHistoryTurns)
	answer, hits, highlightedPaths, warnings := h.answerLensChat(ctx, lens, message, history)
	warnings = append(historyWarnings, warnings...)
	assistantMessage := &corerepo.Interaction{
		TenantID:  authInfo.TenantID,
		ProjectID: projectID,
		Kind:      "assistant",
		Subkind:   lensChatStringPtr("lens.chat"),
		Content:   answer,
		Summary:   lensChatNonEmptyPtr(lensChatTruncateString(answer, 220)),
		SectionID: &threadID,
		ReplyTo:   &userID,
		Model:     lensChatStringPtr(lensChatModel),
		Author: &corerepo.ParticipantRef{
			ParticipantID: "lens-chat",
			Kind:          "ai",
			DisplayName:   lensChatStringPtr("Lens Chat"),
		},
		SearchQueries: []string{message},
		Sources:       collectHitSources(hits, 8),
	}
	assistantID, err := h.interactions.Create(r.Context(), assistantMessage)
	if err != nil {
		log.Printf("lens chat: failed to persist assistant message for lens=%s project=%s thread=%s: %v", lensID, projectID, threadID, err)
		assistantMessage = nil
		warnings = append(warnings, "assistant message was not persisted")
	} else {
		assistantMessage.ID = assistantID
	}

	_ = httputil.WriteJSON(w, http.StatusOK, lensChatResponse{
		ThreadID:         threadID,
		Answer:           answer,
		Hits:             summarizeLensChatHits(hits, defaultLensChatHitLimit),
		HighlightedPaths: highlightedPaths,
		Warnings:         warnings,
		UserMessage:      userMessage,
		AssistantMessage: assistantMessage,
	})
}

func (h *HorizonHandlers) answerLensChat(ctx context.Context, lens *horizonmodels.Lens, message string, history []lensChatTurn) (string, []lensChatInternalHit, []string, []string) {
	warnings := make([]string, 0, 4)
	analyticalIntent := classifyLensChatAnalyticalIntent(message)
	analyticalHits, analyticalDocs, analyticalPaths, analyticalWarnings := h.loadLensChatAnalyticalEvidence(ctx, lens, message, analyticalIntent)
	warnings = append(warnings, analyticalWarnings...)
	warmState := lensChatWarmState(lens)
	requiredTemperature := lensChatRequiredTemperature(message, analyticalIntent)
	warmedReady := lensChatTemperatureReady(warmState, requiredTemperature)
	storeID := strings.TrimSpace(asStringAny(warmState["answerStore"]))
	corpusID := strings.TrimSpace(asStringAny(warmState["corpusId"]))
	scopePaths := lensChatScopePaths(lens)
	searchClient := h.service.VertexSearchClient()
	if searchClient == nil {
		return "Internal knowledge search is unavailable for this lens right now.", nil, nil, []string{"vertex search client is not configured"}
	}
	if storeID != "" {
		searchClient = searchClient.WithDataStore(storeID)
	}
	if !warmedReady {
		if h != nil && h.service != nil && h.service.Lens() != nil {
			_ = h.service.Lens().RequestInternalKnowledgeWarmup(ctx, strings.TrimSpace(lens.ID), "lens_chat_"+requiredTemperature+"_demand")
		}
		if lensAssemblyReadyLocal(lens) {
			warnings = append(warnings, fmt.Sprintf("lens internal knowledge is still warming to %s; answer uses matching prepared system knowledge", requiredTemperature))
		} else {
			warnings = append(warnings, fmt.Sprintf("lens internal knowledge is not warmed to %s yet; answer uses matching prepared system knowledge", requiredTemperature))
		}
	}

	lensHits, searchWarnings := searchLensChatInternalKnowledge(ctx, searchClient, lensChatSearchSpec{
		Question:          message,
		CorpusID:          lensChatFilteredCorpusID(warmedReady, corpusID),
		Limit:             defaultLensChatHitLimit,
		ScopeRefs:         scopePaths,
		PreferredCorpusID: corpusID,
		History:           history,
	})
	warnings = append(warnings, searchWarnings...)
	evidenceDocs, evidenceWarnings := loadLensChatPreparedEvidenceDocs(ctx, h.gcsClient, lensHits, defaultLensChatEvidenceDocLimit)
	warnings = append(warnings, evidenceWarnings...)

	hits := mergeLensChatHits(append([]lensChatInternalHit(nil), analyticalHits...), lensHits, defaultLensChatHitLimit+defaultLensChatGlobalHitLimit)
	evidenceDocs = mergeLensChatPreparedEvidenceDocs(analyticalDocs, evidenceDocs, defaultLensChatEvidenceDocLimit+defaultLensChatGlobalHitLimit)
	if warmedReady && lensChatNeedsGlobalFallback(message, hits, evidenceDocs) {
		globalHits, globalWarnings := searchLensChatInternalKnowledge(ctx, searchClient, lensChatSearchSpec{
			Question:          message,
			Limit:             defaultLensChatGlobalHitLimit,
			ScopeRefs:         scopePaths,
			PreferredCorpusID: corpusID,
			History:           history,
		})
		warnings = append(warnings, globalWarnings...)
		hits = mergeLensChatHits(hits, globalHits, defaultLensChatHitLimit+defaultLensChatGlobalHitLimit)
		evidenceDocs, evidenceWarnings = loadLensChatPreparedEvidenceDocs(ctx, h.gcsClient, hits, defaultLensChatEvidenceDocLimit)
		warnings = append(warnings, evidenceWarnings...)
		evidenceDocs = mergeLensChatPreparedEvidenceDocs(analyticalDocs, evidenceDocs, defaultLensChatEvidenceDocLimit+defaultLensChatGlobalHitLimit)
		if len(globalHits) > 0 {
			warnings = append(warnings, "answer was widened beyond the current lens corpus to use matching prepared system knowledge")
		}
	}
	highlightedPaths := collectLensChatEvidencePaths(hits, evidenceDocs, 32)
	highlightedPaths = limitStringsLocal(filterCanonicalPathsLocal(append(highlightedPaths, analyticalPaths...)), 32)
	if len(hits) == 0 || len(evidenceDocs) == 0 {
		answer := "I could not find enough grounded internal knowledge for this question."
		if !warmedReady {
			answer = fmt.Sprintf("Internal knowledge for this question class is not ready at %s temperature yet. Ask again after warmup completes.", requiredTemperature)
			if lensAssemblyReadyLocal(lens) {
				answer = fmt.Sprintf("Internal knowledge for this lens is still warming to %s. Ask again in a moment.", requiredTemperature)
			}
		} else if corpusID != "" {
			answer = "I could not find enough grounded internal knowledge for this question in the current lens or matching prepared system knowledge."
		}
		return answer, hits, highlightedPaths, uniqueStringsLocal(warnings)
	}

	prompt := buildLensChatSynthesisPrompt(lens, message, history, hits, evidenceDocs, highlightedPaths)
	genResp, err := h.genAIHandler.GenerateDirect(ctx, gen_combined.GenAIRequest{
		Model:             lensChatModel,
		Prompt:            prompt,
		SystemInstruction: lensChatSystemInstruction(),
		MaxOutputTokens:   1400,
		Temperature:       0.15,
		ProjectID:         strings.TrimSpace(lens.ProjectID),
	})
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("final synthesis failed: %v", err))
		return fallbackLensChatAnswer(hits, evidenceDocs, highlightedPaths), hits, highlightedPaths, uniqueStringsLocal(warnings)
	}

	answer := strings.TrimSpace(genResp.Content)
	if answer == "" {
		answer = fallbackLensChatAnswer(hits, evidenceDocs, highlightedPaths)
		warnings = append(warnings, "final synthesis returned empty content")
	}
	answer = rewriteCanonicalPathsForDisplay(answer, hits)
	return answer, hits, highlightedPaths, uniqueStringsLocal(warnings)
}

func lensChatFilteredCorpusID(warmedReady bool, corpusID string) string {
	if !warmedReady {
		return ""
	}
	return strings.TrimSpace(corpusID)
}

type lensChatSearchSpec struct {
	Question          string
	CorpusID          string
	Limit             int
	ScopeRefs         []string
	PreferredCorpusID string
	History           []lensChatTurn
}

func searchLensChatInternalKnowledge(ctx context.Context, client *ai.VertexAISearchClient, spec lensChatSearchSpec) ([]lensChatInternalHit, []string) {
	if client == nil {
		return nil, []string{"internal knowledge search client is nil"}
	}
	limit := spec.Limit
	if limit <= 0 {
		limit = defaultLensChatHitLimit
	}

	hits := make([]lensChatInternalHit, 0, limit*2)
	warnings := make([]string, 0, 4)
	for _, variant := range buildLensChatQueryVariants(spec.Question, spec.History, spec.CorpusID, spec.ScopeRefs) {
		resp, err := client.Search(ctx, &ai.SearchRequest{
			Query:    variant,
			PageSize: maxIntLocal(limit*4, 20),
			SpellCorrectionSpec: &ai.SpellCorrectionSpec{
				Mode: "AUTO",
			},
		})
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("vertex search failed for %q: %v", variant, err))
			continue
		}

		current := make([]lensChatInternalHit, 0, len(resp.Results))
		for _, res := range resp.Results {
			structData := cloneStringAnyMap(res.Document.StructData)
			if strings.TrimSpace(asStringAny(structData["type"])) != "internal_knowledge_capsule" {
				continue
			}
			if spec.CorpusID != "" && strings.TrimSpace(asStringAny(structData["corpusId"])) != spec.CorpusID {
				continue
			}
			current = append(current, lensChatInternalHit{
				ID:         lensChatFirstNonEmptyString(strings.TrimSpace(res.Document.ID), strings.TrimSpace(res.Document.Name)),
				Title:      lensChatFirstNonEmptyString(strings.TrimSpace(res.Document.Title), strings.TrimSpace(asStringAny(structData["title"]))),
				Kind:       strings.TrimSpace(asStringAny(structData["kind"])),
				URI:        lensChatFirstNonEmptyString(strings.TrimSpace(res.Document.URI), strings.TrimSpace(asStringAny(structData["uri"]))),
				Score:      res.RelevanceScore,
				StructData: structData,
			})
		}
		hits = mergeLensChatHits(hits, current, limit*6)
	}
	return rerankLensChatHits(spec.Question, hits, spec.ScopeRefs, spec.PreferredCorpusID, limit), warnings
}

func buildLensChatSynthesisPrompt(lens *horizonmodels.Lens, question string, history []lensChatTurn, hits []lensChatInternalHit, evidenceDocs []lensChatPreparedEvidenceDoc, highlightedPaths []string) string {
	var b strings.Builder
	b.WriteString("Lens: ")
	b.WriteString(lensChatFirstNonEmptyString(strings.TrimSpace(lens.Name), strings.TrimSpace(lens.ID)))
	b.WriteString("\n")
	if len(history) > 0 {
		b.WriteString("Recent conversation context:\n")
		for _, turn := range history {
			role := "Assistant"
			if strings.EqualFold(strings.TrimSpace(turn.Kind), "user") {
				role = "User"
			}
			b.WriteString(role)
			b.WriteString(": ")
			b.WriteString(strings.TrimSpace(turn.Content))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("Question: ")
	b.WriteString(strings.TrimSpace(question))
	b.WriteString("\n\n")
	b.WriteString("Grounded internal hits:\n")
	for i, hit := range hits {
		if i >= 12 {
			break
		}
		b.WriteString(fmt.Sprintf("- kind=%s title=%s score=%.3f", lensChatFirstNonEmptyString(hit.Kind, "unknown"), lensChatFirstNonEmptyString(hit.Title, hit.ID), hit.Score))
		paths := limitStringsLocal(stringSliceAny(hit.StructData["canonicalPaths"]), 6)
		if len(paths) > 0 {
			b.WriteString(" paths=")
			b.WriteString(strings.Join(paths, ", "))
		}
		hints := limitStringsLocal(stringSliceAny(hit.StructData["questionHints"]), 4)
		if len(hints) > 0 {
			b.WriteString(" hints=")
			b.WriteString(strings.Join(hints, ", "))
		}
		b.WriteString("\n")
	}
	b.WriteString("\nPrepared evidence pack:\n")
	b.WriteString(renderLensChatPreparedEvidenceDocs(evidenceDocs))
	if len(highlightedPaths) > 0 {
		b.WriteString("\nCandidate canonical paths:\n")
		b.WriteString(strings.Join(highlightedPaths, "\n"))
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func lensChatSystemInstruction() string {
	return strings.TrimSpace(`
You are Lens Chat for Horizon.
Use only the provided internal grounded context. Do not use web knowledge. Do not invent missing facts.
Answer in the same language as the user's question.
Use human-readable names in prose. Do not expose canonical paths like /org/... or /fac/... unless the user explicitly asks for canonical identifiers.
Use recent conversation context only to resolve references and follow-up questions. Do not treat prior turns as evidence.

Return a concise operator-style answer:
1. Direct answer first.
1a. If fresh signal_landing_summary or recent signal-aware evidence is present, answer from event-core first, then impact halo, then broader structural background.
2. Name the key objects and their roles.
3. Mention suppliers, routes, products, ports, facilities, and relationships if they appear in the grounded evidence pack.
4. Prefer explicit objects and route legs over generic summaries.
4a. For questions about current disruptions or what changed right now, do not let older structural background override fresher signal-aware evidence.
5. End with explicit uncertainty only if the grounded context is incomplete.
`)
}

func fallbackLensChatAnswer(hits []lensChatInternalHit, evidenceDocs []lensChatPreparedEvidenceDoc, highlightedPaths []string) string {
	if len(evidenceDocs) > 0 {
		lines := make([]string, 0, 8)
		for i, doc := range evidenceDocs {
			if i >= 4 {
				break
			}
			line := fmt.Sprintf("%s (%s)", lensChatFirstNonEmptyString(doc.Title, doc.ID), lensChatFirstNonEmptyString(doc.Kind, "evidence"))
			if len(doc.CanonicalPaths) > 0 {
				line += ": " + strings.Join(limitStringsLocal(doc.CanonicalPaths, 4), ", ")
			}
			lines = append(lines, line)
		}
		if len(highlightedPaths) > 0 {
			lines = append(lines, "Canonical paths: "+strings.Join(limitStringsLocal(highlightedPaths, 6), ", "))
		}
		if len(lines) > 0 {
			return strings.Join(lines, "\n")
		}
	}
	lines := make([]string, 0, 8)
	for i, hit := range hits {
		if i >= 4 {
			break
		}
		label := lensChatFirstNonEmptyString(strings.TrimSpace(hit.Title), strings.TrimSpace(hit.ID))
		line := fmt.Sprintf("%s (%s)", label, lensChatFirstNonEmptyString(strings.TrimSpace(hit.Kind), "internal hit"))
		paths := limitStringsLocal(stringSliceAny(hit.StructData["canonicalPaths"]), 3)
		if len(paths) > 0 {
			line += ": " + strings.Join(paths, ", ")
		}
		lines = append(lines, line)
	}
	if len(highlightedPaths) > 0 {
		lines = append(lines, "Canonical paths: "+strings.Join(limitStringsLocal(highlightedPaths, 6), ", "))
	}
	if len(lines) == 0 {
		return "No grounded internal knowledge hits were available for this question."
	}
	return strings.Join(lines, "\n")
}

func rewriteCanonicalPathsForDisplay(answer string, hits []lensChatInternalHit) string {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return answer
	}
	replacements := buildDisplayLabelMap(hits)
	paths := extractCanonicalPathsFromText(answer)
	sort.Slice(paths, func(i, j int) bool {
		return len(paths[i]) > len(paths[j])
	})
	for _, path := range paths {
		label := strings.TrimSpace(replacements[path])
		if label == "" {
			label = humanizeCanonicalPath(path)
		}
		if label == "" || label == path {
			continue
		}
		answer = strings.ReplaceAll(answer, path, label)
	}
	return answer
}

func summarizeLensChatHits(hits []lensChatInternalHit, limit int) []map[string]any {
	out := make([]map[string]any, 0, minIntLocal(limit, len(hits)))
	for i, hit := range hits {
		if i >= limit {
			break
		}
		out = append(out, map[string]any{
			"id":             strings.TrimSpace(hit.ID),
			"title":          strings.TrimSpace(hit.Title),
			"kind":           strings.TrimSpace(hit.Kind),
			"score":          hit.Score,
			"uri":            strings.TrimSpace(hit.URI),
			"canonicalPaths": limitStringsLocal(stringSliceAny(hit.StructData["canonicalPaths"]), 12),
			"questionHints":  limitStringsLocal(stringSliceAny(hit.StructData["questionHints"]), 8),
		})
	}
	return out
}

func collectLensChatPaths(hits []lensChatInternalHit, limit int) []string {
	paths := make([]string, 0, limit)
	for _, hit := range hits {
		paths = append(paths, stringSliceAny(hit.StructData["canonicalPaths"])...)
		paths = append(paths, extractCanonicalPathsFromText(strings.TrimSpace(hit.Title))...)
	}
	return limitStringsLocal(filterCanonicalPathsLocal(paths), limit)
}

func collectLensChatEvidencePaths(hits []lensChatInternalHit, docs []lensChatPreparedEvidenceDoc, limit int) []string {
	paths := collectLensChatPaths(hits, limit)
	for _, doc := range docs {
		paths = append(paths, doc.CanonicalPaths...)
	}
	return limitStringsLocal(filterCanonicalPathsLocal(paths), limit)
}

func lensChatScopePaths(lens *horizonmodels.Lens) []string {
	if lens == nil {
		return nil
	}
	return limitStringsLocal(filterCanonicalPathsLocal(workscope.LensWorkbenchScopePaths(lens)), 48)
}

func lensChatNeedsGlobalFallback(question string, hits []lensChatInternalHit, evidenceDocs []lensChatPreparedEvidenceDoc) bool {
	for _, doc := range evidenceDocs {
		kind := strings.TrimSpace(doc.Kind)
		if strings.HasPrefix(kind, "analytical_") || kind == "signal_landing_summary" {
			return false
		}
	}
	if len(hits) == 0 || len(evidenceDocs) == 0 {
		return true
	}
	if len(hits) >= 6 && len(evidenceDocs) >= 4 {
		return false
	}
	logisticsQuery := strings.Contains(strings.ToLower(question), "supply") || strings.Contains(strings.ToLower(question), "route") || strings.Contains(strings.ToLower(question), "logistics") || strings.Contains(strings.ToLower(question), "постав") || strings.Contains(strings.ToLower(question), "маршрут")
	if logisticsQuery {
		for _, hit := range hits {
			switch strings.ToLower(strings.TrimSpace(hit.Kind)) {
			case "corridor", "route_artifact", "relation", "dossier":
				return false
			}
		}
		return true
	}
	return len(evidenceDocs) < 3
}

func classifyLensChatAnalyticalIntent(question string) string {
	q := strings.ToLower(strings.TrimSpace(question))
	switch {
	case q == "":
		return ""
	case strings.Contains(q, "right now"), strings.Contains(q, "currently"), strings.Contains(q, "current disruption"), strings.Contains(q, "what disruptions"), strings.Contains(q, "what changed"), strings.Contains(q, "affecting "), strings.Contains(q, "сейчас"), strings.Contains(q, "прямо сейчас"), strings.Contains(q, "какие сбои"), strings.Contains(q, "что изменилось"):
		return "current_signal"
	case strings.Contains(q, "what happens if"), strings.Contains(q, "impact if"), strings.Contains(q, "disruption impact"), strings.Contains(q, "что будет если"), strings.Contains(q, "влияние сбоя"), strings.Contains(q, "impact of disruption"):
		return "scenario"
	case strings.Contains(q, "weak point"), strings.Contains(q, "bottleneck"), strings.Contains(q, "critical dependenc"), strings.Contains(q, "single point of failure"), strings.Contains(q, "fragility"), strings.Contains(q, "chokepoint"), strings.Contains(q, "critical path"), strings.Contains(q, "узк"), strings.Contains(q, "слаб"), strings.Contains(q, "зависимост"), strings.Contains(q, "хрупк"), strings.Contains(q, "бутылоч"):
		return "baseline_risk"
	default:
		return ""
	}
}

func (h *HorizonHandlers) loadLensChatAnalyticalEvidence(ctx context.Context, lens *horizonmodels.Lens, message, intent string) ([]lensChatInternalHit, []lensChatPreparedEvidenceDoc, []string, []string) {
	if h == nil || lens == nil || intent == "" {
		return nil, nil, nil, nil
	}
	hits := make([]lensChatInternalHit, 0, 8)
	docs := make([]lensChatPreparedEvidenceDoc, 0, 8)
	paths := make([]string, 0, 24)
	warnings := make([]string, 0, 4)
	scopePaths := lensChatScopePaths(lens)

	if h.signalLandingSummariesRepo != nil && (intent == "current_signal" || intent == "scenario") {
		summaries, err := h.signalLandingSummariesRepo.ListByScopeRefs(ctx, strings.TrimSpace(lens.TenantID), scopePaths, 12)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("signal landing summary load failed: %v", err))
		} else {
			for i, summary := range summaries {
				if i >= 4 || summary == nil {
					break
				}
				hits = append(hits, lensChatHitFromSignalLandingSummary(summary, 1.45))
				docs = append(docs, lensChatDocFromSignalLandingSummary(summary))
				paths = append(paths, signalLandingSummaryEventCoreRefs(summary)...)
				paths = append(paths, signalLandingSummaryHaloRefs(summary)...)
			}
		}
	}

	if h.analyticalPrimitivesRepo != nil && h.analyticalAggregatesRepo != nil {
		aggregate, err := analytical.EnsureLensAggregate(ctx, h.analyticalPrimitivesRepo, h.analyticalAggregatesRepo, h.materializedArtifactsRepo, lens, scopePaths, time.Now().UTC())
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("analytical lens aggregate refresh failed: %v", err))
		} else if aggregate != nil {
			hits = append(hits, lensChatHitFromAggregate(aggregate, 1.2))
			docs = append(docs, lensChatDocFromAggregate(aggregate))
			paths = append(paths, aggregate.BasisRefs...)
			paths = append(paths, aggregate.ProductRefs...)
			paths = append(paths, aggregate.NodeRefs...)
			paths = append(paths, aggregate.RouteRefs...)
			paths = append(paths, aggregate.AffectedRefs...)
			primitives, err := h.analyticalPrimitivesRepo.ListByIDs(ctx, limitStringsLocal(aggregate.DerivedPrimitiveIDs, 8))
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("analytical primitive load failed: %v", err))
			} else {
				for _, primitive := range primitives {
					if primitive == nil {
						continue
					}
					hits = append(hits, lensChatHitFromPrimitive(primitive, 0.95))
					docs = append(docs, lensChatDocFromPrimitive(primitive))
					paths = append(paths, primitive.BasisRefs...)
					paths = append(paths, primitive.ProductRefs...)
					paths = append(paths, primitive.NodeRefs...)
					paths = append(paths, primitive.RouteRefs...)
					paths = append(paths, primitive.AffectedRefs...)
				}
			}
		}
	}

	if intent == "scenario" && h.service != nil && h.service.Simulation() != nil {
		run, err := h.service.Simulation().GetLatestRunByLens(ctx, strings.TrimSpace(lens.TenantID), strings.TrimSpace(lens.ID))
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("latest simulation lookup failed: %v", err))
		} else if run != nil {
			aggregate, err := h.service.Simulation().EnsureScenarioAggregate(ctx, strings.TrimSpace(lens.TenantID), strings.TrimSpace(run.ID))
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("scenario aggregate refresh failed: %v", err))
			} else if aggregate != nil {
				hits = append(hits, lensChatHitFromAggregate(aggregate, 1.35))
				docs = append(docs, lensChatDocFromAggregate(aggregate))
				paths = append(paths, aggregate.BasisRefs...)
				paths = append(paths, aggregate.ProductRefs...)
				paths = append(paths, aggregate.AffectedRefs...)
			}
		}
	}

	return mergeLensChatHits(nil, hits, 12), mergeLensChatPreparedEvidenceDocs(nil, docs, 12), limitStringsLocal(filterCanonicalPathsLocal(paths), 32), uniqueStringsLocal(warnings)
}

func lensChatHitFromAggregate(aggregate *horizonmodels.AnalyticalAggregate, boost float64) lensChatInternalHit {
	paths := uniqueStringsLocal(append(append([]string{aggregate.ScopeRef, aggregate.SubjectRef}, aggregate.BasisRefs...), append(append(append(aggregate.ProductRefs, aggregate.NodeRefs...), aggregate.RouteRefs...), aggregate.AffectedRefs...)...))
	return lensChatInternalHit{
		ID:    strings.TrimSpace(aggregate.ID),
		Title: lensChatFirstNonEmptyString(strings.TrimSpace(aggregate.AggregateType), "analytical aggregate"),
		Kind:  "analytical_aggregate",
		URI:   "analytical://aggregate/" + strings.TrimSpace(aggregate.ID),
		Score: aggregate.SeverityScore * boost,
		StructData: map[string]any{
			"type":           "analytical_aggregate",
			"title":          lensChatFirstNonEmptyString(strings.TrimSpace(aggregate.AggregateType), "analytical aggregate"),
			"canonicalPaths": paths,
			"questionHints": []string{
				"weak points",
				"bottlenecks",
				"critical dependencies",
			},
		},
	}
}

func lensChatHitFromPrimitive(primitive *horizonmodels.AnalyticalPrimitive, boost float64) lensChatInternalHit {
	paths := uniqueStringsLocal(append(append([]string{primitive.ScopeRef, primitive.SubjectRef}, primitive.BasisRefs...), append(append(append(primitive.ProductRefs, primitive.NodeRefs...), primitive.RouteRefs...), primitive.AffectedRefs...)...))
	return lensChatInternalHit{
		ID:    strings.TrimSpace(primitive.ID),
		Title: lensChatFirstNonEmptyString(strings.TrimSpace(primitive.PrimitiveType), "analytical primitive"),
		Kind:  "analytical_primitive",
		URI:   "analytical://primitive/" + strings.TrimSpace(primitive.ID),
		Score: primitive.SeverityScore * boost,
		StructData: map[string]any{
			"type":           "analytical_primitive",
			"title":          lensChatFirstNonEmptyString(strings.TrimSpace(primitive.PrimitiveType), "analytical primitive"),
			"canonicalPaths": paths,
			"questionHints": []string{
				"weak points",
				"risk concentration",
				"dependency analysis",
			},
		},
	}
}

func lensChatHitFromSignalLandingSummary(summary *horizonmodels.SignalLandingSummary, boost float64) lensChatInternalHit {
	paths := uniqueStringsLocal(append(signalLandingSummaryEventCoreRefs(summary), signalLandingSummaryHaloRefs(summary)...))
	return lensChatInternalHit{
		ID:    strings.TrimSpace(summary.ID),
		Title: buildLensChatSignalLandingTitle(summary),
		Kind:  "signal_landing_summary",
		URI:   "signal-landing://summary/" + strings.TrimSpace(summary.ID),
		Score: lensChatSignalSummaryBoost(summary) * boost,
		StructData: map[string]any{
			"type":           "signal_landing_summary",
			"title":          buildLensChatSignalLandingTitle(summary),
			"canonicalPaths": paths,
			"questionHints":  append([]string(nil), summary.TopClaims...),
			"eventCoreRefs":  signalLandingSummaryEventCoreRefs(summary),
			"haloRefs":       signalLandingSummaryHaloRefs(summary),
			"updatedAt":      summary.UpdatedAt.Format(time.RFC3339Nano),
			"freshnessClass": lensChatSignalFreshnessClass(summary.UpdatedAt),
		},
	}
}

func lensChatDocFromAggregate(aggregate *horizonmodels.AnalyticalAggregate) lensChatPreparedEvidenceDoc {
	return lensChatPreparedEvidenceDoc{
		ID:             strings.TrimSpace(aggregate.ID),
		Kind:           "analytical_aggregate",
		Title:          lensChatFirstNonEmptyString(strings.TrimSpace(aggregate.AggregateType), "analytical aggregate"),
		Content:        strings.TrimSpace(aggregate.GenerateVectorText()),
		URI:            "analytical://aggregate/" + strings.TrimSpace(aggregate.ID),
		CanonicalPaths: uniqueStringsLocal(append(append([]string{aggregate.ScopeRef, aggregate.SubjectRef}, aggregate.BasisRefs...), append(append(append(aggregate.ProductRefs, aggregate.NodeRefs...), aggregate.RouteRefs...), aggregate.AffectedRefs...)...)),
	}
}

func lensChatDocFromPrimitive(primitive *horizonmodels.AnalyticalPrimitive) lensChatPreparedEvidenceDoc {
	return lensChatPreparedEvidenceDoc{
		ID:             strings.TrimSpace(primitive.ID),
		Kind:           "analytical_primitive",
		Title:          lensChatFirstNonEmptyString(strings.TrimSpace(primitive.PrimitiveType), "analytical primitive"),
		Content:        strings.TrimSpace(primitive.GenerateVectorText()),
		URI:            "analytical://primitive/" + strings.TrimSpace(primitive.ID),
		CanonicalPaths: uniqueStringsLocal(append(append([]string{primitive.ScopeRef, primitive.SubjectRef}, primitive.BasisRefs...), append(append(append(primitive.ProductRefs, primitive.NodeRefs...), primitive.RouteRefs...), primitive.AffectedRefs...)...)),
	}
}

func lensChatDocFromSignalLandingSummary(summary *horizonmodels.SignalLandingSummary) lensChatPreparedEvidenceDoc {
	return lensChatPreparedEvidenceDoc{
		ID:             strings.TrimSpace(summary.ID),
		Kind:           "signal_landing_summary",
		Title:          buildLensChatSignalLandingTitle(summary),
		Content:        buildLensChatSignalLandingDoc(summary),
		URI:            "signal-landing://summary/" + strings.TrimSpace(summary.ID),
		CanonicalPaths: uniqueStringsLocal(append(signalLandingSummaryEventCoreRefs(summary), signalLandingSummaryHaloRefs(summary)...)),
	}
}

func buildLensChatSignalLandingTitle(summary *horizonmodels.SignalLandingSummary) string {
	if summary == nil {
		return "signal landing summary"
	}
	if len(summary.AffectedZoneRefs) > 0 {
		return "Fresh signal around " + summary.AffectedZoneRefs[0]
	}
	if len(summary.TopConfirmedRefs) > 0 {
		return "Fresh signal around " + summary.TopConfirmedRefs[0]
	}
	return "Fresh signal for " + strings.TrimSpace(summary.ScopeRef)
}

func buildLensChatSignalLandingDoc(summary *horizonmodels.SignalLandingSummary) string {
	if summary == nil {
		return ""
	}
	lines := []string{
		"Fresh signal landing summary",
		"Scope: " + strings.TrimSpace(summary.ScopeRef),
	}
	if !summary.UpdatedAt.IsZero() {
		lines = append(lines, "Updated at: "+summary.UpdatedAt.UTC().Format(time.RFC3339))
	}
	if core := signalLandingSummaryEventCoreRefs(summary); len(core) > 0 {
		lines = append(lines, "Event core: "+strings.Join(limitStringsLocal(core, 8), ", "))
	}
	if halo := signalLandingSummaryHaloRefs(summary); len(halo) > 0 {
		lines = append(lines, "Impact halo: "+strings.Join(limitStringsLocal(halo, 10), ", "))
	}
	if len(summary.TopClaims) > 0 {
		lines = append(lines, "Claims: "+strings.Join(limitStringsLocal(summary.TopClaims, 6), "; "))
	}
	if len(summary.TopMissingCandidates) > 0 {
		lines = append(lines, "Missing anchors: "+strings.Join(limitStringsLocal(summary.TopMissingCandidates, 6), ", "))
	}
	return strings.Join(lines, "\n")
}

func signalLandingSummaryEventCoreRefs(summary *horizonmodels.SignalLandingSummary) []string {
	if summary == nil {
		return nil
	}
	return uniqueStringsLocal(append(append([]string{summary.ScopeRef}, summary.TopConfirmedRefs...), append(summary.TopInferredRefs, summary.AffectedZoneRefs...)...))
}

func signalLandingSummaryHaloRefs(summary *horizonmodels.SignalLandingSummary) []string {
	if summary == nil {
		return nil
	}
	return uniqueStringsLocal(append(summary.Products, append(append(summary.Routes, summary.Facilities...), summary.Chokepoints...)...))
}

func lensChatSignalFreshnessClass(updatedAt time.Time) string {
	if updatedAt.IsZero() {
		return "stale"
	}
	age := time.Since(updatedAt.UTC())
	switch {
	case age <= 2*time.Hour:
		return "flash"
	case age <= 24*time.Hour:
		return "hot"
	case age <= 7*24*time.Hour:
		return "warm"
	default:
		return "stale"
	}
}

func lensChatSignalSummaryBoost(summary *horizonmodels.SignalLandingSummary) float64 {
	if summary == nil {
		return 1
	}
	boost := 1.0
	switch lensChatSignalFreshnessClass(summary.UpdatedAt) {
	case "flash":
		boost += 0.45
	case "hot":
		boost += 0.3
	case "warm":
		boost += 0.15
	}
	boost += 0.03 * float64(len(summary.TopConfirmedRefs))
	boost += 0.02 * float64(len(summary.Products)+len(summary.Routes)+len(summary.Facilities)+len(summary.Chokepoints))
	return boost
}

func mergeLensChatPreparedEvidenceDocs(base, extra []lensChatPreparedEvidenceDoc, limit int) []lensChatPreparedEvidenceDoc {
	if limit <= 0 {
		limit = defaultLensChatEvidenceDocLimit
	}
	seen := make(map[string]struct{}, len(base)+len(extra))
	out := make([]lensChatPreparedEvidenceDoc, 0, minIntLocal(limit, len(base)+len(extra)))
	appendDocs := func(items []lensChatPreparedEvidenceDoc) {
		for _, item := range items {
			if len(out) >= limit {
				return
			}
			key := strings.TrimSpace(lensChatFirstNonEmptyString(item.ID, item.Title, item.URI))
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, item)
		}
	}
	appendDocs(base)
	appendDocs(extra)
	return out
}

func loadLensChatPreparedEvidenceDocs(ctx context.Context, gcsClient interface {
	ReadObject(context.Context, string, string) ([]byte, error)
}, hits []lensChatInternalHit, limit int) ([]lensChatPreparedEvidenceDoc, []string) {
	if gcsClient == nil || len(hits) == 0 || limit <= 0 {
		return nil, nil
	}
	out := make([]lensChatPreparedEvidenceDoc, 0, minIntLocal(limit, len(hits)))
	warnings := make([]string, 0, 4)
	for _, hit := range hits {
		if len(out) >= limit {
			break
		}
		bucket, object, ok := parseLensChatGCSURI(hit.URI)
		if !ok {
			continue
		}
		body, err := gcsClient.ReadObject(ctx, bucket, object)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("read evidence doc failed uri=%s: %v", hit.URI, err))
			continue
		}
		doc, err := decodeLensChatPreparedEvidenceDoc(body)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("decode evidence doc failed uri=%s: %v", hit.URI, err))
			continue
		}
		doc.URI = lensChatFirstNonEmptyString(strings.TrimSpace(doc.URI), strings.TrimSpace(hit.URI))
		doc.ID = lensChatFirstNonEmptyString(strings.TrimSpace(doc.ID), strings.TrimSpace(hit.ID))
		doc.Kind = lensChatFirstNonEmptyString(strings.TrimSpace(doc.Kind), strings.TrimSpace(hit.Kind))
		doc.Title = lensChatFirstNonEmptyString(strings.TrimSpace(doc.Title), strings.TrimSpace(hit.Title), strings.TrimSpace(hit.ID))
		if len(doc.CanonicalPaths) == 0 {
			doc.CanonicalPaths = limitStringsLocal(stringSliceAny(hit.StructData["canonicalPaths"]), 20)
		}
		out = append(out, doc)
	}
	return out, warnings
}

func decodeLensChatPreparedEvidenceDoc(body []byte) (lensChatPreparedEvidenceDoc, error) {
	var payload struct {
		ID         string         `json:"id"`
		StructData map[string]any `json:"structData"`
		Content    struct {
			RawBytes string `json:"rawBytes"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return lensChatPreparedEvidenceDoc{}, err
	}
	contentBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(payload.Content.RawBytes))
	if err != nil {
		return lensChatPreparedEvidenceDoc{}, err
	}
	return lensChatPreparedEvidenceDoc{
		ID:             strings.TrimSpace(payload.ID),
		Kind:           strings.TrimSpace(asStringAny(payload.StructData["kind"])),
		Title:          strings.TrimSpace(asStringAny(payload.StructData["title"])),
		Content:        strings.TrimSpace(string(contentBytes)),
		URI:            strings.TrimSpace(asStringAny(payload.StructData["uri"])),
		CanonicalPaths: limitStringsLocal(stringSliceAny(payload.StructData["canonicalPaths"]), 20),
	}, nil
}

func renderLensChatPreparedEvidenceDocs(docs []lensChatPreparedEvidenceDoc) string {
	if len(docs) == 0 {
		return "No hydrated evidence documents were available."
	}
	var b strings.Builder
	for i, doc := range docs {
		if i >= 10 {
			break
		}
		fmt.Fprintf(&b, "[Doc %d] kind=%s title=%s id=%s\n", i+1, lensChatFirstNonEmptyString(doc.Kind, "unknown"), lensChatFirstNonEmptyString(doc.Title, doc.ID), doc.ID)
		if len(doc.CanonicalPaths) > 0 {
			fmt.Fprintf(&b, "refs=%s\n", strings.Join(doc.CanonicalPaths, ", "))
		}
		if doc.URI != "" {
			fmt.Fprintf(&b, "uri=%s\n", doc.URI)
		}
		fmt.Fprintf(&b, "%s\n\n", doc.Content)
	}
	return strings.TrimSpace(b.String())
}

func buildDisplayLabelMap(hits []lensChatInternalHit) map[string]string {
	out := make(map[string]string, len(hits)*2)
	for _, hit := range hits {
		title := strings.TrimSpace(hit.Title)
		paths := limitStringsLocal(stringSliceAny(hit.StructData["canonicalPaths"]), 12)
		if title != "" && len(paths) == 1 {
			out[paths[0]] = title
		}
		for _, path := range paths {
			if _, exists := out[path]; exists {
				continue
			}
			if label := humanizeCanonicalPath(path); label != "" {
				out[path] = label
			}
		}
	}
	return out
}

func humanizeCanonicalPath(path string) string {
	path = strings.TrimSpace(strings.ToLower(path))
	if path == "" || !strings.HasPrefix(path, "/") {
		return ""
	}
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) == 0 {
		return ""
	}
	start := 1
	if len(segments) > 1 && len(segments[1]) == 2 {
		start = 2
	}
	if start >= len(segments) {
		start = len(segments) - 1
	}
	parts := make([]string, 0, len(segments)-start)
	for _, segment := range segments[start:] {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		words := strings.Split(segment, "-")
		for i, word := range words {
			if word == "" {
				continue
			}
			words[i] = strings.ToUpper(word[:1]) + word[1:]
		}
		parts = append(parts, strings.Join(words, " "))
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func collectHitSources(hits []lensChatInternalHit, limit int) []string {
	sources := make([]string, 0, minIntLocal(limit, len(hits)))
	for _, hit := range hits {
		source := lensChatFirstNonEmptyString(strings.TrimSpace(hit.URI), strings.TrimSpace(hit.Title), strings.TrimSpace(hit.ID))
		if source == "" {
			continue
		}
		sources = append(sources, source)
	}
	return limitStringsLocal(uniqueStringsLocal(sources), limit)
}

func lensChatWarmState(lens *horizonmodels.Lens) map[string]any {
	if lens == nil || lens.ViewConfig == nil {
		return map[string]any{}
	}
	raw, ok := lens.ViewConfig["internalKnowledge"]
	if !ok || raw == nil {
		return map[string]any{}
	}
	if value, ok := raw.(map[string]any); ok && value != nil {
		return value
	}
	if value, ok := raw.(map[string]interface{}); ok && value != nil {
		out := make(map[string]any, len(value))
		for k, v := range value {
			out[k] = v
		}
		return out
	}
	return map[string]any{}
}

func lensChatRequiredTemperature(question, analyticalIntent string) string {
	q := strings.ToLower(strings.TrimSpace(question))
	switch analyticalIntent {
	case "baseline_risk", "scenario", "current_signal":
		return "hot"
	}
	switch {
	case strings.Contains(q, "who supplies"), strings.Contains(q, "supplier"), strings.Contains(q, "постав"), strings.Contains(q, "route"), strings.Contains(q, "маршрут"), strings.Contains(q, "via"), strings.Contains(q, "corridor"):
		return "warm"
	case strings.Contains(q, "what exists"), strings.Contains(q, "who is"), strings.Contains(q, "что есть"):
		return "hot"
	default:
		return "warm"
	}
}

func lensChatTemperatureReady(state map[string]any, temperature string) bool {
	temperature = strings.ToLower(strings.TrimSpace(temperature))
	if temperature == "" {
		return strings.EqualFold(strings.TrimSpace(asStringAny(state["status"])), "ready")
	}
	if strings.EqualFold(strings.TrimSpace(asStringAny(state["status"])), "ready") {
		return true
	}
	if _, ok := parseLensChatRFC3339(state[temperature+"ReadyAt"]); ok {
		return true
	}
	for _, available := range stringSliceAny(state["availableTemperatures"]) {
		if lensChatTemperatureRank(available) >= lensChatTemperatureRank(temperature) {
			return true
		}
	}
	return false
}

func lensChatTemperatureRank(temperature string) int {
	switch strings.ToLower(strings.TrimSpace(temperature)) {
	case "hot":
		return 1
	case "warm":
		return 2
	case "full":
		return 3
	default:
		return 0
	}
}

func parseLensChatRFC3339(value any) (time.Time, bool) {
	raw := strings.TrimSpace(asStringAny(value))
	if raw == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func lensAssemblyReadyLocal(lens *horizonmodels.Lens) bool {
	if lens == nil || lens.ViewConfig == nil {
		return false
	}
	raw, ok := lens.ViewConfig["assemblyStatus"]
	if !ok || raw == nil {
		return false
	}
	state := map[string]any{}
	switch typed := raw.(type) {
	case map[string]interface{}:
		for k, v := range typed {
			state[k] = v
		}
	default:
		return false
	}
	return strings.EqualFold(strings.TrimSpace(asStringAny(state["phase"])), "ready")
}

func lensChatThreadID(lensID string) string {
	return lensChatThreadPrefix + strings.TrimSpace(lensID)
}

func buildLensChatQueryVariants(question string, history []lensChatTurn, corpusID string, scopeRefs []string) []string {
	base := uniqueStringsLocal([]string{
		strings.TrimSpace(question),
		strings.Join(textTokensLocal(question), " "),
		strings.TrimSpace(strings.NewReplacer("?", "", ",", "", ".", "", ":", "", ";", "").Replace(question)),
	})
	if context := lensChatHistorySearchContext(history); context != "" {
		base = append(base, context)
		base = append(base, strings.TrimSpace(question)+" "+context)
	}
	base = append(base, corpusQueryVariantsLocal(corpusID)...)
	base = append(base, lensChatScopeQueryVariants(scopeRefs)...)
	joined := strings.ToLower(strings.Join(base, " "))
	if strings.Contains(joined, "supply") || strings.Contains(joined, "route") || strings.Contains(joined, "logistics") {
		base = append(base, "products suppliers customers routes logistics")
	}
	if intent := classifyLensChatAnalyticalIntent(question); intent == "current_signal" || intent == "scenario" {
		base = append(base, "current disruptions fresh signal event impact affected routes products chokepoints")
	}
	return uniqueStringsLocal(base)
}

func (h *HorizonHandlers) loadLensChatHistory(ctx context.Context, projectID, threadID, excludeID string, limit int) ([]lensChatTurn, []string) {
	if h.interactions == nil || strings.TrimSpace(projectID) == "" || strings.TrimSpace(threadID) == "" || limit <= 0 {
		return nil, nil
	}
	items, err := h.interactions.ListByProject(ctx, projectID)
	if err != nil {
		return nil, []string{fmt.Sprintf("failed to load recent lens conversation: %v", err)}
	}
	turns := make([]lensChatTurn, 0, limit)
	for _, item := range items {
		if item == nil {
			continue
		}
		if strings.TrimSpace(item.ID) == strings.TrimSpace(excludeID) {
			continue
		}
		if strings.TrimSpace(item.ProjectID) != strings.TrimSpace(projectID) {
			continue
		}
		if strings.TrimSpace(asStringPointerLocal(item.SectionID)) != strings.TrimSpace(threadID) {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(asStringPointerLocal(item.Subkind)), "lens.chat") {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(item.Kind))
		if kind != "user" && kind != "assistant" {
			continue
		}
		content := strings.TrimSpace(item.Content)
		if content == "" {
			continue
		}
		turns = append(turns, lensChatTurn{
			Kind:    kind,
			Content: lensChatCompactText(content, 220),
		})
	}
	if len(turns) > limit {
		turns = turns[len(turns)-limit:]
	}
	return turns, nil
}

func lensChatHistorySearchContext(history []lensChatTurn) string {
	if len(history) == 0 {
		return ""
	}
	parts := make([]string, 0, len(history))
	for _, turn := range history {
		if strings.EqualFold(strings.TrimSpace(turn.Kind), "assistant") {
			continue
		}
		text := lensChatCompactText(turn.Content, 120)
		if text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 {
		for _, turn := range history {
			text := lensChatCompactText(turn.Content, 120)
			if text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.TrimSpace(strings.Join(limitStringsLocal(parts, 3), " "))
}

func lensChatScopeQueryVariants(scopeRefs []string) []string {
	if len(scopeRefs) == 0 {
		return nil
	}
	out := make([]string, 0, minIntLocal(8, len(scopeRefs)))
	for _, path := range limitStringsLocal(filterCanonicalPathsLocal(scopeRefs), 6) {
		out = append(out, humanizeCanonicalPath(path))
	}
	return uniqueStringsLocal(out)
}

func corpusQueryVariantsLocal(corpusID string) []string {
	corpusID = strings.TrimSpace(strings.ToLower(corpusID))
	if corpusID == "" {
		return nil
	}
	parts := strings.Split(corpusID, "-")
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || looksLikeHashPart(part) {
			continue
		}
		cleaned = append(cleaned, part)
	}
	if len(cleaned) == 0 {
		return nil
	}
	out := []string{strings.Join(cleaned, " ")}
	if len(cleaned) > 2 {
		out = append(out, strings.Join(cleaned[:minIntLocal(4, len(cleaned))], " "))
	}
	return uniqueStringsLocal(out)
}

func rerankLensChatHits(question string, hits []lensChatInternalHit, scopeRefs []string, preferredCorpusID string, limit int) []lensChatInternalHit {
	if len(hits) == 0 {
		return nil
	}
	queryTokens := normalizedTokenSetLocal(question)
	logisticsQuery := strings.Contains(strings.ToLower(question), "supply") || strings.Contains(strings.ToLower(question), "route") || strings.Contains(strings.ToLower(question), "logistics")
	currentSignalQuery := classifyLensChatAnalyticalIntent(question) == "current_signal"
	scopeRefSet := normalizedTokenSetLocal(strings.Join(scopeRefs, " "))
	signalContext := buildLensChatSignalContext(hits)
	type scoredHit struct {
		hit   lensChatInternalHit
		score float64
	}
	scored := make([]scoredHit, 0, len(hits))
	for _, hit := range hits {
		score := hit.Score * 100
		pathTokens := normalizedTokenSetLocal(strings.Join(stringSliceAny(hit.StructData["canonicalPaths"]), " "))
		score += kindWeightLocal(hit.Kind, logisticsQuery)
		score += 4.0 * tokenOverlapScoreLocal(queryTokens, normalizedTokenSetLocal(lensChatFirstNonEmptyString(hit.Title, asStringAny(hit.StructData["title"]))))
		score += 2.0 * tokenOverlapScoreLocal(queryTokens, pathTokens)
		score += 1.5 * tokenOverlapScoreLocal(queryTokens, normalizedTokenSetLocal(strings.Join(stringSliceAny(hit.StructData["questionHints"]), " ")))
		score += 2.5 * tokenOverlapScoreLocal(scopeRefSet, pathTokens)
		if preferredCorpusID != "" && strings.TrimSpace(asStringAny(hit.StructData["corpusId"])) == preferredCorpusID {
			score += 18
		}
		if currentSignalQuery {
			eventCoreOverlap := tokenOverlapScoreLocal(signalContext.eventCoreTokens, pathTokens)
			haloOverlap := tokenOverlapScoreLocal(signalContext.haloTokens, pathTokens)
			score += lensChatSignalRecencyScore(hit.StructData["updatedAt"])
			score += 5.5 * eventCoreOverlap
			score += 3.25 * haloOverlap
			switch strings.ToLower(strings.TrimSpace(hit.Kind)) {
			case "signal_landing_summary":
				score += 42
			case "analytical_aggregate", "analytical_primitive":
				score += 20
			case "overview", "dossier":
				if eventCoreOverlap == 0 && haloOverlap == 0 {
					score -= 18
				}
			}
		}
		scored = append(scored, scoredHit{hit: hit, score: score})
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].hit.ID < scored[j].hit.ID
		}
		return scored[i].score > scored[j].score
	})
	out := make([]lensChatInternalHit, 0, minIntLocal(limit, len(scored)))
	for _, item := range scored {
		out = append(out, item.hit)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func kindWeightLocal(kind string, logisticsQuery bool) float64 {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "signal_landing_summary":
		if logisticsQuery {
			return 28
		}
		return 34
	case "analytical_aggregate":
		if logisticsQuery {
			return 24
		}
		return 28
	case "analytical_primitive":
		if logisticsQuery {
			return 18
		}
		return 22
	case "corridor":
		if logisticsQuery {
			return 24
		}
		return 10
	case "route_artifact":
		if logisticsQuery {
			return 22
		}
		return 9
	case "dossier":
		return 18
	case "overview":
		return 16
	case "products":
		return 14
	case "relation":
		return 12
	case "object":
		return 11
	case "gaps":
		return 8
	default:
		return 0
	}
}

type lensChatSignalContext struct {
	eventCoreTokens map[string]struct{}
	haloTokens      map[string]struct{}
}

func buildLensChatSignalContext(hits []lensChatInternalHit) lensChatSignalContext {
	ctx := lensChatSignalContext{
		eventCoreTokens: map[string]struct{}{},
		haloTokens:      map[string]struct{}{},
	}
	for _, hit := range hits {
		if strings.TrimSpace(hit.Kind) != "signal_landing_summary" {
			continue
		}
		addNormalizedTokensLocal(ctx.eventCoreTokens, strings.Join(stringSliceAny(hit.StructData["eventCoreRefs"]), " "))
		addNormalizedTokensLocal(ctx.eventCoreTokens, strings.Join(stringSliceAny(hit.StructData["canonicalPaths"]), " "))
		addNormalizedTokensLocal(ctx.haloTokens, strings.Join(stringSliceAny(hit.StructData["haloRefs"]), " "))
	}
	return ctx
}

func lensChatSignalRecencyScore(value any) float64 {
	parsed, ok := parseLensChatRFC3339(value)
	if !ok {
		return 0
	}
	age := time.Since(parsed.UTC())
	switch {
	case age <= 2*time.Hour:
		return 26
	case age <= 24*time.Hour:
		return 18
	case age <= 7*24*time.Hour:
		return 8
	default:
		return 0
	}
}

func addNormalizedTokensLocal(dst map[string]struct{}, value string) {
	for token := range normalizedTokenSetLocal(value) {
		dst[token] = struct{}{}
	}
}

func mergeLensChatHits(base, add []lensChatInternalHit, limit int) []lensChatInternalHit {
	index := make(map[string]int, len(base))
	out := append([]lensChatInternalHit(nil), base...)
	for i, hit := range out {
		index[lensChatHitKey(hit)] = i
	}
	for _, hit := range add {
		key := lensChatHitKey(hit)
		if pos, ok := index[key]; ok {
			if hit.Score > out[pos].Score {
				out[pos] = hit
			}
			continue
		}
		index[key] = len(out)
		out = append(out, hit)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].ID < out[j].ID
		}
		return out[i].Score > out[j].Score
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func lensChatHitKey(hit lensChatInternalHit) string {
	return lensChatFirstNonEmptyString(strings.TrimSpace(hit.ID), strings.TrimSpace(hit.URI), strings.TrimSpace(hit.Title))
}

func lensChatStringPtr(value string) *string {
	return &value
}

func lensChatNonEmptyPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func asStringPointerLocal(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func lensChatCompactText(value string, limit int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" || limit <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit])) + "…"
}

func cloneStringAnyMap(in map[string]interface{}) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func asStringAny(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		if value == nil {
			return ""
		}
		return fmt.Sprintf("%v", value)
	}
}

func lensChatFirstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func stringSliceAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s := strings.TrimSpace(asStringAny(item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func uniqueStringsLocal(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func limitStringsLocal(values []string, limit int) []string {
	values = uniqueStringsLocal(values)
	if limit <= 0 || len(values) <= limit {
		return values
	}
	return values[:limit]
}

func filterCanonicalPathsLocal(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(strings.ToLower(value))
		if strings.HasPrefix(value, "/") {
			out = append(out, value)
		}
	}
	return uniqueStringsLocal(out)
}

func extractCanonicalPathsFromText(value string) []string {
	return uniqueStringsLocal(canonicalPathPattern.FindAllString(strings.ToLower(value), -1))
}

func textTokensLocal(value string) []string {
	replacer := strings.NewReplacer(",", " ", ".", " ", ":", " ", ";", " ", "(", " ", ")", " ", "/", " ", "-", " ", "?", " ")
	fields := strings.Fields(strings.ToLower(replacer.Replace(value)))
	return uniqueStringsLocal(fields)
}

func normalizedTokenSetLocal(value string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, token := range textTokensLocal(value) {
		if len(token) < 3 {
			continue
		}
		out[token] = struct{}{}
	}
	return out
}

func tokenOverlapScoreLocal(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	var overlap float64
	for token := range a {
		if _, ok := b[token]; ok {
			overlap++
		}
	}
	if overlap == 0 {
		return 0
	}
	denominator := float64(len(a))
	if len(b) < len(a) {
		denominator = float64(len(b))
	}
	if denominator <= 0 {
		return 0
	}
	return overlap / denominator
}

func looksLikeHashPart(value string) bool {
	if len(value) != 10 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func parseLensChatGCSURI(uri string) (bucket, object string, ok bool) {
	uri = strings.TrimSpace(uri)
	if !strings.HasPrefix(uri, "gs://") {
		return "", "", false
	}
	trimmed := strings.TrimPrefix(uri, "gs://")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", false
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), true
}

func minIntLocal(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxIntLocal(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func lensChatTruncateString(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 {
		return value
	}
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	if limit <= 3 {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + "..."
}
