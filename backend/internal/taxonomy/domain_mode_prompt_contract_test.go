package taxonomy_test

import (
	"strings"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/contractsemantics"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func TestDomainModePromptRelationSetsMatchUSOTAllowlist(t *testing.T) {
	tests := []struct {
		mode string
		csv  string
	}{
		{mode: taxonomy.DomainModeSupplyChain, csv: taxonomy.SupplyChainEdgeTypesString()},
		{mode: taxonomy.DomainModeServiceChain, csv: taxonomy.ServiceChainEdgeTypesString()},
		{mode: taxonomy.DomainModeGovernance, csv: taxonomy.GovernanceEdgeTypesString()},
		{mode: taxonomy.DomainModeAssurance, csv: taxonomy.AssuranceEdgeTypesString()},
		{mode: taxonomy.DomainModeMarket, csv: taxonomy.MarketEdgeTypesString()},
		{mode: taxonomy.DomainModeStrategic, csv: taxonomy.StrategicEdgeTypesString()},
		{mode: taxonomy.DomainModePolicyImpact, csv: taxonomy.PolicyImpactEdgeTypesString()},
		{mode: taxonomy.DomainModeEcosystem, csv: taxonomy.EcosystemEdgeTypesString()},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.mode, func(t *testing.T) {
			expected := makeSet(contractsemantics.AllowedEdgeStringsForDomainMode(tc.mode))
			got := makeSet(parseCSV(tc.csv))
			if !setsEqual(expected, got) {
				t.Fatalf("prompt relation set drift for mode=%s; expected=%v got=%v", tc.mode, sortedSetValues(expected), sortedSetValues(got))
			}
		})
	}
}

func parseCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	raw := strings.Split(s, ",")
	out := make([]string, 0, len(raw))
	for _, part := range raw {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func makeSet(items []string) map[string]struct{} {
	out := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out[item] = struct{}{}
	}
	return out
}

func setsEqual(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}

func sortedSetValues(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	// tiny, deterministic output for test failure readability
	for i := 0; i < len(out)-1; i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}
