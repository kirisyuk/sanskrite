package qa

import (
	"fmt"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// SufficiencyReport summarises how well the available fragments cover a question plan.
type SufficiencyReport struct {
	Score             float64                    `json:"score"`
	Sufficient        bool                       `json:"sufficient"`
	Gaps              []horizonmodels.QADataGap  `json:"gaps"`
	RecommendedFacets []string                   `json:"recommendedFacets,omitempty"`
}

// SufficiencyChecker performs pure-logic sufficiency analysis. 0 LLM calls.
type SufficiencyChecker struct{}

func NewSufficiencyChecker() *SufficiencyChecker {
	return &SufficiencyChecker{}
}

const sufficiencyThreshold = 0.4

// Check evaluates whether scopeFragments adequately cover the question plan.
// Score = (resolvedSeeds / totalHints) * 0.5 + (coveredAxes / targetAxes) * 0.5.
func (c *SufficiencyChecker) Check(
	plan *horizonmodels.QuestionPlan,
	scopeFragments map[string]string,
	fragments []*horizonmodels.HorizonFragment,
) *SufficiencyReport {
	if plan == nil {
		return &SufficiencyReport{Score: 0, Sufficient: false}
	}

	fragByPath := indexFragmentsByPath(fragments)

	totalHints, resolvedSeeds := c.countResolvedSeeds(plan, scopeFragments, fragByPath)
	targetAxes := c.collectTargetAxes(plan)
	coveredAxes := c.countCoveredAxes(targetAxes, fragments)

	var seedRatio, axisRatio float64
	if totalHints > 0 {
		seedRatio = float64(resolvedSeeds) / float64(totalHints)
	}
	if len(targetAxes) > 0 {
		axisRatio = float64(coveredAxes) / float64(len(targetAxes))
	}
	score := seedRatio*0.5 + axisRatio*0.5

	gaps := c.buildGaps(plan, scopeFragments, fragByPath, targetAxes, fragments)

	return &SufficiencyReport{
		Score:             score,
		Sufficient:        score >= sufficiencyThreshold,
		Gaps:              gaps,
		RecommendedFacets: plan.ContextHints,
	}
}

func (c *SufficiencyChecker) countResolvedSeeds(
	plan *horizonmodels.QuestionPlan,
	scopeFragments map[string]string,
	fragByPath map[string]*horizonmodels.HorizonFragment,
) (total, resolved int) {
	for _, sig := range plan.HypotheticalSignals {
		for _, hint := range sig.SeedHints {
			total++
			if _, ok := scopeFragments[hint]; ok {
				resolved++
				continue
			}
			if _, ok := fragByPath[hint]; ok {
				resolved++
			}
		}
	}
	return total, resolved
}

func (c *SufficiencyChecker) collectTargetAxes(plan *horizonmodels.QuestionPlan) []string {
	seen := map[string]struct{}{}
	for _, sig := range plan.HypotheticalSignals {
		if sig.Axis != "" {
			seen[sig.Axis] = struct{}{}
		}
	}
	for _, a := range plan.TargetFilter.ObserveAxes {
		seen[a] = struct{}{}
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

func (c *SufficiencyChecker) countCoveredAxes(targetAxes []string, fragments []*horizonmodels.HorizonFragment) int {
	covered := map[string]struct{}{}
	for _, f := range fragments {
		for metricKey := range f.State {
			for _, axis := range targetAxes {
				if strings.Contains(metricKey, axis) || strings.HasPrefix(metricKey, axis) {
					covered[axis] = struct{}{}
				}
			}
		}
	}
	return len(covered)
}

func (c *SufficiencyChecker) buildGaps(
	plan *horizonmodels.QuestionPlan,
	scopeFragments map[string]string,
	fragByPath map[string]*horizonmodels.HorizonFragment,
	targetAxes []string,
	fragments []*horizonmodels.HorizonFragment,
) []horizonmodels.QADataGap {
	var gaps []horizonmodels.QADataGap

	for _, sig := range plan.HypotheticalSignals {
		for _, hint := range sig.SeedHints {
			if _, ok := scopeFragments[hint]; ok {
				continue
			}
			if _, ok := fragByPath[hint]; ok {
				continue
			}
			gaps = append(gaps, horizonmodels.QADataGap{
				Description:  fmt.Sprintf("Seed hint %q not resolved to any fragment", hint),
				WhyItMatters: fmt.Sprintf("Signal on axis %s cannot be injected without a matching entity", sig.Axis),
			})
		}
	}

	coveredAxes := map[string]struct{}{}
	for _, f := range fragments {
		for metricKey := range f.State {
			for _, axis := range targetAxes {
				if strings.Contains(metricKey, axis) || strings.HasPrefix(metricKey, axis) {
					coveredAxes[axis] = struct{}{}
				}
			}
		}
	}
	for _, axis := range targetAxes {
		if _, ok := coveredAxes[axis]; !ok {
			gaps = append(gaps, horizonmodels.QADataGap{
				Description:  fmt.Sprintf("No fragment state covers axis %q", axis),
				WhyItMatters: "Answer quality degrades without data on this dimension",
			})
		}
	}

	return gaps
}

func indexFragmentsByPath(fragments []*horizonmodels.HorizonFragment) map[string]*horizonmodels.HorizonFragment {
	m := make(map[string]*horizonmodels.HorizonFragment, len(fragments))
	for _, f := range fragments {
		if f != nil && f.CanonicalPath != "" {
			m[f.CanonicalPath] = f
		}
	}
	return m
}
