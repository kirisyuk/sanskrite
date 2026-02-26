package taxonomy

import "testing"

func TestNormalizeSourceGeoFacet(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{in: "US", want: "us", ok: true},
		{in: "loc/DE", want: "de", ok: true},
		{in: "source/eu/technology/ai", want: "eu", ok: true},
		{in: "international", want: "int", ok: true},
		{in: "NATO", want: "nato", ok: true},
		{in: "united-nations", want: "un", ok: true},
		{in: "not-a-geo-token", want: "", ok: false},
	}

	for _, tt := range tests {
		got, ok := NormalizeSourceGeoFacet(tt.in)
		if ok != tt.ok {
			t.Fatalf("NormalizeSourceGeoFacet(%q) ok=%v, want %v", tt.in, ok, tt.ok)
		}
		if got != tt.want {
			t.Fatalf("NormalizeSourceGeoFacet(%q)=%q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNormalizeSourceFormatFacet(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{in: "wire", want: "wire", ok: true},
		{in: "Wire Service", want: "wire", ok: true},
		{in: "think tank", want: "think-tank", ok: true},
		{in: "international organization", want: "intl-org", ok: true},
		{in: "unknown-format", want: "", ok: false},
	}

	for _, tt := range tests {
		got, ok := NormalizeSourceFormatFacet(tt.in)
		if ok != tt.ok {
			t.Fatalf("NormalizeSourceFormatFacet(%q) ok=%v, want %v", tt.in, ok, tt.ok)
		}
		if got != tt.want {
			t.Fatalf("NormalizeSourceFormatFacet(%q)=%q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNormalizeSourceGeoFacets_Dedup(t *testing.T) {
	in := []string{"US", "us", "eu", "EU", "nato"}
	got := NormalizeSourceGeoFacets(in)
	if len(got) != 3 {
		t.Fatalf("expected 3 normalized values, got %d (%v)", len(got), got)
	}
}

func TestNormalizeSourceFormatFacets_Dedup(t *testing.T) {
	in := []string{"wire", "Wire Service", "gov", "gov"}
	got := NormalizeSourceFormatFacets(in)
	if len(got) != 2 {
		t.Fatalf("expected 2 normalized values, got %d (%v)", len(got), got)
	}
}
