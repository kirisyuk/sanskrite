package propagation

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

const (
	// PolicyImpactContractTypeFamily is the registry key for policy-impact adapter.
	PolicyImpactContractTypeFamily = "policy-impact"
	defaultPolicyImpactLocation    = "/loc/global/na/na"

	// PolicyImpactEngineMacroEconomicalV1 is the first economic engine for policy impact modeling.
	PolicyImpactEngineMacroEconomicalV1 = "macro_economical_v1"
)

var policyImpactEngineAliases = map[string]string{
	"macro":                    PolicyImpactEngineMacroEconomicalV1,
	"macro_v1":                 PolicyImpactEngineMacroEconomicalV1,
	"macro-economical-v1":      PolicyImpactEngineMacroEconomicalV1,
	"macro_economical":         PolicyImpactEngineMacroEconomicalV1,
	"macro_economical_engine":  PolicyImpactEngineMacroEconomicalV1,
	"macroeconomicalengine":    PolicyImpactEngineMacroEconomicalV1,
	"macroeconomicalengine_v1": PolicyImpactEngineMacroEconomicalV1,
}

// PolicyMetricSpec describes one domain-native metric produced by a policy-impact engine.
type PolicyMetricSpec struct {
	MetricPath  string `json:"metricPath"`
	Unit        string `json:"unit"`
	Description string `json:"description"`
	BaselineKey string `json:"baselineKey,omitempty"`
}

// PolicyImpactEngine is a concrete policy-impact model implementation.
// Different engines may interpret the same propagated signals into different native metric sets.
type PolicyImpactEngine interface {
	EngineID() string
	MetricSpecs() []PolicyMetricSpec
	Encode(ctx context.Context, scenario ScenarioInput) ([]*horizonmodels.SignalDescriptor, error)
	TransferHook(ctx context.Context, signal *horizonmodels.SignalDescriptor, edge *horizonmodels.DependencyEdge, node *horizonmodels.HorizonFragment) (*TransferResult, error)
	Decode(ctx context.Context, node *horizonmodels.HorizonFragment, accumulated []AccumulatedSignal, scenario ScenarioInput) ([]BusinessMetric, error)
}

// PolicyImpactAdapter routes encode/transfer/decode to a selected policy-impact engine.
type PolicyImpactAdapter struct {
	engines         map[string]PolicyImpactEngine
	defaultEngineID string
}

func NewPolicyImpactAdapter() *PolicyImpactAdapter {
	a := &PolicyImpactAdapter{
		engines:         make(map[string]PolicyImpactEngine),
		defaultEngineID: PolicyImpactEngineMacroEconomicalV1,
	}
	_ = a.RegisterEngine(NewMacroEconomicalEngine())
	return a
}

func (a *PolicyImpactAdapter) ContractTypeFamily() string { return PolicyImpactContractTypeFamily }

func (a *PolicyImpactAdapter) RegisterEngine(engine PolicyImpactEngine) error {
	if a == nil {
		return fmt.Errorf("policy-impact adapter is nil")
	}
	if engine == nil {
		return fmt.Errorf("engine is nil")
	}
	id := normalizePolicyImpactEngineID(engine.EngineID())
	if id == "" {
		return fmt.Errorf("engine id is empty")
	}
	a.engines[id] = engine
	return nil
}

