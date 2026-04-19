package criticalchainview

import (
	"context"
	"testing"
)

func TestNewServiceNilReposListMaterialsFromConfig(t *testing.T) {
	s := NewService(nil, nil)
	list, err := s.ListMaterials(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 {
		t.Fatal("expected embedded YAML materials")
	}
}

func TestNewServiceNilReposEmptyOverlays(t *testing.T) {
	s := NewService(nil, nil)
	routes, err := s.ListRoutes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if routes == nil || len(routes) != 0 {
		t.Fatalf("expected empty routes, got %v", routes)
	}
	zones, err := s.ListConflictZones(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if zones == nil || len(zones) != 0 {
		t.Fatalf("expected empty zones, got %v", zones)
	}
}
