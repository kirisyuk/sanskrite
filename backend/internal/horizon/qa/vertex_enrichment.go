package qa

import (
	"context"
	"log"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// extractSearchTermsFromScope builds a short query from scope paths for Vertex semantic search.
// Long questions return 0 results; short entity-focused queries (nickel Indonesia battery) work.
func extractSearchTermsFromScope(scopeFragments map[string]string) []string {
	seen := make(map[string]bool)
	var terms []string
	for path := range scopeFragments {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) == 0 {
			continue
		}
		last := strings.TrimSpace(parts[len(parts)-1])
		last = strings.ReplaceAll(last, "-", " ")
		for _, w := range strings.Fields(last) {
			w = strings.ToLower(w)
			if len(w) < 2 || seen[w] {
				continue
			}
			seen[w] = true
			terms = append(terms, w)
		}
	}
	return terms
}

// extractSearchTermsFromQuestion pulls key entity-like words (products, places) from question.
var questionStopwords = map[string]bool{
	"the": true, "a": true, "an": true, "how": true, "what": true, "would": true, "will": true,
	"could": true, "can": true, "does": true, "do": true, "affect": true, "impact": true,
	"increase": true, "decrease": true, "change": true, "cost": true, "costs": true,
	"considering": true, "assess": true, "projected": true, "estimated": true,
}

func extractSearchTermsFromQuestion(question string) []string {
	seen := make(map[string]bool)
	var terms []string
	for _, w := range strings.Fields(strings.ToLower(question)) {
		w = strings.Trim(w, ".,?!;:%")
		if len(w) < 3 || questionStopwords[w] || seen[w] {
			continue
		}
		seen[w] = true
		terms = append(terms, w)
	}
	return terms
}

// buildVertexSearchQuery merges LLM terms (if any) + scope terms + question terms into short query (max ~10 words).
func buildVertexSearchQuery(scopeFragments map[string]string, question string, llmTerms []string) string {
	seen := make(map[string]bool)
	var parts []string
	for _, t := range llmTerms {
		t = strings.TrimSpace(strings.ToLower(t))
		if t != "" && !seen[t] && len(parts) < 8 {
			seen[t] = true
			parts = append(parts, t)
		}
	}
	for _, t := range extractSearchTermsFromScope(scopeFragments) {
		if !seen[t] && len(parts) < 8 {
			seen[t] = true
			parts = append(parts, t)
		}
	}
	for _, t := range extractSearchTermsFromQuestion(question) {
		if !seen[t] && len(parts) < 8 {
			seen[t] = true
			parts = append(parts, t)
		}
	}
	if len(parts) == 0 {
		q := strings.TrimSpace(question)
		if len(q) > 80 {
			q = q[:80]
		}
		if q == "" {
			return ""
		}
		return q + " alternative supplier"
	}
	return strings.Join(parts, " ") + " alternative supplier"
}

// searchTermsExtractionResult is the LLM response for extractSearchTermsViaLLM.
type searchTermsExtractionResult struct {
	SearchTerms []string `json:"searchTerms"`
}

// extractSearchTermsViaLLM uses LLM to extract key search terms from the question (products, places, entities).
// Complements algorithmic extraction; returns nil on error (caller falls back to algorithmic only).
func extractSearchTermsViaLLM(ctx context.Context, executor *ai.AITaskExecutor, question string, scopePaths []string) []string {
	if executor == nil || strings.TrimSpace(question) == "" {
		return nil
	}
	scopeHint := ""
	if len(scopePaths) > 0 {
		scopeHint = "\nScope context (canonical paths): " + strings.Join(scopePaths[:minInt(10, len(scopePaths))], ", ")
	}
	prompt := `Extract 3–8 key search terms from the user question for semantic search. Focus on: product names, materials, countries, companies, commodities.
Return ONLY valid JSON: {"searchTerms": ["term1", "term2", ...]}
Question: ` + strings.TrimSpace(question) + scopeHint + `

JSON:`
	task := &ai.AITask{
		TaskType:     ai.TaskTypeEntityExtraction,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MaxOutputSize:            500,
		},
		Verifier: ai.NewGenericVerifier[searchTermsExtractionResult](func(r searchTermsExtractionResult) error {
			return nil
		}),
	}
	parsed, err := executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		log.Printf("[QA] LLM search term extraction failed (non-fatal): %v", err)
		return nil
	}
	res, ok := parsed.(searchTermsExtractionResult)
	if !ok {
		return nil
	}
	return res.SearchTerms
}

