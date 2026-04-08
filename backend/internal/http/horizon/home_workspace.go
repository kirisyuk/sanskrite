package horizon

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/analytical"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
)

type homeWorkspaceRunRequest struct {
	Query string `json:"query"`
}

type homeWorkspaceEntity struct {
	ID            string   `json:"id,omitempty"`
	Name          string   `json:"name"`
	CanonicalPath string   `json:"canonicalPath"`
	Type          string   `json:"type,omitempty"`
	Description   string   `json:"description,omitempty"`
	Location      string   `json:"location,omitempty"`
	Industry      string   `json:"industry,omitempty"`
	RelatedPaths  []string `json:"relatedPaths,omitempty"`
	Selected      bool     `json:"selected"`
}

type homeWorkspaceRunResponse struct {
	RunID              string                `json:"runId"`
	Query              string                `json:"query"`
	Answer             string                `json:"answer"`
	Hits               []map[string]any      `json:"hits,omitempty"`
	HighlightedPaths   []string              `json:"highlightedPaths,omitempty"`
	EventCorePaths     []string              `json:"eventCorePaths,omitempty"`
	HaloPaths          []string              `json:"haloPaths,omitempty"`
	AffectedZoneRefs   []string              `json:"affectedZoneRefs,omitempty"`
	Entities           []homeWorkspaceEntity `json:"entities,omitempty"`
	SuggestedLensName  string                `json:"suggestedLensName,omitempty"`
	SuggestedLensScope []string              `json:"suggestedLensScope,omitempty"`
	Warnings           []string              `json:"warnings,omitempty"`
}

func (h *HorizonHandlers) HomeWorkspaceRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	authInfo, err := requireAuth(r)
	if err != nil {
		_ = httputil.WriteUnauthorized(w, err.Error())
		return
	}
	if h.service == nil || h.service.AIExecutor() == nil {
		_ = httputil.WriteInternalError(w, "Horizon home workspace is not configured")
		return
	}
	var req homeWorkspaceRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		_ = httputil.WriteBadRequest(w, "INVALID_JSON", "Failed to parse body")
		return
	}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		_ = httputil.WriteBadRequest(w, "INVALID_QUERY", "query is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 55*time.Second)
	defer cancel()

	entities, scopeRefs, entityWarnings := h.resolveHomeWorkspaceEntities(ctx, query)
	answer, hits, highlightedPaths, eventCorePaths, haloPaths, affectedZoneRefs, _, warnings := h.answerHomeWorkspace(ctx, strings.TrimSpace(authInfo.TenantID), query, scopeRefs, entities)
	warnings = append(entityWarnings, warnings...)

	_ = httputil.WriteJSON(w, http.StatusOK, homeWorkspaceRunResponse{
		RunID:              homeWorkspaceRunID(query, scopeRefs),
		Query:              query,
		Answer:             answer,
		Hits:               summarizeLensChatHits(hits, defaultLensChatHitLimit),
		HighlightedPaths:   highlightedPaths,
		EventCorePaths:     eventCorePaths,
		HaloPaths:          haloPaths,
		AffectedZoneRefs:   affectedZoneRefs,
		Entities:           entities,
		SuggestedLensName:  buildHomeWorkspaceLensName(query, entities),
		SuggestedLensScope: buildHomeWorkspaceLensScope(scopeRefs, highlightedPaths, eventCorePaths, haloPaths),
		Warnings:           uniqueStringsLocal(warnings),
	})
}

