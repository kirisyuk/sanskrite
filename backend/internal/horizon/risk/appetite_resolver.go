package risk

import (
	"context"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// AppetiteResolver resolves and auto-seeds canonical external risk appetite profiles.
type AppetiteResolver struct {
	repo          *horizonrepo.RiskAppetiteProfilesRepository
	fragmentsRepo *horizonrepo.HorizonFragmentsRepository
	edgesRepo     *horizonrepo.DependencyEdgesRepository
}

func NewAppetiteResolver(
	repo *horizonrepo.RiskAppetiteProfilesRepository,
	fragmentsRepo *horizonrepo.HorizonFragmentsRepository,
	edgesRepo *horizonrepo.DependencyEdgesRepository,
) *AppetiteResolver {
	return &AppetiteResolver{
		repo:          repo,
		fragmentsRepo: fragmentsRepo,
		edgesRepo:     edgesRepo,
	}
}

func (r *AppetiteResolver) ResolveForFragment(ctx context.Context, fragment *horizonmodels.HorizonFragment, polarity string) (*horizonmodels.RiskAppetiteProfile, []string, error) {
	if r == nil || r.repo == nil || fragment == nil {
		return nil, nil, nil
	}
	polarity = strings.ToLower(strings.TrimSpace(polarity))
	if polarity != "risk" && polarity != "opportunity" {
		return nil, nil, nil
	}

	scope := r.primaryScope(fragment)
	if scope == "" {
		return nil, nil, nil
	}

	profile, err := r.repo.Get(ctx, strings.TrimSpace(fragment.TenantID), scope, polarity)
	if err == horizonrepo.ErrRiskAppetiteProfileNotFound {
		profile = horizonmodels.DefaultRiskAppetiteProfile(strings.TrimSpace(fragment.TenantID), scope, polarity)
		_ = r.repo.Upsert(ctx, profile)
	} else if err != nil {
		return nil, nil, err
	}

	neighbors := r.neighborScopes(ctx, scope)
	if len(neighbors) > 0 {
		profile.NeighborScopes = neighbors
		_ = r.repo.Upsert(ctx, profile)
	}
	return profile, neighbors, nil
}

// EnsureForFragment seeds default profiles for both polarities when scope is resolvable.
func (r *AppetiteResolver) EnsureForFragment(ctx context.Context, fragment *horizonmodels.HorizonFragment) {
	if r == nil || fragment == nil {
		return
	}
	for _, polarity := range []string{"risk", "opportunity"} {
		_, _, _ = r.ResolveForFragment(ctx, fragment, polarity)
	}
}

func (r *AppetiteResolver) primaryScope(fragment *horizonmodels.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	cp := strings.TrimSpace(fragment.CanonicalPath)
	switch {
	case strings.HasPrefix(cp, "/market/"):
		return cp
	case len(fragment.MarketSegments) > 0:
		// /segment/{domain}/{vertical}/{geo} -> /market/{geo}/{domain}/{vertical}
		parts := strings.Split(strings.Trim(strings.TrimSpace(fragment.MarketSegments[0]), "/"), "/")
		if len(parts) == 4 && parts[0] == "segment" {
			return "/market/" + parts[3] + "/" + parts[1] + "/" + parts[2]
		}
	case strings.HasPrefix(cp, "/loc/"):
		return cp
	case fragment.HomeLocation != nil &&
		fragment.HomeLocation.ISO3166_1 != nil &&
		strings.TrimSpace(*fragment.HomeLocation.ISO3166_1) != "":
		return "/loc/" + strings.ToLower(strings.TrimSpace(*fragment.HomeLocation.ISO3166_1)) + "/na/na"
	}
	return ""
}

func (r *AppetiteResolver) neighborScopes(ctx context.Context, scope string) []string {
	scope = strings.TrimSpace(scope)
	if r == nil || r.fragmentsRepo == nil || r.edgesRepo == nil || scope == "" || !strings.HasPrefix(scope, "/market/") {
		return nil
	}
	frag, err := r.fragmentsRepo.GetByCanonicalPath(ctx, scope)
	if err != nil || frag == nil || strings.TrimSpace(frag.ID) == "" {
		return nil
	}
	neighbors := make([]string, 0, 8)
	seen := map[string]struct{}{}
	appendNeighbor := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" || !strings.HasPrefix(path, "/market/") {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		neighbors = append(neighbors, path)
	}
	outEdges, _ := r.edgesRepo.ListBySource(ctx, frag.TenantID, frag.ID, nil, 20)
	inEdges, _ := r.edgesRepo.ListByTarget(ctx, frag.TenantID, frag.ID, nil, 20)
	for _, e := range append(outEdges, inEdges...) {
		if e == nil {
			continue
		}
		otherID := e.SourceFragmentID
		if otherID == frag.ID {
			otherID = e.TargetFragmentID
		}
		if strings.TrimSpace(otherID) == "" {
			continue
		}
		other, err := r.fragmentsRepo.Get(ctx, otherID)
		if err != nil || other == nil {
			continue
		}
		appendNeighbor(other.CanonicalPath)
		if len(neighbors) >= 8 {
			break
		}
	}
	return neighbors
}
