package merge

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestAcronymFromFullName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"Abu Dhabi Agriculture And Food Safety Authority", "ADAFSA"},
		{"Abu Dhabi Agriculture and Food Safety Authority", "ADAFSA"},
		{"Federal Bureau of Investigation", "FBI"},
		{"United Nations", "UN"},
	}
	for _, tt := range tests {
		got := AcronymFromFullName(tt.name)
		if got != tt.want {
			t.Errorf("AcronymFromFullName(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestLettersOnly(t *testing.T) {
	if got := LettersOnly("Adafsa"); got != "ADAFSA" {
		t.Errorf("LettersOnly(Adafsa) = %q, want ADAFSA", got)
	}
}

func TestIsAbbreviationOf(t *testing.T) {
	if !IsAbbreviationOf("Adafsa", "Abu Dhabi Agriculture And Food Safety Authority") {
		t.Error("Adafsa should be abbreviation of Abu Dhabi Agriculture And Food Safety Authority")
	}
	if IsAbbreviationOf("Adafsa", "Some Other Name") {
		t.Error("Adafsa should NOT be abbreviation of Some Other Name")
	}
}

func TestIsLegalNameSuffixContainment(t *testing.T) {
	// Executive Council -> Abu Dhabi Executive Council (user's case)
	if !IsLegalNameSuffixContainment("Executive Council", "Abu Dhabi Executive Council") {
		t.Error("Executive Council should be suffix containment of Abu Dhabi Executive Council")
	}
	// Reject too short/generic
	if IsLegalNameSuffixContainment("Council", "Abu Dhabi Executive Council") {
		t.Error("Council alone should be rejected (too generic)")
	}
	// Reject non-suffix
	if IsLegalNameSuffixContainment("Abu Dhabi", "Abu Dhabi Executive Council") {
		t.Error("Abu Dhabi is prefix, not suffix - should not match as suffix containment")
	}
	// Same length or longer shortLabel
	if IsLegalNameSuffixContainment("Abu Dhabi Executive Council", "Abu Dhabi Executive Council") {
		t.Error("same length should not match")
	}
}

func TestGeoPrefix(t *testing.T) {
	if got := GeoPrefix("/gov/ae/adafsa"); got != "/gov/ae/" {
		t.Errorf("GeoPrefix(/gov/ae/adafsa) = %q, want /gov/ae/", got)
	}
	if got := GeoPrefix("/gov/ae/abu-dhabi-agriculture-and-food-safety-authority"); got != "/gov/ae/" {
		t.Errorf("GeoPrefix(full) = %q, want /gov/ae/", got)
	}
}

func TestFindAbbreviationPairs(t *testing.T) {
	abbrev := &horizonmodels.HorizonFragment{
		ID:            "abbrev",
		CanonicalPath:  "/gov/ae/adafsa",
		Identifiers:    map[string]string{"legalName": "Adafsa", "shortName": "Adafsa"},
		Labels:         map[string]string{"en": "Adafsa"},
		MarketSegments: []string{"government · ae"},
	}
	full := &horizonmodels.HorizonFragment{
		ID:           "full",
		CanonicalPath: "/gov/ae/abu-dhabi-agriculture-and-food-safety-authority",
		Identifiers:   map[string]string{"legalName": "Abu Dhabi Agriculture And Food Safety Authority"},
		Labels:        map[string]string{"en": "Abu Dhabi Agriculture And Food Safety Authority"},
	}
	pairs := FindAbbreviationPairs([]*horizonmodels.HorizonFragment{abbrev, full})
	if len(pairs) != 1 {
		t.Fatalf("expected 1 pair, got %d", len(pairs))
	}
	if pairs[0].Abbrev != abbrev || pairs[0].Full != full {
		t.Error("expected abbrev->full pair")
	}
}
