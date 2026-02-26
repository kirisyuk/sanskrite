package models

import "testing"

func TestSourceDetailsNormalizeLegacyWorldMonitorFields(t *testing.T) {
	mode := "ontology_fact"
	cat := "conflict"
	d := SourceDetails{
		WorldMonitorCategoryLegacy:   &cat,
		WorldMonitorIngestModeLegacy: &mode,
	}

	d.NormalizeHorizonMonitorAliases()

	if d.HorizonMonitorCategory == nil || *d.HorizonMonitorCategory != cat {
		t.Fatalf("expected HorizonMonitorCategory=%q from legacy, got %+v", cat, d.HorizonMonitorCategory)
	}
	if d.HorizonMonitorIngestMode == nil || *d.HorizonMonitorIngestMode != mode {
		t.Fatalf("expected HorizonMonitorIngestMode=%q from legacy, got %+v", mode, d.HorizonMonitorIngestMode)
	}
}

func TestSourceDetailsSyncWritesLegacyAliases(t *testing.T) {
	signal := "risk:protest"
	d := SourceDetails{
		HorizonMonitorSignalType: &signal,
	}

	d.SyncHorizonMonitorAliases()

	if d.WorldMonitorSignalTypeLegacy == nil || *d.WorldMonitorSignalTypeLegacy != signal {
		t.Fatalf("expected legacy worldmonitorSignalType=%q, got %+v", signal, d.WorldMonitorSignalTypeLegacy)
	}
}

func TestDataSourceSyncCompatForWriteNormalizesKind(t *testing.T) {
	ds := &DataSource{
		Kind: SourceKindWorldMonitorLegacy,
	}
	ds.SyncCompatForWrite()
	if ds.Kind != SourceKindHorizonFeed {
		t.Fatalf("expected canonical kind %q, got %q", SourceKindHorizonFeed, ds.Kind)
	}
}

func TestDataSourceSyncCompatForWriteNormalizesSourceCategories(t *testing.T) {
	primary := " /source/medicine/biotech "
	ds := &DataSource{
		SourceCategoryPaths:       []string{"", "/source/medicine/pharma", "/source/medicine/pharma", " /source/medicine/biotech "},
		PrimarySourceCategoryPath: &primary,
		SourceGeoFacets:           []string{" us ", "eu", "us"},
		SourceFormatFacets:        []string{"wire", " wire ", "gov"},
	}

	ds.SyncCompatForWrite()

	if len(ds.SourceCategoryPaths) != 2 {
		t.Fatalf("expected 2 unique category paths, got %d (%v)", len(ds.SourceCategoryPaths), ds.SourceCategoryPaths)
	}
	if ds.PrimarySourceCategoryPath == nil || *ds.PrimarySourceCategoryPath != "/source/medicine/biotech" {
		t.Fatalf("expected normalized primary path, got %+v", ds.PrimarySourceCategoryPath)
	}
	if ds.SourceCategoryPaths[0] != "/source/medicine/biotech" {
		t.Fatalf("expected primary path to be first, got %v", ds.SourceCategoryPaths)
	}
	if len(ds.SourceGeoFacets) != 2 || ds.SourceGeoFacets[0] != "us" || ds.SourceGeoFacets[1] != "eu" {
		t.Fatalf("expected normalized geo facets, got %v", ds.SourceGeoFacets)
	}
	if len(ds.SourceFormatFacets) != 2 || ds.SourceFormatFacets[0] != "wire" || ds.SourceFormatFacets[1] != "gov" {
		t.Fatalf("expected normalized format facets, got %v", ds.SourceFormatFacets)
	}
}
