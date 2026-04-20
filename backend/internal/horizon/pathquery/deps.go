package pathquery

import (
	"context"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

type fragmentLookup interface {
	Get(ctx context.Context, id string) (*horizonmodels.HorizonFragment, error)
	GetByCanonicalPathForTenant(ctx context.Context, tenantID, canonicalPath string) (*horizonmodels.HorizonFragment, error)
	GetByCanonicalPath(ctx context.Context, canonicalPath string) (*horizonmodels.HorizonFragment, error)
}

type materializedSupplyPaths interface {
	FindByNodeRef(ctx context.Context, tenantID string, path string, kind horizonmodels.MaterializedKind) ([]*horizonmodels.MaterializedArtifact, error)
	Get(ctx context.Context, id string) (*horizonmodels.MaterializedArtifact, error)
}

type dependencyEdges interface {
	ListBySource(ctx context.Context, tenantID, sourceFragmentID string, edgeType *ah.RelationName, limit int) ([]*horizonmodels.DependencyEdge, error)
	ListByTarget(ctx context.Context, tenantID, targetFragmentID string, edgeType *ah.RelationName, limit int) ([]*horizonmodels.DependencyEdge, error)
}

// LensMaterializedRepo is the materialized artifact surface used for lens-scoped supply-chain collection.
type LensMaterializedRepo interface {
	ListByLens(ctx context.Context, lensID string, limit int) ([]*horizonmodels.MaterializedArtifact, error)
	GetByCanonicalKeyAnyKind(ctx context.Context, tenantID, key string) (*horizonmodels.MaterializedArtifact, error)
	FindByNodeRefsAny(ctx context.Context, tenantID string, refs []string, kind horizonmodels.MaterializedKind, limit int) ([]*horizonmodels.MaterializedArtifact, error)
	FindByFlowRefsAny(ctx context.Context, tenantID string, refs []string, kind horizonmodels.MaterializedKind, limit int) ([]*horizonmodels.MaterializedArtifact, error)
	FindByZoneRefsAny(ctx context.Context, tenantID string, refs []string, kind horizonmodels.MaterializedKind, limit int) ([]*horizonmodels.MaterializedArtifact, error)
	FindByAffectedZoneRefsAny(ctx context.Context, tenantID string, refs []string, kind horizonmodels.MaterializedKind, limit int) ([]*horizonmodels.MaterializedArtifact, error)
}

// SupplyChainSearchMaterialized extends materialized reads for POST supply-chain search.
type SupplyChainSearchMaterialized interface {
	materializedSupplyPaths
	LensMaterializedRepo
	ListByTenantAndKind(ctx context.Context, tenantID string, kind horizonmodels.MaterializedKind, limit int) ([]*horizonmodels.MaterializedArtifact, error)
}

// FragmentTraceMaterialized is the materialized surface for fragment-trace supply chains (includes lens merge).
type FragmentTraceMaterialized interface {
	LensMaterializedRepo
	FindByNodeRef(ctx context.Context, tenantID string, path string, kind horizonmodels.MaterializedKind) ([]*horizonmodels.MaterializedArtifact, error)
	FindByAffectedZoneRefsAny(ctx context.Context, tenantID string, refs []string, kind horizonmodels.MaterializedKind, limit int) ([]*horizonmodels.MaterializedArtifact, error)
}

// LensGetter loads a lens document (optional in fragment trace).
type LensGetter interface {
	Get(ctx context.Context, id string) (*horizonmodels.Lens, error)
}
