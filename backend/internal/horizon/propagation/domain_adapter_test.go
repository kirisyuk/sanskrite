package propagation

import (
	"context"
	"reflect"
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

type testAdapter struct {
	family string
}

func (a *testAdapter) ContractTypeFamily() string { return a.family }

func (a *testAdapter) Encode(ctx context.Context, scenario ScenarioInput) ([]*horizonmodels.SignalDescriptor, error) {
	return nil, nil
}

func (a *testAdapter) TransferHook(ctx context.Context, signal *horizonmodels.SignalDescriptor, edge *horizonmodels.DependencyEdge, node *horizonmodels.HorizonFragment) (*TransferResult, error) {
	return nil, nil
}

func (a *testAdapter) Decode(ctx context.Context, node *horizonmodels.HorizonFragment, accumulated []AccumulatedSignal, scenario ScenarioInput) ([]BusinessMetric, error) {
	return nil, nil
}

func TestAdapterRegistry_RegisterAndLookupByContractType(t *testing.T) {
	r := NewAdapterRegistry()
	a := &testAdapter{family: "policy-impact"}
	if err := r.Register(a); err != nil {
		t.Fatalf("register: %v", err)
	}

	got, ok := r.ForContractType(" POLICY-IMPACT ")
	if !ok || got == nil {
		t.Fatalf("expected adapter for contract type")
	}
	if got != a {
		t.Fatalf("unexpected adapter instance")
	}
}

func TestAdapterRegistry_ForContractTypeOrMode_FallbackByMode(t *testing.T) {
	r := NewAdapterRegistry()
	a := &testAdapter{family: "policy-impact"}
	if err := r.Register(a); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := r.RegisterModeFallback(taxonomy.DomainModePolicyImpact, "policy-impact"); err != nil {
		t.Fatalf("register fallback: %v", err)
	}

	got, ok := r.ForContractTypeOrMode("", []string{"policy_impact"})
	if !ok || got == nil {
		t.Fatalf("expected adapter by domain mode fallback")
	}
	if got != a {
		t.Fatalf("unexpected adapter instance")
	}
}

func TestAdapterRegistry_ForContractTypeOrMode_PrefersContractType(t *testing.T) {
	r := NewAdapterRegistry()
	modeAdapter := &testAdapter{family: "policy-impact"}
	exactAdapter := &testAdapter{family: "vat-impact"}

	if err := r.Register(modeAdapter); err != nil {
		t.Fatalf("register mode adapter: %v", err)
	}
	if err := r.Register(exactAdapter); err != nil {
		t.Fatalf("register exact adapter: %v", err)
	}
	if err := r.RegisterModeFallback(taxonomy.DomainModePolicyImpact, "policy-impact"); err != nil {
		t.Fatalf("register fallback: %v", err)
	}

	got, ok := r.ForContractTypeOrMode("vat-impact", []string{taxonomy.DomainModePolicyImpact})
	if !ok || got == nil {
		t.Fatalf("expected adapter")
	}
	if got != exactAdapter {
		t.Fatalf("contract type lookup must have priority over mode fallback")
	}
}

func TestAdapterRegistry_RegisterValidation(t *testing.T) {
	r := NewAdapterRegistry()
	if err := r.Register(nil); err == nil {
		t.Fatalf("expected error for nil adapter")
	}
	if err := r.Register(&testAdapter{family: ""}); err == nil {
		t.Fatalf("expected error for empty family")
	}
	if err := r.RegisterModeFallback("", "policy-impact"); err == nil {
		t.Fatalf("expected error for empty mode")
	}
	if err := r.RegisterModeFallback(taxonomy.DomainModePolicyImpact, ""); err == nil {
		t.Fatalf("expected error for empty family in fallback")
	}
}

func TestDomainAdapterBinding_ContextRoundTrip(t *testing.T) {
	ctx := WithDomainAdapterBinding(context.Background(), DomainAdapterBinding{
		ContractType: " POLICY-IMPACT ",
		DomainModes:  []string{"policy-impact", "general"},
		EngineID:     " MACRO ",
	})
	got, ok := DomainAdapterBindingFromContext(ctx)
	if !ok {
		t.Fatalf("expected binding in context")
	}
	if got.ContractType != "policy-impact" {
		t.Fatalf("unexpected contract type: %q", got.ContractType)
	}
	wantModes := []string{taxonomy.DomainModePolicyImpact, taxonomy.DomainModeGeneral}
	if !reflect.DeepEqual(got.DomainModes, wantModes) {
		t.Fatalf("unexpected domain modes: got %v want %v", got.DomainModes, wantModes)
	}
	if got.EngineID != "macro" {
		t.Fatalf("unexpected engine id: %q", got.EngineID)
	}
}
