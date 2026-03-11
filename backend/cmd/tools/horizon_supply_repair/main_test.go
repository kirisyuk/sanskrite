package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractProductRef(t *testing.T) {
	t.Run("camelCase", func(t *testing.T) {
		got := extractProductRef(map[string]interface{}{"productRef": "/product/component/electronic/manufacturing/battery-cell"})
		if got != "/product/component/electronic/manufacturing/battery-cell" {
			t.Fatalf("extractProductRef(camelCase) = %q", got)
		}
	})

	t.Run("snakeCase", func(t *testing.T) {
		got := extractProductRef(map[string]interface{}{"product_ref": "/product/raw/hydrocarbon/extraction/crude-oil"})
		if got != "/product/raw/hydrocarbon/extraction/crude-oil" {
			t.Fatalf("extractProductRef(snakeCase) = %q", got)
		}
	})

	t.Run("ignores non product paths", func(t *testing.T) {
		got := extractProductRef(map[string]interface{}{"productRef": "/org/de/bmw-ag"})
		if got != "" {
			t.Fatalf("extractProductRef(nonProduct) = %q, want empty", got)
		}
	})
}

func TestNormalizeEvidence(t *testing.T) {
	ev := normalizeEvidence(supplyEvidence{
		SupplierRef:      " /org/de/bmw-ag ",
		ProductRef:       " /product/component/electronic/manufacturing/battery-cell ",
		CustomerRef:      " /fac/de/bmw/munich-plant ",
		TransportViaRefs: []string{" /fac/de/port-of-hamburg ", "/fac/de/port-of-hamburg"},
		Confidence:       0,
	})

	if ev.Key == "" {
		t.Fatal("normalizeEvidence should produce a triad key")
	}
	if ev.Confidence != 0.5 {
		t.Fatalf("normalizeEvidence confidence = %v, want default 0.5", ev.Confidence)
	}
	if len(ev.TransportViaRefs) != 1 || ev.TransportViaRefs[0] != "/fac/de/port-of-hamburg" {
		t.Fatalf("normalizeEvidence transportVia = %#v", ev.TransportViaRefs)
	}
}

func TestMatchesProductQualifier(t *testing.T) {
	if !matchesProductQualifier(map[string]interface{}{"product_ref": "/product/raw/metal/extraction/nickel-ore"}, "/product/raw/metal/extraction/nickel-ore") {
		t.Fatal("matchesProductQualifier should match snake_case product attach")
	}
	if matchesProductQualifier(map[string]interface{}{"productRef": "/product/component/electronic/manufacturing/camera-module"}, "/product/component/electronic/manufacturing/oled-display-panel") {
		t.Fatal("matchesProductQualifier should reject different products")
	}
}

func TestSummarizeTopAnchors(t *testing.T) {
	evidence := map[string]*supplyEvidence{
		"a": {
			SupplierRef:         "/org/jp/panasonic",
			SupplierFacilityRef: "/fac/us/gigafactory-nevada",
			ProductRef:          "/product/component/electronic/assembly/ev-battery-cell",
			CustomerRef:         "/org/us/tesla",
			CustomerFacilityRef: "/fac/us/fremont-factory",
		},
		"b": {
			SupplierRef: "/org/jp/panasonic",
			ProductRef:  "/product/component/electronic/assembly/ev-battery-cell",
			CustomerRef: "/org/de/bmw",
		},
	}

	rows := summarizeTopAnchors(evidence, 3, "both")
	if len(rows) != 3 {
		t.Fatalf("len(rows)=%d, want 3", len(rows))
	}
	if rows[0].CanonicalPath != "/org/jp/panasonic" || rows[0].Count != 2 {
		t.Fatalf("top row=%+v", rows[0])
	}
	if len(rows[0].Roles) != 1 || rows[0].Roles[0] != "supplier" {
		t.Fatalf("roles=%v, want supplier", rows[0].Roles)
	}

	customerRows := summarizeTopAnchors(evidence, 10, "customer")
	for _, row := range customerRows {
		for _, role := range row.Roles {
			if role != "customer" && role != "customer_facility" {
				t.Fatalf("unexpected customer role %q in %+v", role, row)
			}
		}
	}
}

func TestLoadSeedRefs_MergesCSVAndFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "refs.txt")
	if err := os.WriteFile(path, []byte("/org/us/tesla\n/product/component/electronic/manufacturing/battery-cell\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	refs, err := loadSeedRefs(path, "/org/us/tesla,/org/jp/panasonic")
	if err != nil {
		t.Fatalf("loadSeedRefs error: %v", err)
	}
	if len(refs) != 3 {
		t.Fatalf("len(refs)=%d, want 3 (%v)", len(refs), refs)
	}
}

func TestShouldEmitProgress(t *testing.T) {
	if !shouldEmitProgress(10, 100, 10) {
		t.Fatal("expected emit at interval")
	}
	if shouldEmitProgress(9, 100, 10) {
		t.Fatal("did not expect emit before interval")
	}
	if !shouldEmitProgress(100, 100, 10) {
		t.Fatal("expected emit at completion")
	}
}
