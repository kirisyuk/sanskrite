package agents

import (
	"context"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// ResearchStrategyInput holds inputs for a research strategy (same shape as EnrichFragmentWithGrounding).
type ResearchStrategyInput struct {
	Fragment           *models.HorizonFragment
	MissingInfo        []string
	Focus              string
	SpherePaths        []string
	Keywords           []string
	Specialties        []string
	ChainContext       string
	OriginalQuery      string
	DecompositionHints []DecompositionHint
	ExistingEntities   []string
	GraphContext       string
	UnitPipelinePhase  models.UnitPipelinePhase
}

// ResearchStrategyOutput holds the result of a single strategy: relations, supply chains, optional fragment merge.
type ResearchStrategyOutput struct {
	Relations       []DiscoveredRelation
	SupplyChains    []SupplyChain
	FragmentPartial *models.HorizonFragment // optional; merged into working fragment by pipeline
}

// ResearchStrategy is a domain-specific enrichment strategy (supply_chain, governance, market, strategic, general).
type ResearchStrategy interface {
	DomainMode() string
	Enrich(ctx context.Context, input ResearchStrategyInput) (*ResearchStrategyOutput, error)
}