func (h *HorizonHandlers) answerHomeWorkspace(ctx context.Context, tenantID, query string, scopeRefs []string, entities []homeWorkspaceEntity) (string, []lensChatInternalHit, []string, []string, []string, []string, string, []string) {
	warnings := make([]string, 0, 6)
	intent := classifyLensChatAnalyticalIntent(query)
	client := h.service.VertexSearchClient()
	if client == nil {
		return "Home workspace search is unavailable right now.", nil, nil, nil, nil, nil, "", []string{"vertex search client is not configured"}
	}

	retrievalCtx, cancelRetrieval := context.WithTimeout(ctx, 32*time.Second)
	defer cancelRetrieval()

	signalHits, signalDocs, signalEventCore, signalHalo, affectedZones, signalWarnings := h.loadHomeWorkspaceSignalEvidence(retrievalCtx, tenantID, query, scopeRefs)
	warnings = append(warnings, signalWarnings...)

	analyticalHits, analyticalDocs, analyticalPaths, analyticalWarnings := h.loadHomeWorkspaceAnalyticalEvidence(retrievalCtx, tenantID, query, scopeRefs)
	warnings = append(warnings, analyticalWarnings...)

	relevanceContextPaths := limitStringsLocal(filterCanonicalPathsLocal(append(append([]string{}, scopeRefs...), append(signalEventCore, analyticalPaths...)...)), 48)

	searchHits, searchWarnings := searchLensChatInternalKnowledge(retrievalCtx, client, lensChatSearchSpec{
		Question:  query,
		Limit:     defaultLensChatHitLimit,
		ScopeRefs: scopeRefs,
	})
	searchHits = filterHomeWorkspaceSearchHits(query, searchHits, relevanceContextPaths, defaultLensChatHitLimit)
	warnings = append(warnings, searchWarnings...)
	searchDocs, evidenceWarnings := loadLensChatPreparedEvidenceDocs(retrievalCtx, h.gcsClient, searchHits, defaultLensChatEvidenceDocLimit)
	warnings = append(warnings, evidenceWarnings...)

	hits := mergeLensChatHits(mergeLensChatHits(nil, signalHits, 12), analyticalHits, 12)
	hits = mergeLensChatHits(hits, searchHits, defaultLensChatHitLimit+defaultLensChatGlobalHitLimit)
	evidenceDocs := mergeLensChatPreparedEvidenceDocs(signalDocs, analyticalDocs, 12)
	evidenceDocs = mergeLensChatPreparedEvidenceDocs(evidenceDocs, searchDocs, defaultLensChatEvidenceDocLimit+defaultLensChatGlobalHitLimit)

	eventCorePaths := limitStringsLocal(filterCanonicalPathsLocal(signalEventCore), 24)
	haloPaths := limitStringsLocal(filterCanonicalPathsLocal(append(signalHalo, analyticalPaths...)), 32)
	highlightedPaths := collectLensChatEvidencePaths(hits, evidenceDocs, 32)
	highlightedPaths = limitStringsLocal(filterCanonicalPathsLocal(append(scopeRefs, append(highlightedPaths, append(eventCorePaths, haloPaths...)...)...)), 40)
	affectedZoneRefs := limitStringsLocal(filterCanonicalPathsLocal(affectedZones), 12)

	if len(hits) == 0 || len(evidenceDocs) == 0 {
		answer := "I could not find enough grounded internal knowledge for this question yet."
		if intent == "current_signal" {
			answer = "I could not find enough fresh grounded signal-aware knowledge for this question yet."
		}
		return answer, hits, highlightedPaths, eventCorePaths, haloPaths, affectedZoneRefs, "", uniqueStringsLocal(warnings)
	}

	prompt := buildHomeWorkspaceSynthesisPrompt(query, entities, hits, evidenceDocs, highlightedPaths, eventCorePaths, haloPaths, affectedZoneRefs)
	synthesisCtx, cancelSynthesis := context.WithTimeout(ctx, 14*time.Second)
	defer cancelSynthesis()
	answer, modelUsed, err := h.executeFinalSynthesis(synthesisCtx, finalSynthesisSpec{
		TenantID:          strings.TrimSpace(tenantID),
		ProjectID:         "",
		CascadeID:         homeWorkspaceRunID(query, scopeRefs),
		UseCase:           "horizon.home_workspace.final_synthesis",
		Prompt:            prompt,
		SystemInstruction: homeWorkspaceSystemInstruction(),
		MaxOutputTokens:   1400,
	})
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("final synthesis failed: %v", err))
		return fallbackHomeWorkspaceAnswer(query, entities, hits, eventCorePaths, haloPaths, affectedZoneRefs, highlightedPaths), hits, highlightedPaths, eventCorePaths, haloPaths, affectedZoneRefs, "", uniqueStringsLocal(warnings)
	}
	if answer == "" {
		answer = fallbackHomeWorkspaceAnswer(query, entities, hits, eventCorePaths, haloPaths, affectedZoneRefs, highlightedPaths)
		warnings = append(warnings, "final synthesis returned empty content")
	}
	answer = rewriteCanonicalPathsForDisplay(answer, hits)
	return answer, hits, highlightedPaths, eventCorePaths, haloPaths, affectedZoneRefs, modelUsed, uniqueStringsLocal(warnings)
}

