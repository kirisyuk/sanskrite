package horizon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathquery"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
)

func TestParsePathqueryEndpoint(t *testing.T) {
	ep, err := parsePathqueryEndpoint("/org/a")
	if err != nil || ep.Kind != pathquery.EndpointCanonicalPath || ep.Value != "/org/a" {
		t.Fatalf("%+v %v", ep, err)
	}
	ep, err = parsePathqueryEndpoint("id:frag-1")
	if err != nil || ep.Kind != pathquery.EndpointFragmentID || ep.Value != "frag-1" {
		t.Fatalf("%+v %v", ep, err)
	}
	_, err = parsePathqueryEndpoint("nope")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGetPathquery_jsonShape(t *testing.T) {
	fr := &pathqueryMemFrags{byCP: map[string]*horizonmodels.HorizonFragment{
		"/org/a": {ID: "fa", CanonicalPath: "/org/a", TenantID: "t"},
	}}
	leg := horizonmodels.SupplyChainPathLeg{Index: 0, SupplierRef: "/org/a", CustomerRef: "/org/z"}
	payload := horizonmodels.BuildSupplyChainPathPayloadDetailed(nil, nil, []horizonmodels.SupplyChainPathLeg{leg}, nil, nil, nil, nil)
	art := &horizonmodels.MaterializedArtifact{
		ID:       "m1",
		TenantID: "t",
		Kind:     horizonmodels.MaterializedKindSupplyChainPath,
		NodeRefs: []string{"/org/a"},
		Payload:  payload,
	}
	mat := &pathqueryMemMat{
		byNode: map[string][]*horizonmodels.MaterializedArtifact{"/org/a": {art}},
	}
	svc := pathquery.NewService(fr, nil, mat, nil, nil)
	h := NewHorizonHandlers(nil).WithPathqueryService(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/horizon/pathquery?from=/org/a&cacheMode=disabled", nil)
	ctx := context.WithValue(req.Context(), httputil.AuthContextKey{}, &httputil.AuthInfo{TenantID: "t"})
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.GetPathquery(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["fromCache"] != false {
		t.Fatalf("fromCache=%v", body["fromCache"])
	}
	paths, _ := body["paths"].([]interface{})
	if len(paths) != 1 {
		t.Fatalf("paths=%v", body["paths"])
	}
	first, _ := paths[0].(map[string]interface{})
	if first == nil || first["id"] != "m1" {
		t.Fatalf("expected pathquery path item with id m1, got %v", paths[0])
	}
	if first["kind"] != string(horizonmodels.MaterializedKindSupplyChainPath) {
		t.Fatalf("kind=%v", first["kind"])
	}
	if first["payload"] == nil {
		t.Fatalf("expected payload on path item, got %v", first)
	}
	if _, leaked := first["tenantId"]; leaked {
		t.Fatalf("path item must not expose persistence fields, got tenantId")
	}
}

func TestGetPathquery_badMethod(t *testing.T) {
	h := NewHorizonHandlers(nil).WithPathqueryService(pathquery.NewService(nil, nil, nil, nil, nil))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/horizon/pathquery?from=/org/a", nil)
	req = req.WithContext(context.WithValue(req.Context(), httputil.AuthContextKey{}, &httputil.AuthInfo{TenantID: "t"}))
	rr := httptest.NewRecorder()
	h.GetPathquery(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got %d", rr.Code)
	}
}

func TestGetPathquery_noAuth(t *testing.T) {
	h := NewHorizonHandlers(nil).WithPathqueryService(pathquery.NewService(nil, nil, nil, nil, nil))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/horizon/pathquery?from=/org/a", nil)
	rr := httptest.NewRecorder()
	h.GetPathquery(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("got %d", rr.Code)
	}
}

func TestGetPathquery_queryParsing(t *testing.T) {
	fr := &pathqueryMemFrags{byCP: map[string]*horizonmodels.HorizonFragment{
		"/org/a": {ID: "fa", CanonicalPath: "/org/a", TenantID: "t"},
		"/org/b": {ID: "fb", CanonicalPath: "/org/b", TenantID: "t"},
	}}
	leg := horizonmodels.SupplyChainPathLeg{Index: 0, SupplierRef: "/org/a", CustomerRef: "/org/b"}
	payload := horizonmodels.BuildSupplyChainPathPayloadDetailed(nil, nil, []horizonmodels.SupplyChainPathLeg{leg}, nil, nil, nil, nil)
	art := &horizonmodels.MaterializedArtifact{
		ID:       "m1",
		TenantID: "t",
		Kind:     horizonmodels.MaterializedKindSupplyChainPath,
		NodeRefs: []string{"/org/a", "/org/b"},
		Payload:  payload,
	}
	mat := &pathqueryMemMat{
		byNode: map[string][]*horizonmodels.MaterializedArtifact{"/org/a": {art}},
		byID:   map[string]*horizonmodels.MaterializedArtifact{"m1": art},
	}
	svc := pathquery.NewService(fr, nil, mat, nil, nil)
	h := NewHorizonHandlers(nil).WithPathqueryService(svc)

	u := "/api/v1/horizon/pathquery?from=/org/a&to=/org/b&productAxis=/p&maxHops=4&relations=supplies&cacheMode=disabled&includeStress=1&includeGeo=true&lensId=l1&scope=s&asOf=2024-06-01"
	req := httptest.NewRequest(http.MethodGet, u, nil)
	req = req.WithContext(context.WithValue(req.Context(), httputil.AuthContextKey{}, &httputil.AuthInfo{TenantID: "t"}))
	rr := httptest.NewRecorder()
	h.GetPathquery(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}
}

// pathqueryMemFrags mirrors pathquery package test helpers (cross-package test doubles).
type pathqueryMemFrags struct {
	byID map[string]*horizonmodels.HorizonFragment
	byCP map[string]*horizonmodels.HorizonFragment
}

func (m *pathqueryMemFrags) Get(ctx context.Context, id string) (*horizonmodels.HorizonFragment, error) {
	if m == nil {
		return nil, nil
	}
	return m.byID[id], nil
}

func (m *pathqueryMemFrags) GetByCanonicalPathForTenant(ctx context.Context, tenantID, canonicalPath string) (*horizonmodels.HorizonFragment, error) {
	if m == nil {
		return nil, nil
	}
	return m.byCP[canonicalPath], nil
}

func (m *pathqueryMemFrags) GetByCanonicalPath(ctx context.Context, canonicalPath string) (*horizonmodels.HorizonFragment, error) {
	if m == nil {
		return nil, nil
	}
	return m.byCP[canonicalPath], nil
}

type pathqueryMemMat struct {
	byNode map[string][]*horizonmodels.MaterializedArtifact
	byID   map[string]*horizonmodels.MaterializedArtifact
}

func (m *pathqueryMemMat) FindByNodeRef(ctx context.Context, tenantID, path string, kind horizonmodels.MaterializedKind) ([]*horizonmodels.MaterializedArtifact, error) {
	if m == nil || m.byNode == nil {
		return nil, nil
	}
	return m.byNode[path], nil
}

func (m *pathqueryMemMat) Get(ctx context.Context, id string) (*horizonmodels.MaterializedArtifact, error) {
	if m == nil || m.byID == nil {
		return nil, nil
	}
	return m.byID[id], nil
}

func TestGetPathquery_asOfRFC3339(t *testing.T) {
	fr := &pathqueryMemFrags{byCP: map[string]*horizonmodels.HorizonFragment{
		"/org/a": {ID: "fa", CanonicalPath: "/org/a", TenantID: "t"},
	}}
	leg := horizonmodels.SupplyChainPathLeg{Index: 0, SupplierRef: "/org/a", CustomerRef: "/org/z"}
	payload := horizonmodels.BuildSupplyChainPathPayloadDetailed(nil, nil, []horizonmodels.SupplyChainPathLeg{leg}, nil, nil, nil, nil)
	art := &horizonmodels.MaterializedArtifact{
		ID:       "m1",
		TenantID: "t",
		Kind:     horizonmodels.MaterializedKindSupplyChainPath,
		NodeRefs: []string{"/org/a"},
		Payload:  payload,
	}
	mat := &pathqueryMemMat{byNode: map[string][]*horizonmodels.MaterializedArtifact{"/org/a": {art}}}
	svc := pathquery.NewService(fr, nil, mat, nil, nil)
	h := NewHorizonHandlers(nil).WithPathqueryService(svc)
	asOf := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC).Format(time.RFC3339)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/horizon/pathquery?from=/org/a&cacheMode=disabled&asOf="+asOf, nil)
	req = req.WithContext(context.WithValue(req.Context(), httputil.AuthContextKey{}, &httputil.AuthInfo{TenantID: "t"}))
	rr := httptest.NewRecorder()
	h.GetPathquery(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}
}
