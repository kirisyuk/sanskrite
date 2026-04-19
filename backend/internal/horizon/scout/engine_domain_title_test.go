package scout

import (
	"strings"
	"testing"

	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

func TestBestEffortDisplayLabel_SkipsNaSegment(t *testing.T) {
	got := bestEffortDisplayLabel("", "/market/na/materials/lithium")
	if strings.EqualFold(strings.TrimSpace(got), "na") {
		t.Fatalf("label must not fallback to na placeholder, got=%q", got)
	}
	if strings.TrimSpace(got) == "" {
		t.Fatalf("label must not be empty")
	}
}

func TestBestEffortFindDomainContract_FallbackByTargetPath(t *testing.T) {
	engine := &ScoutEngine{}
	job := &hmodels.ScoutingJob{
		AgentJob: coremodels.AgentJob{
			State: map[string]interface{}{
				"targetCanonicalPath": "/domain/v1/supply-chain/geo/br/industry/logistics",
				"domains": []map[string]interface{}{
					{
						"canonicalPath": "/domain/v1/supply-chain/geo/br/industry/logistics",
						"title":         "Brazil Logistics Supply Chain Resilience",
						"contractType":  "supply-chain",
					},
				},
			},
		},
	}

	// Simulate a retargeted/rewritten path that no longer matches contract path by exact string.
	got := engine.bestEffortFindDomainContract(job, "/domain/v1/supply-chain/industry/logistics/geo/br")
	if got == nil {
		t.Fatalf("expected domain contract to be found via fallback matching")
	}
	if strings.TrimSpace(got.Title) != "Brazil Logistics Supply Chain Resilience" {
		t.Fatalf("title mismatch: got=%q", got.Title)
	}
}

func TestBuildRelationMissingInfo_IncludesGeoDisambiguation(t *testing.T) {
	relations := []agents.DiscoveredRelation{
		{TargetCanonicalPath: "/loc/ae/na/na"},
	}
	missing := buildRelationMissingInfo(relations, 5)
	if len(missing) == 0 {
		t.Fatalf("expected geo disambiguation missing-info for /loc/*/na/na")
	}
}

func TestBuildRelationMissingInfo_IncludesJurisdictionDisambiguation(t *testing.T) {
	relations := []agents.DiscoveredRelation{
		{TargetCanonicalPath: "/org/na/acme-holdings"},
		{TargetCanonicalPath: "/gov/na/ministry-of-energy"},
	}
	missing := buildRelationMissingInfo(relations, 10)
	if len(missing) < 2 {
		t.Fatalf("expected jurisdiction disambiguation missing-info for /org/na/* and /gov/na/*")
	}
}

func TestShouldSkipRecursivePlaceholderTarget(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{path: "/loc/ch/zg/na", want: true},
		{path: "/loc/ch/na/na", want: true},
		{path: "/org/na/acme-holdings", want: true},
		{path: "/gov/na/ministry-of-energy", want: true},
		{path: "/loc/ch/zg/baar", want: false},
		{path: "/org/ch/glencore-plc", want: false},
	}
	for _, tc := range cases {
		got := shouldSkipRecursivePlaceholderTarget(tc.path)
		if got != tc.want {
			t.Fatalf("path=%q want=%v got=%v", tc.path, tc.want, got)
		}
	}
}

func TestIsSoftDeniedRelationForRecurse_GeoToMarketOperatesIn(t *testing.T) {
	if !isSoftDeniedRelationForRecurse(
		FragmentTypeGeoRegion,
		FragmentTypeMarket,
		"/loc/ch/zg/na",
		"/market/ch/technology/blockchain",
		string(DependencyTypeOperatesIn),
	) {
		t.Fatalf("expected soft deny for GEO_REGION OPERATES_IN MARKET")
	}
}
