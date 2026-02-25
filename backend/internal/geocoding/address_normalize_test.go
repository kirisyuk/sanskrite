package geocoding

import (
	"testing"
)

func TestNormalizeAddressForCache(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"Moscow, Russia", "moscow, ru"},
		{"Moscow, RU", "moscow, ru"},
		{"Moscow, Russian Federation", "moscow, ru"},
		{"Dubai, UAE", "dubai, ae"},
		{"Dubai, United Arab Emirates", "dubai, ae"},
		{"Houston, TX, USA", "houston, tx, us"},
		{"Houston, TX, United States", "houston, tx, us"},
		{"London, UK", "london, gb"},
		{"London, United Kingdom", "london, gb"},
		{"  Paris  ,  France   ", "paris, fr"},
		{"Berlin, Germany", "berlin, de"},
		{"Tokyo, Japan", "tokyo, jp"},
		{"Moscow", "moscow"},
		{"", ""},
	}
	for _, tt := range tests {
		got := normalizeAddressForCache(tt.in)
		if got != tt.want {
			t.Errorf("normalizeAddressForCache(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestAddressVariantsForCacheLookup(t *testing.T) {
	// All variants of "Moscow, Russia" should produce the same primary key
	v1 := addressVariantsForCacheLookup("Moscow, Russia")
	v2 := addressVariantsForCacheLookup("Moscow, RU")
	if len(v1) == 0 || len(v2) == 0 {
		t.Fatal("expected non-empty variants")
	}
	if v1[0] != v2[0] {
		t.Errorf("primary keys should match: %q vs %q", v1[0], v2[0])
	}
	if v1[0] != "moscow, ru" {
		t.Errorf("expected primary 'moscow, ru', got %q", v1[0])
	}
}
