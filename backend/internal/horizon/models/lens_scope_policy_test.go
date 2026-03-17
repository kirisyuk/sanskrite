package models

import "testing"

func TestIsNonFragmentScopeEntry(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "/query/sector:/Technology/**", want: true},
		{path: "/domain/v1/supply-chain/geo/kz/subject/test", want: true},
		{path: "/artifact/supply-chain/global/test", want: true},
		{path: "/org/us/tesla", want: false},
	}

	for _, tt := range tests {
		if got := IsNonFragmentScopeEntry(tt.path); got != tt.want {
			t.Fatalf("IsNonFragmentScopeEntry(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestScopeAutoAddCounterKey(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "/domain/v1/supply-chain/geo/kz/subject/test", want: ""},
		{path: "/artifact/supply-chain/global/test", want: ScopeAutoAddArtifactCounterKey},
		{path: "/org/us/tesla", want: ScopeAutoAddEntityCounterKey},
	}

	for _, tt := range tests {
		if got := ScopeAutoAddCounterKey(tt.path); got != tt.want {
			t.Fatalf("ScopeAutoAddCounterKey(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}
