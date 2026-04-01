package signals

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/geoambiguity"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	signalsys "github.com/igorkirisyuk/sanskrite/internal/horizon/signal"
)

// TimeDecayHalflifeDays — новости старше ~2 лет дают decay ≈ 0.002 (0.5^8 при halflife 90 дней)
const TimeDecayHalflifeDays = 90.0

const (
	// MaxLocationSeedCandidates bounds fallback candidates derived from LocationPath
	// to avoid extremely expensive applicability checks on large geographies.
	MaxLocationSeedCandidates = 60
)

var ErrNoSeedNodes = errors.New("no seed nodes found for signal")

// SeedInjector injects signal metrics into seed nodes.
type SeedInjector struct {
	fragmentsRepo    *repo.HorizonFragmentsRepository
	edgesRepo        *repo.DependencyEdgesRepository
	applicability    ApplicabilityEvaluator
	vertexSearch     *ai.VertexAISearchClient
	materializedRepo *repo.MaterializedArtifactsRepository
}

// NewSeedInjector creates a new SeedInjector.
func NewSeedInjector(fragmentsRepo *repo.HorizonFragmentsRepository, edgesRepo *repo.DependencyEdgesRepository) *SeedInjector {
	return &SeedInjector{
		fragmentsRepo: fragmentsRepo,
		edgesRepo:     edgesRepo,
	}
}

func (s *SeedInjector) WithApplicabilityEvaluator(evaluator ApplicabilityEvaluator) *SeedInjector {
	s.applicability = evaluator
	return s
}

// WithMaterializedRepo sets the materialized artifacts repository for supply chain attribution.
func (s *SeedInjector) WithMaterializedRepo(r *repo.MaterializedArtifactsRepository) *SeedInjector {
	s.materializedRepo = r
	return s
}

// WithVertexSearch enables semantic resolution of ontology paths to graph fragments.
// Vertex AI Search is the primary tool for ontology→graph mapping; exact path match is a fast path when the path exists in the graph.
// Client should target the horizon fragments Data Store (VERTEX_SEARCH_HORIZON_DATA_STORE_ID).
func (s *SeedInjector) WithVertexSearch(client *ai.VertexAISearchClient) *SeedInjector {
	s.vertexSearch = client
	return s
}

// ResolvePath resolves a canonical path to a fragment ID.
// Tries exact path match first (fast path); then Vertex AI Search semantic search when configured.
// searchName: human-readable name for semantic search (e.g. "Microsoft", "ChatGPT"); if empty, derives from path.
// Returns "" if not found.
func (s *SeedInjector) ResolvePath(ctx context.Context, path string, searchName string) string {
	path = strings.TrimSpace(path)
	if path != "" {
		frag, err := s.fragmentsRepo.GetByCanonicalPath(ctx, path)
		if err == nil && frag != nil {
			return frag.ID
		}
		if fragID := s.resolveCountryLocationPath(ctx, path); fragID != "" {
			return fragID
		}
		if fragID := s.ensureLocationSeedFragment(ctx, path); fragID != "" {
			return fragID
		}
	}
	if s.vertexSearch != nil && s.vertexSearch.IsConfigured() {
		q := strings.TrimSpace(searchName)
		if q == "" {
			q = pathToSearchQuery(path)
		}
		return s.resolveByVertexSearch(ctx, path, q)
	}
	return ""
}

func (s *SeedInjector) resolveCountryLocationPath(ctx context.Context, path string) string {
	if s == nil || s.fragmentsRepo == nil {
		return ""
	}
	country := countryFromPath(path)
	if country == "" || !strings.HasPrefix(strings.TrimSpace(path), "/loc/") {
		return ""
	}
	items, err := s.fragmentsRepo.ListByCanonicalPathPrefix(ctx, "/loc/"+country+"/", 8)
	if err != nil || len(items) == 0 {
		return ""
	}
	for _, frag := range items {
		if frag == nil || strings.TrimSpace(frag.ID) == "" {
			continue
		}
		if frag.Type == horizonmodels.FragmentTypeGeoRegion {
			return strings.TrimSpace(frag.ID)
		}
	}
	return ""
}

// ResolveSeedFragmentIDs resolves signal seeds without mutating fragment metrics.
// It mirrors the resolution logic used by Inject and may populate signal.ChainRefs
// as a side effect when supply-chain attribution is available.
func (s *SeedInjector) ResolveSeedFragmentIDs(ctx context.Context, signal *horizonmodels.SignalDescriptor) ([]string, error) {
	if signal == nil {
		return nil, fmt.Errorf("signal is nil")
	}

	if err := signal.Validate(); err != nil {
		return nil, fmt.Errorf("invalid signal: %w", err)
	}

	pathIDs := s.resolveSeedPaths(ctx, signal)
	semanticIDs := s.resolveSeedNames(ctx, signal)
	locationIDs := s.resolveLocationFallback(ctx, signal)
	if len(pathIDs)+len(semanticIDs) > 0 {
		locationIDs = trimSeedIDs(locationIDs, locationFallbackAugmentCapForSignal(signal))
	}
	seedFragmentIDs := mergeSeedFragmentIDs(
		resolveSeedCapForSignal(signal),
		pathIDs,
		semanticIDs,
		locationIDs,
	)

	if len(seedFragmentIDs) == 0 {
		log.Printf("%s no seed nodes found (signalId=%s axis=%s polarity=%s location=%s seeds=%d)",
			signalsys.LogSeed,
			strings.TrimSpace(signal.ID),
			strings.TrimSpace(signal.Axis),
			strings.TrimSpace(signal.Polarity),
			strings.TrimSpace(signal.LocationPath),
			len(signal.SeedPaths),
		)
		return nil, ErrNoSeedNodes
	}

	s.attachChainRefs(ctx, signal)
	return seedFragmentIDs, nil
}

