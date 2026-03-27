package metriccore

import (
	"context"
	"sort"
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/simscope"
)

type targetBinding struct {
	fragment       *horizonmodels.HorizonFragment
	class          string
	whyInScope     string
	requirements   []MetricRequirement
	refreshReasons []string
	priorityScore  float64
}

type refreshedTarget struct {
	Ref      string
	Fragment *horizonmodels.HorizonFragment
	Update   OperationalTargetUpdate
}

func (h *OperationalHydrator) collectTargets(ctx context.Context, tenantID string, recognition map[string]any, artifacts []*horizonmodels.MaterializedArtifact, scopePaths []string, profile simscope.Profile, now time.Time) []targetBinding {
	candidates := make(map[string]*targetBinding)
	addCandidate := func(ref string, why string, score float64) {
		ref = strings.TrimSpace(ref)
		if ref == "" || !strings.HasPrefix(ref, "/") {
			return
		}
		if simscope.PathRelevanceScore(ref, "", profile) < 0.85 {
			return
		}
		entry := candidates[ref]
		if entry == nil {
			fragment, err := h.fragments.GetByCanonicalPathForTenant(ctx, tenantID, ref)
			if err != nil || fragment == nil {
				return
			}
			class, requirements := inferOperationalTargetContract(fragment)
			if class == "" || len(requirements) == 0 {
				return
			}
			entry = &targetBinding{
				fragment:       fragment,
				class:          class,
				whyInScope:     strings.TrimSpace(why),
				requirements:   requirements,
				refreshReasons: EvaluateRefreshReasonsForFragment(fragment, requirements, now, DefaultRefreshPolicy()),
				priorityScore:  targetClassPriority(class),
			}
			candidates[ref] = entry
		} else if why != "" {
			entry.whyInScope = mergeWhyInScope(entry.whyInScope, why)
		}
		entry.priorityScore += score
	}
	for _, object := range recognitionObjectCandidates(recognition) {
		ref := strings.TrimSpace(object.Path)
		if ref == "" {
			continue
		}
		addCandidate(ref, "recognition "+strings.TrimSpace(object.Kind), 4.5+object.Score*recognitionKindPriority(object.Kind))
	}
	for _, path := range compactStrings(scopePaths) {
		addCandidate(path, "scope path", 0.8)
	}
	for _, artifact := range artifacts {
		if artifact == nil || artifact.Kind != horizonmodels.MaterializedKindSupplyChainPath {
			continue
		}
		horizonmodels.PopulateMaterializedArtifactIndexes(artifact)
		for _, ref := range compactStrings(append(append(append(append([]string{}, artifact.NodeRefs...), artifact.GeoRefs...), artifact.ZoneRefs...), artifact.AffectedZoneRefs...)) {
			addCandidate(ref, "materialized path context", 0.45)
		}
		legs, err := horizonmodels.GetSupplyChainPathLegs(artifact)
		if err != nil {
			continue
		}
		for _, leg := range legs {
			for _, ref := range compactStrings(append(append(append([]string{}, leg.TransportViaRefs...), leg.GeoRefs...), append(leg.ZoneRefs, leg.AffectedZoneRefs...)...)) {
				addCandidate(ref, "path leg constraint ref", 1.3)
			}
		}
	}
	type ranked struct {
		ref    string
		target targetBinding
	}
	rankedTargets := make([]ranked, 0, len(candidates))
	for ref, binding := range candidates {
		if binding == nil {
			continue
		}
		rankedTargets = append(rankedTargets, ranked{ref: ref, target: *binding})
	}
	sort.Slice(rankedTargets, func(i, j int) bool {
		if rankedTargets[i].target.priorityScore == rankedTargets[j].target.priorityScore {
			if len(rankedTargets[i].target.refreshReasons) == len(rankedTargets[j].target.refreshReasons) {
				return rankedTargets[i].ref < rankedTargets[j].ref
			}
			return len(rankedTargets[i].target.refreshReasons) > len(rankedTargets[j].target.refreshReasons)
		}
		return rankedTargets[i].target.priorityScore > rankedTargets[j].target.priorityScore
	})
	out := make([]targetBinding, 0, min(len(rankedTargets), 16))
	for _, item := range rankedTargets[:min(len(rankedTargets), 16)] {
		out = append(out, item.target)
	}
	return out
}
