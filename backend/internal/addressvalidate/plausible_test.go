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
		{"12345 Some Very Long Industrial Logistics Boulevard, Zone 4, Export Processing Area, Near Port Gate 7, Jebel Ali, Dubai, AE", true},
		{"comments = 185.000000", false},
		{"The cost of code is decreasing and this headline is definitely not an address", false},
		{"1234567890 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890 1234567890", false},
	}
	for _, tt := range tests {
		got := IsPlausibleAddress(tt.addr)
		if got != tt.plausible {
			t.Errorf("IsPlausibleAddress(%q) = %v, want %v", tt.addr, got, tt.plausible)
		}
	}
}
