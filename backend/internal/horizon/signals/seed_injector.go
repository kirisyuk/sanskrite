package signals

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	signalsys "github.com/igorkirisyuk/sanskrite/internal/horizon/signal"
)

// SeedInjector injects signal metrics into seed nodes.
type SeedInjector struct {
	fragmentsRepo *repo.HorizonFragmentsRepository
	edgesRepo     *repo.DependencyEdgesRepository
}

// NewSeedInjector creates a new SeedInjector.
func NewSeedInjector(fragmentsRepo *repo.HorizonFragmentsRepository, edgesRepo *repo.DependencyEdgesRepository) *SeedInjector {
	return &SeedInjector{
		fragmentsRepo: fragmentsRepo,
		edgesRepo:     edgesRepo,
	}
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

	// Step 1: Resolve SeedPaths to fragments
	if len(signal.SeedPaths) > 0 {
		for _, path := range signal.SeedPaths {
			frag, err := s.fragmentsRepo.GetByCanonicalPath(ctx, strings.TrimSpace(path))
			if err != nil {
				// Skip if fragment not found
				continue
			}
			seedFragmentIDs = append(seedFragmentIDs, frag.ID)
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
				if !seen[edge.SourceFragmentID] {
					seedFragmentIDs = append(seedFragmentIDs, edge.SourceFragmentID)
					seen[edge.SourceFragmentID] = true
				}
			}
			for _, edge := range locatedInEdges {
				if !seen[edge.SourceFragmentID] {
					seedFragmentIDs = append(seedFragmentIDs, edge.SourceFragmentID)
					seen[edge.SourceFragmentID] = true
				}
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

	// Step 3: Update metrics on seed nodes
	updatedIDs := make([]string, 0)
	metricPath := signal.MetricPath()
	now := time.Now().UTC()

	for _, fragID := range seedFragmentIDs {
		frag, err := s.fragmentsRepo.Get(ctx, fragID)
		if err != nil {
			continue
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

		// Update metric value (max; deterministic, bounded).
		if signal.Amplitude > existing.Value {
			existing.Value = signal.Amplitude
		}
		if signal.Confidence > existing.Confidence {
			existing.Confidence = signal.Confidence
		}
		existing.ObservedAt = signal.ObservedAt

		// Update provenance: keep SourceIDs (compat) + structured top-K Contributions.
		existing.Provenance.Method = "signal_injection"
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
			Amplitude:     signal.Amplitude,
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
		if err := s.fragmentsRepo.Update(ctx, frag); err != nil {
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
