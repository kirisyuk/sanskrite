package lens

import (
	"reflect"
	"sort"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func TestClassifyScope(t *testing.T) {
	tests := []struct {
		name             string
		scope            []string
		wantStatic       []string
		wantQueries      []string
		wantArtifactRefs []string
	}{
		{
			name:             "empty scope",
			scope:            nil,
			wantStatic:       []string{},
			wantQueries:      []string{},
			wantArtifactRefs: []string{},
		},
		{
			name:             "static paths only",
			scope:            []string{"/org/albemarle", "/product/lithium"},
			wantStatic:       []string{"/org/albemarle", "/product/lithium"},
			wantQueries:      []string{},
			wantArtifactRefs: []string{},
		},
		{
			name:             "mixed scope",
			scope:            []string{"/org/albemarle", "/query/sector:/Technology/**", "/artifact/supply-chain/global/test"},
			wantStatic:       []string{"/org/albemarle"},
			wantQueries:      []string{"/query/sector:/Technology/**"},
			wantArtifactRefs: []string{"/artifact/supply-chain/global/test"},
		},
		{
			name:             "artifact refs only",
			scope:            []string{"/artifact/supply-chain/global/a", "/artifact/governance-chain/global/b"},
			wantStatic:       []string{},
			wantQueries:      []string{},
			wantArtifactRefs: []string{"/artifact/supply-chain/global/a", "/artifact/governance-chain/global/b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotStatic, gotQueries, gotArtifacts := classifyScope(tt.scope)
			if !reflect.DeepEqual(gotStatic, tt.wantStatic) {
				t.Errorf("staticPaths = %v, want %v", gotStatic, tt.wantStatic)
			}
			if !reflect.DeepEqual(gotQueries, tt.wantQueries) {
				t.Errorf("queryAddresses = %v, want %v", gotQueries, tt.wantQueries)
			}
			sort.Strings(gotArtifacts)
			sort.Strings(tt.wantArtifactRefs)
			if !reflect.DeepEqual(gotArtifacts, tt.wantArtifactRefs) {
				t.Errorf("artifactRefs = %v, want %v", gotArtifacts, tt.wantArtifactRefs)
			}
		})
	}
}

func TestIsArtifactScopeEntry(t *testing.T) {
	tests := []struct {
		entry string
		want  bool
	}{
		{"/artifact/supply-chain/global/test", true},
		{"/artifact/governance-chain/global/x", true},
		{"/org/albemarle", false},
		{"/query/sector:/Technology/**", false},
		{"", false},
		{"artifact/no-slash-prefix", false},
		{" /artifact/with-space ", true},
	}

	for _, tt := range tests {
		t.Run(tt.entry, func(t *testing.T) {
			got := IsArtifactScopeEntry(tt.entry)
			if got != tt.want {
				t.Errorf("IsArtifactScopeEntry(%q) = %v, want %v", tt.entry, got, tt.want)
			}
		})
	}
}

func TestArtifactKeyFromScopeEntry(t *testing.T) {
	tests := []struct {
		entry string
		want  string
	}{
		{"/artifact/supply-chain/global/test", "/supply-chain/global/test"},
		{"/artifact//already-has-slash", "/already-has-slash"},
		{"/artifact/no-leading-slash", "/no-leading-slash"},
		{"/org/albemarle", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.entry, func(t *testing.T) {
			got := ArtifactKeyFromScopeEntry(tt.entry)
			if got != tt.want {
				t.Errorf("ArtifactKeyFromScopeEntry(%q) = %q, want %q", tt.entry, got, tt.want)
			}
		})
	}
}

func TestSplitScopeBackwardCompat(t *testing.T) {
	scope := []string{"/org/test", "/query/sector:/Tech/**", "/artifact/supply-chain/global/x"}
	staticPaths, queryAddresses := splitScope(scope)

	if len(queryAddresses) != 1 || queryAddresses[0] != "/query/sector:/Tech/**" {
		t.Errorf("splitScope queryAddresses = %v, want 1 query address", queryAddresses)
	}

	found := false
	for _, p := range staticPaths {
		if p == "/artifact/supply-chain/global/x" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("splitScope should include artifact refs in static paths for backward compat, got %v", staticPaths)
	}
}

func TestDeferredAnchorCacheHitStillRequiresScoutingWhenNotFresh(t *testing.T) {
	if shouldSkipDeferredCacheHitScouting(false, false) {
		t.Fatal("stale deferred anchor cache hit should not suppress scout job creation")
	}
}

func TestDeferredAnchorFreshCacheHitCanSkipScouting(t *testing.T) {
	if !shouldSkipDeferredCacheHitScouting(false, true) {
		t.Fatal("fresh deferred anchor cache hit should suppress duplicate scout job creation")
	}
	if shouldSkipDeferredCacheHitScouting(true, true) {
		t.Fatal("domain contract paths must not suppress scout execution even when cached")
	}
}

func TestConglomeratesMarketDomainIsValid(t *testing.T) {
	if !taxonomy.IsValidMarketDomain("conglomerates") {
		t.Fatal("expected conglomerates market domain to be valid")
	}
}
