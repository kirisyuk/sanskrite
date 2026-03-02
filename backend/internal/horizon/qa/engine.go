package qa

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/igorkirisyuk/sanskrite/internal/ai"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// Engine runs the Q&A pipeline: Decompose → Simulate → Synthesize.
type Engine struct {
	decomposer          *QuestionDecomposer
	seedResolver        *SeedResolver
	leafEvidence        *LeafEvidenceCollector
	propagationEngine   *propagation.PropagationEngine
	profileLookup       *propagation.ProfileLookup
	fragmentsRepo       *horizonrepo.HorizonFragmentsRepository
	artifactsRepo       *horizonrepo.MaterializedArtifactsRepository
	vertexSearchClient  *ai.VertexAISearchClient
}

// NewEngine creates a new QA Engine.
func NewEngine(
	decomposer *QuestionDecomposer,
	seedResolver *SeedResolver,
	leafEvidence *LeafEvidenceCollector,
	propagationEngine *propagation.PropagationEngine,
	profileLookup *propagation.ProfileLookup,
	fragmentsRepo *horizonrepo.HorizonFragmentsRepository,
	artifactsRepo *horizonrepo.MaterializedArtifactsRepository,
) *Engine {
	return &Engine{
		decomposer:        decomposer,
		seedResolver:      seedResolver,
		leafEvidence:      leafEvidence,
		propagationEngine: propagationEngine,
		profileLookup:     profileLookup,
		fragmentsRepo:     fragmentsRepo,
		artifactsRepo:     artifactsRepo,
	}
}

// WithVertexSearch wires Vertex AI Search for alternative entity enrichment.
// Same client/datastore as horizon fragment sync (GCS-indexed fragments). Part of the pipeline.
func (e *Engine) WithVertexSearch(client *ai.VertexAISearchClient) *Engine {
	if e == nil {
		return e
	}
	e.vertexSearchClient = client
	return e
}

// Run executes the full Q&A pipeline and returns a SimulationResult.
// Caller is responsible for AnswerContextBuilder and AnswerSynthesizer (Phase 3).
func (e *Engine) Run(ctx context.Context, req *QARequest) (*horizonmodels.SimulationResult, *horizonmodels.QuestionPlan, error) {
	if req == nil || strings.TrimSpace(req.Question) == "" {
		return nil, nil, fmt.Errorf("question is required")
	}
	if req.Lens == nil {
		return nil, nil, fmt.Errorf("lens is required")
	}

	// Phase 1: Decompose
	lensCtx := &DecomposeContext{
		ScopePaths:        req.Lens.Scope[:minInt(30, len(req.Lens.Scope))],
		DomainModes:       req.Lens.DomainModes,
		ActiveSignals:    req.ActiveSignals,
		ArtifactSummaries: req.ArtifactSummaries,
	}
	plan, err := e.decomposer.Decompose(ctx, req.Question, lensCtx)
	if err != nil {
		return nil, nil, fmt.Errorf("decompose: %w", err)
	}
	if plan.AnswerMode == "out_of_scope" {
		return &horizonmodels.SimulationResult{
			NodeImpacts:   nil,
			TracePaths:   nil,
			LeafEvidence: nil,
			ChainImpacts: nil,
		}, plan, nil
	}

	// Resolve scope fragments
	scopeFragments := req.Lens.ScopeFragments
	if scopeFragments == nil {
		scopeFragments = make(map[string]string)
	}

	// Vertex AI Search enrichment: find alternative entities (products, orgs) for alternatives in answer
	var vertexAlternatives []*horizonmodels.HorizonFragment
	if e.vertexSearchClient != nil && e.vertexSearchClient.IsConfigured() && e.fragmentsRepo != nil {
		alts, err := e.enrichFromVertexSearch(ctx, plan.NormalizedQuestion, scopeFragments, 8)
		if err != nil {
			log.Printf("[QA] Vertex Search enrichment failed (non-fatal): %v", err)
		} else if len(alts) > 0 {
			vertexAlternatives = alts
			log.Printf("[QA] Vertex Search found %d alternative entities", len(alts))
		}
	}

	// Phase 2: Simulate
	var simResult *horizonmodels.SimulationResult
	if len(plan.HypotheticalSignals) > 0 {
		simResult, err = e.simulateHypothetical(ctx, plan, scopeFragments, req.Fragments)
	} else {
		simResult, err = e.collectCurrentState(ctx, plan, scopeFragments, req.Fragments)
	}
	if err != nil {
		return nil, plan, fmt.Errorf("simulate: %w", err)
	}

	// Collect leaf evidence
	affectedIDs := make(map[string]bool)
	for _, ni := range simResult.NodeImpacts {
		affectedIDs[ni.FragmentID] = true
	}
	leafEv := e.leafEvidence.Collect(ctx, req.Fragments, affectedIDs)
	simResult.LeafEvidence = append(simResult.LeafEvidence, leafEv...)

	// Attach Vertex Search alternatives
	simResult.VertexAlternatives = vertexAlternatives

	return simResult, plan, nil
}

