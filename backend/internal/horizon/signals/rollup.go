package signals

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	horizonmetrics "github.com/igorkirisyuk/sanskrite/internal/horizon/metrics"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// RollupService computes rollups for fragments.
type RollupService struct {
	factsRepo      *repo.OntologyFactsRepository
	fragmentsRepo  *horizonrepo.HorizonFragmentsRepository
	metricRegistry *horizonmetrics.Registry
}

// NewRollupService creates a new RollupService.
func NewRollupService(
	factsRepo *repo.OntologyFactsRepository,
	fragmentsRepo *horizonrepo.HorizonFragmentsRepository,
	metricRegistry *horizonmetrics.Registry,
) *RollupService {
	return &RollupService{
		factsRepo:      factsRepo,
		fragmentsRepo:  fragmentsRepo,
		metricRegistry: metricRegistry,
	}
}

// Rollup represents an aggregated metric value with explain trace.
type Rollup struct {
	MetricName   string          `json:"metricName"`
	Value        float64         `json:"value"`
	Confidence   float64         `json:"confidence"`
	InputCount   int             `json:"inputCount"`
	LastUpdated  time.Time       `json:"lastUpdated"`
	ExplainTrace *ExplainTrace   `json:"explainTrace,omitempty"`
}

// ExplainTrace explains how a rollup was computed.
type ExplainTrace struct {
	TopContributors []ExplainContributor `json:"topContributors"`
	PropagationPath []string              `json:"propagationPath"` // Fragment IDs
	RulesApplied    []string              `json:"rulesApplied"`     // Synthesis rule IDs
}

// ExplainContributor represents a contributing signal.
type ExplainContributor struct {
	SignalID     string  `json:"signalId"`
	SignalType   string  `json:"signalType"`
	SourcePath   string  `json:"sourcePath"`   // Canonical path of origin
	Contribution float64 `json:"contribution"` // % of final value
	Reasoning    string  `json:"reasoning"`    // From LLM or rule
}

// GetFragmentRollup computes a rollup for a fragment and metric kind.
func (s *RollupService) GetFragmentRollup(ctx context.Context, fragmentID, metricKind string) (*Rollup, error) {
	fragment, err := s.fragmentsRepo.Get(ctx, fragmentID)
	if err != nil {
		return nil, fmt.Errorf("fragment not found: %w", err)
	}

	// Get all signals attached to this fragment (optimized query with kind filter)
	signals, err := s.factsRepo.ListByCanonicalPathAndKind(ctx, "global", fragment.CanonicalPath, "signal", 1000)
	if err != nil {
		return nil, fmt.Errorf("failed to list signals: %w", err)
	}

	// Filter by kind (e.g., "risk")
	filtered := s.filterByKind(signals, metricKind)
	if len(filtered) == 0 {
		return &Rollup{
			MetricName:  metricKind + "_score",
			Value:       0,
			Confidence:  0,
			InputCount:  0,
			LastUpdated: time.Now(),
		}, nil
	}

	// Get metric spec
	spec := s.metricRegistry.GetSpec(metricKind + "_score")
	if spec == nil {
		// Default spec
		spec = &horizonmetrics.MetricSpec{
			Name:            metricKind + "_score",
			Kind:            metricKind,
			AggregationFunc: "max",
			DecayFunc:       "none",
		}
	}

	// Get handler
	handler := s.metricRegistry.GetHandler(spec.Name)

	// Apply decay to each signal
	now := time.Now()
	decayedValues := make([]horizonmodels.MetricValue, 0, len(filtered))
	contributors := make([]ExplainContributor, 0, len(filtered))

	for _, sig := range filtered {
		if len(sig.MetricUpdates) == 0 {
			continue
		}
		mu := sig.MetricUpdates[0]
		observedAt := time.Now()
		if mu.ObservedAt != nil {
			observedAt = *mu.ObservedAt
		}
		mv := horizonmodels.MetricValue{
			Value:      mu.Value,
			Confidence: mu.Confidence,
			ObservedAt: observedAt,
		}
		decayed := handler.Decay(mv, now)
		decayedValues = append(decayedValues, decayed)

		// Build contributor
		sourcePath := fragment.CanonicalPath
		if len(sig.CanonicalPaths) > 0 {
			sourcePath = sig.CanonicalPaths[0]
		}
		contributors = append(contributors, ExplainContributor{
			SignalID:     sig.ID,
			SignalType:   sig.Type,
			SourcePath:   sourcePath,
			Contribution: mu.Value,
			Reasoning:    "synthesis_rule",
		})
	}

	// Aggregate
	aggregated := handler.Aggregate(decayedValues)

	// Normalize contributions
	totalContribution := 0.0
	for i := range contributors {
		totalContribution += contributors[i].Contribution
	}
	if totalContribution > 0 {
		for i := range contributors {
			contributors[i].Contribution = (contributors[i].Contribution / totalContribution) * 100
		}
	}

	// Sort by contribution (top 3)
	topContributors := s.topContributors(contributors, 3)

	// Get propagation paths from fragment metadata or recent propagation results
	// For now, we'll leave it empty - it will be populated when propagation completes
	propagationPath := []string{} // TODO: Extract from fragment metadata or propagation history

	return &Rollup{
		MetricName:  spec.Name,
		Value:       aggregated.Value,
		Confidence:  aggregated.Confidence,
		InputCount:  len(filtered),
		LastUpdated: now,
		ExplainTrace: &ExplainTrace{
			TopContributors: topContributors,
			PropagationPath: propagationPath,
			RulesApplied:     []string{"synthesis_rule"},
		},
	}, nil
}

// filterByKind filters signals by kind prefix.
func (s *RollupService) filterByKind(signals []*coremodels.OntologyFact, kind string) []*coremodels.OntologyFact {
	filtered := make([]*coremodels.OntologyFact, 0)
	for _, sig := range signals {
		if strings.HasPrefix(sig.Type, kind+":") {
			filtered = append(filtered, sig)
		}
	}
	return filtered
}

// topContributors returns top N contributors by contribution percentage.
func (s *RollupService) topContributors(contributors []ExplainContributor, n int) []ExplainContributor {
	if len(contributors) <= n {
		return contributors
	}
	// Sort by contribution descending using sort.Slice
	sorted := make([]ExplainContributor, len(contributors))
	copy(sorted, contributors)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Contribution > sorted[j].Contribution
	})
	return sorted[:n]
}

