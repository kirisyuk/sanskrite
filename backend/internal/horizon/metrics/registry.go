package metrics

import (
	"context"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// Registry manages metric specifications and handlers.
type Registry struct {
	specs    map[string]*MetricSpec
	handlers map[string]Handler
}

// MetricSpec defines a metric specification.
type MetricSpec struct {
	Name                 string   `json:"name" firestore:"name"`
	Kind                 string   `json:"kind" firestore:"kind"` // "risk" | "opportunity" | "trend" | "direction" | "operational"
	Unit                 string   `json:"unit" firestore:"unit"` // "ratio" | "score" | "index"
	AggregationFunc      string   `json:"aggregationFunc" firestore:"aggregationFunc"` // "max" | "sum_capped" | "weighted_avg" | "min"
	AggregationCap       *float64 `json:"aggregationCap,omitempty" firestore:"aggregationCap,omitempty"`
	DecayFunc            string   `json:"decayFunc" firestore:"decayFunc"` // "none" | "exponential" | "linear" | "step"
	DecayHalfLife        *string  `json:"decayHalfLife,omitempty" firestore:"decayHalfLife,omitempty"` // "7d" | "30d" | "90d"
	PropagationThreshold float64  `json:"propagationThreshold" firestore:"propagationThreshold"`
	AlertThreshold       *float64 `json:"alertThreshold,omitempty" firestore:"alertThreshold,omitempty"`
	DerivedFrom          []string `json:"derivedFrom,omitempty" firestore:"derivedFrom,omitempty"`
}

// Handler processes metric values (aggregation, decay, transformation).
type Handler interface {
	Aggregate(inputs []horizonmodels.MetricValue) horizonmodels.MetricValue
	Decay(value horizonmodels.MetricValue, now time.Time) horizonmodels.MetricValue
}

// NewRegistry creates a new MetricRegistry.
func NewRegistry() *Registry {
	r := &Registry{
		specs:    make(map[string]*MetricSpec),
		handlers: make(map[string]Handler),
	}
	r.registerBuiltinHandlers()
	return r
}

// GetSpec returns the specification for a metric.
func (r *Registry) GetSpec(metricName string) *MetricSpec {
	return r.specs[metricName]
}

// GetHandler returns the handler for a metric.
func (r *Registry) GetHandler(metricName string) Handler {
	spec := r.specs[metricName]
	if spec == nil {
		return r.handlers["default"]
	}
	handler := r.handlers[spec.AggregationFunc]
	if handler == nil {
		return r.handlers["default"]
	}
	return handler
}

// RegisterSpec registers a metric specification.
func (r *Registry) RegisterSpec(spec *MetricSpec) {
	r.specs[spec.Name] = spec
}

// LoadSpecsFromRepo loads metric specs from a repository (e.g., Firestore).
func (r *Registry) LoadSpecsFromRepo(ctx context.Context, repo MetricSpecsRepo) error {
	specs, err := repo.GetSpecs(ctx)
	if err != nil {
		return err
	}
	for _, spec := range specs {
		r.RegisterSpec(spec)
	}
	return nil
}

// MetricSpecsRepo provides access to metric specifications.
type MetricSpecsRepo interface {
	GetSpecs(ctx context.Context) ([]*MetricSpec, error)
}

func (r *Registry) registerBuiltinHandlers() {
	r.handlers["max"] = &MaxHandler{}
	r.handlers["sum_capped"] = &SumCappedHandler{cap: 1.0}
	r.handlers["weighted_avg"] = &WeightedAvgHandler{}
	r.handlers["min"] = &MinHandler{}
	r.handlers["exponential"] = &ExponentialDecayHandler{HalfLife: 30 * 24 * time.Hour} // 30 days default
	r.handlers["default"] = &MaxHandler{} // Default to max
}

// MaxHandler aggregates by taking the maximum value.
type MaxHandler struct{}

func (h *MaxHandler) Aggregate(inputs []horizonmodels.MetricValue) horizonmodels.MetricValue {
	if len(inputs) == 0 {
		return horizonmodels.MetricValue{Value: 0, Confidence: 0}
	}
	max := inputs[0]
	for _, v := range inputs[1:] {
		if v.Value > max.Value {
			max = v
		}
	}
	return max
}

func (h *MaxHandler) Decay(value horizonmodels.MetricValue, now time.Time) horizonmodels.MetricValue {
	// No decay for max handler
	return value
}

// SumCappedHandler aggregates by summing values up to a cap.
type SumCappedHandler struct {
	cap float64
}

func (h *SumCappedHandler) Aggregate(inputs []horizonmodels.MetricValue) horizonmodels.MetricValue {
	sum := 0.0
	confSum := 0.0
	for _, v := range inputs {
		sum += v.Value
		confSum += v.Confidence
	}
	if sum > h.cap {
		sum = h.cap
	}
	return horizonmodels.MetricValue{
		Value:      sum,
		Confidence: confSum / float64(len(inputs)),
	}
}

func (h *SumCappedHandler) Decay(value horizonmodels.MetricValue, now time.Time) horizonmodels.MetricValue {
	// No decay for sum handler
	return value
}

// WeightedAvgHandler aggregates by weighted average.
type WeightedAvgHandler struct{}

func (h *WeightedAvgHandler) Aggregate(inputs []horizonmodels.MetricValue) horizonmodels.MetricValue {
	if len(inputs) == 0 {
		return horizonmodels.MetricValue{Value: 0, Confidence: 0}
	}
	sum := 0.0
	weightSum := 0.0
	for _, v := range inputs {
		weight := v.Confidence
		sum += v.Value * weight
		weightSum += weight
	}
	if weightSum == 0 {
		return horizonmodels.MetricValue{Value: 0, Confidence: 0}
	}
	return horizonmodels.MetricValue{
		Value:      sum / weightSum,
		Confidence: weightSum / float64(len(inputs)),
	}
}

func (h *WeightedAvgHandler) Decay(value horizonmodels.MetricValue, now time.Time) horizonmodels.MetricValue {
	// No decay for weighted avg handler
	return value
}

// MinHandler aggregates by taking the minimum value.
type MinHandler struct{}

func (h *MinHandler) Aggregate(inputs []horizonmodels.MetricValue) horizonmodels.MetricValue {
	if len(inputs) == 0 {
		return horizonmodels.MetricValue{Value: 0, Confidence: 0}
	}
	min := inputs[0]
	for _, v := range inputs[1:] {
		if v.Value < min.Value {
			min = v
		}
	}
	return min
}

func (h *MinHandler) Decay(value horizonmodels.MetricValue, now time.Time) horizonmodels.MetricValue {
	// No decay for min handler
	return value
}

// ExponentialDecayHandler applies exponential decay based on half-life.
type ExponentialDecayHandler struct {
	HalfLife time.Duration
}

func (h *ExponentialDecayHandler) Aggregate(inputs []horizonmodels.MetricValue) horizonmodels.MetricValue {
	// Use max for aggregation, decay is applied separately
	if len(inputs) == 0 {
		return horizonmodels.MetricValue{Value: 0, Confidence: 0}
	}
	max := inputs[0]
	for _, v := range inputs[1:] {
		if v.Value > max.Value {
			max = v
		}
	}
	return max
}

func (h *ExponentialDecayHandler) Decay(value horizonmodels.MetricValue, now time.Time) horizonmodels.MetricValue {
	if h.HalfLife <= 0 {
		return value // No decay if half-life not set
	}
	
	age := now.Sub(value.ObservedAt)
	if age <= 0 {
		return value
	}
	
	// Exponential decay: value * 0.5^(age/halfLife)
	decayFactor := 0.5
	exponent := float64(age) / float64(h.HalfLife)
	multiplier := pow(decayFactor, exponent)
	
	decayed := value
	decayed.Value = value.Value * multiplier
	decayed.Confidence = value.Confidence * multiplier
	return decayed
}

// pow computes base^exp for float64
func pow(base, exp float64) float64 {
	// Simple implementation using logarithms
	if base <= 0 {
		return 0
	}
	if exp == 0 {
		return 1
	}
	// exp(exp * ln(base))
	return expFunc(exp * lnFunc(base))
}

// expFunc is a simple exponential approximation
func expFunc(x float64) float64 {
	// Use Taylor series for small x, clamped for large x
	if x > 700 {
		return 1e308 // Avoid overflow
	}
	if x < -700 {
		return 0
	}
	// Use math.Exp equivalent via series for portability
	result := 1.0
	term := 1.0
	for i := 1; i < 100; i++ {
		term *= x / float64(i)
		result += term
		if term < 1e-15 && term > -1e-15 {
			break
		}
	}
	return result
}

// lnFunc is a simple natural log approximation
func lnFunc(x float64) float64 {
	if x <= 0 {
		return -1e308
	}
	if x == 1 {
		return 0
	}
	// Use identity: ln(x) = 2 * artanh((x-1)/(x+1)) for x > 0
	z := (x - 1) / (x + 1)
	result := 0.0
	term := z
	for i := 1; i < 200; i += 2 {
		result += term / float64(i)
		term *= z * z
		if term < 1e-15 && term > -1e-15 {
			break
		}
	}
	return 2 * result
}

