package propagation

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
	"strconv"
	"strings"

	horizondomain "github.com/igorkirisyuk/sanskrite/internal/horizon/domain"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// ScenarioRunResult is the result of running a domain scenario (Encode → Propagate → Decode).
type ScenarioRunResult struct {
	ScenarioID        string                  `json:"scenarioId,omitempty"`
	ContractType      string                  `json:"contractType,omitempty"`
	DomainModes       []string                `json:"domainModes,omitempty"`
	EngineID          string                  `json:"engineId,omitempty"`
	Mode              string                  `json:"mode,omitempty"` // preview | apply
	SeedFragmentIDs   []string                `json:"seedFragmentIds,omitempty"`
	InputResolution   ScenarioInputResolution `json:"inputResolution,omitempty"`
	EncodedSignals    []ScenarioSignal        `json:"encodedSignals,omitempty"`
	SignalRuns        []ScenarioSignalRun     `json:"signalRuns,omitempty"`
	AffectedFragments []string                `json:"affectedFragments"`
	DomainMetrics     []ScenarioNodeMetrics   `json:"domainMetrics,omitempty"`
	PropagationStats  map[string]float64      `json:"propagationStats,omitempty"`
}

// ScenarioInputResolution describes how numeric scenario inputs were resolved.
type ScenarioInputResolution struct {
	Resolved     map[string]float64 `json:"resolved,omitempty"`     // param -> value
	ResolvedFrom map[string]string  `json:"resolvedFrom,omitempty"` // param -> request|fragment:<path>:<metric_key>
	Missing      []string           `json:"missing,omitempty"`      // unresolved canonical params
}

// ScenarioSignal is a compact representation of one encoded proxy signal.
type ScenarioSignal struct {
	ID           string   `json:"id,omitempty"`
	Axis         string   `json:"axis,omitempty"`
	Mechanism    string   `json:"mechanism,omitempty"`
	Amplitude    float64  `json:"amplitude"`
	Polarity     string   `json:"polarity,omitempty"`
	SeedPaths    []string `json:"seedPaths,omitempty"`
	LocationPath string   `json:"locationPath,omitempty"`
}

// ScenarioNodeMetrics holds domain metrics decoded for a single node.
type ScenarioNodeMetrics struct {
	FragmentID    string              `json:"fragmentId"`
	CanonicalPath string              `json:"canonicalPath,omitempty"`
	Accumulated   []AccumulatedSignal `json:"accumulated,omitempty"` // proxy signal basis used for Decode
	Metrics       []BusinessMetric    `json:"metrics"`
	Persisted     bool                `json:"persisted"` // true when decode metrics were written to fragment.State
}

// ScenarioSignalRun captures one encoded signal execution details (seeds, affected nodes, trace).
type ScenarioSignalRun struct {
	SignalID          string                        `json:"signalId,omitempty"`
	Axis              string                        `json:"axis,omitempty"`
	Polarity          string                        `json:"polarity,omitempty"`
	Mechanism         string                        `json:"mechanism,omitempty"`
	SeedFragmentIDs   []string                      `json:"seedFragmentIds,omitempty"`
	AffectedFragments []string                      `json:"affectedFragments,omitempty"`
	PathsTraced       int                           `json:"pathsTraced,omitempty"`
	Trace             []SignalPropagationTraceEvent `json:"trace,omitempty"`
	TraceDropped      int                           `json:"traceDropped,omitempty"`
}