// Inject injects signal metrics into seed nodes.
// Returns list of fragment IDs that were updated.
func (s *SeedInjector) Inject(ctx context.Context, signal *horizonmodels.SignalDescriptor) ([]string, error) {
	seedFragmentIDs, err := s.ResolveSeedFragmentIDs(ctx, signal)
	if err != nil {
		return nil, err
	}

	// Step 3: Update metrics on seed nodes
	updatedIDs := make([]string, 0)
	metricPath := signal.MetricPath()
	now := time.Now().UTC()

	for _, fragID := range seedFragmentIDs {
		frag, err := s.fragmentsRepo.Get(ctx, fragID)
		if err != nil {
			continue
		}
		penalty := 1.0
		sparsePenalty := false
		if s.applicability != nil {
			decision := s.applicability.Evaluate(ctx, signal, frag)
			if !decision.Applicable {
				log.Printf("%s skipped_not_applicable signalId=%s fragmentId=%s axis=%s score=%.3f",
					signalsys.LogSeed,
					strings.TrimSpace(signal.ID),
					strings.TrimSpace(frag.ID),
					strings.TrimSpace(signal.Axis),
					decision.Score,
				)
				continue
			}
			if decision.ConfidencePenalty > 0 && decision.ConfidencePenalty < 1 {
				penalty = decision.ConfidencePenalty
				sparsePenalty = true
			}
		}

		// Initialize State if nil
		if frag.State == nil {
			frag.State = make(map[string]horizonmodels.MetricValue)
		}

		existing, ok := frag.State[metricPath]
		if !ok {
			existing = horizonmodels.MetricValue{
				Value:      0.0,
				Confidence: 0.0,
				ObservedAt: signal.ObservedAt,
				Provenance: horizonmodels.Provenance{
					Method: "signal_injection",
				},
			}
		}

		// Time decay: новости 2 лет назад практически не влияют (half-life 90 дней)
		ageDays := now.Sub(signal.ObservedAt).Hours() / 24
		decay := 1.0
		if ageDays > 0 {
			decay = math.Pow(0.5, ageDays/TimeDecayHalflifeDays)
			if decay < 0.01 {
				decay = 0.01
			}
		}
		effectiveAmplitude := signal.Amplitude * penalty * decay
		if effectiveAmplitude < 0 {
			effectiveAmplitude = 0
		}
		effectiveConfidence := signal.Confidence * penalty * decay
		if effectiveConfidence < 0 {
			effectiveConfidence = 0
		}
		if effectiveConfidence > 1 {
			effectiveConfidence = 1
		}

		// Update metric value (max; deterministic, bounded).
		if effectiveAmplitude > existing.Value {
			existing.Value = effectiveAmplitude
		}
		if effectiveConfidence > existing.Confidence {
			existing.Confidence = effectiveConfidence
		}
		existing.ObservedAt = signal.ObservedAt

		// Update provenance: keep SourceIDs (compat) + structured top-K Contributions.
		existing.Provenance.Method = "signal_injection"
		if sparsePenalty {
			existing.Provenance.Method = "signal_injection_sparse_graph"
		}
		existing.Provenance.ObservedAt = &signal.ObservedAt
		existing.Provenance.SourceIDs = append(existing.Provenance.SourceIDs, strings.TrimSpace(signal.ID))
		if len(existing.Provenance.SourceIDs) > propagation.TopKContributions {
			existing.Provenance.SourceIDs = existing.Provenance.SourceIDs[len(existing.Provenance.SourceIDs)-propagation.TopKContributions:]
		}
		existing.Provenance.SourceRefs = append(existing.Provenance.SourceRefs, horizonmodels.SourceRef{
			SourceID:   strings.TrimSpace(signal.FactID),
			SourceType: "ontology_fact",
		})

		c := horizonmodels.Contribution{
			SignalID:      strings.TrimSpace(signal.ID),
			SourceEventID: strings.TrimSpace(signal.FactID),
			Path:          []string{strings.TrimSpace(frag.CanonicalPath)},
			Amplitude:     effectiveAmplitude,
			Hops:          0,
			ObservedAt:    signal.ObservedAt,
		}
		existing.Provenance.Contributions = append(existing.Provenance.Contributions, c)
		// Keep top-K contributions by amplitude (descending).
		sort.Slice(existing.Provenance.Contributions, func(i, j int) bool {
			return existing.Provenance.Contributions[i].Amplitude > existing.Provenance.Contributions[j].Amplitude
		})
		if len(existing.Provenance.Contributions) > propagation.TopKContributions {
			existing.Provenance.Contributions = existing.Provenance.Contributions[:propagation.TopKContributions]
		}

		frag.State[metricPath] = existing
		frag.UpdatedAt = now

		// Save fragment
		if err := s.fragmentsRepo.UpdateFast(ctx, frag); err != nil {
			continue
		}

		updatedIDs = append(updatedIDs, fragID)
	}

	log.Printf("%s injected metric=%s into %d nodes (signalId=%s axis=%s polarity=%s)",
		signalsys.LogSeed,
		strings.TrimSpace(metricPath),
		len(updatedIDs),
		strings.TrimSpace(signal.ID),
		strings.TrimSpace(signal.Axis),
		strings.TrimSpace(signal.Polarity),
	)
	return updatedIDs, nil
}

func (s *SeedInjector) resolveSeedPaths(ctx context.Context, signal *horizonmodels.SignalDescriptor) []string {
	seedFragmentIDs := make([]string, 0, len(signal.SeedPaths))
	if len(signal.SeedPaths) == 0 {
		return seedFragmentIDs
	}
	seen := make(map[string]bool)
	for i, path := range signal.SeedPaths {
		searchName := seedPathSearchName(signal, i)
		fragID := s.ResolvePath(ctx, strings.TrimSpace(path), searchName)
		if fragID != "" && !seen[fragID] {
			seen[fragID] = true
			seedFragmentIDs = append(seedFragmentIDs, fragID)
		}
	}
	return seedFragmentIDs
}

func seedPathSearchName(signal *horizonmodels.SignalDescriptor, idx int) string {
	if signal == nil || idx < 0 {
		return ""
	}
	if len(signal.SeedPaths) != len(signal.SeedNames) || idx >= len(signal.SeedNames) {
		return ""
	}
	return strings.TrimSpace(signal.SeedNames[idx])
}

func (s *SeedInjector) resolveSeedNames(ctx context.Context, signal *horizonmodels.SignalDescriptor) []string {
	if signal == nil || len(signal.SeedNames) == 0 {
		return nil
	}
	type candidate struct {
		fragID string
		score  float64
	}
	scored := make([]candidate, 0, len(signal.SeedNames))
	for _, searchName := range signal.SeedNames {
		fragID, score := s.resolveSeedName(ctx, signal, strings.TrimSpace(searchName))
		if fragID == "" {
			continue
		}
		scored = append(scored, candidate{fragID: fragID, score: score})
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].fragID < scored[j].fragID
		}
		return scored[i].score > scored[j].score
	})
	limit := semanticSeedCapForSignal(signal)
	seedFragmentIDs := make([]string, 0, minInt(limit, len(scored)))
	seen := make(map[string]bool)
	for _, cand := range scored {
		if cand.fragID == "" || seen[cand.fragID] {
			continue
		}
		seen[cand.fragID] = true
		seedFragmentIDs = append(seedFragmentIDs, cand.fragID)
		if len(seedFragmentIDs) >= limit {
			break
		}
	}
	return seedFragmentIDs
}

func semanticSeedCapForSignal(signal *horizonmodels.SignalDescriptor) int {
	if signal == nil {
		return 4
	}
	switch strings.TrimSpace(signal.Axis) {
	case horizonmodels.AxisGeoConflict:
		return 3
	case horizonmodels.AxisLogisticsCapacity, horizonmodels.AxisLogisticsLatency, horizonmodels.AxisPhysicalFlowSupply, horizonmodels.AxisPhysicalFlowDemand:
		return 4
	default:
		return 3
	}
}

func resolveSeedCapForSignal(signal *horizonmodels.SignalDescriptor) int {
	if signal == nil {
		return 12
	}
	switch strings.TrimSpace(signal.Axis) {
	case horizonmodels.AxisGeoConflict:
		return 22
	case horizonmodels.AxisLogisticsCapacity, horizonmodels.AxisLogisticsLatency, horizonmodels.AxisPhysicalFlowSupply, horizonmodels.AxisPhysicalFlowDemand:
		return 20
	default:
		return 16
	}
}