func (h *HorizonHandlers) resolveHomeWorkspaceEntities(ctx context.Context, query string) ([]homeWorkspaceEntity, []string, []string) {
	scopeRefs := limitStringsLocal(filterCanonicalPathsLocal(extractCanonicalPathsFromText(query)), 16)
	warnings := make([]string, 0, 2)
	if h == nil || h.service == nil {
		return nil, scopeRefs, warnings
	}
	fragments, err := h.service.SearchFragments(ctx, query, 8)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("entity search failed: %v", err))
		return nil, scopeRefs, warnings
	}
	out := make([]homeWorkspaceEntity, 0, len(fragments))
	seen := map[string]struct{}{}
	for _, fragment := range fragments {
		if fragment == nil {
			continue
		}
		if !homeWorkspaceEntityRelevant(query, fragment) {
			continue
		}
		canonicalPath := strings.TrimSpace(fragment.CanonicalPath)
		if canonicalPath == "" {
			continue
		}
		if _, ok := seen[canonicalPath]; ok {
			continue
		}
		seen[canonicalPath] = struct{}{}
		scopeRefs = append(scopeRefs, canonicalPath)
		out = append(out, homeWorkspaceEntity{
			ID:            strings.TrimSpace(fragment.ID),
			Name:          buildHomeWorkspaceEntityName(fragment),
			CanonicalPath: canonicalPath,
			Type:          strings.TrimSpace(string(fragment.Type)),
			Description:   strings.TrimSpace(fragment.Description),
			Location:      buildHomeWorkspaceEntityLocation(fragment),
			Industry:      buildHomeWorkspaceEntityIndustry(fragment),
			Selected:      true,
		})
	}
	return out, limitStringsLocal(filterCanonicalPathsLocal(scopeRefs), 20), warnings
}

