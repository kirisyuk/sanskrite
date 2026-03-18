package scout

import (
	"testing"

	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

func TestNormalizeRuntimeDomainCanonicalPath_RepairsSupplyChainNamespacePath(t *testing.T) {
	in := "/domain/v1/supply-chain/org/us/apple-inc/product/iphone"
	got := NormalizeRuntimeDomainCanonicalPath(in)
	want := "/domain/v1/supply-chain/geo/us/product/iphone/subject/apple-inc"
	if got != want {
		t.Fatalf("NormalizeRuntimeDomainCanonicalPath() = %q, want %q", got, want)
	}
}

func TestNormalizeRuntimeDomainCanonicalPath_TrimsIncompleteFacet(t *testing.T) {
	in := "/domain/v1/supply-chain/geo/de/product"
	got := NormalizeRuntimeDomainCanonicalPath(in)
	want := "/domain/v1/supply-chain/geo/de"
	if got != want {
		t.Fatalf("NormalizeRuntimeDomainCanonicalPath() = %q, want %q", got, want)
	}
}

func TestBestEffortFindDomainContract_NormalizesMalformedSupplyChainDomain(t *testing.T) {
	engine := &ScoutEngine{}
	job := &hmodels.ScoutingJob{
		AgentJob: coremodels.AgentJob{
			State: map[string]interface{}{
				"targetCanonicalPath": "/domain/v1/supply-chain/org/us/apple-inc/product/iphone",
				"domains": []map[string]interface{}{
					{
						"canonicalPath": "/domain/v1/supply-chain/org/us/apple-inc/product/iphone",
						"title":         "Apple iPhone Supply Chain",
						"contractType":  "supply-chain",
					},
				},
			},
		},
	}

	got := engine.bestEffortFindDomainContract(job, "/domain/v1/supply-chain/subject/apple-inc/geo/us/product/iphone")
	if got == nil {
		t.Fatalf("expected normalized malformed supply-chain domain to match")
	}
	if got.Title != "Apple iPhone Supply Chain" {
		t.Fatalf("unexpected title: %q", got.Title)
	}
}
