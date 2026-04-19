package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// MetricsAgent computes standard risk metrics for a HorizonFragment.State using an LLM.
// This is necessary because upstream extraction/research may return facts but not normalized risk scores.
type MetricsAgent struct {
	executor *ai.AITaskExecutor
}

func NewMetricsAgent(executor *ai.AITaskExecutor) *MetricsAgent {
	return &MetricsAgent{executor: executor}
}

type metricsResult struct {
	State map[string]models.MetricValue `json:"state"`
}

type metricKind string

const (
	metricKindMagnitude   metricKind = "magnitude"   // value in [0..1]
	metricKindDirectional metricKind = "directional" // value in [-1..1]
)

type metricSpec struct {
	Path string
	Kind metricKind
}

var nonMetricPathChars = regexp.MustCompile(`[^a-z0-9/._-]+`)
var multiSlash = regexp.MustCompile(`/+`)

func slugifyMetricSegment(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonMetricPathChars.ReplaceAllString(s, "-")
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "_") // keep underscores
	s = strings.Trim(s, "-")
	return s
}

// Canonical metric path rules:
// - All metric keys MUST be stored as paths under /metric/...
// - Standard risk metrics live under /metric/risk/<metric_name>
// - Dynamic metrics, if requested without a path, live under /metric/custom/<slug>
func canonicalizeMetricPath(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	// If user passed "risk/foo/bar" treat as path
	if !strings.HasPrefix(s, "/") && strings.Contains(s, "/") {
		s = "/" + s
	}
	// If it's a bare name, put into /metric/custom/<name>
	if !strings.HasPrefix(s, "/") {
		return "/metric/custom/" + slugifyMetricSegment(s)
	}
	// If it doesn't start with /metric/, prefix it.
	if !strings.HasPrefix(s, "/metric/") {
		s = "/metric" + s
	}
	// Normalize slashes and segments
	s = strings.ToLower(s)
	s = multiSlash.ReplaceAllString(s, "/")
	parts := strings.Split(strings.Trim(s, "/"), "/")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		// Keep domain/metric names readable: allow a-z0-9 and underscores/dashes/dots
		out = append(out, slugifyMetricSegment(p))
	}
	if len(out) < 2 || out[0] != "metric" {
		return ""
	}
	return "/" + strings.Join(out, "/")
}

func metricBaseName(metricPathOrName string) string {
	s := strings.TrimSpace(metricPathOrName)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "/") {
		parts := strings.Split(strings.Trim(s, "/"), "/")
		if len(parts) == 0 {
			return ""
		}
		return parts[len(parts)-1]
	}
	return s
}

func defaultMetricSpecsForType(t FragmentType) []metricSpec {
	// Standardized, type-aware defaults.
	// Note: these are intentionally conservative; dynamic requested metrics can extend this set.
	switch t {
	case FragmentTypePerson:
		return []metricSpec{
			{Path: "/metric/risk/public_perception", Kind: metricKindDirectional},
			{Path: "/metric/risk/market_sentiment", Kind: metricKindDirectional},
			{Path: "/metric/risk/geopolitical_alignment", Kind: metricKindDirectional},
			{Path: "/metric/risk/sanction_exposure", Kind: metricKindMagnitude},
		}
	case FragmentTypeGeoRegion:
		return []metricSpec{
			{Path: "/metric/risk/stability_index", Kind: metricKindMagnitude},
			{Path: "/metric/risk/corruption_vulnerability", Kind: metricKindMagnitude},
			{Path: "/metric/risk/sanction_exposure", Kind: metricKindMagnitude},
			{Path: "/metric/risk/geopolitical_alignment", Kind: metricKindDirectional},
			{Path: "/metric/risk/public_perception", Kind: metricKindDirectional},
		}
	case FragmentTypeEvent:
		return []metricSpec{
			{Path: "/metric/risk/market_sentiment", Kind: metricKindDirectional},
			{Path: "/metric/risk/public_perception", Kind: metricKindDirectional},
			{Path: "/metric/risk/geopolitical_alignment", Kind: metricKindDirectional},
			{Path: "/metric/risk/sanction_exposure", Kind: metricKindMagnitude},
		}
	case FragmentTypeFacility:
		return []metricSpec{
			{Path: "/metric/risk/operational_health", Kind: metricKindMagnitude},
			{Path: "/metric/risk/capacity_utilization", Kind: metricKindMagnitude},
			{Path: "/metric/risk/supply_chain_criticality", Kind: metricKindMagnitude},
			{Path: "/metric/risk/stability_index", Kind: metricKindMagnitude},
			{Path: "/metric/risk/corruption_vulnerability", Kind: metricKindMagnitude},
			{Path: "/metric/risk/sanction_exposure", Kind: metricKindMagnitude},
		}
	case FragmentTypeProduct:
		return []metricSpec{
			{Path: "/metric/risk/supply_chain_criticality", Kind: metricKindMagnitude},
			{Path: "/metric/risk/market_sentiment", Kind: metricKindDirectional},
			{Path: "/metric/risk/public_perception", Kind: metricKindDirectional},
		}
	case FragmentTypeOrganization:
		fallthrough
	default:
		return []metricSpec{
			{Path: "/metric/risk/operational_health", Kind: metricKindMagnitude},
			{Path: "/metric/risk/capacity_utilization", Kind: metricKindMagnitude},
			{Path: "/metric/risk/supply_chain_criticality", Kind: metricKindMagnitude},
			{Path: "/metric/risk/stability_index", Kind: metricKindMagnitude},
			{Path: "/metric/risk/corruption_vulnerability", Kind: metricKindMagnitude},
			{Path: "/metric/risk/sanction_exposure", Kind: metricKindMagnitude},
			{Path: "/metric/risk/geopolitical_alignment", Kind: metricKindDirectional},
			{Path: "/metric/risk/market_sentiment", Kind: metricKindDirectional},
			{Path: "/metric/risk/public_perception", Kind: metricKindDirectional},
		}
	}
}