// EnrichVertexAlternatives finds alternative entities via Vertex AI Search. Used by Engine and demo mode.
// Uses short entity-focused query (from scope + question) — long questions return 0 results.
// If executor is provided, LLM-based term extraction complements algorithmic extraction.
func EnrichVertexAlternatives(ctx context.Context, vertexClient *ai.VertexAISearchClient, fragmentsRepo *horizonrepo.HorizonFragmentsRepository, question string, scopeFragments map[string]string, limit int, executor *ai.AITaskExecutor) ([]*horizonmodels.HorizonFragment, error) {
	if vertexClient == nil || !vertexClient.IsConfigured() || fragmentsRepo == nil {
		return nil, nil
	}
	var llmTerms []string
	if executor != nil {
		scopePaths := make([]string, 0, len(scopeFragments))
		for p := range scopeFragments {
			if p != "" {
				scopePaths = append(scopePaths, p)
			}
		}
		llmTerms = extractSearchTermsViaLLM(ctx, executor, strings.TrimSpace(question), scopePaths)
	}
	searchQuery := buildVertexSearchQuery(scopeFragments, strings.TrimSpace(question), llmTerms)
	if searchQuery == "" {
		return nil, nil
	}
	log.Printf("[QA] Vertex search query (extracted): %s", searchQuery)
	resp, err := vertexClient.Search(ctx, &ai.SearchRequest{
		Query:    searchQuery,
		PageSize: limit * 2,
	})
	if err != nil {
		return nil, err
	}
	scopeIDs := make(map[string]bool)
	for _, id := range scopeFragments {
		scopeIDs[id] = true
	}
	var out []*horizonmodels.HorizonFragment
	seen := make(map[string]bool)
	for _, r := range resp.Results {
		id := strings.TrimSpace(r.Document.ID)
		id = strings.TrimSuffix(id, ".json")
		if id == "" || seen[id] || scopeIDs[id] {
			continue
		}
		seen[id] = true
		docType := ""
		if r.Document.StructData != nil {
			if t, _ := r.Document.StructData["type"].(string); t != "" {
				docType = t
			} else if k, _ := r.Document.StructData["kind"].(string); k != "" {
				docType = k
			}
		}
		frag, err := fragmentsRepo.Get(ctx, id)
		if err != nil {
			log.Printf("[QA] Vertex result id=%s type=%s: fragmentsRepo.Get failed (not horizon_fragment?): %v", id, docType, err)
			continue
		}
		if frag != nil && frag.CanonicalPath != "" {
			if scopeFragments[frag.CanonicalPath] != "" {
				continue
			}
			out = append(out, frag)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

// ExtendGraphWithAlternatives loads edges for alternatives, fetches connected fragments, and returns extended fragment list.
// Used so propagation can traverse through alternatives and we have metadata (name, path) for NodeImpacts.
func ExtendGraphWithAlternatives(
	ctx context.Context,
	edgesRepo *horizonrepo.DependencyEdgesRepository,
	fragmentsRepo *horizonrepo.HorizonFragmentsRepository,
	alternatives []*horizonmodels.HorizonFragment,
	scopeFragments map[string]string,
	fragments []*horizonmodels.HorizonFragment,
) []*horizonmodels.HorizonFragment {
	if edgesRepo == nil || fragmentsRepo == nil || len(alternatives) == 0 {
		return fragments
	}
	scopeIDs := make(map[string]bool)
	for _, id := range scopeFragments {
		scopeIDs[id] = true
	}
	seen := make(map[string]bool)
	for _, f := range fragments {
		if f != nil && f.ID != "" {
			seen[f.ID] = true
		}
	}
	connectedIDs := make(map[string]bool)
	for _, alt := range alternatives {
		if alt == nil || alt.ID == "" {
			continue
		}
		connectedIDs[alt.ID] = true
		out, _ := edgesRepo.ListBySource(ctx, "global", alt.ID, nil, 50)
		for _, e := range out {
			if e != nil && e.TargetFragmentID != "" {
				connectedIDs[e.TargetFragmentID] = true
			}
		}
		in, _ := edgesRepo.ListByTarget(ctx, "global", alt.ID, nil, 50)
		for _, e := range in {
			if e != nil && e.SourceFragmentID != "" {
				connectedIDs[e.SourceFragmentID] = true
			}
		}
	}
	var extra []*horizonmodels.HorizonFragment
	for id := range connectedIDs {
		if seen[id] {
			continue
		}
		frag, err := fragmentsRepo.Get(ctx, id)
		if err != nil || frag == nil {
			continue
		}
		seen[id] = true
		extra = append(extra, frag)
	}
	if len(extra) == 0 {
		return fragments
	}
	result := make([]*horizonmodels.HorizonFragment, 0, len(fragments)+len(extra))
	result = append(result, fragments...)
	result = append(result, extra...)
	return result
}
