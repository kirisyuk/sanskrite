package models

import "testing"

func TestParseLensDomainQueries_LegacyMaps(t *testing.T) {
	raw := []map[string]interface{}{
		{
			"CanonicalPath": "/domain/v1/supply-chain/subject/tsmc",
			"ContractType":  "supply-chain",
			"Title":         "TSMC supply chain",
			"Description":   "Reveal chokepoints for TSMC",
			"Facets": map[string]interface{}{
				"geo": []interface{}{"tw", " jp ", "TW"},
			},
			"FirstOrderEntities": []interface{}{"/org/tsmc", " /fac/tw/fab-21 "},
		},
	}

	got := ParseLensDomainQueries(raw)
	if len(got) != 1 {
		t.Fatalf("expected 1 domain query, got %d", len(got))
	}
	if got[0].ContractType != "supply-chain" {
		t.Fatalf("expected contractType to survive parse, got %q", got[0].ContractType)
	}
	if len(got[0].Facets["geo"]) != 2 {
		t.Fatalf("expected compacted facet values, got %#v", got[0].Facets["geo"])
	}
	if len(got[0].FirstOrderEntities) != 2 {
		t.Fatalf("expected compacted firstOrderEntities, got %#v", got[0].FirstOrderEntities)
	}
}

func TestLensDomainQueriesToMaps(t *testing.T) {
	input := []LensDomainQuery{{
		CanonicalPath: "/domain/v1/policy-impact/jurisdiction/eu",
		ContractType:  "policy-impact",
		Title:         "EU policy impact",
		RegulatoryImpactExecutionPlan: &LensRegulatoryImpactExecutionPlan{
			Queries: []LensRegulatoryImpactQuery{{
				Instrument:   "sanctions",
				Jurisdiction: "eu",
			}},
		},
	}}

	got := LensDomainQueriesToMaps(input)
	if len(got) != 1 {
		t.Fatalf("expected 1 map, got %d", len(got))
	}
	if _, ok := got[0]["regulatoryImpactExecutionPlan"]; !ok {
		t.Fatalf("expected regulatoryImpactExecutionPlan in state map, got %#v", got[0])
	}
}
