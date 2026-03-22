package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
)

type pairSignal struct {
	Type   string  `json:"type"`
	Value  string  `json:"value,omitempty"`
	Weight float64 `json:"weight"`
	Reason string  `json:"reason,omitempty"`
}

type pairCandidate struct {
	LeftIdx  int
	RightIdx int
	Score    float64
	Signals  []pairSignal
}

type pairMemberLite struct {
	ID            string   `json:"id"`
	CanonicalPath string   `json:"canonicalPath"`
	PreferredName string   `json:"preferredName"`
	PathShape     string   `json:"pathShape,omitempty"`
	PathSlug      string   `json:"pathSlug,omitempty"`
	TaxonomySig   string   `json:"taxonomySignature,omitempty"`
	WikidataID    string   `json:"wikidataId,omitempty"`
	CASNumber     string   `json:"casNumber,omitempty"`
	ChemFormula   string   `json:"chemicalFormula,omitempty"`
	Alternative   []string `json:"alternativeNames,omitempty"`
	Description   string   `json:"description,omitempty"`
	QualityScore  int      `json:"qualityScore"`
}

type pairReviewEntry struct {
	PairID           string       `json:"pairId"`
	CanonicalID      string       `json:"canonicalId"`
	CanonicalPath    string       `json:"canonicalPath"`
	CanonicalName    string       `json:"canonicalName"`
	SynonymID        string       `json:"synonymId"`
	SynonymPath      string       `json:"synonymPath"`
	SynonymName      string       `json:"synonymName"`
	Confidence       float64      `json:"confidence"`
	DecisionSource   string       `json:"decisionSource"`
	Accepted         bool         `json:"accepted"`
	Reason           string       `json:"reason,omitempty"`
	HeuristicScore   float64      `json:"heuristicScore"`
	Signals          []pairSignal `json:"signals,omitempty"`
	CanonicalQuality int          `json:"canonicalQuality"`
	SynonymQuality   int          `json:"synonymQuality"`
}

type pairReviewSummary struct {
	ProductsLoaded      int  `json:"productsLoaded"`
	CandidatesGenerated int  `json:"candidatesGenerated"`
	CandidatesReviewed  int  `json:"candidatesReviewed"`
	AcceptedPairs       int  `json:"acceptedPairs"`
	RejectedPairs       int  `json:"rejectedPairs"`
	PersistedPairs      int  `json:"persistedPairs"`
	LLMEnabled          bool `json:"llmEnabled"`
}

type pairReviewReport struct {
	GeneratedAt string            `json:"generatedAt"`
	ProjectID   string            `json:"projectId"`
	TenantID    string            `json:"tenantId"`
	Mode        string            `json:"mode"`
	Summary     pairReviewSummary `json:"summary"`
	Pairs       []pairReviewEntry `json:"pairs"`
}

type llmPairInput struct {
	PairID  string         `json:"pairId"`
	Score   float64        `json:"heuristicScore"`
	Signals []pairSignal   `json:"signals"`
	Left    pairMemberLite `json:"left"`
	Right   pairMemberLite `json:"right"`
}

type llmPairDecision struct {
	PairID      string  `json:"pairId"`
	Accepted    bool    `json:"accepted"`
	CanonicalID string  `json:"canonicalId,omitempty"`
	Confidence  float64 `json:"confidence"`
	Reason      string  `json:"reason"`
}

type llmPairBatchResponse struct {
	SchemaVersion string            `json:"schemaVersion"`
	Decisions     []llmPairDecision `json:"decisions"`
}

type pairKnowledge struct {
	exactDocIDs        map[string]struct{}
	mappedSynonymPaths map[string]string
	reviewedPairIDs    map[string]struct{}
}

