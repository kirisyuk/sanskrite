package merge

import (
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// MergeFragmentDataFillFirst merges source fragment data into target (fill-first).
// Used by Graph Intent merge and deduplication tools.
func MergeFragmentDataFillFirst(target, source *horizonmodels.HorizonFragment) bool {
	if target == nil || source == nil {
		return false
	}
	changed := false

	// Labels: fill missing
	if target.Labels == nil {
		target.Labels = map[string]string{}
	}
	for k, v := range source.Labels {
		if strings.TrimSpace(v) != "" && strings.TrimSpace(target.Labels[k]) == "" {
			target.Labels[k] = v
			changed = true
		}
	}

	// Identifiers: fill missing
	if target.Identifiers == nil {
		target.Identifiers = map[string]string{}
	}
	for k, v := range source.Identifiers {
		if strings.TrimSpace(v) != "" && strings.TrimSpace(target.Identifiers[k]) == "" {
			target.Identifiers[k] = v
			changed = true
		}
	}

	// Description: fill if empty
	if strings.TrimSpace(target.Description) == "" && strings.TrimSpace(source.Description) != "" {
		target.Description = source.Description
		changed = true
	}

	// Tags: union
	tagSet := map[string]bool{}
	for _, t := range target.Tags {
		tagSet[t] = true
	}
	for _, t := range source.Tags {
		if !tagSet[t] {
			target.Tags = append(target.Tags, t)
			tagSet[t] = true
			changed = true
		}
	}

	// State: merge preferring richer metrics
	if mergeStatePreferMetrics(target, source.State) {
		changed = true
	}

	// Track merged canonical paths for alias resolution
	if strings.TrimSpace(source.CanonicalPath) != "" {
		if target.Identifiers["merged_paths"] == "" {
			target.Identifiers["merged_paths"] = source.CanonicalPath
			changed = true
		} else if !strings.Contains(target.Identifiers["merged_paths"], source.CanonicalPath) {
			target.Identifiers["merged_paths"] += "," + source.CanonicalPath
			changed = true
		}
	}

	return changed
}

func mergeStatePreferMetrics(target *horizonmodels.HorizonFragment, src map[string]horizonmodels.MetricValue) bool {
	if target == nil || src == nil || len(src) == 0 {
		return false
	}
	if target.State == nil {
		target.State = map[string]horizonmodels.MetricValue{}
	}
	changed := false
	for k, mv := range src {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		cur, ok := target.State[key]
		if !ok {
			target.State[key] = mv
			changed = true
			continue
		}
		if BetterMetricValue(mv, cur) {
			target.State[key] = mv
			changed = true
		}
	}
	return changed
}

// BetterMetricValue returns true if a is preferable to b (higher confidence, newer, etc).
func BetterMetricValue(a, b horizonmodels.MetricValue) bool {
	if a.Confidence > b.Confidence+1e-9 {
		return true
	}
	if b.Confidence > a.Confidence+1e-9 {
		return false
	}
	if !b.ObservedAt.IsZero() && !a.ObservedAt.IsZero() && a.ObservedAt.After(b.ObservedAt) {
		return true
	}
	if b.ObservedAt.IsZero() && !a.ObservedAt.IsZero() {
		return true
	}
	return false
}