// RunScenario executes the full encode → propagate → decode flow for a domain scenario.
// It requires PropagationEngine with adapter registry, ProfileLookup, and fragments repo.
func RunScenario(
	ctx context.Context,
	engine *PropagationEngine,
	profileLookup *ProfileLookup,
	fragmentsRepo *repo.HorizonFragmentsRepository,
	scenario ScenarioInput,
) (*ScenarioRunResult, error) {
	if engine == nil || profileLookup == nil || fragmentsRepo == nil {
		return nil, fmt.Errorf("scenario runner: engine, profileLookup, and fragmentsRepo are required")
	}

	mode := normalizeScenarioMode(scenario.Mode)
	contractType := strings.TrimSpace(scenario.ContractType)
	if contractType == "" {
		contractType = PolicyImpactContractTypeFamily
	}
	domainModes := taxonomy.NormalizeDomainModes(scenario.DomainModes)
	if len(domainModes) == 0 {
		domainModes = horizondomain.DomainModesForContractTypes([]string{contractType})
	}

	scenario.ContractType = contractType
	scenario.DomainModes = domainModes
	scenario.EngineID = strings.TrimSpace(scenario.EngineID)
	if scenario.EngineID == "" {
		if inferred, ok := firstStringFromMap(scenario.Parameters, "engine_id", "engineId", "model_engine", "modelEngine"); ok {
			scenario.EngineID = strings.TrimSpace(inferred)
		}
	}
	scenario.Mode = mode

	binding := DomainAdapterBinding{
		ContractType: scenario.ContractType,
		DomainModes:  append([]string(nil), scenario.DomainModes...),
		EngineID:     scenario.EngineID,
	}
	ctx = WithDomainAdapterBinding(ctx, binding)

	adapter := engine.resolveDomainAdapter(ctx, nil)
	if adapter == nil {
		return nil, fmt.Errorf("scenario runner: no adapter for contract type %q", scenario.ContractType)
	}

	// 0. Resolve scenario parameters from fragment.State (canonical keys from adapter)
	resolvedScenario, resolution := resolveScenarioParams(ctx, scenario, fragmentsRepo, adapter)
	scenario = resolvedScenario

	// 1. Encode
	signals, err := adapter.Encode(ctx, scenario)
	if err != nil {
		return nil, fmt.Errorf("scenario encode: %w", err)
	}
	encodedSignals := make([]ScenarioSignal, 0, len(signals))
	for _, sig := range signals {
		if sig == nil {
			continue
		}
		encodedSignals = append(encodedSignals, ScenarioSignal{
			ID:           strings.TrimSpace(sig.ID),
			Axis:         strings.TrimSpace(sig.Axis),
			Mechanism:    strings.TrimSpace(sig.Mechanism),
			Amplitude:    sig.Amplitude,
			Polarity:     strings.TrimSpace(sig.Polarity),
			SeedPaths:    append([]string(nil), sig.SeedPaths...),
			LocationPath: strings.TrimSpace(sig.LocationPath),
		})
	}

	result := &ScenarioRunResult{
		ScenarioID:      strings.TrimSpace(scenario.ScenarioID),
		ContractType:    scenario.ContractType,
		DomainModes:     append([]string(nil), scenario.DomainModes...),
		EngineID:        scenario.EngineID,
		Mode:            mode,
		SeedFragmentIDs: nil,
		InputResolution: resolution,
		EncodedSignals:  encodedSignals,
		SignalRuns:      make([]ScenarioSignalRun, 0, len(signals)),
		PropagationStats: map[string]float64{
			"signals": float64(len(signals)),
		},
	}
	if len(signals) == 0 {
		result.AffectedFragments = []string{}
		result.DomainMetrics = []ScenarioNodeMetrics{}
		return result, nil
	}

	// 2. Propagate each signal
	affectedIDs := make(map[string]bool)
	allSeedIDs := make(map[string]bool)
	previewAccumulated := make(map[string]map[string]AccumulatedSignal) // fragmentID -> axis|polarity -> accumulated
	propagationPathsN := 0.0
	explicitSeedIDs := normalizeFragmentIDs(scenario.SeedFragmentIDs)
	traceMaxSteps := scenario.MaxTraceSteps
	if traceMaxSteps <= 0 {
		traceMaxSteps = defaultSignalTraceMaxSteps
	}
	traceCtx := WithSignalTraceConfig(ctx, SignalTraceConfig{
		Enabled:  true,
		MaxSteps: traceMaxSteps,
	})

	for _, sig := range signals {
		if sig == nil {
			continue
		}
		seedIDs := append([]string(nil), explicitSeedIDs...)
		if len(seedIDs) == 0 {
			var err error
			seedIDs, err = engine.findSeedNodes(ctx, sig)
			if err != nil {
				continue
			}
			seedIDs = normalizeFragmentIDs(seedIDs)
		}
		if len(seedIDs) == 0 {
			continue
		}
		for _, seedID := range seedIDs {
			allSeedIDs[seedID] = true
		}

		run := ScenarioSignalRun{
			SignalID:        strings.TrimSpace(sig.ID),
			Axis:            strings.TrimSpace(sig.Axis),
			Polarity:        strings.TrimSpace(sig.Polarity),
			Mechanism:       strings.TrimSpace(sig.Mechanism),
			SeedFragmentIDs: append([]string(nil), seedIDs...),
		}

		log.Printf("[horizon-scenario] signal-run scenarioId=%s mode=%s signalId=%s axis=%s polarity=%s seeds=%d",
			result.ScenarioID,
			mode,
			run.SignalID,
			run.Axis,
			run.Polarity,
			len(seedIDs),
		)

		if len(seedIDs) == 0 {
			result.SignalRuns = append(result.SignalRuns, run)
			continue
		}

		result.PropagationStats["seeds_total"] += float64(len(seedIDs))

		if mode == ScenarioModePreview {
			simResult, err := engine.SimulateSignalFromSeeds(traceCtx, sig, profileLookup, seedIDs)
			if err != nil {
				result.SignalRuns = append(result.SignalRuns, run)
				continue
			}
			for _, fid := range simResult.AffectedFragments {
				affectedIDs[fid] = true
			}
			result.PropagationStats["nodes_updated"] += float64(len(simResult.AffectedFragments))
			propagationPathsN += float64(len(simResult.PropagationPaths))
			mergeAccumulatedMaps(previewAccumulated, simResult.Accumulated)

			run.AffectedFragments = append([]string(nil), simResult.AffectedFragments...)
			sort.Strings(run.AffectedFragments)
			run.PathsTraced = len(simResult.PropagationPaths)
			run.Trace = append([]SignalPropagationTraceEvent(nil), simResult.Trace...)
			run.TraceDropped = simResult.TraceDropped
			result.SignalRuns = append(result.SignalRuns, run)

			result.PropagationStats["trace_steps"] += float64(len(simResult.Trace))
			result.PropagationStats["trace_dropped"] += float64(simResult.TraceDropped)

			log.Printf("[horizon-scenario] signal-run done scenarioId=%s signalId=%s affected=%d paths=%d trace=%d dropped=%d",
				result.ScenarioID,
				run.SignalID,
				len(simResult.AffectedFragments),
				len(simResult.PropagationPaths),
				len(simResult.Trace),
				simResult.TraceDropped,
			)
			continue
		}

		applyResult, err := engine.PropagateSignalFromSeeds(traceCtx, sig, profileLookup, seedIDs)
		if err != nil {
			result.SignalRuns = append(result.SignalRuns, run)
			continue
		}
		for _, fid := range applyResult.AffectedFragments {
			affectedIDs[fid] = true
		}
		result.PropagationStats["nodes_updated"] += float64(len(applyResult.AffectedFragments))
		propagationPathsN += float64(len(applyResult.PropagationPaths))
		run.AffectedFragments = append([]string(nil), applyResult.AffectedFragments...)
		sort.Strings(run.AffectedFragments)
		run.PathsTraced = len(applyResult.PropagationPaths)
		run.Trace = append([]SignalPropagationTraceEvent(nil), applyResult.Trace...)
		run.TraceDropped = applyResult.TraceDropped
		result.SignalRuns = append(result.SignalRuns, run)
		result.PropagationStats["trace_steps"] += float64(len(applyResult.Trace))
		result.PropagationStats["trace_dropped"] += float64(applyResult.TraceDropped)

		log.Printf("[horizon-scenario] signal-run done scenarioId=%s signalId=%s affected=%d paths=%d trace=%d dropped=%d",
			result.ScenarioID,
			run.SignalID,
			len(applyResult.AffectedFragments),
			len(applyResult.PropagationPaths),
			len(applyResult.Trace),
			applyResult.TraceDropped,
		)
	}
	result.PropagationStats["paths_traced"] = propagationPathsN

	// 3. Decode and optionally persist domain metrics on affected nodes.
	result.DomainMetrics = make([]ScenarioNodeMetrics, 0, len(affectedIDs))
	for fid := range affectedIDs {
		frag, err := fragmentsRepo.Get(ctx, fid)
		if err != nil || frag == nil {
			continue
		}

		var accumulated []AccumulatedSignal
		if mode == ScenarioModePreview {
			accumulated = accumulatedFromMap(previewAccumulated[fid])
		} else {
			accumulated = CollectAccumulatedFromState(frag, "")
		}
		if len(accumulated) == 0 {
			continue
		}

		decoded, err := adapter.Decode(ctx, frag, accumulated, scenario)
		if err != nil || len(decoded) == 0 {
			continue
		}

		persisted := false
		if mode == ScenarioModeApply {
			if frag.State == nil {
				frag.State = make(map[string]horizonmodels.MetricValue)
			}
			for _, bm := range decoded {
				frag.State[bm.MetricPath] = horizonmodels.MetricValue{
					Value:      bm.Value,
					Unit:       strPtr(bm.Unit),
					Confidence: bm.Confidence,
					Provenance: horizonmodels.Provenance{Method: "scenario_decode"},
				}
			}
			if err := fragmentsRepo.Update(ctx, frag); err != nil {
				continue
			}
			persisted = true
		}

		result.DomainMetrics = append(result.DomainMetrics, ScenarioNodeMetrics{
			FragmentID:    frag.ID,
			CanonicalPath: frag.CanonicalPath,
			Accumulated:   accumulated,
			Metrics:       decoded,
			Persisted:     persisted,
		})
	}

	result.AffectedFragments = make([]string, 0, len(affectedIDs))
	for id := range affectedIDs {
		result.AffectedFragments = append(result.AffectedFragments, id)
	}
	sort.Strings(result.AffectedFragments)
	result.SeedFragmentIDs = make([]string, 0, len(allSeedIDs))
	for id := range allSeedIDs {
		result.SeedFragmentIDs = append(result.SeedFragmentIDs, id)
	}
	sort.Strings(result.SeedFragmentIDs)

	log.Printf("[horizon-scenario] done scenarioId=%s mode=%s contractType=%s signals=%d seeds=%d affected=%d decoded=%d trace=%d dropped=%d",
		result.ScenarioID,
		result.Mode,
		result.ContractType,
		len(result.EncodedSignals),
		len(result.SeedFragmentIDs),
		len(result.AffectedFragments),
		len(result.DomainMetrics),
		int(result.PropagationStats["trace_steps"]),
		int(result.PropagationStats["trace_dropped"]),
	)
	return result, nil
}

func normalizeScenarioMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case ScenarioModePreview:
		return ScenarioModePreview
	case "", ScenarioModeApply:
		return ScenarioModeApply
	default:
		return ScenarioModeApply
	}
}

func mergeAccumulatedMaps(dst map[string]map[string]AccumulatedSignal, src map[string][]AccumulatedSignal) {
	if len(src) == 0 {
		return
	}
	for fragmentID, arr := range src {
		fragmentID = strings.TrimSpace(fragmentID)
		if fragmentID == "" || len(arr) == 0 {
			continue
		}
		if dst[fragmentID] == nil {
			dst[fragmentID] = make(map[string]AccumulatedSignal)
		}
		for _, a := range arr {
			key := strings.TrimSpace(a.Axis) + "|" + strings.TrimSpace(a.Polarity)
			if key == "|" {
				continue
			}
			cur, ok := dst[fragmentID][key]
			if !ok || a.Amplitude > cur.Amplitude {
				dst[fragmentID][key] = a
				continue
			}
			if a.HopCount < cur.HopCount {
				cur.HopCount = a.HopCount
				dst[fragmentID][key] = cur
			}
		}
	}
}

func accumulatedFromMap(m map[string]AccumulatedSignal) []AccumulatedSignal {
	if len(m) == 0 {
		return nil
	}
	out := make([]AccumulatedSignal, 0, len(m))
	for _, a := range m {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Axis != out[j].Axis {
			return out[i].Axis < out[j].Axis
		}
		return out[i].Polarity < out[j].Polarity
	})
	return out
}

