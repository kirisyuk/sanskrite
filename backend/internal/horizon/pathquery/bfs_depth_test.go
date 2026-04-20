package pathquery

import (
	"context"
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

func TestBfsReachable_respectsMaxHops(t *testing.T) {
	rel := ah.RelationNameSupplies
	edges := &memEdges{
		out: map[string][]*horizonmodels.DependencyEdge{
			"fa": {{SourceFragmentID: "fa", TargetFragmentID: "mid", Relation: rel}},
			"mid": {{SourceFragmentID: "mid", TargetFragmentID: "fb", Relation: rel}},
		},
	}
	s := NewService(nil, edges, nil, nil, nil)
	nq := &normalizedQuery{
		Query:          Query{Tenant: "t", MaxHops: 1},
		ToPresent:      true,
		FromFragmentID: "fa",
		ToFragmentID:   "fb",
		MaxHops:        1,
		RelationKinds:  []ah.RelationName{rel},
	}
	d := Diagnostics{}
	if s.bfsReachable(context.Background(), nq, &d) {
		t.Fatal("expected unreachable within 1 hop for 2-hop chain")
	}
	if !d.TruncatedMaxHops {
		t.Fatalf("diag=%+v", d)
	}
}