func (a *PolicyImpactAdapter) AvailableEngines() []string {
	if a == nil {
		return nil
	}
	out := make([]string, 0, len(a.engines))
	for id := range a.engines {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func (a *PolicyImpactAdapter) Encode(ctx context.Context, scenario ScenarioInput) ([]*horizonmodels.SignalDescriptor, error) {
	engine, err := a.resolveEngine(ctx, scenario, true)
	if err != nil {
		return nil, err
	}
	return engine.Encode(ctx, scenario)
}

func (a *PolicyImpactAdapter) TransferHook(ctx context.Context, signal *horizonmodels.SignalDescriptor, edge *horizonmodels.DependencyEdge, node *horizonmodels.HorizonFragment) (*TransferResult, error) {
	engine, err := a.resolveEngine(ctx, ScenarioInput{}, false)
	if err != nil {
		return nil, nil
	}
	return engine.TransferHook(ctx, signal, edge, node)
}

func (a *PolicyImpactAdapter) Decode(ctx context.Context, node *horizonmodels.HorizonFragment, accumulated []AccumulatedSignal, scenario ScenarioInput) ([]BusinessMetric, error) {
	engine, err := a.resolveEngine(ctx, scenario, true)
	if err != nil {
		return nil, err
	}
	return engine.Decode(ctx, node, accumulated, scenario)
}

func (a *PolicyImpactAdapter) resolveEngine(ctx context.Context, scenario ScenarioInput, strict bool) (PolicyImpactEngine, error) {
	if a == nil || len(a.engines) == 0 {
		return nil, fmt.Errorf("policy-impact adapter has no engines")
	}

	engineID, explicit := policyImpactEngineIDFromScenario(ctx, scenario)
	if engineID == "" {
		engineID = normalizePolicyImpactEngineID(a.defaultEngineID)
		explicit = false
	}
	if engine, ok := a.engines[engineID]; ok && engine != nil {
		return engine, nil
	}
	if strict && explicit {
		return nil, fmt.Errorf("policy-impact engine %q is not registered", engineID)
	}

	defaultID := normalizePolicyImpactEngineID(a.defaultEngineID)
	if engine, ok := a.engines[defaultID]; ok && engine != nil {
		return engine, nil
	}
	return nil, fmt.Errorf("policy-impact default engine %q is not registered", defaultID)
}

func policyImpactEngineIDFromScenario(ctx context.Context, scenario ScenarioInput) (string, bool) {
	if scenario.Parameters != nil {
		if v, ok := firstString(scenario.Parameters, "engine_id", "engineId", "model_engine", "modelEngine"); ok {
			return normalizePolicyImpactEngineID(v), true
		}
	}
	if binding, ok := DomainAdapterBindingFromContext(ctx); ok {
		if v := normalizePolicyImpactEngineID(binding.EngineID); v != "" {
			return v, true
		}
	}
	return "", false
}

func normalizePolicyImpactEngineID(v string) string {
	v = normalizeAdapterKey(v)
	if v == "" {
		return ""
	}
	if mapped, ok := policyImpactEngineAliases[v]; ok {
		return mapped
	}
	return v
}

func policyImpactVatDelta(params map[string]interface{}) (float64, bool) {
	if v, ok := firstNumeric(params, "vat_rate_delta", "vatRateDelta"); ok {
		return v, true
	}
	from, hasFrom := firstNumeric(params, "vat_rate_from", "vatRateFrom", "vat_from")
	to, hasTo := firstNumeric(params, "vat_rate_to", "vatRateTo", "vat_to")
	if hasFrom && hasTo {
		return to - from, true
	}
	return 0, false
}

func policyImpactVatRateBounds(params map[string]interface{}) (from float64, to float64, ok bool) {
	from, hasFrom := firstNumeric(params, "vat_rate_from", "vatRateFrom", "vat_from")
	to, hasTo := firstNumeric(params, "vat_rate_to", "vatRateTo", "vat_to")
	if hasFrom && hasTo {
		return from, to, true
	}
	if delta, hasDelta := firstNumeric(params, "vat_rate_delta", "vatRateDelta"); hasDelta && hasFrom {
		return from, from + delta, true
	}
	return 0, 0, false
}

func policyImpactLocationPath(params map[string]interface{}) string {
	if params == nil {
		return ""
	}
	if s, ok := firstString(params, "location_path", "locationPath"); ok {
		return s
	}
	if s, ok := firstString(params, "jurisdiction", "country", "country_code", "countryCode"); ok {
		cc := strings.ToLower(strings.TrimSpace(s))
		if len(cc) == 2 || taxonomy.IsRegionalGeoToken(cc) {
			return "/loc/" + cc + "/na/na"
		}
	}
	return ""
}

func normalizeCanonicalPaths(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, p := range in {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

func firstNumeric(m map[string]interface{}, keys ...string) (float64, bool) {
	if m == nil {
		return 0, false
	}
	for _, key := range keys {
		raw, ok := m[key]
		if !ok || raw == nil {
			continue
		}
		switch t := raw.(type) {
		case float64:
			return t, true
		case float32:
			return float64(t), true
		case int:
			return float64(t), true
		case int64:
			return float64(t), true
		case int32:
			return float64(t), true
		case uint:
			return float64(t), true
		case uint64:
			return float64(t), true
		case json.Number:
			if f, err := t.Float64(); err == nil {
				return f, true
			}
		case string:
			if f, err := strconv.ParseFloat(strings.TrimSpace(t), 64); err == nil {
				return f, true
			}
		}
	}
	return 0, false
}

func firstString(m map[string]interface{}, keys ...string) (string, bool) {
	if m == nil {
		return "", false
	}
	for _, key := range keys {
		raw, ok := m[key]
		if !ok || raw == nil {
			continue
		}
		if s, ok := raw.(string); ok {
			s = strings.TrimSpace(s)
			if s != "" {
				return s, true
			}
		}
	}
	return "", false
}

func qualifierFloat(q map[string]interface{}, keys ...string) (float64, bool) {
	return firstNumeric(q, keys...)
}

func qualifierString(q map[string]interface{}, keys ...string) string {
	if s, ok := firstString(q, keys...); ok {
		return s
	}
	return ""
}

func clamp(v, minV, maxV float64) float64 {
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}

func clamp01(v float64) float64 {
	return clamp(v, 0.0, 1.0)
}