// CollectAccumulatedFromState extracts AccumulatedSignal from fragment.State proxy metrics.
// Proxy metrics are stored as /metric/{polarity}/{axis}/{mechanism} where axis may contain "/" (e.g. financial/settlement).
// If mechanism is non-empty, only metrics matching that mechanism are collected.
func CollectAccumulatedFromState(frag *horizonmodels.HorizonFragment, mechanism string) []AccumulatedSignal {
	if frag == nil || frag.State == nil {
		return nil
	}
	const prefix = "/metric/"
	axisAmplitude := make(map[string]AccumulatedSignal)

	for key, mv := range frag.State {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		rest := strings.TrimPrefix(key, prefix)
		parts := strings.Split(rest, "/")
		if len(parts) < 3 {
			continue
		}
		// rest = "polarity/axis0/axis1[/mechanism]"; all 12 axes have "/" (e.g. financial/settlement)
		if mechanism != "" && (len(parts) < 4 || strings.TrimSpace(parts[3]) != mechanism) {
			continue
		}
		axis := parts[1] + "/" + parts[2]
		if !horizonmodels.IsValidAxis(axis) {
			continue
		}
		amp := mv.Value
		if amp <= 0 {
			continue
		}
		polarity := strings.TrimSpace(parts[0])
		keyAxis := axis + "|" + polarity
		cur, ok := axisAmplitude[keyAxis]
		if !ok || amp > cur.Amplitude {
			axisAmplitude[keyAxis] = AccumulatedSignal{
				Axis:      axis,
				Amplitude: amp,
				Polarity:  polarity,
			}
		}
	}

	if len(axisAmplitude) == 0 {
		return nil
	}
	out := make([]AccumulatedSignal, 0, len(axisAmplitude))
	for _, a := range axisAmplitude {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Axis != out[j].Axis {
			return out[i].Axis < out[j].Axis
		}
		return out[i].Polarity < out[j].Polarity
	})
	return out
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// resolveScenarioParams merges fragment.State values into scenario.Parameters using adapter's InputParamSpecs.
// When a parameter is missing in the request, it is resolved from fragment.State of location or target nodes.
func resolveScenarioParams(
	ctx context.Context,
	scenario ScenarioInput,
	fragmentsRepo *repo.HorizonFragmentsRepository,
	adapter DomainModelAdapter,
) (ScenarioInput, ScenarioInputResolution) {
	resolution := ScenarioInputResolution{
		Resolved:     map[string]float64{},
		ResolvedFrom: map[string]string{},
		Missing:      []string{},
	}

	withInputs, ok := adapter.(DomainAdapterWithInputParams)
	if !ok {
		return scenario, resolution
	}
	specs := withInputs.InputParamSpecs()
	if len(specs) == 0 {
		return scenario, resolution
	}
	if scenario.Parameters == nil {
		scenario.Parameters = make(map[string]interface{})
	}

	// Collect paths by resolve source
	locationPaths := make([]string, 0)
	if locPath := scenarioLocationPath(scenario.Parameters); locPath != "" {
		locationPaths = append(locationPaths, locPath)
	}
	targetPaths := make([]string, 0)
	for _, p := range scenario.TargetPaths {
		p = strings.TrimSpace(p)
		if p != "" {
			targetPaths = append(targetPaths, p)
		}
	}

	type resolvedValue struct {
		Value      float64
		SourcePath string
	}
	fragmentValues := make(map[string]resolvedValue) // fragment metric key -> value + source path

	// Load fragments and collect State values by FragmentKey (first match wins)
	for _, spec := range specs {
		var paths []string
		switch spec.ResolveFrom {
		case "location":
			paths = locationPaths
		case "target":
			paths = targetPaths
		default:
			paths = append(append([]string{}, locationPaths...), targetPaths...)
		}
		for _, path := range paths {
			frag, err := fragmentsRepo.GetByCanonicalPath(ctx, path)
			if err != nil || frag == nil || frag.State == nil {
				continue
			}
			mv, ok := frag.State[spec.FragmentKey]
			if !ok {
				continue
			}
			v := mv.Value
			if math.IsNaN(v) || math.IsInf(v, 0) {
				continue
			}
			fragmentValues[spec.FragmentKey] = resolvedValue{Value: v, SourcePath: strings.TrimSpace(path)}
			break
		}
	}

	seenMissing := make(map[string]struct{})
	for _, spec := range specs {
		if len(spec.ParamKeys) == 0 {
			continue
		}
		primaryKey := strings.TrimSpace(spec.ParamKeys[0])
		if primaryKey == "" {
			continue
		}

		// Prefer explicit request value when present.
		requestSet := false
		for _, pk := range spec.ParamKeys {
			if v, ok := firstNumericFromMap(scenario.Parameters, pk); ok {
				resolution.Resolved[primaryKey] = v
				resolution.ResolvedFrom[primaryKey] = "request"
				requestSet = true
				break
			}
		}
		if requestSet {
			continue
		}

		// Else fill from fragment state resolution.
		if rv, ok := fragmentValues[spec.FragmentKey]; ok {
			scenario.Parameters[primaryKey] = rv.Value
			resolution.Resolved[primaryKey] = rv.Value
			resolution.ResolvedFrom[primaryKey] = "fragment:" + rv.SourcePath + ":" + strings.TrimSpace(spec.FragmentKey)
			continue
		}

		if _, already := seenMissing[primaryKey]; !already {
			seenMissing[primaryKey] = struct{}{}
			resolution.Missing = append(resolution.Missing, primaryKey)
		}
	}

	sort.Strings(resolution.Missing)
	return scenario, resolution
}

func scenarioLocationPath(params map[string]interface{}) string {
	if params == nil {
		return ""
	}
	if s, ok := firstStringFromMap(params, "location_path", "locationPath"); ok {
		return strings.TrimSpace(s)
	}
	if s, ok := firstStringFromMap(params, "jurisdiction", "country", "country_code", "countryCode"); ok {
		cc := strings.ToLower(strings.TrimSpace(s))
		if len(cc) == 2 || len(cc) == 3 {
			return "/loc/" + cc
		}
	}
	return ""
}

func firstNumericFromMap(m map[string]interface{}, keys ...string) (float64, bool) {
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
		case string:
			if f, err := strconv.ParseFloat(strings.TrimSpace(t), 64); err == nil {
				return f, true
			}
		}
	}
	return 0, false
}

func firstStringFromMap(m map[string]interface{}, keys ...string) (string, bool) {
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

func normalizeFragmentIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
