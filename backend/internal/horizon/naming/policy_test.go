package naming

import "testing"

func TestNormalizeLegalName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"Taiwan Semiconductor Manufacturing Company Limited (TSMC)", "taiwan semiconductor manufacturing"},
		{"T-Bank (formerly Tinkoff) PJSC", "t bank"},
		{"Saudi Arabia Central Bank (SAMA)", "saudi arabia central bank"},
	}
	for _, tt := range tests {
		got := NormalizeLegalName(tt.in)
		if got != tt.want {
			t.Fatalf("NormalizeLegalName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestIsLatinScript(t *testing.T) {
	if !IsLatinScript("Sao Paulo") {
		t.Fatalf("expected Latin script for Sao Paulo")
	}
	if !IsLatinScript("São Paulo") {
		t.Fatalf("expected Latin script for São Paulo")
	}
	if IsLatinScript("المصرف المركزي") {
		t.Fatalf("expected non-Latin script to be rejected")
	}
}

func TestNormalizeFragmentNames(t *testing.T) {
	labels := map[string]string{"en": "المصرف المركزي"}
	ids := map[string]string{"legalName": "المصرف المركزي"}

	nLabels, nIDs := NormalizeFragmentNames("/org/sa/saudi-central-bank", labels, ids)
	if nLabels["en"] != "Saudi Central Bank" {
		t.Fatalf("labels.en = %q, want %q", nLabels["en"], "Saudi Central Bank")
	}
	if nIDs["legalName"] != "المصرف المركزي" {
		t.Fatalf("legalName should keep native script, got %q", nIDs["legalName"])
	}
	if nIDs["shortName"] == "" {
		t.Fatalf("shortName should be populated")
	}
}

func TestNormalizeRelationNames(t *testing.T) {
	name, legal, short := NormalizeRelationNames("ORGANIZATION", "T-Bank (formerly Tinkoff)", "", "")
	if name != "T-Bank" {
		t.Fatalf("targetName = %q, want %q", name, "T-Bank")
	}
	if legal == "" || short == "" {
		t.Fatalf("expected legal and short names to be filled")
	}
}
