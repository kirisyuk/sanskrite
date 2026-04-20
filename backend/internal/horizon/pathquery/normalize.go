package pathquery

import (
	"context"
	"fmt"
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

const (
	maxOpenEndedPaths   = 32
	maxBFSNodeVisits    = 10000
	defaultMaxHops      = 8
)

var defaultRelationKinds = []ah.RelationName{
	ah.RelationNameSupplies,
	ah.RelationNameProduces,
	ah.RelationNameLegConnects,
}

type normalizedQuery struct {
	Query           Query
	FromKind        EndpointKind
	FromValue       string
	ToPresent       bool
	ToKind          EndpointKind
	ToValue         string
	FromFragmentID  string
	FromCanonical   string
	ToFragmentID    string
	ToCanonical     string
	MaxHops         int
	RelationKinds   []ah.RelationName
	EffectiveTTL    time.Duration
	CacheRead       bool
	CacheWrite      bool
}

func normalizeQuery(q Query) (normalizedQuery, error) {
	nq := normalizedQuery{Query: q}
	if strings.TrimSpace(q.Tenant) == "" {
		return nq, fmt.Errorf("tenant is required")
	}
	if q.CacheTTL < 0 {
		return nq, fmt.Errorf("cacheTTL must be non-negative")
	}
	switch q.CacheMode {
	case CacheModeDisabled:
		nq.CacheRead, nq.CacheWrite = false, false
	case CacheModeRefresh:
		nq.CacheRead, nq.CacheWrite = false, true
	default:
		nq.CacheRead, nq.CacheWrite = true, true
	}
	nq.EffectiveTTL = q.CacheTTL
	if q.CacheMode == CacheModeDefault && nq.EffectiveTTL == 0 {
		nq.EffectiveTTL = DefaultCacheTTL
	}
	if q.CacheMode == CacheModeDisabled {
		nq.EffectiveTTL = 0
	}

	nq.FromKind = q.From.Kind
	nq.FromValue = strings.TrimSpace(q.From.Value)
	if nq.FromValue == "" {
		return nq, fmt.Errorf("from value is required")
	}
	if q.To != nil {
		nq.ToPresent = true
		nq.ToKind = q.To.Kind
		nq.ToValue = strings.TrimSpace(q.To.Value)
		if nq.ToValue == "" {
			return nq, fmt.Errorf("to value is required when To is set")
		}
	}

	nq.MaxHops = q.MaxHops
	if nq.MaxHops <= 0 {
		nq.MaxHops = defaultMaxHops
	}

	nq.RelationKinds = q.RelationKinds
	if len(nq.RelationKinds) == 0 {
		nq.RelationKinds = append([]ah.RelationName(nil), defaultRelationKinds...)
	}
	return nq, nil
}

func (s *Service) resolveEndpoints(ctx context.Context, nq *normalizedQuery) error {
	tenant := strings.TrimSpace(nq.Query.Tenant)
	switch nq.FromKind {
	case EndpointFragmentID:
		frag, err := s.frags.Get(ctx, nq.FromValue)
		if err != nil {
			return fmt.Errorf("resolve from fragment: %w", err)
		}
		if frag == nil {
			return fmt.Errorf("resolve from fragment: not found")
		}
		nq.FromFragmentID = strings.TrimSpace(frag.ID)
		nq.FromCanonical = strings.TrimSpace(frag.CanonicalPath)
	default:
		frag, err := s.frags.GetByCanonicalPathForTenant(ctx, tenant, nq.FromValue)
		if err != nil {
			return fmt.Errorf("resolve from canonical path: %w", err)
		}
		if frag == nil {
			return fmt.Errorf("resolve from canonical path: not found")
		}
		nq.FromFragmentID = strings.TrimSpace(frag.ID)
		nq.FromCanonical = strings.TrimSpace(frag.CanonicalPath)
	}
	if nq.FromFragmentID == "" || nq.FromCanonical == "" {
		return fmt.Errorf("from endpoint resolved to empty id/path")
	}

	if !nq.ToPresent {
		return nil
	}
	switch nq.ToKind {
	case EndpointFragmentID:
		frag, err := s.frags.Get(ctx, nq.ToValue)
		if err != nil {
			return fmt.Errorf("resolve to fragment: %w", err)
		}
		if frag == nil {
			return fmt.Errorf("resolve to fragment: not found")
		}
		nq.ToFragmentID = strings.TrimSpace(frag.ID)
		nq.ToCanonical = strings.TrimSpace(frag.CanonicalPath)
	default:
		frag, err := s.frags.GetByCanonicalPathForTenant(ctx, tenant, nq.ToValue)
		if err != nil {
			return fmt.Errorf("resolve to canonical path: %w", err)
		}
		if frag == nil {
			return fmt.Errorf("resolve to canonical path: not found")
		}
		nq.ToFragmentID = strings.TrimSpace(frag.ID)
		nq.ToCanonical = strings.TrimSpace(frag.CanonicalPath)
	}
	if nq.ToFragmentID == "" || nq.ToCanonical == "" {
		return fmt.Errorf("to endpoint resolved to empty id/path")
	}
	return nil
}

func filterByProductAxis(artifacts []*horizonmodels.MaterializedArtifact, productAxis string, diag *Diagnostics) []*horizonmodels.MaterializedArtifact {
	axis := strings.TrimSpace(productAxis)
	if axis == "" {
		return artifacts
	}
	diag.ProductAxisFiltered = true
	out := make([]*horizonmodels.MaterializedArtifact, 0, len(artifacts))
	for _, a := range artifacts {
		if a == nil {
			continue
		}
		if artifactTouchesProductAxis(a, axis) {
			out = append(out, a)
		}
	}
	return out
}

func artifactTouchesProductAxis(a *horizonmodels.MaterializedArtifact, axis string) bool {
	for _, ref := range a.NodeRefs {
		if strings.TrimSpace(ref) == axis {
			return true
		}
	}
	parsed, err := horizonmodels.ParseSupplyChainPathPayload(a.Payload)
	if err != nil || parsed == nil {
		return false
	}
	for _, leg := range parsed.Legs {
		if strings.TrimSpace(leg.ProductRef) == axis {
			return true
		}
	}
	return false
}
