package scout

import (
	"context"
	"sort"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

const stateKeyWorkingRegulatoryScope = "workingRegulatoryScope"

func regulatoryScopePathRelevant(p string) bool {
	p = strings.TrimSpace(p)
	if p == "" {
		return false
	}
	return strings.HasPrefix(p, "/gov/") || strings.HasPrefix(p, "/decision/") || strings.HasPrefix(p, "/mechanism/")
}

// mergeWorkingRegulatoryScopeFromRelations accumulates instrument/decision/gov paths into job.State for critic and gap analysis.
func mergeWorkingRegulatoryScopeFromRelations(job *models.ScoutingJob, rels []agents.DiscoveredRelation) {
	if job == nil {
		return
	}
	if job.State == nil {
		job.State = map[string]interface{}{}
	}
	seen := map[string]struct{}{}
	if v, ok := job.State[stateKeyWorkingRegulatoryScope]; ok {
		for _, s := range parseStateStringSlice(v) {
			s = strings.TrimSpace(s)
			if s != "" {
				seen[s] = struct{}{}
			}
		}
	}
	for _, r := range rels {
		for _, p := range []string{strings.TrimSpace(r.SourceCanonicalPath), strings.TrimSpace(r.TargetCanonicalPath)} {
			if regulatoryScopePathRelevant(p) {
				seen[p] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	job.State[stateKeyWorkingRegulatoryScope] = out
}

func regulatoryGraphNeedsAutoRefine(rels []agents.DiscoveredRelation, domainCP string) bool {
	if len(rels) < 8 {
		return true
	}
	hasGov, hasDec, hasMech := false, false, false
	domainCP = strings.TrimSpace(domainCP)
	for _, r := range rels {
		for _, p := range []string{strings.TrimSpace(r.SourceCanonicalPath), strings.TrimSpace(r.TargetCanonicalPath)} {
			if p == "" || p == domainCP {
				continue
			}
			if strings.HasPrefix(p, "/gov/") {
				hasGov = true
			}
			if strings.HasPrefix(p, "/decision/") {
				hasDec = true
			}
			if strings.HasPrefix(p, "/mechanism/") {
				hasMech = true
			}
		}
	}
	return !hasGov || !hasDec || !hasMech
}

func applyPolicyImpactCriticToRelations(
	ctx context.Context,
	e *ScoutEngine,
	job *models.ScoutingJob,
	frag *models.HorizonFragment,
	_ string,
	rels []agents.DiscoveredRelation,
) []agents.DiscoveredRelation {
	if e == nil || job == nil || frag == nil || len(rels) == 0 || e.criticAgent == nil {
		return rels
	}
	input := agents.CriticValidatorInput{
		Fragment:     frag,
		Relations:    rels,
		GraphContext: "domain_expansion_policy_impact",
	}
	vr, err := e.criticAgent.CriticPipeline().Validate(ctx, input, []string{taxonomy.DomainModePolicyImpact})
	if err != nil || vr == nil {
		return rels
	}
	if len(vr.MissingInfo) > 0 {
		job.State["regulatoryImpactCriticMissingInfo"] = vr.MissingInfo
	}
	if !vr.IsValid || vr.Confidence < 1.0 {
		mult := vr.Confidence
		if mult <= 0 || mult > 1 {
			mult = 0.85
		}
		out := make([]agents.DiscoveredRelation, len(rels))
		for i, r := range rels {
			r2 := r
			r2.Confidence = r2.Confidence * mult
			out[i] = r2
		}
		return out
	}
	return rels
}
