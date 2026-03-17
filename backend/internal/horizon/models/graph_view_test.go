package models

import "testing"

func TestGraphViewForDirective(t *testing.T) {
	tests := []struct {
		name          string
		contractType  string
		hierarchyAxis string
		want          string
	}{
		{
			name:         "ownership contract maps to hierarchical",
			contractType: "ownership-structure",
			want:         GraphViewHierarchical,
		},
		{
			name:         "control map contract maps to influence network",
			contractType: "control-map",
			want:         GraphViewInfluenceNet,
		},
		{
			name:         "commonality contract maps to influence network",
			contractType: "commonality-analysis",
			want:         GraphViewInfluenceNet,
		},
		{
			name:          "market hierarchy prefers value chain",
			hierarchyAxis: "market",
			want:          GraphViewValueChain,
		},
		{
			name:         "supply keyword falls back to supply flow",
			contractType: "space-supply-chain",
			want:         GraphViewSupplyFlow,
		},
		{
			name:         "unknown defaults to hierarchical",
			contractType: "unknown",
			want:         GraphViewHierarchical,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := GraphViewForDirective(tc.contractType, tc.hierarchyAxis)
			if got != tc.want {
				t.Fatalf("GraphViewForDirective(%q, %q) = %q, want %q", tc.contractType, tc.hierarchyAxis, got, tc.want)
			}
		})
	}
}
