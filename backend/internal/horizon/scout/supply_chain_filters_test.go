package scout

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
)

func TestSupplyChainMatchesPlanFilters_ProductAndGeo(t *testing.T) {
	chain := agents.SupplyChain{
		Nodes: []agents.SupplyChainNode{
			{CanonicalPath: "/org/us/schlumberger", Type: "ORGANIZATION", Role: "supplier"},
			{CanonicalPath: "/fac/us/houston-parts-hub", Type: "FACILITY", Role: "producer"},
			{CanonicalPath: "/product/component/mechanical/manufacturing/oilfield-spare-parts", Type: "PRODUCT"},
			{CanonicalPath: "/fac/ru/tyumen-service-base", Type: "FACILITY", Role: "consumer"},
			{CanonicalPath: "/org/ru/gazprom-neft", Type: "ORGANIZATION", Role: "customer"},
		},
	}

	if !supplyChainMatchesPlanFilters(chain, []string{"spare-parts"}, []string{"ru"}) {
		t.Fatalf("expected RU spare-parts chain to match geo and product filters")
	}
	if supplyChainMatchesPlanFilters(chain, []string{"drilling-fluid"}, []string{"ru"}) {
		t.Fatalf("unexpected match for non-matching product filter")
	}
}

func TestSupplyChainMatchesPlanFilters_RegionalGeo(t *testing.T) {
	chain := agents.SupplyChain{
		Nodes: []agents.SupplyChainNode{
			{CanonicalPath: "/org/ae/mubadala", Type: "ORGANIZATION", Role: "supplier"},
			{CanonicalPath: "/fac/om/duqm-space-industrial-zone", Type: "FACILITY", Role: "producer"},
			{CanonicalPath: "/product/component/electronics/manufacturing/satellite-bus", Type: "PRODUCT"},
		},
	}

	if !supplyChainMatchesPlanFilters(chain, []string{"satellite-bus"}, []string{"gcc"}) {
		t.Fatalf("expected GCC regional geo filter to match OM facility")
	}
	if supplyChainMatchesPlanFilters(chain, []string{"satellite-bus"}, []string{"br"}) {
		t.Fatalf("unexpected match for non-member country geo filter")
	}
}
