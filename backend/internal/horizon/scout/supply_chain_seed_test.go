package scout

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
)

func TestFilterEmptySupplyChains(t *testing.T) {
	chains := []agents.SupplyChain{
		{Nodes: []agents.SupplyChainNode{{CanonicalPath: "/org/de/bosch", Name: "Bosch", Type: "ORGANIZATION"}}},
		{Nodes: nil},
		{Nodes: []agents.SupplyChainNode{}},
		{Nodes: []agents.SupplyChainNode{
			{CanonicalPath: "/org/jp/denso", Name: "Denso", Type: "ORGANIZATION"},
			{CanonicalPath: "/product/auto/sensors", Name: "Sensors", Type: "PRODUCT"},
		}},
	}
	filtered := filterEmptySupplyChains(chains)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 non-empty chains, got %d", len(filtered))
	}
	if filtered[0].Nodes[0].CanonicalPath != "/org/de/bosch" {
		t.Errorf("first chain should be bosch, got %s", filtered[0].Nodes[0].CanonicalPath)
	}
	if filtered[1].Nodes[0].CanonicalPath != "/org/jp/denso" {
		t.Errorf("second chain should be denso, got %s", filtered[1].Nodes[0].CanonicalPath)
	}
}

func TestFilterEmptySupplyChains_AllEmpty(t *testing.T) {
	chains := []agents.SupplyChain{
		{Nodes: nil, Confidence: 0.8},
		{Nodes: []agents.SupplyChainNode{}},
	}
	filtered := filterEmptySupplyChains(chains)
	if len(filtered) != 0 {
		t.Fatalf("expected 0 chains after filtering, got %d", len(filtered))
	}
}

func TestBuildSeedSupplyChain(t *testing.T) {
	frag := &models.HorizonFragment{
		CanonicalPath: "/org/de/robert-bosch",
		Type:          models.FragmentTypeOrganization,
		Identifiers:   map[string]string{"legalName": "Robert Bosch GmbH"},
	}
	seed := buildSeedSupplyChain(frag)
	if seed == nil {
		t.Fatal("expected non-nil seed chain")
	}
	if len(seed.Nodes) != 1 {
		t.Fatalf("expected 1 node in seed chain, got %d", len(seed.Nodes))
	}
	n := seed.Nodes[0]
	if n.CanonicalPath != "/org/de/robert-bosch" {
		t.Errorf("unexpected canonicalPath: %s", n.CanonicalPath)
	}
	if n.Name != "Robert Bosch GmbH" {
		t.Errorf("unexpected name: %s", n.Name)
	}
	if n.Type != "ORGANIZATION" {
		t.Errorf("unexpected type: %s", n.Type)
	}
	if seed.Confidence >= 0.2 {
		t.Errorf("seed confidence should be very low, got %.2f", seed.Confidence)
	}
}

func TestBuildSeedSupplyChain_FallbackName(t *testing.T) {
	frag := &models.HorizonFragment{
		CanonicalPath: "/org/de/robert-bosch",
		Type:          models.FragmentTypeOrganization,
	}
	seed := buildSeedSupplyChain(frag)
	if seed == nil {
		t.Fatal("expected non-nil seed chain")
	}
	if seed.Nodes[0].Name == "" {
		t.Error("seed node name should be derived from path, got empty")
	}
}

func TestBuildSeedSupplyChain_NilPath(t *testing.T) {
	frag := &models.HorizonFragment{
		Type: models.FragmentTypeOrganization,
	}
	seed := buildSeedSupplyChain(frag)
	if seed != nil {
		t.Error("expected nil seed for fragment with empty canonical path")
	}
}
