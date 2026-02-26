package http

import "testing"

func TestNormalizeSourceCategoryAssignment_PrimaryAndDedup(t *testing.T) {
	primary := "medicine/pharma"
	paths, primaryPath, err := normalizeSourceCategoryAssignment(
		[]string{"/source/medicine/pharma", "/source/medicine", "/source/medicine/pharma"},
		&primary,
	)
	if err != nil {
		t.Fatalf("normalizeSourceCategoryAssignment returned error: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 normalized paths, got %d (%v)", len(paths), paths)
	}
	if paths[0] != "/source/medicine/pharma" {
		t.Fatalf("expected primary path first, got %v", paths)
	}
	if primaryPath == nil || *primaryPath != "/source/medicine/pharma" {
		t.Fatalf("unexpected primary path: %+v", primaryPath)
	}
}

func TestNormalizeSourceCategoryAssignment_InvalidCategory(t *testing.T) {
	_, _, err := normalizeSourceCategoryAssignment([]string{"/source/not-allowed/foo"}, nil)
	if err == nil {
		t.Fatalf("expected validation error for invalid source category")
	}
}

func TestNormalizeSourceCategoryAssignment_RespectsMaxAfterPrimary(t *testing.T) {
	paths := []string{
		"/source/medicine/pharma",
		"/source/medicine/biotech",
		"/source/technology/semiconductors",
		"/source/economy/inflation",
		"/source/energy/oil",
		"/source/geopolitics/sanctions",
		"/source/logistics/ports",
		"/source/transport/aviation",
	}
	primary := "/source/finance/banking"

	_, _, err := normalizeSourceCategoryAssignment(paths, &primary)
	if err == nil {
		t.Fatalf("expected error when primary pushes source categories over max")
	}
}

func TestResolveSourceCategoryUpdate_ReplacesPathsAndResetsPrimary(t *testing.T) {
	existingPaths := []string{"/source/medicine/pharma", "/source/medicine/biotech"}
	existingPrimary := "/source/medicine/pharma"
	reqPaths := []string{"/source/technology/semiconductors"}

	paths, primary := resolveSourceCategoryUpdate(existingPaths, &existingPrimary, &reqPaths, nil)
	if len(paths) != 1 || paths[0] != "/source/technology/semiconductors" {
		t.Fatalf("expected replacement paths, got %v", paths)
	}
	if primary != nil {
		t.Fatalf("expected primary reset when only paths are updated, got %v", *primary)
	}
}

func TestNormalizeSourceGeoFacetAssignment(t *testing.T) {
	normalized, err := normalizeSourceGeoFacetAssignment([]string{" US ", "global", "eu", "us", "NATO"})
	if err != nil {
		t.Fatalf("normalizeSourceGeoFacetAssignment returned error: %v", err)
	}
	if len(normalized) != 4 {
		t.Fatalf("expected 4 normalized geo facets, got %d (%v)", len(normalized), normalized)
	}
}

func TestNormalizeSourceGeoFacetAssignment_Invalid(t *testing.T) {
	_, err := normalizeSourceGeoFacetAssignment([]string{"not-a-geo"})
	if err == nil {
		t.Fatalf("expected validation error for invalid source geo facet")
	}
}

func TestNormalizeSourceFormatFacetAssignment(t *testing.T) {
	normalized, err := normalizeSourceFormatFacetAssignment([]string{"wire", "Wire Service", "think tank"})
	if err != nil {
		t.Fatalf("normalizeSourceFormatFacetAssignment returned error: %v", err)
	}
	if len(normalized) != 2 {
		t.Fatalf("expected 2 normalized format facets, got %d (%v)", len(normalized), normalized)
	}
	if normalized[0] != "wire" || normalized[1] != "think-tank" {
		t.Fatalf("unexpected normalized format facets: %v", normalized)
	}
}

func TestResolveFacetArrayUpdate(t *testing.T) {
	existing := []string{"us", "eu"}
	next := resolveFacetArrayUpdate(existing, nil)
	if len(next) != 2 || next[0] != "us" {
		t.Fatalf("expected existing facets when req is nil, got %v", next)
	}

	req := []string{"global"}
	next = resolveFacetArrayUpdate(existing, &req)
	if len(next) != 1 || next[0] != "global" {
		t.Fatalf("expected replacement facets when req is provided, got %v", next)
	}
}
