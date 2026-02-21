package propagation

import (
	"context"
	"strings"
)

const defaultSignalTraceMaxSteps = 2500

// SignalTraceConfig controls propagation trace collection.
type SignalTraceConfig struct {
	Enabled  bool
	MaxSteps int
}

// SignalPropagationTraceEvent is one propagation step for diagnostics/explainability.
type SignalPropagationTraceEvent struct {
	Step         int                `json:"step"`
	SignalID     string             `json:"signalId,omitempty"`
	FromFragment string             `json:"fromFragmentId,omitempty"`
	ToFragment   string             `json:"toFragmentId,omitempty"`
	FromPath     string             `json:"fromPath,omitempty"`
	ToPath       string             `json:"toPath,omitempty"`
	FromGeo      string             `json:"fromGeo,omitempty"`
	ToGeo        string             `json:"toGeo,omitempty"`
	EdgeID       string             `json:"edgeId,omitempty"`
	EdgeType     string             `json:"edgeType,omitempty"`
	Direction    string             `json:"direction,omitempty"` // source_to_target | target_to_source
	Channel      string             `json:"channel,omitempty"`   // regulatory | economic | informational | geopolitical | operational | mixed
	Scope        string             `json:"scope,omitempty"`     // domestic | cross_border | unknown
	Spillover    string             `json:"spillover,omitempty"` // direct_domestic | cross_border_economic | ...
	AxisIn       string             `json:"axisIn,omitempty"`
	AxisOut      string             `json:"axisOut,omitempty"`
	AmplitudeIn  float64            `json:"amplitudeIn,omitempty"`
	AmplitudeOut float64            `json:"amplitudeOut,omitempty"`
	Hops         int                `json:"hops,omitempty"`
	Outcome      string             `json:"outcome,omitempty"` // seed | node_updated | propagated | dropped
	Reason       string             `json:"reason,omitempty"`  // below_threshold | visited_stronger | hop_limit | transfer_skip | ...
	Factors      map[string]float64 `json:"factors,omitempty"` // Per-step multiplier/physics breakdown for explainability
}

type signalTraceConfigCtxKey struct{}

// WithSignalTraceConfig stores trace config in context.
func WithSignalTraceConfig(ctx context.Context, cfg SignalTraceConfig) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg.MaxSteps <= 0 {
		cfg.MaxSteps = defaultSignalTraceMaxSteps
	}
	return context.WithValue(ctx, signalTraceConfigCtxKey{}, cfg)
}

func signalTraceConfigFromContext(ctx context.Context) SignalTraceConfig {
	if ctx == nil {
		return SignalTraceConfig{Enabled: false, MaxSteps: defaultSignalTraceMaxSteps}
	}
	raw := ctx.Value(signalTraceConfigCtxKey{})
	if raw == nil {
		return SignalTraceConfig{Enabled: false, MaxSteps: defaultSignalTraceMaxSteps}
	}
	cfg, ok := raw.(SignalTraceConfig)
	if !ok {
		return SignalTraceConfig{Enabled: false, MaxSteps: defaultSignalTraceMaxSteps}
	}
	if cfg.MaxSteps <= 0 {
		cfg.MaxSteps = defaultSignalTraceMaxSteps
	}
	return cfg
}

type signalTraceCollector struct {
	enabled bool
	max     int
	events  []SignalPropagationTraceEvent
	dropped int
}

func newSignalTraceCollector(cfg SignalTraceConfig) *signalTraceCollector {
	maxSteps := cfg.MaxSteps
	if maxSteps <= 0 {
		maxSteps = defaultSignalTraceMaxSteps
	}
	return &signalTraceCollector{
		enabled: cfg.Enabled,
		max:     maxSteps,
		events:  make([]SignalPropagationTraceEvent, 0, minInt(maxSteps, 256)),
	}
}

func (c *signalTraceCollector) add(ev SignalPropagationTraceEvent) {
	if c == nil || !c.enabled {
		return
	}
	if len(c.events) >= c.max {
		c.dropped++
		return
	}
	ev.SignalID = strings.TrimSpace(ev.SignalID)
	ev.FromFragment = strings.TrimSpace(ev.FromFragment)
	ev.ToFragment = strings.TrimSpace(ev.ToFragment)
	ev.FromPath = strings.TrimSpace(ev.FromPath)
	ev.ToPath = strings.TrimSpace(ev.ToPath)
	ev.FromGeo = strings.TrimSpace(ev.FromGeo)
	ev.ToGeo = strings.TrimSpace(ev.ToGeo)
	ev.EdgeID = strings.TrimSpace(ev.EdgeID)
	ev.EdgeType = strings.TrimSpace(ev.EdgeType)
	ev.Direction = strings.TrimSpace(ev.Direction)
	ev.Channel = strings.TrimSpace(ev.Channel)
	ev.Scope = strings.TrimSpace(ev.Scope)
	ev.Spillover = strings.TrimSpace(ev.Spillover)
	ev.AxisIn = strings.TrimSpace(ev.AxisIn)
	ev.AxisOut = strings.TrimSpace(ev.AxisOut)
	ev.Outcome = strings.TrimSpace(ev.Outcome)
	ev.Reason = strings.TrimSpace(ev.Reason)
	if len(ev.Factors) > 0 {
		clean := make(map[string]float64, len(ev.Factors))
		for k, v := range ev.Factors {
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			clean[k] = v
		}
		if len(clean) > 0 {
			ev.Factors = clean
		} else {
			ev.Factors = nil
		}
	}
	ev.Step = len(c.events) + 1
	c.events = append(c.events, ev)
}

func (c *signalTraceCollector) snapshot() ([]SignalPropagationTraceEvent, int) {
	if c == nil || !c.enabled {
		return nil, 0
	}
	out := make([]SignalPropagationTraceEvent, len(c.events))
	copy(out, c.events)
	return out, c.dropped
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (c *signalTraceCollector) isEnabled() bool {
	return c != nil && c.enabled
}
