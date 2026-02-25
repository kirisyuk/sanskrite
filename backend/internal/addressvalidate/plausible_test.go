package addressvalidate

import "testing"

func TestIsPlausibleAddress(t *testing.T) {
	tests := []struct {
		addr      string
		plausible bool
	}{
		{"Moscow, Russia", true},
		{"Dubai, UAE", true},
		{"Port of Houston, US", true},
		{"comments = 185.000000", false},
		{"The cost of code is decreasing...", false},
	}
	for _, tt := range tests {
		got := IsPlausibleAddress(tt.addr)
		if got != tt.plausible {
			t.Errorf("IsPlausibleAddress(%q) = %v, want %v", tt.addr, got, tt.plausible)
		}
	}
}
