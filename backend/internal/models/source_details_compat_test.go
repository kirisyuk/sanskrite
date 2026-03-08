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

func TestSourceDetailsNormalizeRoutingFields(t *testing.T) {
	scope := " GLOBAL_SYSTEM "
	mode := " HEURISTIC "
	ds := &DataSource{
		Details: SourceDetails{
			SourceScope:    &scope,
			ProcessingMode: &mode,
		},
	}

	ds.SyncCompatForWrite()

	if ds.Details.SourceScope == nil || *ds.Details.SourceScope != SourceScopeGlobalSystem {
		t.Fatalf("expected normalized sourceScope=%q, got %+v", SourceScopeGlobalSystem, ds.Details.SourceScope)
	}
	if ds.Details.ProcessingMode == nil || *ds.Details.ProcessingMode != SourceProcessingModeHeuristic {
		t.Fatalf("expected normalized processingMode=%q, got %+v", SourceProcessingModeHeuristic, ds.Details.ProcessingMode)
	}
}

func TestSourceRouting_InferLegacyHeuristicSystemSource(t *testing.T) {
	ds := &DataSource{
		Kind:     SourceKindRSS,
		TenantID: "",
		Details: SourceDetails{
			DisableLLMProcessing: cloneBoolPointer(boolPtr(true)),
		},
	}

	if got := SourceScopeValue(ds); got != SourceScopeGlobalSystem {
		t.Fatalf("expected inferred sourceScope=%q, got %q", SourceScopeGlobalSystem, got)
	}
	if got := SourceProcessingModeValue(ds); got != SourceProcessingModeHeuristic {
		t.Fatalf("expected inferred processingMode=%q, got %q", SourceProcessingModeHeuristic, got)
	}
	if !ShouldDisableLLMProcessing(ds) {
		t.Fatalf("expected heuristic source to disable LLM processing")
	}
}

func TestSourceRouting_ExplicitCanvasModeWins(t *testing.T) {
	scope := SourceScopeGlobalSystem
	mode := SourceProcessingModeCanvasLLM
	ds := &DataSource{
		Kind:     SourceKindRSS,
		TenantID: "",
		Details: SourceDetails{
			SourceScope:          &scope,
			ProcessingMode:       &mode,
			DisableLLMProcessing: boolPtr(true),
		},
	}

	if got := SourceProcessingModeValue(ds); got != SourceProcessingModeCanvasLLM {
		t.Fatalf("expected explicit processing mode to win, got %q", got)
	}
	if ShouldDisableLLMProcessing(ds) {
		t.Fatalf("explicit canvas mode must not disable LLM processing")
	}
}

func boolPtr(v bool) *bool { return &v }
