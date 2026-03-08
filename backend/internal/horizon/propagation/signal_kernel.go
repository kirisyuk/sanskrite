package propagation

import (
	"context"
	"fmt"
	"sort"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

const (
	SignalKernelModeBounded  = "bounded"
	SignalKernelModeTypedRWR = "typed_rwr"
	SignalKernelModeHybrid   = "hybrid"
)

type SignalKernelConfig struct {
	Mode               string
	RestartProbability float64
	MaxIterations      int
	ExpansionHops      int
	MinTailScore       float64
	MaxTailNodes       int
	IncludeSeedNodes   bool
}

type signalKernelConfigCtxKey struct{}

func WithSignalKernelConfig(ctx context.Context, cfg SignalKernelConfig) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	cfg = normalizeSignalKernelConfig(cfg)
	return context.WithValue(ctx, signalKernelConfigCtxKey{}, cfg)
}

func signalKernelConfigFromContext(ctx context.Context) SignalKernelConfig {
	if ctx == nil {
		return normalizeSignalKernelConfig(SignalKernelConfig{})
	}
	raw := ctx.Value(signalKernelConfigCtxKey{})
	if raw == nil {
		return normalizeSignalKernelConfig(SignalKernelConfig{})
	}
	cfg, ok := raw.(SignalKernelConfig)
	if !ok {
		return normalizeSignalKernelConfig(SignalKernelConfig{})
	}
	return normalizeSignalKernelConfig(cfg)
}

func DefaultHeuristicSignalKernelConfig(signal *horizonmodels.SignalDescriptor) SignalKernelConfig {
	cfg := SignalKernelConfig{
		Mode:               SignalKernelModeHybrid,
		RestartProbability: 0.34,
		MaxIterations:      18,
		ExpansionHops:      2,
		MinTailScore:       0.035,
		MaxTailNodes:       12,
		IncludeSeedNodes:   true,
	}
	if signal == nil {
		return cfg
	}
	switch strings.TrimSpace(signal.Axis) {
	case horizonmodels.AxisGeoConflict:
		cfg.RestartProbability = 0.36
		cfg.MaxIterations = 20
		cfg.ExpansionHops = 3
		cfg.MinTailScore = 0.030
		cfg.MaxTailNodes = 14
	case horizonmodels.AxisLogisticsCapacity, horizonmodels.AxisLogisticsLatency:
		cfg.RestartProbability = 0.32
		cfg.MaxIterations = 20
		cfg.ExpansionHops = 3
		cfg.MinTailScore = 0.028
		cfg.MaxTailNodes = 16
	case horizonmodels.AxisPhysicalFlowSupply, horizonmodels.AxisPhysicalFlowDemand, horizonmodels.AxisOperationalContinuity:
		cfg.RestartProbability = 0.30
		cfg.MaxIterations = 18
		cfg.ExpansionHops = 3
		cfg.MinTailScore = 0.030
		cfg.MaxTailNodes = 14
	default:
		cfg.Mode = SignalKernelModeBounded
	}
	return cfg
}

func normalizeSignalKernelConfig(cfg SignalKernelConfig) SignalKernelConfig {
	switch strings.ToLower(strings.TrimSpace(cfg.Mode)) {
	case SignalKernelModeTypedRWR:
		cfg.Mode = SignalKernelModeTypedRWR
	case SignalKernelModeHybrid:
		cfg.Mode = SignalKernelModeHybrid
	default:
		cfg.Mode = SignalKernelModeBounded
	}
	if cfg.RestartProbability <= 0 || cfg.RestartProbability >= 1 {
		cfg.RestartProbability = 0.34
	}
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = 18
	}
	if cfg.ExpansionHops < 0 {
		cfg.ExpansionHops = 0
	}
	if cfg.MinTailScore <= 0 {
		cfg.MinTailScore = 0.035
	}
	if cfg.MaxTailNodes <= 0 {
		cfg.MaxTailNodes = 12
	}
	return cfg
}

