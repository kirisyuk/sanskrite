package teslabattery

import (
	"strings"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func TestSpecPathsAndEdgesAreTaxonomySafe(t *testing.T) {
	spec := Spec("prevalent-horizon-test")
	if spec.ID == "" || spec.Title == "" {
		t.Fatalf("spec identity must be populated")
	}
	if len(spec.Fragments) == 0 || len(spec.Edges) == 0 || len(spec.Simulations) == 0 {
		t.Fatalf("spec must contain fragments, edges, and simulations")
	}

	paths := map[string]FragmentType{}
	for _, fragment := range spec.Fragments {
		if err := taxonomy.ValidateCanonicalPath(fragment.CanonicalPath); err != nil {
			t.Fatalf("invalid canonical path %q: %v", fragment.CanonicalPath, err)
		}
		if fragment.Type == "" {
			t.Fatalf("fragment %q missing type", fragment.CanonicalPath)
		}
		paths[fragment.CanonicalPath] = fragment.Type
	}

	allowedEdges := map[string]struct{}{}
	for _, edgeType := range taxonomy.PolicyImpactEdgeTypes {
		allowedEdges[strings.TrimSpace(edgeType)] = struct{}{}
	}
	for _, edgeType := range taxonomy.SupplyChainEdgeTypes {
		allowedEdges[strings.TrimSpace(edgeType)] = struct{}{}
	}
	allowedEdges[string(DependencyTypeUsedIn)] = struct{}{}

	foundDecisionMechanism := false
	foundSupplierFlow := false
	foundProductTransition := false
	for _, edge := range spec.Edges {
		if _, ok := allowedEdges[string(edge.Type)]; !ok {
			t.Fatalf("edge type %q is not allowed in supply_chain/policy_impact mode", edge.Type)
		}
		if _, ok := paths[edge.SourcePath]; !ok {
			t.Fatalf("edge source %q is not declared in fragment seeds", edge.SourcePath)
		}
		if _, ok := paths[edge.TargetPath]; !ok {
			t.Fatalf("edge target %q is not declared in fragment seeds", edge.TargetPath)
		}
		if edge.SourcePath == "/decision/trade/restrict-battery-material-and-cell-flows-to-us-2026" &&
			edge.TargetPath == "/mechanism/regulatory/export-controls-and-logistics-delays" &&
			edge.Type == DependencyTypeUsesMechanism {
			foundDecisionMechanism = true
		}
		if edge.SourcePath == "/fac/jp/panasonic-giga-nevada-lines" &&
			edge.TargetPath == "/fac/us/gigafactory-nevada" &&
			edge.Type == DependencyTypeSuppliesTo {
			foundSupplierFlow = true
		}
		if edge.SourcePath == "/product/component/electronic/manufacturing/ev-battery-cells" &&
			edge.TargetPath == "/product/component/electronic/assembly/ev-battery-module" &&
			edge.Type == DependencyTypeAssemblesInto {
			foundProductTransition = true
		}
	}
	if !foundDecisionMechanism {
		t.Fatalf("missing decision -> mechanism link")
	}
	if !foundSupplierFlow {
		t.Fatalf("missing supplier flow edge")
	}
	if !foundProductTransition {
		t.Fatalf("missing product transition edge")
	}
}

func TestSpecSimulationRequestsAreBoundToHotPath(t *testing.T) {
	spec := Spec("prevalent-horizon-test")
	seedSet := map[string]struct{}{}
	for _, path := range SeedPaths() {
		seedSet[path] = struct{}{}
	}
	if len(spec.Simulations) < 4 {
		t.Fatalf("expected multiple simulations, got %d", len(spec.Simulations))
	}
	for _, sim := range spec.Simulations {
		if err := sim.Request.Validate(); err != nil {
			t.Fatalf("simulation request %q invalid: %v", sim.ID, err)
		}
		if sim.Request.Signal == nil {
			t.Fatalf("simulation request %q missing signal", sim.ID)
		}
		if len(sim.Request.SeedPaths) == 0 {
			t.Fatalf("simulation request %q missing seed paths", sim.ID)
		}
		for _, path := range sim.Request.SeedPaths {
			if _, ok := seedSet[path]; !ok {
				t.Fatalf("simulation request %q uses unknown seed path %q", sim.ID, path)
			}
		}
		if len(sim.Request.DomainModes) != 2 {
			t.Fatalf("simulation request %q must contain supply_chain and policy_impact", sim.ID)
		}
		domainSet := map[string]struct{}{}
		for _, mode := range sim.Request.DomainModes {
			domainSet[mode] = struct{}{}
		}
		if _, ok := domainSet["supply_chain"]; !ok {
			t.Fatalf("simulation request %q missing supply_chain mode: %v", sim.ID, sim.Request.DomainModes)
		}
		if _, ok := domainSet["policy_impact"]; !ok {
			t.Fatalf("simulation request %q missing policy_impact mode: %v", sim.ID, sim.Request.DomainModes)
		}
	}
}
