package signals

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
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

// SeedInjector injects signal metrics into seed nodes.
type SeedInjector struct {
	fragmentsRepo   *repo.HorizonFragmentsRepository
	edgesRepo       *repo.DependencyEdgesRepository
	applicability   ApplicabilityEvaluator
	vertexSearch    *ai.VertexAISearchClient
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
	if path == "" {
		return ""
	}
	frag, err := s.fragmentsRepo.GetByCanonicalPath(ctx, path)
	if err == nil && frag != nil {
		return frag.ID
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

// Inject injects signal metrics into seed nodes.
// Returns list of fragment IDs that were updated.
func (s *SeedInjector) Inject(ctx context.Context, signal *horizonmodels.SignalDescriptor) ([]string, error) {
	if signal == nil {
		return nil, fmt.Errorf("signal is nil")
	}

	if err := signal.Validate(); err != nil {
		return nil, fmt.Errorf("invalid signal: %w", err)
	}

	var seedFragmentIDs []string

	// Step 1: Resolve SeedPaths to fragments (exact path or Vertex AI Search semantic resolution)
	// Use SeedNames[i] for semantic search when available; Vertex index matches titles, not canonical paths.
	if len(signal.SeedPaths) > 0 {
		for i, path := range signal.SeedPaths {
			searchName := ""
			if i < len(signal.SeedNames) {
				searchName = signal.SeedNames[i]
			}
			fragID := s.ResolvePath(ctx, strings.TrimSpace(path), searchName)
			if fragID != "" {
				seedFragmentIDs = append(seedFragmentIDs, fragID)
			}
		}
	}

	// Step 2: If SeedPaths empty, use LocationPath + OPERATES_IN/LOCATED_IN edges
	if len(seedFragmentIDs) == 0 && signal.LocationPath != "" {
		locationFrag, err := s.fragmentsRepo.GetByCanonicalPath(ctx, signal.LocationPath)
		if err == nil && locationFrag != nil {
			// Find fragments that OPERATE_IN or are LOCATED_IN this location
			operatesInType := horizonmodels.DependencyTypeOperatesIn
			locatedInType := horizonmodels.DependencyTypeLocatedIn
			operatesInEdges, _ := s.edgesRepo.ListByTarget(ctx, "global", locationFrag.ID, &operatesInType, 100)
			locatedInEdges, _ := s.edgesRepo.ListByTarget(ctx, "global", locationFrag.ID, &locatedInType, 100)

			seen := make(map[string]bool)
			for _, edge := range operatesInEdges {
				if len(seedFragmentIDs) >= MaxLocationSeedCandidates {
					break
				}
				if !seen[edge.SourceFragmentID] {
					seedFragmentIDs = append(seedFragmentIDs, edge.SourceFragmentID)
					seen[edge.SourceFragmentID] = true
				}
			}
			for _, edge := range locatedInEdges {
				if len(seedFragmentIDs) >= MaxLocationSeedCandidates {
					break
				}
				if !seen[edge.SourceFragmentID] {
					seedFragmentIDs = append(seedFragmentIDs, edge.SourceFragmentID)
					seen[edge.SourceFragmentID] = true
				}
			}
			if len(seedFragmentIDs) >= MaxLocationSeedCandidates {
				log.Printf("%s location_seed_cap_applied signalId=%s location=%s selected=%d cap=%d",
					signalsys.LogSeed,
					strings.TrimSpace(signal.ID),
					strings.TrimSpace(signal.LocationPath),
					len(seedFragmentIDs),
					MaxLocationSeedCandidates,
				)
			}
		}
	}

	if len(seedFragmentIDs) == 0 {
		log.Printf("%s no seed nodes found (signalId=%s axis=%s polarity=%s location=%s seeds=%d)",
			signalsys.LogSeed,
			strings.TrimSpace(signal.ID),
			strings.TrimSpace(signal.Axis),
			strings.TrimSpace(signal.Polarity),
			strings.TrimSpace(signal.LocationPath),
			len(signal.SeedPaths),
		)
		return nil, fmt.Errorf("no seed nodes found for signal")
	}

	// Step 2.5: Resolve supply chain attribution for seed nodes.
	if s.materializedRepo != nil {
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
		if len(chainIDs) > 0 {
			refs := make([]string, 0, len(chainIDs))
			for id := range chainIDs {
				refs = append(refs, id)
			}
			sort.Strings(refs)
			signal.ChainRefs = refs
			log.Printf("%s chain_attribution signalId=%s chains=%d", signalsys.LogSeed, strings.TrimSpace(signal.ID), len(refs))
		}
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
	return ""
}