func mergeSignalPropagationResults(base, tail *SignalPropagationResult) *SignalPropagationResult {
	if base == nil {
		return tail
	}
	if tail == nil {
		return base
	}
	merged := &SignalPropagationResult{
		AffectedFragments: mergeStringSets(base.AffectedFragments, tail.AffectedFragments),
		PropagationPaths:  mergePathLists(base.PropagationPaths, tail.PropagationPaths),
		PropagationTime:   base.PropagationTime + tail.PropagationTime,
		Metrics:           mergeMetricMaps(base.Metrics, tail.Metrics),
		Trace:             append(append([]SignalPropagationTraceEvent{}, base.Trace...), tail.Trace...),
		TraceDropped:      base.TraceDropped + tail.TraceDropped,
	}
	merged.Metrics["hybrid_runs"] = 2
	merged.Metrics["hybrid_tail_paths"] = float64(len(tail.PropagationPaths))
	merged.Metrics["hybrid_tail_nodes"] = float64(len(tail.AffectedFragments))
	return merged
}

func mergeSignalSimulationResults(base, tail *SignalSimulationResult) *SignalSimulationResult {
	if base == nil {
		return tail
	}
	if tail == nil {
		return base
	}
	merged := &SignalSimulationResult{
		AffectedFragments: mergeStringSets(base.AffectedFragments, tail.AffectedFragments),
		PropagationPaths:  mergePathLists(base.PropagationPaths, tail.PropagationPaths),
		PropagationTime:   base.PropagationTime + tail.PropagationTime,
		Metrics:           mergeMetricMaps(base.Metrics, tail.Metrics),
		Accumulated:       mergeAccumulatedResults(base.Accumulated, tail.Accumulated),
		Trace:             append(append([]SignalPropagationTraceEvent{}, base.Trace...), tail.Trace...),
		TraceDropped:      base.TraceDropped + tail.TraceDropped,
	}
	merged.Metrics["hybrid_runs"] = 2
	merged.Metrics["hybrid_tail_paths"] = float64(len(tail.PropagationPaths))
	merged.Metrics["hybrid_tail_nodes"] = float64(len(tail.AffectedFragments))
	return merged
}

func mergeStringSets(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, item := range append(append([]string{}, a...), b...) {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func mergePathLists(a, b [][]string) [][]string {
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([][]string, 0, len(a)+len(b))
	appendPath := func(path []string) {
		key := strings.Join(path, "|")
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, append([]string{}, path...))
	}
	for _, path := range a {
		appendPath(path)
	}
	for _, path := range b {
		appendPath(path)
	}
	return out
}

func mergeMetricMaps(a, b map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(a)+len(b)+4)
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		if _, ok := out[k]; ok {
			out[k] += v
			continue
		}
		out[k] = v
	}
	return out
}

func mergeAccumulatedResults(a, b map[string][]AccumulatedSignal) map[string][]AccumulatedSignal {
	if len(a) == 0 && len(b) == 0 {
		return map[string][]AccumulatedSignal{}
	}
	out := make(map[string][]AccumulatedSignal, len(a)+len(b))
	mergeOne := func(src map[string][]AccumulatedSignal) {
		for fragmentID, items := range src {
			keyed := make(map[string]AccumulatedSignal)
			for _, existing := range out[fragmentID] {
				keyed[existing.Axis+"|"+existing.Polarity] = existing
			}
			for _, item := range items {
				key := item.Axis + "|" + item.Polarity
				cur, ok := keyed[key]
				if !ok || item.Amplitude > cur.Amplitude || (item.Amplitude == cur.Amplitude && item.HopCount < cur.HopCount) {
					keyed[key] = item
				}
			}
			merged := make([]AccumulatedSignal, 0, len(keyed))
			for _, item := range keyed {
				merged = append(merged, item)
			}
			out[fragmentID] = merged
		}
	}
	mergeOne(a)
	mergeOne(b)
	return out
}

func signalKernelModeMetric(mode string) map[string]float64 {
	mode = strings.TrimSpace(mode)
	metrics := map[string]float64{
		"kernel_mode_bounded":   0,
		"kernel_mode_typed_rwr": 0,
		"kernel_mode_hybrid":    0,
	}
	switch mode {
	case SignalKernelModeTypedRWR:
		metrics["kernel_mode_typed_rwr"] = 1
	case SignalKernelModeHybrid:
		metrics["kernel_mode_hybrid"] = 1
	default:
		metrics["kernel_mode_bounded"] = 1
	}
	return metrics
}

func requireProfileLookup(profileLookup *ProfileLookup) error {
	if profileLookup == nil {
		return fmt.Errorf("profileLookup is nil")
	}
	return nil
}