func runPairsMode(ctx context.Context, db *firestore.Client, products []productRecord, aiExecutor *ai.AITaskExecutor) (pairReviewReport, error) {
	if path := strings.TrimSpace(*pairsInPath); path != "" {
		report, err := loadPairsReport(path)
		if err != nil {
			return pairReviewReport{}, err
		}
		if *persistPairs {
			persisted, err := persistAcceptedPairs(ctx, db, report.Pairs)
			if err != nil {
				return report, err
			}
			report.Summary.PersistedPairs = persisted
		}
		return report, nil
	}

	knowledge, err := loadPairKnowledge(ctx, db)
	if err != nil {
		return pairReviewReport{}, err
	}

	candidates := buildPairCandidates(products, knowledge)
	reviewed, err := reviewPairCandidates(ctx, products, candidates, aiExecutor)
	if err != nil {
		return pairReviewReport{}, err
	}

	report := pairReviewReport{
		GeneratedAt: nowRFC3339(),
		ProjectID:   strings.TrimSpace(*projectID),
		TenantID:    strings.TrimSpace(*tenantID),
		Mode:        strings.TrimSpace(*candidateMode),
		Summary: pairReviewSummary{
			ProductsLoaded:      len(products),
			CandidatesGenerated: len(candidates),
			CandidatesReviewed:  len(reviewed),
			LLMEnabled:          aiExecutor != nil && *llmEnabled,
		},
		Pairs: reviewed,
	}
	for _, pair := range reviewed {
		if pair.Accepted {
			report.Summary.AcceptedPairs++
		} else {
			report.Summary.RejectedPairs++
		}
	}

	if *persistPairs {
		persisted, err := persistAcceptedPairs(ctx, db, reviewed)
		if err != nil {
			return report, err
		}
		report.Summary.PersistedPairs = persisted
	}

	return report, nil
}

func loadPairKnowledge(ctx context.Context, db *firestore.Client) (pairKnowledge, error) {
	knowledge := pairKnowledge{
		exactDocIDs:        map[string]struct{}{},
		mappedSynonymPaths: map[string]string{},
		reviewedPairIDs:    map[string]struct{}{},
	}
	if db == nil || (!*skipKnownPairs && !*skipMappedSyns) {
		return loadReviewedPairIDs(knowledge)
	}
	if cached, ok := getCachedPairKnowledge(ctx); ok {
		return loadReviewedPairIDs(cached)
	}

	scanned, err := scanActivePairKnowledge(ctx, db)
	if err != nil {
		return knowledge, err
	}
	setCachedPairKnowledge(ctx, scanned)

	return loadReviewedPairIDs(scanned)
}

func scanActivePairKnowledge(ctx context.Context, db *firestore.Client) (pairKnowledge, error) {
	knowledge := pairKnowledge{
		exactDocIDs:        map[string]struct{}{},
		mappedSynonymPaths: map[string]string{},
		reviewedPairIDs:    map[string]struct{}{},
	}
	if db == nil {
		return knowledge, nil
	}

	iter := db.Collection(strings.TrimSpace(*pairCollection)).Documents(ctx)
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return knowledge, err
		}
		data := doc.Data()
		if strings.TrimSpace(stringValue(data["tenantId"])) != strings.TrimSpace(*tenantID) {
			continue
		}
		if !boolValue(data["active"]) {
			continue
		}
		canonicalPath := strings.TrimSpace(stringValue(data["canonicalPath"]))
		synonymPath := strings.TrimSpace(stringValue(data["synonymPath"]))
		if canonicalPath == "" || synonymPath == "" {
			continue
		}
		knowledge.exactDocIDs[synonymPairDocID(canonicalPath, synonymPath)] = struct{}{}
		knowledge.mappedSynonymPaths[synonymPath] = canonicalPath
	}
	return knowledge, nil
}

func loadReviewedPairIDs(knowledge pairKnowledge) (pairKnowledge, error) {
	paths := splitCSVPaths(strings.TrimSpace(*reviewedPairsIn))
	for _, path := range paths {
		report, err := loadPairsReport(path)
		if err != nil {
			return knowledge, err
		}
		for _, pair := range report.Pairs {
			if id := strings.TrimSpace(pair.PairID); id != "" {
				knowledge.reviewedPairIDs[id] = struct{}{}
			}
		}
	}
	return knowledge, nil
}

