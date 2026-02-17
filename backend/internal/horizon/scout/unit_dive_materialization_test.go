package scout

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
)

func containsString(list []string, item string) bool {
	for _, v := range list {
		if v == item {
			return true
		}
	}
	return false
}

func TestParseUnitDiveRequestState_Normalizes(t *testing.T) {
	raw := map[string]interface{}{
		"requestId":       "req-1",
		"supplierOrgRef":  "/org/cn/huawei",
		"customerOrgRef":  "/org/kz/kazakhtelecom",
		"supplierUnitRef": "Enterprise",
		"customerUnitRef": "consumer",
	}
	req, ok := parseUnitDiveRequestState(raw)
	if !ok || req == nil {
		t.Fatalf("expected valid request")
	}
	if req.RequestID != "req-1" {
		t.Fatalf("requestId=%q, want req-1", req.RequestID)
	}
	if req.SupplierUnitRef != "enterprise" {
		t.Fatalf("supplierUnitRef=%q, want enterprise", req.SupplierUnitRef)
	}
	if req.CustomerUnitRef != "consumer" {
		t.Fatalf("customerUnitRef=%q, want consumer", req.CustomerUnitRef)
	}
}

func TestBuildUnitDiveResultContract_DerivesChainsAndLinks(t *testing.T) {
	req := &models.UnitDiveRequest{
		RequestID:       "req-1",
		SupplierOrgRef:  "/org/cn/huawei",
		CustomerOrgRef:  "/org/kz/kazakhtelecom",
		SupplierUnitRef: "enterprise",
		CustomerUnitRef: "enterprise",
	}
	rels := []agents.DiscoveredRelation{
		{
			RelationType:        "SUPPLIES_TO",
			TargetCanonicalPath: "/org/kz/kazakhtelecom",
			Qualifiers: map[string]interface{}{
				models.UnitQualifierSourceUnitRef: "enterprise",
				models.UnitQualifierTargetUnitRef: "enterprise",
			},
		},
		{
			RelationType:        "DEPENDS_ON",
			SourceCanonicalPath: "/org/kz/kazakhtelecom",
			TargetCanonicalPath: "/org/us/cisco",
			Qualifiers: map[string]interface{}{
				models.UnitQualifierSourceUnitRef: "enterprise",
				models.UnitQualifierTargetUnitRef: "enterprise",
			},
		},
	}
	chains := []agents.SupplyChain{
		{
			Nodes: []agents.SupplyChainNode{
				{CanonicalPath: "/org/cn/huawei"},
				{CanonicalPath: "/product/telecom/mobile-phones"},
				{CanonicalPath: "/org/kz/kazakhtelecom"},
			},
		},
	}
	result := buildUnitDiveResultContract(req, "/org/cn/huawei", 0.81, rels, chains, []string{"edge-1"})
	if result.Status != models.UnitDiveStatusCompleted {
		t.Fatalf("status=%q, want COMPLETED", result.Status)
	}
	if result.RequestID != "req-1" {
		t.Fatalf("requestId=%q, want req-1", result.RequestID)
	}
	if !containsString(result.NewSupplyLinks, "edge-1") {
		t.Fatalf("newSupplyLinks=%v, expected edge-1", result.NewSupplyLinks)
	}
	if !containsString(result.UpstreamChain, "/org/us/cisco") {
		t.Fatalf("upstreamChain=%v, expected /org/us/cisco", result.UpstreamChain)
	}
	if !containsString(result.DownstreamChain, "/org/kz/kazakhtelecom") {
		t.Fatalf("downstreamChain=%v, expected /org/kz/kazakhtelecom", result.DownstreamChain)
	}
	if !containsString(result.ResolvedLegalEntities, "/org/cn/huawei") || !containsString(result.ResolvedLegalEntities, "/org/kz/kazakhtelecom") {
		t.Fatalf("resolvedLegalEntities=%v, expected supplier/customer orgs", result.ResolvedLegalEntities)
	}
}

func TestBuildUnitDiveResultContract_PartialOnEmptyPayload(t *testing.T) {
	req := &models.UnitDiveRequest{
		RequestID:       "req-empty",
		SupplierOrgRef:  "/org/cn/huawei",
		CustomerOrgRef:  "/org/kz/kazakhtelecom",
		SupplierUnitRef: "enterprise",
		CustomerUnitRef: "enterprise",
	}
	result := buildUnitDiveResultContract(req, "/org/cn/huawei", 0.7, nil, nil, nil)
	if result.Status != models.UnitDiveStatusPartial {
		t.Fatalf("status=%q, want PARTIAL", result.Status)
	}
}
