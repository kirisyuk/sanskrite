package graphenrich

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestApplyFragmentHeuristics_InfersGovAndMarketFields(t *testing.T) {
	frag := &models.HorizonFragment{
		Type:          models.FragmentTypeOrganization,
		CanonicalPath: "/gov/sa/ministry-of-energy",
		Labels:        map[string]string{"en": "Ministry of Energy"},
		MarketSegments: []string{
			"segment/energy/lng/sa",
		},
	}
	if !ApplyFragmentHeuristics(frag) {
		t.Fatalf("expected fragment heuristics to change fragment")
	}
	if frag.GovType != "ministry" {
		t.Fatalf("expected govType=ministry, got %q", frag.GovType)
	}
	if frag.MarketDomain != "energy" {
		t.Fatalf("expected marketDomain=energy, got %q", frag.MarketDomain)
	}
	if frag.MarketVertical != "lng" {
		t.Fatalf("expected marketVertical=lng, got %q", frag.MarketVertical)
	}
	if frag.Qualifiers["graphEnrich"] == nil {
		t.Fatalf("expected graded graphEnrich report")
	}
	raw, ok := frag.Qualifiers["graphEnrich"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected graphEnrich qualifier map")
	}
	section, ok := raw["fragment"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected graphEnrich fragment section")
	}
	if _, ok := section["industryPaths"].([]map[string]interface{}); ok {
		return
	}
	if _, ok := section["industryPaths"].([]interface{}); !ok {
		t.Fatalf("expected industryPaths evidence report, got %#v", section["industryPaths"])
	}
}

func TestApplyFragmentHeuristics_TagsChokepoint(t *testing.T) {
	frag := &models.HorizonFragment{
		Type:          models.FragmentTypeOrganization,
		CanonicalPath: "/loc/sea/na/strait-of-hormuz",
		Labels:        map[string]string{"en": "Strait of Hormuz"},
	}
	ApplyFragmentHeuristics(frag)
	if frag.GeoFeatureType != "strait" {
		t.Fatalf("expected geoFeatureType=strait, got %q", frag.GeoFeatureType)
	}
	found := false
	for _, tag := range frag.Tags {
		if tag == "chokepoint" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected chokepoint tag")
	}
}

func TestApplyEdgeHeuristics_NormalizesTransportAndContracts(t *testing.T) {
	edge := &models.DependencyEdge{
		Type: models.DependencyTypeDependsOn,
		Qualifiers: map[string]interface{}{
			"transportVia":        []interface{}{string("/loc/sea/na/strait-of-hormuz")},
			"throughputShare":     "0.72",
			"reroutePenalty":      "0.40",
			"capacityUtilization": "0.81",
			"substitutability":    0.2,
		},
	}
	changed := ApplyEdgeHeuristics(edge, "/fac/qa/ras-laffan-lng-terminal", "/fac/cn/shenzhen-yantian-port")
	if !changed {
		t.Fatalf("expected edge heuristics to change edge")
	}
	if got := edge.Qualifiers[models.QualifierTransportVia]; got == nil {
		t.Fatalf("expected canonical transport_via qualifier, qualifiers=%#v", edge.Qualifiers)
	}
	if got := edge.Qualifiers["facet"]; got != string(models.FacetLogistics) {
		t.Fatalf("expected logistics facet, got %v", got)
	}
	if got := edge.Qualifiers[models.QualifierTransmissionContractStatus]; got != models.TransmissionContractStatusComplete {
		t.Fatalf("expected complete transmission contract, got %v", got)
	}
	if _, ok := edge.Metrics[models.QualifierThroughputShare]; !ok {
		t.Fatalf("expected throughput metric to be mirrored")
	}
}

func TestApplyEdgeHeuristics_RouteConstraintAddsDefaults(t *testing.T) {
	edge := &models.DependencyEdge{
		Type: models.DependencyTypeSuppliesTo,
		Qualifiers: map[string]interface{}{
			"via": "/loc/sea/na/strait-of-hormuz",
		},
	}
	ApplyEdgeHeuristics(edge, "/fac/ir/bandar-abbas-refinery", "/fac/in/mundra-port")
	if _, ok := ParseNumericQualifier(edge.Qualifiers[models.QualifierReroutePenalty]); !ok {
		t.Fatalf("expected reroute penalty heuristic, qualifiers=%#v", edge.Qualifiers)
	}
	if _, ok := ParseNumericQualifier(edge.Qualifiers[models.QualifierCapacityConstraint]); !ok {
		t.Fatalf("expected capacity constraint heuristic")
	}
	if _, ok := ParseNumericQualifier(edge.Qualifiers["substitutability"]); !ok {
		t.Fatalf("expected substitutability heuristic")
	}
	if edge.Qualifiers["graphEnrich"] == nil {
		t.Fatalf("expected graded edge graphEnrich report")
	}
}