func buildPairCandidates(products []productRecord, knowledge pairKnowledge) []pairCandidate {
	pairMap := map[string]*pairCandidate{}
	add := func(leftIdx, rightIdx int, signal pairSignal) {
		if leftIdx == rightIdx || leftIdx < 0 || rightIdx < 0 || leftIdx >= len(products) || rightIdx >= len(products) {
			return
		}
		if leftIdx > rightIdx {
			leftIdx, rightIdx = rightIdx, leftIdx
		}
		key := fmt.Sprintf("%d:%d", leftIdx, rightIdx)
		item := pairMap[key]
		if item == nil {
			item = &pairCandidate{LeftIdx: leftIdx, RightIdx: rightIdx}
			pairMap[key] = item
		}
		item.Score += signal.Weight
		item.Signals = append(item.Signals, signal)
	}

	aliasBuckets := map[string][]int{}
	pathSlugBuckets := map[string][]int{}
	taxonomyBuckets := map[string][]int{}
	tokenPairBuckets := map[string][]int{}
	acronymBuckets := map[string][]int{}

	for idx, rec := range products {
		for _, alias := range rec.AliasKeys {
			if alias != "" {
				aliasBuckets[alias] = append(aliasBuckets[alias], idx)
				if initials := aliasInitials(alias); initials != "" && len(strings.Fields(alias)) >= 2 {
					acronymBuckets[initials] = append(acronymBuckets[initials], idx)
				}
				for _, tokenPair := range aliasTokenPairs(alias) {
					tokenPairBuckets[tokenPair] = append(tokenPairBuckets[tokenPair], idx)
				}
			}
		}
		if rec.PathSlug != "" {
			pathSlugBuckets[rec.PathSlug] = append(pathSlugBuckets[rec.PathSlug], idx)
		}
		if rec.TaxonomySlug != "" {
			taxonomyBuckets[rec.TaxonomySlug] = append(taxonomyBuckets[rec.TaxonomySlug], idx)
		}
	}

	for alias, members := range aliasBuckets {
		members = uniqueInts(members)
		if len(members) < 2 || len(members) > 40 {
			continue
		}
		forEachPair(members, func(a, b int) {
			add(a, b, pairSignal{Type: "alias_key_exact", Value: alias, Weight: 2.75, Reason: "same normalized alias key"})
		})
	}
	for slug, members := range pathSlugBuckets {
		members = uniqueInts(members)
		if len(members) < 2 || len(members) > 40 {
			continue
		}
		forEachPair(members, func(a, b int) {
			add(a, b, pairSignal{Type: "path_slug_exact", Value: slug, Weight: 1.35, Reason: "same canonical path slug"})
		})
	}
	for slug, members := range taxonomyBuckets {
		members = uniqueInts(members)
		if len(members) < 2 || len(members) > 40 {
			continue
		}
		forEachPair(members, func(a, b int) {
			add(a, b, pairSignal{Type: "taxonomy_slug_exact", Value: slug, Weight: 1.6, Reason: "same taxonomy-normalized product slug"})
		})
	}
	for tokenPair, members := range tokenPairBuckets {
		members = uniqueInts(members)
		if len(members) < 2 || len(members) > 20 {
			continue
		}
		forEachPair(members, func(a, b int) {
			add(a, b, pairSignal{Type: "token_pair", Value: tokenPair, Weight: 0.95, Reason: "share two significant tokens"})
		})
	}

	shortAliases := map[string][]int{}
	for alias, members := range aliasBuckets {
		if len(strings.Fields(alias)) == 1 && len(alias) >= 2 && len(alias) <= 8 {
			shortAliases[alias] = uniqueInts(members)
		}
	}
	for acronym, leftMembers := range shortAliases {
		rightMembers := uniqueInts(acronymBuckets[acronym])
		if len(rightMembers) == 0 {
			continue
		}
		for _, leftIdx := range leftMembers {
			for _, rightIdx := range rightMembers {
				if leftIdx == rightIdx {
					continue
				}
				add(leftIdx, rightIdx, pairSignal{Type: "acronym_expansion", Value: acronym, Weight: 1.5, Reason: "short alias matches initials of expanded name"})
			}
		}
	}

	out := make([]pairCandidate, 0, len(pairMap))
	for _, candidate := range pairMap {
		enrichPairCandidate(products, candidate)
		if candidate.Score < *pairMinScore {
			continue
		}
		if shouldSkipKnownCandidate(products, *candidate, knowledge) {
			continue
		}
		out = append(out, *candidate)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		if products[out[i].LeftIdx].QualityScore != products[out[j].LeftIdx].QualityScore {
			return products[out[i].LeftIdx].QualityScore > products[out[j].LeftIdx].QualityScore
		}
		return pairStableID(products, out[i].LeftIdx, out[i].RightIdx) < pairStableID(products, out[j].LeftIdx, out[j].RightIdx)
	})
	if *pairOffset > 0 {
		if *pairOffset >= len(out) {
			return nil
		}
		out = out[*pairOffset:]
	}
	if *pairLimit > 0 && len(out) > *pairLimit {
		out = out[:*pairLimit]
	}
	return out
}

