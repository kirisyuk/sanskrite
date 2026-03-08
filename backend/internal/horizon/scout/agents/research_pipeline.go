package agents

import (
	"context"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/contractsemantics"
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

// Enrich runs the appropriate strategies for the given domain modes and merges fragment updates, relations, supply chains, grounding evidence, and raw research content.
func (p *ResearchPipelineRunner) Enrich(ctx context.Context, input ResearchStrategyInput, domainModes []string) (*models.HorizonFragment, []DiscoveredRelation, []SupplyChain, *ai.GroundingEvidence, string, error) {
	strategies := p.strategiesForModes(domainModes)
	workingFragment := input.Fragment
	if workingFragment == nil {
		workingFragment = &models.HorizonFragment{}
	}
	if len(strategies) == 0 {
		return workingFragment, nil, nil, nil, "", nil
	}
	var allRelations []DiscoveredRelation
	var allChains []SupplyChain
	var mergedGrounding *ai.GroundingEvidence
	var rawContentParts []string

	for _, s := range strategies {
		out, err := s.Enrich(ctx, input)
		if err != nil {
			return nil, nil, nil, nil, "", err
		}
		if out == nil {
			return nil, nil, nil, nil, "", fmt.Errorf("research strategy %T returned nil output", s)
		}
		if out.FragmentPartial != nil {
			p.agent.mergeFragment(workingFragment, out.FragmentPartial)
		}
		allRelations = append(allRelations, out.Relations...)
		allChains = append(allChains, out.SupplyChains...)
		if out.GroundingEvidence != nil {
			mergedGrounding = ai.MergeGroundingEvidence(mergedGrounding, out.GroundingEvidence)
		}
		if strings.TrimSpace(out.RawContent) != "" {
			rawContentParts = append(rawContentParts, strings.TrimSpace(out.RawContent))
		}
	}

	allRelations = deduplicateRelations(allRelations)
	rawContent := strings.Join(rawContentParts, "\n\n---\n\n")
	return workingFragment, allRelations, allChains, mergedGrounding, rawContent, nil
}

// strategiesForModes returns the list of strategies to run for the given domain modes.
// For general or empty modes returns GeneralResearchStrategy.
func (p *ResearchPipelineRunner) strategiesForModes(modes []string) []ResearchStrategy {
	if len(modes) == 0 {
		return []ResearchStrategy{&GeneralResearchStrategy{agent: p.agent}}
	}
	modes = contractsemantics.SortDomainModesForExecution(modes)
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
		case taxonomy.DomainModeAssurance:
			list = append(list, &AssuranceResearchStrategy{agent: p.agent})
		case taxonomy.DomainModeMarket:
			list = append(list, &MarketResearchStrategy{agent: p.agent})
		case taxonomy.DomainModeStrategic:
			list = append(list, &StrategicResearchStrategy{agent: p.agent})
		case taxonomy.DomainModePolicyImpact:
			list = append(list, &PolicyImpactResearchStrategy{agent: p.agent})
		case taxonomy.DomainModeEcosystem:
			list = append(list, &EcosystemResearchStrategy{agent: p.agent})
		}
	}
	if !hasNonGeneral {
		return []ResearchStrategy{&GeneralResearchStrategy{agent: p.agent}}
	}
	return list
}

func deduplicateRelations(rels []DiscoveredRelation) []DiscoveredRelation {
	if len(rels) <= 1 {
		return rels
	}
	type relKey struct{ rt, tcp, scp string }
	seen := make(map[relKey]int, len(rels))
	out := make([]DiscoveredRelation, 0, len(rels))
	for _, r := range rels {
		k := relKey{
			rt:  strings.TrimSpace(strings.ToUpper(r.RelationType)),
			tcp: strings.TrimSpace(r.TargetCanonicalPath),
			scp: strings.TrimSpace(r.SourceCanonicalPath),
		}
		if idx, ok := seen[k]; ok {
			if r.Confidence > out[idx].Confidence {
				out[idx] = r
			}
			continue
		}
		seen[k] = len(out)
		out = append(out, r)
	}
	return out
}
