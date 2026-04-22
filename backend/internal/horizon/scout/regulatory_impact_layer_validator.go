package scout

import (
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// RegulatoryLayerCoverage describes reachability from GOV seeds through policy-impact edges.
type RegulatoryLayerCoverage struct {
	GovReachable       map[string]struct{}
	DecisionReachable  map[string]struct{}
	MechanismReachable map[string]struct{}
	AffectedReachable  map[string]struct{}
	GeoReachable       map[string]struct{}
	CommunityReachable map[string]struct{}
	ProgramReachable   map[string]struct{}
	MediaReachable     map[string]struct{}
	OrphanPaths        []string
}

// ValidateRegulatoryImpactLayerCoverage runs a best-effort BFS from /gov/ sources on policy-impact edge types.
func ValidateRegulatoryImpactLayerCoverage(rels []agents.DiscoveredRelation) RegulatoryLayerCoverage {
	out := RegulatoryLayerCoverage{
		GovReachable:       map[string]struct{}{},
		DecisionReachable:  map[string]struct{}{},
		MechanismReachable: map[string]struct{}{},
		AffectedReachable:  map[string]struct{}{},
		GeoReachable:       map[string]struct{}{},
		CommunityReachable: map[string]struct{}{},
		ProgramReachable:   map[string]struct{}{},
		MediaReachable:     map[string]struct{}{},
	}
	if len(rels) == 0 {
		return out
	}
	seeds := map[string]struct{}{}
	adj := map[string][]regulatoryAdj{}
	for _, r := range rels {
		src := strings.TrimSpace(r.SourceCanonicalPath)
		tgt := strings.TrimSpace(r.TargetCanonicalPath)
		if src == "" || tgt == "" {
			continue
		}
		rt := normalizeRelationToken(r.RelationType)
		if strings.HasPrefix(src, "/gov/") {
			seeds[src] = struct{}{}
		}
		// Forward traversal along regulatory causal direction
		switch rt {
		case string(ah.RelationNameGatekeeps), string(ah.RelationNameRegulates), string(ah.RelationNameOperates):
			adj[src] = append(adj[src], regulatoryAdj{to: tgt, kind: rt})
		case string(ah.RelationNameWields):
			adj[src] = append(adj[src], regulatoryAdj{to: tgt, kind: rt})
		case "applies_to", string(ah.RelationNameImposes), "controls_access", string(ah.RelationNameInfluences), "in_geo_scope", string(ah.RelationNameDisrupts), "participates_in", string(ah.RelationNameListedOn), "has_subgroup", string(ah.RelationNameMemberOf), "affiliated_with", "operates_via":
			adj[src] = append(adj[src], regulatoryAdj{to: tgt, kind: rt})
		}
	}
	if len(seeds) == 0 {
		for _, r := range rels {
			p := strings.TrimSpace(r.SourceCanonicalPath)
			if strings.HasPrefix(p, "/gov/") {
				seeds[p] = struct{}{}
			}
			p = strings.TrimSpace(r.TargetCanonicalPath)
			if strings.HasPrefix(p, "/gov/") {
				seeds[p] = struct{}{}
			}
		}
	}
	seen := map[string]struct{}{}
	queue := make([]string, 0, len(seeds))
	for s := range seeds {
		seen[s] = struct{}{}
		queue = append(queue, s)
		out.GovReachable[s] = struct{}{}
	}
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		for _, e := range adj[u] {
			v := e.to
			if _, ok := seen[v]; ok {
				continue
			}
			seen[v] = struct{}{}
			queue = append(queue, v)
			switch {
			case strings.HasPrefix(v, "/decision/"):
				out.DecisionReachable[v] = struct{}{}
			case strings.HasPrefix(v, "/mechanism/"):
				out.MechanismReachable[v] = struct{}{}
			case strings.HasPrefix(v, "/gov/"):
				out.GovReachable[v] = struct{}{}
			case prefixAny(v, "/org/", "/fac/", "/market/", "/product/"):
				out.AffectedReachable[v] = struct{}{}
			case strings.HasPrefix(v, "/community/"):
				out.CommunityReachable[v] = struct{}{}
			case strings.HasPrefix(v, "/program/"):
				out.ProgramReachable[v] = struct{}{}
			case strings.HasPrefix(v, "/media/"):
				out.MediaReachable[v] = struct{}{}
			case strings.HasPrefix(v, "/loc/"):
				out.GeoReachable[v] = struct{}{}
			}
		}
	}
	// Orphans: key policy-impact layer nodes never reached from any gov seed BFS.
	candidates := map[string]struct{}{}
	for _, r := range rels {
		for _, p := range []string{strings.TrimSpace(r.SourceCanonicalPath), strings.TrimSpace(r.TargetCanonicalPath)} {
			if p == "" {
				continue
			}
			if strings.HasPrefix(p, "/mechanism/") ||
				strings.HasPrefix(p, "/decision/") ||
				strings.HasPrefix(p, "/community/") ||
				strings.HasPrefix(p, "/program/") ||
				strings.HasPrefix(p, "/media/") ||
				strings.HasPrefix(p, "/loc/") {
				candidates[p] = struct{}{}
			}
		}
	}
	for p := range candidates {
		if _, ok := seen[p]; !ok {
			out.OrphanPaths = append(out.OrphanPaths, p)
		}
	}
	sort.Strings(out.OrphanPaths)
	return out
}