func shouldSkipKnownCandidate(products []productRecord, candidate pairCandidate, knowledge pairKnowledge) bool {
	left := products[candidate.LeftIdx]
	right := products[candidate.RightIdx]

	if *skipKnownPairs {
		if _, ok := knowledge.exactDocIDs[synonymPairDocID(left.CanonicalPath, right.CanonicalPath)]; ok {
			return true
		}
		if _, ok := knowledge.exactDocIDs[synonymPairDocID(right.CanonicalPath, left.CanonicalPath)]; ok {
			return true
		}
	}
	if *skipMappedSyns {
		if _, ok := knowledge.mappedSynonymPaths[left.CanonicalPath]; ok {
			return true
		}
		if _, ok := knowledge.mappedSynonymPaths[right.CanonicalPath]; ok {
			return true
		}
	}
	if len(knowledge.reviewedPairIDs) > 0 {
		if _, ok := knowledge.reviewedPairIDs[pairStableID(products, candidate.LeftIdx, candidate.RightIdx)]; ok {
			return true
		}
	}
	return false
}

func enrichPairCandidate(products []productRecord, candidate *pairCandidate) {
	if candidate == nil {
		return
	}
	left := products[candidate.LeftIdx]
	right := products[candidate.RightIdx]

	bestNameScore := 0.0
	for _, leftAlias := range left.AliasKeys {
		for _, rightAlias := range right.AliasKeys {
			sim := aliasSimilarity(leftAlias, rightAlias)
			if sim > bestNameScore {
				bestNameScore = sim
			}
		}
	}
	if bestNameScore >= 0.82 {
		candidate.Score += 1.2
		candidate.Signals = append(candidate.Signals, pairSignal{Type: "name_similarity", Value: fmt.Sprintf("%.3f", bestNameScore), Weight: 1.2, Reason: "strong normalized alias similarity"})
	} else if bestNameScore >= 0.72 {
		candidate.Score += 0.65
		candidate.Signals = append(candidate.Signals, pairSignal{Type: "name_similarity", Value: fmt.Sprintf("%.3f", bestNameScore), Weight: 0.65, Reason: "moderate normalized alias similarity"})
	}

	if left.WikidataID != "" && left.WikidataID == right.WikidataID {
		candidate.Score += 3.5
		candidate.Signals = append(candidate.Signals, pairSignal{Type: "wikidata_id", Value: left.WikidataID, Weight: 3.5, Reason: "same Wikidata ID"})
	}
	if left.CASNumber != "" && left.CASNumber == right.CASNumber {
		candidate.Score += 3.0
		candidate.Signals = append(candidate.Signals, pairSignal{Type: "cas_number", Value: left.CASNumber, Weight: 3.0, Reason: "same CAS number"})
	}
	if left.ChemicalFormula != "" && left.ChemicalFormula == right.ChemicalFormula {
		candidate.Score += 0.4
		candidate.Signals = append(candidate.Signals, pairSignal{Type: "chemical_formula", Value: left.ChemicalFormula, Weight: 0.4, Reason: "same chemical formula (weak signal only)"})
	}
	if left.TaxonomySig != "" && left.TaxonomySig == right.TaxonomySig {
		candidate.Score += 0.55
		candidate.Signals = append(candidate.Signals, pairSignal{Type: "taxonomy_signature", Value: left.TaxonomySig, Weight: 0.55, Reason: "same taxonomy signature"})
	}
	if left.PathShape == right.PathShape && left.PathShape != "" {
		candidate.Score += 0.2
	}
}

func reviewPairCandidates(ctx context.Context, products []productRecord, candidates []pairCandidate, aiExecutor *ai.AITaskExecutor) ([]pairReviewEntry, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	if aiExecutor == nil || !*llmEnabled {
		out := make([]pairReviewEntry, 0, len(candidates))
		for _, candidate := range candidates {
			entry := materializePairReview(products, candidate, true, "heuristic", clamp01(candidate.Score/5.0), "LLM disabled; accepted by heuristic threshold")
			out = append(out, entry)
		}
		return out, nil
	}

	out := make([]pairReviewEntry, 0, len(candidates))
	const batchSize = 10
	for start := 0; start < len(candidates); start += batchSize {
		end := start + batchSize
		if end > len(candidates) {
			end = len(candidates)
		}
		batch := candidates[start:end]
		decisions, err := adjudicatePairBatchWithLLM(ctx, aiExecutor, products, batch)
		if err != nil {
			for _, candidate := range batch {
				out = append(out, materializePairReview(products, candidate, false, "llm_error", 0, err.Error()))
			}
			continue
		}
		decisionMap := map[string]llmPairDecision{}
		for _, decision := range decisions.Decisions {
			decisionMap[strings.TrimSpace(decision.PairID)] = decision
		}
		for _, candidate := range batch {
			pairID := pairStableID(products, candidate.LeftIdx, candidate.RightIdx)
			decision, ok := decisionMap[pairID]
			if !ok {
				out = append(out, materializePairReview(products, candidate, false, "llm_missing", 0, "no decision returned"))
				continue
			}
			out = append(out, materializePairReviewWithDecision(products, candidate, decision, "llm"))
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Accepted != out[j].Accepted {
			return out[i].Accepted && !out[j].Accepted
		}
		if out[i].Confidence != out[j].Confidence {
			return out[i].Confidence > out[j].Confidence
		}
		if out[i].HeuristicScore != out[j].HeuristicScore {
			return out[i].HeuristicScore > out[j].HeuristicScore
		}
		return out[i].PairID < out[j].PairID
	})
	return out, nil
}