func (h *HorizonHandlers) loadHomeWorkspaceSignalEvidence(ctx context.Context, tenantID, query string, scopeRefs []string) ([]lensChatInternalHit, []lensChatPreparedEvidenceDoc, []string, []string, []string, []string) {
	if h == nil || h.signalLandingSummariesRepo == nil {
		return nil, nil, nil, nil, nil, nil
	}
	candidates := make([]*horizonmodels.SignalLandingSummary, 0, 24)
	warnings := make([]string, 0, 4)
	if len(scopeRefs) > 0 {
		byScope, err := h.signalLandingSummariesRepo.ListByScopeRefs(ctx, tenantID, scopeRefs, 12)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("signal landing summary load failed: %v", err))
		} else {
			candidates = append(candidates, byScope...)
		}
	}
	recent, err := h.signalLandingSummariesRepo.ListRecent(ctx, tenantID, 20)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("recent signal landing load failed: %v", err))
	} else {
		candidates = append(candidates, recent...)
	}
	if tenantID != "global" {
		globalRecent, err := h.signalLandingSummariesRepo.ListRecent(ctx, "global", 20)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("recent global signal landing load failed: %v", err))
		} else {
			candidates = append(candidates, globalRecent...)
		}
	}
	summaries := homeWorkspaceRankSignalSummaries(query, scopeRefs, candidates, 6)
	queryTokens := normalizedTokenSetLocal(query)
	scopeRefSet := make(map[string]struct{}, len(scopeRefs))
	for _, ref := range scopeRefs {
		ref = strings.TrimSpace(ref)
		if ref != "" {
			scopeRefSet[ref] = struct{}{}
		}
	}
	scopeTokens := normalizedTokenSetLocal(homeWorkspaceHumanizedPathText(scopeRefs))
	seen := map[string]struct{}{}
	hits := make([]lensChatInternalHit, 0, 6)
	docs := make([]lensChatPreparedEvidenceDoc, 0, 6)
	eventCore := make([]string, 0, 24)
	halo := make([]string, 0, 24)
	affectedZones := make([]string, 0, 12)
	for _, summary := range summaries {
		if summary == nil {
			continue
		}
		key := strings.TrimSpace(summary.ID)
		if key == "" {
			key = strings.TrimSpace(summary.ScopeRef)
		}
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		hit := lensChatHitFromSignalLandingSummary(summary, 1.45)
		hit.Score = homeWorkspaceSignalSummaryScore(queryTokens, scopeTokens, scopeRefSet, summary)
		hits = append(hits, hit)
		docs = append(docs, lensChatDocFromSignalLandingSummary(summary))
		eventCore = append(eventCore, signalLandingSummaryEventCoreRefs(summary)...)
		halo = append(halo, signalLandingSummaryHaloRefs(summary)...)
		affectedZones = append(affectedZones, summary.AffectedZoneRefs...)
		if len(hits) >= 4 {
			break
		}
	}
	return hits, docs, limitStringsLocal(filterCanonicalPathsLocal(eventCore), 24), limitStringsLocal(filterCanonicalPathsLocal(halo), 24), limitStringsLocal(filterCanonicalPathsLocal(affectedZones), 12), warnings
}

func (h *HorizonHandlers) loadHomeWorkspaceAnalyticalEvidence(ctx context.Context, tenantID, query string, scopeRefs []string) ([]lensChatInternalHit, []lensChatPreparedEvidenceDoc, []string, []string) {
	if h == nil || h.analyticalPrimitivesRepo == nil || len(scopeRefs) == 0 {
		return nil, nil, nil, nil
	}
	warnings := make([]string, 0, 4)
	primitives, err := h.analyticalPrimitivesRepo.ListByImpactedRefs(ctx, tenantID, scopeRefs, 240)
	if err != nil {
		return nil, nil, nil, []string{fmt.Sprintf("analytical primitive load failed: %v", err)}
	}
	if len(primitives) == 0 {
		return nil, nil, nil, nil
	}
	hits := make([]lensChatInternalHit, 0, 8)
	docs := make([]lensChatPreparedEvidenceDoc, 0, 8)
	paths := make([]string, 0, 32)
	aggregateInput := analytical.AggregateScopeInput{
		TenantID:     tenantID,
		ScopeType:    "home_workspace",
		ScopeRef:     homeWorkspaceRunID(query, scopeRefs),
		Layer:        "baseline",
		SourceClass:  "home_workspace_query",
		SubjectRef:   query,
		RefreshClass: "on_read",
		Primitives:   primitives,
		ComputedAt:   time.Now().UTC(),
	}
	for _, aggregate := range analytical.BuildScopeAggregates(aggregateInput) {
		if aggregate == nil {
			continue
		}
		hits = append(hits, lensChatHitFromAggregate(aggregate, 1.25))
		docs = append(docs, lensChatDocFromAggregate(aggregate))
		paths = append(paths, aggregate.BasisRefs...)
		paths = append(paths, aggregate.ProductRefs...)
		paths = append(paths, aggregate.NodeRefs...)
		paths = append(paths, aggregate.RouteRefs...)
		paths = append(paths, aggregate.AffectedRefs...)
	}
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
		if len(docs) >= 8 {
			break
		}
	}
	return mergeLensChatHits(nil, hits, 10), mergeLensChatPreparedEvidenceDocs(nil, docs, 10), limitStringsLocal(filterCanonicalPathsLocal(paths), 32), warnings
}

