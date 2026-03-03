package qa

import (
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// QAScoutRequestBuilder creates ScoutingJob parameters from a question plan
// and sufficiency report so the scouting system can fill data gaps.
type QAScoutRequestBuilder struct{}

func NewQAScoutRequestBuilder() *QAScoutRequestBuilder {
	return &QAScoutRequestBuilder{}
}

// Build produces ScoutingJobParams for each unresolved seed hint.
// Strategy is always "Snapshot" for quick turnaround.
func (b *QAScoutRequestBuilder) Build(
	plan *horizonmodels.QuestionPlan,
	report *SufficiencyReport,
	lensID string,
	tenantID string,
	projectID string,
) []*horizonmodels.ScoutingJobParams {
	if plan == nil || report == nil {
		return nil
	}

	unresolvedHints := b.collectUnresolvedHints(plan, report)
	if len(unresolvedHints) == 0 {
		return nil
	}

	var params []*horizonmodels.ScoutingJobParams
	for _, hint := range unresolvedHints {
		params = append(params, &horizonmodels.ScoutingJobParams{
			LensID:              lensID,
			TenantID:            tenantID,
			ProjectID:           projectID,
			TargetCanonicalPath: hint,
			SeedHints:           []string{hint},
			RecommendedFacets:   report.RecommendedFacets,
			Strategy:            string(horizonmodels.ScoutingStrategySnapshot),
		})
	}
	return params
}

// collectUnresolvedHints extracts seed hints that appear in sufficiency gaps
// (their description contains "Seed hint" and "not resolved").
func (b *QAScoutRequestBuilder) collectUnresolvedHints(
	plan *horizonmodels.QuestionPlan,
	report *SufficiencyReport,
) []string {
	unresolvedSet := map[string]struct{}{}
	for _, gap := range report.Gaps {
		if strings.Contains(gap.Description, "Seed hint") && strings.Contains(gap.Description, "not resolved") {
			for _, sig := range plan.HypotheticalSignals {
				for _, hint := range sig.SeedHints {
					if strings.Contains(gap.Description, hint) {
						unresolvedSet[hint] = struct{}{}
					}
				}
			}
		}
	}
	out := make([]string, 0, len(unresolvedSet))
	for h := range unresolvedSet {
		out = append(out, h)
	}
	return out
}