func adjudicatePairBatchWithLLM(ctx context.Context, exec *ai.AITaskExecutor, products []productRecord, batch []pairCandidate) (llmPairBatchResponse, error) {
	var zero llmPairBatchResponse
	input := make([]llmPairInput, 0, len(batch))
	for _, candidate := range batch {
		left := products[candidate.LeftIdx]
		right := products[candidate.RightIdx]
		input = append(input, llmPairInput{
			PairID:  pairStableID(products, candidate.LeftIdx, candidate.RightIdx),
			Score:   candidate.Score,
			Signals: candidate.Signals,
			Left:    productToPairLite(left),
			Right:   productToPairLite(right),
		})
	}

	payload, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return zero, err
	}

	validateDecisions := func(decisions []llmPairDecision) error {
		allowed := map[string]struct{}{}
		for _, item := range input {
			allowed[item.PairID] = struct{}{}
			allowed[item.Left.ID] = struct{}{}
			allowed[item.Right.ID] = struct{}{}
		}
		if len(decisions) == 0 {
			return fmt.Errorf("at least one decision is required")
		}
		for i, decision := range decisions {
			if _, ok := allowed[strings.TrimSpace(decision.PairID)]; !ok {
				return fmt.Errorf("decisions[%d].pairId unknown", i)
			}
			if decision.Confidence < 0 || decision.Confidence > 1 {
				return fmt.Errorf("decisions[%d].confidence out of range", i)
			}
			if decision.Accepted {
				if _, ok := allowed[strings.TrimSpace(decision.CanonicalID)]; !ok {
					return fmt.Errorf("decisions[%d].canonicalId unknown", i)
				}
			}
		}
		return nil
	}
	verifier := ai.NewGenericVerifier[[]llmPairDecision](func(out []llmPairDecision) error {
		return validateDecisions(out)
	})

	prompt := buildPairReviewPrompt(string(payload))

	task := &ai.AITask{
		TaskType:     ai.TaskTypeAliasEnrichment,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			PreferredModel:           strings.TrimSpace(*llmModel),
			FallbackModels:           []string{"gemini-3-flash-preview", "gemini-2.5-flash-lite", "gemini-2.5-flash"},
			MinContextWindow:         32000,
			MaxOutputSize:            16384,
			MinCapabilityScore:       8,
			TextGenerationCapability: true,
		},
		Verifier: verifier,
	}
	parsedAny, err := exec.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return zero, err
	}
	parsed, ok := parsedAny.([]llmPairDecision)
	if !ok {
		return zero, fmt.Errorf("unexpected LLM output type %T", parsedAny)
	}
	return llmPairBatchResponse{
		SchemaVersion: "v1",
		Decisions:     parsed,
	}, nil
}