type regulatoryAdj struct {
	to   string
	kind string
}

// ApplyRegulatoryLayerOrphanPenalty reduces confidence for relations touching orphan mechanism/decision nodes.
func ApplyRegulatoryLayerOrphanPenalty(rels []agents.DiscoveredRelation, cov RegulatoryLayerCoverage) []agents.DiscoveredRelation {
	if len(cov.OrphanPaths) == 0 {
		return rels
	}
	orph := make(map[string]struct{}, len(cov.OrphanPaths))
	for _, p := range cov.OrphanPaths {
		orph[p] = struct{}{}
	}
	out := make([]agents.DiscoveredRelation, len(rels))
	for i, r := range rels {
		out[i] = r
		if touchesAny(orph, r.SourceCanonicalPath, r.TargetCanonicalPath) {
			rr := r
			if rr.Confidence > 0.4 {
				rr.Confidence *= 0.82
			}
			out[i] = rr
		}
	}
	return out
}

func touchesAny(m map[string]struct{}, paths ...string) bool {
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if _, ok := m[p]; ok {
			return true
		}
	}
	return false
}

// RegulatoryGraphSufficiencyTier is a coarse completeness label for regulatory impact graphs.
type RegulatoryGraphSufficiencyTier string

const (
	RegulatorySufficiencyForensicGap RegulatoryGraphSufficiencyTier = "forensic_gap"
	RegulatorySufficiencyHeuristic   RegulatoryGraphSufficiencyTier = "heuristic"
	RegulatorySufficiencyValidated   RegulatoryGraphSufficiencyTier = "validated"
)

// RegulatoryGraphSufficiency summarizes graph completeness heuristics.
type RegulatoryGraphSufficiency struct {
	Tier        RegulatoryGraphSufficiencyTier `json:"tier"`
	Gaps        []string                       `json:"gaps,omitempty"`
	LayerCov    RegulatoryLayerCoverage        `json:"-"`
	QuantScore  float64                        `json:"quantScore"`
	TemporalPct float64                        `json:"temporalPct"`
}

