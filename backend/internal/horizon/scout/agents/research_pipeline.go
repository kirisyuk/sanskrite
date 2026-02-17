package agents

import (
	"context"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// ResearchPipeline returns the pipeline runner for the agent.
func (a *ResearchAgent) ResearchPipeline() *ResearchPipelineRunner {
	return &ResearchPipelineRunner{agent: a}
}

// ResearchPipelineRunner runs domain-specific research strategies and merges results.
type ResearchPipelineRunner struct {
	agent *ResearchAgent
}

// Enrich runs the appropriate strategies for the given domain modes and merges fragment updates, relations, and supply chains.
func (p *ResearchPipelineRunner) Enrich(ctx context.Context, input ResearchStrategyInput, domainModes []string) (*models.HorizonFragment, []DiscoveredRelation, []SupplyChain, error) {
	strategies := p.strategiesForModes(domainModes)
	if len(strategies) == 0 {
		// Fallback: no strategies (e.g. general with no composable path) — caller should use EnrichFragmentWithGrounding
		return nil, nil, nil, nil
	}

	workingFragment := input.Fragment
	if workingFragment == nil {
		workingFragment = &models.HorizonFragment{}
	}
	var allRelations []DiscoveredRelation
	var allChains []SupplyChain

	for _, s := range strategies {
		out, err := s.Enrich(ctx, input)
		if err != nil {
			return nil, nil, nil, err
		}
		if out == nil {
			continue
		}
		if out.FragmentPartial != nil {
			p.agent.mergeFragment(workingFragment, out.FragmentPartial)
		}
		allRelations = append(allRelations, out.Relations...)
		allChains = append(allChains, out.SupplyChains...)
	}

	return workingFragment, allRelations, allChains, nil
}

// strategiesForModes returns the list of strategies to run for the given domain modes.
// For general or empty modes returns nil (caller should use monolithic EnrichFragmentWithGrounding).
func (p *ResearchPipelineRunner) strategiesForModes(modes []string) []ResearchStrategy {
	if len(modes) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	var list []ResearchStrategy
	hasNonGeneral := false
	for _, m := range modes {
		m = strings.TrimSpace(strings.ToLower(m))
		if m == "" {
			continue
		}
		if m == taxonomy.DomainModeGeneral {
			continue
		}
		if seen[m] {
			continue
		}
		seen[m] = true
		hasNonGeneral = true
		switch m {
		case taxonomy.DomainModeSupplyChain:
			list = append(list, &SupplyChainResearchStrategy{agent: p.agent})
		case taxonomy.DomainModeGovernance:
			list = append(list, &GovernanceResearchStrategy{agent: p.agent})
		case taxonomy.DomainModeMarket:
			list = append(list, &MarketResearchStrategy{agent: p.agent})
		case taxonomy.DomainModeStrategic:
			list = append(list, &StrategicResearchStrategy{agent: p.agent})
		}
	}
	if !hasNonGeneral {
		return nil
	}
	return list
}
