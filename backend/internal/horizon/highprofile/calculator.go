package highprofile

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// Calculator computes global "high-profile" signals for Horizon fragments.
// It is deterministic and derived from the graph (edges + fragment fields).
//
// NOTE: This is GLOBAL-only. Lens-specific overlays should be computed and stored elsewhere.
type Calculator struct {
	fragmentsRepo *repo.HorizonFragmentsRepository
	edgesRepo     *repo.DependencyEdgesRepository

	// Limits to keep computation bounded.
	maxEdgesPerDir int
	maxEvidenceK   int
}

func NewCalculator(fragmentsRepo *repo.HorizonFragmentsRepository, edgesRepo *repo.DependencyEdgesRepository) *Calculator {
	return &Calculator{
		fragmentsRepo:  fragmentsRepo,
		edgesRepo:      edgesRepo,
		maxEdgesPerDir: 250,
		maxEvidenceK:   6,
	}
}

func (c *Calculator) WithLimits(maxEdgesPerDir int, maxEvidenceK int) *Calculator {
	if maxEdgesPerDir > 0 {
		c.maxEdgesPerDir = maxEdgesPerDir
	}
	if maxEvidenceK > 0 {
		c.maxEvidenceK = maxEvidenceK
	}
	return c
}

// Compute computes HighProfile for a fragment (read-only).
func (c *Calculator) Compute(ctx context.Context, fragmentID string) (*models.HighProfile, error) {
	if c == nil || c.fragmentsRepo == nil || c.edgesRepo == nil {
		return nil, nil
	}
	frag, err := c.fragmentsRepo.Get(ctx, strings.TrimSpace(fragmentID))
	if err != nil || frag == nil {
		return nil, err
	}

	outEdges, _ := c.edgesRepo.ListBySource(ctx, "global", frag.ID, nil, c.maxEdgesPerDir)
	inEdges, _ := c.edgesRepo.ListByTarget(ctx, "global", frag.ID, nil, c.maxEdgesPerDir)

	neighborIDs := make(map[string]struct{})
	for _, e := range outEdges {
		if e != nil && strings.TrimSpace(e.TargetFragmentID) != "" {
			neighborIDs[strings.TrimSpace(e.TargetFragmentID)] = struct{}{}
		}
	}
	for _, e := range inEdges {
		if e != nil && strings.TrimSpace(e.SourceFragmentID) != "" {
			neighborIDs[strings.TrimSpace(e.SourceFragmentID)] = struct{}{}
		}
	}

	// Load neighbor fragments (best-effort).
	ids := make([]string, 0, len(neighborIDs))
	for id := range neighborIDs {
		ids = append(ids, id)
	}
	neighbors, _ := c.fragmentsRepo.GetMany(ctx, ids)
	neighborByID := make(map[string]*models.HorizonFragment, len(neighbors))
	for _, n := range neighbors {
		if n != nil && strings.TrimSpace(n.ID) != "" {
			neighborByID[strings.TrimSpace(n.ID)] = n
		}
	}

	now := time.Now().UTC()
	hp := &models.HighProfile{
		Version:   1,
		UpdatedAt: now,
		Signals:   map[string]models.HighProfileSignal{},
	}

	// --- v1 signals (minimal, high-signal only) ---
	c.computeDynastySignals(hp, frag, inEdges, outEdges, neighborByID)
	c.computeRegulatorySignals(hp, frag, inEdges, outEdges, neighborByID)
	c.computeMarketSignals(hp, frag, inEdges, outEdges, neighborByID)
	c.computeChokepointSignals(hp, frag, inEdges, outEdges, neighborByID)

	// Drop empty.
	if len(hp.Signals) == 0 {
		return nil, nil
	}
	return hp, nil
}

// Apply computes and persists highProfile for a fragment (best-effort).
func (c *Calculator) Apply(ctx context.Context, fragmentID string) (*models.HighProfile, error) {
	hp, err := c.Compute(ctx, fragmentID)
	if err != nil {
		return nil, err
	}
	// Persist as a field-only update to avoid clobbering concurrent changes.
	if c != nil && c.fragmentsRepo != nil {
		_ = c.fragmentsRepo.UpdateHighProfile(ctx, strings.TrimSpace(fragmentID), hp)
	}
	return hp, nil
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

func levelFromScore(score float64) string {
	s := clamp01(score)
	switch {
	case s >= 0.85:
		return "critical"
	case s >= 0.70:
		return "high"
	case s >= 0.40:
		return "medium"
	case s > 0:
		return "low"
	default:
		return ""
	}
}

type scoredEdge struct {
	edge     *models.DependencyEdge
	score    float64
	neighbor string // canonical path (best-effort)
}

func topKEvidence(in []scoredEdge, k int) (edgeIDs []string, neighborPaths []string) {
	if k <= 0 || len(in) == 0 {
		return nil, nil
	}
	sort.Slice(in, func(i, j int) bool { return in[i].score > in[j].score })
	if len(in) > k {
		in = in[:k]
	}
	for _, se := range in {
		if se.edge != nil && strings.TrimSpace(se.edge.ID) != "" {
			edgeIDs = append(edgeIDs, strings.TrimSpace(se.edge.ID))
		}
		if strings.TrimSpace(se.neighbor) != "" {
			neighborPaths = append(neighborPaths, strings.TrimSpace(se.neighbor))
		}
	}
	return edgeIDs, neighborPaths
}

