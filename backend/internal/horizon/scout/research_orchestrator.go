package scout

import (
	"context"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

type researchOrchestratorInput struct {
	Fragment            *models.HorizonFragment
	MissingInfo         []string
	Focus               string
	SpherePaths         []string
	Keywords            []string
	Specialties         []string
	ChainContext        string
	OriginalQuery       string
	DecompositionHints  []agents.DecompositionHint
	ExistingEntities    []string
	DomainModes         []string
	PreferredDomainMode string
}

type researchOrchestratorOutput struct {
	FragmentPartial   *models.HorizonFragment
	Relations         []agents.DiscoveredRelation
	SupplyChains      []agents.SupplyChain
	GroundingEvidence *ai.GroundingEvidence
	RawContent        string
}

func (e *ScoutEngine) runResearchOrchestrator(
	ctx context.Context,
	job *models.ScoutingJob,
	policy *PostProcessingPolicy,
	in researchOrchestratorInput,
) (*researchOrchestratorOutput, error) {
	if e == nil || e.researchAgent == nil {
		return nil, fmt.Errorf("research agent is not configured")
	}
	researchMode := ResearchModeGapDriven
	if policy != nil {
		researchMode = policy.ResearchMode()
	}
	preferredDomainMode := strings.TrimSpace(in.PreferredDomainMode)
	if preferredDomainMode == "" && policy != nil {
		preferredDomainMode = strings.TrimSpace(policy.PrimaryDomainMode())
	}
	if preferredDomainMode == "" {
		preferredDomainMode = taxonomy.DomainModeGeneral
	}
	if job != nil && HasExecutionPlanContractState(job.State) {
		plan := ExecutionPlanContractFromState(job.State)
		if plan.Mode == "supply_chain" && preferredDomainMode == taxonomy.DomainModeGeneral {
			preferredDomainMode = taxonomy.DomainModeSupplyChain
		}
	}

	out := &researchOrchestratorOutput{}
	switch researchMode {
	case ResearchModeDomainHolistic:
		holisticOut, err := e.runDomainHolisticResearch(ctx, agents.HolisticResearchInput{
			Fragment:      in.Fragment,
			OriginalQuery: strings.TrimSpace(in.OriginalQuery),
			DomainMode:    preferredDomainMode,
			ChainContext:  strings.TrimSpace(in.ChainContext),
		})
		if err != nil {
			return nil, err
		}
		if holisticOut == nil {
			return nil, fmt.Errorf("domain holistic research returned nil")
		}
		out.FragmentPartial = holisticOut.FragmentPartial
		out.Relations = holisticOut.Relations
		out.SupplyChains = holisticOut.SupplyChains
		out.GroundingEvidence = holisticOut.GroundingEvidence
		out.RawContent = holisticOut.RawContent
		return out, nil

	case ResearchModeQueryContextual:
		queryOut, err := e.researchAgent.EnrichQueryContextual(ctx, in.Fragment, strings.TrimSpace(in.OriginalQuery))
		if err != nil {
			return nil, err
		}
		if queryOut == nil {
			return nil, fmt.Errorf("query contextual research returned nil")
		}
		out.FragmentPartial = queryOut.FragmentPartial
		out.Relations = queryOut.Relations
		out.SupplyChains = queryOut.SupplyChains
		out.GroundingEvidence = queryOut.GroundingEvidence
		out.RawContent = queryOut.RawContent
		return out, nil

	case ResearchModeEntityTypeHolistic:
		typeOut, err := e.researchAgent.EnrichEntityTypeHolistic(ctx, in.Fragment)
		if err != nil {
			return nil, err
		}
		if typeOut == nil {
			return nil, fmt.Errorf("entity-type holistic research returned nil")
		}
		out.FragmentPartial = typeOut.FragmentPartial
		out.Relations = typeOut.Relations
		out.SupplyChains = typeOut.SupplyChains
		out.GroundingEvidence = typeOut.GroundingEvidence
		out.RawContent = typeOut.RawContent
		return out, nil

	default:
		frag, rels, chains, evidence, raw, err := e.researchAgent.EnrichFragmentWithGroundingWithDomainModes(
			ctx,
			in.Fragment,
			in.MissingInfo,
			strings.TrimSpace(in.Focus),
			in.SpherePaths,
			in.Keywords,
			in.Specialties,
			strings.TrimSpace(in.ChainContext),
			strings.TrimSpace(in.OriginalQuery),
			in.DecompositionHints,
			in.ExistingEntities,
			in.DomainModes,
		)
		if err != nil {
			return nil, err
		}
		out.FragmentPartial = frag
		out.Relations = rels
		out.SupplyChains = chains
		out.GroundingEvidence = evidence
		out.RawContent = raw
		return out, nil
	}
}
