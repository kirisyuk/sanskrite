package scout

import (
	"context"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// SupplyChainOrgResolver performs Level 3 FAC→ORG resolution: for each facility in supply chains,
// traverses OWNS/CONTROLS edges backward to find the owning organization.
// Max depth SupplyChainMaxTraversalDepth (5), cycles prohibited via visited set.
// Fallback: when no ORG found, uses /fac/{orgSlug}-general-production.
type SupplyChainOrgResolver struct {
	edgesRepo     *repo.DependencyEdgesRepository
	fragmentsRepo *repo.HorizonFragmentsRepository
}

// NewSupplyChainOrgResolver creates a new resolver.
func NewSupplyChainOrgResolver(edgesRepo *repo.DependencyEdgesRepository, fragmentsRepo *repo.HorizonFragmentsRepository) *SupplyChainOrgResolver {
	return &SupplyChainOrgResolver{
		edgesRepo:     edgesRepo,
		fragmentsRepo: fragmentsRepo,
	}
}

// SupplyChainOrgResolveResult holds FAC→owning path (ORG or fallback facility) mappings.
type SupplyChainOrgResolveResult struct {
	// FacToOrg maps facility canonical path -> owning org path or fallback /fac/{orgSlug}-general-production
	FacToOrg map[string]string
}

// Resolve resolves each FAC path in pathToID to its owning ORG (or fallback).
// pathToID: canonical path -> fragment ID (from processSupplyChains).
// Returns mappings for UI/analytics; best-effort, errors are logged but do not fail the call.
func (r *SupplyChainOrgResolver) Resolve(ctx context.Context, tenantID string, pathToID map[string]string) *SupplyChainOrgResolveResult {
	out := &SupplyChainOrgResolveResult{FacToOrg: make(map[string]string)}
	if r == nil || r.edgesRepo == nil || r.fragmentsRepo == nil || len(pathToID) == 0 {
		return out
	}

	// Collect FAC paths only
	facPaths := make([]string, 0, len(pathToID))
	for path := range pathToID {
		path = strings.TrimSpace(path)
		if path != "" && strings.HasPrefix(path, "/fac/") {
			// Skip fallback facilities (already represent org-level)
			if strings.HasSuffix(path, "-general-production") {
				continue
			}
			facPaths = append(facPaths, path)
		}
	}

	edgeTypes := []models.DependencyType{
		models.DependencyTypeOwns,
		models.DependencyTypeControls,
	}

	for _, facPath := range facPaths {
		facID := pathToID[facPath]
		if facID == "" {
			continue
		}
		owningPath := r.traverseToOrg(ctx, tenantID, facID, pathToID, edgeTypes)
		if owningPath == "" {
			owningPath = r.fallbackFromFacPath(facPath)
		}
		if owningPath != "" {
			out.FacToOrg[facPath] = owningPath
		}
	}
	return out
}

// traverseToOrg performs BFS from facility fragment ID along OWNS/CONTROLS (target→source)
// up to an ORG, max depth SupplyChainMaxTraversalDepth, cycle-prohibited.
func (r *SupplyChainOrgResolver) traverseToOrg(
	ctx context.Context,
	tenantID string,
	startFacID string,
	pathToID map[string]string,
	edgeTypes []models.DependencyType,
) string {
	visited := map[string]struct{}{startFacID: {}}
	type levelNode struct {
		fragID string
		depth  int
	}
	queue := []levelNode{{fragID: startFacID, depth: 0}}
	idToPath := r.buildIDToPath(pathToID)

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.depth >= SupplyChainMaxTraversalDepth {
			continue
		}
		for _, et := range edgeTypes {
			edges, err := r.edgesRepo.ListByTarget(ctx, tenantID, cur.fragID, &et, 200)
			if err != nil || len(edges) == 0 {
				continue
			}
			for _, edge := range edges {
				if edge == nil || edge.SourceFragmentID == "" {
					continue
				}
				sourceID := strings.TrimSpace(edge.SourceFragmentID)
				if _, ok := visited[sourceID]; ok {
					continue // cycle: abort this path
				}
				visited[sourceID] = struct{}{}

				// Resolve source fragment type
				frag, err := r.fragmentsRepo.GetMany(ctx, []string{sourceID})
				if err != nil || frag == nil {
					queue = append(queue, levelNode{fragID: sourceID, depth: cur.depth + 1})
					continue
				}
				srcFrag, ok := frag[sourceID]
				if !ok || srcFrag == nil {
					queue = append(queue, levelNode{fragID: sourceID, depth: cur.depth + 1})
					continue
				}
				srcPath := strings.TrimSpace(srcFrag.CanonicalPath)
				if srcPath == "" {
					srcPath = idToPath[sourceID]
				}
				// ORG found: /org/ or /gov/
				if srcFrag.Type == models.FragmentTypeOrganization ||
					strings.HasPrefix(srcPath, "/org/") ||
					strings.HasPrefix(srcPath, "/gov/") {
					return srcPath
				}
				queue = append(queue, levelNode{fragID: sourceID, depth: cur.depth + 1})
			}
		}
	}
	return ""
}

func (r *SupplyChainOrgResolver) buildIDToPath(pathToID map[string]string) map[string]string {
	out := make(map[string]string, len(pathToID))
	for path, id := range pathToID {
		if id != "" && path != "" {
			out[id] = path
		}
	}
	return out
}

// fallbackFromFacPath derives /fac/{orgSlug}-general-production when no ORG found.
// Path format: /fac/{orgSlug}/{facSlug} or /fac/{orgSlug}-general-production.
func (r *SupplyChainOrgResolver) fallbackFromFacPath(facPath string) string {
	facPath = strings.TrimSpace(facPath)
	if facPath == "" || !strings.HasPrefix(facPath, "/fac/") {
		return ""
	}
	parts := strings.Split(strings.Trim(facPath, "/"), "/")
	if len(parts) < 2 {
		return "" // need at least "fac" and one segment
	}
	// parts[0] = "fac", parts[1] = first segment
	firstSeg := parts[1]
	if strings.HasSuffix(firstSeg, "-general-production") {
		return facPath // already fallback
	}
	return taxonomy.FacilityGeneralProductionPath(firstSeg)
}