// RequiredMetricPathsForType is the metrics schema contract for runtime refresh decisions:
// it returns canonical metric paths that must exist (type defaults + explicit requested metrics).
func RequiredMetricPathsForType(t FragmentType, requestedMetrics []string) []string {
	specs := defaultMetricSpecsForType(t)
	for _, rm := range requestedMetrics {
		p := canonicalizeMetricPath(rm)
		if p == "" {
			continue
		}
		base := metricBaseName(p)
		kind := metricKindMagnitude
		if base == "geopolitical_alignment" || base == "market_sentiment" || base == "public_perception" {
			kind = metricKindDirectional
		}
		specs = append(specs, metricSpec{Path: p, Kind: kind})
	}

	uniq := make(map[string]struct{}, len(specs))
	for _, s := range specs {
		cp := canonicalizeMetricPath(s.Path)
		if cp == "" {
			continue
		}
		uniq[cp] = struct{}{}
	}
	out := make([]string, 0, len(uniq))
	for path := range uniq {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

// ComputeState produces a normalized risk metric state map. If it cannot infer any meaningful metrics,
// it should return an empty map and a non-nil error.
func (a *MetricsAgent) ComputeState(
	ctx context.Context,
	fragment *models.HorizonFragment,
	sourceURLs []string,
	aggregatedContent string,
	relations []DiscoveredRelation,
	requestedMetrics []string,
) (map[string]models.MetricValue, error) {
	if a.executor == nil {
		return nil, fmt.Errorf("metrics agent: executor is nil")
	}
	if fragment == nil || strings.TrimSpace(fragment.CanonicalPath) == "" {
		return nil, fmt.Errorf("metrics agent: fragment with canonicalPath is required")
	}

	// Build allowed metrics set: defaults by type + explicitly requested metrics.
	specs := defaultMetricSpecsForType(fragment.Class)
	for _, rm := range requestedMetrics {
		p := canonicalizeMetricPath(rm)
		if p == "" {
			continue
		}
		// Default dynamic metrics are magnitude unless they match known directional ones.
		base := metricBaseName(p)
		kind := metricKindMagnitude
		if base == "geopolitical_alignment" || base == "market_sentiment" || base == "public_perception" {
			kind = metricKindDirectional
		}
		specs = append(specs, metricSpec{Path: p, Kind: kind})
	}

	allowed := make(map[string]metricKind, len(specs))
	for _, s := range specs {
		cp := canonicalizeMetricPath(s.Path)
		if cp == "" {
			continue
		}
		allowed[cp] = s.Kind
	}
	if len(allowed) == 0 {
		return nil, fmt.Errorf("metrics agent: no allowed metrics for type=%s", fragment.Class)
	}

	prompt := a.buildMetricsPrompt(fragment, sourceURLs, aggregatedContent, relations, specs, requestedMetrics)

	verifier := ai.NewGenericVerifier(func(res metricsResult) error {
		if len(res.State) == 0 {
			return fmt.Errorf("state must contain at least 1 metric")
		}
		for k, v := range res.State {
			cp := canonicalizeMetricPath(k)
			if cp == "" {
				return fmt.Errorf("metric key must be a canonical /metric/... path, got: %q", k)
			}
			kind, ok := allowed[cp]
			if !ok {
				return fmt.Errorf("unsupported metric path: %q", cp)
			}
			if v.Confidence < 0 || v.Confidence > 1 {
				return fmt.Errorf("metric %q: confidence must be 0..1", cp)
			}
			if v.ObservedAt.IsZero() {
				return fmt.Errorf("metric %q: observedAt is required", cp)
			}
			// Validate value ranges
			switch kind {
			case metricKindMagnitude:
				if v.Value < 0.0 || v.Value > 1.0 {
					return fmt.Errorf("metric %q: magnitude value must be 0..1, got %v", cp, v.Value)
				}
			case metricKindDirectional:
				if v.Value < -1.0 || v.Value > 1.0 {
					return fmt.Errorf("metric %q: directional value must be -1..1, got %v", cp, v.Value)
				}
			default:
				return fmt.Errorf("metric %q: unknown metric kind %q", cp, kind)
			}
			if strings.TrimSpace(v.Provenance.Method) == "" {
				return fmt.Errorf("metric %q: provenance.method is required", cp)
			}
			if len(v.Provenance.SourceIDs) == 0 {
				return fmt.Errorf("metric %q: provenance.sourceIds must include at least one entry", cp)
			}
		}
		return nil
	})

	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentAnalysis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       5,
			MinContextWindow:         16000,
			MaxOutputSize:            4096,
		},
		Verifier: verifier,
	}

	result, err := a.executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("metrics agent task failed: %w", err)
	}

	b, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metrics result: %w", err)
	}
	var out metricsResult
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metrics result: %w", err)
	}

	// Normalize keys to canonical paths.
	normalized := make(map[string]models.MetricValue, len(out.State))
	for k, v := range out.State {
		cp := canonicalizeMetricPath(k)
		if cp == "" {
			continue
		}
		normalized[cp] = v
	}
	return normalized, nil
}