func (s *SeedInjector) resolveSeedName(ctx context.Context, signal *horizonmodels.SignalDescriptor, searchName string) (string, float64) {
	searchName = strings.TrimSpace(searchName)
	if searchName == "" {
		return "", 0
	}
	if !shouldSearchSemanticSeedQuery(signal, searchName) {
		return "", 0
	}
	if s.vertexSearch == nil || !s.vertexSearch.IsConfigured() {
		fragID := s.ResolvePath(ctx, "", searchName)
		if fragID == "" {
			return "", 0
		}
		return fragID, 0.6
	}

	req := &ai.SearchRequest{
		Query:    searchName,
		PageSize: 5,
	}
	resp, err := s.vertexSearch.Search(ctx, req)
	if err != nil || resp == nil || len(resp.Results) == 0 {
		return "", 0
	}

	const minScoreLow = 0.60
	bestID := ""
	bestScore := -1.0
	for _, r := range resp.Results {
		score := r.RelevanceScore
		if score == 0 && r.RankSignals != nil {
			score = r.RankSignals.SemanticSimilarityScore
		}
		if score < minScoreLow {
			continue
		}
		fragID := extractFragmentIDFromGCSURI(r.Document.URI)
		if fragID == "" {
			fragID = strings.TrimSuffix(strings.TrimSpace(r.Document.ID), ".json")
		}
		if fragID == "" {
			continue
		}
		frag, gerr := s.fragmentsRepo.Get(ctx, fragID)
		if gerr != nil || frag == nil || !semanticSeedFragmentAllowed(signal, frag) {
			continue
		}
		if semanticSeedCountryMismatch(searchName, frag) {
			continue
		}
		totalScore := score + seedPriorityForAxis(signal, frag) + semanticSeedLocationBonus(signal, frag) + semanticSeedQueryGeoBonus(searchName, frag)
		if totalScore > bestScore {
			bestScore = totalScore
			bestID = fragID
		}
	}
	if bestID != "" {
		log.Printf("%s vertex_semantic_seed query=%q -> fragment=%s score=%.2f",
			signalsys.LogSeed,
			searchName,
			bestID,
			bestScore,
		)
	}
	return bestID, bestScore
}

func (s *SeedInjector) resolveLocationFallback(ctx context.Context, signal *horizonmodels.SignalDescriptor) []string {
	capLimit := locationSeedCapForSignal(signal)
	seedFragmentIDs := make([]string, 0, capLimit)
	if s == nil || s.fragmentsRepo == nil || strings.TrimSpace(signal.LocationPath) == "" {
		return seedFragmentIDs
	}

	candidates := make([]*horizonmodels.HorizonFragment, 0, MaxLocationSeedCandidates)
	seen := make(map[string]bool)
	appendFragment := func(frag *horizonmodels.HorizonFragment) {
		if frag == nil || strings.TrimSpace(frag.ID) == "" || seen[frag.ID] {
			return
		}
		seen[frag.ID] = true
		candidates = append(candidates, frag)
	}

	locationFrag, err := s.fragmentsRepo.GetByCanonicalPath(ctx, signal.LocationPath)
	if err == nil && locationFrag != nil {
		appendFragment(locationFrag)
		if s.edgesRepo != nil {
			operatesInType := horizonmodels.DependencyTypeOperatesIn
			locatedInType := horizonmodels.DependencyTypeLocatedIn
			operatesInEdges, _ := s.edgesRepo.ListByTarget(ctx, "global", locationFrag.ID, &operatesInType, 100)
			locatedInEdges, _ := s.edgesRepo.ListByTarget(ctx, "global", locationFrag.ID, &locatedInType, 100)
			appendEdgeSources := func(edges []*horizonmodels.DependencyEdge) {
				for _, edge := range edges {
					if edge == nil || strings.TrimSpace(edge.SourceFragmentID) == "" {
						continue
					}
					frag, gerr := s.fragmentsRepo.Get(ctx, strings.TrimSpace(edge.SourceFragmentID))
					if gerr != nil || frag == nil {
						continue
					}
					appendFragment(frag)
				}
			}
			appendEdgeSources(operatesInEdges)
			appendEdgeSources(locatedInEdges)
		}
	}
	if len(candidates) == 0 {
		if fragID := s.ensureLocationSeedFragment(ctx, signal.LocationPath); fragID != "" {
			return []string{fragID}
		}
	}

	if len(candidates) == 0 {
		for _, frag := range s.listCountryPrefixCandidates(ctx, countryFromPath(signal.LocationPath), signal) {
			appendFragment(frag)
		}
	}

	for _, frag := range s.rankLocationFallbackCandidates(ctx, signal, candidates) {
		if len(seedFragmentIDs) >= capLimit {
			break
		}
		seedFragmentIDs = append(seedFragmentIDs, frag.ID)
	}
	if len(seedFragmentIDs) >= capLimit {
		log.Printf("%s location_seed_cap_applied signalId=%s location=%s selected=%d cap=%d",
			signalsys.LogSeed,
			strings.TrimSpace(signal.ID),
			strings.TrimSpace(signal.LocationPath),
			len(seedFragmentIDs),
			capLimit,
		)
	}
	return seedFragmentIDs
}

