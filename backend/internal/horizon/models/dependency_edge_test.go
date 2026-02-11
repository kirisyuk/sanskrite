package models

import (
	"testing"
	"time"
)

func TestDependencyEdge_IsActive(t *testing.T) {
	now := time.Now()
	past := now.Add(-24 * time.Hour)
	future := now.Add(24 * time.Hour)

	tests := []struct {
		name      string
		edge      *DependencyEdge
		checkTime time.Time
		want      bool
	}{
		{
			name:      "no validity bounds - always active",
			edge:      &DependencyEdge{},
			checkTime: now,
			want:      true,
		},
		{
			name:      "only ValidFrom in past - active",
			edge:      &DependencyEdge{ValidFrom: &past},
			checkTime: now,
			want:      true,
		},
		{
			name:      "only ValidFrom in future - not active yet",
			edge:      &DependencyEdge{ValidFrom: &future},
			checkTime: now,
			want:      false,
		},
		{
			name:      "only ValidTo in future - still active",
			edge:      &DependencyEdge{ValidTo: &future},
			checkTime: now,
			want:      true,
		},
		{
			name:      "only ValidTo in past - already ended",
			edge:      &DependencyEdge{ValidTo: &past},
			checkTime: now,
			want:      false,
		},
		{
			name:      "ValidFrom past, ValidTo future - active window",
			edge:      &DependencyEdge{ValidFrom: &past, ValidTo: &future},
			checkTime: now,
			want:      true,
		},
		{
			name:      "both in past - historical",
			edge:      &DependencyEdge{ValidFrom: timePtr(past.Add(-48 * time.Hour)), ValidTo: &past},
			checkTime: now,
			want:      false,
		},
		{
			name:      "both in future - projected",
			edge:      &DependencyEdge{ValidFrom: &future, ValidTo: timePtr(future.Add(48 * time.Hour))},
			checkTime: now,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.edge.IsActive(tt.checkTime); got != tt.want {
				t.Errorf("IsActive() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDependencyEdge_IsHistorical(t *testing.T) {
	past := time.Now().Add(-24 * time.Hour)
	future := time.Now().Add(24 * time.Hour)

	tests := []struct {
		name string
		edge *DependencyEdge
		want bool
	}{
		{
			name: "no ValidTo - not historical",
			edge: &DependencyEdge{},
			want: false,
		},
		{
			name: "ValidTo in past - historical",
			edge: &DependencyEdge{ValidTo: &past},
			want: true,
		},
		{
			name: "ValidTo in future - not historical",
			edge: &DependencyEdge{ValidTo: &future},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.edge.IsHistorical(); got != tt.want {
				t.Errorf("IsHistorical() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDependencyEdge_IsFuture(t *testing.T) {
	past := time.Now().Add(-24 * time.Hour)
	future := time.Now().Add(24 * time.Hour)

	tests := []struct {
		name string
		edge *DependencyEdge
		want bool
	}{
		{
			name: "no ValidFrom - not future",
			edge: &DependencyEdge{},
			want: false,
		},
		{
			name: "ValidFrom in past - not future",
			edge: &DependencyEdge{ValidFrom: &past},
			want: false,
		},
		{
			name: "ValidFrom in future - is future",
			edge: &DependencyEdge{ValidFrom: &future},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.edge.IsFuture(); got != tt.want {
				t.Errorf("IsFuture() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDependencyEdge_GetEdgeModality(t *testing.T) {
	past := time.Now().Add(-24 * time.Hour)
	future := time.Now().Add(24 * time.Hour)

	tests := []struct {
		name string
		edge *DependencyEdge
		want string
	}{
		{
			name: "no dates, no qualifier - actual",
			edge: &DependencyEdge{},
			want: "actual",
		},
		{
			name: "explicit qualifier - uses qualifier",
			edge: &DependencyEdge{
				Qualifiers: map[string]interface{}{"edgeModality": "potential"},
			},
			want: "potential",
		},
		{
			name: "ValidFrom in future - projected",
			edge: &DependencyEdge{ValidFrom: &future},
			want: "projected",
		},
		{
			name: "ValidTo in past - historical",
			edge: &DependencyEdge{ValidTo: &past},
			want: "historical",
		},
		{
			name: "ValidFrom in past, ValidTo in future - actual",
			edge: &DependencyEdge{ValidFrom: &past, ValidTo: &future},
			want: "actual",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.edge.GetEdgeModality(); got != tt.want {
				t.Errorf("GetEdgeModality() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDependencyEdge_Validate_EXTRACTS_FROM(t *testing.T) {
	edge := &DependencyEdge{
		ID:               "test-edge",
		TenantID:         "global",
		SourceFragmentID: "facility-1",
		TargetFragmentID: "deposit-1",
		Type:             DependencyTypeExtractsFrom,
		Properties: EdgeProperties{
			Criticality:   0.8,
			Amplification: 1.0,
			LatencyHours:  0,
		},
	}

	if err := edge.Validate(); err != nil {
		t.Errorf("Validate() error = %v, expected nil for valid EXTRACTS_FROM edge", err)
	}
}

func TestDependencyEdge_Validate_WithMetrics(t *testing.T) {
	edge := &DependencyEdge{
		ID:               "test-edge",
		TenantID:         "global",
		SourceFragmentID: "org-1",
		TargetFragmentID: "org-2",
		Type:             DependencyTypeOwns,
		Properties: EdgeProperties{
			Criticality:   0.9,
			Amplification: 1.0,
			LatencyHours:  0,
		},
		Metrics: map[string]MetricValue{
			"ownership_percent": {
				Value:      45.0,
				Confidence: 0.95,
			},
		},
	}

	if err := edge.Validate(); err != nil {
		t.Errorf("Validate() error = %v, expected nil for valid edge with metrics", err)
	}
}

func timePtr(t time.Time) *time.Time {
	return &t
}

func TestDependencyEdge_TransmissionQualifiers(t *testing.T) {
	edge := &DependencyEdge{
		Qualifiers: map[string]interface{}{
			QualifierThroughputShare:        "0.75",
			QualifierReroutePenalty:         0.4,
			QualifierRerouteLatencyDeltaHrs: "48",
			QualifierRerouteCostDeltaPct:    "35%",
			QualifierCapacityConstraint:     82.0, // percent style should normalize to 0.82
		},
	}

	if got := edge.GetThroughputShare(); got != 0.75 {
		t.Fatalf("GetThroughputShare() = %.2f, want 0.75", got)
	}
	if got := edge.GetReroutePenalty(); got != 0.4 {
		t.Fatalf("GetReroutePenalty() = %.2f, want 0.40", got)
	}
	if got := edge.GetRerouteLatencyDeltaHours(); got != 48 {
		t.Fatalf("GetRerouteLatencyDeltaHours() = %.2f, want 48", got)
	}
	if got := edge.GetRerouteCostDeltaPct(); got != 35 {
		t.Fatalf("GetRerouteCostDeltaPct() = %.2f, want 35", got)
	}
	if got := edge.GetCapacityConstraint(); got < 0.819 || got > 0.821 {
		t.Fatalf("GetCapacityConstraint() = %.4f, want ~0.82", got)
	}
}

func TestDependencyEdge_HasAnyTransmissionQualifier(t *testing.T) {
	edge := &DependencyEdge{
		Qualifiers: map[string]interface{}{
			QualifierThroughputShare: "0.0",
		},
	}
	if !edge.HasAnyTransmissionQualifier() {
		t.Fatalf("expected HasAnyTransmissionQualifier to detect numeric qualifier even when value is 0")
	}
}

func TestDependencyEdge_IsTransmissionSensitive(t *testing.T) {
	tests := []struct {
		name string
		edge *DependencyEdge
		want bool
	}{
		{
			name: "depends_on logistics facet",
			edge: &DependencyEdge{
				Type:       DependencyTypeDependsOn,
				Qualifiers: map[string]interface{}{"facet": string(FacetLogistics)},
			},
			want: true,
		},
		{
			name: "depends_on non transmission facet",
			edge: &DependencyEdge{
				Type:       DependencyTypeDependsOn,
				Qualifiers: map[string]interface{}{"facet": string(FacetCommunication)},
			},
			want: false,
		},
		{
			name: "depends_on unknown facet but with transmission qualifier",
			edge: &DependencyEdge{
				Type:       DependencyTypeDependsOn,
				Qualifiers: map[string]interface{}{QualifierReroutePenalty: 0.2},
			},
			want: true,
		},
		{
			name: "non depends_on never transmission sensitive",
			edge: &DependencyEdge{
				Type:       DependencyTypeSuppliesTo,
				Qualifiers: map[string]interface{}{"facet": string(FacetLogistics)},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.edge.IsTransmissionSensitive(); got != tt.want {
				t.Fatalf("IsTransmissionSensitive()=%v, want %v", got, tt.want)
			}
		})
	}
}