func (a *MetricsAgent) buildMetricsPrompt(fragment *models.HorizonFragment, sourceURLs []string, aggregatedContent string, relations []DiscoveredRelation, allowed []metricSpec, requested []string) string {
	fragJSON, _ := json.MarshalIndent(fragment, "", "  ")
	relJSON, _ := json.MarshalIndent(relations, "", "  ")

	urls := strings.Join(sourceURLs, "\n")
	if urls == "" {
		urls = "(none)"
	}

	// Limit content to reduce prompt explosion; the extractor already had it, but metrics inference does not need the whole page.
	content := strings.TrimSpace(aggregatedContent)
	if len(content) > 12000 {
		content = content[:12000] + "\n...[truncated]..."
	}

	now := time.Now().UTC().Format(time.RFC3339)

	allowedLines := make([]string, 0, len(allowed))
	for _, s := range allowed {
		cp := canonicalizeMetricPath(s.Path)
		if cp == "" {
			continue
		}
		kind := "magnitude [0..1]"
		if s.Kind == metricKindDirectional {
			kind = "directional [-1..1]"
		}
		allowedLines = append(allowedLines, fmt.Sprintf("- %s (%s)", cp, kind))
	}
	reqLines := "(none)"
	if len(requested) > 0 {
		canon := make([]string, 0, len(requested))
		for _, r := range requested {
			if cp := canonicalizeMetricPath(r); cp != "" {
				canon = append(canon, cp)
			}
		}
		if len(canon) > 0 {
			reqLines = strings.Join(canon, "\n")
		}
	}

	return fmt.Sprintf(`You are a metrics scoring agent for a risk analysis system.

You MUST output standardized RISK METRICS for the entity as fragment.state.

INPUT FRAGMENT (JSON):
%s

RELATIONS (JSON):
%s

SOURCE URLS (for provenance.sourceIds):
%s

CONTENT (may be truncated):
%s

ALLOWED METRIC PATHS (ONLY these keys are allowed):
%s

REQUESTED DYNAMIC METRICS (these are explicitly requested and MUST be included if possible):
%s

TASK:
Return a SINGLE JSON object that matches EXACTLY this schema:
{
  "state": {
    "<metricPath>": {
      "value": 0.0,
      "confidence": 0.0,
      "observedAt": "%s",
      "provenance": {
        "sourceIds": ["<pick 1-3 URLs from SOURCE URLS>"],
        "method": "llm_metric_inference",
        "observedAt": "%s"
      }
    }
  }
}

RULES:
- You MUST output at least 1 metric in state.
- Keys MUST be canonical metric paths under /metric/...
- Use only the allowed metric paths above.
- confidence must be 0..1.
- observedAt must be RFC3339.
- provenance.sourceIds MUST contain at least one URL from SOURCE URLS (if SOURCE URLS is none, use ["unknown"]).
- Output ONLY JSON. No markdown. No extra text.
`, string(fragJSON), string(relJSON), urls, content, strings.Join(allowedLines, "\n"), reqLines, now, now)
}
