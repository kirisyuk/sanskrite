package models

import (
	"testing"
	"time"
)

func TestExtractInfluenceQualifiers(t *testing.T) {
	tests := []struct {
		name    string
		edge    *DependencyEdge
		wantErr bool
		check   func(*InfluenceQualifiers) bool
	}{
		{
			name: "valid influence edge with all qualifiers",
			edge: &DependencyEdge{
				Type: DependencyTypeInfluences,
				Qualifiers: map[string]interface{}{
					QualifierVia:           "/product/raw/energy/extraction/natural-gas",
					QualifierMechanism:     "energy_dependency",
					QualifierDirection:     "negative",
					QualifierReversible:    true,
					QualifierConditionality: []interface{}{"winter", "low_storage"},
					QualifierStrength:      0.8,
				},
			},
			wantErr: false,
			check: func(q *InfluenceQualifiers) bool {
				return q.Via != nil && *q.Via == "/product/raw/energy/extraction/natural-gas" &&
					q.Mechanism != nil && *q.Mechanism == "energy_dependency" &&
					q.Direction != nil && *q.Direction == "negative" &&
					q.Reversible != nil && *q.Reversible == true &&
					len(q.Conditionality) == 2 &&
					q.Strength != nil && *q.Strength == 0.8
			},
		},
		{
			name: "non-influence edge",
			edge: &DependencyEdge{
				Type: DependencyTypeProduces,
			},
			wantErr: true,
		},
		{
			name: "nil edge",
			edge: nil,
			wantErr: true,
		},
		{
			name: "influence edge without qualifiers",
			edge: &DependencyEdge{
				Type: DependencyTypeInfluences,
			},
			wantErr: false,
			check: func(q *InfluenceQualifiers) bool {
				return q != nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qualifiers, err := ExtractInfluenceQualifiers(tt.edge)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExtractInfluenceQualifiers() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.check != nil {
				if !tt.check(qualifiers) {
					t.Errorf("ExtractInfluenceQualifiers() qualifiers check failed")
				}
			}
		})
	}
}

func TestSetInfluenceQualifiers(t *testing.T) {
	edge := &DependencyEdge{
		Type:       DependencyTypeInfluences,
		Qualifiers: make(map[string]interface{}),
	}

	via := "/product/natural-gas"
	mechanism := "energy_dependency"
	direction := "negative"
	reversible := true
	strength := 0.8
	qualifiers := &InfluenceQualifiers{
		Via:           &via,
		Mechanism:     &mechanism,
		Direction:     &direction,
		Reversible:    &reversible,
		Conditionality: []string{"winter", "low_storage"},
		Strength:      &strength,
	}

	err := SetInfluenceQualifiers(edge, qualifiers)
	if err != nil {
		t.Fatalf("SetInfluenceQualifiers() error = %v", err)
	}

	// Verify qualifiers were set
	if edge.Qualifiers[QualifierVia] != "/product/natural-gas" {
		t.Errorf("via qualifier not set correctly")
	}
	if edge.Qualifiers[QualifierMechanism] != "energy_dependency" {
		t.Errorf("mechanism qualifier not set correctly")
	}
	if edge.Qualifiers[QualifierDirection] != "negative" {
		t.Errorf("direction qualifier not set correctly")
	}
	if edge.Qualifiers[QualifierReversible] != true {
		t.Errorf("reversible qualifier not set correctly")
	}
	if edge.Qualifiers[QualifierStrength] != 0.8 {
		t.Errorf("strength qualifier not set correctly")
	}
}

