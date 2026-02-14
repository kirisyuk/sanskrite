package enrich

import (
	"testing"
	"time"

	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
)

func TestEnrichFragmentFromEntityData_SetsCoordinatesFromP625(t *testing.T) {
	frag := &hmodels.HorizonFragment{
		Type: hmodels.FragmentTypeGeoRegion,
		KBRefs: []hmodels.KBReference{
			{KB: "Wikidata", ExternalID: "Q89"},
		},
	}
	ed := &coreontology.WikidataEntityData{
		ID: "Q89",
		PropertiesRaw: map[string]interface{}{
			"P625": map[string]interface{}{
				"latitude":  25.2048,
				"longitude": 55.2708,
			},
		},
	}

	changed, err := EnrichFragmentFromEntityData(frag, ed, time.Now().UTC(), ConservativeWikidataOptions{Overwrite: true})
	if err != nil {
		t.Fatalf("EnrichFragmentFromEntityData() error = %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true")
	}
	if frag.Coordinates == nil {
		t.Fatalf("expected coordinates to be set")
	}
	if !hmodels.IsValidCoordinates(frag.Coordinates.Lat, frag.Coordinates.Lon) {
		t.Fatalf("expected valid coordinates, got %+v", frag.Coordinates)
	}
	if frag.HomeLocation == nil || frag.HomeLocation.ISO6709 == nil || *frag.HomeLocation.ISO6709 == "" {
		t.Fatalf("expected homeLocation.iso6709 to be set")
	}
}

func TestEnrichFragmentFromEntityData_DoesNotOverwriteCoordinatesWhenDisabled(t *testing.T) {
	existingISO := "+24.466670+54.366670/"
	frag := &hmodels.HorizonFragment{
		Type: hmodels.FragmentTypeGeoRegion,
		Coordinates: &hmodels.Coordinates{
			Lat: 24.46667,
			Lon: 54.36667,
		},
		HomeLocation: &hmodels.Location{
			ISO6709: &existingISO,
		},
	}
	ed := &coreontology.WikidataEntityData{
		ID: "Q89",
		PropertiesRaw: map[string]interface{}{
			"P625": map[string]interface{}{
				"latitude":  25.2048,
				"longitude": 55.2708,
			},
		},
	}

	_, err := EnrichFragmentFromEntityData(frag, ed, time.Now().UTC(), ConservativeWikidataOptions{Overwrite: false})
	if err != nil {
		t.Fatalf("EnrichFragmentFromEntityData() error = %v", err)
	}
	if frag.Coordinates == nil {
		t.Fatalf("expected coordinates to remain set")
	}
	if frag.Coordinates.Lat != 24.46667 || frag.Coordinates.Lon != 54.36667 {
		t.Fatalf("expected coordinates to remain unchanged, got %+v", frag.Coordinates)
	}
	if frag.HomeLocation == nil || frag.HomeLocation.ISO6709 == nil || *frag.HomeLocation.ISO6709 != existingISO {
		t.Fatalf("expected ISO6709 to remain unchanged, got %+v", frag.HomeLocation)
	}
}
