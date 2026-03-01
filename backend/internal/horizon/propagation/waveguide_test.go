package propagation

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestEdgeArtifactKey(t *testing.T) {
	tests := []struct {
		name string
		edge *horizonmodels.DependencyEdge
		want string
	}{
		{
			name: "nil edge",
			edge: nil,
			want: "",
		},
		{
			name: "no qualifiers",
			edge: &horizonmodels.DependencyEdge{},
			want: "",
		},
		{
			name: "qualifiers without artifactKey",
			edge: &horizonmodels.DependencyEdge{
				Qualifiers: map[string]interface{}{"fromSupplyChain": true},
			},
			want: "",
		},
		{
			name: "artifactKey present",
			edge: &horizonmodels.DependencyEdge{
				Qualifiers: map[string]interface{}{
					"artifactKey": "/supply-chain/global/albemarle/battery-cell/catl",
				},
			},
			want: "/supply-chain/global/albemarle/battery-cell/catl",
		},
		{
			name: "artifactKey with whitespace",
			edge: &horizonmodels.DependencyEdge{
				Qualifiers: map[string]interface{}{
					"artifactKey": "  /supply-chain/global/test  ",
				},
			},
			want: "/supply-chain/global/test",
		},
		{
			name: "artifactKey non-string",
			edge: &horizonmodels.DependencyEdge{
				Qualifiers: map[string]interface{}{
					"artifactKey": 42,
				},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EdgeArtifactKey(tt.edge)
			if got != tt.want {
				t.Errorf("EdgeArtifactKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWaveguideBoostForEdge(t *testing.T) {
	chainEdge := &horizonmodels.DependencyEdge{
		Qualifiers: map[string]interface{}{
			"artifactKey": "/supply-chain/global/test",
		},
	}
	plainEdge := &horizonmodels.DependencyEdge{
		Qualifiers: map[string]interface{}{"fromSupplyChain": true},
	}

	tests := []struct {
		name    string
		edge    *horizonmodels.DependencyEdge
		profile *horizonmodels.PropagationProfile
		want    float64
	}{
		{
			name:    "plain edge returns 1.0",
			edge:    plainEdge,
			profile: &horizonmodels.PropagationProfile{AxisKey: horizonmodels.AxisPhysicalFlowSupply},
			want:    1.0,
		},
		{
			name:    "nil edge returns 1.0",
			edge:    nil,
			profile: nil,
			want:    1.0,
		},
		{
			name:    "chain edge physical_flow/supply",
			edge:    chainEdge,
			profile: &horizonmodels.PropagationProfile{AxisKey: horizonmodels.AxisPhysicalFlowSupply},
			want:    waveguideBoostPhysicalFlow,
		},
		{
			name:    "chain edge physical_flow/demand",
			edge:    chainEdge,
			profile: &horizonmodels.PropagationProfile{AxisKey: horizonmodels.AxisPhysicalFlowDemand},
			want:    waveguideBoostPhysicalFlow,
		},
		{
			name:    "chain edge logistics/capacity",
			edge:    chainEdge,
			profile: &horizonmodels.PropagationProfile{AxisKey: horizonmodels.AxisLogisticsCapacity},
			want:    waveguideBoostLogistics,
		},
		{
			name:    "chain edge logistics/latency",
			edge:    chainEdge,
			profile: &horizonmodels.PropagationProfile{AxisKey: horizonmodels.AxisLogisticsLatency},
			want:    waveguideBoostLogistics,
		},
		{
			name:    "chain edge legal/access",
			edge:    chainEdge,
			profile: &horizonmodels.PropagationProfile{AxisKey: horizonmodels.AxisLegalAccess},
			want:    waveguideBoostLegalOps,
		},
		{
			name:    "chain edge operational/continuity",
			edge:    chainEdge,
			profile: &horizonmodels.PropagationProfile{AxisKey: horizonmodels.AxisOperationalContinuity},
			want:    waveguideBoostLegalOps,
		},
		{
			name:    "chain edge default axis",
			edge:    chainEdge,
			profile: &horizonmodels.PropagationProfile{AxisKey: "some_other_axis"},
			want:    waveguideBoostDefault,
		},
		{
			name:    "chain edge nil profile",
			edge:    chainEdge,
			profile: nil,
			want:    waveguideBoostDefault,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WaveguideBoostForEdge(tt.edge, tt.profile)
			if got != tt.want {
				t.Errorf("WaveguideBoostForEdge() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWaveguideCoherenceBonus(t *testing.T) {
	tests := []struct {
		name     string
		current  string
		neighbor string
		want     float64
	}{
		{
			name:     "same chain",
			current:  "/supply-chain/global/test",
			neighbor: "/supply-chain/global/test",
			want:     waveguideCoherenceBonus,
		},
		{
			name:     "different chains",
			current:  "/supply-chain/global/test1",
			neighbor: "/supply-chain/global/test2",
			want:     1.0,
		},
		{
			name:     "current empty",
			current:  "",
			neighbor: "/supply-chain/global/test",
			want:     1.0,
		},
		{
			name:     "neighbor empty",
			current:  "/supply-chain/global/test",
			neighbor: "",
			want:     1.0,
		},
		{
			name:     "both empty",
			current:  "",
			neighbor: "",
			want:     1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WaveguideCoherenceBonus(tt.current, tt.neighbor)
			if got != tt.want {
				t.Errorf("WaveguideCoherenceBonus() = %v, want %v", got, tt.want)
			}
		})
	}
}