func buildPairReviewPrompt(payload string) string {
	return "You are reviewing PRODUCT synonym pairs for a supply-chain graph.\n" +
		"You need TRUE SYNONYMS of the SAME product/material only.\n" +
		"Accept only when both fragments refer to the exact same thing sold, sourced, transported, or used as the same product/material under different names.\n" +
		"Think: same thing, different label. Do NOT think: related thing, nearby thing, same family, same industry, or adjacent processing step.\n" +
		"Good accepts: sodium hydroxide = caustic soda; liquefied natural gas = LNG; liquid helium = LHe; multilayer ceramic capacitor = MLCC capacitor; jasmine rice = Hom Mali rice.\n" +
		"Synonyms include spelling variants, acronym/full-form, exact commercial aliases, duplicate canonical paths, plural/singular variants, and obviously equivalent labels.\n" +
		"Class-subclass relationships are NOT synonyms. General-specific relationships are NOT synonyms. If one side is a class and the other is a subtype, variant, grade, or subclass, reject it.\n" +
		"Reject if the pair is broader vs narrower, class vs subclass, family vs subtype, product vs service, law/compliance vs product, bundle vs part, brand/model vs category, or two products from different processing stages.\n" +
		"Bad rejects: alumina vs aluminum; bauxite vs alumina; iron ore vs iron ore concentrate; helium gas vs liquid helium; natural gas vs LNG; battery cell vs battery pack; PCB vs PCBA; steel sheet vs automotive steel sheet; pCAM vs cathode active material; semiconductor wafer vs logic wafer; tires vs automotive tires; fruit vs pineapple; vehicle vs passenger electric vehicle.\n" +
		"Different processing stages are NOT synonyms even if strongly related: ore -> concentrate, concentrate -> refined metal, gas -> liquefied gas, alumina -> aluminum, lithium carbonate -> lithium hydroxide, precursor -> active material.\n" +
		"If one side is just a worse taxonomy path, typo, awkward wording, pluralization, acronym, or alternate commercial label for the same product, accept it and pick the better canonical reference.\n" +
		"If accepted=true, canonicalId must be one of the two fragment IDs and should pick the better canonical reference.\n" +
		"Return a decision for every pair.\n" +
		"Return EXACTLY one strict JSON array. Do not wrap it in an outer object. Do not add markdown. Do not add prose.\n" +
		"Each array item must use this exact structure:\n" +
		"[\n" +
		"  {\n" +
		"    \"pairId\": \"<string pair id from input>\",\n" +
		"    \"accepted\": <true|false>,\n" +
		"    \"canonicalId\": \"<one of the two fragment ids when accepted, otherwise empty string or omitted>\",\n" +
		"    \"confidence\": <number from 0 to 1>,\n" +
		"    \"reason\": \"<short explanation>\"\n" +
		"  }\n" +
		"]\n" +
		"Include every input pair exactly once. Keep keys spelled exactly as above.\n\n" +
		"Pairs JSON:\n" + payload + "\n\n" +
		"Return strict JSON only."
}

func materializePairReview(products []productRecord, candidate pairCandidate, accepted bool, source string, confidence float64, reason string) pairReviewEntry {
	decision := llmPairDecision{
		Accepted:    accepted,
		CanonicalID: choosePairCanonical(products, candidate).ID,
		Confidence:  confidence,
		Reason:      reason,
	}
	return materializePairReviewWithDecision(products, candidate, decisionWithPairID(products, candidate, decision, source), source)
}

func materializePairReviewWithDecision(products []productRecord, candidate pairCandidate, decision llmPairDecision, source string) pairReviewEntry {
	left := products[candidate.LeftIdx]
	right := products[candidate.RightIdx]
	canonical := choosePairCanonical(products, candidate)
	synonym := right
	if decision.Accepted {
		if strings.TrimSpace(decision.CanonicalID) == left.ID {
			canonical = left
			synonym = right
		} else if strings.TrimSpace(decision.CanonicalID) == right.ID {
			canonical = right
			synonym = left
		}
	}
	if !decision.Accepted {
		canonical = choosePairCanonical(products, candidate)
		if canonical.ID == left.ID {
			synonym = right
		} else {
			synonym = left
		}
	}

	return pairReviewEntry{
		PairID:           pairStableID(products, candidate.LeftIdx, candidate.RightIdx),
		CanonicalID:      canonical.ID,
		CanonicalPath:    canonical.CanonicalPath,
		CanonicalName:    canonical.PreferredName,
		SynonymID:        synonym.ID,
		SynonymPath:      synonym.CanonicalPath,
		SynonymName:      synonym.PreferredName,
		Confidence:       decision.Confidence,
		DecisionSource:   source,
		Accepted:         decision.Accepted,
		Reason:           strings.TrimSpace(decision.Reason),
		HeuristicScore:   candidate.Score,
		Signals:          candidate.Signals,
		CanonicalQuality: canonical.QualityScore,
		SynonymQuality:   synonym.QualityScore,
	}
}

