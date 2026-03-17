package pathbuilder

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func TestBuildRelationsGraphPayload_RoundTrip(t *testing.T) {
	nodes := []models.RelationsGraphNode{
		{Type: "org", Ref: "/org/gazprom", Name: "Gazprom"},
		{Type: "facility", Ref: "/facility/nordstream-2", Name: "Nord Stream 2"},
		{Type: "geo", Ref: "/geo/turkey", Name: "Turkey"},
	}
	edges := []models.RelationsGraphEdge{
		{From: 0, To: 1, Relation: "OWNS"},
		{From: 1, To: 2, Relation: "OPERATES_IN"},
	}
	payload := models.BuildRelationsGraphPayload(nodes, edges, "governance", "/org/gazprom", nil)

	parsed, err := models.ParseRelationsGraphPayload(payload)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(parsed.Nodes) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(parsed.Nodes))
	}
	if len(parsed.Edges) != 2 {
		t.Errorf("expected 2 edges, got %d", len(parsed.Edges))
	}
	if parsed.DomainMode != "governance" {
		t.Errorf("expected domainMode=governance, got %q", parsed.DomainMode)
	}
	if parsed.RootEntity != "/org/gazprom" {
		t.Errorf("expected rootEntity=/org/gazprom, got %q", parsed.RootEntity)
	}
}

func TestRelationsGraphPayload_Validation(t *testing.T) {
	tests := []struct {
		name    string
		payload map[string]interface{}
		wantErr bool
	}{
		{
			"valid",
			models.BuildRelationsGraphPayload(
				[]models.RelationsGraphNode{{Type: "org", Ref: "/org/test"}},
				nil, "governance", "/org/test", nil,
			),
			false,
		},
		{
			"missing_version",
			map[string]interface{}{"nodes": []interface{}{}},
			true,
		},
		{
			"empty_nodes",
			map[string]interface{}{"v": 1, "nodes": []interface{}{}},
			true,
		},
		{
			"node_missing_ref",
			map[string]interface{}{
				"v": 1,
				"nodes": []interface{}{
					map[string]interface{}{"type": "org"},
				},
			},
			true,
		},
		{
			"edge_out_of_range",
			map[string]interface{}{
				"v": 1,
				"nodes": []interface{}{
					map[string]interface{}{"type": "org", "ref": "/org/test"},
				},
				"edges": []interface{}{
					map[string]interface{}{"from": 0, "to": 5, "relation": "OWNS"},
				},
			},
			true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := &models.MaterializedArtifact{
				TenantID:     "t",
				Kind:         models.MaterializedKindGovernanceChain,
				CanonicalKey: "/test/key",
				Payload:      tc.payload,
			}
			err := a.Validate()
			if tc.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestDomainModeToKind(t *testing.T) {
	tests := []struct {
		mode string
		kind models.MaterializedKind
	}{
		{taxonomy.DomainModeGovernance, models.MaterializedKindGovernanceChain},
		{taxonomy.DomainModePolicyImpact, models.MaterializedKindRegulatoryImpactMap},
		{taxonomy.DomainModeEcosystem, models.MaterializedKindEcosystemMap},
		{taxonomy.DomainModeSupplyChain, ""},
		{taxonomy.DomainModeGeneral, ""},
	}
	for _, tc := range tests {
		t.Run(tc.mode, func(t *testing.T) {
			got := domainModeToKind(tc.mode)
			if got != tc.kind {
				t.Errorf("expected %q, got %q", tc.kind, got)
			}
		})
	}
}

func TestGovernanceArtifactConfigForContractType(t *testing.T) {
	ownership := governanceArtifactConfigForContractType("ownership-structure")
	if ownership.GovernanceView != "ownership" {
		t.Fatalf("ownership GovernanceView=%q, want ownership", ownership.GovernanceView)
	}
	if _, ok := ownership.PrimaryEdges[models.DependencyTypeOwns]; !ok {
		t.Fatal("ownership config must prioritize OWNS")
	}
	if _, ok := ownership.PrimaryEdges[models.DependencyTypeSubsidiaryOf]; !ok {
		t.Fatal("ownership config must prioritize SUBSIDIARY_OF")
	}
	if _, ok := ownership.PrimaryEdges[models.DependencyTypeManages]; ok {
		t.Fatal("ownership config must not prioritize MANAGES")
	}

	control := governanceArtifactConfigForContractType("control-map")
	if control.GovernanceView != "control" {
		t.Fatalf("control GovernanceView=%q, want control", control.GovernanceView)
	}
	for _, edgeType := range []models.DependencyType{
		models.DependencyTypeControls,
		models.DependencyTypeManages,
		models.DependencyTypeAdministers,
	} {
		if _, ok := control.PrimaryEdges[edgeType]; !ok {
			t.Fatalf("control-map config must prioritize %s", edgeType)
		}
	}
	if _, ok := control.PrimaryEdges[models.DependencyTypeOwns]; ok {
		t.Fatal("control-map config must not make OWNS a primary traversal edge")
	}
}

func TestIsAllowedEdge(t *testing.T) {
	allowed := []string{"OWNS", "CONTROLS", "MANAGES"}
	if !isAllowedEdge("OWNS", allowed) {
		t.Error("OWNS should be allowed")
	}
	if !isAllowedEdge("owns", allowed) {
		t.Error("case-insensitive: owns should be allowed")
	}
	if isAllowedEdge("SUPPLIES_TO", allowed) {
		t.Error("SUPPLIES_TO should not be in governance allowlist")
	}
}

func TestGenerateVectorText_GovernanceChain(t *testing.T) {
	nodes := []models.RelationsGraphNode{
		{Type: "org", Ref: "/org/gazprom", Name: "Gazprom"},
		{Type: "person", Ref: "/person/miller", Name: "Alexei Miller"},
	}
	edges := []models.RelationsGraphEdge{
		{From: 1, To: 0, Relation: "EMPLOYED_BY"},
	}
	payload := models.BuildRelationsGraphPayload(nodes, edges, "governance", "/org/gazprom", nil)

	a := &models.MaterializedArtifact{
		Kind:        models.MaterializedKindGovernanceChain,
		DisplayName: "Gazprom Governance",
		NodeRefs:    []string{"/org/gazprom", "/person/miller"},
		Payload:     payload,
	}
	text := a.GenerateVectorText()
	if text == "" {
		t.Error("vector text should not be empty")
	}
	if !containsSubstring(text, "Gazprom Governance") {
		t.Error("should contain display name")
	}
	if !containsSubstring(text, "EMPLOYED_BY") {
		t.Error("should contain relation type")
	}
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && findSubstring(s, sub) >= 0
}

func findSubstring(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestInferNameFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/org/gazprom-neft", "gazprom neft"},
		{"/facility/lng-terminal", "lng terminal"},
		{"/geo/turkey", "turkey"},
	}
	for _, tc := range tests {
		got := inferNameFromPath(tc.path)
		if got != tc.want {
			t.Errorf("inferNameFromPath(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestCollectArtifactGeoRefs(t *testing.T) {
	got := collectArtifactGeoRefs([]string{
		"/community/om/dynasty/al-said",
		"/market/gcc/technology/space",
		"/org/ru/roscosmos",
		"/org/ru/roscosmos",
		"/person/elon-musk",
	})

	want := []string{"/loc/gcc", "/loc/om", "/loc/ru"}
	if len(got) != len(want) {
		t.Fatalf("expected %d geo refs, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected geo ref %q at index %d, got %q", want[i], i, got[i])
		}
	}
}
