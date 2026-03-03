package qa

import (
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// SnapshotToSeedCandidates extracts fragment IDs from a DomainAnswerSnapshot's critical nodes.
func SnapshotToSeedCandidates(snap *horizonmodels.DomainAnswerSnapshot) []string {
	if snap == nil {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	for _, cn := range snap.CriticalNodes {
		id := strings.TrimSpace(cn.FragmentID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// SnapshotToDataGaps maps domain snapshot data gaps to QA data gaps.
func SnapshotToDataGaps(snap *horizonmodels.DomainAnswerSnapshot) []horizonmodels.QADataGap {
	if snap == nil {
		return nil
	}
	var out []horizonmodels.QADataGap
	for _, dg := range snap.DataGaps {
		desc := strings.TrimSpace(dg.GapType)
		if h := strings.TrimSpace(dg.Hint); h != "" {
			desc += ": " + h
		}
		why := "Affects facet " + strings.TrimSpace(dg.AffectedFacet)
		if dg.AffectedNodePath != "" {
			why += " at " + strings.TrimSpace(dg.AffectedNodePath)
		}
		out = append(out, horizonmodels.QADataGap{
			Description:  desc,
			WhyItMatters: why,
		})
	}
	return out
}

// SnapshotToTracePaths extracts impact paths (canonical path sequences) from a domain snapshot.
func SnapshotToTracePaths(snap *horizonmodels.DomainAnswerSnapshot) [][]string {
	if snap == nil {
		return nil
	}
	var out [][]string
	for _, ip := range snap.ImpactPaths {
		if len(ip.NodePaths) < 2 {
			continue
		}
		path := make([]string, 0, len(ip.NodePaths))
		for _, p := range ip.NodePaths {
			p = strings.TrimSpace(p)
			if p != "" {
				path = append(path, p)
			}
		}
		if len(path) > 1 {
			out = append(out, path)
		}
	}
	return out
}

// SufficiencySignals aggregated sufficiency indicators from a domain snapshot.
type SufficiencySignals struct {
	GateScore       float64
	GateStatus      string
	NodeCount       int
	EdgeCount       int
	DataGapCount    int
	FailedCheckIDs  []string
	FacetCoverage   map[string]int
}

// SnapshotToSufficiencySignals aggregates gate pass rates and coverage from a domain snapshot.
func SnapshotToSufficiencySignals(snap *horizonmodels.DomainAnswerSnapshot) SufficiencySignals {
	if snap == nil {
		return SufficiencySignals{}
	}
	facetCoverage := make(map[string]int)
	for facet, fs := range snap.FacetSummary {
		facetCoverage[facet] = fs.NodeCount
	}
	nodeCount := 0
	for _, fs := range snap.FacetSummary {
		nodeCount += fs.NodeCount
	}
	return SufficiencySignals{
		GateScore:      snap.Gate.EvidenceCoverage,
		GateStatus:     snap.Gate.OverallStatus,
		NodeCount:      nodeCount,
		EdgeCount:      len(snap.CriticalDependencies),
		DataGapCount:   len(snap.DataGaps),
		FailedCheckIDs: snap.Gate.FailedCheckIDs,
		FacetCoverage:  facetCoverage,
	}
}
