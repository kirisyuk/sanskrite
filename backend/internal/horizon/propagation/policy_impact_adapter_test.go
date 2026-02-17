package propagation

import (
	"context"
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestPolicyImpactAdapter_Encode_DefaultMacroEngine(t *testing.T) {
	a := NewPolicyImpactAdapter()
	out, err := a.Encode(context.Background(), ScenarioInput{
		ScenarioID:   "kz-vat-16",
		ContractType: "policy-impact",
		Parameters: map[string]interface{}{
			"vat_rate_from": 12.0,
			"vat_rate_to":   16.0,
			"jurisdiction":  "kz",
		},
		TargetPaths: []string{"/market/kz/consumer/retail"},
	})
	if err != nil {
		t.Fatalf("encode error: %v", err)
	}
	if len(out) < 4 {
		t.Fatalf("expected at least 4 macro signals, got %d", len(out))
	}
	if out[0].Polarity != "risk" {
		t.Fatalf("expected risk polarity for VAT increase, got %q", out[0].Polarity)
	}
	if out[0].Amplitude <= 0 {
		t.Fatalf("expected positive amplitude")
	}
	if err := out[0].Validate(); err != nil {
		t.Fatalf("encoded signal must be valid: %v", err)
	}
}

func TestPolicyImpactAdapter_Encode_UnknownEngineFails(t *testing.T) {
	a := NewPolicyImpactAdapter()
	_, err := a.Encode(context.Background(), ScenarioInput{
		ContractType: "policy-impact",
		Parameters: map[string]interface{}{
			"engine_id":          "unknown_engine_v42",
			"vat_rate_from":      12.0,
			"vat_rate_to":        16.0,
			"price_basket_share": 0.5,
		},
	})
	if err == nil {
		t.Fatalf("expected error for unknown engine")
	}
}

func TestPolicyImpactAdapter_TransferHook_EvasionRedirect(t *testing.T) {
	a := NewPolicyImpactAdapter()
	ctx := WithDomainAdapterBinding(context.Background(), DomainAdapterBinding{
		EngineID: "macro",
	})
	signal := &horizonmodels.SignalDescriptor{
		Axis:      horizonmodels.AxisFinancialSettlement,
		Amplitude: 0.50,
		Polarity:  "risk",
	}
	edge := &horizonmodels.DependencyEdge{
		Type: horizonmodels.DependencyTypeInfluences,
		Qualifiers: map[string]interface{}{
			"pass_through_estimate": 0.6,
			"channel_type":          "evasion",
		},
	}

	got, err := a.TransferHook(ctx, signal, edge, nil)
	if err != nil {
		t.Fatalf("transfer hook error: %v", err)
	}
	if got == nil {
		t.Fatalf("expected transfer result")
	}
	if got.AdjustedAxis != horizonmodels.AxisInfoUncertainty {
		t.Fatalf("expected axis redirect to info/uncertainty, got %q", got.AdjustedAxis)
	}
	if got.AdjustedAmplitude >= signal.Amplitude {
		t.Fatalf("expected reduced amplitude, got %.3f", got.AdjustedAmplitude)
	}
}

func TestPolicyImpactAdapter_Decode_ReturnsMacroMetricSet(t *testing.T) {
	a := NewPolicyImpactAdapter()
	out, err := a.Decode(context.Background(), nil, []AccumulatedSignal{
		{Axis: horizonmodels.AxisFinancialSettlement, Amplitude: 0.30},
		{Axis: horizonmodels.AxisPhysicalFlowDemand, Amplitude: 0.25},
		{Axis: horizonmodels.AxisInfoUncertainty, Amplitude: 0.10},
		{Axis: horizonmodels.AxisLegalCompliance, Amplitude: 0.08},
	}, ScenarioInput{
		ContractType: "policy-impact",
		Parameters: map[string]interface{}{
			"engine_id":                    PolicyImpactEngineMacroEconomicalV1,
			"vat_rate_from":                12.0,
			"vat_rate_to":                  16.0,
			"baseline_vat_revenue_bln_kzt": 4000.0,
			"baseline_gdp_bln_kzt":         120000.0,
		},
	})
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(out) < 10 {
		t.Fatalf("expected rich macro metric set, got %d metrics", len(out))
	}

	seen := map[string]bool{}
	for _, m := range out {
		seen[m.MetricPath] = true
	}
	for _, want := range []string{
		MetricBudgetRevenueDeltaBlnKZT,
		MetricGDPRealDeltaPct,
		MetricShadowEconomyShareDeltaPct,
		MetricCPIDeltaPct,
		MetricComplianceRateDeltaPct,
	} {
		if !seen[want] {
			t.Fatalf("missing expected macro metric: %s", want)
		}
	}
}