func (s *SeedInjector) ensureLocationSeedFragment(ctx context.Context, path string) string {
	if s == nil || s.fragmentsRepo == nil {
		return ""
	}
	path = strings.TrimSpace(path)
	if !strings.HasPrefix(path, "/loc/") {
		return ""
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	if len(parts) == 2 {
		return ""
	}
	country := strings.TrimSpace(parts[1])
	isCountryAnchor := len(country) == 2 && country != "na" && country != "xx" && country != "zz"
	isInternationalAnchor := len(parts) >= 4 && strings.EqualFold(country, "int") && strings.EqualFold(strings.TrimSpace(parts[2]), "na")
	if !isCountryAnchor && !isInternationalAnchor {
		return ""
	}
	if frag, err := s.fragmentsRepo.GetByCanonicalPath(ctx, path); err == nil && frag != nil && strings.TrimSpace(frag.ID) != "" {
		return strings.TrimSpace(frag.ID)
	}
	label := strings.ReplaceAll(strings.TrimSpace(parts[len(parts)-1]), "-", " ")
	if label == "" {
		label = strings.ToUpper(strings.TrimSpace(parts[1]))
	}
	frag := &horizonmodels.HorizonFragment{
		TenantID:      "global",
		CanonicalPath: path,
		Type:          horizonmodels.FragmentTypeGeoRegion,
		Labels:        map[string]string{"en": label},
		Identifiers: map[string]string{
			"geoKind": "country_anchor",
		},
		State: make(map[string]horizonmodels.MetricValue),
		Governance: horizonmodels.Governance{
			Owner:  "SYSTEM",
			Status: string(horizonmodels.FragmentStatusActive),
		},
		Metadata: horizonmodels.FragmentMetadata{
			CreatedAt:       time.Now().UTC(),
			LastUpdatedAt:   time.Now().UTC(),
			EpistemicStatus: horizonmodels.EpistemicStatusLowConfidence,
		},
		Provenance: horizonmodels.Provenance{
			Method: "signal_location_anchor",
		},
		Status: "active",
	}
	if isInternationalAnchor {
		frag.Identifiers["geoKind"] = "chokepoint_anchor"
	}
	id, err := s.fragmentsRepo.Create(ctx, frag)
	if err != nil {
		if existing, gerr := s.fragmentsRepo.GetByCanonicalPath(ctx, path); gerr == nil && existing != nil {
			return strings.TrimSpace(existing.ID)
		}
		return ""
	}
	return strings.TrimSpace(id)
}

func locationSeedCapForSignal(signal *horizonmodels.SignalDescriptor) int {
	if signal == nil {
		return MaxLocationSeedCandidates
	}
	switch strings.TrimSpace(signal.Axis) {
	case horizonmodels.AxisGeoConflict:
		return 18
	case horizonmodels.AxisLogisticsCapacity, horizonmodels.AxisLogisticsLatency:
		return 16
	case horizonmodels.AxisPhysicalFlowSupply, horizonmodels.AxisPhysicalFlowDemand, horizonmodels.AxisOperationalContinuity:
		return 20
	default:
		return 24
	}
}

func locationFallbackAugmentCapForSignal(signal *horizonmodels.SignalDescriptor) int {
	if signal == nil {
		return 6
	}
	switch strings.TrimSpace(signal.Axis) {
	case horizonmodels.AxisGeoConflict:
		return 6
	case horizonmodels.AxisLogisticsCapacity, horizonmodels.AxisLogisticsLatency, horizonmodels.AxisPhysicalFlowSupply, horizonmodels.AxisPhysicalFlowDemand:
		return 8
	default:
		return 6
	}
}

func (s *SeedInjector) listCountryPrefixCandidates(ctx context.Context, countryCode string, signal *horizonmodels.SignalDescriptor) []*horizonmodels.HorizonFragment {
	countryCode = strings.ToLower(strings.TrimSpace(countryCode))
	if s == nil || s.fragmentsRepo == nil || countryCode == "" {
		return nil
	}
	hints := inferSignalThemeHints(signal)
	prefixes := []string{
		"/fac/" + countryCode + "/",
		"/org/" + countryCode + "/",
		"/community/" + countryCode + "/",
		"/gov/" + countryCode + "/",
	}
	switch strings.TrimSpace(signal.Axis) {
	case horizonmodels.AxisLogisticsCapacity, horizonmodels.AxisLogisticsLatency, horizonmodels.AxisPhysicalFlowSupply, horizonmodels.AxisPhysicalFlowDemand:
		prefixes = []string{
			"/fac/" + countryCode + "/",
			"/deposit/" + countryCode + "/",
			"/market/" + countryCode + "/",
			"/org/" + countryCode + "/",
			"/gov/" + countryCode + "/",
		}
	case horizonmodels.AxisGeoConflict:
		if hints.energy || hints.logistics || hints.maritime {
			prefixes = []string{
				"/fac/" + countryCode + "/",
				"/deposit/" + countryCode + "/",
				"/market/" + countryCode + "/",
				"/org/" + countryCode + "/",
				"/community/" + countryCode + "/",
				"/gov/" + countryCode + "/",
			}
		}
	}

	seen := make(map[string]bool)
	fragments := make([]*horizonmodels.HorizonFragment, 0, 48)
	for _, prefix := range prefixes {
		items, err := s.fragmentsRepo.ListByCanonicalPathPrefix(ctx, prefix, 24)
		if err != nil {
			continue
		}
		for _, frag := range items {
			if frag == nil || strings.TrimSpace(frag.ID) == "" || seen[frag.ID] {
				continue
			}
			seen[frag.ID] = true
			fragments = append(fragments, frag)
		}
	}
	return fragments
}

func (s *SeedInjector) rankLocationFallbackCandidates(ctx context.Context, signal *horizonmodels.SignalDescriptor, candidates []*horizonmodels.HorizonFragment) []*horizonmodels.HorizonFragment {
	if len(candidates) <= 1 {
		return candidates
	}
	type scoredCandidate struct {
		frag       *horizonmodels.HorizonFragment
		score      float64
		applicable bool
	}
	scored := make([]scoredCandidate, 0, len(candidates))
	hasApplicable := false
	for _, frag := range candidates {
		if frag == nil {
			continue
		}
		score := locationFallbackPriorityForAxis(signal, frag)
		applicable := true
		if s.applicability != nil {
			decision := s.applicability.Evaluate(ctx, signal, frag)
			score += decision.Score
			applicable = decision.Applicable
			if applicable {
				hasApplicable = true
				score += 1.0
			}
		}
		scored = append(scored, scoredCandidate{frag: frag, score: score, applicable: applicable})
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].applicable != scored[j].applicable {
			return scored[i].applicable
		}
		if scored[i].score == scored[j].score {
			return strings.TrimSpace(scored[i].frag.CanonicalPath) < strings.TrimSpace(scored[j].frag.CanonicalPath)
		}
		return scored[i].score > scored[j].score
	})
	ranked := make([]*horizonmodels.HorizonFragment, 0, len(scored))
	for _, item := range scored {
		if hasApplicable && !item.applicable {
			continue
		}
		ranked = append(ranked, item.frag)
	}
	if len(ranked) == 0 {
		for _, item := range scored {
			ranked = append(ranked, item.frag)
		}
	}
	return ranked
}

func seedPriorityForAxis(signal *horizonmodels.SignalDescriptor, frag *horizonmodels.HorizonFragment) float64 {
	if signal == nil || frag == nil {
		return 0
	}
	score := 0.0
	hints := inferSignalThemeHints(signal)
	overlay := propagation.BuildPropagationNodeOverlay(frag)
	switch strings.TrimSpace(signal.Axis) {
	case horizonmodels.AxisGeoConflict:
		switch {
		case strings.HasPrefix(strings.TrimSpace(frag.CanonicalPath), "/fac/"):
			score += 0.98
		case strings.HasPrefix(strings.TrimSpace(frag.CanonicalPath), "/community/"):
			score += 0.72
		case strings.HasPrefix(strings.TrimSpace(frag.CanonicalPath), "/org/"):
			score += 0.64
		case strings.HasPrefix(strings.TrimSpace(frag.CanonicalPath), "/loc/"):
			score += 0.58
		case strings.HasPrefix(strings.TrimSpace(frag.CanonicalPath), "/gov/"):
			score += 0.28
		case strings.HasPrefix(strings.TrimSpace(frag.CanonicalPath), "/deposit/"):
			if hints.energy || hints.logistics || hints.maritime {
				score += 0.82
			} else {
				score -= 0.08
			}
		case strings.HasPrefix(strings.TrimSpace(frag.CanonicalPath), "/market/"):
			if hints.energy || hints.logistics || hints.maritime {
				score += 0.76
			} else {
				score += 0.06
			}
		}
	case horizonmodels.AxisLogisticsCapacity, horizonmodels.AxisLogisticsLatency, horizonmodels.AxisPhysicalFlowSupply, horizonmodels.AxisPhysicalFlowDemand:
		switch {
		case strings.HasPrefix(strings.TrimSpace(frag.CanonicalPath), "/fac/"):
			score += 0.95
		case strings.HasPrefix(strings.TrimSpace(frag.CanonicalPath), "/market/"):
			score += 0.85
		case strings.HasPrefix(strings.TrimSpace(frag.CanonicalPath), "/org/"):
			score += 0.60
		case strings.HasPrefix(strings.TrimSpace(frag.CanonicalPath), "/gov/"):
			score += 0.40
		}
	default:
		switch frag.Type {
		case horizonmodels.FragmentTypeFacility, horizonmodels.FragmentTypeOrganization, horizonmodels.FragmentTypeMarket, horizonmodels.FragmentTypeGeoRegion:
			score += 0.40
		}
	}
	if strings.TrimSpace(frag.CanonicalPath) == strings.TrimSpace(signal.LocationPath) {
		score += 0.25
	}
	score += overlaySeedBonus(strings.TrimSpace(signal.Axis), hints, overlay)
	if hints.defense && strings.HasPrefix(strings.TrimSpace(frag.CanonicalPath), "/fac/") {
		score += 0.08
	}
	if hints.maritime && (strings.HasPrefix(strings.TrimSpace(frag.CanonicalPath), "/fac/") || strings.HasPrefix(strings.TrimSpace(frag.CanonicalPath), "/deposit/")) {
		score += 0.05
	}
	return score
}

