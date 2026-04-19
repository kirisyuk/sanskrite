package repo

import (
	"context"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/productdict"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

func TestApplyProductRelationDictionarySwapsCanonicalQualifiers(t *testing.T) {
	ctx := context.Background()
	dict := productdict.NewRuntime(nil)
	if err := dict.SyncTransformPairWrite(
		ctx,
		"global",
		"/product/finished/mechanical/assembly/helium",
		"/product/finished/mechanical/assembly/mri-scanner",
		"usage",
		true,
	); err != nil {
		t.Fatalf("SyncTransformPairWrite() error = %v", err)
	}

	repo := (&DependencyEdgesRepository{}).WithProductDictionary(dict)
	edge := &models.DependencyEdge{
		TenantID:         "global",
		SourceFragmentID: "mri-id",
		TargetFragmentID: "helium-id",
		Relation:         ah.RelationName("transforms_into"),
		Qualifiers: map[string]interface{}{
			"sourceCanonicalPath": "/product/finished/mechanical/assembly/mri-scanner",
			"targetCanonicalPath": "/product/finished/mechanical/assembly/helium",
		},
	}

	sourcePath, targetPath, swapped := repo.applyProductRelationDictionary(
		ctx,
		edge,
		string(ah.FragmentClassProduct),
		string(ah.FragmentClassProduct),
		"/product/finished/mechanical/assembly/mri-scanner",
		"/product/finished/mechanical/assembly/helium",
	)
	if !swapped {
		t.Fatalf("expected swapped=true")
	}
	if sourcePath != "/product/finished/mechanical/assembly/helium" || targetPath != "/product/finished/mechanical/assembly/mri-scanner" {
		t.Fatalf("unexpected normalized paths: %q -> %q", sourcePath, targetPath)
	}
	if edge.SourceFragmentID != "helium-id" || edge.TargetFragmentID != "mri-id" {
		t.Fatalf("unexpected fragment ids: %q -> %q", edge.SourceFragmentID, edge.TargetFragmentID)
	}
	if edge.Relation != ah.RelationName("used_in") {
		t.Fatalf("type=%q, want USED_IN", edge.Relation)
	}
	if got, _ := edge.Qualifiers["sourceCanonicalPath"].(string); got != "/product/finished/mechanical/assembly/helium" {
		t.Fatalf("sourceCanonicalPath=%q, want helium", got)
	}
	if got, _ := edge.Qualifiers["targetCanonicalPath"].(string); got != "/product/finished/mechanical/assembly/mri-scanner" {
		t.Fatalf("targetCanonicalPath=%q, want mri-scanner", got)
	}
	if got, _ := edge.Qualifiers["productRelationKind"].(string); got != "usage" {
		t.Fatalf("productRelationKind=%q, want usage", got)
	}
}

func TestApplyProductRelationDictionary_SkipsNonProductEndpoints(t *testing.T) {
	ctx := context.Background()
	dict := productdict.NewRuntime(nil)
	if err := dict.SyncTransformPairWrite(
		ctx,
		"global",
		"/product/raw/mineral/extraction/iron-ore",
		"/product/finished/chemical/refining/iron-pellet",
		"transformation",
		true,
	); err != nil {
		t.Fatalf("SyncTransformPairWrite() error = %v", err)
	}

	repo := (&DependencyEdgesRepository{}).WithProductDictionary(dict)
	edge := &models.DependencyEdge{
		TenantID:         "global",
		SourceFragmentID: "org-a",
		TargetFragmentID: "org-b",
		Relation:         ah.RelationName("transforms_into"),
	}

	sourcePath, targetPath, swapped := repo.applyProductRelationDictionary(
		ctx,
		edge,
		string(ah.FragmentClassOrg),
		string(ah.FragmentClassProduct),
		"/org/us/example-miner",
		"/product/raw/mineral/extraction/iron-ore",
	)
	if swapped {
		t.Fatal("expected swapped=false for non-product endpoints")
	}
	if sourcePath != "/org/us/example-miner" || targetPath != "/product/raw/mineral/extraction/iron-ore" {
		t.Fatalf("unexpected paths after no-op normalization: %q -> %q", sourcePath, targetPath)
	}
	if edge.Relation != ah.RelationName("transforms_into") {
		t.Fatalf("edge type changed unexpectedly: %s", edge.Relation)
	}
	if len(edge.Qualifiers) != 0 {
		t.Fatalf("expected no qualifiers to be added for non-product endpoints, got %v", edge.Qualifiers)
	}
}

func TestMergeEdgeForIdempotentUpsertOverwritesCanonicalRelationQualifiers(t *testing.T) {
	dst := &models.DependencyEdge{
		Qualifiers: map[string]interface{}{
			"sourceCanonicalPath":                   "/product/finished/chemical/refining/niobium-oxide",
			"targetCanonicalPath":                   "/product/raw/mineral/extraction/niobium-ore",
			"productRelationKind":                   "transformation",
			"normalizedByProductRelationDictionary": false,
			"repairSource":                          "old",
			"repairSourceMode":                      "legacy",
		},
	}
	src := &models.DependencyEdge{
		Qualifiers: map[string]interface{}{
			"sourceCanonicalPath":                   "/product/raw/mineral/extraction/niobium-ore",
			"targetCanonicalPath":                   "/product/finished/chemical/refining/niobium-oxide",
			"productRelationKind":                   "transformation",
			"normalizedByProductRelationDictionary": true,
			"repairSource":                          "tool:horizon_repair_supply_chain_product_relations",
			"repairSourceMode":                      "dictionary",
		},
	}

	if !mergeEdgeForIdempotentUpsert(dst, src) {
		t.Fatal("expected mergeEdgeForIdempotentUpsert to report changes")
	}
	if got := dst.Qualifiers["sourceCanonicalPath"]; got != "/product/raw/mineral/extraction/niobium-ore" {
		t.Fatalf("sourceCanonicalPath=%v, want ore", got)
	}
	if got := dst.Qualifiers["targetCanonicalPath"]; got != "/product/finished/chemical/refining/niobium-oxide" {
		t.Fatalf("targetCanonicalPath=%v, want oxide", got)
	}
	if got := dst.Qualifiers["normalizedByProductRelationDictionary"]; got != true {
		t.Fatalf("normalizedByProductRelationDictionary=%v, want true", got)
	}
	if got := dst.Qualifiers["repairSourceMode"]; got != "dictionary" {
		t.Fatalf("repairSourceMode=%v, want dictionary", got)
	}
}