// ComputeRegulatoryGraphSufficiency evaluates coverage and qualifier completeness.
func ComputeRegulatoryGraphSufficiency(rels []agents.DiscoveredRelation) RegulatoryGraphSufficiency {
	cov := ValidateRegulatoryImpactLayerCoverage(rels)
	gaps := make([]string, 0, 12)
	hasGov, hasDecision, hasMech, hasAffected := false, false, false, false
	hasGeo, hasCommunity, hasProgram, hasMedia := false, false, false, false
	for _, r := range rels {
		for _, p := range []string{strings.TrimSpace(r.SourceCanonicalPath), strings.TrimSpace(r.TargetCanonicalPath)} {
			if strings.HasPrefix(p, "/gov/") {
				hasGov = true
			}
			if strings.HasPrefix(p, "/decision/") {
				hasDecision = true
			}
			if strings.HasPrefix(p, "/mechanism/") {
				hasMech = true
			}
			if prefixAny(p, "/org/", "/fac/", "/market/", "/community/", "/product/") {
				hasAffected = true
			}
			if strings.HasPrefix(p, "/loc/") {
				hasGeo = true
			}
			if strings.HasPrefix(p, "/community/") {
				hasCommunity = true
			}
			if strings.HasPrefix(p, "/program/") {
				hasProgram = true
			}
			if strings.HasPrefix(p, "/media/") {
				hasMedia = true
			}
		}
	}
	if !hasGov {
		gaps = append(gaps, "missing_gov_layer")
	}
	if !hasDecision {
		gaps = append(gaps, "missing_decision_layer")
	}
	if !hasMech {
		gaps = append(gaps, "missing_mechanism_layer")
	}
	if !hasAffected {
		gaps = append(gaps, "missing_affected_segment")
	}
	if hasGeo && len(cov.GeoReachable) == 0 {
		gaps = append(gaps, "missing_geo_scope")
	}
	if hasCommunity && len(cov.CommunityReachable) == 0 {
		gaps = append(gaps, "missing_community_transmission")
	}
	if hasProgram && len(cov.ProgramReachable) == 0 {
		gaps = append(gaps, "missing_program_execution_layer")
	}
	if hasMedia && len(cov.MediaReachable) == 0 {
		gaps = append(gaps, "missing_media_channel_layer")
	}
	if len(cov.OrphanPaths) > 0 {
		gaps = append(gaps, "orphan_mechanism_or_decision_nodes")
	}

	regCond := 0
	regCondQual := 0
	mechWithDate := 0
	mechTotal := 0
	for _, r := range rels {
		rt := normalizeRelationToken(r.RelationType)
		if rt == string(ah.RelationNameRegulates) || rt == string(ah.RelationNameImposes) {
			regCond++
			if q := r.Qualifiers; q != nil {
				if _, ok := q["rate_from"]; ok {
					regCondQual++
				} else if _, ok := q["rateFrom"]; ok {
					regCondQual++
				} else if _, ok := q["rate_to"]; ok {
					regCondQual++
				} else if _, ok := q["rateTo"]; ok {
					regCondQual++
				}
			}
		}
		src, tgt := strings.TrimSpace(r.SourceCanonicalPath), strings.TrimSpace(r.TargetCanonicalPath)
		if strings.HasPrefix(src, "/mechanism/") || strings.HasPrefix(tgt, "/mechanism/") {
			mechTotal++
			if q := r.Qualifiers; q != nil {
				if _, ok := q["effective_date"]; ok {
					mechWithDate++
				} else if _, ok := q["effectiveDate"]; ok {
					mechWithDate++
				}
			}
		}
	}
	quantScore := 0.0
	if regCond > 0 {
		quantScore = float64(regCondQual) / float64(regCond)
	}
	temporalPct := 0.0
	if mechTotal > 0 {
		temporalPct = float64(mechWithDate) / float64(mechTotal)
	}

	tier := RegulatorySufficiencyHeuristic
	if len(gaps) > 0 {
		tier = RegulatorySufficiencyForensicGap
	} else if quantScore >= 0.5 && temporalPct >= 0.5 {
		tier = RegulatorySufficiencyValidated
	}

	return RegulatoryGraphSufficiency{
		Tier:        tier,
		Gaps:        gaps,
		LayerCov:    cov,
		QuantScore:  quantScore,
		TemporalPct: temporalPct,
	}
}

// RegulatoryTemporalIssue describes an inconsistency between related qualifiers.
type RegulatoryTemporalIssue struct {
	Message string `json:"message"`
}

