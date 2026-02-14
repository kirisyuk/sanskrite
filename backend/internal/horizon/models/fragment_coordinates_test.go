package models

import (
	"testing"
	"time"
)

func baseFragmentForCoordinatesTest() *HorizonFragment {
	return &HorizonFragment{
		Type:          FragmentTypeOrganization,
		TenantID:      "global",
		CanonicalPath: "/org/global/coords-test",
		Governance: Governance{
			Owner: "SYSTEM",
		},
	}
}

func TestValidateBackfillsCoordinatesFromHorizonMonitor(t *testing.T) {
	f := baseFragmentForCoordinatesTest()
	f.HorizonMonitor = &HorizonMonitorFragmentData{
		Coordinates: &HorizonMonitorCoordinates{Lat: 48.8566, Lon: 2.3522},
	}

	if err := f.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if f.Coordinates == nil {
		t.Fatalf("Coordinates should be backfilled from horizonMonitor")
	}
	if f.Coordinates.Lat != 48.8566 || f.Coordinates.Lon != 2.3522 {
		t.Fatalf("Coordinates = %+v, want lat=48.8566 lon=2.3522", *f.Coordinates)
	}
}

func TestValidateBackfillsCoordinatesFromLegacyWorldMonitorField(t *testing.T) {
	f := baseFragmentForCoordinatesTest()
	f.WorldMonitorLegacy = &HorizonMonitorFragmentData{
		Coordinates: &HorizonMonitorCoordinates{Lat: 40.7128, Lon: -74.0060},
	}

	if err := f.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if f.HorizonMonitor == nil || f.HorizonMonitor.Coordinates == nil {
		t.Fatalf("HorizonMonitor should be normalized from legacy worldMonitor")
	}
	if f.Coordinates == nil {
		t.Fatalf("Coordinates should be backfilled from legacy worldMonitor")
	}
	if f.Coordinates.Lat != 40.7128 || f.Coordinates.Lon != -74.0060 {
		t.Fatalf("Coordinates = %+v, want lat=40.7128 lon=-74.0060", *f.Coordinates)
	}
}

func TestValidateBackfillsCoordinatesFromISO6709(t *testing.T) {
	f := baseFragmentForCoordinatesTest()
	iso := "+37.7749-122.4194/"
	f.HomeLocation = &Location{ISO6709: &iso}

	if err := f.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if f.Coordinates == nil {
		t.Fatalf("Coordinates should be backfilled from homeLocation.iso6709")
	}
	if f.Coordinates.Lat != 37.7749 || f.Coordinates.Lon != -122.4194 {
		t.Fatalf("Coordinates = %+v, want lat=37.7749 lon=-122.4194", *f.Coordinates)
	}
}

func TestValidateBackfillsCoordinatesFromTrack(t *testing.T) {
	f := baseFragmentForCoordinatesTest()
	f.Track = &TrackFragmentData{
		Kind:       TrackKindSea,
		Lat:        25.276987,
		Lon:        55.296249,
		LastSeenAt: time.Now().UTC(),
	}

	if err := f.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if f.Coordinates == nil {
		t.Fatalf("Coordinates should be backfilled from track")
	}
	if f.Coordinates.Lat != 25.276987 || f.Coordinates.Lon != 55.296249 {
		t.Fatalf("Coordinates = %+v, want lat=25.276987 lon=55.296249", *f.Coordinates)
	}
}

func TestValidateKeepsExplicitCoordinates(t *testing.T) {
	f := baseFragmentForCoordinatesTest()
	f.Coordinates = &Coordinates{Lat: 10, Lon: 20}
	f.HorizonMonitor = &HorizonMonitorFragmentData{
		Coordinates: &HorizonMonitorCoordinates{Lat: 30, Lon: 40},
	}

	if err := f.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if f.Coordinates == nil {
		t.Fatalf("Coordinates should remain set")
	}
	if f.Coordinates.Lat != 10 || f.Coordinates.Lon != 20 {
		t.Fatalf("Coordinates = %+v, want explicit lat=10 lon=20", *f.Coordinates)
	}
}

func TestIsHorizonMonitorGeoEligibleFragment(t *testing.T) {
	f := baseFragmentForCoordinatesTest()
	f.Type = FragmentTypeFacility
	f.Coordinates = &Coordinates{Lat: 55.7558, Lon: 37.6176}
	if !IsHorizonMonitorGeoEligibleFragment(f) {
		t.Fatalf("expected geo-eligible fragment for valid geo + allowed type")
	}
}

func TestIsHorizonMonitorGeoEligibleFragmentRejectsNullIsland(t *testing.T) {
	f := baseFragmentForCoordinatesTest()
	f.Type = FragmentTypeFacility
	f.Coordinates = &Coordinates{Lat: 0, Lon: 0}
	if IsHorizonMonitorGeoEligibleFragment(f) {
		t.Fatalf("null-island coordinates must be rejected")
	}
}