func locationFallbackPriorityForAxis(signal *horizonmodels.SignalDescriptor, frag *horizonmodels.HorizonFragment) float64 {
	if signal == nil || frag == nil {
		return 0
	}
	path := strings.TrimSpace(frag.CanonicalPath)
	score := 0.0
	hints := inferSignalThemeHints(signal)
	overlay := propagation.BuildPropagationNodeOverlay(frag)
	switch strings.TrimSpace(signal.Axis) {
	case horizonmodels.AxisGeoConflict:
		switch {
		case strings.HasPrefix(path, "/loc/"):
			score += 0.98
		case strings.HasPrefix(path, "/fac/"):
			score += 1.02
		case strings.HasPrefix(path, "/community/"):
			score += 0.78
		case strings.HasPrefix(path, "/org/"):
			score += 0.66
		case strings.HasPrefix(path, "/gov/"):
			score += 0.24
		case strings.HasPrefix(path, "/deposit/"):
			if hints.energy || hints.logistics || hints.maritime {
				score += 0.94
			} else {
				score -= 0.10
			}
		case strings.HasPrefix(path, "/market/"):
			if hints.energy || hints.logistics || hints.maritime {
				score += 0.88
			} else {
				score += 0.08
			}
		}
	case horizonmodels.AxisLogisticsCapacity, horizonmodels.AxisLogisticsLatency, horizonmodels.AxisPhysicalFlowSupply, horizonmodels.AxisPhysicalFlowDemand:
		switch {
		case strings.HasPrefix(path, "/fac/"):
			score += 1.00
		case strings.HasPrefix(path, "/market/"):
			score += 0.92
		case strings.HasPrefix(path, "/deposit/"):
			score += 0.86
		case strings.HasPrefix(path, "/org/"):
			score += 0.62
		case strings.HasPrefix(path, "/loc/"):
			score += 0.48
		case strings.HasPrefix(path, "/gov/"):
			score += 0.28
		}
	default:
		score += seedPriorityForAxis(signal, frag)
	}
	if strings.TrimSpace(signal.LocationPath) == path {
		score += 0.25
	}
	score += overlaySeedBonus(strings.TrimSpace(signal.Axis), hints, overlay)
	if hints.defense && strings.HasPrefix(path, "/fac/") {
		score += 0.12
	}
	if hints.defense && hasAnyThemeToken(path, "air", "naval", "military", "defense", "defence", "base", "arsenal", "shipyard") {
		score += 0.18
	}
	if hints.aviation && strings.Contains(path, "air") {
		score += 0.08
	}
	if hints.maritime && (strings.Contains(path, "port") || strings.Contains(path, "naval") || strings.Contains(path, "strait")) {
		score += 0.12
	}
	return score
}

func overlaySeedBonus(axis string, hints signalThemeHints, overlay propagation.PropagationNodeOverlay) float64 {
	score := 0.0
	switch axis {
	case horizonmodels.AxisGeoConflict:
		score += overlay.OperationalScore * 0.18
		score += overlay.BridgeScore * 0.10
		if overlay.PrimaryRole == "defense_operator" {
			score += 0.20
		}
		if overlay.PrimaryRole == "chokepoint" {
			score += 0.18
		}
		if overlay.BusinessRole == "distributor" && !(hints.logistics || hints.maritime || hints.energy) {
			score -= 0.22
		}
		if overlay.HasExposure("route_dependency") && (hints.logistics || hints.maritime) {
			score += 0.14
		}
		if overlay.HasExposure("energy_input") && hints.energy {
			score += 0.12
		}
	case horizonmodels.AxisLogisticsCapacity, horizonmodels.AxisLogisticsLatency, horizonmodels.AxisPhysicalFlowSupply, horizonmodels.AxisPhysicalFlowDemand:
		score += overlay.BridgeScore * 0.18
		score += overlay.CriticalityScore * 0.12
		if overlay.HasExposure("route_dependency") {
			score += 0.16
		}
		if overlay.HasExposure("energy_input") {
			score += 0.12
		}
		if overlay.BusinessRole == "distributor" {
			score += 0.06
		}
	default:
		score += overlay.OperationalScore * 0.08
	}
	return score
}

func shouldSearchSemanticSeedQuery(signal *horizonmodels.SignalDescriptor, searchName string) bool {
	searchName = strings.TrimSpace(searchName)
	if searchName == "" {
		return false
	}
	norm := normalizeSemanticSeedQuery(searchName)
	if norm == "" {
		return false
	}
	if semanticSeedStoplist[norm] {
		return false
	}
	if semanticSeedTooGenericServiceBranch(norm) && !semanticSeedAllowlist[norm] {
		return false
	}
	if semanticSeedCountryGovernmentQuery(norm) && !semanticSeedAllowlist[norm] {
		return false
	}
	if semanticSeedOpaqueAcronymQuery(norm) && !semanticSeedAllowlist[norm] {
		return false
	}
	if semanticSeedMediaQuery(norm) && !semanticSeedAllowlist[norm] {
		return false
	}
	if semanticSeedConjunctionNoise(norm) && !semanticSeedAllowlist[norm] {
		return false
	}
	if semanticSeedWeakPoliticalQuery(norm) && !semanticSeedAllowlist[norm] {
		return false
	}
	if semanticSeedDocumentMarkupQuery(norm) && !semanticSeedAllowlist[norm] {
		return false
	}
	if semanticSeedGenericNounPhrase(norm) && !semanticSeedAllowlist[norm] {
		return false
	}
	if signal == nil {
		return true
	}
	switch strings.TrimSpace(signal.Axis) {
	case horizonmodels.AxisGeoConflict, horizonmodels.AxisLogisticsCapacity, horizonmodels.AxisLogisticsLatency, horizonmodels.AxisPhysicalFlowSupply, horizonmodels.AxisPhysicalFlowDemand, horizonmodels.AxisOperationalContinuity:
		if semanticSeedWeakPoliticalQuery(norm) && !semanticSeedOperationalActor(norm) {
			return false
		}
	}
	return true
}

func normalizeSemanticSeedQuery(v string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(v))), " ")
}

var semanticSeedStoplist = map[string]bool{
	"plants and production":  true,
	"plant and production":   true,
	"production and plants":  true,
	"president donald trump": true,
	"donald trump":           true,
	"trump administration":   true,
	"the white house":        true,
	"white house":            true,
	"the associated press":   true,
	"associated press":       true,
	"american and israeli":   true,
	"patriot and thaad":      true,
	"the osmp":               true,
}

var semanticSeedAllowlist = map[string]bool{
	"islamic revolutionary guard corps":   true,
	"israel defense forces":               true,
	"united states department of defense": true,
	"royal air force":                     true,
	"houthis":                             true,
	"hamas":                               true,
	"hezbollah":                           true,
	"qatarenergy":                         true,
	"gazprom":                             true,
}

