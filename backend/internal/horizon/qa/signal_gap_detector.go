package qa

import (
	"fmt"
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

const (
	signalGapReasonMissing = "missing"
	signalGapReasonStale   = "stale"
	stalenessThreshold     = 7 * 24 * time.Hour // 7 days
	maxSignalGaps          = 10
)

// SignalGap describes a missing or stale signal on a seed fragment.
type SignalGap struct {
	FragmentID    string     `json:"fragmentId"`
	CanonicalPath string     `json:"canonicalPath"`
	EntityName    string     `json:"entityName"`
	Axis          string     `json:"axis"`
	Reason        string     `json:"reason"` // "missing" | "stale"
	LastObserved  *time.Time `json:"lastObserved,omitempty"`
}

// SignalGapDetector checks seed fragments for missing or stale signals on
// question-relevant axes. Pure logic, no I/O.
type SignalGapDetector struct{}

func NewSignalGapDetector() *SignalGapDetector {
	return &SignalGapDetector{}
}

// Detect inspects each seed fragment on each relevant axis and returns gaps.
func (d *SignalGapDetector) Detect(
	seedFragmentIDs []string,
	fragments []*horizonmodels.HorizonFragment,
	plan *horizonmodels.QuestionPlan,
) []SignalGap {
	if plan == nil || len(seedFragmentIDs) == 0 {
		return nil
	}

	fragByID := make(map[string]*horizonmodels.HorizonFragment, len(fragments))
	for _, f := range fragments {
		if f != nil {
			fragByID[f.ID] = f
		}
	}

	relevantAxes := d.relevantAxes(plan)
	now := time.Now()

	var gaps []SignalGap
	for _, fid := range seedFragmentIDs {
		f, ok := fragByID[fid]
		if !ok {
			continue
		}
		for _, axis := range relevantAxes {
			if len(gaps) >= maxSignalGaps {
				return gaps
			}
			gap := d.checkAxis(f, axis, now)
			if gap != nil {
				gaps = append(gaps, *gap)
			}
		}
	}
	return gaps
}

func (d *SignalGapDetector) relevantAxes(plan *horizonmodels.QuestionPlan) []string {
	seen := map[string]struct{}{}
	for _, sig := range plan.HypotheticalSignals {
		if sig.Axis != "" {
			seen[sig.Axis] = struct{}{}
		}
	}
	for _, axis := range plan.TargetFilter.ObserveAxes {
		axis = strings.TrimSpace(axis)
		if axis != "" {
			seen[axis] = struct{}{}
		}
	}
	for _, hint := range plan.ContextHints {
		for _, axis := range horizonmodels.AllAxes() {
			if strings.Contains(strings.ToLower(hint), strings.ReplaceAll(axis, "/", " ")) {
				seen[axis] = struct{}{}
			}
		}
	}
	if len(seen) == 0 {
		return horizonmodels.AllAxes()
	}
	out := make([]string, 0, len(seen))
	for a := range seen {
		out = append(out, a)
	}
	return out
}

func (d *SignalGapDetector) checkAxis(f *horizonmodels.HorizonFragment, axis string, now time.Time) *SignalGap {
	name := fragmentDisplayName(f)

	for metricKey, mv := range f.State {
		if !strings.Contains(metricKey, axis) && !strings.HasPrefix(metricKey, axis) {
			continue
		}
		if !mv.ObservedAt.IsZero() && now.Sub(mv.ObservedAt) > stalenessThreshold {
			t := mv.ObservedAt
			return &SignalGap{
				FragmentID:    f.ID,
				CanonicalPath: f.CanonicalPath,
				EntityName:    name,
				Axis:          axis,
				Reason:        signalGapReasonStale,
				LastObserved:  &t,
			}
		}
		return nil
	}

	return &SignalGap{
		FragmentID:    f.ID,
		CanonicalPath: f.CanonicalPath,
		EntityName:    name,
		Axis:          axis,
		Reason:        signalGapReasonMissing,
	}
}

func fragmentDisplayName(f *horizonmodels.HorizonFragment) string {
	if f.Labels != nil {
		if en, ok := f.Labels["en"]; ok && strings.TrimSpace(en) != "" {
			return strings.TrimSpace(en)
		}
		for _, v := range f.Labels {
			if strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
	}
	if f.CanonicalPath != "" {
		parts := strings.Split(strings.Trim(f.CanonicalPath, "/"), "/")
		return parts[len(parts)-1]
	}
	return fmt.Sprintf("fragment:%s", f.ID)
}
