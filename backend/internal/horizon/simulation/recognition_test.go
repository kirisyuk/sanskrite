package simulation

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestBuildInputRecognitionFindsSupplyAxisAndObjects(t *testing.T) {
	lens := &horizonmodels.Lens{
		ID:            "lens-fr-auto",
		Name:          "French Automotive Industry",
		OriginalQuery: "French whole automotive industry and its supply chain",
		Scope: []string{
			"/domain/v1/supply-chain/geo/fr/industry/automotive",
			"/org/fr/renault-group-sa",
			"/org/fr/michelin-sca",
			"/product/component/electronic/manufacturing/ev-battery-cells",
		},
		DomainModes: []string{"supply_chain", "market"},
		DomainQueries: []horizonmodels.LensDomainQuery{
			{CanonicalPath: "/domain/v1/supply-chain/geo/fr/industry/automotive", ContractType: "supply-chain", Title: "French Automotive Industry Ecosystem and Supply Chain Map"},
		},
		ViewConfig: map[string]interface{}{
			"deferredScopePaths": []interface{}{"/market/fr/industry/automotive", "/org/fr/forvia"},
		},
	}
	req := &horizonmodels.SimulationRunRequest{
		Question: "French whole automotive industry and its supply chain",
		LensID:   lens.ID,
	}
	refs := horizonmodels.SimulationBaselineRefs{
		LensID:        lens.ID,
		ScopePaths:    append([]string{}, lens.Scope...),
		FragmentPaths: append([]string{}, lens.Scope...),
	}
	rec := BuildInputRecognition(req, lens, refs)
	if got := stringValue(rec, "primaryAxis"); got != horizonmodels.AxisPhysicalFlowSupply {
		t.Fatalf("expected primary axis %s, got %s", horizonmodels.AxisPhysicalFlowSupply, got)
	}
	objects := mapSlice(rec["objectCandidates"])
	if len(objects) == 0 {
		t.Fatal("expected object candidates")
	}
	found := false
	for _, item := range objects {
		if stringValue(item, "path") == "/org/fr/renault-group-sa" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected Renault to be recognized, got %#v", objects)
	}
}

func TestBuildInputRecognitionProfilesFocusedObject(t *testing.T) {
	lens := &horizonmodels.Lens{
		ID:    "lens-helium",
		Name:  "Helium · Natural Gas",
		Scope: []string{"/product/finished/mechanical/assembly/helium", "/product/raw/hydrocarbon/extraction/natural-gas"},
	}
	rec := BuildInputRecognition(&horizonmodels.SimulationRunRequest{
		Question: "Identify the main exposure contours and critical objects for Helium · Natural Gas.",
		LensID:   lens.ID,
	}, lens, horizonmodels.SimulationBaselineRefs{
		LensID:        lens.ID,
		ScopePaths:    append([]string{}, lens.Scope...),
		FragmentPaths: append([]string{}, lens.Scope...),
	})
	volume := mapValue(rec["volumeProfile"])
	if got := stringValue(volume, "profile"); got != "focused_object" {
		t.Fatalf("expected focused_object profile, got %s", got)
	}
	if got := stringValue(rec, "primaryAxis"); got != horizonmodels.AxisPhysicalFlowSupply {
		t.Fatalf("expected physical_flow/supply axis for product-only lens, got %s", got)
	}
}

func TestBuildInputRecognitionProfilesSectorNetwork(t *testing.T) {
	lens := &horizonmodels.Lens{
		ID:            "lens-fr-auto",
		Name:          "French Automotive Industry",
		OriginalQuery: "French whole automotive industry and its supply chain",
		Scope: []string{
			"/domain/v1/supply-chain/geo/fr/industry/automotive",
			"/market/fr/industry/automotive",
			"/org/fr/renault-group-sa",
			"/org/fr/michelin-sca",
			"/product/component/electronic/manufacturing/ev-battery-cells",
			"/fac/fr/renault/flins",
		},
		DomainQueries: []horizonmodels.LensDomainQuery{
			{CanonicalPath: "/domain/v1/supply-chain/geo/fr/industry/automotive", ContractType: "supply-chain", Title: "French Automotive Industry Ecosystem and Supply Chain Map"},
		},
	}
	rec := BuildInputRecognition(&horizonmodels.SimulationRunRequest{
		Question:  lens.OriginalQuery,
		LensID:    lens.ID,
		ProjectID: "proj",
	}, lens, horizonmodels.SimulationBaselineRefs{
		LensID:        lens.ID,
		ScopePaths:    append([]string{}, lens.Scope...),
		FragmentPaths: append([]string{}, lens.Scope...),
	})
	volume := mapValue(rec["volumeProfile"])
	if got := stringValue(volume, "profile"); got != "sector_network" {
		t.Fatalf("expected sector_network profile, got %s", got)
	}
}
