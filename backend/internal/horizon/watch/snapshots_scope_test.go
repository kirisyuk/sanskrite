package watch

import "testing"

func TestIsWatchScopeTargetPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "/org/us/tesla", want: true},
		{path: "/query/sector:/Technology/**", want: false},
		{path: "/domain/v1/supply-chain/geo/kz/subject/test", want: false},
		{path: "/artifact/supply-chain/global/test", want: false},
	}

	for _, tt := range tests {
		if got := isWatchScopeTargetPath(tt.path); got != tt.want {
			t.Fatalf("isWatchScopeTargetPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}