var semanticSeedCountryAliases = map[string]string{
	"algeria":              "dz",
	"argentina":            "ar",
	"australia":            "au",
	"brazil":               "br",
	"canada":               "ca",
	"egypt":                "eg",
	"france":               "fr",
	"germany":              "de",
	"greece":               "gr",
	"indonesia":            "id",
	"iran":                 "ir",
	"iraq":                 "iq",
	"israel":               "il",
	"italy":                "it",
	"japan":                "jp",
	"qatar":                "qa",
	"kazakhstan":           "kz",
	"kuwait":               "kw",
	"malaysia":             "my",
	"mexico":               "mx",
	"morocco":              "ma",
	"oman":                 "om",
	"panama":               "pa",
	"philippines":          "ph",
	"poland":               "pl",
	"saudi arabia":         "sa",
	"south korea":          "kr",
	"korea":                "kr",
	"spain":                "es",
	"taiwan":               "tw",
	"thailand":             "th",
	"united arab emirates": "ae",
	"uae":                  "ae",
	"united kingdom":       "gb",
	"britain":              "gb",
	"uk":                   "gb",
	"turkey":               "tr",
	"turkiye":              "tr",
	"russia":               "ru",
	"ukraine":              "ua",
	"china":                "cn",
	"india":                "in",
	"pakistan":             "pk",
	"singapore":            "sg",
	"jordan":               "jo",
	"georgia":              "ge",
	"chad":                 "td",
	"united states":        "us",
	"usa":                  "us",
	"vietnam":              "vn",
}

func semanticSeedWeakPoliticalQuery(norm string) bool {
	return strings.Contains(norm, "president ") ||
		strings.Contains(norm, "prime minister") ||
		strings.Contains(norm, "administration") ||
		norm == "white house" ||
		norm == "the white house" ||
		norm == "united states government"
}

func semanticSeedOperationalActor(norm string) bool {
	return strings.Contains(norm, "defense") ||
		strings.Contains(norm, "forces") ||
		strings.Contains(norm, "guard") ||
		strings.Contains(norm, "corps") ||
		strings.Contains(norm, "air force") ||
		strings.Contains(norm, "navy") ||
		strings.Contains(norm, "army")
}

func semanticSeedGenericNounPhrase(norm string) bool {
	return strings.Contains(norm, "production") ||
		strings.Contains(norm, "strategy") ||
		strings.Contains(norm, "response to threats") ||
		strings.Contains(norm, "calls for") ||
		strings.Contains(norm, "by the numbers") ||
		strings.Contains(norm, "appears with mysterious") ||
		strings.Contains(norm, "let s talk about")
}

func semanticSeedDocumentMarkupQuery(norm string) bool {
	if norm == "" {
		return true
	}
	for _, phrase := range []string{
		"top secret",
		"secret noforn",
		"hand distribution only",
		"distribution only",
		"printed by",
		"classified by",
		"declassify on",
		"codex level",
		"for official use only",
		"controlled unclassified information",
	} {
		if strings.Contains(norm, phrase) {
			return true
		}
	}
	words := strings.Fields(norm)
	if len(words) < 2 {
		return false
	}
	alphaWords := 0
	for _, word := range words {
		if word == strings.ToUpper(word) && strings.ToLower(word) != word && len(word) >= 2 {
			alphaWords++
		}
	}
	return alphaWords == len(words)
}

func semanticSeedMediaQuery(norm string) bool {
	return strings.Contains(norm, "associated press") ||
		strings.Contains(norm, "reuters") ||
		strings.Contains(norm, "bloomberg") ||
		strings.Contains(norm, "news agency") ||
		strings.Contains(norm, "press")
}

func semanticSeedConjunctionNoise(norm string) bool {
	if !strings.Contains(norm, " and ") && !strings.Contains(norm, " & ") {
		return false
	}
	if semanticSeedOperationalActor(norm) {
		return false
	}
	if strings.Contains(norm, "strait") || strings.Contains(norm, "canal") || strings.Contains(norm, "gulf") || strings.Contains(norm, "sea") {
		return false
	}
	if semanticSeedNationalityPair(norm) {
		return true
	}
	parts := strings.FieldsFunc(norm, func(r rune) bool {
		return r == '&'
	})
	if len(parts) == 1 {
		parts = strings.Split(norm, " and ")
	}
	meaningful := 0
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if semanticSeedAllowlist[part] {
			return false
		}
		if strings.Contains(part, "department") || strings.Contains(part, "ministry") || strings.Contains(part, "forces") ||
			strings.Contains(part, "guard") || strings.Contains(part, "corps") || strings.Contains(part, "command") ||
			strings.Contains(part, "port") || strings.Contains(part, "refinery") || strings.Contains(part, "pipeline") ||
			strings.Contains(part, "terminal") || strings.Contains(part, "energy") {
			meaningful++
			continue
		}
		if len(strings.Fields(part)) >= 2 {
			meaningful++
		}
	}
	return meaningful < 2
}

func semanticSeedNationalityPair(norm string) bool {
	nationalities := map[string]bool{
		"american": true, "israeli": true, "british": true, "iranian": true, "russian": true,
		"ukrainian": true, "qatari": true, "saudi": true, "emirati": true, "turkish": true,
		"chinese": true, "indian": true, "pakistani": true, "french": true, "german": true,
	}
	parts := strings.Split(norm, " and ")
	if len(parts) != 2 {
		return false
	}
	left := strings.TrimSpace(parts[0])
	right := strings.TrimSpace(parts[1])
	return nationalities[left] && nationalities[right]
}

func semanticSeedTooGenericServiceBranch(norm string) bool {
	switch norm {
	case "air force", "navy", "army", "task force", "joint interagency task force":
		return true
	default:
		return false
	}
}

func semanticSeedCountryGovernmentQuery(norm string) bool {
	if strings.HasPrefix(norm, "government of ") {
		return true
	}
	return strings.HasSuffix(norm, " government") && len(strings.Fields(norm)) <= 3
}

