package pipeline

import (
	"context"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// DomainContract captures the domain-intent contract layer extracted from intent resolution or job state.
// This is a lightweight struct to avoid importing scout internals in the pipeline package.
type DomainContract struct {
	CanonicalPath      string
	ContractType       string
	Title              string
	Description        string
	Facets             map[string][]string
	FirstOrderEntities []string
	RelatedDomains     []string
	AffectedDomains    []string
}

// ContractHandler executes a contractType-specific computation and returns view directives.
type ContractHandler interface {
	ContractType() string
	Execute(ctx context.Context, eng Engine, in ContractInput) (*ContractOutput, error)
}

// FragmentsReader is a minimal fragment repository interface for contract handlers.
type FragmentsReader interface {
	Get(ctx context.Context, id string) (*models.HorizonFragment, error)
	GetByCanonicalPath(ctx context.Context, canonicalPath string) (*models.HorizonFragment, error)
	GetMany(ctx context.Context, ids []string) (map[string]*models.HorizonFragment, error)
}

// EdgesReader is a minimal edges repository interface for contract handlers.
type EdgesReader interface {
	ListBySource(ctx context.Context, tenantID, sourceFragmentID string, edgeType *models.DependencyType, limit int) ([]*models.DependencyEdge, error)
	ListByTarget(ctx context.Context, tenantID, targetFragmentID string, edgeType *models.DependencyType, limit int) ([]*models.DependencyEdge, error)
}

type ContractInput struct {
	Job                    *models.ScoutingJob
	EffectiveCanonicalPath string
	ExistingFragment       *models.HorizonFragment
	Contract               *DomainContract
	FragmentsRepo          FragmentsReader
	EdgesRepo              EdgesReader
}

type ContractOutput struct {
	ViewDirective      *models.ViewDirective
	CreatedFragmentIDs []string
	CreatedEdgeIDs     []string
}

// ContractRegistry is used by the pipeline to locate handlers.
type ContractRegistry interface {
	Get(contractType string) ContractHandler
}
