package geocoding

import "testing"

func TestIsPlausibleAddress(t *testing.T) {
	tests := []struct {
		addr     string
		plausible bool
	}{
		// Valid addresses
		{"Moscow, Russia", true},
		{"Dubai, UAE", true},
		{"Korea Ryonbong General Corporation, KP", true},
		{"Port of Houston, US", true},
		{"London, UK", true},
		{"Tokyo, JP", true},
		// Non-addresses (from GCP logs)
		{"comments = 185.000000", false},
		{"upvotes = 344.000000", false},
		{"hn_comments = 0.000000", false},
		{"social_media_engagement = 45.000000", false},
		{"bulk_insert_improvement_ratio = 0.100000", false},
		{"random_reads_improvement_ratio = 0.680000", false},
		{"The cost of code is decreasing, shifting the focus to agent orchestration.", false},
		{"Blood test boosts Alzheimer diagnosis accuracy", false},
		{"RocksDB beats SNKV on write-heavy workloads (LSM-tree).", false},
		{"sqlite3 CLI won't open the database (schema layer is bypassed by design).", false},
		{"Release of x86css", true}, // short, no pattern
	}
	for _, tt := range tests {
		got := IsPlausibleAddress(tt.addr)
		if got != tt.plausible {
			t.Errorf("IsPlausibleAddress(%q) = %v, want %v", tt.addr, got, tt.plausible)
		}
	}
}