func semanticSeedOpaqueAcronymQuery(norm string) bool {
	if !strings.HasPrefix(norm, "the ") {
		return false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(norm, "the "))
	if rest == "" || len(rest) > 8 {
		return false
	}
	for _, r := range rest {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return len(strings.Fields(rest)) == 1
}

func seedFragmentTypeAllowed(signal *horizonmodels.SignalDescriptor, frag *horizonmodels.HorizonFragment) bool {
	if signal == nil || frag == nil {
		return false
	}
	hints := inferSignalThemeHints(signal)
	switch strings.TrimSpace(signal.Axis) {
	case horizonmodels.AxisGeoConflict:
		switch frag.Type {
		case horizonmodels.FragmentTypeGeoRegion, horizonmodels.FragmentTypeOrganization, horizonmodels.FragmentTypeFacility, horizonmodels.FragmentTypeCommunity:
			return true
		case horizonmodels.FragmentTypeMarket, horizonmodels.FragmentTypeDeposit:
			return hints.energy || hints.logistics || hints.maritime
		default:
			return false
		}
	case horizonmodels.AxisLogisticsCapacity, horizonmodels.AxisLogisticsLatency, horizonmodels.AxisPhysicalFlowSupply, horizonmodels.AxisPhysicalFlowDemand:
		switch frag.Type {
		case horizonmodels.FragmentTypeFacility, horizonmodels.FragmentTypeMarket, horizonmodels.FragmentTypeOrganization, horizonmodels.FragmentTypeGeoRegion, horizonmodels.FragmentTypeDeposit, horizonmodels.FragmentTypeProgram:
			return true
		default:
			return false
		}
	default:
		switch frag.Type {
		case horizonmodels.FragmentTypePerson, horizonmodels.FragmentTypeEvent, horizonmodels.FragmentTypeMediaChannel, horizonmodels.FragmentTypeSupplyLink:
			return false
		default:
			return true
		}
	}
}

func semanticSeedFragmentAllowed(signal *horizonmodels.SignalDescriptor, frag *horizonmodels.HorizonFragment) bool {
	if !seedFragmentTypeAllowed(signal, frag) {
		return false
	}
	if frag == nil {
		return false
	}
	path := strings.TrimSpace(frag.CanonicalPath)
	if path == "" {
		return false
	}
	if frag.Type == horizonmodels.FragmentTypeSupplyLink || strings.HasPrefix(path, "/supply-link/") {
		return false
	}
	if semanticSeedGenericLocPath(path) {
		return false
	}
	if semanticSeedAbstractPath(path) {
		return false
	}
	return true
}

type signalThemeHints struct {
	energy    bool
	logistics bool
	defense   bool
	maritime  bool
	aviation  bool
}

func inferSignalThemeHints(signal *horizonmodels.SignalDescriptor) signalThemeHints {
	if signal == nil {
		return signalThemeHints{}
	}
	text := strings.ToLower(strings.Join(append(append([]string{
		strings.TrimSpace(signal.Axis),
		strings.TrimSpace(signal.Mechanism),
	}, signal.SeedNames...), signal.SpherePaths...), " "))
	return signalThemeHints{
		energy: hasAnyThemeToken(text,
			"lng", "natural gas", "gas", "oil", "refinery", "pipeline", "petrochem", "energy", "crude",
		),
		logistics: hasAnyThemeToken(text,
			"logistics", "shipping", "port", "tanker", "maritime", "freight", "canal", "strait", "transit",
		),
		defense: hasAnyThemeToken(text,
			"defense", "defence", "military", "air defense", "air defence", "navy", "naval", "air force", "fighter",
		),
		maritime: hasAnyThemeToken(text,
			"navy", "naval", "maritime", "port", "shipping", "tanker", "strait", "canal", "gulf", "sea",
		),
		aviation: hasAnyThemeToken(text,
			"air force", "fighter", "aircraft", "airport", "aviation", "typhoon", "f-16", "raf",
		),
	}
}

func hasAnyThemeToken(text string, tokens ...string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return false
	}
	for _, token := range tokens {
		token = strings.ToLower(strings.TrimSpace(token))
		if token != "" && strings.Contains(text, token) {
			return true
		}
	}
	return false
}

func mergeSeedFragmentIDs(limit int, groups ...[]string) []string {
	if limit <= 0 {
		limit = 16
	}
	seen := make(map[string]bool)
	merged := make([]string, 0, limit)
	for _, group := range groups {
		for _, id := range group {
			id = strings.TrimSpace(id)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			merged = append(merged, id)
			if len(merged) >= limit {
				return merged
			}
		}
	}
	return merged
}

func trimSeedIDs(ids []string, limit int) []string {
	if limit <= 0 || len(ids) <= limit {
		return ids
	}
	out := make([]string, limit)
	copy(out, ids[:limit])
	return out
}

func semanticSeedLocationBonus(signal *horizonmodels.SignalDescriptor, frag *horizonmodels.HorizonFragment) float64 {
	if signal == nil || frag == nil {
		return 0
	}
	sigCountry := countryFromPath(signal.LocationPath)
	if sigCountry == "" {
		return 0
	}
	fragCountry := countryFromSeedFragment(frag)
	if fragCountry == "" {
		return 0
	}
	if sigCountry == fragCountry {
		return 0.35
	}
	return -0.25
}

func semanticSeedQueryGeoBonus(searchName string, frag *horizonmodels.HorizonFragment) float64 {
	queryCountry := semanticSeedCountryFromQuery(searchName)
	fragCountry := countryFromSeedFragment(frag)
	if queryCountry == "" || fragCountry == "" {
		return 0
	}
	if queryCountry == fragCountry {
		return 0.38
	}
	return -0.48
}

func semanticSeedCountryMismatch(searchName string, frag *horizonmodels.HorizonFragment) bool {
	queryCountry := semanticSeedCountryFromQuery(searchName)
	fragCountry := countryFromSeedFragment(frag)
	return queryCountry != "" && fragCountry != "" && queryCountry != fragCountry
}

func semanticSeedGenericLocPath(path string) bool {
	path = strings.ToLower(strings.TrimSpace(path))
	if !strings.HasPrefix(path, "/loc/") {
		return false
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		return true
	}
	if parts[1] == "" || parts[1] == "na" {
		return true
	}
	if len(parts) >= 4 && parts[1] == "int" && parts[2] == "na" && parts[3] != "" && parts[3] != "na" {
		return false
	}
	if len(parts) >= 3 && (parts[2] == "" || parts[2] == "na" || parts[2] == "global") {
		return true
	}
	return strings.HasSuffix(path, "/na")
}

func semanticSeedAbstractPath(path string) bool {
	path = strings.ToLower(strings.TrimSpace(path))
	return strings.HasPrefix(path, "/sphere/") || strings.HasPrefix(path, "/product/")
}

func semanticSeedCountryFromQuery(query string) string {
	rawQuery := strings.TrimSpace(query)
	query = normalizeSemanticSeedQuery(query)
	if query == "" {
		return ""
	}
	for alias, cc := range semanticSeedCountryAliases {
		if geoambiguity.IsAmbiguousCountryName(alias) && !geoambiguity.CountryAliasAllowedInText(alias, rawQuery) {
			continue
		}
		if strings.Contains(query, alias) {
			return cc
		}
	}
	return ""
}

func countryFromSeedFragment(fragment *horizonmodels.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	if fragment.HomeLocation != nil && fragment.HomeLocation.ISO3166_1 != nil {
		return strings.ToLower(strings.TrimSpace(*fragment.HomeLocation.ISO3166_1))
	}
	path := strings.TrimSpace(fragment.CanonicalPath)
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	switch parts[0] {
	case "org", "gov", "loc", "market", "community", "fac":
		return strings.ToLower(strings.TrimSpace(parts[1]))
	case "track":
		if len(parts) >= 3 {
			return strings.ToLower(strings.TrimSpace(parts[2]))
		}
	}
	return ""
}

func (s *SeedInjector) attachChainRefs(ctx context.Context, signal *horizonmodels.SignalDescriptor) {
	if s == nil || s.materializedRepo == nil || signal == nil {
		return
	}
	chainIDs := map[string]bool{}
	for _, path := range signal.SeedPaths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		chains, err := s.materializedRepo.FindByNodeRef(ctx, "global", path, horizonmodels.MaterializedKindSupplyChainPath)
		if err != nil {
			continue
		}
		for _, c := range chains {
			if c.ID != "" && !chainIDs[c.ID] {
				chainIDs[c.ID] = true
			}
		}
	}
	if len(chainIDs) == 0 {
		return
	}
	refs := make([]string, 0, len(chainIDs))
	for id := range chainIDs {
		refs = append(refs, id)
	}
	sort.Strings(refs)
	signal.ChainRefs = refs
	log.Printf("%s chain_attribution signalId=%s chains=%d", signalsys.LogSeed, strings.TrimSpace(signal.ID), len(refs))
}

