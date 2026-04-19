package propagation

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// PropagationTraceResult holds the result of tracing propagation paths without persisting.
type PropagationTraceResult struct {
	SourceFragmentID string              `json:"sourceFragmentId"`
	NodeIDs         []string            `json:"nodeIds"`   // All nodes that would be affected (including source)
	Edges           []PropagationTraceEdge `json:"edges"`   // Edges that propagation would traverse
	Paths           [][]string           `json:"paths"`    // Paths as sequences of fragment IDs (source → ... → leaf)
	MaxHops         int                  `json:"maxHops"`
}

// PropagationTraceEdge represents one edge in the propagation trace.
type PropagationTraceEdge struct {
	EdgeID         string `json:"edgeId"`
	SourceFragmentID string `json:"sourceFragmentId"`
	TargetFragmentID string `json:"targetFragmentId"`
	EdgeType       string `json:"edgeType,omitempty"`
	HopsFromSource int    `json:"hopsFromSource"`
}

// TracePropagationPaths traces how propagation would flow from a node without persisting.
// Returns node IDs, edges, and paths for frontend visualization (e.g. red glow on graph).
func (e *PropagationEngine) TracePropagationPaths(
	ctx context.Context,
	fragmentID string,
	metricName string,
	delta float64,
	maxHops int,
) (*PropagationTraceResult, error) {
	if e == nil || e.fragmentsRepo == nil || e.edgesRepo == nil {
		return nil, fmt.Errorf("propagation engine or repos not configured")
	}
	fragmentID = strings.TrimSpace(fragmentID)
	if fragmentID == "" {
		return nil, fmt.Errorf("fragmentId is required")
	}
	if metricName == "" {
		metricName = "propagation_trace"
	}
	if maxHops <= 0 || maxHops > hotPathMaxHops {
		maxHops = hotPathMaxHops
	}

	sourceFragment, err := e.fragmentsRepo.Get(ctx, fragmentID)
	if err != nil || sourceFragment == nil {
		return nil, fmt.Errorf("source fragment not found: %w", err)
	}

	adapter := e.resolveDomainAdapter(ctx, sourceFragment)
	nodeIDs := make(map[string]bool)
	edges := make([]PropagationTraceEdge, 0)
	paths := make([][]string, 0)
	visitedMagnitude := map[string]float64{fragmentID + "|" + metricName: math.Abs(delta)}
	nodeIDs[sourceFragment.ID] = true

	type traceItem struct {
		frag       *models.HorizonFragment
		metricName string
		d          float64
		hops       int
		path       []string
	}
	stack := []traceItem{{frag: sourceFragment, metricName: metricName, d: delta, hops: 0, path: []string{sourceFragment.ID}}}

	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if it.frag == nil || it.hops >= maxHops {
			continue
		}

		edgeList, err := e.edgesRepo.ListBySource(ctx, "global", it.frag.ID, nil, 100)
		if err != nil {
			continue
		}

		for _, edge := range edgeList {
			if edge == nil || edge.TargetFragmentID == "" || edge.TargetFragmentID == it.frag.ID {
				continue
			}
			if !edge.IsActiveNow() || edge.IsQuarantined() {
				continue
			}

			dampedDelta := e.damping.ApplyDamping(it.d, edge.Properties.Amplification, edge.Properties.Criticality)
			propagatedMetric := it.metricName
			if adapter != nil {
				nextMetric, nextDelta, skip := e.applyAdapterTransfer(ctx, adapter, it.metricName, dampedDelta, edge, it.frag)
				if skip {
					continue
				}
				if strings.TrimSpace(nextMetric) != "" {
					propagatedMetric = strings.TrimSpace(nextMetric)
				}
				dampedDelta = nextDelta
			}
			if !e.threshold.ExceedsThreshold(propagatedMetric, dampedDelta, nil) {
				continue
			}

			absD := math.Abs(dampedDelta)
			if absD < 0 {
				absD = -absD
			}
			visitKey := edge.TargetFragmentID + "|" + propagatedMetric
			if prev, ok := visitedMagnitude[visitKey]; ok && prev >= absD {
				continue
			}

			targetFragment, err := e.fragmentsRepo.Get(ctx, edge.TargetFragmentID)
			if err != nil || targetFragment == nil {
				continue
			}

			visitedMagnitude[visitKey] = absD
			nodeIDs[targetFragment.ID] = true
			edges = append(edges, PropagationTraceEdge{
				EdgeID:           strings.TrimSpace(edge.ID),
				SourceFragmentID: it.frag.ID,
				TargetFragmentID: targetFragment.ID,
				EdgeType:         string(edge.Relation),
				HopsFromSource:   it.hops + 1,
			})
			newPath := make([]string, len(it.path)+1)
			copy(newPath, it.path)
			newPath[len(newPath)-1] = targetFragment.ID
			paths = append(paths, newPath)

			stack = append(stack, traceItem{
				frag:       targetFragment,
				metricName: propagatedMetric,
				d:          dampedDelta,
				hops:       it.hops + 1,
				path:       newPath,
			})
		}
	}

	nodeIDList := make([]string, 0, len(nodeIDs))
	for id := range nodeIDs {
		nodeIDList = append(nodeIDList, id)
	}

	return &PropagationTraceResult{
		SourceFragmentID: sourceFragment.ID,
		NodeIDs:          nodeIDList,
		Edges:            edges,
		Paths:            paths,
		MaxHops:          maxHops,
	}, nil
}
