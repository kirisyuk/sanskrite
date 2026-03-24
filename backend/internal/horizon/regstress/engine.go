package regstress

import (
	"math"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// RegulatoryImpactLeg is one hop in a regulatory transmission path.
type RegulatoryImpactLeg struct {
	FromPath       string  `json:"fromPath"`
	ToPath         string  `json:"toPath"`
	RelationType   string  `json:"relationType"`
	PassThrough    float64 `json:"passThrough"`
	ElasticityHint float64 `json:"elasticityHint,omitempty"`
	LagQuarters    float64 `json:"lagQuarters,omitempty"`
}

// RegulatoryImpactPath is an ordered regulatory impact chain.
type RegulatoryImpactPath struct {
	Legs       []RegulatoryImpactLeg `json:"legs"`
	Confidence float64               `json:"confidence"`
}

// RegulatoryPathStress aggregates simple multiplicative exposure along a path.
type RegulatoryPathStress struct {
	CumulativeExposure float64 `json:"cumulativeExposure"`
	WorstLagQuarters   float64 `json:"worstLagQuarters"`
	LegCount           int     `json:"legCount"`
	// ObservabilityScore estimates how traceable the transmission path is in data (0=opaque, 1=highly observable).
	ObservabilityScore float64 `json:"observabilityScore"`
}

// EvaluatePathStress computes Π(passThrough) with per-leg defaults from qualifiers on discovered relations.
func EvaluatePathStress(path RegulatoryImpactPath) RegulatoryPathStress {
	st := RegulatoryPathStress{LegCount: len(path.Legs)}
	if len(path.Legs) == 0 {
		return st
	}
	prod := 1.0
	maxLag := 0.0
	for _, leg := range path.Legs {
		pt := leg.PassThrough
		if pt <= 0 {
			pt = 0.85
		}
		pt = clamp(pt, 0.1, 1.35)
		prod *= pt
		if leg.LagQuarters > maxLag {
			maxLag = leg.LagQuarters
		}
	}
	st.CumulativeExposure = clamp(prod, 0, 2.5)
	st.WorstLagQuarters = maxLag
	// Longer paths with more relation hops are harder to observe end-to-end; higher pass-through concentration helps.
	obs := 0.42
	if len(path.Legs) > 0 {
		obs += 0.18 * (1.0 - minFloat(1.0, float64(len(path.Legs))/6.0))
		if st.CumulativeExposure < 0.75 {
			obs += 0.12
		}
	}
	if path.Confidence > 0 {
		obs += 0.15 * clamp(path.Confidence, 0, 1)
	}
	st.ObservabilityScore = clamp(obs, 0, 1)
	return st
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

const maxRegulatoryPathsFromRelations = 32

// BuildPathsFromRelations extracts bounded mechanism-rooted paths (BFS, acyclic by node visit) from flat relations.
func BuildPathsFromRelations(rels []agents.DiscoveredRelation, _ string, maxHops int) []RegulatoryImpactPath {
	if maxHops <= 0 {
		maxHops = 5
	}
	bySrc := map[string][]agents.DiscoveredRelation{}
	for _, r := range rels {
		s := strings.TrimSpace(r.SourceCanonicalPath)
		if s == "" {
			continue
		}
		bySrc[s] = append(bySrc[s], r)
	}
	roots := make([]string, 0, 8)
	seenRoot := map[string]struct{}{}
	for _, r := range rels {
		for _, p := range []string{strings.TrimSpace(r.SourceCanonicalPath), strings.TrimSpace(r.TargetCanonicalPath)} {
			if strings.HasPrefix(p, "/mechanism/") {
				if _, ok := seenRoot[p]; ok {
					continue
				}
				seenRoot[p] = struct{}{}
				roots = append(roots, p)
			}
		}
	}
	var out []RegulatoryImpactPath
	for _, root := range roots {
		type state struct {
			at   string
			legs []RegulatoryImpactLeg
			vis  map[string]struct{}
		}
		queue := []state{{at: root, legs: nil, vis: map[string]struct{}{root: {}}}}
		for len(queue) > 0 && len(out) < maxRegulatoryPathsFromRelations {
			cur := queue[0]
			queue = queue[1:]
			if len(cur.legs) >= maxHops {
				if len(cur.legs) > 0 {
					out = append(out, RegulatoryImpactPath{Legs: append([]RegulatoryImpactLeg(nil), cur.legs...), Confidence: 0.72})
				}
				continue
			}
			for _, r := range bySrc[cur.at] {
				tgt := strings.TrimSpace(r.TargetCanonicalPath)
				if tgt == "" {
					continue
				}
				if _, dup := cur.vis[tgt]; dup {
					continue
				}
				rt := strings.TrimSpace(taxonomy.NormalizeRelationType(r.RelationType))
				leg := RegulatoryImpactLeg{
					FromPath:     cur.at,
					ToPath:       tgt,
					RelationType: rt,
					PassThrough:  passThroughFromQualifiers(r.Qualifiers),
					LagQuarters:  lagQuartersFromQualifiers(r.Qualifiers),
				}
				nextVis := make(map[string]struct{}, len(cur.vis)+1)
				for k := range cur.vis {
					nextVis[k] = struct{}{}
				}
				nextVis[tgt] = struct{}{}
				nextLegs := append(append([]RegulatoryImpactLeg(nil), cur.legs...), leg)
				queue = append(queue, state{at: tgt, legs: nextLegs, vis: nextVis})
				if isRegulatoryTerminalPath(tgt) {
					out = append(out, RegulatoryImpactPath{Legs: nextLegs, Confidence: normalizeEdgeConfidence(r.Confidence)})
					if len(out) >= maxRegulatoryPathsFromRelations {
						break
					}
				}
			}
		}
	}
	return out
}

func isRegulatoryTerminalPath(path string) bool {
	path = strings.TrimSpace(path)
	return strings.HasPrefix(path, "/org/") ||
		strings.HasPrefix(path, "/fac/") ||
		strings.HasPrefix(path, "/market/") ||
		strings.HasPrefix(path, "/product/") ||
		strings.HasPrefix(path, "/community/") ||
		strings.HasPrefix(path, "/program/") ||
		strings.HasPrefix(path, "/media/") ||
		strings.HasPrefix(path, "/loc/")
}

func passThroughFromQualifiers(q map[string]interface{}) float64 {
	if q == nil {
		return 0
	}
	for _, key := range []string{"pass_through_estimate", "passThroughEstimate", "pass_through", "passThrough"} {
		if v, ok := q[key]; ok {
			switch t := v.(type) {
			case float64:
				if t > 0 {
					return t
				}
			case float32:
				if t > 0 {
					return float64(t)
				}
			}
		}
	}
	return 0
}

func lagQuartersFromQualifiers(q map[string]interface{}) float64 {
	if q == nil {
		return 0
	}
	for _, key := range []string{"lag_quarters", "lagQuarters"} {
		if v, ok := q[key]; ok {
			switch t := v.(type) {
			case float64:
				if t >= 0 {
					return t
				}
			case float32:
				if t >= 0 {
					return float64(t)
				}
			case int:
				if t >= 0 {
					return float64(t)
				}
			case int64:
				if t >= 0 {
					return float64(t)
				}
			}
		}
	}
	for _, key := range []string{"measurement_lag_months", "measurementLagMonths"} {
		if v, ok := q[key]; ok {
			switch t := v.(type) {
			case float64:
				if t >= 0 {
					return t / 3.0
				}
			case float32:
				if t >= 0 {
					return float64(t) / 3.0
				}
			case int:
				if t >= 0 {
					return float64(t) / 3.0
				}
			case int64:
				if t >= 0 {
					return float64(t) / 3.0
				}
			}
		}
	}
	return 0
}

func normalizeEdgeConfidence(c float64) float64 {
	if c <= 0 {
		return 0.7
	}
	if c > 1 {
		return 1
	}
	return c
}

func clamp(v, lo, hi float64) float64 {
	return math.Min(hi, math.Max(lo, v))
}

// RegulatoryScenario is a parameter override set for what-if replays.
type RegulatoryScenario struct {
	ID        string             `json:"id"`
	Overrides map[string]float64 `json:"overrides,omitempty"`
}

// ScenarioResult is a minimal diff placeholder for scenario evaluation.
type ScenarioResult struct {
	ScenarioID     string  `json:"scenarioId"`
	BaseStress     float64 `json:"baseStress"`
	ScenarioStress float64 `json:"scenarioStress"`
	Delta          float64 `json:"delta"`
}

// EvaluateScenario applies simple rate multipliers keyed by mechanism path prefix to path stress.
func EvaluateScenario(base RegulatoryImpactPath, scenario RegulatoryScenario) ScenarioResult {
	baseStress := EvaluatePathStress(base).CumulativeExposure
	mult := 1.0
	if scenario.Overrides != nil {
		for k, v := range scenario.Overrides {
			if v <= 0 {
				continue
			}
			k = strings.TrimSpace(k)
			for _, leg := range base.Legs {
				if k != "" && strings.HasPrefix(leg.FromPath, k) {
					mult *= v
				}
			}
		}
	}
	scenarioStress := clamp(baseStress*mult, 0, 3)
	return ScenarioResult{
		ScenarioID:     scenario.ID,
		BaseStress:     baseStress,
		ScenarioStress: scenarioStress,
		Delta:          scenarioStress - baseStress,
	}
}
