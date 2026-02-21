package propagation

import (
	"sort"
	"strings"
)

// AccumulatedWithSeed extends AccumulatedSignal with optional SeedPath for multi-seed scenarios.
type AccumulatedWithSeed struct {
	AccumulatedSignal
	SeedPath string
}

// MergeAccumulatedWithSeedDedup merges multiple (Accumulated, SeedPath) pairs.
// For each (fragmentID, seedPath, axis, polarity) keeps the entry with max amplitude (or min hops if equal).
// Callers (e.g. calibration tool, batch propagation) use this to dedupe when multiple runs
// contribute the same axis+polarity to the same node from the same seed.
func MergeAccumulatedWithSeedDedup(pairs []struct {
	Accumulated map[string][]AccumulatedSignal
	SeedPath    string
}) map[string][]AccumulatedWithSeed {
	type key struct {
		fragID   string
		seedPath string
		axis     string
		polarity string
	}
	best := make(map[key]AccumulatedWithSeed)
	for _, p := range pairs {
		seedPath := strings.TrimSpace(p.SeedPath)
		for fragID, arr := range p.Accumulated {
			fragID = strings.TrimSpace(fragID)
			if fragID == "" || len(arr) == 0 {
				continue
			}
			for _, a := range arr {
				axis := strings.TrimSpace(a.Axis)
				polarity := strings.TrimSpace(a.Polarity)
				if axis == "" {
					continue
				}
				k := key{fragID: fragID, seedPath: seedPath, axis: axis, polarity: polarity}
				cur, ok := best[k]
				if !ok || a.Amplitude > cur.Amplitude {
					best[k] = AccumulatedWithSeed{
						AccumulatedSignal: AccumulatedSignal{
							Axis:      axis,
							Amplitude: a.Amplitude,
							Polarity:  polarity,
							HopCount:  a.HopCount,
						},
						SeedPath: seedPath,
					}
					continue
				}
				if a.Amplitude == cur.Amplitude && a.HopCount < cur.HopCount {
					cur.HopCount = a.HopCount
					best[k] = cur
				}
			}
		}
	}
	out := make(map[string][]AccumulatedWithSeed)
	for k, v := range best {
		out[k.fragID] = append(out[k.fragID], v)
	}
	for fragID := range out {
		sort.Slice(out[fragID], func(i, j int) bool {
			if out[fragID][i].Axis != out[fragID][j].Axis {
				return out[fragID][i].Axis < out[fragID][j].Axis
			}
			if out[fragID][i].Polarity != out[fragID][j].Polarity {
				return out[fragID][i].Polarity < out[fragID][j].Polarity
			}
			return out[fragID][i].SeedPath < out[fragID][j].SeedPath
		})
	}
	return out
}