func buildHomeWorkspaceSynthesisPrompt(query string, entities []homeWorkspaceEntity, hits []lensChatInternalHit, evidenceDocs []lensChatPreparedEvidenceDoc, highlightedPaths, eventCorePaths, haloPaths, affectedZoneRefs []string) string {
	var b strings.Builder
	b.WriteString("Home workspace question: ")
	b.WriteString(strings.TrimSpace(query))
	b.WriteString("\n\n")
	if len(entities) > 0 {
		b.WriteString("Resolved entities:\n")
		for _, entity := range entities {
			b.WriteString("- ")
			b.WriteString(entity.Name)
			if entity.Type != "" {
				b.WriteString(" (")
				b.WriteString(entity.Type)
				b.WriteString(")")
			}
			if entity.CanonicalPath != "" {
				b.WriteString(" ")
				b.WriteString(entity.CanonicalPath)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	if len(eventCorePaths) > 0 {
		b.WriteString("Event core refs:\n")
		b.WriteString(strings.Join(eventCorePaths, "\n"))
		b.WriteString("\n\n")
	}
	if len(haloPaths) > 0 {
		b.WriteString("Impact halo refs:\n")
		b.WriteString(strings.Join(limitStringsLocal(haloPaths, 16), "\n"))
		b.WriteString("\n\n")
	}
	if len(affectedZoneRefs) > 0 {
		b.WriteString("Affected zones:\n")
		b.WriteString(strings.Join(affectedZoneRefs, "\n"))
		b.WriteString("\n\n")
	}
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

func homeWorkspaceSystemInstruction() string {
	return strings.TrimSpace(`
You are Horizon Home Workspace.
Use only the provided grounded internal context. Do not use web knowledge. Do not invent missing facts.
Answer in the same language as the user's question.
Use human-readable names in prose. Do not expose canonical paths like /org/... or /fac/... unless the user explicitly asks for canonical identifiers.

Return a concise workspace-style answer:
1. Direct answer first.
2. If fresh signal landing evidence exists, answer from event-core first, then impact halo, then broader structural background.
3. Name the key objects, zones, products, routes, facilities, and chokepoints if they appear in the grounded evidence.
4. For current-event questions, prioritize what changed right now over older background.
5. End with explicit uncertainty only if the grounded context is incomplete.
`)
}

func fallbackHomeWorkspaceAnswer(query string, entities []homeWorkspaceEntity, hits []lensChatInternalHit, eventCorePaths, haloPaths, affectedZoneRefs, highlightedPaths []string) string {
	lines := make([]string, 0, 6)

	if len(eventCorePaths) > 0 {
		core := limitStringsLocal(eventCorePaths, 3)
		labels := make([]string, 0, len(core))
		for _, path := range core {
			labels = append(labels, humanizeCanonicalPath(path))
		}
		lines = append(lines, "Fresh signal is centered on "+joinHumanListLocal(labels)+".")
	}

	if len(affectedZoneRefs) > 0 {
		zones := limitStringsLocal(affectedZoneRefs, 3)
		labels := make([]string, 0, len(zones))
		for _, path := range zones {
			labels = append(labels, humanizeCanonicalPath(path))
		}
		lines = append(lines, "Affected zones include "+joinHumanListLocal(labels)+".")
	}

	if len(haloPaths) > 0 {
		halo := limitStringsLocal(haloPaths, 4)
		labels := make([]string, 0, len(halo))
		for _, path := range halo {
			labels = append(labels, humanizeCanonicalPath(path))
		}
		lines = append(lines, "The immediate impact halo touches "+joinHumanListLocal(labels)+".")
	}

	if len(entities) > 0 {
		names := make([]string, 0, minIntLocal(4, len(entities)))
		for i, entity := range entities {
			if i >= 4 {
				break
			}
			name := strings.TrimSpace(entity.Name)
			if name == "" {
				name = humanizeCanonicalPath(entity.CanonicalPath)
			}
			if name != "" {
				names = append(names, name)
			}
		}
		if len(names) > 0 {
			lines = append(lines, "Grounded objects in this workspace include "+joinHumanListLocal(names)+".")
		}
	}

	if len(lines) == 0 && len(highlightedPaths) > 0 {
		labels := make([]string, 0, minIntLocal(4, len(highlightedPaths)))
		for i, path := range highlightedPaths {
			if i >= 4 {
				break
			}
			labels = append(labels, humanizeCanonicalPath(path))
		}
		if len(labels) > 0 {
			lines = append(lines, "Grounded context was found around "+joinHumanListLocal(labels)+".")
		}
	}

	if len(lines) == 0 && len(hits) > 0 {
		top := make([]string, 0, minIntLocal(3, len(hits)))
		for i, hit := range hits {
			if i >= 3 {
				break
			}
			label := strings.TrimSpace(hit.Title)
			if label == "" {
				label = strings.TrimSpace(hit.ID)
			}
			if label != "" {
				top = append(top, label)
			}
		}
		if len(top) > 0 {
			lines = append(lines, "Grounded evidence was found around "+joinHumanListLocal(top)+".")
		}
	}

	if len(lines) == 0 {
		return "I found some grounded context for this workspace, but not enough structured evidence to produce a reliable explanation yet."
	}
	return strings.Join(lines, " ")
}

func joinHumanListLocal(items []string) string {
	clean := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		value := strings.TrimSpace(item)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		clean = append(clean, value)
	}
	switch len(clean) {
	case 0:
		return ""
	case 1:
		return clean[0]
	case 2:
		return clean[0] + " and " + clean[1]
	default:
		return strings.Join(clean[:len(clean)-1], ", ") + ", and " + clean[len(clean)-1]
	}
}

func homeWorkspaceRankSignalSummaries(query string, scopeRefs []string, summaries []*horizonmodels.SignalLandingSummary, limit int) []*horizonmodels.SignalLandingSummary {
	if len(summaries) == 0 || limit <= 0 {
		return nil
	}
	queryTokens := normalizedTokenSetLocal(query)
	scopeRefSet := make(map[string]struct{}, len(scopeRefs))
	for _, ref := range scopeRefs {
		ref = strings.TrimSpace(ref)
		if ref != "" {
			scopeRefSet[ref] = struct{}{}
		}
	}
	scopeTokens := normalizedTokenSetLocal(homeWorkspaceHumanizedPathText(scopeRefs))
	type scored struct {
		summary *horizonmodels.SignalLandingSummary
		score   float64
	}
	scoredItems := make([]scored, 0, len(summaries))
	seen := make(map[string]struct{}, len(summaries))
	for _, summary := range summaries {
		if summary == nil {
			continue
		}
		key := strings.TrimSpace(summary.ID)
		if key == "" {
			key = strings.TrimSpace(summary.ScopeRef)
		}
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		score := homeWorkspaceSignalSummaryScore(queryTokens, scopeTokens, scopeRefSet, summary)
		scoredItems = append(scoredItems, scored{summary: summary, score: score})
	}
	sort.Slice(scoredItems, func(i, j int) bool {
		if scoredItems[i].score == scoredItems[j].score {
			return scoredItems[i].summary.UpdatedAt.After(scoredItems[j].summary.UpdatedAt)
		}
		return scoredItems[i].score > scoredItems[j].score
	})
	out := make([]*horizonmodels.SignalLandingSummary, 0, minIntLocal(limit, len(scoredItems)))
	for _, item := range scoredItems {
		if item.summary == nil || item.score <= 0 {
			continue
		}
		out = append(out, item.summary)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func homeWorkspaceSignalSummaryScore(queryTokens, scopeTokens map[string]struct{}, scopeRefSet map[string]struct{}, summary *horizonmodels.SignalLandingSummary) float64 {
	if summary == nil {
		return 0
	}
	paths := uniqueStringsLocal(append(signalLandingSummaryEventCoreRefs(summary), signalLandingSummaryHaloRefs(summary)...))
	combinedParts := append([]string{}, summary.TopClaims...)
	combinedParts = append(combinedParts, summary.TopConfirmedRefs...)
	combinedParts = append(combinedParts, summary.Products...)
	combinedParts = append(combinedParts, summary.Routes...)
	combinedParts = append(combinedParts, summary.AffectedZoneRefs...)
	combinedParts = append(combinedParts, summary.Facilities...)
	combinedParts = append(combinedParts, summary.Chokepoints...)
	combinedTokens := normalizedTokenSetLocal(strings.Join(combinedParts, " "))
	pathTokens := normalizedTokenSetLocal(homeWorkspaceHumanizedPathText(paths))
	score := 6.0*tokenOverlapScoreLocal(queryTokens, combinedTokens) + lensChatSignalSummaryBoost(summary)
	score += 3.5 * tokenOverlapScoreLocal(queryTokens, pathTokens)
	score += 2.5 * tokenOverlapScoreLocal(scopeTokens, pathTokens)
	scopeMatches := 0
	for _, path := range paths {
		if _, ok := scopeRefSet[path]; ok {
			scopeMatches++
		}
	}
	score += 4.0 * float64(minIntLocal(scopeMatches, 3))
	if len(scopeRefSet) > 0 && scopeMatches == 0 && tokenOverlapCountLocal(scopeTokens, pathTokens) == 0 && tokenOverlapCountLocal(queryTokens, combinedTokens) == 0 {
		score -= 4.5
	}
	return score
}

func filterHomeWorkspaceSearchHits(query string, hits []lensChatInternalHit, contextPaths []string, limit int) []lensChatInternalHit {
	if len(hits) == 0 {
		return nil
	}
	queryTokens := normalizedTokenSetLocal(query)
	contextTokens := normalizedTokenSetLocal(homeWorkspaceHumanizedPathText(contextPaths))
	contextPathSet := make(map[string]struct{}, len(contextPaths))
	for _, path := range contextPaths {
		path = strings.TrimSpace(path)
		if path != "" {
			contextPathSet[path] = struct{}{}
		}
	}
	type scoredHit struct {
		hit   lensChatInternalHit
		score int
	}
	scored := make([]scoredHit, 0, len(hits))
	for _, hit := range hits {
		kind := strings.ToLower(strings.TrimSpace(hit.Kind))
		score := 0
		paths := filterCanonicalPathsLocal(stringSliceAny(hit.StructData["canonicalPaths"]))
		for _, path := range paths {
			if _, ok := contextPathSet[path]; ok {
				score += 4
				break
			}
		}
		pathTokens := normalizedTokenSetLocal(homeWorkspaceHumanizedPathText(paths))
		pathContextOverlap := tokenOverlapCountLocal(pathTokens, contextTokens)
		pathQueryOverlap := tokenOverlapCountLocal(pathTokens, queryTokens)
		score += minIntLocal(3, pathContextOverlap)
		score += minIntLocal(3, pathQueryOverlap)
		titleTokens := normalizedTokenSetLocal(strings.TrimSpace(hit.Title))
		titleQueryOverlap := tokenOverlapCountLocal(titleTokens, queryTokens)
		score += minIntLocal(2, titleQueryOverlap)
		hintsTokens := normalizedTokenSetLocal(strings.Join(stringSliceAny(hit.StructData["questionHints"]), " "))
		hintsQueryOverlap := tokenOverlapCountLocal(hintsTokens, queryTokens)
		score += minIntLocal(2, hintsQueryOverlap)
		if strings.Contains(strings.ToLower(strings.TrimSpace(hit.Title)), "fresh signal") {
			score += 2
		}
		switch kind {
		case "signal_landing_summary":
			if pathContextOverlap == 0 && pathQueryOverlap == 0 && titleQueryOverlap == 0 && hintsQueryOverlap == 0 {
				continue
			}
			score += 2
			if score < 5 {
				continue
			}
		case "analytical_aggregate", "analytical_primitive":
			score += 1
			if score < 4 {
				continue
			}
		}
		if score >= 3 {
			scored = append(scored, scoredHit{hit: hit, score: score})
		}
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].hit.Score > scored[j].hit.Score
		}
		return scored[i].score > scored[j].score
	})
	out := make([]lensChatInternalHit, 0, minIntLocal(limit, len(scored)))
	for _, item := range scored {
		if len(out) >= limit {
			break
		}
		out = append(out, item.hit)
	}
	return out
}

func homeWorkspaceEntityRelevant(query string, fragment *horizonmodels.HorizonFragment) bool {
	if fragment == nil {
		return false
	}
	canonicalPath := strings.TrimSpace(fragment.CanonicalPath)
	if canonicalPath == "" {
		return false
	}
	queryTokens := normalizedTokenSetLocal(query)
	if len(queryTokens) == 0 {
		return true
	}
	candidateTokens := normalizedTokenSetLocal(strings.Join([]string{
		strings.TrimSpace(buildHomeWorkspaceEntityName(fragment)),
		canonicalPath,
		strings.TrimSpace(fragment.Description),
	}, " "))
	if tokenOverlapCountLocal(candidateTokens, queryTokens) >= 2 {
		return true
	}
	typ := strings.ToUpper(strings.TrimSpace(string(fragment.Type)))
	if (typ == "AFFECTED_ZONE" || typ == "FACILITY" || typ == "GEO_REGION" || typ == "PRODUCT" || typ == "ORGANIZATION") &&
		tokenOverlapCountLocal(candidateTokens, queryTokens) >= 1 {
		return true
	}
	return false
}

func tokenOverlapCountLocal(a, b map[string]struct{}) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	count := 0
	for token := range a {
		if _, ok := b[token]; ok {
			count++
		}
	}
	return count
}

func buildHomeWorkspaceEntityName(fragment *horizonmodels.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	if fragment.Labels != nil {
		if value := strings.TrimSpace(fragment.Labels["en"]); value != "" {
			return value
		}
	}
	if fragment.Identifiers != nil {
		for _, key := range []string{"shortName", "legalName", "displayName", "name"} {
			if value := strings.TrimSpace(fragment.Identifiers[key]); value != "" {
				return value
			}
		}
	}
	return humanizeCanonicalPath(strings.TrimSpace(fragment.CanonicalPath))
}

func homeWorkspaceHumanizedPathText(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	labels := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		labels = append(labels, humanizeCanonicalPath(path))
	}
	return strings.Join(labels, " ")
}