func TestValidateInfluenceQualifiers(t *testing.T) {
	tests := []struct {
		name       string
		qualifiers *InfluenceQualifiers
		wantErr    bool
	}{
		{
			name: "valid qualifiers",
			qualifiers: func() *InfluenceQualifiers {
				via := "/product/natural-gas"
				direction := "negative"
				strength := 0.5
				return &InfluenceQualifiers{
					Via:       &via,
					Direction: &direction,
					Strength:  &strength,
				}
			}(),
			wantErr: false,
		},
		{
			name: "invalid via (not canonical path)",
			qualifiers: func() *InfluenceQualifiers {
				via := "product/natural-gas" // missing leading /
				return &InfluenceQualifiers{
					Via: &via,
				}
			}(),
			wantErr: true,
		},
		{
			name: "invalid direction",
			qualifiers: func() *InfluenceQualifiers {
				direction := "invalid"
				return &InfluenceQualifiers{
					Direction: &direction,
				}
			}(),
			wantErr: true,
		},
		{
			name: "invalid strength (out of range)",
			qualifiers: func() *InfluenceQualifiers {
				strength := 1.5 // > 1
				return &InfluenceQualifiers{
					Strength: &strength,
				}
			}(),
			wantErr: true,
		},
		{
			name:       "nil qualifiers",
			qualifiers: nil,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateInfluenceQualifiers(tt.qualifiers); (err != nil) != tt.wantErr {
				t.Errorf("ValidateInfluenceQualifiers() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIsInfluenceEdge(t *testing.T) {
	tests := []struct {
		name string
		edge *DependencyEdge
		want bool
	}{
		{
			name: "influence edge",
			edge: &DependencyEdge{Type: DependencyTypeInfluences},
			want: true,
		},
		{
			name: "non-influence edge",
			edge: &DependencyEdge{Type: DependencyTypeProduces},
			want: false,
		},
		{
			name: "nil edge",
			edge: nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsInfluenceEdge(tt.edge); got != tt.want {
				t.Errorf("IsInfluenceEdge() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetViaQualifier(t *testing.T) {
	tests := []struct {
		name  string
		edge  *DependencyEdge
		want  string
		found bool
	}{
		{
			name: "edge with via qualifier",
			edge: &DependencyEdge{
				Qualifiers: map[string]interface{}{
					QualifierVia: "/product/natural-gas",
				},
			},
			want:  "/product/natural-gas",
			found: true,
		},
		{
			name: "edge without via qualifier",
			edge: &DependencyEdge{
				Qualifiers: map[string]interface{}{},
			},
			found: false,
		},
		{
			name:  "nil edge",
			edge:  nil,
			found: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := GetViaQualifier(tt.edge)
			if found != tt.found {
				t.Errorf("GetViaQualifier() found = %v, want %v", found, tt.found)
			}
			if found && got != tt.want {
				t.Errorf("GetViaQualifier() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDependencyEdgeValidateWithInfluenceQualifiers(t *testing.T) {
	tests := []struct {
		name    string
		edge    *DependencyEdge
		wantErr bool
	}{
		{
			name: "valid influence edge with valid qualifiers",
			edge: &DependencyEdge{
				SourceFragmentID: "source-1",
				TargetFragmentID: "target-1",
				Type:             DependencyTypeInfluences,
				TenantID:         "global",
				Qualifiers: map[string]interface{}{
					QualifierVia:       "/product/natural-gas",
					QualifierDirection: "negative",
				},
				Properties: EdgeProperties{
					Criticality: 0.5,
				},
				Metadata: EdgeMetadata{
					CreatedAt:     time.Now(),
					LastUpdatedAt: time.Now(),
				},
			},
			wantErr: false,
		},
		{
			name: "influence edge with invalid qualifiers",
			edge: &DependencyEdge{
				SourceFragmentID: "source-1",
				TargetFragmentID: "target-1",
				Type:             DependencyTypeInfluences,
				TenantID:         "global",
				Qualifiers: map[string]interface{}{
					QualifierVia:       "invalid-path", // not canonical
					QualifierDirection: "invalid",      // invalid direction
				},
				Properties: EdgeProperties{
					Criticality: 0.5,
				},
				Metadata: EdgeMetadata{
					CreatedAt:     time.Now(),
					LastUpdatedAt: time.Now(),
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.edge.Validate(); (err != nil) != tt.wantErr {
				t.Errorf("DependencyEdge.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}