// pathToSearchQuery derives a semantic search query from a canonical path.
// e.g. /org/na/microsoft -> "microsoft", /product/.../gemini-1-5-pro -> "gemini 1.5 pro"
func pathToSearchQuery(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	parts := strings.Split(path, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		p := strings.TrimSpace(parts[i])
		if p != "" && p != "na" {
			// Replace dashes with spaces for better semantic match (e.g. gemini-1-5-pro -> gemini 1 5 pro)
			return strings.ReplaceAll(p, "-", " ")
		}
	}
	return path
}

// extractFragmentIDFromGCSURI extracts fragment ID from gs://.../horizon_fragments/{id}.json
func extractFragmentIDFromGCSURI(uri string) string {
	uri = strings.TrimSpace(uri)
	const marker = "horizon_fragments/"
	idx := strings.LastIndex(uri, marker)
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSuffix(uri[idx+len(marker):], ".json")
	rest = strings.TrimSpace(rest)
	if rest == "" || strings.Contains(rest, "/") {
		return ""
	}
	return rest
}

// pathTypeFromPath returns the first segment of a canonical path, e.g. /org/na/x -> "org"
func pathTypeFromPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" || !strings.HasPrefix(p, "/") {
		return ""
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(parts[0]))
}

// pathTypesCompatible returns true if seed path type matches fragment canonical path type.
// org/gov/program — одна группа (организации, госорганы); product, market, loc — свои.
func pathTypesCompatible(seedPath, canonicalPath string) bool {
	seedType := pathTypeFromPath(seedPath)
	fragType := pathTypeFromPath(canonicalPath)
	if seedType == "" || fragType == "" {
		return true // неизвестный тип — не фильтруем
	}
	orgLike := map[string]bool{"org": true, "gov": true, "program": true}
	if orgLike[seedType] {
		return orgLike[fragType]
	}
	return seedType == fragType
}

// resolveByVertexSearch finds the closest fragment by semantic search using a human-readable query.
// Uses semanticSimilarityScore only (0–1 scale). Below 0.81 may be unreliable; path-type matching filters mismatches.
func (s *SeedInjector) resolveByVertexSearch(ctx context.Context, path string, query string) string {
	q := strings.TrimSpace(query)
	if q == "" {
		return ""
	}
	req := &ai.SearchRequest{
		Query:    q,
		PageSize: 5,
	}
	resp, err := s.vertexSearch.Search(ctx, req)
	if err != nil || resp == nil || len(resp.Results) == 0 {
		if edgeID := s.resolveByVertexEdgeSearch(ctx, path, req); edgeID != "" {
			return edgeID
		}
		return ""
	}

	const (
		minScoreHigh = 0.81 // выше — принимаем без проверки типа пути
		minScoreLow  = 0.60 // ниже — отбрасываем (0.61 Gemini даёт правильный /product/gemini)
	)

	for _, r := range resp.Results {
		score := r.RelevanceScore
		if score == 0 && r.RankSignals != nil {
			score = r.RankSignals.SemanticSimilarityScore
		}
		if score == 0 {
			score = 0.5 // fallback при отсутствии rankSignals
		}
		if score < minScoreLow {
			log.Printf("%s vertex_skip path=%q score=%.2f (<%.2f)", signalsys.LogSeed, path, score, minScoreLow)
			continue
		}

		canonicalPath := ""
		if r.Document.StructData != nil {
			if cp, ok := r.Document.StructData["canonicalPath"].(string); ok {
				canonicalPath = cp
			}
		}
		if score < minScoreHigh && !pathTypesCompatible(path, canonicalPath) {
			log.Printf("%s vertex_skip path=%q -> %s score=%.2f type_mismatch (need >=%.2f for mismatch)",
				signalsys.LogSeed, path, canonicalPath, score, minScoreHigh)
			continue
		}

		fragID := extractFragmentIDFromGCSURI(r.Document.URI)
		if fragID == "" {
			fragID = strings.TrimSuffix(strings.TrimSpace(r.Document.ID), ".json")
		}
		if fragID == "" && r.Document.StructData != nil {
			if u, ok := r.Document.StructData["uri"].(string); ok && u != "" {
				fragID = extractFragmentIDFromGCSURI(u)
			}
		}
		if fragID == "" {
			continue
		}
		frag, err := s.fragmentsRepo.Get(ctx, fragID)
		if err == nil && frag != nil {
			log.Printf("%s vertex_resolved path=%q -> fragment=%s (score=%.2f)", signalsys.LogSeed, path, fragID, score)
			return fragID
		}
	}
	return s.resolveByVertexEdgeSearch(ctx, path, req)
}

func (s *SeedInjector) resolveByVertexEdgeSearch(ctx context.Context, path string, req *ai.SearchRequest) string {
	if s == nil || s.vertexSearch == nil || req == nil {
		return ""
	}
	edgeDS := strings.TrimSpace(os.Getenv("VERTEX_SEARCH_NV_HORIZON_EDGE_DATA_STORE_ID"))
	if edgeDS == "" {
		edgeDS = strings.TrimSpace(os.Getenv("VERTEX_SEARCH_HORIZON_EDGES_DATA_STORE_ID"))
	}
	if edgeDS == "" {
		return ""
	}
	resp, err := s.vertexSearch.WithDataStore(edgeDS).Search(ctx, req)
	if err != nil || resp == nil || len(resp.Results) == 0 {
		return ""
	}

	const (
		minScoreHigh = 0.81
		minScoreLow  = 0.60
	)

	for _, r := range resp.Results {
		score := r.RelevanceScore
		if score == 0 && r.RankSignals != nil {
			score = r.RankSignals.SemanticSimilarityScore
		}
		if score == 0 {
			score = 0.5
		}
		if score < minScoreLow {
			continue
		}
		for _, candidate := range vertexEdgeCandidates(r) {
			if candidate.fragmentID == "" {
				continue
			}
			if score < minScoreHigh && !pathTypesCompatible(path, candidate.canonicalPath) {
				continue
			}
			frag, ferr := s.fragmentsRepo.Get(ctx, candidate.fragmentID)
			if ferr == nil && frag != nil {
				log.Printf("%s vertex_edge_resolved path=%q -> fragment=%s edgePath=%s (score=%.2f)", signalsys.LogSeed, path, candidate.fragmentID, candidate.canonicalPath, score)
				return candidate.fragmentID
			}
		}
	}
	return ""
}

type vertexEdgeCandidate struct {
	fragmentID    string
	canonicalPath string
}

func vertexEdgeCandidates(res ai.SearchResult) []vertexEdgeCandidate {
	if res.Document.StructData == nil {
		return nil
	}
	out := make([]vertexEdgeCandidate, 0, 2)
	if sourceID, ok := res.Document.StructData["sourceFragmentId"].(string); ok {
		sourcePath, _ := res.Document.StructData["sourcePath"].(string)
		out = append(out, vertexEdgeCandidate{
			fragmentID:    strings.TrimSpace(sourceID),
			canonicalPath: strings.TrimSpace(sourcePath),
		})
	}
	if targetID, ok := res.Document.StructData["targetFragmentId"].(string); ok {
		targetPath, _ := res.Document.StructData["targetPath"].(string)
		out = append(out, vertexEdgeCandidate{
			fragmentID:    strings.TrimSpace(targetID),
			canonicalPath: strings.TrimSpace(targetPath),
		})
	}
	return out
}
