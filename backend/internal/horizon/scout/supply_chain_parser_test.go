package scout

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
)

func TestParseSupplyChains_SupplyLinkSegmentCarriesUnitRefs(t *testing.T) {
	parser := NewSupplyChainParser()
	chains := []agents.SupplyChain{
		{
			Confidence:   0.92,
			TransportVia: []string{"/fac/ae/jebel-ali-port", "/loc/ae/na/na"},
			Nodes: []agents.SupplyChainNode{
				{
					CanonicalPath: "/org/cn/huawei-technologies-co-ltd",
					Type:          "ORGANIZATION",
					UnitRef:       "Enterprise",
				},
				{
					CanonicalPath: "/product/telecom/mobile-phones",
					Type:          "PRODUCT",
				},
				{
					CanonicalPath: "/org/kz/kcell-jsc",
					Type:          "ORGANIZATION",
					UnitRef:       "consumer",
				},
			},
		},
	}

	result, err := parser.ParseSupplyChains(chains, "/org/cn/huawei-technologies-co-ltd")
	if err != nil {
		t.Fatalf("ParseSupplyChains() error = %v", err)
	}
	if len(result.SupplyLinkSegments) != 1 {
		t.Fatalf("segments=%d, want 1", len(result.SupplyLinkSegments))
	}
	seg := result.SupplyLinkSegments[0]
	if seg.SupplierUnitRef != "enterprise" {
		t.Fatalf("supplierUnitRef=%q, want enterprise", seg.SupplierUnitRef)
	}
	if seg.SupplierUnitCode != "enterprise" {
		t.Fatalf("supplierUnitCode=%q, want enterprise", seg.SupplierUnitCode)
	}
	if seg.CustomerUnitRef != "consumer" {
		t.Fatalf("customerUnitRef=%q, want consumer", seg.CustomerUnitRef)
	}
	if seg.CustomerUnitCode != "consumer" {
		t.Fatalf("customerUnitCode=%q, want consumer", seg.CustomerUnitCode)
	}
	if len(seg.TransportViaRefs) != 1 || seg.TransportViaRefs[0] != "/fac/ae/jebel-ali-port" {
		t.Fatalf("transportViaRefs=%v, want only /fac/ae/jebel-ali-port", seg.TransportViaRefs)
	}
}

func TestParseSupplyChains_SuppliesToEdgeCarriesUnitQualifiers(t *testing.T) {
	parser := NewSupplyChainParser()
	chains := []agents.SupplyChain{
		{
			Confidence:   0.87,
			TransportVia: []string{"/fac/us/port-long-beach"},
			Nodes: []agents.SupplyChainNode{
				{
					CanonicalPath: "/org/us/intel-corporation",
					Type:          "ORGANIZATION",
					UnitCode:      "core_ops",
				},
				{
					CanonicalPath: "/product/semiconductors/fabs/chips",
					Type:          "PRODUCT",
				},
				{
					CanonicalPath: "/org/tw/tsmc",
					Type:          "ORGANIZATION",
					UnitCode:      "enterprise",
				},
			},
		},
	}

	result, err := parser.ParseSupplyChains(chains, "/org/us/intel-corporation")
	if err != nil {
		t.Fatalf("ParseSupplyChains() error = %v", err)
	}

	var suppliesEdge *ChainEdge
	for _, edge := range result.Edges {
		if edge == nil || edge.RelationType != string(models.DependencyTypeSuppliesTo) {
			continue
		}
		if edge.SourceCanonicalPath == "/org/us/intel-corporation" && edge.TargetCanonicalPath == "/org/tw/tsmc" {
			suppliesEdge = edge
			break
		}
	}
	if suppliesEdge == nil {
		t.Fatalf("expected inferred SUPPLIES_TO edge")
	}
	if got := suppliesEdge.Qualifiers[models.UnitQualifierSourceUnitRef]; got != "core_ops" {
		t.Fatalf("source_unit_ref=%v, want core_ops", got)
	}
	if got := suppliesEdge.Qualifiers[models.UnitQualifierTargetUnitRef]; got != "enterprise" {
		t.Fatalf("target_unit_ref=%v, want enterprise", got)
	}
	if got := suppliesEdge.Qualifiers[models.UnitQualifierSourceUnitCode]; got != "core_ops" {
		t.Fatalf("source_unit_code=%v, want core_ops", got)
	}
	if got := suppliesEdge.Qualifiers[models.UnitQualifierTargetUnitCode]; got != "enterprise" {
		t.Fatalf("target_unit_code=%v, want enterprise", got)
	}
	rawVia, ok := suppliesEdge.Qualifiers[models.QualifierTransportVia]
	if !ok {
		t.Fatalf("expected transport_via qualifier")
	}
	via, ok := rawVia.([]string)
	if !ok {
		t.Fatalf("transport_via type=%T, want []string", rawVia)
	}
	if len(via) != 1 || via[0] != "/fac/us/port-long-beach" {
		t.Fatalf("transport_via=%v, want [/fac/us/port-long-beach]", via)
	}
}
