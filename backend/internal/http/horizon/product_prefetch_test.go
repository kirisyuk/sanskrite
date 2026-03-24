package horizon

import "testing"

func TestProductPrefetchQueryVariants(t *testing.T) {
	variants := productPrefetchQueryVariants("lithium ore")
	want := []string{"lithium ore", "Lithium Ore"}
	if len(variants) != len(want) {
		t.Fatalf("variants=%v want=%v", variants, want)
	}
	for i, item := range want {
		if variants[i] != item {
			t.Fatalf("variants=%v want=%v", variants, want)
		}
	}
}

func TestProductPrefetchQueryVariantsPreserveDistinctCaseForms(t *testing.T) {
	variants := productPrefetchQueryVariants("Lithium")
	want := []string{"Lithium", "lithium"}
	if len(variants) != len(want) {
		t.Fatalf("variants=%v want=%v", variants, want)
	}
	for i, item := range want {
		if variants[i] != item {
			t.Fatalf("variants=%v want=%v", variants, want)
		}
	}
}

