package lens

import (
	"testing"
)

func TestSplitScope(t *testing.T) {
	tests := []struct {
		name           string
		scope          []string
		wantStatic     []string
		wantQueryAddrs []string
	}{
		{
			name:           "only static paths",
			scope:          []string{"/org/tsmc", "/fac/tsmc/fab-21"},
			wantStatic:     []string{"/org/tsmc", "/fac/tsmc/fab-21"},
			wantQueryAddrs: []string{},
		},
		{
			name:           "only query addresses",
			scope:          []string{"/query/sector:/Technology/**", "/query/location:/loc/id/**"},
			wantStatic:     []string{},
			wantQueryAddrs: []string{"/query/sector:/Technology/**", "/query/location:/loc/id/**"},
		},
		{
			name:           "mixed scope",
			scope:          []string{"/org/tsmc", "/query/sector:/Technology/**", "/fac/tsmc/fab-21"},
			wantStatic:     []string{"/org/tsmc", "/fac/tsmc/fab-21"},
			wantQueryAddrs: []string{"/query/sector:/Technology/**"},
		},
		{
			name:           "empty scope",
			scope:          []string{},
			wantStatic:     []string{},
			wantQueryAddrs: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotStatic, gotQueryAddrs := splitScope(tt.scope)
			
			if len(gotStatic) != len(tt.wantStatic) {
				t.Errorf("splitScope() static paths count = %d, want %d", len(gotStatic), len(tt.wantStatic))
			}
			
			if len(gotQueryAddrs) != len(tt.wantQueryAddrs) {
				t.Errorf("splitScope() query addresses count = %d, want %d", len(gotQueryAddrs), len(tt.wantQueryAddrs))
			}
			
			// Check static paths
			staticMap := make(map[string]bool)
			for _, p := range gotStatic {
				staticMap[p] = true
			}
			for _, want := range tt.wantStatic {
				if !staticMap[want] {
					t.Errorf("splitScope() missing static path %q", want)
				}
			}
			
			// Check query addresses
			queryMap := make(map[string]bool)
			for _, p := range gotQueryAddrs {
				queryMap[p] = true
			}
			for _, want := range tt.wantQueryAddrs {
				if !queryMap[want] {
					t.Errorf("splitScope() missing query address %q", want)
				}
			}
		})
	}
}




