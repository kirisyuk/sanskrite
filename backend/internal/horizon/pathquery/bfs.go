package pathquery

import (
	"context"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

func (s *Service) expandNeighbors(ctx context.Context, tenantID, fragmentID string, rels []ah.RelationName, diag *Diagnostics, perNodeEdgeCap int) []string {
	if s.edges == nil || fragmentID == "" {
		return nil
	}
	diag.RelationFilterApplied = true
	seen := make(map[string]struct{})
	var out []string
	for _, rel := range rels {
		r := rel
		edgesOut, err := s.edges.ListBySource(ctx, tenantID, fragmentID, &r, perNodeEdgeCap)
		if err == nil {
			for _, e := range edgesOut {
				if e == nil {
					continue
				}
				tid := strings.TrimSpace(e.TargetFragmentID)
				if tid == "" {
					continue
				}
				if _, ok := seen[tid]; ok {
					continue
				}
				seen[tid] = struct{}{}
				out = append(out, tid)
			}
		}
		edgesIn, err := s.edges.ListByTarget(ctx, tenantID, fragmentID, &r, perNodeEdgeCap)
		if err == nil {
			for _, e := range edgesIn {
				if e == nil {
					continue
				}
				sid := strings.TrimSpace(e.SourceFragmentID)
				if sid == "" {
					continue
				}
				if _, ok := seen[sid]; ok {
					continue
				}
				seen[sid] = struct{}{}
				out = append(out, sid)
			}
		}
	}
	return out
}

// bfsReachable returns whether target is reachable within maxDepth undirected expansion along rels.
func (s *Service) bfsReachable(ctx context.Context, nq *normalizedQuery, diag *Diagnostics) bool {
	if !nq.ToPresent || s.edges == nil {
		return false
	}
	start := strings.TrimSpace(nq.FromFragmentID)
	target := strings.TrimSpace(nq.ToFragmentID)
	if start == "" || target == "" {
		return false
	}
	tenant := strings.TrimSpace(nq.Query.Tenant)
	type qitem struct {
		id   string
		depth int
	}
	queue := []qitem{{id: start, depth: 0}}
	visited := map[string]struct{}{start: {}}
	diag.BFSExpandedNodes = 1
	const perNodeEdgeCap = 64
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.id == target {
			return true
		}
		if cur.depth >= nq.MaxHops {
			diag.TruncatedMaxHops = true
			continue
		}
		if diag.BFSExpandedNodes >= maxBFSNodeVisits {
			diag.Warnings = append(diag.Warnings, "bfs node visit budget exhausted")
			break
		}
		for _, next := range s.expandNeighbors(ctx, tenant, cur.id, nq.RelationKinds, diag, perNodeEdgeCap) {
			if _, ok := visited[next]; ok {
				continue
			}
			visited[next] = struct{}{}
			diag.BFSExpandedNodes++
			if diag.BFSExpandedNodes >= maxBFSNodeVisits {
				diag.Warnings = append(diag.Warnings, "bfs node visit budget exhausted")
				return false
			}
			queue = append(queue, qitem{id: next, depth: cur.depth + 1})
		}
	}
	return false
}
