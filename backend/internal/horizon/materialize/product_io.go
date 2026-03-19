package materialize

import (
	"context"
	"fmt"
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	scoutagents "github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
)

// NOTE (v1): Materializes only PRODUCT fragments (FragmentTypeProduct).
// HorizonFragment is primary; Entity is best-effort enrichment via link.

type ProductIOCandidate struct {
	ProductName string
	Confidence  float64
	Reason      string
}

type ProductIOApplyRequest struct {
	TenantID          string // expected "global"
	ProjectID         string
	LensID            string
	ObjectiveFragment *horizonmodels.HorizonFragment
	Produces          []ProductIOCandidate
	Consumes          []ProductIOCandidate
}

type ProductIOApplyResult struct {
	FragmentsUpserted []*horizonmodels.HorizonFragment `json:"fragmentsUpserted"`
	EdgesUpserted     []*horizonmodels.DependencyEdge  `json:"edgesUpserted"`
}

type ProductIOMaterializer struct {
	fragmentsRepo         *horizonrepo.HorizonFragmentsRepository
	edgesRepo             *horizonrepo.DependencyEdgesRepository
	canonicalPathResolver *scout.CanonicalPathResolver // Optional: canonicalize/dedup paths before IO materialization
}

func NewProductIOMaterializer(fragmentsRepo *horizonrepo.HorizonFragmentsRepository, edgesRepo *horizonrepo.DependencyEdgesRepository) *ProductIOMaterializer {
	return &ProductIOMaterializer{fragmentsRepo: fragmentsRepo, edgesRepo: edgesRepo}
}

func (m *ProductIOMaterializer) WithCanonicalPathResolver(resolver *scout.CanonicalPathResolver) *ProductIOMaterializer {
	if m == nil {
		return m
	}
	m.canonicalPathResolver = resolver
	return m
}