// ValidateRegulatoryTemporalConsistency performs lightweight date ordering checks on relation qualifiers.
func ValidateRegulatoryTemporalConsistency(rels []agents.DiscoveredRelation) []RegulatoryTemporalIssue {
	var issues []RegulatoryTemporalIssue
	for _, r := range rels {
		q := r.Qualifiers
		if q == nil {
			continue
		}
		effective := parseDateish(q, "effective_date", "effectiveDate")
		sunset := parseDateish(q, "sunset_clause", "sunsetClause", "sunset")
		if !effective.IsZero() && !sunset.IsZero() && sunset.Before(effective) {
			issues = append(issues, RegulatoryTemporalIssue{
				Message: "sunset_before_effective: " + strings.TrimSpace(r.SourceCanonicalPath) + " -> " + strings.TrimSpace(r.TargetCanonicalPath),
			})
		}
		phaseStart := parseDateish(q, "phase_in_start", "phaseInStart", "phase_in_begin")
		phaseEnd := parseDateish(q, "phase_in_end", "phaseInEnd", "phase_in_finish")
		if !phaseStart.IsZero() && !phaseEnd.IsZero() && phaseEnd.Before(phaseStart) {
			issues = append(issues, RegulatoryTemporalIssue{
				Message: "phase_in_end_before_start: " + strings.TrimSpace(r.SourceCanonicalPath) + " -> " + strings.TrimSpace(r.TargetCanonicalPath),
			})
		}
		// Full effectiveness dated before phase-in completes is suspicious when both are explicit.
		if !effective.IsZero() && !phaseEnd.IsZero() && effective.Before(phaseEnd) {
			issues = append(issues, RegulatoryTemporalIssue{
				Message: "effective_before_phase_in_end: " + strings.TrimSpace(r.SourceCanonicalPath) + " -> " + strings.TrimSpace(r.TargetCanonicalPath),
			})
		}
	}
	// Cross-edge: IMPOSES_CONDITION target should not activate before mechanism effective (best-effort path match).
	mechEffective := map[string]time.Time{}
	condEffective := map[string]struct {
		mechHint string
		t        time.Time
	}{}
	for _, r := range rels {
		src := strings.TrimSpace(r.SourceCanonicalPath)
		tgt := strings.TrimSpace(r.TargetCanonicalPath)
		if src == "" || tgt == "" || r.Qualifiers == nil {
			continue
		}
		rt := normalizeRelationToken(r.RelationType)
		if strings.HasPrefix(src, "/mechanism/") {
			if t := parseDateish(r.Qualifiers, "effective_date", "effectiveDate"); !t.IsZero() {
				mechEffective[src] = t
			}
		}
		if rt == string(ah.RelationNameImposes) && strings.HasPrefix(src, "/mechanism/") {
			if t := parseDateish(r.Qualifiers, "effective_date", "effectiveDate"); !t.IsZero() {
				condEffective[tgt] = struct {
					mechHint string
					t        time.Time
				}{src, t}
			}
		}
	}
	for tgt, ce := range condEffective {
		if mt, ok := mechEffective[ce.mechHint]; ok && !ce.t.IsZero() && !mt.IsZero() && ce.t.Before(mt) {
			issues = append(issues, RegulatoryTemporalIssue{
				Message: "condition_effective_before_mechanism: mechanism=" + ce.mechHint + " affected=" + tgt,
			})
		}
	}
	return issues
}

func parseDateish(q map[string]interface{}, keys ...string) time.Time {
	for _, k := range keys {
		if v, ok := q[k]; ok {
			switch t := v.(type) {
			case string:
				if ts := strings.TrimSpace(t); ts != "" {
					if tt, err := time.Parse(time.RFC3339, ts); err == nil {
						return tt
					}
					if tt, err := time.Parse("2006-01-02", ts); err == nil {
						return tt
					}
				}
			}
		}
	}
	return time.Time{}
}