func persistAcceptedPairs(ctx context.Context, db *firestore.Client, reviewed []pairReviewEntry) (int, error) {
	col := db.Collection(strings.TrimSpace(*pairCollection))
	now := firestore.ServerTimestamp
	persisted := 0
	for _, pair := range reviewed {
		if !pair.Accepted {
			continue
		}
		docID := synonymPairDocID(pair.CanonicalPath, pair.SynonymPath)
		doc := map[string]interface{}{
			"tenantId":            strings.TrimSpace(*tenantID),
			"canonicalFragmentId": pair.CanonicalID,
			"canonicalPath":       pair.CanonicalPath,
			"canonicalName":       pair.CanonicalName,
			"synonymFragmentId":   pair.SynonymID,
			"synonymPath":         pair.SynonymPath,
			"synonymName":         pair.SynonymName,
			"normalizedSynonym":   normalizeProductAliasKey(pair.SynonymName),
			"decisionSource":      pair.DecisionSource,
			"confidence":          pair.Confidence,
			"reason":              pair.Reason,
			"active":              true,
			"heuristicScore":      pair.HeuristicScore,
			"signals":             pair.Signals,
			"updatedAt":           now,
		}
		if _, err := col.Doc(docID).Set(ctx, doc, firestore.MergeAll); err != nil {
			return persisted, err
		}
		persisted++
	}
	if persisted > 0 {
		if refreshed, err := scanActivePairKnowledge(ctx, db); err == nil {
			setCachedPairKnowledge(ctx, refreshed)
		}
	}
	return persisted, nil
}

func writePairsReport(path string, report pairReviewReport) error {
	f, err := os.Create(strings.TrimSpace(path))
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func loadPairsReport(path string) (pairReviewReport, error) {
	var report pairReviewReport
	data, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return report, err
	}
	if err := json.Unmarshal(data, &report); err != nil {
		return report, err
	}
	return report, nil
}

func printPairsSummary(report pairReviewReport) {
	fmt.Printf("\nPair Summary\n")
	fmt.Printf("  products_loaded: %d\n", report.Summary.ProductsLoaded)
	fmt.Printf("  candidates_generated: %d\n", report.Summary.CandidatesGenerated)
	fmt.Printf("  candidates_reviewed: %d\n", report.Summary.CandidatesReviewed)
	fmt.Printf("  accepted_pairs: %d\n", report.Summary.AcceptedPairs)
	fmt.Printf("  rejected_pairs: %d\n", report.Summary.RejectedPairs)
	fmt.Printf("  persisted_pairs: %d\n", report.Summary.PersistedPairs)
	fmt.Printf("  llm_enabled: %t\n", report.Summary.LLMEnabled)

	limit := 25
	if len(report.Pairs) < limit {
		limit = len(report.Pairs)
	}
	for i := 0; i < limit; i++ {
		pair := report.Pairs[i]
		status := "reject"
		if pair.Accepted {
			status = "accept"
		}
		fmt.Printf("%d. %s conf=%.3f canonical=%s synonym=%s reason=%s\n",
			i+1,
			status,
			pair.Confidence,
			pair.CanonicalPath,
			pair.SynonymPath,
			pair.Reason,
		)
	}
}

func productToPairLite(rec productRecord) pairMemberLite {
	return pairMemberLite{
		ID:            rec.ID,
		CanonicalPath: rec.CanonicalPath,
		PreferredName: rec.PreferredName,
		PathShape:     rec.PathShape,
		PathSlug:      rec.PathSlug,
		TaxonomySig:   rec.TaxonomySig,
		WikidataID:    rec.WikidataID,
		CASNumber:     rec.CASNumber,
		ChemFormula:   rec.ChemicalFormula,
		Alternative:   uniqueStrings(rec.Fragment.AlternativeNames),
		Description:   strings.TrimSpace(rec.Fragment.Description),
		QualityScore:  rec.QualityScore,
	}
}

func stringValue(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	default:
		return ""
	}
}

func boolValue(v interface{}) bool {
	switch t := v.(type) {
	case bool:
		return t
	default:
		return false
	}
}

