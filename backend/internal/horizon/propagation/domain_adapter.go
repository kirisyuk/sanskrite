package propagation

import (
	"context"
	"fmt"
	"strings"
	"sync"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// ScenarioInput describes a domain-modeling scenario applied to the graph.
// The fields are intentionally generic so each contract family can define its own parameters.
type ScenarioInput struct {
	ScenarioID   string                 `json:"scenarioId"`
	ContractType string                 `json:"contractType"`
	Parameters   map[string]interface{} `json:"parameters,omitempty"`
	BaselineRef  string                 `json:"baselineRef,omitempty"`
	TargetPaths  []string               `json:"targetPaths,omitempty"`
}

// TransferResult is a domain-specific propagation adjustment returned by adapter hooks.
type TransferResult struct {
	AdjustedAmplitude float64 `json:"adjustedAmplitude"`
	AdjustedAxis      string  `json:"adjustedAxis,omitempty"` // empty => keep current axis
	Skip              bool    `json:"skip,omitempty"`         // true => block propagation over this edge
}

// AccumulatedSignal represents a compact signal view for endpoint decoding.
type AccumulatedSignal struct {
	Axis      string  `json:"axis"`
	Amplitude float64 `json:"amplitude"`
	Polarity  string  `json:"polarity"`
	HopCount  int     `json:"hopCount"`
}

// BusinessMetric is a domain-level metric produced by adapter Decode on endpoint nodes.
type BusinessMetric struct {
	MetricPath string  `json:"metricPath"`
	Value      float64 `json:"value"`
	Unit       string  `json:"unit,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
}

// DomainModelAdapter encapsulates domain-specific math/algorithmics around signal propagation.
// Binding is done by contract type family, with optional fallback by domain mode.
type DomainModelAdapter interface {
	// ContractTypeFamily returns the primary registry key for this adapter.
	ContractTypeFamily() string
	// Encode converts a scenario into proxy signals compatible with existing propagation basis.
	Encode(ctx context.Context, scenario ScenarioInput) ([]*horizonmodels.SignalDescriptor, error)
	// TransferHook allows domain-specific adjustments during signal traversal.
	TransferHook(ctx context.Context, signal *horizonmodels.SignalDescriptor, edge *horizonmodels.DependencyEdge, node *horizonmodels.HorizonFragment) (*TransferResult, error)
	// Decode projects accumulated proxy signals into business metrics on endpoint nodes.
	Decode(ctx context.Context, node *horizonmodels.HorizonFragment, accumulated []AccumulatedSignal, scenario ScenarioInput) ([]BusinessMetric, error)
}

// AdapterRegistry stores adapters by contract type family and optional fallback mapping by domain mode.
type AdapterRegistry struct {
	mu           sync.RWMutex
	adapters     map[string]DomainModelAdapter // contract type family -> adapter
	modeFallback map[string]string             // domain mode -> contract type family
}

// DomainAdapterBinding carries contract/mode routing hints for adapter selection during propagation.
// It is optional and can be attached to context by callers (e.g., what-if simulation endpoints).
type DomainAdapterBinding struct {
	ContractType string
	DomainModes  []string
	EngineID     string
}

type domainAdapterBindingCtxKey struct{}

// NewAdapterRegistry creates an empty adapter registry.
func NewAdapterRegistry() *AdapterRegistry {
	return &AdapterRegistry{
		adapters:     make(map[string]DomainModelAdapter),
		modeFallback: make(map[string]string),
	}
}

// Register stores adapter by its ContractTypeFamily().
func (r *AdapterRegistry) Register(adapter DomainModelAdapter) error {
	if adapter == nil {
		return fmt.Errorf("adapter is nil")
	}
	family := normalizeAdapterKey(adapter.ContractTypeFamily())
	if family == "" {
		return fmt.Errorf("adapter contract type family is empty")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[family] = adapter
	return nil
}

// RegisterModeFallback maps domain mode to contract type family.
// It is valid to register fallback before adapter itself; lookup just won't resolve until adapter is registered.
func (r *AdapterRegistry) RegisterModeFallback(mode, contractTypeFamily string) error {
	mode = normalizeAdapterKey(mode)
	contractTypeFamily = normalizeAdapterKey(contractTypeFamily)
	if mode == "" {
		return fmt.Errorf("domain mode is empty")
	}
	if contractTypeFamily == "" {
		return fmt.Errorf("contract type family is empty")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.modeFallback[mode] = contractTypeFamily
	return nil
}

// ForContractType returns adapter for exact contract type key.
func (r *AdapterRegistry) ForContractType(contractType string) (DomainModelAdapter, bool) {
	if r == nil {
		return nil, false
	}
	key := normalizeAdapterKey(contractType)
	if key == "" {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	adapter, ok := r.adapters[key]
	return adapter, ok
}

// ForContractTypeOrMode resolves adapter by contract type first, then by fallback domain modes.
func (r *AdapterRegistry) ForContractTypeOrMode(contractType string, domainModes []string) (DomainModelAdapter, bool) {
	if r == nil {
		return nil, false
	}
	if adapter, ok := r.ForContractType(contractType); ok {
		return adapter, true
	}

	modes := taxonomy.NormalizeDomainModes(domainModes)
	if len(modes) == 0 {
		return nil, false
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, mode := range modes {
		mode = normalizeAdapterKey(mode)
		family, ok := r.modeFallback[mode]
		if !ok || family == "" {
			continue
		}
		if adapter, ok := r.adapters[family]; ok {
			return adapter, true
		}
	}
	return nil, false
}

func normalizeAdapterKey(v string) string {
	return strings.TrimSpace(strings.ToLower(v))
}

// WithDomainAdapterBinding stores adapter routing hints in context.
func WithDomainAdapterBinding(ctx context.Context, binding DomainAdapterBinding) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	binding.ContractType = normalizeAdapterKey(binding.ContractType)
	binding.DomainModes = taxonomy.NormalizeDomainModes(binding.DomainModes)
	binding.EngineID = normalizeAdapterKey(binding.EngineID)
	return context.WithValue(ctx, domainAdapterBindingCtxKey{}, binding)
}

// DomainAdapterBindingFromContext reads adapter routing hints from context.
func DomainAdapterBindingFromContext(ctx context.Context) (DomainAdapterBinding, bool) {
	if ctx == nil {
		return DomainAdapterBinding{}, false
	}
	raw := ctx.Value(domainAdapterBindingCtxKey{})
	if raw == nil {
		return DomainAdapterBinding{}, false
	}
	binding, ok := raw.(DomainAdapterBinding)
	if !ok {
		return DomainAdapterBinding{}, false
	}
	binding.ContractType = normalizeAdapterKey(binding.ContractType)
	binding.DomainModes = taxonomy.NormalizeDomainModes(binding.DomainModes)
	binding.EngineID = normalizeAdapterKey(binding.EngineID)
	return binding, true
}