func (m *ProductIOMaterializer) Apply(ctx context.Context, req *ProductIOApplyRequest) (*ProductIOApplyResult, error) {
	if m == nil || m.fragmentsRepo == nil || m.edgesRepo == nil {
		return nil, fmt.Errorf("materializer not configured")
	}
	if req == nil || req.ObjectiveFragment == nil || req.ObjectiveFragment.ID == "" {
		return nil, fmt.Errorf("objective fragment is required")
	}

	now := time.Now().UTC()
	out := &ProductIOApplyResult{
		FragmentsUpserted: []*horizonmodels.HorizonFragment{},
		EdgesUpserted:     []*horizonmodels.DependencyEdge{},
	}

	ensureProductFragment := func(name string) (*horizonmodels.HorizonFragment, error) {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("empty product name")
		}

		// Reuse existing canonicalization rules from scout canonicalizer (PRODUCT namespace).
		rel := scoutagents.DiscoveredRelation{
			TargetType:      string(horizonmodels.FragmentTypeProduct),
			TargetShortName: name,
			TargetName:      name,
		}
		candidates := scout.PreferredCanonicalPaths(rel)
		if len(candidates) == 0 {
			// Fallback keeps strict product canonical contract.
			candidates = []string{scout.BuildProductCanonicalPath("", "", "", scout.Slugify(name))}
		}

		// Prefer existing fragments in candidate order (with optional canonicalization/dedup).
		resolvedCandidates := make([]string, 0, len(candidates))
		for _, cp0 := range candidates {
			cp := strings.TrimSpace(cp0)
			if cp == "" {
				continue
			}
			// Canonicalize/upgrade flat product paths to hierarchical taxonomy when possible.
			if m.canonicalPathResolver != nil {
				if resolved, rerr := m.canonicalPathResolver.Resolve(ctx, cp, horizonmodels.FragmentTypeProduct); rerr == nil && resolved != nil && strings.TrimSpace(resolved.CanonicalPath) != "" {
					cp = strings.TrimSpace(resolved.CanonicalPath)
					if resolved.ExistingFragment != nil && strings.TrimSpace(resolved.ExistingFragment.ID) != "" {
						return resolved.ExistingFragment, nil
					}
				}
			}
			if ex, err := m.fragmentsRepo.GetByCanonicalPath(ctx, cp); err == nil && ex != nil && ex.ID != "" {
				return ex, nil
			}
			resolvedCandidates = append(resolvedCandidates, cp)
		}
		if len(resolvedCandidates) == 0 {
			return nil, fmt.Errorf("no canonical path candidates for product %q", name)
		}

		// Create using the primary repo (applies governance + version + indexPaths + vertex export).
		cp := strings.TrimSpace(resolvedCandidates[0])
		frag := &horizonmodels.HorizonFragment{
			TenantID:      "global",
			CreatedAt:     now,
			UpdatedAt:     now,
			CreatedBy:     "system",
			UpdatedBy:     "system",
			Labels:        map[string]string{"en": name},
			Status:        "active",
			CanonicalPath: cp,
			Type:          horizonmodels.FragmentTypeProduct,
			EntityPath:    "/Product",
			State:         map[string]horizonmodels.MetricValue{},
			Provenance: horizonmodels.Provenance{
				Method:    "hypothesis_product_io",
				SourceIDs: []string{strings.TrimSpace(req.LensID)},
			},
			Metadata:   horizonmodels.FragmentMetadata{CreatedAt: now, LastUpdatedAt: now, LastVerifiedAt: now, LastFactsSeenAt: now, LastFactsAppliedAt: now, EpistemicStatus: horizonmodels.EpistemicStatusLowConfidence},
			Governance: horizonmodels.Governance{Owner: "SYSTEM", Status: string(horizonmodels.FragmentStatusActive)},
		}

		id, err := m.fragmentsRepo.Create(ctx, frag)
		if err != nil {
			return nil, err
		}
		frag.ID = id
		out.FragmentsUpserted = append(out.FragmentsUpserted, frag)
		return frag, nil
	}

	ensureEdge := func(sourceID, targetID string, typ horizonmodels.DependencyType, confidence float64, reason string, qualifier map[string]interface{}) (*horizonmodels.DependencyEdge, error) {
		sourceID = strings.TrimSpace(sourceID)
		targetID = strings.TrimSpace(targetID)
		if sourceID == "" || targetID == "" || sourceID == targetID {
			return nil, fmt.Errorf("invalid edge endpoints")
		}
		if confidence < 0 {
			confidence = 0
		}
		if confidence > 1 {
			confidence = 1
		}
		if _, err := m.edgesRepo.GetBySourceTargetAndType(ctx, "global", sourceID, targetID, typ); err == nil {
			return nil, nil // already exists
		}
		if qualifier == nil {
			qualifier = map[string]interface{}{}
		}
		if strings.TrimSpace(reason) != "" {
			qualifier["reason"] = strings.TrimSpace(reason)
		}
		qualifier["materializedBy"] = "hypothesis_product_io"
		qualifier["edgeType"] = string(typ)

		e := &horizonmodels.DependencyEdge{
			TenantID:         "global",
			SourceFragmentID: sourceID,
			TargetFragmentID: targetID,
			Type:             typ,
			Properties: horizonmodels.EdgeProperties{
				Criticality:   confidence,
				Amplification: 1.0,
				LatencyHours:  24,
			},
			Qualifiers: qualifier,
			Provenance: horizonmodels.Provenance{
				Method:    "hypothesis_product_io",
				SourceIDs: []string{strings.TrimSpace(req.LensID)},
			},
			Metadata: horizonmodels.EdgeMetadata{
				CreatedAt:     now,
				LastUpdatedAt: now,
			},
		}
		id, err := m.edgesRepo.Create(ctx, e)
		if err != nil {
			return nil, err
		}
		e.ID = id
		out.EdgesUpserted = append(out.EdgesUpserted, e)
		return e, nil
	}

	// Apply produces: objective -> product (PRODUCES)
	for _, c := range req.Produces {
		p, err := ensureProductFragment(c.ProductName)
		if err != nil || p == nil {
			continue
		}
		_, _ = ensureEdge(req.ObjectiveFragment.ID, p.ID, horizonmodels.DependencyTypeProduces, c.Confidence, c.Reason, nil)
	}

	// Apply consumes: product -> objective (SUPPLIES_TO). CONSUMES removed from taxonomy.
	for _, c := range req.Consumes {
		p, err := ensureProductFragment(c.ProductName)
		if err != nil || p == nil {
			continue
		}
		_, _ = ensureEdge(p.ID, req.ObjectiveFragment.ID, horizonmodels.DependencyTypeSuppliesTo, c.Confidence, c.Reason, nil)
	}

	return out, nil
}
