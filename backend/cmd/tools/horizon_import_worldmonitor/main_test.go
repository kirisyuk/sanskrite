package main

import "testing"

func TestNormalizeImportedIntelSourceURL_OverrideByName(t *testing.T) {
	got := normalizeImportedIntelSourceURL("CSIS", "https://www.csis.org/analysis?type=analysis")
	if got == "" {
		t.Fatalf("expected normalized URL, got empty")
	}
	if got == "https://www.csis.org/analysis?type=analysis" {
		t.Fatalf("expected fallback URL, got original %q", got)
	}
}

func TestNormalizeImportedIntelSourceURL_PreservesHealthyURL(t *testing.T) {
	in := "https://www.defenseone.com/rss/all/"
	got := normalizeImportedIntelSourceURL("Defense One", in)
	if got != in {
		t.Fatalf("expected URL to be unchanged, got %q", got)
	}
}

func TestMergeIntelSources_DeduplicatesByURL(t *testing.T) {
	primary := []wmIntelSource{
		{Name: "A", URL: "https://www.defenseone.com/rss/all/", Type: "defense"},
	}
	extra := []wmIntelSource{
		{Name: "A2", URL: "https://www.defenseone.com/rss/all/", Type: "defense"},
		{Name: "B", URL: "https://www.bbc.com/rss.xml", Type: "mainstream"},
	}
	got := mergeIntelSources(primary, extra)
	if len(got) != 2 {
		t.Fatalf("expected 2 unique sources, got %d", len(got))
	}
}
