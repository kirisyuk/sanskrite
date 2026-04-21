package kzvat

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

	foundDecisionMechanism := false
	foundProgramRetail := false
	foundCommunityRetail := false
	for _, edge := range spec.Edges {
		if _, ok := allowedEdges[string(edge.Type)]; !ok {
			t.Fatalf("edge type %q is not allowed in policy_impact mode", edge.Type)
		}
		if _, ok := paths[edge.SourcePath]; !ok {
			t.Fatalf("edge source %q is not declared in fragment seeds", edge.SourcePath)
		}
		if _, ok := paths[edge.TargetPath]; !ok {
			t.Fatalf("edge target %q is not declared in fragment seeds", edge.TargetPath)
		}
		if edge.SourcePath == "/decision/taxation/raise-vat-to-16pct-kazakhstan-2026" &&
			edge.TargetPath == "/mechanism/regulatory/vat-rate-increase" &&
			edge.Type == DependencyTypeUsesMechanism {
			foundDecisionMechanism = true
		}
		if edge.SourcePath == "/program/kz/vat-reform-2026" &&
			edge.TargetPath == "/market/kz/consumer/retail" &&
			edge.Type == DependencyTypeImposesCondition {
			foundProgramRetail = true
		}
		if edge.SourcePath == "/community/kz/trade/bazaar-traders" &&
			edge.TargetPath == "/market/kz/consumer/retail" &&
			edge.Type == DependencyTypeParticipatesIn {
			foundCommunityRetail = true
		}
	}
	if !foundDecisionMechanism {
		t.Fatalf("missing decision -> mechanism link")
	}
	if !foundProgramRetail {
		t.Fatalf("missing program -> retail impact link")
	}
	if !foundCommunityRetail {
		t.Fatalf("missing community -> retail substitution link")
	}
}

func TestSpecSimulationRequestsAreBoundToHotPath(t *testing.T) {
	spec := Spec("prevalent-horizon-test")
	seedSet := map[string]struct{}{}
	for _, path := range SeedPaths() {
		seedSet[path] = struct{}{}
	}
	if len(spec.Simulations) < 3 {
		t.Fatalf("expected multiple axis simulations, got %d", len(spec.Simulations))
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
		if len(sim.Request.DomainModes) != 1 || sim.Request.DomainModes[0] != "policy_impact" {
			t.Fatalf("simulation request %q must stay in policy_impact mode", sim.ID)
		}
	}
}