// QARequest is the input for the Q&A pipeline.
type QARequest struct {
	Question           string
	Lens               *horizonmodels.Lens
	Fragments          []*horizonmodels.HorizonFragment
	ActiveSignals      []string
	ArtifactSummaries  []string
}

func (e *Engine) simulateHypothetical(
	ctx context.Context,
	plan *horizonmodels.QuestionPlan,
	scopeFragments map[string]string,
	fragments []*horizonmodels.HorizonFragment,
) (*horizonmodels.SimulationResult, error) {
	fragByID := make(map[string]*horizonmodels.HorizonFragment)
	for _, f := range fragments {
		if f != nil && f.ID != "" {
			fragByID[f.ID] = f
		}
	}

	var allNodeImpacts []horizonmodels.NodeImpact
	var allTracePaths []horizonmodels.TracePath
	affectedSet := make(map[string]bool)

	// Build fragmentID -> canonicalPath for SeedPaths (Validate requires SeedPaths or LocationPath)
	idToPath := make(map[string]string)
	for path, id := range scopeFragments {
		if path != "" && id != "" {
			idToPath[id] = path
		}
	}
	for _, f := range fragments {
		if f != nil && f.ID != "" && f.CanonicalPath != "" {
			idToPath[f.ID] = f.CanonicalPath
		}
	}

	for _, hyp := range plan.HypotheticalSignals {
		seedIDs := e.seedResolver.Resolve(hyp.SeedHints, scopeFragments)
		if len(seedIDs) == 0 {
			continue
		}

		seedPaths := make([]string, 0, len(seedIDs))
		for _, id := range seedIDs {
			if p := idToPath[id]; p != "" {
				seedPaths = append(seedPaths, p)
			}
		}
		if len(seedPaths) == 0 {
			seedPaths = append(seedPaths, hyp.SeedHints...) // fallback: use hints as paths
		}

		sig := &horizonmodels.SignalDescriptor{
			ID:        "qa_" + uuid.New().String()[:8],
			Axis:      hyp.Axis,
			Mechanism: hyp.Mechanism,
			Amplitude: hyp.Amplitude,
			Polarity:  hyp.Polarity,
			SeedPaths: seedPaths,
			Constraints: horizonmodels.SignalConstraints{
				TimeHorizon: hyp.TimeHorizon,
				Modality:    hyp.Modality,
			},
		}
		if sig.Polarity == "" {
			sig.Polarity = "risk"
		}

		sim, err := e.propagationEngine.SimulateSignalFromSeeds(ctx, sig, e.profileLookup, seedIDs)
		if err != nil {
			return nil, err
		}

		// Convert from Accumulated (fragmentID -> []AccumulatedSignal)
		for fragID, accList := range sim.Accumulated {
			var total float64
			axisAmps := make(map[string]float64)
			for _, a := range accList {
				if a.Amplitude > 0.05 {
					axisAmps[a.Axis] = a.Amplitude
					if a.Amplitude > total {
						total = a.Amplitude
					}
				}
			}
			if total < 0.05 {
				continue
			}
			affectedSet[fragID] = true
			frag := fragByID[fragID]
			name := fragID
			canonPath := ""
			typ := ""
			if frag != nil {
				canonPath = frag.CanonicalPath
				if frag.Labels != nil {
					if en, ok := frag.Labels["en"]; ok {
						name = en
					}
				}
				typ = string(frag.Type)
			}
			allNodeImpacts = append(allNodeImpacts, horizonmodels.NodeImpact{
				FragmentID:     fragID,
				CanonicalPath:  canonPath,
				Name:           name,
				Type:           typ,
				AxisAmplitudes: axisAmps,
				TotalAmplitude: total,
				Hops:           0,
			})
		}

		// Trace paths from PropagationPaths (canonical paths)
		for _, path := range sim.PropagationPaths {
			// Filter empty paths
			cleanPaths := make([]string, 0, len(path))
			for _, p := range path {
				if p = strings.TrimSpace(p); p != "" {
					cleanPaths = append(cleanPaths, p)
				}
			}
			if len(cleanPaths) == 0 {
				continue
			}
			amps := make([]float64, len(cleanPaths))
			for i, canonPath := range cleanPaths {
				fragID := scopeFragments[canonPath]
				if fragID == "" {
					for _, f := range fragments {
						if f != nil && f.CanonicalPath == canonPath {
							fragID = f.ID
							break
						}
					}
				}
				if fragID != "" {
					if arr, ok := sim.Accumulated[fragID]; ok {
						for _, a := range arr {
							if a.Amplitude > amps[i] {
								amps[i] = a.Amplitude
							}
						}
					}
				}
			}
			allTracePaths = append(allTracePaths, horizonmodels.TracePath{
				NodeCanonicalPaths: cleanPaths,
				Axis:               hyp.Axis,
				Amplitudes:         amps,
			})
		}
	}

	// Sort by amplitude desc
	sort.Slice(allNodeImpacts, func(i, j int) bool {
		return allNodeImpacts[i].TotalAmplitude > allNodeImpacts[j].TotalAmplitude
	})

	// Limit to top 20 for context
	if len(allNodeImpacts) > 20 {
		allNodeImpacts = allNodeImpacts[:20]
	}

	return &horizonmodels.SimulationResult{
		NodeImpacts:  allNodeImpacts,
		TracePaths:   allTracePaths,
		LeafEvidence: nil,
		ChainImpacts: nil,
	}, nil
}

