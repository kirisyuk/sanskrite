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