func buildHomeWorkspaceEntityLocation(fragment *horizonmodels.HorizonFragment) string {
	if fragment == nil || fragment.HomeLocation == nil || fragment.HomeLocation.ISO3166_1 == nil {
		return ""
	}
	return strings.TrimSpace(*fragment.HomeLocation.ISO3166_1)
}

func buildHomeWorkspaceEntityIndustry(fragment *horizonmodels.HorizonFragment) string {
	if fragment == nil || len(fragment.IndustryPaths) == 0 {
		return ""
	}
	parts := strings.Split(strings.Trim(fragment.IndustryPaths[0], "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[len(parts)-1])
}

func buildHomeWorkspaceLensName(query string, entities []homeWorkspaceEntity) string {
	if len(entities) > 0 {
		if len(entities) == 1 {
			return strings.TrimSpace(entities[0].Name)
		}
		return strings.TrimSpace(entities[0].Name + " workspace")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return "Home workspace lens"
	}
	if utf8.RuneCountInString(query) > 72 {
		query = lensChatCompactText(query, 72)
	}
	return query
}

func buildHomeWorkspaceLensScope(scopeRefs, highlightedPaths, eventCorePaths, haloPaths []string) []string {
	scope := make([]string, 0, 24)
	scope = append(scope, eventCorePaths...)
	scope = append(scope, haloPaths...)
	scope = append(scope, highlightedPaths...)
	scope = append(scope, scopeRefs...)
	return limitStringsLocal(filterCanonicalPathsLocal(scope), 24)
}

func homeWorkspaceRunID(query string, scopeRefs []string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(query) + "::" + strings.Join(scopeRefs, "|")))
	return "home_" + hex.EncodeToString(sum[:])[:20]
}