func (e *Engine) collectCurrentState(
	_ context.Context,
	plan *horizonmodels.QuestionPlan,
	_ map[string]string,
	fragments []*horizonmodels.HorizonFragment,
) (*horizonmodels.SimulationResult, error) {
	targetAxes := plan.TargetFilter.ObserveAxes
	if len(targetAxes) == 0 {
		targetAxes = []string{"physical_flow/supply", "legal/access", "geo/conflict"}
	}
	maxNodes := plan.TargetFilter.MaxNodes
	if maxNodes <= 0 {
		maxNodes = 20
	}

	var impacts []horizonmodels.NodeImpact
	for _, frag := range fragments {
		if frag == nil || frag.State == nil {
			continue
		}
		var total float64
		axisAmps := make(map[string]float64)
		for metricPath, mv := range frag.State {
			if mv.Value <= 0 {
				continue
			}
			axis := extractAxisFromMetricPath(metricPath)
			if axis == "" {
				continue
			}
			matches := false
			for _, a := range targetAxes {
				if axis == a || strings.HasPrefix(axis, a+"/") {
					matches = true
					break
				}
			}
			if !matches {
				continue
			}
			if mv.Value > axisAmps[axis] {
				axisAmps[axis] = mv.Value
			}
			if mv.Value > total {
				total = mv.Value
			}
		}
		if total <= 0 {
			continue
		}
		name := frag.CanonicalPath
		if frag.Labels != nil {
			if en, ok := frag.Labels["en"]; ok {
				name = en
			}
		}
		impacts = append(impacts, horizonmodels.NodeImpact{
			FragmentID:     frag.ID,
			CanonicalPath:  frag.CanonicalPath,
			Name:           name,
			Type:           string(frag.Type),
			AxisAmplitudes: axisAmps,
			TotalAmplitude: total,
		})
	}
	sort.Slice(impacts, func(i, j int) bool {
		return impacts[i].TotalAmplitude > impacts[j].TotalAmplitude
	})
	if len(impacts) > maxNodes {
		impacts = impacts[:maxNodes]
	}
	return &horizonmodels.SimulationResult{
		NodeImpacts:  impacts,
		TracePaths:   nil,
		LeafEvidence: nil,
		ChainImpacts: nil,
	}, nil
}

func extractAxisFromMetricPath(path string) string {
	path = strings.TrimSpace(path)
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 3 {
		return ""
	}
	return parts[2]
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// enrichFromVertexSearch finds alternative entities via Vertex AI Search (GCS-indexed horizon fragments).
// Excludes fragments already in scope. Returns up to limit fragments.
func (e *Engine) enrichFromVertexSearch(ctx context.Context, query string, scopeFragments map[string]string, limit int) ([]*horizonmodels.HorizonFragment, error) {
	if e.vertexSearchClient == nil || !e.vertexSearchClient.IsConfigured() || e.fragmentsRepo == nil {
		return nil, nil
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	// Add "alternative" to bias toward substitute suppliers/products
	searchQuery := query + " alternative supplier"
	resp, err := e.vertexSearchClient.Search(ctx, &ai.SearchRequest{
		Query:    searchQuery,
		PageSize: limit * 2, // fetch extra in case some are filtered
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
		frag, err := e.fragmentsRepo.Get(ctx, id)
		if err != nil {
			continue // skip stale index entries
		}
		if frag != nil && frag.CanonicalPath != "" {
			// Also exclude by canonical path if already in scope
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