func TestApplyFragmentHeuristics_HandlesRetailAndFinanceProfiles(t *testing.T) {
	retail := &models.HorizonFragment{
		Type:          models.FragmentTypeFacility,
		CanonicalPath: "/fac/us/walmart-supercenter-dallas",
		Labels:        map[string]string{"en": "Walmart Supercenter Dallas"},
		Description:   "Grocery retail store and fulfillment location",
	}
	ApplyFragmentHeuristics(retail)
	if retail.MarketDomain != "consumer" {
		t.Fatalf("expected consumer domain, got %q", retail.MarketDomain)
	}
	if retail.MarketVertical != "retail" {
		t.Fatalf("expected retail vertical, got %q", retail.MarketVertical)
	}

	finance := &models.HorizonFragment{
		Type:          models.FragmentTypeOrganization,
		CanonicalPath: "/org/us/visa-inc",
		Labels:        map[string]string{"en": "Visa Payment Network"},
		Description:   "Global digital payments and merchant acquiring services",
	}
	ApplyFragmentHeuristics(finance)
	if finance.MarketDomain != "finance" {
		t.Fatalf("expected finance domain, got %q", finance.MarketDomain)
	}
	if finance.MarketVertical != "payments" {
		t.Fatalf("expected payments vertical, got %q", finance.MarketVertical)
	}
}

func TestApplyFragmentHeuristicsWithContext_UsesNearbyOrgAndProduct(t *testing.T) {
	frag := &models.HorizonFragment{
		Type:          models.FragmentTypeFacility,
		CanonicalPath: "/fac/na/douai-site",
		Labels:        map[string]string{"en": "Douai Site"},
	}
	ctx := FragmentContext{
		NearbyFragments: []*models.HorizonFragment{
			{
				Type:          models.FragmentTypeOrganization,
				CanonicalPath: "/org/fr/renault",
				Labels:        map[string]string{"en": "Renault Group"},
				Description:   "Automotive manufacturer and EV assembly operator",
			},
			{
				Type:          models.FragmentTypeProduct,
				CanonicalPath: "/product/technology/automotive/ev-battery-pack",
				Labels:        map[string]string{"en": "EV Battery Pack"},
				Description:   "Battery pack for electric vehicles",
			},
		},
	}
	if !ApplyFragmentHeuristicsWithContext(frag, ctx) {
		t.Fatalf("expected fragment to be enriched")
	}
	if frag.MarketDomain != "industrial" {
		t.Fatalf("expected industrial domain, got %q", frag.MarketDomain)
	}
	if frag.MarketVertical != "automotive" {
		t.Fatalf("expected automotive vertical, got %q", frag.MarketVertical)
	}
}

func TestApplyFragmentHeuristics_MultilingualAliases(t *testing.T) {
	frag := &models.HorizonFragment{
		Type:          models.FragmentTypeFacility,
		CanonicalPath: "/fac/es/puerto-de-valencia",
		Labels:        map[string]string{"es": "Puerto de Valencia"},
		Description:   "Autoridad portuaria y terminal maritimo de contenedores",
	}
	ApplyFragmentHeuristics(frag)
	if frag.MarketDomain != "industrial" {
		t.Fatalf("expected industrial domain, got %q", frag.MarketDomain)
	}
	if frag.MarketVertical != "port-services" {
		t.Fatalf("expected port-services vertical, got %q", frag.MarketVertical)
	}
}

func TestApplyFragmentHeuristics_QualifierIncludesBucket(t *testing.T) {
	frag := &models.HorizonFragment{
		Type:          models.FragmentTypeOrganization,
		CanonicalPath: "/org/us/visa-inc",
		Labels:        map[string]string{"en": "Visa Payment Network"},
		Description:   "Global digital payments and merchant acquiring services",
	}
	ApplyFragmentHeuristics(frag)
	raw, ok := frag.Qualifiers["graphEnrich"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing graphEnrich qualifier")
	}
	section, ok := raw["fragment"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing fragment section")
	}
	domain, ok := section["marketDomain"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing marketDomain report")
	}
	if _, ok := domain["bucket"].(string); !ok {
		t.Fatalf("expected confidence bucket in marketDomain report: %#v", domain)
	}
}
