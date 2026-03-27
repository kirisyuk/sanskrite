package metriccore

import (
	"fmt"
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

const (
	DefaultMetricsMaxAge        = 30 * 24 * time.Hour
	DefaultMetricsMinConfidence = 0.60
	DefaultFutureSkewTolerance  = 24 * time.Hour
)

type MetricRequirement struct {
	Path          string
	MaxAge        time.Duration
	MinConfidence float64
}

type RefreshPolicy struct {
	DefaultMaxAge        time.Duration
	DefaultMinConfidence float64
	FutureSkewTolerance  time.Duration
}

func DefaultRefreshPolicy() RefreshPolicy {
	return RefreshPolicy{
		DefaultMaxAge:        DefaultMetricsMaxAge,
		DefaultMinConfidence: DefaultMetricsMinConfidence,
		FutureSkewTolerance:  DefaultFutureSkewTolerance,
	}
}

func MetricObservedAt(metric horizonmodels.MetricValue) time.Time {
	if metric.ObservedAt.IsZero() && metric.Provenance.ObservedAt != nil && !metric.Provenance.ObservedAt.IsZero() {
		return metric.Provenance.ObservedAt.UTC()
	}
	return metric.ObservedAt.UTC()
}

func EvaluateRefreshReasonsForState(state map[string]horizonmodels.MetricValue, requirements []MetricRequirement, now time.Time, policy RefreshPolicy) []string {
	if state == nil {
		return []string{"state_missing"}
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if policy.DefaultMaxAge <= 0 {
		policy.DefaultMaxAge = DefaultMetricsMaxAge
	}
	if policy.DefaultMinConfidence <= 0 {
		policy.DefaultMinConfidence = DefaultMetricsMinConfidence
	}
	if policy.FutureSkewTolerance <= 0 {
		policy.FutureSkewTolerance = DefaultFutureSkewTolerance
	}
	reasons := make([]string, 0)
	for _, requirement := range requirements {
		path := strings.TrimSpace(requirement.Path)
		if path == "" {
			continue
		}
		maxAge := requirement.MaxAge
		if maxAge <= 0 {
			maxAge = policy.DefaultMaxAge
		}
		minConfidence := requirement.MinConfidence
		if minConfidence <= 0 {
			minConfidence = policy.DefaultMinConfidence
		}
		metric, ok := state[path]
		if !ok {
			reasons = append(reasons, "missing:"+path)
			continue
		}
		if metric.Confidence < minConfidence {
			reasons = append(reasons, "low_confidence:"+path)
			continue
		}
		observedAt := MetricObservedAt(metric)
		if observedAt.IsZero() {
			reasons = append(reasons, "stale:"+path)
			continue
		}
		if metric.FreshUntil != nil && !metric.FreshUntil.IsZero() {
			if metric.FreshUntil.Before(now) {
				reasons = append(reasons, "expired:"+path)
				continue
			}
		} else {
			age := now.Sub(observedAt)
			if age > maxAge || observedAt.After(now.Add(policy.FutureSkewTolerance)) {
				reasons = append(reasons, "stale:"+path)
				continue
			}
		}
	}
	return reasons
}

func EvaluateRefreshReasonsForFragment(fragment *horizonmodels.HorizonFragment, requirements []MetricRequirement, now time.Time, policy RefreshPolicy) []string {
	if fragment == nil {
		return []string{"fragment_nil"}
	}
	return EvaluateRefreshReasonsForState(fragment.State, requirements, now, policy)
}

func BetterMetricValue(existing, incoming horizonmodels.MetricValue) bool {
	inObserved := MetricObservedAt(incoming)
	exObserved := MetricObservedAt(existing)
	if incoming.Confidence != existing.Confidence {
		return incoming.Confidence > existing.Confidence
	}
	switch {
	case existing.FreshUntil == nil && incoming.FreshUntil != nil:
		return true
	case existing.FreshUntil != nil && incoming.FreshUntil != nil:
		if !incoming.FreshUntil.Equal(*existing.FreshUntil) {
			return incoming.FreshUntil.After(*existing.FreshUntil)
		}
	}
	if !inObserved.Equal(exObserved) {
		return inObserved.After(exObserved)
	}
	if incoming.ErrorMargin != nil && existing.ErrorMargin == nil {
		return true
	}
	if incoming.SourceKind != nil && existing.SourceKind == nil {
		return true
	}
	return false
}

func NormalizeMetricValue(metric horizonmodels.MetricValue, sourceKind string, freshUntil *time.Time) horizonmodels.MetricValue {
	metric.Confidence = clamp01(metric.Confidence)
	if metric.ObservedAt.IsZero() && metric.Provenance.ObservedAt != nil && !metric.Provenance.ObservedAt.IsZero() {
		metric.ObservedAt = metric.Provenance.ObservedAt.UTC()
	}
	if metric.ObservedAt.IsZero() {
		metric.ObservedAt = time.Now().UTC()
	}
	if freshUntil != nil && !freshUntil.IsZero() {
		ft := freshUntil.UTC()
		metric.FreshUntil = &ft
	}
	sourceKind = strings.TrimSpace(sourceKind)
	if sourceKind != "" {
		metric.SourceKind = ptr(sourceKind)
	}
	return metric
}

func requirementSet(paths []string, maxAge time.Duration, minConfidence float64) []MetricRequirement {
	out := make([]MetricRequirement, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		out = append(out, MetricRequirement{
			Path:          path,
			MaxAge:        maxAge,
			MinConfidence: minConfidence,
		})
	}
	return out
}

func ptr[T any](v T) *T { return &v }

func clamp01(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}

func Requirement(path string, maxAge time.Duration, minConfidence float64) MetricRequirement {
	return MetricRequirement{Path: strings.TrimSpace(path), MaxAge: maxAge, MinConfidence: minConfidence}
}

func RequirementSet(paths []string, maxAge time.Duration, minConfidence float64) []MetricRequirement {
	return requirementSet(paths, maxAge, minConfidence)
}

func RequirementKey(requirements []MetricRequirement) string {
	if len(requirements) == 0 {
		return ""
	}
	keys := make([]string, 0, len(requirements))
	for _, requirement := range requirements {
		path := strings.TrimSpace(requirement.Path)
		if path == "" {
			continue
		}
		keys = append(keys, fmt.Sprintf("%s@%s/%.2f", path, requirement.MaxAge, requirement.MinConfidence))
	}
	return strings.Join(keys, "|")
}
