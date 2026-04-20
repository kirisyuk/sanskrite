package pathquery

import (
	"context"
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestQueryPaths_resolveFromMissing(t *testing.T) {
	fr := &memFrags{byID: map[string]*horizonmodels.HorizonFragment{}}
	mat := &memMat{}
	s := NewService(fr, nil, mat, nil, nil)
	_, err := s.QueryPaths(context.Background(), Query{
		Tenant:    "t",
		From:      Endpoint{Kind: EndpointFragmentID, Value: "nope"},
		CacheMode: CacheModeDisabled,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestQueryPaths_resolveToMissing(t *testing.T) {
	fr := &memFrags{
		byID: map[string]*horizonmodels.HorizonFragment{
			"fa": {ID: "fa", CanonicalPath: "/org/a", TenantID: "t"},
		},
	}
	mat := &memMat{}
	s := NewService(fr, nil, mat, nil, nil)
	_, err := s.QueryPaths(context.Background(), Query{
		Tenant:    "t",
		From:      Endpoint{Kind: EndpointFragmentID, Value: "fa"},
		To:        &Endpoint{Kind: EndpointFragmentID, Value: "missing"},
		CacheMode: CacheModeDisabled,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestQueryPaths_resolveCanonicalMissing(t *testing.T) {
	fr := &memFrags{byCP: map[string]*horizonmodels.HorizonFragment{}}
	mat := &memMat{}
	s := NewService(fr, nil, mat, nil, nil)
	_, err := s.QueryPaths(context.Background(), Query{
		Tenant:    "t",
		From:      Endpoint{Kind: EndpointCanonicalPath, Value: "/gone"},
		CacheMode: CacheModeDisabled,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}
