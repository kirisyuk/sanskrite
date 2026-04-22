package scout

import (
	"encoding/json"
	"sort"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/regstress"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// dedupeDiscoveredRelations collapses duplicate edges (same source, target, relation type).
// When duplicates differ in confidence, the higher-confidence relation wins.
func dedupeDiscoveredRelations(rels []agents.DiscoveredRelation) []agents.DiscoveredRelation {
	if len(rels) <= 1 {
		return rels
	}
	type key struct {
		src, tgt, rel string
	}
	best := make(map[key]agents.DiscoveredRelation, len(rels))
	order := make([]key, 0, len(rels))
	for _, rel := range rels {
		src := strings.TrimSpace(rel.SourceCanonicalPath)
		tgt := strings.TrimSpace(rel.TargetCanonicalPath)
		rt := normalizeRelationToken(rel.RelationType)
		k := key{src: src, tgt: tgt, rel: rt}
		if ex, ok := best[k]; ok {
			if rel.Confidence > ex.Confidence {
				best[k] = rel
			}
			continue
		}
		best[k] = rel
		order = append(order, k)
	}
	out := make([]agents.DiscoveredRelation, 0, len(order))
	for _, k := range order {
		out = append(out, best[k])
	}
	return out
}

// policyImpactMaterializationRoot picks a non-domain root fragment path for REGULATORY_IMPACT_MAP
// materialization. Domain artifacts need an existing entity fragment as BFS root; pure domain
// contracts often have empty firstOrder while the impact graph centers on a mechanism/decision.
func policyImpactMaterializationRoot(
	domainCP string,
	firstOrder []string,
	plan *agents.RegulatoryImpactExecutionPlan,
	rels []agents.DiscoveredRelation,
) string {
	if plan != nil {
		for _, q := range plan.Queries {
			p := strings.TrimSpace(q.Instrument)
			if p == "" || strings.HasPrefix(p, "/domain/") {
				continue
			}
			if err := taxonomy.ValidateCanonicalPath(p); err == nil {
				return p
			}
		}
	}
	for _, p := range firstOrder {
		p = strings.TrimSpace(p)
		if p == "" || strings.HasPrefix(p, "/domain/") {
			continue
		}
		if err := taxonomy.ValidateCanonicalPath(p); err == nil {
			return p
		}
	}
	domainCP = strings.TrimSpace(domainCP)
	mechanisms := make([]string, 0, 8)
	decisions := make([]string, 0, 8)
	seenMech := map[string]struct{}{}
	seenDec := map[string]struct{}{}
	for _, r := range rels {
		for _, p := range []string{strings.TrimSpace(r.TargetCanonicalPath), strings.TrimSpace(r.SourceCanonicalPath)} {
			if p == "" || strings.HasPrefix(p, "/domain/") {
				continue
			}
			if p == domainCP {
				continue
			}
			if err := taxonomy.ValidateCanonicalPath(p); err != nil {
				continue
			}
			if strings.HasPrefix(p, "/mechanism/") {
				if _, ok := seenMech[p]; ok {
					continue
				}
				seenMech[p] = struct{}{}
				mechanisms = append(mechanisms, p)
				continue
			}
			if strings.HasPrefix(p, "/decision/") {
				if _, ok := seenDec[p]; ok {
					continue
				}
				seenDec[p] = struct{}{}
				decisions = append(decisions, p)
			}
		}
	}
	if len(mechanisms) > 0 {
		sort.Strings(mechanisms)
		return mechanisms[0]
	}
	if len(decisions) > 0 {
		sort.Strings(decisions)
		return decisions[0]
	}
	return ""
}

// filterPolicyImpactRelations keeps edges whose types appear in taxonomy.PolicyImpactEdgeTypes.
func filterPolicyImpactRelations(all []agents.DiscoveredRelation) []agents.DiscoveredRelation {
	if len(all) == 0 {
		return nil
	}
	allow := make(map[string]struct{}, len(taxonomy.PolicyImpactEdgeTypes))
	for _, t := range taxonomy.PolicyImpactEdgeTypes {
		if normalized := normalizeRelationToken(t); normalized != "" {
			allow[normalized] = struct{}{}
		}
	}
	var out []agents.DiscoveredRelation
	for _, r := range all {
		rt := normalizeRelationToken(r.RelationType)
		if _, ok := allow[rt]; ok {
			out = append(out, r)
		}
	}
	return out
}

// regulatoryCascadeDirectAffectedPaths lists directly targeted downstream nodes from policy action roots.
func regulatoryCascadeDirectAffectedPaths(rels []agents.DiscoveredRelation) []string {
	seen := map[string]struct{}{}
	for _, r := range rels {
		rt := normalizeRelationToken(r.RelationType)
		if rt != "applies_to" &&
			rt != string(ah.RelationNameImposes) &&
			rt != string(ah.RelationNameRegulates) &&
			rt != "controls_access" &&
			rt != string(ah.RelationNameDisrupts) &&
			rt != "in_geo_scope" {
			continue
		}
		src := strings.TrimSpace(r.SourceCanonicalPath)
		if !strings.HasPrefix(src, "/mechanism/") && !strings.HasPrefix(src, "/gov/") && !strings.HasPrefix(src, "/project/") {
			continue
		}
		tgt := strings.TrimSpace(r.TargetCanonicalPath)
		if tgt == "" {
			continue
		}
		if strings.HasPrefix(tgt, "/org/") ||
			strings.HasPrefix(tgt, "/fac/") ||
			strings.HasPrefix(tgt, "/market/") ||
			strings.HasPrefix(tgt, "/product/") ||
			strings.HasPrefix(tgt, "/community/") ||
			strings.HasPrefix(tgt, "/project/") ||
			strings.HasPrefix(tgt, "/media/") ||
			strings.HasPrefix(tgt, "/loc/") {
			seen[tgt] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func defaultRegulatoryWhatIfScenarios() []regstress.RegulatoryScenario {
	return []regstress.RegulatoryScenario{{
		ID:        "mechanism_rate_plus15pct",
		Overrides: map[string]float64{"/mechanism/": 1.15},
	}}
}

// regulatoryWhatIfScenariosFromJob reads optional job state for what-if overrides.
// Supported keys:
//   - regulatoryWhatIfScenarios: JSON array of { "id", "overrides": { "/mechanism/...": 1.1 } }
//   - regulatoryWhatIfOverrides + optional regulatoryWhatIfScenarioId: single custom scenario
func regulatoryWhatIfScenariosFromJob(job *horizonmodels.ScoutingJob) []regstress.RegulatoryScenario {
	if job == nil || job.State == nil {
		return defaultRegulatoryWhatIfScenarios()
	}
	raw := job.State["regulatoryWhatIfScenarios"]
	if raw == nil {
		if ov := parseRegulatoryWhatIfOverridesMap(job.State["regulatoryWhatIfOverrides"]); len(ov) > 0 {
			id := parseStateString(job.State["regulatoryWhatIfScenarioId"])
			if id == "" {
				id = "job_state_custom"
			}
			return []regstress.RegulatoryScenario{{ID: id, Overrides: ov}}
		}
		return defaultRegulatoryWhatIfScenarios()
	}
	b, err := json.Marshal(raw)
	if err != nil || len(b) == 0 {
		return defaultRegulatoryWhatIfScenarios()
	}
	var scenarios []regstress.RegulatoryScenario
	if err := json.Unmarshal(b, &scenarios); err != nil || len(scenarios) == 0 {
		return defaultRegulatoryWhatIfScenarios()
	}
	return scenarios
}

func parseRegulatoryWhatIfOverridesMap(v interface{}) map[string]float64 {
	if v == nil {
		return nil
	}
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	out := make(map[string]float64, len(m))
	for k, val := range m {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		switch t := val.(type) {
		case float64:
			out[k] = t
		case float32:
			out[k] = float64(t)
		case int:
			out[k] = float64(t)
		case int64:
			out[k] = float64(t)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
