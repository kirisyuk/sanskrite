package main

import (
	"strings"
	"testing"
)

func TestSafeAliasGroupRejectsMixedTaxonomyWithoutFlatAnchor(t *testing.T) {
	products := []productRecord{
		{
			ID:            "a",
			CanonicalPath: "/product/raw/hydrocarbon/extraction/natural-gas",
			PathShape:     "hierarchical",
			PathSlug:      "natural-gas",
			TaxonomySig:   "raw|hydrocarbon|extraction",
		},
		{
			ID:             "b",
			CanonicalPath:  "/product/service/energy/regasification/natural-gas",
			PathShape:      "service",
			PathSlug:       "natural-gas",
			TaxonomySig:    "service|energy|regasification",
			HasServicePath: true,
		},
	}

	if safeAliasGroup(products, []int{0, 1}, "natural gas") {
		t.Fatalf("safeAliasGroup() = true, want false for mixed stages without flat anchor")
	}
}

func TestSafeAliasGroupAcceptsMixedTaxonomyWithFlatAnchor(t *testing.T) {
	products := []productRecord{
		{
			ID:          "a",
			PathShape:   "flat",
			PathSlug:    "semiconductor-wafer",
			TaxonomySig: "",
			HasFlatPath: true,
		},
		{
			ID:              "b",
			PathShape:       "hierarchical",
			PathSlug:        "semiconductor-wafer",
			TaxonomySig:     "component|electronic|manufacturing",
			HasHierarchical: true,
		},
	}

	if !safeAliasGroup(products, []int{0, 1}, "semiconductor wafer") {
		t.Fatalf("safeAliasGroup() = false, want true when a flat anchor exists")
	}
}

func TestNormalizeProductAliasKey(t *testing.T) {
	got := normalizeProductAliasKey("SiC-MOSFET")
	if got != "sic mosfet" {
		t.Fatalf("normalizeProductAliasKey() = %q, want %q", got, "sic mosfet")
	}
}

func TestShouldSkipKnownCandidate_SkipsExistingAndMappedSynonyms(t *testing.T) {
	origSkipKnown := *skipKnownPairs
	origSkipMapped := *skipMappedSyns
	t.Cleanup(func() {
		*skipKnownPairs = origSkipKnown
		*skipMappedSyns = origSkipMapped
	})
	*skipKnownPairs = true
	*skipMappedSyns = true

	products := []productRecord{
		{ID: "a", CanonicalPath: "/product/a"},
		{ID: "b", CanonicalPath: "/product/b"},
	}
	candidate := pairCandidate{LeftIdx: 0, RightIdx: 1}

	if !shouldSkipKnownCandidate(products, candidate, pairKnowledge{
		exactDocIDs: map[string]struct{}{synonymPairDocID("/product/a", "/product/b"): {}},
	}) {
		t.Fatalf("expected candidate to be skipped when exact pair doc exists")
	}

	if !shouldSkipKnownCandidate(products, candidate, pairKnowledge{
		exactDocIDs:        map[string]struct{}{},
		mappedSynonymPaths: map[string]string{"/product/b": "/product/canonical"},
	}) {
		t.Fatalf("expected candidate to be skipped when synonym path is already mapped")
	}

	if shouldSkipKnownCandidate(products, candidate, pairKnowledge{
		exactDocIDs:        map[string]struct{}{},
		mappedSynonymPaths: map[string]string{},
	}) {
		t.Fatalf("expected candidate to remain when no known pair knowledge exists")
	}
}

func TestBuildPairReviewPrompt_EmphasizesTrueMaterialSynonyms(t *testing.T) {
	prompt := buildPairReviewPrompt("[]")

	requiredSnippets := []string{
		"TRUE SYNONYMS of the SAME product/material only",
		"sodium hydroxide = caustic soda",
		"liquefied natural gas = LNG",
		"Class-subclass relationships are NOT synonyms",
		"General-specific relationships are NOT synonyms",
		"alumina vs aluminum",
		"iron ore vs iron ore concentrate",
		"fruit vs pineapple",
		"vehicle vs passenger electric vehicle",
		"Different processing stages are NOT synonyms",
		"ore -> concentrate",
		"precursor -> active material",
		"Return EXACTLY one strict JSON array",
		"\"pairId\": \"<string pair id from input>\"",
		"\"accepted\": <true|false>",
		"Include every input pair exactly once",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("buildPairReviewPrompt() missing snippet %q", snippet)
		}
	}
}
