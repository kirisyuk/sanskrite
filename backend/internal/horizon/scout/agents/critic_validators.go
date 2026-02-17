package agents

import (
	"context"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// CriticValidator validates a fragment and its relations in a single domain mode.
// Returns conflicts and missingInfo; does not modify the graph.
type CriticValidator interface {
	DomainMode() string
	Validate(ctx context.Context, input CriticValidatorInput) (*ValidationResult, error)
}

// CriticValidatorInput is the input for a single validator run.
type CriticValidatorInput struct {
	Fragment          *models.HorizonFragment
	Relations         []DiscoveredRelation
	SupplyChains      []SupplyChain
	SourceContent     string
	GraphContext      string
	UnitPipelinePhase models.UnitPipelinePhase
}
