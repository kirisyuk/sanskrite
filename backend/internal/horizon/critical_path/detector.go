package critical_path

import (
	"context"
	"fmt"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// Detector detects critical paths in the graph.
type Detector struct {
	fragmentsRepo *repo.HorizonFragmentsRepository
	edgesRepo     *repo.DependencyEdgesRepository
}

// NewDetector creates a new CriticalPathDetector.
func NewDetector(fragmentsRepo *repo.HorizonFragmentsRepository, edgesRepo *repo.DependencyEdgesRepository) *Detector {
	return &Detector{
		fragmentsRepo: fragmentsRepo,
		edgesRepo:     edgesRepo,
	}
}

// DetectCriticalPaths detects critical paths between anchor nodes in a Lens.
// Returns map of edge IDs that are on critical paths.
func (d *Detector) DetectCriticalPaths(ctx context.Context, anchorPaths []string, criticalityThreshold float64) (map[string]bool, error) {
	if len(anchorPaths) < 2 {
		return make(map[string]bool), nil
	}

	criticalEdges := make(map[string]bool)

	// For each pair of anchors, find shortest path by criticality
	for i := 0; i < len(anchorPaths); i++ {
		for j := i + 1; j < len(anchorPaths); j++ {
			path, err := d.findShortestPathByCriticality(ctx, anchorPaths[i], anchorPaths[j], criticalityThreshold)
			if err != nil {
				continue
			}
			// Mark all edges on this path as critical
			for _, edgeID := range path {
				criticalEdges[edgeID] = true
			}
		}
	}

	return criticalEdges, nil
}

// findShortestPathByCriticality finds shortest path between two fragments using criticality as edge weight.
func (d *Detector) findShortestPathByCriticality(ctx context.Context, sourcePath, targetPath string, threshold float64) ([]string, error) {
	// Get fragments
	sourceFragment, err := d.fragmentsRepo.GetByCanonicalPath(ctx, sourcePath)
	if err != nil || sourceFragment == nil {
		return nil, fmt.Errorf("source fragment not found: %s", sourcePath)
	}

	targetFragment, err := d.fragmentsRepo.GetByCanonicalPath(ctx, targetPath)
	if err != nil || targetFragment == nil {
		return nil, fmt.Errorf("target fragment not found: %s", targetPath)
	}

	// BFS with criticality-based priority
	visited := make(map[string]bool)
	queue := []pathNode{{fragmentID: sourceFragment.ID, path: []string{}}}
	visited[sourceFragment.ID] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if current.fragmentID == targetFragment.ID {
			return current.path, nil
		}

		// Get outgoing edges
		edges, err := d.edgesRepo.ListBySource(ctx, "global", current.fragmentID, nil, 100)
		if err != nil {
			continue
		}

		// Sort edges by criticality (highest first)
		for _, edge := range edges {
			if edge.Properties.Criticality < threshold {
				continue
			}
			if visited[edge.TargetFragmentID] {
				continue
			}

			visited[edge.TargetFragmentID] = true
			newPath := append(current.path, edge.ID)
			queue = append(queue, pathNode{
				fragmentID: edge.TargetFragmentID,
				path:       newPath,
			})
		}
	}

	return nil, fmt.Errorf("no path found")
}

type pathNode struct {
	fragmentID string
	path       []string
}

// IsCriticalPath checks if an edge is on a critical path (by checking qualifiers or criticality).
func (d *Detector) IsCriticalPath(edge *horizonmodels.DependencyEdge, sourceFragment, targetFragment *horizonmodels.HorizonFragment) bool {
	// Check explicit marker
	if v, ok := edge.Qualifiers["critical_path"].(bool); ok && v {
		return true
	}

	// Check criticality threshold
	if edge.Properties.Criticality >= 0.7 {
		return true
	}

	if hasHighNodeCriticality(sourceFragment) || hasHighNodeCriticality(targetFragment) {
		return true
	}

	return false
}

func hasHighNodeCriticality(fragment *horizonmodels.HorizonFragment) bool {
	if fragment == nil || len(fragment.State) == 0 {
		return false
	}
	for _, key := range []string{
		"supply_chain_criticality",
		"/metric/risk/supply_chain_criticality",
		horizonmodels.MetricPathImportanceStrategic,
		horizonmodels.MetricPathImportanceBusinessBlastRadius,
		horizonmodels.MetricPathImportanceConflictBlastRadius,
	} {
		if metric, ok := fragment.State[key]; ok && metric.Value >= 0.7 {
			return true
		}
	}
	return false
}
