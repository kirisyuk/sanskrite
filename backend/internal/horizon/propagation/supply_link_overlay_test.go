package propagation

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

func TestBuildSupplyLinkOverlayRecord_UsesPrimaryEndpointsAndMetrics(t *testing.T) {
	supplier := "/org/no/equinor"
	product := "/product/raw/hydrocarbon/extraction/crude-oil"
	customer := "/fac/gb/teesside-terminal"
	link := &horizonmodels.HorizonFragment{
		Class:            ah.FragmentClassSite,
		Subtype:          "supply_link",
		Identifiers:      map[string]string{"legacy_supply_link": "true"},
		TenantID:         "global",
		CanonicalPath:    "/supply-link/global/equinor/crude-oil/teesside-terminal",
		SupplierRef:      &supplier,
		ProductRef:       &product,
		CustomerRef:      &customer,
		TransportViaRefs: []string{"/fac/sea/strait-of-hormuz-terminal"},
		Qualifiers: map[string]interface{}{
			"flow_edge_contract": map[string]interface{}{"contractCompleteness": "full"},
		},
		State: map[string]horizonmodels.MetricValue{
			"confidence": {Value: 0.82},
			"share":      {Value: 37},
		},
	}

	rec, ok := buildSupplyLinkOverlayRecord(link)
	if !ok {
		t.Fatal("expected overlay record")
	}
	if rec.SupplierPath != supplier || rec.CustomerPath != customer || rec.ProductPath != product {
		t.Fatalf("unexpected paths: %+v", rec)
	}
	if rec.Contract != "full" {
		t.Fatalf("contract=%q, want full", rec.Contract)
	}
	if rec.Share < 0.36 || rec.Share > 0.38 {
		t.Fatalf("share=%f, want normalized percent", rec.Share)
	}
}

func TestBuildSupplyLinkVirtualTraversals_IncludesSupplyAndRouteDependencies(t *testing.T) {
	rec := supplyLinkOverlayRecord{
		SupplierPath:   "/fac/ae/jebel-ali-terminal",
		CustomerPath:   "/fac/de/duisburg-plant",
		SupplierGeoRef: "/loc/ae",
		CustomerGeoRef: "/loc/de",
		ProductPath:    "/product/finished/hydrocarbon/distribution/lng",
		TransportVia:   []string{"/fac/sea/strait-of-hormuz-terminal"},
		Confidence:     0.8,
		Share:          0.55,
		Contract:       "full",
	}

	supplierTraversals := buildSupplyLinkVirtualTraversals(rec.SupplierPath, rec)
	if len(supplierTraversals) != 1 || supplierTraversals[0].edgeType != DependencyTypeSuppliesTo {
		t.Fatalf("supplier traversals=%+v, want one SUPPLIES_TO", supplierTraversals)
	}

	customerTraversals := buildSupplyLinkVirtualTraversals(rec.CustomerPath, rec)
	if len(customerTraversals) != 2 {
		t.Fatalf("customer traversals=%d, want 2", len(customerTraversals))
	}
	foundRoute := false
	for _, item := range customerTraversals {
		if item.edgeType == DependencyTypeDependsOn && item.targetPath == rec.TransportVia[0] {
			foundRoute = true
		}
	}
	if !foundRoute {
		t.Fatalf("customer traversals=%+v, want route dependency", customerTraversals)
	}

	viaTraversals := buildSupplyLinkVirtualTraversals(rec.TransportVia[0], rec)
	if len(viaTraversals) != 1 || viaTraversals[0].edgeType != DependencyTypeDependsOn || viaTraversals[0].traversalDirection != "target_to_source" {
		t.Fatalf("via traversals=%+v, want target_to_source DEPENDS_ON", viaTraversals)
	}

	geoTraversals := buildSupplyLinkVirtualTraversals(rec.SupplierGeoRef, rec)
	foundGeoAffects := false
	for _, item := range geoTraversals {
		if item.edgeType == DependencyTypeAffects && item.targetPath == rec.SupplierPath {
			foundGeoAffects = true
		}
	}
	if !foundGeoAffects {
		t.Fatalf("geo traversals=%+v, want geo->supplier affects traversal", geoTraversals)
	}
}

func TestSupplyLinkOperationalFallbackPath_ForOrgDoesNotSynthesizeFacility(t *testing.T) {
	got := supplyLinkOperationalFallbackPath("/org/de/volkswagen-ag")
	want := ""
	if got != want {
		t.Fatalf("fallback=%q, want %q", got, want)
	}
}

func TestBuildSupplyLinkQualifiers_LogisticsAddsTransmissionMetadata(t *testing.T) {
	rec := supplyLinkOverlayRecord{
		ProductPath: "/product/component/electronic/manufacturing/ev-battery-cell",
		TransportVia: []string{
			"/fac/sea/strait-of-hormuz-terminal",
		},
		Share:              0.61,
		Contract:           "partial",
		StressScore:        0.64,
		Fragility:          0.58,
		Rerouteability:     0.31,
		ZonePressure:       0.44,
		AffectedPressure:   0.22,
		RecoveryLatencyHrs: 72,
	}
	q := buildSupplyLinkQualifiers(rec, "logistics")
	if q["facet"] != string(FacetLogistics) {
		t.Fatalf("facet=%v, want logistics", q["facet"])
	}
	if q[horizonmodels.QualifierThroughputShare] == nil {
		t.Fatal("expected throughput share")
	}
	if q[horizonmodels.QualifierReroutePenalty] == nil {
		t.Fatal("expected reroute penalty for constrained route")
	}
	if q[QualifierTransmissionContractStatus] != TransmissionContractStatusPartial {
		t.Fatalf("transmission status=%v, want partial", q[QualifierTransmissionContractStatus])
	}
	if q[QualifierPathStressScore] == nil || q[QualifierPathFragility] == nil {
		t.Fatal("expected path stress qualifiers")
	}
}