func splitCSVPaths(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func choosePairCanonical(products []productRecord, candidate pairCandidate) productRecord {
	left := products[candidate.LeftIdx]
	right := products[candidate.RightIdx]
	if left.QualityScore != right.QualityScore {
		if left.QualityScore > right.QualityScore {
			return left
		}
		return right
	}
	if len(left.PathSlug) != len(right.PathSlug) {
		if len(left.PathSlug) > len(right.PathSlug) {
			return left
		}
		return right
	}
	if left.CanonicalPath <= right.CanonicalPath {
		return left
	}
	return right
}

func pairStableID(products []productRecord, leftIdx, rightIdx int) string {
	left := strings.TrimSpace(products[leftIdx].ID)
	right := strings.TrimSpace(products[rightIdx].ID)
	if left > right {
		left, right = right, left
	}
	raw := left + "|" + right
	sum := sha1.Sum([]byte(raw))
	return hex.EncodeToString(sum[:8])
}

func decisionWithPairID(products []productRecord, candidate pairCandidate, decision llmPairDecision, source string) llmPairDecision {
	decision.PairID = pairStableID(products, candidate.LeftIdx, candidate.RightIdx)
	if strings.TrimSpace(decision.CanonicalID) == "" {
		decision.CanonicalID = choosePairCanonical(products, candidate).ID
	}
	if decision.Confidence <= 0 {
		decision.Confidence = clamp01(candidate.Score / 5.0)
	}
	if strings.TrimSpace(decision.Reason) == "" {
		decision.Reason = source
	}
	return decision
}

func aliasTokenPairs(alias string) []string {
	tokens := significantTokens(alias)
	if len(tokens) < 2 {
		return nil
	}
	out := make([]string, 0, len(tokens))
	for i := 0; i < len(tokens); i++ {
		for j := i + 1; j < len(tokens) && j < i+4; j++ {
			out = append(out, tokens[i]+"|"+tokens[j])
		}
	}
	return uniqueStrings(out)
}

func aliasInitials(alias string) string {
	tokens := significantTokens(alias)
	if len(tokens) < 2 {
		return ""
	}
	var b strings.Builder
	for _, token := range tokens {
		b.WriteByte(token[0])
	}
	return b.String()
}

func significantTokens(alias string) []string {
	stop := map[string]struct{}{
		"and": {}, "for": {}, "with": {}, "the": {}, "into": {}, "from": {}, "grade": {}, "series": {}, "systems": {},
		"system": {}, "service": {}, "services": {}, "product": {}, "products": {}, "material": {}, "materials": {},
	}
	raw := strings.Fields(normalizeProductAliasKey(alias))
	out := make([]string, 0, len(raw))
	for _, token := range raw {
		if len(token) < 3 {
			continue
		}
		if _, ok := stop[token]; ok {
			continue
		}
		out = append(out, token)
	}
	return uniqueStrings(out)
}

func aliasSimilarity(a, b string) float64 {
	a = normalizeProductAliasKey(a)
	b = normalizeProductAliasKey(b)
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1
	}
	aTokens := strings.Fields(a)
	bTokens := strings.Fields(b)
	jaccard := tokenJaccard(aTokens, bTokens)
	containment := tokenContainment(aTokens, bTokens)
	edit := normalizedEditSimilarity(a, b)
	return math.Max(jaccard*0.45+containment*0.25+edit*0.30, containment*0.5+edit*0.5)
}

func tokenJaccard(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	setA := map[string]struct{}{}
	setB := map[string]struct{}{}
	for _, token := range a {
		setA[token] = struct{}{}
	}
	for _, token := range b {
		setB[token] = struct{}{}
	}
	inter := 0
	union := len(setA)
	for token := range setB {
		if _, ok := setA[token]; ok {
			inter++
			continue
		}
		union++
	}
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func tokenContainment(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	setA := map[string]struct{}{}
	setB := map[string]struct{}{}
	for _, token := range a {
		setA[token] = struct{}{}
	}
	for _, token := range b {
		setB[token] = struct{}{}
	}
	inter := 0
	for token := range setA {
		if _, ok := setB[token]; ok {
			inter++
		}
	}
	den := len(setA)
	if len(setB) < den {
		den = len(setB)
	}
	if den == 0 {
		return 0
	}
	return float64(inter) / float64(den)
}

func normalizedEditSimilarity(a, b string) float64 {
	if a == "" || b == "" {
		return 0
	}
	dist := levenshteinDistance(a, b)
	den := len([]rune(a))
	if len([]rune(b)) > den {
		den = len([]rune(b))
	}
	if den == 0 {
		return 0
	}
	return 1 - float64(dist)/float64(den)
}

func levenshteinDistance(a, b string) int {
	ra := []rune(a)
	rb := []rune(b)
	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		curr := make([]int, len(rb)+1)
		curr[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 0
			if ra[i-1] != rb[j-1] {
				cost = 1
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev = curr
	}
	return prev[len(rb)]
}

func min3(a, b, c int) int {
	if a <= b && a <= c {
		return a
	}
	if b <= c {
		return b
	}
	return c
}

func forEachPair(values []int, fn func(a, b int)) {
	for i := 0; i < len(values); i++ {
		for j := i + 1; j < len(values); j++ {
			fn(values[i], values[j])
		}
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func synonymPairDocID(canonicalPath, synonymPath string) string {
	raw := strings.TrimSpace(canonicalPath) + "|" + strings.TrimSpace(synonymPath)
	sum := sha1.Sum([]byte(raw))
	return hex.EncodeToString(sum[:10])
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
